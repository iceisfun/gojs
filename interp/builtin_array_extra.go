package interp

import (
	"context"
	"sort"
)

// This file holds additional Array.prototype methods kept separate from the
// core set in builtin_array.go for readability.

// arraySort sorts the array in place. With no comparator, elements are compared
// by their string representation (with undefined sorting last); otherwise the
// provided comparator is used. A stable sort is used, matching modern engines.
func (i *Interpreter) arraySort(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	var cmp *Object
	if c, ok := arg(args, 0).(*Object); ok && c.IsCallable() {
		cmp = c
	}
	// undefined elements always sort to the end and are not passed to cmp.
	var defined []Value
	undefinedCount := 0
	for _, v := range o.elems {
		if IsUndefined(v) {
			undefinedCount++
		} else {
			defined = append(defined, v)
		}
	}

	var sortErr error
	sort.SliceStable(defined, func(a, b int) bool {
		if sortErr != nil {
			return false
		}
		if cmp != nil {
			r, err := cmp.fn.call(ctx, Undef, []Value{defined[a], defined[b]})
			if err != nil {
				sortErr = err
				return false
			}
			n, err := i.ToNumberV(ctx, r)
			if err != nil {
				sortErr = err
				return false
			}
			return n < 0
		}
		// Default: compare by string value.
		sa, err := i.ToStringV(ctx, defined[a])
		if err != nil {
			sortErr = err
			return false
		}
		sb, err := i.ToStringV(ctx, defined[b])
		if err != nil {
			sortErr = err
			return false
		}
		return sa < sb
	})
	if sortErr != nil {
		return nil, sortErr
	}
	for k := 0; k < undefinedCount; k++ {
		defined = append(defined, Undef)
	}
	o.elems = defined
	return o, nil
}

// arrayFlatMap maps each element then flattens the result by one level.
func (i *Interpreter) arrayFlatMap(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	var out []Value
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(_ int, res Value) (bool, error) {
		if ro, ok := res.(*Object); ok && ro.isArray {
			out = append(out, ro.elems...)
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
	return o.elems[idx], nil
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
	out := make([]Value, len(o.elems))
	for k, v := range o.elems {
		out[len(o.elems)-1-k] = v
	}
	return i.newArray(out), nil
}

// arrayToSorted returns a sorted copy without mutating the receiver.
func (i *Interpreter) arrayToSorted(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	copyArr := i.newArray(append([]Value(nil), o.elems...))
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
	copyArr := i.newArray(append([]Value(nil), o.elems...))
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
	out := append([]Value(nil), o.elems...)
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
	var acc Value
	start := len(o.elems) - 1
	if len(args) >= 2 {
		acc = args[1]
	} else {
		if len(o.elems) == 0 {
			return nil, i.throwError(ctx, "TypeError", "Reduce of empty array with no initial value")
		}
		acc = o.elems[start]
		start--
	}
	for j := start; j >= 0; j-- {
		acc, err = callback.fn.call(ctx, Undef, []Value{acc, o.elems[j], Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
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
	for j := len(o.elems) - 1; j >= 0; j-- {
		r, err := cb.fn.call(ctx, arg(args, 1), []Value{o.elems[j], Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
		if ToBoolean(r) {
			return o.elems[j], nil
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
	for j := len(o.elems) - 1; j >= 0; j-- {
		r, err := cb.fn.call(ctx, arg(args, 1), []Value{o.elems[j], Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
		if ToBoolean(r) {
			return Number(float64(j)), nil
		}
	}
	return Number(-1), nil
}
