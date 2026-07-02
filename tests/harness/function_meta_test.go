package harness

import "testing"

// TestFunctionNameLengthDescriptors verifies that a function's "name" and
// "length" own properties are non-writable, non-enumerable, and configurable
// (ECMA-262 sec-function-instances-length / sec-function-instances-name), and
// that "length" is enumerated before "name".
func TestFunctionNameLengthDescriptors(t *testing.T) {
	Expect(t, `
		function decl(a, b) {}
		var d = Object.getOwnPropertyDescriptor(decl, "length");
		assert.sameValue(d.value, 2, "length value");
		assert.sameValue(d.writable, false, "length not writable");
		assert.sameValue(d.enumerable, false, "length not enumerable");
		assert.sameValue(d.configurable, true, "length configurable");

		var n = Object.getOwnPropertyDescriptor(decl, "name");
		assert.sameValue(n.value, "decl", "name value");
		assert.sameValue(n.writable, false, "name not writable");
		assert.sameValue(n.enumerable, false, "name not enumerable");
		assert.sameValue(n.configurable, true, "name configurable");

		// Inferred name (anonymous expression assigned to a variable) too.
		var f = function(){};
		var fn = Object.getOwnPropertyDescriptor(f, "name");
		assert.sameValue(fn.value, "f", "inferred name value");
		assert.sameValue(fn.writable, false, "inferred name not writable");

		// length precedes name in own-key order.
		var keys = Object.getOwnPropertyNames(decl);
		assert(keys.indexOf("length") < keys.indexOf("name"), "length before name");

		// Class and method functions carry the same descriptors.
		var o = { m(x){} };
		var mn = Object.getOwnPropertyDescriptor(o.m, "name");
		assert.sameValue(mn.writable, false, "method name not writable");
		assert.sameValue(mn.value, "m", "method name value");
	`)
}
