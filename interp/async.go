package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// This file implements async functions and the await operator.
//
// An async function is driven exactly like a generator: its body runs on a
// coroutine goroutine (see startCoroutine), and each `await x` suspends the body
// just as `yield` does — handing x out to a driver that runs on the VM
// goroutine. The driver converts x to a promise and, when that promise settles,
// resumes the coroutine as a microtask. Calling the async function returns a
// promise that settles when the body returns or throws.
//
// Because the coroutine and the driver never run simultaneously (each blocks
// while the other runs), the single-threaded interpreter invariant holds — the
// same reason generators are safe.

// asyncRun invokes an async function: it starts the body coroutine and returns a
// promise for its eventual completion, driving awaits through the microtask
// queue.
func (i *Interpreter) asyncRun(fnObj *Object, def *ast.FuncDef, closure *Environment, homeObj *Object, this Value, args []Value, arrow bool) (Value, error) {
	_, advance, err := i.startCoroutine(fnObj, def, closure, homeObj, this, args, arrow)
	if err != nil {
		// An error raised while instantiating the async function (e.g. a
		// parameter-default expression that throws, or a direct-eval early error
		// in a default) rejects the returned promise rather than throwing to the
		// caller (ECMA-262 AsyncFunctionStart / OrdinaryCallEvaluateBody: the
		// abrupt completion is delivered through the promise capability). A
		// non-JS error (e.g. cancellation) still propagates.
		if tv, ok := ThrownValue(err); ok {
			promise, _, reject := i.newPromise()
			reject(tv)
			return promise, nil
		}
		return nil, err
	}

	promise, resolve, reject := i.newPromise()

	// drive advances the coroutine one step. On an await suspension it wires the
	// awaited value's promise to resume the coroutine; on completion it settles
	// the returned promise.
	var drive func(resumeMsg)
	drive = func(msg resumeMsg) {
		res := advance(msg)
		if res.done {
			switch {
			case res.err != nil:
				// The body threw (or the context was cancelled). Reject with the
				// thrown JS value when there is one.
				if tv, ok := ThrownValue(res.err); ok {
					reject(tv)
				} else {
					reject(String(res.err.Error()))
				}
			default:
				resolve(res.value)
			}
			return
		}

		// res.value is the awaited operand. Resolve it to a promise and schedule
		// resumption when it settles. Promise reactions already run as
		// microtasks, so this preserves async ordering. awaitResolve short-
		// circuits a native promise (returning it unchanged) so `await p` costs
		// exactly one microtask tick, matching Await's PromiseResolve step.
		awaited := i.awaitResolve(res.value)
		onFulfilled := i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			drive(resumeMsg{value: arg(a, 0), mode: resumeNext})
			return Undef, nil
		})
		onRejected := i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			drive(resumeMsg{value: arg(a, 0), mode: resumeThrow})
			return Undef, nil
		})
		i.promiseThen(awaited, onFulfilled, onRejected)
	}

	// Kick off: the first resume value is ignored by the coroutine.
	drive(resumeMsg{mode: resumeNext})
	return promise, nil
}

// runNativeAsync runs body as an async function whose body is implemented in Go
// rather than as a JS FuncDef. body receives an `await` callback it can invoke to
// suspend on a value and resume with the settled result (or a thrown JS value on
// rejection), exactly as the `await` operator does inside a JS async function. It
// returns a Promise that settles when body returns (fulfil) or errors (reject).
//
// It mirrors asyncRun + startCoroutine: the body runs on a dedicated coroutine
// goroutine and each await suspension is driven through the microtask queue, so
// the single-threaded interpreter invariant holds (the driver and the body never
// run simultaneously). This backs native async builtins such as Array.fromAsync.
func (i *Interpreter) runNativeAsync(body func(ctx context.Context, await func(Value) (Value, error)) (Value, error)) Value {
	gs := &generatorState{
		resume: make(chan resumeMsg),
		out:    make(chan yieldMsg),
		ctx:    i.ctx,
	}
	await := func(v Value) (Value, error) { return i.doAwait(gs, v) }

	started := false
	start := func() {
		started = true
		i.wg.Add(1)
		go func() {
			defer i.wg.Done()
			select {
			case <-gs.resume: // first resume value is ignored, per spec
			case <-gs.ctx.Done():
				return
			}
			val, err := body(gs.ctx, await)
			var final yieldMsg
			if err != nil {
				final = yieldMsg{err: err, done: true}
			} else {
				final = yieldMsg{value: val, done: true}
			}
			select {
			case gs.out <- final:
			case <-gs.ctx.Done():
			}
		}()
	}

	advance := func(msg resumeMsg) yieldMsg {
		if gs.done {
			return yieldMsg{value: Undef, done: true}
		}
		if !started {
			start()
		}
		gs.executing = true
		select {
		case gs.resume <- msg:
		case <-gs.ctx.Done():
			gs.executing = false
			gs.done = true
			return yieldMsg{done: true, err: gs.ctx.Err()}
		}
		select {
		case res := <-gs.out:
			gs.executing = false
			if res.done {
				gs.done = true
			}
			return res
		case <-gs.ctx.Done():
			gs.executing = false
			gs.done = true
			return yieldMsg{done: true, err: gs.ctx.Err()}
		}
	}

	promise, resolve, reject := i.newPromise()
	var drive func(resumeMsg)
	drive = func(msg resumeMsg) {
		res := advance(msg)
		if res.done {
			if res.err != nil {
				if tv, ok := ThrownValue(res.err); ok {
					reject(tv)
				} else {
					reject(String(res.err.Error()))
				}
			} else {
				resolve(res.value)
			}
			return
		}
		awaited := i.awaitResolve(res.value)
		onFulfilled := i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			drive(resumeMsg{value: arg(a, 0), mode: resumeNext})
			return Undef, nil
		})
		onRejected := i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
			drive(resumeMsg{value: arg(a, 0), mode: resumeThrow})
			return Undef, nil
		})
		i.promiseThen(awaited, onFulfilled, onRejected)
	}
	drive(resumeMsg{mode: resumeNext})
	return promise
}

// evalAwait implements the await operator. Inside an async function body it
// suspends the coroutine (handing the operand to the async driver) and resumes
// with the settled value, or throws the rejection reason. Outside any coroutine
// (top-level await, not supported) it falls back to synchronously unwrapping a
// settled promise so simple scripts still behave sensibly.
func (i *Interpreter) evalAwait(ctx context.Context, e *ast.AwaitExpr, env *Environment) (Value, error) {
	operand, err := i.evalExpr(ctx, e.Argument, env)
	if err != nil {
		return nil, err
	}
	gs := env.generator()
	if gs == nil {
		// Top-level await: best-effort synchronous unwrap of a settled promise.
		if p, ok := operand.(*Object); ok && p.class == "Promise" {
			if st, ok := p.internal["state"].(*promiseState); ok && st.status != promisePending {
				if st.status == promiseRejected {
					return nil, NewThrow(st.result)
				}
				return st.result, nil
			}
		}
		return operand, nil
	}
	// Suspend: hand the awaited operand to the async driver and resume with the
	// settled value (doAwait turns a rejection into a thrown value). The awaited
	// flag keeps an async generator's driver from treating this as a yield.
	return i.doAwait(gs, operand)
}
