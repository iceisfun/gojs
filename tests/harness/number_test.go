package harness

import "testing"

// TestNumberToExponential exercises Number.prototype.toExponential (§21.1.3.2):
// fixed fractionDigits, the undefined (shortest round-trip) form, the special
// NaN/Infinity/-0 receivers, and round-half-up ("pick the larger n") rounding.
func TestNumberToExponential(t *testing.T) {
	Expect(t, `
		assert.sameValue((123.456).toExponential(0), "1e+2");
		assert.sameValue((123.456).toExponential(2), "1.23e+2");
		assert.sameValue((123.456).toExponential(4), "1.2346e+2");
		assert.sameValue((123.456).toExponential(6), "1.234560e+2");
		assert.sameValue((123.456).toExponential(17), "1.23456000000000003e+2");
		assert.sameValue((123.456).toExponential(20), "1.23456000000000003070e+2");
		assert.sameValue((-123.456).toExponential(3), "-1.235e+2");
		assert.sameValue((0.0001).toExponential(0), "1e-4");
		assert.sameValue((0.0001).toExponential(2), "1.00e-4");
		assert.sameValue((0.9999).toExponential(2), "1.00e+0");
		assert.sameValue((0.9999).toExponential(3), "9.999e-1");

		// Round half up (toward the larger significand), not half-to-even.
		assert.sameValue((25).toExponential(0), "3e+1");
		assert.sameValue((12345).toExponential(3), "1.235e+4");

		// fractionDigits undefined uses as many digits as uniquely identify x.
		assert.sameValue((123.456).toExponential(), "1.23456e+2");
		assert.sameValue((123.456).toExponential(undefined), "1.23456e+2");
		assert.sameValue((100).toExponential(), "1e+2");
		assert.sameValue((1.1e-32).toExponential(), "1.1e-32");
		assert.sameValue((0).toExponential(), "0e+0");

		// Zero / negative zero at various fraction counts.
		assert.sameValue((0).toExponential(0), "0e+0");
		assert.sameValue((0).toExponential(4), "0.0000e+0");
		assert.sameValue((-0).toExponential(4), "0.0000e+0");
		assert.sameValue((3).toExponential(100).length, 105);

		// NaN / Infinity ignore fractionDigits (even out-of-range ones).
		assert.sameValue(NaN.toExponential(Infinity), "NaN");
		assert.sameValue((Infinity).toExponential(1000), "Infinity");
		assert.sameValue((-Infinity).toExponential(1000), "-Infinity");

		// fractionDigits is coerced with ToIntegerOrInfinity.
		assert.sameValue((123.456).toExponential(true), "1.2e+2");
		assert.sameValue((123.456).toExponential("2"), "1.23e+2");
		assert.sameValue((123.456).toExponential(2.9), "1.23e+2");
	`)
}

// TestNumberToExponentialRange checks the RangeError bounds (0..100) and that a
// RangeError is only raised for a finite receiver.
func TestNumberToExponentialRange(t *testing.T) {
	Expect(t, `
		assert.sameValue((3).toExponential(0), "3e+0");
		assert.sameValue(typeof (3).toExponential(100), "string");
	`)
	ExpectError(t, `(3).toExponential(-1)`, "RangeError")
	ExpectError(t, `(3).toExponential(101)`, "RangeError")
	ExpectError(t, `(3).toExponential(Infinity)`, "RangeError")
}

// TestNumberToExponentialThrows checks that a non-number receiver throws a
// TypeError and that an abrupt fractionDigits coercion propagates (a Symbol
// throws TypeError; a poisoned valueOf throws before the range check).
func TestNumberToExponentialThrows(t *testing.T) {
	ExpectError(t, `Number.prototype.toExponential.call({})`, "TypeError")
	ExpectError(t, `Number.prototype.toExponential.call("1")`, "TypeError")
	ExpectError(t, `(1).toExponential(Symbol())`, "TypeError")
	ExpectError(t, `(1).toExponential({ valueOf() { throw new RangeError("boom"); } })`, "RangeError")
	// The abrupt coercion is evaluated before the non-finite short-circuit.
	ExpectError(t, `NaN.toExponential({ valueOf() { throw new TypeError("boom"); } })`, "TypeError")
}

// TestNumberToExponentialProp verifies the property is a non-enumerable, non-
// constructor own method with name "toExponential" and length 1.
func TestNumberToExponentialProp(t *testing.T) {
	Expect(t, `
		assert.sameValue(Number.prototype.hasOwnProperty("toExponential"), true);
		var d = Object.getOwnPropertyDescriptor(Number.prototype, "toExponential");
		assert.sameValue(d.enumerable, false);
		assert.sameValue(d.writable, true);
		assert.sameValue(d.configurable, true);
		assert.sameValue(typeof d.value, "function");
		assert.sameValue(Number.prototype.toExponential.length, 1);
		assert.sameValue(Number.prototype.toExponential.name, "toExponential");
	`)
}

// TestNumberValueProps confirms the Number "value properties" are frozen
// (§21.1.2): non-writable, non-enumerable, non-configurable.
func TestNumberValueProps(t *testing.T) {
	Expect(t, `
		var names = ["MAX_VALUE", "MIN_VALUE", "MAX_SAFE_INTEGER",
			"MIN_SAFE_INTEGER", "EPSILON", "POSITIVE_INFINITY",
			"NEGATIVE_INFINITY", "NaN"];
		for (var k = 0; k < names.length; k++) {
			var d = Object.getOwnPropertyDescriptor(Number, names[k]);
			assert.sameValue(d.writable, false, names[k] + " writable");
			assert.sameValue(d.enumerable, false, names[k] + " enumerable");
			assert.sameValue(d.configurable, false, names[k] + " configurable");
		}
	`)
}

// TestNumberToLocaleString checks that Number.prototype.toLocaleString exists,
// matches toString for the default (no-Intl) case, and rejects a non-number
// receiver.
func TestNumberToLocaleString(t *testing.T) {
	Expect(t, `
		assert.sameValue(Number.prototype.hasOwnProperty("toLocaleString"), true);
		assert.sameValue((1234.5).toLocaleString(), "1234.5");
		assert.sameValue((0).toLocaleString(), "0");
		assert.sameValue((-0).toLocaleString(), "0");
		assert.sameValue(NaN.toLocaleString(), "NaN");
	`)
	ExpectError(t, `Number.prototype.toLocaleString.call({})`, "TypeError")
}

// TestNumberMethodsRejectForeignThis confirms toString/valueOf/toFixed/
// toPrecision are not generic: transferring them to a Date (whose internal
// primitive is a Number) still throws a TypeError.
func TestNumberMethodsRejectForeignThis(t *testing.T) {
	ExpectError(t, `Number.prototype.toString.call(new Date(0))`, "TypeError")
	ExpectError(t, `Number.prototype.valueOf.call(new Date(0))`, "TypeError")
	ExpectError(t, `Number.prototype.toFixed.call(new Date(0))`, "TypeError")
	ExpectError(t, `Number.prototype.toPrecision.call(new Date(0), 3)`, "TypeError")
}

// TestNumberToStringRadixCoercion checks that a poisoned radix argument throws
// before the radix-10 shortcut and before NaN/Infinity are handled (§21.1.3.6).
func TestNumberToStringRadixCoercion(t *testing.T) {
	var poison = `{ valueOf() { throw new RangeError("boom"); } }`
	ExpectError(t, `(0).toString(`+poison+`)`, "RangeError")
	ExpectError(t, `NaN.toString(`+poison+`)`, "RangeError")
	ExpectError(t, `Infinity.toString(`+poison+`)`, "RangeError")
}

// TestNumberFixedPrecisionCoercion checks that toFixed/toPrecision propagate an
// abrupt fractionDigits/precision coercion (a Symbol yields TypeError, not the
// RangeError from the range check).
func TestNumberFixedPrecisionCoercion(t *testing.T) {
	ExpectError(t, `(1).toFixed(Symbol())`, "TypeError")
	ExpectError(t, `(1).toFixed(0n)`, "TypeError")
	ExpectError(t, `(1).toPrecision(Symbol())`, "TypeError")
	ExpectError(t, `(1).toPrecision({ valueOf() { throw new RangeError("boom"); } })`, "RangeError")
}

// TestNumberStringParsing checks StringNumericLiteral coercion: exact "Infinity"
// spellings work, Go's "inf"/"nan" spellings do not, and an overflowing literal
// rounds to Infinity.
func TestNumberStringParsing(t *testing.T) {
	Expect(t, `
		assert.sameValue(Number("Infinity"), Infinity);
		assert.sameValue(Number("+Infinity"), Infinity);
		assert.sameValue(Number("-Infinity"), -Infinity);
		assert.sameValue(Number("  Infinity  "), Infinity);
		assert.sameValue(Number("INFINITY"), NaN);
		assert.sameValue(Number("infinity"), NaN);
		assert.sameValue(Number("inf"), NaN);
		assert.sameValue(Number("nan"), NaN);
		assert.sameValue(Number("NaN"), NaN);
		assert.sameValue(Number("Infinity"), Number.POSITIVE_INFINITY);
		assert.sameValue(Number("1e400"), Infinity);
		assert.sameValue(10e10000, Infinity);
		assert.sameValue(Number("1e-400"), 0);
	`)
}
