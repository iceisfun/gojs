package harness

import "testing"

// Array.from is a generic factory: the `this` value is used as the constructor
// for the result, a non-callable mapfn is rejected up front, and the iterator
// is closed when the mapping function throws.

func TestArrayFromRejectsNonCallableMapfn(t *testing.T) {
	ExpectError(t, `Array.from([], null)`, "TypeError")
	ExpectError(t, `Array.from([], Symbol('x'))`, "TypeError")
	ExpectError(t, `Array.from([], 42)`, "TypeError")
}

func TestArrayFromUsesThisConstructor(t *testing.T) {
	Expect(t, `
		var calls = [];
		function C() { calls.push(arguments.length); this.marker = true; }
		// array-like path forwards the length to the constructor
		var r = Array.from.call(C, { length: 3, 0: 'a', 1: 'b', 2: 'c' });
		assert.sameValue(r instanceof C, true, 'result is a C instance');
		assert.sameValue(r.marker, true);
		assert.sameValue(calls[0], 1, 'array-like path passes length as the sole arg');
		assert.sameValue(r[0], 'a');
		assert.sameValue(r.length, 3);
	`)
	Expect(t, `
		var calls = [];
		function C() { calls.push(arguments.length); }
		// iterator path constructs with no arguments
		var r = Array.from.call(C, ['x', 'y']);
		assert.sameValue(r instanceof C, true);
		assert.sameValue(calls[0], 0, 'iterator path passes no constructor args');
		assert.sameValue(r[0], 'x');
		assert.sameValue(r.length, 2);
	`)
}

func TestArrayFromClosesIteratorOnMapfnThrow(t *testing.T) {
	Expect(t, `
		var closed = false;
		var iter = {
			[Symbol.iterator]() {
				var i = 0;
				return {
					next() { return { value: i++, done: false }; },
					return() { closed = true; return {}; }
				};
			}
		};
		var threw = false;
		try {
			Array.from(iter, function () { throw new Error('boom'); });
		} catch (e) { threw = true; }
		assert.sameValue(threw, true, 'the mapfn error propagates');
		assert.sameValue(closed, true, 'the iterator was closed');
	`)
}

func TestArrayFromStringUsesIterator(t *testing.T) {
	Expect(t, `
		var r = Array.from('abc');
		assert.sameValue(r.length, 3);
		assert.sameValue(r.join(','), 'a,b,c');
	`)
}
