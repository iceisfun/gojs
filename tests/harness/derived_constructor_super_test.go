package harness

import "testing"

// A derived class constructor must call super() before it can use `this` or
// return normally. Completing (or returning a non-object) without having called
// super() leaves the `this` binding uninitialized, a ReferenceError
// (ECMA-262 GetThisBinding on an uninitialized environment record). Returning an
// object from the constructor is allowed and bypasses `this` entirely.

func TestDerivedConstructorRequiresSuper(t *testing.T) {
	Expect(t, `
		class E extends Error { constructor() {} }
		assert.throws(ReferenceError, function () { new E(); },
			"constructor that never calls super()");
	`)
}

func TestDerivedConstructorPrimitiveReturnRequiresSuper(t *testing.T) {
	Expect(t, `
		class E extends Error { constructor() { return 5; } }
		assert.throws(ReferenceError, function () { new E(); },
			"primitive return without super()");
	`)
}

func TestDerivedConstructorObjectReturnBypassesSuper(t *testing.T) {
	Expect(t, `
		var sentinel = { tag: "ok" };
		class E extends Error { constructor() { return sentinel; } }
		assert.sameValue(new E(), sentinel, "object return replaces this without super()");
	`)
}

func TestDerivedConstructorThisBeforeSuper(t *testing.T) {
	Expect(t, `
		class E extends Error { constructor() { this.x = 1; super(); } }
		assert.throws(ReferenceError, function () { new E(); },
			"reading this before super()");
	`)
}

func TestDerivedConstructorSuperOk(t *testing.T) {
	Expect(t, `
		class E extends Error { constructor(m) { super(m); this.tag = "t"; } }
		var e = new E("hi");
		assert.sameValue(e.message, "hi", "super() forwarded the argument");
		assert.sameValue(e.tag, "t", "this usable after super()");

		class F extends Error {} // default derived constructor calls super(...args)
		assert.sameValue(new F("yo").message, "yo", "default derived constructor");
	`)
}

func TestBaseConstructorNeedsNoSuper(t *testing.T) {
	Expect(t, `
		class B { constructor() {} }
		assert.sameValue(new B() instanceof B, true, "base class needs no super()");
	`)
}
