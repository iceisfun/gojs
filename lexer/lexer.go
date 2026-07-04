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
	"strings"
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

	// braceKinds records, for each currently-open '{' emitted as an LBRACE
	// token, whether closing it ends a value expression (true) — an object
	// literal or the body of a function expression — as opposed to a block or
	// statement body (false). lastRBraceObject holds the kind of the most
	// recently closed '}' so regexAllowed can treat a value-ending '}' as the
	// end of an expression (a following '/' is then division, not a regex).
	braceKinds       []bool
	lastRBraceObject bool

	// Function-expression body tracking. A function expression is a value, so a
	// '/' after its body's '}' is division (`function(){}/2`), whereas a '/'
	// after a function *declaration* or a plain block begins a regex. When the
	// `function` keyword appears in operand position, funcExprParamNext marks
	// that its parameter '(' is next; parenIsFuncParams records which open
	// parens are such lists; funcExprBodyNext marks that the next '{' opens the
	// body of a function expression.
	parenIsFuncParams []bool
	funcExprParamNext bool
	funcExprBodyNext  bool

	// legacyEscape records, while a string literal is being scanned, that it
	// contained a legacy octal escape (\1..\7, \0 followed by a digit) or a
	// non-octal decimal escape (\8, \9). These are legal in sloppy mode but are
	// strict-mode early errors; scanString transfers the flag to the token's
	// StrictError for the parser to raise when the code is strict.
	legacyEscape string

	// templateMode is set while scanEscape is decoding an escape inside a template
	// segment. In that mode an invalid escape does not raise an immediate lexer
	// error (a tagged template tolerates it); instead cookedInvalid is set so the
	// segment can report an undefined cooked value, and the parser raises the
	// early error only for the untagged case.
	templateMode  bool
	cookedInvalid bool

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

	// Identifiers and keywords. An IdentifierName may also begin with a
	// UnicodeEscapeSequence (\uXXXX or \u{...}), e.g. abc (ECMA-262 §12.7).
	if isIdentStart(l.ch) || (l.ch == '\\' && l.peek() == 'u') {
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
		// A `function` keyword in operand position introduces a function
		// expression, whose body's closing '}' ends a value (so a following '/'
		// is division). Compute this against the *previous* token, before
		// prevType is overwritten below. The parameter '(' is the next paren.
		if t.Type == token.FUNCTION {
			l.funcExprParamNext = l.braceOpensObjectLiteral(t.NewlineBefore)
		}
		l.prevType = t.Type
	}
	return t
}

// scanIdent scans an identifier or keyword. The cursor is at an IdentifierStart
// character or at a '\' beginning a UnicodeEscapeSequence. When the name
// contains any escape it is always an IdentifierName (never a keyword token):
// its Literal is the decoded StringValue and Escaped is set, so the parser can
// apply the "escaped reserved word" early errors (ECMA-262 §12.7.2, §13.1.1).
func (l *Lexer) scanIdent(start token.Pos, nl bool) token.Token {
	begin := l.offset
	name, escaped, ok := l.scanIdentName(true)
	if !ok {
		return token.Token{Type: token.ILLEGAL, Pos: start, NewlineBefore: nl}
	}
	raw := l.input[begin:l.offset]
	typ := token.IDENT
	if !escaped {
		typ = token.LookupIdent(name)
	}
	return token.Token{Type: typ, Literal: name, Raw: raw, Pos: start, NewlineBefore: nl, Escaped: escaped}
}

// scanPrivate scans a private name such as #count or #\u{63}ount.
func (l *Lexer) scanPrivate(start token.Pos, nl bool) token.Token {
	begin := l.offset
	l.readRune() // consume '#'
	if !(isIdentStart(l.ch) || (l.ch == '\\' && l.peek() == 'u')) {
		return l.errorf("unexpected character after '#'")
	}
	name, escaped, ok := l.scanIdentName(true)
	if !ok {
		return token.Token{Type: token.ILLEGAL, Pos: start, NewlineBefore: nl}
	}
	raw := l.input[begin:l.offset]
	return token.Token{Type: token.PRIVATE, Literal: "#" + name, Raw: raw, Pos: start, NewlineBefore: nl, Escaped: escaped}
}

// scanIdentName scans an IdentifierName from the current cursor, decoding any
// \uXXXX / \u{...} UnicodeEscapeSequences to the code points they denote and
// returning the decoded StringValue. The first code point must satisfy
// IdentifierStart and the rest IdentifierPart; an escape that decodes to a code
// point failing that test is a SyntaxError (ECMA-262 §12.7.1). start reports
// whether the first element is being scanned (it always is for the two callers).
func (l *Lexer) scanIdentName(start bool) (name string, escaped bool, ok bool) {
	var b strings.Builder
	first := start
	for {
		if l.ch == '\\' && l.peek() == 'u' {
			l.readRune() // '\'
			l.readRune() // 'u'
			r, _, decoded := l.scanUnicodeEscape()
			if !decoded {
				return "", true, false // scanUnicodeEscape already recorded the error
			}
			if (first && !isIdentStart(r)) || (!first && !isIdentPart(r)) {
				l.errorf("invalid Unicode escape in identifier")
				return "", true, false
			}
			b.WriteRune(r)
			escaped = true
			first = false
			continue
		}
		if first {
			if !isIdentStart(l.ch) {
				return "", escaped, false
			}
		} else if !isIdentPart(l.ch) {
			break
		}
		b.WriteRune(l.ch)
		l.readRune()
		first = false
	}
	return b.String(), escaped, true
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
		token.INC, token.DEC,
		// Contextual keywords that are also IdentifierReferences: when one is used
		// as a value (e.g. `instance/of/g`, `let/2`), a following '/' is division.
		// Their keyword forms are never immediately followed by a regex literal —
		// unlike `yield`/`await`, whose operand may be a regex (`yield /re/`), so
		// those are deliberately excluded and keep starting a regex.
		token.OF, token.LET, token.STATIC, token.GET, token.SET, token.ASYNC:
		return false
	case token.RBRACE:
		// A '}' is ambiguous: it may close an object literal (a value, after
		// which '/' is division) or a block/function body (after which '/'
		// begins a regex, e.g. `{}` `/re/`). braceKinds tracked which one this
		// '}' closed; only an object-literal close switches to division.
		return !l.lastRBraceObject
	default:
		return true
	}
}

// braceOpensObjectLiteral reports whether a '{' just scanned begins an object
// literal (an expression) rather than a block statement, based on the preceding
// significant token. It is only ever true in operand position — after a token
// that requires an expression to follow — so a block-opening '{' is never
// misclassified. nl reports whether a line terminator preceded the '{', which
// matters for the newline-restricted productions (`return`/`throw`/`yield`),
// where an intervening newline triggers ASI and makes '{' a block.
func (l *Lexer) braceOpensObjectLiteral(nl bool) bool {
	switch l.prevType {
	// Punctuators and operators that require an operand next.
	case token.LPAREN, token.LBRACKET, token.COMMA, token.QUESTION, token.ELLIPSIS,
		token.ASSIGN, token.PLUS_ASSIGN, token.MINUS_ASSIGN, token.STAR_ASSIGN,
		token.SLASH_ASSIGN, token.PERCENT_ASSIGN, token.EXP_ASSIGN,
		token.SHL_ASSIGN, token.SHR_ASSIGN, token.USHR_ASSIGN,
		token.BIT_AND_ASSIGN, token.BIT_OR_ASSIGN, token.BIT_XOR_ASSIGN,
		token.AND_ASSIGN, token.OR_ASSIGN, token.NULLISH_ASSIGN,
		token.PLUS, token.MINUS, token.STAR, token.SLASH, token.PERCENT, token.EXP,
		token.EQ, token.NE, token.STRICT_EQ, token.STRICT_NE,
		token.LT, token.GT, token.LE, token.GE,
		token.AND, token.OR, token.NOT, token.NULLISH,
		token.BIT_AND, token.BIT_OR, token.BIT_XOR, token.BIT_NOT,
		token.SHL, token.SHR, token.USHR,
		// Keyword operators/expression heads after which '{' is an object literal
		// and no ASI can intervene to make it a block.
		token.TYPEOF, token.VOID, token.DELETE, token.NEW,
		token.IN, token.INSTANCEOF, token.AWAIT, token.CASE:
		return true
	// Newline-restricted productions: `return {}`, `throw {}`, `yield {}` open an
	// object literal only when the '{' is on the same line; a preceding newline
	// triggers ASI, making '{' a block statement.
	case token.RETURN, token.THROW, token.YIELD:
		return !nl
	default:
		return false
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
		switch typ {
		case token.LBRACE:
			l.braceDepth++
			valueEnding := l.funcExprBodyNext || l.braceOpensObjectLiteral(nl)
			l.funcExprBodyNext = false
			l.braceKinds = append(l.braceKinds, valueEnding)
		case token.RBRACE:
			l.braceDepth--
			if n := len(l.braceKinds); n > 0 {
				l.lastRBraceObject = l.braceKinds[n-1]
				l.braceKinds = l.braceKinds[:n-1]
			} else {
				l.lastRBraceObject = false
			}
		case token.LPAREN:
			isParams := l.funcExprParamNext
			l.funcExprParamNext = false
			l.parenIsFuncParams = append(l.parenIsFuncParams, isParams)
		case token.RPAREN:
			if n := len(l.parenIsFuncParams); n > 0 {
				isParams := l.parenIsFuncParams[n-1]
				l.parenIsFuncParams = l.parenIsFuncParams[:n-1]
				if isParams {
					// The next '{' opens this function expression's body.
					l.funcExprBodyNext = true
				}
			}
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

// isIdentStart reports whether ch may start an identifier. It implements the
// ECMAScript IdentifierStartChar production: '$', '_', or any code point with
// the Unicode ID_Start property (UnicodeIDStart, ECMA-262 §12.7). The ID_Start
// set is taken from idStartTable (generated from the Unicode Standard) rather
// than Go's category tables, which lag the current Unicode version.
func isIdentStart(ch rune) bool {
	if ch == '$' || ch == '_' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
		return true
	}
	if ch <= 0x7F {
		return false
	}
	return unicode.Is(idStartTable, ch)
}

// isIdentPart reports whether ch may continue an identifier. It implements the
// ECMAScript IdentifierPartChar production: '$', any code point with the
// Unicode ID_Continue property (UnicodeIDContinue, which subsumes ID_Start),
// and the ZWNJ/ZWJ joiners U+200C and U+200D (ECMA-262 §12.7). The ID_Continue
// set is taken from idContinueTable.
func isIdentPart(ch rune) bool {
	if ch == '$' || ch == '_' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || isDigit(ch) {
		return true
	}
	// ZWNJ and ZWJ are permitted in IdentifierPart.
	if ch == 0x200C || ch == 0x200D {
		return true
	}
	if ch <= 0x7F {
		return false
	}
	return unicode.Is(idContinueTable, ch)
}

// isLineTerminator reports whether ch is an ECMAScript line terminator.
func isLineTerminator(ch rune) bool {
	return ch == '\n' || ch == '\r' || ch == 0x2028 || ch == 0x2029
}
