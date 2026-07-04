// Package token defines the lexical token types, keywords, and source
// positions for the gojs JavaScript engine.
//
// The token model is loosely based on the ECMAScript lexical grammar and the
// Ladybird LibJS token set (see reference/ladybird Token.h). Every token type
// belongs to a broad [Category] (number, string, punctuation, keyword, etc.)
// which the parser and syntax-highlighting tools use for quick classification.
//
// ECMA-262 Reference: §12 – Lexical Grammar.
package token

import "fmt"

// Type identifies a single lexical token kind (a keyword, a punctuator, a
// literal, or a structural marker such as EOF).
type Type int

// Category is a coarse classification of a token, useful for tools such as
// syntax highlighters and for the parser's fast dispatch. It mirrors LibJS's
// TokenCategory enum.
type Category int

const (
	// CategoryInvalid marks a malformed or unrecognized token.
	CategoryInvalid Category = iota
	// CategoryTrivia marks whitespace and comments (normally skipped).
	CategoryTrivia
	// CategoryNumber marks numeric literals (including BigInt).
	CategoryNumber
	// CategoryString marks string and template literals.
	CategoryString
	// CategoryPunctuation marks structural punctuation such as braces.
	CategoryPunctuation
	// CategoryOperator marks operators such as + and &&.
	CategoryOperator
	// CategoryKeyword marks reserved words such as function and return.
	CategoryKeyword
	// CategoryIdentifier marks identifiers and private names.
	CategoryIdentifier
)

// Token types. The zero value is [ILLEGAL] so an uninitialized token is never
// mistaken for a valid one. The ordering here is not load-bearing (unlike the
// LibJS enum, which is synchronized with Rust); ranges such as keywords are
// bounded by explicit sentinels (keywordBeg/keywordEnd) instead.
const (
	// --- Special / structural ---------------------------------------------

	ILLEGAL Type = iota // an unrecognized or malformed token
	EOF                 // end of source input
	COMMENT             // // line comment or /* block comment */

	// --- Literals ---------------------------------------------------------

	IDENT   // identifier: foo, bar, $x, _y
	PRIVATE // private class field name: #count
	NUMBER  // numeric literal: 42, 3.14, 0xff, 1e10
	BIGINT  // bigint literal: 123n
	STRING  // string literal: "abc", 'abc'
	// Template literal pieces. A template like `a${b}c` lexes to:
	//   TEMPLATE_HEAD("a") EXPR... TEMPLATE_TAIL("c")
	// while a simple `abc` template lexes to a single TEMPLATE_NOSUB.
	TEMPLATE_NOSUB  // `no substitution template`
	TEMPLATE_HEAD   // `head${
	TEMPLATE_MIDDLE // }middle${
	TEMPLATE_TAIL   // }tail`
	REGEX           // regular expression literal: /ab+c/gi

	// --- Punctuators ------------------------------------------------------

	LPAREN    // (
	RPAREN    // )
	LBRACKET  // [
	RBRACKET  // ]
	LBRACE    // {
	RBRACE    // }
	DOT       // .
	ELLIPSIS  // ...
	SEMICOLON // ;
	COMMA     // ,
	COLON     // :
	ARROW     // =>
	QUESTION  // ?
	OPTIONAL  // ?.  (optional chaining)

	// --- Operators --------------------------------------------------------

	ASSIGN  // =
	PLUS    // +
	MINUS   // -
	STAR    // *
	SLASH   // /
	PERCENT // %
	EXP     // **

	INC // ++
	DEC // --

	EQ        // ==
	NE        // !=
	STRICT_EQ // ===
	STRICT_NE // !==
	LT        // <
	GT        // >
	LE        // <=
	GE        // >=

	AND     // &&
	OR      // ||
	NOT     // !
	NULLISH // ??

	BIT_AND // &
	BIT_OR  // |
	BIT_XOR // ^
	BIT_NOT // ~
	SHL     // <<
	SHR     // >>
	USHR    // >>> (unsigned right shift)

	// Compound assignment operators.
	PLUS_ASSIGN    // +=
	MINUS_ASSIGN   // -=
	STAR_ASSIGN    // *=
	SLASH_ASSIGN   // /=
	PERCENT_ASSIGN // %=
	EXP_ASSIGN     // **=
	SHL_ASSIGN     // <<=
	SHR_ASSIGN     // >>=
	USHR_ASSIGN    // >>>=
	BIT_AND_ASSIGN // &=
	BIT_OR_ASSIGN  // |=
	BIT_XOR_ASSIGN // ^=
	AND_ASSIGN     // &&=
	OR_ASSIGN      // ||=
	NULLISH_ASSIGN // ??=

	// --- Keywords ---------------------------------------------------------
	// keywordBeg/keywordEnd bracket the reserved-word range so IsKeyword can
	// be a simple bounds check. Keep all keyword constants between them.

	keywordBeg
	BREAK
	CASE
	CATCH
	CLASS
	CONST
	CONTINUE
	DEBUGGER
	DEFAULT
	DELETE
	DO
	ELSE
	ENUM
	EXPORT
	EXTENDS
	FALSE
	FINALLY
	FOR
	FUNCTION
	IF
	IMPORT
	IN
	INSTANCEOF
	NEW
	NULL
	RETURN
	SUPER
	SWITCH
	THIS
	THROW
	TRUE
	TRY
	TYPEOF
	VAR
	VOID
	WHILE
	WITH
	// Contextual / strict-mode keywords. These are only reserved in certain
	// positions; the lexer still tags them so the parser can decide.
	LET
	STATIC
	YIELD
	ASYNC
	AWAIT
	OF
	GET
	SET
	keywordEnd
)

// keywords maps reserved-word source text to its token type.
var keywords = map[string]Type{
	"break":      BREAK,
	"case":       CASE,
	"catch":      CATCH,
	"class":      CLASS,
	"const":      CONST,
	"continue":   CONTINUE,
	"debugger":   DEBUGGER,
	"default":    DEFAULT,
	"delete":     DELETE,
	"do":         DO,
	"else":       ELSE,
	"enum":       ENUM,
	"export":     EXPORT,
	"extends":    EXTENDS,
	"false":      FALSE,
	"finally":    FINALLY,
	"for":        FOR,
	"function":   FUNCTION,
	"if":         IF,
	"import":     IMPORT,
	"in":         IN,
	"instanceof": INSTANCEOF,
	"new":        NEW,
	"null":       NULL,
	"return":     RETURN,
	"super":      SUPER,
	"switch":     SWITCH,
	"this":       THIS,
	"throw":      THROW,
	"true":       TRUE,
	"try":        TRY,
	"typeof":     TYPEOF,
	"var":        VAR,
	"void":       VOID,
	"while":      WHILE,
	"with":       WITH,
	// Contextual keywords.
	"let":    LET,
	"static": STATIC,
	"yield":  YIELD,
	"async":  ASYNC,
	"await":  AWAIT,
	"of":     OF,
	"get":    GET,
	"set":    SET,
}

// tokenNames maps token types to a human-readable label used by String and in
// parser error messages.
var tokenNames = map[Type]string{
	ILLEGAL: "ILLEGAL",
	EOF:     "<eof>",
	COMMENT: "<comment>",

	IDENT:           "<identifier>",
	PRIVATE:         "<private-name>",
	NUMBER:          "<number>",
	BIGINT:          "<bigint>",
	STRING:          "<string>",
	TEMPLATE_NOSUB:  "<template>",
	TEMPLATE_HEAD:   "<template-head>",
	TEMPLATE_MIDDLE: "<template-middle>",
	TEMPLATE_TAIL:   "<template-tail>",
	REGEX:           "<regex>",

	LPAREN:    "(",
	RPAREN:    ")",
	LBRACKET:  "[",
	RBRACKET:  "]",
	LBRACE:    "{",
	RBRACE:    "}",
	DOT:       ".",
	ELLIPSIS:  "...",
	SEMICOLON: ";",
	COMMA:     ",",
	COLON:     ":",
	ARROW:     "=>",
	QUESTION:  "?",
	OPTIONAL:  "?.",

	ASSIGN:  "=",
	PLUS:    "+",
	MINUS:   "-",
	STAR:    "*",
	SLASH:   "/",
	PERCENT: "%",
	EXP:     "**",

	INC: "++",
	DEC: "--",

	EQ:        "==",
	NE:        "!=",
	STRICT_EQ: "===",
	STRICT_NE: "!==",
	LT:        "<",
	GT:        ">",
	LE:        "<=",
	GE:        ">=",

	AND:     "&&",
	OR:      "||",
	NOT:     "!",
	NULLISH: "??",

	BIT_AND: "&",
	BIT_OR:  "|",
	BIT_XOR: "^",
	BIT_NOT: "~",
	SHL:     "<<",
	SHR:     ">>",
	USHR:    ">>>",

	PLUS_ASSIGN:    "+=",
	MINUS_ASSIGN:   "-=",
	STAR_ASSIGN:    "*=",
	SLASH_ASSIGN:   "/=",
	PERCENT_ASSIGN: "%=",
	EXP_ASSIGN:     "**=",
	SHL_ASSIGN:     "<<=",
	SHR_ASSIGN:     ">>=",
	USHR_ASSIGN:    ">>>=",
	BIT_AND_ASSIGN: "&=",
	BIT_OR_ASSIGN:  "|=",
	BIT_XOR_ASSIGN: "^=",
	AND_ASSIGN:     "&&=",
	OR_ASSIGN:      "||=",
	NULLISH_ASSIGN: "??=",
}

// LookupIdent returns the keyword token type for ident, or [IDENT] if ident is
// not a reserved word.
func LookupIdent(ident string) Type {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return IDENT
}

// IsKeyword reports whether t is a reserved-word token.
func (t Type) IsKeyword() bool {
	return t > keywordBeg && t < keywordEnd
}

// IsReservedWord reports whether name is a ReservedWord (ECMA-262 §12.7.2): one
// of the always-reserved keywords. The contextual keywords (let, static, yield,
// async, await, of, get, set) are deliberately excluded — they are only reserved
// in specific positions, which the parser decides. This is used to reject an
// escaped IdentifierName whose StringValue is a reserved word where an Identifier
// is required.
func IsReservedWord(name string) bool {
	t, ok := keywords[name]
	if !ok {
		return false
	}
	switch t {
	case LET, STATIC, YIELD, ASYNC, AWAIT, OF, GET, SET:
		return false
	}
	return true
}

// Category returns the coarse [Category] for t.
func (t Type) Category() Category {
	switch {
	case t == ILLEGAL:
		return CategoryInvalid
	case t == COMMENT:
		return CategoryTrivia
	case t == NUMBER || t == BIGINT:
		return CategoryNumber
	case t == STRING || t == REGEX ||
		t == TEMPLATE_NOSUB || t == TEMPLATE_HEAD ||
		t == TEMPLATE_MIDDLE || t == TEMPLATE_TAIL:
		return CategoryString
	case t == IDENT || t == PRIVATE:
		return CategoryIdentifier
	case t.IsKeyword():
		return CategoryKeyword
	case t >= ASSIGN && t <= NULLISH_ASSIGN:
		return CategoryOperator
	default:
		return CategoryPunctuation
	}
}

// String returns a human-readable label for the token type. Keywords render as
// their source spelling; punctuators and operators as their symbol.
func (t Type) String() string {
	if t.IsKeyword() {
		for kw, kt := range keywords {
			if kt == t {
				return kw
			}
		}
	}
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("token(%d)", int(t))
}

// Pos is a location in a source file. Offset is a 0-based byte offset; Line and
// Column are 1-based. The zero value denotes an unknown position.
type Pos struct {
	Source string // source name (e.g. filename or "<eval>")
	Offset int    // 0-based byte offset into the source
	Line   int    // 1-based line number
	Column int    // 1-based column number
}

// String formats the position as "source:line:column".
func (p Pos) String() string {
	if p.Source == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Column)
	}
	return fmt.Sprintf("%s:%d:%d", p.Source, p.Line, p.Column)
}

// Token is a single lexical token with its type, source text, and source span.
//
// Pos is the position of the token's first byte; End is the position just past
// its last byte. Together they form a half-open span [Pos, End) that downstream
// tooling (the parser, error reporter, and highlighter) uses to underline the
// exact source range of a diagnostic.
type Token struct {
	Type    Type
	Literal string // decoded/normalized text (identifier name, string contents)
	Raw     string // exact source slice, delimiters included (for error "near" text)
	Pos     Pos    // start of the token
	End     Pos    // one past the end of the token

	// NewlineBefore reports whether at least one line terminator appeared
	// between the previous token and this one. The parser needs this to
	// implement Automatic Semicolon Insertion (ECMA-262 §12.10).
	NewlineBefore bool

	// StrictError, when non-empty, describes an early error that applies only
	// when the token appears in strict-mode code (e.g. a LegacyOctalIntegerLiteral
	// or an octal/non-octal escape in a string literal). The lexer cannot know
	// the strictness of the enclosing scope, so it records the condition here and
	// the parser raises it when the surrounding code is strict.
	StrictError string

	// CookedInvalid reports that a template segment (TEMPLATE_NOSUB / _HEAD /
	// _MIDDLE / _TAIL) contained an escape sequence with no valid cooked value —
	// a LegacyOctalEscapeSequence, a NonOctalDecimalEscapeSequence (\8, \9), or a
	// malformed hex/unicode escape. Unlike a string literal, an untagged template
	// has no Annex B leniency, so such a segment is an unconditional early
	// SyntaxError (ECMA-262 §12.9.6); a tagged template instead tolerates it and
	// yields an undefined cooked value.
	CookedInvalid bool

	// Escaped reports whether an IDENT or PRIVATE token's source contained a
	// UnicodeEscapeSequence (\uXXXX or \u{...}) in its IdentifierName. Such a
	// token is always an IdentifierName, never a keyword, but its StringValue
	// (Literal) may spell a reserved word — which the parser must reject wherever
	// an Identifier (binding/reference/label), rather than an IdentifierName, is
	// required (ECMA-262 §12.7.2, §13.1.1).
	Escaped bool
}

// Span is a half-open range of source positions [Start, End). It is the unit of
// location used throughout error reporting so a diagnostic can point at a range
// rather than a single caret.
type Span struct {
	Start Pos
	End   Pos
}

// String formats the span. A zero-width or single-position span renders as just
// its start; otherwise as "start-endline:endcol".
func (s Span) String() string {
	if s.Start == s.End {
		return s.Start.String()
	}
	return fmt.Sprintf("%s-%d:%d", s.Start, s.End.Line, s.End.Column)
}

// Span returns the token's source span.
func (t Token) Span() Span { return Span{Start: t.Pos, End: t.End} }

// String returns a debugging representation of the token.
func (t Token) String() string {
	switch t.Type {
	case IDENT, STRING, PRIVATE, REGEX:
		return fmt.Sprintf("%s(%q)", t.Type, t.Literal)
	case NUMBER, BIGINT:
		return fmt.Sprintf("%s(%s)", t.Type, t.Literal)
	case EOF:
		return "<eof>"
	default:
		return t.Type.String()
	}
}

// SyntaxError is a parse or lexical error carrying the source position at which
// it occurred. It mirrors the shape of a JavaScript SyntaxError.
type SyntaxError struct {
	Pos Pos
	Msg string
}

// Error formats the error as "source:line:column: message".
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}
