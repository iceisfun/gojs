package interp

import "context"

// This file implements the spec-accurate Promise static combinators
// (Promise.all / allSettled / race / any) and the abstract operations they
// depend on: NewPromiseCapability (§27.2.1.5), GetPromiseResolve (§27.2.4.1.1),
// and GetIterator (§7.4.2). Unlike a naive native implementation, these honour
// the `this` value as the constructor C, drive the user-observable iterator
// protocol one element at a time (so a non-terminating iterator that throws mid
// -stream closes correctly), and route every element through C.resolve and
// Invoke(nextPromise, "then", ...) — exactly what the conformance tests probe.

// promiseCapability mirrors the spec PromiseCapability Record { [[Promise]],
// [[Resolve]], [[Reject]] }. Resolve/Reject are callable Values.
type promiseCapability struct {
	promise Value
	resolve Value
	reject  Value
}

// newPromiseCapability implements NewPromiseCapability(C) (§27.2.1.5). It
// constructs C with a capturing executor and validates that the executor set
// callable resolve/reject functions.
func (i *Interpreter) newPromiseCapability(ctx context.Context, c *Object) (*promiseCapability, error) {
	if !c.IsConstructor() {
		return nil, i.throwError(ctx, "TypeError", "Promise capability requires a constructor")
	}
	cap := &promiseCapability{resolve: Undef, reject: Undef}
	executor := i.newNativeFunc("", 2, func(_ context.Context, _ Value, args []Value) (Value, error) {
		// GetCapabilitiesExecutor: resolve/reject may each be set at most once.
		if !IsUndefined(cap.resolve) || !IsUndefined(cap.reject) {
			return nil, i.throwError(ctx, "TypeError", "promise capability already has resolve/reject")
		}
		cap.resolve = arg(args, 0)
		cap.reject = arg(args, 1)
		return Undef, nil
	})
	promiseV, err := c.fn.construct(ctx, c, []Value{executor})
	if err != nil {
		return nil, err
	}
	if !isCallableValue(cap.resolve) {
		return nil, i.throwError(ctx, "TypeError", "promise capability resolve is not callable")
	}
	if !isCallableValue(cap.reject) {
		return nil, i.throwError(ctx, "TypeError", "promise capability reject is not callable")
	}
	cap.promise = promiseV
	return cap, nil
}

// isCallableValue reports whether v is a callable object.
func isCallableValue(v Value) bool {
	o, ok := v.(*Object)
	return ok && o.IsCallable()
}

// getPromiseResolve implements GetPromiseResolve(C) (§27.2.4.1.1): Get(C,
// "resolve") and require it to be callable.
func (i *Interpreter) getPromiseResolve(ctx context.Context, c *Object) (Value, error) {
	r, err := c.GetStr(ctx, "resolve")
	if err != nil {
		return nil, err
	}
	if !isCallableValue(r) {
		return nil, i.throwError(ctx, "TypeError", "Promise resolve is not callable")
	}
	return r, nil
}

// getIterator implements GetIterator(obj, sync) (§7.4.2): look up @@iterator,
// call it, and build an iterator record with the next method.
func (i *Interpreter) getIterator(ctx context.Context, obj Value) (*iterRecord, error) {
	method, err := i.getMethod(ctx, obj, i.symIterator)
	if err != nil {
		return nil, err
	}
	if method == nil {
		return nil, i.throwError(ctx, "TypeError", briefValue(obj)+" is not iterable")
	}
	iterator, err := method.fn.call(ctx, obj, nil)
	if err != nil {
		return nil, err
	}
	itObj, ok := iterator.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "iterator is not an object")
	}
	return i.getIteratorDirect(ctx, itObj)
}

// ifAbruptRejectPromise implements IfAbruptRejectPromise: on a thrown JS value
// it rejects the capability and returns its promise; a host error (e.g. context
// cancellation) propagates unchanged.
func (i *Interpreter) ifAbruptRejectPromise(ctx context.Context, cap *promiseCapability, err error) (Value, error) {
	if tv, ok := ThrownValue(err); ok {
		if _, e := i.call(ctx, cap.reject, Undef, []Value{tv}); e != nil {
			return nil, e
		}
		return cap.promise, nil
	}
	return nil, err
}

// combinatorPreamble runs the shared prologue of every Promise combinator:
// resolve C (=this), build the result capability, obtain C.resolve, and get the
// iterator. It returns the capability, promiseResolve, and iterator record, or —
// when an abrupt completion occurs after the capability exists — a settled
// promise via IfAbruptRejectPromise (settled != nil signals early return).
func (i *Interpreter) combinatorPreamble(ctx context.Context, this Value, iterable Value) (cap *promiseCapability, promiseResolve Value, ir *iterRecord, settled Value, err error) {
	c, ok := this.(*Object)
	if !ok {
		return nil, nil, nil, nil, i.throwError(ctx, "TypeError", "Promise combinator called on non-object")
	}
	cap, err = i.newPromiseCapability(ctx, c)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	promiseResolve, err = i.getPromiseResolve(ctx, c)
	if err != nil {
		settled, err = i.ifAbruptRejectPromise(ctx, cap, err)
		return cap, nil, nil, settled, err
	}
	ir, err = i.getIterator(ctx, iterable)
	if err != nil {
		settled, err = i.ifAbruptRejectPromise(ctx, cap, err)
		return cap, nil, nil, settled, err
	}
	return cap, promiseResolve, ir, nil, nil
}

// finishCombinator applies the abrupt-completion handling shared by the
// combinators: close the iterator if not done, then IfAbruptRejectPromise.
func (i *Interpreter) finishCombinator(ctx context.Context, cap *promiseCapability, ir *iterRecord, perr error) (Value, error) {
	if perr != nil {
		if !ir.done {
			perr = i.iteratorClose(ctx, ir, perr)
		}
		return i.ifAbruptRejectPromise(ctx, cap, perr)
	}
	return cap.promise, nil
}

// invokeThen performs Invoke(nextPromise, "then", « onFulfilled, onRejected »).
func (i *Interpreter) invokeThen(ctx context.Context, nextPromise Value, onFulfilled, onRejected Value) error {
	thenFn, err := i.getMethodStr2(ctx, nextPromise, "then")
	if err != nil {
		return err
	}
	_, err = thenFn.fn.call(ctx, nextPromise, []Value{onFulfilled, onRejected})
	return err
}

// getMethodStr2 is GetMethod for a string key on an arbitrary value, requiring a
// callable result (used for Invoke).
func (i *Interpreter) getMethodStr2(ctx context.Context, v Value, name string) (*Object, error) {
	o, ok := v.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", briefValue(v)+" is not an object")
	}
	m, err := o.GetStr(ctx, name)
	if err != nil {
		return nil, err
	}
	fo, ok := m.(*Object)
	if !ok || !fo.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", name+" is not a function")
	}
	return fo, nil
}

// ---------------------------------------------------------------------------
// Promise.all
// ---------------------------------------------------------------------------

func (i *Interpreter) promiseAllStatic(ctx context.Context, this Value, iterable Value) (Value, error) {
	cap, promiseResolve, ir, settled, err := i.combinatorPreamble(ctx, this, iterable)
	if err != nil || settled != nil {
		return settled, err
	}
	c := this.(*Object)
	perr := i.performPromiseAll(ctx, ir, c, cap, promiseResolve)
	return i.finishCombinator(ctx, cap, ir, perr)
}

// allState is the shared closure state for the resolve element functions.
type allState struct {
	values    []Value
	remaining int
}

func (i *Interpreter) performPromiseAll(ctx context.Context, ir *iterRecord, c *Object, cap *promiseCapability, promiseResolve Value) error {
	st := &allState{remaining: 1}
	index := 0
	for {
		val, done, err := i.iteratorStepValue(ctx, ir)
		if err != nil {
			return err
		}
		if done {
			st.remaining--
			if st.remaining == 0 {
				arr := i.newArray(append([]Value{}, st.values...))
				if _, e := i.call(ctx, cap.resolve, Undef, []Value{arr}); e != nil {
					return e
				}
			}
			return nil
		}
		st.values = append(st.values, Undef)
		nextPromise, err := i.call(ctx, promiseResolve, c, []Value{val})
		if err != nil {
			return err
		}
		idx := index
		alreadyCalled := false
		onFulfilled := i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			if alreadyCalled {
				return Undef, nil
			}
			alreadyCalled = true
			st.values[idx] = arg(a, 0)
			st.remaining--
			if st.remaining == 0 {
				arr := i.newArray(append([]Value{}, st.values...))
				if _, e := i.call(ctx, cap.resolve, Undef, []Value{arr}); e != nil {
					return nil, e
				}
			}
			return Undef, nil
		})
		st.remaining++
		if err := i.invokeThen(ctx, nextPromise, onFulfilled, cap.reject); err != nil {
			return err
		}
		index++
	}
}

// ---------------------------------------------------------------------------
// Promise.allSettled
// ---------------------------------------------------------------------------

func (i *Interpreter) promiseAllSettledStatic(ctx context.Context, this Value, iterable Value) (Value, error) {
	cap, promiseResolve, ir, settled, err := i.combinatorPreamble(ctx, this, iterable)
	if err != nil || settled != nil {
		return settled, err
	}
	c := this.(*Object)
	perr := i.performPromiseAllSettled(ctx, ir, c, cap, promiseResolve)
	return i.finishCombinator(ctx, cap, ir, perr)
}

func (i *Interpreter) performPromiseAllSettled(ctx context.Context, ir *iterRecord, c *Object, cap *promiseCapability, promiseResolve Value) error {
	st := &allState{remaining: 1}
	index := 0
	settle := func() error {
		st.remaining--
		if st.remaining == 0 {
			arr := i.newArray(append([]Value{}, st.values...))
			if _, e := i.call(ctx, cap.resolve, Undef, []Value{arr}); e != nil {
				return e
			}
		}
		return nil
	}
	for {
		val, done, err := i.iteratorStepValue(ctx, ir)
		if err != nil {
			return err
		}
		if done {
			if err := settle(); err != nil {
				return err
			}
			return nil
		}
		st.values = append(st.values, Undef)
		nextPromise, err := i.call(ctx, promiseResolve, c, []Value{val})
		if err != nil {
			return err
		}
		idx := index
		alreadyCalled := false
		mk := func(status, key string) *Object {
			return i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
				if alreadyCalled {
					return Undef, nil
				}
				alreadyCalled = true
				desc := NewObject(i.objectProto)
				desc.SetData("status", String(status))
				desc.SetData(key, arg(a, 0))
				st.values[idx] = desc
				if err := settle(); err != nil {
					return nil, err
				}
				return Undef, nil
			})
		}
		onFulfilled := mk("fulfilled", "value")
		onRejected := mk("rejected", "reason")
		st.remaining++
		if err := i.invokeThen(ctx, nextPromise, onFulfilled, onRejected); err != nil {
			return err
		}
		index++
	}
}

// ---------------------------------------------------------------------------
// Promise.race
// ---------------------------------------------------------------------------

func (i *Interpreter) promiseRaceStatic(ctx context.Context, this Value, iterable Value) (Value, error) {
	cap, promiseResolve, ir, settled, err := i.combinatorPreamble(ctx, this, iterable)
	if err != nil || settled != nil {
		return settled, err
	}
	c := this.(*Object)
	perr := i.performPromiseRace(ctx, ir, c, cap, promiseResolve)
	return i.finishCombinator(ctx, cap, ir, perr)
}

func (i *Interpreter) performPromiseRace(ctx context.Context, ir *iterRecord, c *Object, cap *promiseCapability, promiseResolve Value) error {
	for {
		val, done, err := i.iteratorStepValue(ctx, ir)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		nextPromise, err := i.call(ctx, promiseResolve, c, []Value{val})
		if err != nil {
			return err
		}
		if err := i.invokeThen(ctx, nextPromise, cap.resolve, cap.reject); err != nil {
			return err
		}
	}
}

// ---------------------------------------------------------------------------
// Promise.any
// ---------------------------------------------------------------------------

func (i *Interpreter) promiseAnyStatic(ctx context.Context, this Value, iterable Value) (Value, error) {
	cap, promiseResolve, ir, settled, err := i.combinatorPreamble(ctx, this, iterable)
	if err != nil || settled != nil {
		return settled, err
	}
	c := this.(*Object)
	perr := i.performPromiseAny(ctx, ir, c, cap, promiseResolve)
	return i.finishCombinator(ctx, cap, ir, perr)
}

func (i *Interpreter) performPromiseAny(ctx context.Context, ir *iterRecord, c *Object, cap *promiseCapability, promiseResolve Value) error {
	st := &allState{remaining: 1} // values holds the collected rejection reasons
	index := 0
	rejectAll := func() error {
		st.remaining--
		if st.remaining == 0 {
			agg := i.newAggregateError(append([]Value{}, st.values...), "All promises were rejected")
			if _, e := i.call(ctx, cap.reject, Undef, []Value{agg}); e != nil {
				return e
			}
		}
		return nil
	}
	for {
		val, done, err := i.iteratorStepValue(ctx, ir)
		if err != nil {
			return err
		}
		if done {
			if err := rejectAll(); err != nil {
				return err
			}
			return nil
		}
		st.values = append(st.values, Undef)
		nextPromise, err := i.call(ctx, promiseResolve, c, []Value{val})
		if err != nil {
			return err
		}
		idx := index
		alreadyCalled := false
		onRejected := i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			if alreadyCalled {
				return Undef, nil
			}
			alreadyCalled = true
			st.values[idx] = arg(a, 0)
			if err := rejectAll(); err != nil {
				return nil, err
			}
			return Undef, nil
		})
		st.remaining++
		if err := i.invokeThen(ctx, nextPromise, cap.resolve, onRejected); err != nil {
			return err
		}
		index++
	}
}

// newAggregateError builds an AggregateError instance from a Go slice of reasons
// and a message, using the realm's AggregateError.prototype.
func (i *Interpreter) newAggregateError(errors []Value, message string) Value {
	proto := i.aggregateErrorProto
	if proto == nil {
		proto = i.errorProto
	}
	obj := NewObject(proto)
	obj.class = "Error"
	obj.SetHidden("message", String(message))
	obj.defineOwn(StrKey("errors"), &Property{
		Value: i.newArray(errors), Writable: true, Enumerable: false, Configurable: true,
	})
	obj.SetHidden("stack", String("AggregateError: "+message))
	return obj
}
