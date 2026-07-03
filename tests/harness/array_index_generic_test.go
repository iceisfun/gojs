package harness

import "testing"

// The Array.prototype index/copy/mutator methods (slice, splice, fill, reverse,
// copyWithin, at, with, toReversed, toSorted, toSpliced) are generic: ToObject on
// this, LengthOfArrayLike, ArraySpeciesCreate for slice/splice, ArrayCreate for
// the ES2023 copy methods, and per-index HasProperty/Get/Set/Delete so holes are
// preserved and array-like receivers work.

func TestSliceHonorsSpecies(t *testing.T) {
	// A gojs subclass-Array stores its elements as named properties rather than
	// dense storage, so the values are read with bracket ([[Get]]) access instead
	// of Array.prototype.join.
	Expect(t, `
		class MyArray extends Array {}
		var r = new MyArray(1, 2, 3, 4).slice(1, 3);
		assert.sameValue(r instanceof MyArray, true, 'slice result honors @@species');
		assert.sameValue(r.length, 2);
		assert.sameValue(r[0], 2);
		assert.sameValue(r[1], 3);
	`)
}

func TestSliceGenericOverArrayLike(t *testing.T) {
	Expect(t, `
		var arrayLike = { length: 3, 0: 'a', 1: 'b', 2: 'c' };
		var r = Array.prototype.slice.call(arrayLike, 1);
		assert.sameValue(Array.isArray(r), true, 'slice over array-like yields a real Array');
		assert.sameValue(r.length, 2);
		assert.sameValue(r.join(','), 'b,c');
	`)
}

func TestSlicePreservesHoles(t *testing.T) {
	Expect(t, `
		var r = [1, , 3, , 5].slice(1, 4);
		assert.sameValue(r.length, 3);
		assert.sameValue(0 in r, false, 'source hole stays a hole in the slice');
		assert.sameValue(2 in r, false, 'trailing hole stays a hole in the slice');
		assert.sameValue(1 in r, true, 'present index copied');
		assert.sameValue(r[1], 3);
	`)
}

func TestSpliceReturnsSpeciesAndPreservesHoles(t *testing.T) {
	// The removed array is allocated via ArraySpeciesCreate; a removed hole stays a
	// hole. Values are read with bracket access (see TestSliceHonorsSpecies).
	Expect(t, `
		class MyArray extends Array {}
		var src = new MyArray(1, 2, 3, 4);
		delete src[1];
		var removed = src.splice(1, 2, 'x');
		assert.sameValue(removed instanceof MyArray, true, 'splice removed-array honors @@species');
		assert.sameValue(removed.length, 2);
		assert.sameValue(0 in removed, false, 'removed hole stays a hole');
		assert.sameValue(removed[1], 3, 'removed present element copied');
		assert.sameValue(src.length, 3);
		assert.sameValue(src[0], 1);
		assert.sameValue(src[1], 'x');
		assert.sameValue(src[2], 4);
	`)
}

func TestSpliceShiftPreservesTrailingHole(t *testing.T) {
	Expect(t, `
		var a = [0, 1, 2, , 4];
		a.splice(1, 1);
		assert.sameValue(a.length, 4);
		assert.sameValue(a.join(','), '0,2,,4');
		assert.sameValue(2 in a, false, 'the shifted hole remains a hole');
	`)
}

func TestFillGenericOverArrayLike(t *testing.T) {
	Expect(t, `
		var arrayLike = { length: 4, 0: 'a', 1: 'b', 2: 'c', 3: 'd' };
		var r = Array.prototype.fill.call(arrayLike, 'z', 1, 3);
		assert.sameValue(r, arrayLike, 'fill returns the same object');
		assert.sameValue(arrayLike[0], 'a');
		assert.sameValue(arrayLike[1], 'z');
		assert.sameValue(arrayLike[2], 'z');
		assert.sameValue(arrayLike[3], 'd');
	`)
}

func TestReverseGenericPreservesHoles(t *testing.T) {
	Expect(t, `
		var a = [0, , 2, 3];
		a.reverse();
		assert.sameValue(a.length, 4);
		assert.sameValue(a.join(','), '3,2,,0');
		assert.sameValue(2 in a, false, 'the hole is reversed as a hole');
		assert.sameValue(a[0], 3);
	`)
}

func TestWithThrowsRangeErrorOutOfRange(t *testing.T) {
	ExpectError(t, `[1, 2, 3].with(5, 9)`, "RangeError")
	ExpectError(t, `[1, 2, 3].with(-4, 9)`, "RangeError")
}

func TestWithReplacesIndex(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3];
		var r = a.with(-1, 9);
		assert.sameValue(r.join(','), '1,2,9');
		assert.sameValue(a.join(','), '1,2,3', 'receiver is unchanged');
	`)
}

func TestGenericMethodsRequireObjectCoercible(t *testing.T) {
	ExpectError(t, `Array.prototype.slice.call(null)`, "TypeError")
	ExpectError(t, `Array.prototype.splice.call(undefined)`, "TypeError")
	ExpectError(t, `Array.prototype.reverse.call(null)`, "TypeError")
	ExpectError(t, `Array.prototype.at.call(undefined, 0)`, "TypeError")
	ExpectError(t, `Array.prototype.with.call(null, 0, 1)`, "TypeError")
}

func TestToSpliancedIsGenericCopy(t *testing.T) {
	Expect(t, `
		var arrayLike = { length: 3, 0: 'a', 1: 'b', 2: 'c' };
		var r = Array.prototype.toSpliced.call(arrayLike, 1, 1, 'x', 'y');
		assert.sameValue(Array.isArray(r), true);
		assert.sameValue(r.join(','), 'a,x,y,c');
	`)
}
