package interp

import "context"

// This file provides helpers for creating callable objects (native Go
// functions and constructors) and small utilities for reading arguments inside
// native functions.

// newNativeFunc wraps a Go CallFn as an ordinary (non-constructable) function
// object with the given name and declared arity.
func (i *Interpreter) newNativeFunc(name string, length int, fn CallFn) *Object {
	o := NewObject(i.functionProto)
	o.class = "Function"
	o.fn = &functionData{call: fn, name: name, length: length, realm: i}
	// Per ECMA-262 §20.2.4, a function's "length" and "name" are
	// non-writable, non-enumerable, and configurable.
	o.defineOwn(StrKey("length"), &Property{Value: Number(float64(length)), Writable: false, Enumerable: false, Configurable: true})
	o.defineOwn(StrKey("name"), &Property{Value: String(name), Writable: false, Enumerable: false, Configurable: true})
	return o
}

// setFuncLength defines a function's "length" own property with the
// spec-mandated attributes { writable: false, enumerable: false,
// configurable: true }.
func setFuncLength(o *Object, length int) {
	o.defineOwn(StrKey("length"), &Property{Value: Number(float64(length)), Writable: false, Enumerable: false, Configurable: true})
}

// setFuncNameProp defines a function's "name" own property with the
// spec-mandated attributes { writable: false, enumerable: false,
// configurable: true }.
func setFuncNameProp(o *Object, name string) {
	o.defineOwn(StrKey("name"), &Property{Value: String(name), Writable: false, Enumerable: false, Configurable: true})
}

// newNativeCtor creates a constructable function. call backs plain invocation;
// construct backs `new`. If construct is nil, call is reused for both.
func (i *Interpreter) newNativeCtor(name string, length int, call, construct CallFn) *Object {
	o := i.newNativeFunc(name, length, call)
	if construct == nil {
		construct = call
	}
	o.fn.construct = construct
	o.fn.ctor = true
	return o
}

// defineMethod installs a hidden (non-enumerable) native method on target.
func (i *Interpreter) defineMethod(target *Object, name string, length int, fn CallFn) *Object {
	m := i.newNativeFunc(name, length, fn)
	target.SetHidden(name, m)
	return m
}

// defineGetter installs a non-enumerable, configurable accessor property named
// name whose get accessor is a native function named "get <name>" (matching the
// spec's naming for built-in getters) and whose set accessor is undefined.
func (i *Interpreter) defineGetter(target *Object, name string, fn CallFn) *Object {
	get := i.newNativeFunc("get "+name, 0, fn)
	target.DefineAccessor(name, get, nil, false)
	return get
}

// defineSymbolMethod installs a hidden native method under a symbol key.
func (i *Interpreter) defineSymbolMethod(target *Object, sym *Symbol, name string, length int, fn CallFn) *Object {
	m := i.newNativeFunc(name, length, fn)
	target.defineOwn(SymKey(sym), &Property{Value: m, Writable: true, Enumerable: false, Configurable: true})
	return m
}

// linkCtor wires a constructor and its prototype together: ctor.prototype =
// proto and proto.constructor = ctor (both non-enumerable).
func linkCtor(ctor, proto *Object) {
	ctor.defineOwn(StrKey("prototype"), &Property{Value: proto, Writable: false, Enumerable: false, Configurable: false})
	proto.defineOwn(StrKey("constructor"), &Property{Value: ctor, Writable: true, Enumerable: false, Configurable: true})
}

// ---------------------------------------------------------------------------
// Argument helpers for native functions
// ---------------------------------------------------------------------------

// arg returns args[n], or undefined when n is out of range.
func arg(args []Value, n int) Value {
	if n < 0 || n >= len(args) {
		return Undef
	}
	return args[n]
}

// argStr converts args[n] to a string.
func (i *Interpreter) argStr(ctx context.Context, args []Value, n int) (string, error) {
	return i.ToStringV(ctx, arg(args, n))
}

// argNum converts args[n] to a number.
func (i *Interpreter) argNum(ctx context.Context, args []Value, n int) (float64, error) {
	return i.ToNumberV(ctx, arg(args, n))
}

// argInt converts args[n] to a truncated integer.
func (i *Interpreter) argInt(ctx context.Context, args []Value, n int) (int, error) {
	f, err := i.argNum(ctx, args, n)
	if err != nil {
		return 0, err
	}
	return int(ToInteger(f)), nil
}

// call invokes a callable value with the given this and args, throwing a
// TypeError when v is not callable.
func (i *Interpreter) call(ctx context.Context, v Value, this Value, args []Value) (Value, error) {
	fn, ok := v.(*Object)
	if !ok || !fn.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", briefValue(v)+" is not a function")
	}
	return fn.fn.call(ctx, this, args)
}
