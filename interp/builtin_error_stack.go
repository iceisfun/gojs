package interp

import "context"

// This file implements the Error.prototype.stack accessor (the error-stack
// accessor proposal). stack is an accessor property of %Error.prototype%; error
// instances do not carry an own "stack" data property. The getter reads the
// implementation-defined trace stored in the [[ErrorData]] object's internal
// slot; the setter follows SetterThatIgnoresPrototypeProperties so that writing
// through it stamps an own data property on the receiver.

// setErrorStack records the (implementation-defined) stack trace for an error
// instance in its internal slot, where the get stack accessor finds it.
func setErrorStack(o *Object, s string) {
	if o.internal == nil {
		o.internal = make(map[string]any)
	}
	o.internal["errorStack"] = s
}

// initErrorStack installs the get/set stack accessor on %Error.prototype%.
func (i *Interpreter) initErrorStack(proto *Object) {
	get := i.newNativeFunc("get stack", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Error.prototype.stack getter called on non-object")
		}
		// No [[ErrorData]] internal slot: return undefined.
		if o.class != "Error" {
			return Undef, nil
		}
		if s, ok := o.internal["errorStack"].(string); ok {
			return String(s), nil
		}
		return String(""), nil
	})
	set := i.newNativeFunc("set stack", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Error.prototype.stack setter called on non-object")
		}
		v := arg(args, 0)
		if _, ok := v.(String); !ok {
			return nil, i.throwError(ctx, "TypeError", "Error.prototype.stack setter requires a string value")
		}
		// SetterThatIgnoresPrototypeProperties(this, %Error.prototype%, "stack", v):
		// writing to the home object itself emulates a non-writable data property.
		if o == proto {
			return nil, i.throwError(ctx, "TypeError", "Cannot assign to read only property 'stack' of Error.prototype")
		}
		if _, has := o.getOwn(StrKey("stack")); !has {
			// CreateDataPropertyOrThrow: fails on a non-extensible receiver.
			if !o.extensible {
				return nil, i.throwError(ctx, "TypeError", "Cannot define property stack, object is not extensible")
			}
			o.defineOwn(StrKey("stack"), &Property{Value: v, Writable: true, Enumerable: true, Configurable: true})
			return Undef, nil
		}
		// Otherwise perform an ordinary [[Set]] with Throw=true.
		if err := i.setThrow(ctx, o, "stack", v); err != nil {
			return nil, err
		}
		return Undef, nil
	})
	proto.DefineAccessor("stack", get, set, false)
}
