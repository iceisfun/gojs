package interp

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf16"
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
	s, ok := arg(args, 0).(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.escape argument must be a string")
	}
	var b strings.Builder
	first := true
	for _, cp := range string(s) {
		if first && isASCIIAlnum(cp) {
			// A leading digit/letter is hex-escaped so it cannot extend a
			// preceding \0/\1/\c sequence in the surrounding pattern.
			fmt.Fprintf(&b, "\\x%02x", cp)
		} else {
			b.WriteString(encodeForRegExpEscape(cp))
		}
		first = false
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
		var b strings.Builder
		for _, u := range utf16.Encode([]rune{cp}) {
			fmt.Fprintf(&b, "\\u%04x", u)
		}
		return b.String()
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
