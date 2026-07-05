package interp

import "strings"

// vmString is gojs's rich, immutable string primitive: the representation a JS
// string takes when it is more than a plain flat literal. A JS string VALUE is
// immutable, but a vmString's *representation* may be refined — a cons flattened,
// a length cached — without changing the value it denotes. All cached fields are
// therefore safe to populate lazily; the interpreter runs each string on a single
// goroutine (SharedArrayBuffer shares bytes, not string objects), so no locking
// is needed.
//
// Most strings in a program stay the bare `String` value type (no indirection,
// usable directly as a property key). A *vmString appears only when a string is
//
//   - built lazily by concatenation (kind cons — the classic "cons-string"/rope
//     produced by `+`, join, and template literals),
//   - a materialized result carrying cached metadata (kind flat),
//   - a byte span shared with a parent (kind slice — slice/substring/charAt), or
//   - a read-only borrow of host-owned bytes (kind external — fetch, Go interop).
//
// Its Typeof reports "string", and every string-consuming abstract operation
// flattens it to a String at the boundary (see flattenRope and the
// ToPrimitive/ToStringV/equality paths).
type vmString struct {
	// cons: children, each a String or *vmString. left is written first.
	left, right Value
	// slice: source string (String or *vmString) and byte span within it.
	parent Value
	off    int
	// flat/external: the materialized bytes. For cons/slice, `flat` also serves
	// as the memoized flatten cache once build() has run (fFlattened is set).
	flat string
	// length is the flattened BYTE length; always known at construction so
	// concatenation stays O(1).
	length int
	// Cached, lazily-computed metadata. utf16Len < 0 means unknown; hash == 0
	// means uncomputed. Populated on first observation (Phase 3).
	utf16Len int32
	hash     uint64
	kind     strKind
	flags    uint8
}

// strKind tags a vmString's representation. The zero value is strCons so an
// &vmString{left, right, length} built the classic rope way is a cons node.
type strKind uint8

const (
	strCons     strKind = iota // left+right concatenation (rope node)
	strFlat                    // fully materialized bytes in `flat`, with metadata
	strSlice                   // a [off, off+length) byte span of `parent`
	strExternal                // read-only borrow of host-owned bytes in `flat`
)

const (
	fFlattened  uint8 = 1 << iota // `flat` holds the complete, canonical bytes
	fCanonical                    // bytes are known canonical WTF-8 (no coalescible pair)
	fASCIIKnown                   // the fASCII bit is meaningful
	fASCII                        // all bytes < 0x80 (⇒ Latin-1, and utf16Len == length)
)

func (*vmString) Typeof() string { return "string" }

// stringByteLen returns the flattened byte length of a String or *vmString.
func stringByteLen(v Value) int {
	switch x := v.(type) {
	case String:
		return len(x)
	case *vmString:
		return x.length
	}
	return 0
}

// isStringish reports whether v is already a string primitive (a String or a
// *vmString) and therefore needs no ToPrimitive/ToString coercion.
func isStringish(v Value) bool {
	switch v.(type) {
	case String, *vmString:
		return true
	}
	return false
}

// stringValue returns the Go string value of a string primitive, flattening a
// *vmString. It panics if v is not stringish; callers must have checked.
func stringValue(v Value) string {
	switch x := v.(type) {
	case String:
		return string(x)
	case *vmString:
		return x.build()
	}
	return ""
}

// concatStrings builds the concatenation of two string primitives as a cons node
// in O(1), collapsing an empty operand so a node never wraps "".
func concatStrings(l, r Value) Value {
	ln, rn := stringByteLen(l), stringByteLen(r)
	if ln == 0 {
		return r
	}
	if rn == 0 {
		return l
	}
	return &vmString{kind: strCons, left: l, right: r, length: ln + rn, utf16Len: -1}
}

// flattenRope resolves a *vmString to its String; any other value passes through
// unchanged. Use it at the boundary of code that needs a concrete String.
func flattenRope(v Value) Value {
	if r, ok := v.(*vmString); ok {
		return String(r.build())
	}
	return v
}

// build flattens a vmString to its Go string, memoizing the result so repeated
// boundary crossings of the same value are O(1). The value is immutable, so the
// cache can never go stale.
func (r *vmString) build() string {
	if r.flags&fFlattened != 0 {
		return r.flat
	}
	switch r.kind {
	case strFlat, strExternal:
		// Bytes were materialized at construction; just mark them flattened.
		r.flags |= fFlattened
		return r.flat
	case strSlice:
		// A slice is a byte span of its parent's flattened bytes. The span is
		// taken at code-unit boundaries by the constructor, so canonicalization
		// only has to fix a surrogate pair that the span itself now abuts.
		ps := stringValue(r.parent)
		r.flat = canonicalizeWTF8(ps[r.off : r.off+r.length])
		r.flags |= fFlattened | fCanonical
		r.parent = nil // release the parent once materialized
		return r.flat
	default: // strCons
		var b strings.Builder
		b.Grow(r.length)
		// Walk the tree with an explicit stack (not recursion) so a left-leaning
		// `((a+b)+c)+…` chain of any depth — the exact shape `s += chunk`
		// produces — cannot overflow the Go stack.
		stack := []Value{r}
		for len(stack) > 0 {
			n := len(stack) - 1
			v := stack[n]
			stack = stack[:n]
			switch x := v.(type) {
			case String:
				b.WriteString(string(x))
			case *vmString:
				if x.kind == strCons && x.flags&fFlattened == 0 {
					stack = append(stack, x.right, x.left) // left popped first
				} else {
					b.WriteString(x.build()) // flat/slice/external/already-flat child
				}
			}
		}
		// Concatenation may have placed a high surrogate immediately before a low
		// surrogate across a join; coalesce so the result is canonical WTF-8.
		r.flat = canonicalizeWTF8(b.String())
		r.flags |= fFlattened | fCanonical
		r.left, r.right = nil, nil // release the tree once materialized
		return r.flat
	}
}
