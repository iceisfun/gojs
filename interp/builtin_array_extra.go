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

// arrayFlatMap maps each element then flattens the result by one level.
func (i *Interpreter) arrayFlatMap(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	var out []Value
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), true, func(_ int, res Value) (bool, error) {
		if ro, ok := res.(*Object); ok && ro.isArray {
			// FlattenIntoArray uses HasProperty; skip holes in the mapped array.
			for _, ev := range ro.elems {
				if !isHole(ev) {
					out = append(out, ev)
				}
			}
		} else {
			out = append(out, res)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return i.newArray(out), nil
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

// arrayReduceRight is Array.prototype.reduceRight: fold from right to left.
func (i *Interpreter) arrayReduceRight(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	callback, ok := arg(args, 0).(*Object)
	if !ok || !callback.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "Reduce callback is not a function")
	}
	// reduceRight skips holes, just like reduce.
	var acc Value
	haveAcc := false
	if len(args) >= 2 {
		acc = args[1]
		haveAcc = true
	}
	for j := len(o.elems) - 1; j >= 0; j-- {
		if isHole(o.elems[j]) {
			continue
		}
		if !haveAcc {
			acc = o.elems[j]
			haveAcc = true
			continue
		}
		acc, err = callback.fn.call(ctx, Undef, []Value{acc, o.elems[j], Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
	}
	if !haveAcc {
		return nil, i.throwError(ctx, "TypeError", "Reduce of empty array with no initial value")
	}
	return acc, nil
}

// arrayFindLast returns the last element satisfying the predicate.
func (i *Interpreter) arrayFindLast(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	cb, ok := arg(args, 0).(*Object)
	if !ok || !cb.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "predicate is not a function")
	}
	// findLast does not skip holes; a hole is visited as undefined.
	for j := len(o.elems) - 1; j >= 0; j-- {
		v := elemAt(o, j)
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
// predicate, or -1.
func (i *Interpreter) arrayFindLastIndex(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	cb, ok := arg(args, 0).(*Object)
	if !ok || !cb.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "predicate is not a function")
	}
	// findLastIndex does not skip holes; a hole is visited as undefined.
	for j := len(o.elems) - 1; j >= 0; j-- {
		r, err := cb.fn.call(ctx, arg(args, 1), []Value{elemAt(o, j), Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
		if ToBoolean(r) {
			return Number(float64(j)), nil
		}
	}
	return Number(-1), nil
}
