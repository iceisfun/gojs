package harness

import "testing"

// A small generator producing 1..n used across the iterator-helper tests.
const genPrologue = `
function* nums(n) { for (let k = 0; k < n; k++) yield k; }
function log(x) { calls.push(x); return x; }
`

func TestIteratorGlobalShape(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof Iterator, "function", "Iterator is a function");
		assert.sameValue(Iterator.name, "Iterator");
		assert.sameValue(Iterator.length, 0);
		assert.throws(TypeError, function () { new Iterator(); }, "abstract");
		assert.throws(TypeError, function () { Iterator(); }, "no new");
		var proto = Iterator.prototype;
		assert.sameValue(typeof proto, "object");
		assert.sameValue(proto[Symbol.iterator].call(proto), proto, "@@iterator returns this");
		assert.sameValue(proto[Symbol.toStringTag], "Iterator", "toStringTag getter");
		assert.sameValue(proto.constructor, Iterator, "constructor getter");
		// Subclassing works.
		class MyIter extends Iterator {}
		var m = new MyIter();
		assert.sameValue(m instanceof Iterator, true);
	`)
}

func TestIteratorToStringTagSetter(t *testing.T) {
	Expect(t, `
		var proto = Iterator.prototype;
		// Setting on the prototype itself throws (emulates non-writable data prop).
		assert.throws(TypeError, function () { proto[Symbol.toStringTag] = "x"; });
		assert.throws(TypeError, function () { proto.constructor = 1; });
		// Setting on an object that inherits the accessor creates an own data
		// property (SetterThatIgnoresPrototypeProperties).
		var it = Object.create(Iterator.prototype);
		it[Symbol.toStringTag] = "custom";
		assert.sameValue(Object.getOwnPropertyDescriptor(it, Symbol.toStringTag).value, "custom");
		it.constructor = "c";
		assert.sameValue(Object.getOwnPropertyDescriptor(it, "constructor").value, "c");
	`)
}

func TestIteratorFrom(t *testing.T) {
	Expect(t, genPrologue+`
		// From an array (built-in iterator inherits from %Iterator.prototype% via
		// helper wrapping): result is iterable and has helper methods.
		var it = Iterator.from([10, 20, 30]);
		assert.sameValue(typeof it.next, "function");
		assert.sameValue(typeof it.map, "function", "wrapped gets Iterator.prototype methods");
		var r = it.next();
		assert.sameValue(r.value, 10);
		assert.sameValue(r.done, false);
		// From a generator (already an Iterator) returns the iterator unchanged.
		var g = nums(3);
		assert.sameValue(Iterator.from(g), g, "already an Iterator");
		// From a string.
		assert.sameValue(Iterator.from("ab").toArray().join(","), "a,b");
		// Non-iterable throws.
		assert.throws(TypeError, function () { Iterator.from(42); });
		assert.throws(TypeError, function () { Iterator.from(null); });
	`)
}

func TestIteratorToArrayMapFilter(t *testing.T) {
	Expect(t, genPrologue+`
		assert.sameValue(nums(4).toArray().join(","), "0,1,2,3");
		assert.sameValue(nums(4).map(function (x) { return x * 2; }).toArray().join(","), "0,2,4,6");
		assert.sameValue(nums(5).filter(function (x) { return x % 2 === 0; }).toArray().join(","), "0,2,4");
		// counter argument.
		var idx = [];
		nums(3).map(function (x, i) { idx.push(i); return x; }).toArray();
		assert.sameValue(idx.join(","), "0,1,2");
	`)
}

func TestIteratorTakeDrop(t *testing.T) {
	Expect(t, genPrologue+`
		assert.sameValue(nums(10).take(3).toArray().join(","), "0,1,2");
		assert.sameValue(nums(10).drop(7).toArray().join(","), "7,8,9");
		assert.sameValue(nums(3).take(10).toArray().join(","), "0,1,2");
		assert.sameValue(nums(3).drop(10).toArray().join(","), "");
		assert.sameValue(nums(5).take(Infinity).toArray().join(","), "0,1,2,3,4");
		assert.throws(RangeError, function () { nums(1).take(NaN); });
		assert.throws(RangeError, function () { nums(1).take(-1); });
		assert.throws(RangeError, function () { nums(1).drop(NaN); });
		assert.throws(RangeError, function () { nums(1).drop(-1); });
	`)
}

func TestIteratorFlatMap(t *testing.T) {
	Expect(t, genPrologue+`
		var out = nums(3).flatMap(function (x) { return [x, x * 10]; }).toArray();
		assert.sameValue(out.join(","), "0,0,1,10,2,20");
		// flatMap uses reject-primitives: a returned primitive (even a string)
		// throws; you must return an iterable/iterator.
		var s = ["a", "bc"].values().flatMap(function (x) { return x[Symbol.iterator](); }).toArray();
		assert.sameValue(s.join(","), "a,b,c");
		assert.throws(TypeError, function () {
			["a"].values().flatMap(function (x) { return x; }).toArray();
		}, "returned string primitive is rejected");
		// Non-iterable mapped value throws.
		assert.throws(TypeError, function () {
			nums(2).flatMap(function () { return 5; }).toArray();
		});
	`)
}

func TestIteratorReduce(t *testing.T) {
	Expect(t, genPrologue+`
		assert.sameValue(nums(5).reduce(function (a, b) { return a + b; }), 10);
		assert.sameValue(nums(5).reduce(function (a, b) { return a + b; }, 100), 110);
		assert.throws(TypeError, function () {
			nums(0).reduce(function (a, b) { return a + b; });
		}, "empty with no init");
	`)
}

func TestIteratorTerminalMethods(t *testing.T) {
	Expect(t, genPrologue+`
		var seen = [];
		nums(3).forEach(function (x) { seen.push(x); });
		assert.sameValue(seen.join(","), "0,1,2");
		assert.sameValue(nums(5).some(function (x) { return x === 3; }), true);
		assert.sameValue(nums(5).some(function (x) { return x === 9; }), false);
		assert.sameValue(nums(5).every(function (x) { return x < 5; }), true);
		assert.sameValue(nums(5).every(function (x) { return x < 3; }), false);
		assert.sameValue(nums(5).find(function (x) { return x > 2; }), 3);
		assert.sameValue(nums(5).find(function (x) { return x > 9; }), undefined);
	`)
}

func TestIteratorHelperProtoShape(t *testing.T) {
	Expect(t, genPrologue+`
		var h = nums(3).map(function (x) { return x; });
		var proto = Object.getPrototypeOf(h);
		assert.sameValue(proto[Symbol.toStringTag], "Iterator Helper");
		assert.sameValue(typeof proto.next, "function");
		assert.sameValue(typeof proto.return, "function");
		// Helper is itself an Iterator (inherits Iterator.prototype methods).
		assert.sameValue(typeof h.map, "function");
		assert.sameValue(h[Symbol.iterator](), h, "helper @@iterator returns this");
		// next brand check.
		assert.throws(TypeError, function () { proto.next.call({}); });
	`)
}

func TestIteratorLaziness(t *testing.T) {
	Expect(t, genPrologue+`
		var calls = [];
		function* watched() {
			for (let k = 0; k < 5; k++) { calls.push(k); yield k; }
		}
		var it = watched().map(function (x) { return x; });
		assert.sameValue(calls.length, 0, "no work before first next");
		it.next();
		assert.sameValue(calls.join(","), "0", "one source step per next");
		it.next();
		assert.sameValue(calls.join(","), "0,1");
	`)
}

func TestIteratorEarlyReturnClosesSource(t *testing.T) {
	Expect(t, `
		var closed = false;
		var src = {
			i: 0,
			next() { return { value: this.i++, done: false }; },
			return() { closed = true; return { done: true }; },
			[Symbol.iterator]() { return this; }
		};
		Object.setPrototypeOf(src, Iterator.prototype);
		var h = src.map(function (x) { return x; });
		h.next();
		h.return();
		assert.sameValue(closed, true, "return() closes the underlying iterator");
		assert.sameValue(h.next().done, true, "helper is done after return");
	`)
}

func TestIteratorTakeClosesOnLimit(t *testing.T) {
	Expect(t, `
		var closed = 0;
		function make() {
			var i = 0;
			var src = {
				next() { return { value: i++, done: false }; },
				return() { closed++; return { done: true }; }
			};
			Object.setPrototypeOf(src, Iterator.prototype);
			return src;
		}
		var out = make().take(2).toArray();
		assert.sameValue(out.join(","), "0,1");
		assert.sameValue(closed, 1, "take closes source when limit reached");
	`)
}

func TestIteratorCallbackErrorClosesSource(t *testing.T) {
	Expect(t, `
		var closed = false;
		var src = {
			i: 0,
			next() { return { value: this.i++, done: false }; },
			return() { closed = true; return { done: true }; }
		};
		Object.setPrototypeOf(src, Iterator.prototype);
		assert.throws(Error, function () {
			src.map(function () { throw new Error("boom"); }).next();
		});
		assert.sameValue(closed, true, "a throwing mapper closes the source");
	`)
}

func TestIteratorHelperReentrancyGuard(t *testing.T) {
	Expect(t, `
		var loop = 0, enter = 0;
		function* g() { while (true) { loop++; yield; } }
		function mapper() { enter++; iter.next(); }
		var iter = g().map(mapper);
		assert.throws(TypeError, function () { iter.next(); }, "re-entrant next throws");
		assert.sameValue(loop, 1);
		assert.sameValue(enter, 1);
	`)
}

func TestStringIterator(t *testing.T) {
	Expect(t, `
		var it = "abc"[Symbol.iterator]();
		assert.sameValue(it.next().value, "a");
		assert.sameValue(it.next().value, "b");
		assert.sameValue(it.next().value, "c");
		assert.sameValue(it.next().done, true);
		assert.sameValue(Object.getPrototypeOf(it)[Symbol.toStringTag], "String Iterator");
		assert.sameValue([..."hi"].join(","), "h,i");
	`)
}
