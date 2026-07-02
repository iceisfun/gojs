package harness

import "testing"

// TestFunctionRestrictedProperties covers the poison-pill "caller" and
// "arguments" accessors inherited from %Function.prototype%
// (AddRestrictedFunctionProperties, ECMA-262).
func TestFunctionRestrictedProperties(t *testing.T) {
	Expect(t, `
		function target() {}
		// Ordinary functions do not have own caller/arguments.
		assert.sameValue(target.hasOwnProperty("caller"), false, "no own caller");
		assert.sameValue(target.hasOwnProperty("arguments"), false, "no own arguments");
		// Reading and writing throw a TypeError via the inherited poison accessor.
		assert.throws(TypeError, function(){ return target.caller; }, "get caller throws");
		assert.throws(TypeError, function(){ target.caller = {}; }, "set caller throws");
		assert.throws(TypeError, function(){ return target.arguments; }, "get arguments throws");
		assert.throws(TypeError, function(){ target.arguments = {}; }, "set arguments throws");

		// Bound functions inherit them too, without own copies.
		var bound = target.bind(null);
		assert.sameValue(bound.hasOwnProperty("caller"), false, "bound no own caller");
		assert.throws(TypeError, function(){ return bound.caller; }, "bound get caller throws");

		// Dynamic functions likewise.
		var nf = new Function('"use strict"');
		assert.sameValue(nf.hasOwnProperty("caller"), false, "dyn no own caller");
		assert.throws(TypeError, function(){ return nf.arguments; }, "dyn get arguments throws");
	`)
}

// TestFunctionHasInstance covers Function.prototype[Symbol.hasInstance]:
// its property descriptor and the TypeError when prototype is non-object.
func TestFunctionHasInstance(t *testing.T) {
	Expect(t, `
		var d = Object.getOwnPropertyDescriptor(Function.prototype, Symbol.hasInstance);
		assert.sameValue(d.writable, false, "hasInstance not writable");
		assert.sameValue(d.enumerable, false, "hasInstance not enumerable");
		assert.sameValue(d.configurable, false, "hasInstance not configurable");

		var f = function(){};
		f.prototype = undefined;
		assert.throws(TypeError, function(){ f[Symbol.hasInstance]({}); }, "non-object prototype throws");
		f.prototype = 86;
		assert.throws(TypeError, function(){ ({}) instanceof f; }, "instanceof with non-object prototype throws");
	`)
}
