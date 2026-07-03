package harness

import "testing"

// The Array.prototype search / flatten / iterator / join methods are generic:
// ToObject(this) (RequireObjectCoercible), LengthOfArrayLike, and per-index
// HasProperty/[[Get]] so array-like receivers, holes, and coercion order all
// match the spec. These tests lock in that genericity.

func TestIndexOfGenericOverArrayLike(t *testing.T) {
	Expect(t, `
		var like = { length: 3, 0: 'a', 1: 'b', 2: 'c' };
		assert.sameValue(Array.prototype.indexOf.call(like, 'b'), 1, 'finds present index');
		assert.sameValue(Array.prototype.indexOf.call(like, 'z'), -1, 'absent -> -1');
		assert.sameValue(Array.prototype.indexOf.call(like, 'c', -1), 2, 'negative fromIndex');
		assert.sameValue(Array.prototype.indexOf.call(like, 'a', 5), -1, 'fromIndex past length');
	`)
}

func TestIncludesGenericOverArrayLike(t *testing.T) {
	Expect(t, `
		var like = { length: 2, 0: 'x', 1: 'y' };
		assert.sameValue(Array.prototype.includes.call(like, 'y'), true, 'finds present value');
		assert.sameValue(Array.prototype.includes.call(like, 'z'), false, 'absent value');
	`)
}

func TestIncludesFindsNaN(t *testing.T) {
	Expect(t, `
		assert.sameValue([1, NaN, 3].includes(NaN), true, 'SameValueZero finds NaN');
		assert.sameValue([1, NaN, 3].indexOf(NaN), -1, 'strict equality does not find NaN');
	`)
}

func TestIndexOfSkipsHolesIncludesTreatsHoleAsUndefined(t *testing.T) {
	Expect(t, `
		var a = [0, , 2];
		// indexOf uses HasProperty: the hole at index 1 is not visited.
		assert.sameValue(a.indexOf(undefined), -1, 'indexOf skips a hole');
		// includes does not skip holes: the hole reads as undefined.
		assert.sameValue(a.includes(undefined), true, 'includes treats a hole as undefined');
	`)
}

func TestLastIndexOfGenericAndFromIndexPresence(t *testing.T) {
	Expect(t, `
		var like = { length: 3, 0: 'a', 1: 'b', 2: 'a' };
		assert.sameValue(Array.prototype.lastIndexOf.call(like, 'a'), 2, 'scans high to low');
		var a = [2, 1];
		// An explicit undefined fromIndex coerces to 0 (present), not len-1.
		assert.sameValue(a.lastIndexOf(1, undefined), -1, 'explicit undefined fromIndex -> 0');
		assert.sameValue(a.lastIndexOf(1), 1, 'absent fromIndex -> len-1');
	`)
}

func TestFlatIsGeneric(t *testing.T) {
	Expect(t, `
		var like = { length: 2, 0: [1, 2], 1: [3, [4]] };
		var r = Array.prototype.flat.call(like);
		assert.sameValue(r.length, 4, 'array-like flattens one level');
		assert.sameValue(r.join(','), '1,2,3,4');
		assert.sameValue([1, [2, [3, [4]]]].flat(Infinity).join(','), '1,2,3,4', 'flat(Infinity)');
	`)
}

func TestJoinIsGeneric(t *testing.T) {
	Expect(t, `
		var like = { length: 3, 0: 'a', 1: null, 2: 'c' };
		// null/undefined render as the empty string; each index read via [[Get]].
		assert.sameValue(Array.prototype.join.call(like, '-'), 'a--c', 'array-like join');
		assert.sameValue([1, 2, 3].join(), '1,2,3', 'default separator is comma');
	`)
}

func TestIteratorsAreGenericAndLatchDone(t *testing.T) {
	Expect(t, `
		var like = { length: 2, 0: 'a', 1: 'b' };
		var keys = Array.prototype.keys.call(like);
		assert.sameValue(keys.next().value, 0);
		assert.sameValue(keys.next().value, 1);
		assert.sameValue(keys.next().done, true, 'array-like keys iterator');

		// A finished array iterator stays done even after the array grows.
		var arr = [];
		var it = arr.keys();
		arr.push('a');
		assert.sameValue(it.next().value, 0, 'sees element pushed before exhaustion');
		assert.sameValue(it.next().done, true, 'exhausted');
		arr.push('b');
		assert.sameValue(it.next().done, true, 'stays done after a later push');
	`)
}

func TestSearchMethodsRequireObjectCoercible(t *testing.T) {
	ExpectError(t, `Array.prototype.indexOf.call(null, 1)`, "TypeError")
	ExpectError(t, `Array.prototype.includes.call(undefined, 1)`, "TypeError")
	ExpectError(t, `Array.prototype.lastIndexOf.call(null, 1)`, "TypeError")
	ExpectError(t, `Array.prototype.join.call(undefined)`, "TypeError")
	ExpectError(t, `Array.prototype.flat.call(null)`, "TypeError")
	ExpectError(t, `Array.prototype.keys.call(null)`, "TypeError")
}
