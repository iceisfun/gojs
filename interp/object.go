package interp

import (
	"context"
	"math/big"
)

// This file defines the object model: [*Object] (used for plain objects,
// arrays, functions, and wrappers), property descriptors, property keys, plus
// the [Symbol] and [BigInt] primitive reference types.

// ---------------------------------------------------------------------------
// Symbols and BigInt
// ---------------------------------------------------------------------------

// Symbol is a unique, immutable primitive used as a property key. Two symbols
// are equal only by pointer identity (except registered symbols, not yet
// implemented).
type Symbol struct {
	Desc string // optional description, for debugging and Symbol.prototype.toString
}

// Typeof returns "symbol".
func (*Symbol) Typeof() string { return "symbol" }

// BigInt is an arbitrary-precision integer primitive. It wraps a math/big.Int.
type BigInt struct {
	Int *big.Int
}

// Typeof returns "bigint".
func (*BigInt) Typeof() string { return "bigint" }

// sign returns -1, 0, or +1. It is used by ToBoolean.
func (b *BigInt) sign() int { return b.Int.Sign() }

// NewBigInt returns a BigInt with the given int64 value.
func NewBigInt(v int64) *BigInt { return &BigInt{Int: big.NewInt(v)} }

// ---------------------------------------------------------------------------
// Property keys
// ---------------------------------------------------------------------------

// PropertyKey identifies an own property. Exactly one of Sym/Str is
// significant: when Sym is non-nil the key is a symbol, otherwise it is the
// string in Str.
type PropertyKey struct {
	Str string
	Sym *Symbol
}

// StrKey returns a string property key.
func StrKey(s string) PropertyKey { return PropertyKey{Str: s} }

// SymKey returns a symbol property key.
func SymKey(s *Symbol) PropertyKey { return PropertyKey{Sym: s} }

// IsSymbol reports whether the key is a symbol key.
func (k PropertyKey) IsSymbol() bool { return k.Sym != nil }

// ---------------------------------------------------------------------------
// Property descriptors
// ---------------------------------------------------------------------------

// Property is an own-property descriptor. A property is either a data property
// (Value + Writable) or an accessor property (Get/Set) when Accessor is true.
type Property struct {
	Value        Value
	Get          *Object
	Set          *Object
	Writable     bool
	Enumerable   bool
	Configurable bool
	Accessor     bool
}

// ---------------------------------------------------------------------------
// CallFn — the uniform callable signature
// ---------------------------------------------------------------------------

// CallFn is the signature shared by native (Go) and script (JS) callables. this
// is the receiver; args are the actual arguments. Native functions may return a
// Go error, which the interpreter converts into a thrown JavaScript value; a
// [*Throw] error carries an explicit JS value.
type CallFn func(ctx context.Context, this Value, args []Value) (Value, error)

// functionData holds the internal slots of a callable object.
type functionData struct {
	call      CallFn // [[Call]]
	construct CallFn // [[Construct]] (nil when not constructable)
	name      string
	length    int  // declared parameter count (arity)
	ctor      bool // whether the function can be used with `new`
}

// ---------------------------------------------------------------------------
// Object
// ---------------------------------------------------------------------------

// Object is the representation of every non-primitive JavaScript value. The
// class field records the object's kind ("Object", "Array", "Function",
// "Error", ...). Arrays additionally use elems for dense element storage;
// functions use fn; boxed primitives and Date use primitive.
type Object struct {
	proto      *Object
	keys       []PropertyKey // own-property insertion order
	props      map[PropertyKey]*Property
	extensible bool
	class      string

	fn        *functionData // non-nil for callable objects
	elems     []Value       // dense element storage for arrays
	isArray   bool
	primitive Value          // wrapped primitive (String/Number/Boolean/Date)
	internal  map[string]any // misc internal slots (RegExp source, Map data, ...)
}

// NewObject creates a bare object with the given prototype (which may be nil).
func NewObject(proto *Object) *Object {
	return &Object{
		proto:      proto,
		props:      make(map[PropertyKey]*Property),
		extensible: true,
		class:      "Object",
	}
}

// Typeof returns "function" for callable objects and "object" otherwise.
func (o *Object) Typeof() string {
	if o.fn != nil {
		return "function"
	}
	return "object"
}

// Proto returns the object's prototype (may be nil).
func (o *Object) Proto() *Object { return o.proto }

// SetProto sets the object's prototype.
func (o *Object) SetProto(p *Object) { o.proto = p }

// Class returns the internal class string.
func (o *Object) Class() string { return o.class }

// IsCallable reports whether the object can be called.
func (o *Object) IsCallable() bool { return o.fn != nil }

// IsConstructor reports whether the object can be used with `new`.
func (o *Object) IsConstructor() bool { return o.fn != nil && o.fn.construct != nil }

// IsArray reports whether the object is an Array exotic object.
func (o *Object) IsArray() bool { return o.isArray }

// ---------------------------------------------------------------------------
// Own-property access
// ---------------------------------------------------------------------------

// getOwn returns the own-property descriptor for key, synthesizing descriptors
// for array elements and array length on demand.
func (o *Object) getOwn(key PropertyKey) (*Property, bool) {
	if o.isArray && !key.IsSymbol() {
		if key.Str == "length" {
			return &Property{Value: Number(float64(len(o.elems))), Writable: true}, true
		}
		if idx, ok := arrayIndex(key.Str); ok && idx < len(o.elems) {
			return &Property{Value: o.elems[idx], Writable: true, Enumerable: true, Configurable: true}, true
		}
	}
	p, ok := o.props[key]
	return p, ok
}

// defineOwn installs or replaces an own-property descriptor, preserving
// insertion order for new keys.
func (o *Object) defineOwn(key PropertyKey, p *Property) {
	if _, exists := o.props[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.props[key] = p
}

// SetData defines (or overwrites) an enumerable, writable, configurable data
// property. It is the common path for populating objects from Go.
func (o *Object) SetData(name string, v Value) {
	key := StrKey(name)
	if o.isArray {
		if name == "length" {
			o.setArrayLength(v)
			return
		}
		if idx, ok := arrayIndex(name); ok {
			o.ensureLen(idx + 1)
			o.elems[idx] = v
			return
		}
	}
	if p, ok := o.props[key]; ok && !p.Accessor {
		p.Value = v
		return
	}
	o.defineOwn(key, &Property{Value: v, Writable: true, Enumerable: true, Configurable: true})
}

// SetHidden defines a non-enumerable data property (used for methods and
// internal wiring like "constructor").
func (o *Object) SetHidden(name string, v Value) {
	o.defineOwn(StrKey(name), &Property{Value: v, Writable: true, Enumerable: false, Configurable: true})
}

// deleteOwn removes an own property, returning whether it existed.
func (o *Object) deleteOwn(key PropertyKey) bool {
	if o.isArray && !key.IsSymbol() {
		if idx, ok := arrayIndex(key.Str); ok && idx < len(o.elems) {
			o.elems[idx] = Undef // create a hole
			return true
		}
	}
	if _, ok := o.props[key]; !ok {
		return false
	}
	delete(o.props, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	return true
}

// OwnKeys returns the own enumerable and non-enumerable string keys in the
// order mandated by the spec: integer indices ascending, then other string keys
// in insertion order. Symbol keys are excluded.
func (o *Object) OwnKeys() []string {
	var indices []int
	var strs []string
	if o.isArray {
		for i, v := range o.elems {
			if v != nil && !IsUndefined(v) {
				indices = append(indices, i)
			} else if v != nil {
				indices = append(indices, i)
			}
		}
	}
	for _, k := range o.keys {
		if k.IsSymbol() {
			continue
		}
		if idx, ok := arrayIndex(k.Str); ok {
			indices = append(indices, idx)
		} else {
			strs = append(strs, k.Str)
		}
	}
	sortInts(indices)
	out := make([]string, 0, len(indices)+len(strs))
	for _, i := range indices {
		out = append(out, intToStr(i))
	}
	out = append(out, strs...)
	return out
}

// ---------------------------------------------------------------------------
// Array helpers
// ---------------------------------------------------------------------------

// ensureLen grows the element slice to at least n, padding with undefined.
func (o *Object) ensureLen(n int) {
	for len(o.elems) < n {
		o.elems = append(o.elems, Undef)
	}
}

// setArrayLength adjusts an array's length, truncating or padding elements.
func (o *Object) setArrayLength(v Value) {
	n := int(ToInteger(ToNumber(v)))
	if n < 0 {
		n = 0
	}
	if n < len(o.elems) {
		o.elems = o.elems[:n]
	} else {
		o.ensureLen(n)
	}
}

// ArrayLen returns the length of an array object.
func (o *Object) ArrayLen() int { return len(o.elems) }

// arrayIndex parses s as a canonical array index (a non-negative integer with
// no leading zeros, below 2^32-1), returning the index and whether it matched.
func arrayIndex(s string) (int, bool) {
	if s == "" || len(s) > 10 {
		return 0, false
	}
	if s == "0" {
		return 0, true
	}
	if s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
