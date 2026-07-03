package harness

import "testing"

// Function.prototype is itself a built-in function object, so it owns "length"
// (0) and "name" ("") data properties with { writable:false, enumerable:false,
// configurable:true }, and "length" appears immediately before "name" in
// own-property order (sec-createbuiltinfunction).
func TestFunctionPrototypeOwnLengthName(t *testing.T) {
	Expect(t, `
		assert(Function.prototype.hasOwnProperty("length"), "length is own");
		assert(Function.prototype.hasOwnProperty("name"), "name is own");
		assert.sameValue(Function.prototype.length, 0, "length value");
		assert.sameValue(Function.prototype.name, "", "name value");

		var ld = Object.getOwnPropertyDescriptor(Function.prototype, "length");
		assert.sameValue(ld.writable, false, "length writable");
		assert.sameValue(ld.enumerable, false, "length enumerable");
		assert.sameValue(ld.configurable, true, "length configurable");

		var nd = Object.getOwnPropertyDescriptor(Function.prototype, "name");
		assert.sameValue(nd.writable, false, "name writable");
		assert.sameValue(nd.enumerable, false, "name enumerable");
		assert.sameValue(nd.configurable, true, "name configurable");

		var names = Object.getOwnPropertyNames(Function.prototype);
		var li = names.indexOf("length");
		var ni = names.indexOf("name");
		assert(li >= 0 && ni === li + 1, "length comes immediately before name");
	`)
}

// A built-in function orders "length" before "name" in own-property order.
func TestBuiltinFunctionLengthBeforeName(t *testing.T) {
	Expect(t, `
		var names = Object.getOwnPropertyNames(Array.prototype.map);
		assert.sameValue(names[0], "length", "length first");
		assert.sameValue(names[1], "name", "name second");
	`)
}

// Function.prototype.bind reads the target's "name" via Get (honoring a
// redefined "name" own property) and prefixes it with "bound "; the result is
// an own, non-writable, non-enumerable, configurable property.
func TestBindName(t *testing.T) {
	Expect(t, `
		var target = Object.defineProperty(function() {}, "name", { value: "target" });
		assert.sameValue(target.bind().name, "bound target", "single bind");
		assert.sameValue(target.bind().bind().name, "bound bound target", "chained bind");

		var b = target.bind();
		assert(b.hasOwnProperty("name"), "name is own");
		var d = Object.getOwnPropertyDescriptor(b, "name");
		assert.sameValue(d.writable, false, "not writable");
		assert.sameValue(d.enumerable, false, "not enumerable");
		assert.sameValue(d.configurable, true, "configurable");

		var anon = function() {};
		Object.defineProperty(anon, "name", { value: 42 });
		assert.sameValue(anon.bind().name, "bound ", "non-string name coerces to empty");
	`)
}

// An abrupt completion from reading the target's "name" propagates out of bind.
func TestBindNameError(t *testing.T) {
	Expect(t, `
		var target = Object.defineProperty(function() {}, "name", {
			get: function() { throw new Test262Error(); }
		});
		assert.throws(Test262Error, function() { target.bind(); });
	`)
}

// SetFunctionLength: a bound function's length uses the target's OWN, numeric
// "length" minus the bound-argument count (floored at 0); +Infinity is
// preserved, -Infinity and non-own/non-number lengths yield 0.
func TestBindLength(t *testing.T) {
	Expect(t, `
		function fn(a, b, c) {}
		assert.sameValue(fn.bind().length, 3, "no bound args");
		assert.sameValue(fn.bind(null, 1).length, 2, "one bound arg");
		assert.sameValue(fn.bind(null, 1, 2, 3, 4).length, 0, "over-bound floors at 0");

		Object.defineProperty(fn, "length", { value: Infinity });
		assert.sameValue(fn.bind().length, Infinity, "infinity preserved");
		assert.sameValue(fn.bind(0, 0).length, Infinity, "infinity minus args stays infinity");

		Object.defineProperty(fn, "length", { value: -Infinity });
		assert.sameValue(fn.bind().length, 0, "negative infinity floors to 0");

		Object.defineProperty(fn, "length", { value: 3.66 });
		assert.sameValue(fn.bind().length, 3, "ToInteger truncates");

		Object.defineProperty(fn, "length", { value: "5" });
		assert.sameValue(fn.bind().length, 0, "non-number length ignored");

		// Non-own length (inherited from the prototype) is ignored.
		function bar() {}
		Object.setPrototypeOf(bar, { length: 42 });
		delete bar.length;
		assert.sameValue(Function.prototype.bind.call(bar, null, 1).length, 0, "non-own length ignored");
	`)
}

// OrdinaryHasInstance delegates a bound function's [[BoundTargetFunction]] so
// `instanceof` (and Symbol.hasInstance) consult the target's prototype chain.
func TestBoundInstanceof(t *testing.T) {
	Expect(t, `
		var BC = function() {};
		var bc = new BC();
		var bound = BC.bind();
		assert.sameValue(bound[Symbol.hasInstance](bc), true, "hasInstance on bound");
		assert.sameValue(bc instanceof bound, true, "instanceof bound");
		assert.sameValue(bc instanceof bound.bind(), true, "instanceof doubly-bound");
		assert.sameValue({} instanceof bound, false, "non-instance");
	`)
}

// instanceof throws a TypeError when the constructor's "prototype" is not an
// object (OrdinaryHasInstance).
func TestInstanceofNonObjectPrototype(t *testing.T) {
	Expect(t, `
		function F() {}
		F.prototype = 1;
		assert.throws(TypeError, function() { ({}) instanceof F; });
	`)
}

// Function.prototype.apply propagates an abrupt completion from reading the
// array-like argument list's "length" or an index (CreateListFromArrayLike).
func TestApplyAbruptArgumentList(t *testing.T) {
	Expect(t, `
		var lenThrows = { get length() { throw new Test262Error(); } };
		assert.throws(Test262Error, function() {
			(function() {}).apply(null, lenThrows);
		}, "abrupt length");

		var indexThrows = { length: 2, 0: 0, get 1() { throw new Test262Error(); } };
		assert.throws(Test262Error, function() {
			(function() {}).apply(null, indexThrows);
		}, "abrupt index");
	`)
}
