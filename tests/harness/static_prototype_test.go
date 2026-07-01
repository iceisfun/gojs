package harness

import "testing"

// No static class element may be named "prototype". A non-computed name is a
// parse-time SyntaxError; a computed name that evaluates to "prototype" is a
// TypeError thrown while the class is defined (ECMA-262 ClassDefinitionEvaluation).
func TestStaticComputedPrototypeThrows(t *testing.T) {
	Expect(t, `
		assert.throws(TypeError, function () { class C { static ["prototype"] = 1; } },
			"static computed field named prototype");
		assert.throws(TypeError, function () { class C { static ["prototype"]() {} } },
			"static computed method named prototype");
		assert.throws(TypeError, function () { class C { static get ["prototype"]() {} } },
			"static computed getter named prototype");
	`)
}

func TestStaticNonPrototypeAllowed(t *testing.T) {
	Expect(t, `
		class A { static ["prot" + "o"] = 1; }
		assert.sameValue(A.proto, 1, "a different computed static name is fine");
		class B { prototype() { return 5; } }
		assert.sameValue(new B().prototype(), 5, "an instance method named prototype is fine");
		class C { static ["x"] = 9; }
		assert.sameValue(C.x, 9, "an ordinary computed static name is fine");
	`)
}
