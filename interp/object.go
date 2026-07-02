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
// are equal only by pointer identity.
type Symbol struct {
	Desc string // optional description, for debugging and Symbol.prototype.toString
	// HasDesc reports whether a description was supplied. It distinguishes
	// Symbol() (description undefined) from Symbol("") (empty-string
	// description), which Symbol.prototype.description must report differently.
	HasDesc bool
	// Registered marks a symbol produced by Symbol.for and held in the
	// GlobalSymbolRegistry. Registered symbols are excluded from CanBeHeldWeakly
	// (§7.3.11): they can never be reclaimed, so keying a WeakMap/WeakSet or
	// registering a WeakRef/FinalizationRegistry target with one is a TypeError.
	Registered bool
}

// canBeHeldWeakly implements CanBeHeldWeakly (§7.3.11): a value may serve as a
// weak-collection key, WeakRef target, or FinalizationRegistry target if it is
// an Object or a Symbol that is not in the GlobalSymbolRegistry.
func canBeHeldWeakly(v Value) bool {
	switch x := v.(type) {
	case *Object:
		return true
	case *Symbol:
		return !x.Registered
	default:
		return false
	}
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
	// lengthNonWritable records that an Array's "length" property has had its
	// [[Writable]] attribute set to false (via defineProperty). Length is
	// otherwise a writable, non-enumerable, non-configurable data property.
	lengthNonWritable bool
	primitive Value          // wrapped primitive (String/Number/Boolean/Date)
	internal  map[string]any // misc internal slots (RegExp source, Map data, ...)

	// proxy is non-nil for a Proxy exotic object; it routes every essential
	// internal method through the handler's traps (see builtin_proxy.go).
	proxy *proxyState

	// typedArray is non-nil for an integer-indexed (TypedArray) exotic object.
	// When set, canonical numeric index property keys are served from the
	// viewed ArrayBuffer's bytes rather than ordinary storage; see
	// builtin_typedarray.go.
	typedArray *typedArrayData

	// private holds ECMAScript private class elements (#fields, #methods, and
	// private accessors), keyed by PrivateName identity. These are not ordinary
	// properties: they are invisible to property enumeration, [[Get]]/[[Set]],
	// hasOwnProperty, and JSON, and are guarded by a brand check on access.
	private map[*PrivateName]*Property
}

// PrivateName is the unique identity of a private class element (#x). Each
// evaluation of a class body mints fresh PrivateNames for the names it declares,
// so the same textual name produced by two evaluations of the same class are
// distinct identities that fail each other's brand checks (ECMA-262 uses a
// PrivateName value for exactly this). desc is the "#name" text, kept only for
// diagnostics.
type PrivateName struct {
	desc string
}

// String returns the textual private name (e.g. "#x").
func (p *PrivateName) String() string { return p.desc }

// getPrivate returns the descriptor for the private element pn, or (nil, false)
// when the object does not carry that private brand.
func (o *Object) getPrivate(pn *PrivateName) (*Property, bool) {
	if o.private == nil {
		return nil, false
	}
	p, ok := o.private[pn]
	return p, ok
}

// hasPrivate reports whether the object carries the private element pn.
func (o *Object) hasPrivate(pn *PrivateName) bool {
	_, ok := o.getPrivate(pn)
	return ok
}

// definePrivate installs (or replaces) a private element descriptor.
func (o *Object) definePrivate(pn *PrivateName, p *Property) {
	if o.private == nil {
		o.private = make(map[*PrivateName]*Property)
	}
	o.private[pn] = p
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
// Array holes (sparse arrays)
// ---------------------------------------------------------------------------

// holeElem is the sentinel stored in an array's dense element slice to mark an
// absent index — a "hole" — in a sparse array. It is distinct from undefined:
// [[Get]] on a hole reads through the prototype chain (normally yielding
// undefined), but `in` / hasOwnProperty / Object.keys report the index as
// absent, and the HasProperty-family iteration methods skip it. The sentinel is
// confined to the interpreter and must never be handed to user code; helpers
// such as elemAt and undefIfHole enforce that boundary.
type holeElem struct{}

// Typeof reports "undefined" so an accidental leak degrades gracefully.
func (holeElem) Typeof() string { return "undefined" }

// theHole is the single hole sentinel value.
var theHole Value = holeElem{}

// isHole reports whether v is the array-hole sentinel.
func isHole(v Value) bool { _, ok := v.(holeElem); return ok }

// undefIfHole maps the hole sentinel to undefined, passing any other value
// through unchanged. It is the boundary that keeps holes from escaping to user
// code.
func undefIfHole(v Value) Value {
	if isHole(v) {
		return Undef
	}
	return v
}

// elemAt returns the element at index j with the hole sentinel mapped to
// undefined. The caller must ensure j is within range.
func elemAt(o *Object, j int) Value { return undefIfHole(o.elems[j]) }

// denseCopy returns a copy of the element slice with holes densified to
// undefined, matching the [[Get]]-over-0..len behavior of the copying array
// methods (toReversed, toSorted, toSpliced, with).
func (o *Object) denseCopy() []Value {
	out := make([]Value, len(o.elems))
	for j := range o.elems {
		out[j] = undefIfHole(o.elems[j])
	}
	return out
}

// ---------------------------------------------------------------------------
// Own-property access
// ---------------------------------------------------------------------------

// getOwn returns the own-property descriptor for key, synthesizing descriptors
// for array elements and array length on demand.
func (o *Object) getOwn(key PropertyKey) (*Property, bool) {
	// A TypedArray serves canonical numeric index keys from its backing buffer
	// (IntegerIndexed [[GetOwnProperty]], §10.4.5.1). An out-of-bounds index is
	// absent; other keys fall through to ordinary storage.
	if o.typedArray != nil && !key.IsSymbol() {
		if n, ok := canonicalNumericIndex(key.Str); ok {
			if idx, ok := o.typedArray.validIndex(n); ok {
				return &Property{Value: o.typedArray.getElement(idx), Writable: true, Enumerable: true, Configurable: true}, true
			}
			return nil, false
		}
	}
	if o.isArray && !key.IsSymbol() {
		if key.Str == "length" {
			return &Property{Value: Number(float64(len(o.elems))), Writable: !o.lengthNonWritable}, true
		}
		if idx, ok := arrayIndex(key.Str); ok {
			// A de-optimized index (redefined with non-default attributes or as an
			// accessor) lives in the ordinary props map and shadows dense storage.
			if p, ok := o.props[key]; ok {
				return p, true
			}
			if idx < len(o.elems) {
				if isHole(o.elems[idx]) {
					return nil, false // hole: not an own property
				}
				return &Property{Value: o.elems[idx], Writable: true, Enumerable: true, Configurable: true}, true
			}
			return nil, false
		}
	}
	p, ok := o.props[key]
	return p, ok
}

// defineOwn installs or replaces an own-property descriptor, preserving
// insertion order for new keys.
func (o *Object) defineOwn(key PropertyKey, p *Property) {
	// De-optimize an array index: once it carries a descriptor in the props map
	// (because it was redefined with non-default attributes or as an accessor),
	// dense storage must not shadow it, so its dense slot becomes a hole. getOwn
	// consults props first for such indices.
	if o.isArray && !key.IsSymbol() {
		if idx, ok := arrayIndex(key.Str); ok && idx < len(o.elems) {
			o.elems[idx] = theHole
		}
	}
	if _, exists := o.props[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.props[key] = p
}

// installProperty stores the finalized descriptor p for key, choosing dense
// array-element storage when an array index can be represented with the default
// element attributes ({writable, enumerable, configurable} all true, data
// property) and de-optimizing into the ordinary props map otherwise. Either way
// the array's length is extended to cover a de-optimized index (the Array
// [[DefineOwnProperty]] length coupling, §10.4.2.1), except for a far-out index
// whose dense backing would be pathologically large.
func (o *Object) installProperty(key PropertyKey, p *Property) {
	if o.isArray && !key.IsSymbol() {
		if idx, ok := arrayIndex(key.Str); ok {
			far := idx >= len(o.elems) && idx >= maxDenseArrayLen
			if !p.Accessor && p.Writable && p.Enumerable && p.Configurable && !far {
				// Dense-representable: drop any de-optimized shadow and store the
				// value in the dense element backing.
				o.removeProp(key)
				o.ensureLen(idx + 1)
				o.elems[idx] = p.Value
				return
			}
			// De-optimize into the props map. Extend length to cover the index
			// (unless far-out) so defineOwn holes the now-covered dense slot.
			if !far && idx >= len(o.elems) {
				o.ensureLen(idx + 1)
			}
			o.defineOwn(key, p)
			return
		}
	}
	o.defineOwn(key, p)
}

// removeProp deletes key from the ordinary props map and the insertion-order
// list, leaving array dense storage untouched. It is a no-op when absent.
func (o *Object) removeProp(key PropertyKey) {
	if _, ok := o.props[key]; !ok {
		return
	}
	delete(o.props, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
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
		if idx, ok := arrayIndex(key.Str); ok {
			// A de-optimized index lives in props; fall through to remove it there.
			if _, deopt := o.props[key]; !deopt && idx < len(o.elems) {
				o.elems[idx] = theHole // create a hole (length is unchanged)
				return true
			}
		}
	}
	if _, ok := o.props[key]; !ok {
		return false
	}
	o.removeProp(key)
	return true
}

// OwnKeys returns the own enumerable and non-enumerable string keys in the
// order mandated by the spec: integer indices ascending, then other string keys
// in insertion order. Symbol keys are excluded.
func (o *Object) OwnKeys() []string {
	// A TypedArray enumerates its in-bounds integer indices (0..length-1)
	// ascending, then its ordinary non-index string keys in insertion order
	// (§10.4.5.6). A canonical numeric index is never stored ordinarily.
	if o.typedArray != nil {
		var out []string
		if oob, length := o.typedArray.outOfBounds(); !oob {
			for j := 0; j < length; j++ {
				out = append(out, intToStr(j))
			}
		}
		for _, k := range o.keys {
			if !k.IsSymbol() {
				out = append(out, k.Str)
			}
		}
		return out
	}
	var indices []int
	var strs []string
	if o.isArray {
		for i, v := range o.elems {
			if v != nil && !isHole(v) {
				indices = append(indices, i) // holes are absent, so excluded
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

// ensureLen grows the element slice to at least n, padding new slots with holes
// (a gap created by an out-of-bounds index write or a length extension is
// sparse, not filled with explicit undefined).
func (o *Object) ensureLen(n int) {
	for len(o.elems) < n {
		o.elems = append(o.elems, theHole)
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
	} else if n <= maxDenseArrayLen {
		o.ensureLen(n)
	}
	// Growing beyond the dense limit is refused rather than eagerly allocating
	// billions of holes; such lengths are unsupported by the dense backing.
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
