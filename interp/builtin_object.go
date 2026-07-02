package interp

import "context"

// initObject installs the Object constructor and Object.prototype methods.
func (i *Interpreter) initObject() {
	proto := i.objectProto

	i.defineMethod(proto, "hasOwnProperty", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		_, ok, err := i.getOwnPropertyV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		return Bool(ok), nil
	})
	i.defineMethod(proto, "isPrototypeOf", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		target, ok := arg(args, 0).(*Object)
		if !ok {
			return False, nil
		}
		self, ok := this.(*Object)
		if !ok {
			return False, nil
		}
		for p := target.proto; p != nil; p = p.proto {
			if p == self {
				return True, nil
			}
		}
		return False, nil
	})
	i.defineMethod(proto, "propertyIsEnumerable", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		p, ok, err := i.getOwnPropertyV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		if ok {
			return Bool(p.Enumerable), nil
		}
		return False, nil
	})
	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		switch this.(type) {
		case Undefined:
			return String("[object Undefined]"), nil
		case Null:
			return String("[object Null]"), nil
		}
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		tag := o.class
		if o.isArray {
			tag = "Array"
		} else if o.IsCallable() {
			tag = "Function"
		}
		return String("[object " + tag + "]"), nil
	})
	i.defineMethod(proto, "toLocaleString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.call(ctx, mustGet(ctx, this, "toString"), this, nil)
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.ToObject(ctx, this)
	})

	// Object constructor.
	ctor := i.newNativeCtor("Object", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		v := arg(args, 0)
		if IsNullish(v) {
			return NewObject(i.objectProto), nil
		}
		return i.ToObject(ctx, v)
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		v := arg(args, 0)
		if IsNullish(v) {
			return NewObject(i.objectProto), nil
		}
		return i.ToObject(ctx, v)
	})
	linkCtor(ctor, i.objectProto)

	i.defineMethod(ctor, "keys", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		vals, err := i.enumerableKeys(ctx, o, func(k string, _ Value) Value { return String(k) })
		if err != nil {
			return nil, err
		}
		return i.newArray(vals), nil
	})
	// Object.hasOwn(O, P) → HasOwnProperty(ToObject(O), ToPropertyKey(P)) (ES2022).
	i.defineMethod(ctor, "hasOwn", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return Bool(o.HasOwn(key)), nil
	})
	i.defineMethod(ctor, "values", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		vals, err := i.enumerableKeys(ctx, o, func(_ string, v Value) Value { return v })
		if err != nil {
			return nil, err
		}
		return i.newArray(vals), nil
	})
	i.defineMethod(ctor, "entries", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		pairs, err := i.enumerableKeys(ctx, o, func(k string, v Value) Value {
			return i.newArray([]Value{String(k), v})
		})
		if err != nil {
			return nil, err
		}
		return i.newArray(pairs), nil
	})
	i.defineMethod(ctor, "assign", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		target, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		for _, src := range args[min(1, len(args)):] {
			if IsNullish(src) {
				continue
			}
			so, err := i.ToObject(ctx, src)
			if err != nil {
				return nil, err
			}
			for _, name := range so.OwnKeys() {
				if p, ok := so.getOwn(StrKey(name)); ok && p.Enumerable {
					v, err := so.GetStr(ctx, name)
					if err != nil {
						return nil, err
					}
					if err := target.SetStr(ctx, name, v); err != nil {
						return nil, err
					}
				}
			}
		}
		return target, nil
	})
	i.defineMethod(ctor, "freeze", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok {
			o.extensible = false
			for _, p := range o.props {
				p.Writable = false
				p.Configurable = false
			}
		}
		return arg(args, 0), nil
	})
	i.defineMethod(ctor, "isFrozen", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok {
			return Bool(!o.extensible), nil
		}
		return True, nil
	})
	i.defineMethod(ctor, "create", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		var proto *Object
		switch p := arg(args, 0).(type) {
		case *Object:
			proto = p
		case Null:
			proto = nil
		default:
			return nil, i.throwError(ctx, "TypeError", "Object prototype may only be an Object or null")
		}
		o := NewObject(proto)
		if props, ok := arg(args, 1).(*Object); ok {
			if err := i.defineProperties(ctx, o, props); err != nil {
				return nil, err
			}
		}
		return o, nil
	})
	i.defineMethod(ctor, "getPrototypeOf", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		return i.getProtoV(ctx, o)
	})
	i.defineMethod(ctor, "setPrototypeOf", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if IsNullish(arg(args, 0)) {
			return nil, i.throwError(ctx, "TypeError", "Object.setPrototypeOf called on null or undefined")
		}
		proto := arg(args, 1)
		if _, ok := proto.(*Object); !ok {
			if _, ok := proto.(Null); !ok {
				return nil, i.throwError(ctx, "TypeError", "Object prototype may only be an Object or null")
			}
		}
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return arg(args, 0), nil
		}
		status, err := i.setProtoV(ctx, o, proto)
		if err != nil {
			return nil, err
		}
		if !status {
			return nil, i.throwError(ctx, "TypeError", "Object.setPrototypeOf: cannot set prototype")
		}
		return o, nil
	})
	i.defineMethod(ctor, "defineProperty", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Object.defineProperty called on non-object")
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		desc, ok := arg(args, 2).(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Property description must be an object")
		}
		if err := i.applyDescriptor(ctx, o, key, desc); err != nil {
			return nil, err
		}
		return o, nil
	})
	i.defineMethod(ctor, "defineProperties", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Object.defineProperties called on non-object")
		}
		props, ok := arg(args, 1).(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Properties must be an object")
		}
		if err := i.defineProperties(ctx, o, props); err != nil {
			return nil, err
		}
		return o, nil
	})
	i.defineMethod(ctor, "getOwnPropertyNames", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		keys, err := i.ownKeysV(ctx, o)
		if err != nil {
			return nil, err
		}
		var out []Value
		for _, k := range keys {
			if !k.IsSymbol() {
				out = append(out, String(k.Str))
			}
		}
		return i.newArray(out), nil
	})
	i.defineMethod(ctor, "fromEntries", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o := NewObject(i.objectProto)
		err := i.iterate(ctx, arg(args, 0), func(entry Value) error {
			eo, err := i.ToObject(ctx, entry)
			if err != nil {
				return err
			}
			k, err := eo.GetStr(ctx, "0")
			if err != nil {
				return err
			}
			v, err := eo.GetStr(ctx, "1")
			if err != nil {
				return err
			}
			key, err := i.ToPropertyKey(ctx, k)
			if err != nil {
				return err
			}
			o.writeData(key, v)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return o, nil
	})

	i.defineMethod(ctor, "getOwnPropertyDescriptor", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		p, ok, err := i.getOwnPropertyV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return Undef, nil
		}
		return i.descriptorToObject(p), nil
	})
	i.defineMethod(ctor, "getOwnPropertyDescriptors", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		out := NewObject(i.objectProto)
		for _, name := range o.OwnKeys() {
			if p, ok := o.getOwn(StrKey(name)); ok {
				out.SetData(name, i.descriptorToObject(p))
			}
		}
		return out, nil
	})
	i.defineMethod(ctor, "getOwnPropertySymbols", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		keys, err := i.ownKeysV(ctx, o)
		if err != nil {
			return nil, err
		}
		var syms []Value
		for _, k := range keys {
			if k.IsSymbol() {
				syms = append(syms, k.Sym)
			}
		}
		return i.newArray(syms), nil
	})
	i.defineMethod(ctor, "isExtensible", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return False, nil
		}
		ext, err := i.isExtensibleV(ctx, o)
		if err != nil {
			return nil, err
		}
		return Bool(ext), nil
	})
	i.defineMethod(ctor, "preventExtensions", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok {
			if _, err := i.preventExtensionsV(ctx, o); err != nil {
				return nil, err
			}
		}
		return arg(args, 0), nil
	})
	i.defineMethod(ctor, "seal", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok {
			o.extensible = false
			for _, p := range o.props {
				p.Configurable = false
			}
		}
		return arg(args, 0), nil
	})
	i.defineMethod(ctor, "isSealed", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return True, nil
		}
		if o.extensible {
			return False, nil
		}
		for _, p := range o.props {
			if p.Configurable {
				return False, nil
			}
		}
		return True, nil
	})
	i.defineMethod(ctor, "is", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Bool(sameValue(arg(args, 0), arg(args, 1))), nil
	})
	i.defineMethod(ctor, "groupBy", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cb, ok := arg(args, 1).(*Object)
		if !ok || !cb.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "groupBy callback is not a function")
		}
		// Result is an object with a null prototype, keyed by the callback's
		// return value coerced to a property key.
		result := NewObject(nil)
		idx := 0
		err := i.iterate(ctx, arg(args, 0), func(v Value) error {
			kv, err := cb.fn.call(ctx, Undef, []Value{v, Number(float64(idx))})
			if err != nil {
				return err
			}
			key, err := i.ToPropertyKey(ctx, kv)
			if err != nil {
				return err
			}
			bucket, ok := result.props[key]
			if !ok || bucket.Value == nil {
				arr := i.newArray(nil)
				result.writeData(key, arr)
				bucket = result.props[key]
			}
			if arr, ok := bucket.Value.(*Object); ok {
				arr.elems = append(arr.elems, v)
			}
			idx++
			return nil
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	})

	i.objectCtor = ctor
	i.setGlobalHidden("Object", ctor)
}

// descriptorToObject renders a property descriptor as a plain object, matching
// Object.getOwnPropertyDescriptor's result shape.
func (i *Interpreter) descriptorToObject(p *Property) *Object {
	d := NewObject(i.objectProto)
	if p.Accessor {
		if p.Get != nil {
			d.SetData("get", p.Get)
		} else {
			d.SetData("get", Undef)
		}
		if p.Set != nil {
			d.SetData("set", p.Set)
		} else {
			d.SetData("set", Undef)
		}
	} else {
		d.SetData("value", p.Value)
		d.SetData("writable", Bool(p.Writable))
	}
	d.SetData("enumerable", Bool(p.Enumerable))
	d.SetData("configurable", Bool(p.Configurable))
	return d
}

// enumerableKeys collects a value for each own enumerable string-keyed property
// of o, in key order, via project. It routes through the object internal
// methods so a Proxy's ownKeys/getOwnPropertyDescriptor/get traps run.
func (i *Interpreter) enumerableKeys(ctx context.Context, o *Object, project func(string, Value) Value) ([]Value, error) {
	keys, err := i.ownKeysV(ctx, o)
	if err != nil {
		return nil, err
	}
	var out []Value
	for _, k := range keys {
		if k.IsSymbol() {
			continue
		}
		p, ok, err := i.getOwnPropertyV(ctx, o, k)
		if err != nil {
			return nil, err
		}
		if !ok || !p.Enumerable {
			continue
		}
		v, err := i.getV(ctx, o, k, o)
		if err != nil {
			return nil, err
		}
		out = append(out, project(k.Str, v))
	}
	return out, nil
}

// applyDescriptor is the Throw-flag form of OrdinaryDefineOwnProperty used by
// Object.defineProperty: it applies the descriptor and raises a TypeError when
// an invariant forbids the change. Reflect.defineProperty uses
// defineOwnFromDescriptor directly, observing the boolean result instead.
func (i *Interpreter) applyDescriptor(ctx context.Context, o *Object, key PropertyKey, desc *Object) error {
	ok, err := i.definePropertyV(ctx, o, key, desc)
	if err != nil {
		return err
	}
	if !ok {
		if _, exists := o.getOwn(key); o.proxy == nil && !exists {
			return i.throwError(ctx, "TypeError",
				"Cannot define property "+keyName(key)+", object is not extensible")
		}
		return i.throwError(ctx, "TypeError", "Cannot redefine property: "+keyName(key))
	}
	return nil
}

// defineOwnFromDescriptor implements OrdinaryDefineOwnProperty /
// ValidateAndApplyPropertyDescriptor (§10.1.6). It merges only the fields the
// descriptor actually specifies onto any existing property (leaving the rest
// untouched) and enforces the invariants for non-configurable and non-writable
// properties. It returns whether the definition was applied: an unmet invariant
// yields (false, nil), while a malformed descriptor (accessors combined with a
// value/writable, or a non-callable getter/setter) — a ToPropertyDescriptor
// failure that throws even for Reflect.defineProperty — yields a non-nil error.
func (i *Interpreter) defineOwnFromDescriptor(ctx context.Context, o *Object, key PropertyKey, desc *Object) (bool, error) {
	// Which attributes does the descriptor specify? Presence — not truthiness —
	// determines what gets applied; absent fields are inherited from the
	// current property (or take spec defaults for a brand-new one).
	hasEnum := desc.HasOwn(StrKey("enumerable"))
	hasConf := desc.HasOwn(StrKey("configurable"))
	hasValue := desc.HasOwn(StrKey("value"))
	hasWritable := desc.HasOwn(StrKey("writable"))
	hasGet := desc.HasOwn(StrKey("get"))
	hasSet := desc.HasOwn(StrKey("set"))

	if (hasGet || hasSet) && (hasValue || hasWritable) {
		return false, i.throwError(ctx, "TypeError",
			"Invalid property descriptor. Cannot both specify accessors and a value or writable attribute, "+keyName(key))
	}

	var enumerable, configurable, writable bool
	var value Value = Undef
	var getter, setter *Object
	if hasEnum {
		v, err := desc.GetStr(ctx, "enumerable")
		if err != nil {
			return false, err
		}
		enumerable = ToBoolean(v)
	}
	if hasConf {
		v, err := desc.GetStr(ctx, "configurable")
		if err != nil {
			return false, err
		}
		configurable = ToBoolean(v)
	}
	if hasValue {
		v, err := desc.GetStr(ctx, "value")
		if err != nil {
			return false, err
		}
		value = v
	}
	if hasWritable {
		v, err := desc.GetStr(ctx, "writable")
		if err != nil {
			return false, err
		}
		writable = ToBoolean(v)
	}
	if hasGet {
		v, err := desc.GetStr(ctx, "get")
		if err != nil {
			return false, err
		}
		if fn, ok := v.(*Object); ok && fn.IsCallable() {
			getter = fn
		} else if !IsUndefined(v) {
			return false, i.throwError(ctx, "TypeError", "Getter must be a function: "+keyName(key))
		}
	}
	if hasSet {
		v, err := desc.GetStr(ctx, "set")
		if err != nil {
			return false, err
		}
		if fn, ok := v.(*Object); ok && fn.IsCallable() {
			setter = fn
		} else if !IsUndefined(v) {
			return false, i.throwError(ctx, "TypeError", "Setter must be a function: "+keyName(key))
		}
	}
	isAccessorDesc := hasGet || hasSet
	isDataDesc := hasValue || hasWritable

	current, exists := o.getOwn(key)
	if !exists {
		if !o.extensible {
			return false, nil
		}
		p := &Property{Enumerable: enumerable, Configurable: configurable}
		if isAccessorDesc {
			p.Accessor = true
			p.Get = getter
			p.Set = setter
		} else {
			p.Value = value
			p.Writable = writable
		}
		o.defineOwn(key, p)
		return true, nil
	}

	// The property already exists: a non-configurable property constrains which
	// redefinitions are permitted (§10.1.6.3).
	if !current.Configurable {
		if hasConf && configurable {
			return false, nil
		}
		if hasEnum && enumerable != current.Enumerable {
			return false, nil
		}
		switch {
		case isAccessorDesc && !current.Accessor, isDataDesc && current.Accessor:
			// Changing the kind of a non-configurable property is forbidden.
			return false, nil
		case current.Accessor:
			if hasGet && getter != current.Get {
				return false, nil
			}
			if hasSet && setter != current.Set {
				return false, nil
			}
		case !current.Writable:
			if hasWritable && writable {
				return false, nil
			}
			if hasValue && !sameValue(value, current.Value) {
				return false, nil
			}
		}
	}

	// Apply: start from a copy of the current descriptor and overwrite only the
	// specified fields, converting between data and accessor forms as needed.
	np := *current
	switch {
	case isAccessorDesc:
		if !np.Accessor {
			np = Property{Accessor: true, Enumerable: np.Enumerable, Configurable: np.Configurable}
		}
		if hasGet {
			np.Get = getter
		}
		if hasSet {
			np.Set = setter
		}
	case isDataDesc:
		if np.Accessor {
			np = Property{Enumerable: np.Enumerable, Configurable: np.Configurable}
		}
		if hasValue {
			np.Value = value
		}
		if hasWritable {
			np.Writable = writable
		}
	}
	if hasEnum {
		np.Enumerable = enumerable
	}
	if hasConf {
		np.Configurable = configurable
	}
	o.defineOwn(key, &np)
	return true, nil
}

// defineProperties applies each own enumerable descriptor in props to o.
func (i *Interpreter) defineProperties(ctx context.Context, o, props *Object) error {
	for _, name := range props.OwnKeys() {
		if p, ok := props.getOwn(StrKey(name)); ok && p.Enumerable {
			desc, _ := props.GetStr(ctx, name)
			descObj, ok := desc.(*Object)
			if !ok {
				return i.throwError(ctx, "TypeError", "Property description must be an object")
			}
			if err := i.applyDescriptor(ctx, o, StrKey(name), descObj); err != nil {
				return err
			}
		}
	}
	return nil
}

// mustGet fetches a property, ignoring errors (used where the receiver is known
// to be a well-formed object).
func mustGet(ctx context.Context, this Value, name string) Value {
	if o, ok := this.(*Object); ok {
		v, _ := o.GetStr(ctx, name)
		return v
	}
	return Undef
}
