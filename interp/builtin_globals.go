package interp

import (
	"context"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// initGlobals installs the global value bindings and free functions
// (globalThis, NaN, Infinity, parseInt/parseFloat, isNaN/isFinite, eval, and
// the number/string encoding helpers).
func (i *Interpreter) initGlobals() {
	i.global.defineOwn(StrKey("globalThis"), &Property{Value: i.global, Writable: true, Enumerable: false, Configurable: true})
	i.global.defineOwn(StrKey("undefined"), &Property{Value: Undef, Writable: false, Enumerable: false, Configurable: false})
	i.global.defineOwn(StrKey("NaN"), &Property{Value: Number(math.NaN()), Writable: false, Enumerable: false, Configurable: false})
	i.global.defineOwn(StrKey("Infinity"), &Property{Value: Number(math.Inf(1)), Writable: false, Enumerable: false, Configurable: false})

	// parseInt and parseFloat are shared: Number.parseInt === parseInt and
	// Number.parseFloat === parseFloat are the very same function objects.
	parseIntFn := i.newNativeFunc("parseInt", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		radix := 0
		if !IsUndefined(arg(args, 1)) {
			radix, _ = i.argInt(ctx, args, 1)
		}
		return Number(parseIntImpl(s, radix)), nil
	})
	parseFloatFn := i.newNativeFunc("parseFloat", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(parseFloatImpl(s)), nil
	})
	i.setGlobalHidden("parseInt", parseIntFn)
	i.setGlobalHidden("parseFloat", parseFloatFn)
	if numCtor, ok := i.GetGlobal("Number").(*Object); ok {
		numCtor.SetHidden("parseInt", parseIntFn)
		numCtor.SetHidden("parseFloat", parseFloatFn)
	}
	i.setGlobalFunc("isNaN", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(math.IsNaN(f)), nil
	})
	i.setGlobalFunc("isFinite", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(!math.IsNaN(f) && !math.IsInf(f, 0)), nil
	})

	// eval. An indirect call (the callee is not the identifier `eval`, or it
	// does not resolve to this intrinsic) runs in the global scope; a direct
	// call is intercepted in evalCall and runs in the caller's context instead.
	i.evalFn = i.newNativeFunc("eval", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.evalSource(ctx, arg(args, 0))
	})
	i.setGlobalHidden("eval", i.evalFn)

	// URI helpers (thin wrappers over Go's URL escaping semantics).
	i.setGlobalFunc("encodeURIComponent", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		out, ok := encodeURI(s, uriComponentUnreserved)
		if !ok {
			return nil, i.throwError(ctx, "URIError", "URI malformed")
		}
		return String(out), nil
	})
	i.setGlobalFunc("encodeURI", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		out, ok := encodeURI(s, uriUnreserved)
		if !ok {
			return nil, i.throwError(ctx, "URIError", "URI malformed")
		}
		return String(out), nil
	})
	i.setGlobalFunc("decodeURIComponent", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		out, ok := decodeURI(s, "")
		if !ok {
			return nil, i.throwError(ctx, "URIError", "URI malformed")
		}
		return String(out), nil
	})
	i.setGlobalFunc("decodeURI", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		// decodeURI preserves the reserved characters (and "#") as escapes so a
		// decoded URI still parses into the same components.
		out, ok := decodeURI(s, uriReserved)
		if !ok {
			return nil, i.throwError(ctx, "URIError", "URI malformed")
		}
		return String(out), nil
	})
}

// setGlobalFunc defines a non-enumerable global function.
func (i *Interpreter) setGlobalFunc(name string, length int, fn CallFn) {
	i.setGlobalHidden(name, i.newNativeFunc(name, length, fn))
}

// parseIntImpl implements the parseInt algorithm (§19.2.5).
func parseIntImpl(s string, radix int) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return math.NaN()
	}
	sign := 1.0
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	if radix == 0 {
		if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
			radix = 16
			s = s[2:]
		} else {
			radix = 10
		}
	} else if radix == 16 {
		if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
			s = s[2:]
		}
	}
	if radix < 2 || radix > 36 {
		return math.NaN()
	}
	var result float64
	consumed := 0
	for _, c := range s {
		d := digitValue(c)
		if d < 0 || d >= radix {
			break
		}
		result = result*float64(radix) + float64(d)
		consumed++
	}
	if consumed == 0 {
		return math.NaN()
	}
	return sign * result
}

// parseFloatImpl implements the parseFloat algorithm, reading the longest valid
// numeric prefix.
func parseFloatImpl(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "Infinity") || strings.HasPrefix(s, "+Infinity") {
		return math.Inf(1)
	}
	if strings.HasPrefix(s, "-Infinity") {
		return math.Inf(-1)
	}
	// Find the longest parseable prefix.
	end := 0
	for end < len(s) {
		if _, err := strconv.ParseFloat(s[:end+1], 64); err != nil {
			// Allow trailing exponent/decimal build-up.
			if end+1 < len(s) && (s[end] == 'e' || s[end] == 'E' || s[end] == '.' || s[end] == '+' || s[end] == '-') {
				end++
				continue
			}
			break
		}
		end++
	}
	if end == 0 {
		return math.NaN()
	}
	// Trim any invalid trailing build-up back to a parseable value.
	for end > 0 {
		if f, err := strconv.ParseFloat(s[:end], 64); err == nil {
			return f
		}
		end--
	}
	return math.NaN()
}

// digitValue returns the value of a base-36 digit, or -1.
func digitValue(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

// URI escaping helpers.
const (
	uriUnreserved          = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'();/?:@&=+$,#"
	uriComponentUnreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
)

// stringToCodeUnits interprets a gojs string as a sequence of UTF-16 code
// units, as the Encode/Decode abstract operations require. gojs stores strings
// as (WTF-8) bytes and normally indexes by code point, but Encode must iterate
// code units and detect unpaired surrogates. An astral scalar value is split
// into its surrogate pair; a WTF-8-encoded surrogate (see String.fromCharCode)
// is recovered as a single surrogate code unit.
func stringToCodeUnits(s string) []rune {
	units := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		b0 := s[i]
		// A WTF-8-encoded surrogate is 0xED 0xA0-0xBF 0x80-0xBF, denoting a
		// code unit in U+D800..U+DFFF. Recognize it before utf8.DecodeRune,
		// which would otherwise report three separate RuneErrors.
		if b0 == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
			units = append(units, rune(b0&0x0F)<<12|rune(s[i+1]&0x3F)<<6|rune(s[i+2]&0x3F))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			r -= 0x10000
			units = append(units, 0xD800+(r>>10), 0xDC00+(r&0x3FF))
		} else {
			units = append(units, r)
		}
		i += size
	}
	return units
}

// encodeURI implements the ECMA-262 Encode abstract operation (§19.2.6.5). It
// iterates the string as UTF-16 code units: an unescaped ASCII code unit is
// copied verbatim, an unpaired surrogate raises a URIError (ok=false), and any
// other code point is emitted as its UTF-8 octets in "%XX" form. unreserved is
// the concatenation of the always-unescaped set and the operation's extra set.
func encodeURI(s, unreserved string) (string, bool) {
	const hex = "0123456789ABCDEF"
	units := stringToCodeUnits(s)
	var b strings.Builder
	for k := 0; k < len(units); {
		cu := units[k]
		if cu < 0x80 && strings.IndexByte(unreserved, byte(cu)) >= 0 {
			b.WriteByte(byte(cu))
			k++
			continue
		}
		// CodePointAt: pair a high surrogate with a following low surrogate;
		// any other surrogate is unpaired and must be rejected.
		var cp rune
		switch {
		case cu >= 0xD800 && cu <= 0xDBFF:
			if k+1 < len(units) && units[k+1] >= 0xDC00 && units[k+1] <= 0xDFFF {
				cp = 0x10000 + (cu-0xD800)<<10 + (units[k+1] - 0xDC00)
				k += 2
			} else {
				return "", false // unpaired high surrogate
			}
		case cu >= 0xDC00 && cu <= 0xDFFF:
			return "", false // unpaired low surrogate
		default:
			cp = cu
			k++
		}
		var buf [utf8.UTFMax]byte
		n := utf8.EncodeRune(buf[:], cp)
		for _, by := range buf[:n] {
			b.WriteByte('%')
			b.WriteByte(hex[by>>4])
			b.WriteByte(hex[by&0xF])
		}
	}
	return b.String(), true
}

// uriReserved is the set of characters decodeURI leaves escaped: the URI
// reserved set plus "#", so that a decoded URI still splits into the same
// components. decodeURIComponent preserves nothing (passes "").
const uriReserved = ";/?:@&=+$,#"

// hexDigit returns the value 0–15 of a hexadecimal digit rune, or -1.
func hexDigit(c rune) int {
	if d := digitValue(c); d >= 0 && d < 16 {
		return d
	}
	return -1
}

// decodeURI implements the ECMA-262 Decode abstract operation (§19.2.6.5): it
// unescapes "%XX" sequences, reassembling multi-byte UTF-8 octets into a code
// point, while leaving any single-byte character in preserve escaped verbatim.
// It returns ok=false on malformed input, letting the caller raise a URIError.
func decodeURI(s, preserve string) (string, bool) {
	// Decode only ever interprets the ASCII code unit "%" and the hex digits
	// that follow it; every other code unit is passed through verbatim. Working
	// on bytes (rather than []rune) therefore both simplifies the index math and
	// preserves any non-UTF-8 bytes — notably the WTF-8 encoding of surrogate
	// code units produced by String.fromCharCode — exactly as received.
	n := len(s)
	var b strings.Builder

	// readByte parses the two hex digits of a "%XX" whose '%' is at index p.
	readByte := func(p int) (byte, bool) {
		if p+2 >= n {
			return 0, false
		}
		hi, lo := hexDigit(rune(s[p+1])), hexDigit(rune(s[p+2]))
		if hi < 0 || lo < 0 {
			return 0, false
		}
		return byte(hi<<4 | lo), true
	}

	k := 0
	for k < n {
		c := s[k]
		if c != '%' {
			b.WriteByte(c)
			k++
			continue
		}
		start := k
		first, ok := readByte(k)
		if !ok {
			return "", false
		}
		k += 2 // k now indexes the second hex digit
		if first < 0x80 {
			if strings.IndexByte(preserve, first) >= 0 {
				b.WriteString(s[start : k+1]) // keep "%XX" verbatim
			} else {
				b.WriteByte(first)
			}
			k++
			continue
		}
		// Multi-byte lead: the count of leading 1-bits is the sequence length.
		seqLen := 0
		for mask := byte(0x80); first&mask != 0; mask >>= 1 {
			seqLen++
		}
		if seqLen < 2 || seqLen > 4 {
			return "", false
		}
		octets := make([]byte, 1, 4)
		octets[0] = first
		for j := 1; j < seqLen; j++ {
			k++ // advance to the expected '%'
			if k >= n || s[k] != '%' {
				return "", false
			}
			cont, ok := readByte(k)
			if !ok || cont&0xC0 != 0x80 { // continuation byte is 10xxxxxx
				return "", false
			}
			k += 2
			octets = append(octets, cont)
		}
		// utf8.DecodeRune rejects overlong forms, surrogate halves, and
		// out-of-range code points, reporting size 1 on any of them. A size
		// short of the octet count means the sequence was malformed — but a
		// full-width decode to U+FFFD (which equals utf8.RuneError) is a valid
		// character, so the size, not the rune, is what distinguishes them.
		r, size := utf8.DecodeRune(octets)
		if size != len(octets) {
			return "", false
		}
		b.WriteRune(r)
		k++
	}
	return b.String(), true
}
