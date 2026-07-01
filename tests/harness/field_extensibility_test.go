package harness

import "testing"

// A class field is defined on the instance after its initializer runs, using
// CreateDataPropertyOrThrow (public) or a private-field add (private). Both
// require the target to be extensible, so an initializer that freezes or
// prevents extension of the object makes a subsequent field a TypeError
// (ECMA-262 DefineField / PrivateFieldAdd).
func TestFieldOnNonExtensibleObjectThrows(t *testing.T) {
	Expect(t, `
		assert.throws(TypeError, function () {
			new (class { f = Object.freeze(this); g = "x"; })();
		}, "public field after freezing this");

		assert.throws(TypeError, function () {
			new (class { f = Object.freeze(this); })();
		}, "the freezing field itself cannot be defined afterwards");

		assert.throws(TypeError, function () {
			new (class { #g = (Object.preventExtensions(this), "x"); })();
		}, "private instance field on a non-extensible object");

		assert.throws(TypeError, function () {
			(class { static #g = (Object.preventExtensions(this), "x"); });
		}, "private static field on a non-extensible constructor");
	`)
}

func TestFieldsOnExtensibleObjectsWork(t *testing.T) {
	Expect(t, `
		class C { a = 1; #b = 2; sum() { return this.a + this.#b; } }
		assert.sameValue(new C().sum(), 3, "ordinary public and private fields");

		class D { f = Object.freeze(this); }
		assert.throws(TypeError, function () { new D(); },
			"even a lone freezing field cannot then define itself");

		class E { a = 1; b = 2; }
		var e = new E();
		assert.sameValue(e.a + e.b, 3, "multiple ordinary fields");
	`)
}
