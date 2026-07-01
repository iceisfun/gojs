package harness

import "testing"

// TestNumberToStringRadix exercises Number.prototype.toString with non-decimal
// radixes (2, 8, 16, 36) including negative values and the default radix-10 path.
func TestNumberToStringRadix(t *testing.T) {
	Expect(t, `
		// binary (radix 2)
		assert.sameValue((0).toString(2), "0");
		assert.sameValue((1).toString(2), "1");
		assert.sameValue((10).toString(2), "1010");
		assert.sameValue((255).toString(2), "11111111");
		assert.sameValue((1024).toString(2), "10000000000");
		// negative binary
		assert.sameValue((-10).toString(2), "-1010");
		assert.sameValue((-255).toString(2), "-11111111");

		// octal (radix 8)
		assert.sameValue((0).toString(8), "0");
		assert.sameValue((7).toString(8), "7");
		assert.sameValue((8).toString(8), "10");
		assert.sameValue((255).toString(8), "377");
		assert.sameValue((511).toString(8), "777");
		assert.sameValue((-8).toString(8), "-10");

		// hexadecimal (radix 16)
		assert.sameValue((0).toString(16), "0");
		assert.sameValue((10).toString(16), "a");
		assert.sameValue((15).toString(16), "f");
		assert.sameValue((16).toString(16), "10");
		assert.sameValue((255).toString(16), "ff");
		assert.sameValue((256).toString(16), "100");
		assert.sameValue((0xDEAD).toString(16), "dead");
		assert.sameValue((-255).toString(16), "-ff");

		// base 36
		assert.sameValue((0).toString(36), "0");
		assert.sameValue((9).toString(36), "9");
		assert.sameValue((10).toString(36), "a");
		assert.sameValue((35).toString(36), "z");
		assert.sameValue((36).toString(36), "10");
		assert.sameValue((1295).toString(36), "zz");

		// radix 10 default
		assert.sameValue((1234).toString(10), "1234");
		assert.sameValue((1234).toString(), "1234");
	`)
}

// TestNumberToStringRadixFraction exercises Number.prototype.toString with a
// non-decimal radix on non-integer values, where the fractional part must also
// be rendered in the target radix (not left in base 10).
func TestNumberToStringRadixFraction(t *testing.T) {
	Expect(t, `
		// terminating fractions in binary
		assert.sameValue((0.5).toString(2), "0.1");
		assert.sameValue((0.25).toString(2), "0.01");
		assert.sameValue((0.75).toString(2), "0.11");
		assert.sameValue((5.5).toString(2), "101.1");
		assert.sameValue((-5.5).toString(2), "-101.1");
		// terminating fractions in hex/octal
		assert.sameValue((255.5).toString(16), "ff.8");
		assert.sameValue((0.5).toString(16), "0.8");
		assert.sameValue((8.5).toString(8), "10.4");
		// repeating fraction: 0.1 has no finite binary expansion, so the result
		// must be a long radix-2 string beginning "0.000110011...", NOT "0.1".
		assert.sameValue((0.1).toString(2).indexOf("0.0001100110011"), 0);
		assert.notSameValue((0.1).toString(2), "0.1");
	`)
}

// TestNumberToFixed exercises Number.prototype.toFixed.
func TestNumberToFixed(t *testing.T) {
	Expect(t, `
		assert.sameValue((0).toFixed(0), "0");
		assert.sameValue((0).toFixed(2), "0.00");
		assert.sameValue((0).toFixed(5), "0.00000");
		assert.sameValue((1).toFixed(0), "1");
		assert.sameValue((1).toFixed(3), "1.000");
		assert.sameValue((3.14159).toFixed(2), "3.14");
		assert.sameValue((3.14159).toFixed(4), "3.1416");
		assert.sameValue((1234.5678).toFixed(2), "1234.57");
		assert.sameValue((1234.5678).toFixed(0), "1235");
		assert.sameValue((1.5).toFixed(0), "2");
		assert.sameValue((2.5).toFixed(0), "3");
		assert.sameValue((1.23456789).toFixed(7), "1.2345679");
		// negative
		assert.sameValue((-3.14159).toFixed(2), "-3.14");
		assert.sameValue((-1234.5678).toFixed(2), "-1234.57");
		// -0 toFixed produces "0" (no sign)
		assert.sameValue((-0).toFixed(0), "0");
		assert.sameValue((-0).toFixed(2), "0.00");
		// zero decimal places
		assert.sameValue((100).toFixed(0), "100");
		assert.sameValue((100.4).toFixed(0), "100");
		assert.sameValue((100.5).toFixed(0), "101");
	`)
}

// TestNumberToPrecision exercises Number.prototype.toPrecision.
func TestNumberToPrecision(t *testing.T) {
	Expect(t, `
		assert.sameValue((123.456).toPrecision(6), "123.456");
		assert.sameValue((123.456).toPrecision(5), "123.46");
		assert.sameValue((123.456).toPrecision(4), "123.5");
		assert.sameValue((123.456).toPrecision(2), "1.2e+2");
		assert.sameValue((123.456).toPrecision(1), "1e+2");
		assert.sameValue((0.00123).toPrecision(2), "0.0012");
		assert.sameValue((0.00123).toPrecision(3), "0.00123");
		assert.sameValue((1234.5678).toPrecision(7), "1234.568");
		assert.sameValue((5).toPrecision(1), "5");
		assert.sameValue((5).toPrecision(3), "5.00");
		// without argument => same as toString
		assert.sameValue((123.456).toPrecision(), "123.456");
		// very large exponent uses exponential notation
		assert.sameValue((1e20).toPrecision(3), "1.00e+20");
		// very small — exponent < -6 uses exponential notation
		assert.sameValue((0.0000001).toPrecision(1), "1e-7");
		assert.sameValue((0.0000001).toPrecision(3), "1.00e-7");
	`)
}

// TestNumberValueOf exercises Number.prototype.valueOf and basic wrapper behavior.
func TestNumberValueOf(t *testing.T) {
	Expect(t, `
		assert.sameValue((42).valueOf(), 42);
		assert.sameValue((-3.14).valueOf(), -3.14);
		assert.sameValue((0).valueOf(), 0);
		assert.sameValue((Infinity).valueOf(), Infinity);
		assert.sameValue((-Infinity).valueOf(), -Infinity);
		// NaN valueOf
		assert.sameValue(NaN.valueOf(), NaN);
		// Number object (not primitive) — valueOf returns primitive
		var n = new Number(7);
		assert.sameValue(n.valueOf(), 7);
		assert.sameValue(typeof n, "object");
		assert.sameValue(typeof n.valueOf(), "number");
		assert.sameValue(n + 1, 8);
	`)
}

// TestNumberCoercionBasic covers the primary Number() coercion cases.
func TestNumberCoercionBasic(t *testing.T) {
	Expect(t, `
		// numeric strings
		assert.sameValue(Number("42"), 42);
		assert.sameValue(Number("  42  "), 42);
		assert.sameValue(Number("42.5"), 42.5);
		assert.sameValue(Number(".5"), 0.5);
		assert.sameValue(Number("3e2"), 300);
		// empty string and whitespace
		assert.sameValue(Number(""), 0);
		assert.sameValue(Number("   "), 0);
		// non-numeric string => NaN
		assert.sameValue(Number("abc"), NaN);
		assert.sameValue(Number("12px"), NaN);
		assert.sameValue(Number("1.2.3"), NaN);
		// booleans
		assert.sameValue(Number(true), 1);
		assert.sameValue(Number(false), 0);
		// null and undefined
		assert.sameValue(Number(null), 0);
		assert.sameValue(Number(undefined), NaN);
		// special strings
		assert.sameValue(Number("Infinity"), Infinity);
		assert.sameValue(Number("-Infinity"), -Infinity);
		assert.sameValue(Number("+Infinity"), Infinity);
	`)
}

// TestNumberCoercionRadixStrings covers hex, binary, and octal string coercion.
func TestNumberCoercionRadixStrings(t *testing.T) {
	Expect(t, `
		// hex strings
		assert.sameValue(Number("0x1F"), 31);
		assert.sameValue(Number("0xFF"), 255);
		assert.sameValue(Number("0x0"), 0);
		assert.sameValue(Number("0xDEAD"), 57005);
		// binary strings (ES2015+)
		assert.sameValue(Number("0b0"), 0);
		assert.sameValue(Number("0b1"), 1);
		assert.sameValue(Number("0b101"), 5);
		assert.sameValue(Number("0b1111"), 15);
		// octal strings (ES2015+)
		assert.sameValue(Number("0o0"), 0);
		assert.sameValue(Number("0o7"), 7);
		assert.sameValue(Number("0o17"), 15);
		assert.sameValue(Number("0o77"), 63);
	`)
}

// TestNumberCoercionArrays covers array-to-number coercion (via toString).
func TestNumberCoercionArrays(t *testing.T) {
	Expect(t, `
		// [] -> "" -> 0
		assert.sameValue(Number([]), 0);
		// [5] -> "5" -> 5
		assert.sameValue(Number([5]), 5);
		// [1,2] -> "1,2" -> NaN
		assert.sameValue(Number([1, 2]), NaN);
		// [null] -> "" -> 0
		assert.sameValue(Number([null]), 0);
		// [true] -> "true" -> NaN
		assert.sameValue(Number([true]), NaN);
		// [" 3 "] -> " 3 " -> 3
		assert.sameValue(Number([" 3 "]), 3);
	`)
}

// TestNumberIsInteger covers Number.isInteger (no coercion).
func TestNumberIsInteger(t *testing.T) {
	Expect(t, `
		assert.sameValue(Number.isInteger(0), true);
		assert.sameValue(Number.isInteger(1), true);
		assert.sameValue(Number.isInteger(-1), true);
		assert.sameValue(Number.isInteger(1000000), true);
		assert.sameValue(Number.isInteger(5.0), true);
		assert.sameValue(Number.isInteger(5.5), false);
		assert.sameValue(Number.isInteger(0.1), false);
		assert.sameValue(Number.isInteger(NaN), false);
		assert.sameValue(Number.isInteger(Infinity), false);
		assert.sameValue(Number.isInteger(-Infinity), false);
		// does NOT coerce
		assert.sameValue(Number.isInteger("5"), false);
		assert.sameValue(Number.isInteger(true), false);
		assert.sameValue(Number.isInteger(null), false);
		assert.sameValue(Number.isInteger(undefined), false);
		assert.sameValue(Number.isInteger([]), false);
		// MAX_SAFE_INTEGER is an integer
		assert.sameValue(Number.isInteger(Number.MAX_SAFE_INTEGER), true);
		assert.sameValue(Number.isInteger(Number.MIN_SAFE_INTEGER), true);
	`)
}

// TestNumberIsNaN covers Number.isNaN (strict, no coercion).
func TestNumberIsNaN(t *testing.T) {
	Expect(t, `
		assert.sameValue(Number.isNaN(NaN), true);
		assert.sameValue(Number.isNaN(Number.NaN), true);
		assert.sameValue(Number.isNaN(0 / 0), true);
		assert.sameValue(Number.isNaN(Infinity - Infinity), true);
		// does NOT coerce — these are all false
		assert.sameValue(Number.isNaN("NaN"), false);
		assert.sameValue(Number.isNaN(undefined), false);
		assert.sameValue(Number.isNaN(null), false);
		assert.sameValue(Number.isNaN(""), false);
		assert.sameValue(Number.isNaN(true), false);
		assert.sameValue(Number.isNaN({}), false);
		// finite/infinite numbers are not NaN
		assert.sameValue(Number.isNaN(0), false);
		assert.sameValue(Number.isNaN(42), false);
		assert.sameValue(Number.isNaN(Infinity), false);
		assert.sameValue(Number.isNaN(-Infinity), false);
	`)
}

// TestNumberIsFinite covers Number.isFinite (strict, no coercion).
func TestNumberIsFinite(t *testing.T) {
	Expect(t, `
		assert.sameValue(Number.isFinite(0), true);
		assert.sameValue(Number.isFinite(42), true);
		assert.sameValue(Number.isFinite(-42), true);
		assert.sameValue(Number.isFinite(3.14), true);
		assert.sameValue(Number.isFinite(Number.MAX_VALUE), true);
		assert.sameValue(Number.isFinite(Number.MIN_VALUE), true);
		assert.sameValue(Number.isFinite(Infinity), false);
		assert.sameValue(Number.isFinite(-Infinity), false);
		assert.sameValue(Number.isFinite(NaN), false);
		// does NOT coerce
		assert.sameValue(Number.isFinite("42"), false);
		assert.sameValue(Number.isFinite(true), false);
		assert.sameValue(Number.isFinite(null), false);
		assert.sameValue(Number.isFinite(undefined), false);
		assert.sameValue(Number.isFinite([]), false);
	`)
}

// TestNumberIsSafeInteger covers Number.isSafeInteger.
func TestNumberIsSafeInteger(t *testing.T) {
	Expect(t, `
		assert.sameValue(Number.isSafeInteger(0), true);
		assert.sameValue(Number.isSafeInteger(1), true);
		assert.sameValue(Number.isSafeInteger(-1), true);
		assert.sameValue(Number.isSafeInteger(9007199254740991), true);   // MAX_SAFE_INTEGER
		assert.sameValue(Number.isSafeInteger(-9007199254740991), true);  // MIN_SAFE_INTEGER
		assert.sameValue(Number.isSafeInteger(9007199254740992), false);  // one beyond
		assert.sameValue(Number.isSafeInteger(-9007199254740992), false);
		assert.sameValue(Number.isSafeInteger(3.14), false);
		assert.sameValue(Number.isSafeInteger(NaN), false);
		assert.sameValue(Number.isSafeInteger(Infinity), false);
		assert.sameValue(Number.isSafeInteger(-Infinity), false);
		// does NOT coerce
		assert.sameValue(Number.isSafeInteger("5"), false);
		assert.sameValue(Number.isSafeInteger(true), false);
		assert.sameValue(Number.isSafeInteger(null), false);
	`)
}

// TestNumberParseIntParseFloat covers Number.parseInt and Number.parseFloat,
// which are the same functions as global parseInt and parseFloat.
func TestNumberParseIntParseFloat(t *testing.T) {
	Expect(t, `
		// Number.parseInt is the same function as global parseInt
		assert.sameValue(Number.parseInt === parseInt, true);
		assert.sameValue(Number.parseFloat === parseFloat, true);
		// basic integer parsing
		assert.sameValue(Number.parseInt("42"), 42);
		assert.sameValue(Number.parseInt("  42  "), 42);
		assert.sameValue(Number.parseInt("42px"), 42);
		assert.sameValue(Number.parseInt("ff", 16), 255);
		assert.sameValue(Number.parseInt("10", 2), 2);
		assert.sameValue(Number.parseInt("10", 8), 8);
		assert.sameValue(Number.parseInt("z", 36), 35);
		assert.sameValue(Number.parseInt("", 10), NaN);
		assert.sameValue(Number.parseInt("abc", 10), NaN);
		// basic float parsing
		assert.sameValue(Number.parseFloat("3.14"), 3.14);
		assert.sameValue(Number.parseFloat("3.14xyz"), 3.14);
		assert.sameValue(Number.parseFloat("Infinity"), Infinity);
		assert.sameValue(Number.parseFloat(".5"), 0.5);
		assert.sameValue(Number.parseFloat(""), NaN);
		assert.sameValue(Number.parseFloat("3.14e2"), 314);
	`)
}

// TestNumberConstants covers all standard Number.* constants.
func TestNumberConstants(t *testing.T) {
	Expect(t, `
		// MAX_SAFE_INTEGER and MIN_SAFE_INTEGER
		assert.sameValue(Number.MAX_SAFE_INTEGER, 9007199254740991);
		assert.sameValue(Number.MIN_SAFE_INTEGER, -9007199254740991);
		assert.sameValue(Number.MAX_SAFE_INTEGER, Math.pow(2, 53) - 1);
		assert.sameValue(Number.MIN_SAFE_INTEGER, -(Math.pow(2, 53) - 1));

		// EPSILON: smallest difference between 1 and next representable value
		assert.sameValue(Number.EPSILON, 2.220446049250313e-16);
		assert(1 + Number.EPSILON > 1, "1 + EPSILON > 1");
		assert(1 + Number.EPSILON / 2 === 1, "1 + EPSILON/2 === 1");

		// MAX_VALUE and MIN_VALUE
		assert.sameValue(Number.MAX_VALUE, 1.7976931348623157e+308);
		assert.sameValue(Number.MIN_VALUE, 5e-324);
		assert(Number.MAX_VALUE > 0, "MAX_VALUE positive");
		assert(Number.MIN_VALUE > 0, "MIN_VALUE positive");

		// Infinities
		assert.sameValue(Number.POSITIVE_INFINITY, Infinity);
		assert.sameValue(Number.NEGATIVE_INFINITY, -Infinity);
		assert.sameValue(Number.POSITIVE_INFINITY, 1 / 0);
		assert.sameValue(Number.NEGATIVE_INFINITY, -1 / 0);

		// NaN
		assert.sameValue(Number.isNaN(Number.NaN), true);
		assert(Number.NaN !== Number.NaN, "Number.NaN !== Number.NaN");
	`)
}

// TestNumberSpecialValues covers NaN identity, -0 behavior, Object.is,
// large integer formatting, and the floating-point classic 0.1+0.2.
func TestNumberSpecialValues(t *testing.T) {
	Expect(t, `
		// NaN is not equal to itself
		assert.sameValue(NaN !== NaN, true);
		assert.sameValue(NaN === NaN, false);
		assert.sameValue(NaN == NaN, false);

		// -0 and +0 are === but Object.is distinguishes them
		assert.sameValue(-0 === 0, true);
		assert.sameValue(Object.is(-0, 0), false);
		assert.sameValue(Object.is(-0, -0), true);
		assert.sameValue(Object.is(0, 0), true);
		assert.sameValue(Object.is(NaN, NaN), true);

		// toString of -0 is "0", not "-0"
		assert.sameValue((0).toString(), "0");
		assert.sameValue((-0).toString(), "0");

		// large integer switches to exponential notation at 1e21
		assert.sameValue((1e21).toString(), "1e+21");
		assert.sameValue((1e20).toString(), "100000000000000000000");
		assert.sameValue((999999999999999999999).toString(), "1e+21");

		// the 0.1 + 0.2 classic floating-point result
		assert.sameValue(0.1 + 0.2, 0.30000000000000004);
		assert(0.1 + 0.2 !== 0.3, "0.1+0.2 !== 0.3");

		// 1/Infinity and -1/Infinity
		assert.sameValue(1 / Infinity, 0);
		assert.sameValue(Object.is(1 / -Infinity, -0), true);

		// Infinity arithmetic
		assert.sameValue(Infinity + 1, Infinity);
		assert.sameValue(Infinity - Infinity, NaN);
		assert.sameValue(Infinity * -1, -Infinity);
		assert.sameValue(Infinity * 0, NaN);
	`)
}

// TestGlobalParseInt covers the global parseInt function with radix detection,
// trailing junk, leading whitespace, empty string, and explicit radixes.
func TestGlobalParseInt(t *testing.T) {
	Expect(t, `
		// basic decimal
		assert.sameValue(parseInt("42"), 42);
		assert.sameValue(parseInt("-42"), -42);
		assert.sameValue(parseInt("  42  "), 42);
		assert.sameValue(parseInt("42.9"), 42);   // truncates fractional part

		// trailing junk is ignored
		assert.sameValue(parseInt("42px"), 42);
		assert.sameValue(parseInt("42abc"), 42);
		assert.sameValue(parseInt("1e2"), 1);      // stops before 'e' in base 10

		// empty string or no digits => NaN
		assert.sameValue(parseInt(""), NaN);
		assert.sameValue(parseInt("abc"), NaN);
		assert.sameValue(parseInt("  "), NaN);

		// 0x prefix auto-detected as hex
		assert.sameValue(parseInt("0xff"), 255);
		assert.sameValue(parseInt("0xFF"), 255);
		assert.sameValue(parseInt("0x10"), 16);

		// explicit radix 16
		assert.sameValue(parseInt("ff", 16), 255);
		assert.sameValue(parseInt("FF", 16), 255);
		assert.sameValue(parseInt("10", 16), 16);

		// explicit radix 2
		assert.sameValue(parseInt("101", 2), 5);
		assert.sameValue(parseInt("1111", 2), 15);
		assert.sameValue(parseInt("10", 2), 2);

		// explicit radix 8
		assert.sameValue(parseInt("17", 8), 15);
		assert.sameValue(parseInt("10", 8), 8);

		// explicit radix 36
		assert.sameValue(parseInt("z", 36), 35);
		assert.sameValue(parseInt("10", 36), 36);
		assert.sameValue(parseInt("zz", 36), 1295);

		// radix 10 with leading zeros (no longer treated as octal)
		assert.sameValue(parseInt("010", 10), 10);
		assert.sameValue(parseInt("09", 10), 9);

		// negative
		assert.sameValue(parseInt("-10", 2), -2);
		assert.sameValue(parseInt("-ff", 16), -255);
	`)
}

// TestGlobalParseFloat covers the global parseFloat function.
func TestGlobalParseFloat(t *testing.T) {
	Expect(t, `
		assert.sameValue(parseFloat("3.14"), 3.14);
		assert.sameValue(parseFloat("3.14xyz"), 3.14);
		assert.sameValue(parseFloat("  3.14  "), 3.14);
		assert.sameValue(parseFloat(".5"), 0.5);
		assert.sameValue(parseFloat("-.5"), -0.5);
		assert.sameValue(parseFloat("1.5e2"), 150);
		assert.sameValue(parseFloat("3.14e+2"), 314);
		assert.sameValue(parseFloat("3.14e-2"), 0.0314);
		assert.sameValue(parseFloat("Infinity"), Infinity);
		assert.sameValue(parseFloat("-Infinity"), -Infinity);
		assert.sameValue(parseFloat("+Infinity"), Infinity);
		assert.sameValue(parseFloat(""), NaN);
		assert.sameValue(parseFloat("abc"), NaN);
		assert.sameValue(parseFloat("   "), NaN);
		// stops at non-numeric (after decimal point)
		assert.sameValue(parseFloat("3.14.159"), 3.14);
		// integer-valued float
		assert.sameValue(parseFloat("42"), 42);
		assert.sameValue(parseFloat("-0"), -0);
		assert.sameValue(Object.is(parseFloat("-0"), -0), true);
	`)
}

// TestGlobalIsNaNIsFinite covers the COERCING global isNaN and isFinite,
// contrasting them with the non-coercing Number.isNaN / Number.isFinite.
func TestGlobalIsNaNIsFinite(t *testing.T) {
	Expect(t, `
		// isNaN coerces its argument first
		assert.sameValue(isNaN(NaN), true);
		assert.sameValue(isNaN("NaN"), true);      // "NaN" -> NaN
		assert.sameValue(isNaN(undefined), true);   // undefined -> NaN
		assert.sameValue(isNaN({}), true);          // {} -> NaN
		assert.sameValue(isNaN("abc"), true);
		assert.sameValue(isNaN(""), false);          // "" -> 0
		assert.sameValue(isNaN(null), false);        // null -> 0
		assert.sameValue(isNaN("42"), false);
		assert.sameValue(isNaN(true), false);        // true -> 1
		assert.sameValue(isNaN(false), false);       // false -> 0
		assert.sameValue(isNaN(42), false);
		assert.sameValue(isNaN(Infinity), false);    // Infinity is not NaN

		// contrast: Number.isNaN does NOT coerce
		assert.sameValue(Number.isNaN("NaN"), false);
		assert.sameValue(Number.isNaN(undefined), false);

		// isFinite coerces its argument first
		assert.sameValue(isFinite(42), true);
		assert.sameValue(isFinite("42"), true);      // coerces
		assert.sameValue(isFinite(""), true);         // "" -> 0
		assert.sameValue(isFinite(null), true);       // null -> 0
		assert.sameValue(isFinite(false), true);      // false -> 0
		assert.sameValue(isFinite(true), true);       // true -> 1
		assert.sameValue(isFinite(Infinity), false);
		assert.sameValue(isFinite(-Infinity), false);
		assert.sameValue(isFinite(NaN), false);
		assert.sameValue(isFinite(undefined), false); // undefined -> NaN
		assert.sameValue(isFinite("abc"), false);     // "abc" -> NaN

		// contrast: Number.isFinite does NOT coerce
		assert.sameValue(Number.isFinite("42"), false);
		assert.sameValue(Number.isFinite(null), false);
	`)
}

// TestMathRounding exercises Math.abs, Math.floor, Math.ceil, Math.round,
// and Math.trunc with positive, negative, and fractional inputs.
func TestMathRounding(t *testing.T) {
	Expect(t, `
		// abs
		assert.sameValue(Math.abs(0), 0);
		assert.sameValue(Math.abs(-0), 0);
		assert.sameValue(Math.abs(5), 5);
		assert.sameValue(Math.abs(-5), 5);
		assert.sameValue(Math.abs(3.7), 3.7);
		assert.sameValue(Math.abs(-3.7), 3.7);
		assert.sameValue(Math.abs(Infinity), Infinity);
		assert.sameValue(Math.abs(-Infinity), Infinity);
		assert.sameValue(Math.abs(NaN), NaN);

		// floor
		assert.sameValue(Math.floor(0), 0);
		assert.sameValue(Math.floor(3), 3);
		assert.sameValue(Math.floor(3.7), 3);
		assert.sameValue(Math.floor(3.1), 3);
		assert.sameValue(Math.floor(-3.1), -4);
		assert.sameValue(Math.floor(-3.7), -4);
		assert.sameValue(Math.floor(-3), -3);
		assert.sameValue(Math.floor(Infinity), Infinity);
		assert.sameValue(Math.floor(-Infinity), -Infinity);

		// ceil
		assert.sameValue(Math.ceil(0), 0);
		assert.sameValue(Math.ceil(3), 3);
		assert.sameValue(Math.ceil(3.1), 4);
		assert.sameValue(Math.ceil(3.7), 4);
		assert.sameValue(Math.ceil(-3.1), -3);
		assert.sameValue(Math.ceil(-3.7), -3);
		assert.sameValue(Math.ceil(-3), -3);
		assert.sameValue(Math.ceil(Infinity), Infinity);

		// trunc
		assert.sameValue(Math.trunc(0), 0);
		assert.sameValue(Math.trunc(3.9), 3);
		assert.sameValue(Math.trunc(3.1), 3);
		assert.sameValue(Math.trunc(-3.9), -3);
		assert.sameValue(Math.trunc(-3.1), -3);
		assert.sameValue(Math.trunc(Infinity), Infinity);
		assert.sameValue(Math.trunc(-Infinity), -Infinity);
		assert.sameValue(Math.trunc(NaN), NaN);
	`)
}

// TestMathRoundHalfUp verifies the "round half up toward +Infinity" rule
// (i.e. ties break toward the more positive integer).
func TestMathRoundHalfUp(t *testing.T) {
	Expect(t, `
		// ties round toward +Infinity (positive halves round up)
		assert.sameValue(Math.round(0.5), 1);
		assert.sameValue(Math.round(1.5), 2);
		assert.sameValue(Math.round(2.5), 3);
		assert.sameValue(Math.round(3.5), 4);
		// negative halves round toward zero (i.e. toward +Infinity still)
		assert.sameValue(Math.round(-0.5), 0);
		assert.sameValue(Math.round(-1.5), -1);
		assert.sameValue(Math.round(-2.5), -2);
		assert.sameValue(Math.round(-3.5), -3);
		// non-half cases
		assert.sameValue(Math.round(0), 0);
		assert.sameValue(Math.round(0.4), 0);
		assert.sameValue(Math.round(0.6), 1);
		assert.sameValue(Math.round(3.2), 3);
		assert.sameValue(Math.round(3.7), 4);
		assert.sameValue(Math.round(-3.2), -3);
		assert.sameValue(Math.round(-3.7), -4);
		// special values
		assert.sameValue(Math.round(Infinity), Infinity);
		assert.sameValue(Math.round(-Infinity), -Infinity);
		assert.sameValue(Math.round(NaN), NaN);
	`)
}

// TestMathSign exercises Math.sign, including the -0 and NaN edge cases.
func TestMathSign(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.sign(1), 1);
		assert.sameValue(Math.sign(42), 1);
		assert.sameValue(Math.sign(0.001), 1);
		assert.sameValue(Math.sign(Infinity), 1);
		assert.sameValue(Math.sign(-1), -1);
		assert.sameValue(Math.sign(-42), -1);
		assert.sameValue(Math.sign(-0.001), -1);
		assert.sameValue(Math.sign(-Infinity), -1);
		// zero: sign of +0 is +0, sign of -0 is -0
		assert.sameValue(Math.sign(0), 0);
		assert.sameValue(Object.is(Math.sign(0), 0), true);
		assert.sameValue(Object.is(Math.sign(-0), -0), true);
		// NaN: sign of NaN is NaN
		assert.sameValue(Math.sign(NaN), NaN);
	`)
}

// TestMathMinMax covers Math.min and Math.max including empty argument lists
// and NaN propagation.
func TestMathMinMax(t *testing.T) {
	Expect(t, `
		// basic cases
		assert.sameValue(Math.max(1, 5, 3), 5);
		assert.sameValue(Math.min(1, 5, 3), 1);
		assert.sameValue(Math.max(-1, -5, -3), -1);
		assert.sameValue(Math.min(-1, -5, -3), -5);
		assert.sameValue(Math.max(0, -0), 0);
		assert.sameValue(Math.min(0, -0), 0);   // both 0 per spec comparison

		// single argument
		assert.sameValue(Math.max(42), 42);
		assert.sameValue(Math.min(42), 42);

		// no arguments: max() => -Infinity, min() => Infinity
		assert.sameValue(Math.max(), -Infinity);
		assert.sameValue(Math.min(), Infinity);

		// NaN propagates
		assert.sameValue(Math.max(1, NaN), NaN);
		assert.sameValue(Math.min(1, NaN), NaN);
		assert.sameValue(Math.max(NaN, NaN), NaN);
		assert.sameValue(Math.min(NaN, 5, 1), NaN);

		// Infinity handling
		assert.sameValue(Math.max(Infinity, 1000), Infinity);
		assert.sameValue(Math.min(-Infinity, -1000), -Infinity);
		assert.sameValue(Math.max(Infinity, -Infinity), Infinity);
		assert.sameValue(Math.min(Infinity, -Infinity), -Infinity);
	`)
}

// TestMathPow exercises Math.pow with integer, fractional, and special exponents.
func TestMathPow(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.pow(2, 0), 1);
		assert.sameValue(Math.pow(2, 1), 2);
		assert.sameValue(Math.pow(2, 10), 1024);
		assert.sameValue(Math.pow(2, 32), 4294967296);
		assert.sameValue(Math.pow(2, -1), 0.5);
		assert.sameValue(Math.pow(2, -2), 0.25);
		assert.sameValue(Math.pow(10, 3), 1000);
		assert.sameValue(Math.pow(10, -3), 0.001);
		assert.sameValue(Math.pow(9, 0.5), 3);    // sqrt(9)
		assert(Math.abs(Math.pow(8, 1 / 3) - 2) < 1e-10, "cbrt via pow (libm last-ulp; see NOTES-divergences.md)");
		assert.sameValue(Math.pow(1, Infinity), 1);
		assert.sameValue(Math.pow(Infinity, 0), 1);
		assert.sameValue(Math.pow(0, 0), 1);
		assert.sameValue(Math.pow(0, 1), 0);
		assert.sameValue(Math.pow(Infinity, 1), Infinity);
		assert.sameValue(Math.pow(Infinity, -1), 0);
		assert.sameValue(Math.pow(-2, 3), -8);
		assert.sameValue(Math.pow(-2, 2), 4);
		assert.sameValue(Math.pow(NaN, 0), 1);   // any^0 === 1 per spec
		assert.sameValue(Math.pow(NaN, 1), NaN);
	`)
}

// TestMathSqrtCbrt exercises Math.sqrt and Math.cbrt.
func TestMathSqrtCbrt(t *testing.T) {
	Expect(t, `
		// sqrt
		assert.sameValue(Math.sqrt(0), 0);
		assert.sameValue(Math.sqrt(1), 1);
		assert.sameValue(Math.sqrt(4), 2);
		assert.sameValue(Math.sqrt(9), 3);
		assert.sameValue(Math.sqrt(16), 4);
		assert.sameValue(Math.sqrt(25), 5);
		assert.sameValue(Math.sqrt(100), 10);
		assert.sameValue(Math.sqrt(0.25), 0.5);
		assert.sameValue(Math.sqrt(Infinity), Infinity);
		assert.sameValue(Math.sqrt(-1), NaN);   // imaginary => NaN
		assert.sameValue(Math.sqrt(NaN), NaN);
		// sqrt(2) approximate
		assert(Math.abs(Math.sqrt(2) - 1.4142135623730951) < 1e-10, "sqrt(2)");

		// cbrt
		assert.sameValue(Math.cbrt(0), 0);
		assert.sameValue(Math.cbrt(1), 1);
		assert.sameValue(Math.cbrt(8), 2);
		assert.sameValue(Math.cbrt(27), 3);
		assert.sameValue(Math.cbrt(125), 5);
		assert.sameValue(Math.cbrt(-8), -2);
		assert.sameValue(Math.cbrt(-27), -3);
		assert.sameValue(Math.cbrt(Infinity), Infinity);
		assert.sameValue(Math.cbrt(-Infinity), -Infinity);
		assert.sameValue(Math.cbrt(NaN), NaN);
		// cbrt(2) approximate
		assert(Math.abs(Math.cbrt(2) - 1.2599210498948732) < 1e-10, "cbrt(2)");
	`)
}

// TestMathHypot exercises Math.hypot.
func TestMathHypot(t *testing.T) {
	Expect(t, `
		// classic 3-4-5 right triangle
		assert.sameValue(Math.hypot(3, 4), 5);
		// 5-12-13 right triangle
		assert.sameValue(Math.hypot(5, 12), 13);
		// single argument
		assert.sameValue(Math.hypot(3), 3);
		assert.sameValue(Math.hypot(-3), 3);   // abs of argument
		// zero arguments
		assert.sameValue(Math.hypot(), 0);
		// three arguments
		assert(Math.abs(Math.hypot(1, 1, 1) - Math.sqrt(3)) < 1e-10, "hypot(1,1,1)");
		// special values
		assert.sameValue(Math.hypot(Infinity, 0), Infinity);
		assert.sameValue(Math.hypot(-Infinity, 0), Infinity);
		assert.sameValue(Math.hypot(NaN, 0), NaN);
		// negative inputs — hypot squares them so sign does not matter
		assert.sameValue(Math.hypot(-3, -4), 5);
	`)
}

// TestMathConstants verifies Math.PI and Math.E to many significant figures
// using a tolerance comparison, since exact equality with an irrational is not
// meaningful across representations.
func TestMathConstants(t *testing.T) {
	Expect(t, `
		// PI
		assert(Math.abs(Math.PI - 3.141592653589793) < 1e-15, "PI value");
		assert(Math.PI > 3.14, "PI > 3.14");
		assert(Math.PI < 3.15, "PI < 3.15");

		// E
		assert(Math.abs(Math.E - 2.718281828459045) < 1e-15, "E value");
		assert(Math.E > 2.71, "E > 2.71");
		assert(Math.E < 2.72, "E < 2.72");

		// SQRT2
		assert(Math.abs(Math.SQRT2 - 1.4142135623730951) < 1e-15, "SQRT2");
		assert.sameValue(Math.SQRT2, Math.sqrt(2));

		// LN2 and LN10
		assert(Math.abs(Math.LN2 - 0.6931471805599453) < 1e-15, "LN2");
		assert(Math.abs(Math.LN10 - 2.302585092994046) < 1e-14, "LN10");

		// LOG2E and LOG10E
		assert(Math.abs(Math.LOG2E - 1.4426950408889634) < 1e-14, "LOG2E");
		assert(Math.abs(Math.LOG10E - 0.4342944819032518) < 1e-15, "LOG10E");
	`)
}

// TestMathLog exercises Math.log, Math.log2, Math.log10, and Math.exp.
func TestMathLog(t *testing.T) {
	Expect(t, `
		// natural log
		assert.sameValue(Math.log(1), 0);
		assert.sameValue(Math.log(Math.E), 1);
		assert(Math.abs(Math.log(2) - Math.LN2) < 1e-15, "log(2)===LN2");
		assert(Math.abs(Math.log(10) - Math.LN10) < 1e-14, "log(10)===LN10");
		assert.sameValue(Math.log(0), -Infinity);
		assert.sameValue(Math.log(-1), NaN);
		assert.sameValue(Math.log(Infinity), Infinity);
		assert.sameValue(Math.log(NaN), NaN);

		// log2
		assert.sameValue(Math.log2(1), 0);
		assert.sameValue(Math.log2(2), 1);
		assert.sameValue(Math.log2(4), 2);
		assert.sameValue(Math.log2(8), 3);
		assert.sameValue(Math.log2(1024), 10);
		assert.sameValue(Math.log2(0), -Infinity);
		assert.sameValue(Math.log2(-1), NaN);
		assert.sameValue(Math.log2(Infinity), Infinity);

		// log10
		assert.sameValue(Math.log10(1), 0);
		assert.sameValue(Math.log10(10), 1);
		assert.sameValue(Math.log10(100), 2);
		assert.sameValue(Math.log10(1000), 3);
		assert(Math.abs(Math.log10(0.1) - (-1)) < 1e-10, "log10(0.1) (libm last-ulp; see NOTES-divergences.md)");
		assert.sameValue(Math.log10(0), -Infinity);
		assert.sameValue(Math.log10(-1), NaN);

		// exp
		assert.sameValue(Math.exp(0), 1);
		assert(Math.abs(Math.exp(1) - Math.E) < 1e-15, "exp(1)===E");
		assert(Math.abs(Math.exp(-1) - 1 / Math.E) < 1e-15, "exp(-1)===1/E");
		assert.sameValue(Math.exp(Infinity), Infinity);
		assert.sameValue(Math.exp(-Infinity), 0);
		assert.sameValue(Math.exp(NaN), NaN);

		// log and exp are inverses
		assert(Math.abs(Math.log(Math.exp(3)) - 3) < 1e-14, "log(exp(3))===3");
		assert(Math.abs(Math.exp(Math.log(5)) - 5) < 1e-14, "exp(log(5))===5");
	`)
}

// TestMathTrig exercises the basic trigonometric functions (sin, cos, tan, asin,
// acos, atan, atan2) with exact and approximate values.
func TestMathTrig(t *testing.T) {
	Expect(t, `
		// sin
		assert.sameValue(Math.sin(0), 0);
		assert(Math.abs(Math.sin(Math.PI / 6) - 0.5) < 1e-15, "sin(PI/6)==0.5");
		assert(Math.abs(Math.sin(Math.PI / 2) - 1) < 1e-15, "sin(PI/2)==1");
		assert(Math.abs(Math.sin(Math.PI)) < 1e-10, "sin(PI)~=0");
		assert.sameValue(Math.sin(NaN), NaN);
		assert.sameValue(Math.sin(Infinity), NaN);

		// cos
		assert.sameValue(Math.cos(0), 1);
		assert(Math.abs(Math.cos(Math.PI / 3) - 0.5) < 1e-15, "cos(PI/3)==0.5");
		assert.sameValue(Math.cos(Math.PI), -1);
		assert(Math.abs(Math.cos(Math.PI / 2)) < 1e-10, "cos(PI/2)~=0");
		assert.sameValue(Math.cos(NaN), NaN);

		// tan
		assert.sameValue(Math.tan(0), 0);
		assert(Math.abs(Math.tan(Math.PI / 4) - 1) < 1e-15, "tan(PI/4)==1");
		assert(Math.abs(Math.tan(Math.PI)) < 1e-10, "tan(PI)~=0");

		// asin
		assert.sameValue(Math.asin(0), 0);
		assert(Math.abs(Math.asin(1) - Math.PI / 2) < 1e-15, "asin(1)==PI/2");
		assert.sameValue(Math.asin(2), NaN);   // out of domain

		// acos
		assert(Math.abs(Math.acos(1)) < 1e-15, "acos(1)==0");
		assert(Math.abs(Math.acos(0) - Math.PI / 2) < 1e-15, "acos(0)==PI/2");
		assert(Math.abs(Math.acos(-1) - Math.PI) < 1e-15, "acos(-1)==PI");
		assert.sameValue(Math.acos(2), NaN);   // out of domain

		// atan
		assert.sameValue(Math.atan(0), 0);
		assert(Math.abs(Math.atan(1) - Math.PI / 4) < 1e-15, "atan(1)==PI/4");
		assert(Math.abs(Math.atan(Infinity) - Math.PI / 2) < 1e-15, "atan(Inf)==PI/2");

		// atan2
		assert(Math.abs(Math.atan2(1, 1) - Math.PI / 4) < 1e-15, "atan2(1,1)==PI/4");
		assert(Math.abs(Math.atan2(0, 1)) < 1e-15, "atan2(0,1)==0");
		assert(Math.abs(Math.atan2(1, 0) - Math.PI / 2) < 1e-15, "atan2(1,0)==PI/2");
		assert(Math.abs(Math.atan2(0, -1) - Math.PI) < 1e-15, "atan2(0,-1)==PI");
	`)
}

// TestExponentOperator exercises the ** (exponentiation) operator, with special
// attention to right-associativity.
func TestExponentOperator(t *testing.T) {
	Expect(t, `
		// basic cases
		assert.sameValue(2 ** 0, 1);
		assert.sameValue(2 ** 1, 2);
		assert.sameValue(2 ** 10, 1024);
		assert.sameValue(3 ** 3, 27);
		assert.sameValue(10 ** 3, 1000);
		assert.sameValue(2 ** -1, 0.5);
		assert.sameValue(2 ** -2, 0.25);
		assert.sameValue(4 ** 0.5, 2);   // sqrt via **
		assert.sameValue((-2) ** 3, -8);
		assert.sameValue((-2) ** 2, 4);

		// right-associativity: 2**3**2 === 2**(3**2) === 2**9 === 512
		assert.sameValue(2 ** 3 ** 2, 512);

		// special values matching Math.pow
		assert.sameValue(0 ** 0, 1);
		assert.sameValue(Infinity ** 0, 1);
		assert.sameValue(NaN ** 0, 1);
		assert.sameValue(NaN ** 1, NaN);
		assert.sameValue(1 ** Infinity, 1);

		// large powers
		assert.sameValue(2 ** 53, 9007199254740992);
		assert.sameValue(10 ** 20, 1e20);

		// ** operator result is same as Math.pow
		assert.sameValue(3 ** 4, Math.pow(3, 4));
		assert.sameValue(2 ** 0.5, Math.sqrt(2));
	`)
}
