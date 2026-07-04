package interp

import (
	"context"
	"fmt"
	"strings"
)

// initRegExpStatics installs the RegExp constructor's own members that are not
// the constructor call itself: the RegExp.escape static method (§22.2.5.2) and
// the get RegExp[@@species] accessor (§22.2.5.4).
func (i *Interpreter) initRegExpStatics(ctor *Object) {
	i.defineMethod(ctor, "escape", 1, i.regexpEscape)

	// get RegExp[Symbol.species] returns the this value (the constructor), so
	// subclasses inherit species construction.
	speciesGet := i.newNativeFunc("get [Symbol.species]", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return this, nil
	})
	ctor.defineOwn(SymKey(i.symSpecies), &Property{Get: speciesGet, Accessor: true, Configurable: true})
}

// regexpEscape implements RegExp.escape (§22.2.5.2): return a copy of the string
// in which characters potentially special in a Pattern are escaped.
func (i *Interpreter) regexpEscape(ctx context.Context, this Value, args []Value) (Value, error) {
	s, ok := flattenRope(arg(args, 0)).(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.escape argument must be a string")
	}
	// §22.2.5.2 iterates the code points of S: a surrogate pair combines into
	// its astral scalar value, while a lone surrogate is yielded as its own
	// (surrogate) code point and escaped as \uXXXX below.
	units := codeUnits(string(s))
	var b strings.Builder
	first := true
	for k := 0; k < len(units); first = false {
		cu := units[k]
		var cp rune
		if cu >= 0xD800 && cu <= 0xDBFF && k+1 < len(units) && units[k+1] >= 0xDC00 && units[k+1] <= 0xDFFF {
			cp = 0x10000 + (rune(cu)-0xD800)<<10 + (rune(units[k+1]) - 0xDC00)
			k += 2
		} else {
			cp = rune(cu)
			k++
		}
		if first && isASCIIAlnum(cp) {
			// A leading digit/letter is hex-escaped so it cannot extend a
			// preceding \0/\1/\c sequence in the surrounding pattern.
			fmt.Fprintf(&b, "\\x%02x", cp)
		} else {
			b.WriteString(encodeForRegExpEscape(cp))
		}
	}
	return String(b.String()), nil
}

func isASCIIAlnum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// encodeForRegExpEscape implements EncodeForRegExpEscape (§22.2.5.2.1).
func encodeForRegExpEscape(cp rune) string {
	if isRegExpSyntaxChar(cp) || cp == '/' {
		return "\\" + string(cp)
	}
	switch cp {
	case '\t':
		return "\\t"
	case '\n':
		return "\\n"
	case '\v':
		return "\\v"
	case '\f':
		return "\\f"
	case '\r':
		return "\\r"
	}
	if isRegExpOtherPunctuator(cp) || isJSWhiteSpaceOrLineTerminator(cp) || (cp >= 0xD800 && cp <= 0xDFFF) {
		if cp <= 0xFF {
			return fmt.Sprintf("\\x%02x", cp)
		}
		// A lone surrogate is escaped as its own code unit; utf16.Encode would
		// fold it to U+FFFD, so emit it directly. (All other characters reaching
		// this branch are BMP and encode to a single code unit == cp.)
		return fmt.Sprintf("\\u%04x", cp)
	}
	return string(cp)
}

func isRegExpSyntaxChar(r rune) bool {
	switch r {
	case '^', '$', '\\', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|':
		return true
	}
	return false
}

// isRegExpOtherPunctuator is the set ",-=<>#&!%:;@~'`\"".
func isRegExpOtherPunctuator(r rune) bool {
	switch r {
	case ',', '-', '=', '<', '>', '#', '&', '!', '%', ':', ';', '@', '~', '\'', '`', '"':
		return true
	}
	return false
}

func isJSWhiteSpaceOrLineTerminator(r rune) bool {
	switch r {
	case 0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x00A0, 0x1680,
		0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}
