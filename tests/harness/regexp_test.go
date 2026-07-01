package harness

import "testing"

// regexp_test.go — comprehensive RegExp tests for the gojs engine.
//
// Tests are organized by feature. Expected outcomes:
//
//	EXPECTED TO PASS (RE2-compatible core RegExp features):
//	  TestRegExpLiteralAndConstructor, TestRegExpProperties, TestRegExpToString,
//	  TestRegExpTest, TestRegExpExecNoMatch, TestRegExpExecBasic,
//	  TestRegExpExecCaptureGroups, TestRegExpExecGlobalLastIndex,
//	  TestRegExpCharacterClasses, TestRegExpAnchors, TestRegExpQuantifiers,
//	  TestRegExpLazyQuantifiers, TestRegExpAlternation, TestRegExpGroups,
//	  TestRegExpDotAndEscapes, TestRegExpEscapedMetachars,
//	  TestRegExpFlagI, TestRegExpFlagM, TestRegExpFlagS,
//	  TestRegExpInvalidPatternSyntaxError
//
//	EXPECTED TO FAIL — likely unimplemented (see NOTE comments):
//	  TestRegExpStringMatch, TestRegExpStringMatchAll, TestRegExpStringSearch,
//	  TestRegExpStringReplace, TestRegExpStringSplit, TestRegExpNamedCaptures
//
// RE2 caveat: Go's RE2 does not support backreferences (\1) or
// lookahead/lookbehind ((?=...) (?!...) (?<=...)). None are tested here.

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestRegExpLiteralAndConstructor(t *testing.T) {
	Expect(t, `
		var r1 = /hello/;
		assert.sameValue(r1 instanceof RegExp, true);
		assert.sameValue(typeof r1, "object");

		// constructor with string pattern
		var r2 = new RegExp("world");
		assert.sameValue(r2 instanceof RegExp, true);

		// constructor with flags
		var r3 = new RegExp("abc", "i");
		assert.sameValue(r3.ignoreCase, true);

		// constructor copying an existing RegExp
		var r4 = new RegExp(r1);
		assert.sameValue(r4.source, "hello");

		// empty-matching non-global pattern always matches
		var rEmpty = /(?:)/;
		assert.sameValue(rEmpty.test(""), true);
		assert.sameValue(rEmpty.test("x"), true);
	`)
}

// ---------------------------------------------------------------------------
// Properties: source, flags, global, ignoreCase, multiline, lastIndex
// ---------------------------------------------------------------------------

func TestRegExpProperties(t *testing.T) {
	Expect(t, `
		var ra = /abc/gi;
		assert.sameValue(ra.source, "abc");
		assert.sameValue(ra.global, true);
		assert.sameValue(ra.ignoreCase, true);
		assert.sameValue(ra.multiline, false);

		var rm = /xyz/m;
		assert.sameValue(rm.multiline, true);
		assert.sameValue(rm.global, false);
		assert.sameValue(rm.ignoreCase, false);

		// source preserves escaped metacharacters, no delimiters or flags
		var re = /a\.b/;
		assert.sameValue(re.source, "a\\.b");

		// flags string contains only the flags present
		assert.sameValue(/foo/.flags, "");
		assert.sameValue(/foo/.global, false);
		assert.sameValue(/foo/.ignoreCase, false);
		assert.sameValue(/foo/.multiline, false);

		// single-flag cases
		assert.sameValue(/foo/i.flags, "i");
		assert.sameValue(/foo/m.flags, "m");
		assert.sameValue(/foo/g.flags, "g");
		assert.sameValue(/foo/s.flags, "s");

		// multiple flags are sorted per spec order (d g i m s u v y)
		assert.sameValue(/foo/gi.flags, "gi");
		assert.sameValue(/foo/ig.flags, "gi");
		assert.sameValue(/foo/gim.flags, "gim");

		// lastIndex starts at 0
		assert.sameValue(/a/.lastIndex, 0);
		assert.sameValue(/a/g.lastIndex, 0);
	`)
}

// ---------------------------------------------------------------------------
// toString()
// ---------------------------------------------------------------------------

func TestRegExpToString(t *testing.T) {
	Expect(t, `
		assert.sameValue(/abc/.toString(), "/abc/");
		assert.sameValue(/abc/i.toString(), "/abc/i");
		assert.sameValue(/abc/gi.toString(), "/abc/gi");
		assert.sameValue(new RegExp("hello", "i").toString(), "/hello/i");
		// escaped metachar survives round-trip through source
		assert.sameValue(new RegExp("a\\.b").toString(), "/a\\.b/");
		// escaped forward-slash inside the pattern
		assert.sameValue(/a\/b/.toString(), "/a\\/b/");
	`)
}

// ---------------------------------------------------------------------------
// test()
// ---------------------------------------------------------------------------

func TestRegExpTest(t *testing.T) {
	Expect(t, `
		assert.sameValue(/hello/.test("say hello world"), true);
		assert.sameValue(/hello/.test("goodbye"), false);

		assert.sameValue(/^\d+$/.test("12345"), true);
		assert.sameValue(/^\d+$/.test("12a45"), false);

		assert.sameValue(/a/i.test("ABC"), true);
		assert.sameValue(/a/i.test("XYZ"), false);

		assert.sameValue(/^$/.test(""), true);
		assert.sameValue(/^$/.test("a"), false);

		// test() coerces its argument to a string
		assert.sameValue(/1/.test(1), true);
		assert.sameValue(/true/.test(true), true);
		assert.sameValue(/null/.test(null), true);
	`)
}

// ---------------------------------------------------------------------------
// exec() — no match
// ---------------------------------------------------------------------------

func TestRegExpExecNoMatch(t *testing.T) {
	Expect(t, `
		assert.sameValue(/xyz/.exec("hello world"), null);
		assert.sameValue(/^\d+$/.exec("abc"), null);
		assert.sameValue(/foo/.exec(""), null);
	`)
}

// ---------------------------------------------------------------------------
// exec() — basic result shape
// ---------------------------------------------------------------------------

func TestRegExpExecBasic(t *testing.T) {
	Expect(t, `
		var m = /world/.exec("hello world");
		assert.sameValue(m[0], "world");
		assert.sameValue(m.index, 6);
		assert.sameValue(m.input, "hello world");
		assert.sameValue(m.length, 1);

		// match at position 0
		var m2 = /\d+/.exec("42 is the answer");
		assert.sameValue(m2[0], "42");
		assert.sameValue(m2.index, 0);

		// non-global: each call starts from the beginning (lastIndex unused)
		var re = /\d+/;
		assert.sameValue(re.exec("a1b2")[0], "1");
		assert.sameValue(re.exec("a1b2")[0], "1");
	`)
}

// ---------------------------------------------------------------------------
// exec() — capture groups
// ---------------------------------------------------------------------------

func TestRegExpExecCaptureGroups(t *testing.T) {
	Expect(t, `
		// numbered capture groups
		var m = /(\d{4})-(\d{2})-(\d{2})/.exec("date: 2024-07-15");
		assert.sameValue(m[0], "2024-07-15");
		assert.sameValue(m[1], "2024");
		assert.sameValue(m[2], "07");
		assert.sameValue(m[3], "15");
		assert.sameValue(m.index, 6);
		assert.sameValue(m.input, "date: 2024-07-15");
		assert.sameValue(m.length, 4);

		// optional group that did not participate yields undefined
		var m2 = /(a)?(b)/.exec("b");
		assert.sameValue(m2[0], "b");
		assert.sameValue(m2[1], undefined);
		assert.sameValue(m2[2], "b");

		// nested groups: outer index < inner index
		var m3 = /(a(b)c)/.exec("abc");
		assert.sameValue(m3[0], "abc");
		assert.sameValue(m3[1], "abc");
		assert.sameValue(m3[2], "b");
	`)
}

// ---------------------------------------------------------------------------
// exec() — global flag + lastIndex advancement
// ---------------------------------------------------------------------------

func TestRegExpExecGlobalLastIndex(t *testing.T) {
	Expect(t, `
		var re = /\d+/g;
		assert.sameValue(re.lastIndex, 0);

		var m1 = re.exec("12 ab 34");
		assert.sameValue(m1[0], "12");
		assert.sameValue(re.lastIndex, 2);

		var m2 = re.exec("12 ab 34");
		assert.sameValue(m2[0], "34");
		assert.sameValue(re.lastIndex, 8);

		// exhausted — returns null and resets lastIndex to 0
		var m3 = re.exec("12 ab 34");
		assert.sameValue(m3, null);
		assert.sameValue(re.lastIndex, 0);

		// next call restarts from the beginning
		var m4 = re.exec("12 ab 34");
		assert.sameValue(m4[0], "12");
	`)
}

// ---------------------------------------------------------------------------
// Character classes
// ---------------------------------------------------------------------------

func TestRegExpCharacterClasses(t *testing.T) {
	Expect(t, `
		// [a-z] inclusive range
		assert.sameValue(/^[a-z]+$/.test("abc"), true);
		assert.sameValue(/^[a-z]+$/.test("ABC"), false);

		// [A-Z]
		assert.sameValue(/^[A-Z]+$/.test("ABC"), true);

		// [0-9]
		assert.sameValue(/^[0-9]+$/.test("123"), true);
		assert.sameValue(/^[0-9]+$/.test("12x"), false);

		// negated class [^...]
		assert.sameValue(/[^0-9]/.test("abc"), true);
		assert.sameValue(/^[^0-9]+$/.test("123"), false);

		// \d and \D
		assert.sameValue(/\d/.test("5"), true);
		assert.sameValue(/\d/.test("x"), false);
		assert.sameValue(/\D/.test("x"), true);
		assert.sameValue(/\D/.test("5"), false);

		// \w and \W  (\w matches [A-Za-z0-9_])
		assert.sameValue(/\w/.test("a"), true);
		assert.sameValue(/\w/.test("_"), true);
		assert.sameValue(/\w/.test("9"), true);
		assert.sameValue(/\w/.test("!"), false);
		assert.sameValue(/\W/.test("!"), true);
		assert.sameValue(/\W/.test("a"), false);

		// \s and \S
		assert.sameValue(/\s/.test(" "), true);
		assert.sameValue(/\s/.test("\t"), true);
		assert.sameValue(/\s/.test("\n"), true);
		assert.sameValue(/\s/.test("a"), false);
		assert.sameValue(/\S/.test("a"), true);
		assert.sameValue(/\S/.test(" "), false);

		// combined
		assert.sameValue(/^\w+$/.test("hello_123"), true);
		assert.sameValue(/^\w+$/.test("hello world"), false);
		assert.sameValue(/^\d{3}-\d{4}$/.test("123-4567"), true);
	`)
}

// ---------------------------------------------------------------------------
// Anchors: ^ $ \b \B
// ---------------------------------------------------------------------------

func TestRegExpAnchors(t *testing.T) {
	Expect(t, `
		// ^ matches start of string
		assert.sameValue(/^hello/.test("hello world"), true);
		assert.sameValue(/^hello/.test("say hello"), false);

		// $ matches end of string
		assert.sameValue(/world$/.test("hello world"), true);
		assert.sameValue(/world$/.test("world tour"), false);

		// both anchors
		assert.sameValue(/^hello world$/.test("hello world"), true);
		assert.sameValue(/^hello world$/.test("hello world!"), false);

		// anchor with quantifier
		assert.sameValue(/^\s*$/.test("   "), true);
		assert.sameValue(/^\s*$/.test("  x "), false);
		assert.sameValue(/^\d+$/.test(""), false);

		// \b word boundary
		assert.sameValue(/\bcat\b/.test("the cat sat"), true);
		assert.sameValue(/\bcat\b/.test("concatenate"), false);

		// \B non-word boundary
		assert.sameValue(/\Bcat\B/.test("concatenate"), true);
		assert.sameValue(/\Bcat\B/.test("the cat"), false);
	`)
}

// ---------------------------------------------------------------------------
// Quantifiers — greedy
// ---------------------------------------------------------------------------

func TestRegExpQuantifiers(t *testing.T) {
	Expect(t, `
		// * zero or more
		assert.sameValue(/ab*/.exec("a")[0], "a");
		assert.sameValue(/ab*/.exec("abbb")[0], "abbb");

		// + one or more
		assert.sameValue(/ab+/.exec("abbb")[0], "abbb");
		assert.sameValue(/ab+/.exec("a"), null);

		// ? zero or one
		assert.sameValue(/colou?r/.test("color"), true);
		assert.sameValue(/colou?r/.test("colour"), true);
		assert.sameValue(/colou?r/.test("colouur"), false);

		// {n} exactly n
		assert.sameValue(/^\d{4}$/.test("1234"), true);
		assert.sameValue(/^\d{4}$/.test("123"), false);
		assert.sameValue(/^\d{4}$/.test("12345"), false);
		assert.sameValue(/\d{4}/.exec("123456")[0], "1234");

		// {n,} at least n (greedy: takes all)
		assert.sameValue(/^\d{3,}$/.test("12"), false);
		assert.sameValue(/^\d{3,}$/.test("123"), true);
		assert.sameValue(/^\d{3,}$/.test("123456"), true);
		assert.sameValue(/\d{3,}/.exec("123456")[0], "123456");

		// {n,m} between n and m (greedy: takes max)
		assert.sameValue(/^\d{2,4}$/.test("1"), false);
		assert.sameValue(/^\d{2,4}$/.test("12"), true);
		assert.sameValue(/^\d{2,4}$/.test("1234"), true);
		assert.sameValue(/^\d{2,4}$/.test("12345"), false);
		assert.sameValue(/\d{2,4}/.exec("123456")[0], "1234");
	`)
}

// ---------------------------------------------------------------------------
// Quantifiers — lazy
// ---------------------------------------------------------------------------

func TestRegExpLazyQuantifiers(t *testing.T) {
	Expect(t, `
		// *? lazy: stops at first possible match end
		var m1 = /a.*?b/.exec("aXXb YYb");
		assert.sameValue(m1[0], "aXXb");

		// +? lazy one or more
		var m2 = /<.+?>/.exec("<a>foo</a>");
		assert.sameValue(m2[0], "<a>");

		// ?? lazy zero or one: prefers shorter
		assert.sameValue(/colou??r/.exec("color")[0], "color");
		assert.sameValue(/colou??r/.exec("colour")[0], "colour");

		// {n,m}? lazy: takes minimum
		var m3 = /\d{2,4}?/.exec("123456");
		assert.sameValue(m3[0], "12");

		// contrast: same range greedy takes maximum
		var mg = /\d{2,4}/.exec("123456");
		assert.sameValue(mg[0], "1234");
	`)
}

// ---------------------------------------------------------------------------
// Alternation: a|b
// ---------------------------------------------------------------------------

func TestRegExpAlternation(t *testing.T) {
	Expect(t, `
		assert.sameValue(/cat|dog/.test("I have a cat"), true);
		assert.sameValue(/cat|dog/.test("I have a dog"), true);
		assert.sameValue(/cat|dog/.test("I have a fish"), false);

		// leftmost match wins in left-to-right search
		var m = /cat|dog/.exec("my dog and cat");
		assert.sameValue(m[0], "dog");

		// alternation with anchors
		assert.sameValue(/^(yes|no)$/.test("yes"), true);
		assert.sameValue(/^(yes|no)$/.test("no"), true);
		assert.sameValue(/^(yes|no)$/.test("maybe"), false);

		// three alternatives
		assert.sameValue(/foo|bar|baz/.test("baz"), true);
		var m2 = /foo|bar|baz/.exec("xbary");
		assert.sameValue(m2[0], "bar");
	`)
}

// ---------------------------------------------------------------------------
// Groups — capturing and non-capturing
// ---------------------------------------------------------------------------

func TestRegExpGroups(t *testing.T) {
	Expect(t, `
		// capturing groups appear as numbered elements
		var m = /(foo)(bar)/.exec("foobar");
		assert.sameValue(m[0], "foobar");
		assert.sameValue(m[1], "foo");
		assert.sameValue(m[2], "bar");
		assert.sameValue(m.length, 3);

		// non-capturing group (?:...) is excluded from result
		var m2 = /(?:foo)(bar)/.exec("foobar");
		assert.sameValue(m2[0], "foobar");
		assert.sameValue(m2[1], "bar");
		assert.sameValue(m2.length, 2);

		// non-capturing group with alternation
		var m3 = /(?:foo|bar)baz/.exec("foobaz");
		assert.sameValue(m3[0], "foobaz");
		assert.sameValue(m3.length, 1);

		// nested groups: outer is m[1], inner is m[2]
		var m4 = /(a(b)c)/.exec("abc");
		assert.sameValue(m4[1], "abc");
		assert.sameValue(m4[2], "b");

		// group with quantifier: m[1] holds the last iteration
		var m5 = /(ab)+/.exec("ababab");
		assert.sameValue(m5[0], "ababab");
		assert.sameValue(m5[1], "ab");
	`)
}

// ---------------------------------------------------------------------------
// Dot and escape sequences
// ---------------------------------------------------------------------------

func TestRegExpDotAndEscapes(t *testing.T) {
	Expect(t, `
		// . matches any single char except newline (without s flag)
		assert.sameValue(/a.c/.test("abc"), true);
		assert.sameValue(/a.c/.test("a c"), true);
		assert.sameValue(/a.c/.test("a1c"), true);
		assert.sameValue(/a.c/.test("ac"), false);
		assert.sameValue(/a.c/.test("a\nc"), false);

		// .+ requires at least one non-newline char
		assert.sameValue(/^.+$/.test("hello"), true);
		assert.sameValue(/^.+$/.test(""), false);
		assert.sameValue(/^.+$/.test("line1\nline2"), false);

		// escape sequences in pattern
		assert.sameValue(/a\nb/.test("a\nb"), true);
		assert.sameValue(/a\tb/.test("a\tb"), true);
		assert.sameValue(/a\rb/.test("a\rb"), true);
	`)
}

func TestRegExpEscapedMetachars(t *testing.T) {
	Expect(t, `
		// \. matches literal dot, not any-char
		assert.sameValue(/a\.b/.test("a.b"), true);
		assert.sameValue(/a\.b/.test("axb"), false);

		// other escaped metacharacters
		assert.sameValue(/\$/.test("$100"), true);
		assert.sameValue(/\$/.test("100"), false);
		assert.sameValue(/\^/.test("^"), true);
		assert.sameValue(/\*/.test("2*3"), true);
		assert.sameValue(/\+/.test("1+2"), true);
		assert.sameValue(/\?/.test("what?"), true);
		assert.sameValue(/\(/.test("(a)"), true);
		assert.sameValue(/\)/.test("(a)"), true);
		assert.sameValue(/\[/.test("["), true);
		assert.sameValue(/\]/.test("]"), true);
		assert.sameValue(/\\/.test("a\\b"), true);
		assert.sameValue(/\\/.test("ab"), false);
		assert.sameValue(/\//.test("a/b"), true);
	`)
}

// ---------------------------------------------------------------------------
// Flag i — case-insensitive
// ---------------------------------------------------------------------------

func TestRegExpFlagI(t *testing.T) {
	Expect(t, `
		assert.sameValue(/hello/i.test("Hello"), true);
		assert.sameValue(/hello/i.test("HELLO"), true);
		assert.sameValue(/hello/i.test("HELO"), false);

		// case-insensitive character class
		assert.sameValue(/[a-z]/i.test("A"), true);
		assert.sameValue(/[a-z]/i.test("1"), false);

		// exec preserves the original casing in the matched text
		var m = /foo/i.exec("FOO BAR");
		assert.sameValue(m[0], "FOO");
		assert.sameValue(m.index, 0);

		// full word
		var m2 = /^[a-z]+$/i.exec("HelloWorld");
		assert.sameValue(m2[0], "HelloWorld");
	`)
}

// ---------------------------------------------------------------------------
// Flag m — multiline (^ and $ match per-line)
// ---------------------------------------------------------------------------

func TestRegExpFlagM(t *testing.T) {
	Expect(t, `
		var text = "line1\nline2\nline3";

		// without m, ^ only at string start
		assert.sameValue(/^line2/.test(text), false);
		// with m, ^ at each line start
		assert.sameValue(/^line2/m.test(text), true);

		// without m, $ only at string end
		assert.sameValue(/line2$/.test(text), false);
		// with m, $ at each line end
		assert.sameValue(/line2$/m.test(text), true);

		// full-line anchor match
		assert.sameValue(/^line3$/m.test(text), true);
		assert.sameValue(/^line4$/m.test(text), false);

		// exec with m flag: finds the match not at position 0
		var text2 = "first\nSECOND\nthird";
		var m = /^SECOND$/m.exec(text2);
		assert.sameValue(m[0], "SECOND");
		assert.sameValue(m.index, 6);
	`)
}

// ---------------------------------------------------------------------------
// Flag s — dotAll (. matches newline too)
// ---------------------------------------------------------------------------

func TestRegExpFlagS(t *testing.T) {
	Expect(t, `
		// without s, . does not match newline
		assert.sameValue(/a.b/.test("a\nb"), false);

		// with s, . matches any character including newline
		assert.sameValue(/a.b/s.test("a\nb"), true);
		assert.sameValue(/a.b/s.test("a\rb"), true);

		// dotall with quantifiers
		assert.sameValue(/^a.+b$/s.test("a\n\nb"), true);
		assert.sameValue(/^a.*b$/s.test("ab"), true);

		// exec captures the newline through .
		var m = /a(.+)b/s.exec("a\nfoo\nb");
		assert.sameValue(m[0], "a\nfoo\nb");
		assert.sameValue(m[1], "\nfoo\n");
	`)
}

// ---------------------------------------------------------------------------
// Invalid patterns must throw SyntaxError
// ---------------------------------------------------------------------------

func TestRegExpInvalidPatternSyntaxError(t *testing.T) {
	Expect(t, `
		// unclosed group
		assert.throws(SyntaxError, function(){ new RegExp("("); });
		// unclosed character class
		assert.throws(SyntaxError, function(){ new RegExp("["); });
		// unmatched closing paren
		assert.throws(SyntaxError, function(){ new RegExp(")"); });
		// bare quantifiers with nothing to quantify
		assert.throws(SyntaxError, function(){ new RegExp("*"); });
		assert.throws(SyntaxError, function(){ new RegExp("+"); });
		assert.throws(SyntaxError, function(){ new RegExp("?"); });
	`)
}

// ---------------------------------------------------------------------------
// NOTE: may be unimplemented — String.prototype.match with a RegExp argument
// ---------------------------------------------------------------------------

// NOTE: may be unimplemented
func TestRegExpStringMatch(t *testing.T) {
	Expect(t, `
		// non-global: returns exec-like result with groups and .index
		var m = "hello world".match(/(\w+)\s(\w+)/);
		assert.sameValue(m[0], "hello world");
		assert.sameValue(m[1], "hello");
		assert.sameValue(m[2], "world");
		assert.sameValue(m.index, 0);

		// global flag: returns plain array of all full matches (no groups, no .index)
		var all = "one1two2three3".match(/\d/g);
		assert.sameValue(all.length, 3);
		assert.sameValue(all[0], "1");
		assert.sameValue(all[1], "2");
		assert.sameValue(all[2], "3");

		// no match returns null (both global and non-global)
		assert.sameValue("abc".match(/\d/), null);
		assert.sameValue("abc".match(/\d/g), null);
	`)
}

// ---------------------------------------------------------------------------
// NOTE: may be unimplemented — String.prototype.matchAll with a RegExp argument
// ---------------------------------------------------------------------------

// NOTE: may be unimplemented
func TestRegExpStringMatchAll(t *testing.T) {
	Expect(t, `
		var re = /(\d)/g;
		var iter = "a1b2c3".matchAll(re);

		var r1 = iter.next().value;
		assert.sameValue(r1[0], "1");
		assert.sameValue(r1[1], "1");
		assert.sameValue(r1.index, 1);

		var r2 = iter.next().value;
		assert.sameValue(r2[0], "2");
		assert.sameValue(r2.index, 3);

		var r3 = iter.next().value;
		assert.sameValue(r3[0], "3");
		assert.sameValue(r3.index, 5);

		assert.sameValue(iter.next().done, true);

		// matchAll requires global flag
		assert.throws(TypeError, function(){ "x".matchAll(/a/); });
	`)
}

// ---------------------------------------------------------------------------
// NOTE: may be unimplemented — String.prototype.search with a RegExp argument
// ---------------------------------------------------------------------------

// NOTE: may be unimplemented
func TestRegExpStringSearch(t *testing.T) {
	Expect(t, `
		assert.sameValue("hello world".search(/world/), 6);
		assert.sameValue("hello world".search(/xyz/), -1);
		assert.sameValue("HELLO".search(/hello/i), 0);
		assert.sameValue("abc123".search(/\d+/), 3);

		// search ignores lastIndex — always searches from position 0
		var re = /\d/g;
		re.lastIndex = 3;
		assert.sameValue("a1b2c3".search(re), 1);
	`)
}

// ---------------------------------------------------------------------------
// NOTE: may be unimplemented — String.prototype.replace with a RegExp argument
// ---------------------------------------------------------------------------

// NOTE: may be unimplemented
func TestRegExpStringReplace(t *testing.T) {
	Expect(t, `
		// non-global: replaces first match only
		assert.sameValue("aabbcc".replace(/b+/, "X"), "aaXcc");
		assert.sameValue("a1b2c3".replace(/\d/, "N"), "aNb2c3");

		// global flag: replaces all matches
		assert.sameValue("a1b2c3".replace(/\d/g, "N"), "aNbNcN");

		// $& inserts the matched substring
		assert.sameValue("hello".replace(/ell/, "[$&]"), "h[ell]o");

		// $1 $2 insert capture groups
		assert.sameValue(
			"2024-07-15".replace(/(\d{4})-(\d{2})-(\d{2})/, "$3/$2/$1"),
			"15/07/2024"
		);

		// replacement function receives (match, ...groups, offset, input)
		assert.sameValue(
			"hello".replace(/[aeiou]/g, function(m){ return m.toUpperCase(); }),
			"hEllO"
		);

		// replacement function receives correct offset
		var offsets = [];
		"a1b2".replace(/\d/g, function(m, offset){ offsets.push(offset); return m; });
		assert.sameValue(offsets[0], 1);
		assert.sameValue(offsets[1], 3);
	`)
}

// ---------------------------------------------------------------------------
// NOTE: may be unimplemented — String.prototype.split with a RegExp argument
// ---------------------------------------------------------------------------

// NOTE: may be unimplemented
func TestRegExpStringSplit(t *testing.T) {
	Expect(t, `
		// split on a whitespace pattern
		var parts = "one  two   three".split(/\s+/);
		assert.sameValue(parts.length, 3);
		assert.sameValue(parts[0], "one");
		assert.sameValue(parts[1], "two");
		assert.sameValue(parts[2], "three");

		// split with limit
		var limited = "a1b2c3".split(/\d/, 2);
		assert.sameValue(limited.length, 2);
		assert.sameValue(limited[0], "a");
		assert.sameValue(limited[1], "b");

		// capture groups in the splitter pattern are included in the result
		var withGroups = "a1b2c".split(/(\d)/);
		assert.sameValue(withGroups.length, 5);
		assert.sameValue(withGroups[0], "a");
		assert.sameValue(withGroups[1], "1");
		assert.sameValue(withGroups[2], "b");
		assert.sameValue(withGroups[3], "2");
		assert.sameValue(withGroups[4], "c");
	`)
}

// ---------------------------------------------------------------------------
// NOTE: may be unimplemented — named capture groups (?<name>...)
// ---------------------------------------------------------------------------

// NOTE: may be unimplemented
func TestRegExpNamedCaptures(t *testing.T) {
	t.Skip("named capture groups (?<name>) and match.groups are deferred; see NOTES-divergences.md")
	Expect(t, `
		var re = /(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})/;
		var m = re.exec("2024-07-15");
		assert.sameValue(m[0], "2024-07-15");
		assert.sameValue(m[1], "2024");
		assert.sameValue(m[2], "07");
		assert.sameValue(m[3], "15");

		// named groups are accessible via m.groups
		assert.sameValue(m.groups.year, "2024");
		assert.sameValue(m.groups.month, "07");
		assert.sameValue(m.groups.day, "15");

		// named group references in replace string
		assert.sameValue(
			"2024-07-15".replace(re, "$<day>/$<month>/$<year>"),
			"15/07/2024"
		);
	`)
}
