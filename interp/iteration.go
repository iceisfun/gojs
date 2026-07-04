package interp

import "context"

// This file implements the iteration protocol: creating native iterators and
// consuming any iterable (array, string, or object with Symbol.iterator).

// newIterator wraps a Go generator function as an iterator object with a next
// method and a self-returning Symbol.iterator. next yields (value, ok); ok
// false signals completion.
func (i *Interpreter) newIterator(next func() (Value, bool)) *Object {
	return i.newIteratorProto(i.iteratorProto, "Array Iterator", next)
}

// newIteratorProto is newIterator with an explicit [[Prototype]] and class, so
// callers can hang the iterator off a dedicated intrinsic (e.g.
// %ArrayIteratorPrototype%) that itself inherits %Iterator.prototype%.
func (i *Interpreter) newIteratorProto(proto *Object, class string, next func() (Value, bool)) *Object {
	it := NewObject(proto)
	it.class = class
	i.defineMethod(it, "next", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		res := NewObject(i.objectProto)
		v, ok := next()
		if !ok {
			res.SetData("value", Undef)
			res.SetData("done", True)
			return res, nil
		}
		res.SetData("value", v)
		res.SetData("done", False)
		return res, nil
	})
	it.defineOwn(SymKey(i.symIterator), &Property{
		Value:        i.newNativeFunc("[Symbol.iterator]", 0, func(ctx context.Context, this Value, args []Value) (Value, error) { return this, nil }),
		Writable:     true,
		Configurable: true,
	})
	return it
}

// newCollectionIterator builds a Map/Set iterator instance. Unlike
// newIteratorProto, the generator closure is stored in an internal slot keyed by
// slotKey rather than captured by a per-instance next method. The shared next
// method lives on proto (installed by defineCollectionIteratorNext) and
// brand-checks this slot, so calling next on an incompatible receiver — a plain
// object, the prototype itself, a Map/Set instance, or the other collection's
// iterator — throws a TypeError as the spec requires (§24.1.5.2, §24.2.6.2).
// Symbol.iterator is inherited from %Iterator.prototype%.
func (i *Interpreter) newCollectionIterator(proto *Object, class, slotKey string, next func() (Value, bool)) *Object {
	it := NewObject(proto)
	it.class = class
	it.internal = map[string]any{slotKey: next}
	return it
}

// defineCollectionIteratorNext installs the shared, brand-checked next method on
// a %MapIteratorPrototype%/%SetIteratorPrototype%. It reads the generator closure
// from this's slotKey internal slot; a receiver lacking that slot throws a
// TypeError (the required Map/Set Iterator brand check).
func (i *Interpreter) defineCollectionIteratorNext(proto *Object, slotKey, label string) {
	i.defineMethod(proto, "next", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		var gen func() (Value, bool)
		if ok && o.internal != nil {
			gen, _ = o.internal[slotKey].(func() (Value, bool))
		}
		if gen == nil {
			return nil, i.throwError(ctx, "TypeError", label+".prototype.next called on incompatible receiver")
		}
		res := NewObject(i.objectProto)
		v, ok := gen()
		if !ok {
			res.SetData("value", Undef)
			res.SetData("done", True)
			return res, nil
		}
		res.SetData("value", v)
		res.SetData("done", False)
		return res, nil
	})
}

// arrayIteratorSlot marks an object as an Array Iterator instance and holds its
// step closure (the [[IteratedArrayLike]]/[[ArrayLikeNextIndex]] state captured
// in a Go closure). Its presence is the brand checked by
// %ArrayIteratorPrototype%.next. The closure returns (value, done, error): the
// error channel lets a Proxy [[Get]] trap or a detached/out-of-bounds TypedArray
// check throw a TypeError out of next (§23.1.5.1).
const arrayIteratorSlot = "ArrayIterator"

// newArrayIteratorObj builds an Array Iterator instance (CreateArrayIterator,
// §23.1.5.1). Like the Map/Set iterators, the per-instance step state lives in an
// internal slot rather than a captured next method, so the single brand-checked
// next installed on %ArrayIteratorPrototype% by defineArrayIteratorNext serves
// every instance. Symbol.iterator is inherited from %Iterator.prototype%.
func (i *Interpreter) newArrayIteratorObj(next func(ctx context.Context) (Value, bool, error)) *Object {
	it := NewObject(i.arrayIteratorProto)
	it.class = "Array Iterator"
	it.internal = map[string]any{arrayIteratorSlot: next}
	return it
}

// defineArrayIteratorNext installs the shared, brand-checked
// %ArrayIteratorPrototype%.next as an own, writable/configurable data property.
// It reads the step closure from this's arrayIteratorSlot internal slot; a
// receiver lacking that slot — a plain object, the prototype itself, an array, or
// undefined/null — throws a TypeError (§23.1.5.1 steps 2-3).
func (i *Interpreter) defineArrayIteratorNext() {
	i.defineMethod(i.arrayIteratorProto, "next", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		var gen func(ctx context.Context) (Value, bool, error)
		if ok && o.internal != nil {
			gen, _ = o.internal[arrayIteratorSlot].(func(ctx context.Context) (Value, bool, error))
		}
		if gen == nil {
			return nil, i.throwError(ctx, "TypeError", "%ArrayIteratorPrototype%.next called on incompatible receiver")
		}
		v, done, err := gen(ctx)
		if err != nil {
			return nil, err
		}
		res := NewObject(i.objectProto)
		if done {
			res.SetData("value", Undef)
			res.SetData("done", True)
			return res, nil
		}
		res.SetData("value", v)
		res.SetData("done", False)
		return res, nil
	})
}

// iterate consumes an iterable, invoking fn for each produced value. It fast-
// paths arrays and strings, and otherwise drives the Symbol.iterator protocol.
func (i *Interpreter) iterate(ctx context.Context, iterable Value, fn func(Value) error) error {
	switch v := flattenRope(iterable).(type) {
	case *Object:
		if v.isArray {
			// Snapshot length to mirror spec-ish behavior on mutation.
			for j := 0; j < len(v.elems); j++ {
				// The array iterator reads via [[Get]], so holes densify to undefined.
				if err := fn(undefIfHole(v.elems[j])); err != nil {
					return err
				}
			}
			return nil
		}
		return i.iterateProtocol(ctx, v, fn)
	case String:
		for _, r := range string(v) {
			if err := fn(String(string(r))); err != nil {
				return err
			}
		}
		return nil
	case Undefined, Null:
		return i.throwError(ctx, "TypeError", briefValue(iterable)+" is not iterable")
	default:
		return i.throwError(ctx, "TypeError", briefValue(iterable)+" is not iterable")
	}
}

// iterateProtocol drives the full Symbol.iterator protocol on an object.
func (i *Interpreter) iterateProtocol(ctx context.Context, obj *Object, fn func(Value) error) error {
	// GetMethod(obj, @@iterator) reads the method through [[Get]], so an accessor
	// @@iterator runs its getter (and a throwing getter propagates) rather than
	// being silently treated as non-iterable (§7.4.2 GetIterator).
	itFn, err := i.getMethod(ctx, obj, i.symIterator)
	if err != nil {
		return err
	}
	if itFn == nil {
		return i.throwError(ctx, "TypeError", briefValue(obj)+" is not iterable")
	}
	iterator, err := itFn.fn.call(ctx, obj, nil)
	if err != nil {
		return err
	}
	itObj, ok := iterator.(*Object)
	if !ok {
		return i.throwError(ctx, "TypeError", "iterator is not an object")
	}
	nextV, err := itObj.GetStr(ctx, "next")
	if err != nil {
		return err
	}
	next, ok := nextV.(*Object)
	if !ok || !next.IsCallable() {
		return i.throwError(ctx, "TypeError", "iterator.next is not a function")
	}
	for {
		if err := i.checkContext(); err != nil {
			return err
		}
		resV, err := next.fn.call(ctx, itObj, nil)
		if err != nil {
			return err
		}
		res, ok := resV.(*Object)
		if !ok {
			return i.throwError(ctx, "TypeError", "iterator result is not an object")
		}
		doneV, err := res.GetStr(ctx, "done")
		if err != nil {
			return err
		}
		if ToBoolean(doneV) {
			return nil
		}
		val, err := res.GetStr(ctx, "value")
		if err != nil {
			return err
		}
		if err := fn(val); err != nil {
			return err
		}
	}
}

// iterableToSlice collects an iterable into a Go slice (used for spread).
func (i *Interpreter) iterableToSlice(ctx context.Context, iterable Value) ([]Value, error) {
	var out []Value
	err := i.iterate(ctx, iterable, func(v Value) error {
		out = append(out, v)
		return nil
	})
	return out, err
}
