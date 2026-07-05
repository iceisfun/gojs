package harness

import "testing"

// TestFunctionRestrictedProperties covers the poison-pill "caller" and
// "arguments" accessors inherited from %Function.prototype%
// (AddRestrictedFunctionProperties, ECMA-262).
func TestFunctionRestrictedProperties(t *testing.T) {
	// A strict IIFE, not a leading program directive: Run prepends the assert
	// prologue, so a "use strict" at the top of this snippet would no longer be in
	// the directive prologue (and correctly would not make the program strict). A
	// function-body directive is robust to that prepending.
	Expect(t, `(function () {
		"use strict";
		// Strict functions do not have own caller/arguments; they inherit the
		// poison-pill accessors from %Function.prototype%.
		function target() {}
		assert.sameValue(target.hasOwnProperty("caller"), false, "strict no own caller");
		assert.sameValue(target.hasOwnProperty("arguments"), false, "strict no own arguments");
		assert.throws(TypeError, function(){ return target.caller; }, "get caller throws");
		assert.throws(TypeError, function(){ target.caller = {}; }, "set caller throws");
		assert.throws(TypeError, function(){ return target.arguments; }, "get arguments throws");
		assert.throws(TypeError, function(){ target.arguments = {}; }, "set arguments throws");

		// Bound functions inherit the poison pills too, without own copies.
		var bound = target.bind(null);
		assert.sameValue(bound.hasOwnProperty("caller"), false, "bound no own caller");
		assert.throws(TypeError, function(){ return bound.caller; }, "bound get caller throws");

		// Strict dynamic functions likewise.
		var nf = new Function('"use strict"');
		assert.sameValue(nf.hasOwnProperty("caller"), false, "dyn no own caller");
		assert.throws(TypeError, function(){ return nf.arguments; }, "dyn get arguments throws");

		// The caller and arguments poison accessors share one %ThrowTypeError%.
		var c = Object.getOwnPropertyDescriptor(Function.prototype, "caller");
		var a = Object.getOwnPropertyDescriptor(Function.prototype, "arguments");
		assert.sameValue(c.get, a.get, "shared ThrowTypeError getter");
		assert.sameValue(c.get, c.set, "getter and setter are the same");
	})();`)
	// Sloppy plain functions carry Annex B legacy caller/arguments returning null.
	Expect(t, `
		function sloppy() {}
		assert.sameValue(sloppy.hasOwnProperty("caller"), true, "sloppy own caller");
		assert.sameValue(sloppy.caller, null, "sloppy caller null");
		assert.sameValue(sloppy.arguments, null, "sloppy arguments null");
		// Generators and methods do NOT get the legacy own properties.
		function* gen() {}
		assert.sameValue(gen.hasOwnProperty("caller"), false, "generator no own caller");
		var o = { m(){} };
		assert.sameValue(o.m.hasOwnProperty("caller"), false, "method no own caller");
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
