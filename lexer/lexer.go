// Package lexer implements a lexical scanner for JavaScript (ECMAScript)
// source text.
//
// The scanner converts UTF-8 source into a stream of [token.Token] values via
// repeated calls to [Lexer.Next]. It handles the full punctuator/operator set,
// identifiers (including Unicode and $ / _), all numeric literal forms
// (decimal, hex, octal, binary, BigInt, separators), string literals with
// escape sequences, template literals with ${} substitutions, and regular
// expression literals.
//
// # Regex vs. division
//
// A leading '/' is ambiguous: it may begin a regular-expression literal or a
// division operator. Like most hand-written JS lexers, we disambiguate using
// the previous significant token — a '/' after a value-producing token (an
// identifier, literal, ')' or ']') is division; otherwise it starts a regex.
// See regexAllowed.
//
// # Automatic Semicolon Insertion
//
// The parser implements ASI (ECMA-262 §12.10), but it needs to know where line
// terminators occur. Each token records whether a newline preceded it via
// [token.Token.NewlineBefore].
//
// ECMA-262 Reference: §12 – Lexical Grammar.
package lexer

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/iceisfun/gojs/token"
)

// eof is the sentinel rune returned once the input is exhausted.
const eof = rune(-1)

// Lexer scans JavaScript source into tokens.
type Lexer struct {
	source string // source name, for error messages
	input  string // full source text

	ch       rune // current rune (eof at end of input)
	offset   int  // byte offset of ch
	rdOffset int  // byte offset of the rune after ch
	line     int  // 1-based line of ch
	col      int  // 1-based column of ch

	// prevType is the type of the last non-trivia token emitted. It drives
	// regex-vs-division disambiguation.
	prevType token.Type

	// pendingNewline records whether a line terminator has been skipped since
	// the previous token; it is transferred to the next token's NewlineBefore.
	pendingNewline bool

	// templateBraceStack tracks, for each open template substitution, the brace
	// nesting depth at which the substitution began. When a '}' closes back to
	// that depth, template scanning resumes instead of emitting RBRACE.
	templateBraceStack []int
	braceDepth         int

	// legacyEscape records, while a string literal is being scanned, that it
	// contained a legacy octal escape (\1..\7, \0 followed by a digit) or a
	// non-octal decimal escape (\8, \9). These are legal in sloppy mode but are
	// strict-mode early errors; scanString transfers the flag to the token's
	// StrictError for the parser to raise when the code is strict.
	legacyEscape string

	err *token.SyntaxError
}

// New creates a Lexer for the given source. sourceName appears in error
// messages. If stripShebang is true, a leading "#!" line is skipped.
func New(sourceName, input string, stripShebang bool) *Lexer {
	l := &Lexer{
		source:   sourceName,
		input:    input,
		line:     1,
		col:      0,
		prevType: token.ILLEGAL,
	}
	l.readRune()
	if stripShebang && l.ch == '#' && l.peek() == '!' {
		for l.ch != eof && !isLineTerminator(l.ch) {
			l.readRune()
		}
	}
	return l
}

// Err returns the first lexical error encountered, or nil.
func (l *Lexer) Err() error {
	if l.err == nil {
		return nil
	}
	return l.err
}

// readRune advances to the next rune, decoding UTF-8. Invalid bytes are passed
// through as their raw value so binary content in strings survives.
func (l *Lexer) readRune() {
	if l.ch == eof {
		return // already at end; nothing to advance
	}
	// Advance line/column bookkeeping based on the rune we are leaving. This
	// runs before the end-of-input check so the final rune still advances the
	// column, giving end-of-file tokens accurate span end positions.
	if l.ch == '\n' {
		l.line++
		l.col = 1
	} else {
		// The initial sentinel (l.ch == 0 before the first read) also lands
		// here, moving the cursor from the notional column 0 to column 1.
		l.col++
	}
	if l.rdOffset >= len(l.input) {
		l.offset = len(l.input)
		l.ch = eof
		return
	}
	l.offset = l.rdOffset
	r, size := utf8.DecodeRuneInString(l.input[l.rdOffset:])
	if r == utf8.RuneError && size == 1 {
		r = rune(l.input[l.rdOffset])
	}
	l.ch = r
	l.rdOffset += size
}

// peek returns the rune after the current one without advancing.
func (l *Lexer) peek() rune {
	if l.rdOffset >= len(l.input) {
		return eof
	}
	r, size := utf8.DecodeRuneInString(l.input[l.rdOffset:])
	if r == utf8.RuneError && size == 1 {
		return rune(l.input[l.rdOffset])
	}
	return r
}

// peek2 returns the rune two positions ahead (used for e.g. "...", ">>>").
func (l *Lexer) peek2() rune {
	off := l.rdOffset
	if off >= len(l.input) {
		return eof
	}
	_, size := utf8.DecodeRuneInString(l.input[off:])
	off += size
	if off >= len(l.input) {
		return eof
	}
	r, size2 := utf8.DecodeRuneInString(l.input[off:])
	if r == utf8.RuneError && size2 == 1 {
		return rune(l.input[off])
	}
	return r
}

// pos returns the current source position.
func (l *Lexer) pos() token.Pos {
	return token.Pos{Source: l.source, Offset: l.offset, Line: l.line, Column: l.col}
}

// errorf records a syntax error at the current position (only the first is
// kept) and returns an ILLEGAL token.
func (l *Lexer) errorf(format string, args ...any) token.Token {
	p := l.pos()
	if l.err == nil {
		l.err = &token.SyntaxError{Pos: p, Msg: fmt.Sprintf(format, args...)}
	}
	return token.Token{Type: token.ILLEGAL, Pos: p, NewlineBefore: l.takeNewline()}
}

// takeNewline consumes and returns the pending-newline flag.
func (l *Lexer) takeNewline() bool {
	n := l.pendingNewline
	l.pendingNewline = false
	return n
}

// skipTrivia consumes whitespace and comments, recording whether any line
// terminator was seen (for ASI).
func (l *Lexer) skipTrivia() {
	for {
		switch {
		case l.ch == '\n' || l.ch == '\r' || isLineTerminator(l.ch):
			l.pendingNewline = true
			l.readRune()
		case l.ch == ' ' || l.ch == '\t' || l.ch == '\v' || l.ch == '\f' ||
			l.ch == 0xFEFF || l.ch == 0xA0 || (l.ch > 0x7F && unicode.IsSpace(l.ch)):
			l.readRune()
		case l.ch == '/' && l.peek() == '/':
			l.readRune()
			l.readRune()
			for l.ch != eof && !isLineTerminator(l.ch) {
				l.readRune()
			}
		case l.ch == '/' && l.peek() == '*':
			l.readRune()
			l.readRune()
			for l.ch != eof && !(l.ch == '*' && l.peek() == '/') {
				if isLineTerminator(l.ch) {
					l.pendingNewline = true
				}
				l.readRune()
			}
			if l.ch == eof {
				l.errorf("unterminated block comment")
				return
			}
			l.readRune() // '*'
			l.readRune() // '/'
		default:
			return
		}
	}
}

// Next scans and returns the next token. At end of input it repeatedly returns
// an EOF token.
func (l *Lexer) Next() token.Token {
	l.skipTrivia()
	nl := l.takeNewline()
	start := l.pos()

	if l.ch == eof {
		return l.emit(token.Token{Type: token.EOF, Pos: start, NewlineBefore: nl})
	}

	// Identifiers and keywords.
	if isIdentStart(l.ch) {
		return l.emit(l.scanIdent(start, nl))
	}
	// Private names (#foo).
	if l.ch == '#' {
		return l.emit(l.scanPrivate(start, nl))
	}
	// Numeric literals.
	if isDigit(l.ch) || (l.ch == '.' && isDigit(l.peek())) {
		return l.emit(l.scanNumber(start, nl))
	}
	// String literals.
	if l.ch == '"' || l.ch == '\'' {
		return l.emit(l.scanString(start, nl))
	}
	// Template literals.
	if l.ch == '`' {
		return l.emit(l.scanTemplate(start, nl, true))
	}

	return l.emit(l.scanPunct(start, nl))
}

// emit finalizes a scanned token: it fills in the end of the token's source
// span (the scanner leaves the cursor one past the token) and records the type
// as prevType for regex-vs-division disambiguation.
func (l *Lexer) emit(t token.Token) token.Token {
	if (t.End == token.Pos{}) {
		t.End = l.pos()
	}
	if t.Type != token.COMMENT {
		l.prevType = t.Type
	}
	return t
}

// scanIdent scans an identifier or keyword.
func (l *Lexer) scanIdent(start token.Pos, nl bool) token.Token {
	begin := l.offset
	for isIdentPart(l.ch) {
		l.readRune()
	}
	name := l.input[begin:l.offset]
	typ := token.LookupIdent(name)
	return token.Token{Type: typ, Literal: name, Raw: name, Pos: start, NewlineBefore: nl}
}

// scanPrivate scans a private name such as #count.
func (l *Lexer) scanPrivate(start token.Pos, nl bool) token.Token {
	begin := l.offset
	l.readRune() // consume '#'
	if !isIdentStart(l.ch) {
		return l.errorf("unexpected character after '#'")
	}
	for isIdentPart(l.ch) {
		l.readRune()
	}
	name := l.input[begin:l.offset]
	return token.Token{Type: token.PRIVATE, Literal: name, Raw: name, Pos: start, NewlineBefore: nl}
}

// regexAllowed reports whether a '/' at the current point should begin a
// regular-expression literal (true) or a division operator (false), based on
// the previous significant token.
func (l *Lexer) regexAllowed() bool {
	switch l.prevType {
	case token.IDENT, token.PRIVATE, token.NUMBER, token.BIGINT, token.STRING,
		token.REGEX, token.TEMPLATE_NOSUB, token.TEMPLATE_TAIL,
		token.RPAREN, token.RBRACKET,
		token.THIS, token.SUPER, token.TRUE, token.FALSE, token.NULL,
		token.INC, token.DEC:
		return false
	default:
		return true
	}
}

// scanPunct scans a punctuator, operator, or a regex literal (when a '/' is in
// regex position).
func (l *Lexer) scanPunct(start token.Pos, nl bool) token.Token {
	ch := l.ch
	mk := func(typ token.Type, n int) token.Token {
		raw := l.input[l.offset : l.offset+n]
		for i := 0; i < n; i++ {
			l.readRune()
		}
		if typ == token.LBRACE {
			l.braceDepth++
		} else if typ == token.RBRACE {
			l.braceDepth--
		}
		return token.Token{Type: typ, Literal: raw, Raw: raw, Pos: start, NewlineBefore: nl}
	}

	switch ch {
	case '{':
		return mk(token.LBRACE, 1)
	case '}':
		// A '}' may close a template substitution rather than a block.
		if n := len(l.templateBraceStack); n > 0 && l.templateBraceStack[n-1] == l.braceDepth {
			l.templateBraceStack = l.templateBraceStack[:n-1]
			return l.scanTemplate(start, nl, false)
		}
		return mk(token.RBRACE, 1)
	case '(':
		return mk(token.LPAREN, 1)
	case ')':
		return mk(token.RPAREN, 1)
	case '[':
		return mk(token.LBRACKET, 1)
	case ']':
		return mk(token.RBRACKET, 1)
	case ';':
		return mk(token.SEMICOLON, 1)
	case ',':
		return mk(token.COMMA, 1)
	case ':':
		return mk(token.COLON, 1)
	case '~':
		return mk(token.BIT_NOT, 1)
	case '.':
		if l.peek() == '.' && l.peek2() == '.' {
			return mk(token.ELLIPSIS, 3)
		}
		return mk(token.DOT, 1)
	case '?':
		switch l.peek() {
		case '.':
			// ?. is optional chaining, but ?.5 is a conditional followed by a
			// number, so only treat as OPTIONAL when not followed by a digit.
			if !isDigit(l.peek2()) {
				return mk(token.OPTIONAL, 2)
			}
			return mk(token.QUESTION, 1)
		case '?':
			if l.peek2() == '=' {
				return mk(token.NULLISH_ASSIGN, 3)
			}
			return mk(token.NULLISH, 2)
		default:
			return mk(token.QUESTION, 1)
		}
	case '+':
		switch l.peek() {
		case '+':
			return mk(token.INC, 2)
		case '=':
			return mk(token.PLUS_ASSIGN, 2)
		}
		return mk(token.PLUS, 1)
	case '-':
		switch l.peek() {
		case '-':
			return mk(token.DEC, 2)
		case '=':
			return mk(token.MINUS_ASSIGN, 2)
		}
		return mk(token.MINUS, 1)
	case '*':
		switch l.peek() {
		case '*':
			if l.peek2() == '=' {
				return mk(token.EXP_ASSIGN, 3)
			}
			return mk(token.EXP, 2)
		case '=':
			return mk(token.STAR_ASSIGN, 2)
		}
		return mk(token.STAR, 1)
	case '/':
		if l.regexAllowed() {
			return l.scanRegex(start, nl)
		}
		if l.peek() == '=' {
			return mk(token.SLASH_ASSIGN, 2)
		}
		return mk(token.SLASH, 1)
	case '%':
		if l.peek() == '=' {
			return mk(token.PERCENT_ASSIGN, 2)
		}
		return mk(token.PERCENT, 1)
	case '=':
		switch l.peek() {
		case '=':
			if l.peek2() == '=' {
				return mk(token.STRICT_EQ, 3)
			}
			return mk(token.EQ, 2)
		case '>':
			return mk(token.ARROW, 2)
		}
		return mk(token.ASSIGN, 1)
	case '!':
		if l.peek() == '=' {
			if l.peek2() == '=' {
				return mk(token.STRICT_NE, 3)
			}
			return mk(token.NE, 2)
		}
		return mk(token.NOT, 1)
	case '<':
		switch l.peek() {
		case '<':
			if l.peek2() == '=' {
				return mk(token.SHL_ASSIGN, 3)
			}
			return mk(token.SHL, 2)
		case '=':
			return mk(token.LE, 2)
		}
		return mk(token.LT, 1)
	case '>':
		switch l.peek() {
		case '>':
			if l.peek2() == '>' {
				// >>> or >>>=
				if l.thirdIs('=') {
					return mk(token.USHR_ASSIGN, 4)
				}
				return mk(token.USHR, 3)
			}
			if l.peek2() == '=' {
				return mk(token.SHR_ASSIGN, 3)
			}
			return mk(token.SHR, 2)
		case '=':
			return mk(token.GE, 2)
		}
		return mk(token.GT, 1)
	case '&':
		switch l.peek() {
		case '&':
			if l.peek2() == '=' {
				return mk(token.AND_ASSIGN, 3)
			}
			return mk(token.AND, 2)
		case '=':
			return mk(token.BIT_AND_ASSIGN, 2)
		}
		return mk(token.BIT_AND, 1)
	case '|':
		switch l.peek() {
		case '|':
			if l.peek2() == '=' {
				return mk(token.OR_ASSIGN, 3)
			}
			return mk(token.OR, 2)
		case '=':
			return mk(token.BIT_OR_ASSIGN, 2)
		}
		return mk(token.BIT_OR, 1)
	case '^':
		if l.peek() == '=' {
			return mk(token.BIT_XOR_ASSIGN, 2)
		}
		return mk(token.BIT_XOR, 1)
	}
	tok := l.errorf("unexpected character %q", ch)
	l.readRune()
	return tok
}

// thirdIs reports whether the rune three positions ahead equals r. Used to
// distinguish ">>>" from ">>>=".
func (l *Lexer) thirdIs(r rune) bool {
	off := l.rdOffset
	for i := 0; i < 2 && off < len(l.input); i++ {
		_, size := utf8.DecodeRuneInString(l.input[off:])
		off += size
	}
	if off >= len(l.input) {
		return false
	}
	nr, _ := utf8.DecodeRuneInString(l.input[off:])
	return nr == r
}

// ---------------------------------------------------------------------------
// Character classification helpers
// ---------------------------------------------------------------------------

func isDigit(ch rune) bool { return ch >= '0' && ch <= '9' }

func isHexDigit(ch rune) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// isIdentStart reports whether ch may start an identifier.
func isIdentStart(ch rune) bool {
	return ch == '$' || ch == '_' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch > 0x7F && unicode.IsLetter(ch))
}

// isIdentPart reports whether ch may continue an identifier.
func isIdentPart(ch rune) bool {
	return isIdentStart(ch) || isDigit(ch) ||
		(ch > 0x7F && (unicode.IsDigit(ch) || unicode.IsMark(ch) || ch == 0x200C || ch == 0x200D))
}

// isLineTerminator reports whether ch is an ECMAScript line terminator.
func isLineTerminator(ch rune) bool {
	return ch == '\n' || ch == '\r' || ch == 0x2028 || ch == 0x2029
}
