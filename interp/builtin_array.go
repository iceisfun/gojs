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
		err := i.iterate(ctx, arg(args, 0), func(v Value) error {
			if mapFn != nil && mapFn.IsCallable() {
				mv, err := mapFn.fn.call(ctx, Undef, []Value{v, Number(float64(idx))})
				if err != nil {
					return err
				}
				v = mv
			}
			out = append(out, v)
			idx++
			return nil
		})
		if err != nil {
			return nil, err
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
		{"find", 1, i.arrayFind},
		{"findIndex", 1, i.arrayFindIndex},
		{"some", 1, i.arraySome},
		{"every", 1, i.arrayEvery},
		{"reverse", 0, i.arrayReverse},
		{"fill", 1, i.arrayFill},
		{"flat", 0, i.arrayFlat},
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
			elems := make([]Value, length)
			for j := range elems {
				elems[j] = Undef
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
	return v, nil
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
	return v, nil
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
	return i.arrayJoin(ctx, this, nil)
}

func (i *Interpreter) arrayIndexOf(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	target := arg(args, 0)
	for j, v := range o.elems {
		if strictEquals(v, target) {
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
	for _, v := range o.elems {
		if sameValueZero(v, target) {
			return True, nil
		}
	}
	return False, nil
}

// iterCallback runs fn(this=thisArg, [element, index, array]) for each element.
func (i *Interpreter) eachElem(ctx context.Context, o *Object, cb Value, thisArg Value, fn func(idx int, res Value) (bool, error)) error {
	callback, ok := cb.(*Object)
	if !ok || !callback.IsCallable() {
		return i.throwError(ctx, "TypeError", briefValue(cb)+" is not a function")
	}
	for j := 0; j < len(o.elems); j++ {
		res, err := callback.fn.call(ctx, thisArg, []Value{o.elems[j], Number(float64(j)), o})
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
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(int, Value) (bool, error) { return false, nil })
	return Undef, err
}

func (i *Interpreter) arrayMap(ctx context.Context, this Value, args []Value) (Value, error) {
	o, err := i.thisArray(ctx, this)
	if err != nil {
		return nil, err
	}
	out := make([]Value, 0, len(o.elems))
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(_ int, res Value) (bool, error) {
		out = append(out, res)
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
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(idx int, res Value) (bool, error) {
		if ToBoolean(res) {
			out = append(out, o.elems[idx])
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
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(idx int, res Value) (bool, error) {
		if ToBoolean(res) {
			result = o.elems[idx]
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
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(idx int, res Value) (bool, error) {
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
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(_ int, res Value) (bool, error) {
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
	err = i.eachElem(ctx, o, arg(args, 0), arg(args, 1), func(_ int, res Value) (bool, error) {
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
	var acc Value
	start := 0
	if len(args) >= 2 {
		acc = args[1]
	} else {
		if len(o.elems) == 0 {
			return nil, i.throwError(ctx, "TypeError", "Reduce of empty array with no initial value")
		}
		acc = o.elems[0]
		start = 1
	}
	for j := start; j < len(o.elems); j++ {
		acc, err = callback.fn.call(ctx, Undef, []Value{acc, o.elems[j], Number(float64(j)), o})
		if err != nil {
			return nil, err
		}
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
		depth = argIntOr(ctx, i, args, 0, 1)
	}
	var flatten func(elems []Value, d int) []Value
	flatten = func(elems []Value, d int) []Value {
		var out []Value
		for _, v := range elems {
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
	return i.newIterator(func() (Value, bool) {
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
	return i.newIterator(func() (Value, bool) {
		if idx >= len(o.elems) {
			return Undef, false
		}
		v := o.elems[idx]
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
	return i.newIterator(func() (Value, bool) {
		if idx >= len(o.elems) {
			return Undef, false
		}
		pair := i.newArray([]Value{Number(float64(idx)), o.elems[idx]})
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
