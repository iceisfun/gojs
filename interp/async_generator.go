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

// enqueue adds a request (AsyncGeneratorEnqueue) and returns its promise.
func (d *asyncGenDriver) enqueue(c resumeMsg) Value {
	p, resolve, reject := d.i.newPromise()
	d.queue = append(d.queue, &asyncGenReq{completion: c, resolve: resolve, reject: reject})
	d.resumeNext()
	return p
}

// resumeNext drives the front request (AsyncGeneratorResumeNext). It is a no-op
// while the coroutine is executing (an in-flight await will call back in), and
// settles requests directly once the generator has completed.
func (d *asyncGenDriver) resumeNext() {
	if d.state == agExecuting || len(d.queue) == 0 {
		return
	}
	req := d.queue[0]
	if d.state == agCompleted {
		d.queue = d.queue[1:]
		switch req.completion.mode {
		case resumeThrow:
			req.reject(req.completion.value)
		case resumeReturn:
			req.resolve(d.i.createIterResult(req.completion.value, true))
		default:
			req.resolve(d.i.createIterResult(Undef, true))
		}
		d.resumeNext()
		return
	}
	d.state = agExecuting
	d.step(req.completion)
}

// step resumes the coroutine once and dispatches on how it next suspends:
// completion settles the front request, an await schedules a resume when the
// awaited promise settles, and a yield delivers a result to the front request.
func (d *asyncGenDriver) step(msg resumeMsg) {
	res := d.advance(msg)
	switch {
	case res.done:
		d.state = agCompleted
		req := d.queue[0]
		d.queue = d.queue[1:]
		if res.err != nil {
			if tv, ok := ThrownValue(res.err); ok {
				req.reject(tv)
			} else {
				req.reject(String(res.err.Error()))
			}
		} else {
			req.resolve(d.i.createIterResult(res.value, true))
		}
		d.resumeNext()
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
		// A `yield`: hand { value, done:false } to the front request, then serve
		// the next queued request if any.
		d.state = agSuspendedYield
		req := d.queue[0]
		d.queue = d.queue[1:]
		req.resolve(d.i.createIterResult(res.value, false))
		d.resumeNext()
	}
}

// makeAsyncGenerator builds an async generator object: calling an async
// generator function returns it without running the body, which advances lazily
// as next/return/throw requests arrive.
func (i *Interpreter) makeAsyncGenerator(def *ast.FuncDef, closure *Environment, homeObj *Object, this Value, args []Value) (Value, error) {
	_, advance, err := i.startCoroutine(def, closure, homeObj, this, args, false)
	if err != nil {
		return nil, err
	}
	obj := NewObject(i.asyncGeneratorProto)
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
