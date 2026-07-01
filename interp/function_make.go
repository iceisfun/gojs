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
			return i.makeGenerator(def, closure, homeObj, this, args)
		}
		// An async function returns a promise driven through the microtask
		// queue (see asyncRun).
		if def.Async {
			t := this
			if kind == kindNormal && IsNullish(t) {
				t = i.global
			}
			return i.asyncRun(def, closure, homeObj, t, args, kind == kindArrow)
		}
		env := NewEnvironment(closure, true)
		if kind == kindNormal {
			// Non-strict `this` substitution: a normal function called with no
			// (or a nullish) receiver sees the global object as `this`.
			// (Methods and constructors pass a concrete receiver, so this only
			// affects plain calls like a detached `fn()`.)
			if IsNullish(this) {
				this = i.global
			}
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
			env.vars["arguments"] = &binding{value: i.makeArguments(args), mutable: true, initialized: true}
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
	return fnObj
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

// makeArguments builds a simple (array-like) arguments object.
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
