package harness

import "testing"

// TestSetMethodsES2025 exercises the ES2025 Set-method family: union,
// intersection, difference, symmetricDifference and the three predicates.
func TestSetMethodsES2025(t *testing.T) {
	Expect(t, `
		function eq(a, b) {
			assert.sameValue(a.size, b.length, "size");
			var i = 0;
			for (var v of a) { assert.sameValue(v, b[i++], "el " + (i-1)); }
		}

		eq(new Set([1,2,3]).union(new Set([3,4])), [1,2,3,4]);
		eq(new Set([1,2,3]).intersection(new Set([2,3,4])), [2,3]);
		eq(new Set([1,2,3]).difference(new Set([2,3,4])), [1]);
		eq(new Set([1,2,3]).symmetricDifference(new Set([2,3,4])), [1,4]);

		assert.sameValue(new Set([1,2]).isSubsetOf(new Set([1,2,3])), true, "subset");
		assert.sameValue(new Set([1,4]).isSubsetOf(new Set([1,2,3])), false, "not subset");
		assert.sameValue(new Set([1,2,3]).isSupersetOf(new Set([1,2])), true, "superset");
		assert.sameValue(new Set([1,2]).isSupersetOf(new Set([1,2,3])), false, "not superset");
		assert.sameValue(new Set([1,2]).isDisjointFrom(new Set([3,4])), true, "disjoint");
		assert.sameValue(new Set([1,2]).isDisjointFrom(new Set([2,3])), false, "not disjoint");

		// Results are real Sets with the correct prototype.
		var u = new Set([1]).union(new Set([2]));
		assert.sameValue(Object.getPrototypeOf(u), Set.prototype, "proto");
		assert.sameValue(u instanceof Set, true, "instanceof");
	`)
}

// TestSetMethodsSetLike verifies the methods operate on set-like records via
// GetSetRecord (size/has/keys), not just real Sets.
func TestSetMethodsSetLike(t *testing.T) {
	Expect(t, `
		function setLike(arr) {
			return {
				size: arr.length,
				has: function(v) { return arr.indexOf(v) !== -1; },
				keys: function() {
					var i = 0;
					return { next: function() {
						return i < arr.length
							? { value: arr[i++], done: false }
							: { value: undefined, done: true };
					}};
				}
			};
		}

		var r = new Set([1,2,3]).union(setLike([3,4]));
		assert.sameValue(r.size, 4, "union size");
		assert.sameValue([...r].join(","), "1,2,3,4", "union order");

		assert.sameValue(new Set([1,2,3]).intersection(setLike([2,3,4])).size, 2, "isect");
		assert.sameValue(new Set([1,2,3]).difference(setLike([2])).size, 2, "diff");
		assert.sameValue(new Set([1,2]).isSubsetOf(setLike([1,2,3])), true, "subset set-like");
		assert.sameValue(new Set([1,2,3]).isSupersetOf(setLike([1,2])), true, "superset set-like");
		assert.sameValue(new Set([1]).isDisjointFrom(setLike([2,3])), true, "disjoint set-like");
	`)
}

// TestSetMethodsValidation checks GetSetRecord coercion/validation errors.
func TestSetMethodsValidation(t *testing.T) {
	// Non-object argument → TypeError.
	ExpectError(t, `new Set([1]).union(42)`, "TypeError")
	// size is NaN (undefined) → TypeError.
	ExpectError(t, `new Set([1]).union({ has: function(){}, keys: function(){} })`, "TypeError")
	// negative size → RangeError.
	ExpectError(t, `new Set([1]).union({ size: -1, has: function(){}, keys: function(){} })`, "RangeError")
	// has not callable → TypeError.
	ExpectError(t, `new Set([1]).union({ size: 1, has: 5, keys: function(){} })`, "TypeError")
	// keys not callable → TypeError.
	ExpectError(t, `new Set([1]).union({ size: 1, has: function(){}, keys: 5 })`, "TypeError")
	// Receiver must be a Set.
	ExpectError(t, `Set.prototype.union.call({}, new Set())`, "TypeError")
}

// TestSetToStringTag verifies Set/Map carry the correct Symbol.toStringTag.
func TestSetToStringTag(t *testing.T) {
	Expect(t, `
		var sd = Object.getOwnPropertyDescriptor(Set.prototype, Symbol.toStringTag);
		assert.sameValue(sd.value, "Set", "Set tag value");
		assert.sameValue(sd.writable, false, "Set tag writable");
		assert.sameValue(sd.enumerable, false, "Set tag enumerable");
		assert.sameValue(sd.configurable, true, "Set tag configurable");

		var md = Object.getOwnPropertyDescriptor(Map.prototype, Symbol.toStringTag);
		assert.sameValue(md.value, "Map", "Map tag value");
	`)
}

// TestCollectionCtorNewTarget verifies Set()/Map() without new throw.
func TestCollectionCtorNewTarget(t *testing.T) {
	ExpectError(t, `Set()`, "TypeError")
	ExpectError(t, `Map()`, "TypeError")
}
