package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// This file implements class evaluation: building the constructor function, the
// prototype with methods and accessors, static members, instance fields, and
// super support.

// classData is stashed in a class constructor's internal slots so instance
// construction can initialize fields and locate the parent constructor.
type classData struct {
	def            *ast.ClassDef
	env            *Environment
	proto          *Object
	superCtor      *Object // parent constructor (nil when no extends, or `extends null`)
	derived        bool    // ClassHeritage present (`extends X` or `extends null`)
	fieldInits     []*ast.ClassMember
	privateMethods []*ast.ClassMember // instance #methods and private accessors
	// computedKeys holds each computed member's property key, evaluated once at
	// class-definition time (ECMA-262 evaluates ClassElementName when the class is
	// defined, not per instance), so instance-field keys are not re-evaluated.
	computedKeys map[*ast.ClassMember]PropertyKey
	// sharedPrivate holds this class's instance private methods and accessors,
	// created once at class-definition time and installed by reference on every
	// instance (ECMA-262: private methods/accessors are defined once and shared,
	// so the same function object is observed across all instances).
	sharedPrivate map[*PrivateName]*Property
}

// evalClass evaluates a class definition to its constructor object. inferredName
// is the NamedEvaluation name for an anonymous class expression (e.g. "C" in
// `const C = class {}`); it is applied as the class name so it is observable
// (via this.name) inside static field initializers and static blocks, which run
// during this evaluation (ClassDefinitionEvaluation sets the name at step 18,
// before running static elements).
func (i *Interpreter) evalClass(ctx context.Context, def *ast.ClassDef, env *Environment, inferredName string) (Value, error) {
	// The class body runs in its own scope so the class name is in scope inside
	// methods (for recursion) and so `extends` can be evaluated.
	classEnv := NewEnvironment(env, false)

	// Mint a fresh PrivateName identity for each distinct private element this
	// class declares. Methods, accessors, and field initializers defined in this
	// evaluation capture classEnv, so they resolve #names to these identities;
	// a separate evaluation of the same class mints different identities, so
	// their instances fail one another's brand checks.
	for _, m := range def.Members {
		if priv, ok := m.Key.(*ast.PrivateIdent); ok {
			if classEnv.privNames == nil {
				classEnv.privNames = make(map[string]*PrivateName)
			}
			if _, exists := classEnv.privNames[priv.Name]; !exists {
				classEnv.privNames[priv.Name] = &PrivateName{desc: priv.Name}
			}
		}
	}

	// The class name is an immutable binding in the class scope, created
	// uninitialized (in its Temporal Dead Zone) before the heritage expression is
	// evaluated, and evaluated *in that scope* (§15.7.14 ClassDefinitionEvaluation
	// steps 2-6): so `class x extends x {}` reads `x` in TDZ (a ReferenceError),
	// and a heritage closure captures this inner binding independently of any
	// same-named outer binding.
	if def.Name != nil {
		classEnv.vars[def.Name.Name] = &binding{mutable: false, initialized: false, lexical: true}
	}

	var superCtor *Object
	protoParent := i.objectProto
	if def.SuperClass != nil {
		sv, err := i.evalExpr(ctx, def.SuperClass, classEnv)
		if err != nil {
			return nil, err
		}
		if IsNull(sv) {
			protoParent = nil
		} else {
			sc, ok := sv.(*Object)
			if !ok || !sc.IsConstructor() {
				return nil, i.throwError(ctx, "TypeError", "Class extends value is not a constructor or null")
			}
			superCtor = sc
			protoV, err := sc.GetStr(ctx, "prototype")
			if err != nil {
				return nil, err
			}
			// The superclass's .prototype must be an Object or null; any other
			// value (e.g. a getter that returns a primitive) is a TypeError
			// (ECMA-262 ClassDefinitionEvaluation step: protoParent).
			switch pv := protoV.(type) {
			case *Object:
				protoParent = pv
			case Null:
				protoParent = nil
			default:
				return nil, i.throwError(ctx, "TypeError", "Class extends value does not have valid prototype property")
			}
		}
	}

	proto := NewObject(protoParent)
	cd := &classData{def: def, env: classEnv, proto: proto, superCtor: superCtor, derived: def.SuperClass != nil}

	// Find an explicit constructor method.
	var ctorDef *ast.FuncDef
	for _, m := range def.Members {
		if !m.Static && !m.Field && m.Kind == ast.PropInit {
			if id, ok := m.Key.(*ast.Ident); ok && id.Name == "constructor" {
				ctorDef = m.Value.(*ast.FuncExpr).Def
			}
		}
		if m.Field && !m.Static {
			cd.fieldInits = append(cd.fieldInits, m)
		}
		// Instance private methods and private accessors are installed per
		// instance (they belong to the object's private brand, not the shared
		// prototype).
		if !m.Field && !m.Static {
			if _, ok := m.Key.(*ast.PrivateIdent); ok {
				cd.privateMethods = append(cd.privateMethods, m)
			}
		}
	}

	className := inferredName
	if def.Name != nil {
		className = def.Name.Name
	}
	ctor := i.makeClassConstructor(def, cd, ctorDef, classEnv, proto, className)
	ctor.internal = map[string]any{"class": cd}
	if superCtor != nil {
		ctor.SetProto(superCtor)
	}
	proto.defineOwn(StrKey("constructor"), &Property{Value: ctor, Writable: true, Enumerable: false, Configurable: true})
	ctor.defineOwn(StrKey("prototype"), &Property{Value: proto, Writable: false, Enumerable: false, Configurable: false})

	// InitializeBinding: the class name binding (created uninitialized above) is
	// now bound to the constructor, so methods can self-reference the class.
	if def.Name != nil {
		if b := classEnv.vars[def.Name.Name]; b != nil {
			b.value = ctor
			b.initialized = true
		}
	}

	// Evaluate every computed element name exactly once, in source order, at
	// class-definition time. A computed name that throws, or that evaluates to a
	// value whose ToPropertyKey fails (e.g. a private-brand miss), is an error
	// raised here rather than per instance.
	for _, m := range def.Members {
		if !m.Computed {
			continue
		}
		v, err := i.evalExpr(ctx, m.Key, classEnv)
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, v)
		if err != nil {
			return nil, err
		}
		if cd.computedKeys == nil {
			cd.computedKeys = make(map[*ast.ClassMember]PropertyKey)
		}
		cd.computedKeys[m] = key
	}

	// Build the shared instance private methods/accessors once (see sharedPrivate).
	if err := i.buildSharedPrivate(ctx, cd, classEnv, proto); err != nil {
		return nil, err
	}

	// Install methods, accessors, and static members.
	for _, m := range def.Members {
		if m.StaticBlock != nil {
			if err := i.runStaticBlock(ctx, ctor, m, classEnv); err != nil {
				return nil, err
			}
			continue
		}
		if m.Field {
			if m.Static {
				if err := i.initStaticField(ctx, cd, ctor, m, classEnv); err != nil {
					return nil, err
				}
			}
			continue
		}
		if id, ok := m.Key.(*ast.Ident); ok && id.Name == "constructor" && !m.Static {
			continue // handled as the constructor
		}
		// Instance private methods/accessors are installed per instance during
		// construction, not here.
		if _, isPriv := m.Key.(*ast.PrivateIdent); isPriv && !m.Static {
			continue
		}
		target := proto
		home := proto
		if m.Static {
			target = ctor
			home = ctor
		}
		if _, isPriv := m.Key.(*ast.PrivateIdent); isPriv && m.Static {
			// Static private methods/accessors live in the constructor's private
			// storage (home object is the constructor for super lookups).
			if err := i.installPrivateMember(ctx, ctor, ctor, m, classEnv); err != nil {
				return nil, err
			}
			continue
		}
		if err := i.installClassMethod(ctx, cd, target, home, m, classEnv); err != nil {
			return nil, err
		}
	}

	return ctor, nil
}

// makeClassConstructor builds the constructor callable for a class.
func (i *Interpreter) makeClassConstructor(def *ast.ClassDef, cd *classData, ctorDef *ast.FuncDef, classEnv *Environment, proto *Object, name string) *Object {
	fnObj := NewObject(i.functionProto)
	fnObj.class = "Function"

	construct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		if err := i.enterCall(); err != nil {
			return nil, err
		}
		defer i.leaveCall()
		// new.target is the constructor originally invoked with `new` (propagated
		// unchanged down a super() chain). Default to undefined for safety.
		if newTarget == nil {
			newTarget = Undef
		}
		env := NewEnvironment(classEnv, true)
		env.homeObj = proto
		env.newTgt = newTarget
		// A class constructor (like any ordinary function) has an `arguments`
		// object. Class code is strict, so it is unmapped; a formal named
		// "arguments" is a SyntaxError, so there is never a shadowing conflict.
		env.vars["arguments"] = &binding{value: i.makeArguments(args, nil, true), mutable: true, initialized: true}

		// -------------------------------------------------------------------
		// Base class (no ClassHeritage): `this` is created here from
		// new.target.prototype (OrdinaryCreateFromConstructor), then fields and
		// private elements are installed before the body runs.
		// -------------------------------------------------------------------
		if !cd.derived {
			instProto := proto
			if nt, ok := newTarget.(*Object); ok {
				if pv, _ := nt.GetStr(ctx, "prototype"); pv != nil {
					if po, ok := pv.(*Object); ok {
						instProto = po
					}
				}
			}
			self := NewObject(instProto)
			env.setThis(self)
			if err := i.installInstancePrivateMethods(ctx, self, cd); err != nil {
				return nil, err
			}
			if err := i.initInstanceFields(ctx, self, cd, env); err != nil {
				return nil, err
			}
			var result Value = self
			if ctorDef != nil {
				if err := i.bindParams(ctx, ctorDef.Params, args, env); err != nil {
					return nil, err
				}
				ret, err := i.runConstructorBody(ctx, "new "+frameName(name), ctorDef.Body, env)
				if err != nil {
					return nil, err
				}
				// A base class ignores a primitive/undefined return (§10.2.2 step
				// 13b); only an explicit object return replaces `this`.
				if obj, ok := ret.(*Object); ok {
					result = obj
				}
			}
			return result, nil
		}

		// -------------------------------------------------------------------
		// Derived class (`extends X` or `extends null`): `this` is uninitialized
		// until super() binds it to the object the parent [[Construct]] returns.
		// Reading `this` before super() is a ReferenceError (getThisBinding on the
		// uninitialized environment). The active function object is recorded so
		// GetSuperConstructor reads its current [[Prototype]] dynamically
		// (§13.3.7.1 SuperCall / GetSuperConstructor).
		// -------------------------------------------------------------------
		env.superInit = &superInitState{}
		env.setThis(Undef) // marks the this-scope; superInit.called governs uninitialized
		env.vars["%activefunc%"] = &binding{value: fnObj, mutable: false, initialized: true}
		env.vars["%superctor%"] = &binding{value: cd.superCtor, mutable: false, initialized: true}
		env.vars["%fieldinit%"] = &binding{value: i.fieldInitThunk(cd, env), mutable: false, initialized: true}

		if ctorDef != nil {
			if err := i.bindParams(ctx, ctorDef.Params, args, env); err != nil {
				return nil, err
			}
			ret, err := i.runConstructorBody(ctx, "new "+frameName(name), ctorDef.Body, env)
			if err != nil {
				return nil, err
			}
			// §10.2.2 [[Construct]] pops the callee (this constructor's realm)
			// execution context at step 9, so the post-body steps run in the
			// *caller's* realm: an explicit object return replaces `this`; a
			// non-undefined non-object return is a TypeError (step 11d); an
			// undefined return yields GetThisBinding, a ReferenceError when super()
			// never ran (step 13). Both errors therefore come from the running
			// (caller) realm, not this constructor's realm.
			if obj, ok := ret.(*Object); ok {
				return obj, nil
			}
			caller := i
			if cur := currentRealm(ctx); cur != nil {
				caller = cur
			}
			if !IsUndefined(ret) {
				return nil, caller.throwError(ctx, "TypeError",
					"Derived constructors may only return an object or undefined")
			}
			return caller.getThisBinding(ctx, env)
		}

		// Default derived constructor: `constructor(...args) { super(...args); }`.
		// GetSuperConstructor is the active function's [[Prototype]]; for
		// `extends null` it is %Function.prototype%, which is not a constructor.
		superCtor := fnObj.proto
		if superCtor == nil || !superCtor.IsConstructor() {
			return nil, i.throwError(ctx, "TypeError", "Super constructor is not a constructor")
		}
		result, err := superCtor.fn.construct(ctx, newTarget, args)
		if err != nil {
			return nil, err
		}
		self, ok := result.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Super constructor returned a non-object")
		}
		env.setThis(self)
		env.superInit.called = true
		if err := i.installInstancePrivateMethods(ctx, self, cd); err != nil {
			return nil, err
		}
		if err := i.initInstanceFields(ctx, self, cd, env); err != nil {
			return nil, err
		}
		return self, nil
	}

	fnObj.fn = &functionData{
		call: func(ctx context.Context, this Value, args []Value) (Value, error) {
			return nil, i.throwError(ctx, "TypeError", "Class constructor "+name+" cannot be invoked without 'new'")
		},
		construct: construct,
		name:      name,
		length:    0,
		ctor:      true,
		realm:     i,
	}
	if ctorDef != nil {
		fnObj.fn.length = countParams(ctorDef.Params)
	}
	setFuncLength(fnObj, fnObj.fn.length)
	setFuncNameProp(fnObj, name)
	return fnObj
}

// fieldInitThunk returns a native function that installs this class's private
// elements and initializes its instance fields on the constructor's current
// `this` binding. It is invoked by super() after `this` has been bound to the
// object the parent [[Construct]] returned, so it reads `this` from ctorEnv at
// call time rather than capturing a pre-created instance.
func (i *Interpreter) fieldInitThunk(cd *classData, ctorEnv *Environment) *Object {
	return i.newNativeFunc("%fieldinit%", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		tv, _ := ctorEnv.thisBinding()
		self, ok := tv.(*Object)
		if !ok {
			return Undef, nil
		}
		if err := i.installInstancePrivateMethods(ctx, self, cd); err != nil {
			return Undef, err
		}
		return Undef, i.initInstanceFields(ctx, self, cd, ctorEnv)
	})
}

// fieldFuncName returns the name a class field's anonymous function/class
// initializer receives under NamedEvaluation (§15.7.10 ClassFieldDefinition-
// Evaluation): a private field #x names it "#x", a symbol key names it "[desc]"
// (or "" for a descriptionless symbol), and any other key uses the key string.
func fieldFuncName(m *ast.ClassMember, key PropertyKey) string {
	if priv, ok := m.Key.(*ast.PrivateIdent); ok {
		return priv.Name
	}
	if key.IsSymbol() {
		if key.Sym.Desc != "" {
			return "[" + key.Sym.Desc + "]"
		}
		return ""
	}
	return key.Str
}

// initInstanceFields evaluates and assigns instance field initializers.
func (i *Interpreter) initInstanceFields(ctx context.Context, self *Object, cd *classData, env *Environment) error {
	for _, m := range cd.fieldInits {
		key, err := i.classMemberKey(ctx, cd, m, env)
		if err != nil {
			return err
		}
		var v Value = Undef
		if m.Value != nil {
			fieldEnv := NewEnvironment(cd.env, true)
			fieldEnv.setThis(self)
			fieldEnv.homeObj = cd.proto
			fieldEnv.fieldInit = true
			// A field initializer is a function context: new.target is bound
			// (value undefined, since it is [[Call]]ed), so `new.target` — and a
			// direct eval that mentions it — is valid inside the initializer.
			fieldEnv.newTgt = Undef
			v, err = i.evalExprWithName(ctx, m.Value, fieldEnv, fieldFuncName(m, key))
			if err != nil {
				return err
			}
		}
		if priv, ok := m.Key.(*ast.PrivateIdent); ok {
			if !self.extensible {
				return i.throwError(ctx, "TypeError",
					"Cannot add private field "+priv.Name+" to a non-extensible object")
			}
			self.definePrivate(cd.env.resolvePrivate(priv.Name), &Property{Value: v, Writable: true})
			continue
		}
		if err := i.defineFieldOrThrow(ctx, self, key, v); err != nil {
			return err
		}
	}
	return nil
}

// defineFieldOrThrow creates a public class field with CreateDataPropertyOrThrow
// semantics (§sec-define-field DefineField step 9): it routes through the
// object's [[DefineOwnProperty]] so a Proxy receiver's defineProperty trap runs
// and it is observable, and a refusal — e.g. a non-extensible receiver (a field
// initializer froze `this`) — is a TypeError.
func (i *Interpreter) defineFieldOrThrow(ctx context.Context, obj *Object, key PropertyKey, v Value) error {
	return i.createDataPropertyOrThrow(ctx, obj, key, v)
}

// initStaticField evaluates a static field initializer on the constructor.
func (i *Interpreter) initStaticField(ctx context.Context, cd *classData, ctor *Object, m *ast.ClassMember, classEnv *Environment) error {
	key, err := i.classMemberKey(ctx, cd, m, classEnv)
	if err != nil {
		return err
	}
	if err := i.forbidStaticPrototypeKey(ctx, m, key); err != nil {
		return err
	}
	var v Value = Undef
	if m.Value != nil {
		env := NewEnvironment(classEnv, true)
		env.setThis(ctor)
		env.homeObj = ctor
		env.fieldInit = true
		v, err = i.evalExprWithName(ctx, m.Value, env, fieldFuncName(m, key))
		if err != nil {
			return err
		}
	}
	if priv, ok := m.Key.(*ast.PrivateIdent); ok {
		if !ctor.extensible {
			return i.throwError(ctx, "TypeError",
				"Cannot add private field "+priv.Name+" to a non-extensible object")
		}
		ctor.definePrivate(classEnv.resolvePrivate(priv.Name), &Property{Value: v, Writable: true})
		return nil
	}
	return i.defineFieldOrThrow(ctx, ctor, key, v)
}

// runStaticBlock evaluates a `static { ... }` initialization block at class
// definition time. Its `this` is the constructor, new.target is undefined, and
// the class name is in scope (via classEnv). It has its own function-like var
// scope (ECMA-262 EvaluateStaticBlock).
func (i *Interpreter) runStaticBlock(ctx context.Context, ctor *Object, m *ast.ClassMember, classEnv *Environment) error {
	if err := i.enterCall(); err != nil {
		return err
	}
	defer i.leaveCall()
	env := NewEnvironment(classEnv, true)
	env.setThis(ctor)
	env.homeObj = ctor
	env.newTgt = Undef
	env.fieldInit = true
	_, err := i.runFunctionBody(ctx, "static block", m.StaticBlock, env)
	return err
}

// forbidStaticPrototypeKey returns a TypeError when a static class element's
// name evaluates to "prototype", which no static element may use (ECMA-262
// ClassDefinitionEvaluation). Non-computed forms are rejected by the parser, so
// at evaluation time this covers computed names such as static ["prototype"].
func (i *Interpreter) forbidStaticPrototypeKey(ctx context.Context, m *ast.ClassMember, key PropertyKey) error {
	if m.Static && !key.IsSymbol() && key.Str == "prototype" {
		return i.throwError(ctx, "TypeError", "Classes may not have a static property named 'prototype'")
	}
	return nil
}

// installClassMethod installs a method or accessor on target with home as its
// [[HomeObject]] (for super).
func (i *Interpreter) installClassMethod(ctx context.Context, cd *classData, target, home *Object, m *ast.ClassMember, classEnv *Environment) error {
	key, err := i.classMemberKey(ctx, cd, m, classEnv)
	if err != nil {
		return err
	}
	if err := i.forbidStaticPrototypeKey(ctx, m, key); err != nil {
		return err
	}
	fnExpr := m.Value.(*ast.FuncExpr)
	fn := i.makeFunction(fnExpr.Def, classEnv, kindNormal, home, false)
	// The function name follows SetFunctionName: a symbol key becomes "[desc]"
	// (or "" for a descriptionless symbol), and an accessor takes a "get"/"set"
	// prefix (§15.4.5 / §15.4.4 MethodDefinitionEvaluation).
	prefix := ""
	switch m.Kind {
	case ast.PropGet:
		prefix = "get"
	case ast.PropSet:
		prefix = "set"
	}
	i.setFuncName(fn, key, prefix)
	switch m.Kind {
	case ast.PropGet:
		i.mergeAccessor(target, key, fn, nil)
	case ast.PropSet:
		i.mergeAccessor(target, key, nil, fn)
	default:
		target.defineOwn(key, &Property{Value: fn, Writable: true, Enumerable: false, Configurable: true})
	}
	return nil
}

// installPrivateMember installs a private method or private accessor into the
// target object's private storage, with home as its [[HomeObject]].
func (i *Interpreter) installPrivateMember(ctx context.Context, target, home *Object, m *ast.ClassMember, classEnv *Environment) error {
	priv, ok := m.Key.(*ast.PrivateIdent)
	if !ok {
		return i.throwError(ctx, "SyntaxError", "invalid private member")
	}
	name := priv.Name
	pn := classEnv.resolvePrivate(name)
	fnExpr := m.Value.(*ast.FuncExpr)
	fn := i.makeFunction(fnExpr.Def, classEnv, kindNormal, home, false)
	setFuncNameProp(fn, name)
	switch m.Kind {
	case ast.PropGet:
		p, ok := target.getPrivate(pn)
		if !ok || !p.Accessor {
			p = &Property{Accessor: true}
			target.definePrivate(pn, p)
		}
		p.Get = fn
	case ast.PropSet:
		p, ok := target.getPrivate(pn)
		if !ok || !p.Accessor {
			p = &Property{Accessor: true}
			target.definePrivate(pn, p)
		}
		p.Set = fn
	default:
		// A private method is non-writable, so assigning to it throws.
		target.definePrivate(pn, &Property{Value: fn, Writable: false})
	}
	return nil
}

// installInstancePrivateMethods installs the class's per-instance private
// methods and accessors onto self's private storage. It first enforces the
// brand check that a private element may not be added to an object twice
// (ECMA-262 PrivateMethodOrAccessorAdd / PrivateFieldAdd): this happens when a
// constructor return-overrides to an object that a prior construction of the
// same class already initialized.
func (i *Interpreter) installInstancePrivateMethods(ctx context.Context, self *Object, cd *classData) error {
	if err := i.checkNoDuplicateBrand(ctx, self, cd); err != nil {
		return err
	}
	// A private method or accessor may not be added to a non-extensible object,
	// consistent with private fields (nonextensible-applies-to-private).
	if len(cd.sharedPrivate) > 0 && !self.extensible {
		return i.throwError(ctx, "TypeError",
			"Cannot add private member to a non-extensible object")
	}
	// Install the shared private elements by reference, so every instance observes
	// the same private method/accessor function objects.
	for pn, p := range cd.sharedPrivate {
		self.definePrivate(pn, p)
	}
	return nil
}

// buildSharedPrivate creates this class's instance private methods and accessors
// once (with home as their [[HomeObject]]), merging a get/set pair for the same
// private name into a single accessor element.
func (i *Interpreter) buildSharedPrivate(ctx context.Context, cd *classData, classEnv *Environment, home *Object) error {
	for _, m := range cd.privateMethods {
		priv, ok := m.Key.(*ast.PrivateIdent)
		if !ok {
			continue
		}
		pn := classEnv.resolvePrivate(priv.Name)
		fnExpr := m.Value.(*ast.FuncExpr)
		fn := i.makeFunction(fnExpr.Def, classEnv, kindNormal, home, false)
		setFuncNameProp(fn, priv.Name)
		if cd.sharedPrivate == nil {
			cd.sharedPrivate = make(map[*PrivateName]*Property)
		}
		switch m.Kind {
		case ast.PropGet:
			p := cd.sharedPrivate[pn]
			if p == nil || !p.Accessor {
				p = &Property{Accessor: true}
				cd.sharedPrivate[pn] = p
			}
			p.Get = fn
		case ast.PropSet:
			p := cd.sharedPrivate[pn]
			if p == nil || !p.Accessor {
				p = &Property{Accessor: true}
				cd.sharedPrivate[pn] = p
			}
			p.Set = fn
		default:
			// A private method is non-writable, so assigning to it throws.
			cd.sharedPrivate[pn] = &Property{Value: fn, Writable: false}
		}
	}
	return nil
}

// checkNoDuplicateBrand throws a TypeError when self already carries any of the
// private elements (methods, accessors, or fields) that this class installs on
// each instance — i.e. this object was already initialized by this class.
func (i *Interpreter) checkNoDuplicateBrand(ctx context.Context, self *Object, cd *classData) error {
	seen := map[*PrivateName]bool{}
	check := func(name string) error {
		pn := cd.env.resolvePrivate(name)
		if pn == nil || seen[pn] {
			return nil
		}
		seen[pn] = true
		if self.hasPrivate(pn) {
			return i.throwError(ctx, "TypeError",
				"Cannot initialize private element "+name+" twice on the same object")
		}
		return nil
	}
	for _, m := range cd.privateMethods {
		if priv, ok := m.Key.(*ast.PrivateIdent); ok {
			if err := check(priv.Name); err != nil {
				return err
			}
		}
	}
	for _, m := range cd.fieldInits {
		if priv, ok := m.Key.(*ast.PrivateIdent); ok {
			if err := check(priv.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeAccessor installs or augments an accessor property (non-enumerable, as
// class methods are).
func (i *Interpreter) mergeAccessor(target *Object, key PropertyKey, get, set *Object) {
	existing, ok := target.props[key]
	if !ok || !existing.Accessor {
		existing = &Property{Accessor: true, Enumerable: false, Configurable: true}
		target.defineOwn(key, existing)
	}
	if get != nil {
		existing.Get = get
	}
	if set != nil {
		existing.Set = set
	}
}

// classMemberKey computes a class member's property key. For a computed name it
// returns the value evaluated once at class-definition time (cached in cd);
// falling back to evaluating in env only when no cache is available.
func (i *Interpreter) classMemberKey(ctx context.Context, cd *classData, m *ast.ClassMember, env *Environment) (PropertyKey, error) {
	if m.Computed {
		if cd != nil {
			if k, ok := cd.computedKeys[m]; ok {
				return k, nil
			}
		}
		v, err := i.evalExpr(ctx, m.Key, env)
		if err != nil {
			return PropertyKey{}, err
		}
		return i.ToPropertyKey(ctx, v)
	}
	switch k := m.Key.(type) {
	case *ast.Ident:
		return StrKey(k.Name), nil
	case *ast.PrivateIdent:
		return StrKey(k.Name), nil
	case *ast.StringLit:
		return StrKey(k.Value), nil
	case *ast.NumberLit:
		return StrKey(NumberToString(k.Value)), nil
	case *ast.BigIntLit:
		return StrKey(bigIntLitKeyString(k.Digits)), nil
	default:
		return PropertyKey{}, i.throwError(ctx, "SyntaxError", "invalid class member key")
	}
}

// runConstructorBody runs a class constructor body, translating a return signal.
func (i *Interpreter) runConstructorBody(ctx context.Context, name string, body *ast.BlockStmt, env *Environment) (Value, error) {
	defer i.enterFrame(name)()
	// Clear any enclosing parameter-default context (see runFunctionBody).
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

// resolveSuperCall handles a super(...) call inside a derived constructor by
// returning a synthetic callable. When invoked it constructs the parent class
// and binds the derived `this` to the object the parent [[Construct]] returns
// (§13.3.7.1 SuperCall / §9.1.1.3.1 BindThisValue), then runs this class's field
// and private-element initializers on that object.
func (i *Interpreter) resolveSuperCall(ctx context.Context, env *Environment) (Value, Value, error) {
	b := env.lookup("%superctor%")
	if b == nil {
		return nil, nil, i.throwError(ctx, "SyntaxError", "'super' keyword unexpected here")
	}
	// GetSuperConstructor (§13.3.7.1): the active function object's current
	// [[Prototype]], not the value statically captured at class definition, so a
	// later Object.setPrototypeOf on the class is honored.
	superCtor, _ := b.value.(*Object)
	if af := env.lookup("%activefunc%"); af != nil {
		if fn, ok := af.value.(*Object); ok {
			superCtor = fn.proto
		}
	}
	fieldInit := env.lookup("%fieldinit%")
	newTarget := env.newTarget()
	// The super-init state and `this` binding live on the derived constructor's
	// this-scope.
	ts := env.thisScope()
	initState := (*superInitState)(nil)
	if ts != nil {
		initState = ts.superInit
	}

	caller := i.newNativeFunc("super", 0, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		// IsConstructor(superCtor) is checked after ArgumentListEvaluation, so the
		// arguments' side effects are observed even when the super value is not a
		// constructor (§13.3.7.1 SuperCall steps 4-5).
		if superCtor == nil || !superCtor.IsConstructor() {
			return nil, i.throwError(ctx, "TypeError", "Super constructor is not a constructor")
		}
		// Step 6: Construct(func, argList, newTarget). This runs even when `this`
		// is already initialized (its side effects are observable); the duplicate
		// binding is only rejected afterward by BindThisValue.
		result, err := superCtor.fn.construct(ctx, newTarget, args)
		if err != nil {
			return nil, err
		}
		self, ok := result.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Super constructor returned a non-object")
		}
		// Step 8: BindThisValue — a second super() in the same constructor throws,
		// because the this-binding is already initialized (§9.1.1.3.1).
		if initState != nil && initState.called {
			return nil, i.throwError(ctx, "ReferenceError", "Super constructor may only be called once")
		}
		if ts != nil {
			ts.setThis(self)
		}
		if initState != nil {
			initState.called = true
		}
		// InitializeInstanceElements: install this class's private elements and
		// instance fields on the newly bound `this`.
		if fieldInit != nil {
			if fn, ok := fieldInit.value.(*Object); ok {
				if _, err := fn.fn.call(ctx, self, nil); err != nil {
					return nil, err
				}
			}
		}
		// A SuperCall evaluates to the newly bound `this` (§13.3.7.1 step 8 returns
		// the result of BindThisValue), so `x = super()` observes the new object.
		return self, nil
	})
	return caller, Undef, nil
}
