package harness

import "testing"

// Array iteration methods capture their length once. A callback that grows the
// array must not make the method read or write out of bounds (regression for an
// index-out-of-range host panic in Array.prototype.map), and one that shrinks
// the array leaves the unvisited tail as holes.
func TestArrayIterationLengthCapturedOnce(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3, 4, 5];
		var r = a.map(function (x, idx) { if (idx === 0) { a.push(6, 7, 8); } return x * 2; });
		assert.sameValue(r.length, 5, "map length captured once");
		assert.sameValue(r[4], 10);

		var b = [1, 2, 3, 4, 5];
		var r2 = b.map(function (x, idx) { if (idx === 0) { b.length = 2; } return x; });
		assert.sameValue(r2.length, 5);
		assert.sameValue(r2.hasOwnProperty(4), false, "unvisited tail stays a hole");

		// forEach/filter over a growing array likewise stop at the original length.
		var seen = 0, c = [1, 2, 3];
		c.forEach(function (x, idx) { if (idx === 0) { c.push(4, 5); } seen++; });
		assert.sameValue(seen, 3, "forEach visits only the original elements");
	`)
}
