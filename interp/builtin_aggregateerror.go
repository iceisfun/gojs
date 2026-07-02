package interp

import "context"

// initAggregateError installs AggregateError (§20.5.7), whose constructor takes
// (errors, message, options) and stores the iterated errors on an own "errors"
// property. errorCtor is the %Error% constructor, used as AggregateError's
// [[Prototype]] and AggregateError.prototype's parent (via %Error.prototype%).
func (i *Interpreter) initAggregateError(errorCtor *Object) {
	proto := NewObject(i.errorProto)
	proto.SetHidden("name", String("AggregateError"))
	proto.SetHidden("message", String(""))
	// Recorded so Promise.any can build a spec-correct AggregateError instance.
	i.aggregateErrorProto = proto

	build := func(ctx context.Context, this Value, args []Value) (Value, error) {
		obj := NewObject(proto)
		obj.class = "Error"

		// Step 3: message (the second argument).
		if m := arg(args, 1); !IsUndefined(m) {
			s, err := i.ToStringV(ctx, m)
			if err != nil {
				return nil, err
			}
			obj.defineOwn(StrKey("message"), &Property{Value: String(s), Writable: true, Enumerable: false, Configurable: true})
		}
		// Step 4: InstallErrorCause(O, options) — the third argument.
		if opts, ok := arg(args, 2).(*Object); ok {
			if opts.Has(StrKey("cause")) {
				c, err := opts.GetStr(ctx, "cause")
				if err != nil {
					return nil, err
				}
				obj.defineOwn(StrKey("cause"), &Property{Value: c, Writable: true, Enumerable: false, Configurable: true})
			}
		}
		// Step 5: errorsList = IterableToList(errors) — the first argument.
		errs, err := i.iterableToList(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		// Step 6: install the "errors" array.
		obj.defineOwn(StrKey("errors"), &Property{Value: i.newArray(errs), Writable: true, Enumerable: false, Configurable: true})

		setErrorStack(obj, "AggregateError: captured stack unavailable")
		return obj, nil
	}

	ctor := i.newNativeCtor("AggregateError", 2, build, build)
	// AggregateError inherits from Error (its [[Prototype]] is %Error%).
	ctor.SetProto(errorCtor)
	linkCtor(ctor, proto)
	i.setGlobalHidden("AggregateError", ctor)
}

// iterableToList implements IterableToList using the full GetIterator protocol
// via [[Get]], so that a throwing Symbol.iterator accessor (or next/value)
// propagates rather than being silently treated as non-iterable.
func (i *Interpreter) iterableToList(ctx context.Context, items Value) ([]Value, error) {
	obj, ok := items.(*Object)
	if !ok {
		// Strings and other iterables without accessor pitfalls go through the
		// ordinary iteration path; nullish/non-iterable values throw there.
		var out []Value
		err := i.iterate(ctx, items, func(v Value) error {
			out = append(out, v)
			return nil
		})
		return out, err
	}
	methodV, err := obj.Get(ctx, SymKey(i.symIterator))
	if err != nil {
		return nil, err
	}
	if IsNullish(methodV) {
		return nil, i.throwError(ctx, "TypeError", briefValue(items)+" is not iterable")
	}
	method, ok := methodV.(*Object)
	if !ok || !method.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "Symbol.iterator is not a function")
	}
	iterator, err := method.fn.call(ctx, obj, nil)
	if err != nil {
		return nil, err
	}
	itObj, ok := iterator.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "iterator is not an object")
	}
	nextV, err := itObj.GetStr(ctx, "next")
	if err != nil {
		return nil, err
	}
	next, ok := nextV.(*Object)
	if !ok || !next.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "iterator.next is not a function")
	}
	var out []Value
	for {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		resV, err := next.fn.call(ctx, itObj, nil)
		if err != nil {
			return nil, err
		}
		res, ok := resV.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "iterator result is not an object")
		}
		doneV, err := res.GetStr(ctx, "done")
		if err != nil {
			return nil, err
		}
		if ToBoolean(doneV) {
			return out, nil
		}
		val, err := res.GetStr(ctx, "value")
		if err != nil {
			return nil, err
		}
		out = append(out, val)
	}
}
