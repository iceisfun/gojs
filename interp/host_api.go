package interp

import (
	"context"
	"sync"
)

// This file exposes the host-facing event-loop API. It is the sanctioned way
// for a concurrent host provider (HTTP, filesystem, DB, DNS, subprocess, …) to
// hand results back to the interpreter.
//
// # Threading model
//
// The interpreter is single-threaded: every JavaScript object, environment,
// closure, and iterator is touched by exactly one goroutine — the one that
// calls RunProgram/RunString and, after it, the event loop. A provider may do
// its blocking work on its own goroutine, but it must NOT touch Value/*Object
// state from there. Instead it posts exactly one continuation back onto the
// event loop with [Interpreter.Enqueue] or resolves a promise with
// [Interpreter.ResolvePromise]; that continuation runs on the VM goroutine,
// serialized with all other JavaScript execution.
//
// This mirrors how real JavaScript engines integrate host I/O:
//
//	provider goroutine        VM goroutine (event loop)
//	    do I/O           ─────────────────────────────►
//	    Enqueue(fn)  ── posts ──►  fn() runs here, single-threaded
//
// Timers and promise reactions already use this mechanism internally.

// Enqueue schedules fn to run as a macrotask on the interpreter's event loop.
// It is safe to call from any goroutine. fn runs on the VM goroutine, so it may
// freely read and mutate interpreter state. A non-nil error returned by fn
// aborts the event loop and surfaces from RunProgram/RunString.
//
// Enqueue also registers the continuation as pending work, so a running event
// loop will not exit before fn has had a chance to run — this keeps the process
// alive while an outstanding host operation is in flight (like a pending timer).
func (i *Interpreter) Enqueue(fn func() error) {
	i.loop.addTimer()
	i.loop.pushMacro(func() error {
		i.loop.removeTimer()
		return fn()
	})
}

// Pin registers a unit of outstanding host work so the event loop keeps running
// even when both task queues are momentarily empty — the same mechanism a
// pending timer uses. It returns a release function that removes the
// registration; call it exactly once when the work completes (extra calls are
// no-ops). Use it to hold the loop open for the lifetime of a long-lived host
// resource such as an open socket or event stream, so RunString/RunLoop does not
// return while the resource can still deliver events. Safe to call from any
// goroutine.
func (i *Interpreter) Pin() (release func()) {
	i.loop.addTimer()
	var once sync.Once
	return func() {
		once.Do(i.loop.removeTimer)
	}
}

// QueueMicrotask schedules fn to run as a microtask: after the current task and
// before the next macrotask. Promise reactions use this. Safe to call from any
// goroutine; fn runs on the VM goroutine.
func (i *Interpreter) QueueMicrotask(fn func() error) {
	i.loop.pushMicro(fn)
}

// PromiseCapability is a promise together with Go functions to settle it. Host
// providers create one, hand the promise to the script, and later — from any
// goroutine — call Resolve or Reject (which internally marshal the settlement
// onto the VM goroutine via the microtask queue).
type PromiseCapability struct {
	Promise *Object
	resolve func(Value)
	reject  func(Value)
	i       *Interpreter
}

// NewPromiseCapability creates a pending promise and its settlement functions.
// It must be called on the VM goroutine (e.g. from inside a native function),
// but the returned Resolve/Reject may be called from any goroutine.
func (i *Interpreter) NewPromiseCapability() *PromiseCapability {
	p, res, rej := i.newPromise()
	return &PromiseCapability{Promise: p, resolve: res, reject: rej, i: i}
}

// Resolve settles the promise with value. Safe to call from any goroutine: the
// settlement is scheduled onto the VM event loop, preserving single-threaded
// access to interpreter state.
func (c *PromiseCapability) Resolve(value Value) {
	c.i.QueueMicrotask(func() error {
		c.resolve(value)
		return nil
	})
}

// Reject settles the promise as rejected with reason. Safe from any goroutine.
func (c *PromiseCapability) Reject(reason Value) {
	c.i.QueueMicrotask(func() error {
		c.reject(reason)
		return nil
	})
}

// ResolvePromise settles a promise object previously created by
// NewPromiseCapability's Promise (or any native promise), from any goroutine.
// It is a convenience for providers that pass the *Object around rather than the
// capability.
func (i *Interpreter) ResolvePromise(p *Object, value Value) {
	i.QueueMicrotask(func() error {
		i.settleNativePromise(p, value, false)
		return nil
	})
}

// RejectPromise settles a promise object as rejected, from any goroutine.
func (i *Interpreter) RejectPromise(p *Object, reason Value) {
	i.QueueMicrotask(func() error {
		i.settleNativePromise(p, reason, true)
		return nil
	})
}

// RunLoop drains the event loop until there is no pending work (no queued tasks
// and no outstanding timers/enqueued continuations). Embedders that drive the
// interpreter manually — rather than via RunString — call this after posting
// work. RunString already calls it.
func (i *Interpreter) RunLoop(_ context.Context) error {
	return i.loop.run()
}
