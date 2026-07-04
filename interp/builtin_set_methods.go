package interp

import (
	"context"
	"math"
)

// This file implements the ES2025 Set-method family (§24.2.3): union,
// intersection, difference, symmetricDifference, isSubsetOf, isSupersetOf and
// isDisjointFrom, along with the shared abstract operations they rely on
// (GetSetRecord, GetIteratorFromMethod) and constructor helpers shared with
// Map/Set (OrdinaryCreateFromConstructor's prototype lookup and
// AddEntriesFromIterable-style iteration with IteratorClose on failure).

// addFromIterable drives the iterator protocol over iterable, invoking add for
// each value. If add returns an abrupt completion the iterator is closed,
// preserving the abrupt completion (IfAbruptCloseIterator). Used by the Map and
// Set constructors so a user-visible adder ("set"/"add") is observed.
func (i *Interpreter) addFromIterable(ctx context.Context, iterable Value, add func(Value) error) error {
	rec, err := i.getIterator(ctx, iterable)
	if err != nil {
		return err
	}
	for {
		if err := i.checkContext(); err != nil {
			return err
		}
		v, done, err := i.iteratorStepValue(ctx, rec)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if err := add(v); err != nil {
			return i.iteratorClose(ctx, rec, err)
		}
	}
}

// getIteratorFromMethod implements GetIteratorFromMethod (§7.4.2): call method
// on obj and wrap the returned iterator with GetIteratorDirect.
func (i *Interpreter) getIteratorFromMethod(ctx context.Context, obj Value, method Value) (*iterRecord, error) {
	res, err := i.call(ctx, method, obj, nil)
	if err != nil {
		return nil, err
	}
	itObj, ok := res.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "iterator method returned a non-object")
	}
	return i.getIteratorDirect(ctx, itObj)
}

// setRecord mirrors the spec's Set Record { [[SetObject]], [[Size]], [[Has]],
// [[Keys]] }. Size is stored as a float64 so it may be +∞.
type setRecord struct {
	obj  *Object
	size float64
	has  Value
	keys Value
}

// getSetRecord implements GetSetRecord (§24.2.1.2).
func (i *Interpreter) getSetRecord(ctx context.Context, other Value) (*setRecord, error) {
	obj, ok := other.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Set method argument must be an object")
	}
	rawSize, err := obj.GetStr(ctx, "size")
	if err != nil {
		return nil, err
	}
	numSize, err := i.ToNumberV(ctx, rawSize)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(numSize) {
		return nil, i.throwError(ctx, "TypeError", "set-like object 'size' is NaN")
	}
	intSize := ToInteger(numSize)
	if intSize < 0 {
		return nil, i.throwError(ctx, "RangeError", "set-like object 'size' is negative")
	}
	has, err := obj.GetStr(ctx, "has")
	if err != nil {
		return nil, err
	}
	if !isCallableValue(has) {
		return nil, i.throwError(ctx, "TypeError", "set-like object 'has' is not callable")
	}
	keys, err := obj.GetStr(ctx, "keys")
	if err != nil {
		return nil, err
	}
	if !isCallableValue(keys) {
		return nil, i.throwError(ctx, "TypeError", "set-like object 'keys' is not callable")
	}
	return &setRecord{obj: obj, size: intSize, has: has, keys: keys}, nil
}

// setReceiver performs RequireInternalSlot(this, [[SetData]]) and returns the
// backing orderedSet, or a TypeError on an incompatible receiver.
func (i *Interpreter) setReceiver(ctx context.Context, this Value, method string) (*orderedSet, error) {
	s := setSlot(this)
	if s == nil {
		return nil, i.throwError(ctx, "TypeError", "Set.prototype."+method+" called on incompatible receiver")
	}
	return s, nil
}

// wrapSet wraps a freshly-built orderedSet in a Set object backed by
// %Set.prototype% (OrdinaryObjectCreate(%Set.prototype%, « [[SetData]] »)).
func (i *Interpreter) wrapSet(s *orderedSet) *Object {
	obj := NewObject(i.setProto)
	obj.class = "Set"
	obj.internal = map[string]any{"Set": s}
	return obj
}

// setUnion implements Set.prototype.union (§24.2.3.20).
func (i *Interpreter) setUnion(ctx context.Context, this Value, args []Value) (Value, error) {
	s, err := i.setReceiver(ctx, this, "union")
	if err != nil {
		return nil, err
	}
	rec, err := i.getSetRecord(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	iter, err := i.getIteratorFromMethod(ctx, rec.obj, rec.keys)
	if err != nil {
		return nil, err
	}
	result := &orderedSet{}
	for _, v := range s.values() {
		result.add(v)
	}
	for {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		next, done, err := i.iteratorStepValue(ctx, iter)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		result.add(canonicalizeKey(next))
	}
	return i.wrapSet(result), nil
}

// setIntersection implements Set.prototype.intersection (§24.2.3.17).
func (i *Interpreter) setIntersection(ctx context.Context, this Value, args []Value) (Value, error) {
	s, err := i.setReceiver(ctx, this, "intersection")
	if err != nil {
		return nil, err
	}
	rec, err := i.getSetRecord(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	result := &orderedSet{}
	if float64(s.size()) <= rec.size {
		idx := 0
		for {
			e, next, ok := s.m.nextLive(idx)
			if !ok {
				break
			}
			idx = next
			inOther, err := i.call(ctx, rec.has, rec.obj, []Value{e.key})
			if err != nil {
				return nil, err
			}
			if ToBoolean(inOther) {
				result.add(e.key)
			}
		}
	} else {
		iter, err := i.getIteratorFromMethod(ctx, rec.obj, rec.keys)
		if err != nil {
			return nil, err
		}
		for {
			if err := i.checkContext(); err != nil {
				return nil, err
			}
			next, done, err := i.iteratorStepValue(ctx, iter)
			if err != nil {
				return nil, err
			}
			if done {
				break
			}
			next = canonicalizeKey(next)
			if s.has(next) {
				result.add(next)
			}
		}
	}
	return i.wrapSet(result), nil
}

// setDifference implements Set.prototype.difference (§24.2.3.8).
func (i *Interpreter) setDifference(ctx context.Context, this Value, args []Value) (Value, error) {
	s, err := i.setReceiver(ctx, this, "difference")
	if err != nil {
		return nil, err
	}
	rec, err := i.getSetRecord(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	result := &orderedSet{}
	for _, v := range s.values() {
		result.add(v)
	}
	if float64(s.size()) <= rec.size {
		for _, entry := range result.values() {
			inOther, err := i.call(ctx, rec.has, rec.obj, []Value{entry})
			if err != nil {
				return nil, err
			}
			if ToBoolean(inOther) {
				result.delete(entry)
			}
		}
	} else {
		iter, err := i.getIteratorFromMethod(ctx, rec.obj, rec.keys)
		if err != nil {
			return nil, err
		}
		for {
			if err := i.checkContext(); err != nil {
				return nil, err
			}
			next, done, err := i.iteratorStepValue(ctx, iter)
			if err != nil {
				return nil, err
			}
			if done {
				break
			}
			result.delete(canonicalizeKey(next))
		}
	}
	return i.wrapSet(result), nil
}

// setSymmetricDifference implements Set.prototype.symmetricDifference
// (§24.2.3.19).
func (i *Interpreter) setSymmetricDifference(ctx context.Context, this Value, args []Value) (Value, error) {
	s, err := i.setReceiver(ctx, this, "symmetricDifference")
	if err != nil {
		return nil, err
	}
	rec, err := i.getSetRecord(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	iter, err := i.getIteratorFromMethod(ctx, rec.obj, rec.keys)
	if err != nil {
		return nil, err
	}
	result := &orderedSet{}
	for _, v := range s.values() {
		result.add(v)
	}
	for {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		next, done, err := i.iteratorStepValue(ctx, iter)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		next = canonicalizeKey(next)
		alreadyInResult := result.has(next)
		if s.has(next) {
			if alreadyInResult {
				result.delete(next)
			}
		} else if !alreadyInResult {
			result.add(next)
		}
	}
	return i.wrapSet(result), nil
}

// setIsSubsetOf implements Set.prototype.isSubsetOf (§24.2.3.14).
func (i *Interpreter) setIsSubsetOf(ctx context.Context, this Value, args []Value) (Value, error) {
	s, err := i.setReceiver(ctx, this, "isSubsetOf")
	if err != nil {
		return nil, err
	}
	rec, err := i.getSetRecord(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	if float64(s.size()) > rec.size {
		return False, nil
	}
	idx := 0
	for {
		e, next, ok := s.m.nextLive(idx)
		if !ok {
			break
		}
		idx = next
		inOther, err := i.call(ctx, rec.has, rec.obj, []Value{e.key})
		if err != nil {
			return nil, err
		}
		if !ToBoolean(inOther) {
			return False, nil
		}
	}
	return True, nil
}

// setIsSupersetOf implements Set.prototype.isSupersetOf (§24.2.3.15).
func (i *Interpreter) setIsSupersetOf(ctx context.Context, this Value, args []Value) (Value, error) {
	s, err := i.setReceiver(ctx, this, "isSupersetOf")
	if err != nil {
		return nil, err
	}
	rec, err := i.getSetRecord(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	if float64(s.size()) < rec.size {
		return False, nil
	}
	iter, err := i.getIteratorFromMethod(ctx, rec.obj, rec.keys)
	if err != nil {
		return nil, err
	}
	for {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		next, done, err := i.iteratorStepValue(ctx, iter)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		if !s.has(next) {
			if cerr := i.iteratorClose(ctx, iter, nil); cerr != nil {
				return nil, cerr
			}
			return False, nil
		}
	}
	return True, nil
}

// setIsDisjointFrom implements Set.prototype.isDisjointFrom (§24.2.3.13).
func (i *Interpreter) setIsDisjointFrom(ctx context.Context, this Value, args []Value) (Value, error) {
	s, err := i.setReceiver(ctx, this, "isDisjointFrom")
	if err != nil {
		return nil, err
	}
	rec, err := i.getSetRecord(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	if float64(s.size()) <= rec.size {
		idx := 0
		for {
			e, next, ok := s.m.nextLive(idx)
			if !ok {
				break
			}
			idx = next
			inOther, err := i.call(ctx, rec.has, rec.obj, []Value{e.key})
			if err != nil {
				return nil, err
			}
			if ToBoolean(inOther) {
				return False, nil
			}
		}
		return True, nil
	}
	iter, err := i.getIteratorFromMethod(ctx, rec.obj, rec.keys)
	if err != nil {
		return nil, err
	}
	for {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		next, done, err := i.iteratorStepValue(ctx, iter)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		if s.has(next) {
			if cerr := i.iteratorClose(ctx, iter, nil); cerr != nil {
				return nil, cerr
			}
			return False, nil
		}
	}
	return True, nil
}
