package interp

import (
	"context"
	"strconv"
)

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

	ctor := i.newNativeCtor("Array", 1, i.arrayConstruct, i.arrayConstructNewTarget)
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
			// gojs backs arrays densely (length == len(elems)), so a valid-but-huge
			// length like new Array(2**32-1) would eagerly allocate billions of
			// holes and exhaust host memory — a DoS from a one-line untrusted
			// script. Refuse lengths past the dense backing limit rather than OOM;
			// spec-correct sparse arrays of that size are a known unsupported case.
			if length > maxDenseArrayLen {
				return nil, i.throwError(ctx, "RangeError", "array length exceeds gojs dense-array limit")
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

// arrayConstructNewTarget is the Array [[Construct]] entry point. It builds the
// array like the plain constructor, then adopts the prototype derived from
// new.target (§23.1.1.1 / GetPrototypeFromConstructor), so
// Reflect.construct(Array, [], NewTarget) and `class X extends Array` produce an
// instance with the subclass prototype.
func (i *Interpreter) arrayConstructNewTarget(ctx context.Context, newTarget Value, args []Value) (Value, error) {
	arrV, err := i.arrayConstruct(ctx, newTarget, args)
	if err != nil {
		return nil, err
	}
	arr := arrV.(*Object)
	proto, err := i.protoFromConstructor(ctx, newTarget, i.arrayProto)
	if err != nil {
		return nil, err
	}
	arr.SetProto(proto)
	return arr, nil
}

// protoFromConstructor implements GetPrototypeFromConstructor (§10.1.13): it
// reads new.target's "prototype" via [[Get]] (honoring a Proxy new.target),
// falling back to the supplied intrinsic when it is not an object.
func (i *Interpreter) protoFromConstructor(ctx context.Context, newTarget Value, fallback *Object) (*Object, error) {
	nt, ok := newTarget.(*Object)
	if !ok {
		return fallback, nil
	}
	protoV, err := nt.GetStr(ctx, "prototype")
	if err != nil {
		return nil, err
	}
	if proto, ok := protoV.(*Object); ok {
		return proto, nil
	}
	return fallback, nil
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

// arraySlice implements Array.prototype.slice (§23.1.3.25) generically: it
// coerces `this` with ToObject, resolves the relative start/end against
// LengthOfArrayLike, allocates the result via ArraySpeciesCreate, and copies only
// present indices (via [[HasProperty]]/[[Get]]) so holes stay holes.
func (i *Interpreter) arraySlice(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	start, err := i.relativeIndex(ctx, arg(args, 0), length, 0)
	if err != nil {
		return nil, err
	}
	end, err := i.relativeIndex(ctx, arg(args, 1), length, length)
	if err != nil {
		return nil, err
	}
	count := end - start
	if count < 0 {
		count = 0
	}
	a, err := i.arraySpeciesCreate(ctx, o, count)
	if err != nil {
		return nil, err
	}
	n := 0
	for k := start; k < end; k++ {
		kKey := StrKey(strconv.Itoa(k))
		present, err := i.hasV(ctx, o, kKey)
		if err != nil {
			return nil, err
		}
		if present {
			v, err := i.getV(ctx, o, kKey, o)
			if err != nil {
				return nil, err
			}
			if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(n)), v); err != nil {
				return nil, err
			}
		}
		n++
	}
	ok, err := i.setV(ctx, a, StrKey("length"), Number(float64(count)), a)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot set length of slice result")
	}
	return a, nil
}

// arraySplice implements Array.prototype.splice (§23.1.3.29) generically over an
// array-like `this`: it computes the actual start/delete-count against
// LengthOfArrayLike, returns the removed elements in an ArraySpeciesCreate array
// (preserving holes), then shifts the surviving elements and inserts the new
// items through [[Get]]/[[Set]]/[[Delete]], updating "length" at the end.
func (i *Interpreter) arraySplice(ctx context.Context, this Value, args []Value) (Value, error) {
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
	var insertCount, actualDeleteCount int
	switch {
	case len(args) == 0:
		insertCount, actualDeleteCount = 0, 0
	case len(args) == 1:
		insertCount, actualDeleteCount = 0, length-actualStart
	default:
		insertCount = len(args) - 2
		dc, err := i.argInteger(ctx, args, 1, 0)
		if err != nil {
			return nil, err
		}
		actualDeleteCount = clampIndexF(dc, length-actualStart)
	}
	const maxSafe = float64(1<<53 - 1)
	if float64(length)+float64(insertCount)-float64(actualDeleteCount) > maxSafe {
		return nil, i.throwError(ctx, "TypeError", "Array length exceeds 2^53-1")
	}
	a, err := i.arraySpeciesCreate(ctx, o, actualDeleteCount)
	if err != nil {
		return nil, err
	}
	for k := 0; k < actualDeleteCount; k++ {
		from := StrKey(strconv.Itoa(actualStart + k))
		present, err := i.hasV(ctx, o, from)
		if err != nil {
			return nil, err
		}
		if present {
			v, err := i.getV(ctx, o, from, o)
			if err != nil {
				return nil, err
			}
			if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(k)), v); err != nil {
				return nil, err
			}
		}
	}
	if ok, err := i.setV(ctx, a, StrKey("length"), Number(float64(actualDeleteCount)), a); err != nil {
		return nil, err
	} else if !ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot set length of splice result")
	}
	var items []Value
	if len(args) > 2 {
		items = args[2:]
	}
	itemCount := len(items)
	switch {
	case itemCount < actualDeleteCount:
		for k := actualStart; k < length-actualDeleteCount; k++ {
			from := StrKey(strconv.Itoa(k + actualDeleteCount))
			to := StrKey(strconv.Itoa(k + itemCount))
			if err := i.spliceMove(ctx, o, from, to); err != nil {
				return nil, err
			}
		}
		for k := length; k > length-actualDeleteCount+itemCount; k-- {
			if err := i.deletePropertyOrThrow(ctx, o, StrKey(strconv.Itoa(k-1))); err != nil {
				return nil, err
			}
		}
	case itemCount > actualDeleteCount:
		for k := length - actualDeleteCount; k > actualStart; k-- {
			from := StrKey(strconv.Itoa(k + actualDeleteCount - 1))
			to := StrKey(strconv.Itoa(k + itemCount - 1))
			if err := i.spliceMove(ctx, o, from, to); err != nil {
				return nil, err
			}
		}
	}
	k := actualStart
	for _, e := range items {
		if ok, err := i.setV(ctx, o, StrKey(strconv.Itoa(k)), e, o); err != nil {
			return nil, err
		} else if !ok {
			return nil, i.throwError(ctx, "TypeError", "Cannot set property '"+strconv.Itoa(k)+"'")
		}
		k++
	}
	newLen := length - actualDeleteCount + itemCount
	if ok, err := i.setV(ctx, o, StrKey("length"), Number(float64(newLen)), o); err != nil {
		return nil, err
	} else if !ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot set length")
	}
	return a, nil
}

// spliceMove copies present index `from` to `to` via [[Get]]/[[Set]], or deletes
// `to` when `from` is a hole (the element shift performed by splice).
func (i *Interpreter) spliceMove(ctx context.Context, o *Object, from, to PropertyKey) error {
	present, err := i.hasV(ctx, o, from)
	if err != nil {
		return err
	}
	if present {
		v, err := i.getV(ctx, o, from, o)
		if err != nil {
			return err
		}
		if ok, err := i.setV(ctx, o, to, v, o); err != nil {
			return err
		} else if !ok {
			return i.throwError(ctx, "TypeError", "Cannot set property '"+to.Str+"'")
		}
		return nil
	}
	return i.deletePropertyOrThrow(ctx, o, to)
}

// deletePropertyOrThrow implements DeletePropertyOrThrow (§7.3.9).
func (i *Interpreter) deletePropertyOrThrow(ctx context.Context, o *Object, key PropertyKey) error {
	ok, err := i.deleteV(ctx, o, key)
	if err != nil {
		return err
	}
	if !ok {
		return i.throwError(ctx, "TypeError", "Cannot delete property '"+key.Str+"'")
	}
	return nil
}

// arrayConcat implements Array.prototype.concat (§23.1.3.1) generically: it
// coerces `this` with ToObject, allocates the result via ArraySpeciesCreate, and
// spreads each argument whose IsConcatSpreadable is true through
// [[Get]]/[[HasProperty]] (preserving holes and surfacing poisoned getters and
// Proxy traps) while appending non-spreadable items directly.
func (i *Interpreter) arrayConcat(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	a, err := i.arraySpeciesCreate(ctx, o, 0)
	if err != nil {
		return nil, err
	}
	const maxSafe = float64(1<<53 - 1)
	n := 0
	items := append([]Value{o}, args...)
	for _, e := range items {
		spreadable, err := i.isConcatSpreadable(ctx, e)
		if err != nil {
			return nil, err
		}
		if spreadable {
			eo := e.(*Object)
			length, err := i.lengthOfArrayLike(ctx, eo)
			if err != nil {
				return nil, err
			}
			if float64(n)+float64(length) > maxSafe {
				return nil, i.throwError(ctx, "TypeError", "Array length exceeds 2^53-1")
			}
			for k := 0; k < length; k++ {
				kKey := StrKey(strconv.Itoa(k))
				present, err := i.hasV(ctx, eo, kKey)
				if err != nil {
					return nil, err
				}
				if present {
					sub, err := i.getV(ctx, eo, kKey, eo)
					if err != nil {
						return nil, err
					}
					if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(n)), sub); err != nil {
						return nil, err
					}
				}
				n++
			}
		} else {
			if float64(n) >= maxSafe {
				return nil, i.throwError(ctx, "TypeError", "Array length exceeds 2^53-1")
			}
			if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(n)), e); err != nil {
				return nil, err
			}
			n++
		}
	}
	// Set(A, "length", n, true).
	ok, err := i.setV(ctx, a, StrKey("length"), Number(float64(n)), a)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot set length of concat result")
	}
	return a, nil
}

// isArrayV implements IsArray (§7.2.2): true for an Array exotic object,
// recursing through a Proxy to its target and throwing on a revoked proxy.
func (i *Interpreter) isArrayV(ctx context.Context, v Value) (bool, error) {
	o, ok := v.(*Object)
	if !ok {
		return false, nil
	}
	for o.proxy != nil {
		if o.proxy.revoked() || o.proxy.target == nil {
			return false, i.throwError(ctx, "TypeError", "Cannot perform IsArray on a revoked Proxy")
		}
		o = o.proxy.target
	}
	return o.isArray, nil
}

// isConcatSpreadable implements IsConcatSpreadable (§23.1.3.1.1): consult
// @@isConcatSpreadable when present, otherwise fall back to IsArray.
func (i *Interpreter) isConcatSpreadable(ctx context.Context, v Value) (bool, error) {
	o, ok := v.(*Object)
	if !ok {
		return false, nil
	}
	spreadable, err := o.Get(ctx, SymKey(i.symIsConcatSpreadable))
	if err != nil {
		return false, err
	}
	if !IsUndefined(spreadable) {
		return ToBoolean(spreadable), nil
	}
	return i.isArrayV(ctx, o)
}

// arraySpeciesCreate implements ArraySpeciesCreate (§23.1.3.2.3): allocate a new
// array of the given length, honoring the original array's constructor @@species.
func (i *Interpreter) arraySpeciesCreate(ctx context.Context, original *Object, length int) (*Object, error) {
	isArr, err := i.isArrayV(ctx, original)
	if err != nil {
		return nil, err
	}
	if !isArr {
		return i.arrayCreate(ctx, length)
	}
	c, err := original.GetStr(ctx, "constructor")
	if err != nil {
		return nil, err
	}
	if co, ok := c.(*Object); ok {
		sv, err := co.Get(ctx, SymKey(i.symSpecies))
		if err != nil {
			return nil, err
		}
		if IsNullish(sv) {
			c = Undef
		} else {
			c = sv
		}
	}
	if IsUndefined(c) {
		return i.arrayCreate(ctx, length)
	}
	co, ok := c.(*Object)
	if !ok || !co.IsConstructor() {
		return nil, i.throwError(ctx, "TypeError", "Array species is not a constructor")
	}
	res, err := co.fn.construct(ctx, co, []Value{Number(float64(length))})
	if err != nil {
		return nil, err
	}
	ro, ok := res.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Array species constructor did not return an object")
	}
	return ro, nil
}

// arrayCreate implements ArrayCreate(length): a fresh array of `length` holes.
// A length above 2^32-1 is a RangeError per spec; gojs additionally rejects a
// length past its dense backing limit (which it cannot allocate) with the same
// RangeError rather than exhausting memory.
func (i *Interpreter) arrayCreate(ctx context.Context, n int) (*Object, error) {
	if n < 0 || n > maxDenseArrayLen {
		return nil, i.throwError(ctx, "RangeError", "Invalid array length")
	}
	arr := i.newArray(nil)
	arr.ensureLen(n)
	return arr, nil
}

// createDataPropertyOrThrow implements CreateDataPropertyOrThrow (§7.3.7) via
// [[DefineOwnProperty]], so it honors a Proxy target and array-index storage.
func (i *Interpreter) createDataPropertyOrThrow(ctx context.Context, o *Object, key PropertyKey, v Value) error {
	ok, err := i.definePropertyV(ctx, o, key, i.dataDescriptorObject(v))
	if err != nil {
		return err
	}
	if !ok {
		return i.throwError(ctx, "TypeError", "Cannot create property")
	}
	return nil
}

// dataDescriptorObject builds a {value, writable, enumerable, configurable:true}
// descriptor object for CreateDataProperty.
func (i *Interpreter) dataDescriptorObject(v Value) *Object {
	d := NewObject(i.objectProto)
	d.SetData("value", v)
	d.SetData("writable", Bool(true))
	d.SetData("enumerable", Bool(true))
	d.SetData("configurable", Bool(true))
	return d
}

func (i *Interpreter) arrayJoin(ctx context.Context, this Value, args []Value) (Value, error) {
	// Array.prototype.join (§23.1.3.18) is generic: ToObject + LengthOfArrayLike,
	// then each index is read via [[Get]] and coerced with ToString unless it is
	// null or undefined (which render as the empty string).
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
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
	for k := 0; k < length; k++ {
		if k > 0 {
			out += sep
		}
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(k)), o)
		if err != nil {
			return nil, err
		}
		if IsNullish(v) {
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
	// Array.prototype.indexOf (§23.1.3.14) is generic: ToObject, then length is
	// read (and coerced) BEFORE ToIntegerOrInfinity(fromIndex), and present
	// indices are visited via HasProperty/[[Get]] using strict equality.
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return Number(-1), nil
	}
	target := arg(args, 0)
	nf := 0.0
	if !IsUndefined(arg(args, 1)) {
		f, err := i.argNum(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		nf = ToInteger(f)
	}
	// n == +Infinity (or beyond length) means the search cannot find anything.
	if nf >= float64(length) {
		return Number(-1), nil
	}
	var k int
	switch {
	case nf >= 0:
		k = int(nf)
	case -nf >= float64(length): // n == -Infinity, or clamps below 0
		k = 0
	default:
		k = length + int(nf)
	}
	for ; k < length; k++ {
		key := StrKey(strconv.Itoa(k))
		present, err := i.hasV(ctx, o, key)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		v, err := i.getV(ctx, o, key, o)
		if err != nil {
			return nil, err
		}
		if strictEquals(v, target) {
			return Number(float64(k)), nil
		}
	}
	return Number(-1), nil
}

func (i *Interpreter) arrayIncludes(ctx context.Context, this Value, args []Value) (Value, error) {
	// Array.prototype.includes (§23.1.3.13) is generic and, unlike indexOf, uses
	// SameValueZero (so it finds NaN) and does NOT skip holes: an absent index is
	// read via [[Get]] and treated as undefined.
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return False, nil
	}
	target := arg(args, 0)
	nf := 0.0
	if !IsUndefined(arg(args, 1)) {
		f, err := i.argNum(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		nf = ToInteger(f)
	}
	if nf >= float64(length) {
		return False, nil
	}
	var k int
	switch {
	case nf >= 0:
		k = int(nf)
	case -nf >= float64(length):
		k = 0
	default:
		k = length + int(nf)
	}
	for ; k < length; k++ {
		v, err := i.getV(ctx, o, StrKey(strconv.Itoa(k)), o)
		if err != nil {
			return nil, err
		}
		if sameValueZero(v, target) {
			return True, nil
		}
	}
	return False, nil
}

// eachElem is the generic iteration core shared by the callback-visiting Array
// methods (forEach, map, filter, some, every, find, findIndex, flatMap). It is
// spec-faithful: the receiver has already been coerced with ToObject and its
// length read with LengthOfArrayLike, so it works over any array-like object,
// reading elements through [[Get]] and probing holes through [[HasProperty]].
//
// The callback (cb) is validated as callable here — a missing or non-callable
// callback throws a TypeError before any element is visited. length is captured
// once by the caller (per spec), so elements the callback appends are not
// visited and a callback that shrinks the array reads holes, not out of bounds.
//
// When skipHoles is true (the HasProperty-family methods: forEach, map, filter,
// some, every, flatMap) an absent index is not visited at all. When false (the
// Get-from-0..len methods: find, findIndex) every index is read via [[Get]], so
// a hole surfaces as undefined (or an inherited value from the prototype chain).
// fn receives the read value v alongside the callback's result res.
func (i *Interpreter) eachElem(ctx context.Context, o *Object, length int, cb Value, thisArg Value, skipHoles bool, fn func(idx int, v Value, res Value) (bool, error)) error {
	callback, ok := cb.(*Object)
	if !ok || !callback.IsCallable() {
		return i.throwError(ctx, "TypeError", briefValue(cb)+" is not a function")
	}
	for j := 0; j < length; j++ {
		key := StrKey(strconv.Itoa(j))
		if skipHoles {
			present, err := i.hasV(ctx, o, key)
			if err != nil {
				return err
			}
			if !present {
				continue
			}
		}
		v, err := i.getV(ctx, o, key, o)
		if err != nil {
			return err
		}
		res, err := callback.fn.call(ctx, thisArg, []Value{v, Number(float64(j)), o})
		if err != nil {
			return err
		}
		if stop, err := fn(j, v, res); err != nil || stop {
			return err
		}
	}
	return nil
}

func (i *Interpreter) arrayForEach(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	err = i.eachElem(ctx, o, length, arg(args, 0), arg(args, 1), true, func(int, Value, Value) (bool, error) { return false, nil })
	return Undef, err
}

func (i *Interpreter) arrayMap(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	// The callback must be callable before ArraySpeciesCreate runs (§23.1.3.19).
	cb := arg(args, 0)
	if callback, ok := cb.(*Object); !ok || !callback.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", briefValue(cb)+" is not a function")
	}
	a, err := i.arraySpeciesCreate(ctx, o, length)
	if err != nil {
		return nil, err
	}
	// map preserves hole positions: only the indices the callback actually
	// visits get a data property, leaving the rest of the result array absent.
	err = i.eachElem(ctx, o, length, cb, arg(args, 1), true, func(idx int, v Value, res Value) (bool, error) {
		return false, i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(idx)), res)
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (i *Interpreter) arrayFilter(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	// The callback must be callable before ArraySpeciesCreate runs (§23.1.3.7).
	cb := arg(args, 0)
	if callback, ok := cb.(*Object); !ok || !callback.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", briefValue(cb)+" is not a function")
	}
	a, err := i.arraySpeciesCreate(ctx, o, 0)
	if err != nil {
		return nil, err
	}
	n := 0
	err = i.eachElem(ctx, o, length, cb, arg(args, 1), true, func(idx int, v Value, res Value) (bool, error) {
		if ToBoolean(res) {
			if err := i.createDataPropertyOrThrow(ctx, a, StrKey(strconv.Itoa(n)), v); err != nil {
				return false, err
			}
			n++
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (i *Interpreter) arrayFind(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	result := Value(Undef)
	// find does not skip holes; every index is read via [[Get]].
	err = i.eachElem(ctx, o, length, arg(args, 0), arg(args, 1), false, func(idx int, v Value, res Value) (bool, error) {
		if ToBoolean(res) {
			result = v
			return true, nil
		}
		return false, nil
	})
	return result, err
}

func (i *Interpreter) arrayFindIndex(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	result := Number(-1)
	// findIndex does not skip holes; every index is read via [[Get]].
	err = i.eachElem(ctx, o, length, arg(args, 0), arg(args, 1), false, func(idx int, v Value, res Value) (bool, error) {
		if ToBoolean(res) {
			result = Number(float64(idx))
			return true, nil
		}
		return false, nil
	})
	return result, err
}

func (i *Interpreter) arraySome(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	found := false
	err = i.eachElem(ctx, o, length, arg(args, 0), arg(args, 1), true, func(_ int, v Value, res Value) (bool, error) {
		if ToBoolean(res) {
			found = true
			return true, nil
		}
		return false, nil
	})
	return Bool(found), err
}

func (i *Interpreter) arrayEvery(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	all := true
	err = i.eachElem(ctx, o, length, arg(args, 0), arg(args, 1), true, func(_ int, v Value, res Value) (bool, error) {
		if !ToBoolean(res) {
			all = false
			return true, nil
		}
		return false, nil
	})
	return Bool(all), err
}

func (i *Interpreter) arrayReduce(ctx context.Context, this Value, args []Value) (Value, error) {
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
	// reduce skips holes: they seed neither the accumulator nor a callback call.
	var acc Value
	haveAcc := len(args) >= 2
	if haveAcc {
		acc = args[1]
	}
	k := 0
	if !haveAcc {
		// Seed the accumulator from the first present element (§23.1.3.24 step 8).
		for ; k < length; k++ {
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
				k++
				break
			}
		}
		if !haveAcc {
			return nil, i.throwError(ctx, "TypeError", "Reduce of empty array with no initial value")
		}
	}
	for ; k < length; k++ {
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

// arrayReverse implements Array.prototype.reverse (§23.1.3.26) generically: it
// reverses an array-like `this` in place through [[HasProperty]]/[[Get]]/[[Set]]/
// [[Delete]], preserving hole positions in the mirror image.
func (i *Interpreter) arrayReverse(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	middle := length / 2
	for lower := 0; lower != middle; lower++ {
		upper := length - lower - 1
		lowerP := StrKey(strconv.Itoa(lower))
		upperP := StrKey(strconv.Itoa(upper))
		lowerExists, err := i.hasV(ctx, o, lowerP)
		if err != nil {
			return nil, err
		}
		var lowerValue Value
		if lowerExists {
			if lowerValue, err = i.getV(ctx, o, lowerP, o); err != nil {
				return nil, err
			}
		}
		upperExists, err := i.hasV(ctx, o, upperP)
		if err != nil {
			return nil, err
		}
		var upperValue Value
		if upperExists {
			if upperValue, err = i.getV(ctx, o, upperP, o); err != nil {
				return nil, err
			}
		}
		switch {
		case lowerExists && upperExists:
			if err := i.reverseSet(ctx, o, lowerP, upperValue); err != nil {
				return nil, err
			}
			if err := i.reverseSet(ctx, o, upperP, lowerValue); err != nil {
				return nil, err
			}
		case upperExists:
			if err := i.reverseSet(ctx, o, lowerP, upperValue); err != nil {
				return nil, err
			}
			if err := i.deletePropertyOrThrow(ctx, o, upperP); err != nil {
				return nil, err
			}
		case lowerExists:
			if err := i.deletePropertyOrThrow(ctx, o, lowerP); err != nil {
				return nil, err
			}
			if err := i.reverseSet(ctx, o, upperP, lowerValue); err != nil {
				return nil, err
			}
		}
	}
	return o, nil
}

// reverseSet performs Set(O, key, v, true), throwing on a failed write.
func (i *Interpreter) reverseSet(ctx context.Context, o *Object, key PropertyKey, v Value) error {
	ok, err := i.setV(ctx, o, key, v, o)
	if err != nil {
		return err
	}
	if !ok {
		return i.throwError(ctx, "TypeError", "Cannot set property '"+key.Str+"'")
	}
	return nil
}

// arrayFill implements Array.prototype.fill (§23.1.3.7) generically: it resolves
// the relative start/end against LengthOfArrayLike and writes the value into each
// index in range through [[Set]] (throwing on a failed write).
func (i *Interpreter) arrayFill(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	length, err := i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return nil, err
	}
	val := arg(args, 0)
	start, err := i.relativeIndex(ctx, arg(args, 1), length, 0)
	if err != nil {
		return nil, err
	}
	end, err := i.relativeIndex(ctx, arg(args, 2), length, length)
	if err != nil {
		return nil, err
	}
	for k := start; k < end; k++ {
		if err := i.reverseSet(ctx, o, StrKey(strconv.Itoa(k)), val); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (i *Interpreter) arrayFlat(ctx context.Context, this Value, args []Value) (Value, error) {
	// Array.prototype.flat (§23.1.3.11): ToObject, LengthOfArrayLike, then
	// ArraySpeciesCreate and FlattenIntoArray, which is fully generic — it walks
	// the source via HasProperty/[[Get]] and recurses into elements that IsArray,
	// so the observable "length","constructor","0",... access sequence is exact.
	o, err := i.ToObject(ctx, this)
	if err != nil {
		return nil, err
	}
	sourceLen, err := i.lengthOfArrayLike(ctx, o)
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
	a, err := i.arraySpeciesCreate(ctx, o, 0)
	if err != nil {
		return nil, err
	}
	if _, err := i.flattenIntoArray(ctx, a, o, sourceLen, 0, depth); err != nil {
		return nil, err
	}
	return a, nil
}

// flattenIntoArray implements FlattenIntoArray (§23.1.3.11.1) for flat: it copies
// present source elements into target starting at start, recursing one level
// deeper (down to depth 0) for each element that IsArray. It returns the next
// free target index.
func (i *Interpreter) flattenIntoArray(ctx context.Context, target, source *Object, sourceLen, start, depth int) (int, error) {
	const maxSafe = float64(1<<53 - 1)
	targetIndex := start
	for k := 0; k < sourceLen; k++ {
		key := StrKey(strconv.Itoa(k))
		present, err := i.hasV(ctx, source, key)
		if err != nil {
			return 0, err
		}
		if !present {
			continue
		}
		element, err := i.getV(ctx, source, key, source)
		if err != nil {
			return 0, err
		}
		shouldFlatten := false
		if depth > 0 {
			shouldFlatten, err = i.isArrayV(ctx, element)
			if err != nil {
				return 0, err
			}
		}
		if shouldFlatten {
			eo := element.(*Object)
			elementLen, err := i.lengthOfArrayLike(ctx, eo)
			if err != nil {
				return 0, err
			}
			targetIndex, err = i.flattenIntoArray(ctx, target, eo, elementLen, targetIndex, depth-1)
			if err != nil {
				return 0, err
			}
		} else {
			if float64(targetIndex) >= maxSafe {
				return 0, i.throwError(ctx, "TypeError", "Array length exceeds 2^53-1")
			}
			if err := i.createDataPropertyOrThrow(ctx, target, StrKey(strconv.Itoa(targetIndex)), element); err != nil {
				return 0, err
			}
			targetIndex++
		}
	}
	return targetIndex, nil
}

// arrayKeys/Values/Entries return array iterators (keys, values, entries).
// arrayIterKind selects what an array iterator yields per step.
type arrayIterKind int

const (
	arrayIterKeys arrayIterKind = iota
	arrayIterValues
	arrayIterEntries
)

func (i *Interpreter) arrayKeys(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.newArrayIterator(ctx, this, arrayIterKeys)
}

func (i *Interpreter) arrayValues(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.newArrayIterator(ctx, this, arrayIterValues)
}

func (i *Interpreter) arrayEntries(ctx context.Context, this Value, args []Value) (Value, error) {
	return i.newArrayIterator(ctx, this, arrayIterEntries)
}

// newArrayIterator builds a CreateArrayIterator (§23.1.5) result. A plain dense
// array steps directly over its backing store; any other array-like receiver
// (notably a Proxy over an array) reads its length and elements via [[Get]] so
// the exotic behavior is honored.
func (i *Interpreter) newArrayIterator(ctx context.Context, this Value, kind arrayIterKind) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	if o.isArray && o.proxy == nil {
		idx := 0
		done := false
		return i.newIteratorProto(i.arrayIteratorProto, "Array Iterator", func() (Value, bool) {
			// Once the index reaches the length the iterator drops its target and
			// stays done (§23.1.5.1), so elements appended afterward are not
			// revisited even though the dense backing may have grown.
			if done || idx >= len(o.elems) {
				done = true
				return Undef, false
			}
			cur := idx
			idx++
			switch kind {
			case arrayIterKeys:
				return Number(float64(cur)), true
			case arrayIterEntries:
				return i.newArray([]Value{Number(float64(cur)), elemAt(o, cur)}), true
			default:
				return elemAt(o, cur), true
			}
		}), nil
	}
	// Generic array-like path: length and elements are obtained through [[Get]]
	// (LengthOfArrayLike), and errors from an exotic receiver propagate.
	it := NewObject(i.arrayIteratorProto)
	it.class = "Array Iterator"
	idx := 0
	done := false
	i.defineMethod(it, "next", 0, func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		res := NewObject(i.objectProto)
		if !done {
			length, err := i.lengthOfArrayLike(ctx, o)
			if err != nil {
				return nil, err
			}
			if idx < length {
				cur := idx
				idx++
				var val Value
				switch kind {
				case arrayIterKeys:
					val = Number(float64(cur))
				case arrayIterEntries:
					ev, err := i.getV(ctx, o, StrKey(intToStr(cur)), o)
					if err != nil {
						return nil, err
					}
					val = i.newArray([]Value{Number(float64(cur)), ev})
				default:
					v, err := i.getV(ctx, o, StrKey(intToStr(cur)), o)
					if err != nil {
						return nil, err
					}
					val = v
				}
				res.SetData("value", val)
				res.SetData("done", False)
				return res, nil
			}
			done = true
		}
		res.SetData("value", Undef)
		res.SetData("done", True)
		return res, nil
	})
	it.defineOwn(SymKey(i.symIterator), &Property{
		Value:        i.newNativeFunc("[Symbol.iterator]", 0, func(ctx context.Context, this Value, args []Value) (Value, error) { return this, nil }),
		Writable:     true,
		Configurable: true,
	})
	return it, nil
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
