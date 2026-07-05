package interp

import "context"

// This file implements the joint-iteration proposal's Iterator.zip and
// Iterator.zipKeyed statics. Both collect a set of source iterators up front,
// then return an %IteratorHelperPrototype% iterator that, on each step, advances
// every source once and yields a combined value — a fresh array (zip) or a
// null-prototype object keyed by the source keys (zipKeyed). The "mode" option
// selects behavior when the sources have unequal lengths:
//   - "shortest" (default): stop when any source is exhausted.
//   - "longest": continue until all are exhausted, substituting each exhausted
//     source's padding value.
//   - "strict": require all sources to have equal length, else throw a TypeError.

func (i *Interpreter) iteratorZip(ctx context.Context, _ Value, args []Value) (Value, error) {
	return i.iteratorZipImpl(ctx, arg(args, 0), arg(args, 1), false)
}

func (i *Interpreter) iteratorZipKeyed(ctx context.Context, _ Value, args []Value) (Value, error) {
	return i.iteratorZipImpl(ctx, arg(args, 0), arg(args, 1), true)
}

// getOptionsObject implements GetOptionsObject: undefined yields a fresh
// null-prototype object, an object is returned as-is, anything else throws.
func (i *Interpreter) getOptionsObject(ctx context.Context, options Value) (*Object, error) {
	if IsUndefined(options) {
		return NewObject(nil), nil
	}
	if o, ok := options.(*Object); ok {
		return o, nil
	}
	return nil, i.throwError(ctx, "TypeError", "options must be an object or undefined")
}

// closeOpenIters closes every still-open iterator in reverse List order,
// threading a pending completion (IteratorCloseAll). Closed entries are niled so
// a later close — e.g. the iterator helper's return() — does not re-close them.
func (i *Interpreter) closeOpenIters(ctx context.Context, open []*iterRecord, pending error) error {
	for k := len(open) - 1; k >= 0; k-- {
		if open[k] != nil {
			pending = i.iteratorClose(ctx, open[k], pending)
			open[k] = nil
		}
	}
	return pending
}

func compactIters(open []*iterRecord) []*iterRecord {
	out := make([]*iterRecord, 0, len(open))
	for _, r := range open {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

func (i *Interpreter) iteratorZipImpl(ctx context.Context, iterablesV, optionsV Value, keyed bool) (Value, error) {
	iterablesObj, ok := iterablesV.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Iterator.zip called on a non-object")
	}
	options, err := i.getOptionsObject(ctx, optionsV)
	if err != nil {
		return nil, err
	}
	// mode: undefined defaults to "shortest"; otherwise it must be exactly one of
	// the three mode strings.
	modeV, err := options.GetStr(ctx, "mode")
	if err != nil {
		return nil, err
	}
	mode := "shortest"
	if !IsUndefined(modeV) {
		ms, ok := asString(modeV)
		if !ok || (ms != "shortest" && ms != "longest" && ms != "strict") {
			return nil, i.throwError(ctx, "TypeError", "Iterator.zip mode must be 'shortest', 'longest', or 'strict'")
		}
		mode = string(ms)
	}
	var paddingOption Value = Undef
	if mode == "longest" {
		paddingOption, err = options.GetStr(ctx, "padding")
		if err != nil {
			return nil, err
		}
		if !IsUndefined(paddingOption) {
			if _, ok := paddingOption.(*Object); !ok {
				return nil, i.throwError(ctx, "TypeError", "Iterator.zip padding must be an object")
			}
		}
	}

	// Collect the source iterators (and, for zipKeyed, their keys). An abrupt
	// completion while collecting closes the iterators gathered so far.
	var iters []*iterRecord
	var keys []PropertyKey
	if !keyed {
		inputIter, err := i.getIterator(ctx, iterablesV)
		if err != nil {
			return nil, err
		}
		for {
			v, done, err := i.iteratorStepValue(ctx, inputIter)
			if err != nil {
				return nil, i.closeOpenIters(ctx, iters, err)
			}
			if done {
				break
			}
			iter, err := i.getIteratorFlattenable(ctx, v, false)
			if err != nil {
				return nil, i.closeOpenIters(ctx, inputIterAnd(iters, inputIter), err)
			}
			iters = append(iters, iter)
		}
	} else {
		allKeys, err := i.ownKeysV(ctx, iterablesObj)
		if err != nil {
			return nil, err
		}
		for _, key := range allKeys {
			desc, ok, err := i.getOwnPropertyV(ctx, iterablesObj, key)
			if err != nil {
				return nil, i.closeOpenIters(ctx, iters, err)
			}
			if !ok || !desc.Enumerable {
				continue
			}
			val, err := i.getV(ctx, iterablesObj, key, iterablesObj)
			if err != nil {
				return nil, i.closeOpenIters(ctx, iters, err)
			}
			// An undefined value means "no source for this key": skip it entirely
			// (spec step 12.c.ii, "If value is not undefined"), so the key is absent
			// from every yielded result object.
			if IsUndefined(val) {
				continue
			}
			iter, err := i.getIteratorFlattenable(ctx, val, false)
			if err != nil {
				return nil, i.closeOpenIters(ctx, iters, err)
			}
			iters = append(iters, iter)
			keys = append(keys, key)
		}
	}
	iterCount := len(iters)

	// padding[j] is the value substituted for source j once it is exhausted in
	// "longest" mode; defaults to undefined.
	padding := make([]Value, iterCount)
	for j := range padding {
		padding[j] = Undef
	}
	if mode == "longest" && !IsUndefined(paddingOption) {
		if !keyed {
			paddingIter, err := i.getIterator(ctx, paddingOption)
			if err != nil {
				return nil, i.closeOpenIters(ctx, iters, err)
			}
			using := true
			for j := 0; j < iterCount; j++ {
				if !using {
					break
				}
				pv, done, err := i.iteratorStepValue(ctx, paddingIter)
				if err != nil {
					return nil, i.closeOpenIters(ctx, iters, err)
				}
				if done {
					using = false
				} else {
					padding[j] = pv
				}
			}
			if using {
				if cerr := i.iteratorClose(ctx, paddingIter, nil); cerr != nil {
					return nil, i.closeOpenIters(ctx, iters, cerr)
				}
			}
		} else {
			po := paddingOption.(*Object)
			for j, key := range keys {
				pv, err := i.getV(ctx, po, key, po)
				if err != nil {
					return nil, i.closeOpenIters(ctx, iters, err)
				}
				padding[j] = pv
			}
		}
	}

	// finishResults builds one combined result from the per-source values.
	finish := func(values []Value) Value {
		if !keyed {
			return i.newArray(append([]Value{}, values...))
		}
		result := NewObject(nil) // null-prototype keyed result object
		for j, k := range keys {
			result.defineOwn(k, &Property{Value: values[j], Writable: true, Enumerable: true, Configurable: true})
		}
		return result
	}

	return i.makeIteratorZip(iters, mode, padding, finish), nil
}

// inputIterAnd returns the collected inner iterators plus the outer input
// iterator, so an abrupt GetIteratorFlattenable closes the input iterator too
// (it is the outermost, so it is closed last in reverse order).
func inputIterAnd(iters []*iterRecord, inputIter *iterRecord) []*iterRecord {
	return append([]*iterRecord{inputIter}, iters...)
}

// makeIteratorZip builds the %IteratorHelperPrototype% result whose pull closure
// advances every open source once per step and applies the mode's length policy.
func (i *Interpreter) makeIteratorZip(iters []*iterRecord, mode string, padding []Value, finish func([]Value) Value) *Object {
	iterCount := len(iters)
	open := make([]*iterRecord, iterCount)
	copy(open, iters)
	return i.newIterHelper(func(st *iterHelperState) {
		st.live = compactIters(open)
		st.pull = func(ctx context.Context) (Value, bool, error) {
			if iterCount == 0 {
				return Undef, true, nil
			}
			results := make([]Value, iterCount)
			for k := 0; k < iterCount; k++ {
				if open[k] == nil {
					results[k] = padding[k] // longest: already-exhausted source
					continue
				}
				v, done, err := i.iteratorStepValue(ctx, open[k])
				if err != nil {
					open[k] = nil
					return nil, false, i.closeOpenIters(ctx, open, err)
				}
				if !done {
					results[k] = v
					continue
				}
				// Source k is exhausted this step.
				open[k] = nil
				switch mode {
				case "shortest":
					// Close every other still-open source and finish.
					st.live = nil
					if cerr := i.closeOpenIters(ctx, open, nil); cerr != nil {
						return nil, false, cerr
					}
					return Undef, true, nil
				case "strict":
					if k != 0 {
						// Earlier sources yielded but this one is short → mismatch. The
						// TypeError is the completion threaded through IteratorCloseAll,
						// so it is preserved even if a source's return() throws.
						st.live = nil
						return nil, false, i.closeOpenIters(ctx, open, i.throwError(ctx, "TypeError", "Iterator.zip strict mode: iterators must have the same length"))
					}
					// First source ended; every remaining source must also end now.
					for j := 1; j < iterCount; j++ {
						_, dj, ej := i.iteratorStepValue(ctx, open[j])
						if ej != nil {
							open[j] = nil
							return nil, false, i.closeOpenIters(ctx, open, ej)
						}
						if !dj {
							st.live = nil
							return nil, false, i.closeOpenIters(ctx, open, i.throwError(ctx, "TypeError", "Iterator.zip strict mode: iterators must have the same length"))
						}
						open[j] = nil
					}
					return Undef, true, nil
				default: // "longest"
					results[k] = padding[k]
				}
			}
			// In "longest", once every source has been exhausted the round consists
			// entirely of padding and must NOT be yielded — the iterator is done.
			// (shortest/strict already returned from inside the loop.)
			allNil := true
			for _, r := range open {
				if r != nil {
					allNil = false
					break
				}
			}
			if allNil {
				st.live = nil
				return Undef, true, nil
			}
			st.live = compactIters(open)
			return finish(results), false, nil
		}
	})
}
