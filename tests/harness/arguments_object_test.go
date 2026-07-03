package harness

import "testing"

// The arguments object is an ordinary object, not an Array exotic: an
// out-of-range index assignment must not grow "length", and Array.isArray is
// false. Array.prototype methods still work over it via the array-like protocol.

func TestArgumentsIsNotArrayExotic(t *testing.T) {
	Expect(t, `
		function f() {
			assert.sameValue(arguments.length, 2, 'initial length');
			arguments[5] = 99;                       // out-of-range assignment
			assert.sameValue(arguments.length, 2, 'length is not coupled to indices');
			assert.sameValue(Array.isArray(arguments), false, 'arguments is not an Array');
			assert.sameValue(Object.prototype.toString.call(arguments), '[object Arguments]');
			var d = Object.getOwnPropertyDescriptor(arguments, 'length');
			assert.sameValue(d.enumerable, false, 'length is non-enumerable');
			assert.sameValue(d.writable, true);
			assert.sameValue(d.configurable, true);
		}
		f(1, 2);
	`)
}

func TestArgumentsWorksWithGenericArrayMethods(t *testing.T) {
	Expect(t, `
		function f() {
			assert.sameValue(Array.prototype.slice.call(arguments, 1).join(','), 'b,c');
			assert.sameValue([].concat.apply([], arguments).length, 3);
			var out = '';
			for (var x of arguments) out += x;   // @@iterator
			assert.sameValue(out, 'abc');
			assert.sameValue([...arguments].join(','), 'a,b,c');   // spread
			var sum = 0;
			Array.prototype.forEach.call(arguments, function () { sum++; });
			assert.sameValue(sum, 3, 'forEach.call does not observe an appended index');
		}
		f('a', 'b', 'c');
	`)
}

// Array.prototype[Symbol.iterator] is the very same function object as
// Array.prototype.values (§23.1.3.40), and the arguments object's @@iterator is
// that same function.
func TestArrayIteratorIdentity(t *testing.T) {
	Expect(t, `
		assert.sameValue(Array.prototype.values, Array.prototype[Symbol.iterator],
			'Array.prototype[@@iterator] === Array.prototype.values');
		function f() {
			assert.sameValue(arguments[Symbol.iterator], Array.prototype.values,
				'arguments[@@iterator] === Array.prototype.values');
		}
		f();
	`)
}
