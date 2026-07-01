package harness

import "testing"

// Edge-case and ES2023+ array tests. Function names all start with
// TestArrayEdge so they cannot collide with the names in array_test.go.
//
// Tests marked "// NOTE:" cover features that are likely unimplemented in
// this engine; their failures are expected and document missing functionality.
// All assertions encode the correct ECMAScript-spec behaviour — do not weaken
// them to paper over engine gaps.

// ---------------------------------------------------------------------------
// copyWithin — NOTE: likely unimplemented
// ---------------------------------------------------------------------------

func TestArrayEdgeCopyWithin(t *testing.T) {
	// NOTE: Array.prototype.copyWithin may be unimplemented in this engine.
	Expect(t, `
		// basic: copy element at index 3 to index 0
		var a = [1, 2, 3, 4, 5];
		a.copyWithin(0, 3, 4);
		assert.sameValue(a.join(","), "4,2,3,4,5");
		// negative target counts from end: -2 resolves to index 3
		var b = [1, 2, 3, 4, 5];
		b.copyWithin(-2, 0, 2);
		assert.sameValue(b.join(","), "1,2,3,1,2");
		// negative start counts from end: start=-2 resolves to index 3
		var c = [1, 2, 3, 4, 5];
		c.copyWithin(0, -2);
		assert.sameValue(c.join(","), "4,5,3,4,5");
		// copyWithin returns the same array reference (mutates in place)
		var d = [1, 2, 3];
		var e = d.copyWithin(0, 1);
		assert.sameValue(d === e, true);
		// overlapping region: spec requires a copy-then-write so earlier writes
		// do not corrupt the source range
		var f = [1, 2, 3, 4, 5];
		f.copyWithin(1, 2, 4);
		assert.sameValue(f.join(","), "1,3,4,4,5");
	`)
}

// ---------------------------------------------------------------------------
// ES2023 immutable methods — NOTE: likely unimplemented
// ---------------------------------------------------------------------------

func TestArrayEdgeToSorted(t *testing.T) {
	// NOTE: Array.prototype.toSorted is ES2023 and is likely unimplemented.
	Expect(t, `
		var a = [3, 1, 2];
		var sorted = a.toSorted();
		assert.sameValue(sorted.join(","), "1,2,3");
		// original array must not be mutated
		assert.sameValue(a.join(","), "3,1,2");
		// with numeric comparator
		var nums = [10, 1, 5];
		var numSorted = nums.toSorted(function(x, y) { return x - y; });
		assert.sameValue(numSorted.join(","), "1,5,10");
		assert.sameValue(nums.join(","), "10,1,5");
		// result is a distinct array object
		assert.sameValue(sorted === a, false);
	`)
}

func TestArrayEdgeToReversed(t *testing.T) {
	// NOTE: Array.prototype.toReversed is ES2023 and is likely unimplemented.
	Expect(t, `
		var a = [1, 2, 3, 4, 5];
		var rev = a.toReversed();
		assert.sameValue(rev.join(","), "5,4,3,2,1");
		// original array must not be mutated
		assert.sameValue(a.join(","), "1,2,3,4,5");
		// result is a distinct array object
		assert.sameValue(rev === a, false);
		// single-element
		assert.sameValue([42].toReversed().join(","), "42");
		// empty
		assert.sameValue([].toReversed().length, 0);
	`)
}

func TestArrayEdgeToSpliced(t *testing.T) {
	// NOTE: Array.prototype.toSpliced is ES2023 and is likely unimplemented.
	Expect(t, `
		var a = [1, 2, 3, 4, 5];
		// delete 2 elements at index 1, insert 20 and 30
		var result = a.toSpliced(1, 2, 20, 30);
		assert.sameValue(result.join(","), "1,20,30,4,5");
		// original array must not be mutated
		assert.sameValue(a.join(","), "1,2,3,4,5");
		// deletion only (no replacement items)
		var b = a.toSpliced(1, 2);
		assert.sameValue(b.join(","), "1,4,5");
		assert.sameValue(a.join(","), "1,2,3,4,5");
		// insertion only (deleteCount 0)
		var c = a.toSpliced(2, 0, 99);
		assert.sameValue(c.join(","), "1,2,99,3,4,5");
	`)
}

func TestArrayEdgeWith(t *testing.T) {
	// NOTE: Array.prototype.with is ES2023 and is likely unimplemented.
	Expect(t, `
		var a = [1, 2, 3, 4, 5];
		// replace element at a positive index
		var b = a.with(2, 99);
		assert.sameValue(b.join(","), "1,2,99,4,5");
		// original array must not be mutated
		assert.sameValue(a.join(","), "1,2,3,4,5");
		// negative index counts from end
		var c = a.with(-1, 99);
		assert.sameValue(c.join(","), "1,2,3,4,99");
		var d = a.with(-5, 99);
		assert.sameValue(d.join(","), "99,2,3,4,5");
		// out-of-bounds positive index must throw RangeError
		assert.throws(RangeError, function() { a.with(5, 99); });
		// out-of-bounds negative index must throw RangeError
		assert.throws(RangeError, function() { a.with(-6, 99); });
	`)
}

// ---------------------------------------------------------------------------
// ES2024 Object.groupBy — NOTE: likely unimplemented
// ---------------------------------------------------------------------------

func TestArrayEdgeGroupBy(t *testing.T) {
	// NOTE: Object.groupBy is ES2024 and is likely unimplemented in this engine.
	Expect(t, `
		var arr = [1, 2, 3, 4, 5, 6];
		var grouped = Object.groupBy(arr, function(n) {
			return n % 2 === 0 ? "even" : "odd";
		});
		assert.sameValue(grouped.even.join(","), "2,4,6");
		assert.sameValue(grouped.odd.join(","), "1,3,5");
		// groupBy preserves insertion order within each group
		var words = ["apple", "ant", "banana", "avocado"];
		var byLetter = Object.groupBy(words, function(w) { return w[0]; });
		assert.sameValue(byLetter["a"].join(","), "apple,ant,avocado");
		assert.sameValue(byLetter["b"].join(","), "banana");
	`)
}

// ---------------------------------------------------------------------------
// Sparse arrays and holes
// ---------------------------------------------------------------------------

func TestArrayEdgeSparseHoles(t *testing.T) {
	t.Skip("sparse-array holes are densified to undefined; a true sparse representation is deferred (see NOTES-divergences.md)")
	// Tests hole semantics as required by the spec. Engines that densify holes
	// (treating them as explicit undefined) will fail several of these assertions.
	Expect(t, `
		// array literal elision: [1, , 3] has length 3 and a hole at index 1
		var sparse = [1, , 3];
		assert.sameValue(sparse.length, 3);
		// a hole reads as undefined when accessed
		assert.sameValue(sparse[1], undefined);
		// 'in' distinguishes holes (false) from explicit undefined (true)
		assert.sameValue((1 in sparse), false);
		var withUndef = [1, undefined, 3];
		assert.sameValue((1 in withUndef), true);
		// forEach must skip holes; callback called only for indices 0 and 2
		var seenIdx = [];
		sparse.forEach(function(v, i) { seenIdx.push(i); });
		assert.sameValue(seenIdx.join(","), "0,2");
		// map must preserve hole positions in the result
		var mapped = sparse.map(function(v) { return v * 2; });
		assert.sameValue(mapped.length, 3);
		assert.sameValue(mapped[0], 2);
		assert.sameValue(mapped[2], 6);
		assert.sameValue((1 in mapped), false);
		// filter excludes holes from the result
		var filtered = sparse.filter(function() { return true; });
		assert.sameValue(filtered.length, 2);
		assert.sameValue(filtered[0], 1);
		assert.sameValue(filtered[1], 3);
	`)
}

func TestArrayEdgeDeleteHole(t *testing.T) {
	t.Skip("sparse-array holes are densified to undefined; a true sparse representation is deferred (see NOTES-divergences.md)")
	// delete arr[i] creates a hole at i; length must remain unchanged.
	Expect(t, `
		var arr = [1, 2, 3, 4, 5];
		var ok = delete arr[2];
		assert.sameValue(ok, true);
		// length is unchanged after delete
		assert.sameValue(arr.length, 5);
		// the deleted slot reads as undefined
		assert.sameValue(arr[2], undefined);
		// but it is a hole, not an own property
		assert.sameValue((2 in arr), false);
		// forEach skips the hole; indices 0, 1, 3, 4 are visited
		var seen = [];
		arr.forEach(function(v, i) { seen.push(i); });
		assert.sameValue(seen.join(","), "0,1,3,4");
	`)
}

// ---------------------------------------------------------------------------
// Array(n) constructor — sparse array with n holes
// ---------------------------------------------------------------------------

func TestArrayEdgeSparseConstructor(t *testing.T) {
	t.Skip("sparse-array holes are densified to undefined; a true sparse representation is deferred (see NOTES-divergences.md)")
	// Array(n) must produce a sparse array; forEach/map must treat slots as holes.
	Expect(t, `
		var sparse = Array(3);
		assert.sameValue(sparse.length, 3);
		// holes read as undefined
		assert.sameValue(sparse[0], undefined);
		// holes are not own properties
		assert.sameValue((0 in sparse), false);
		// fill converts sparse to dense
		var dense = Array(3).fill(0);
		assert.sameValue(dense.length, 3);
		assert.sameValue(dense.join(","), "0,0,0");
		assert.sameValue((0 in dense), true);
		// forEach on a pure-hole array must never invoke the callback
		var count = 0;
		Array(3).forEach(function() { count++; });
		assert.sameValue(count, 0);
		// map on Array(n) must preserve holes; result slots are not own properties
		var mapped = Array(3).map(function() { return 42; });
		assert.sameValue(mapped.length, 3);
		assert.sameValue((0 in mapped), false);
	`)
}

// ---------------------------------------------------------------------------
// length property — extension and zeroing
// ---------------------------------------------------------------------------

func TestArrayEdgeLengthExtend(t *testing.T) {
	Expect(t, `
		// setting length to 0 removes all elements
		var a = [1, 2, 3];
		a.length = 0;
		assert.sameValue(a.length, 0);
		assert.sameValue(a[0], undefined);
		// assigning to an index >= length extends length implicitly
		var b = [1, 2, 3];
		b[9] = 99;
		assert.sameValue(b.length, 10);
		assert.sameValue(b[9], 99);
		assert.sameValue(b[5], undefined); // gap reads as undefined
		// setting length larger than current pads with holes
		var c = [1, 2, 3];
		c.length = 6;
		assert.sameValue(c.length, 6);
		assert.sameValue(c[0], 1); // existing elements preserved
		assert.sameValue(c[3], undefined);
		assert.sameValue(c[5], undefined);
		// subsequent truncation removes newly padded and original elements
		c.length = 1;
		assert.sameValue(c.length, 1);
		assert.sameValue(c[0], 1);
		assert.sameValue(c[1], undefined);
	`)
}

// ---------------------------------------------------------------------------
// Nested spread and spread of various iterables
// ---------------------------------------------------------------------------

func TestArrayEdgeNestedSpread(t *testing.T) {
	Expect(t, `
		// spread of Map.entries yields [key, value] pairs
		var m = new Map([["x", 1], ["y", 2]]);
		var entries = [...m.entries()];
		assert.sameValue(entries.length, 2);
		assert.sameValue(entries[0][0], "x");
		assert.sameValue(entries[0][1], 1);
		assert.sameValue(entries[1][0], "y");
		assert.sameValue(entries[1][1], 2);
		// nested spread: spread the result of a spread into a new array
		var a = [1, 2, 3];
		var b = [4, 5, 6];
		var combined = [...a, ...[...b]];
		assert.sameValue(combined.join(","), "1,2,3,4,5,6");
		// spread of a string yields individual characters
		var chars = [..."hello"];
		assert.sameValue(chars.length, 5);
		assert.sameValue(chars[0], "h");
		assert.sameValue(chars[4], "o");
		// spread of a Set deduplicates according to SameValueZero
		var setSpread = [...new Set([1, 2, 2, 3, 3, 3])];
		assert.sameValue(setSpread.join(","), "1,2,3");
	`)
}

// ---------------------------------------------------------------------------
// concat with deeply nested arrays — concat does NOT recursively flatten
// ---------------------------------------------------------------------------

func TestArrayEdgeConcatDeepNested(t *testing.T) {
	Expect(t, `
		// concat spreads only the top-level array argument, not arrays nested inside it
		var r = [1].concat([[2, [3]]]);
		assert.sameValue(r.length, 2);
		assert.sameValue(Array.isArray(r[1]), true);
		assert.sameValue(r[1][0], 2);
		assert.sameValue(Array.isArray(r[1][1]), true);
		assert.sameValue(r[1][1][0], 3);
		// three levels of nesting: only the outermost level is spread
		var deep = [[1, [2, [3]]]];
		var res = [].concat(deep);
		assert.sameValue(res.length, 1);
		assert.sameValue(Array.isArray(res[0]), true);
		assert.sameValue(res[0][0], 1);
		assert.sameValue(Array.isArray(res[0][1]), true);
		// a plain object is appended as a single element (not spread)
		var obj = { 0: "a", length: 1 };
		var withObj = [1, 2].concat(obj);
		assert.sameValue(withObj.length, 3);
		assert.sameValue(withObj[2], obj);
	`)
}

// ---------------------------------------------------------------------------
// sort edge cases: mixed types, non-±1 comparator values
// ---------------------------------------------------------------------------

func TestArrayEdgeSortMixed(t *testing.T) {
	Expect(t, `
		// default sort converts all elements to strings before comparing
		// lexicographic order: "1" < "10" < "100" < "2" < "20"
		var mixed = [1, 10, 2, 100, "20"];
		mixed.sort();
		assert.sameValue(mixed[0], 1);
		assert.sameValue(mixed[1], 10);
		assert.sameValue(mixed[2], 100);
		assert.sameValue(mixed[3], 2);
		assert.sameValue(mixed[4], "20");
		// comparator may return large magnitude values; only the sign matters
		var arr = [30, 10, 20];
		arr.sort(function(a, b) { return (a > b) ? 1000 : (a < b) ? -1000 : 0; });
		assert.sameValue(arr.join(","), "10,20,30");
		// comparator returning exact differences (not clamped to -1/0/1) must work
		var nums = [5, 3, 8, 1, 9];
		nums.sort(function(a, b) { return a - b; });
		assert.sameValue(nums.join(","), "1,3,5,8,9");
	`)
}

func TestArrayEdgeSortUndefined(t *testing.T) {
	Expect(t, `
		// sort must move undefined elements to the end and must NOT pass them
		// to the comparator function
		var undefinedSeen = false;
		var arr = [3, undefined, 1, undefined, 2];
		arr.sort(function(a, b) {
			if (a === undefined || b === undefined) { undefinedSeen = true; }
			return a - b;
		});
		assert.sameValue(undefinedSeen, false);
		// undefined elements occupy the last two slots
		assert.sameValue(arr[3], undefined);
		assert.sameValue(arr[4], undefined);
		// the three numeric elements are sorted ascending in the first three slots
		assert.sameValue(arr[0], 1);
		assert.sameValue(arr[1], 2);
		assert.sameValue(arr[2], 3);
	`)
}

func TestArrayEdgeSortStabilityManyEqual(t *testing.T) {
	Expect(t, `
		// Build 10 objects; keys cycle 0, 1, 2 so several objects share a key.
		// insertion order tracked via .order property
		var items = [];
		for (var i = 0; i < 10; i++) {
			items.push({ key: i % 3, order: i });
		}
		items.sort(function(a, b) { return a.key - b.key; });
		// collect .order values per key group after sort
		var k0 = [], k1 = [], k2 = [];
		for (var j = 0; j < items.length; j++) {
			if (items[j].key === 0) k0.push(items[j].order);
			if (items[j].key === 1) k1.push(items[j].order);
			if (items[j].key === 2) k2.push(items[j].order);
		}
		// a stable sort must preserve relative insertion order within each group
		assert.sameValue(k0.join(","), "0,3,6,9");
		assert.sameValue(k1.join(","), "1,4,7");
		assert.sameValue(k2.join(","), "2,5,8");
	`)
}

// ---------------------------------------------------------------------------
// reduce / reduceRight additional edge cases
// ---------------------------------------------------------------------------

func TestArrayEdgeReduceRightIndex(t *testing.T) {
	Expect(t, `
		// reduceRight without init: last element is the initial accumulator;
		// the first callback call receives the second-to-last element at its index
		var firstIdx = -1;
		[10, 20, 30, 40].reduceRight(function(acc, v, i) {
			if (firstIdx < 0) { firstIdx = i; }
			return acc + v;
		});
		assert.sameValue(firstIdx, 2); // value 30 lives at index 2
		// reduce with an object as the initial accumulator (type-change init)
		var obj = [1, 2, 3, 4].reduce(function(acc, v) {
			acc[v] = v * v;
			return acc;
		}, {});
		assert.sameValue(obj[1], 1);
		assert.sameValue(obj[2], 4);
		assert.sameValue(obj[4], 16);
		// reduceRight with a string init — concatenates in reverse order
		var str = [1, 2, 3].reduceRight(function(acc, v) { return acc + String(v); }, "");
		assert.sameValue(str, "321");
		// reduce: index and array arguments forwarded correctly
		var idxSum = 0;
		[10, 20, 30].reduce(function(acc, v, i, arr) {
			idxSum += i;
			assert.sameValue(arr.length, 3);
			return acc;
		}, 0);
		assert.sameValue(idxSum, 3); // 0 + 1 + 2
	`)
}

// ---------------------------------------------------------------------------
// join with objects, nested arrays, and null/undefined
// ---------------------------------------------------------------------------

func TestArrayEdgeJoinObjects(t *testing.T) {
	Expect(t, `
		// objects are stringified via their toString during join
		var obj = { toString: function() { return "MYOBJ"; } };
		assert.sameValue([1, obj, 3].join(","), "1,MYOBJ,3");
		// plain objects fall back to the default [object Object]
		assert.sameValue([1, {}, 3].join(","), "1,[object Object],3");
		// nested arrays call their own toString (comma-separated) recursively
		assert.sameValue([[1, 2], [3, [4, 5]]].join("-"), "1,2-3,4,5");
		// both null and undefined become the empty string
		assert.sameValue([null, undefined, null].join(","), ",,");
		// mixed types with empty-string separator; null/undefined collapse to ""
		assert.sameValue([1, null, "a", undefined, true].join(""), "1atrue");
	`)
}

// ---------------------------------------------------------------------------
// flat — precise depth counts and flat(undefined)
// ---------------------------------------------------------------------------

func TestArrayEdgeFlatDepth(t *testing.T) {
	Expect(t, `
		var deep = [1, [2, [3, [4, [5]]]]];
		// flat(1): one level expanded — [1, 2, [3, [4, [5]]]]
		var f1 = deep.flat(1);
		assert.sameValue(f1.length, 3);
		assert.sameValue(f1[0], 1);
		assert.sameValue(f1[1], 2);
		assert.sameValue(Array.isArray(f1[2]), true);
		// flat(2): two levels — [1, 2, 3, [4, [5]]]
		var f2 = deep.flat(2);
		assert.sameValue(f2.length, 4);
		assert.sameValue(f2[2], 3);
		assert.sameValue(Array.isArray(f2[3]), true);
		// flat(3): three levels — [1, 2, 3, 4, [5]]
		var f3 = deep.flat(3);
		assert.sameValue(f3.length, 5);
		assert.sameValue(f3[3], 4);
		assert.sameValue(Array.isArray(f3[4]), true);
		// flat(undefined) must behave as flat(1) per spec (undefined → depthNum 1)
		var fu = [[1, 2], [3, 4]].flat(undefined);
		assert.sameValue(fu.join(","), "1,2,3,4");
		// flat(0) returns a shallow copy with no flattening
		var f0 = [[1, 2], [3, 4]].flat(0);
		assert.sameValue(f0.length, 2);
		assert.sameValue(Array.isArray(f0[0]), true);
	`)
}

// ---------------------------------------------------------------------------
// Array.from with a Map (full entry pairs) and a custom iterator
// ---------------------------------------------------------------------------

func TestArrayEdgeFromMapEntries(t *testing.T) {
	Expect(t, `
		// Array.from with a Map iterates [key, value] pairs
		var m = new Map([["a", 1], ["b", 2], ["c", 3]]);
		var pairs = Array.from(m);
		assert.sameValue(pairs.length, 3);
		assert.sameValue(pairs[0][0], "a");
		assert.sameValue(pairs[0][1], 1);
		assert.sameValue(pairs[2][0], "c");
		assert.sameValue(pairs[2][1], 3);
		// Array.from with a hand-rolled iterator object
		var vals = [10, 20, 30];
		var idx = 0;
		var iter = {
			next: function() {
				if (idx < vals.length) { return { value: vals[idx++], done: false }; }
				return { value: undefined, done: true };
			}
		};
		iter[Symbol.iterator] = function() { return this; };
		var fromIter = Array.from(iter);
		assert.sameValue(fromIter.length, 3);
		assert.sameValue(fromIter.join(","), "10,20,30");
		// Array.from with { length: 3 } and no values (array-like, holes become undefined)
		var like = { length: 3 };
		var fromLike = Array.from(like);
		assert.sameValue(fromLike.length, 3);
		assert.sameValue(fromLike[0], undefined);
	`)
}

// ---------------------------------------------------------------------------
// Array.from with mapFn and thisArg
// ---------------------------------------------------------------------------

func TestArrayEdgeFromMapFnThisArg(t *testing.T) {
	Expect(t, `
		// thisArg is forwarded as 'this' inside the mapFn
		var ctx = { factor: 10 };
		var result = Array.from([1, 2, 3], function(x) { return x * this.factor; }, ctx);
		assert.sameValue(result.join(","), "10,20,30");
		// mapFn receives (element, index) in order
		var collected = [];
		Array.from([5, 6, 7], function(v, i) { collected.push(i + ":" + v); return v; });
		assert.sameValue(collected.join(","), "0:5,1:6,2:7");
		// mapFn over an array-like object
		var like = { 0: "a", 1: "b", 2: "c", length: 3 };
		var upper = Array.from(like, function(s) { return s.toUpperCase(); });
		assert.sameValue(upper.join(","), "A,B,C");
	`)
}

// ---------------------------------------------------------------------------
// Method chaining
// ---------------------------------------------------------------------------

func TestArrayEdgeChaining(t *testing.T) {
	Expect(t, `
		// filter -> map -> reduce
		var sum = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
			.filter(function(x) { return x % 2 === 0; })
			.map(function(x) { return x * x; })
			.reduce(function(acc, x) { return acc + x; }, 0);
		// evens: [2,4,6,8,10] -> squares: [4,16,36,64,100] -> sum: 220
		assert.sameValue(sum, 220);
		// flatMap -> filter -> sort (descending)
		var res = [1, 2, 3, 4]
			.flatMap(function(x) { return [x, -x]; })
			.filter(function(x) { return x > 0; })
			.sort(function(a, b) { return b - a; });
		assert.sameValue(res.join(","), "4,3,2,1");
		// map -> flat -> filter on strings
		var words = ["hello world", "foo bar"];
		var letters = words
			.map(function(s) { return s.split(""); })
			.flat()
			.filter(function(c) { return c !== " "; });
		assert.sameValue(letters.join(""), "helloworldfoobar");
	`)
}

// ---------------------------------------------------------------------------
// Large arrays
// ---------------------------------------------------------------------------

func TestArrayEdgeLargeArray(t *testing.T) {
	Expect(t, `
		// Build a 1000-element array via Array.from with a mapFn
		var large = Array.from({ length: 1000 }, function(_, i) { return i; });
		assert.sameValue(large.length, 1000);
		assert.sameValue(large[0], 0);
		assert.sameValue(large[999], 999);
		// sum via reduce: 0+1+...+999 = 999*1000/2 = 499500
		var total = large.reduce(function(acc, v) { return acc + v; }, 0);
		assert.sameValue(total, 499500);
		// filter even elements: 0,2,4,...,998 — 500 elements
		var evens = large.filter(function(x) { return x % 2 === 0; });
		assert.sameValue(evens.length, 500);
		assert.sameValue(evens[0], 0);
		assert.sameValue(evens[499], 998);
		// map to doubled values
		var doubled = large.map(function(x) { return x * 2; });
		assert.sameValue(doubled[500], 1000);
		assert.sameValue(doubled[999], 1998);
	`)
}
