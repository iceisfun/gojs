package interp

import (
	"strings"
	"unicode/utf8"
)

// ECMAScript strings are sequences of UTF-16 code units, but gojs stores String
// as (WTF-8) bytes and Go's own string APIs work over runes/scalar values. The
// utf16View type below provides the canonical code-unit view the spec requires,
// so the indexing- and length-sensitive String operations (charCodeAt, slice,
// length, s[i], …) match exactly for astral characters and lone surrogates.
//
// Storage is WTF-8: a Unicode scalar value uses standard UTF-8, and a lone
// surrogate (U+D800..U+DFFF), which has no UTF-8 encoding, is stored as the
// 3-byte sequence 0xED 0xA0-0xBF 0x80-0xBF (see String.fromCharCode). These
// helpers understand that sequence; the standard library does not.
//
// The overwhelmingly common case is a pure-ASCII string, where byte index ==
// code-unit index == rune index. utf16View detects that and works directly on
// the backing bytes, allocating no []uint16 at all. Only a string containing a
// non-ASCII byte materializes the code-unit slice.

// isASCIIStr reports whether s contains only bytes < 0x80.
func isASCIIStr(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// wtf8Surrogate decodes a WTF-8-encoded lone surrogate at s[i] and returns its
// code unit plus true, or 0 and false if s[i] does not begin one.
func wtf8Surrogate(s string, i int) (uint16, bool) {
	if i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
		return uint16(rune(s[i]&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)), true
	}
	return 0, false
}

// codeUnits interprets s as the sequence of UTF-16 code units it denotes. A
// scalar value >= U+10000 becomes a surrogate pair; a WTF-8-encoded lone
// surrogate is recovered as a single surrogate code unit.
func codeUnits(s string) []uint16 {
	if isASCIIStr(s) {
		u := make([]uint16, len(s))
		for i := 0; i < len(s); i++ {
			u[i] = uint16(s[i])
		}
		return u
	}
	units := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		if cu, ok := wtf8Surrogate(s, i); ok {
			units = append(units, cu)
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			r -= 0x10000
			units = append(units, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			units = append(units, uint16(r))
		}
		i += size
	}
	return units
}

// codeUnitLen returns the number of UTF-16 code units in s without allocating.
// This is the ECMAScript "length" of the string.
func codeUnitLen(s string) int {
	if isASCIIStr(s) {
		return len(s)
	}
	n := 0
	for i := 0; i < len(s); {
		if _, ok := wtf8Surrogate(s, i); ok {
			n++
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		i += size
	}
	return n
}

// appendCodeUnit writes one UTF-16 code unit to buf as WTF-8: a lone surrogate
// becomes its 3-byte encoding, any other code unit its normal UTF-8.
func appendCodeUnit(buf []byte, cu uint16) []byte {
	if cu >= 0xD800 && cu <= 0xDFFF {
		return append(buf, 0xE0|byte(cu>>12), 0x80|byte((cu>>6)&0x3F), 0x80|byte(cu&0x3F))
	}
	return utf8.AppendRune(buf, rune(cu))
}

// unitsToString re-encodes a UTF-16 code-unit slice to a (WTF-8) string. An
// adjacent high/low surrogate pair is combined into its astral scalar value;
// any other surrogate is emitted as a lone surrogate. Inverse of codeUnits.
func unitsToString(units []uint16) string {
	if len(units) == 0 {
		return ""
	}
	buf := make([]byte, 0, len(units))
	for k := 0; k < len(units); {
		cu := units[k]
		if cu >= 0xD800 && cu <= 0xDBFF && k+1 < len(units) && units[k+1] >= 0xDC00 && units[k+1] <= 0xDFFF {
			cp := 0x10000 + (rune(cu)-0xD800)<<10 + (rune(units[k+1]) - 0xDC00)
			buf = utf8.AppendRune(buf, cp)
			k += 2
			continue
		}
		buf = appendCodeUnit(buf, cu)
		k++
	}
	return string(buf)
}

// canonicalizeWTF8 rewrites s to well-formed WTF-8: an adjacent high+low
// surrogate pair — which concatenation can leave encoded as two separate 3-byte
// surrogate sequences — is coalesced into the single astral scalar value it
// denotes. This keeps a string's byte representation canonical, so that byte
// equality matches code-unit equality (as ===, SameValue, and string property
// keys require). A string with no coalescible pair is returned unchanged.
//
// Surrogates encode as 0xED 0xA0-0xBF xx; a coalescible pair is a high surrogate
// (0xED 0xA0-0xAF) immediately followed by a low surrogate (0xED 0xB0-0xBF).
func canonicalizeWTF8(s string) string {
	for i := 0; i+5 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xAF &&
			s[i+3] == 0xED && s[i+4] >= 0xB0 && s[i+4] <= 0xBF {
			// Rebuilding through the code-unit view coalesces every such pair.
			return unitsToString(codeUnits(s))
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// utf16View: the canonical code-unit view of a String.
// ---------------------------------------------------------------------------

// utf16View presents a gojs string as its UTF-16 code-unit sequence. For a
// pure-ASCII string it works directly on the backing bytes and allocates
// nothing; for any other string it materializes the code-unit slice once.
type utf16View struct {
	s     string   // backing bytes
	units []uint16 // nil in the ASCII fast path (byte == code unit)
	ascii bool
}

// viewOf builds the code-unit view of s.
func viewOf(s string) utf16View {
	if isASCIIStr(s) {
		return utf16View{s: s, ascii: true}
	}
	return utf16View{s: s, units: codeUnits(s)}
}

// Len is the number of UTF-16 code units.
func (v utf16View) Len() int {
	if v.ascii {
		return len(v.s)
	}
	return len(v.units)
}

// At returns the code unit at index i (which must be in range).
func (v utf16View) At(i int) uint16 {
	if v.ascii {
		return uint16(v.s[i])
	}
	return v.units[i]
}

// Slice returns the substring spanning code units [i, j) as a gojs string. A
// surrogate pair split at a boundary yields a lone surrogate, per spec.
func (v utf16View) Slice(i, j int) string {
	if v.ascii {
		return v.s[i:j]
	}
	return unitsToString(v.units[i:j])
}

// unitsSlice returns the code-unit slice, building it on demand for the ASCII
// fast path. Used by the non-ASCII search fallback.
func (v utf16View) unitsSlice() []uint16 {
	if v.units != nil {
		return v.units
	}
	u := make([]uint16, len(v.s))
	for i := 0; i < len(v.s); i++ {
		u[i] = uint16(v.s[i])
	}
	return u
}

// HasAt reports whether sub occurs starting exactly at code-unit index at.
func (v utf16View) HasAt(sub utf16View, at int) bool {
	if at < 0 || at+sub.Len() > v.Len() {
		return false
	}
	if v.ascii && sub.ascii {
		return v.s[at:at+len(sub.s)] == sub.s
	}
	return unitHasAt(v.unitsSlice(), sub.unitsSlice(), at)
}

// IndexOf returns the first code-unit index >= from at which sub occurs, or -1.
// An empty sub matches at from (clamped to Len). ASCII operands use Go's
// optimized byte search and allocate nothing.
func (v utf16View) IndexOf(sub utf16View, from int) int {
	if from < 0 {
		from = 0
	}
	if v.ascii && sub.ascii {
		if len(sub.s) == 0 {
			if from > len(v.s) {
				return len(v.s)
			}
			return from
		}
		if from > len(v.s) {
			return -1
		}
		idx := strings.Index(v.s[from:], sub.s)
		if idx < 0 {
			return -1
		}
		return from + idx
	}
	return unitIndex(v.unitsSlice(), sub.unitsSlice(), from)
}

// LastIndexOf returns the greatest code-unit index k <= start at which sub
// occurs, or -1. An empty sub matches at start (clamped to Len).
func (v utf16View) LastIndexOf(sub utf16View, start int) int {
	if v.ascii && sub.ascii {
		if len(sub.s) == 0 {
			if start > len(v.s) {
				return len(v.s)
			}
			return start
		}
		upper := start + len(sub.s)
		if upper > len(v.s) {
			return strings.LastIndex(v.s, sub.s) // occurrence, if any, starts <= start
		}
		return strings.LastIndex(v.s[:upper], sub.s)
	}
	rs, rsub := v.unitsSlice(), sub.unitsSlice()
	last := -1
	for k := 0; k+len(rsub) <= len(rs) && k <= start; k++ {
		if unitHasAt(rs, rsub, k) {
			last = k
		}
	}
	return last
}
