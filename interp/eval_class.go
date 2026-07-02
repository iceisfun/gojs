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
	superCtor      *Object // parent constructor (nil when no extends)
	fieldInits     []*ast.ClassMember
	privateMethods []*ast.ClassMember // instance #methods and private accessors
}

// evalClass evaluates a class definition to its constructor object.
func (i *Interpreter) evalClass(ctx context.Context, def *ast.ClassDef, env *Environment) (Value, error) {
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

	var superCtor *Object
	protoParent := i.objectProto
	if def.SuperClass != nil {
		sv, err := i.evalExpr(ctx, def.SuperClass, env)
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
			protoV, _ := sc.GetStr(ctx, "prototype")
			if pp, ok := protoV.(*Object); ok {
				protoParent = pp
			}
		}
	}

	proto := NewObject(protoParent)
	cd := &classData{def: def, env: classEnv, proto: proto, superCtor: superCtor}

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

	ctor := i.makeClassConstructor(def, cd, ctorDef, classEnv, proto)
	ctor.internal = map[string]any{"class": cd}
	if superCtor != nil {
		ctor.SetProto(superCtor)
	}
	proto.defineOwn(StrKey("constructor"), &Property{Value: ctor, Writable: true, Enumerable: false, Configurable: true})
	ctor.defineOwn(StrKey("prototype"), &Property{Value: proto, Writable: false, Enumerable: false, Configurable: false})

	// Bind the class name inside the class scope for self-reference.
	if def.Name != nil {
		classEnv.vars[def.Name.Name] = &binding{value: ctor, mutable: false, initialized: true}
	}

	// Install methods, accessors, and static members.
	for _, m := range def.Members {
		if m.Field {
			if m.Static {
				if err := i.initStaticField(ctx, ctor, m, classEnv); err != nil {
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
		if err := i.installClassMethod(ctx, target, home, m, classEnv); err != nil {
			return nil, err
		}
	}

	return ctor, nil
}

// makeClassConstructor builds the constructor callable for a class.
func (i *Interpreter) makeClassConstructor(def *ast.ClassDef, cd *classData, ctorDef *ast.FuncDef, classEnv *Environment, proto *Object) *Object {
	name := ""
	if def.Name != nil {
		name = def.Name.Name
	}
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
		// A base class's instance takes its prototype from new.target.prototype
		// (which differs under Reflect.construct with an explicit newTarget); a
		// derived class populates a fresh object whose prototype is this class's
		// own proto and reconciles it in invokeSuperOnto.
		instProto := proto
		if cd.superCtor == nil {
			if nt, ok := newTarget.(*Object); ok {
				if pv, _ := nt.GetStr(ctx, "prototype"); pv != nil {
					if po, ok := pv.(*Object); ok {
						instProto = po
					}
				}
			}
		}
		self := NewObject(instProto)
		env := NewEnvironment(classEnv, true)
		env.homeObj = proto
		env.setThis(self)
		env.newTgt = newTarget

		// With a superclass, `this` field/private init and body run after super()
		// is called; a base class initializes everything up front.
		if cd.superCtor == nil {
			if err := i.installInstancePrivateMethods(ctx, self, cd); err != nil {
				return nil, err
			}
			if err := i.initInstanceFields(ctx, self, cd, env); err != nil {
				return nil, err
			}
		} else {
			// Provide a super() binding that constructs the parent onto self, and
			// mark `this` as uninitialized until super() runs.
			env.superInit = &superInitState{}
			env.vars["%superctor%"] = &binding{value: cd.superCtor, mutable: false, initialized: true}
			env.vars["%fieldinit%"] = &binding{value: i.fieldInitThunk(cd, self), mutable: false, initialized: true}
		}

		var result Value = self
		if ctorDef != nil {
			if err := i.bindParams(ctx, ctorDef.Params, args, env); err != nil {
				return nil, err
			}
			ret, err := i.runConstructorBody(ctx, ctorDef.Body, env)
			if err != nil {
				return nil, err
			}
			// An explicit object return from a constructor replaces `this`; a
			// primitive (or undefined) return is ignored. In a derived class a
			// non-object return instead reads the `this` binding, which is still
			// uninitialized if super() was never called — a ReferenceError
			// (ECMA-262 10.2.2 / GetThisBinding on an uninitialized environment).
			if obj, ok := ret.(*Object); ok {
				result = obj
			} else if cd.superCtor != nil && (env.superInit == nil || !env.superInit.called) {
				return nil, i.throwError(ctx, "ReferenceError",
					"Must call super constructor in derived class before accessing 'this' or returning from derived constructor")
			}
		} else if cd.superCtor != nil {
			// Default derived constructor behaves as `constructor(...args) {
			// super(...args); }`: construct the parent, fold its own properties
			// onto self, then initialize this class's private elements and fields.
			if err := i.invokeSuperOnto(ctx, self, cd.superCtor, args, newTarget); err != nil {
				return nil, err
			}
			env.superInit.called = true
			if err := i.installInstancePrivateMethods(ctx, self, cd); err != nil {
				return nil, err
			}
			if err := i.initInstanceFields(ctx, self, cd, env); err != nil {
				return nil, err
			}
		}
		return result, nil
	}

	fnObj.fn = &functionData{
		call: func(ctx context.Context, this Value, args []Value) (Value, error) {
			return nil, i.throwError(ctx, "TypeError", "Class constructor "+name+" cannot be invoked without 'new'")
		},
		construct: construct,
		name:      name,
		length:    0,
		ctor:      true,
	}
	if ctorDef != nil {
		fnObj.fn.length = countParams(ctorDef.Params)
	}
	setFuncLength(fnObj, fnObj.fn.length)
	setFuncNameProp(fnObj, name)
	return fnObj
}

// fieldInitThunk returns a native function that initializes instance fields on
// self; the derived constructor calls it after super().
func (i *Interpreter) fieldInitThunk(cd *classData, self *Object) *Object {
	return i.newNativeFunc("%fieldinit%", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		env := NewEnvironment(cd.env, true)
		env.setThis(self)
		env.homeObj = cd.proto
		if err := i.installInstancePrivateMethods(ctx, self, cd); err != nil {
			return Undef, err
		}
		return Undef, i.initInstanceFields(ctx, self, cd, env)
	})
}

// initInstanceFields evaluates and assigns instance field initializers.
func (i *Interpreter) initInstanceFields(ctx context.Context, self *Object, cd *classData, env *Environment) error {
	for _, m := range cd.fieldInits {
		key, err := i.classMemberKey(ctx, m, env)
		if err != nil {
			return err
		}
		var v Value = Undef
		if m.Value != nil {
			fieldEnv := NewEnvironment(cd.env, true)
			fieldEnv.setThis(self)
			fieldEnv.homeObj = cd.proto
			v, err = i.evalExpr(ctx, m.Value, fieldEnv)
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

// defineFieldOrThrow creates a public class field as an own data property with
// CreateDataPropertyOrThrow semantics: adding a new property to a non-extensible
// object (e.g. a field initializer froze `this`) is a TypeError.
func (i *Interpreter) defineFieldOrThrow(ctx context.Context, obj *Object, key PropertyKey, v Value) error {
	if !obj.extensible {
		if _, exists := obj.getOwn(key); !exists {
			return i.throwError(ctx, "TypeError",
				"Cannot define property "+keyName(key)+", object is not extensible")
		}
	}
	obj.writeData(key, v)
	return nil
}

// initStaticField evaluates a static field initializer on the constructor.
func (i *Interpreter) initStaticField(ctx context.Context, ctor *Object, m *ast.ClassMember, classEnv *Environment) error {
	key, err := i.classMemberKey(ctx, m, classEnv)
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
		v, err = i.evalExpr(ctx, m.Value, env)
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
func (i *Interpreter) installClassMethod(ctx context.Context, target, home *Object, m *ast.ClassMember, classEnv *Environment) error {
	key, err := i.classMemberKey(ctx, m, classEnv)
	if err != nil {
		return err
	}
	if err := i.forbidStaticPrototypeKey(ctx, m, key); err != nil {
		return err
	}
	fnExpr := m.Value.(*ast.FuncExpr)
	fn := i.makeFunction(fnExpr.Def, classEnv, kindNormal, home)
	setFuncNameProp(fn, keyName(key))
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
	fn := i.makeFunction(fnExpr.Def, classEnv, kindNormal, home)
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
// methods and accessors onto self's private storage.
func (i *Interpreter) installInstancePrivateMethods(ctx context.Context, self *Object, cd *classData) error {
	for _, m := range cd.privateMethods {
		if err := i.installPrivateMember(ctx, self, cd.proto, m, cd.env); err != nil {
			return err
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

// classMemberKey computes a class member's property key.
func (i *Interpreter) classMemberKey(ctx context.Context, m *ast.ClassMember, env *Environment) (PropertyKey, error) {
	if m.Computed {
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
	default:
		return PropertyKey{}, i.throwError(ctx, "SyntaxError", "invalid class member key")
	}
}

// runConstructorBody runs a class constructor body, translating a return signal.
func (i *Interpreter) runConstructorBody(ctx context.Context, body *ast.BlockStmt, env *Environment) (Value, error) {
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
// returning a synthetic callable that constructs the parent onto `this` and
// runs field initializers.
func (i *Interpreter) resolveSuperCall(ctx context.Context, env *Environment) (Value, Value, error) {
	b := env.lookup("%superctor%")
	if b == nil {
		return nil, nil, i.throwError(ctx, "SyntaxError", "'super' keyword unexpected here")
	}
	superCtor := b.value.(*Object)
	thisVal, _ := env.thisBinding()
	self, _ := thisVal.(*Object)
	fieldInit := env.lookup("%fieldinit%")
	newTarget := env.newTarget()
	// The super-init state lives on the derived constructor's `this` scope.
	initState := (*superInitState)(nil)
	if ts := env.thisScope(); ts != nil {
		initState = ts.superInit
	}

	caller := i.newNativeFunc("super", 0, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		// super() may be called at most once in a derived constructor.
		if initState != nil && initState.called {
			return nil, i.throwError(ctx, "ReferenceError", "Super constructor may only be called once")
		}
		if err := i.invokeSuperOnto(ctx, self, superCtor, args, newTarget); err != nil {
			return nil, err
		}
		if initState != nil {
			initState.called = true
		}
		// Initialize this class's private elements and instance fields after
		// super returns.
		if fieldInit != nil {
			if fn, ok := fieldInit.value.(*Object); ok {
				if _, err := fn.fn.call(ctx, self, nil); err != nil {
					return nil, err
				}
			}
		}
		return Undef, nil
	})
	return caller, Undef, nil
}

// invokeSuperOnto constructs the parent class with args and folds the resulting
// object's own properties onto self. gojs uses a single-object instance model,
// so a derived instance is one object; super() populates it with the fields the
// parent constructor would have set.
func (i *Interpreter) invokeSuperOnto(ctx context.Context, self *Object, superCtor *Object, args []Value, newTarget Value) error {
	if newTarget == nil {
		newTarget = superCtor
	}
	result, err := superCtor.fn.construct(ctx, newTarget, args)
	if err != nil {
		return err
	}
	parentObj, ok := result.(*Object)
	if !ok || self == nil || parentObj == self {
		return nil
	}
	// If the parent is an integer-indexed exotic (class X extends Uint8Array),
	// adopt its TypedArray backing so element access routes through the shared
	// buffer. The numeric-index "own properties" are not ordinary storage, so
	// they must not be folded on (that is handled by the exotic slot).
	if parentObj.typedArray != nil {
		self.typedArray = parentObj.typedArray
		self.class = parentObj.class
		for key, p := range parentObj.props {
			self.defineOwn(key, p)
		}
		for pn, p := range parentObj.private {
			self.definePrivate(pn, p)
		}
		return nil
	}
	// If the parent is an exotic Array (class X extends Array), the instance
	// must itself be array-backed so length/indexing/push work on it.
	if parentObj.isArray {
		self.isArray = true
		self.elems = parentObj.elems
		self.class = "Array"
	}
	for _, name := range parentObj.OwnKeys() {
		if p, ok := parentObj.getOwn(StrKey(name)); ok {
			self.defineOwn(StrKey(name), p)
		}
	}
	// Preserve any extra own (e.g. symbol) properties too.
	for key, p := range parentObj.props {
		if key.IsSymbol() {
			self.defineOwn(key, p)
		}
	}
	// Fold the parent's private brand onto self so inherited methods can access
	// the base class's private elements through the single derived instance. The
	// keys are the parent evaluation's PrivateName identities, so the parent's
	// methods (which resolve to those same identities) still find them.
	for pn, p := range parentObj.private {
		self.definePrivate(pn, p)
	}
	// Carry over internal slots the parent set up (Map/Set backing storage,
	// boxed primitives, etc.) so built-in subclassing works on the instance.
	for k, v := range parentObj.internal {
		if self.internal == nil {
			self.internal = make(map[string]any)
		}
		self.internal[k] = v
	}
	if parentObj.primitive != nil {
		self.primitive = parentObj.primitive
	}
	if parentObj.class != "Object" && self.class == "Object" {
		self.class = parentObj.class
	}
	return nil
}
