package harness

import "testing"

// TestStringLength exercises the length property of strings, including that it
// is read-only on primitives.
func TestStringLength(t *testing.T) {
	Expect(t, `
		assert.sameValue("".length, 0);
		assert.sameValue("a".length, 1);
		assert.sameValue("abc".length, 3);
		assert.sameValue("hello world".length, 11);
		assert.sameValue("123".length, 3);
		assert.sameValue("\0".length, 1);
		assert.sameValue("  ".length, 2);
		assert.sameValue("\n\r\t".length, 3);
		assert.sameValue("☃".length, 1);
		assert.sameValue("ABC".length, 3);
		var s = "abc";
		s.length = 99;
		assert.sameValue(s.length, 3);
	`)
}

// TestStringCharAt exercises String.prototype.charAt including out-of-range and
// fractional index coercion.
func TestStringCharAt(t *testing.T) {
	Expect(t, `
		assert.sameValue("abc".charAt(0), "a");
		assert.sameValue("abc".charAt(1), "b");
		assert.sameValue("abc".charAt(2), "c");
		assert.sameValue("abc".charAt(3), "");
		assert.sameValue("abc".charAt(-1), "");
		assert.sameValue("abc".charAt(100), "");
		assert.sameValue("abc".charAt(), "a");
		assert.sameValue("".charAt(0), "");
		assert.sameValue("a".charAt(0), "a");
		assert.sameValue("abc".charAt(0.9), "a");
		assert.sameValue("abc".charAt(1.9), "b");
		assert.sameValue("☃abc".charAt(0), "☃");
		assert.sameValue("☃abc".charAt(1), "a");
	`)
}

// TestStringCharCodeAt exercises String.prototype.charCodeAt including NaN for
// out-of-range positions.
func TestStringCharCodeAt(t *testing.T) {
	Expect(t, `
		assert.sameValue("a".charCodeAt(0), 97);
		assert.sameValue("A".charCodeAt(0), 65);
		assert.sameValue("abc".charCodeAt(0), 97);
		assert.sameValue("abc".charCodeAt(1), 98);
		assert.sameValue("abc".charCodeAt(2), 99);
		assert.sameValue("abc".charCodeAt(3), NaN);
		assert.sameValue("abc".charCodeAt(-1), NaN);
		assert.sameValue("abc".charCodeAt(), 97);
		assert.sameValue("".charCodeAt(0), NaN);
		assert.sameValue("☃".charCodeAt(0), 9731);
		assert.sameValue(" ".charCodeAt(0), 32);
	`)
}

// TestStringCodePointAt exercises String.prototype.codePointAt.
func TestStringCodePointAt(t *testing.T) {
	Expect(t, `
		assert.sameValue("a".codePointAt(0), 97);
		assert.sameValue("A".codePointAt(0), 65);
		assert.sameValue("abc".codePointAt(0), 97);
		assert.sameValue("abc".codePointAt(1), 98);
		assert.sameValue("abc".codePointAt(2), 99);
		assert.sameValue("abc".codePointAt(3), undefined);
		assert.sameValue("abc".codePointAt(-1), undefined);
		assert.sameValue("".codePointAt(0), undefined);
		assert.sameValue("☃".codePointAt(0), 9731);
		assert.sameValue("☃".codePointAt(1), undefined);
		assert.sameValue("abc".codePointAt(0), "abc".charCodeAt(0));
	`)
}

// TestStringAt exercises String.prototype.at including negative index wrapping.
func TestStringAt(t *testing.T) {
	Expect(t, `
		assert.sameValue("abc".at(0), "a");
		assert.sameValue("abc".at(1), "b");
		assert.sameValue("abc".at(2), "c");
		assert.sameValue("abc".at(-1), "c");
		assert.sameValue("abc".at(-2), "b");
		assert.sameValue("abc".at(-3), "a");
		assert.sameValue("abc".at(3), undefined);
		assert.sameValue("abc".at(-4), undefined);
		assert.sameValue("".at(0), undefined);
		assert.sameValue("".at(-1), undefined);
		assert.sameValue("abc".at(0.9), "a");
		assert.sameValue("abc".at(-1.9), "c");
		assert.sameValue("x".at(0), "x");
		assert.sameValue("x".at(-1), "x");
	`)
}

// TestStringIndexOf exercises String.prototype.indexOf with the optional
// position argument and edge cases for empty search strings.
func TestStringIndexOf(t *testing.T) {
	Expect(t, `
		assert.sameValue("abcabc".indexOf("b"), 1);
		assert.sameValue("abcabc".indexOf("b", 2), 4);
		assert.sameValue("abcabc".indexOf("b", 5), -1);
		assert.sameValue("abc".indexOf("x"), -1);
		assert.sameValue("abc".indexOf("b", -10), 1);
		assert.sameValue("abc".indexOf(""), 0);
		assert.sameValue("abc".indexOf("", 0), 0);
		assert.sameValue("abc".indexOf("", 1), 1);
		assert.sameValue("abc".indexOf("", 3), 3);
		assert.sameValue("abc".indexOf("", 10), 3);
		assert.sameValue("abc".indexOf("abc"), 0);
		assert.sameValue("abc".indexOf("abc", 1), -1);
		assert.sameValue("abcabc".indexOf("abc", 1), 3);
		assert.sameValue("abc".indexOf("abcd"), -1);
	`)
}

// TestStringLastIndexOf exercises String.prototype.lastIndexOf with its
// reverse-search position argument.
func TestStringLastIndexOf(t *testing.T) {
	Expect(t, `
		assert.sameValue("abcabc".lastIndexOf("b"), 4);
		assert.sameValue("abcabc".lastIndexOf("b", 3), 1);
		assert.sameValue("abcabc".lastIndexOf("b", 0), -1);
		assert.sameValue("abc".lastIndexOf("x"), -1);
		assert.sameValue("abc".lastIndexOf("a"), 0);
		assert.sameValue("abc".lastIndexOf("c"), 2);
		assert.sameValue("abc".lastIndexOf(""), 3);
		assert.sameValue("abc".lastIndexOf("", 1), 1);
		assert.sameValue("abc".lastIndexOf("", 0), 0);
		assert.sameValue("abcabc".lastIndexOf("abc"), 3);
		assert.sameValue("abcabc".lastIndexOf("abc", 2), 0);
		assert.sameValue("abc".lastIndexOf("abcd"), -1);
	`)
}

// TestStringIncludesStartsEndsWith exercises includes, startsWith, and endsWith
// with their optional position/endPosition arguments.
func TestStringIncludesStartsEndsWith(t *testing.T) {
	Expect(t, `
		assert.sameValue("abc".includes("b"), true);
		assert.sameValue("abc".includes("x"), false);
		assert.sameValue("abc".includes(""), true);
		assert.sameValue("abc".includes("b", 2), false);
		assert.sameValue("abc".includes("c", 2), true);
		assert.sameValue("abc".startsWith("a"), true);
		assert.sameValue("abc".startsWith("b"), false);
		assert.sameValue("abc".startsWith(""), true);
		assert.sameValue("abc".startsWith("abc"), true);
		assert.sameValue("abc".startsWith("abcd"), false);
		assert.sameValue("abc".startsWith("b", 1), true);
		assert.sameValue("abc".startsWith("a", 1), false);
		assert.sameValue("abc".endsWith("c"), true);
		assert.sameValue("abc".endsWith("b"), false);
		assert.sameValue("abc".endsWith(""), true);
		assert.sameValue("abc".endsWith("abc"), true);
		assert.sameValue("abc".endsWith("abcd"), false);
		assert.sameValue("abcdef".endsWith("cd", 4), true);
		assert.sameValue("abcdef".endsWith("ef", 4), false);
		assert.sameValue("abc".endsWith("b", 2), true);
	`)
}

// TestStringSlice exercises String.prototype.slice with positive, negative, and
// out-of-range arguments.
func TestStringSlice(t *testing.T) {
	Expect(t, `
		assert.sameValue("abcdef".slice(0), "abcdef");
		assert.sameValue("abcdef".slice(1), "bcdef");
		assert.sameValue("abcdef".slice(2, 4), "cd");
		assert.sameValue("abcdef".slice(2, 2), "");
		assert.sameValue("abcdef".slice(3, 2), "");
		assert.sameValue("abcdef".slice(-1), "f");
		assert.sameValue("abcdef".slice(-2), "ef");
		assert.sameValue("abcdef".slice(-4, -2), "cd");
		assert.sameValue("abcdef".slice(0, -1), "abcde");
		assert.sameValue("abc".slice(0, 100), "abc");
		assert.sameValue("abc".slice(-100), "abc");
		assert.sameValue("abc".slice(100), "");
		assert.sameValue("abc".slice(1, 2), "b");
		assert.sameValue("".slice(0), "");
	`)
}

// TestStringSubstring exercises String.prototype.substring including argument
// swapping and negative-argument clamping.
func TestStringSubstring(t *testing.T) {
	Expect(t, `
		assert.sameValue("abcdef".substring(0), "abcdef");
		assert.sameValue("abcdef".substring(2), "cdef");
		assert.sameValue("abcdef".substring(2, 4), "cd");
		assert.sameValue("abcdef".substring(4, 2), "cd");
		assert.sameValue("abc".substring(-1, 2), "ab");
		assert.sameValue("abc".substring(0, -1), "");
		assert.sameValue("abc".substring(-5, -1), "");
		assert.sameValue("abc".substring(1, 1), "");
		assert.sameValue("abc".substring(0, 0), "");
		assert.sameValue("abc".substring(0, 100), "abc");
		assert.sameValue("abc".substring(100), "");
		assert.sameValue("abc".substring(2, 0), "ab");
	`)
}

// TestStringSubstr exercises the legacy String.prototype.substr including
// negative start and zero/negative length.
func TestStringSubstr(t *testing.T) {
	Expect(t, `
		assert.sameValue("abcdef".substr(0), "abcdef");
		assert.sameValue("abcdef".substr(2), "cdef");
		assert.sameValue("abcdef".substr(2, 3), "cde");
		assert.sameValue("abcdef".substr(2, 0), "");
		assert.sameValue("abcdef".substr(2, -1), "");
		assert.sameValue("abcdef".substr(-2), "ef");
		assert.sameValue("abcdef".substr(-2, 1), "e");
		assert.sameValue("abcdef".substr(-100), "abcdef");
		assert.sameValue("abc".substr(5), "");
		assert.sameValue("abc".substr(1, 2), "bc");
	`)
}

// TestStringCase exercises toUpperCase and toLowerCase.
func TestStringCase(t *testing.T) {
	Expect(t, `
		assert.sameValue("hello".toUpperCase(), "HELLO");
		assert.sameValue("HELLO".toLowerCase(), "hello");
		assert.sameValue("Hello World".toUpperCase(), "HELLO WORLD");
		assert.sameValue("Hello World".toLowerCase(), "hello world");
		assert.sameValue("".toUpperCase(), "");
		assert.sameValue("".toLowerCase(), "");
		assert.sameValue("abc123!@#".toUpperCase(), "ABC123!@#");
		assert.sameValue("ABC123!@#".toLowerCase(), "abc123!@#");
		assert.sameValue("aBcDeF".toUpperCase(), "ABCDEF");
		assert.sameValue("aBcDeF".toLowerCase(), "abcdef");
	`)
}

// TestStringTrim exercises trim, trimStart, and trimEnd across several
// whitespace character types.
func TestStringTrim(t *testing.T) {
	Expect(t, `
		assert.sameValue("  abc  ".trim(), "abc");
		assert.sameValue("  abc  ".trimStart(), "abc  ");
		assert.sameValue("  abc  ".trimEnd(), "  abc");
		assert.sameValue("\t\n abc \t\n".trim(), "abc");
		assert.sameValue("abc".trim(), "abc");
		assert.sameValue("".trim(), "");
		assert.sameValue("   ".trim(), "");
		assert.sameValue("   ".trimStart(), "");
		assert.sameValue("   ".trimEnd(), "");
		assert.sameValue("\tabc".trimStart(), "abc");
		assert.sameValue("abc\t".trimEnd(), "abc");
		assert.sameValue("\nabc\n".trim(), "abc");
	`)
}

// TestStringPadding exercises padStart and padEnd with default space fill,
// custom single-char fill, multi-char fill (truncation and repetition).
func TestStringPadding(t *testing.T) {
	Expect(t, `
		assert.sameValue("5".padStart(3, "0"), "005");
		assert.sameValue("5".padStart(3), "  5");
		assert.sameValue("abc".padStart(2), "abc");
		assert.sameValue("abc".padStart(3), "abc");
		assert.sameValue("abc".padStart(5, "xy"), "xyabc");
		assert.sameValue("abc".padStart(6, "xy"), "xyxabc");
		assert.sameValue("5".padStart(4, "01"), "0105");
		assert.sameValue("5".padEnd(3, "0"), "500");
		assert.sameValue("5".padEnd(3), "5  ");
		assert.sameValue("abc".padEnd(2), "abc");
		assert.sameValue("abc".padEnd(3), "abc");
		assert.sameValue("abc".padEnd(5, "xy"), "abcxy");
		assert.sameValue("abc".padEnd(6, "xy"), "abcxyx");
		assert.sameValue("5".padEnd(4, "01"), "5010");
	`)
}

// TestStringRepeat exercises String.prototype.repeat including zero count,
// and RangeError for negative count or Infinity.
func TestStringRepeat(t *testing.T) {
	Expect(t, `
		assert.sameValue("ab".repeat(3), "ababab");
		assert.sameValue("ab".repeat(1), "ab");
		assert.sameValue("ab".repeat(0), "");
		assert.sameValue("".repeat(100), "");
		assert.sameValue("abc".repeat(2), "abcabc");
		assert.sameValue("-".repeat(5), "-----");
		assert.sameValue("x".repeat(0), "");
		assert.throws(RangeError, function() { "a".repeat(-1); });
		assert.throws(RangeError, function() { "a".repeat(Infinity); });
		assert.throws(RangeError, function() { "a".repeat(-Infinity); });
	`)
}

// TestStringConcat exercises String.prototype.concat with multiple args and
// non-string coercion.
func TestStringConcat(t *testing.T) {
	Expect(t, `
		assert.sameValue("a".concat("b", "c"), "abc");
		assert.sameValue("a".concat(), "a");
		assert.sameValue("a".concat("b"), "ab");
		assert.sameValue("".concat("abc"), "abc");
		assert.sameValue("hello".concat(" ", "world"), "hello world");
		assert.sameValue("a".concat(1, 2), "a12");
		assert.sameValue("a".concat(true, null, undefined), "atruenullundefined");
		assert.sameValue("".concat(""), "");
		assert.sameValue("abc".concat(""), "abc");
		assert.sameValue("".concat("abc", "def"), "abcdef");
	`)
}

// TestStringSplit exercises String.prototype.split with separator, empty
// separator, no-match, limit, adjacent separators, and empty input.
func TestStringSplit(t *testing.T) {
	Expect(t, `
		var parts = "a,b,c".split(",");
		assert.sameValue(parts.length, 3);
		assert.sameValue(parts[0], "a");
		assert.sameValue(parts[1], "b");
		assert.sameValue(parts[2], "c");
		var chars = "abc".split("");
		assert.sameValue(chars.length, 3);
		assert.sameValue(chars[0], "a");
		assert.sameValue(chars[1], "b");
		assert.sameValue(chars[2], "c");
		assert.sameValue("".split("").length, 0);
		var noMatch = "abc".split("x");
		assert.sameValue(noMatch.length, 1);
		assert.sameValue(noMatch[0], "abc");
		var empty = "".split(",");
		assert.sameValue(empty.length, 1);
		assert.sameValue(empty[0], "");
		var limited = "a,b,c".split(",", 2);
		assert.sameValue(limited.length, 2);
		assert.sameValue(limited[0], "a");
		assert.sameValue(limited[1], "b");
		var adjacent = "a,,b".split(",");
		assert.sameValue(adjacent.length, 3);
		assert.sameValue(adjacent[1], "");
	`)
}

// TestStringReplace exercises String.prototype.replace with string patterns,
// special replacement tokens ($& $$ $' and $`), and a function replacer that
// receives (match, offset, string).
func TestStringReplace(t *testing.T) {
	Expect(t, `
		assert.sameValue("a-b-c".replace("-", "+"), "a+b-c");
		assert.sameValue("a-b-c".replace("-", "$&$&"), "a--b-c");
		assert.sameValue("a-b".replace("-", "$$"), "a$b");
		assert.sameValue("a-b".replace("-", "$'"), "abb");
		assert.sameValue("a-b".replace("-", "$\x60"), "aab");
		assert.sameValue("abc".replace("x", "y"), "abc");
		assert.sameValue("aaa".replace("a", "b"), "baa");
		assert.sameValue("abc".replace("", "X"), "Xabc");
		var fnResult = "a-b-c".replace("-", function(match, offset, str) {
			return "[" + match + "," + offset + "," + str + "]";
		});
		assert.sameValue(fnResult, "a[-,1,a-b-c]b-c");
		var fnResult2 = "hello".replace("ll", function(match, offset, str) {
			return match.toUpperCase();
		});
		assert.sameValue(fnResult2, "heLLo");
		var offsetCheck = "xxbxx".replace("b", function(match, offset, str) {
			return String(offset);
		});
		assert.sameValue(offsetCheck, "xx2xx");
	`)
}

// TestStringReplaceAll exercises String.prototype.replaceAll with string
// patterns, $& token, function replacer, and empty-pattern insertion.
func TestStringReplaceAll(t *testing.T) {
	Expect(t, `
		assert.sameValue("a-b-c".replaceAll("-", "+"), "a+b+c");
		assert.sameValue("aaa".replaceAll("a", "b"), "bbb");
		assert.sameValue("abc".replaceAll("x", "y"), "abc");
		assert.sameValue("abc".replaceAll("", "X"), "XaXbXcX");
		assert.sameValue("a-b-c".replaceAll("-", "$&$&"), "a--b--c");
		assert.sameValue("a-b-c".replaceAll("-", "$$"), "a$b$c");
		assert.sameValue("hello world".replaceAll("o", "0"), "hell0 w0rld");
		assert.sameValue("ababab".replaceAll("ab", "X"), "XXX");
		var count = 0;
		var result = "a-b-c".replaceAll("-", function(match, offset, str) {
			count++;
			return "+";
		});
		assert.sameValue(result, "a+b+c");
		assert.sameValue(count, 2);
	`)
}

// TestStringIndexing exercises bracket-notation character access and verifies
// out-of-range returns undefined.
func TestStringIndexing(t *testing.T) {
	Expect(t, `
		assert.sameValue("abc"[0], "a");
		assert.sameValue("abc"[1], "b");
		assert.sameValue("abc"[2], "c");
		assert.sameValue("abc"[3], undefined);
		assert.sameValue("abc"[-1], undefined);
		assert.sameValue(""[0], undefined);
		assert.sameValue("abc"[100], undefined);
		var s = "hello";
		assert.sameValue(s[0], "h");
		assert.sameValue(s[4], "o");
		assert.sameValue(s[5], undefined);
	`)
}

// TestStringCoercion exercises String() as a coercion function for numbers,
// booleans, null, undefined, and arrays/objects.
func TestStringCoercion(t *testing.T) {
	Expect(t, `
		assert.sameValue(String(123), "123");
		assert.sameValue(String(0), "0");
		assert.sameValue(String(-0), "0");
		assert.sameValue(String(-123), "-123");
		assert.sameValue(String(3.14), "3.14");
		assert.sameValue(String(NaN), "NaN");
		assert.sameValue(String(Infinity), "Infinity");
		assert.sameValue(String(-Infinity), "-Infinity");
		assert.sameValue(String(true), "true");
		assert.sameValue(String(false), "false");
		assert.sameValue(String(null), "null");
		assert.sameValue(String(undefined), "undefined");
		assert.sameValue(String([1, 2, 3]), "1,2,3");
		assert.sameValue(String([]), "");
		assert.sameValue(String({}), "[object Object]");
	`)
}

// TestStringFromCharCode exercises String.fromCharCode.
func TestStringFromCharCode(t *testing.T) {
	Expect(t, `
		assert.sameValue(String.fromCharCode(65), "A");
		assert.sameValue(String.fromCharCode(97), "a");
		assert.sameValue(String.fromCharCode(97, 98, 99), "abc");
		assert.sameValue(String.fromCharCode(72, 101, 108, 108, 111), "Hello");
		assert.sameValue(String.fromCharCode(0), "\0");
		assert.sameValue(String.fromCharCode(48), "0");
		assert.sameValue(String.fromCharCode(32), " ");
		assert.sameValue(String.fromCharCode(), "");
		assert.sameValue(String.fromCharCode(0x2603), "☃");
		assert.sameValue(String.fromCharCode(65, 66, 67), "ABC");
	`)
}

// TestStringFromCodePoint exercises String.fromCodePoint including BMP
// characters, an empty call, and RangeError for non-integer or out-of-range
// code points.
func TestStringFromCodePoint(t *testing.T) {
	Expect(t, `
		assert.sameValue(String.fromCodePoint(65), "A");
		assert.sameValue(String.fromCodePoint(97), "a");
		assert.sameValue(String.fromCodePoint(97, 98, 99), "abc");
		assert.sameValue(String.fromCodePoint(0), "\0");
		assert.sameValue(String.fromCodePoint(9731), "☃");
		assert.sameValue(String.fromCodePoint(), "");
		assert.sameValue(String.fromCodePoint(0x41, 0x42, 0x43), "ABC");
		assert.throws(RangeError, function() { String.fromCodePoint(-1); });
		assert.throws(RangeError, function() { String.fromCodePoint(0x110000); });
		assert.throws(RangeError, function() { String.fromCodePoint(1.5); });
	`)
}

// TestStringComparison exercises lexicographic ordering with <, >, <=, >= and
// confirms that comparison is code-unit-based, not numeric.
func TestStringComparison(t *testing.T) {
	Expect(t, `
		assert.sameValue("a" < "b", true);
		assert.sameValue("b" < "a", false);
		assert.sameValue("a" > "b", false);
		assert.sameValue("b" > "a", true);
		assert.sameValue("" < "a", true);
		assert.sameValue("a" < "aa", true);
		assert.sameValue("10" < "9", true);
		assert.sameValue("A" < "a", true);
		assert.sameValue("Z" < "a", true);
		assert.sameValue("abc" <= "abc", true);
		assert.sameValue("abc" >= "abc", true);
		assert.sameValue("abc" <= "abd", true);
		assert.sameValue("abc" >= "abd", false);
		assert.sameValue("abc" < "abcd", true);
	`)
}

// TestStringImmutability verifies that assigning to a string index in
// non-strict mode silently fails and leaves the original string unchanged.
func TestStringImmutability(t *testing.T) {
	Expect(t, `
		var s = "abc";
		s[0] = "X";
		assert.sameValue(s[0], "a");
		assert.sameValue(s, "abc");
		var s2 = "hello";
		s2[0] = "H";
		assert.sameValue(s2, "hello");
		var s3 = "foo";
		s3[1] = "X";
		s3[2] = "X";
		assert.sameValue(s3, "foo");
		assert.sameValue(typeof s3, "string");
		assert.sameValue(s3 === "foo", true);
	`)
}
