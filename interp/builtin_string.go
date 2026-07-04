package interp

import (
	"context"
	"math"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// argInteger returns ToIntegerOrInfinity(args[n]) as a float64 (which may be
// ±Inf), or def when the argument is absent or undefined. Unlike argIntOr it
// propagates coercion errors (e.g. a throwing valueOf, or a Symbol/BigInt
// operand) rather than swallowing them, as the spec requires for the position
// arguments of String.prototype methods.
func (i *Interpreter) argInteger(ctx context.Context, args []Value, n int, def float64) (float64, error) {
	v := arg(args, n)
	if IsUndefined(v) {
		return def, nil
	}
	f, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	return ToInteger(f), nil
}

// clampIndexF clamps a (possibly infinite) integer position into [0, n].
func clampIndexF(x float64, n int) int {
	if x <= 0 {
		return 0
	}
	if x >= float64(n) {
		return n
	}
	return int(x)
}

// relIndexF resolves a (possibly infinite) relative index (negative counts from
// the end) into an absolute index clamped to [0, n]; used by slice.
func relIndexF(x float64, n int) int {
	if x < 0 {
		x += float64(n)
		if x < 0 {
			return 0
		}
	}
	if x >= float64(n) {
		return n
	}
	return int(x)
}

// isECMAWhiteSpace reports whether r is in the union of the ECMAScript
// WhiteSpace and LineTerminator code point sets (used by String.prototype
// trim/trimStart/trimEnd). See ECMA-262 §12.2 (WhiteSpace) and §12.3
// (LineTerminator).
func isECMAWhiteSpace(r rune) bool {
	switch r {
	case 0x0009, 0x000A, 0x000B, 0x000C, 0x000D, // TAB, LF, VT, FF, CR
		0x0020, // SPACE
		0x00A0, // NO-BREAK SPACE
		0x1680, // OGHAM SPACE MARK
		0x2028, // LINE SEPARATOR
		0x2029, // PARAGRAPH SEPARATOR
		0x202F, // NARROW NO-BREAK SPACE
		0x205F, // MEDIUM MATHEMATICAL SPACE
		0x3000, // IDEOGRAPHIC SPACE
		0xFEFF: // ZERO WIDTH NO-BREAK SPACE (BOM)
		return true
	}
	// U+2000..U+200A (various fixed-width spaces).
	return r >= 0x2000 && r <= 0x200A
}

// initString installs the String constructor and String.prototype methods.
// Strings are stored as Go UTF-8; methods that the spec defines over UTF-16
// code units operate over runes here, which is a faithful approximation for the
// Basic Multilingual Plane.
func (i *Interpreter) initString() {
	proto := i.stringProto
	proto.class = "String"
	proto.primitive = String("")

	strOf := func(ctx context.Context, this Value) (string, error) {
		switch x := this.(type) {
		case String:
			return string(x), nil
		case *strRope:
			return x.build(), nil
		case *Object:
			if s, ok := x.primitive.(String); ok {
				return string(s), nil
			}
		}
		if IsNullish(this) {
			return "", i.throwError(ctx, "TypeError", "String.prototype method called on null or undefined")
		}
		return i.ToStringV(ctx, this)
	}

	m := func(name string, n int, fn func(ctx context.Context, s string, args []Value) (Value, error)) {
		i.defineMethod(proto, name, n, func(ctx context.Context, this Value, args []Value) (Value, error) {
			s, err := strOf(ctx, this)
			if err != nil {
				return nil, err
			}
			return fn(ctx, s, args)
		})
	}

	// toString and valueOf are non-generic: thisStringValue accepts only a
	// String primitive or a String wrapper object, otherwise a TypeError.
	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.thisStringValue(ctx, this)
		if err != nil {
			return nil, err
		}
		return String(s), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.thisStringValue(ctx, this)
		if err != nil {
			return nil, err
		}
		return String(s), nil
	})
	m("charAt", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		v := viewOf(s)
		idx, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		if idx < 0 || idx >= float64(v.Len()) {
			return String(""), nil
		}
		return String(v.Slice(int(idx), int(idx)+1)), nil
	})
	m("charCodeAt", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		v := viewOf(s)
		idx, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		if idx < 0 || idx >= float64(v.Len()) {
			return Number(nan()), nil
		}
		return Number(float64(v.At(int(idx)))), nil
	})
	m("codePointAt", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		// §22.1.3.4: position is a code-unit index; the result is the code point
		// beginning there (a high surrogate followed by a low surrogate combines).
		v := viewOf(s)
		idx, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		if idx < 0 || idx >= float64(v.Len()) {
			return Undef, nil
		}
		k := int(idx)
		cu := v.At(k)
		if cu >= 0xD800 && cu <= 0xDBFF && k+1 < v.Len() && v.At(k+1) >= 0xDC00 && v.At(k+1) <= 0xDFFF {
			cp := 0x10000 + (rune(cu)-0xD800)<<10 + (rune(v.At(k+1)) - 0xDC00)
			return Number(float64(cp)), nil
		}
		return Number(float64(cu)), nil
	})
	m("at", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		v := viewOf(s)
		n := v.Len()
		idx, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		if idx < 0 {
			idx += float64(n)
		}
		if idx < 0 || idx >= float64(n) {
			return Undef, nil
		}
		return String(v.Slice(int(idx), int(idx)+1)), nil
	})
	// Search methods operate over runes (gojs indexes strings by code point) and
	// honor their position/endPosition arguments.
	m("indexOf", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		pos, err := i.argInteger(ctx, args, 1, 0)
		if err != nil {
			return nil, err
		}
		sv, subv := viewOf(s), viewOf(sub)
		from := clampIndexF(pos, sv.Len())
		return Number(float64(sv.IndexOf(subv, from))), nil
	})
	m("lastIndexOf", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		// A NaN position means "search the whole string" (+Infinity), per
		// §22.1.3.9 step 5; other values use ToIntegerOrInfinity.
		pos := math.Inf(1)
		if v := arg(args, 1); !IsUndefined(v) {
			num, err := i.ToNumberV(ctx, v)
			if err != nil {
				return nil, err
			}
			if !math.IsNaN(num) {
				pos = ToInteger(num)
			}
		}
		sv, subv := viewOf(s), viewOf(sub)
		start := clampIndexF(pos, sv.Len())
		return Number(float64(sv.LastIndexOf(subv, start))), nil
	})
	m("includes", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		if err := i.rejectRegExpArg(ctx, args, "includes"); err != nil {
			return nil, err
		}
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		pos, err := i.argInteger(ctx, args, 1, 0)
		if err != nil {
			return nil, err
		}
		sv, subv := viewOf(s), viewOf(sub)
		from := clampIndexF(pos, sv.Len())
		return Bool(sv.IndexOf(subv, from) >= 0), nil
	})
	m("startsWith", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		if err := i.rejectRegExpArg(ctx, args, "startsWith"); err != nil {
			return nil, err
		}
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		pos, err := i.argInteger(ctx, args, 1, 0)
		if err != nil {
			return nil, err
		}
		sv, subv := viewOf(s), viewOf(sub)
		return Bool(sv.HasAt(subv, clampIndexF(pos, sv.Len()))), nil
	})
	m("endsWith", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		if err := i.rejectRegExpArg(ctx, args, "endsWith"); err != nil {
			return nil, err
		}
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		sv, subv := viewOf(s), viewOf(sub)
		end := sv.Len()
		if !IsUndefined(arg(args, 1)) {
			pos, err := i.argInteger(ctx, args, 1, float64(sv.Len()))
			if err != nil {
				return nil, err
			}
			end = clampIndexF(pos, sv.Len())
		}
		start := end - subv.Len()
		return Bool(start >= 0 && sv.HasAt(subv, start)), nil
	})
	m("slice", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		v := viewOf(s)
		n := v.Len()
		startF, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		start := relIndexF(startF, n)
		end := n
		if !IsUndefined(arg(args, 1)) {
			endF, err := i.argInteger(ctx, args, 1, float64(n))
			if err != nil {
				return nil, err
			}
			end = relIndexF(endF, n)
		}
		if start > end {
			return String(""), nil
		}
		return String(v.Slice(start, end)), nil
	})
	m("substring", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		v := viewOf(s)
		n := v.Len()
		aF, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		a := clampIndexF(aF, n)
		b := n
		if !IsUndefined(arg(args, 1)) {
			bF, err := i.argInteger(ctx, args, 1, float64(n))
			if err != nil {
				return nil, err
			}
			b = clampIndexF(bF, n)
		}
		if a > b {
			a, b = b, a
		}
		return String(v.Slice(a, b)), nil
	})
	m("substr", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		v := viewOf(s)
		n := v.Len()
		startF, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		start := clampIndexF(startF, n)
		if startF < 0 {
			start = clampIndexF(float64(n)+startF, n)
		}
		lengthF := float64(n - start)
		if !IsUndefined(arg(args, 1)) {
			lengthF, err = i.argInteger(ctx, args, 1, 0)
			if err != nil {
				return nil, err
			}
		}
		if start >= n || lengthF <= 0 {
			return String(""), nil
		}
		end := start + clampIndexF(lengthF, n-start)
		return String(v.Slice(start, end)), nil
	})
	m("toUpperCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(toUpperCaseFull(s)), nil
	})
	m("toLowerCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(toLowerCaseFull(s)), nil
	})
	m("toLocaleUpperCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(toUpperCaseFull(s)), nil
	})
	m("toLocaleLowerCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(toLowerCaseFull(s)), nil
	})
	m("trim", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.TrimFunc(s, isECMAWhiteSpace)), nil
	})
	m("trimStart", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.TrimLeftFunc(s, isECMAWhiteSpace)), nil
	})
	m("trimEnd", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.TrimRightFunc(s, isECMAWhiteSpace)), nil
	})
	m("localeCompare", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		var that string
		if len(args) > 0 {
			var err error
			that, err = i.ToStringV(ctx, args[0])
			if err != nil {
				return nil, err
			}
		} else {
			that = "undefined"
		}
		// Minimal non-Intl implementation: lexicographic comparison over
		// UTF-16 code units. Comparing the Go UTF-8 strings yields the same
		// ordering for the Basic Multilingual Plane. Per §22.1.3.10, canonically
		// equivalent Strings must compare as identical, so normalize both sides
		// (NFC) before comparing.
		s = norm.NFC.String(s)
		that = norm.NFC.String(that)
		if s < that {
			return Number(-1), nil
		}
		if s > that {
			return Number(1), nil
		}
		return Number(0), nil
	})
	m("normalize", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		// §22.1.3.13: default form is "NFC"; any other value is coerced with
		// ToString and must name one of the four Unicode normalization forms,
		// otherwise a RangeError.
		form := "NFC"
		if v := arg(args, 0); !IsUndefined(v) {
			var err error
			form, err = i.ToStringV(ctx, v)
			if err != nil {
				return nil, err
			}
		}
		var f norm.Form
		switch form {
		case "NFC":
			f = norm.NFC
		case "NFD":
			f = norm.NFD
		case "NFKC":
			f = norm.NFKC
		case "NFKD":
			f = norm.NFKD
		default:
			return nil, i.throwError(ctx, "RangeError", "The normalization form should be one of NFC, NFD, NFKC, NFKD.")
		}
		return String(f.String(s)), nil
	})
	// isWellFormed (§22.1.3.9) / toWellFormed (§22.1.3.35): report or repair
	// unpaired UTF-16 surrogates, viewing the string as its code-unit sequence.
	m("isWellFormed", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		units := codeUnits(s)
		for k := 0; k < len(units); k++ {
			cu := units[k]
			if cu >= 0xD800 && cu <= 0xDBFF {
				if k+1 < len(units) && units[k+1] >= 0xDC00 && units[k+1] <= 0xDFFF {
					k++ // valid pair
					continue
				}
				return Boolean(false), nil // unpaired high surrogate
			}
			if cu >= 0xDC00 && cu <= 0xDFFF {
				return Boolean(false), nil // unpaired low surrogate
			}
		}
		return Boolean(true), nil
	})
	m("toWellFormed", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		units := codeUnits(s)
		out := make([]uint16, 0, len(units))
		for k := 0; k < len(units); k++ {
			cu := units[k]
			switch {
			case cu >= 0xD800 && cu <= 0xDBFF:
				if k+1 < len(units) && units[k+1] >= 0xDC00 && units[k+1] <= 0xDFFF {
					out = append(out, cu, units[k+1])
					k++
				} else {
					out = append(out, 0xFFFD)
				}
			case cu >= 0xDC00 && cu <= 0xDFFF:
				out = append(out, 0xFFFD)
			default:
				out = append(out, cu)
			}
		}
		return String(unitsToString(out)), nil
	})
	m("padStart", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		return i.stringPad(ctx, s, args, true)
	})
	m("padEnd", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		return i.stringPad(ctx, s, args, false)
	})
	m("repeat", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		count, err := i.argInteger(ctx, args, 0, 0)
		if err != nil {
			return nil, err
		}
		if count < 0 || math.IsInf(count, 1) {
			return nil, i.throwError(ctx, "RangeError", "Invalid count value")
		}
		// Repetition can place s's trailing surrogate before the next copy's
		// leading surrogate; coalesce any pair so the result stays canonical.
		return String(canonicalizeWTF8(strings.Repeat(s, int(count)))), nil
	})
	m("concat", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		var b strings.Builder
		b.WriteString(s)
		for _, a := range args {
			as, err := i.ToStringV(ctx, a)
			if err != nil {
				return nil, err
			}
			b.WriteString(as)
		}
		return String(canonicalizeWTF8(b.String())), nil
	})
	m("split", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		return i.stringSplitString(ctx, s, args)
	})
	m("replace", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		return i.stringReplace(ctx, s, args, false)
	})
	m("replaceAll", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		return i.stringReplace(ctx, s, args, true)
	})

	// Indexed access and length on the prototype fall back to per-instance
	// wrappers; primitive strings are handled directly in member access.

	ctor := i.newNativeCtor("String", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if len(args) == 0 {
			return String(""), nil
		}
		if sym, ok := args[0].(*Symbol); ok {
			return String("Symbol(" + sym.Desc + ")"), nil
		}
		s, err := i.ToStringV(ctx, args[0])
		if err != nil {
			return nil, err
		}
		return String(s), nil
	}, func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		s := ""
		if len(args) > 0 {
			var err error
			s, err = i.ToStringV(ctx, args[0])
			if err != nil {
				return nil, err
			}
		}
		o := i.newStringObject(String(s))
		// GetPrototypeFromConstructor (§22.1.1.1): a subclass instance takes its
		// prototype from new.target rather than %String.prototype%.
		p, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.stringProto })
		if err != nil {
			return nil, err
		}
		if p != i.stringProto {
			o.SetProto(p)
		}
		return o, nil
	})
	linkCtor(ctor, proto)

	i.defineMethod(ctor, "fromCharCode", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// §22.1.2.1: each argument becomes one UTF-16 code unit. Building the
		// code-unit slice and re-encoding it (unitsToString) yields well-formed
		// WTF-8, so two adjacent surrogate arguments that form a pair coalesce
		// into the single astral scalar value they denote.
		units := make([]uint16, len(args))
		for k, a := range args {
			n, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			units[k] = uint16(int64(n))
		}
		return String(unitsToString(units)), nil
	})
	i.defineMethod(ctor, "fromCodePoint", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		var units []uint16
		for _, a := range args {
			n, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			// Each argument must be a non-negative integer code point <= 0x10FFFF.
			if n != ToInteger(n) || n < 0 || n > 0x10FFFF {
				return nil, i.throwError(ctx, "RangeError", "Invalid code point "+NumberToString(n))
			}
			cp := rune(int64(n))
			if cp >= 0x10000 {
				cp -= 0x10000
				units = append(units, uint16(0xD800+(cp>>10)), uint16(0xDC00+(cp&0x3FF)))
			} else {
				units = append(units, uint16(cp))
			}
		}
		return String(unitsToString(units)), nil
	})
	// String.raw (§22.1.3.28): the tag function for template literals. It
	// concatenates the raw literal segments interleaved with the string forms
	// of the supplied substitutions.
	i.defineMethod(ctor, "raw", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		var template Value = Undef
		if len(args) > 0 {
			template = args[0]
		}
		cooked, err := i.ToObject(ctx, template)
		if err != nil {
			return nil, err
		}
		rawv, err := i.getV(ctx, cooked, StrKey("raw"), cooked)
		if err != nil {
			return nil, err
		}
		raw, err := i.ToObject(ctx, rawv)
		if err != nil {
			return nil, err
		}
		lengthv, err := i.getV(ctx, raw, StrKey("length"), raw)
		if err != nil {
			return nil, err
		}
		literalSegments, err := i.toLength(ctx, lengthv)
		if err != nil {
			return nil, err
		}
		if literalSegments <= 0 {
			return String(""), nil
		}
		var b strings.Builder
		for idx := 0; idx < literalSegments; idx++ {
			seg, err := i.getV(ctx, raw, StrKey(intToStr(idx)), raw)
			if err != nil {
				return nil, err
			}
			segStr, err := i.ToStringV(ctx, seg)
			if err != nil {
				return nil, err
			}
			b.WriteString(segStr)
			if idx+1 == literalSegments {
				break
			}
			// Substitutions are args[1:]; substitutions[idx] == args[idx+1].
			// Absent substitutions contribute the empty String (§22.1.3.28
			// steps 12.f/12.g), not "undefined".
			if idx+1 < len(args) {
				nextStr, err := i.ToStringV(ctx, args[idx+1])
				if err != nil {
					return nil, err
				}
				b.WriteString(nextStr)
			}
		}
		return String(b.String()), nil
	})

	i.setGlobalHidden("String", ctor)
}

// stringReplace implements String.prototype.replace/replaceAll for string
// patterns and string or function replacements. (Regex patterns are handled
// by the RegExp integration when present.)
func (i *Interpreter) stringReplace(ctx context.Context, s string, args []Value, all bool) (Value, error) {
	pattern, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	repl := arg(args, 1)
	replFn, isFn := repl.(*Object)
	isFn = isFn && replFn.IsCallable()
	// A non-callable replacement is coerced to a String exactly once (§22.1.3.19
	// step 5 / GetSubstitution), not per match.
	var rs string
	if !isFn {
		var err error
		rs, err = i.ToStringV(ctx, repl)
		if err != nil {
			return nil, err
		}
	}
	doReplace := func(match string, idx int) (string, error) {
		if isFn {
			r, err := replFn.fn.call(ctx, Undef, []Value{String(match), Number(float64(idx)), String(s)})
			if err != nil {
				return "", err
			}
			return i.ToStringV(ctx, r)
		}
		// Expand the $ patterns for a string pattern: $$ -> $, $& -> match,
		// $` -> portion before the match, $' -> portion after the match.
		var b strings.Builder
		for k := 0; k < len(rs); k++ {
			if rs[k] == '$' && k+1 < len(rs) {
				switch rs[k+1] {
				case '$':
					b.WriteByte('$')
					k++
					continue
				case '&':
					b.WriteString(match)
					k++
					continue
				case '`':
					b.WriteString(s[:idx])
					k++
					continue
				case '\'':
					b.WriteString(s[idx+len(match):])
					k++
					continue
				}
			}
			b.WriteByte(rs[k])
		}
		return b.String(), nil
	}
	if all {
		// Empty pattern: insert the replacement between every character and at
		// both ends ("abc".replaceAll("", "X") -> "XaXbXcX").
		if pattern == "" {
			var b strings.Builder
			r0, err := doReplace("", 0)
			if err != nil {
				return nil, err
			}
			b.WriteString(r0)
			units := codeUnits(s)
			for k := range units {
				b.WriteString(unitsToString(units[k : k+1]))
				rN, err := doReplace("", 0)
				if err != nil {
					return nil, err
				}
				b.WriteString(rN)
			}
			return String(canonicalizeWTF8(b.String())), nil
		}
		var b strings.Builder
		rest := s
		offset := 0
		for {
			idx := strings.Index(rest, pattern)
			if idx < 0 {
				b.WriteString(rest)
				break
			}
			b.WriteString(rest[:idx])
			r, err := doReplace(pattern, offset+idx)
			if err != nil {
				return nil, err
			}
			b.WriteString(r)
			rest = rest[idx+len(pattern):]
			offset += idx + len(pattern)
		}
		return String(canonicalizeWTF8(b.String())), nil
	}
	idx := strings.Index(s, pattern)
	if idx < 0 {
		return String(s), nil
	}
	r, err := doReplace(pattern, idx)
	if err != nil {
		return nil, err
	}
	return String(canonicalizeWTF8(s[:idx] + r + s[idx+len(pattern):])), nil
}

// unitHasAt reports whether sub appears in rs starting exactly at index at.
func unitHasAt(rs, sub []uint16, at int) bool {
	if at < 0 || at+len(sub) > len(rs) {
		return false
	}
	for k := range sub {
		if rs[at+k] != sub[k] {
			return false
		}
	}
	return true
}

// unitIndex returns the first index >= from at which sub occurs in rs, or -1.
// An empty sub matches at from (clamped to len(rs)).
func unitIndex(rs, sub []uint16, from int) int {
	if from < 0 {
		from = 0
	}
	if len(sub) == 0 {
		if from > len(rs) {
			return len(rs)
		}
		return from
	}
	for k := from; k+len(sub) <= len(rs); k++ {
		if unitHasAt(rs, sub, k) {
			return k
		}
	}
	return -1
}

// thisStringValue implements the abstraction of the same name (§22.1.3.3): it
// accepts only a String primitive or a String wrapper object, otherwise it
// throws a TypeError. Used by the non-generic toString/valueOf methods.
func (i *Interpreter) thisStringValue(ctx context.Context, this Value) (string, error) {
	switch x := this.(type) {
	case String:
		return string(x), nil
	case *strRope:
		return x.build(), nil
	case *Object:
		if s, ok := x.primitive.(String); ok {
			return string(s), nil
		}
	}
	return "", i.throwError(ctx, "TypeError", "String.prototype method requires that 'this' be a String")
}

// rejectRegExpArg throws a TypeError when the search argument of includes /
// startsWith / endsWith is a RegExp (per IsRegExp, §22.1.3.7/20/22).
func (i *Interpreter) rejectRegExpArg(ctx context.Context, args []Value, method string) error {
	isRe, err := i.isRegExpValue(ctx, arg(args, 0))
	if err != nil {
		return err
	}
	if isRe {
		return i.throwError(ctx, "TypeError", "First argument to String.prototype."+method+" must not be a regular expression")
	}
	return nil
}

// stringPad implements the StringPad abstraction (§22.1.3.16.1) for padStart /
// padEnd: maxLength is coerced before the fill string, and the fill is repeated
// then truncated to the exact number of code units required (a surrogate pair
// may be split at the truncation boundary, per spec).
func (i *Interpreter) stringPad(ctx context.Context, s string, args []Value, start bool) (Value, error) {
	maxF, err := i.argInteger(ctx, args, 0, 0)
	if err != nil {
		return nil, err
	}
	stringLength := codeUnitLen(s)
	if maxF <= float64(stringLength) {
		return String(s), nil
	}
	filler := " "
	if v := arg(args, 1); !IsUndefined(v) {
		filler, err = i.ToStringV(ctx, v)
		if err != nil {
			return nil, err
		}
	}
	if filler == "" {
		return String(s), nil
	}
	need := int(maxF) - stringLength
	fillUnits := codeUnits(filler)
	padding := make([]uint16, need)
	for j := 0; j < need; j++ {
		padding[j] = fillUnits[j%len(fillUnits)]
	}
	pad := unitsToString(padding)
	// The pad/string boundary may join a high and low surrogate; canonicalize.
	if start {
		return String(canonicalizeWTF8(pad + s)), nil
	}
	return String(canonicalizeWTF8(s + pad)), nil
}
