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
		// set Error.prototype.stack (error-stack-accessor proposal):
		//   1. If E is not an Object, throw a TypeError.
		//   2. If v is not a String, throw a TypeError.
		//   3. Perform ? SetterThatIgnoresPrototypeProperties(E, %Error.prototype%, "stack", v).
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Error.prototype.stack setter called on non-object")
		}
		v := arg(args, 0)
		// A string primitive may be either a String or a lazy rope; both satisfy
		// "v is a String".
		if !isStringish(v) {
			return nil, i.throwError(ctx, "TypeError", "Error.prototype.stack setter requires a string value")
		}
		// SetterThatIgnoresPrototypeProperties(this, home=%Error.prototype%, "stack", v):
		//   2. If SameValue(this, home), throw a TypeError (emulates writing a
		//      non-writable own data property on the home object). The check is
		//      object identity, so a Proxy wrapping %Error.prototype% is NOT home.
		if o == proto {
			return nil, i.throwError(ctx, "TypeError", "Cannot assign to read only property 'stack' of Error.prototype")
		}
		//   3. Let desc be ? this.[[GetOwnProperty]]("stack") (trap-aware).
		_, has, err := i.getOwnPropertyV(ctx, o, StrKey("stack"))
		if err != nil {
			return nil, err
		}
		if !has {
			//   4a. Perform ? CreateDataPropertyOrThrow(this, "stack", v).
			if err := i.createDataPropertyOrThrow(ctx, o, StrKey("stack"), v); err != nil {
				return nil, err
			}
			return Undef, nil
		}
		//   5a. Perform ? Set(this, "stack", v, true).
		if err := i.setThrow(ctx, o, "stack", v); err != nil {
			return nil, err
		}
		return Undef, nil
	})
	proto.DefineAccessor("stack", get, set, false)
}
