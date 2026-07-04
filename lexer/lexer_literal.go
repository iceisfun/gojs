package lexer

import (
	"strings"

	"github.com/iceisfun/gojs/token"
)

// This file holds the scanners for multi-character literal forms: numbers,
// strings, template literals, and regular expressions. They are kept separate
// from the core dispatch in lexer.go for readability.

// scanNumber scans a numeric literal in any of JavaScript's forms: decimal
// (42, 3.14, .5, 1e10), hexadecimal (0xFF), octal (0o17), binary (0b1010),
// legacy octal (0777), non-octal decimal (08), and BigInt (123n). Numeric
// separators ('_') are permitted strictly between two digits.
//
// Early errors (a misplaced separator, a radix prefix with no digits, a BigInt
// suffix on a non-integer or legacy-octal literal, etc.) are reported here.
// Legacy octal and non-octal-decimal integers are legal only in sloppy mode, so
// scanNumber records that condition in the token's StrictError for the parser to
// raise when the enclosing code is strict.
func (l *Lexer) scanNumber(start token.Pos, nl bool) token.Token {
	begin := l.offset
	isBigInt := false
	bigIntAllowed := true // BigInt is only valid on an integer literal
	strictErr := ""

	if l.ch == '0' && (l.peek() == 'x' || l.peek() == 'X' ||
		l.peek() == 'o' || l.peek() == 'O' || l.peek() == 'b' || l.peek() == 'B') {
		// Prefixed radix literal: 0x.., 0o.., 0b..
		prefix := l.peek()
		l.readRune() // 0
		l.readRune() // x/o/b
		var pred func(rune) bool
		switch prefix {
		case 'x', 'X':
			pred = isHexDigit
		case 'o', 'O':
			pred = func(r rune) bool { return r >= '0' && r <= '7' }
		default:
			pred = func(r rune) bool { return r == '0' || r == '1' }
		}
		n, ok := l.scanDigitsSep(pred)
		if !ok {
			return l.errorf("invalid numeric separator")
		}
		if n == 0 {
			return l.errorf("missing digits after numeric base prefix")
		}
	} else if l.ch == '0' && (isDigit(l.peek()) || l.peek() == '_') {
		// Legacy leading-zero integer: LegacyOctalIntegerLiteral (all digits
		// 0-7) or NonOctalDecimalIntegerLiteral (contains 8 or 9). Neither
		// permits numeric separators or a BigInt suffix, and both are strict
		// early errors. A lone leading zero followed by '_' (0_0) is always
		// invalid and is rejected by the separator check below.
		strictErr = "Octal literals are not allowed in strict mode"
		bigIntAllowed = false
		l.readRune() // 0
		for isDigit(l.ch) {
			l.readRune()
		}
		if l.ch == '_' {
			return l.errorf("numeric separator is not allowed in legacy octal literal")
		}
		// A following '.' or exponent turns this into an ordinary decimal
		// (e.g. 08.5, 09e2); those are not integer legacy literals.
		if l.ch == '.' || l.ch == 'e' || l.ch == 'E' {
			strictErr = ""
			bigIntAllowed = false
			l.scanDecimalTail()
		}
	} else {
		// Ordinary decimal: integer part, optional fraction, optional exponent.
		if _, ok := l.scanDigitsSep(isDigit); !ok {
			return l.errorf("invalid numeric separator")
		}
		if l.ch == '.' || l.ch == 'e' || l.ch == 'E' {
			bigIntAllowed = false
			if !l.scanDecimalTail() {
				return l.cur()
			}
		}
	}

	// Optional BigInt suffix.
	if l.ch == 'n' {
		if !bigIntAllowed {
			return l.errorf("invalid BigInt literal")
		}
		isBigInt = true
		l.readRune()
	}

	// An identifier character immediately following a number is an error
	// (e.g. 3in). This catches most malformed literals.
	if isIdentStart(l.ch) || l.ch == '_' {
		return l.errorf("identifier starts immediately after numeric literal")
	}

	raw := l.input[begin:l.offset]
	if isBigInt {
		digits := strings.ReplaceAll(strings.TrimSuffix(raw, "n"), "_", "")
		return token.Token{Type: token.BIGINT, Literal: digits, Raw: raw, Pos: start, NewlineBefore: nl}
	}
	return token.Token{Type: token.NUMBER, Literal: raw, Raw: raw, Pos: start, NewlineBefore: nl, StrictError: strictErr}
}

// scanDecimalTail scans an optional fractional part and optional exponent of a
// decimal literal, enforcing separator placement. It returns false (after
// recording an error) on a malformed exponent or misplaced separator.
func (l *Lexer) scanDecimalTail() bool {
	if l.ch == '.' {
		l.readRune()
		if _, ok := l.scanDigitsSep(isDigit); !ok {
			l.errorf("invalid numeric separator")
			return false
		}
	}
	if l.ch == 'e' || l.ch == 'E' {
		l.readRune()
		if l.ch == '+' || l.ch == '-' {
			l.readRune()
		}
		if !isDigit(l.ch) {
			l.errorf("missing exponent in number literal")
			return false
		}
		if _, ok := l.scanDigitsSep(isDigit); !ok {
			l.errorf("invalid numeric separator")
			return false
		}
	}
	return true
}

// cur returns the ILLEGAL token for the current position after an error has
// already been recorded by a helper.
func (l *Lexer) cur() token.Token {
	return token.Token{Type: token.ILLEGAL, Pos: l.pos()}
}

// scanDigitsSep consumes a run of digits accepted by pred, allowing single '_'
// numeric separators only strictly between two digits. It returns the number of
// digits consumed and whether the separator placement was valid (a leading,
// trailing, or doubled separator is invalid).
func (l *Lexer) scanDigitsSep(pred func(rune) bool) (int, bool) {
	n := 0
	prevWasSep := false
	for {
		switch {
		case pred(l.ch):
			n++
			prevWasSep = false
			l.readRune()
		case l.ch == '_':
			// A separator must sit between two digits: at least one digit must
			// precede it, it may not follow another separator, and a digit must
			// follow it.
			if n == 0 || prevWasSep || !pred(l.peek()) {
				return n, false
			}
			prevWasSep = true
			l.readRune()
		default:
			return n, true
		}
	}
}

// scanString scans a single- or double-quoted string literal, decoding escape
// sequences into Literal while preserving the exact source in Raw.
func (l *Lexer) scanString(start token.Pos, nl bool) token.Token {
	quote := l.ch
	begin := l.offset
	l.readRune() // opening quote
	l.legacyEscape = ""

	var b strings.Builder
	for {
		// U+2028 LINE SEPARATOR and U+2029 PARAGRAPH SEPARATOR are permitted as
		// literal characters in a string literal (ES2019+); only <LF> and <CR>
		// terminate one.
		if l.ch == eof || l.ch == '\n' || l.ch == '\r' {
			return l.errorf("unterminated string literal")
		}
		if l.ch == quote {
			l.readRune() // closing quote
			break
		}
		if l.ch == '\\' {
			l.readRune()
			l.scanEscape(&b)
			continue
		}
		b.WriteRune(l.ch)
		l.readRune()
	}
	raw := l.input[begin:l.offset]
	return token.Token{Type: token.STRING, Literal: b.String(), Raw: raw, Pos: start, NewlineBefore: nl, StrictError: l.legacyEscape}
}

// escapeError reports a malformed escape sequence. Inside a template segment
// (templateMode) the error is deferred: cookedInvalid is set so the segment has
// no cooked value, but no lexer error is raised — a tagged template tolerates
// it. Outside a template (a string literal) it is an immediate SyntaxError.
func (l *Lexer) escapeError(format string, args ...any) {
	if l.templateMode {
		l.cookedInvalid = true
		return
	}
	l.errorf(format, args...)
}

// scanEscape decodes a single escape sequence (the backslash is already
// consumed) and appends the result to b.
func (l *Lexer) scanEscape(b *strings.Builder) {
	switch l.ch {
	case 'n':
		b.WriteByte('\n')
		l.readRune()
	case 't':
		b.WriteByte('\t')
		l.readRune()
	case 'r':
		b.WriteByte('\r')
		l.readRune()
	case 'b':
		b.WriteByte('\b')
		l.readRune()
	case 'f':
		b.WriteByte('\f')
		l.readRune()
	case 'v':
		b.WriteByte('\v')
		l.readRune()
	case '0', '1', '2', '3', '4', '5', '6', '7':
		// \0 not followed by a digit is the NUL character and is legal in strict
		// mode. Any other octal digit run is a LegacyOctalEscapeSequence: legal
		// in sloppy mode, a strict-mode early error.
		if l.ch == '0' && !isDigit(l.peek()) {
			b.WriteByte(0)
			l.readRune()
			return
		}
		l.legacyEscape = "Octal escape sequences are not allowed in strict mode"
		// A template has no Annex B octal leniency: a legacy octal escape has no
		// cooked value at all (ECMA-262 §12.9.6).
		if l.templateMode {
			l.cookedInvalid = true
		}
		l.scanOctalEscape(b)
	case '8', '9':
		// \8 and \9 are NonOctalDecimalEscapeSequences: in sloppy mode they
		// denote the digit itself; in strict mode they are early errors. In a
		// template they have no cooked value.
		l.legacyEscape = "\\8 and \\9 are not allowed in strict mode"
		if l.templateMode {
			l.cookedInvalid = true
		}
		b.WriteRune(l.ch)
		l.readRune()
	case 'x':
		l.readRune()
		hi := hexVal(l.ch)
		if hi < 0 {
			// Stop at the offending character rather than consuming it, so the
			// enclosing scanner (e.g. a template still searching for its closing
			// backtick) does not swallow a delimiter.
			l.escapeError("invalid hexadecimal escape sequence")
			return
		}
		l.readRune()
		lo := hexVal(l.ch)
		if lo < 0 {
			l.escapeError("invalid hexadecimal escape sequence")
			return
		}
		l.readRune()
		b.WriteRune(rune(hi*16 + lo))
	case 'u':
		l.readRune()
		l.scanUnicodeEscapeCombining(b)
	case '\r':
		// Line continuation: \<CR><LF>? produces nothing.
		l.readRune()
		if l.ch == '\n' {
			l.readRune()
		}
	case '\n', 0x2028, 0x2029:
		l.readRune() // line continuation
	default:
		// Any other escaped character is itself (e.g. \' \" \\ \/).
		b.WriteRune(l.ch)
		l.readRune()
	}
}

// scanOctalEscape decodes a LegacyOctalEscapeSequence (the leading octal digit
// is the current rune). It consumes up to three octal digits, but only two when
// the first is 4-7, matching ECMA-262 Annex B.1.2.
func (l *Lexer) scanOctalEscape(b *strings.Builder) {
	maxDigits := 3
	if l.ch >= '4' && l.ch <= '7' {
		maxDigits = 2
	}
	val := 0
	for i := 0; i < maxDigits && l.ch >= '0' && l.ch <= '7'; i++ {
		val = val*8 + int(l.ch-'0')
		l.readRune()
	}
	b.WriteRune(rune(val))
}

// scanUnicodeEscape decodes \uXXXX or \u{XXXXXX} (the "\u" is already consumed)
// and returns the decoded code point, whether the braced \u{} form was used, and
// whether decoding succeeded.
func (l *Lexer) scanUnicodeEscape() (val rune, braced, ok bool) {
	if l.ch == '{' {
		l.readRune()
		v := 0
		digits := 0
		for isHexDigit(l.ch) {
			v = v*16 + hexVal(l.ch)
			if v > 0x10FFFF {
				// Clamp so the accumulator cannot overflow; the out-of-range
				// condition is checked after the closing brace is consumed.
				v = 0x110000
			}
			l.readRune()
			digits++
		}
		if l.ch != '}' {
			l.escapeError("invalid Unicode escape sequence")
			return 0, true, false
		}
		l.readRune() // consume '}'
		// CodePoint requires at least one hex digit and a value <= 0x10FFFF
		// (ECMA-262 §12.9.4.1). Consuming the brace above keeps a template scan
		// aligned so it can still find its closing backtick.
		if digits == 0 || v > 0x10FFFF {
			l.escapeError("invalid Unicode escape sequence")
			return 0, true, false
		}
		return rune(v), true, true
	}
	v := 0
	for i := 0; i < 4; i++ {
		d := hexVal(l.ch)
		if d < 0 {
			l.escapeError("invalid Unicode escape sequence")
			return 0, false, false
		}
		v = v*16 + d
		l.readRune()
	}
	return rune(v), false, true
}

// scanUnicodeEscapeCombining decodes a \u escape and writes it to b, combining a
// leading-surrogate \uHHHH escape with an immediately following trailing-surrogate
// \uHHHH escape into the single astral code point they denote (ECMAScript string
// literal SV). This preserves astral characters written as surrogate-pair escapes,
// which Go's strings.Builder would otherwise turn into two U+FFFD replacements.
// Only the non-braced 4-hex form pairs; a \u{...} code-point escape does not.
func (l *Lexer) scanUnicodeEscapeCombining(b *strings.Builder) {
	hi, braced, ok := l.scanUnicodeEscape()
	if !ok {
		return
	}
	if !braced && hi >= 0xD800 && hi <= 0xDBFF && l.ch == '\\' && l.peek() == 'u' {
		l.readRune() // consume '\'
		l.readRune() // consume 'u'
		lo, loBraced, ok := l.scanUnicodeEscape()
		if !ok {
			writeStrCodePoint(b, hi)
			return
		}
		if !loBraced && lo >= 0xDC00 && lo <= 0xDFFF {
			b.WriteRune((hi-0xD800)<<10 + (lo - 0xDC00) + 0x10000)
			return
		}
		writeStrCodePoint(b, hi)
		writeStrCodePoint(b, lo)
		return
	}
	writeStrCodePoint(b, hi)
}

// writeStrCodePoint writes a decoded string-literal code point. A lone surrogate
// (U+D800..U+DFFF), which strings.Builder.WriteRune would fold to U+FFFD, is
// preserved verbatim in WTF-8 so that "\uD83D" and String.fromCharCode(0xD83D)
// produce identical internal strings (matching the interp storage format).
func writeStrCodePoint(b *strings.Builder, r rune) {
	if r >= 0xD800 && r <= 0xDFFF {
		b.WriteByte(0xE0 | byte(r>>12))
		b.WriteByte(0x80 | byte((r>>6)&0x3F))
		b.WriteByte(0x80 | byte(r&0x3F))
		return
	}
	b.WriteRune(r)
}

// scanTemplate scans a template literal segment. When head is true the current
// rune is the opening backtick; otherwise it is the '}' that closes a
// substitution and resumes the template. The returned token is one of
// TEMPLATE_NOSUB, TEMPLATE_HEAD, TEMPLATE_MIDDLE, or TEMPLATE_TAIL.
func (l *Lexer) scanTemplate(start token.Pos, nl bool, head bool) token.Token {
	l.readRune() // consume '`' (head) or '}' (continuation)
	// innerBegin marks the first character after the opening delimiter. The
	// Template Raw Value (ECMA-262 §12.9.6) is the source text between the
	// delimiters with escape sequences left undecoded and line terminators
	// normalized to LF; it is captured from the raw source slice below.
	innerBegin := l.offset
	// cookedInvalid records whether this segment contains an escape with no valid
	// cooked value; a fresh scan starts valid.
	l.cookedInvalid = false

	var b strings.Builder
	for {
		switch {
		case l.ch == eof:
			return l.errorf("unterminated template literal")
		case l.ch == '`':
			raw := templateRawValue(l.input[innerBegin:l.offset])
			l.readRune()
			typ := token.TEMPLATE_TAIL
			if head {
				typ = token.TEMPLATE_NOSUB
			}
			return token.Token{Type: typ, Literal: b.String(), Raw: raw, Pos: start, NewlineBefore: nl, CookedInvalid: l.cookedInvalid}
		case l.ch == '$' && l.peek() == '{':
			raw := templateRawValue(l.input[innerBegin:l.offset])
			l.readRune() // $
			l.readRune() // {
			// Remember the brace depth so the matching '}' resumes template
			// scanning rather than closing a block.
			l.templateBraceStack = append(l.templateBraceStack, l.braceDepth)
			typ := token.TEMPLATE_MIDDLE
			if head {
				typ = token.TEMPLATE_HEAD
			}
			return token.Token{Type: typ, Literal: b.String(), Raw: raw, Pos: start, NewlineBefore: nl, CookedInvalid: l.cookedInvalid}
		case l.ch == '\\':
			l.readRune()
			l.templateMode = true
			l.scanEscape(&b)
			l.templateMode = false
		case l.ch == '\r':
			// The Template Value normalizes a <CR> or <CR><LF> LineTerminatorSequence
			// to a single <LF> (ECMA-262 §12.9.6, TV). (U+2028/U+2029 are kept as
			// themselves and fall through to the default case.)
			b.WriteByte('\n')
			l.readRune()
			if l.ch == '\n' {
				l.readRune()
			}
		default:
			b.WriteRune(l.ch)
			l.readRune()
		}
	}
}

// templateRawValue computes a template segment's Template Raw Value from the
// source text between its delimiters: escape sequences are preserved verbatim,
// while <CR><LF> and lone <CR> line terminators are normalized to a single
// <LF> (ECMA-262 §12.9.6, TRV).
func templateRawValue(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' {
			b.WriteByte('\n')
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// scanRegex scans a regular-expression literal /pattern/flags. The current rune
// is the opening '/'.
func (l *Lexer) scanRegex(start token.Pos, nl bool) token.Token {
	begin := l.offset
	l.readRune() // opening '/'

	inClass := false // inside a [...] character class, where '/' is literal
	var pattern strings.Builder
	for {
		if l.ch == eof || isLineTerminator(l.ch) {
			return l.errorf("unterminated regular expression literal")
		}
		if l.ch == '\\' {
			pattern.WriteRune(l.ch)
			l.readRune()
			if l.ch == eof || isLineTerminator(l.ch) {
				return l.errorf("unterminated regular expression literal")
			}
			pattern.WriteRune(l.ch)
			l.readRune()
			continue
		}
		if l.ch == '[' {
			inClass = true
		} else if l.ch == ']' {
			inClass = false
		} else if l.ch == '/' && !inClass {
			l.readRune() // closing '/'
			break
		}
		pattern.WriteRune(l.ch)
		l.readRune()
	}
	// Flags.
	var flags strings.Builder
	for isIdentPart(l.ch) {
		flags.WriteRune(l.ch)
		l.readRune()
	}
	raw := l.input[begin:l.offset]
	return token.Token{
		Type:          token.REGEX,
		Literal:       pattern.String(),
		Raw:           raw,
		Pos:           start,
		NewlineBefore: nl,
	}
}

// hexVal returns the numeric value of a hex digit, or -1 if ch is not one.
func hexVal(ch rune) int {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0')
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10
	case ch >= 'A' && ch <= 'F':
		return int(ch-'A') + 10
	}
	return -1
}
