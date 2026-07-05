package interp

import (
	"context"
	"math"
	"strings"
)

// initFunction installs Function.prototype methods (call/apply/bind/toString).
func (i *Interpreter) initFunction() {
	proto := i.functionProto

	// %Function.prototype% is itself a built-in function object, so it owns
	// "length" (0) and "name" ("") data properties with the standard
	// { writable:false, enumerable:false, configurable:true } attributes.
	// "length" is defined before "name" so their observable insertion order
	// matches every other built-in function (sec-createbuiltinfunction).
	proto.defineOwn(StrKey("length"), &Property{Value: Number(0), Writable: false, Enumerable: false, Configurable: true})
	proto.defineOwn(StrKey("name"), &Property{Value: String(""), Writable: false, Enumerable: false, Configurable: true})

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
				// CreateListFromArrayLike: len = ToLength(Get(obj,"length")),
				// then Get each index. Any abrupt completion propagates.
				lenV, err := arr.GetStr(ctx, "length")
				if err != nil {
					return nil, err
				}
				n, err := i.toLength(ctx, lenV)
				if err != nil {
					return nil, err
				}
				callArgs = make([]Value, 0, n)
				for j := 0; j < n; j++ {
					v, err := arr.GetStr(ctx, intToStr(j))
					if err != nil {
						return nil, err
					}
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

		// SetFunctionLength (§20.2.3.2 steps 5-7): L defaults to 0. If Target has
		// an OWN "length" property whose value is a Number, L is that length
		// (ToInteger) minus the bound-argument count, floored at 0; +∞ stays +∞
		// and -∞ becomes 0. A non-own or non-Number "length" is ignored.
		boundLen := 0.0
		if _, hasLen, err := i.getOwnPropertyV(ctx, fn, StrKey("length")); err != nil {
			return nil, err
		} else if hasLen {
			lenV, err := fn.GetStr(ctx, "length")
			if err != nil {
				return nil, err
			}
			if num, ok := lenV.(Number); ok {
				f := float64(num)
				switch {
				case math.IsInf(f, 1):
					boundLen = math.Inf(1)
				case math.IsInf(f, -1):
					boundLen = 0
				default:
					if n := ToInteger(f) - float64(len(boundArgs)); n > 0 {
						boundLen = n
					}
				}
			}
		}

		// SetFunctionName (§20.2.3.2 steps 12-15): targetName = Get(Target,"name"),
		// coerced to the empty string when it is not a String; the bound name is
		// "bound " + targetName. Reading the target's "name" may throw.
		targetNameV, err := fn.GetStr(ctx, "name")
		if err != nil {
			return nil, err
		}
		targetName := ""
		if s, ok := asString(targetNameV); ok {
			targetName = s
		}
		name := "bound " + targetName

		bound := i.newNativeFunc(name, 0, func(ctx context.Context, _ Value, callArgs []Value) (Value, error) {
			return fn.fn.call(ctx, boundThis, append(append([]Value{}, boundArgs...), callArgs...))
		})
		// Record [[BoundTargetFunction]] so OrdinaryHasInstance delegates to the
		// target, and set the correct "length" (which may be +∞). Redefining the
		// existing "length" property keeps its insertion position (before "name").
		bound.fn.boundTarget = fn
		bound.defineOwn(StrKey("length"), &Property{Value: Number(boundLen), Writable: false, Enumerable: false, Configurable: true})
		// A bound constructor stays constructable, ignoring boundThis on `new`.
		if fn.fn.construct != nil {
			bound.fn.construct = func(ctx context.Context, newTarget Value, callArgs []Value) (Value, error) {
				// Per BoundFunctionCreate's [[Construct]]: when new.target is the
				// bound function itself, substitute the target so the instance's
				// prototype derives from the target, not the (prototype-less) bound
				// wrapper.
				if newTarget == Value(bound) {
					newTarget = fn
				}
				return fn.fn.construct(ctx, newTarget, append(append([]Value{}, boundArgs...), callArgs...))
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
		// Function.prototype.toString (§20.2.3.5): if the function has source text
		// (an ECMAScript function/method/arrow with a captured [[SourceText]]),
		// return it verbatim. A bound function, a native function, or one built by
		// the Function constructor has no captured source and uses the
		// NativeFunction form `function name() { [native code] }`.
		if fn.fn.source != "" && fn.fn.boundTarget == nil {
			return String(fn.fn.source), nil
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
	// The %ThrowTypeError% intrinsic (§10.2.4) is a single shared anonymous
	// function defined once per realm. It is frozen: its "length" and "name"
	// are non-writable/non-configurable and the object itself is non-extensible.
	// The SAME object is the get AND set accessor for every poison pill.
	thrower := i.newNativeFunc("", 0, func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "'caller', 'callee', and 'arguments' properties may not be accessed on ordinary functions")
	})
	thrower.defineOwn(StrKey("length"), &Property{Value: Number(0), Writable: false, Enumerable: false, Configurable: false})
	thrower.defineOwn(StrKey("name"), &Property{Value: String(""), Writable: false, Enumerable: false, Configurable: false})
	thrower.extensible = false
	i.throwTypeError = thrower
	proto.defineOwn(StrKey("caller"), &Property{Get: thrower, Set: thrower, Accessor: true, Enumerable: false, Configurable: true})
	proto.defineOwn(StrKey("arguments"), &Property{Get: thrower, Set: thrower, Accessor: true, Enumerable: false, Configurable: true})

	// Annex B legacy getter shared by the sloppy-function own "caller"/
	// "arguments" accessors: it always returns null (never a strict function),
	// which is all the forbidden-extension tests require.
	i.legacyNullGetter = i.newNativeFunc("", 0, func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return Nul, nil
	})

	// The Function constructor builds a function from source strings
	// (CreateDynamicFunction). Both `Function(...)` and `new Function(...)`
	// route here; it can be gated off via Security.DisableFunctionCtor.
	var ctor *Object
	ctor = i.newNativeCtor("Function", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		// Called (not via `new`): NewTarget is the active function object.
		return i.createDynamicFunction(ctx, ctor, args)
	}, func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		return i.createDynamicFunction(ctx, newTarget, args)
	})
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
	// OrdinaryHasInstance step 2: a bound function delegates to its
	// [[BoundTargetFunction]] (which itself may be bound), so `instanceof`
	// consults the target's prototype chain rather than the prototype-less
	// bound wrapper.
	if ctor.fn != nil && ctor.fn.boundTarget != nil {
		return i.ordinaryHasInstance(ctx, ctor.fn.boundTarget, v)
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
	// Walk the prototype chain via [[GetPrototypeOf]] so a Proxy's trap runs.
	cur := obj
	for {
		pv, err := i.getProtoV(ctx, cur)
		if err != nil {
			return false, err
		}
		next, ok := pv.(*Object)
		if !ok {
			return false, nil
		}
		if next == proto {
			return true, nil
		}
		cur = next
	}
}
