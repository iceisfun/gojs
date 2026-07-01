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
		return Bool(o.HasOwn(key)), nil
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
		if p, ok := o.getOwn(key); ok {
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
		return i.newArray(i.enumerableKeys(o, func(k string, _ Value) Value { return String(k) })), nil
	})
	i.defineMethod(ctor, "values", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		vals := i.enumerableKeys(o, func(_ string, v Value) Value { return v })
		return i.newArray(vals), nil
	})
	i.defineMethod(ctor, "entries", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		pairs := i.enumerableKeys(o, func(k string, v Value) Value {
			return i.newArray([]Value{String(k), v})
		})
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
		if o.proto == nil {
			return Nul, nil
		}
		return o.proto, nil
	})
	i.defineMethod(ctor, "setPrototypeOf", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return arg(args, 0), nil
		}
		switch p := arg(args, 1).(type) {
		case *Object:
			o.proto = p
		case Null:
			o.proto = nil
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
		var out []Value
		for _, name := range o.OwnKeys() {
			out = append(out, String(name))
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

	i.objectCtor = ctor
	i.setGlobalHidden("Object", ctor)
}

// enumerableKeys collects a value for each own enumerable string-keyed property
// of o, in key order, via project.
func (i *Interpreter) enumerableKeys(o *Object, project func(string, Value) Value) []Value {
	var out []Value
	for _, name := range o.OwnKeys() {
		if p, ok := o.getOwn(StrKey(name)); ok && p.Enumerable {
			v, _ := o.GetStr(context.Background(), name)
			out = append(out, project(name, v))
		}
	}
	return out
}

// applyDescriptor installs a property from a descriptor object.
func (i *Interpreter) applyDescriptor(ctx context.Context, o *Object, key PropertyKey, desc *Object) error {
	p := &Property{}
	hasGet := desc.HasOwn(StrKey("get"))
	hasSet := desc.HasOwn(StrKey("set"))
	if hasGet || hasSet {
		p.Accessor = true
		if g, _ := desc.GetStr(ctx, "get"); g != nil {
			if go_, ok := g.(*Object); ok {
				p.Get = go_
			}
		}
		if s, _ := desc.GetStr(ctx, "set"); s != nil {
			if so, ok := s.(*Object); ok {
				p.Set = so
			}
		}
	} else {
		v, _ := desc.GetStr(ctx, "value")
		p.Value = v
		if w, _ := desc.GetStr(ctx, "writable"); ToBoolean(w) {
			p.Writable = true
		}
	}
	if e, _ := desc.GetStr(ctx, "enumerable"); ToBoolean(e) {
		p.Enumerable = true
	}
	if c, _ := desc.GetStr(ctx, "configurable"); ToBoolean(c) {
		p.Configurable = true
	}
	o.defineOwn(key, p)
	return nil
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
