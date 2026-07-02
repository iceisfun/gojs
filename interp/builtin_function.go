package interp

import (
	"context"
	"strings"
)

// initFunction installs Function.prototype methods (call/apply/bind/toString).
func (i *Interpreter) initFunction() {
	proto := i.functionProto

	i.defineMethod(proto, "call", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		fn, ok := this.(*Object)
		if !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Function.prototype.call called on non-callable")
		}
		var thisArg Value = Undef
		var rest []Value
		if len(args) > 0 {
			thisArg = args[0]
			rest = args[1:]
		}
		return fn.fn.call(ctx, thisArg, rest)
	})

	i.defineMethod(proto, "apply", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		fn, ok := this.(*Object)
		if !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Function.prototype.apply called on non-callable")
		}
		var thisArg Value = Undef
		if len(args) > 0 {
			thisArg = args[0]
		}
		var callArgs []Value
		if len(args) > 1 && !IsNullish(args[1]) {
			arr, ok := args[1].(*Object)
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "CreateListFromArrayLike called on non-object")
			}
			if arr.isArray {
				callArgs = append(callArgs, arr.denseCopy()...)
			} else {
				lenV, _ := arr.GetStr(ctx, "length")
				n := int(ToInteger(ToNumber(lenV)))
				for j := 0; j < n; j++ {
					v, _ := arr.GetStr(ctx, intToStr(j))
					callArgs = append(callArgs, v)
				}
			}
		}
		return fn.fn.call(ctx, thisArg, callArgs)
	})

	i.defineMethod(proto, "bind", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		fn, ok := this.(*Object)
		if !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Function.prototype.bind called on non-callable")
		}
		var boundThis Value = Undef
		var boundArgs []Value
		if len(args) > 0 {
			boundThis = args[0]
			boundArgs = append(boundArgs, args[1:]...)
		}
		name := "bound " + fn.fn.name
		// The bound function's length is the target's length minus the number of
		// pre-bound arguments, floored at zero (per Function.prototype.bind).
		boundLen := 0
		if lenV, err := fn.GetStr(ctx, "length"); err == nil {
			if n := int(ToInteger(ToNumber(lenV))) - len(boundArgs); n > 0 {
				boundLen = n
			}
		}
		bound := i.newNativeFunc(name, boundLen, func(ctx context.Context, _ Value, callArgs []Value) (Value, error) {
			return fn.fn.call(ctx, boundThis, append(append([]Value{}, boundArgs...), callArgs...))
		})
		// A bound constructor stays constructable, ignoring boundThis on `new`.
		if fn.fn.construct != nil {
			bound.fn.construct = func(ctx context.Context, newThis Value, callArgs []Value) (Value, error) {
				return fn.fn.construct(ctx, newThis, append(append([]Value{}, boundArgs...), callArgs...))
			}
			bound.fn.ctor = true
		}
		return bound, nil
	})

	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		fn, ok := this.(*Object)
		if !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Function.prototype.toString called on non-callable")
		}
		name := fn.fn.name
		var b strings.Builder
		b.WriteString("function ")
		b.WriteString(name)
		b.WriteString("() { [native code] }")
		return String(b.String()), nil
	})

	// Symbol.hasInstance controls the instanceof operator. Per spec this
	// property is non-writable, non-enumerable, and non-configurable.
	hasInstance := i.newNativeFunc("[Symbol.hasInstance]", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		fn, ok := this.(*Object)
		if !ok || !fn.IsCallable() {
			return False, nil
		}
		res, err := i.ordinaryHasInstance(ctx, fn, arg(args, 0))
		if err != nil {
			return nil, err
		}
		return Bool(res), nil
	})
	proto.defineOwn(SymKey(i.symHasInstance), &Property{Value: hasInstance, Writable: false, Enumerable: false, Configurable: false})

	// AddRestrictedFunctionProperties: %Function.prototype% exposes "caller"
	// and "arguments" as poison-pill accessors whose get and set both throw a
	// TypeError. Ordinary, bound, and dynamic functions inherit these rather
	// than owning them.
	thrower := i.newNativeFunc("", 0, func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "'caller', 'callee', and 'arguments' properties may not be accessed on ordinary functions")
	})
	proto.defineOwn(StrKey("caller"), &Property{Get: thrower, Set: thrower, Accessor: true, Enumerable: false, Configurable: true})
	proto.defineOwn(StrKey("arguments"), &Property{Get: thrower, Set: thrower, Accessor: true, Enumerable: false, Configurable: true})

	// The Function constructor builds a function from source strings
	// (CreateDynamicFunction). Both `Function(...)` and `new Function(...)`
	// route here; it can be gated off via Security.DisableFunctionCtor.
	ctor := i.newNativeCtor("Function", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.createDynamicFunction(ctx, args)
	}, nil)
	linkCtor(ctor, proto)
	i.functionCtor = ctor
	i.setGlobalHidden("Function", ctor)
}

// ordinaryHasInstance implements the default instanceof check: whether ctor's
// .prototype appears on v's prototype chain.
func (i *Interpreter) ordinaryHasInstance(ctx context.Context, ctor *Object, v Value) (bool, error) {
	if !ctor.IsCallable() {
		return false, nil
	}
	obj, ok := v.(*Object)
	if !ok {
		return false, nil
	}
	protoV, err := ctor.GetStr(ctx, "prototype")
	if err != nil {
		return false, err
	}
	proto, ok := protoV.(*Object)
	if !ok {
		return false, i.throwError(ctx, "TypeError", "Function has non-object prototype in instanceof check")
	}
	for p := obj.proto; p != nil; p = p.proto {
		if p == proto {
			return true, nil
		}
	}
	return false, nil
}
