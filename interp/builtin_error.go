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
		nameV, _ := o.GetStr(ctx, "name")
		name := "Error"
		if !IsUndefined(nameV) {
			name, _ = i.ToStringV(ctx, nameV)
		}
		msgV, _ := o.GetStr(ctx, "message")
		msg, _ := i.ToStringV(ctx, msgV)
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
}

// newErrorCtor builds an Error-family constructor whose instances use proto.
func (i *Interpreter) newErrorCtor(name string, proto *Object) *Object {
	build := func(ctx context.Context, this Value, args []Value) (Value, error) {
		var obj *Object
		if o, ok := this.(*Object); ok && o != i.global && o.proto != nil && o != proto {
			obj = o // called via `new` with a fresh instance
		} else {
			obj = NewObject(proto)
		}
		obj.class = "Error"
		if m := arg(args, 0); !IsUndefined(m) {
			s, err := i.ToStringV(ctx, m)
			if err != nil {
				return nil, err
			}
			obj.SetHidden("message", String(s))
		}
		// Options bag: { cause }.
		if opts, ok := arg(args, 1).(*Object); ok {
			if opts.HasOwn(StrKey("cause")) {
				c, _ := opts.GetStr(ctx, "cause")
				obj.SetHidden("cause", c)
			}
		}
		obj.SetHidden("stack", String(name+": captured stack unavailable"))
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
	obj.SetHidden("stack", String(name+": "+message))
	return obj
}

// throwError returns a *Throw wrapping a freshly constructed error of the named
// kind. It is the standard way runtime code raises a JavaScript exception.
func (i *Interpreter) throwError(_ context.Context, name, message string) error {
	return NewThrow(i.newError(name, message))
}
