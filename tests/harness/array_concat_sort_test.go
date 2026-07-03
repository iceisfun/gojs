package harness

import "testing"

// Array.prototype.concat is generic: ToObject(this), ArraySpeciesCreate for the
// result, IsConcatSpreadable (@@isConcatSpreadable then IsArray), and per-index
// HasProperty/Get so holes are preserved and getters/proxies are observed.

func TestConcatSpreadableFlag(t *testing.T) {
	Expect(t, `
		var arrayLike = { length: 2, 0: 'a', 1: 'b' };
		arrayLike[Symbol.isConcatSpreadable] = true;
		var spread = [].concat(arrayLike);
		assert.sameValue(spread.length, 2, 'spreadable array-like is spread');
		assert.sameValue(spread.join(','), 'a,b');
		var arr = ['x'];
		arr[Symbol.isConcatSpreadable] = false;
		var r = [].concat(arr);
		assert.sameValue(r.length, 1, 'non-spreadable array is appended whole');
		assert.sameValue(r[0], arr);
	`)
}

func TestConcatUsesSpecies(t *testing.T) {
	Expect(t, `
		class MyArray extends Array {}
		var r = new MyArray(1, 2, 3).concat([4]);
		assert.sameValue(r instanceof MyArray, true, 'concat result honors @@species');
		assert.sameValue(r.length, 4);
		assert.sameValue(r.join(','), '1,2,3,4');
	`)
}

func TestConcatPreservesHolesAndBoxesPrimitive(t *testing.T) {
	Expect(t, `
		var r = [1, , 3].concat([, 5]);
		assert.sameValue(r.length, 5);
		assert.sameValue(1 in r, false, 'source hole stays a hole');
		assert.sameValue(3 in r, false, 'appended hole stays a hole');
		var boxed = Array.prototype.concat.call(true);
		assert.sameValue(boxed[0] instanceof Boolean, true, 'primitive this is boxed via ToObject');
	`)
}

// Array.prototype.sort: rejects a non-callable comparator, is generic over
// array-likes, and drives index accessors through [[Get]]/[[Set]].

func TestSortRejectsNonCallableComparator(t *testing.T) {
	ExpectError(t, `[3,1,2].sort(null)`, "TypeError")
	ExpectError(t, `[3,1,2].sort(42)`, "TypeError")
	ExpectError(t, `[3,1,2].sort({})`, "TypeError")
}

func TestSortIsGenericOverArrayLike(t *testing.T) {
	Expect(t, `
		var o = { length: 3, 0: 10, 1: 9, 2: 8 };
		o.sort = Array.prototype.sort;
		o.sort(function (a, b) { return a - b; });
		assert.sameValue(o[0], 8);
		assert.sameValue(o[1], 9);
		assert.sameValue(o[2], 10);
	`)
}

func TestSortMovesHolesAndUndefinedToEnd(t *testing.T) {
	Expect(t, `
		var a = ['c', undefined, , 'a', 'b'];
		a.sort();
		assert.sameValue(a[0], 'a');
		assert.sameValue(a[1], 'b');
		assert.sameValue(a[2], 'c');
		assert.sameValue(a[3], undefined, 'undefined sorts after defined values');
		assert.sameValue(4 in a, false, 'the hole migrates to the tail');
	`)
}
