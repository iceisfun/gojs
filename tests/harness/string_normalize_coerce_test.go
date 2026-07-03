package harness

import "testing"

// TestStringNormalize covers String.prototype.normalize (§22.1.3.13): the four
// Unicode normalization forms, the default (NFC), and the RangeError raised for
// an unrecognized form.
func TestStringNormalize(t *testing.T) {
	Expect(t, `
		// U+00E9 (é) is a single code point in NFC and decomposes to e + U+0301
		// (combining acute accent) in NFD.
		var precomposed = "é";
		var decomposed = "é";
		assert.sameValue(precomposed.normalize("NFC"), precomposed);
		assert.sameValue(decomposed.normalize("NFC"), precomposed);
		assert.sameValue(precomposed.normalize("NFD"), decomposed);
		assert.sameValue(decomposed.normalize("NFD"), decomposed);
		// Default form is NFC when no argument is given.
		assert.sameValue(decomposed.normalize(), precomposed);
		// Compatibility forms fold the ligature U+FB01 (ﬁ) to "fi".
		assert.sameValue("ﬁ".normalize("NFKC"), "fi");
		assert.sameValue("ﬁ".normalize("NFKD"), "fi");
		// undefined argument selects NFC (not "undefined").
		assert.sameValue(decomposed.normalize(undefined), precomposed);
	`)
	// An unrecognized normalization form is a RangeError, not a TypeError.
	ExpectError(t, `"x".normalize("NFE")`, "RangeError")
	ExpectError(t, `"x".normalize("nfc")`, "RangeError")
}

// TestStringRequireObjectCoercible verifies that String.prototype methods throw
// a TypeError when called on undefined or null (RequireObjectCoercible), and
// that a Symbol supplied where a string is expected throws a TypeError.
func TestStringRequireObjectCoercible(t *testing.T) {
	ExpectError(t, `String.prototype.indexOf.call(undefined, "x")`, "TypeError")
	ExpectError(t, `String.prototype.indexOf.call(null, "x")`, "TypeError")
	ExpectError(t, `String.prototype.slice.call(undefined)`, "TypeError")
	ExpectError(t, `String.prototype.normalize.call(null)`, "TypeError")
	ExpectError(t, `String.prototype.charAt.call(undefined, 0)`, "TypeError")
	// A Symbol coerced to a string throws a TypeError (via ToString).
	ExpectError(t, `"abc".includes(Symbol())`, "TypeError")
	ExpectError(t, `"abc".indexOf(Symbol())`, "TypeError")
	// includes/startsWith/endsWith reject a RegExp search argument.
	ExpectError(t, `"abc".includes(/b/)`, "TypeError")
	ExpectError(t, `"abc".startsWith(/a/)`, "TypeError")
	ExpectError(t, `"abc".endsWith(/c/)`, "TypeError")
	// toString/valueOf are non-generic.
	ExpectError(t, `String.prototype.toString.call({})`, "TypeError")
	ExpectError(t, `String.prototype.valueOf.call(42)`, "TypeError")
}

// TestStringPositionCoercion checks that position/count arguments are coerced
// with ToIntegerOrInfinity: Infinity clamps to the string bounds, a throwing
// valueOf propagates, and normal calls are unaffected.
func TestStringPositionCoercion(t *testing.T) {
	Expect(t, `
		var s = "The future is cool!";
		// Infinity position clamps past the end -> not found.
		assert.sameValue(s.includes("!", Infinity), false);
		assert.sameValue(s.indexOf("!", Infinity), -1);
		// NaN position on lastIndexOf means "search the whole string".
		assert.sameValue("ABBABAB".lastIndexOf("AB", NaN), 5);
		// substring/slice clamp Infinity to length.
		assert.sameValue("hello".substring(NaN, Infinity), "hello");
		assert.sameValue("hello".slice(-Infinity, Infinity), "hello");
		// Normal calls still work.
		assert.sameValue("abc".indexOf("b"), 1);
		assert.sameValue("hello".slice(1, 3), "el");
		// split with a 0 limit yields an empty array.
		assert.sameValue("a,b,c".split(",", 0).length, 0);
	`)
	// A throwing valueOf on the position argument propagates.
	ExpectError(t, `"abc".indexOf("a", { valueOf: function(){ throw new TypeError("boom"); } })`, "TypeError")
	// A Symbol position throws via ToNumber.
	ExpectError(t, `"abc".charCodeAt(Symbol())`, "TypeError")
}
