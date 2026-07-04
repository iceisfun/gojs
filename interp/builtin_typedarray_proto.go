package interp

import (
	"context"
	"math"
	"math/big"
	"sort"
)

// This file implements the %TypedArray.prototype% accessors and methods
// (ECMA-262 §23.2.3).

// thisTA validates that `this` is a TypedArray and returns it.
func (i *Interpreter) thisTA(ctx context.Context, this Value, method string) (*Object, *typedArrayData, error) {
	o, ok := this.(*Object)
	if !ok || o.typedArray == nil {
		return nil, nil, i.throwError(ctx, "TypeError", "TypedArray.prototype."+method+" called on a non-TypedArray")
	}
	return o, o.typedArray, nil
}

// validateTA implements ValidateTypedArray (§23.2.3.1): it validates `this` is a
// TypedArray, is not out of bounds, and returns the current element length.
func (i *Interpreter) validateTA(ctx context.Context, this Value, method string) (*Object, *typedArrayData, int, error) {
	o, td, err := i.thisTA(ctx, this, method)
	if err != nil {
		return nil, nil, 0, err
	}
	oob, length := td.outOfBounds()
	if oob {
		return nil, nil, 0, i.throwError(ctx, "TypeError", "TypedArray.prototype."+method+" called on an out-of-bounds TypedArray")
	}
	return o, td, length, nil
}

// taGetIdx reads element k, returning undefined for an out-of-bounds index.
func taGetIdx(td *typedArrayData, k int) Value {
	if _, ok := td.validIndex(float64(k)); ok {
		return td.getElement(k)
	}
	return Undef
}

// taSpeciesCreate implements TypedArraySpeciesCreate (§23.2.4.2): it constructs a
// new TypedArray via the exemplar's species constructor and verifies the content
// type matches.
func (i *Interpreter) taSpeciesCreate(ctx context.Context, exemplar *Object, args []Value) (*Object, error) {
	defCtor := i.typedArrayKindCtors[exemplar.typedArray.kind]
	ctor, err := i.speciesConstructor(ctx, exemplar, defCtor)
	if err != nil {
		return nil, err
	}
	result, err := i.typedArrayCreateFromCtor(ctx, ctor, args)
	if err != nil {
		return nil, err
	}
	if taKinds[result.typedArray.kind].bigInt != taKinds[exemplar.typedArray.kind].bigInt {
		return nil, i.throwError(ctx, "TypeError", "TypedArray species has an incompatible content type")
	}
	return result, nil
}

// initTypedArrayProto installs %TypedArray.prototype%'s accessors and methods.
func (i *Interpreter) initTypedArrayProto() {
	proto := i.typedArrayProto

	// get buffer — §23.2.3.2.
	i.defineGetter(proto, "buffer", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		_, td, err := i.thisTA(ctx, this, "buffer")
		if err != nil {
			return nil, err
		}
		return td.buffer, nil
	})

	// get byteLength — §23.2.3.3.
	i.defineGetter(proto, "byteLength", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		_, td, err := i.thisTA(ctx, this, "byteLength")
		if err != nil {
			return nil, err
		}
		oob, length := td.outOfBounds()
		if oob {
			return Number(0), nil
		}
		return Number(float64(length * taKinds[td.kind].size)), nil
	})

	// get byteOffset — §23.2.3.4.
	i.defineGetter(proto, "byteOffset", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		_, td, err := i.thisTA(ctx, this, "byteOffset")
		if err != nil {
			return nil, err
		}
		if oob, _ := td.outOfBounds(); oob {
			return Number(0), nil
		}
		return Number(float64(td.byteOffset)), nil
	})

	// get length — §23.2.3.20.
	i.defineGetter(proto, "length", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		_, td, err := i.thisTA(ctx, this, "length")
		if err != nil {
			return nil, err
		}
		oob, length := td.outOfBounds()
		if oob {
			return Number(0), nil
		}
		return Number(float64(length)), nil
	})

	// get [Symbol.toStringTag] — §23.2.3.38. Returns the type name for a
	// TypedArray and undefined otherwise (and never throws).
	tag := i.newNativeFunc("get [Symbol.toStringTag]", 0, func(ctx context.Context, this Value, _ []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok || o.typedArray == nil {
			return Undef, nil
		}
		return String(taKinds[o.typedArray.kind].name), nil
	})
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Get: tag, Accessor: true, Configurable: true})

	i.defineMethod(proto, "at", 1, i.taAt)
	i.defineMethod(proto, "copyWithin", 2, i.taCopyWithin)
	i.defineMethod(proto, "entries", 0, i.taEntries)
	i.defineMethod(proto, "every", 1, i.taEvery)
	i.defineMethod(proto, "fill", 1, i.taFill)
	i.defineMethod(proto, "filter", 1, i.taFilter)
	i.defineMethod(proto, "find", 1, i.taFind)
	i.defineMethod(proto, "findIndex", 1, i.taFindIndex)
	i.defineMethod(proto, "findLast", 1, i.taFindLast)
	i.defineMethod(proto, "findLastIndex", 1, i.taFindLastIndex)
	i.defineMethod(proto, "forEach", 1, i.taForEach)
	i.defineMethod(proto, "includes", 1, i.taIncludes)
	i.defineMethod(proto, "indexOf", 1, i.taIndexOf)
	i.defineMethod(proto, "join", 1, i.taJoin)
	i.defineMethod(proto, "keys", 0, i.taKeys)
	i.defineMethod(proto, "lastIndexOf", 1, i.taLastIndexOf)
	i.defineMethod(proto, "map", 1, i.taMap)
	i.defineMethod(proto, "reduce", 1, i.taReduce)
	i.defineMethod(proto, "reduceRight", 1, i.taReduceRight)
	i.defineMethod(proto, "reverse", 0, i.taReverse)
	i.defineMethod(proto, "set", 1, i.taSet)
	i.defineMethod(proto, "slice", 2, i.taSlice)
	i.defineMethod(proto, "some", 1, i.taSome)
	i.defineMethod(proto, "sort", 1, i.taSort)
	i.defineMethod(proto, "subarray", 2, i.taSubarray)
	i.defineMethod(proto, "toLocaleString", 0, i.taToLocaleString)
	i.defineMethod(proto, "toReversed", 0, i.taToReversed)
	i.defineMethod(proto, "toSorted", 1, i.taToSorted)
	i.defineMethod(proto, "with", 2, i.taWith)
	values := i.defineMethod(proto, "values", 0, i.taValues)

	// Array.prototype.toString is shared as %TypedArray.prototype.toString isn't
	// specified separately — TypedArray.prototype.toString is %Array.prototype.
	// toString (§23.2.3.34).
	if ts, ok := i.arrayProto.getOwn(StrKey("toString")); ok {
		proto.defineOwn(StrKey("toString"), &Property{Value: ts.Value, Writable: true, Enumerable: false, Configurable: true})
	}

	// [Symbol.iterator] is the same function object as "values" (§23.2.3.36).
	proto.defineOwn(SymKey(i.symIterator), &Property{Value: values, Writable: true, Enumerable: false, Configurable: true})
}

// ---------------------------------------------------------------------------
// Iteration methods
// ---------------------------------------------------------------------------

// taNextLength reports the current TypedArray length for an in-progress array
// iterator, throwing a TypeError when the view has become detached or out of
// bounds (§23.1.5.1 step 8: the check runs on every next until exhaustion).
func (i *Interpreter) taNextLength(ctx context.Context, td *typedArrayData) (int, error) {
	oob, n := td.outOfBounds()
	if oob {
		return 0, i.throwError(ctx, "TypeError", "TypedArray is out of bounds")
	}
	return n, nil
}

func (i *Interpreter) taKeys(ctx context.Context, this Value, _ []Value) (Value, error) {
	_, td, _, err := i.validateTA(ctx, this, "keys")
	if err != nil {
		return nil, err
	}
	idx := 0
	done := false
	return i.newArrayIteratorObj(func(ctx context.Context) (Value, bool, error) {
		if done {
			return Undef, true, nil
		}
		length, err := i.taNextLength(ctx, td)
		if err != nil {
			return nil, false, err
		}
		if idx >= length {
			done = true
			return Undef, true, nil
		}
		k := Number(float64(idx))
		idx++
		return k, false, nil
	}), nil
}

func (i *Interpreter) taValues(ctx context.Context, this Value, _ []Value) (Value, error) {
	_, td, _, err := i.validateTA(ctx, this, "values")
	if err != nil {
		return nil, err
	}
	idx := 0
	done := false
	return i.newArrayIteratorObj(func(ctx context.Context) (Value, bool, error) {
		if done {
			return Undef, true, nil
		}
		length, err := i.taNextLength(ctx, td)
		if err != nil {
			return nil, false, err
		}
		if idx >= length {
			done = true
			return Undef, true, nil
		}
		v := taGetIdx(td, idx)
		idx++
		return v, false, nil
	}), nil
}

func (i *Interpreter) taEntries(ctx context.Context, this Value, _ []Value) (Value, error) {
	_, td, _, err := i.validateTA(ctx, this, "entries")
	if err != nil {
		return nil, err
	}
	idx := 0
	done := false
	return i.newArrayIteratorObj(func(ctx context.Context) (Value, bool, error) {
		if done {
			return Undef, true, nil
		}
		length, err := i.taNextLength(ctx, td)
		if err != nil {
			return nil, false, err
		}
		if idx >= length {
			done = true
			return Undef, true, nil
		}
		pair := i.newArray([]Value{Number(float64(idx)), taGetIdx(td, idx)})
		idx++
		return pair, false, nil
	}), nil
}

// ---------------------------------------------------------------------------
// Simple index methods
// ---------------------------------------------------------------------------

func (i *Interpreter) taAt(ctx context.Context, this Value, args []Value) (Value, error) {
	_, td, length, err := i.validateTA(ctx, this, "at")
	if err != nil {
		return nil, err
	}
	rel, err := i.argInt(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	k := rel
	if k < 0 {
		k += length
	}
	if k < 0 || k >= length {
		return Undef, nil
	}
	return taGetIdx(td, k), nil
}

func (i *Interpreter) taFill(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "fill")
	if err != nil {
		return nil, err
	}
	// Coerce the value to the content type before resolving the range.
	var num float64
	var bnum *big.Int
	if taKinds[td.kind].bigInt {
		bv, err := i.toBigIntStrict(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		bnum = bv.(*BigInt).Int
	} else {
		num, err = i.ToNumberV(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
	}
	start, err := i.taRelativeIndex(ctx, arg(args, 1), length, 0)
	if err != nil {
		return nil, err
	}
	end, err := i.taRelativeIndex(ctx, arg(args, 2), length, length)
	if err != nil {
		return nil, err
	}
	// The coercions above may have detached or shrunk the view; the spec
	// re-checks the witness record here and re-clamps the fill range.
	oob, curLen := td.outOfBounds()
	if oob {
		return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.fill called on an out-of-bounds TypedArray")
	}
	if curLen < length {
		length = curLen
		if end > length {
			end = length
		}
		if start > length {
			start = length
		}
	}
	for k := start; k < end; k++ {
		if _, ok := td.validIndex(float64(k)); !ok {
			continue
		}
		if bnum != nil {
			td.setElementBig(k, bnum)
		} else {
			td.setElementNum(k, num)
		}
	}
	return o, nil
}

func (i *Interpreter) taCopyWithin(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "copyWithin")
	if err != nil {
		return nil, err
	}
	target, err := i.taRelativeIndex(ctx, arg(args, 0), length, 0)
	if err != nil {
		return nil, err
	}
	start, err := i.taRelativeIndex(ctx, arg(args, 1), length, 0)
	if err != nil {
		return nil, err
	}
	end, err := i.taRelativeIndex(ctx, arg(args, 2), length, length)
	if err != nil {
		return nil, err
	}
	count := end - start
	if lim := length - target; count > lim {
		count = lim
	}
	if count <= 0 {
		return o, nil
	}
	// The argument coercions above may have detached or resized the buffer; the
	// spec re-checks the witness record and re-reads the length here, then
	// re-clamps the copy so it stays within the (possibly smaller) view.
	oob, curLen := td.outOfBounds()
	if oob {
		return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.copyWithin called on an out-of-bounds TypedArray")
	}
	if start >= curLen || target >= curLen {
		return o, nil
	}
	if start+count > curLen {
		count = curLen - start
	}
	if target+count > curLen {
		count = curLen - target
	}
	if count <= 0 {
		return o, nil
	}
	// A single overlap-safe byte move (Go's copy behaves like memmove).
	ab, _ := arrayBufferOf(td.buffer)
	size := taKinds[td.kind].size
	base := td.byteOffset
	src := base + start*size
	dst := base + target*size
	n := count * size
	copy(ab.data[dst:dst+n], ab.data[src:src+n])
	return o, nil
}

func (i *Interpreter) taReverse(ctx context.Context, this Value, _ []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "reverse")
	if err != nil {
		return nil, err
	}
	for lo, hi := 0, length-1; lo < hi; lo, hi = lo+1, hi-1 {
		a := td.getElement(lo)
		b := td.getElement(hi)
		i.writeElem(td, lo, b)
		i.writeElem(td, hi, a)
	}
	return o, nil
}

// writeElem stores an already-typed element value (Number or BigInt) at index k.
func (i *Interpreter) writeElem(td *typedArrayData, k int, v Value) {
	if _, ok := td.validIndex(float64(k)); !ok {
		return
	}
	if b, ok := v.(*BigInt); ok {
		td.setElementBig(k, b.Int)
	} else {
		td.setElementNum(k, ToNumber(v))
	}
}

// ---------------------------------------------------------------------------
// Search methods
// ---------------------------------------------------------------------------

func (i *Interpreter) taIndexOf(ctx context.Context, this Value, args []Value) (Value, error) {
	_, td, length, err := i.validateTA(ctx, this, "indexOf")
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return Number(-1), nil
	}
	target := arg(args, 0)
	from, err := i.taFromIndex(ctx, arg(args, 1), length)
	if err != nil {
		return nil, err
	}
	for k := from; k < length; k++ {
		idx, ok := td.validIndex(float64(k))
		if !ok {
			continue // HasProperty is false for an out-of-bounds index
		}
		if strictEquals(td.getElement(idx), target) {
			return Number(float64(k)), nil
		}
	}
	return Number(-1), nil
}

func (i *Interpreter) taLastIndexOf(ctx context.Context, this Value, args []Value) (Value, error) {
	_, td, length, err := i.validateTA(ctx, this, "lastIndexOf")
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return Number(-1), nil
	}
	target := arg(args, 0)
	from := length - 1
	if len(args) > 1 {
		n, err := i.argNum(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		fromN := ToInteger(n)
		if math.IsInf(fromN, -1) {
			return Number(-1), nil
		}
		if fromN >= 0 {
			if fromN < float64(from) { // avoids int(+Inf)
				from = int(fromN)
			}
		} else {
			from = length + int(fromN)
		}
	}
	for k := from; k >= 0; k-- {
		idx, ok := td.validIndex(float64(k))
		if !ok {
			continue // HasProperty is false for an out-of-bounds index
		}
		if strictEquals(td.getElement(idx), target) {
			return Number(float64(k)), nil
		}
	}
	return Number(-1), nil
}

func (i *Interpreter) taIncludes(ctx context.Context, this Value, args []Value) (Value, error) {
	_, td, length, err := i.validateTA(ctx, this, "includes")
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return False, nil
	}
	target := arg(args, 0)
	from, err := i.taFromIndex(ctx, arg(args, 1), length)
	if err != nil {
		return nil, err
	}
	for k := from; k < length; k++ {
		v := taGetIdx(td, k)
		if sameValueZero(v, target) {
			return True, nil
		}
	}
	return False, nil
}

// ---------------------------------------------------------------------------
// Callback methods
// ---------------------------------------------------------------------------

func (i *Interpreter) taCallback(ctx context.Context, args []Value, method string) (*Object, error) {
	cb, ok := arg(args, 0).(*Object)
	if !ok || !cb.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype."+method+": callback is not a function")
	}
	return cb, nil
}

func (i *Interpreter) taForEach(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "forEach")
	if err != nil {
		return nil, err
	}
	cb, err := i.taCallback(ctx, args, "forEach")
	if err != nil {
		return nil, err
	}
	thisArg := arg(args, 1)
	for k := 0; k < length; k++ {
		if _, err := cb.fn.call(ctx, thisArg, []Value{taGetIdx(td, k), Number(float64(k)), o}); err != nil {
			return nil, err
		}
	}
	return Undef, nil
}

func (i *Interpreter) taEvery(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "every")
	if err != nil {
		return nil, err
	}
	cb, err := i.taCallback(ctx, args, "every")
	if err != nil {
		return nil, err
	}
	thisArg := arg(args, 1)
	for k := 0; k < length; k++ {
		r, err := cb.fn.call(ctx, thisArg, []Value{taGetIdx(td, k), Number(float64(k)), o})
		if err != nil {
			return nil, err
		}
		if !ToBoolean(r) {
			return False, nil
		}
	}
	return True, nil
}

func (i *Interpreter) taSome(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "some")
	if err != nil {
		return nil, err
	}
	cb, err := i.taCallback(ctx, args, "some")
	if err != nil {
		return nil, err
	}
	thisArg := arg(args, 1)
	for k := 0; k < length; k++ {
		r, err := cb.fn.call(ctx, thisArg, []Value{taGetIdx(td, k), Number(float64(k)), o})
		if err != nil {
			return nil, err
		}
		if ToBoolean(r) {
			return True, nil
		}
	}
	return False, nil
}

func (i *Interpreter) taFindImpl(ctx context.Context, this Value, args []Value, method string, fromEnd, wantIndex bool) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, method)
	if err != nil {
		return nil, err
	}
	cb, err := i.taCallback(ctx, args, method)
	if err != nil {
		return nil, err
	}
	thisArg := arg(args, 1)
	// step returns the value read before invoking the predicate (find/findLast
	// return that captured kValue, not a re-read after the callback).
	step := func(k int) (Value, bool, error) {
		v := taGetIdx(td, k)
		r, err := cb.fn.call(ctx, thisArg, []Value{v, Number(float64(k)), o})
		if err != nil {
			return nil, false, err
		}
		return v, ToBoolean(r), nil
	}
	if fromEnd {
		for k := length - 1; k >= 0; k-- {
			v, match, err := step(k)
			if err != nil {
				return nil, err
			}
			if match {
				if wantIndex {
					return Number(float64(k)), nil
				}
				return v, nil
			}
		}
	} else {
		for k := 0; k < length; k++ {
			v, match, err := step(k)
			if err != nil {
				return nil, err
			}
			if match {
				if wantIndex {
					return Number(float64(k)), nil
				}
				return v, nil
			}
		}
	}
	if wantIndex {
		return Number(-1), nil
	}
	return Undef, nil
}

func (i *Interpreter) taFind(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.taFindImpl(ctx, this, args, "find", false, false)
}
func (i *Interpreter) taFindIndex(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.taFindImpl(ctx, this, args, "findIndex", false, true)
}
func (i *Interpreter) taFindLast(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.taFindImpl(ctx, this, args, "findLast", true, false)
}
func (i *Interpreter) taFindLastIndex(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.taFindImpl(ctx, this, args, "findLastIndex", true, true)
}

func (i *Interpreter) taReduce(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.taReduceImpl(ctx, this, args, false)
}
func (i *Interpreter) taReduceRight(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.taReduceImpl(ctx, this, args, true)
}

func (i *Interpreter) taReduceImpl(ctx context.Context, this Value, args []Value, fromRight bool) (Value, error) {
	method := "reduce"
	if fromRight {
		method = "reduceRight"
	}
	o, td, length, err := i.validateTA(ctx, this, method)
	if err != nil {
		return nil, err
	}
	cb, err := i.taCallback(ctx, args, method)
	if err != nil {
		return nil, err
	}
	hasInit := len(args) > 1
	var acc Value
	idxs := make([]int, length)
	for k := 0; k < length; k++ {
		if fromRight {
			idxs[k] = length - 1 - k
		} else {
			idxs[k] = k
		}
	}
	pos := 0
	if hasInit {
		acc = args[1]
	} else {
		if length == 0 {
			return nil, i.throwError(ctx, "TypeError", "Reduce of empty TypedArray with no initial value")
		}
		acc = taGetIdx(td, idxs[0])
		pos = 1
	}
	for ; pos < length; pos++ {
		k := idxs[pos]
		acc, err = cb.fn.call(ctx, Undef, []Value{acc, taGetIdx(td, k), Number(float64(k)), o})
		if err != nil {
			return nil, err
		}
	}
	return acc, nil
}

// ---------------------------------------------------------------------------
// Methods producing new TypedArrays / strings
// ---------------------------------------------------------------------------

func (i *Interpreter) taMap(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "map")
	if err != nil {
		return nil, err
	}
	cb, err := i.taCallback(ctx, args, "map")
	if err != nil {
		return nil, err
	}
	thisArg := arg(args, 1)
	target, err := i.taSpeciesCreate(ctx, o, []Value{Number(float64(length))})
	if err != nil {
		return nil, err
	}
	for k := 0; k < length; k++ {
		mapped, err := cb.fn.call(ctx, thisArg, []Value{taGetIdx(td, k), Number(float64(k)), o})
		if err != nil {
			return nil, err
		}
		if _, err := i.typedArraySetElement(ctx, target.typedArray, float64(k), mapped); err != nil {
			return nil, err
		}
	}
	return target, nil
}

func (i *Interpreter) taFilter(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "filter")
	if err != nil {
		return nil, err
	}
	cb, err := i.taCallback(ctx, args, "filter")
	if err != nil {
		return nil, err
	}
	thisArg := arg(args, 1)
	var kept []Value
	for k := 0; k < length; k++ {
		v := taGetIdx(td, k)
		r, err := cb.fn.call(ctx, thisArg, []Value{v, Number(float64(k)), o})
		if err != nil {
			return nil, err
		}
		if ToBoolean(r) {
			kept = append(kept, v)
		}
	}
	target, err := i.taSpeciesCreate(ctx, o, []Value{Number(float64(len(kept)))})
	if err != nil {
		return nil, err
	}
	for k, v := range kept {
		if _, err := i.typedArraySetElement(ctx, target.typedArray, float64(k), v); err != nil {
			return nil, err
		}
	}
	return target, nil
}

func (i *Interpreter) taSlice(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "slice")
	if err != nil {
		return nil, err
	}
	start, err := i.taRelativeIndex(ctx, arg(args, 0), length, 0)
	if err != nil {
		return nil, err
	}
	end, err := i.taRelativeIndex(ctx, arg(args, 1), length, length)
	if err != nil {
		return nil, err
	}
	count := end - start
	if count < 0 {
		count = 0
	}
	target, err := i.taSpeciesCreate(ctx, o, []Value{Number(float64(count))})
	if err != nil {
		return nil, err
	}
	if count > 0 {
		// Creating the destination ran user code (the species lookup), which may
		// have detached or shrunk the source; re-check and re-clamp (§23.2.3.27).
		oob, curLen := td.outOfBounds()
		if oob {
			return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.slice called on an out-of-bounds TypedArray")
		}
		if curLen < start+count {
			count = curLen - start
			if count < 0 {
				count = 0
			}
		}
	}
	for n := 0; n < count; n++ {
		v := taGetIdx(td, start+n)
		if _, err := i.typedArraySetElement(ctx, target.typedArray, float64(n), v); err != nil {
			return nil, err
		}
	}
	return target, nil
}

func (i *Interpreter) taSubarray(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, err := i.thisTA(ctx, this, "subarray")
	if err != nil {
		return nil, err
	}
	oob, srcLength := td.outOfBounds()
	if oob {
		srcLength = 0
	}
	begin, err := i.taRelativeIndex(ctx, arg(args, 0), srcLength, 0)
	if err != nil {
		return nil, err
	}
	elementSize := taKinds[td.kind].size
	beginByteOffset := td.byteOffset + begin*elementSize
	// A length-tracking view with no explicit end yields a length-tracking
	// subarray: only the buffer and offset are passed to the constructor.
	if td.autoLength && IsUndefined(arg(args, 1)) {
		return i.taSpeciesCreate(ctx, o, []Value{td.buffer, Number(float64(beginByteOffset))})
	}
	end, err := i.taRelativeIndex(ctx, arg(args, 1), srcLength, srcLength)
	if err != nil {
		return nil, err
	}
	newLength := end - begin
	if newLength < 0 {
		newLength = 0
	}
	return i.taSpeciesCreate(ctx, o, []Value{td.buffer, Number(float64(beginByteOffset)), Number(float64(newLength))})
}

func (i *Interpreter) taJoin(ctx context.Context, this Value, args []Value) (Value, error) {
	_, td, length, err := i.validateTA(ctx, this, "join")
	if err != nil {
		return nil, err
	}
	sep := ","
	if s := arg(args, 0); !IsUndefined(s) {
		sep, err = i.ToStringV(ctx, s)
		if err != nil {
			return nil, err
		}
	}
	var b []byte
	for k := 0; k < length; k++ {
		if k > 0 {
			b = append(b, sep...)
		}
		v := taGetIdx(td, k)
		if IsUndefined(v) {
			continue
		}
		s, err := i.ToStringV(ctx, v)
		if err != nil {
			return nil, err
		}
		b = append(b, s...)
	}
	return String(string(b)), nil
}

func (i *Interpreter) taToLocaleString(ctx context.Context, this Value, args []Value) (Value, error) {
	_, td, length, err := i.validateTA(ctx, this, "toLocaleString")
	if err != nil {
		return nil, err
	}
	var b []byte
	for k := 0; k < length; k++ {
		if k > 0 {
			b = append(b, ',')
		}
		v := taGetIdx(td, k)
		// len is captured once (before the loop), but a user toLocaleString may
		// shrink a resizable buffer mid-iteration, making later original indices
		// out of bounds. Get then returns undefined, and §23.2.3.32 step 7.c only
		// invokes toLocaleString when the element is not undefined — the separator
		// is still emitted, so the trailing slots render as empty strings.
		if v == Value(Undef) {
			continue
		}
		// Invoke(nextElement, "toLocaleString"): the element's own
		// toLocaleString is called (Number/BigInt.prototype.toLocaleString for a
		// numeric element), and any thrown value propagates.
		m, err := i.getProperty(ctx, v, StrKey("toLocaleString"))
		if err != nil {
			return nil, err
		}
		mo, ok := m.(*Object)
		if !ok || !mo.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.toLocaleString: element has no toLocaleString method")
		}
		rv, err := mo.fn.call(ctx, v, nil)
		if err != nil {
			return nil, err
		}
		s, err := i.ToStringV(ctx, rv)
		if err != nil {
			return nil, err
		}
		b = append(b, s...)
	}
	return String(string(b)), nil
}

func (i *Interpreter) taToReversed(ctx context.Context, this Value, _ []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "toReversed")
	if err != nil {
		return nil, err
	}
	target, err := i.taSpeciesCreateSameKind(ctx, o, length)
	if err != nil {
		return nil, err
	}
	for k := 0; k < length; k++ {
		i.writeElem(target.typedArray, k, taGetIdx(td, length-1-k))
	}
	return target, nil
}

func (i *Interpreter) taToSorted(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "toSorted")
	if err != nil {
		return nil, err
	}
	var cmp *Object
	if c := arg(args, 0); !IsUndefined(c) {
		co, ok := c.(*Object)
		if !ok || !co.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.toSorted: comparator is not a function")
		}
		cmp = co
	}
	elems := make([]Value, length)
	for k := 0; k < length; k++ {
		elems[k] = taGetIdx(td, k)
	}
	if err := i.taSortValues(ctx, td.kind, elems, cmp); err != nil {
		return nil, err
	}
	target, err := i.taSpeciesCreateSameKind(ctx, o, length)
	if err != nil {
		return nil, err
	}
	for k, v := range elems {
		i.writeElem(target.typedArray, k, v)
	}
	return target, nil
}

func (i *Interpreter) taWith(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "with")
	if err != nil {
		return nil, err
	}
	relN, err := i.ToNumberV(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	relative := ToInteger(relN)
	actual := relative
	if relative < 0 {
		actual = float64(length) + relative
	}
	// actualIndex is a mathematical integer, so 𝔽(actualIndex) is never -0.
	// Normalize a negative zero (e.g. from ToIntegerOrInfinity(-0) or a small
	// negative fraction) to +0 so that IsValidIntegerIndex does not reject it.
	if actual == 0 {
		actual = 0
	}
	// Coerce the value to the content type (which may throw TypeError).
	value := arg(args, 1)
	var conv Value
	if taKinds[td.kind].bigInt {
		bv, err := i.toBigIntStrict(ctx, value)
		if err != nil {
			return nil, err
		}
		conv = bv
	} else {
		n, err := i.ToNumberV(ctx, value)
		if err != nil {
			return nil, err
		}
		conv = Number(n)
	}
	// IsValidIntegerIndex is evaluated after coercion against the current length.
	actualIndex, ok := td.validIndex(actual)
	if !ok {
		return nil, i.throwError(ctx, "RangeError", "TypedArray.prototype.with: invalid index")
	}
	target, err := i.taSpeciesCreateSameKind(ctx, o, length)
	if err != nil {
		return nil, err
	}
	for k := 0; k < length; k++ {
		if k == actualIndex {
			i.writeElem(target.typedArray, k, conv)
		} else {
			i.writeElem(target.typedArray, k, taGetIdx(td, k))
		}
	}
	return target, nil
}

// taSpeciesCreateSameKind creates a new TypedArray of the same concrete kind as
// exemplar with the given length (used by the copying methods, which per spec
// use the intrinsic constructor rather than SpeciesConstructor).
func (i *Interpreter) taSpeciesCreateSameKind(ctx context.Context, exemplar *Object, length int) (*Object, error) {
	ctor := i.typedArrayKindCtors[exemplar.typedArray.kind]
	return i.typedArrayCreateFromCtor(ctx, ctor, []Value{Number(float64(length))})
}

// ---------------------------------------------------------------------------
// sort / set
// ---------------------------------------------------------------------------

func (i *Interpreter) taSort(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, length, err := i.validateTA(ctx, this, "sort")
	if err != nil {
		return nil, err
	}
	var cmp *Object
	if c := arg(args, 0); !IsUndefined(c) {
		co, ok := c.(*Object)
		if !ok || !co.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.sort: comparator is not a function")
		}
		cmp = co
	}
	elems := make([]Value, length)
	for k := 0; k < length; k++ {
		elems[k] = td.getElement(k)
	}
	if err := i.taSortValues(ctx, td.kind, elems, cmp); err != nil {
		return nil, err
	}
	for k, v := range elems {
		i.writeElem(td, k, v)
	}
	return o, nil
}

// taSortValues sorts elems in place using the default TypedArray comparison or a
// user comparator. A comparator error is propagated.
func (i *Interpreter) taSortValues(ctx context.Context, kind taKind, elems []Value, cmp *Object) error {
	var sortErr error
	sort.SliceStable(elems, func(a, b int) bool {
		if sortErr != nil {
			return false
		}
		if cmp != nil {
			r, err := cmp.fn.call(ctx, Undef, []Value{elems[a], elems[b]})
			if err != nil {
				sortErr = err
				return false
			}
			n, err := i.ToNumberV(ctx, r)
			if err != nil {
				sortErr = err
				return false
			}
			if math.IsNaN(n) {
				return false
			}
			return n < 0
		}
		return taDefaultLess(kind, elems[a], elems[b])
	})
	return sortErr
}

// taDefaultLess implements the default CompareTypedArrayElements ordering: NaN
// last, -0 before +0, otherwise ascending numeric order.
func taDefaultLess(kind taKind, a, b Value) bool {
	if taKinds[kind].bigInt {
		return a.(*BigInt).Int.Cmp(b.(*BigInt).Int) < 0
	}
	x, y := float64(a.(Number)), float64(b.(Number))
	if math.IsNaN(x) {
		return false
	}
	if math.IsNaN(y) {
		return true
	}
	if x < y {
		return true
	}
	if x > y {
		return false
	}
	// Equal magnitude: -0 sorts before +0.
	if x == 0 && y == 0 {
		return math.Signbit(x) && !math.Signbit(y)
	}
	return false
}

func (i *Interpreter) taSet(ctx context.Context, this Value, args []Value) (Value, error) {
	o, td, err := i.thisTA(ctx, this, "set")
	if err != nil {
		return nil, err
	}
	offset, err := i.argNum(ctx, args, 1)
	if err != nil {
		return nil, err
	}
	targetOffset := ToInteger(offset)
	if targetOffset < 0 {
		return nil, i.throwError(ctx, "RangeError", "TypedArray.prototype.set: offset is negative")
	}
	oob, targetLength := td.outOfBounds()
	if oob {
		return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.set called on an out-of-bounds TypedArray")
	}
	_ = o
	source := arg(args, 0)
	if src, ok := source.(*Object); ok && src.typedArray != nil {
		// Source is a TypedArray.
		soob, srcLength := src.typedArray.outOfBounds()
		if soob {
			return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.set: source is out of bounds")
		}
		if targetOffset+float64(srcLength) > float64(targetLength) {
			return nil, i.throwError(ctx, "RangeError", "TypedArray.prototype.set: source is too large")
		}
		if taKinds[src.typedArray.kind].bigInt != taKinds[td.kind].bigInt {
			return nil, i.throwError(ctx, "TypeError", "TypedArray.prototype.set: cannot mix BigInt and non-BigInt arrays")
		}
		// Snapshot source values first to handle overlapping buffers.
		vals := make([]Value, srcLength)
		for k := 0; k < srcLength; k++ {
			vals[k] = src.typedArray.getElement(k)
		}
		for k := 0; k < srcLength; k++ {
			i.writeElem(td, int(targetOffset)+k, vals[k])
		}
		return Undef, nil
	}
	// Source is array-like: ToObject coerces a primitive (throwing for
	// null/undefined) and yields a length-0 view for e.g. a number.
	so, err := i.ToObject(ctx, source)
	if err != nil {
		return nil, err
	}
	srcLength, err := i.lengthOfArrayLike(ctx, so)
	if err != nil {
		return nil, err
	}
	if targetOffset+float64(srcLength) > float64(targetLength) {
		return nil, i.throwError(ctx, "RangeError", "TypedArray.prototype.set: source is too large")
	}
	for k := 0; k < srcLength; k++ {
		v, err := so.GetStr(ctx, intToStr(k))
		if err != nil {
			return nil, err
		}
		if _, err := i.typedArraySetElement(ctx, td, float64(int(targetOffset)+k), v); err != nil {
			return nil, err
		}
	}
	return Undef, nil
}

// ---------------------------------------------------------------------------
// Index helpers
// ---------------------------------------------------------------------------

// taRelativeIndex resolves a relative index argument (ToIntegerOrInfinity
// clamped to [0, length]).
func (i *Interpreter) taRelativeIndex(ctx context.Context, v Value, length, def int) (int, error) {
	if IsUndefined(v) {
		return def, nil
	}
	n, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	rel := ToInteger(n)
	switch {
	case math.IsInf(rel, -1):
		return 0, nil
	case rel < 0:
		if r := float64(length) + rel; r > 0 {
			return int(r), nil
		}
		return 0, nil
	case math.IsInf(rel, 1) || rel > float64(length):
		return length, nil
	default:
		return int(rel), nil
	}
}

// taFromIndex resolves the fromIndex argument of indexOf/includes.
func (i *Interpreter) taFromIndex(ctx context.Context, v Value, length int) (int, error) {
	if IsUndefined(v) {
		return 0, nil
	}
	n, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	from := ToInteger(n)
	if math.IsInf(from, 1) {
		return length, nil
	}
	if from < 0 {
		if r := float64(length) + from; r > 0 {
			return int(r), nil
		}
		return 0, nil
	}
	if from > float64(length) {
		return length, nil
	}
	return int(from), nil
}
