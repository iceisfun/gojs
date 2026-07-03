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
		// An Arguments object is backed by an Array in this engine, but its
		// builtin tag is "Arguments" (§20.1.3.6 checks [[ParameterMap]] before
		// falling back to the ordinary class), so it must not report "Array".
		if o.isArray && o.class != "Arguments" {
			tag = "Array"
		} else if o.IsCallable() {
			tag = "Function"
		}
		return String("[object " + tag + "]"), nil
	})
	i.defineMethod(proto, "toLocaleString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// Invoke(this, "toString"): getProperty boxes a primitive receiver so
		// e.g. (42).toLocaleString() reaches Number.prototype.toString.
		m, err := i.getProperty(ctx, this, StrKey("toString"))
		if err != nil {
			return nil, err
		}
		return i.call(ctx, m, this, nil)
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.ToObject(ctx, this)
	})

	// Annex B accessor helpers. __defineGetter__/__defineSetter__ install an
	// accessor half (merging with an existing accessor); __lookupGetter__/
	// __lookupSetter__ walk the prototype chain for the accessor half.
	defineAccessorHalf := func(ctx context.Context, this Value, args []Value, isGet bool) (Value, error) {
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		fn, ok := arg(args, 1).(*Object)
		if !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Object.prototype.__define"+map[bool]string{true: "Getter", false: "Setter"}[isGet]+"__: Expecting function")
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		existing, had := o.getOwn(key)
		if had && existing.Accessor {
			if isGet {
				existing.Get = fn
			} else {
				existing.Set = fn
			}
			return Undef, nil
		}
		p := &Property{Accessor: true, Enumerable: true, Configurable: true}
		if isGet {
			p.Get = fn
		} else {
			p.Set = fn
		}
		o.defineOwn(key, p)
		return Undef, nil
	}
	lookupAccessorHalf := func(ctx context.Context, this Value, args []Value, isGet bool) (Value, error) {
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		for cur := o; cur != nil; cur = cur.proto {
			p, ok := cur.getOwn(key)
			if !ok {
				continue
			}
			if p.Accessor {
				fn := p.Get
				if !isGet {
					fn = p.Set
				}
				if fn == nil {
					return Undef, nil
				}
				return fn, nil
			}
			return Undef, nil // a data property shadows any inherited accessor
		}
		return Undef, nil
	}
	i.defineMethod(proto, "__defineGetter__", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return defineAccessorHalf(ctx, this, args, true)
	})
	i.defineMethod(proto, "__defineSetter__", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return defineAccessorHalf(ctx, this, args, false)
	})
	i.defineMethod(proto, "__lookupGetter__", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return lookupAccessorHalf(ctx, this, args, true)
	})
	i.defineMethod(proto, "__lookupSetter__", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return lookupAccessorHalf(ctx, this, args, false)
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
			// CopyDataProperties (§7.3.25): copy every own *enumerable* property —
			// string- and symbol-keyed, in [[OwnPropertyKeys]] order — reading each
			// value with [[Get]] and writing it with [[Set]] so getters/setters and
			// Proxy traps run.
			keys, err := i.ownKeysV(ctx, so)
			if err != nil {
				return nil, err
			}
			for _, k := range keys {
				p, ok, err := i.getOwnPropertyV(ctx, so, k)
				if err != nil {
					return nil, err
				}
				if !ok || !p.Enumerable {
					continue
				}
				v, err := i.getV(ctx, so, k, so)
				if err != nil {
					return nil, err
				}
				ok, err = i.setV(ctx, target, k, v, target)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, i.throwError(ctx, "TypeError", "Cannot assign to read only property '"+keyName(k)+"'")
				}
			}
		}
		return target, nil
	})
	i.defineMethod(ctor, "freeze", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok {
			o.setIntegrityLevel(true)
		}
		return arg(args, 0), nil
	})
	i.defineMethod(ctor, "isFrozen", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok {
			return Bool(o.testIntegrityLevel(true)), nil
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
		if !IsUndefined(arg(args, 1)) {
			props, err := i.ToObject(ctx, arg(args, 1))
			if err != nil {
				return nil, err
			}
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
		props, err := i.ToObject(ctx, arg(args, 1))
		if err != nil {
			return nil, err
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
		// §20.1.2.9: iterate every own key (string then symbol) through
		// [[OwnPropertyKeys]]/[[GetOwnProperty]] and CreateDataProperty each
		// descriptor onto the result under its original (possibly symbol) key.
		keys, err := i.ownKeysV(ctx, o)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			p, ok, err := i.getOwnPropertyV(ctx, o, k)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			out.writeData(k, i.descriptorToObject(p))
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
			// §20.1.2.18: if O.[[PreventExtensions]]() returns false, throw a
			// TypeError (a Proxy's preventExtensions trap may reject the request).
			ok, err := i.preventExtensionsV(ctx, o)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "Object.preventExtensions called on object that could not be made non-extensible")
			}
		}
		return arg(args, 0), nil
	})
	i.defineMethod(ctor, "seal", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok {
			o.setIntegrityLevel(false)
		}
		return arg(args, 0), nil
	})
	i.defineMethod(ctor, "isSealed", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return True, nil
		}
		return Bool(o.testIntegrityLevel(false)), nil
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
	// A TypedArray's canonical numeric index [[DefineOwnProperty]] (§10.4.5.3):
	// only a {writable, enumerable, configurable} data descriptor for a valid
	// index is accepted, and its value is written through TypedArraySetElement.
	if o.typedArray != nil && !key.IsSymbol() {
		if n, ok := canonicalNumericIndex(key.Str); ok {
			if _, valid := o.typedArray.validIndex(n); !valid {
				return false, nil
			}
			ok, err := i.taValidateElementDescriptor(ctx, o, n, desc)
			if err != nil || !ok {
				return ok, err
			}
			return true, nil
		}
	}
	// An Array is an exotic object (§10.4.2): "length" and array-index keys are
	// backed by the dense element store rather than ordinary property slots, so
	// they need [[DefineOwnProperty]] specialization (ArraySetLength and the
	// index/length coupling). Accessor or otherwise-unrepresentable index
	// descriptors fall through to ordinary storage.
	if o.isArray && !key.IsSymbol() {
		if key.Str == "length" {
			return i.arraySetLength(ctx, o, desc)
		}
		// Adding an index at or beyond the current length is refused when length
		// is non-writable (Array [[DefineOwnProperty]] index case, §10.4.2.1).
		if idx, ok := arrayIndex(key.Str); ok && o.lengthNonWritable && idx >= len(o.elems) {
			return false, nil
		}
	}
	// Array index keys are handled by the generic path: ordinaryDefineOwn
	// validates the redefinition and installProperty stores the result, keeping
	// a default-attribute data property dense and de-optimizing anything else
	// (non-default attributes or an accessor) into the ordinary props map while
	// extending the array's length to cover the index (§10.4.2.1).
	return i.ordinaryDefineOwn(ctx, o, key, desc)
}

// ordinaryDefineOwn implements OrdinaryDefineOwnProperty /
// ValidateAndApplyPropertyDescriptor (§10.1.6) for a plain property slot. It
// merges only the fields the descriptor actually specifies onto any existing
// property (leaving the rest untouched) and enforces the invariants for
// non-configurable and non-writable properties.
func (i *Interpreter) ordinaryDefineOwn(ctx context.Context, o *Object, key PropertyKey, desc *Object) (bool, error) {

	// Which attributes does the descriptor specify? Presence — not truthiness —
	// determines what gets applied; absent fields are inherited from the
	// current property (or take spec defaults for a brand-new one). ToProperty-
	// Descriptor (§6.2.6.5) uses HasProperty, so an inherited descriptor field
	// counts (desc.Has walks the prototype chain, as GetStr already does).
	hasEnum := desc.Has(StrKey("enumerable"))
	hasConf := desc.Has(StrKey("configurable"))
	hasValue := desc.Has(StrKey("value"))
	hasWritable := desc.Has(StrKey("writable"))
	hasGet := desc.Has(StrKey("get"))
	hasSet := desc.Has(StrKey("set"))

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
		o.installProperty(key, p)
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
	o.installProperty(key, &np)
	return true, nil
}

// arraySetLength implements the "length" case of Array [[DefineOwnProperty]]
// (§10.4.2.4 ArraySetLength). Array length is a non-configurable, non-enumerable
// data property whose value is a uint32; its [[Writable]] attribute is tracked
// via lengthNonWritable. Truncating length deletes elements from the highest
// index down and is blocked by any non-configurable (de-optimized) element.
func (i *Interpreter) arraySetLength(ctx context.Context, o *Object, desc *Object) (bool, error) {
	// Length can never become an accessor, configurable, or enumerable.
	if desc.Has(StrKey("get")) || desc.Has(StrKey("set")) {
		return false, nil
	}
	if desc.Has(StrKey("configurable")) {
		v, err := desc.GetStr(ctx, "configurable")
		if err != nil {
			return false, err
		}
		if ToBoolean(v) {
			return false, nil
		}
	}
	if desc.Has(StrKey("enumerable")) {
		v, err := desc.GetStr(ctx, "enumerable")
		if err != nil {
			return false, err
		}
		if ToBoolean(v) {
			return false, nil
		}
	}
	// Read the requested [[Writable]] attribute, if any. Once length is
	// non-writable it can never be made writable again (unless configurable,
	// which it never is here).
	newWritable := true
	hasWritable := desc.Has(StrKey("writable"))
	if hasWritable {
		v, err := desc.GetStr(ctx, "writable")
		if err != nil {
			return false, err
		}
		newWritable = ToBoolean(v)
		if o.lengthNonWritable && newWritable {
			return false, nil
		}
	}

	if !desc.Has(StrKey("value")) {
		// Attribute-only change: apply the writable transition (to false).
		if hasWritable && !newWritable {
			o.lengthNonWritable = true
		}
		return true, nil
	}

	v, err := desc.GetStr(ctx, "value")
	if err != nil {
		return false, err
	}
	num, err := i.ToNumberV(ctx, v)
	if err != nil {
		return false, err
	}
	newLen := ToUint32(num)
	if float64(newLen) != num {
		return false, i.throwError(ctx, "RangeError", "Invalid array length")
	}
	oldLen := len(o.elems)
	// A non-writable length rejects any change to its value.
	if o.lengthNonWritable && int(newLen) != oldLen {
		return false, nil
	}
	// The array's backing store is dense, so growing to a very large length
	// would eagerly allocate that many holes. Refuse such lengths rather than
	// exhaust memory (a limitation shared with ordinary "length" assignment).
	if int(newLen) > oldLen && newLen > maxDenseArrayLen {
		return false, i.throwError(ctx, "RangeError", "Array length exceeds the supported dense-array limit")
	}
	if int(newLen) < oldLen {
		// Delete elements from oldLen-1 down to newLen. A de-optimized index
		// that is non-configurable cannot be deleted: stop there, set length to
		// that index + 1, apply the writable transition, and report failure.
		for idx := oldLen - 1; idx >= int(newLen); idx-- {
			if p, ok := o.props[StrKey(intToStr(idx))]; ok && !p.Configurable {
				o.setArrayLength(Number(float64(idx + 1)))
				if hasWritable && !newWritable {
					o.lengthNonWritable = true
				}
				return false, nil
			}
		}
		for idx := oldLen - 1; idx >= int(newLen); idx-- {
			o.removeProp(StrKey(intToStr(idx)))
		}
	}
	o.setArrayLength(Number(float64(newLen)))
	if hasWritable && !newWritable {
		o.lengthNonWritable = true
	}
	return true, nil
}

// maxDenseArrayLen bounds how far the dense element backing may be grown in a
// single [[DefineOwnProperty]] to avoid pathological allocation.
const maxDenseArrayLen = 1 << 24

// defineProperties implements the shared body of ObjectDefineProperties
// (§20.1.2.3.1): it gathers every own enumerable property key of props (string
// and symbol), reads and ToPropertyDescriptor-validates each — invoking any
// getters — and only then applies them to o, matching the spec's read-all-then-
// define-all ordering and error behavior.
func (i *Interpreter) defineProperties(ctx context.Context, o, props *Object) error {
	keys, err := i.ownKeysV(ctx, props)
	if err != nil {
		return err
	}
	type keyedDesc struct {
		key  PropertyKey
		desc *Object
	}
	var pending []keyedDesc
	for _, k := range keys {
		p, ok, err := i.getOwnPropertyV(ctx, props, k)
		if err != nil {
			return err
		}
		if !ok || !p.Enumerable {
			continue
		}
		dv, err := i.getV(ctx, props, k, props)
		if err != nil {
			return err
		}
		descObj, ok := dv.(*Object)
		if !ok {
			return i.throwError(ctx, "TypeError", "Property description must be an object")
		}
		pending = append(pending, keyedDesc{k, descObj})
	}
	for _, e := range pending {
		if err := i.applyDescriptor(ctx, o, e.key, e.desc); err != nil {
			return err
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
