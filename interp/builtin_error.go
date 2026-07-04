package interp

import "context"

// This file implements the Error type hierarchy and the interpreter's helpers
// for constructing and throwing errors from Go code.

// nativeErrorNames lists the standard Error subclasses created at bootstrap.
var nativeErrorNames = []string{
	"TypeError", "RangeError", "ReferenceError", "SyntaxError",
	"EvalError", "URIError",
}

// initError installs Error and its subclasses (TypeError, RangeError, …).
func (i *Interpreter) initError() {
	proto := i.errorProto
	proto.SetHidden("name", String("Error"))
	proto.SetHidden("message", String(""))
	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Error.prototype.toString called on non-object")
		}
		// §20.5.3.4: name defaults to "Error" when undefined, message to the
		// empty String; both Get and ToString are observable and may throw.
		nameV, err := o.GetStr(ctx, "name")
		if err != nil {
			return nil, err
		}
		name := "Error"
		if !IsUndefined(nameV) {
			if name, err = i.ToStringV(ctx, nameV); err != nil {
				return nil, err
			}
		}
		msgV, err := o.GetStr(ctx, "message")
		if err != nil {
			return nil, err
		}
		msg := ""
		if !IsUndefined(msgV) {
			if msg, err = i.ToStringV(ctx, msgV); err != nil {
				return nil, err
			}
		}
		switch {
		case msg == "":
			return String(name), nil
		case name == "":
			return String(msg), nil
		default:
			return String(name + ": " + msg), nil
		}
	})

	ctor := i.newErrorCtor("Error", i.errorProto)
	linkCtor(ctor, i.errorProto)
	// Error.isError reports whether its argument has an [[ErrorData]] slot,
	// which gojs marks with the object's internal class "Error" (§20.5.2.1).
	i.defineMethod(ctor, "isError", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		return Bool(ok && o.class == "Error"), nil
	})
	i.setGlobalHidden("Error", ctor)

	for _, name := range nativeErrorNames {
		sub := NewObject(i.errorProto)
		sub.SetHidden("name", String(name))
		sub.SetHidden("message", String(""))
		subCtor := i.newErrorCtor(name, sub)
		// Subclass constructors inherit from Error.
		subCtor.SetProto(ctor)
		linkCtor(subCtor, sub)
		i.nativeErrorProtos[name] = sub
		i.nativeErrorCtors[name] = subCtor
		i.setGlobalHidden(name, subCtor)
	}

	i.initAggregateError(ctor)
	i.initErrorStack(i.errorProto)
}

// newErrorCtor builds an Error-family constructor whose instances use proto.
func (i *Interpreter) newErrorCtor(name string, proto *Object) *Object {
	// buildInto materializes the error object with instProto as its
	// [[Prototype]] (§20.5.1.1 steps 3-4 / NativeError equivalent).
	buildInto := func(ctx context.Context, instProto *Object, args []Value) (Value, error) {
		obj := NewObject(instProto)
		obj.class = "Error"
		msg := ""
		if m := arg(args, 0); !IsUndefined(m) {
			s, err := i.ToStringV(ctx, m)
			if err != nil {
				return nil, err
			}
			msg = s
			obj.SetHidden("message", String(s))
		}
		// Step 4: InstallErrorCause(O, options) — trap-aware HasProperty + Get.
		if err := i.installErrorCause(ctx, obj, arg(args, 1)); err != nil {
			return nil, err
		}
		// The stack trace lives in an internal slot, exposed via the
		// Error.prototype.stack accessor rather than an own data property.
		i.captureError(obj, name, msg)
		return obj, nil
	}
	// Called as a plain function: NewTarget is undefined, so the intrinsic
	// %...prototype% is used (§20.5.1.1 step 1).
	call := func(ctx context.Context, _ Value, args []Value) (Value, error) {
		return buildInto(ctx, proto, args)
	}
	// [[Construct]]: the instance's [[Prototype]] comes from NewTarget via
	// OrdinaryCreateFromConstructor / GetPrototypeFromConstructor, so
	// Reflect.construct(E, args, foreignTarget) and `class X extends E` produce
	// an instance whose prototype is NewTarget.prototype.
	construct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		instProto, err := i.protoFromConstructor(ctx, newTarget, proto)
		if err != nil {
			return nil, err
		}
		return buildInto(ctx, instProto, args)
	}
	return i.newNativeCtor(name, 1, call, construct)
}

// installErrorCause implements InstallErrorCause (§20.5.8.1): when options is an
// Object and HasProperty(options, "cause") is true, it reads the cause via
// [[Get]] and installs it as a non-enumerable own data property of O
// (CreateNonEnumerableDataPropertyOrThrow). Both the HasProperty and Get are
// trap-aware, so a Proxy options' throwing has/get trap (or a throwing "cause"
// getter) propagates rather than being swallowed.
func (i *Interpreter) installErrorCause(ctx context.Context, o *Object, options Value) error {
	opts, ok := options.(*Object)
	if !ok {
		return nil
	}
	has, err := i.hasV(ctx, opts, StrKey("cause"))
	if err != nil {
		return err
	}
	if !has {
		return nil
	}
	c, err := i.getV(ctx, opts, StrKey("cause"), opts)
	if err != nil {
		return err
	}
	o.SetHidden("cause", c)
	return nil
}

// newError constructs an Error-family object of the given kind with a message.
func (i *Interpreter) newError(name, message string) *Object {
	proto := i.errorProto
	if p, ok := i.nativeErrorProtos[name]; ok {
		proto = p
	}
	obj := NewObject(proto)
	obj.class = "Error"
	obj.SetHidden("message", String(message))
	i.captureError(obj, name, message)
	return obj
}

// throwError returns a *Throw wrapping a freshly constructed error of the named
// kind. It is the standard way runtime code raises a JavaScript exception.
func (i *Interpreter) throwError(_ context.Context, name, message string) error {
	return NewThrow(i.newError(name, message))
}
