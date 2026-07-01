package harness

import "testing"

// Array destructuring must consume its iterator lazily and, when the pattern is
// satisfied before the iterator is exhausted, close it by calling return()
// (ECMA-262 IteratorClose). Draining eagerly would hang on an infinite iterator
// and skip the observable return() call. Regression for the class-method
// parameter timeouts (method([x]) over an infinite iterator).

const infiniteIter = `
	var doneCallCount = 0;
	var iter = {};
	iter[Symbol.iterator] = function () {
		return {
			next: function () { return { value: null, done: false }; },
			return: function () { doneCallCount += 1; return {}; }
		};
	};
`

func TestDestructuringClosesIteratorBinding(t *testing.T) {
	Expect(t, infiniteIter+`
		let [x] = iter;
		assert.sameValue(doneCallCount, 1, "let binding closes the iterator");
	`)
}

func TestDestructuringClosesIteratorParam(t *testing.T) {
	Expect(t, infiniteIter+`
		var called = 0;
		class C {
			method([x]) { assert.sameValue(doneCallCount, 1); called++; }
		}
		new C().method(iter);
		assert.sameValue(called, 1, "class method param destructuring runs once");
		assert.sameValue(doneCallCount, 1, "param destructuring closes the iterator");
	`)
}

func TestDestructuringClosesIteratorAssignment(t *testing.T) {
	Expect(t, infiniteIter+`
		var x;
		[x] = iter;
		assert.sameValue(doneCallCount, 1, "assignment destructuring closes the iterator");
	`)
}

// An abrupt completion while binding an element must still close the iterator,
// and the binding error must propagate (not the return() result).
func TestDestructuringClosesIteratorOnAbrupt(t *testing.T) {
	Expect(t, infiniteIter+`
		var obj = {};
		Object.defineProperty(obj, "x", {
			set: function () { throw new TypeError("boom"); }
		});
		var threw = false;
		try {
			[obj.x] = iter;
		} catch (e) {
			threw = e instanceof TypeError;
		}
		assert.sameValue(threw, true, "binding error propagates");
		assert.sameValue(doneCallCount, 1, "iterator closed on abrupt completion");
	`)
}

// The finite paths must remain correct: full consumption, rest gathering,
// holes, string iteration, and assignment targets.
func TestDestructuringFinitePaths(t *testing.T) {
	Expect(t, `
		var [a, b, c] = [1, 2, 3];
		assert.sameValue(a + b + c, 6, "full array destructuring");

		var [h, ...rest] = [1, 2, 3, 4];
		assert.sameValue(rest.join(","), "2,3,4", "rest gathers the tail");

		var [, y, ] = [1, 2, 3];
		assert.sameValue(y, 2, "elision skips holes");

		var [s1, s2] = "hi";
		assert.sameValue(s1 + s2, "hi", "string destructuring");

		var m, n;
		[m, n] = [7, 8];
		assert.sameValue(m + n, 15, "assignment destructuring");

		var [p = 10, q = 20] = [1];
		assert.sameValue(p + "," + q, "1,20", "defaults apply to missing elements");
	`)
}
