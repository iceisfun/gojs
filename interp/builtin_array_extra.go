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

// arrayAt returns the element at a possibly-negative index.
func (i *Interpreter) arrayAt(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	idx, _ := i.argInt(ctx, args, 0)
	if idx < 0 {
		idx += len(o.elems)
	}
	if idx < 0 || idx >= len(o.elems) {
		return Undef, nil
	}
	return elemAt(o, idx), nil
}

// arrayCopyWithin copies a slice of the array to another position within the
// same array (mutating in place) and returns the array.
func (i *Interpreter) arrayCopyWithin(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	n := len(o.elems)
	target := relIndex(argIntOr(ctx, i, args, 0, 0), n)
	start := relIndex(argIntOr(ctx, i, args, 1, 0), n)
	end := n
	if !IsUndefined(arg(args, 2)) {
		end = relIndex(argIntOr(ctx, i, args, 2, n), n)
	}
	// Copy the source range first so overlapping ranges behave correctly.
	src := append([]Value(nil), o.elems[start:end]...)
	for k := 0; k < len(src) && target+k < n; k++ {
		o.elems[target+k] = src[k]
	}
	return o, nil
}

// arrayToReversed returns a reversed copy without mutating the receiver.
func (i *Interpreter) arrayToReversed(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	// The to* methods read 0..len via [[Get]], so holes densify to undefined.
	out := make([]Value, len(o.elems))
	for k := range o.elems {
		out[len(o.elems)-1-k] = elemAt(o, k)
	}
	return i.newArray(out), nil
}

// arrayToSorted returns a sorted copy without mutating the receiver.
func (i *Interpreter) arrayToSorted(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	copyArr := i.newArray(o.denseCopy())
	if _, err := i.arraySort(ctx, copyArr, args); err != nil {
		return nil, err
	}
	return copyArr, nil
}

// arrayToSpliced returns a copy with a splice applied, leaving the receiver
// unchanged.
func (i *Interpreter) arrayToSpliced(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	copyArr := i.newArray(o.denseCopy())
	if _, err := i.arraySplice(ctx, copyArr, args); err != nil {
		return nil, err
	}
	return copyArr, nil
}

// arrayWith returns a copy with a single index replaced, throwing RangeError for
// an out-of-bounds index.
func (i *Interpreter) arrayWith(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	n := len(o.elems)
	idx, _ := i.argInt(ctx, args, 0)
	if idx < 0 {
		idx += n
	}
	if idx < 0 || idx >= n {
		return nil, i.throwError(ctx, "RangeError", "Invalid index")
	}
	out := o.denseCopy()
	out[idx] = arg(args, 1)
	return i.newArray(out), nil
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
