package harness

import (
	"testing"

	"github.com/iceisfun/gojs/parser"
)

// expectParseError asserts that parsing src (as a whole program) fails. It is
// used for strict-mode early errors, whose directive prologue must be the very
// first statement in the program — something the assert-prologue-injecting Run
// helper cannot express.
func expectParseError(t *testing.T, src string) {
	t.Helper()
	if _, err := parser.Parse("<literals>", src); err == nil {
		t.Fatalf("expected a parse error but parsing succeeded: %q", src)
	}
}

// This file covers ECMAScript literal early-errors and values that the Test262
// language/literals category exercises: numeric separators, legacy octal /
// non-octal-decimal integers, BigInt edge cases, string escape sequences, and
// line/paragraph separators inside string literals.

// --- Numeric separator early errors (mode-independent SyntaxErrors) ---

func TestNumericSeparatorErrors(t *testing.T) {
	bad := []string{
		"0x_1",   // separator right after hex prefix
		"0b_1",   // separator right after binary prefix
		"0o_1",   // separator right after octal prefix
		"1_",     // trailing separator
		"1__2",   // doubled separator
		"1_.5",   // separator before dot
		"1._5",   // separator after dot
		"1_e5",   // separator before exponent
		"1e_5",   // separator after exponent-marker
		"0_8",    // leading-zero + separator
		"08_0",   // non-octal decimal + separator
		"09_0",   // non-octal decimal + separator
		"00_0",   // legacy octal + separator
		"01_0",   // legacy octal + separator
		"07_0",   // legacy octal + separator
		"0_0123", // leading-zero + separator
		"0x1__2", // doubled separator in hex
		"0x1_",   // trailing separator in hex
	}
	for _, src := range bad {
		ExpectError(t, "var x = "+src+";", "SyntaxError")
	}
}

func TestNumericSeparatorValues(t *testing.T) {
	Expect(t, `
		assert.sameValue(1_000, 1000);
		assert.sameValue(1_000_000, 1000000);
		assert.sameValue(0x1_0, 16);
		assert.sameValue(0b1_0, 2);
		assert.sameValue(0o1_7, 15);
		assert.sameValue(1_000.5, 1000.5);
		assert.sameValue(1e1_0, 1e10);
		assert.sameValue(1_2.3_4e1_0, 12.34e10);
	`)
}

// --- Truncated radix literals (missing digits) ---

func TestTruncatedRadixLiterals(t *testing.T) {
	for _, src := range []string{"0x", "0X", "0b", "0B", "0o", "0O", "0xg", "0b2", "0o8"} {
		ExpectError(t, "var x = "+src+";", "SyntaxError")
	}
}

// --- Legacy octal / non-octal-decimal integers ---

func TestLegacyOctalSloppyValues(t *testing.T) {
	Expect(t, `
		assert.sameValue(010, 8);
		assert.sameValue(00, 0);
		assert.sameValue(0777, 511);
		assert.sameValue(0o17, 15);
		assert.sameValue(08, 8);
		assert.sameValue(09, 9);
		assert.sameValue(0118, 118);
		assert.sameValue(0119, 119);
	`)
}

func TestLegacyOctalStrictErrors(t *testing.T) {
	for _, src := range []string{"010", "01", "0777", "00", "08", "09", "0118"} {
		expectParseError(t, "\"use strict\";\nvar x = "+src+";")
	}
}

// --- BigInt edge cases (always SyntaxError) ---

func TestBigIntInvalid(t *testing.T) {
	bad := []string{
		"08n", "09n", "00n", "01n", "07n", "0008n", "012348n",
		"1.5n", "1.0n", ".5n", "1e2n", "1E2n",
	}
	for _, src := range bad {
		ExpectError(t, "var x = "+src+";", "SyntaxError")
	}
}

func TestBigIntValid(t *testing.T) {
	Expect(t, `
		assert.sameValue(0n, 0n);
		assert.sameValue(123n, 123n);
		assert.sameValue(0x1Fn, 31n);
		assert.sameValue(0o17n, 15n);
		assert.sameValue(0b101n, 5n);
		assert.sameValue(123_456n, 123456n);
	`)
}

// --- String legacy octal / non-octal escape sequences ---

func TestStringOctalEscapeSloppyValues(t *testing.T) {
	Expect(t, `
		assert.sameValue("\1".charCodeAt(0), 1);
		assert.sameValue("\7".charCodeAt(0), 7);
		assert.sameValue("\12".charCodeAt(0), 10);
		assert.sameValue("\101", "A");
		assert.sameValue("\377".charCodeAt(0), 255);
		assert.sameValue("\40", " ");
		assert.sameValue("\400", " 0");
		assert.sameValue("\0".charCodeAt(0), 0);
		assert.sameValue("\8", "8");
		assert.sameValue("\9", "9");
	`)
}

func TestStringOctalEscapeStrictErrors(t *testing.T) {
	for _, src := range []string{`"\1"`, `"\01"`, `"\123"`, `"\7"`, `"\8"`, `"\9"`} {
		expectParseError(t, "\"use strict\";\nvar x = "+src+";")
	}
}

// A legacy octal escape inside the directive prologue of strict code is an
// early error (the "use strict" directive still turns the code strict).
func TestStringOctalEscapeInStrictPrologue(t *testing.T) {
	expectParseError(t, "\"use strict\";\n\"\\145\";\n")
}

// A function whose own body prologue contains "use strict" turns the body
// strict, so a legacy escape or octal literal preceding that directive inside
// the body is an early error even when the surrounding program is sloppy.
func TestFunctionBodyStrictPrologue(t *testing.T) {
	expectParseError(t, `function invalid() { "\1"; "use strict"; }`)
	expectParseError(t, `(function() { "asterisk: \052"; "use strict"; });`)
	expectParseError(t, `var f = () => { "\1"; "use strict"; };`)
	expectParseError(t, `function outer() { "use strict"; return 010; }`)
	// Sanity: without the directive the same body is legal in sloppy code.
	if _, err := parser.Parse("<ok>", `function ok() { "\1"; return 010; }`); err != nil {
		t.Fatalf("unexpected parse error for sloppy function body: %v", err)
	}
}

// --- RegExp named-group / named-backreference early errors ---

func TestRegexpNamedGroupErrors(t *testing.T) {
	bad := []string{
		`/(?<>a)/`,           // empty group name
		`/(?<42a>a)/`,        // group name starting with a digit
		`/(?<:a>a)/`,         // punctuator starting group name
		`/(?<a:>a)/`,         // punctuator within group name
		`/(?<aa)/`,           // unterminated group specifier
		"/(?<❤>a)/",     // non-identifier group name
		`/(?<a>a)(?<a>a)/`,   // duplicate group name in a sequence
		`/(?<a>a)(?<b>b)(?<a>a)/`,
		`/(?<a>(?<a>x))/`, // duplicate name nested on the same path
		`/(?<a>.)\k<b>/`, // dangling backreference
		`/(?<a>.)\k/`,    // incomplete backreference
		`/(?<a>.)\k<>/`,  // empty backreference name
		`/(?<a>.)\k<a/`,      // unterminated backreference
		`/\k<a(?<a>a)/`,      // backreference name must not swallow a group
		`/\k<a>/u`,           // unicode mode: \k with no matching group is an error
		`/\k/u`,              // unicode mode: bare \k is an invalid escape
		`/\k<>/u`,            // unicode mode: empty backreference name
		`/(?<a\uD801>.)/`,    // lone-surrogate escape in name
		`/(?<a\u{1F08B}>.)/`, // non-identifier astral escape in name
	}
	for _, src := range bad {
		expectParseError(t, "var re = "+src+";")
	}
}

func TestRegexpNamedGroupValid(t *testing.T) {
	// These must parse without error (RE2 may still reject backreferences at
	// runtime, but that is a separate, documented divergence — parsing succeeds).
	ok := []string{
		`/(?<a>x)/`,
		`/(?<year>\d{4})-(?<month>\d{2})/`,
		`/(?<$_0>x)/`,
		`/(?<a>.)\k<a>/`,   // valid named backreference (well-formed)
		`/(?<=x)y/`,        // lookbehind, not a named group
		`/(?<!x)y/`,        // negative lookbehind
		`/\k<a>/`,          // \k with no named groups is a legacy identity escape
		`/[(?<a>)]/`,               // '(' and named-group syntax are literal in a class
		`/(?<name1>x)(?<name2>y)/`, // multiple valid identifier names
		`/(?<a>x)|(?<a>y)/`,        // duplicate name across alternatives is allowed
		`/(?:(?<a>x))|(?<a>y)/`,    // duplicate across alternatives, one nested
	}
	for i, src := range ok {
		if _, err := parseOK(src); err != nil {
			t.Fatalf("case %d %q: unexpected parse error: %v", i, src, err)
		}
	}
}

func parseOK(src string) (interface{}, error) {
	return parser.Parse("<literals>", "var re = "+src+";")
}

// --- Line / paragraph separators are permitted inside string literals ---

func TestLineParagraphSeparatorInString(t *testing.T) {
	Expect(t, "var s = ' '; assert.sameValue(s.length, 1);")
	Expect(t, "var s = ' '; assert.sameValue(s.length, 1);")
	Expect(t, "var s = \"a b\"; assert.sameValue(s.length, 3);")
}
