package harness

import "testing"

// The Array.prototype callback-iteration methods (map/filter/forEach/every/some/
// reduce/find*/flatMap) are spec-generic: they coerce the receiver with ToObject,
// read length with LengthOfArrayLike, allocate results with ArraySpeciesCreate,
// and visit elements through [[Get]]/[[HasProperty]]. These tests lock in that
// behavior against a regression to the old dense-fast-path implementation.

// map honors the constructor's @@species when allocating the result array.
func TestArrayMapSpecies(t *testing.T) {
	Expect(t, `
		class X extends Array {
			static get [Symbol.species]() { return Array; }
		}
		var x = new X(1, 2, 3);
		var y = x.map(function (v) { return v * 2; });
		assert.sameValue(Array.isArray(y), true, "result is an Array");
		assert.sameValue(y instanceof X, false, "@@species Array, not X");
		assert.sameValue(y.length, 3, "length");
		assert.sameValue(y.join(","), "2,4,6", "values");

		// A @@species that returns the subclass keeps the subclass.
		class Z extends Array {}
		var z = new Z(1, 2, 3).map(function (v) { return v; });
		assert.sameValue(z instanceof Z, true, "default @@species keeps subclass");
	`)
}

// map and filter operate over a plain array-like {length, 0:..} receiver.
func TestArrayMapFilterGenericArrayLike(t *testing.T) {
	Expect(t, `
		var like = { length: 3, 0: "a", 1: "b", 2: "c" };
		var mapped = Array.prototype.map.call(like, function (v, k) { return v + k; });
		assert.sameValue(Array.isArray(mapped), true, "map returns a real Array");
		assert.sameValue(mapped.length, 3, "map length");
		assert.sameValue(mapped.join(","), "a0,b1,c2", "map values");

		var filtered = Array.prototype.filter.call(like, function (v) { return v !== "b"; });
		assert.sameValue(filtered.length, 2, "filter length");
		assert.sameValue(filtered.join(","), "a,c", "filter values");

		// length is taken from the "length" property (ToLength), not real slots.
		var truncated = { length: 2, 0: "x", 1: "y", 2: "z" };
		var t2 = Array.prototype.map.call(truncated, function (v) { return v; });
		assert.sameValue(t2.length, 2, "map respects the length property");
		assert.sameValue(t2.join(","), "x,y", "map ignores index 2 past length");
	`)
}

// forEach and map skip holes (absent indices), visiting them for neither the
// callback nor the result array.
func TestArrayCallbackSkipsHoles(t *testing.T) {
	Expect(t, `
		var seen = [];
		[0, , 2].forEach(function (v, k) { seen.push(k); });
		assert.sameValue(seen.join(","), "0,2", "forEach skips the hole at index 1");

		var m = [0, , 2].map(function (v) { return v * 10; });
		assert.sameValue(m.length, 3, "map preserves length");
		assert.sameValue(m.hasOwnProperty(1), false, "map preserves the hole");
		assert.sameValue(m[0], 0, "index 0");
		assert.sameValue(m[2], 20, "index 2");
	`)
}

// A missing or non-callable callback throws a TypeError before iterating, and
// for map/filter/flatMap before ArraySpeciesCreate.
func TestArrayCallbackNotCallableThrows(t *testing.T) {
	ExpectError(t, `[1, 2, 3].map(undefined);`, "TypeError")
	ExpectError(t, `[1, 2, 3].map(42);`, "TypeError")
	ExpectError(t, `[1, 2, 3].filter("nope");`, "TypeError")
	ExpectError(t, `[1, 2, 3].forEach({});`, "TypeError")
	ExpectError(t, `[1, 2, 3].every(null);`, "TypeError")
	ExpectError(t, `[1, 2, 3].some(1);`, "TypeError")
	ExpectError(t, `[1, 2, 3].flatMap(true);`, "TypeError")
	ExpectError(t, `[1, 2, 3].find(0);`, "TypeError")
	ExpectError(t, `[1, 2, 3].reduce("x");`, "TypeError")
}

// RequireObjectCoercible: a null or undefined receiver throws a TypeError.
func TestArrayCallbackRequireObjectCoercible(t *testing.T) {
	ExpectError(t, `Array.prototype.map.call(null, function () {});`, "TypeError")
	ExpectError(t, `Array.prototype.map.call(undefined, function () {});`, "TypeError")
	ExpectError(t, `Array.prototype.forEach.call(null, function () {});`, "TypeError")
	ExpectError(t, `Array.prototype.filter.call(undefined, function () {});`, "TypeError")
	ExpectError(t, `Array.prototype.reduce.call(null, function () {});`, "TypeError")
	ExpectError(t, `Array.prototype.flatMap.call(undefined, function () {});`, "TypeError")
}

// reduce/reduceRight seed and fold over an array-like receiver, skipping holes,
// and throw on an empty reduction with no initial value.
func TestArrayReduceGeneric(t *testing.T) {
	Expect(t, `
		var like = { length: 4, 0: 1, 1: 2, 3: 4 }; // index 2 is a hole
		var sum = Array.prototype.reduce.call(like, function (a, v) { return a + v; });
		assert.sameValue(sum, 7, "reduce seeds from index 0 and skips the hole");

		var right = Array.prototype.reduceRight.call(like, function (a, v) { return a + "" + v; }, "");
		assert.sameValue(right, "421", "reduceRight folds right-to-left, skipping holes");
	`)
	ExpectError(t, `[].reduce(function (a, v) { return v; });`, "TypeError")
	ExpectError(t, `[ , , ].reduce(function (a, v) { return v; });`, "TypeError")
}

// flatMap flattens one level and is generic over the mapped array-like results.
func TestArrayFlatMapGeneric(t *testing.T) {
	Expect(t, `
		var r = [1, 2, 3].flatMap(function (v) { return [v, v * 10]; });
		assert.sameValue(r.length, 6, "flatMap flattens one level");
		assert.sameValue(r.join(","), "1,10,2,20,3,30", "flatMap values");

		// Non-array results are appended as-is (not flattened).
		var mixed = [1, 2].flatMap(function (v) { return v === 1 ? [v] : v; });
		assert.sameValue(mixed.join(","), "1,2", "flatMap appends non-arrays as-is");
	`)
}
