package interp

import "context"

// objectProtoToString implements %Object.prototype.toString% (§20.1.3.6). It is
// exposed as a named method so Array.prototype.toString can fall back to the
// intrinsic directly (§23.1.3.36 step 3) even when the user has deleted
// Object.prototype.toString from the prototype chain.
func (i *Interpreter) objectProtoToString(ctx context.Context, this Value) (Value, error) {
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
	// Determine builtinTag from the object's kind. IsArray is proxy-aware and
	// throws for a revoked proxy; the remaining slots are recognized by the
	// object's internal class. A callable object (including a callable Proxy)
	// is "Function" regardless of its class, so the more specific generator/
	// async tags come only from @@toStringTag on the prototype chain.
	isArr, err := i.isArrayV(ctx, o)
	if err != nil {
		return nil, err
	}
	var builtinTag string
	switch {
	case isArr:
		builtinTag = "Array"
	case o.class == "Arguments":
		builtinTag = "Arguments"
	case o.IsCallable():
		builtinTag = "Function"
	case o.class == "Error":
		builtinTag = "Error"
	case o.class == "Boolean":
		builtinTag = "Boolean"
	case o.class == "Number":
		builtinTag = "Number"
	case o.class == "String":
		builtinTag = "String"
	case o.class == "Date":
		builtinTag = "Date"
	case o.class == "RegExp":
		builtinTag = "RegExp"
	default:
		builtinTag = "Object"
	}
	// tag = Get(O, @@toStringTag); a String result overrides builtinTag, and an
	// abrupt getter propagates. Any non-string tag is ignored.
	tagVal, err := i.getV(ctx, o, SymKey(i.symToStringTag), o)
	if err != nil {
		return nil, err
	}
	tag := builtinTag
	if s, ok := tagVal.(String); ok {
		tag = string(s)
	}
	return String("[object " + tag + "]"), nil
}

// initObject installs the Object constructor and Object.prototype methods.
func (i *Interpreter) initObject() {
	proto := i.objectProto
	// %Object.prototype% is an immutable-prototype exotic object (§10.4.7):
	// setting its prototype to anything but its current [[Prototype]] (null)
	// throws.
	proto.immutableProto = true

	i.defineMethod(proto, "hasOwnProperty", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// §20.1.3.2: ToPropertyKey(P) runs before ToObject(this value).
		key, err := i.ToPropertyKey(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		o, err := i.ToObject(ctx, this)
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
		// §20.1.3.4: if V is not an object, return false; otherwise ToObject(this)
		// (throwing for a nullish receiver) and walk V's prototype chain via
		// [[GetPrototypeOf]] so a Proxy's getPrototypeOf trap is observed.
		target, ok := arg(args, 0).(*Object)
		if !ok {
			return False, nil
		}
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		cur := target
		for {
			p, err := i.getProtoV(ctx, cur)
			if err != nil {
				return nil, err
			}
			po, ok := p.(*Object)
			if !ok {
				return False, nil
			}
			if po == o {
				return True, nil
			}
			cur = po
		}
	})
	i.defineMethod(proto, "propertyIsEnumerable", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// §20.1.3.5: ToPropertyKey(V) runs before ToObject(this value).
		key, err := i.ToPropertyKey(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		o, err := i.ToObject(ctx, this)
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
		return i.objectProtoToString(ctx, this)
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
	// §B.2.2.2/3: build the accessor half descriptor { [[Get]]/[[Set]]: fn,
	// [[Enumerable]]: true, [[Configurable]]: true } and DefinePropertyOrThrow it,
	// so an existing configurable property is merged, a non-configurable one (or a
	// non-extensible/Proxy-rejected target) throws a TypeError, and a Proxy's
	// defineProperty trap runs.
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
		desc := NewObject(i.objectProto)
		if isGet {
			desc.SetData("get", fn)
		} else {
			desc.SetData("set", fn)
		}
		desc.SetData("enumerable", True)
		desc.SetData("configurable", True)
		if err := i.applyDescriptor(ctx, o, key, desc); err != nil {
			return nil, err
		}
		return Undef, nil
	}
	// §B.2.2.4/5: walk the prototype chain via [[GetOwnProperty]]/[[GetPrototypeOf]]
	// (so a Proxy's traps run and can propagate an abrupt completion), returning
	// the accessor half of the first own property found.
	lookupAccessorHalf := func(ctx context.Context, this Value, args []Value, isGet bool) (Value, error) {
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		for cur := o; cur != nil; {
			p, ok, err := i.getOwnPropertyV(ctx, cur, key)
			if err != nil {
				return nil, err
			}
			if ok {
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
			next, err := i.getProtoV(ctx, cur)
			if err != nil {
				return nil, err
			}
			no, ok := next.(*Object)
			if !ok {
				break
			}
			cur = no
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

	// §B.2.2.1: Object.prototype.__proto__ is an accessor property
	// { enumerable: false, configurable: true } whose get/set expose
	// [[GetPrototypeOf]]/[[SetPrototypeOf]].
	protoGet := i.newNativeFunc("get __proto__", 0, func(ctx context.Context, this Value, _ []Value) (Value, error) {
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		return i.getProtoV(ctx, o)
	})
	protoSet := i.newNativeFunc("set __proto__", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if IsNullish(this) {
			return nil, i.throwError(ctx, "TypeError", "Object.prototype.__proto__ called on null or undefined")
		}
		v := arg(args, 0)
		// Silently ignore a value that is neither an Object nor null.
		if _, isObj := v.(*Object); !isObj {
			if _, isNull := v.(Null); !isNull {
				return Undef, nil
			}
		}
		// A primitive receiver has no observable prototype slot to set: no-op.
		o, ok := this.(*Object)
		if !ok {
			return Undef, nil
		}
		status, err := i.setProtoV(ctx, o, v)
		if err != nil {
			return nil, err
		}
		if !status {
			return nil, i.throwError(ctx, "TypeError", "Object.prototype.__proto__: cannot set prototype")
		}
		return Undef, nil
	})
	proto.defineOwn(StrKey("__proto__"), &Property{
		Accessor: true, Get: protoGet, Set: protoSet, Enumerable: false, Configurable: true,
	})

	// Object constructor.
	var ctor *Object
	objectCall := func(ctx context.Context, this Value, args []Value) (Value, error) {
		v := arg(args, 0)
		if IsNullish(v) {
			return NewObject(i.objectProto), nil
		}
		return i.ToObject(ctx, v)
	}
	ctor = i.newNativeCtor("Object", 1, objectCall, func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		// §20.1.1.1: when Object is subclassed (NewTarget is neither the Object
		// constructor itself nor undefined), ignore the argument and create a fresh
		// ordinary object using NewTarget's "prototype".
		if nt, ok := newTarget.(*Object); ok && nt != ctor {
			p, err := i.protoFromConstructor(ctx, nt, func(r *Interpreter) *Object { return r.objectProto })
			if err != nil {
				return nil, err
			}
			return NewObject(p), nil
		}
		return objectCall(ctx, newTarget, args)
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
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return arg(args, 0), nil
		}
		status, err := i.integritySet(ctx, o, true)
		if err != nil {
			return nil, err
		}
		if !status {
			return nil, i.throwError(ctx, "TypeError", "Object.freeze: unable to freeze object")
		}
		return o, nil
	})
	i.defineMethod(ctor, "isFrozen", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return True, nil
		}
		frozen, err := i.integrityTest(ctx, o, true)
		if err != nil {
			return nil, err
		}
		return Bool(frozen), nil
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
		// §20.1.2.7: RequireObjectCoercible(iterable), then AddEntriesFromIterable
		// (§7.1.16) — for each iterator value, require an Object entry, read its
		// "0"/"1", and CreateDataPropertyOrThrow. Any abrupt completion closes the
		// iterator.
		if IsNullish(arg(args, 0)) {
			return nil, i.throwError(ctx, "TypeError", "Object.fromEntries called on null or undefined")
		}
		o := NewObject(i.objectProto)
		err := i.addFromIterable(ctx, arg(args, 0), func(entry Value) error {
			eo, ok := entry.(*Object)
			if !ok {
				return i.throwError(ctx, "TypeError", "Object.fromEntries: iterator value "+briefValue(entry)+" is not an entry object")
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
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return arg(args, 0), nil
		}
		status, err := i.integritySet(ctx, o, false)
		if err != nil {
			return nil, err
		}
		if !status {
			return nil, i.throwError(ctx, "TypeError", "Object.seal: unable to seal object")
		}
		return o, nil
	})
	i.defineMethod(ctor, "isSealed", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return True, nil
		}
		sealed, err := i.integrityTest(ctx, o, false)
		if err != nil {
			return nil, err
		}
		return Bool(sealed), nil
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

// integritySet implements SetIntegrityLevel (§7.3.15). Ordinary objects use the
// in-place fast path; a Proxy or TypedArray routes through [[PreventExtensions]]
// and [[DefineOwnProperty]] so handler traps run and any refusal (a rejected
// preventExtensions, a non-writable TypedArray element, ...) surfaces as false —
// which Object.freeze/seal turn into a TypeError.
func (i *Interpreter) integritySet(ctx context.Context, o *Object, frozen bool) (bool, error) {
	if o.proxy == nil && o.typedArray == nil {
		o.setIntegrityLevel(frozen)
		return true, nil
	}
	ok, err := i.preventExtensionsV(ctx, o)
	if err != nil || !ok {
		return ok, err
	}
	keys, err := i.ownKeysV(ctx, o)
	if err != nil {
		return false, err
	}
	for _, k := range keys {
		desc := NewObject(i.objectProto)
		if frozen {
			// Freezing reads the current descriptor to decide whether the property
			// is an accessor (only [[Configurable]] cleared) or data (also
			// [[Writable]] cleared).
			cur, ok, err := i.getOwnPropertyV(ctx, o, k)
			if err != nil {
				return false, err
			}
			if !ok {
				continue
			}
			desc.SetData("configurable", False)
			if !cur.Accessor {
				desc.SetData("writable", False)
			}
		} else {
			// Sealing only makes properties non-configurable.
			desc.SetData("configurable", False)
		}
		if err := i.applyDescriptor(ctx, o, k, desc); err != nil {
			return false, err
		}
	}
	return true, nil
}

// integrityTest implements TestIntegrityLevel (§7.3.16). Ordinary objects use
// the fast path; a Proxy routes through [[IsExtensible]], [[OwnPropertyKeys]]
// and [[GetOwnProperty]] so its traps are observed.
func (i *Interpreter) integrityTest(ctx context.Context, o *Object, frozen bool) (bool, error) {
	if o.proxy == nil {
		return o.testIntegrityLevel(frozen), nil
	}
	ext, err := i.isExtensibleV(ctx, o)
	if err != nil {
		return false, err
	}
	if ext {
		return false, nil
	}
	keys, err := i.ownKeysV(ctx, o)
	if err != nil {
		return false, err
	}
	for _, k := range keys {
		cur, ok, err := i.getOwnPropertyV(ctx, o, k)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		if cur.Configurable {
			return false, nil
		}
		if frozen && !cur.Accessor && cur.Writable {
			return false, nil
		}
	}
	return true, nil
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
	// A Module Namespace exotic object's [[DefineOwnProperty]] (§10.4.6.7): a
	// string export accepts only a redefinition that leaves its fixed attributes
	// unchanged (and its value equal); symbol keys are ordinary.
	if o.namespace != nil && !key.IsSymbol() {
		return i.namespaceDefineOwn(ctx, o, key, desc)
	}
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
	// A mapped arguments object's [[DefineOwnProperty]] (§10.4.4.2) applies the
	// descriptor ordinarily, then reconciles the parameter map: redefining a
	// mapped index as an accessor or as non-writable breaks the alias, while a
	// new [[Value]] flows through to the aliased parameter binding.
	if _, ok := o.mappedBinding(key); ok {
		return i.argumentsDefineOwn(ctx, o, key, desc)
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
		// is non-writable (Array [[DefineOwnProperty]] index case, §10.4.2.1 step
		// 3.b). The bound is the logical length (ArrayLen), so an index within a
		// sparse tail below length is still allowed.
		if idx, ok := arrayIndex(key.Str); ok && o.lengthNonWritable && idx >= o.ArrayLen() {
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

// argumentsDefineOwn implements the mapped-arguments [[DefineOwnProperty]]
// specialization (§10.4.4.2) for a currently-mapped integer index. The ordinary
// definition is applied first; because getOwn already reports the live binding
// value as the property's current [[Value]], an attribute-only redefinition
// (e.g. {writable:false} with no value) freezes the property at that live value
// with no extra work (step 3). Afterwards the parameter map is reconciled:
//   - an accessor redefinition breaks the alias (step 8.a);
//   - a supplied [[Value]] flows through to the aliased parameter binding
//     (step 8.b.i, Set(map, P, Desc.[[Value]]));
//   - {writable:false} breaks the alias (step 8.b.ii).
func (i *Interpreter) argumentsDefineOwn(ctx context.Context, o *Object, key PropertyKey, desc *Object) (bool, error) {
	b := o.paramMap[key.Str]
	// Descriptor shape via HasProperty (matching ordinaryDefineOwn's
	// ToPropertyDescriptor), which never invokes user getters.
	isAccessor := desc.Has(StrKey("get")) || desc.Has(StrKey("set"))
	hasValue := desc.Has(StrKey("value"))
	hasWritable := desc.Has(StrKey("writable"))

	allowed, err := i.ordinaryDefineOwn(ctx, o, key, desc)
	if err != nil || !allowed {
		return allowed, err
	}
	// The index is (still) stored in the props map after a successful define; its
	// applied descriptor drives the reconciliation.
	p, ok := o.props[key]
	switch {
	case isAccessor:
		delete(o.paramMap, key.Str)
	case ok && !p.Accessor:
		if hasValue {
			b.value = p.Value
		}
		if hasWritable && !p.Writable {
			delete(o.paramMap, key.Str)
		}
	default:
		delete(o.paramMap, key.Str)
	}
	return true, nil
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
	// ToPropertyDescriptor step 9: the "cannot specify both accessors and a
	// value/writable attribute" TypeError is raised only AFTER every present field
	// has been Get in order (enumerable, configurable, value, writable, get, set) —
	// so a descriptor whose "value" and "get" are themselves getters runs both of
	// them before this rejection.
	if (hasGet || hasSet) && (hasValue || hasWritable) {
		return false, i.throwError(ctx, "TypeError",
			"Invalid property descriptor. Cannot both specify accessors and a value or writable attribute, "+keyName(key))
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
	// A descriptor with no [[Value]] cannot change length's value: it is an
	// attribute-only change. (An accessor descriptor has no value either and,
	// against the data "length" property, is rejected below.)
	if !desc.Has(StrKey("value")) {
		return i.arrayDefineLengthAttrs(ctx, o, desc, nil)
	}

	// Steps 3–5: coerce the requested value BEFORE validating the descriptor's
	// attributes or the current length's writability. ToUint32 performs one
	// ToNumber and step 4's ToNumber performs a second, so a value with a
	// user-defined [Symbol.toPrimitive]/valueOf is coerced twice (both with the
	// "number" hint), and a RangeError for a non-uint32 length is thrown ahead
	// of any TypeError from descriptor validation.
	v, err := desc.GetStr(ctx, "value")
	if err != nil {
		return false, err
	}
	num1, err := i.ToNumberV(ctx, v) // ToUint32's internal ToNumber (step 3)
	if err != nil {
		return false, err
	}
	newLen := ToUint32(num1)
	num2, err := i.ToNumberV(ctx, v) // ToNumber (step 4)
	if err != nil {
		return false, err
	}
	if float64(newLen) != num2 { // SameValueZero(newLen, numberLen) is false
		return false, i.throwError(ctx, "RangeError", "Invalid array length")
	}
	return i.arrayDefineLengthAttrs(ctx, o, desc, &newLen)
}

// arrayDefineLengthAttrs validates and applies the non-value part of an Array
// "length" [[DefineOwnProperty]] (the OrdinaryDefineOwnProperty of ArraySetLength
// steps 9/13): length is a non-configurable, non-enumerable data property, so a
// descriptor requesting configurable/enumerable true, or making an already
// non-writable length writable, is rejected. When newLen is non-nil it also
// applies the length change, honoring non-configurable-element blocking. It
// assumes any value coercion (and its RangeError) has already happened.
func (i *Interpreter) arrayDefineLengthAttrs(ctx context.Context, o *Object, desc *Object, newLen *uint32) (bool, error) {
	if desc.Has(StrKey("get")) || desc.Has(StrKey("set")) {
		return false, nil // an accessor descriptor cannot redefine a data property here
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
	newWritable := true
	hasWritable := desc.Has(StrKey("writable"))
	if hasWritable {
		v, err := desc.GetStr(ctx, "writable")
		if err != nil {
			return false, err
		}
		newWritable = ToBoolean(v)
		// A non-writable length can never be made writable again (length is
		// never configurable).
		if o.lengthNonWritable && newWritable {
			return false, nil
		}
	}
	if newLen == nil {
		// Attribute-only change: apply the writable transition (to false).
		if hasWritable && !newWritable {
			o.lengthNonWritable = true
		}
		return true, nil
	}

	oldLen := o.ArrayLen()
	// Any change to length's value while it is non-writable is rejected: growing
	// routes through OrdinaryDefineOwnProperty (ArraySetLength step 10), which
	// refuses to change a non-writable data property's value, and shrinking is
	// rejected by ArraySetLength step 12. A value that leaves length unchanged is
	// a no-op regardless of writability.
	if int(*newLen) != oldLen && o.lengthNonWritable {
		return false, nil
	}
	// Apply the new length. Growing past the dense limit records a sparse tail
	// rather than eagerly allocating holes; shrinking deletes the covered
	// elements, stopping (and reporting failure) at any non-configurable one.
	ok := o.applyArrayLength(int(*newLen))
	if hasWritable && !newWritable {
		o.lengthNonWritable = true
	}
	return ok, nil
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
