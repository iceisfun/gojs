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
	build := func(ctx context.Context, this Value, args []Value) (Value, error) {
		// Always create a fresh instance whose prototype is this constructor's
		// error prototype. (When invoked via super() from a subclass, the
		// caller folds this object's own properties onto the real instance.)
		obj := NewObject(proto)
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
		// Options bag: { cause }.
		if opts, ok := arg(args, 1).(*Object); ok {
			if opts.HasOwn(StrKey("cause")) {
				c, _ := opts.GetStr(ctx, "cause")
				obj.SetHidden("cause", c)
			}
		}
		// The stack trace lives in an internal slot, exposed via the
		// Error.prototype.stack accessor rather than an own data property.
		i.captureError(obj, name, msg)
		return obj, nil
	}
	return i.newNativeCtor(name, 1, build, build)
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
