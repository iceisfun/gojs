package interp

import "context"

// This file implements the iteration protocol: creating native iterators and
// consuming any iterable (array, string, or object with Symbol.iterator).

// newIterator wraps a Go generator function as an iterator object with a next
// method and a self-returning Symbol.iterator. next yields (value, ok); ok
// false signals completion.
func (i *Interpreter) newIterator(next func() (Value, bool)) *Object {
	it := NewObject(i.iteratorProto)
	it.class = "Array Iterator"
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

// iterate consumes an iterable, invoking fn for each produced value. It fast-
// paths arrays and strings, and otherwise drives the Symbol.iterator protocol.
func (i *Interpreter) iterate(ctx context.Context, iterable Value, fn func(Value) error) error {
	switch v := iterable.(type) {
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
	itFn, ok := i.methodBySymbol(obj, i.symIterator)
	if !ok {
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

// isIterable reports whether v can be driven by the iteration protocol (a
// string, an array, or an object exposing Symbol.iterator).
func isIterable(i *Interpreter, v Value) bool {
	switch x := v.(type) {
	case String:
		return true
	case *Object:
		if x.isArray {
			return true
		}
		_, ok := i.methodBySymbol(x, i.symIterator)
		return ok
	default:
		return false
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
