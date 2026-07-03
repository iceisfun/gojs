package interp

import (
	"context"
	"math"
	"sort"
	"strconv"
)

// This file holds additional Array.prototype methods kept separate from the
// core set in builtin_array.go for readability.

// sortCompare implements the SortCompare abstract closure (§23.1.3.30.2):
// undefined sorts after everything, otherwise the comparator's ToNumber result
// (NaN treated as +0) is used, falling back to a lexicographic ToString compare.
// It returns a negative/zero/positive number like a C comparator.
func (i *Interpreter) sortCompare(ctx context.Context, cmp *Object, x, y Value) (float64, error) {
	xu, yu := IsUndefined(x), IsUndefined(y)
	switch {
	case xu && yu:
		return 0, nil
	case xu:
		return 1, nil
	case yu:
		return -1, nil
	}
	if cmp != nil {
		r, err := cmp.fn.call(ctx, Undef, []Value{x, y})
		if err != nil {
			return 0, err
		}
		n, err := i.ToNumberV(ctx, r)
		if err != nil {
			return 0, err
		}
		if math.IsNaN(n) {
			return 0, nil
		}
		return n, nil
	}
	sa, err := i.ToStringV(ctx, x)
	if err != nil {
		return 0, err
	}
	sb, err := i.ToStringV(ctx, y)
	if err != nil {
		return 0, err
	}
	switch {
	case sa < sb:
		return -1, nil
	case sa > sb:
		return 1, nil
	}
	return 0, nil
}

// arraySort implements Array.prototype.sort (§23.1.3.30) in place. It is generic:
// a non-callable, non-undefined comparator throws, and an array-like receiver is
// sorted through [[Get]]/[[Set]]/[[Delete]]. A plain dense array takes a direct
// fast path. Present elements are sorted (undefined last); holes migrate to the
// tail. The sort is stable, matching modern engines.
func (i *Interpreter) arraySort(ctx context.Context, this Value, args []Value) (Value, error) {
	// Comparefn must be undefined or callable (validated before any coercion).
	var cmp *Object
	if comparefn := arg(args, 0); !IsUndefined(comparefn) {
		c, ok := comparefn.(*Object)
		if !ok || !c.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "The comparison function must be either a function or undefined")
		}
		cmp = c
	}
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}

	// sortValues stably sorts a slice by sortCompare, surfacing a comparator error.
	sortValues := func(vals []Value) error {
		var sortErr error
		sort.SliceStable(vals, func(a, b int) bool {
			if sortErr != nil {
				return false
			}
			n, err := i.sortCompare(ctx, cmp, vals[a], vals[b])
			if err != nil {
				sortErr = err
				return false
			}
			return n < 0
		})
		return sortErr
	}

	// Sort an array-like object through its internal methods. There is no dense
	// fast path: the spec reads present elements with [[Get]] (after a proto-
	// chain-walking HasProperty) and writes them back with [[Set]], so an index
	// accessor anywhere on the prototype chain — or one that mutates the array
	// mid-sort — is observed exactly (see the sort/precise-* tests).
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	// Collect the values at present indices (holes are skipped, not deleted yet).
	var items []Value
	for k := 0; k < length; k++ {
		key := StrKey(strconv.Itoa(k))
		present, err := i.hasV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		if present {
			v, err := i.getV(ctx, o, key, o)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
	}
	if err := sortValues(items); err != nil {
		return nil, err
	}
	// Write the sorted values back over the low indices, then delete the tail so
	// the trailing holes (absent indices) reappear at the end.
	j := 0
	for ; j < len(items); j++ {
		key := StrKey(strconv.Itoa(j))
		ok, err := i.setV(ctx, o, key, items[j], o)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Cannot assign to read only property '"+strconv.Itoa(j)+"'")
		}
	}
	for ; j < length; j++ {
		key := StrKey(strconv.Itoa(j))
		ok, err := i.deleteV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Cannot delete property '"+strconv.Itoa(j)+"'")
		}
	}
	return o, nil
}

// arrayFlatMap maps each element then flattens the result by one level
// (§23.1.3.12). It is generic: the receiver is coerced with ToObject, the result
// is allocated with ArraySpeciesCreate, and both the source and each mapped
// sub-array are read through [[HasProperty]]/[[Get]].
func (i *Interpreter) arrayFlatMap(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	// The mapper must be callable before ArraySpeciesCreate runs.
	cb := arg(args, 0)
	if callback, ok := cb.(*Object); !ok || !callback.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", briefValue(cb)+" is not a function")
	}
	a, err := i.arraySpeciesCreate(ctx, o, 0)
	if err != nil {
		return nil, err
	}
	const maxSafe = float64(1<<53 - 1)
	n := 0
	err = i.eachElem(ctx, o, length, cb, arg(args, 1), true, func(_ int, v Value, res Value) (bool, error) {
		// FlattenIntoArray with depth 1: array results are flattened one level
		// (their present elements copied), non-arrays are appended as-is.
		isArr, err := i.isArrayV(ctx, res)
		if err != nil {
			return false, err
		}
		if isArr {
			ro := res.(*Object)
			elemLen, err := i.lengthOfArrayLike(ctx, ro)
			if err != nil {
				return false, err
			}
			for k := 0; k < elemLen; k++ {
				key := StrKey(strconv.Itoa(k))
				present, err := i.hasV(ctx, ro, key)
				if err != nil {
					return false, err
				}
				if !present {
					continue
				}
				ev, err := i.getV(ctx, ro, key, ro)
				if err != nil {
					return false, err
				}
				if float64(n) >= maxSafe {
					return false, i.throwError(ctx, "TypeError", "Array length exceeds 2^53-1")
				}
				if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(n)), ev); err != nil {
					return false, err
				}
				n++
			}
			return false, nil
		}
		if float64(n) >= maxSafe {
			return false, i.throwError(ctx, "TypeError", "Array length exceeds 2^53-1")
		}
		if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(n)), res); err != nil {
			return false, err
		}
		n++
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// arrayAt implements Array.prototype.at (§23.1.3.1) generically: it resolves the
// relative index against LengthOfArrayLike and returns the element via [[Get]],
// or undefined when the index is out of range.
func (i *Interpreter) arrayAt(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	rel, err := i.argInteger(ctx, args, 0, 0)
	if err != nil {
		return nil, err
	}
	if rel < 0 {
		rel += float64(length)
	}
	if rel < 0 || rel >= float64(length) {
		return Undef, nil
	}
	return i.getV(ctx, o, StrKey(strconv.Itoa(int(rel))), o)
}

// arrayCopyWithin implements Array.prototype.copyWithin (§23.1.3.4) generically:
// it resolves target/start/end against LengthOfArrayLike, then copies the source
// range (choosing a safe direction for overlap) through
// [[HasProperty]]/[[Get]]/[[Set]]/[[Delete]], preserving holes.
func (i *Interpreter) arrayCopyWithin(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	to, err := i.relativeIndex(ctx, arg(args, 0), length, 0)
	if err != nil {
		return nil, err
	}
	from, err := i.relativeIndex(ctx, arg(args, 1), length, 0)
	if err != nil {
		return nil, err
	}
	final, err := i.relativeIndex(ctx, arg(args, 2), length, length)
	if err != nil {
		return nil, err
	}
	count := final - from
	if l := length - to; l < count {
		count = l
	}
	direction := 1
	if from < to && to < from+count {
		direction = -1
		from = from + count - 1
		to = to + count - 1
	}
	for ; count > 0; count-- {
		fromKey := StrKey(strconv.Itoa(from))
		toKey := StrKey(strconv.Itoa(to))
		present, err := i.hasV(ctx, o, fromKey)
		if err != nil {
			return nil, err
		}
		if present {
			v, err := i.getV(ctx, o, fromKey, o)
			if err != nil {
				return nil, err
			}
			if err := i.reverseSet(ctx, o, toKey, v); err != nil {
				return nil, err
			}
		} else if err := i.deletePropertyOrThrow(ctx, o, toKey); err != nil {
			return nil, err
		}
		from += direction
		to += direction
	}
	return o, nil
}

// arrayToReversed implements Array.prototype.toReversed (§23.1.3.33): it builds a
// fresh dense array whose elements are the receiver's read in reverse via [[Get]]
// (holes densify to undefined) and never mutates the receiver.
func (i *Interpreter) arrayToReversed(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	a, err := i.arrayCreate(ctx, length)
	if err != nil {
		return nil, err
	}
	for k := 0; k < length; k++ {
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(length-k-1)), o)
		if err != nil {
			return nil, err
		}
		if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(k)), v); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// arrayToSorted implements Array.prototype.toSorted (§23.1.3.34): it validates the
// comparator, densifies the receiver into a fresh array via [[Get]], then sorts
// that copy in place with arraySort, leaving the receiver unchanged.
func (i *Interpreter) arrayToSorted(ctx context.Context, this Value, args []Value) (Value, error) {
	// The comparator is validated before any coercion (§23.1.3.34 step 1).
	if comparefn := arg(args, 0); !IsUndefined(comparefn) {
		c, ok := comparefn.(*Object)
		if !ok || !c.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "The comparison function must be either a function or undefined")
		}
	}
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	a, err := i.arrayCreate(ctx, length)
	if err != nil {
		return nil, err
	}
	for k := 0; k < length; k++ {
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(k)), o)
		if err != nil {
			return nil, err
		}
		if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(k)), v); err != nil {
			return nil, err
		}
	}
	if _, err := i.arraySort(ctx, a, args); err != nil {
		return nil, err
	}
	return a, nil
}

// arrayToSpliced implements Array.prototype.toSpliced (§23.1.3.35): it builds a
// fresh dense array reflecting the splice, reading the receiver through [[Get]]
// and never mutating it.
func (i *Interpreter) arrayToSpliced(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	actualStart, err := i.relativeIndex(ctx, arg(args, 0), length, 0)
	if err != nil {
		return nil, err
	}
	var insertCount, actualSkipCount int
	switch {
	case len(args) == 0:
		insertCount, actualSkipCount = 0, 0
	case len(args) == 1:
		insertCount, actualSkipCount = 0, length-actualStart
	default:
		insertCount = len(args) - 2
		sc, err := i.argInteger(ctx, args, 1, 0)
		if err != nil {
			return nil, err
		}
		actualSkipCount = clampIndexF(sc, length-actualStart)
	}
	newLen := length + insertCount - actualSkipCount
	if float64(newLen) > float64(1<<53-1) {
		return nil, i.throwError(ctx, "TypeError", "Array length exceeds 2^53-1")
	}
	a, err := i.arrayCreate(ctx, newLen)
	if err != nil {
		return nil, err
	}
	var items []Value
	if len(args) > 2 {
		items = args[2:]
	}
	idx := 0
	for ; idx < actualStart; idx++ {
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(idx)), o)
		if err != nil {
			return nil, err
		}
		if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(idx)), v); err != nil {
			return nil, err
		}
	}
	for _, e := range items {
		if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(idx)), e); err != nil {
			return nil, err
		}
		idx++
	}
	r := actualStart + actualSkipCount
	for ; idx < newLen; idx++ {
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(r)), o)
		if err != nil {
			return nil, err
		}
		if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(idx)), v); err != nil {
			return nil, err
		}
		r++
	}
	return a, nil
}

// arrayWith implements Array.prototype.with (§23.1.3.39): it builds a fresh dense
// copy with a single index replaced, throwing RangeError for an out-of-range
// index and reading the rest of the receiver through [[Get]].
func (i *Interpreter) arrayWith(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	rel, err := i.argInteger(ctx, args, 0, 0)
	if err != nil {
		return nil, err
	}
	if rel < 0 {
		rel += float64(length)
	}
	if rel < 0 || rel >= float64(length) {
		return nil, i.throwError(ctx, "RangeError", "Invalid index")
	}
	actualIndex := int(rel)
	value := arg(args, 1)
	a, err := i.arrayCreate(ctx, length)
	if err != nil {
		return nil, err
	}
	for k := 0; k < length; k++ {
		var v Value
		if k == actualIndex {
			v = value
		} else {
			if v, err = i.getV(ctx, o, StrKey(strconv.Itoa(k)), o); err != nil {
				return nil, err
			}
		}
		if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(k)), v); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// arrayLastIndexOf finds the last index of a value using strict equality,
// searching backward from an optional fromIndex (default: last element).
func (i *Interpreter) arrayLastIndexOf(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	target := arg(args, 0)
	n := len(o.elems)
	start := n - 1
	if !IsUndefined(arg(args, 1)) {
		start = argIntOr(ctx, i, args, 1, n-1)
		if start < 0 {
			start += n
		}
		if start >= n {
			start = n - 1
		}
	}
	for j := start; j >= 0; j-- {
		if strictEquals(o.elems[j], target) {
			return Number(float64(j)), nil
		}
	}
	return Number(-1), nil
}

// arrayReduceRight is Array.prototype.reduceRight (§23.1.3.25): fold from right
// to left. It is generic over any array-like receiver, reading present elements
// through [[HasProperty]]/[[Get]] and skipping holes, just like reduce.
func (i *Interpreter) arrayReduceRight(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	callback, ok := arg(args, 0).(*Object)
	if !ok || !callback.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "Reduce callback is not a function")
	}
	var acc Value
	haveAcc := len(args) >= 2
	if haveAcc {
		acc = args[1]
	}
	k := length - 1
	if !haveAcc {
		// Seed the accumulator from the last present element.
		for ; k >= 0; k-- {
			key := StrKey(strconv.Itoa(k))
			present, err := i.hasV(ctx, o, key)
			if err != nil {
				return nil, err
			}
			if present {
				acc, err = i.getV(ctx, o, key, o)
				if err != nil {
					return nil, err
				}
				haveAcc = true
				k--
				break
			}
		}
		if !haveAcc {
			return nil, i.throwError(ctx, "TypeError", "Reduce of empty array with no initial value")
		}
	}
	for ; k >= 0; k-- {
		key := StrKey(strconv.Itoa(k))
		present, err := i.hasV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		kv, err := i.getV(ctx, o, key, o)
		if err != nil {
			return nil, err
		}
		acc, err = callback.fn.call(ctx, Undef, []Value{acc, kv, Number(float64(k)), o})
		if err != nil {
			return nil, err
		}
	}
	return acc, nil
}

// arrayFindLast returns the last element satisfying the predicate (§23.1.3.11).
// It is generic and does not skip holes; every index is read via [[Get]].
func (i *Interpreter) arrayFindLast(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	cb, ok := arg(args, 0).(*Object)
	if !ok || !cb.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "predicate is not a function")
	}
	for j := length - 1; j >= 0; j-- {
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(j)), o)
		if err != nil {
			return nil, err
		}
		r, err := cb.fn.call(ctx, arg(args, 1), []Value{v, Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
		if ToBoolean(r) {
			return v, nil
		}
	}
	return Undef, nil
}

// arrayFindLastIndex returns the index of the last element satisfying the
// predicate, or -1 (§23.1.3.12). It is generic and does not skip holes; every
// index is read via [[Get]].
func (i *Interpreter) arrayFindLastIndex(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	cb, ok := arg(args, 0).(*Object)
	if !ok || !cb.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "predicate is not a function")
	}
	for j := length - 1; j >= 0; j-- {
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(j)), o)
		if err != nil {
			return nil, err
		}
		r, err := cb.fn.call(ctx, arg(args, 1), []Value{v, Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
		if ToBoolean(r) {
			return Number(float64(j)), nil
		}
	}
	return Number(-1), nil
}
