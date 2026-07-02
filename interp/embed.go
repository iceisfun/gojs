package interp

import "context"

// This file is the embedding API: the surface a Go host uses to move values and
// control across the Go/JavaScript boundary — set and read globals, expose Go
// functions to scripts, call script functions from Go, and convert values.

// HostFunc is the ergonomic signature for a Go function exposed to JavaScript.
// It receives the call arguments and returns a result value or an error; a
// returned error is thrown into the script (wrap a Value with NewThrow to throw
// a specific JS value, or return an ordinary error for a generic Error).
type HostFunc func(args []Value) (Value, error)

// SetGlobal defines a global binding visible to scripts (enumerable, like a
// user-declared global). Use it to install host objects and functions.
func (i *Interpreter) SetGlobal(name string, v Value) {
	i.global.SetData(name, v)
}

// GetGlobal reads a global binding, returning undefined when absent. It checks
// the global lexical environment first (where script-level function/var/let/
// const/class declarations live) and then the global object (host-installed
// globals and built-ins), using the interpreter's context for any accessor
// invocation.
func (i *Interpreter) GetGlobal(name string) Value {
	if b := i.globalEnv.lookup(name); b != nil && b.initialized {
		return b.value
	}
	v, err := i.global.GetStr(i.ctx, name)
	if err != nil {
		return Undef
	}
	return v
}

// NewFunction wraps a Go function as a callable JavaScript value with the given
// name. The wrapper adapts the ergonomic [HostFunc] signature; errors it
// returns are surfaced as thrown exceptions in the script.
func (i *Interpreter) NewFunction(name string, fn HostFunc) *Object {
	return i.newNativeFunc(name, 0, func(_ context.Context, _ Value, args []Value) (Value, error) {
		return fn(args)
	})
}

// NewFunctionRaw wraps a Go function using the full native signature, giving
// access to the call context and `this` receiver. Prefer [NewFunction] unless
// you need those.
func (i *Interpreter) NewFunctionRaw(name string, length int, fn CallFn) *Object {
	return i.newNativeFunc(name, length, fn)
}

// NewPlainObject creates an empty JavaScript object with Object.prototype.
func (i *Interpreter) NewPlainObject() *Object {
	return NewObject(i.objectProto)
}

// NewError creates an Error-family object (name is "Error", "TypeError", …).
func (i *Interpreter) NewError(name, message string) *Object {
	return i.newError(name, message)
}

// Call invokes a callable JavaScript value from Go with the given receiver and
// arguments, returning its result. It is the counterpart to exposing a Go
// function: use it to drive script logic from the host.
func (i *Interpreter) Call(fn Value, this Value, args ...Value) (Value, error) {
	return i.call(i.ctx, fn, this, args)
}

// ---------------------------------------------------------------------------
// Binary data (ArrayBuffer / TypedArray) interop
// ---------------------------------------------------------------------------

// NewArrayBuffer returns a fresh ArrayBuffer that owns a copy of b. The returned
// object is a fully-formed %ArrayBuffer% instance usable from scripts (e.g. as
// the backing store for a DataView or TypedArray).
func (i *Interpreter) NewArrayBuffer(b []byte) *Object {
	data := make([]byte, len(b))
	copy(data, b)
	obj := NewObject(i.arrayBufferProto)
	obj.class = "ArrayBuffer"
	obj.internal = map[string]any{"ArrayBuffer": &arrayBufferData{data: data}}
	return obj
}

// NewUint8Array returns a fresh Uint8Array viewing a fresh ArrayBuffer that owns
// a copy of b. It is the convenient way to hand binary data to a script.
func (i *Interpreter) NewUint8Array(b []byte) *Object {
	buf := i.NewArrayBuffer(b)
	obj := i.allocateTypedArray(taUint8, i.typedArrayKindProtos[taUint8])
	td := obj.typedArray
	td.buffer = buf
	td.byteOffset = 0
	td.arrayLength = len(b)
	return obj
}

// ArrayBufferBytes returns the live backing bytes of an ArrayBuffer. Mutating
// the returned slice mutates the buffer in place. It returns (nil, false) when v
// is not an ArrayBuffer or has been detached.
func (i *Interpreter) ArrayBufferBytes(v Value) ([]byte, bool) {
	ab, ok := arrayBufferOf(v)
	if !ok || ab.detached {
		return nil, false
	}
	return ab.data, true
}

// TypedArrayBytes returns the live bytes viewed by any TypedArray or DataView,
// respecting its byteOffset and (current) byteLength. Mutating the returned
// slice mutates the underlying buffer. It returns (nil, false) when v is not a
// view or the view is out of bounds / detached.
func (i *Interpreter) TypedArrayBytes(v Value) ([]byte, bool) {
	if td, ok := typedArrayOf(v); ok {
		oob, length := td.outOfBounds()
		if oob {
			return nil, false
		}
		ab, ok := arrayBufferOf(td.buffer)
		if !ok || ab.detached {
			return nil, false
		}
		start := td.byteOffset
		end := start + length*taKinds[td.kind].size
		return ab.data[start:end], true
	}
	if dv, ok := dataViewOf(v); ok {
		oob, size := viewOutOfBounds(dv)
		if oob {
			return nil, false
		}
		ab, ok := arrayBufferOf(dv.buffer)
		if !ok || ab.detached {
			return nil, false
		}
		return ab.data[dv.byteOffset : dv.byteOffset+size], true
	}
	return nil, false
}

// DetachArrayBuffer detaches an ArrayBuffer (as the spec's DetachArrayBuffer
// operation does), freeing its data block and rendering every view over it
// out-of-bounds. It reports whether v was an attachable ArrayBuffer. This backs
// host-driven transfer scenarios and the Test262 $262.detachArrayBuffer hook.
func (i *Interpreter) DetachArrayBuffer(v Value) bool {
	ab, ok := arrayBufferOf(v)
	if !ok {
		return false
	}
	ab.data = nil
	ab.detached = true
	return true
}

// ToString converts any value to its JavaScript string form using the
// interpreter's context (running user toString/valueOf if needed).
func (i *Interpreter) ToString(v Value) (string, error) {
	return i.ToStringV(i.ctx, v)
}

// ---------------------------------------------------------------------------
// Value <-> Go conversions
// ---------------------------------------------------------------------------

// ToGo converts a JavaScript value to an idiomatic Go value:
//
//	undefined / null -> nil
//	boolean          -> bool
//	number           -> float64
//	string           -> string
//	array            -> []any
//	other object     -> map[string]any (own enumerable string keys)
//
// Functions and symbols convert to nil. Cyclic objects are not followed past
// the first repeat (the repeat becomes nil).
func (i *Interpreter) ToGo(v Value) any {
	return i.toGo(v, map[*Object]bool{})
}

func (i *Interpreter) toGo(v Value, seen map[*Object]bool) any {
	switch x := v.(type) {
	case Undefined, Null:
		return nil
	case Boolean:
		return bool(x)
	case Number:
		return float64(x)
	case String:
		return string(x)
	case *BigInt:
		return x.Int
	case *Object:
		if x.IsCallable() {
			return nil
		}
		if seen[x] {
			return nil
		}
		seen[x] = true
		defer delete(seen, x)
		if x.isArray {
			out := make([]any, len(x.elems))
			for idx := range x.elems {
				out[idx] = i.toGo(elemAt(x, idx), seen)
			}
			return out
		}
		out := map[string]any{}
		for _, name := range x.OwnKeys() {
			if p, ok := x.getOwn(StrKey(name)); ok && p.Enumerable {
				val, _ := x.GetStr(i.ctx, name)
				out[name] = i.toGo(val, seen)
			}
		}
		return out
	default:
		return nil
	}
}

// FromGo converts an idiomatic Go value to a JavaScript value:
//
//	nil                       -> null
//	bool                      -> boolean
//	int kinds / float kinds   -> number
//	string                    -> string
//	[]any / []T               -> array
//	map[string]any / map[string]T -> object
//	Value                     -> returned unchanged
//
// Unsupported types convert to undefined.
func (i *Interpreter) FromGo(x any) Value {
	switch v := x.(type) {
	case nil:
		return Nul
	case Value:
		return v
	case bool:
		return Bool(v)
	case int:
		return Number(float64(v))
	case int8:
		return Number(float64(v))
	case int16:
		return Number(float64(v))
	case int32:
		return Number(float64(v))
	case int64:
		return Number(float64(v))
	case uint:
		return Number(float64(v))
	case uint8:
		return Number(float64(v))
	case uint16:
		return Number(float64(v))
	case uint32:
		return Number(float64(v))
	case uint64:
		return Number(float64(v))
	case float32:
		return Number(float64(v))
	case float64:
		return Number(v)
	case string:
		return String(v)
	case []any:
		elems := make([]Value, len(v))
		for idx, e := range v {
			elems[idx] = i.FromGo(e)
		}
		return i.newArray(elems)
	case []string:
		elems := make([]Value, len(v))
		for idx, e := range v {
			elems[idx] = String(e)
		}
		return i.newArray(elems)
	case []float64:
		elems := make([]Value, len(v))
		for idx, e := range v {
			elems[idx] = Number(e)
		}
		return i.newArray(elems)
	case map[string]any:
		o := i.NewPlainObject()
		for k, val := range v {
			o.SetData(k, i.FromGo(val))
		}
		return o
	default:
		return Undef
	}
}
