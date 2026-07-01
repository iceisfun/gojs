package lexer

import (
	"strings"

	"github.com/iceisfun/gojs/token"
)

// This file holds the scanners for multi-character literal forms: numbers,
// strings, template literals, and regular expressions. They are kept separate
// from the core dispatch in lexer.go for readability.

// scanNumber scans a numeric literal in any of JavaScript's forms: decimal
// (42, 3.14, .5, 1e10), hexadecimal (0xFF), octal (0o17), binary (0b1010), and
// BigInt (123n). Numeric separators ('_') are permitted between digits.
func (l *Lexer) scanNumber(start token.Pos, nl bool) token.Token {
	begin := l.offset
	isBigInt := false

	if l.ch == '0' && (l.peek() == 'x' || l.peek() == 'X') {
		l.readRune() // 0
		l.readRune() // x
		l.scanDigits(isHexDigit)
	} else if l.ch == '0' && (l.peek() == 'o' || l.peek() == 'O') {
		l.readRune()
		l.readRune()
		l.scanDigits(func(r rune) bool { return r >= '0' && r <= '7' })
	} else if l.ch == '0' && (l.peek() == 'b' || l.peek() == 'B') {
		l.readRune()
		l.readRune()
		l.scanDigits(func(r rune) bool { return r == '0' || r == '1' })
	} else {
		// Decimal integer part.
		l.scanDigits(isDigit)
		// Fractional part.
		if l.ch == '.' {
			l.readRune()
			l.scanDigits(isDigit)
		}
		// Exponent part.
		if l.ch == 'e' || l.ch == 'E' {
			l.readRune()
			if l.ch == '+' || l.ch == '-' {
				l.readRune()
			}
			if !isDigit(l.ch) {
				return l.errorf("missing exponent in number literal")
			}
			l.scanDigits(isDigit)
		}
	}

	// Optional BigInt suffix.
	if l.ch == 'n' {
		isBigInt = true
		l.readRune()
	}

	// An identifier character immediately following a number is an error
	// (e.g. 3in). This catches most malformed literals.
	if isIdentStart(l.ch) {
		return l.errorf("identifier starts immediately after numeric literal")
	}

	raw := l.input[begin:l.offset]
	if isBigInt {
		digits := strings.ReplaceAll(strings.TrimSuffix(raw, "n"), "_", "")
		return token.Token{Type: token.BIGINT, Literal: digits, Raw: raw, Pos: start, NewlineBefore: nl}
	}
	return token.Token{Type: token.NUMBER, Literal: raw, Raw: raw, Pos: start, NewlineBefore: nl}
}

// scanDigits consumes a run of digits accepted by pred, allowing single '_'
// separators between digits.
func (l *Lexer) scanDigits(pred func(rune) bool) {
	for pred(l.ch) || (l.ch == '_' && pred(l.peek())) {
		l.readRune()
	}
}

// scanString scans a single- or double-quoted string literal, decoding escape
// sequences into Literal while preserving the exact source in Raw.
func (l *Lexer) scanString(start token.Pos, nl bool) token.Token {
	quote := l.ch
	begin := l.offset
	l.readRune() // opening quote

	var b strings.Builder
	for {
		if l.ch == eof || isLineTerminator(l.ch) {
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
	return token.Token{Type: token.STRING, Literal: b.String(), Raw: raw, Pos: start, NewlineBefore: nl}
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
	case '0':
		// \0 not followed by a digit is a NUL character.
		if !isDigit(l.peek()) {
			b.WriteByte(0)
			l.readRune()
			return
		}
		// Legacy octal; consume the digit literally for simplicity.
		b.WriteRune(l.ch)
		l.readRune()
	case 'x':
		l.readRune()
		hi := hexVal(l.ch)
		l.readRune()
		lo := hexVal(l.ch)
		l.readRune()
		if hi < 0 || lo < 0 {
			l.errorf("invalid hexadecimal escape sequence")
			return
		}
		b.WriteRune(rune(hi*16 + lo))
	case 'u':
		l.readRune()
		l.scanUnicodeEscape(b)
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

// scanUnicodeEscape decodes \uXXXX or \u{XXXXXX} (the "\u" is already consumed).
func (l *Lexer) scanUnicodeEscape(b *strings.Builder) {
	if l.ch == '{' {
		l.readRune()
		val := 0
		for isHexDigit(l.ch) {
			val = val*16 + hexVal(l.ch)
			l.readRune()
		}
		if l.ch != '}' {
			l.errorf("invalid Unicode escape sequence")
			return
		}
		l.readRune()
		b.WriteRune(rune(val))
		return
	}
	val := 0
	for i := 0; i < 4; i++ {
		d := hexVal(l.ch)
		if d < 0 {
			l.errorf("invalid Unicode escape sequence")
			return
		}
		val = val*16 + d
		l.readRune()
	}
	b.WriteRune(rune(val))
}

// scanTemplate scans a template literal segment. When head is true the current
// rune is the opening backtick; otherwise it is the '}' that closes a
// substitution and resumes the template. The returned token is one of
// TEMPLATE_NOSUB, TEMPLATE_HEAD, TEMPLATE_MIDDLE, or TEMPLATE_TAIL.
func (l *Lexer) scanTemplate(start token.Pos, nl bool, head bool) token.Token {
	begin := l.offset
	l.readRune() // consume '`' (head) or '}' (continuation)

	var b strings.Builder
	for {
		switch {
		case l.ch == eof:
			return l.errorf("unterminated template literal")
		case l.ch == '`':
			l.readRune()
			raw := l.input[begin:l.offset]
			typ := token.TEMPLATE_TAIL
			if head {
				typ = token.TEMPLATE_NOSUB
			}
			return token.Token{Type: typ, Literal: b.String(), Raw: raw, Pos: start, NewlineBefore: nl}
		case l.ch == '$' && l.peek() == '{':
			l.readRune() // $
			l.readRune() // {
			// Remember the brace depth so the matching '}' resumes template
			// scanning rather than closing a block.
			l.templateBraceStack = append(l.templateBraceStack, l.braceDepth)
			raw := l.input[begin:l.offset]
			typ := token.TEMPLATE_MIDDLE
			if head {
				typ = token.TEMPLATE_HEAD
			}
			return token.Token{Type: typ, Literal: b.String(), Raw: raw, Pos: start, NewlineBefore: nl}
		case l.ch == '\\':
			l.readRune()
			l.scanEscape(&b)
		default:
			b.WriteRune(l.ch)
			l.readRune()
		}
	}
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
