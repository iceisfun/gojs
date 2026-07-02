package harness

import "testing"

// TestFunctionConstructorBasic covers the dynamic Function constructor
// (CreateDynamicFunction, ECMA-262 sec-createdynamicfunction).
func TestFunctionConstructorBasic(t *testing.T) {
	Expect(t, `
		// Body only.
		var f = new Function("return 1 + 2;");
		assert.sameValue(f(), 3, "body-only function runs");

		// Parameters as separate arguments.
		var add = new Function("a", "b", "return a + b;");
		assert.sameValue(add(4, 5), 9, "two-parameter function");
		assert.sameValue(add.length, 2, "length reflects parameter count");

		// Parameters may be comma-joined in a single argument.
		var add2 = new Function("a, b", "return a + b;");
		assert.sameValue(add2(6, 7), 13, "comma-joined parameters");
		assert.sameValue(add2.length, 2, "comma-joined length");

		// Name is always "anonymous".
		assert.sameValue(add.name, "anonymous", "name is anonymous");

		// Called without new, behaves the same.
		var g = Function("return 42;");
		assert.sameValue(g(), 42, "callable without new");

		// No arguments -> empty function returning undefined.
		var empty = new Function();
		assert.sameValue(empty(), undefined, "empty function returns undefined");
		assert.sameValue(empty.length, 0, "empty function length 0");

		// The created function is a constructor with a prototype.
		assert(typeof new Function("").prototype === "object", "has prototype");
	`)
}

// TestFunctionConstructorStrict verifies that a "use strict" directive in the
// dynamic function body makes the function strict.
func TestFunctionConstructorStrict(t *testing.T) {
	Expect(t, `
		var f = new Function("'use strict'; return this;");
		assert.sameValue(f.call(undefined), undefined, "strict this is undefined");
		var g = new Function("return this;");
		assert(g.call(undefined) !== undefined, "sloppy this is boxed to global");
	`)
}

// TestFunctionConstructorSyntaxErrors verifies bad params/body throw SyntaxError,
// including the classic comment-injection case which must be rejected because
// parameters and body are parsed separately.
func TestFunctionConstructorSyntaxErrors(t *testing.T) {
	ExpectError(t, `new Function("a", "b", "return a +");`, "SyntaxError")
	ExpectError(t, `new Function("a b c", "return 1;");`, "SyntaxError")
	// Comment-injection: params "/*" and body "*/ ) {" must each be invalid alone.
	ExpectError(t, `new Function("/*", "*/ ) {");`, "SyntaxError")
}
