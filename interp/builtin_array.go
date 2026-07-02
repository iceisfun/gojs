package interp

import "context"

// newArray creates an Array object holding the given elements.
func (i *Interpreter) newArray(elems []Value) *Object {
	o := NewObject(i.arrayProto)
	o.class = "Array"
	o.isArray = true
	if elems == nil {
		elems = []Value{}
	}
	o.elems = elems
	return o
}

// NewArray is the exported constructor used by embedders and other packages.
func (i *Interpreter) NewArray(elems ...Value) *Object { return i.newArray(elems) }

// initArray installs the Array constructor and Array.prototype methods.
func (i *Interpreter) initArray() {
	proto := i.arrayProto

	ctor := i.newNativeCtor("Array", 1, i.arrayConstruct, i.arrayConstruct)
	linkCtor(ctor, proto)
	i.defineSpeciesGetter(ctor)
	i.defineMethod(ctor, "isArray", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		return Bool(ok && o.isArray), nil
	})
	i.defineMethod(ctor, "of", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cp := make([]Value, len(args))
		copy(cp, args)
		return i.newArray(cp), nil
	})
	i.defineMethod(ctor, "from", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		var out []Value
		mapFn, _ := arg(args, 1).(*Object)
		idx := 0
		thisArg := arg(args, 2)
		apply := func(v Value) (Value, error) {
			if mapFn != nil && mapFn.IsCallable() {
				mv, err := mapFn.fn.call(ctx, thisArg, []Value{v, Number(float64(idx))})
				if err != nil {
					return nil, err
				}
				v = mv
			}
			idx++
			return v, nil
		}
		src := arg(args, 0)
		// Prefer the iterator protocol; fall back to an array-like object with a
		// length property (e.g. { length: 3, 0: "a" }).
		if isIterable(i, src) {
			err := i.iterate(ctx, src, func(v Value) error {
				mv, err := apply(v)
				if err != nil {
					return err
				}
				out = append(out, mv)
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else if o, ok := src.(*Object); ok {
			lenV, _ := o.GetStr(ctx, "length")
			n := int(ToInteger(ToNumber(lenV)))
			if n < 0 {
				n = 0
			}
			for j := 0; j < n; j++ {
				ev, _ := o.GetStr(ctx, intToStr(j))
				mv, err := apply(ev)
				if err != nil {
					return nil, err
				}
				out = append(out, mv)
			}
		} else if IsNullish(src) {
			return nil, i.throwError(ctx, "TypeError", "Cannot convert undefined or null to object")
		}
		return i.newArray(out), nil
	})

	type method struct {
		name string
		n    int
		fn   CallFn
	}
	methods := []method{
		{"push", 1, i.arrayPush},
		{"pop", 0, i.arrayPop},
		{"shift", 0, i.arrayShift},
		{"unshift", 1, i.arrayUnshift},
		{"slice", 2, i.arraySlice},
		{"splice", 2, i.arraySplice},
		{"concat", 1, i.arrayConcat},
		{"join", 1, i.arrayJoin},
		{"indexOf", 1, i.arrayIndexOf},
		{"includes", 1, i.arrayIncludes},
		{"forEach", 1, i.arrayForEach},
		{"map", 1, i.arrayMap},
		{"filter", 1, i.arrayFilter},
		{"reduce", 1, i.arrayReduce},
		{"reduceRight", 1, i.arrayReduceRight},
		{"find", 1, i.arrayFind},
		{"findIndex", 1, i.arrayFindIndex},
		{"some", 1, i.arraySome},
		{"every", 1, i.arrayEvery},
		{"reverse", 0, i.arrayReverse},
		{"fill", 1, i.arrayFill},
		{"flat", 0, i.arrayFlat},
		{"flatMap", 1, i.arrayFlatMap},
		{"sort", 1, i.arraySort},
		{"at", 1, i.arrayAt},
		{"lastIndexOf", 1, i.arrayLastIndexOf},
		{"findLast", 1, i.arrayFindLast},
		{"findLastIndex", 1, i.arrayFindLastIndex},
		{"copyWithin", 2, i.arrayCopyWithin},
		{"toReversed", 0, i.arrayToReversed},
		{"toSorted", 1, i.arrayToSorted},
		{"toSpliced", 2, i.arrayToSpliced},
		{"with", 2, i.arrayWith},
		{"keys", 0, i.arrayKeys},
		{"values", 0, i.arrayValues},
		{"entries", 0, i.arrayEntries},
		{"toString", 0, i.arrayToString},
	}
	for _, m := range methods {
		i.defineMethod(proto, m.name, m.n, m.fn)
	}
	// Array.prototype[Symbol.iterator] === values
	valuesFn := i.newNativeFunc("values", 0, i.arrayValues)
	proto.defineOwn(SymKey(i.symIterator), &Property{Value: valuesFn, Writable: true, Configurable: true})

	i.arrayCtor = ctor
	i.setGlobalHidden("Array", ctor)
}

// arrayConstruct implements Array(...) / new Array(...).
func (i *Interpreter) arrayConstruct(ctx context.Context, this Value, args []Value) (Value, error) {
	if len(args) == 1 {
		if n, ok := args[0].(Number); ok {
			length := int(float64(n))
			if float64(n) != float64(length) || length < 0 {
				return nil, i.throwError(ctx, "RangeError", "Invalid array length")
			}
			// Array(n) produces a sparse array of n holes, not n undefineds.
			elems := make([]Value, length)
			for j := range elems {
				elems[j] = theHole
			}
			return i.newArray(elems), nil
		}
	}
	cp := make([]Value, len(args))
	copy(cp, args)
	return i.newArray(cp), nil
}

// thisArray coerces this to an array-like *Object, throwing otherwise.
func (i *Interpreter) thisArray(ctx context.Context, this Value) (*Object, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (i *Interpreter) arrayPush(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	o.elems = append(o.elems, args...)
	return Number(float64(len(o.elems))), nil
}

func (i *Interpreter) arrayPop(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	if len(o.elems) == 0 {
		return Undef, nil
	}
	v := o.elems[len(o.elems)-1]
	o.elems = o.elems[:len(o.elems)-1]
	return undefIfHole(v), nil
}

func (i *Interpreter) arrayShift(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	if len(o.elems) == 0 {
		return Undef, nil
	}
	v := o.elems[0]
	o.elems = o.elems[1:]
	return undefIfHole(v), nil
}

func (i *Interpreter) arrayUnshift(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	o.elems = append(append([]Value{}, args...), o.elems...)
	return Number(float64(len(o.elems))), nil
}

func (i *Interpreter) arraySlice(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	n := len(o.elems)
	start := relIndex(argIntOr(ctx, i, args, 0, 0), n)
	end := n
	if !IsUndefined(arg(args, 1)) {
		end = relIndex(argIntOr(ctx, i, args, 1, n), n)
	}
	var out []Value
	for j := start; j < end; j++ {
		out = append(out, o.elems[j])
	}
	return i.newArray(out), nil
}

func (i *Interpreter) arraySplice(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	n := len(o.elems)
	start := relIndex(argIntOr(ctx, i, args, 0, 0), n)
	deleteCount := n - start
	if len(args) >= 2 {
		deleteCount = argIntOr(ctx, i, args, 1, 0)
		if deleteCount < 0 {
			deleteCount = 0
		}
		if deleteCount > n-start {
			deleteCount = n - start
		}
	}
	removed := make([]Value, 0, deleteCount)
	removed = append(removed, o.elems[start:start+deleteCount]...)
	var inserted []Value
	if len(args) > 2 {
		inserted = args[2:]
	}
	tail := append([]Value{}, o.elems[start+deleteCount:]...)
	o.elems = append(o.elems[:start], append(append([]Value{}, inserted...), tail...)...)
	return i.newArray(removed), nil
}

func (i *Interpreter) arrayConcat(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	out := append([]Value{}, o.elems...)
	for _, a := range args {
		if ao, ok := a.(*Object); ok && ao.isArray {
			out = append(out, ao.elems...)
		} else {
			out = append(out, a)
		}
	}
	return i.newArray(out), nil
}

func (i *Interpreter) arrayJoin(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	sep := ","
	if !IsUndefined(arg(args, 0)) {
		sep, err = i.ToStringV(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
	}
	out := ""
	for j, v := range o.elems {
		if j > 0 {
			out += sep
		}
		// Holes, null, and undefined all render as the empty string.
		if isHole(v) || IsNullish(v) {
			continue
		}
		s, err := i.ToStringV(ctx, v)
		if err != nil {
			return nil, err
		}
		out += s
	}
	return String(out), nil
}

func (i *Interpreter) arrayToString(ctx context.Context, this Value, args []Value) (Value, error) {
	// Array.prototype.toString (§23.1.3.36) is generic: it calls the object's own
	// "join" method, falling back to %Object.prototype.toString%. This is also
	// %TypedArray.prototype.toString%, so it must not assume dense array storage.
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	joinV, err := o.GetStr(ctx, "join")
	if err != nil {
		return nil, err
	}
	if jo, ok := joinV.(*Object); ok && jo.IsCallable() {
		return jo.fn.call(ctx, o, nil)
	}
	toStr, err := i.objectProto.GetStr(ctx, "toString")
	if err != nil {
		return nil, err
	}
	if to, ok := toStr.(*Object); ok && to.IsCallable() {
		return to.fn.call(ctx, o, nil)
	}
	return i.arrayJoin(ctx, this, nil)
}

func (i *Interpreter) arrayIndexOf(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	target := arg(args, 0)
	start := fromIndex(argIntOr(ctx, i, args, 1, 0), len(o.elems))
	for j := start; j < len(o.elems); j++ {
		if strictEquals(o.elems[j], target) {
			return Number(float64(j)), nil
		}
	}
	return Number(-1), nil
}

func (i *Interpreter) arrayIncludes(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	target := arg(args, 0)
	start := fromIndex(argIntOr(ctx, i, args, 1, 0), len(o.elems))
	// includes does not skip holes; a hole is treated as undefined.
	for j := start; j < len(o.elems); j++ {
		if sameValueZero(elemAt(o, j), target) {
			return True, nil
		}
	}
	return False, nil
}

// fromIndex resolves an indexOf/includes start index, clamping a negative value
// relative to length and never below 0.
func fromIndex(idx, n int) int {
	if idx < 0 {
		idx += n
	}
	if idx < 0 {
		return 0
	}
	return idx
}

// eachElem runs fn(this=thisArg, [element, index, array]) for each element.
// When skipHoles is true (the HasProperty-family methods: forEach, map, filter,
// some, every, flatMap) hole indices are not visited at all; when false (the
// Get-from-0..len methods: find, findIndex) a hole is visited with the callback
// receiving undefined. The hole sentinel is never passed to the callback.
func (i *Interpreter) eachElem(ctx context.Context, o *Object, cb Value, thisArg Value, skipHoles bool, fn func(idx int, res Value) (bool, error)) error {
	callback, ok := cb.(*Object)
	if !ok || !callback.IsCallable() {
		return i.throwError(ctx, "TypeError", briefValue(cb)+" is not a function")
	}
	// The iteration length is captured once (per spec): elements the callback
	// appends are not visited, and a callback that shrinks the array leaves the
	// tail as holes rather than reading out of bounds.
	n := len(o.elems)
	for j := 0; j < n; j++ {
		v := theHole
		if j < len(o.elems) {
			v = o.elems[j]
		}
		if isHole(v) {
			if skipHoles {
				continue
			}
			v = Undef
		}
		res, err := callback.fn.call(ctx, thisArg, []Value{v, Number(float64(j)), o})
		if err != nil {
			return err
		}
		if stop, err := fn(j, res); err != nil || stop {
			return err
		}
	}
	return nil
}

func (i *Interpreter) arrayForEach(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), true, func(int, Value) (bool, error) { return false, nil })
	return Undef, err
}

func (i *Interpreter) arrayMap(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	// map preserves hole positions: pre-fill with holes, then overwrite only the
	// indices the callback actually visits.
	out := make([]Value, len(o.elems))
	for j := range out {
		out[j] = theHole
	}
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), true, func(idx int, res Value) (bool, error) {
		out[idx] = res
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return i.newArray(out), nil
}

func (i *Interpreter) arrayFilter(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	var out []Value
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), true, func(idx int, res Value) (bool, error) {
		if ToBoolean(res) {
			out = append(out, elemAt(o, idx))
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return i.newArray(out), nil
}

func (i *Interpreter) arrayFind(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	result := Value(Undef)
	// find does not skip holes; a hole is visited as undefined.
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), false, func(idx int, res Value) (bool, error) {
		if ToBoolean(res) {
			result = elemAt(o, idx)
			return true, nil
		}
		return false, nil
	})
	return result, err
}

func (i *Interpreter) arrayFindIndex(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	result := Number(-1)
	// findIndex does not skip holes; a hole is visited as undefined.
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), false, func(idx int, res Value) (bool, error) {
		if ToBoolean(res) {
			result = Number(float64(idx))
			return true, nil
		}
		return false, nil
	})
	return result, err
}

func (i *Interpreter) arraySome(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	found := false
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), true, func(_ int, res Value) (bool, error) {
		if ToBoolean(res) {
			found = true
			return true, nil
		}
		return false, nil
	})
	return Bool(found), err
}

func (i *Interpreter) arrayEvery(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	all := true
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), true, func(_ int, res Value) (bool, error) {
		if !ToBoolean(res) {
			all = false
			return true, nil
		}
		return false, nil
	})
	return Bool(all), err
}

func (i *Interpreter) arrayReduce(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	callback, ok := arg(args, 0).(*Object)
	if !ok || !callback.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "Reduce callback is not a function")
	}
	// reduce skips holes: they seed neither the accumulator nor a callback call.
	var acc Value
	haveAcc := false
	if len(args) >= 2 {
		acc = args[1]
		haveAcc = true
	}
	for j := 0; j < len(o.elems); j++ {
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

func (i *Interpreter) arrayReverse(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	for a, b := 0, len(o.elems)-1; a < b; a, b = a+1, b-1 {
		o.elems[a], o.elems[b] = o.elems[b], o.elems[a]
	}
	return o, nil
}

func (i *Interpreter) arrayFill(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	val := arg(args, 0)
	n := len(o.elems)
	start := 0
	end := n
	if !IsUndefined(arg(args, 1)) {
		start = relIndex(argIntOr(ctx, i, args, 1, 0), n)
	}
	if !IsUndefined(arg(args, 2)) {
		end = relIndex(argIntOr(ctx, i, args, 2, n), n)
	}
	for j := start; j < end; j++ {
		o.elems[j] = val
	}
	return o, nil
}

func (i *Interpreter) arrayFlat(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	depth := 1
	if !IsUndefined(arg(args, 0)) {
		d, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		// flat(Infinity) flattens all levels; guard the float->int conversion
		// (int(+Inf) is undefined in Go).
		switch {
		case d >= float64(1<<30):
			depth = 1 << 30
		case d < 0:
			depth = 0
		default:
			depth = int(ToInteger(d))
		}
	}
	var flatten func(elems []Value, d int) []Value
	flatten = func(elems []Value, d int) []Value {
		var out []Value
		for _, v := range elems {
			if isHole(v) {
				continue // flatten uses HasProperty; holes are skipped
			}
			if vo, ok := v.(*Object); ok && vo.isArray && d > 0 {
				out = append(out, flatten(vo.elems, d-1)...)
			} else {
				out = append(out, v)
			}
		}
		return out
	}
	return i.newArray(flatten(o.elems, depth)), nil
}

// arrayKeys/Values/Entries return array iterators.
func (i *Interpreter) arrayKeys(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	idx := 0
	return i.newIteratorProto(i.arrayIteratorProto, "Array Iterator", func() (Value, bool) {
		if idx >= len(o.elems) {
			return Undef, false
		}
		k := Number(float64(idx))
		idx++
		return k, true
	}), nil
}

func (i *Interpreter) arrayValues(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	idx := 0
	return i.newIteratorProto(i.arrayIteratorProto, "Array Iterator", func() (Value, bool) {
		if idx >= len(o.elems) {
			return Undef, false
		}
		// The array iterator reads 0..len via [[Get]], densifying holes.
		v := elemAt(o, idx)
		idx++
		return v, true
	}), nil
}

func (i *Interpreter) arrayEntries(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	idx := 0
	return i.newIteratorProto(i.arrayIteratorProto, "Array Iterator", func() (Value, bool) {
		if idx >= len(o.elems) {
			return Undef, false
		}
		pair := i.newArray([]Value{Number(float64(idx)), elemAt(o, idx)})
		idx++
		return pair, true
	}), nil
}

// ---------------------------------------------------------------------------
// Index helpers
// ---------------------------------------------------------------------------

// relIndex resolves a possibly negative index against length n, clamping into
// [0, n].
func relIndex(idx, n int) int {
	if idx < 0 {
		idx += n
	}
	if idx < 0 {
		return 0
	}
	if idx > n {
		return n
	}
	return idx
}

// argIntOr converts args[n] to an int, or returns def when absent/undefined.
func argIntOr(ctx context.Context, i *Interpreter, args []Value, n, def int) int {
	if IsUndefined(arg(args, n)) {
		return def
	}
	v, err := i.argInt(ctx, args, n)
	if err != nil {
		return def
	}
	return v
}
