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
				callArgs = append(callArgs, arr.elems...)
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
		bound := i.newNativeFunc(name, 0, func(ctx context.Context, _ Value, callArgs []Value) (Value, error) {
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

	// Symbol.hasInstance controls the instanceof operator.
	i.defineSymbolMethod(proto, i.symHasInstance, "[Symbol.hasInstance]", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		fn, ok := this.(*Object)
		if !ok || !fn.IsCallable() {
			return False, nil
		}
		return Bool(i.ordinaryHasInstance(ctx, fn, arg(args, 0))), nil
	})

	// The Function constructor (dynamic code) is intentionally not supported;
	// expose a stub that throws, matching a locked-down sandbox.
	ctor := i.newNativeCtor("Function", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "EvalError", "Function constructor is disabled in this sandbox")
	}, nil)
	linkCtor(ctor, proto)
	i.functionCtor = ctor
	i.setGlobalHidden("Function", ctor)
}

// ordinaryHasInstance implements the default instanceof check: whether ctor's
// .prototype appears on v's prototype chain.
func (i *Interpreter) ordinaryHasInstance(ctx context.Context, ctor *Object, v Value) bool {
	obj, ok := v.(*Object)
	if !ok {
		return false
	}
	protoV, _ := ctor.GetStr(ctx, "prototype")
	proto, ok := protoV.(*Object)
	if !ok {
		return false
	}
	for p := obj.proto; p != nil; p = p.proto {
		if p == proto {
			return true
		}
	}
	return false
}
