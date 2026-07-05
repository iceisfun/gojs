package interp

import (
	"context"
	"math"
	"math/big"
	"sync"
	"time"
)

// initAtomics installs the Atomics namespace object (§25.4).
//
// gojs runs a single agent — one VM has one thread of control — so the
// read-modify-write operations are atomic by construction: nothing can
// interleave between the read and the write within an agent. The
// add/sub/and/or/xor, exchange, compareExchange, load, store, isLockFree and
// pause operations are fully functional on any integer TypedArray (shared or
// not). SharedArrayBuffer is implemented (Phase 1, single-agent): Atomics.wait,
// waitAsync and notify recognise a shared buffer and use a waiter registry, but
// with only one agent there is never another thread to issue a notify, so the
// only outcomes are "not-equal", "timed-out" and zero woken. On a non-shared
// buffer wait/waitAsync throw a TypeError and notify reports 0. See
// atomicsDoWait for the Phase-1 blocking semantics.
func (i *Interpreter) initAtomics() {
	a := NewObject(i.objectProto)
	a.class = "Atomics"
	// Atomics[Symbol.toStringTag] = "Atomics", { w:false, e:false, c:true } (§25.4.15).
	a.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("Atomics"), Writable: false, Enumerable: false, Configurable: true})

	// The binary read-modify-write operations. numOp acts on integer element
	// types (computing in a wide domain and letting the element write mask to
	// width); bigOp acts on BigInt element types.
	rmw := func(name string, numOp func(old, val int64) int64, bigOp func(old, val *big.Int) *big.Int) {
		i.defineMethod(a, name, 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
			td, idx, err := i.atomicAccess(ctx, args, false)
			if err != nil {
				return nil, err
			}
			if taKinds[td.kind].bigInt {
				bv, err := i.toBigInt(ctx, arg(args, 2))
				if err != nil {
					return nil, err
				}
				defer i.atomicsLock(td)()
				if _, ok := td.validIndex(float64(idx)); !ok {
					return nil, i.throwError(ctx, "TypeError", "Atomics."+name+" on an out-of-bounds TypedArray")
				}
				old := td.getElement(idx).(*BigInt)
				td.setElementBig(idx, bigOp(new(big.Int).Set(old.Int), bv.(*BigInt).Int))
				return old, nil
			}
			val, err := i.atomicIntArg(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			defer i.atomicsLock(td)()
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics."+name+" on an out-of-bounds TypedArray")
			}
			old := td.getElement(idx).(Number)
			td.setElementNum(idx, float64(numOp(int64(float64(old)), val)))
			return old, nil
		})
	}
	rmw("add", func(o, v int64) int64 { return o + v }, func(o, v *big.Int) *big.Int { return o.Add(o, v) })
	rmw("sub", func(o, v int64) int64 { return o - v }, func(o, v *big.Int) *big.Int { return o.Sub(o, v) })
	rmw("and", func(o, v int64) int64 { return o & v }, func(o, v *big.Int) *big.Int { return o.And(o, v) })
	rmw("or", func(o, v int64) int64 { return o | v }, func(o, v *big.Int) *big.Int { return o.Or(o, v) })
	rmw("xor", func(o, v int64) int64 { return o ^ v }, func(o, v *big.Int) *big.Int { return o.Xor(o, v) })

	// exchange(ta, index, value): store value, return the previous value.
	i.defineMethod(a, "exchange", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		if taKinds[td.kind].bigInt {
			bv, err := i.toBigInt(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			defer i.atomicsLock(td)()
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics.exchange on an out-of-bounds TypedArray")
			}
			old := td.getElement(idx)
			td.setElementBig(idx, bv.(*BigInt).Int)
			return old, nil
		}
		val, err := i.atomicNumArg(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		defer i.atomicsLock(td)()
		if _, ok := td.validIndex(float64(idx)); !ok {
			return nil, i.throwError(ctx, "TypeError", "Atomics.exchange on an out-of-bounds TypedArray")
		}
		old := td.getElement(idx)
		td.setElementNum(idx, val)
		return old, nil
	})

	// compareExchange(ta, index, expected, replacement): store replacement only
	// if the current value equals expected (compared in element representation);
	// return the previous value.
	i.defineMethod(a, "compareExchange", 4, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		if taKinds[td.kind].bigInt {
			expBV, err := i.toBigInt(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			repBV, err := i.toBigInt(ctx, arg(args, 3))
			if err != nil {
				return nil, err
			}
			defer i.atomicsLock(td)()
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics.compareExchange on an out-of-bounds TypedArray")
			}
			old := td.getElement(idx).(*BigInt)
			// Compare in the element's stored bit-pattern (mod 2^64).
			if td.bigBits(old.Int) == td.bigBits(expBV.(*BigInt).Int) {
				td.setElementBig(idx, repBV.(*BigInt).Int)
			}
			return old, nil
		}
		expected, err := i.atomicNumArg(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		replacement, err := i.atomicNumArg(ctx, arg(args, 3))
		if err != nil {
			return nil, err
		}
		defer i.atomicsLock(td)()
		if _, ok := td.validIndex(float64(idx)); !ok {
			return nil, i.throwError(ctx, "TypeError", "Atomics.compareExchange on an out-of-bounds TypedArray")
		}
		old := td.getElement(idx).(Number)
		if td.numBits(float64(old)) == td.numBits(expected) {
			td.setElementNum(idx, replacement)
		}
		return old, nil
	})

	// load(ta, index): return the current value.
	i.defineMethod(a, "load", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		defer i.atomicsLock(td)()
		return td.getElement(idx), nil
	})

	// store(ta, index, value): store value, returning the integer value stored
	// (not the previous value).
	i.defineMethod(a, "store", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		if taKinds[td.kind].bigInt {
			bv, err := i.toBigInt(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			defer i.atomicsLock(td)()
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics.store on an out-of-bounds TypedArray")
			}
			td.setElementBig(idx, bv.(*BigInt).Int)
			return bv, nil
		}
		// The spec converts with ToIntegerOrInfinity and returns that integer
		// Number, independent of what the element write truncates it to.
		f, err := i.ToNumberV(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		v := integerOrInfinity(f)
		defer i.atomicsLock(td)()
		if _, ok := td.validIndex(float64(idx)); !ok {
			return nil, i.throwError(ctx, "TypeError", "Atomics.store on an out-of-bounds TypedArray")
		}
		td.setElementNum(idx, v)
		return Number(v), nil
	})

	// isLockFree(size): whether an atomic op on an element of the given byte size
	// is lock-free. gojs treats 1, 2, 4 and 8 as lock-free.
	i.defineMethod(a, "isLockFree", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		n := integerOrInfinity(f)
		return Boolean(n == 1 || n == 2 || n == 4 || n == 8), nil
	})

	// wait(ta, index, value, timeout) — §25.4.11. Valid only on an Int32Array/
	// BigInt64Array over a SharedArrayBuffer; a non-shared buffer throws a
	// TypeError. On a shared buffer it performs a single-agent synchronous wait
	// (see atomicsWaitSync for the Phase-1 blocking semantics).
	i.defineMethod(a, "wait", 4, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.atomicsDoWait(ctx, args, false)
	})

	// notify(ta, index, count) — §25.4.12. Wakes up to count agents waiting on
	// the location, returning the number woken. A non-shared buffer has no
	// waiters and reports 0.
	i.defineMethod(a, "notify", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, true)
		if err != nil {
			return nil, err
		}
		count := -1 // undefined => +Infinity => "all"
		if v := arg(args, 2); !IsUndefined(v) {
			f, err := i.ToNumberV(ctx, v)
			if err != nil {
				return nil, err
			}
			c := integerOrInfinity(f)
			if c < 0 {
				c = 0
			}
			if !math.IsInf(c, 1) {
				count = int(c)
			}
		}
		ab, ok := arrayBufferOf(td.buffer)
		if !ok || !ab.shared {
			return Number(0), nil
		}
		return Number(float64(i.atomicsNotify(ab, idx, count))), nil
	})

	// waitAsync(ta, index, value, timeout) — §25.4.13. Like wait, but never
	// blocks: it returns a result record { async, value }. When the element does
	// not match, or the timeout is 0, it resolves synchronously
	// (async:false, value:"not-equal"/"timed-out"); otherwise async:true with a
	// Promise as value.
	i.defineMethod(a, "waitAsync", 4, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.atomicsDoWait(ctx, args, true)
	})

	// pause(N): a hint that the caller is in a spin-wait loop (§25.4.14). N, when
	// given, must be an integral Number; the value is otherwise ignored. Returns
	// undefined.
	i.defineMethod(a, "pause", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if v := arg(args, 0); v != Undef {
			n, ok := v.(Number)
			if !ok || math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) || float64(n) != math.Trunc(float64(n)) {
				return nil, i.throwError(ctx, "TypeError", "Atomics.pause: iterationNumber must be an integer")
			}
		}
		return Undef, nil
	})

	i.setGlobalHidden("Atomics", a)
}

// atomicAccess implements ValidateIntegerTypedArray followed by the index part
// of ValidateAtomicAccess (§25.4.3): it checks that args[0] is an integer (or,
// when waitable, an Int32Array/BigInt64Array) TypedArray on an in-bounds,
// non-detached buffer, and that args[1] converts to an in-range access index.
// Value coercion (which may run user code and shrink the buffer) is left to the
// caller, which re-validates the index with validIndex before the access.
func (i *Interpreter) atomicAccess(ctx context.Context, args []Value, waitable bool) (*typedArrayData, int, error) {
	td, length, err := i.validateIntegerTA(ctx, args, waitable)
	if err != nil {
		return nil, 0, err
	}
	idx, err := i.validateAtomicIndex(ctx, args, length)
	if err != nil {
		return nil, 0, err
	}
	return td, idx, nil
}

// validateIntegerTA implements ValidateIntegerTypedArray (§25.4.3.1): args[0]
// must be an integer TypedArray (or, when waitable, an Int32Array/BigInt64Array)
// on a non-detached, in-bounds buffer. It returns the record and current element
// length WITHOUT coercing the index — the spec's IsSharedArrayBuffer check for
// wait/waitAsync happens between this and the index coercion (§25.4.11 steps
// 3–4), so the two are split.
func (i *Interpreter) validateIntegerTA(ctx context.Context, args []Value, waitable bool) (*typedArrayData, int, error) {
	td, ok := typedArrayOf(arg(args, 0))
	if !ok {
		return nil, 0, i.throwError(ctx, "TypeError", "Atomics operation called on a non-TypedArray")
	}
	switch {
	case waitable:
		if td.kind != taInt32 && td.kind != taBigInt64 {
			return nil, 0, i.throwError(ctx, "TypeError", "Atomics.wait/notify requires an Int32Array or BigInt64Array")
		}
	case td.kind == taUint8Clamped || td.kind == taFloat32 || td.kind == taFloat64:
		return nil, 0, i.throwError(ctx, "TypeError", "Atomics operation requires an integer TypedArray")
	}
	oob, length := td.outOfBounds()
	if oob {
		return nil, 0, i.throwError(ctx, "TypeError", "Atomics operation called on an out-of-bounds TypedArray")
	}
	return td, length, nil
}

// validateAtomicIndex implements the index part of ValidateAtomicAccess
// (§25.4.3.2): args[1] converts via ToIndex and must be below the element
// length. Coercion may run user code, so callers re-check bounds before the
// access.
func (i *Interpreter) validateAtomicIndex(ctx context.Context, args []Value, length int) (int, error) {
	idx, err := i.toIndex(ctx, arg(args, 1))
	if err != nil {
		return 0, err
	}
	if idx >= length {
		return 0, i.throwError(ctx, "RangeError", "Atomics access index "+NumberToString(float64(idx))+" is out of bounds")
	}
	return idx, nil
}

// atomicNumArg coerces an Atomics value argument for a non-BigInt element to the
// float the element write expects (ToIntegerOrInfinity, per the spec's numeric
// read-modify-write conversion).
func (i *Interpreter) atomicNumArg(ctx context.Context, v Value) (float64, error) {
	f, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	return integerOrInfinity(f), nil
}

// atomicIntArg is atomicNumArg reduced to an int64 for the integer bitwise/
// arithmetic ops; the element write masks the result to the element width.
func (i *Interpreter) atomicIntArg(ctx context.Context, v Value) (int64, error) {
	f, err := i.atomicNumArg(ctx, v)
	if err != nil {
		return 0, err
	}
	switch {
	case f >= 9.223372036854776e18:
		return math.MaxInt64, nil
	case f <= -9.223372036854776e18:
		return math.MinInt64, nil
	}
	return int64(f), nil
}

// numBits returns the element-width bit pattern taWriteNum would store for f, so
// two values can be compared the way the memory does (compareExchange).
func (td *typedArrayData) numBits(f float64) uint64 {
	var buf [8]byte
	size := taKinds[td.kind].size
	taWriteNum(td.kind, buf[:size], f)
	var b uint64
	for k := size - 1; k >= 0; k-- {
		b = b<<8 | uint64(buf[k])
	}
	return b
}

// bigBits returns the low 64 bits of v, the pattern setElementBig stores.
func (td *typedArrayData) bigBits(v *big.Int) uint64 {
	return bigIntToUint64(v)
}

// ---------------------------------------------------------------------------
// Atomics.wait / notify / waitAsync — shared-buffer coordination
// ---------------------------------------------------------------------------
//
// Phase 1 runs a single agent, so there is never another thread to issue the
// notify that would wake a waiter. The machinery below is nonetheless a real
// waiter registry (buffer+index -> waiters) so a future Phase 2 that spawns
// worker agents can wake waiters correctly; in Phase 1 the only outcomes are
// "not-equal" (value mismatch), "timed-out" (finite timeout elapsed, or a zero
// timeout), and — from a notify with no registered waiter — zero woken.
//
// SIMPLIFICATION (Phase 1): a synchronous Atomics.wait with an *infinite*
// timeout would block the agent's single goroutine forever (nothing can notify
// it), i.e. a program-authored deadlock. We honour it literally (block on the
// waiter channel) rather than silently returning; callers should pass a finite
// timeout in a single-agent VM. A finite timeout blocks the goroutine only for
// its duration and then returns "timed-out".

// waiterKey identifies a wait location: a specific buffer and element index.
type waiterKey struct {
	ab  *arrayBufferData
	idx int
}

// sabWaiter is a single parked waiter. A synchronous Atomics.wait waiter is
// woken by closing ch; an Atomics.waitAsync waiter is woken by invoking settle
// (which resolves its Promise with "ok"). notify sets notified so a waiter is
// woken at most once.
type sabWaiter struct {
	ch       chan struct{}        // sync waiter: closed on notify
	notified bool                 // woken (by notify) — guards double wake
	settle   func(outcome string) // async waiter: resolve the Promise once; nil for a sync waiter
}

// waiterList is the per-interpreter registry of parked waiters.
type waiterList struct {
	mu sync.Mutex
	m  map[waiterKey][]*sabWaiter
}

func (i *Interpreter) waiters() *waiterList {
	if i.cluster != nil {
		return i.cluster.waiters
	}
	if i.sabWaiters == nil {
		i.sabWaiters = &waiterList{m: map[waiterKey][]*sabWaiter{}}
	}
	return i.sabWaiters
}

func (reg *waiterList) add(key waiterKey, w *sabWaiter) {
	reg.mu.Lock()
	reg.m[key] = append(reg.m[key], w)
	reg.mu.Unlock()
}

func (reg *waiterList) remove(key waiterKey, w *sabWaiter) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	list := reg.m[key]
	for j, x := range list {
		if x == w {
			reg.m[key] = append(list[:j], list[j+1:]...)
			break
		}
	}
	if len(reg.m[key]) == 0 {
		delete(reg.m, key)
	}
}

// atomicsNotify wakes up to count waiters on (ab, idx), returning the number
// woken. count < 0 means "all".
func (i *Interpreter) atomicsNotify(ab *arrayBufferData, idx, count int) int {
	reg := i.waiters()
	reg.mu.Lock()
	key := waiterKey{ab, idx}
	list := reg.m[key]
	var woken []*sabWaiter
	for len(list) > 0 && (count < 0 || len(woken) < count) {
		w := list[0]
		list = list[1:]
		if !w.notified {
			w.notified = true
			woken = append(woken, w)
		}
	}
	if len(list) == 0 {
		delete(reg.m, key)
	} else {
		reg.m[key] = list
	}
	reg.mu.Unlock()
	// Wake outside the lock: a sync waiter unblocks its goroutine (close), an
	// async waiter resolves its Promise to "ok" (settle schedules on the loop).
	for _, w := range woken {
		if w.settle != nil {
			w.settle("ok")
		} else {
			close(w.ch)
		}
	}
	return len(woken)
}

// atomicsDoWait implements the shared body of Atomics.wait (async=false) and
// Atomics.waitAsync (async=true). It validates the access, requires a shared
// buffer, coerces the expected value and timeout, and either blocks (wait) or
// returns a { async, value } result record (waitAsync).
func (i *Interpreter) atomicsDoWait(ctx context.Context, args []Value, async bool) (Value, error) {
	// §25.4.11 steps 1–4: validate the (Int32/BigInt64) typed array, THEN require
	// a shared buffer, THEN coerce the index. The shared check precedes any
	// user-observable coercion, so a non-shared buffer throws TypeError even when
	// the index/value/timeout arguments have poisoned valueOf hooks.
	td, length, err := i.validateIntegerTA(ctx, args, true)
	if err != nil {
		return nil, err
	}
	ab, ok := arrayBufferOf(td.buffer)
	if !ok || !ab.shared {
		name := "Atomics.wait"
		if async {
			name = "Atomics.waitAsync"
		}
		return nil, i.throwError(ctx, "TypeError", name+" cannot be used on a non-shared ArrayBuffer")
	}
	idx, err := i.validateAtomicIndex(ctx, args, length)
	if err != nil {
		return nil, err
	}

	// Coerce the expected value in the element's domain (§25.4.3.11 / 25.4.3.12).
	var expectedBig *big.Int
	var expectedI32 int32
	bigKind := taKinds[td.kind].bigInt
	if bigKind {
		bv, err := i.toBigInt(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		expectedBig = bv.(*BigInt).Int
	} else {
		f, err := i.ToNumberV(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		expectedI32 = ToInt32(f)
	}

	// Coerce the timeout (§25.4.3.13 ToTimeout): ToNumber, NaN -> +Infinity, then
	// clamp to a non-negative value; the unit is milliseconds.
	tf, err := i.ToNumberV(ctx, arg(args, 3))
	if err != nil {
		return nil, err
	}
	timeoutMs := tf
	if math.IsNaN(timeoutMs) {
		timeoutMs = math.Inf(1)
	} else if timeoutMs < 0 {
		timeoutMs = 0
	}

	// Re-validate the index (value/timeout coercion can run user code; a shared
	// buffer never detaches and never shrinks, but a growable one can move idx
	// back in bounds — validIndex is cheap insurance).
	if _, okIdx := td.validIndex(float64(idx)); !okIdx {
		return nil, i.throwError(ctx, "RangeError", "Atomics wait index is out of bounds")
	}

	// Cross-agent atomicity (§25.4.3 WaitForNotification): the value compare and
	// the waiter registration must be indivisible against a concurrent
	// Atomics.store + Atomics.notify from another agent, or a notify that lands
	// between the two is a lost wakeup. Hold the cluster Atomics lock across
	// compare+register; the wait helpers release it (unlock) the instant the
	// waiter is registered — BEFORE suspending — so a notify arriving after
	// release still wakes us through the waiter channel. For a single-agent VM
	// atomicsLock is a no-op.
	unlock := i.atomicsLock(td)
	matched := false
	if bigKind {
		cur := td.getElement(idx).(*BigInt)
		matched = td.bigBits(cur.Int) == td.bigBits(expectedBig)
	} else {
		cur := int32(float64(td.getElement(idx).(Number)))
		matched = cur == expectedI32
	}

	if async {
		return i.atomicsWaitAsyncResult(ab, idx, matched, timeoutMs, unlock), nil
	}
	return String(i.atomicsWaitSync(ab, idx, matched, timeoutMs, unlock)), nil
}

// atomicsWaitSync performs the blocking wait for Atomics.wait, returning
// "not-equal", "timed-out" or "ok".
func (i *Interpreter) atomicsWaitSync(ab *arrayBufferData, idx int, matched bool, timeoutMs float64, unlock func()) string {
	if !matched {
		unlock()
		return "not-equal"
	}
	if timeoutMs == 0 {
		unlock()
		return "timed-out"
	}
	key := waiterKey{ab, idx}
	w := &sabWaiter{ch: make(chan struct{})}
	i.waiters().add(key, w)
	unlock() // waiter registered: a notify from another agent now reliably targets w

	if math.IsInf(timeoutMs, 1) {
		// Blocks until another agent in the cluster notifies this location. In a
		// single-agent VM no notifier exists, so this is a program-authored
		// deadlock, honoured literally — except we also unblock on context
		// cancellation (the test/embedder deadline) so a torn-down agent's
		// goroutine never leaks forever.
		select {
		case <-w.ch:
			return "ok"
		case <-i.ctx.Done():
			i.waiters().remove(key, w)
			return "timed-out"
		}
	}
	t := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
	defer t.Stop()
	select {
	case <-w.ch:
		return "ok"
	case <-i.ctx.Done():
		i.waiters().remove(key, w)
		return "timed-out"
	case <-t.C:
		i.waiters().remove(key, w)
		return "timed-out"
	}
}

// atomicsWaitAsyncResult builds the { async, value } result record for
// Atomics.waitAsync without ever blocking the agent.
func (i *Interpreter) atomicsWaitAsyncResult(ab *arrayBufferData, idx int, matched bool, timeoutMs float64, unlock func()) Value {
	// The lock spans compare→register (as for the sync wait); for waitAsync the
	// whole record is built without blocking, so release it on return once the
	// waiter (and its timeout timer) are installed.
	defer unlock()
	res := NewObject(i.objectProto)
	if !matched {
		res.SetData("async", Boolean(false))
		res.SetData("value", String("not-equal"))
		return res
	}
	if timeoutMs == 0 {
		res.SetData("async", Boolean(false))
		res.SetData("value", String("timed-out"))
		return res
	}
	// async:true — a genuine pending Promise settled by whichever comes first: a
	// notify on this location (-> "ok") or, for a finite timeout, the timeout
	// firing (-> "timed-out").
	p, resolve, _ := i.newPromise()
	key := waiterKey{ab, idx}
	w := &sabWaiter{ch: make(chan struct{})}
	finite := !math.IsInf(timeoutMs, 1) && i.timer != nil
	// Keep the event loop alive while this async wait is pending, but only when it
	// can actually settle: a finite timeout will fire, or — in an agent cluster —
	// another agent may notify. In a single-agent VM an infinite waitAsync can
	// never settle, so we do NOT hold the loop open: the Promise simply stays
	// pending and the program (RunString) returns, matching single-agent
	// semantics. Without this keepalive a cluster child would drain its loop and
	// exit before the parent's notify arrived, and settle would push a macro onto
	// a dead loop (a lost resolution).
	keepAlive := finite || i.cluster != nil
	if keepAlive {
		i.loop.addTimer()
	}
	var cancelTimer func()
	var once sync.Once
	// settle resolves the Promise exactly once, on the event loop, and tears down
	// the waiter registration, the loop keepalive, and any pending timeout timer.
	w.settle = func(outcome string) {
		once.Do(func() {
			i.loop.pushMacro(func() error {
				i.waiters().remove(key, w)
				if cancelTimer != nil {
					cancelTimer()
				}
				if keepAlive {
					i.loop.removeTimer()
				}
				resolve(String(outcome))
				return nil
			})
		})
	}
	i.waiters().add(key, w)
	if finite {
		cancelTimer = i.timer.AfterFunc(i.ctx, time.Duration(timeoutMs)*time.Millisecond, func() {
			w.settle("timed-out")
		})
	}
	// In a cluster, a torn-down agent (its context cancelled by the host at end of
	// the test) must settle so its kept-alive event loop stops and the goroutine
	// exits, rather than parking forever on a notify that will never come. settle
	// is idempotent, so a normal notify still wins the race.
	if i.cluster != nil {
		go func() {
			<-i.ctx.Done()
			w.settle("timed-out")
		}()
	}
	res.SetData("async", Boolean(true))
	res.SetData("value", p)
	return res
}

// integerOrInfinity implements ToIntegerOrInfinity on an already-computed Number:
// NaN becomes 0, ±Infinity is preserved, and any finite value is truncated
// toward zero.
func integerOrInfinity(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	if math.IsInf(f, 0) {
		return f
	}
	// ToIntegerOrInfinity maps both +0 and -0 to (mathematical) 0, so normalize
	// -0 to +0 (e.g. Atomics.store(i32a, 0, -0) returns +0).
	if f == 0 {
		return 0
	}
	return math.Trunc(f)
}
