package harness

import "testing"

// This file contains comprehensive tests for Array.prototype methods and
// Array static methods. Each test function exercises one cohesive area.
// Tests are written against the JavaScript specification; some may reveal
// engine bugs rather than failing due to test errors.

// ---------------------------------------------------------------------------
// Mutating methods: push, pop, shift, unshift
// ---------------------------------------------------------------------------

func TestArrayPushPopShiftUnshift(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3];
		// push returns new length
		assert.sameValue(a.push(4), 4);
		assert.sameValue(a.length, 4);
		assert.sameValue(a[3], 4);
		// push multiple elements at once
		assert.sameValue(a.push(5, 6), 6);
		assert.sameValue(a.length, 6);
		// pop returns removed element and shrinks
		assert.sameValue(a.pop(), 6);
		assert.sameValue(a.length, 5);
		// pop on empty array returns undefined
		var empty = [];
		assert.sameValue(empty.pop(), undefined);
		assert.sameValue(empty.length, 0);
		// shift returns first element
		var b = [10, 20, 30];
		assert.sameValue(b.shift(), 10);
		assert.sameValue(b.length, 2);
		assert.sameValue(b[0], 20);
		// shift on empty array returns undefined
		assert.sameValue([].shift(), undefined);
		// unshift returns new length and prepends in order
		var c = [3, 4];
		assert.sameValue(c.unshift(1, 2), 4);
		assert.sameValue(c[0], 1);
		assert.sameValue(c[1], 2);
		assert.sameValue(c[2], 3);
		assert.sameValue(c.length, 4);
	`)
}

// ---------------------------------------------------------------------------
// slice
// ---------------------------------------------------------------------------

func TestArraySlice(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3, 4, 5];
		// basic with start and end
		assert.sameValue(a.slice(1, 3).join(","), "2,3");
		// no arguments returns a full copy
		assert.sameValue(a.slice().length, 5);
		// only start
		assert.sameValue(a.slice(2).join(","), "3,4,5");
		// negative start counts from end
		assert.sameValue(a.slice(-2).join(","), "4,5");
		// negative end counts from end
		assert.sameValue(a.slice(0, -2).join(","), "1,2,3");
		// both negative
		assert.sameValue(a.slice(-3, -1).join(","), "3,4");
		// out-of-range start yields empty
		assert.sameValue(a.slice(10).length, 0);
		// out-of-range end is clamped to length
		assert.sameValue(a.slice(0, 100).join(","), "1,2,3,4,5");
		// slice is a copy, not a reference
		var b = a.slice();
		b.push(6);
		assert.sameValue(a.length, 5);
		// start >= end yields empty
		assert.sameValue(a.slice(2, 1).length, 0);
		// very negative start is clamped to 0
		assert.sameValue(a.slice(-100).join(","), "1,2,3,4,5");
	`)
}

// ---------------------------------------------------------------------------
// splice
// ---------------------------------------------------------------------------

func TestArraySplice(t *testing.T) {
	Expect(t, `
		// delete elements and return them
		var a = [1, 2, 3, 4, 5];
		var removed = a.splice(1, 2);
		assert.sameValue(removed.join(","), "2,3");
		assert.sameValue(a.join(","), "1,4,5");
		// insert without deleting
		var b = [1, 4, 5];
		b.splice(1, 0, 2, 3);
		assert.sameValue(b.join(","), "1,2,3,4,5");
		// replace elements
		var c = [1, 2, 3, 4, 5];
		var r = c.splice(1, 2, 20, 30);
		assert.sameValue(r.join(","), "2,3");
		assert.sameValue(c.join(","), "1,20,30,4,5");
		// splice from start to end (one arg)
		var d = [1, 2, 3, 4, 5];
		var tail = d.splice(2);
		assert.sameValue(tail.join(","), "3,4,5");
		assert.sameValue(d.join(","), "1,2");
		// negative deleteCount treated as 0 — no deletion
		var e = [1, 2, 3];
		var rem = e.splice(1, -1, 99);
		assert.sameValue(rem.length, 0);
		assert.sameValue(e.join(","), "1,99,2,3");
		// splice at index 0
		var f = [1, 2, 3];
		f.splice(0, 1);
		assert.sameValue(f.join(","), "2,3");
		// returned value is always an array
		assert.sameValue(Array.isArray([1, 2, 3].splice(0, 0)), true);
	`)
}

// ---------------------------------------------------------------------------
// concat
// ---------------------------------------------------------------------------

func TestArrayConcat(t *testing.T) {
	Expect(t, `
		var a = [1, 2];
		// concat another array
		assert.sameValue(a.concat([3, 4]).join(","), "1,2,3,4");
		// concat non-array values
		assert.sameValue(a.concat(3, 4).join(","), "1,2,3,4");
		// concat mixed
		assert.sameValue(a.concat([3], 4, [5]).join(","), "1,2,3,4,5");
		// original is unchanged
		assert.sameValue(a.length, 2);
		// no-argument concat returns a copy
		assert.sameValue(a.concat().join(","), "1,2");
		// nested array is NOT spread (only top-level arrays are)
		assert.sameValue([1].concat([[2, 3]]).length, 2);
		assert.sameValue(Array.isArray([1].concat([[2, 3]])[1]), true);
		// string is treated as a non-array value
		assert.sameValue([1].concat("hello")[1], "hello");
		// null and undefined are appended as-is
		assert.sameValue([1].concat(null)[1], null);
		assert.sameValue([1].concat(undefined)[1], undefined);
		// result is a new array
		var r = a.concat([3]);
		assert.sameValue(r.length, 3);
		assert.sameValue(a.length, 2);
	`)
}

// ---------------------------------------------------------------------------
// join
// ---------------------------------------------------------------------------

func TestArrayJoin(t *testing.T) {
	Expect(t, `
		// default comma separator
		assert.sameValue([1, 2, 3].join(), "1,2,3");
		// custom separator
		assert.sameValue([1, 2, 3].join("-"), "1-2-3");
		// empty-string separator
		assert.sameValue([1, 2, 3].join(""), "123");
		// null elements become empty string
		assert.sameValue([1, null, 3].join(","), "1,,3");
		// undefined elements become empty string
		assert.sameValue([1, undefined, 3].join(","), "1,,3");
		// nested arrays are joined via their own toString
		assert.sameValue([[1, 2], [3, 4]].join(","), "1,2,3,4");
		// single element
		assert.sameValue([42].join(","), "42");
		// empty array
		assert.sameValue([].join(","), "");
		// multi-character separator
		assert.sameValue([1, 2, 3].join(" | "), "1 | 2 | 3");
		// booleans
		assert.sameValue([true, false].join(","), "true,false");
	`)
}

// ---------------------------------------------------------------------------
// indexOf
// ---------------------------------------------------------------------------

func TestArrayIndexOf(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3, 2, 1];
		// first occurrence
		assert.sameValue(a.indexOf(2), 1);
		assert.sameValue(a.indexOf(1), 0);
		// not found
		assert.sameValue(a.indexOf(99), -1);
		// strict equality: string "2" != number 2
		assert.sameValue([1, 2, 3].indexOf("2"), -1);
		// NaN is never found by indexOf (uses strict equality)
		assert.sameValue([NaN].indexOf(NaN), -1);
		// indexOf with fromIndex: skip the first occurrence
		assert.sameValue(a.indexOf(1, 1), 4);
		assert.sameValue(a.indexOf(2, 2), 3);
		// fromIndex beyond length
		assert.sameValue(a.indexOf(1, 10), -1);
		// negative fromIndex counts from end
		assert.sameValue(a.indexOf(1, -2), 4);
		// indexOf undefined
		assert.sameValue([undefined, 1].indexOf(undefined), 0);
		// indexOf null
		assert.sameValue([null, 1].indexOf(null), 0);
	`)
}

// ---------------------------------------------------------------------------
// lastIndexOf
// ---------------------------------------------------------------------------

func TestArrayLastIndexOf(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3, 2, 1];
		// last occurrence
		assert.sameValue(a.lastIndexOf(2), 3);
		assert.sameValue(a.lastIndexOf(1), 4);
		// not found
		assert.sameValue(a.lastIndexOf(99), -1);
		// NaN is never found (strict equality)
		assert.sameValue([NaN].lastIndexOf(NaN), -1);
		// strict equality: string vs number
		assert.sameValue([1, 2, 3].lastIndexOf("2"), -1);
		// fromIndex: search backwards from given position
		assert.sameValue(a.lastIndexOf(2, 2), 1);
		assert.sameValue(a.lastIndexOf(1, 3), 0);
		// negative fromIndex counts from end
		assert.sameValue(a.lastIndexOf(2, -2), 3);
	`)
}

// ---------------------------------------------------------------------------
// includes
// ---------------------------------------------------------------------------

func TestArrayIncludes(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3];
		assert.sameValue(a.includes(2), true);
		assert.sameValue(a.includes(99), false);
		// includes finds NaN via SameValueZero — unlike indexOf
		assert.sameValue([1, NaN, 3].includes(NaN), true);
		assert.sameValue([1, NaN, 3].indexOf(NaN), -1);
		// +0 and -0 are treated as the same by SameValueZero
		assert.sameValue([0].includes(-0), true);
		assert.sameValue([-0].includes(0), true);
		// includes with undefined
		assert.sameValue([undefined].includes(undefined), true);
		assert.sameValue([1, 2].includes(undefined), false);
		// includes with null
		assert.sameValue([null].includes(null), true);
		// fromIndex: skip leading elements
		assert.sameValue([1, 2, 1].includes(1, 1), true);
		// fromIndex beyond length
		assert.sameValue([1, 2].includes(1, 5), false);
		// negative fromIndex counts from end
		assert.sameValue([1, 2, 3].includes(2, -2), true);
	`)
}

// ---------------------------------------------------------------------------
// find / findIndex
// ---------------------------------------------------------------------------

func TestArrayFind(t *testing.T) {
	Expect(t, `
		// finds first element satisfying predicate
		assert.sameValue([1, 2, 3, 4].find(function(x) { return x > 2; }), 3);
		// returns undefined when nothing matches
		assert.sameValue([1, 2, 3].find(function(x) { return x > 10; }), undefined);
		// callback receives (element, index, array)
		var capturedIdx = -1;
		[10, 20, 30].find(function(v, i, arr) {
			if (v === 20) { capturedIdx = i; }
			assert.sameValue(arr.length, 3);
			return false;
		});
		assert.sameValue(capturedIdx, 1);
		// empty array
		assert.sameValue([].find(function() { return true; }), undefined);
		// returns the value, not just truthy
		assert.sameValue([0, false, null, 5].find(function(x) { return x === 0; }), 0);
		// works with arrow function
		assert.sameValue([5, 10, 15].find(function(x) { return x > 7; }), 10);
	`)
}

func TestArrayFindIndex(t *testing.T) {
	Expect(t, `
		assert.sameValue([1, 2, 3, 4].findIndex(function(x) { return x > 2; }), 2);
		// returns -1 when nothing matches
		assert.sameValue([1, 2, 3].findIndex(function(x) { return x > 10; }), -1);
		// empty array
		assert.sameValue([].findIndex(function() { return true; }), -1);
		// callback receives (element, index, array) for every element
		var seenIndices = [];
		[10, 20, 30].findIndex(function(v, i) { seenIndices.push(i); return false; });
		assert.sameValue(seenIndices.join(","), "0,1,2");
		// correct index returned for first match
		assert.sameValue(["a", "b", "c"].findIndex(function(v) { return v === "b"; }), 1);
		// can find index 0
		assert.sameValue([99, 1, 2].findIndex(function(x) { return x === 99; }), 0);
	`)
}

// ---------------------------------------------------------------------------
// findLast / findLastIndex
// ---------------------------------------------------------------------------

func TestArrayFindLast(t *testing.T) {
	Expect(t, `
		// finds last matching element
		assert.sameValue([1, 2, 3, 4].findLast(function(x) { return x % 2 === 0; }), 4);
		// returns undefined when nothing matches
		assert.sameValue([1, 2, 3].findLast(function(x) { return x > 10; }), undefined);
		// empty array
		assert.sameValue([].findLast(function() { return true; }), undefined);
		// callback is called in reverse order
		var order = [];
		[10, 20, 30].findLast(function(v, i) { order.push(i); return false; });
		assert.sameValue(order.join(","), "2,1,0");
		// multiple matches — the last value is returned
		assert.sameValue([1, 2, 1, 2].findLast(function(x) { return x === 2; }), 2);
		// callback receives correct element at correct index
		var capturedPair = null;
		["a", "b", "a"].findLast(function(v, i) {
			if (v === "a") { capturedPair = i + ":" + v; return true; }
			return false;
		});
		assert.sameValue(capturedPair, "2:a");
	`)
}

func TestArrayFindLastIndex(t *testing.T) {
	Expect(t, `
		assert.sameValue([1, 2, 3, 4].findLastIndex(function(x) { return x % 2 === 0; }), 3);
		// returns -1 when nothing matches
		assert.sameValue([1, 2, 3].findLastIndex(function(x) { return x > 10; }), -1);
		// empty array
		assert.sameValue([].findLastIndex(function() { return true; }), -1);
		// last index of duplicate value
		assert.sameValue([1, 2, 1, 2].findLastIndex(function(x) { return x === 2; }), 3);
		assert.sameValue([1, 2, 1, 2].findLastIndex(function(x) { return x === 1; }), 2);
		// single-element match at index 0
		assert.sameValue([99].findLastIndex(function(x) { return x === 99; }), 0);
	`)
}

// ---------------------------------------------------------------------------
// some / every
// ---------------------------------------------------------------------------

func TestArraySome(t *testing.T) {
	Expect(t, `
		// true when at least one element matches
		assert.sameValue([1, 2, 3].some(function(x) { return x > 2; }), true);
		// false when no element matches
		assert.sameValue([1, 2, 3].some(function(x) { return x > 10; }), false);
		// empty array always returns false
		assert.sameValue([].some(function() { return true; }), false);
		// short-circuits on first match
		var count = 0;
		[1, 2, 3, 4].some(function(x) { count++; return x === 2; });
		assert.sameValue(count, 2);
		// callback receives (element, index, array)
		var sawIndex = -1;
		[10, 20, 30].some(function(v, i, arr) {
			if (v === 20) { sawIndex = i; return true; }
			assert.sameValue(arr.length, 3);
			return false;
		});
		assert.sameValue(sawIndex, 1);
	`)
}

func TestArrayEvery(t *testing.T) {
	Expect(t, `
		// true when all elements match
		assert.sameValue([2, 4, 6].every(function(x) { return x % 2 === 0; }), true);
		// false when any element fails
		assert.sameValue([2, 3, 6].every(function(x) { return x % 2 === 0; }), false);
		// empty array always returns true
		assert.sameValue([].every(function() { return false; }), true);
		// short-circuits on first failure
		var count = 0;
		[2, 3, 4, 6].every(function(x) { count++; return x % 2 === 0; });
		assert.sameValue(count, 2);
		// callback receives (element, index, array)
		var lastIndex = -1;
		[10, 20, 30].every(function(v, i, arr) {
			lastIndex = i;
			assert.sameValue(arr.length, 3);
			return true;
		});
		assert.sameValue(lastIndex, 2);
	`)
}

// ---------------------------------------------------------------------------
// map
// ---------------------------------------------------------------------------

func TestArrayMap(t *testing.T) {
	Expect(t, `
		// basic map
		assert.sameValue([1, 2, 3].map(function(x) { return x * 2; }).join(","), "2,4,6");
		// original array is not modified
		var a = [1, 2, 3];
		a.map(function(x) { return x * 10; });
		assert.sameValue(a.join(","), "1,2,3");
		// result has same length
		assert.sameValue([1, 2, 3, 4].map(function(x) { return x; }).length, 4);
		// callback receives (element, index, array)
		var indices = [];
		[10, 20, 30].map(function(v, i, arr) {
			indices.push(i);
			assert.sameValue(arr.length, 3);
			return v;
		});
		assert.sameValue(indices.join(","), "0,1,2");
		// thisArg is forwarded to callback
		var multiplier = { factor: 3 };
		var result = [1, 2, 3].map(function(x) { return x * this.factor; }, multiplier);
		assert.sameValue(result.join(","), "3,6,9");
		// returns a new array
		var orig = [1, 2, 3];
		var mapped = orig.map(function(x) { return x; });
		assert.sameValue(mapped === orig, false);
	`)
}

// ---------------------------------------------------------------------------
// filter
// ---------------------------------------------------------------------------

func TestArrayFilter(t *testing.T) {
	Expect(t, `
		// basic filter
		assert.sameValue([1, 2, 3, 4, 5].filter(function(x) { return x % 2 === 0; }).join(","), "2,4");
		// original unchanged
		var a = [1, 2, 3];
		a.filter(function() { return false; });
		assert.sameValue(a.length, 3);
		// callback receives (element, index, array)
		var indices = [];
		[10, 20, 30].filter(function(v, i, arr) {
			indices.push(i);
			assert.sameValue(arr.length, 3);
			return true;
		});
		assert.sameValue(indices.join(","), "0,1,2");
		// thisArg is forwarded
		var ctx = { min: 2 };
		var res = [1, 2, 3, 4].filter(function(x) { return x >= this.min; }, ctx);
		assert.sameValue(res.join(","), "2,3,4");
		// filter nothing
		assert.sameValue([1, 2, 3].filter(function() { return false; }).length, 0);
		// filter all
		assert.sameValue([1, 2, 3].filter(function() { return true; }).length, 3);
		// result is a new array
		var orig = [1, 2];
		var filtered = orig.filter(function() { return true; });
		assert.sameValue(filtered === orig, false);
	`)
}

// ---------------------------------------------------------------------------
// reduce
// ---------------------------------------------------------------------------

func TestArrayReduce(t *testing.T) {
	Expect(t, `
		// with initial value
		assert.sameValue([1, 2, 3, 4].reduce(function(acc, val) { return acc + val; }, 0), 10);
		// without initial value (first element becomes initial accumulator)
		assert.sameValue([1, 2, 3, 4].reduce(function(acc, val) { return acc + val; }), 10);
		// single element without initial value
		assert.sameValue([42].reduce(function(acc, val) { return acc + val; }), 42);
		// single element with initial value
		assert.sameValue([42].reduce(function(acc, val) { return acc + val; }, 0), 42);
		// empty array with initial value returns init
		assert.sameValue([].reduce(function() { return 0; }, 99), 99);
		// TypeError on empty array without initial value
		assert.throws(TypeError, function() {
			[].reduce(function(a, b) { return a + b; });
		});
		// callback receives (accumulator, element, index, array)
		var indices = [];
		[1, 2, 3].reduce(function(acc, v, i, arr) {
			indices.push(i);
			assert.sameValue(arr.length, 3);
			return acc + v;
		}, 0);
		assert.sameValue(indices.join(","), "0,1,2");
		// string accumulation
		assert.sameValue(["a", "b", "c"].reduce(function(acc, v) { return acc + v; }), "abc");
		// without init, first index passed to callback is 1
		var firstIdx = -1;
		[10, 20, 30].reduce(function(acc, v, i) {
			if (firstIdx < 0) { firstIdx = i; }
			return acc + v;
		});
		assert.sameValue(firstIdx, 1);
	`)
}

// ---------------------------------------------------------------------------
// reduceRight — not in the engine's method list; this test reveals the bug
// ---------------------------------------------------------------------------

func TestArrayReduceRight(t *testing.T) {
	Expect(t, `
		// sum in reverse (commutative, same result)
		assert.sameValue([1, 2, 3, 4].reduceRight(function(acc, val) { return acc + val; }, 0), 10);
		// order matters for non-commutative op
		assert.sameValue(
			[[1, 2], [3, 4], [5, 6]].reduceRight(function(acc, val) { return acc.concat(val); }, []).join(","),
			"5,6,3,4,1,2"
		);
		// without initial value: last element is initial accumulator
		assert.sameValue([1, 2, 3].reduceRight(function(acc, val) { return acc - val; }), 0);
		// single element without init
		assert.sameValue([42].reduceRight(function(acc, val) { return acc + val; }), 42);
		// empty array with init returns init
		assert.sameValue([].reduceRight(function() { return 0; }, 99), 99);
		// TypeError on empty array without init
		assert.throws(TypeError, function() {
			[].reduceRight(function(a, b) { return a + b; });
		});
	`)
}

// ---------------------------------------------------------------------------
// forEach
// ---------------------------------------------------------------------------

func TestArrayForEach(t *testing.T) {
	Expect(t, `
		// forEach always returns undefined
		var result = [1, 2, 3].forEach(function() {});
		assert.sameValue(result, undefined);
		// iterates all elements in order
		var sum = 0;
		[1, 2, 3, 4].forEach(function(x) { sum += x; });
		assert.sameValue(sum, 10);
		// callback receives (element, index, array)
		var items = [];
		[10, 20, 30].forEach(function(v, i, arr) {
			items.push(v + ":" + i);
			assert.sameValue(arr.length, 3);
		});
		assert.sameValue(items.join(","), "10:0,20:1,30:2");
		// thisArg is forwarded
		var ctx = { acc: 0 };
		[1, 2, 3].forEach(function(x) { this.acc += x; }, ctx);
		assert.sameValue(ctx.acc, 6);
		// empty array — callback never called
		var called = false;
		[].forEach(function() { called = true; });
		assert.sameValue(called, false);
	`)
}

// ---------------------------------------------------------------------------
// flat / flatMap
// ---------------------------------------------------------------------------

func TestArrayFlat(t *testing.T) {
	Expect(t, `
		// default depth is 1: flattens one level
		var r1 = [1, [2, 3], [4, [5]]].flat();
		assert.sameValue(r1.length, 5);
		assert.sameValue(r1[0], 1);
		assert.sameValue(Array.isArray(r1[4]), true); // [5], not 5
		// explicit depth 0 — no flattening
		assert.sameValue([[1, 2], [3, 4]].flat(0).length, 2);
		// depth 1 leaves deeper nesting intact
		var f1 = [1, [2, [3]]].flat(1);
		assert.sameValue(f1.length, 3);
		assert.sameValue(Array.isArray(f1[2]), true);
		// depth 2
		var f2 = [1, [2, [3, [4]]]].flat(2);
		assert.sameValue(f2.length, 4);
		assert.sameValue(Array.isArray(f2[3]), true);
		// Infinity flattens all nesting
		assert.sameValue([1, [2, [3, [4, [5]]]]].flat(Infinity).join(","), "1,2,3,4,5");
		assert.sameValue([1, [2, [3, [4, [5]]]]].flat(Infinity).length, 5);
		// empty inner arrays
		assert.sameValue([[], [1], [2, 3]].flat().join(","), "1,2,3");
		// already flat is unaffected
		assert.sameValue([1, 2, 3].flat().join(","), "1,2,3");
		// original is not mutated
		var orig = [1, [2, 3]];
		orig.flat();
		assert.sameValue(orig.length, 2);
	`)
}

func TestArrayFlatMap(t *testing.T) {
	Expect(t, `
		// maps then flattens one level
		assert.sameValue([1, 2, 3].flatMap(function(x) { return [x, x * 2]; }).join(","), "1,2,2,4,3,6");
		// callback returning a non-array scalar is kept as-is
		assert.sameValue([1, 2, 3].flatMap(function(x) { return x * 2; }).join(","), "2,4,6");
		// empty array
		assert.sameValue([].flatMap(function(x) { return [x]; }).length, 0);
		// only flattens one level — nested arrays survive
		var r = [1, 2].flatMap(function(x) { return [[x, x]]; });
		assert.sameValue(r.length, 2);
		assert.sameValue(Array.isArray(r[0]), true);
		// callback receives (element, index, array)
		var indices = [];
		[10, 20, 30].flatMap(function(v, i, arr) {
			indices.push(i);
			assert.sameValue(arr.length, 3);
			return v;
		});
		assert.sameValue(indices.join(","), "0,1,2");
	`)
}

// ---------------------------------------------------------------------------
// sort
// ---------------------------------------------------------------------------

func TestArraySort(t *testing.T) {
	Expect(t, `
		// default sort is lexicographic (string comparison), NOT numeric
		assert.sameValue([1, 10, 2, 20].sort().join(","), "1,10,2,20");
		assert.sameValue([10, 9, 2, 1, 100].sort().join(","), "1,10,100,2,9");
		// strings
		assert.sameValue(["banana", "apple", "cherry"].sort().join(","), "apple,banana,cherry");
		// sort mutates in place
		var a = [3, 1, 2];
		var b = a.sort();
		assert.sameValue(a.join(","), "1,2,3");
		// sort returns the same array object
		assert.sameValue(b === a, true);
		// undefined elements always sort to the end
		var c = [3, undefined, 1, undefined, 2];
		c.sort();
		assert.sameValue(c[3], undefined);
		assert.sameValue(c[4], undefined);
		// empty array
		assert.sameValue([].sort().length, 0);
		// single element
		assert.sameValue([42].sort().join(","), "42");
	`)
}

func TestArraySortComparator(t *testing.T) {
	Expect(t, `
		// numeric ascending
		assert.sameValue([10, 1, 5, 2].sort(function(a, b) { return a - b; }).join(","), "1,2,5,10");
		// numeric descending
		assert.sameValue([1, 5, 10, 2].sort(function(a, b) { return b - a; }).join(","), "10,5,2,1");
		// sort is stable: equal keys retain original relative order
		var items = [
			{ k: 1, v: "a" },
			{ k: 2, v: "b" },
			{ k: 1, v: "c" },
			{ k: 2, v: "d" }
		];
		items.sort(function(a, b) { return a.k - b.k; });
		assert.sameValue(items[0].v, "a");
		assert.sameValue(items[1].v, "c");
		assert.sameValue(items[2].v, "b");
		assert.sameValue(items[3].v, "d");
		// duplicates
		assert.sameValue([3, 1, 4, 1, 5].sort(function(a, b) { return a - b; }).join(","), "1,1,3,4,5");
		// comparator returning 0 keeps equal elements in place (stability)
		assert.sameValue([2, 1, 3].sort(function(a, b) { return a - b; }).join(","), "1,2,3");
	`)
}

// ---------------------------------------------------------------------------
// reverse
// ---------------------------------------------------------------------------

func TestArrayReverse(t *testing.T) {
	Expect(t, `
		// reverse mutates in place and returns same array
		var a = [1, 2, 3, 4, 5];
		var b = a.reverse();
		assert.sameValue(a.join(","), "5,4,3,2,1");
		assert.sameValue(b === a, true);
		// even-length array
		assert.sameValue([1, 2, 3, 4].reverse().join(","), "4,3,2,1");
		// single element
		assert.sameValue([1].reverse().join(","), "1");
		// empty array
		assert.sameValue([].reverse().length, 0);
		// double reverse is identity
		var c = [1, 2, 3];
		c.reverse().reverse();
		assert.sameValue(c.join(","), "1,2,3");
	`)
}

// ---------------------------------------------------------------------------
// fill
// ---------------------------------------------------------------------------

func TestArrayFill(t *testing.T) {
	Expect(t, `
		// fill entire array
		assert.sameValue([1, 2, 3].fill(0).join(","), "0,0,0");
		// fill from start index to end
		assert.sameValue([1, 2, 3, 4].fill(0, 1).join(","), "1,0,0,0");
		// fill between start and end (exclusive)
		assert.sameValue([1, 2, 3, 4].fill(0, 1, 3).join(","), "1,0,0,4");
		// negative start counts from end
		assert.sameValue([1, 2, 3, 4].fill(0, -2).join(","), "1,2,0,0");
		// negative end counts from end
		assert.sameValue([1, 2, 3, 4].fill(0, 1, -1).join(","), "1,0,0,4");
		// fill mutates in place and returns same array
		var a = [1, 2, 3];
		var b = a.fill(9);
		assert.sameValue(a === b, true);
		assert.sameValue(a.join(","), "9,9,9");
		// fill with object: all slots share the same reference
		var obj = { x: 1 };
		var arr = [0, 0, 0];
		arr.fill(obj);
		assert.sameValue(arr[0] === arr[1], true);
		// out-of-range range: no-op
		assert.sameValue([1, 2, 3].fill(0, 5, 10).join(","), "1,2,3");
	`)
}

// ---------------------------------------------------------------------------
// at
// ---------------------------------------------------------------------------

func TestArrayAt(t *testing.T) {
	Expect(t, `
		var a = [10, 20, 30, 40, 50];
		// positive indices
		assert.sameValue(a.at(0), 10);
		assert.sameValue(a.at(2), 30);
		assert.sameValue(a.at(4), 50);
		// negative indices count from end
		assert.sameValue(a.at(-1), 50);
		assert.sameValue(a.at(-2), 40);
		assert.sameValue(a.at(-5), 10);
		// out of bounds returns undefined
		assert.sameValue(a.at(5), undefined);
		assert.sameValue(a.at(-6), undefined);
		// single-element array
		assert.sameValue([99].at(0), 99);
		assert.sameValue([99].at(-1), 99);
		// empty array
		assert.sameValue([].at(0), undefined);
		assert.sameValue([].at(-1), undefined);
	`)
}

// ---------------------------------------------------------------------------
// keys / values / entries iterators
// ---------------------------------------------------------------------------

func TestArrayKeys(t *testing.T) {
	Expect(t, `
		var a = ["x", "y", "z"];
		// spread collects keys as numbers
		var keys = [...a.keys()];
		assert.sameValue(keys.length, 3);
		assert.sameValue(keys[0], 0);
		assert.sameValue(keys[1], 1);
		assert.sameValue(keys[2], 2);
		// empty array
		assert.sameValue([...[].keys()].length, 0);
		// iterator protocol: next() returns {value, done}
		var it = a.keys();
		assert.sameValue(it.next().value, 0);
		assert.sameValue(it.next().value, 1);
		assert.sameValue(it.next().done, false);
		it.next(); // consume index 2
		assert.sameValue(it.next().done, true);
		// for-of over keys
		var sum = 0;
		for (var k of [10, 20, 30].keys()) { sum += k; }
		assert.sameValue(sum, 3); // 0+1+2
	`)
}

func TestArrayValues(t *testing.T) {
	Expect(t, `
		var a = [10, 20, 30];
		var vals = [...a.values()];
		assert.sameValue(vals.length, 3);
		assert.sameValue(vals[0], 10);
		assert.sameValue(vals[2], 30);
		// Symbol.iterator is the same as values
		var byIter = [...a[Symbol.iterator]()];
		assert.sameValue(byIter.join(","), "10,20,30");
		// for-of uses the values iterator implicitly
		var sum = 0;
		for (var v of a) { sum += v; }
		assert.sameValue(sum, 60);
		// iterator protocol
		var it = a.values();
		assert.sameValue(it.next().value, 10);
		assert.sameValue(it.next().done, false);
		it.next();
		assert.sameValue(it.next().done, true);
	`)
}

func TestArrayEntries(t *testing.T) {
	Expect(t, `
		var a = ["x", "y", "z"];
		var entries = [...a.entries()];
		assert.sameValue(entries.length, 3);
		// each entry is [index, value]
		assert.sameValue(entries[0][0], 0);
		assert.sameValue(entries[0][1], "x");
		assert.sameValue(entries[2][0], 2);
		assert.sameValue(entries[2][1], "z");
		// destructuring in for-of
		var out = [];
		for (var e of a.entries()) {
			out.push(e[0] + ":" + e[1]);
		}
		assert.sameValue(out.join(","), "0:x,1:y,2:z");
		// empty array
		assert.sameValue([...([].entries())].length, 0);
	`)
}

// ---------------------------------------------------------------------------
// Array.from
// ---------------------------------------------------------------------------

func TestArrayFrom(t *testing.T) {
	Expect(t, `
		// from a string — each code unit is one element
		assert.sameValue(Array.from("abc").join(","), "a,b,c");
		assert.sameValue(Array.from("abc").length, 3);
		// from an existing array (creates a copy)
		var orig = [1, 2, 3];
		var copy = Array.from(orig);
		assert.sameValue(copy.join(","), "1,2,3");
		copy.push(4);
		assert.sameValue(orig.length, 3);
		// from a Set (iterable)
		var s = new Set([1, 2, 2, 3]);
		assert.sameValue(Array.from(s).join(","), "1,2,3");
		// from a Map's keys iterator
		var m = new Map([["a", 1], ["b", 2]]);
		assert.sameValue(Array.from(m.keys()).join(","), "a,b");
		// with mapFn
		assert.sameValue(Array.from([1, 2, 3], function(x) { return x * 2; }).join(","), "2,4,6");
		// mapFn receives (element, index)
		var indices = [];
		Array.from([10, 20, 30], function(v, i) { indices.push(i); return v; });
		assert.sameValue(indices.join(","), "0,1,2");
	`)
}

// TestArrayFromArrayLike tests Array.from with an array-like object (has
// .length and numeric keys, no Symbol.iterator). The spec requires this to
// work; the engine may throw TypeError if it only supports proper iterables.
func TestArrayFromArrayLike(t *testing.T) {
	Expect(t, `
		var like = { 0: "a", 1: "b", 2: "c", length: 3 };
		var arr = Array.from(like);
		assert.sameValue(arr.length, 3);
		assert.sameValue(arr[0], "a");
		assert.sameValue(arr[1], "b");
		assert.sameValue(arr[2], "c");
	`)
}

// ---------------------------------------------------------------------------
// Array.of
// ---------------------------------------------------------------------------

func TestArrayOf(t *testing.T) {
	Expect(t, `
		assert.sameValue(Array.of(1, 2, 3).length, 3);
		assert.sameValue(Array.of(1, 2, 3).join(","), "1,2,3");
		// Array.of(7) creates [7], unlike new Array(7) which creates 7 holes
		assert.sameValue(Array.of(7).length, 1);
		assert.sameValue(Array.of(7)[0], 7);
		// no arguments
		assert.sameValue(Array.of().length, 0);
		// mixed types
		assert.sameValue(Array.of(1, "two", true).length, 3);
		assert.sameValue(Array.of(1, "two", true)[1], "two");
		// result is a proper array
		assert.sameValue(Array.isArray(Array.of(1, 2)), true);
	`)
}

// ---------------------------------------------------------------------------
// Array.isArray
// ---------------------------------------------------------------------------

func TestArrayIsArray(t *testing.T) {
	Expect(t, `
		assert.sameValue(Array.isArray([]), true);
		assert.sameValue(Array.isArray([1, 2, 3]), true);
		assert.sameValue(Array.isArray(new Array(3)), true);
		assert.sameValue(Array.isArray(Array.of(1, 2)), true);
		assert.sameValue(Array.isArray({}), false);
		assert.sameValue(Array.isArray("string"), false);
		assert.sameValue(Array.isArray(42), false);
		assert.sameValue(Array.isArray(null), false);
		assert.sameValue(Array.isArray(undefined), false);
		assert.sameValue(Array.isArray(true), false);
		// Array-like object without isArray flag
		assert.sameValue(Array.isArray({ length: 3, 0: "a" }), false);
	`)
}

// ---------------------------------------------------------------------------
// length property
// ---------------------------------------------------------------------------

func TestArrayLength(t *testing.T) {
	Expect(t, `
		// length reflects element count
		assert.sameValue([].length, 0);
		assert.sameValue([1, 2, 3].length, 3);
		// push/pop update length
		var a = [1, 2];
		a.push(3);
		assert.sameValue(a.length, 3);
		a.pop();
		assert.sameValue(a.length, 2);
		// new Array(n) creates array with given length
		var b = new Array(5);
		assert.sameValue(b.length, 5);
		// setting length truncates
		var c = [1, 2, 3, 4, 5];
		c.length = 3;
		assert.sameValue(c.length, 3);
		assert.sameValue(c[3], undefined);
		assert.sameValue(c[4], undefined);
		// join after truncation only sees remaining elements
		assert.sameValue(c.join(","), "1,2,3");
	`)
}

// ---------------------------------------------------------------------------
// spread operator
// ---------------------------------------------------------------------------

func TestArraySpread(t *testing.T) {
	Expect(t, `
		// spread into a new array literal
		var a = [1, 2, 3];
		var b = [...a];
		assert.sameValue(b.join(","), "1,2,3");
		// spread creates a copy
		b.push(4);
		assert.sameValue(a.length, 3);
		// concatenation via spread
		var c = [...[1, 2], ...[3, 4]];
		assert.sameValue(c.join(","), "1,2,3,4");
		// spread a string into chars
		var d = [..."abc"];
		assert.sameValue(d.join(","), "a,b,c");
		// spread into a function call
		function sum3(x, y, z) { return x + y + z; }
		assert.sameValue(sum3(...[1, 2, 3]), 6);
		// spread a Set
		var s = new Set([1, 2, 3]);
		assert.sameValue([...s].join(","), "1,2,3");
		// spread inside an array with other elements
		assert.sameValue([0, ...[1, 2], 3].join(","), "0,1,2,3");
	`)
}

// ---------------------------------------------------------------------------
// for-of iteration
// ---------------------------------------------------------------------------

func TestArrayForOf(t *testing.T) {
	Expect(t, `
		// basic iteration
		var sum = 0;
		for (var x of [1, 2, 3, 4]) { sum += x; }
		assert.sameValue(sum, 10);
		// collects into another array
		var out = [];
		for (var v of ["a", "b", "c"]) { out.push(v); }
		assert.sameValue(out.join(","), "a,b,c");
		// break exits early
		var count = 0;
		for (var n of [1, 2, 3, 4, 5]) {
			if (n === 3) break;
			count++;
		}
		assert.sameValue(count, 2);
		// continue skips
		var evens = [];
		for (var m of [1, 2, 3, 4, 5]) {
			if (m % 2 !== 0) continue;
			evens.push(m);
		}
		assert.sameValue(evens.join(","), "2,4");
		// nested for-of
		var pairs = [];
		for (var p of [[1, 2], [3, 4]]) {
			pairs.push(p[0] + "+" + p[1]);
		}
		assert.sameValue(pairs.join(","), "1+2,3+4");
	`)
}

// ---------------------------------------------------------------------------
// Array constructor
// ---------------------------------------------------------------------------

func TestArrayConstructor(t *testing.T) {
	Expect(t, `
		// no args — empty array
		assert.sameValue(new Array().length, 0);
		// single numeric arg — creates sparse array of that length
		var a = new Array(3);
		assert.sameValue(a.length, 3);
		// multiple args — creates array with those elements
		var b = new Array(1, 2, 3);
		assert.sameValue(b.length, 3);
		assert.sameValue(b.join(","), "1,2,3");
		// Array() without new behaves identically
		var c = Array(3);
		assert.sameValue(c.length, 3);
		// negative length throws RangeError
		assert.throws(RangeError, function() { new Array(-1); });
		// non-integer length throws RangeError
		assert.throws(RangeError, function() { new Array(2.5); });
		// result is a proper array
		assert.sameValue(Array.isArray(new Array()), true);
		assert.sameValue(Array.isArray(new Array(1, 2)), true);
	`)
}
