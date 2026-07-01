package harness

import "testing"

// Comprehensive var/let/const binding-semantics suite (per the Variable Binding
// Semantics SoW). Each test executes real JavaScript and asserts observable
// behavior; ECMAScript references are noted where behavior is non-obvious.
// Parse-time errors (redeclaration, const-without-init) surface as thrown
// SyntaxError values, so ExpectError catches them.

// --- 1. var hoisting -------------------------------------------------------

func TestVarHoisting(t *testing.T) {
	Expect(t, `
		// Declaration is hoisted; initialization is not: the read sees undefined.
		assert.sameValue(x, undefined, "var read before init is undefined");
		var x = 5;
		assert.sameValue(x, 5);
	`)
	// A var inside a function is hoisted to the function's top.
	Expect(t, `
		function f() { assert.sameValue(y, undefined); var y = 2; return y; }
		assert.sameValue(f(), 2);
	`)
}

// --- 2. let / const Temporal Dead Zone -------------------------------------

func TestLetConstTDZ(t *testing.T) {
	// Reading a let/const before initialization throws ReferenceError.
	Expect(t, `
		assert.throws(ReferenceError, function () { { x; let x = 1; } });
		assert.throws(ReferenceError, function () { { y; const y = 1; } });
	`)
	// typeof does NOT bypass the TDZ for a lexically-declared name.
	Expect(t, `
		assert.throws(ReferenceError, function () { { typeof t; let t = 1; } });
	`)
	// A write before initialization also throws.
	Expect(t, `
		assert.throws(ReferenceError, function () { { w = 2; let w = 1; } });
	`)
}

// --- 3. function scope vs block scope --------------------------------------

func TestVarFunctionScopedLetBlockScoped(t *testing.T) {
	// var leaks out of a block into the enclosing function/global scope.
	Expect(t, `
		if (true) { var x = 1; }
		assert.sameValue(x, 1, "var is function-scoped, not block-scoped");
	`)
	// let does not escape its block.
	Expect(t, `
		if (true) { let y = 1; }
		assert.throws(ReferenceError, function () { return y; });
	`)
	// A var in a loop body is visible after the loop.
	Expect(t, `
		for (var i = 0; i < 3; i++) {}
		assert.sameValue(i, 3);
	`)
}

// --- 4. redeclaration rules ------------------------------------------------

func TestRedeclarationVarAllowed(t *testing.T) {
	Expect(t, `var x = 1; var x = 2; assert.sameValue(x, 2);`)
	Expect(t, `var x; var x; assert.sameValue(typeof x, "undefined");`)
}

func TestRedeclarationLetRejected(t *testing.T) {
	ExpectError(t, `let x; let x;`, "SyntaxError")
	ExpectError(t, `let x = 1; let x = 2;`, "SyntaxError")
}

func TestRedeclarationConstRejected(t *testing.T) {
	ExpectError(t, `const x = 1; const x = 2;`, "SyntaxError")
}

func TestRedeclarationMixedRejected(t *testing.T) {
	ExpectError(t, `var x; let x;`, "SyntaxError")
	ExpectError(t, `let x; var x;`, "SyntaxError")
	ExpectError(t, `const x = 1; var x;`, "SyntaxError")
}

// --- 5. const initialization -----------------------------------------------

func TestConstInitialization(t *testing.T) {
	Expect(t, `const x = 1; assert.sameValue(x, 1);`)
}

func TestConstWithoutInitializerRejected(t *testing.T) {
	ExpectError(t, `const x;`, "SyntaxError")
}

// --- 6. const assignment ---------------------------------------------------

func TestConstReassignmentThrows(t *testing.T) {
	Expect(t, `
		assert.throws(TypeError, function () { const x = 1; x = 2; });
	`)
}

func TestConstObjectMutationAllowed(t *testing.T) {
	Expect(t, `
		const obj = {};
		obj.value = 5;              // mutating the object is fine
		assert.sameValue(obj.value, 5);
		const arr = [];
		arr.push(1);
		assert.sameValue(arr.length, 1);
	`)
}

// --- 7. global binding semantics -------------------------------------------

func TestGlobalVarBecomesGlobalProperty(t *testing.T) {
	// A top-level `var` (and function declaration) creates a property on the
	// global object; `let`/`const`/`class` do not.
	Expect(t, `
		var x = 1;
		function fn() {}
		assert.sameValue(globalThis.x, 1, "var creates globalThis.x");
		assert.sameValue(typeof globalThis.fn, "function", "function decl creates globalThis.fn");
		let y = 2;
		const z = 3;
		class C {}
		assert.sameValue(globalThis.y, undefined, "let does not create a global property");
		assert.sameValue(globalThis.z, undefined, "const does not create a global property");
		assert.sameValue(globalThis.C, undefined, "class does not create a global property");
	`)
}

// --- 8. delete semantics ---------------------------------------------------

func TestDeleteVarReturnsFalse(t *testing.T) {
	// A `var`-declared global is non-configurable: delete returns false and the
	// binding survives.
	Expect(t, `
		var x = 1;
		assert.sameValue(delete x, false, "cannot delete a var binding");
		assert.sameValue(x, 1);
	`)
}

func TestDeleteConfigurableGlobalProperty(t *testing.T) {
	// A property assigned to globalThis (not via var) is configurable.
	Expect(t, `
		globalThis.p = 7;
		assert.sameValue(delete globalThis.p, true);
		assert.sameValue(typeof globalThis.p, "undefined");
	`)
}

// --- 9. closure capture ----------------------------------------------------

func TestClosureCaptureVar(t *testing.T) {
	Expect(t, `
		var f = [];
		for (var i = 0; i < 3; i++) f.push(function () { return i; });
		assert.sameValue(f[0]() + "," + f[1]() + "," + f[2](), "3,3,3");
	`)
}

func TestClosureCaptureLet(t *testing.T) {
	Expect(t, `
		var f = [];
		for (let i = 0; i < 3; i++) f.push(function () { return i; });
		assert.sameValue(f[0]() + "," + f[1]() + "," + f[2](), "0,1,2");
	`)
}

// --- 10. per-iteration lexical environments --------------------------------

func TestPerIterationLetBindings(t *testing.T) {
	// Each iteration gets an independent `let` binding, even with break/continue
	// and nested loops.
	Expect(t, `
		var fns = [];
		outer:
		for (let a = 0; a < 3; a++) {
			for (let b = 0; b < 3; b++) {
				if (b === 2) continue;
				fns.push(function () { return a + "" + b; });
			}
			if (a === 2) break outer;
		}
		var got = fns.map(function (f) { return f(); }).join(",");
		assert.sameValue(got, "00,01,10,11,20,21");
	`)
}

// --- 11. function declarations vs var/let ----------------------------------

func TestFunctionDeclarationHoisting(t *testing.T) {
	Expect(t, `
		assert.sameValue(f(), 1, "function declarations are fully hoisted");
		function f() { return 1; }
	`)
	// Block-level function declaration is visible in its block.
	Expect(t, `
		{
			function g() { return 2; }
			assert.sameValue(g(), 2);
		}
	`)
}

// --- 12. catch bindings ----------------------------------------------------

func TestCatchBindingScope(t *testing.T) {
	Expect(t, `
		var e = "outer";
		try { throw "inner"; } catch (e) { assert.sameValue(e, "inner"); }
		assert.sameValue(e, "outer", "catch parameter is block-scoped");
	`)
	// Optional catch binding.
	Expect(t, `
		var ran = false;
		try { throw 1; } catch { ran = true; }
		assert.sameValue(ran, true);
	`)
}

// --- 13. shadowing ---------------------------------------------------------

func TestShadowing(t *testing.T) {
	Expect(t, `
		let x = 1;
		{ let x = 2; assert.sameValue(x, 2, "inner shadows outer"); }
		assert.sameValue(x, 1, "outer is restored after the block");
	`)
	Expect(t, `
		const a = 10;
		function f() { const a = 20; return a; }
		assert.sameValue(f(), 20);
		assert.sameValue(a, 10);
	`)
}

// --- 14. hoisting order ----------------------------------------------------

func TestHoistingOrder(t *testing.T) {
	// A function declaration and a var of the same name: the function wins after
	// hoisting; a later var assignment overrides the value.
	Expect(t, `
		assert.sameValue(typeof h, "function");
		var h = 5;
		function h() {}
		assert.sameValue(h, 5);
	`)
}

// --- 15. destructuring bindings --------------------------------------------

func TestDestructuringBindings(t *testing.T) {
	Expect(t, `
		var [a, b = 9] = [1];
		assert.sameValue(a, 1); assert.sameValue(b, 9);
		let { p, q: r } = { p: 2, q: 3 };
		assert.sameValue(p, 2); assert.sameValue(r, 3);
		const { nested: { deep = 4 } = {} } = {};
		assert.sameValue(deep, 4);
		const [x, ...rest] = [1, 2, 3, 4];
		assert.sameValue(x, 1); assert.sameValue(rest.join(","), "2,3,4");
	`)
	// const destructuring binds each name as const (reassignment throws).
	Expect(t, `
		assert.throws(TypeError, function () { const [c] = [1]; c = 2; });
	`)
}

// --- 16. binding lifetime --------------------------------------------------

func TestBindingLifetimeClosureRetainsAfterBlock(t *testing.T) {
	Expect(t, `
		function make() {
			let captured = 42;
			return function () { return captured; };
		}
		var g = make();
		assert.sameValue(g(), 42, "closure retains the block binding after exit");
	`)
}
