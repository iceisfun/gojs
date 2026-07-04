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
// (nil outside methods). selfBind requests the named-function-expression
// self-reference (see below); only a named function expression sets it.
func (i *Interpreter) makeFunction(def *ast.FuncDef, closure *Environment, kind funcKind, homeObj *Object, selfBind bool) *Object {
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

	// When the bytecode VM is enabled, try to compile the body once (generators
	// and async functions keep their coroutine drivers and are never compiled).
	// A nil result means the compiler declined; the tree-walker path runs instead.
	var bcCode *codeObject
	if i.useBytecode && !def.Generator && !def.Async {
		if code, ok := i.compileFunctionBody(def, strict); ok {
			bcCode = code
		}
	}

	call := func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		// PrepareForOrdinaryCall pushes an execution context whose Realm is this
		// function's [[Realm]]; tag ctx so realm-sensitive operations in the body
		// (e.g. Proxy TypeErrors) resolve against it. No-op for a same-realm call.
		ctx = i.withCurrentRealm(ctx)
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
			return i.makeAsyncGenerator(fnObj, def, closure, homeObj, i.bindThisValue(this, strict), args, selfBind)
		}
		// A generator function returns a generator object; its body runs
		// lazily on a dedicated goroutine (see makeGenerator).
		if def.Generator && kind == kindNormal {
			return i.makeGenerator(fnObj, def, closure, homeObj, i.bindThisValue(this, strict), args, selfBind)
		}
		// An async function returns a promise driven through the microtask
		// queue (see asyncRun).
		if def.Async {
			t := this
			if kind == kindNormal {
				t = i.bindThisValue(this, strict)
			}
			return i.asyncRun(fnObj, def, closure, homeObj, t, args, kind == kindArrow, selfBind)
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
		// A named function *expression* refers to itself by name through an
		// immutable binding in a dedicated environment that wraps its closure; that
		// funcEnv is established at function-creation time (see the FuncExpr case in
		// evalExpr), so no per-call self-binding is needed here. A function
		// declaration's name is instead a mutable binding in the enclosing scope.
		_ = selfBind
		// Slot-eligible compiled body: params and vars live in frame slots, so skip
		// the arguments object, parameter binding into env, and var hoisting — the
		// slot prologue handles them. env is kept (this/globals/self-name).
		if bcCode != nil && bcCode.numSlots > 0 {
			return i.runFunctionBodyBCSlots(ctx, funcFrameName(fnObj, name), bcCode, env, args)
		}
		// The arguments object is created before the parameters are bound so it is
		// visible to default-value initializers (ECMA-262 FunctionDeclaration-
		// Instantiation creates it before IteratorBindingInitialization). A formal
		// parameter (or lexical binding) literally named "arguments" shadows it,
		// so bindParams below overwrites the binding when such a name exists.
		//
		// A sloppy-mode function with a simple parameter list gets a *mapped*
		// arguments object (§10.4.4.6): arguments[i] aliases the i-th named
		// parameter. The alias is wired only after bindParams has created the
		// parameter bindings, so the map references the same *binding the body
		// reads and writes. A strict or non-simple-parameter function gets an
		// unmapped snapshot (§10.4.4.7) with no aliasing.
		mapped := kind == kindNormal && !strict && simpleParameterList(def.Params)
		var argsObj *Object
		if kind == kindNormal {
			argsObj = i.makeArguments(args, fnObj, !mapped)
			env.vars["arguments"] = &binding{value: argsObj, mutable: true, initialized: true}
		}
		if err := i.bindParams(ctx, def.Params, args, env); err != nil {
			return nil, err
		}
		// Wire the parameter map only if the arguments binding still holds the
		// object we created (a parameter literally named "arguments" would have
		// replaced it, in which case no arguments object is observable).
		if mapped {
			if b, ok := env.vars["arguments"]; ok && b.value == argsObj {
				i.mapArguments(argsObj, def.Params, args, env)
			}
		}
		bodyEnv := bodyVarEnv(def.Params, def.Body, env)
		if bcCode != nil {
			return i.runFunctionBodyBC(ctx, funcFrameName(fnObj, name), def.Body, bcCode, bodyEnv)
		}
		return i.runFunctionBody(ctx, funcFrameName(fnObj, name), def.Body, bodyEnv)
	}

	length := countParams(def.Params)
	fnObj.fn = &functionData{call: call, name: name, length: length, realm: i, source: def.Source}
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
			// GetPrototypeFromConstructor step 4: the default %Object.prototype%
			// comes from new.target's own realm. GetFunctionRealm throws for a
			// revoked proxy (which a "get" trap may have revoked while reading
			// "prototype").
			realm, err := i.getFunctionRealm(ctx, protoSource)
			if err != nil {
				return nil, err
			}
			proto = realm.objectProto
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
	// While binding parameters, a direct eval in a default value runs with this
	// parameter environment as its lexical scope but the enclosing scope as its
	// variable environment, which governs the EvalDeclarationInstantiation
	// var-hoisting early error (see Interpreter.paramDefaultEnv). Restore the
	// previous value afterward so a nested function's own parameter binding, or
	// the body that follows, is unaffected.
	savedParamEnv := i.paramDefaultEnv
	i.paramDefaultEnv = env
	defer func() { i.paramDefaultEnv = savedParamEnv }()
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
// This builds only the object skeleton and its data properties. The sloppy-mode
// "mapped" aliasing between arguments[i] and the corresponding named parameter
// (§10.4.4.6) is layered on afterwards by mapArguments, once the parameter
// bindings exist; see the [[ParameterMap]] handling in getOwn / setStatus /
// Delete / argumentsDefineOwn.
//
// The "callee" property distinguishes unmapped from mapped arguments objects
// (§10.4.4.6/§10.4.4.7). An unmapped object — created whenever the function is
// strict OR its parameter list is not simple — exposes "callee" as a poison-pill
// accessor whose get and set are both %ThrowTypeError%; a mapped object exposes
// it as a plain data property referring to the enclosing function.
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

// mapArguments installs the [[ParameterMap]] of a mapped arguments object
// (CreateMappedArgumentsObject, §10.4.4.6 steps 19–22): scanning the simple
// formal parameter list from last to first, it links each index that has a
// corresponding argument to the *binding of its parameter name. A duplicated
// name (legal in sloppy mode) maps only its last occurrence, since an earlier
// index with the same name is skipped once the name has been seen. The bindings
// are the very ones bindParams installed, so arguments[i] and the named
// parameter alias each other through shared pointer identity.
func (i *Interpreter) mapArguments(o *Object, params []ast.Expr, args []Value, env *Environment) {
	seen := make(map[string]bool)
	for idx := len(params) - 1; idx >= 0; idx-- {
		id, ok := params[idx].(*ast.Ident)
		if !ok {
			continue // a non-simple list never reaches here; guard defensively
		}
		name := id.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		if idx >= len(args) {
			continue // no corresponding argument: index is unmapped
		}
		b, ok := env.vars[name]
		if !ok {
			continue
		}
		if o.paramMap == nil {
			o.paramMap = make(map[string]*binding, len(params))
		}
		o.paramMap[intToStr(idx)] = b
	}
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

// hasParameterExpressions reports whether a formal parameter list "contains an
// expression" in the sense of ECMA-262 ContainsExpression: a default
// initializer, or a computed property key inside a destructuring pattern. Rest
// elements and plain (nested) BindingIdentifiers do not count. When true, the
// function body runs in a VariableEnvironment separate from the parameter
// environment (§10.2.11 step 27).
func hasParameterExpressions(params []ast.Expr) bool {
	var walk func(ast.Expr) bool
	walk = func(e ast.Expr) bool {
		switch t := e.(type) {
		case *ast.AssignPattern:
			return true
		case *ast.AssignExpr:
			if t.Op == token.ASSIGN {
				return true
			}
		case *ast.RestElement:
			return walk(t.Target)
		case *ast.ArrayLit:
			for _, el := range t.Elements {
				if el != nil && walk(el) {
					return true
				}
			}
		case *ast.ObjectLit:
			for _, pr := range t.Properties {
				if pr.Computed {
					return true
				}
				if pr.Value != nil && walk(pr.Value) {
					return true
				}
			}
		case *ast.SpreadElement:
			return walk(t.Argument)
		}
		return false
	}
	for _, p := range params {
		if walk(p) {
			return true
		}
	}
	return false
}

// bodyVarEnv returns the environment in which a function body's var/function
// declarations and top-level statements execute. When the parameter list has
// parameter expressions, the body gets a fresh VariableEnvironment that is a
// child of the parameter environment (§10.2.11 step 27), so a parameter
// default's closure and the body observe distinct bindings for a name the body
// re-declares with `var`. Parameter values are copied in for any var-declared
// name that shadows a parameter (function-declared names are excluded — they are
// (re)bound to their function objects when the body is instantiated). When there
// are no parameter expressions, the parameter environment is reused as-is.
func bodyVarEnv(params []ast.Expr, body *ast.BlockStmt, paramEnv *Environment) *Environment {
	if body == nil || !hasParameterExpressions(params) {
		return paramEnv
	}
	varEnv := NewEnvironment(paramEnv, true)
	paramNames := map[string]bool{}
	for _, n := range collectParamNames(params) {
		paramNames[n] = true
	}
	varNames := map[string]bool{}
	collectVarNames(body.Body, varNames)
	for n := range varNames {
		if paramNames[n] {
			if pb := paramEnv.vars[n]; pb != nil {
				varEnv.vars[n] = &binding{value: pb.value, mutable: true, initialized: true}
			}
		}
	}
	return varEnv
}

// runFunctionBody hoists declarations in the body and executes it, translating a
// return signal into the function's return value.
func (i *Interpreter) runFunctionBody(ctx context.Context, name string, body *ast.BlockStmt, env *Environment) (Value, error) {
	defer i.enterFrame(name)()
	// A direct eval in this body must not observe an enclosing function's
	// parameter-default context (which governs a var-hoisting early error), so
	// clear it while the body runs. This matters when the body executes during an
	// outer parameter default (e.g. `f(p = (function(){ eval(...) })())`).
	savedParamEnv := i.paramDefaultEnv
	i.paramDefaultEnv = nil
	defer func() { i.paramDefaultEnv = savedParamEnv }()
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

// runFunctionBodyBC is runFunctionBody for a body compiled to bytecode: it does
// the same frame/paramEnv bookkeeping and function-level hoisting, then runs the
// compiled code on the stack VM. execCode returns the completion value directly
// (a `return` yields its value; falling off the end yields undefined), so no
// returnSignal translation is needed here.
func (i *Interpreter) runFunctionBodyBC(ctx context.Context, name string, body *ast.BlockStmt, code *codeObject, env *Environment) (Value, error) {
	defer i.enterFrame(name)()
	savedParamEnv := i.paramDefaultEnv
	i.paramDefaultEnv = nil
	defer func() { i.paramDefaultEnv = savedParamEnv }()
	if err := i.hoistDeclarations(ctx, body.Body, env, true); err != nil {
		return nil, err
	}
	return i.execCode(ctx, code, env, nil)
}

// runFunctionBodyBCSlots runs a slot-eligible compiled body: locals live in a
// frame array (params bound by position, remaining slots var-hoisted to
// undefined) instead of the environment, so there is no per-binding map work and
// no arguments object. The env is still present for `this`, globals, and a named
// function expression's self-reference.
func (i *Interpreter) runFunctionBodyBCSlots(ctx context.Context, name string, code *codeObject, env *Environment, args []Value) (Value, error) {
	defer i.enterFrame(name)()
	savedParamEnv := i.paramDefaultEnv
	i.paramDefaultEnv = nil
	defer func() { i.paramDefaultEnv = savedParamEnv }()
	locals := make([]Value, code.numSlots)
	for j := range locals {
		locals[j] = Undef // var hoisting: every slot starts undefined
	}
	// Bind parameters by position; a duplicated name's later position wins because
	// paramSlots maps both positions to the same slot. Bind UNCONDITIONALLY (undef
	// when the caller supplied no argument) so that for `function f(x,a,b,x)` the
	// last x with no argument overrides an earlier x that did receive one.
	for pos, slot := range code.paramSlots {
		if pos < len(args) {
			locals[slot] = args[pos]
		} else {
			locals[slot] = Undef
		}
	}
	return i.execCode(ctx, code, env, locals)
}
