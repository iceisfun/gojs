package harness

import "testing"

// Math.clz32 counts leading zero bits of ToUint32(x); +0/-0 give 32 and a value
// with the high bit set gives 0 (ECMA-262 §21.3.2.11).
func TestMathClz32Conformance(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.clz32(0), 32, "clz32(0)");
		assert.sameValue(Math.clz32(-0), 32, "clz32(-0)");
		assert.sameValue(Math.clz32(1), 31, "clz32(1)");
		assert.sameValue(Math.clz32(2), 30, "clz32(2)");
		assert.sameValue(Math.clz32(0x80000000), 0, "clz32 high bit");
		assert.sameValue(Math.clz32(0xffffffff), 0, "clz32 all ones");
		assert.sameValue(Math.clz32(NaN), 32, "clz32(NaN) -> ToUint32 -> 0");
		assert.sameValue(Math.clz32(Infinity), 32, "clz32(Infinity) -> ToUint32 -> 0");
		assert.sameValue(Math.clz32(4294967297), 31, "clz32 wraps modulo 2^32");
		assert.sameValue(Math.clz32.length, 1, "length");
		assert.sameValue(Math.clz32.name, "clz32", "name");
	`)
}

// Math.imul performs a 32-bit signed integer multiply of ToUint32(x)*ToUint32(y)
// (ECMA-262 §21.3.2.19).
func TestMathImulConformance(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.imul(3, 4), 12, "imul(3,4)");
		assert.sameValue(Math.imul(-1, 8), -8, "imul(-1,8)");
		assert.sameValue(Math.imul(0xffffffff, 5), -5, "imul wraps to signed");
		assert.sameValue(Math.imul(0x7fffffff, 0x7fffffff), 1, "imul large operands");
		assert.sameValue(Math.imul(NaN, 1), 0, "imul(NaN,1)");
		assert.sameValue(Math.imul(1.9, 1.9), 1, "imul truncates via ToUint32");
		assert.sameValue(Math.imul.length, 2, "length");
		assert.sameValue(Math.imul.name, "imul", "name");
	`)
}

// Math.f16round rounds to IEEE 754-2019 binary16 (ties to even) and back
// (ECMA-262 §21.3.2.16).
func TestMathF16RoundConformance(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.f16round(0), 0, "f16round(+0)");
		assert.sameValue(1 / Math.f16round(-0), -Infinity, "f16round(-0) keeps -0");
		assert.sameValue(Math.f16round(1), 1, "f16round(1)");
		assert.sameValue(Math.f16round(1.337), 1.3369140625, "f16round rounds to binary16");
		assert.sameValue(Math.f16round(Infinity), Infinity, "f16round(Infinity)");
		assert.sameValue(Math.f16round(-Infinity), -Infinity, "f16round(-Infinity)");
		assert.sameValue(Math.f16round(NaN), NaN, "f16round(NaN)");
		assert.sameValue(Math.f16round(65520), Infinity, "f16round overflow to Infinity");
		assert.sameValue(Math.f16round.length, 1, "length");
		assert.sameValue(Math.f16round.name, "f16round", "name");
	`)
}

// Math.round rounds half toward +Infinity but preserves -0 for inputs in the
// range [-0.5, -0] (ECMA-262 §21.3.2.28).
func TestMathRoundNegativeZeroConformance(t *testing.T) {
	Expect(t, `
		assert.sameValue(1 / Math.round(-0.5), -Infinity, "round(-0.5) is -0");
		assert.sameValue(1 / Math.round(-0.4), -Infinity, "round(-0.4) is -0");
		assert.sameValue(1 / Math.round(-0), -Infinity, "round(-0) is -0");
		assert.sameValue(Math.round(0.5), 1, "round(0.5) is 1");
		assert.sameValue(Math.round(-0.6), -1, "round(-0.6) is -1");
		assert.sameValue(Math.round(3.5), 4, "round(3.5)");
		assert.sameValue(Math.round(-3.5), -3, "round(-3.5)");
		assert.sameValue(1 / Math.round(0.1), Infinity, "round(0.1) is +0");
	`)
}

// Math.pow follows the historical ECMAScript rule that a ±Infinity exponent with
// a base of magnitude 1, and any NaN exponent, both yield NaN (ECMA-262 §6.1.6.1.3).
func TestMathPowEdgeCasesConformance(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.pow(1, Infinity), NaN, "pow(1, Infinity)");
		assert.sameValue(Math.pow(1, -Infinity), NaN, "pow(1, -Infinity)");
		assert.sameValue(Math.pow(-1, Infinity), NaN, "pow(-1, Infinity)");
		assert.sameValue(Math.pow(-1, -Infinity), NaN, "pow(-1, -Infinity)");
		assert.sameValue(Math.pow(1, NaN), NaN, "pow(1, NaN)");
		assert.sameValue(Math.pow(2, 10), 1024, "pow(2, 10)");
		assert.sameValue(Math.pow(2, Infinity), Infinity, "pow(2, Infinity)");
		assert.sameValue(Math.pow(5, 0), 1, "pow(x, 0) is 1");
		assert.sameValue(Math.pow(NaN, 0), 1, "pow(NaN, 0) is 1");
	`)
}

// Math.max/min treat +0 as larger than -0 and coerce every argument even after a
// result is otherwise determined (ECMA-262 §21.3.2.24/25).
func TestMathMaxMinZerosConformance(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.max(0, -0), 0, "max(0, -0) is +0");
		assert.sameValue(1 / Math.max(-0, 0), Infinity, "max(-0, 0) is +0");
		assert.sameValue(1 / Math.min(0, -0), -Infinity, "min(0, -0) is -0");
		assert.sameValue(1 / Math.min(-0, 0), -Infinity, "min(-0, 0) is -0");
		var count = 0;
		var probe = { valueOf: function () { count++; return 1; } };
		Math.max(NaN, probe);
		assert.sameValue(count, 1, "max coerces every argument even past a NaN");
		count = 0;
		Math.min(NaN, probe);
		assert.sameValue(count, 1, "min coerces every argument even past a NaN");
	`)
}

// Math.hypot lets ±Infinity take precedence over NaN (ECMA-262 §21.3.2.18).
func TestMathHypotConformance(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.hypot(3, 4), 5, "hypot(3, 4)");
		assert.sameValue(Math.hypot(NaN, Infinity), Infinity, "hypot(NaN, Infinity)");
		assert.sameValue(Math.hypot(Infinity, NaN), Infinity, "hypot(Infinity, NaN)");
		assert.sameValue(Math.hypot(-Infinity, 1), Infinity, "hypot(-Infinity, 1)");
		assert.sameValue(Math.hypot(NaN, 2), NaN, "hypot(NaN, 2)");
		assert.sameValue(1 / Math.hypot(0, -0), Infinity, "hypot(0, -0) is +0");
	`)
}

// The Math constants are non-writable and non-configurable, and Math has a
// non-writable, configurable Symbol.toStringTag of "Math" (ECMA-262 §21.3.1).
func TestMathConstantsDescriptorsConformance(t *testing.T) {
	Expect(t, `
		var names = ["PI", "E", "LN2", "LN10", "LOG2E", "LOG10E", "SQRT2", "SQRT1_2"];
		for (var i = 0; i < names.length; i++) {
			var d = Object.getOwnPropertyDescriptor(Math, names[i]);
			assert.sameValue(d.writable, false, names[i] + " not writable");
			assert.sameValue(d.enumerable, false, names[i] + " not enumerable");
			assert.sameValue(d.configurable, false, names[i] + " not configurable");
		}
		assert.sameValue(Object.prototype.toString.call(Math), "[object Math]", "toStringTag");
		var td = Object.getOwnPropertyDescriptor(Math, Symbol.toStringTag);
		assert.sameValue(td.value, "Math", "toStringTag value");
		assert.sameValue(td.writable, false, "toStringTag not writable");
		assert.sameValue(td.configurable, true, "toStringTag configurable");
	`)
}

// The new Math functions are not constructors.
func TestMathNewFunctionsNotConstructorsConformance(t *testing.T) {
	ExpectError(t, "new Math.clz32(1)", "TypeError")
	ExpectError(t, "new Math.imul(1, 2)", "TypeError")
	ExpectError(t, "new Math.f16round(1)", "TypeError")
}
