package interp

import "context"

// This file implements the %Reflect% namespace object (ECMA-262 §28.1). Each
// method delegates to the corresponding object internal method through the
// dispatch helpers below, which are the single choke points where a Proxy
// exotic object intercepts an operation (see builtin_proxy.go). For an ordinary
// object the helpers perform the standard OrdinaryXxx behavior.

// ---------------------------------------------------------------------------
// Internal-method dispatch helpers
//
// These wrap the [[Get]]/[[Set]]/[[HasProperty]]/[[Delete]]/[[GetOwnProperty]]/
// [[OwnPropertyKeys]]/[[DefineOwnProperty]]/[[GetPrototypeOf]]/
// [[SetPrototypeOf]]/[[IsExtensible]]/[[PreventExtensions]] internal methods so
// that both Reflect and the evaluator can reach them uniformly and so a Proxy
// can hook every one in a single place.
// ---------------------------------------------------------------------------

// getV performs [[Get]] with an explicit receiver.
func (i *Interpreter) getV(ctx context.Context, o *Object, key PropertyKey, receiver Value) (Value, error) {
	return o.getWithReceiver(ctx, key, receiver)
}

// setV performs [[Set]] with an explicit receiver, returning whether the write
// succeeded.
func (i *Interpreter) setV(ctx context.Context, o *Object, key PropertyKey, v, receiver Value) (bool, error) {
	if o.proxy != nil {
		return o.proxy.set(ctx, key, v, receiver)
	}
	return i.ordinarySet(ctx, o, key, v, receiver)
}

// hasV performs [[HasProperty]], walking the prototype chain so a Proxy at any
// depth intercepts the lookup.
func (i *Interpreter) hasV(ctx context.Context, o *Object, key PropertyKey) (bool, error) {
	if o.typedArray != nil && !key.IsSymbol() {
		if n, ok := canonicalNumericIndex(key.Str); ok {
			_, valid := o.typedArray.validIndex(n)
			return valid, nil
		}
	}
	for cur := o; cur != nil; cur = cur.proto {
		if cur.proxy != nil {
			return cur.proxy.has(ctx, key)
		}
		if _, ok := cur.getOwn(key); ok {
			return true, nil
		}
	}
	return false, nil
}

// deleteV performs [[Delete]].
func (i *Interpreter) deleteV(ctx context.Context, o *Object, key PropertyKey) (bool, error) {
	if o.proxy != nil {
		return o.proxy.deleteProperty(ctx, key)
	}
	return o.Delete(key), nil
}

// getOwnPropertyV performs [[GetOwnProperty]], returning the descriptor or
// ok=false when the property is absent.
func (i *Interpreter) getOwnPropertyV(ctx context.Context, o *Object, key PropertyKey) (*Property, bool, error) {
	if o.proxy != nil {
		return o.proxy.getOwnProperty(ctx, key)
	}
	p, ok := o.getOwn(key)
	return p, ok, nil
}

// ownKeysV performs [[OwnPropertyKeys]], returning every own key (string then
// symbol) in the spec-mandated order.
func (i *Interpreter) ownKeysV(ctx context.Context, o *Object) ([]PropertyKey, error) {
	if o.proxy != nil {
		return o.proxy.ownKeys(ctx)
	}
	return o.ownPropertyKeys(), nil
}

// definePropertyV performs [[DefineOwnProperty]] from a descriptor object,
// returning whether it was applied.
func (i *Interpreter) definePropertyV(ctx context.Context, o *Object, key PropertyKey, desc *Object) (bool, error) {
	if o.proxy != nil {
		return o.proxy.defineProperty(ctx, key, desc)
	}
	return i.defineOwnFromDescriptor(ctx, o, key, desc)
}

// getProtoV performs [[GetPrototypeOf]], returning the prototype object or Null.
func (i *Interpreter) getProtoV(ctx context.Context, o *Object) (Value, error) {
	if o.proxy != nil {
		return o.proxy.getPrototypeOf(ctx)
	}
	if o.proto == nil {
		return Nul, nil
	}
	return o.proto, nil
}

// setProtoV performs [[SetPrototypeOf]] (proto is an *Object or Null), returning
// whether it succeeded.
func (i *Interpreter) setProtoV(ctx context.Context, o *Object, proto Value) (bool, error) {
	if o.proxy != nil {
		return o.proxy.setPrototypeOf(ctx, proto)
	}
	return i.ordinarySetPrototypeOf(o, proto), nil
}

// isExtensibleV performs [[IsExtensible]].
func (i *Interpreter) isExtensibleV(ctx context.Context, o *Object) (bool, error) {
	if o.proxy != nil {
		return o.proxy.isExtensible(ctx)
	}
	return o.extensible, nil
}

// preventExtensionsV performs [[PreventExtensions]].
func (i *Interpreter) preventExtensionsV(ctx context.Context, o *Object) (bool, error) {
	if o.proxy != nil {
		return o.proxy.preventExtensions(ctx)
	}
	o.extensible = false
	return true, nil
}

// ordinarySet implements OrdinarySet / OrdinarySetWithOwnDescriptor (§10.1.9)
// with an explicit receiver, returning whether the assignment took effect.
func (i *Interpreter) ordinarySet(ctx context.Context, o *Object, key PropertyKey, v, receiver Value) (bool, error) {
	// A TypedArray's canonical numeric index [[Set]] (§10.4.5.5): when the
	// receiver is the typed array itself, write the element; otherwise an
	// out-of-bounds index is a silent success and an in-bounds index falls
	// through to OrdinarySet with the alternate receiver.
	if o.typedArray != nil && !key.IsSymbol() {
		if n, ok := canonicalNumericIndex(key.Str); ok {
			if receiver == Value(o) {
				return i.typedArraySetElement(ctx, o.typedArray, n, v)
			}
			if _, valid := o.typedArray.validIndex(n); !valid {
				return true, nil
			}
		}
	}
	ownDesc, ok := o.getOwn(key)
	if !ok {
		if o.proto != nil {
			return i.setV(ctx, o.proto, key, v, receiver)
		}
		ownDesc = &Property{Value: Undef, Writable: true, Enumerable: true, Configurable: true}
	}
	if ownDesc.Accessor {
		if ownDesc.Set == nil {
			return false, nil
		}
		_, err := ownDesc.Set.fn.call(ctx, receiver, []Value{v})
		return err == nil, err
	}
	if !ownDesc.Writable {
		return false, nil
	}
	recv, ok := receiver.(*Object)
	if !ok {
		return false, nil
	}
	// OrdinarySetWithOwnDescriptor (§10.1.9.2): the write is applied to the
	// receiver's own property. For a Proxy receiver the descriptor must be read
	// through its [[GetOwnProperty]] (which runs the trap / forwards), and an
	// existing property must be redefined with a value-only descriptor so its
	// other attributes (e.g. a non-configurable "length") are preserved.
	if recv.proxy != nil {
		existing, exists, err := i.getOwnPropertyV(ctx, recv, key)
		if err != nil {
			return false, err
		}
		if exists {
			if existing.Accessor || !existing.Writable {
				return false, nil
			}
			return recv.proxy.defineProperty(ctx, key, i.valueOnlyDescriptor(v))
		}
		return recv.proxy.defineDataValue(ctx, key, v)
	}
	if existing, exists := recv.getOwn(key); exists {
		if existing.Accessor || !existing.Writable {
			return false, nil
		}
		recv.writeData(key, v)
		return true, nil
	}
	if !recv.extensible {
		return false, nil
	}
	recv.writeData(key, v)
	return true, nil
}

// valueOnlyDescriptor builds a descriptor object carrying just a [[Value]]
// field, used by OrdinarySet to update an existing property without disturbing
// its other attributes.
func (i *Interpreter) valueOnlyDescriptor(v Value) *Object {
	d := NewObject(i.objectProto)
	d.SetData("value", v)
	return d
}

// ordinarySetPrototypeOf implements OrdinarySetPrototypeOf (§10.1.2): the change
// is refused (returns false) when the object is non-extensible and the new
// prototype differs, or when it would introduce a cycle.
func (i *Interpreter) ordinarySetPrototypeOf(o *Object, proto Value) bool {
	var np *Object
	switch p := proto.(type) {
	case *Object:
		np = p
	case Null:
		np = nil
	default:
		return false
	}
	if o.proto == np {
		return true
	}
	if !o.extensible {
		return false
	}
	// Reject a prototype cycle.
	for p := np; p != nil; p = p.proto {
		if p == o {
			return false
		}
	}
	o.proto = np
	return true
}

// ownPropertyKeys returns every own property key of an ordinary object in the
// order mandated by OrdinaryOwnPropertyKeys (§10.1.11.1): integer indices in
// ascending order, then the remaining string keys in insertion order, then the
// symbol keys in insertion order.
func (o *Object) ownPropertyKeys() []PropertyKey {
	out := make([]PropertyKey, 0, len(o.keys)+1)
	for _, name := range o.OwnKeys() {
		out = append(out, StrKey(name))
	}
	// An Array exposes a non-enumerable own "length" key immediately after its
	// integer indices (OwnKeys omits it because length is stored implicitly).
	if o.isArray {
		at := 0
		for at < len(out) {
			if _, isIdx := arrayIndex(out[at].Str); isIdx {
				at++
			} else {
				break
			}
		}
		out = append(out, PropertyKey{})
		copy(out[at+1:], out[at:])
		out[at] = StrKey("length")
	}
	for _, k := range o.keys {
		if k.IsSymbol() {
			out = append(out, k)
		}
	}
	return out
}

// createListFromArrayLike implements CreateListFromArrayLike (§7.3.19): it reads
// the "length" of an array-like object and collects its 0..length-1 elements.
func (i *Interpreter) createListFromArrayLike(ctx context.Context, v Value) ([]Value, error) {
	o, ok := v.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "CreateListFromArrayLike called on non-object")
	}
	lenV, err := o.GetStr(ctx, "length")
	if err != nil {
		return nil, err
	}
	n, err := i.ToNumberV(ctx, lenV)
	if err != nil {
		return nil, err
	}
	length := int(ToInteger(n))
	if length < 0 {
		length = 0
	}
	out := make([]Value, 0, length)
	for k := 0; k < length; k++ {
		el, err := o.GetStr(ctx, intToStr(k))
		if err != nil {
			return nil, err
		}
		out = append(out, el)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// The Reflect namespace object
// ---------------------------------------------------------------------------

// initReflect installs the %Reflect% namespace object.
func (i *Interpreter) initReflect() {
	r := NewObject(i.objectProto)
	r.class = "Reflect"
	r.defineOwn(SymKey(i.symToStringTag), &Property{
		Value: String("Reflect"), Writable: false, Enumerable: false, Configurable: true,
	})

	i.defineMethod(r, "apply", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		target, ok := arg(args, 0).(*Object)
		if !ok || !target.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Reflect.apply target is not a function")
		}
		list, err := i.createListFromArrayLike(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		return target.fn.call(ctx, arg(args, 1), list)
	})

	i.defineMethod(r, "construct", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		target, ok := arg(args, 0).(*Object)
		if !ok || !target.IsConstructor() {
			return nil, i.throwError(ctx, "TypeError", "Reflect.construct target is not a constructor")
		}
		newTarget := Value(target)
		if len(args) > 2 {
			nt, ok := arg(args, 2).(*Object)
			if !ok || !nt.IsConstructor() {
				return nil, i.throwError(ctx, "TypeError", "Reflect.construct newTarget is not a constructor")
			}
			newTarget = nt
		}
		list, err := i.createListFromArrayLike(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return target.fn.construct(ctx, newTarget, list)
	})

	i.defineMethod(r, "defineProperty", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "defineProperty")
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		desc, ok := arg(args, 2).(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Property description must be an object")
		}
		ok2, err := i.definePropertyV(ctx, o, key, desc)
		if err != nil {
			return nil, err
		}
		return Bool(ok2), nil
	})

	i.defineMethod(r, "deleteProperty", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "deleteProperty")
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		ok, err := i.deleteV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		return Bool(ok), nil
	})

	i.defineMethod(r, "get", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "get")
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		receiver := Value(o)
		if len(args) > 2 {
			receiver = arg(args, 2)
		}
		return i.getV(ctx, o, key, receiver)
	})

	i.defineMethod(r, "getOwnPropertyDescriptor", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "getOwnPropertyDescriptor")
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

	i.defineMethod(r, "getPrototypeOf", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "getPrototypeOf")
		if err != nil {
			return nil, err
		}
		return i.getProtoV(ctx, o)
	})

	i.defineMethod(r, "has", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "has")
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		ok, err := i.hasV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		return Bool(ok), nil
	})

	i.defineMethod(r, "isExtensible", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "isExtensible")
		if err != nil {
			return nil, err
		}
		ok, err := i.isExtensibleV(ctx, o)
		if err != nil {
			return nil, err
		}
		return Bool(ok), nil
	})

	i.defineMethod(r, "ownKeys", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "ownKeys")
		if err != nil {
			return nil, err
		}
		keys, err := i.ownKeysV(ctx, o)
		if err != nil {
			return nil, err
		}
		out := make([]Value, 0, len(keys))
		for _, k := range keys {
			if k.IsSymbol() {
				out = append(out, k.Sym)
			} else {
				out = append(out, String(k.Str))
			}
		}
		return i.newArray(out), nil
	})

	i.defineMethod(r, "preventExtensions", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "preventExtensions")
		if err != nil {
			return nil, err
		}
		ok, err := i.preventExtensionsV(ctx, o)
		if err != nil {
			return nil, err
		}
		return Bool(ok), nil
	})

	i.defineMethod(r, "set", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "set")
		if err != nil {
			return nil, err
		}
		key, err := i.ToPropertyKey(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		receiver := Value(o)
		if len(args) > 3 {
			receiver = arg(args, 3)
		}
		ok, err := i.setV(ctx, o, key, arg(args, 2), receiver)
		if err != nil {
			return nil, err
		}
		return Bool(ok), nil
	})

	i.defineMethod(r, "setPrototypeOf", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.reflectTargetObject(ctx, args, "setPrototypeOf")
		if err != nil {
			return nil, err
		}
		proto := arg(args, 1)
		if _, ok := proto.(*Object); !ok {
			if _, ok := proto.(Null); !ok {
				return nil, i.throwError(ctx, "TypeError", "Reflect.setPrototypeOf called with an invalid prototype")
			}
		}
		ok, err := i.setProtoV(ctx, o, proto)
		if err != nil {
			return nil, err
		}
		return Bool(ok), nil
	})

	i.setGlobalHidden("Reflect", r)
}

// reflectTargetObject validates that the first argument to a Reflect method is
// an object, throwing a TypeError (naming the method) otherwise.
func (i *Interpreter) reflectTargetObject(ctx context.Context, args []Value, method string) (*Object, error) {
	o, ok := arg(args, 0).(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Reflect."+method+" called on non-object")
	}
	return o, nil
}
