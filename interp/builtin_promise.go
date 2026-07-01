package interp

import "context"

// This file implements the Promise built-in (ECMA-262 §27.2), a Promises/A+
// compliant state machine integrated with the interpreter's microtask queue.
//
// # Architecture
//
// Each Promise object carries a *promiseState in its internal["state"] slot.
// The state machine has three statuses: pending, fulfilled, or rejected.
// Reactions (then/catch/finally continuations) are stored as slices on the
// pending state and drained — via i.loop.pushMicro — when the promise settles.
//
// # Thenable adoption
//
// When a promise is resolved with a value that has a callable .then, the
// engine schedules a microtask (PromiseResolveThenableJob) that calls
// then(resolvePromise, rejectPromise) with a fresh pair of resolving functions.
// This matches §27.2.1.3.2 step 8-11.
//
// # Microtask scheduling
//
// All reaction callbacks and thenable-adoption jobs are enqueued via
// i.loop.pushMicro so they run after the current synchronous task, in FIFO
// order, before any macrotask (timer) fires. This mirrors the JavaScript
// job-queue semantics required by the spec.

// ---------------------------------------------------------------------------
// Internal promise state
// ---------------------------------------------------------------------------

// promiseStatus is the settlement state of a Promise.
type promiseStatus uint8

const (
	promisePending   promiseStatus = iota // initial state; reactions are queued
	promiseFulfilled                      // settled with a value
	promiseRejected                       // settled with a reason
)

// promiseReaction pairs the handler callback with the downstream promise's
// resolve/reject functions.  isReject controls the pass-through direction when
// handler is not callable: fulfill reactions pass through via resolve, reject
// reactions pass through via reject (§27.2.2.1).
type promiseReaction struct {
	handler  Value       // onFulfilled or onRejected (may be non-callable)
	resolve  func(Value) // resolve of the downstream (result) promise
	reject   func(Value) // reject of the downstream (result) promise
	isReject bool        // true for reject reactions
}

// promiseState holds the [[PromiseState]], [[PromiseResult]], and reaction
// queue slots described in ECMA-262 §27.2.
type promiseState struct {
	status           promiseStatus
	result           Value
	fulfillReactions []promiseReaction
	rejectReactions  []promiseReaction

	// hostResolve/hostReject are the promise's resolver pair, stashed so the
	// host-facing ResolvePromise/RejectPromise API can settle this promise
	// object from outside the Promise implementation (see host_api.go).
	hostResolve func(Value)
	hostReject  func(Value)
}

// ---------------------------------------------------------------------------
// initPromise — entry point called from bootstrap
// ---------------------------------------------------------------------------

// initPromise installs the Promise constructor and Promise.prototype on the
// global object.  The interpreter's promiseProto intrinsic is populated here;
// it is already allocated (as a bare Object) by bootstrap before initPromise
// is called.
func (i *Interpreter) initPromise() {
	proto := i.promiseProto
	proto.class = "Promise"

	// -----------------------------------------------------------------------
	// Promise.prototype.then(onFulfilled, onRejected)
	//
	// Registers fulfil/reject reactions and returns a new promise that
	// settles based on the handler return values.  §27.2.5.4
	// -----------------------------------------------------------------------
	i.defineMethod(proto, "then", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		p, ok := this.(*Object)
		if !ok || p.class != "Promise" {
			return nil, i.throwError(ctx, "TypeError", "Promise.prototype.then called on non-Promise")
		}
		return i.promiseThen(p, arg(args, 0), arg(args, 1)), nil
	})

	// -----------------------------------------------------------------------
	// Promise.prototype.catch(onRejected)
	//
	// Shorthand for then(undefined, onRejected).  §27.2.5.1
	// -----------------------------------------------------------------------
	i.defineMethod(proto, "catch", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		p, ok := this.(*Object)
		if !ok || p.class != "Promise" {
			return nil, i.throwError(ctx, "TypeError", "Promise.prototype.catch called on non-Promise")
		}
		return i.promiseThen(p, Undef, arg(args, 0)), nil
	})

	// -----------------------------------------------------------------------
	// Promise.prototype.finally(onFinally)
	//
	// Runs onFinally on settlement without changing the value/reason.
	// If onFinally is not callable, it passes through like a plain .then.
	// §27.2.5.3
	// -----------------------------------------------------------------------
	i.defineMethod(proto, "finally", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		p, ok := this.(*Object)
		if !ok || p.class != "Promise" {
			return nil, i.throwError(ctx, "TypeError", "Promise.prototype.finally called on non-Promise")
		}
		onFinally := arg(args, 0)
		fn, isFn := onFinally.(*Object)
		if !isFn || !fn.IsCallable() {
			// Non-callable: behaves like .then(onFinally, onFinally) — both
			// handlers will pass through value/reason unchanged.
			return i.promiseThen(p, onFinally, onFinally), nil
		}
		// Callable: wrap to preserve the original value/reason.
		//
		// thenFinally:  v => Promise.resolve(onFinally()).then(() => v)
		// catchFinally: r => Promise.resolve(onFinally()).then(() => { throw r })
		thenFinally := i.newNativeFunc("", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
			v := arg(args, 0)
			result, err := i.call(ctx, onFinally, Undef, nil)
			if err != nil {
				return nil, err
			}
			// Wrap result in a resolved promise then map to v.
			rp := i.promiseResolveValue(result)
			return i.promiseThen(rp, i.newNativeFunc("", 0, func(_ context.Context, _ Value, _ []Value) (Value, error) {
				return v, nil
			}), Undef), nil
		})
		catchFinally := i.newNativeFunc("", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
			r := arg(args, 0)
			result, err := i.call(ctx, onFinally, Undef, nil)
			if err != nil {
				return nil, err
			}
			rp := i.promiseResolveValue(result)
			return i.promiseThen(rp, i.newNativeFunc("", 0, func(_ context.Context, _ Value, _ []Value) (Value, error) {
				return nil, NewThrow(r)
			}), Undef), nil
		})
		return i.promiseThen(p, thenFinally, catchFinally), nil
	})

	// -----------------------------------------------------------------------
	// Promise constructor — new Promise(executor)
	//
	// executor(resolve, reject) is called synchronously.  If it throws, the
	// promise is rejected with the thrown value.  §27.2.3.1
	// -----------------------------------------------------------------------
	callFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Promise constructor must be called with new")
	}
	constructFn := func(ctx context.Context, _ Value, args []Value) (Value, error) {
		executor := arg(args, 0)
		if fn, ok := executor.(*Object); !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Promise resolver must be a function")
		}
		pObj, resolve, reject := i.newPromise()
		resolveFn := i.newNativeFunc("resolve", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
			resolve(arg(args, 0))
			return Undef, nil
		})
		rejectFn := i.newNativeFunc("reject", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
			reject(arg(args, 0))
			return Undef, nil
		})
		_, err := i.call(ctx, executor, Undef, []Value{resolveFn, rejectFn})
		if err != nil {
			// If the executor threw, reject the promise with the thrown value.
			if tv, ok := ThrownValue(err); ok {
				reject(tv)
			} else {
				reject(String(err.Error()))
			}
		}
		return pObj, nil
	}
	ctor := i.newNativeCtor("Promise", 1, callFn, constructFn)
	linkCtor(ctor, proto)

	// -----------------------------------------------------------------------
	// Promise.resolve(value)
	//
	// If value is already a native Promise, return it unchanged.  Otherwise
	// wrap it in a resolved promise (handles thenables via resolution).
	// §27.2.4.5
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "resolve", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		v := arg(args, 0)
		if obj, ok := v.(*Object); ok && obj.class == "Promise" {
			return obj, nil
		}
		return i.promiseResolveValue(v), nil
	})

	// -----------------------------------------------------------------------
	// Promise.reject(reason)
	//
	// Returns a promise already rejected with reason.  §27.2.4.4
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "reject", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		pObj, _, reject := i.newPromise()
		reject(arg(args, 0))
		return pObj, nil
	})

	// -----------------------------------------------------------------------
	// Promise.all(iterable)
	//
	// Resolves with an array of all fulfilled values; rejects on the first
	// rejection.  §27.2.4.1
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "all", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		return i.promiseAll(ctx, arg(args, 0))
	})

	// -----------------------------------------------------------------------
	// Promise.allSettled(iterable)
	//
	// Resolves once every promise settles; result is an array of
	// {status, value|reason} descriptor objects.  §27.2.4.2
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "allSettled", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		return i.promiseAllSettled(ctx, arg(args, 0))
	})

	// -----------------------------------------------------------------------
	// Promise.race(iterable)
	//
	// Resolves or rejects with the value/reason of the first settled promise.
	// An empty iterable produces a forever-pending promise.  §27.2.4.3
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "race", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		return i.promiseRace(ctx, arg(args, 0))
	})

	// -----------------------------------------------------------------------
	// Promise.any(iterable)
	//
	// Resolves with the first fulfilled value; if all reject, rejects with an
	// AggregateError carrying all reasons.  §27.2.4.4 (ES2021)
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "any", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		return i.promiseAny(ctx, arg(args, 0))
	})

	i.setGlobalHidden("Promise", ctor)
}

// ---------------------------------------------------------------------------
// Core state machine
// ---------------------------------------------------------------------------

// newPromise allocates a pending Promise object and returns it together with
// Go closures for resolving and rejecting it.  The closures are idempotent
// (only the first call has any effect) and safe to hold indefinitely.
//
// Internally, newPromise produces resolving-function pairs via makeResolvers so
// that thenable adoption (§27.2.1.3.2) can create a second pair on the same
// promise without re-entering the first pair's alreadyResolved guard.
func (i *Interpreter) newPromise() (pObj *Object, resolve func(Value), reject func(Value)) {
	state := &promiseState{
		status: promisePending,
		result: Undef,
	}
	pObj = NewObject(i.promiseProto)
	pObj.class = "Promise"
	pObj.internal = map[string]any{"state": state}

	// directFulfill and directReject settle the promise unconditionally,
	// trigger all queued reactions as microtasks, and clear the reaction lists.
	// Callers must guard against calling these on an already-settled promise.
	directFulfill := func(value Value) {
		if state.status != promisePending {
			return
		}
		state.status = promiseFulfilled
		state.result = value
		reactions := state.fulfillReactions
		state.fulfillReactions = nil
		state.rejectReactions = nil
		for _, r := range reactions {
			r := r // capture loop variable
			i.loop.pushMicro(func() error {
				return i.triggerReaction(r, value)
			})
		}
	}
	directReject := func(reason Value) {
		if state.status != promisePending {
			return
		}
		state.status = promiseRejected
		state.result = reason
		reactions := state.rejectReactions
		state.fulfillReactions = nil
		state.rejectReactions = nil
		for _, r := range reactions {
			r := r // capture loop variable
			i.loop.pushMicro(func() error {
				return i.triggerReaction(r, reason)
			})
		}
	}

	// makeResolvers produces a new resolve/reject pair whose shared
	// alreadyResolved flag prevents double-settlement from a single pair.
	// Multiple pairs can exist (for thenable resolution chains) because each
	// pair has an independent alreadyResolved, while the actual settlement is
	// guarded by state.status.
	var makeResolvers func() (func(Value), func(Value))
	makeResolvers = func() (func(Value), func(Value)) {
		alreadyResolved := false

		var rejFn func(Value)
		rejFn = func(reason Value) {
			if alreadyResolved {
				return
			}
			alreadyResolved = true
			directReject(reason)
		}

		var resFn func(Value)
		resFn = func(value Value) {
			if alreadyResolved {
				return
			}
			alreadyResolved = true

			// §27.2.1.3.2 step 6: resolving with the promise itself is a cycle.
			if value == pObj {
				directReject(i.newError("TypeError", "Chaining cycle detected for promise"))
				return
			}

			// §27.2.1.3.2 step 8-11: if value is a thenable, adopt its state
			// asynchronously via a microtask (PromiseResolveThenableJob).
			if obj, ok := value.(*Object); ok {
				thenV, err := obj.GetStr(i.ctx, "then")
				if err != nil {
					// Getting .then threw; reject with that value.
					if tv, ok2 := ThrownValue(err); ok2 {
						directReject(tv)
					} else {
						directReject(String(err.Error()))
					}
					return
				}
				if thenFn, ok2 := thenV.(*Object); ok2 && thenFn.IsCallable() {
					capturedThen := thenFn
					capturedObj := obj
					i.loop.pushMicro(func() error {
						// Fresh resolving functions for the same promise, per spec.
						innerRes, innerRej := makeResolvers()
						innerResFn := i.newNativeFunc("resolve", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
							innerRes(arg(args, 0))
							return Undef, nil
						})
						innerRejFn := i.newNativeFunc("reject", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
							innerRej(arg(args, 0))
							return Undef, nil
						})
						_, callErr := capturedThen.fn.call(i.ctx, capturedObj, []Value{innerResFn, innerRejFn})
						if callErr != nil {
							if tv, ok := ThrownValue(callErr); ok {
								innerRej(tv)
							} else {
								innerRej(String(callErr.Error()))
							}
						}
						return nil
					})
					return
				}
			}

			// Plain value: fulfill directly.
			directFulfill(value)
		}

		return resFn, rejFn
	}

	resolve, reject = makeResolvers()
	// Stash a resolver pair on the state so the host-facing ResolvePromise /
	// RejectPromise API (host_api.go) can settle this promise object directly.
	state.hostResolve = resolve
	state.hostReject = reject
	return pObj, resolve, reject
}

// settleNativePromise settles a native promise object via its stashed resolver,
// running the full resolution algorithm (including thenable adoption). It must
// run on the VM goroutine.
func (i *Interpreter) settleNativePromise(p *Object, value Value, isReject bool) {
	if p == nil || p.internal == nil {
		return
	}
	state, ok := p.internal["state"].(*promiseState)
	if !ok {
		return
	}
	if isReject {
		if state.hostReject != nil {
			state.hostReject(value)
		}
		return
	}
	if state.hostResolve != nil {
		state.hostResolve(value)
	}
}

// promiseState extracts the *promiseState from a Promise object.
// Returns a dummy fulfilled state if the slot is absent (defensive).
func (i *Interpreter) getPromiseState(p *Object) *promiseState {
	if p.internal != nil {
		if s, ok := p.internal["state"].(*promiseState); ok {
			return s
		}
	}
	return &promiseState{status: promiseFulfilled, result: Undef}
}

// promiseThen registers fulfill/reject reactions on p and returns a new
// promise whose settlement is driven by the reactions.  This is the core of
// the Promises/A+ "then" operation (§27.2.5.4.1).
func (i *Interpreter) promiseThen(p *Object, onFulfilled, onRejected Value) *Object {
	state := i.getPromiseState(p)
	resultP, resolve, reject := i.newPromise()

	// Fulfill reaction: if handler is callable, call it and adopt the result;
	// otherwise pass the value through to resolve.
	fulfillReaction := promiseReaction{
		handler:  onFulfilled,
		resolve:  resolve,
		reject:   reject,
		isReject: false,
	}
	// Reject reaction: if handler is callable, call it and adopt the result;
	// otherwise pass the reason through to reject.
	rejectReaction := promiseReaction{
		handler:  onRejected,
		resolve:  resolve,
		reject:   reject,
		isReject: true,
	}

	switch state.status {
	case promisePending:
		// Queue reactions to be triggered when the promise settles.
		state.fulfillReactions = append(state.fulfillReactions, fulfillReaction)
		state.rejectReactions = append(state.rejectReactions, rejectReaction)

	case promiseFulfilled:
		// Already settled: schedule a microtask immediately.
		value := state.result
		i.loop.pushMicro(func() error {
			return i.triggerReaction(fulfillReaction, value)
		})

	case promiseRejected:
		reason := state.result
		i.loop.pushMicro(func() error {
			return i.triggerReaction(rejectReaction, reason)
		})
	}

	return resultP
}

// triggerReaction executes a single promise reaction microtask.
// If the handler is callable, it is invoked with value; the return is adopted
// by the downstream promise (via its resolve function, which handles thenables).
// If the handler is not callable, value is passed through: to resolve for
// fulfill reactions, to reject for reject reactions.
func (i *Interpreter) triggerReaction(r promiseReaction, value Value) error {
	if fn, ok := r.handler.(*Object); ok && fn.IsCallable() {
		result, err := fn.fn.call(i.ctx, Undef, []Value{value})
		if err != nil {
			// Handler threw: reject the downstream promise.
			if tv, ok2 := ThrownValue(err); ok2 {
				r.reject(tv)
			} else {
				r.reject(String(err.Error()))
			}
			return nil
		}
		// Handler returned normally: resolve the downstream promise with the
		// result (which may itself be a thenable and will be adopted).
		r.resolve(result)
		return nil
	}
	// Non-callable handler: pass through in the appropriate direction.
	if r.isReject {
		r.reject(value)
	} else {
		r.resolve(value)
	}
	return nil
}

// promiseResolveValue wraps v in a resolved Promise, handling thenables via
// the normal resolution algorithm.  Equivalent to Promise.resolve(v) but
// without the "already a Promise" short-circuit (that is done by the static
// method itself).
func (i *Interpreter) promiseResolveValue(v Value) *Object {
	pObj, resolve, _ := i.newPromise()
	resolve(v)
	return pObj
}

// ---------------------------------------------------------------------------
// Static combinators
// ---------------------------------------------------------------------------

// promiseAll implements Promise.all: resolves with an array of fulfillment
// values once all input promises fulfil; rejects on the first rejection.
// If iteration itself throws, that error propagates synchronously (matching
// IfAbruptRejectPromise semantics for non-iterable inputs).
func (i *Interpreter) promiseAll(ctx context.Context, iterable Value) (*Object, error) {
	resultP, resolve, reject := i.newPromise()

	var promises []Value
	err := i.iterate(ctx, iterable, func(v Value) error {
		promises = append(promises, v)
		return nil
	})
	if err != nil {
		if tv, ok := ThrownValue(err); ok {
			reject(tv)
			return resultP, nil
		}
		return nil, err
	}

	if len(promises) == 0 {
		resolve(i.newArray(nil))
		return resultP, nil
	}

	results := make([]Value, len(promises))
	remaining := len(promises)

	for idx, p := range promises {
		idx := idx
		pObj := i.promiseResolveValue(p)
		i.promiseThen(pObj,
			i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
				results[idx] = arg(args, 0)
				remaining--
				if remaining == 0 {
					resolve(i.newArray(append([]Value{}, results...)))
				}
				return Undef, nil
			}),
			i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
				reject(arg(args, 0))
				return Undef, nil
			}))
	}
	return resultP, nil
}

// promiseAllSettled implements Promise.allSettled: resolves once every input
// promise has settled, producing an array of {status, value|reason} objects.
func (i *Interpreter) promiseAllSettled(ctx context.Context, iterable Value) (*Object, error) {
	resultP, resolve, _ := i.newPromise()

	var promises []Value
	err := i.iterate(ctx, iterable, func(v Value) error {
		promises = append(promises, v)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(promises) == 0 {
		resolve(i.newArray(nil))
		return resultP, nil
	}

	results := make([]Value, len(promises))
	remaining := len(promises)

	makeSettleHandler := func(idx int, status string, key string) *Object {
		return i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
			desc := NewObject(i.objectProto)
			desc.SetData("status", String(status))
			desc.SetData(key, arg(args, 0))
			results[idx] = desc
			remaining--
			if remaining == 0 {
				resolve(i.newArray(append([]Value{}, results...)))
			}
			return Undef, nil
		})
	}

	for idx, p := range promises {
		pObj := i.promiseResolveValue(p)
		i.promiseThen(pObj,
			makeSettleHandler(idx, "fulfilled", "value"),
			makeSettleHandler(idx, "rejected", "reason"))
	}
	return resultP, nil
}

// promiseRace implements Promise.race: resolves or rejects with the first
// promise to settle.  An empty iterable yields a forever-pending promise.
func (i *Interpreter) promiseRace(ctx context.Context, iterable Value) (*Object, error) {
	resultP, resolve, reject := i.newPromise()

	err := i.iterate(ctx, iterable, func(v Value) error {
		pObj := i.promiseResolveValue(v)
		i.promiseThen(pObj,
			i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
				resolve(arg(args, 0))
				return Undef, nil
			}),
			i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
				reject(arg(args, 0))
				return Undef, nil
			}))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resultP, nil
}

// promiseAny implements Promise.any (ES2021): resolves with the first
// fulfilled value; if every promise rejects, rejects with an AggregateError
// carrying all rejection reasons.
func (i *Interpreter) promiseAny(ctx context.Context, iterable Value) (*Object, error) {
	resultP, resolve, reject := i.newPromise()

	var promises []Value
	err := i.iterate(ctx, iterable, func(v Value) error {
		promises = append(promises, v)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(promises) == 0 {
		// Vacuously all rejected.
		reject(i.makeAggregateError(i.newArray(nil), "All promises were rejected"))
		return resultP, nil
	}

	errors := make([]Value, len(promises))
	remaining := len(promises)

	for idx, p := range promises {
		idx := idx
		pObj := i.promiseResolveValue(p)
		i.promiseThen(pObj,
			i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
				resolve(arg(args, 0))
				return Undef, nil
			}),
			i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
				errors[idx] = arg(args, 0)
				remaining--
				if remaining == 0 {
					reject(i.makeAggregateError(
						i.newArray(append([]Value{}, errors...)),
						"All promises were rejected",
					))
				}
				return Undef, nil
			}))
	}
	return resultP, nil
}

// makeAggregateError constructs a minimal AggregateError-like object carrying
// an .errors array and a .message string.  A full AggregateError subclass is
// not yet installed as a global; this object inherits from Error.prototype.
func (i *Interpreter) makeAggregateError(errors *Object, message string) Value {
	obj := NewObject(i.errorProto)
	obj.class = "Error"
	obj.SetHidden("name", String("AggregateError"))
	obj.SetHidden("message", String(message))
	obj.SetHidden("errors", errors)
	obj.SetHidden("stack", String("AggregateError: "+message))
	return obj
}
