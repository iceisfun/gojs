package interp

import (
	"strings"
	"unsafe"
)

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
	// means uncomputed. Populated on first observation.
	utf16Len int32
	hash     uint64
	// units is the cached UTF-16 code-unit view of a non-ASCII string, built once
	// on first indexing so a `for (i<s.length) s.charCodeAt(i)` walk is O(1) per
	// step instead of re-materializing the slice each call. nil until needed (and
	// never needed for an ASCII string, whose byte index is its code-unit index).
	units []uint16
	kind  strKind
	flags uint8
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

// metaStrThreshold is the byte length at or above which a freshly-materialized
// string is boxed as a metadata-bearing *vmString rather than returned as a bare
// String. Below it, a repeated .length scan is cheap and boxing would only add an
// allocation; above it, caching the UTF-16 length turns an O(n)-per-read into
// O(1) (the difference between an O(n²) and O(n) `for (i<s.length)` loop).
const metaStrThreshold = 64

// newComputedString wraps already-canonical, freshly-materialized bytes as a
// string primitive. Large results become a flat *vmString whose UTF-16 length
// and ASCII-ness are computed once, lazily, on first observation and then cached;
// small results stay a bare String. The caller must pass canonical WTF-8 (every
// producer canonicalizes before calling).
func newComputedString(s string) Value {
	if len(s) < metaStrThreshold {
		return String(s)
	}
	return &vmString{kind: strFlat, flat: s, length: len(s), utf16Len: -1, flags: fFlattened | fCanonical}
}

// NewReadOnlyString wraps host bytes as a JavaScript string. It COPIES b, so the
// caller may reuse or mutate b freely afterward — the safe default for handing
// host data (an HTTP body, a file's contents, a Go→VM argument) to a script. The
// result is a metadata-bearing flat string. b must be valid UTF-8.
//
// A []byte is mutable and its backing is not copy-on-write, so a zero-copy borrow
// of one is a "dangerous primitive": if the host later mutates b, an immutable JS
// string would observably change, and a borrowed string reaching a durable sink
// (a property key, a Map key, an object slot) would alias the mutable backing —
// Go maps store the string header, they do not copy its bytes. Copying here shuts
// that door. Use NewBorrowedString only when you can honor its lifetime contract.
func NewReadOnlyString(b []byte) Value {
	return newComputedString(string(b)) // string(b) copies; canonical since valid UTF-8
}

// NewBorrowedString wraps host bytes as a JavaScript string WITHOUT copying, via
// an unsafe borrow of b's backing array. It is the zero-copy path for a large
// buffer whose lifetime the host controls, but it is UNSAFE: the caller MUST NOT
// mutate or free b for as long as the returned string (or any string derived from
// it) is reachable from the VM, and b MUST be valid UTF-8. Because the engine
// never writes to a string, reads are always safe; the hazard is solely the host
// mutating the shared backing. When in doubt use NewReadOnlyString, which copies.
func NewBorrowedString(b []byte) Value {
	if len(b) == 0 {
		return String("")
	}
	s := unsafe.String(&b[0], len(b))
	return &vmString{kind: strExternal, flat: s, length: len(b), utf16Len: -1, flags: fFlattened | fCanonical}
}

// newSliceString returns the byte span [off, off+blen) of a string primitive.
// Go's string slicing already shares the parent's backing array (zero copy), so
// a large window is boxed as a metadata-bearing flat string that borrows those
// bytes. A small window into a large parent is instead *copied*, so a one-line
// substring of a multi-megabyte string does not pin the whole parent alive.
func newSliceString(parent Value, off, blen int) Value {
	ps := stringValue(parent)
	span := ps[off : off+blen]
	if blen < sliceMinBytes || blen*sliceShareInvFrac < len(ps) {
		span = strings.Clone(span) // detach from the parent's backing
	}
	return newComputedString(span)
}

const (
	sliceMinBytes     = 256 // below this, always copy (parent-pinning not worth it)
	sliceShareInvFrac = 4   // share only if the window is >= 1/4 of the parent
)

// codeUnitLen returns the string's UTF-16 length, computing it (and the ASCII
// flag) once and caching both.
func (r *vmString) codeUnitLen() int {
	if r.utf16Len >= 0 {
		return int(r.utf16Len)
	}
	n, ascii := codeUnitLenASCII(r.build())
	r.utf16Len = int32(n)
	if ascii {
		r.flags |= fASCII
	}
	r.flags |= fASCIIKnown
	return n
}

// isASCII reports whether the string is pure ASCII, caching the result.
func (r *vmString) isASCII() bool {
	if r.flags&fASCIIKnown != 0 {
		return r.flags&fASCII != 0
	}
	s := r.build()
	ascii := isASCIIStr(s)
	if ascii {
		r.flags |= fASCII
	}
	r.flags |= fASCIIKnown
	return ascii
}

// codeUnitLenValue returns the UTF-16 length of any string primitive, consulting
// the cache when v is a *vmString.
func codeUnitLenValue(v Value) int {
	switch x := v.(type) {
	case String:
		return codeUnitLen(string(x))
	case *vmString:
		return x.codeUnitLen()
	}
	return 0
}

// view returns the string's UTF-16 code-unit view, caching the materialized
// code-unit slice for a non-ASCII string so repeated indexing is O(1) per access.
func (r *vmString) view() utf16View {
	s := r.build()
	if r.isASCII() {
		return utf16View{s: s, ascii: true}
	}
	if r.units == nil {
		r.units = codeUnits(s)
	}
	return utf16View{s: s, units: r.units}
}

// viewOfValue returns the code-unit view of a string primitive, using the cached
// view of a *vmString and falling back to a fresh scan for a bare String.
func viewOfValue(recv Value, s string) utf16View {
	if r, ok := recv.(*vmString); ok {
		return r.view()
	}
	return viewOf(s)
}

// stringReceiver returns the string-primitive Value underlying a String.prototype
// `this` — a String, a *vmString, or the primitive of a String wrapper object —
// WITHOUT flattening a *vmString, so its cached metadata survives into the method.
func stringReceiver(this Value) (Value, bool) {
	switch x := this.(type) {
	case String:
		return x, true
	case *vmString:
		return x, true
	case *Object:
		if isStringish(x.primitive) {
			return x.primitive, true
		}
	}
	return nil, false
}

// stringCharAt returns the one-code-unit String at code-unit index idx of s (a
// String at idx+1 span), or undefined when out of range. ascii selects the
// no-alloc byte path; callers pass a cached flag when they have one.
func stringCharAt(s string, ascii bool, idx int) Value {
	if idx < 0 {
		return Undef
	}
	if ascii {
		if idx < len(s) {
			return String(s[idx : idx+1])
		}
		return Undef
	}
	units := codeUnits(s)
	if idx < len(units) {
		return String(unitsToString(units[idx : idx+1]))
	}
	return Undef
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
