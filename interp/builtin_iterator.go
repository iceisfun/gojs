package interp

import (
	"context"
	"math"
)

// This file implements the ES2025 Iterator Helpers feature: the %Iterator%
// constructor, %Iterator.prototype% and its lazy helper methods (map, filter,
// take, drop, flatMap, reduce, toArray, forEach, some, every, find), the
// %IteratorHelperPrototype% returned by the lazy helpers, Iterator.from and the
// %WrapForValidIteratorPrototype% it produces. It also installs a
// String.prototype[@@iterator] so strings participate in the iterator protocol
// (needed by Iterator.from and generally correct).
//
// The lazy helpers are modelled as pull-based state machines rather than the
// spec's CreateIteratorFromClosure generators: each helper object holds an
// iterHelperState whose pull closure produces the next value (or reports done),
// mirroring the observable Yield/IfAbruptCloseIterator semantics.

// ---------------------------------------------------------------------------
// Iterator Records and the abstract operations over them
// ---------------------------------------------------------------------------

// iterRecord mirrors the spec's Iterator Record { [[Iterator]], [[NextMethod]],
// [[Done]] }.
type iterRecord struct {
	iterator   *Object
	nextMethod Value
	done       bool
}

// getMethodStr implements GetMethod (§7.3.11) for a string key: Get(V, P);
// undefined/null → nil; otherwise it must be callable.
func (i *Interpreter) getMethodStr(ctx context.Context, o *Object, name string) (*Object, error) {
	v, err := o.GetStr(ctx, name)
	if err != nil {
		return nil, err
	}
	if IsNullish(v) {
		return nil, nil
	}
	fo, ok := v.(*Object)
	if !ok || !fo.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", name+" is not a function")
	}
	return fo, nil
}

// getIteratorDirect implements GetIteratorDirect (§7.4.4).
func (i *Interpreter) getIteratorDirect(ctx context.Context, obj *Object) (*iterRecord, error) {
	next, err := obj.GetStr(ctx, "next")
	if err != nil {
		return nil, err
	}
	return &iterRecord{iterator: obj, nextMethod: next, done: false}, nil
}

// iteratorStep implements IteratorStep (§7.4.6): it advances the record and
// returns the result object, or done=true when the iterator is exhausted.
func (i *Interpreter) iteratorStep(ctx context.Context, rec *iterRecord) (*Object, bool, error) {
	nm, ok := rec.nextMethod.(*Object)
	if !ok || !nm.IsCallable() {
		rec.done = true
		return nil, false, i.throwError(ctx, "TypeError", "iterator.next is not a function")
	}
	resV, err := nm.fn.call(ctx, rec.iterator, nil)
	if err != nil {
		rec.done = true
		return nil, false, err
	}
	res, ok := resV.(*Object)
	if !ok {
		rec.done = true
		return nil, false, i.throwError(ctx, "TypeError", "iterator result is not an object")
	}
	doneV, err := res.GetStr(ctx, "done")
	if err != nil {
		rec.done = true
		return nil, false, err
	}
	if ToBoolean(doneV) {
		rec.done = true
		return nil, true, nil
	}
	return res, false, nil
}

// iteratorStepValue implements IteratorStepValue (§7.4.8).
func (i *Interpreter) iteratorStepValue(ctx context.Context, rec *iterRecord) (Value, bool, error) {
	res, done, err := i.iteratorStep(ctx, rec)
	if err != nil || done {
		return Undef, done, err
	}
	val, err := res.GetStr(ctx, "value")
	if err != nil {
		rec.done = true
		return Undef, false, err
	}
	return val, false, nil
}

// iteratorClose implements IteratorClose (§7.4.11). pending is the completion to
// preserve: a non-nil error means the surrounding completion is abrupt (a throw)
// and takes precedence over anything the return method does.
func (i *Interpreter) iteratorClose(ctx context.Context, rec *iterRecord, pending error) error {
	returnMethod, err := i.getMethodStr(ctx, rec.iterator, "return")
	if err != nil {
		// GetMethod threw. If the surrounding completion is a throw, it wins.
		if pending != nil {
			return pending
		}
		return err
	}
	if returnMethod == nil {
		return pending
	}
	res, cerr := returnMethod.fn.call(ctx, rec.iterator, nil)
	if pending != nil {
		return pending
	}
	if cerr != nil {
		return cerr
	}
	if _, ok := res.(*Object); !ok {
		return i.throwError(ctx, "TypeError", "iterator return method returned a non-object")
	}
	return nil
}

// getIteratorFlattenable implements GetIteratorFlattenable (§7.4.3).
func (i *Interpreter) getIteratorFlattenable(ctx context.Context, obj Value, stringPrimitives bool) (*iterRecord, error) {
	o, isObj := obj.(*Object)
	if !isObj {
		if !stringPrimitives {
			return nil, i.throwError(ctx, "TypeError", briefValue(obj)+" is not an object")
		}
		if _, isStr := obj.(String); !isStr {
			return nil, i.throwError(ctx, "TypeError", briefValue(obj)+" is not an object")
		}
		// Box the string so its @@iterator can be looked up on String.prototype.
		o = i.newStringObject(obj.(String))
	}
	method, err := i.getMethod(ctx, o, i.symIterator)
	if err != nil {
		return nil, err
	}
	var iterator Value
	if method == nil {
		iterator = o
	} else {
		it, err := method.fn.call(ctx, o, nil)
		if err != nil {
			return nil, err
		}
		iterator = it
	}
	itObj, ok := iterator.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "iterator is not an object")
	}
	return i.getIteratorDirect(ctx, itObj)
}

// ---------------------------------------------------------------------------
// Iterator Helper objects (%IteratorHelperPrototype%)
// ---------------------------------------------------------------------------

// iterHelperState is the internal state of an Iterator Helper object. pull
// produces the next value (or reports done); live lists the underlying iterator
// records that must be closed if the consumer calls return() before completion.
type iterHelperState struct {
	done    bool
	started bool
	pull    func(ctx context.Context) (Value, bool, error)
	live    []*iterRecord
}

// helperState extracts an Iterator Helper's state, performing the brand check
// that RequireInternalSlot(obj, [[UnderlyingIterators]]) would.
func helperState(this Value) *iterHelperState {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	st, _ := o.internal["iterHelper"].(*iterHelperState)
	return st
}

// newIterHelper builds an Iterator Helper object; setup wires its pull closure
// and initial live-iterator list onto the shared state.
func (i *Interpreter) newIterHelper(setup func(st *iterHelperState)) *Object {
	st := &iterHelperState{}
	setup(st)
	o := NewObject(i.iteratorHelperProto)
	o.class = "Iterator Helper"
	o.internal = map[string]any{"iterHelper": st}
	return o
}

func (i *Interpreter) iteratorHelperNext(ctx context.Context, this Value, _ []Value) (Value, error) {
	st := helperState(this)
	if st == nil {
		return nil, i.throwError(ctx, "TypeError", "next called on an incompatible receiver")
	}
	if st.done {
		return i.newIterResult(Undef, true), nil
	}
	st.started = true
	v, done, err := st.pull(ctx)
	if err != nil {
		st.done = true
		return nil, err
	}
	if done {
		st.done = true
		return i.newIterResult(Undef, true), nil
	}
	return i.newIterResult(v, false), nil
}

func (i *Interpreter) iteratorHelperReturn(ctx context.Context, this Value, _ []Value) (Value, error) {
	st := helperState(this)
	if st == nil {
		return nil, i.throwError(ctx, "TypeError", "return called on an incompatible receiver")
	}
	if st.done {
		return i.newIterResult(Undef, true), nil
	}
	st.done = true
	// IteratorCloseAll closes the underlying iterators in reverse List order.
	var pending error
	for k := len(st.live) - 1; k >= 0; k-- {
		pending = i.iteratorClose(ctx, st.live[k], pending)
	}
	if pending != nil {
		return nil, pending
	}
	return i.newIterResult(Undef, true), nil
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func (i *Interpreter) initIterator() {
	proto := i.iteratorProto

	// %IteratorHelperPrototype% — [[Prototype]] is %Iterator.prototype%.
	helperProto := NewObject(proto)
	i.iteratorHelperProto = helperProto
	i.defineMethod(helperProto, "next", 0, i.iteratorHelperNext)
	i.defineMethod(helperProto, "return", 0, i.iteratorHelperReturn)
	helperProto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value: String("Iterator Helper"), Writable: false, Enumerable: false, Configurable: true,
	})

	// %WrapForValidIteratorPrototype% — [[Prototype]] is %Iterator.prototype%.
	i.initWrapForValidIterator()

	// %StringIteratorPrototype% plus String.prototype[@@iterator].
	i.initStringIterator()

	// -----------------------------------------------------------------------
	// The Iterator constructor (abstract, subclassable). §27.1.3
	// -----------------------------------------------------------------------
	callFn := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		// Called without new: NewTarget is undefined → throw.
		return nil, i.throwError(ctx, "TypeError", "Abstract class Iterator not directly constructable")
	}
	constructFn := func(ctx context.Context, newTarget Value, _ []Value) (Value, error) {
		nt, ok := newTarget.(*Object)
		if !ok || nt == i.iteratorCtor {
			return nil, i.throwError(ctx, "TypeError", "Abstract class Iterator not directly constructable")
		}
		// OrdinaryCreateFromConstructor(newTarget, "%Iterator.prototype%").
		p := proto
		if pv, err := nt.GetStr(ctx, "prototype"); err == nil {
			if po, ok := pv.(*Object); ok {
				p = po
			}
		}
		return NewObject(p), nil
	}
	ctor := i.newNativeCtor("Iterator", 0, callFn, constructFn)
	i.iteratorCtor = ctor
	linkCtor(ctor, proto)

	// Iterator.from(obj). §27.1.4.1
	i.defineMethod(ctor, "from", 1, i.iteratorFrom)

	// -----------------------------------------------------------------------
	// %Iterator.prototype% accessors and helper methods.
	// -----------------------------------------------------------------------

	// constructor is (for web-compat) an accessor returning %Iterator%. §27.1.5.2
	ctorGet := i.newNativeFunc("get constructor", 0, func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return ctor, nil
	})
	ctorSet := i.newNativeFunc("set constructor", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Undef, i.setterIgnoringProto(ctx, this, proto, StrKey("constructor"), arg(args, 0))
	})
	// linkCtor installed a data "constructor"; replace it with the accessor.
	proto.defineOwn(StrKey("constructor"), &Property{
		Get: ctorGet, Set: ctorSet, Accessor: true, Enumerable: false, Configurable: true,
	})

	// [@@toStringTag] is (for web-compat) an accessor returning "Iterator". §27.1.5.15
	tagGet := i.newNativeFunc("get [Symbol.toStringTag]", 0, func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return String("Iterator"), nil
	})
	tagSet := i.newNativeFunc("set [Symbol.toStringTag]", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Undef, i.setterIgnoringProto(ctx, this, proto, SymKey(i.symToStringTag), arg(args, 0))
	})
	proto.defineOwn(SymKey(i.symToStringTag), &Property{
		Get: tagGet, Set: tagSet, Accessor: true, Enumerable: false, Configurable: true,
	})

	// [@@iterator]() returns this. §27.1.5.14
	i.defineSymbolMethod(proto, i.symIterator, "[Symbol.iterator]", 0,
		func(ctx context.Context, this Value, _ []Value) (Value, error) { return this, nil })

	i.defineMethod(proto, "map", 1, i.iteratorProtoMap)
	i.defineMethod(proto, "filter", 1, i.iteratorProtoFilter)
	i.defineMethod(proto, "take", 1, i.iteratorProtoTake)
	i.defineMethod(proto, "drop", 1, i.iteratorProtoDrop)
	i.defineMethod(proto, "flatMap", 1, i.iteratorProtoFlatMap)
	i.defineMethod(proto, "reduce", 1, i.iteratorProtoReduce)
	i.defineMethod(proto, "toArray", 0, i.iteratorProtoToArray)
	i.defineMethod(proto, "forEach", 1, i.iteratorProtoForEach)
	i.defineMethod(proto, "some", 1, i.iteratorProtoSome)
	i.defineMethod(proto, "every", 1, i.iteratorProtoEvery)
	i.defineMethod(proto, "find", 1, i.iteratorProtoFind)

	i.setGlobalHidden("Iterator", ctor)
}

// setterIgnoringProto implements SetterThatIgnoresPrototypeProperties (§10.4.9-ish
// helper used by Iterator.prototype's accessor setters).
func (i *Interpreter) setterIgnoringProto(ctx context.Context, this Value, home *Object, key PropertyKey, value Value) error {
	o, ok := this.(*Object)
	if !ok {
		return i.throwError(ctx, "TypeError", "receiver is not an object")
	}
	if o == home {
		return i.throwError(ctx, "TypeError", "cannot assign to a non-writable property")
	}
	if _, has := o.getOwn(key); has {
		return o.Set(ctx, key, value)
	}
	o.defineOwn(key, &Property{Value: value, Writable: true, Enumerable: true, Configurable: true})
	return nil
}

// requireIteratorThis validates the receiver of a helper method: it must be an
// Object. It returns the object or an error.
func (i *Interpreter) requireIteratorThis(ctx context.Context, this Value) (*Object, error) {
	o, ok := this.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Iterator method called on a non-object")
	}
	return o, nil
}

// ---------------------------------------------------------------------------
// Iterator.from and %WrapForValidIteratorPrototype%
// ---------------------------------------------------------------------------

func (i *Interpreter) initWrapForValidIterator() {
	wp := NewObject(i.iteratorProto)
	i.wrapForValidIterProto = wp
	i.defineMethod(wp, "next", 0, func(ctx context.Context, this Value, _ []Value) (Value, error) {
		rec := wrapRecord(this)
		if rec == nil {
			return nil, i.throwError(ctx, "TypeError", "next called on an incompatible receiver")
		}
		nm, ok := rec.nextMethod.(*Object)
		if !ok || !nm.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "iterator.next is not a function")
		}
		return nm.fn.call(ctx, rec.iterator, nil)
	})
	i.defineMethod(wp, "return", 0, func(ctx context.Context, this Value, _ []Value) (Value, error) {
		rec := wrapRecord(this)
		if rec == nil {
			return nil, i.throwError(ctx, "TypeError", "return called on an incompatible receiver")
		}
		ret, err := i.getMethodStr(ctx, rec.iterator, "return")
		if err != nil {
			return nil, err
		}
		if ret == nil {
			return i.newIterResult(Undef, true), nil
		}
		return ret.fn.call(ctx, rec.iterator, nil)
	})
}

func wrapRecord(this Value) *iterRecord {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	rec, _ := o.internal["wrapIterated"].(*iterRecord)
	return rec
}

func (i *Interpreter) iteratorFrom(ctx context.Context, _ Value, args []Value) (Value, error) {
	rec, err := i.getIteratorFlattenable(ctx, arg(args, 0), true)
	if err != nil {
		return nil, err
	}
	// If the iterator already inherits from %Iterator.prototype%, return it as-is.
	if i.hasInPrototypeChain(rec.iterator, i.iteratorProto) {
		return rec.iterator, nil
	}
	wrapper := NewObject(i.wrapForValidIterProto)
	wrapper.class = "Iterator Wrap"
	wrapper.internal = map[string]any{"wrapIterated": rec}
	return wrapper, nil
}

// hasInPrototypeChain reports whether target appears on obj's prototype chain.
func (i *Interpreter) hasInPrototypeChain(obj *Object, target *Object) bool {
	for cur := obj.proto; cur != nil; cur = cur.proto {
		if cur == target {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// String iterator (%StringIteratorPrototype% + String.prototype[@@iterator])
// ---------------------------------------------------------------------------

func (i *Interpreter) initStringIterator() {
	sp := NewObject(i.iteratorProto)
	i.stringIteratorProto = sp
	i.defineMethod(sp, "next", 0, func(ctx context.Context, this Value, _ []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok || o.internal == nil {
			return nil, i.throwError(ctx, "TypeError", "next called on an incompatible receiver")
		}
		runes, ok := o.internal["strIterRunes"].([]rune)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "next called on an incompatible receiver")
		}
		pos, _ := o.internal["strIterPos"].(int)
		if pos >= len(runes) {
			return i.newIterResult(Undef, true), nil
		}
		o.internal["strIterPos"] = pos + 1
		return i.newIterResult(String(string(runes[pos])), false), nil
	})
	sp.defineOwn(SymKey(i.symToStringTag), &Property{
		Value: String("String Iterator"), Writable: false, Enumerable: false, Configurable: true,
	})

	// String.prototype[@@iterator]. §22.1.3.37
	i.defineSymbolMethod(i.stringProto, i.symIterator, "[Symbol.iterator]", 0,
		func(ctx context.Context, this Value, _ []Value) (Value, error) {
			if IsNullish(this) {
				return nil, i.throwError(ctx, "TypeError", "String.prototype[Symbol.iterator] called on null or undefined")
			}
			s, err := i.ToStringV(ctx, this)
			if err != nil {
				return nil, err
			}
			it := NewObject(i.stringIteratorProto)
			it.class = "String Iterator"
			it.internal = map[string]any{"strIterRunes": []rune(s), "strIterPos": 0}
			return it, nil
		})
}

// ---------------------------------------------------------------------------
// Helper methods that return an Iterator Helper (lazy)
// ---------------------------------------------------------------------------

// closeAndThrowNotCallable mirrors the shared prologue of map/filter/flatMap/
// forEach/some/every/find: validate obj, and if the callback is not callable
// close the iterator and throw a TypeError.
func (i *Interpreter) requireCallableClosing(ctx context.Context, obj *Object, v Value) error {
	if fn, ok := v.(*Object); ok && fn.IsCallable() {
		return nil
	}
	rec := &iterRecord{iterator: obj}
	return i.iteratorClose(ctx, rec, i.throwError(ctx, "TypeError", briefValue(v)+" is not a function"))
}

func (i *Interpreter) iteratorProtoMap(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	mapper := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, mapper); err != nil {
		return nil, err
	}
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	fn := mapper.(*Object)
	return i.newIterHelper(func(st *iterHelperState) {
		st.live = []*iterRecord{iterated}
		counter := 0
		st.pull = func(ctx context.Context) (Value, bool, error) {
			value, done, err := i.iteratorStepValue(ctx, iterated)
			if err != nil || done {
				return Undef, done, err
			}
			mapped, err := fn.fn.call(ctx, Undef, []Value{value, Number(float64(counter))})
			if err != nil {
				return nil, false, i.iteratorClose(ctx, iterated, err)
			}
			counter++
			return mapped, false, nil
		}
	}), nil
}

func (i *Interpreter) iteratorProtoFilter(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	predicate := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, predicate); err != nil {
		return nil, err
	}
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	fn := predicate.(*Object)
	return i.newIterHelper(func(st *iterHelperState) {
		st.live = []*iterRecord{iterated}
		counter := 0
		st.pull = func(ctx context.Context) (Value, bool, error) {
			for {
				value, done, err := i.iteratorStepValue(ctx, iterated)
				if err != nil || done {
					return Undef, done, err
				}
				selected, err := fn.fn.call(ctx, Undef, []Value{value, Number(float64(counter))})
				if err != nil {
					return nil, false, i.iteratorClose(ctx, iterated, err)
				}
				counter++
				if ToBoolean(selected) {
					return value, false, nil
				}
			}
		}
	}), nil
}

func (i *Interpreter) iteratorProtoTake(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	pre := &iterRecord{iterator: obj}
	numberLimit, err := i.ToNumberV(ctx, arg(args, 0))
	if err != nil {
		return nil, i.iteratorClose(ctx, pre, err)
	}
	if math.IsNaN(numberLimit) {
		return nil, i.iteratorClose(ctx, pre, i.throwError(ctx, "RangeError", "take limit must not be NaN"))
	}
	limit := ToInteger(numberLimit)
	if limit < 0 {
		return nil, i.iteratorClose(ctx, pre, i.throwError(ctx, "RangeError", "take limit must be non-negative"))
	}
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	return i.newIterHelper(func(st *iterHelperState) {
		st.live = []*iterRecord{iterated}
		remaining := limit
		st.pull = func(ctx context.Context) (Value, bool, error) {
			if remaining == 0 {
				if err := i.iteratorClose(ctx, iterated, nil); err != nil {
					return nil, false, err
				}
				return Undef, true, nil
			}
			if !math.IsInf(remaining, 1) {
				remaining--
			}
			value, done, err := i.iteratorStepValue(ctx, iterated)
			if err != nil || done {
				return Undef, done, err
			}
			return value, false, nil
		}
	}), nil
}

func (i *Interpreter) iteratorProtoDrop(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	pre := &iterRecord{iterator: obj}
	numberLimit, err := i.ToNumberV(ctx, arg(args, 0))
	if err != nil {
		return nil, i.iteratorClose(ctx, pre, err)
	}
	if math.IsNaN(numberLimit) {
		return nil, i.iteratorClose(ctx, pre, i.throwError(ctx, "RangeError", "drop limit must not be NaN"))
	}
	limit := ToInteger(numberLimit)
	if limit < 0 {
		return nil, i.iteratorClose(ctx, pre, i.throwError(ctx, "RangeError", "drop limit must be non-negative"))
	}
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	return i.newIterHelper(func(st *iterHelperState) {
		st.live = []*iterRecord{iterated}
		remaining := limit
		dropped := false
		st.pull = func(ctx context.Context) (Value, bool, error) {
			if !dropped {
				dropped = true
				for remaining > 0 {
					if !math.IsInf(remaining, 1) {
						remaining--
					}
					_, done, err := i.iteratorStep(ctx, iterated)
					if err != nil {
						return nil, false, err
					}
					if done {
						return Undef, true, nil
					}
				}
			}
			value, done, err := i.iteratorStepValue(ctx, iterated)
			if err != nil || done {
				return Undef, done, err
			}
			return value, false, nil
		}
	}), nil
}

func (i *Interpreter) iteratorProtoFlatMap(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	mapper := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, mapper); err != nil {
		return nil, err
	}
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	fn := mapper.(*Object)
	return i.newIterHelper(func(st *iterHelperState) {
		st.live = []*iterRecord{iterated}
		counter := 0
		var inner *iterRecord
		st.pull = func(ctx context.Context) (Value, bool, error) {
			for {
				if inner != nil {
					innerValue, done, err := i.iteratorStepValue(ctx, inner)
					if err != nil {
						// IfAbruptCloseIterator(innerValue, iterated).
						inner = nil
						st.live = []*iterRecord{iterated}
						return nil, false, i.iteratorClose(ctx, iterated, err)
					}
					if done {
						inner = nil
						st.live = []*iterRecord{iterated}
						continue
					}
					return innerValue, false, nil
				}
				value, done, err := i.iteratorStepValue(ctx, iterated)
				if err != nil || done {
					return Undef, done, err
				}
				mapped, err := fn.fn.call(ctx, Undef, []Value{value, Number(float64(counter))})
				if err != nil {
					return nil, false, i.iteratorClose(ctx, iterated, err)
				}
				innerRec, err := i.getIteratorFlattenable(ctx, mapped, false)
				if err != nil {
					return nil, false, i.iteratorClose(ctx, iterated, err)
				}
				counter++
				inner = innerRec
				// Close order on return(): inner first, then outer.
				st.live = []*iterRecord{iterated, inner}
			}
		}
	}), nil
}

// ---------------------------------------------------------------------------
// Eager (consuming) helper methods
// ---------------------------------------------------------------------------

func (i *Interpreter) iteratorProtoReduce(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	reducer := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, reducer); err != nil {
		return nil, err
	}
	fn := reducer.(*Object)
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	var accumulator Value
	counter := 0
	if len(args) < 2 {
		acc, done, err := i.iteratorStepValue(ctx, iterated)
		if err != nil {
			return nil, err
		}
		if done {
			return nil, i.throwError(ctx, "TypeError", "reduce of empty iterator with no initial value")
		}
		accumulator = acc
		counter = 1
	} else {
		accumulator = args[1]
	}
	for {
		value, done, err := i.iteratorStepValue(ctx, iterated)
		if err != nil {
			return nil, err
		}
		if done {
			return accumulator, nil
		}
		res, err := fn.fn.call(ctx, Undef, []Value{accumulator, value, Number(float64(counter))})
		if err != nil {
			return nil, i.iteratorClose(ctx, iterated, err)
		}
		accumulator = res
		counter++
	}
}

func (i *Interpreter) iteratorProtoToArray(ctx context.Context, this Value, _ []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	var items []Value
	for {
		value, done, err := i.iteratorStepValue(ctx, iterated)
		if err != nil {
			return nil, err
		}
		if done {
			return i.newArray(items), nil
		}
		items = append(items, value)
	}
}

func (i *Interpreter) iteratorProtoForEach(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	procedure := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, procedure); err != nil {
		return nil, err
	}
	fn := procedure.(*Object)
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	counter := 0
	for {
		value, done, err := i.iteratorStepValue(ctx, iterated)
		if err != nil {
			return nil, err
		}
		if done {
			return Undef, nil
		}
		if _, err := fn.fn.call(ctx, Undef, []Value{value, Number(float64(counter))}); err != nil {
			return nil, i.iteratorClose(ctx, iterated, err)
		}
		counter++
	}
}

func (i *Interpreter) iteratorProtoSome(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	predicate := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, predicate); err != nil {
		return nil, err
	}
	fn := predicate.(*Object)
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	counter := 0
	for {
		value, done, err := i.iteratorStepValue(ctx, iterated)
		if err != nil {
			return nil, err
		}
		if done {
			return False, nil
		}
		res, err := fn.fn.call(ctx, Undef, []Value{value, Number(float64(counter))})
		if err != nil {
			return nil, i.iteratorClose(ctx, iterated, err)
		}
		if ToBoolean(res) {
			if err := i.iteratorClose(ctx, iterated, nil); err != nil {
				return nil, err
			}
			return True, nil
		}
		counter++
	}
}

func (i *Interpreter) iteratorProtoEvery(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	predicate := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, predicate); err != nil {
		return nil, err
	}
	fn := predicate.(*Object)
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	counter := 0
	for {
		value, done, err := i.iteratorStepValue(ctx, iterated)
		if err != nil {
			return nil, err
		}
		if done {
			return True, nil
		}
		res, err := fn.fn.call(ctx, Undef, []Value{value, Number(float64(counter))})
		if err != nil {
			return nil, i.iteratorClose(ctx, iterated, err)
		}
		if !ToBoolean(res) {
			if err := i.iteratorClose(ctx, iterated, nil); err != nil {
				return nil, err
			}
			return False, nil
		}
		counter++
	}
}

func (i *Interpreter) iteratorProtoFind(ctx context.Context, this Value, args []Value) (Value, error) {
	obj, err := i.requireIteratorThis(ctx, this)
	if err != nil {
		return nil, err
	}
	predicate := arg(args, 0)
	if err := i.requireCallableClosing(ctx, obj, predicate); err != nil {
		return nil, err
	}
	fn := predicate.(*Object)
	iterated, err := i.getIteratorDirect(ctx, obj)
	if err != nil {
		return nil, err
	}
	counter := 0
	for {
		value, done, err := i.iteratorStepValue(ctx, iterated)
		if err != nil {
			return nil, err
		}
		if done {
			return Undef, nil
		}
		res, err := fn.fn.call(ctx, Undef, []Value{value, Number(float64(counter))})
		if err != nil {
			return nil, i.iteratorClose(ctx, iterated, err)
		}
		if ToBoolean(res) {
			if err := i.iteratorClose(ctx, iterated, nil); err != nil {
				return nil, err
			}
			return value, nil
		}
		counter++
	}
}
