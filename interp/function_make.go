package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
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
	// A generator/async-generator/async function object's [[Prototype]] is the
	// matching %…Function.prototype% intrinsic rather than %Function.prototype%,
	// so its .constructor chain reaches %GeneratorFunction% / etc. Arrow functions
	// are never generators/async-generators and keep %Function.prototype%.
	if kind == kindNormal {
		switch {
		case def.Generator && def.Async:
			if i.asyncGenFuncProto != nil {
				fnObj.SetProto(i.asyncGenFuncProto)
			}
		case def.Generator:
			if i.genFuncProto != nil {
				fnObj.SetProto(i.genFuncProto)
			}
		case def.Async:
			if i.asyncFuncProto != nil {
				fnObj.SetProto(i.asyncFuncProto)
			}
		}
	} else if kind == kindArrow && def.Async && i.asyncFuncProto != nil {
		fnObj.SetProto(i.asyncFuncProto)
	}

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
		// An async generator returns an async generator object whose
		// next/return/throw yield promises (see makeAsyncGenerator). Checked
		// before the plain-generator case since it is both Async and Generator.
		if def.Generator && def.Async && kind == kindNormal {
			return i.makeAsyncGenerator(fnObj, def, closure, homeObj, i.bindThisValue(this, strict), args)
		}
		// A generator function returns a generator object; its body runs
		// lazily on a dedicated goroutine (see makeGenerator).
		if def.Generator && kind == kindNormal {
			return i.makeGenerator(fnObj, def, closure, homeObj, i.bindThisValue(this, strict), args)
		}
		// An async function returns a promise driven through the microtask
		// queue (see asyncRun).
		if def.Async {
			t := this
			if kind == kindNormal {
				t = i.bindThisValue(this, strict)
			}
			return i.asyncRun(fnObj, def, closure, homeObj, t, args, kind == kindArrow)
		}
		env := NewEnvironment(closure, true)
		env.strict = strict
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
				env.vars[name] = &binding{value: fnObj, mutable: false, weakImmutable: true, initialized: true}
			}
		}
		// The arguments object is created before the parameters are bound so it is
		// visible to default-value initializers (ECMA-262 FunctionDeclaration-
		// Instantiation creates it before IteratorBindingInitialization). gojs
		// uses an unmapped snapshot, so there is no aliasing to defer. A formal
		// parameter (or lexical binding) literally named "arguments" shadows it,
		// so bindParams below overwrites the binding when such a name exists.
		if kind == kindNormal {
			env.vars["arguments"] = &binding{value: i.makeArguments(args, fnObj, strict || !simpleParameterList(def.Params)), mutable: true, initialized: true}
		}
		if err := i.bindParams(ctx, def.Params, args, env); err != nil {
			return nil, err
		}
		return i.runFunctionBody(ctx, funcFrameName(fnObj, name), def.Body, env)
	}

	length := countParams(def.Params)
	fnObj.fn = &functionData{call: call, name: name, length: length}
	setFuncLength(fnObj, length)
	setFuncNameProp(fnObj, name)

	// Ordinary (non-arrow, non-async, non-generator) functions are
	// constructable and carry a fresh .prototype object. A concise method or
	// accessor (homeObj != nil) is never a constructor and has no .prototype
	// (ECMA-262 MethodDefinitionEvaluation uses OrdinaryFunctionCreate without a
	// prototype), so it is excluded here.
	if kind == kindNormal && !def.Async && !def.Generator && homeObj == nil {
		proto := NewObject(i.objectProto)
		proto.defineOwn(StrKey("constructor"), &Property{Value: fnObj, Writable: true, Enumerable: false, Configurable: true})
		fnObj.defineOwn(StrKey("prototype"), &Property{Value: proto, Writable: true, Enumerable: false, Configurable: false})
		fnObj.fn.construct = i.makeConstruct(fnObj, call)
		fnObj.fn.ctor = true
	}

	// Generator and async-generator function instances carry an own .prototype
	// data property whose [[Prototype]] is %GeneratorPrototype% /
	// %AsyncGeneratorPrototype% (ECMA-262 §27.3/§27.4). It is
	// { writable:true, enumerable:false, configurable:false }, and unlike an
	// ordinary function's .prototype it has no "constructor" back-reference. Async
	// (non-generator) functions have no .prototype at all.
	if kind == kindNormal && def.Generator {
		instProto := i.generatorProto
		if def.Async {
			instProto = i.asyncGeneratorProto
		}
		if instProto != nil {
			proto := NewObject(instProto)
			fnObj.defineOwn(StrKey("prototype"), &Property{Value: proto, Writable: true, Enumerable: false, Configurable: false})
		}
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
	setFuncNameProp(fn, name)
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
		// Publish new.target for the body (see Interpreter.pendingNewTarget).
		if newTarget == nil {
			newTarget = fnObj
		}
		// OrdinaryCreateFromConstructor: the instance's prototype comes from
		// new.target (which differs from the invoked function under
		// Reflect.construct or a subclass), falling back to %Object.prototype%.
		protoSource := fnObj
		if nt, ok := newTarget.(*Object); ok {
			protoSource = nt
		}
		protoV, err := protoSource.GetStr(ctx, "prototype")
		if err != nil {
			return nil, err
		}
		proto, ok := protoV.(*Object)
		if !ok {
			// GetPrototypeFromConstructor falls back to new.target's realm's
			// %Object.prototype%; GetFunctionRealm throws for a revoked proxy
			// (which a "get" trap may have revoked while reading "prototype").
			if protoSource.proxy != nil && protoSource.proxy.revoked() {
				return nil, i.throwError(ctx, "TypeError", "Cannot construct with a revoked proxy as new.target")
			}
			proto = i.objectProto
		}
		self := NewObject(proto)
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
	// Pre-declare every parameter name as an uninitialized binding, so a default
	// value that references a parameter which is not yet bound (e.g. `(x = x)` or
	// `(a = b, b)`) reads it in its Temporal Dead Zone and throws a ReferenceError
	// (ECMA-262 FunctionDeclarationInstantiation instantiates the formals before
	// IteratorBindingInitialization). Names already present (arguments, or the
	// function's own name) are left untouched.
	for _, name := range collectParamNames(params) {
		if _, exists := env.vars[name]; !exists {
			env.vars[name] = &binding{mutable: true, initialized: false}
		}
	}
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

// collectParamNames returns the bound identifier names of a formal parameter
// list, recursing through defaults, rest elements, and array/object patterns.
func collectParamNames(params []ast.Expr) []string {
	var names []string
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch t := e.(type) {
		case *ast.Ident:
			names = append(names, t.Name)
		case *ast.AssignPattern:
			walk(t.Target)
		case *ast.AssignExpr:
			if t.Op == token.ASSIGN {
				walk(t.Target)
			}
		case *ast.RestElement:
			walk(t.Target)
		case *ast.ArrayLit:
			for _, el := range t.Elements {
				if el != nil {
					walk(el)
				}
			}
		case *ast.ObjectLit:
			for _, pr := range t.Properties {
				if pr.Value != nil {
					walk(pr.Value)
				} else if pr.Key != nil {
					walk(pr.Key)
				}
			}
		case *ast.SpreadElement:
			walk(t.Argument)
		}
	}
	for _, p := range params {
		walk(p)
	}
	return names
}

// makeArguments builds the arguments object for a function invocation: an
// ordinary object holding a snapshot of the actual arguments.
//
// It is deliberately NOT an Array exotic object: assigning an out-of-range index
// must not grow "length" (§10.4.4 — the arguments object has an ordinary "length"
// data property, not the Array length-coupling), and Array.isArray(arguments) is
// false. "length" is {W:true, E:false, C:true}, the indices are {W,E,C:true}, and
// @@iterator is %Array.prototype.values% so for-of and spread still work; the
// "Arguments" class drives Object.prototype.toString's [object Arguments] tag.
// Every Array.prototype method is generic over array-likes, so slice.call and
// friends reach the elements through the length + indexed-property protocol
// without dense backing.
//
// gojs does NOT implement the sloppy-mode "mapped" aliasing between arguments[i]
// and the corresponding named parameter (writes to one do not appear in the
// other). See wontfix/function-code.md for the rationale and plan. Unmapped
// behavior — where no such aliasing exists — is therefore exact.
//
// The "callee" property distinguishes unmapped from mapped arguments objects
// (§10.4.4.6/§10.4.4.7). An unmapped object — created whenever the function is
// strict OR its parameter list is not simple — exposes "callee" as a poison-pill
// accessor whose get and set are both %ThrowTypeError%; a mapped object exposes
// it as a plain data property referring to the enclosing function. gojs treats
// every arguments object as unmapped for element aliasing, but still models
// "callee" per the unmapped flag so accessing it on a strict/non-simple function
// yields %ThrowTypeError% (the canonical way test262 reaches that intrinsic).
func (i *Interpreter) makeArguments(args []Value, callee Value, unmapped bool) *Object {
	o := NewObject(i.objectProto)
	o.class = "Arguments"
	for idx, v := range args {
		o.defineOwn(StrKey(intToStr(idx)), &Property{Value: v, Writable: true, Enumerable: true, Configurable: true})
	}
	o.defineOwn(StrKey("length"), &Property{Value: Number(float64(len(args))), Writable: true, Enumerable: false, Configurable: true})
	if it, ok := i.arrayProto.getOwn(StrKey("values")); ok {
		o.defineOwn(SymKey(i.symIterator), &Property{Value: it.Value, Writable: true, Enumerable: false, Configurable: true})
	}
	if unmapped {
		if tt := i.throwTypeError; tt != nil {
			o.defineOwn(StrKey("callee"), &Property{Get: tt, Set: tt, Accessor: true, Enumerable: false, Configurable: false})
		}
	} else if callee != nil {
		o.defineOwn(StrKey("callee"), &Property{Value: callee, Writable: true, Enumerable: false, Configurable: true})
	}
	return o
}

// simpleParameterList reports whether every formal is a plain BindingIdentifier
// (no defaults, rest elements, or destructuring patterns). A non-simple list
// forces an unmapped arguments object even in sloppy mode (§10.2.11 /
// FunctionDeclarationInstantiation, "Let hasParameterExpressions ...").
func simpleParameterList(params []ast.Expr) bool {
	for _, p := range params {
		if _, ok := p.(*ast.Ident); !ok {
			return false
		}
	}
	return true
}

// runFunctionBody hoists declarations in the body and executes it, translating a
// return signal into the function's return value.
func (i *Interpreter) runFunctionBody(ctx context.Context, name string, body *ast.BlockStmt, env *Environment) (Value, error) {
	defer i.enterFrame(name)()
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
