package lexer

import (
	"testing"

	"github.com/iceisfun/gojs/token"
)

// collect runs the lexer to EOF and returns all token types.
func collect(t *testing.T, src string) []token.Token {
	t.Helper()
	l := New("test", src, false)
	var toks []token.Token
	for {
		tk := l.Next()
		if tk.Type == token.EOF {
			break
		}
		toks = append(toks, tk)
	}
	if err := l.Err(); err != nil {
		t.Fatalf("lexer error for %q: %v", src, err)
	}
	return toks
}

func TestPunctuatorsAndOperators(t *testing.T) {
	src := `= == === != !== => ... ?? ?. >>> >>>= ** **= &&= ||=`
	want := []token.Type{
		token.ASSIGN, token.EQ, token.STRICT_EQ, token.NE, token.STRICT_NE,
		token.ARROW, token.ELLIPSIS, token.NULLISH, token.OPTIONAL,
		token.USHR, token.USHR_ASSIGN, token.EXP, token.EXP_ASSIGN,
		token.AND_ASSIGN, token.OR_ASSIGN,
	}
	toks := collect(t, src)
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, tk := range toks {
		if tk.Type != want[i] {
			t.Errorf("token %d = %v, want %v", i, tk.Type, want[i])
		}
	}
}

func TestKeywordsAndIdent(t *testing.T) {
	toks := collect(t, `function foo let x return`)
	want := []token.Type{token.FUNCTION, token.IDENT, token.LET, token.IDENT, token.RETURN}
	for i, tk := range toks {
		if tk.Type != want[i] {
			t.Errorf("token %d = %v, want %v", i, tk.Type, want[i])
		}
	}
}

func TestNumbers(t *testing.T) {
	cases := map[string]token.Type{
		"42":      token.NUMBER,
		"3.14":    token.NUMBER,
		".5":      token.NUMBER,
		"1e10":    token.NUMBER,
		"0xFF":    token.NUMBER,
		"0o17":    token.NUMBER,
		"0b1010":  token.NUMBER,
		"1_000":   token.NUMBER,
		"123n":    token.BIGINT,
		"0xdeadn": token.BIGINT,
	}
	for src, want := range cases {
		toks := collect(t, src)
		if len(toks) != 1 || toks[0].Type != want {
			t.Errorf("%q => %v, want single %v", src, toks, want)
		}
	}
}

func TestStringsAndEscapes(t *testing.T) {
	toks := collect(t, `"a\nb" 'c\td' "\x41B\u{1F600}"`)
	if toks[0].Literal != "a\nb" {
		t.Errorf("string 0 literal = %q", toks[0].Literal)
	}
	if toks[1].Literal != "c\td" {
		t.Errorf("string 1 literal = %q", toks[1].Literal)
	}
	if toks[2].Literal != "AB\U0001F600" {
		t.Errorf("string 2 literal = %q", toks[2].Literal)
	}
}

func TestRegexVsDivision(t *testing.T) {
	// After an identifier, '/' is division.
	toks := collect(t, `a / b`)
	if toks[1].Type != token.SLASH {
		t.Errorf("expected SLASH, got %v", toks[1].Type)
	}
	// After '=', '/' starts a regex.
	toks = collect(t, `x = /ab+c/gi`)
	if toks[2].Type != token.REGEX {
		t.Errorf("expected REGEX, got %v", toks[2].Type)
	}
	if toks[2].Literal != "ab+c" || toks[2].Raw != "/ab+c/gi" {
		t.Errorf("regex literal=%q raw=%q", toks[2].Literal, toks[2].Raw)
	}
}

func TestTemplateLiteral(t *testing.T) {
	toks := collect(t, "`a${b + 1}c${d}e`")
	want := []token.Type{
		token.TEMPLATE_HEAD, token.IDENT, token.PLUS, token.NUMBER,
		token.TEMPLATE_MIDDLE, token.IDENT, token.TEMPLATE_TAIL,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens %v, want %d", len(toks), toks, len(want))
	}
	for i, tk := range toks {
		if tk.Type != want[i] {
			t.Errorf("token %d = %v, want %v", i, tk.Type, want[i])
		}
	}
	if toks[0].Literal != "a" || toks[4].Literal != "c" || toks[6].Literal != "e" {
		t.Errorf("template segments: %q %q %q", toks[0].Literal, toks[4].Literal, toks[6].Literal)
	}
}

func TestTemplateNestedBraces(t *testing.T) {
	// An object literal inside a substitution must not prematurely close it.
	toks := collect(t, "`${ {a:1} }`")
	want := []token.Type{
		token.TEMPLATE_HEAD, token.LBRACE, token.IDENT, token.COLON,
		token.NUMBER, token.RBRACE, token.TEMPLATE_TAIL,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %v", toks)
	}
	for i, tk := range toks {
		if tk.Type != want[i] {
			t.Errorf("token %d = %v, want %v", i, tk.Type, want[i])
		}
	}
}

func TestNewlineBefore(t *testing.T) {
	toks := collect(t, "a\nb")
	if toks[0].NewlineBefore {
		t.Errorf("first token should not have NewlineBefore")
	}
	if !toks[1].NewlineBefore {
		t.Errorf("token after newline should have NewlineBefore")
	}
}

func TestTokenSpans(t *testing.T) {
	// "foo" spans columns 1..4; "==" on the next line spans its own range.
	toks := collect(t, "foo ==\n  bar")
	if toks[0].Pos.Line != 1 || toks[0].Pos.Column != 1 {
		t.Errorf("foo start = %v", toks[0].Pos)
	}
	if toks[0].End.Column != 4 {
		t.Errorf("foo end column = %d, want 4", toks[0].End.Column)
	}
	// bar begins on line 2, column 3.
	bar := toks[2]
	if bar.Pos.Line != 2 || bar.Pos.Column != 3 {
		t.Errorf("bar start = %v, want 2:3", bar.Pos)
	}
	if bar.End.Column != 6 {
		t.Errorf("bar end column = %d, want 6", bar.End.Column)
	}
	if got := bar.Span().String(); got != "test:2:3-2:6" {
		t.Errorf("bar span = %q", got)
	}
}
