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
	def        *ast.ClassDef
	env        *Environment
	proto      *Object
	superCtor  *Object // parent constructor (nil when no extends)
	fieldInits []*ast.ClassMember
}

// evalClass evaluates a class definition to its constructor object.
func (i *Interpreter) evalClass(ctx context.Context, def *ast.ClassDef, env *Environment) (Value, error) {
	// The class body runs in its own scope so the class name is in scope inside
	// methods (for recursion) and so `extends` can be evaluated.
	classEnv := NewEnvironment(env, false)

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
		target := proto
		home := proto
		if m.Static {
			target = ctor
			home = ctor
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
		self := NewObject(proto)
		env := NewEnvironment(classEnv, true)
		env.homeObj = proto
		env.setThis(self)

		// With a superclass, `this` field init and body run after super() is
		// called; we initialize fields up front for simplicity.
		if cd.superCtor == nil {
			if err := i.initInstanceFields(ctx, self, cd, env); err != nil {
				return nil, err
			}
		} else {
			// Provide a super() binding that constructs the parent onto self.
			env.vars["%superctor%"] = &binding{value: cd.superCtor, mutable: false, initialized: true}
			env.vars["%fieldinit%"] = &binding{value: i.fieldInitThunk(cd, self), mutable: false, initialized: true}
		}

		if ctorDef != nil {
			if err := i.bindParams(ctx, ctorDef.Params, args, env); err != nil {
				return nil, err
			}
			_, err := i.runConstructorBody(ctx, ctorDef.Body, env)
			if err != nil {
				return nil, err
			}
		} else if cd.superCtor != nil {
			// Default derived constructor behaves as `constructor(...args) {
			// super(...args); }`: construct the parent, fold its own
			// properties onto self, then initialize this class's fields.
			if err := i.invokeSuperOnto(ctx, self, cd.superCtor, args); err != nil {
				return nil, err
			}
			if err := i.initInstanceFields(ctx, self, cd, env); err != nil {
				return nil, err
			}
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
	}
	if ctorDef != nil {
		fnObj.fn.length = countParams(ctorDef.Params)
	}
	fnObj.SetHidden("name", String(name))
	fnObj.SetHidden("length", Number(float64(fnObj.fn.length)))
	return fnObj
}

// fieldInitThunk returns a native function that initializes instance fields on
// self; the derived constructor calls it after super().
func (i *Interpreter) fieldInitThunk(cd *classData, self *Object) *Object {
	return i.newNativeFunc("%fieldinit%", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		env := NewEnvironment(cd.env, true)
		env.setThis(self)
		env.homeObj = cd.proto
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
		self.writeData(key, v)
	}
	return nil
}

// initStaticField evaluates a static field initializer on the constructor.
func (i *Interpreter) initStaticField(ctx context.Context, ctor *Object, m *ast.ClassMember, classEnv *Environment) error {
	key, err := i.classMemberKey(ctx, m, classEnv)
	if err != nil {
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
	ctor.writeData(key, v)
	return nil
}

// installClassMethod installs a method or accessor on target with home as its
// [[HomeObject]] (for super).
func (i *Interpreter) installClassMethod(ctx context.Context, target, home *Object, m *ast.ClassMember, classEnv *Environment) error {
	key, err := i.classMemberKey(ctx, m, classEnv)
	if err != nil {
		return err
	}
	fnExpr := m.Value.(*ast.FuncExpr)
	fn := i.makeFunction(fnExpr.Def, classEnv, kindNormal, home)
	fn.SetHidden("name", String(keyName(key)))
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
	i.hoistDeclarations(ctx, body.Body, env, true)
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

	caller := i.newNativeFunc("super", 0, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		if err := i.invokeSuperOnto(ctx, self, superCtor, args); err != nil {
			return nil, err
		}
		// Initialize this class's instance fields after super returns.
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
func (i *Interpreter) invokeSuperOnto(ctx context.Context, self *Object, superCtor *Object, args []Value) error {
	result, err := superCtor.fn.construct(ctx, superCtor, args)
	if err != nil {
		return err
	}
	parentObj, ok := result.(*Object)
	if !ok || self == nil || parentObj == self {
		return nil
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
	return nil
}
