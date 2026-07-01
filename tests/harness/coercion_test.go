package harness

import "testing"

// TestCoercionLooseEquality covers Abstract Equality Comparison (==) with type
// coercions for boolean, string, number, null/undefined, NaN, and objects.
func TestCoercionLooseEquality(t *testing.T) {
	Expect(t, `
		// string-number: string is coerced to number
		assert.sameValue(1 == "1", true);
		// boolean: false -> 0
		assert.sameValue(0 == false, true);
		// empty string to number is 0
		assert.sameValue(0 == "", true);
		assert.sameValue("" == false, true);
		// null/undefined only equal each other
		assert.sameValue(null == undefined, true);
		assert.sameValue(null == 0, false);
		assert.sameValue(undefined == 0, false);
		assert.sameValue(null == false, false);
		assert.sameValue(undefined == false, false);
		// NaN is never equal to anything, including itself
		assert.sameValue(NaN == NaN, false);
		// array ToPrimitive: [1] -> "1" -> 1
		assert.sameValue([1] == 1, true);
		// [0] -> "0" -> 0; false -> 0
		assert.sameValue([0] == false, true);
		// [] -> "" then "" == ""
		assert.sameValue("" == [], true);
		// "0" == false: false -> 0; "0" -> 0
		assert.sameValue("0" == false, true);
		// whitespace-only string becomes 0
		assert.sameValue(" \t\n " == 0, true);
		// [null] and [undefined] both stringify to ""
		assert.sameValue([null] == "", true);
		assert.sameValue([undefined] == "", true);
	`)
}

// TestCoercionStrictEquality verifies that === performs no type coercion, and
// that NaN and +0/-0 are handled per SameValue semantics.
func TestCoercionStrictEquality(t *testing.T) {
	Expect(t, `
		assert.sameValue(1 === "1", false);
		assert.sameValue(1 === 1, true);
		// NaN is not strictly equal to itself
		assert.sameValue(NaN === NaN, false);
		// +0 and -0 are strictly equal
		assert.sameValue(0 === -0, true);
		// null and undefined differ in type
		assert.sameValue(null === undefined, false);
		assert.sameValue(null === null, true);
		assert.sameValue(undefined === undefined, true);
		// no boolean/number coercion under ===
		assert.sameValue(0 === false, false);
		assert.sameValue("" === false, false);
		assert.sameValue("" === 0, false);
	`)
}

// TestCoercionAdditionOperator covers binary + (string concatenation vs
// numeric addition) and unary + (ToNumber).
func TestCoercionAdditionOperator(t *testing.T) {
	Expect(t, `
		assert.sameValue(1 + 2, 3);
		// either operand is string -> concatenation
		assert.sameValue("1" + 2, "12");
		assert.sameValue(1 + "2", "12");
		// arrays ToPrimitive to empty or comma-joined string
		assert.sameValue([] + [], "");
		assert.sameValue([] + {}, "[object Object]");
		assert.sameValue([1,2] + [3,4], "1,23,4");
		// null and undefined under +
		assert.sameValue(1 + null, 1);
		assert.sameValue(1 + undefined, NaN);
		assert.sameValue("a" + null, "anull");
		assert.sameValue("a" + undefined, "aundefined");
		// boolean to number
		assert.sameValue(true + true, 2);
		// unary + calls ToNumber
		assert.sameValue(+[], 0);
		assert.sameValue(+[5], 5);
		assert.sameValue(+[1,2], NaN);
		assert.sameValue(+"", 0);
		assert.sameValue(+"  3  ", 3);
		assert.sameValue(+"0x10", 16);
		assert.sameValue(+true, 1);
		assert.sameValue(+null, 0);
		assert.sameValue(+undefined, NaN);
	`)
}

// TestCoercionSubtractMultiply verifies that -, *, and / always coerce both
// operands to numbers via ToNumeric, regardless of type.
func TestCoercionSubtractMultiply(t *testing.T) {
	Expect(t, `
		assert.sameValue("5" - 2, 3);
		assert.sameValue("5" * "2", 10);
		assert.sameValue("a" - 1, NaN);
		assert.sameValue(true - 1, 0);
		assert.sameValue([] - 0, 0);
		assert.sameValue(false * 5, 0);
		assert.sameValue(null - 1, -1);
		assert.sameValue("10" / "2", 5);
	`)
}

// TestCoercionToPrimitive exercises the valueOf / toString resolution order
// for ordinary objects, and the Symbol.toPrimitive override.
func TestCoercionToPrimitive(t *testing.T) {
	Expect(t, `
		// valueOf returning a primitive wins for numeric contexts
		assert.sameValue(({valueOf: function(){return 42;}}) + 0, 42);
		// toString fallback when valueOf returns a non-primitive
		assert.sameValue(({toString: function(){return "x";}}) + "", "x");
		// valueOf wins over toString for * (ToNumeric)
		assert.sameValue(
			({valueOf: function(){return 10;}, toString: function(){return "y";}}) * 2,
			20
		);
		// Symbol.toPrimitive receives the correct hint string
		var symObj = {};
		symObj[Symbol.toPrimitive] = function(hint) {
			if (hint === "number")  return 100;
			if (hint === "string")  return "sym";
			return "default-val";
		};
		// unary + -> "number" hint
		assert.sameValue(+symObj, 100);
		// String() -> "string" hint
		assert.sameValue(String(symObj), "sym");
		// binary + -> "default" hint
		assert.sameValue(symObj + "", "default-val");
	`)
}

// TestCoercionTruthiness verifies the seven falsy values and representative
// truthy values using Boolean() and !! double-negation.
func TestCoercionTruthiness(t *testing.T) {
	Expect(t, `
		// the seven falsy values
		assert.sameValue(Boolean(false), false);
		assert.sameValue(Boolean(0), false);
		assert.sameValue(Boolean(-0), false);
		assert.sameValue(Boolean(""), false);
		assert.sameValue(Boolean(null), false);
		assert.sameValue(Boolean(undefined), false);
		assert.sameValue(Boolean(NaN), false);
		// non-empty strings, [], {} and functions are truthy
		assert.sameValue(Boolean("0"), true);
		assert.sameValue(Boolean("false"), true);
		assert.sameValue(Boolean([]), true);
		assert.sameValue(Boolean({}), true);
		assert.sameValue(Boolean(function(){}), true);
		assert.sameValue(Boolean(" "), true);
		// !! double-negation mirrors Boolean()
		assert.sameValue(!!false, false);
		assert.sameValue(!!0, false);
		assert.sameValue(!!-0, false);
		assert.sameValue(!!"", false);
		assert.sameValue(!!null, false);
		assert.sameValue(!!undefined, false);
		assert.sameValue(!!NaN, false);
		assert.sameValue(!!"0", true);
		assert.sameValue(!![], true);
		assert.sameValue(!!{}, true);
	`)
}

// TestCoercionLogicalOperators verifies that ||, &&, and ?? return operand
// values (not Booleans) and short-circuit as expected.
func TestCoercionLogicalOperators(t *testing.T) {
	Expect(t, `
		assert.sameValue(0 || "x", "x");
		assert.sameValue("a" && "b", "b");
		assert.sameValue(null ?? "d", "d");
		// 0 is falsy but not null/undefined, so ?? passes it through
		assert.sameValue(0 ?? "d", 0);
		assert.sameValue("" || 0 || "last", "last");
		// truthy LHS short-circuits ||
		assert.sameValue(1 || "x", 1);
		// falsy LHS short-circuits &&
		assert.sameValue(null && "b", null);
		// chained || finds first truthy
		assert.sameValue(false || false || 0 || null || "found", "found");
		// ?? only skips null/undefined; other falsy values pass through
		assert.sameValue(0 ?? "skip", 0);
		assert.sameValue("" ?? "skip", "");
		assert.sameValue(false ?? "skip", false);
	`)
}

// TestCoercionRelational covers <, >, >=, and <= with mixed types, including
// the notorious null-relational quirks and NaN ordering rules.
func TestCoercionRelational(t *testing.T) {
	Expect(t, `
		// both strings: lexicographic comparison
		assert.sameValue("10" < "9", true);
		assert.sameValue("9" > "10", true);
		// mixed string/number: coerce to numeric
		assert.sameValue(10 < 9, false);
		assert.sameValue("10" < 9, false);
		assert.sameValue("2" > 10, false);
		// null quirk: coerced to 0 in relational, but null==0 is still false
		assert.sameValue(null >= 0, true);
		assert.sameValue(null > 0, false);
		assert.sameValue(null < 1, true);
		assert.sameValue(null == 0, false);
		// undefined becomes NaN; all NaN comparisons yield false
		assert.sameValue(undefined < 1, false);
		assert.sameValue(undefined > 1, false);
		assert.sameValue(NaN < 1, false);
		assert.sameValue(NaN > 1, false);
		assert.sameValue(NaN >= 1, false);
		assert.sameValue(NaN <= 1, false);
	`)
}

// TestCoercionExplicitConversions covers String(), Number(), Boolean(), and
// string-concatenation as a shorthand for explicit type conversion.
func TestCoercionExplicitConversions(t *testing.T) {
	Expect(t, `
		// String()
		assert.sameValue(String(123), "123");
		assert.sameValue(String(null), "null");
		assert.sameValue(String(undefined), "undefined");
		assert.sameValue(String([1,2]), "1,2");
		assert.sameValue(String({}), "[object Object]");
		assert.sameValue("" + 123, "123");
		// Number()
		assert.sameValue(Number("42"), 42);
		assert.sameValue(Number(""), 0);
		assert.sameValue(Number("  "), 0);
		assert.sameValue(Number(null), 0);
		assert.sameValue(Number(undefined), NaN);
		assert.sameValue(Number(false), 0);
		assert.sameValue(Number(true), 1);
		assert.sameValue(Number([]), 0);
		assert.sameValue(Number([5]), 5);
		assert.sameValue(Number([1,2]), NaN);
		assert.sameValue(Number("0xff"), 255);
		// Boolean()
		assert.sameValue(Boolean(1), true);
		assert.sameValue(Boolean(0), false);
		// typeof does not throw for undeclared identifiers
		assert.sameValue(typeof 1, "number");
		assert.sameValue(typeof "s", "string");
		assert.sameValue(typeof true, "boolean");
		assert.sameValue(typeof undefined, "undefined");
		assert.sameValue(typeof null, "object");
		assert.sameValue(typeof {}, "object");
		assert.sameValue(typeof [], "object");
		assert.sameValue(typeof undeclaredVariableXYZ, "undefined");
	`)
}
