package harness

import "testing"

// A for-of loop must close its iterator (call the iterator's return method) on
// any abrupt completion: break, a break/continue targeting an enclosing loop, a
// throw, or a return out of the enclosing function (ECMA-262 14.7.5.7). It must
// NOT call return when the iterator is exhausted normally.

const countingIter = `
	var closed = 0;
	var iter = {};
	iter[Symbol.iterator] = function () {
		var n = 0;
		return {
			next: function () { return { value: n++, done: false }; },
			return: function () { closed += 1; return {}; }
		};
	};
`

func TestForOfClosesOnBreak(t *testing.T) {
	Expect(t, countingIter+`
		for (var x of iter) { if (x >= 2) break; }
		assert.sameValue(closed, 1, "break closes the iterator");
	`)
}

func TestForOfClosesOnThrow(t *testing.T) {
	Expect(t, countingIter+`
		try { for (var x of iter) { if (x >= 2) throw new Error("stop"); } } catch (e) {}
		assert.sameValue(closed, 1, "throw closes the iterator");
	`)
}

func TestForOfClosesOnReturn(t *testing.T) {
	Expect(t, countingIter+`
		(function () { for (var x of iter) { if (x >= 2) return; } })();
		assert.sameValue(closed, 1, "return closes the iterator");
	`)
}

func TestForOfClosesOnLabeledBreak(t *testing.T) {
	Expect(t, countingIter+`
		outer: for (var y of [0]) {
			for (var x of iter) { if (x >= 2) break outer; }
		}
		assert.sameValue(closed, 1, "labeled break to an outer loop closes the inner iterator");
	`)
}

func TestForOfDoesNotCloseOnExhaustion(t *testing.T) {
	Expect(t, `
		var closed = 0;
		var iter = {};
		iter[Symbol.iterator] = function () {
			var n = 0;
			return {
				next: function () { return { value: n, done: n++ >= 3 }; },
				return: function () { closed += 1; return {}; }
			};
		};
		var sum = 0;
		for (var x of iter) { sum += x; }
		assert.sameValue(sum, 3, "0 + 1 + 2");
		assert.sameValue(closed, 0, "normal exhaustion does not call return");
	`)
}
