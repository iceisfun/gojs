package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// Async generators (ECMA-262 §27.6) combine generators and async functions:
// the body runs on a coroutine (see startCoroutine) where `yield` delivers a
// value to the consumer and `await` suspends transparently. Unlike a sync
// generator, next/return/throw return promises and are driven through a request
// queue so overlapping calls are serialized (AsyncGeneratorEnqueue /
// AsyncGeneratorResumeNext, §27.6.3.4 / §27.6.3.5).

// agState is the [[AsyncGeneratorState]] of an async generator instance.
type agState int

const (
	agSuspendedStart agState = iota
	agSuspendedYield
	agExecuting
	agDrainingQueue
	agCompleted
)

// asyncGenReq is one queued next/return/throw request together with the promise
// capability that reports its result.
type asyncGenReq struct {
	completion resumeMsg
	resolve    func(Value)
	reject     func(Value)
}

// asyncGenDriver owns an async generator instance's request queue and state,
// stepping the underlying coroutine. All of its methods run on the VM goroutine
// (from a native method call or a promise reaction), so no locking is needed.
type asyncGenDriver struct {
	i       *Interpreter
	advance func(resumeMsg) yieldMsg
	state   agState
	queue   []*asyncGenReq
}

// enqueue adds a request and dispatches it per the %AsyncGeneratorPrototype%
// next/return/throw algorithms (ECMA-262 §27.6.1.2/.3/.4), returning its
// promise. next/return/throw differ in how they treat a completed or
// suspended-start generator, so dispatch is by completion mode rather than a
// single shared resume path.
func (d *asyncGenDriver) enqueue(c resumeMsg) Value {
	p, resolve, reject := d.i.newPromise()
	req := &asyncGenReq{completion: c, resolve: resolve, reject: reject}
	switch c.mode {
	case resumeNext:
		// A completed generator answers next() with { undefined, true } at once,
		// without queuing.
		if d.state == agCompleted {
			resolve(d.i.createIterResult(Undef, true))
			return p
		}
		d.queue = append(d.queue, req)
		if d.state == agSuspendedStart || d.state == agSuspendedYield {
			d.resume(c)
		}
	case resumeReturn:
		d.queue = append(d.queue, req)
		switch d.state {
		case agSuspendedStart, agCompleted:
			// The body never resumes; await the return value and then close
			// (AsyncGeneratorAwaitReturn).
			d.state = agDrainingQueue
			d.awaitReturn()
		case agSuspendedYield:
			// Resume the suspended yield with a return completion, which the body
			// unwraps by awaiting the value (AsyncGeneratorUnwrapYieldResumption).
			d.resume(c)
		}
	case resumeThrow:
		// throw() on a suspended-start generator closes it without resuming.
		if d.state == agSuspendedStart {
			d.state = agCompleted
		}
		if d.state == agCompleted {
			reject(c.value)
			return p
		}
		d.queue = append(d.queue, req)
		if d.state == agSuspendedYield {
			d.resume(c)
		}
	}
	return p
}

// resume runs the coroutine one step from a suspended state (AsyncGeneratorResume).
func (d *asyncGenDriver) resume(c resumeMsg) {
	d.state = agExecuting
	d.step(c)
}

// completeStep settles the front request (AsyncGeneratorCompleteStep): a throw
// rejects it, otherwise it resolves with a { value, done } iterator result.
func (d *asyncGenDriver) completeStep(isThrow bool, value Value, done bool) {
	req := d.queue[0]
	d.queue = d.queue[1:]
	if isThrow {
		req.reject(value)
	} else {
		req.resolve(d.i.createIterResult(value, done))
	}
}

// step resumes the coroutine once and dispatches on how it next suspends:
// completion drains the queue, an await schedules a resume when the awaited
// promise settles, and a yield delivers a result to the front request.
func (d *asyncGenDriver) step(msg resumeMsg) {
	res := d.advance(msg)
	switch {
	case res.done:
		// The body returned or threw: settle the front request with the final
		// completion and drain any requests queued while it ran.
		d.state = agDrainingQueue
		if res.err != nil {
			if tv, ok := ThrownValue(res.err); ok {
				d.completeStep(true, tv, true)
			} else {
				d.completeStep(true, String(res.err.Error()), true)
			}
		} else {
			d.completeStep(false, res.value, true)
		}
		d.drainQueue()
	case res.awaited:
		// An `await` inside the body: resolve the operand and resume when it
		// settles, without delivering anything to the consumer. The request
		// stays at the front of the queue.
		awaited := d.i.awaitResolve(res.value)
		onFulfilled := d.i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			d.step(resumeMsg{value: arg(a, 0), mode: resumeNext})
			return Undef, nil
		})
		onRejected := d.i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			d.step(resumeMsg{value: arg(a, 0), mode: resumeThrow})
			return Undef, nil
		})
		d.i.promiseThen(awaited, onFulfilled, onRejected)
	default:
		// A `yield` (AsyncGeneratorYield): hand { value, done:false } to the front
		// request. If more requests queued while executing, continue without
		// suspending and serve the next one; otherwise suspend at the yield.
		d.completeStep(false, res.value, false)
		if len(d.queue) > 0 {
			d.state = agExecuting
			d.step(d.queue[0].completion)
			return
		}
		d.state = agSuspendedYield
	}
}

// drainQueue settles queued requests after the body has finished
// (AsyncGeneratorDrainQueue). It stops to await a return request's value; a
// normal request yields { undefined, true } and a throw rejects.
func (d *asyncGenDriver) drainQueue() {
	for len(d.queue) > 0 {
		c := d.queue[0].completion
		switch c.mode {
		case resumeReturn:
			d.awaitReturn()
			return
		case resumeThrow:
			d.completeStep(true, c.value, true)
		default:
			d.completeStep(false, Undef, true)
		}
	}
	d.state = agCompleted
}

// awaitReturn awaits a return request's value before closing the generator
// (AsyncGeneratorAwaitReturn). PromiseResolve(%Promise%, value) may throw (e.g.
// a broken-promise `constructor` getter), which settles the request as a
// rejection; otherwise the resolved value is delivered once the promise settles.
func (d *asyncGenDriver) awaitReturn() {
	value := d.queue[0].completion.value
	promise, err := d.i.promiseResolve(d.i.ctx, d.i.promiseCtor, value)
	if err != nil {
		if tv, ok := ThrownValue(err); ok {
			d.completeStep(true, tv, true)
		} else {
			d.completeStep(true, String(err.Error()), true)
		}
		d.drainQueue()
		return
	}
	po, _ := promise.(*Object)
	onFulfilled := d.i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
		d.completeStep(false, arg(a, 0), true)
		d.drainQueue()
		return Undef, nil
	})
	onRejected := d.i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
		d.completeStep(true, arg(a, 0), true)
		d.drainQueue()
		return Undef, nil
	})
	d.i.promiseThen(po, onFulfilled, onRejected)
}

// makeAsyncGenerator builds an async generator object: calling an async
// generator function returns it without running the body, which advances lazily
// as next/return/throw requests arrive.
func (i *Interpreter) makeAsyncGenerator(fnObj *Object, def *ast.FuncDef, closure *Environment, homeObj *Object, this Value, args []Value, selfBind bool) (Value, error) {
	_, advance, err := i.startCoroutine(fnObj, def, closure, homeObj, this, args, false, selfBind)
	if err != nil {
		return nil, err
	}
	// OrdinaryCreateFromConstructor(fnObj, "%AsyncGeneratorPrototype%"): the
	// instance's [[Prototype]] is the async generator function's own .prototype
	// object (which itself inherits from %AsyncGeneratorPrototype%), falling back
	// to the intrinsic when that property is not an object.
	instProto := i.asyncGeneratorProto
	if fnObj != nil {
		if p, ok := fnObj.getOwn(StrKey("prototype")); ok && !p.Accessor {
			if po, isObj := p.Value.(*Object); isObj {
				instProto = po
			}
		}
	}
	obj := NewObject(instProto)
	obj.class = "AsyncGenerator"
	obj.internal = map[string]any{"AsyncGenerator": &asyncGenDriver{i: i, advance: advance, state: agSuspendedStart}}
	return obj, nil
}

// asyncGenDriverOf returns the driver backing an async generator receiver, or
// nil when this is not an async generator.
func asyncGenDriverOf(this Value) *asyncGenDriver {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	d, _ := o.internal["AsyncGenerator"].(*asyncGenDriver)
	return d
}

// initAsyncGenerator installs %AsyncIteratorPrototype% and
// %AsyncGeneratorPrototype% with next/return/throw (which always return a
// promise) and the async-iterator hooks.
func (i *Interpreter) initAsyncGenerator() {
	// %AsyncIteratorPrototype% [ @@asyncIterator ] () { return this }
	i.asyncIteratorProto.defineOwn(SymKey(i.symAsyncIterator), &Property{
		Value:        i.newNativeFunc("[Symbol.asyncIterator]", 0, func(_ context.Context, this Value, _ []Value) (Value, error) { return this, nil }),
		Writable:     true,
		Configurable: true,
	})

	proto := i.asyncGeneratorProto

	// rejectedPromise returns an already-rejected promise; next/return/throw
	// report a receiver-brand mismatch this way rather than throwing.
	rejectedPromise := func(reason Value) Value {
		p, _, reject := i.newPromise()
		reject(reason)
		return p
	}
	method := func(name string, mode resumeMode) {
		i.defineMethod(proto, name, 1, func(_ context.Context, this Value, a []Value) (Value, error) {
			d := asyncGenDriverOf(this)
			if d == nil {
				return rejectedPromise(i.newError("TypeError", "AsyncGenerator.prototype."+name+" called on a non-AsyncGenerator object")), nil
			}
			return d.enqueue(resumeMsg{value: arg(a, 0), mode: mode}), nil
		})
	}
	method("next", resumeNext)
	method("return", resumeReturn)
	method("throw", resumeThrow)

	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("AsyncGenerator"), Writable: false, Enumerable: false, Configurable: true})
}
