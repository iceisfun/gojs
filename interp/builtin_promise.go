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

	// handled records whether a reaction was ever attached to this promise
	// (PerformPromiseThen). It drives unhandled-rejection tracking: a promise that
	// rejects while unhandled is reported unless a handler is attached later.
	handled bool
}

// rejectionRecord notes a promise that rejected while it had no handler, for
// HostPromiseRejectionTracker-style unhandled-rejection reporting. The state
// pointer lets a later handler (which sets state.handled) retroactively clear it.
type rejectionRecord struct {
	state  *promiseState
	reason Value
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
	// Promise.prototype[Symbol.toStringTag] = "Promise" (§27.2.5.5).
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("Promise"), Writable: false, Enumerable: false, Configurable: true})

	// -----------------------------------------------------------------------
	// Promise.prototype.then(onFulfilled, onRejected)
	//
	// Registers fulfil/reject reactions and returns a new promise that
	// settles based on the handler return values.  §27.2.5.4
	// -----------------------------------------------------------------------
	i.defineMethod(proto, "then", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		p, err := i.requirePromise(ctx, this, "then")
		if err != nil {
			return nil, err
		}
		// C = SpeciesConstructor(p, %Promise%); result = NewPromiseCapability(C).
		c, err := i.speciesConstructor(ctx, p, i.promiseCtor)
		if err != nil {
			return nil, err
		}
		cap, err := i.newPromiseCapability(ctx, c)
		if err != nil {
			return nil, err
		}
		resolve, reject := i.capabilityResolvers(cap)
		i.performPromiseThen(p, arg(args, 0), arg(args, 1), resolve, reject)
		return cap.promise, nil
	})

	// -----------------------------------------------------------------------
	// Promise.prototype.catch(onRejected) — §27.2.5.1
	//
	// Generic: Invoke(this, "then", « undefined, onRejected »).
	// -----------------------------------------------------------------------
	i.defineMethod(proto, "catch", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.invokeMethod(ctx, this, "then", []Value{Undef, arg(args, 0)})
	})

	// -----------------------------------------------------------------------
	// Promise.prototype.finally(onFinally) — §27.2.5.3
	//
	// Generic in `this` (only requires an object): computes the species
	// constructor, wraps onFinally so the original value/reason passes through,
	// and dispatches through Invoke(this, "then", ...).
	// -----------------------------------------------------------------------
	i.defineMethod(proto, "finally", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		p, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Promise.prototype.finally called on non-object")
		}
		c, err := i.speciesConstructor(ctx, p, i.promiseCtor)
		if err != nil {
			return nil, err
		}
		onFinally := arg(args, 0)
		thenArg0, thenArg1 := onFinally, onFinally
		if fn, isFn := onFinally.(*Object); isFn && fn.IsCallable() {
			// thenFinally:  v => PromiseResolve(C, onFinally()).then(() => v)
			thenArg0 = i.newNativeFunc("", 1, func(ctx context.Context, _ Value, a []Value) (Value, error) {
				v := arg(a, 0)
				result, err := i.call(ctx, onFinally, Undef, nil)
				if err != nil {
					return nil, err
				}
				rp, err := i.promiseResolve(ctx, c, result)
				if err != nil {
					return nil, err
				}
				valueThunk := i.newNativeFunc("", 0, func(_ context.Context, _ Value, _ []Value) (Value, error) {
					return v, nil
				})
				return i.invokeThenValue(ctx, rp, valueThunk)
			})
			// catchFinally: r => PromiseResolve(C, onFinally()).then(() => { throw r })
			thenArg1 = i.newNativeFunc("", 1, func(ctx context.Context, _ Value, a []Value) (Value, error) {
				r := arg(a, 0)
				result, err := i.call(ctx, onFinally, Undef, nil)
				if err != nil {
					return nil, err
				}
				rp, err := i.promiseResolve(ctx, c, result)
				if err != nil {
					return nil, err
				}
				thrower := i.newNativeFunc("", 0, func(_ context.Context, _ Value, _ []Value) (Value, error) {
					return nil, NewThrow(r)
				})
				return i.invokeThenValue(ctx, rp, thrower)
			})
		}
		thenFn, err := i.getMethodStr2(ctx, this, "then")
		if err != nil {
			return nil, err
		}
		return thenFn.fn.call(ctx, this, []Value{thenArg0, thenArg1})
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
	constructFn := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		executor := arg(args, 0)
		if fn, ok := executor.(*Object); !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Promise resolver must be a function")
		}
		// OrdinaryCreateFromConstructor: derive the prototype from new.target
		// (§27.2.3.1 step 3).  A throwing "prototype" getter must propagate
		// before the executor runs.
		proto, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.promiseProto })
		if err != nil {
			return nil, err
		}
		pObj, resolve, reject := i.newPromise()
		if proto != i.promiseProto {
			pObj.SetProto(proto)
		}
		// The resolving functions are anonymous built-ins (name "") per
		// CreateResolvingFunctions (§27.2.1.3).
		resolveFn := i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
			resolve(arg(args, 0))
			return Undef, nil
		})
		rejectFn := i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
			reject(arg(args, 0))
			return Undef, nil
		})
		_, err = i.call(ctx, executor, Undef, []Value{resolveFn, rejectFn})
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
	i.defineSpeciesGetter(ctor)

	// get Promise[Symbol.species] returns the this value (§27.2.4.7), so that
	// subclasses inherit the base constructor for SpeciesConstructor.
	i.defineSpeciesGetter(ctor)

	// -----------------------------------------------------------------------
	// Promise.resolve(value) — §27.2.4.7 → PromiseResolve(C, value)
	//
	// C is the `this` value and must be an object. If value is a promise whose
	// constructor is C, it is returned unchanged; otherwise a new capability of
	// C is created and resolved with value.
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "resolve", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		c, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Promise.resolve called on non-object")
		}
		return i.promiseResolve(ctx, c, arg(args, 0))
	})

	// -----------------------------------------------------------------------
	// Promise.reject(reason) — §27.2.4.6
	//
	// Builds a capability of `this` and rejects it with reason.
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "reject", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		c, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Promise.reject called on non-object")
		}
		cap, err := i.newPromiseCapability(ctx, c)
		if err != nil {
			return nil, err
		}
		if _, err := i.call(ctx, cap.reject, Undef, []Value{arg(args, 0)}); err != nil {
			return nil, err
		}
		return cap.promise, nil
	})

	// -----------------------------------------------------------------------
	// Promise.all / allSettled / race / any — §27.2.4.1-.4
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "all", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.promiseAllStatic(ctx, this, arg(args, 0))
	})
	i.defineMethod(ctor, "allSettled", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.promiseAllSettledStatic(ctx, this, arg(args, 0))
	})
	// Promise.allKeyed / allSettledKeyed (await-dictionary proposal): the same
	// combinators over an object's own enumerable keys instead of an iterable.
	i.defineMethod(ctor, "allKeyed", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.promiseAllKeyedStatic(ctx, this, arg(args, 0))
	})
	i.defineMethod(ctor, "allSettledKeyed", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.promiseAllSettledKeyedStatic(ctx, this, arg(args, 0))
	})
	i.defineMethod(ctor, "race", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.promiseRaceStatic(ctx, this, arg(args, 0))
	})
	i.defineMethod(ctor, "any", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.promiseAnyStatic(ctx, this, arg(args, 0))
	})

	// -----------------------------------------------------------------------
	// Promise.withResolvers() — §27.2.4.8
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "withResolvers", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		c, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Promise.withResolvers called on non-object")
		}
		cap, err := i.newPromiseCapability(ctx, c)
		if err != nil {
			return nil, err
		}
		obj := NewObject(i.objectProto)
		obj.SetData("promise", cap.promise)
		obj.SetData("resolve", cap.resolve)
		obj.SetData("reject", cap.reject)
		return obj, nil
	})

	// -----------------------------------------------------------------------
	// Promise.try(callback, ...args) — §27.2.4.7 (ES2025)
	// -----------------------------------------------------------------------
	i.defineMethod(ctor, "try", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		c, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Promise.try called on non-object")
		}
		cap, err := i.newPromiseCapability(ctx, c)
		if err != nil {
			return nil, err
		}
		callback := arg(args, 0)
		var rest []Value
		if len(args) > 1 {
			rest = args[1:]
		}
		result, cbErr := i.call(ctx, callback, Undef, rest)
		if cbErr != nil {
			return i.ifAbruptRejectPromise(ctx, cap, cbErr)
		}
		if _, err := i.call(ctx, cap.resolve, Undef, []Value{result}); err != nil {
			return nil, err
		}
		return cap.promise, nil
	})

	i.promiseCtor = ctor
	i.setGlobalHidden("Promise", ctor)
}

// promiseResolve implements PromiseResolve(C, x) (§27.2.4.7.1).
func (i *Interpreter) promiseResolve(ctx context.Context, c *Object, x Value) (Value, error) {
	if xo, ok := x.(*Object); ok && xo.class == "Promise" {
		xctor, err := xo.GetStr(ctx, "constructor")
		if err != nil {
			return nil, err
		}
		if xctor == Value(c) {
			return xo, nil
		}
	}
	cap, err := i.newPromiseCapability(ctx, c)
	if err != nil {
		return nil, err
	}
	if _, err := i.call(ctx, cap.resolve, Undef, []Value{x}); err != nil {
		return nil, err
	}
	return cap.promise, nil
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
		// HostPromiseRejectionTracker "reject" (§27.2.1.9): a promise that rejects
		// with no handler yet attached is a candidate unhandled rejection. If a
		// handler is attached later, performPromiseThen sets state.handled and the
		// candidate is filtered out when the host collects the survivors.
		if !state.handled {
			i.unhandledRejections = append(i.unhandledRejections, rejectionRecord{state, reason})
		}
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
						// These are anonymous built-ins (name "") per
						// CreateResolvingFunctions (§27.2.1.3).
						innerRes, innerRej := makeResolvers()
						innerResFn := i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
							innerRes(arg(args, 0))
							return Undef, nil
						})
						innerRejFn := i.newNativeFunc("", 1, func(_ context.Context, _ Value, args []Value) (Value, error) {
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

// promiseThen registers fulfill/reject reactions on p and returns a new native
// promise whose settlement is driven by the reactions. It is the internal fast
// path used by async/await and Promise.prototype.finally where the result
// promise is always a plain %Promise%.
func (i *Interpreter) promiseThen(p *Object, onFulfilled, onRejected Value) *Object {
	resultP, resolve, reject := i.newPromise()
	i.performPromiseThen(p, onFulfilled, onRejected, resolve, reject)
	return resultP
}

// performPromiseThen implements PerformPromiseThen (§27.2.5.4.1), wiring the
// fulfill/reject reactions to the supplied resolve/reject functions of the
// result capability.
func (i *Interpreter) performPromiseThen(p *Object, onFulfilled, onRejected Value, resolve, reject func(Value)) {
	state := i.getPromiseState(p)

	// HostPromiseRejectionTracker "handle": attaching a reaction marks the promise
	// handled, so a rejection that was recorded as a candidate unhandled rejection
	// is no longer reported. The rejection propagates instead to the derived
	// promise, which is tracked on its own if it too rejects unhandled.
	state.handled = true

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
}

// capabilityResolvers adapts a promise capability's callable resolve/reject into
// the func(Value) shape used by the reaction machinery.
func (i *Interpreter) capabilityResolvers(cap *promiseCapability) (resolve, reject func(Value)) {
	resolve = func(v Value) { _, _ = i.call(i.ctx, cap.resolve, Undef, []Value{v}) }
	reject = func(v Value) { _, _ = i.call(i.ctx, cap.reject, Undef, []Value{v}) }
	return resolve, reject
}

// requirePromise performs the RequireInternalSlot([[PromiseState]]) brand check:
// this must be an object carrying a promise state slot.
func (i *Interpreter) requirePromise(ctx context.Context, this Value, method string) (*Object, error) {
	if p, ok := this.(*Object); ok && p.internal != nil {
		if _, ok := p.internal["state"].(*promiseState); ok {
			return p, nil
		}
	}
	return nil, i.throwError(ctx, "TypeError", "Promise.prototype."+method+" called on non-Promise")
}

// invokeMethod implements Invoke(V, P, args) (§7.3.18): GetV (which boxes a
// primitive via ToObject), require callable, then Call with V as this.
func (i *Interpreter) invokeMethod(ctx context.Context, v Value, name string, args []Value) (Value, error) {
	o, err := i.ToObject(ctx, v)
	if err != nil {
		return nil, err
	}
	fnV, err := o.GetStr(ctx, name)
	if err != nil {
		return nil, err
	}
	fn, ok := fnV.(*Object)
	if !ok || !fn.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", name+" is not a function")
	}
	return fn.fn.call(ctx, v, args)
}

// invokeThenValue performs Invoke(promise, "then", « onFulfilled »).
func (i *Interpreter) invokeThenValue(ctx context.Context, promise Value, onFulfilled Value) (Value, error) {
	thenFn, err := i.getMethodStr2(ctx, promise, "then")
	if err != nil {
		return nil, err
	}
	return thenFn.fn.call(ctx, promise, []Value{onFulfilled})
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

// awaitResolve implements PromiseResolve(%Promise%, v) for the Await abstract
// operation (§27.2.4.7 / §6.2.4): when v is already a native promise whose
// constructor is the intrinsic %Promise%, it is returned unchanged; otherwise v
// is wrapped in a freshly resolved promise. The short-circuit is what makes
// `await p` cost a single microtask tick — re-wrapping a native promise would
// interpose an extra thenable-adoption tick — and it also means the internal
// PerformPromiseThen (not a monkey-patched p.then) drives the resumption.
func (i *Interpreter) awaitResolve(v Value) *Object {
	if p, ok := v.(*Object); ok && p.class == "Promise" {
		if ctor, err := p.GetStr(i.ctx, "constructor"); err == nil && ctor == Value(i.promiseCtor) {
			return p
		}
	}
	return i.promiseResolveValue(v)
}
