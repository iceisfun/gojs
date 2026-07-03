package interp

import "context"

// This file implements the async-iteration protocol: GetIterator(obj, async)
// (ECMA-262 §7.4.2) and the %AsyncFromSyncIteratorPrototype% wrapper (§27.1.4)
// used when an async consumer (for-await-of, async-generator yield*) is handed a
// value that only exposes a synchronous @@iterator. Each wrapped next/return/
// throw awaits the sync result's value and re-wraps { value, done } in a
// resolved promise.

// getAsyncIterator implements GetIterator(obj, async): try @@asyncIterator, and
// if it is absent fall back to the sync @@iterator wrapped in
// %AsyncFromSyncIteratorPrototype%.
func (i *Interpreter) getAsyncIterator(ctx context.Context, obj Value) (*iterRecord, error) {
	method, err := i.getMethod(ctx, obj, i.symAsyncIterator)
	if err != nil {
		return nil, err
	}
	if method != nil {
		iterator, err := i.call(ctx, method, obj, nil)
		if err != nil {
			return nil, err
		}
		itObj, ok := iterator.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "[Symbol.asyncIterator]() returned a non-object")
		}
		return i.getIteratorDirect(ctx, itObj)
	}
	// No @@asyncIterator: obtain the sync iterator and wrap it.
	syncMethod, err := i.getMethod(ctx, obj, i.symIterator)
	if err != nil {
		return nil, err
	}
	if syncMethod == nil {
		return nil, i.throwError(ctx, "TypeError", briefValue(obj)+" is not iterable")
	}
	syncIterator, err := i.call(ctx, syncMethod, obj, nil)
	if err != nil {
		return nil, err
	}
	itObj, ok := syncIterator.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "iterator is not an object")
	}
	syncRec, err := i.getIteratorDirect(ctx, itObj)
	if err != nil {
		return nil, err
	}
	return i.createAsyncFromSyncIterator(syncRec), nil
}

// createAsyncFromSyncIterator implements CreateAsyncFromSyncIterator (§27.1.4.1):
// it builds an %AsyncFromSyncIteratorPrototype% object over a sync iterator
// record and returns the async iterator record wrapping it.
func (i *Interpreter) createAsyncFromSyncIterator(syncRec *iterRecord) *iterRecord {
	o := NewObject(i.asyncFromSyncIterProto)
	o.internal = map[string]any{"AsyncFromSyncSyncRec": syncRec}
	next, _ := o.GetStr(i.ctx, "next")
	return &iterRecord{iterator: o, nextMethod: next, done: false}
}

// asyncFromSyncRecOf returns the wrapped sync iterator record of an
// %AsyncFromSyncIteratorPrototype% object, or nil.
func asyncFromSyncRecOf(v Value) *iterRecord {
	o, ok := v.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	r, _ := o.internal["AsyncFromSyncSyncRec"].(*iterRecord)
	return r
}

// rejectedPromiseVal returns an already-rejected promise for reason.
func (i *Interpreter) rejectedPromiseVal(reason Value) Value {
	p, _, reject := i.newPromise()
	reject(reason)
	return p
}

// asyncFromSyncContinuation implements AsyncFromSyncIteratorContinuation
// (§27.1.4.4): given a sync iterator result, it awaits the result's value and
// resolves to { value, done }. closeOnRejection closes the sync iterator if
// awaiting the value rejects. It returns a promise; a JS abrupt completion is
// surfaced as a rejected promise, while a host error propagates as a Go error.
func (i *Interpreter) asyncFromSyncContinuation(ctx context.Context, result Value, syncRec *iterRecord, closeOnRejection bool) (Value, error) {
	ro, ok := result.(*Object)
	if !ok {
		return i.rejectedPromiseVal(i.newError("TypeError", "iterator result is not an object")), nil
	}
	doneV, err := ro.GetStr(ctx, "done")
	if err != nil {
		if tv, ok := ThrownValue(err); ok {
			return i.rejectedPromiseVal(tv), nil
		}
		return nil, err
	}
	done := ToBoolean(doneV)
	value, err := ro.GetStr(ctx, "value")
	if err != nil {
		if tv, ok := ThrownValue(err); ok {
			return i.rejectedPromiseVal(tv), nil
		}
		return nil, err
	}
	valueWrapper := i.promiseResolveValue(value)
	onFulfilled := i.newNativeFunc("", 1, func(_ context.Context, _ Value, a []Value) (Value, error) {
		return i.createIterResult(arg(a, 0), done), nil
	})
	var onRejected Value = Undef
	if !done && closeOnRejection {
		onRejected = i.newNativeFunc("", 1, func(cctx context.Context, _ Value, a []Value) (Value, error) {
			_ = i.iteratorClose(cctx, syncRec, NewThrow(arg(a, 0)))
			return nil, NewThrow(arg(a, 0))
		})
	}
	return i.promiseThen(valueWrapper, onFulfilled, onRejected), nil
}

// initAsyncFromSyncIterator installs %AsyncFromSyncIteratorPrototype% with
// next/return/throw. It inherits %AsyncIteratorPrototype% so its @@asyncIterator
// returns the iterator itself.
func (i *Interpreter) initAsyncFromSyncIterator() {
	i.asyncFromSyncIterProto = NewObject(i.asyncIteratorProto)
	p := i.asyncFromSyncIterProto

	// next(value): step the sync iterator and continue asynchronously.
	i.defineMethod(p, "next", 1, func(ctx context.Context, this Value, a []Value) (Value, error) {
		syncRec := asyncFromSyncRecOf(this)
		if syncRec == nil {
			return i.rejectedPromiseVal(i.newError("TypeError", "AsyncFromSyncIterator.prototype.next called on incompatible receiver")), nil
		}
		var args []Value
		if len(a) > 0 {
			args = []Value{a[0]}
		}
		res, err := i.call(ctx, syncRec.nextMethod, syncRec.iterator, args)
		if err != nil {
			if tv, ok := ThrownValue(err); ok {
				return i.rejectedPromiseVal(tv), nil
			}
			return nil, err
		}
		return i.asyncFromSyncContinuation(ctx, res, syncRec, true)
	})

	// return(value): forward to the sync iterator's return, or synthesize a
	// { value, done: true } result when it has none.
	i.defineMethod(p, "return", 1, func(ctx context.Context, this Value, a []Value) (Value, error) {
		syncRec := asyncFromSyncRecOf(this)
		if syncRec == nil {
			return i.rejectedPromiseVal(i.newError("TypeError", "AsyncFromSyncIterator.prototype.return called on incompatible receiver")), nil
		}
		retMethod, err := i.getMethodStr(ctx, syncRec.iterator, "return")
		if err != nil {
			if tv, ok := ThrownValue(err); ok {
				return i.rejectedPromiseVal(tv), nil
			}
			return nil, err
		}
		if retMethod == nil {
			pr, resolve, _ := i.newPromise()
			resolve(i.createIterResult(arg(a, 0), true))
			return pr, nil
		}
		var args []Value
		if len(a) > 0 {
			args = []Value{a[0]}
		}
		res, err := i.call(ctx, retMethod, syncRec.iterator, args)
		if err != nil {
			if tv, ok := ThrownValue(err); ok {
				return i.rejectedPromiseVal(tv), nil
			}
			return nil, err
		}
		if _, ok := res.(*Object); !ok {
			return i.rejectedPromiseVal(i.newError("TypeError", "iterator return method returned a non-object")), nil
		}
		return i.asyncFromSyncContinuation(ctx, res, syncRec, false)
	})

	// throw(value): forward to the sync iterator's throw, or close it and reject
	// with a TypeError when it has none.
	i.defineMethod(p, "throw", 1, func(ctx context.Context, this Value, a []Value) (Value, error) {
		syncRec := asyncFromSyncRecOf(this)
		if syncRec == nil {
			return i.rejectedPromiseVal(i.newError("TypeError", "AsyncFromSyncIterator.prototype.throw called on incompatible receiver")), nil
		}
		throwMethod, err := i.getMethodStr(ctx, syncRec.iterator, "throw")
		if err != nil {
			if tv, ok := ThrownValue(err); ok {
				return i.rejectedPromiseVal(tv), nil
			}
			return nil, err
		}
		if throwMethod == nil {
			if cerr := i.iteratorClose(ctx, syncRec, nil); cerr != nil {
				if tv, ok := ThrownValue(cerr); ok {
					return i.rejectedPromiseVal(tv), nil
				}
				return nil, cerr
			}
			return i.rejectedPromiseVal(i.newError("TypeError", "The iterator does not provide a throw method")), nil
		}
		var args []Value
		if len(a) > 0 {
			args = []Value{a[0]}
		}
		res, err := i.call(ctx, throwMethod, syncRec.iterator, args)
		if err != nil {
			if tv, ok := ThrownValue(err); ok {
				return i.rejectedPromiseVal(tv), nil
			}
			return nil, err
		}
		if _, ok := res.(*Object); !ok {
			return i.rejectedPromiseVal(i.newError("TypeError", "iterator throw method returned a non-object")), nil
		}
		return i.asyncFromSyncContinuation(ctx, res, syncRec, true)
	})
}
