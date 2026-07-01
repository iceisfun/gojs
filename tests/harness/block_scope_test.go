package harness

import "testing"

// Block-scope, lexical-environment, and TDZ semantics suite (per the Block Scope
// and Lexical Environment Semantics SoW). It extends var_let_const_test.go and
// scoping_test.go rather than duplicating them, going deeper on nested blocks,
// per-iteration bindings, the Temporal Dead Zone, and — the bulk of it — the
// block-level "early errors" that must be reported as SyntaxError at parse time
// (lexical redeclaration and function declarations in single-statement
// positions). Each early-error case was first written as a failing regression
// test against the engine's too-lenient parser, then fixed in parser/.
//
// Parse-time SyntaxErrors surface through RunString as thrown SyntaxError
// values, so ExpectError(..., "SyntaxError") catches them. Strict-mode-only
// early errors are exercised by wrapping the construct in a function whose body
// opens with a "use strict" directive prologue.

// --- 1. basic block scope --------------------------------------------------

func TestBlockScopeBasic(t *testing.T) {
	Expect(t, `
		let outer = 1;
		{
			let inner = 2;
			assert.sameValue(outer, 1, "block sees enclosing let");
			assert.sameValue(inner, 2, "block sees own let");
		}
		assert.throws(ReferenceError, function () { return inner; },
			"inner block let not visible after the block");
	`)
}

// --- 2. var vs block: a bare block does not scope var ----------------------

func TestBlockScopeVarLeaksBlockLetDoesNot(t *testing.T) {
	Expect(t, `
		function f() {
			{ var v = 10; let l = 20; }
			assert.sameValue(v, 10, "var leaks out of a bare block");
			assert.throws(ReferenceError, function () { return l; },
				"let does not leak out of a bare block");
		}
		f();
	`)
}

// --- 3. nested blocks: independent scopes at each depth --------------------

func TestBlockScopeNestedIndependentScopes(t *testing.T) {
	Expect(t, `
		let x = "a";
		{
			let x = "b";
			{
				let x = "c";
				assert.sameValue(x, "c", "deepest block binding wins");
			}
			assert.sameValue(x, "b", "middle block binding restored");
		}
		assert.sameValue(x, "a", "outer binding restored");
	`)
}

// --- 4. shadowing across binding forms (let/const/class/function) ----------

func TestBlockScopeShadowingForms(t *testing.T) {
	Expect(t, `
		function fn() { return "outer-fn"; }
		let captured;
		{
			class fn { static tag() { return "inner-class"; } }
			assert.sameValue(fn.tag(), "inner-class", "class shadows outer function in block");
			captured = fn;
		}
		assert.sameValue(fn(), "outer-fn", "outer function restored after block");

		const c = 1;
		{ const c = 2; assert.sameValue(c, 2, "const shadows const"); }
		assert.sameValue(c, 1, "outer const unchanged");
	`)
}

// --- 5. Temporal Dead Zone: read, typeof, and write before init -----------

func TestBlockScopeTDZVariants(t *testing.T) {
	// typeof does not exempt a lexical name from the TDZ.
	Expect(t, `
		assert.throws(ReferenceError, function () { { typeof t; let t = 1; } },
			"typeof of a let in its TDZ still throws");
	`)
	// A write before initialization throws too.
	Expect(t, `
		assert.throws(ReferenceError, function () { { w = 5; let w; } },
			"assignment to a let in its TDZ throws");
	`)
	// The TDZ ends exactly at the initializer, not at the end of the block.
	Expect(t, `
		{
			let a = 1;
			assert.sameValue(a, 1, "readable immediately after initialization");
		}
	`)
}

// --- 6. self-referential initializer is in the TDZ (let a = a) -------------

func TestBlockScopeSelfReferenceTDZ(t *testing.T) {
	Expect(t, `
		assert.throws(ReferenceError, function () { let a = a; },
			"let a = a reads a while a is in its TDZ");
		assert.throws(ReferenceError, function () { const b = b + 1; },
			"const b = b + 1 reads b in its TDZ");
	`)
}

// --- 7. const semantics inside blocks --------------------------------------

func TestBlockScopeConstSemantics(t *testing.T) {
	Expect(t, `
		{
			const k = { n: 1 };
			k.n = 2;                       // mutation is allowed
			assert.sameValue(k.n, 2);
			assert.throws(TypeError, function () { k = {}; }, "rebinding const throws");
		}
	`)
}

// --- 8. redeclaration: let/const/class conflict with each other (block) ----

func TestBlockRedeclarationLexicalDuplicatesReject(t *testing.T) {
	ExpectError(t, `{ let f; let f; }`, "SyntaxError")
	ExpectError(t, `{ const f = 0; const f = 1; }`, "SyntaxError")
	ExpectError(t, `{ let f; const f = 0; }`, "SyntaxError")
	ExpectError(t, `{ class f {} const f = 0; }`, "SyntaxError")
	ExpectError(t, `{ let f; class f {} }`, "SyntaxError")
}

// --- 9. redeclaration: lexical name conflicts with a var (block) -----------

func TestBlockRedeclarationLexicalVsVarReject(t *testing.T) {
	ExpectError(t, `{ let f; var f; }`, "SyntaxError")
	ExpectError(t, `{ var f; let f; }`, "SyntaxError")
	ExpectError(t, `{ const f = 0; var f; }`, "SyntaxError")
	ExpectError(t, `{ var f; const f = 0; }`, "SyntaxError")
	ExpectError(t, `{ class f {} var f; }`, "SyntaxError")
	ExpectError(t, `{ var f; class f {} }`, "SyntaxError")
}

// --- 10. redeclaration: a block-level function name is a lexical name ------

func TestBlockRedeclarationFunctionNameIsLexical(t *testing.T) {
	// A FunctionDeclaration in a block binds a lexical name, so it collides with
	// let/const/class and with a var (in both strict and sloppy code).
	ExpectError(t, `{ let f; function f() {} }`, "SyntaxError")
	ExpectError(t, `{ function f() {} let f; }`, "SyntaxError")
	ExpectError(t, `{ function f() {} var f; }`, "SyntaxError")
	ExpectError(t, `{ var f; function f() {} }`, "SyntaxError")
	ExpectError(t, `{ class f {} function f() {} }`, "SyntaxError")
}

// --- 11. redeclaration: generator / async names never merge ---------------

func TestBlockRedeclarationAsyncGeneratorReject(t *testing.T) {
	// The Annex B "two plain FunctionDeclarations" relaxation does not extend to
	// generators or async functions: these always duplicate-conflict.
	ExpectError(t, `{ function* f() {} function* f() {} }`, "SyntaxError")
	ExpectError(t, `{ async function f() {} async function f() {} }`, "SyntaxError")
	ExpectError(t, `{ function f() {} function* f() {} }`, "SyntaxError")
	ExpectError(t, `{ function f() {} async function f() {} }`, "SyntaxError")
}

// --- 12. redeclaration: var hoisted through inner blocks conflicts ---------

func TestBlockRedeclarationInnerBlockVarBubbles(t *testing.T) {
	// A `var` nested in an inner block is a VarDeclaredName of the outer block,
	// so it collides with the outer block's lexical name.
	ExpectError(t, `{ let f; { var f; } }`, "SyntaxError")
	ExpectError(t, `{ { var f; } let f; }`, "SyntaxError")
	ExpectError(t, `{ function f() {} { var f; } }`, "SyntaxError")
	// ...but a var nested inside a function does NOT bubble past the function.
	ExpectError(t, `
		function g() {
			{
				function f() {}
				{ var f; }
			}
		}
	`, "SyntaxError")
}

// --- 13. redeclaration positives: what must NOT throw ----------------------

func TestBlockRedeclarationLegalCases(t *testing.T) {
	// Plain var/var in a block is legal.
	Expect(t, `{ var f; var f; assert.sameValue(typeof f, "undefined"); }`)
	Expect(t, `{ { var f; } var f; }`)
	// Two plain FunctionDeclarations in a block are legal in sloppy mode (Annex B).
	Expect(t, `{ function f() { return 1; } function f() { return 2; } assert.sameValue(f(), 2); }`)
	// The same name may be lexically bound in sibling (non-nested) blocks.
	Expect(t, `{ let f; } { let f; }`)
}

// --- 14. strict mode tightens the duplicate-function relaxation ------------

func TestBlockRedeclarationStrictDuplicateFunctionReject(t *testing.T) {
	// In strict code, even two plain FunctionDeclarations duplicate-conflict.
	ExpectError(t, `function s() { "use strict"; { function f() {} function f() {} } }`, "SyntaxError")
	// A class body is always strict, so a method's block is strict too.
	ExpectError(t, `class C { m() { { function f() {} function f() {} } } }`, "SyntaxError")
	// eval of strict source parses under strict early errors.
	Expect(t, `
		assert.throws(SyntaxError, function () {
			eval('"use strict"; { function f() {} function f() {} }');
		}, "strict eval rejects duplicate block functions");
	`)
}

// --- 15. function declarations in single-statement positions --------------

func TestFunctionDeclarationStatementPositionReject(t *testing.T) {
	// A FunctionDeclaration is never allowed as the sole body of an iteration
	// statement (Annex B relaxes only if/else and labels), in either mode.
	ExpectError(t, `do function g() {} while (false);`, "SyntaxError")
	ExpectError(t, `while (false) function g() {}`, "SyntaxError")
	ExpectError(t, `for (;false;) function g() {}`, "SyntaxError")
	ExpectError(t, `for (var i in {}) function g() {}`, "SyntaxError")
	// In strict code it is also forbidden as an if/else clause body.
	ExpectError(t, `function s() { "use strict"; if (true) function g() {} }`, "SyntaxError")
	ExpectError(t, `function s() { "use strict"; if (true) {} else function g() {} }`, "SyntaxError")
	// A generator/async declaration is never permitted, even for if in sloppy.
	ExpectError(t, `if (true) function* g() {}`, "SyntaxError")
	ExpectError(t, `if (true) async function g() {}`, "SyntaxError")
	// Lexical declarations are never valid in a single-statement position.
	ExpectError(t, `if (true) let x = 1;`, "SyntaxError")
	ExpectError(t, `while (false) const x = 1;`, "SyntaxError")
	ExpectError(t, `if (true) class C {}`, "SyntaxError")
}

// --- 16. function declarations in statement positions: sloppy Annex B OK ---

func TestFunctionDeclarationStatementPositionSloppyAllowed(t *testing.T) {
	// In sloppy mode a plain FunctionDeclaration may be the body of if/else and
	// of a labelled statement; the program must parse and run without error.
	Expect(t, `if (true) function g() {}`)
	Expect(t, `if (false) function g() {} else function h() {}`)
	Expect(t, `label: function g() {}`)
	// A FunctionDeclaration is valid inside a switch clause's StatementList.
	Expect(t, `switch (1) { case 1: function g() { return 7; } }`)
	Expect(t, `switch (1) { default: function g() {} }`)
}

// --- 17. switch: the CaseBlock is one lexical scope ------------------------

func TestSwitchLexicalScope(t *testing.T) {
	// Lexical names are shared across clauses of one CaseBlock, so duplicates and
	// var conflicts are early errors.
	ExpectError(t, `switch (1) { case 1: let x = 1; break; default: let x = 2; }`, "SyntaxError")
	ExpectError(t, `switch (1) { case 1: let x = 1; break; case 2: var x = 2; }`, "SyntaxError")
	// Distinct names across clauses are fine, and lexical bindings are scoped to
	// the switch.
	Expect(t, `
		let hit = 0;
		switch (2) {
			case 1: { let a = 1; hit = a; break; }
			case 2: { let b = 2; hit = b; break; }
		}
		assert.sameValue(hit, 2, "case 2 binding used");
		assert.throws(ReferenceError, function () { return b; }, "case binding not visible outside switch");
	`)
}

// --- 18. catch bindings and try/catch/finally environment survival --------

func TestCatchBindingAndVarSurvival(t *testing.T) {
	// The catch parameter is block-scoped; a var inside catch survives to the
	// enclosing function scope.
	Expect(t, `
		function f() {
			try { throw "boom"; }
			catch (err) { var leaked = err; assert.sameValue(err, "boom"); }
			assert.throws(ReferenceError, function () { return err; }, "catch param not visible after catch");
			assert.sameValue(leaked, "boom", "var declared in catch survives");
		}
		f();
	`)
	// A let inside a finally block does not escape it.
	Expect(t, `
		try {} finally { let only = 1; assert.sameValue(only, 1); }
		assert.throws(ReferenceError, function () { return only; }, "finally let is block-scoped");
	`)
}

// --- 19. for-loop per-iteration let bindings (C-style, for-of, for-in) -----

func TestForLoopPerIterationBindings(t *testing.T) {
	// C-style for with let: each iteration captures its own i.
	Expect(t, `
		var fns = [];
		for (let i = 0; i < 3; i++) fns.push(function () { return i; });
		assert.sameValue(fns.map(function (f) { return f(); }).join(","), "0,1,2");
	`)
	// for-of with let: fresh binding per iteration.
	Expect(t, `
		var out = [];
		for (const v of [10, 20, 30]) out.push(function () { return v; });
		assert.sameValue(out.map(function (f) { return f(); }).join(","), "10,20,30");
	`)
	// for-in with let: fresh binding per key.
	Expect(t, `
		var keys = [];
		for (let k in { a: 1, b: 2 }) keys.push(function () { return k; });
		assert.sameValue(keys.map(function (f) { return f(); }).sort().join(","), "a,b");
	`)
}

// --- 20. nested-loop capture with break/continue/labels --------------------

func TestNestedLoopCaptureControlFlow(t *testing.T) {
	Expect(t, `
		var fns = [];
		outer:
		for (let a = 0; a < 3; a++) {
			for (let b = 0; b < 3; b++) {
				if (b === 1) continue;
				if (a === 2 && b === 2) break outer;
				fns.push(function () { return a + "" + b; });
			}
		}
		assert.sameValue(fns.map(function (f) { return f(); }).join(","), "00,02,10,12,20");
	`)
}

// --- 21. closures capture the binding by reference, per block -------------

func TestBlockClosureCaptureByReference(t *testing.T) {
	Expect(t, `
		let get, set;
		{
			let n = 0;
			get = function () { return n; };
			set = function (v) { n = v; };
		}
		set(42);
		assert.sameValue(get(), 42, "closures share the block's binding after the block exits");
	`)
}

// --- 22. class declaration is a lexical binding (block-scoped + TDZ) -------

func TestClassDeclarationLexical(t *testing.T) {
	// A class declaration is block-scoped and sits in a TDZ before its line.
	Expect(t, `
		{
			assert.throws(ReferenceError, function () { return C; }, "class name in TDZ before declaration");
			class C {}
			assert.sameValue(typeof C, "function", "class readable after declaration");
		}
		assert.throws(ReferenceError, function () { return C; }, "class not visible outside its block");
	`)
}

// --- 23. destructuring bindings are block-scoped and honor TDZ ------------

func TestBlockDestructuringBindings(t *testing.T) {
	Expect(t, `
		{
			let { a, b: [c] } = { a: 1, b: [2] };
			assert.sameValue(a, 1);
			assert.sameValue(c, 2);
		}
		assert.throws(ReferenceError, function () { return a; }, "destructured let is block-scoped");
	`)
	// A destructuring pattern name duplicating another lexical name is an error.
	ExpectError(t, `{ let x; let [x] = [1]; }`, "SyntaxError")
	ExpectError(t, `{ let { y } = {}; var y; }`, "SyntaxError")
}

// --- 24. the global lexical environment is not globalThis ------------------

func TestGlobalLexicalEnvNotGlobalThis(t *testing.T) {
	Expect(t, `
		let gl = 1;
		const gc = 2;
		class GK {}
		assert.sameValue(gl, 1, "top-level let is readable");
		assert.sameValue(typeof globalThis.gl, "undefined", "top-level let is not a globalThis property");
		assert.sameValue(typeof globalThis.gc, "undefined", "top-level const is not a globalThis property");
		assert.sameValue(typeof globalThis.GK, "undefined", "top-level class is not a globalThis property");
	`)
}

// --- 25. deep lookup resolution through many nested scopes -----------------

func TestBlockDeepLookupResolution(t *testing.T) {
	Expect(t, `
		let depth0 = "d0";
		{
			{
				{
					{
						function reach() { return depth0; }
						assert.sameValue(reach(), "d0", "innermost function resolves an outer-outer let");
					}
				}
			}
		}
	`)
}

// --- 26. environment lifetime: binding retained by an escaping closure -----

func TestBlockEnvironmentLifetime(t *testing.T) {
	Expect(t, `
		function makeSeq() {
			let items = [];
			for (let i = 0; i < 3; i++) items.push(function () { return i; });
			return items;
		}
		var seq = makeSeq();
		assert.sameValue(seq[0]() + seq[1]() + seq[2](), 3, "each retained binding keeps its own value");
	`)
}

// --- 27. initialization ordering within a block ---------------------------

func TestBlockInitializationOrdering(t *testing.T) {
	Expect(t, `
		{
			let a = 1;
			let b = a + 1;     // a is initialized before b's initializer runs
			let c = b + 1;
			assert.sameValue(a + "," + b + "," + c, "1,2,3", "lexical initializers run top-to-bottom");
		}
	`)
}

// --- 28. eval interaction: block scoping inside eval'd source --------------

func TestBlockEvalLexicalScope(t *testing.T) {
	// eval evaluates its source as a program.
	Expect(t, `assert.sameValue(eval("1 + 2"), 3, "eval evaluates an expression");`)
	// A lexical binding inside an eval'd nested block stays confined to that
	// block: it is not observable after the block within the eval'd program.
	Expect(t, `
		assert.sameValue(eval("{ let inner = 3; } typeof inner"), "undefined",
			"a let inside an eval'd block does not escape that block");
	`)
	// The parser's block early errors also apply to eval'd source.
	Expect(t, `
		assert.throws(SyntaxError, function () { eval("{ let d; let d; }"); },
			"a lexical redeclaration inside eval'd source is a SyntaxError");
	`)
	// NOTE: this engine implements eval with indirect (global-scope) semantics.
	// It does not access the caller's lexical scope, and top-level let/const/
	// class declared directly in eval leak into the global scope. See
	// wontfix/block-scope.md.
}

// --- 29. error reporting: the correct error TYPE per failure mode ----------

func TestBlockErrorReportingTypes(t *testing.T) {
	// Reassigning a const is a runtime TypeError, not a SyntaxError.
	Expect(t, `assert.throws(TypeError, function () { const x = 1; x = 2; });`)
	// Reading a lexical name in its TDZ is a runtime ReferenceError.
	Expect(t, `assert.throws(ReferenceError, function () { { z; let z = 1; } });`)
	// A block-level lexical redeclaration is a parse-time SyntaxError.
	ExpectError(t, `{ let dup; let dup; }`, "SyntaxError")
}

// --- 30. cross-feature integration ----------------------------------------

func TestBlockScopeCrossFeatureIntegration(t *testing.T) {
	Expect(t, `
		function build() {
			const results = [];
			for (let i = 0; i < 3; i++) {
				switch (i % 2) {
					case 0: {
						const tag = "even" + i;
						results.push(() => tag);
						break;
					}
					default: {
						let tag = "odd" + i;
						results.push(function () { return tag; });
					}
				}
			}
			try {
				throw new Error("x");
			} catch (e) {
				results.push(() => e.message);
			}
			return results;
		}
		var r = build().map(function (f) { return f(); });
		assert.sameValue(r.join(","), "even0,odd1,even2,x",
			"per-iteration let, switch-clause const/let, and catch binding all captured correctly");
	`)
}
