package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// This file builds callable objects from AST function definitions, wiring the
// closure environment, `this`/arguments/new.target binding, parameter
// destructuring, and construct behavior.

// funcKind distinguishes the callable forms that differ in `this` handling.
type funcKind int

const (
	kindNormal funcKind = iota // function declarations/expressions and methods
	kindArrow                  // arrow functions: lexical this/arguments
)

// makeFunction constructs a callable object from a function definition captured
// in the closure environment. homeObj is the [[HomeObject]] for super lookups
// (nil outside methods).
func (i *Interpreter) makeFunction(def *ast.FuncDef, closure *Environment, kind funcKind, homeObj *Object) *Object {
	name := ""
	if def.Name != nil {
		name = def.Name.Name
	}
	fnObj := NewObject(i.functionProto)
	fnObj.class = "Function"

	// A function is strict if its body carries "use strict" or it is lexically
	// nested in strict code; modules force strict globally.
	strict := def.Strict || i.forceStrict()

	call := func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		if err := i.enterCall(); err != nil {
			return nil, err
		}
		defer i.leaveCall()
		// Consume any pending new.target set by a [[Construct]] call. A plain call
		// leaves it nil, so new.target reads as undefined inside the body.
		nt := i.pendingNewTarget
		i.pendingNewTarget = nil
		// A generator function returns a generator object; its body runs
		// lazily on a dedicated goroutine (see makeGenerator).
		if def.Generator && kind == kindNormal {
			return i.makeGenerator(def, closure, homeObj, i.bindThisValue(this, strict), args)
		}
		// An async function returns a promise driven through the microtask
		// queue (see asyncRun).
		if def.Async {
			t := this
			if kind == kindNormal {
				t = i.bindThisValue(this, strict)
			}
			return i.asyncRun(def, closure, homeObj, t, args, kind == kindArrow)
		}
		env := NewEnvironment(closure, true)
		if kind == kindNormal {
			// OrdinaryCallBindThis: in strict mode `this` is passed through
			// unchanged (undefined stays undefined); in sloppy mode a nullish
			// receiver becomes the global object and a primitive receiver is
			// boxed to its wrapper object. A [[Construct]] call already supplies
			// the fresh object, which bindThisValue leaves untouched.
			this = i.bindThisValue(this, strict)
			env.setThis(this)
			if homeObj != nil {
				env.homeObj = homeObj
			}
			// new.target: the constructor when invoked via `new`, else undefined.
			if nt != nil {
				env.newTgt = nt
			} else {
				env.newTgt = Undef
			}
		}
		// A named function expression can refer to itself by name.
		if def.Name != nil && kind == kindNormal {
			if _, exists := closure.vars[name]; !exists {
				env.vars[name] = &binding{value: fnObj, mutable: false, initialized: true}
			}
		}
		if err := i.bindParams(ctx, def.Params, args, env); err != nil {
			return nil, err
		}
		// The arguments object is created after the parameters are bound so a
		// mapped arguments object can alias their bindings. A formal parameter
		// (or lexical binding) literally named "arguments" shadows it.
		if kind == kindNormal {
			if _, exists := env.vars["arguments"]; !exists {
				env.vars["arguments"] = &binding{value: i.makeArguments(args), mutable: true, initialized: true}
			}
		}
		return i.runFunctionBody(ctx, def.Body, env)
	}

	length := countParams(def.Params)
	fnObj.fn = &functionData{call: call, name: name, length: length}
	fnObj.SetHidden("length", Number(float64(length)))
	fnObj.SetHidden("name", String(name))

	// Ordinary (non-arrow, non-async, non-generator) functions are
	// constructable and carry a fresh .prototype object.
	if kind == kindNormal && !def.Async && !def.Generator {
		proto := NewObject(i.objectProto)
		proto.defineOwn(StrKey("constructor"), &Property{Value: fnObj, Writable: true, Enumerable: false, Configurable: true})
		fnObj.defineOwn(StrKey("prototype"), &Property{Value: proto, Writable: true, Enumerable: false, Configurable: false})
		fnObj.fn.construct = i.makeConstruct(fnObj, call)
		fnObj.fn.ctor = true
	}

	// Annex B legacy "caller"/"arguments": only sloppy plain function
	// declarations/expressions get own accessors (returning null). Strict
	// functions, generators, async functions, methods (homeObj != nil), arrows,
	// and bound functions instead inherit the poison pills from
	// %Function.prototype%.
	if kind == kindNormal && !strict && !def.Generator && !def.Async && homeObj == nil && i.legacyNullGetter != nil {
		g := i.legacyNullGetter
		fnObj.defineOwn(StrKey("caller"), &Property{Get: g, Accessor: true, Enumerable: false, Configurable: true})
		fnObj.defineOwn(StrKey("arguments"), &Property{Get: g, Accessor: true, Enumerable: false, Configurable: true})
	}
	return fnObj
}

// setFuncName gives an (anonymous) function object its inferred name, updating
// both the internal slot and the observable "name" own property. prefix is
// "get"/"set" for accessors and empty otherwise. It is a no-op for a function
// that already carries a name.
func (i *Interpreter) setFuncName(fn *Object, key PropertyKey, prefix string) {
	if fn == nil || fn.fn == nil || fn.fn.name != "" {
		return
	}
	var base string
	if key.IsSymbol() {
		if key.Sym.Desc != "" {
			base = "[" + key.Sym.Desc + "]"
		}
	} else {
		base = key.Str
	}
	name := base
	if prefix != "" {
		name = prefix + " " + base
	}
	fn.fn.name = name
	fn.SetHidden("name", String(name))
}

// bindThisValue implements the OrdinaryCallBindThis coercion for a normal
// (non-arrow) function call. In strict mode the receiver is used verbatim. In
// sloppy mode a nullish receiver becomes the global object and any primitive is
// boxed to its wrapper object (so `typeof this` is "object"). Objects (including
// the fresh instance of a [[Construct]] call) pass through unchanged.
func (i *Interpreter) bindThisValue(this Value, strict bool) Value {
	if strict {
		return this
	}
	if IsNullish(this) {
		return i.global
	}
	if _, ok := this.(*Object); ok {
		return this
	}
	obj, err := i.ToObject(i.ctx, this)
	if err != nil {
		return this
	}
	return obj
}

// makeConstruct builds the [[Construct]] behavior for an ordinary function:
// create an object whose prototype is the function's .prototype, run the body
// with it as `this`, and return the body's object result or the new object.
func (i *Interpreter) makeConstruct(fnObj *Object, call CallFn) CallFn {
	return func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		protoV, _ := fnObj.GetStr(ctx, "prototype")
		proto, ok := protoV.(*Object)
		if !ok {
			proto = i.objectProto
		}
		self := NewObject(proto)
		// Publish new.target for the body (see Interpreter.pendingNewTarget).
		if newTarget == nil {
			newTarget = fnObj
		}
		i.pendingNewTarget = newTarget
		result, err := call(ctx, self, args)
		if err != nil {
			return nil, err
		}
		if obj, ok := result.(*Object); ok {
			return obj, nil
		}
		return self, nil
	}
}

// bindParams binds actual arguments to a function's formal parameters,
// destructuring patterns and gathering rest parameters.
func (i *Interpreter) bindParams(ctx context.Context, params []ast.Expr, args []Value, env *Environment) error {
	declare := func(name string, v Value) {
		env.vars[name] = &binding{value: v, mutable: true, initialized: true}
	}
	idx := 0
	for _, param := range params {
		if rest, ok := param.(*ast.RestElement); ok {
			var remaining []Value
			if idx < len(args) {
				remaining = append(remaining, args[idx:]...)
			}
			if err := i.bindPattern(ctx, rest.Target, i.newArray(remaining), env, declare); err != nil {
				return err
			}
			return nil
		}
		var v Value = Undef
		if idx < len(args) {
			v = args[idx]
		}
		if err := i.bindPattern(ctx, param, v, env, declare); err != nil {
			return err
		}
		idx++
	}
	return nil
}

// makeArguments builds the arguments object for a function invocation: an
// array-like snapshot of the actual arguments.
//
// gojs backs the arguments object with an Array so the ubiquitous generic
// idioms (Array.prototype.slice.call(arguments), for-of, spread) work directly.
// It does NOT implement the sloppy-mode "mapped" aliasing between arguments[i]
// and the corresponding named parameter (writes to one do not appear in the
// other). See wontfix/function-code.md for the rationale and plan. Strict-mode
// (unmapped) behavior — where no such aliasing exists — is therefore exact.
func (i *Interpreter) makeArguments(args []Value) *Object {
	o := i.newArray(append([]Value{}, args...))
	o.class = "Arguments"
	return o
}

// runFunctionBody hoists declarations in the body and executes it, translating a
// return signal into the function's return value.
func (i *Interpreter) runFunctionBody(ctx context.Context, body *ast.BlockStmt, env *Environment) (Value, error) {
	if err := i.hoistDeclarations(ctx, body.Body, env, true); err != nil {
		return nil, err
	}
	_, err := i.execStmts(ctx, body.Body, env)
	if err != nil {
		if ret, ok := err.(*returnSignal); ok {
			return ret.value, nil
		}
		return nil, err
	}
	return Undef, nil
}
