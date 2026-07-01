package harness

import "testing"

// Comprehensive switch-statement semantics suite (per the Switch Statement,
// Case Clauses, and Lexical Declaration Semantics SoW). Each test executes real
// JavaScript and asserts observable behavior. Two flavors are used:
//
//   - Expect(t, ...)     runtime behavior; the script self-reports via assert.
//   - ExpectError(t, s)  parse/runtime errors surface as thrown JS values
//                        (SyntaxError for switch early errors, ReferenceError
//                        for TDZ), which ExpectError catches.
//
// Completion-value semantics are exercised with eval(), whose result is the
// program's completion value (CaseBlockEvaluation + UpdateEmpty).
//
// The whole CaseBlock is ONE lexical scope shared across every clause, so
// duplicate let/const/class/function bindings across ANY two clauses (even
// unreachable ones) are early SyntaxErrors, checked at parse time regardless of
// which case matches.

// --- 1. Basic dispatch -----------------------------------------------------

func TestSwitchBasicDispatch(t *testing.T) {
	Expect(t, `
		function pick(x) {
			switch (x) {
				case 1: return "one";
				case 2: return "two";
				default: return "other";
			}
		}
		assert.sameValue(pick(1), "one", "match first");
		assert.sameValue(pick(2), "two", "match second");
		assert.sameValue(pick(3), "other", "default when no match");
	`)
	// No default and no match: the switch completes with no effect.
	Expect(t, `
		var hit = 0;
		switch (99) { case 1: hit = 1; break; case 2: hit = 2; break; }
		assert.sameValue(hit, 0, "no default, no match -> nothing runs");
	`)
	// Empty switch body is legal and does nothing.
	Expect(t, `
		var ran = true;
		switch (1) {}
		assert.sameValue(ran, true, "empty case block is a no-op");
	`)
}

// --- 2. Strict-equality matching -------------------------------------------

func TestSwitchStrictEquality(t *testing.T) {
	Expect(t, `
		function m(x) { switch (x) { case 1: return "num"; case "1": return "str"; default: return "none"; } }
		assert.sameValue(m(1), "num", "number matches number, not string");
		assert.sameValue(m("1"), "str", "string matches string, not number");
	`)
	// No coercion: "0" does not match 0; true does not match 1.
	Expect(t, `
		assert.sameValue(eval('switch(0){case "0": 1; break; default: 2}'), 2, "0 !== \"0\"");
		assert.sameValue(eval('switch(1){case true: 1; break; default: 2}'), 2, "1 !== true");
	`)
	// booleans, null, undefined match themselves.
	Expect(t, `
		assert.sameValue(eval('switch(true){case true: 10; break; default: 0}'), 10);
		assert.sameValue(eval('switch(null){case null: 11; break; default: 0}'), 11);
		assert.sameValue(eval('switch(undefined){case undefined: 12; break; default: 0}'), 12);
		assert.sameValue(eval('switch(null){case undefined: 1; break; default: 0}'), 0, "null !== undefined");
	`)
}

func TestSwitchNaNNeverMatches(t *testing.T) {
	// NaN is never === to anything, including NaN, so a case NaN never matches.
	Expect(t, `
		var hit = "none";
		switch (NaN) { case NaN: hit = "nan"; break; default: hit = "default"; }
		assert.sameValue(hit, "default", "NaN case never matches");
	`)
}

func TestSwitchSignedZero(t *testing.T) {
	// +0 and -0 are === to each other, so either matches the other.
	Expect(t, `
		assert.sameValue(eval('switch(-0){case 0: "matched"; break; default: "no"}'), "matched", "-0 matches +0");
		assert.sameValue(eval('switch(0){case -0: "matched"; break; default: "no"}'), "matched", "+0 matches -0");
	`)
}

func TestSwitchObjectIdentity(t *testing.T) {
	// Objects match by identity, not structural equality.
	Expect(t, `
		var a = {}, b = {};
		function m(x) { switch (x) { case a: return "a"; case b: return "b"; default: return "other"; } }
		assert.sameValue(m(a), "a");
		assert.sameValue(m(b), "b");
		assert.sameValue(m({}), "other", "distinct object identity does not match");
	`)
}

func TestSwitchSymbolIdentity(t *testing.T) {
	Expect(t, `
		var s1 = Symbol("s"), s2 = Symbol("s");
		function m(x) { switch (x) { case s1: return "s1"; case s2: return "s2"; default: return "none"; } }
		assert.sameValue(m(s1), "s1");
		assert.sameValue(m(s2), "s2");
		assert.sameValue(m(Symbol("s")), "none", "same-description symbols are distinct");
	`)
}

// --- 3. Case expression evaluation order + side effects + exceptions -------

func TestSwitchCaseEvalOrderAndSideEffects(t *testing.T) {
	// Case tests are evaluated top-to-bottom until one matches; later tests are
	// not evaluated once a match is found.
	Expect(t, `
		var log = [];
		function t(n, v) { log.push(n); return v; }
		switch (2) {
			case t("a", 1): break;
			case t("b", 2): break;
			case t("c", 3): break;
			default: break;
		}
		assert.sameValue(log.join(","), "a,b", "stops evaluating tests after the match");
	`)
	// The discriminant is evaluated exactly once, before any case test.
	Expect(t, `
		var log = [];
		function d() { log.push("disc"); return 1; }
		function c(n) { log.push(n); return n; }
		switch (d()) { case c(0): break; case c(1): break; }
		assert.sameValue(log.join(","), "disc,0,1", "discriminant first, then tests in order");
	`)
}

func TestSwitchCaseTestException(t *testing.T) {
	// An exception thrown while evaluating a case test unwinds the switch.
	Expect(t, `
		var reached = false;
		try {
			switch (2) {
				case (function(){ throw new Error("boom"); })(): reached = true; break;
				default: reached = true; break;
			}
		} catch (e) {
			assert.sameValue(e.message, "boom");
		}
		assert.sameValue(reached, false, "case-test exception skips clause bodies");
	`)
}

func TestSwitchDiscriminantException(t *testing.T) {
	Expect(t, `
		var caught = "";
		try { switch ((function(){ throw new TypeError("d"); })()) { default: } }
		catch (e) { caught = e.name; }
		assert.sameValue(caught, "TypeError", "discriminant exception unwinds before any clause");
	`)
}

// --- 4. Default placement (first / middle / last) --------------------------

func TestSwitchDefaultPlacement(t *testing.T) {
	// default in the middle is only entered on no match, but fallthrough still
	// flows into and out of it in source order.
	Expect(t, `
		function m(x) {
			var out = [];
			switch (x) {
				case 1: out.push("one");
				default: out.push("def");
				case 2: out.push("two");
			}
			return out.join(",");
		}
		assert.sameValue(m(1), "one,def,two", "match before default falls through default");
		assert.sameValue(m(2), "two", "match after default runs only from there");
		assert.sameValue(m(9), "def,two", "no match enters default then falls through");
	`)
	// default first.
	Expect(t, `
		function m(x) {
			switch (x) { default: return "d"; case 1: return "1"; }
		}
		assert.sameValue(m(1), "1");
		assert.sameValue(m(5), "d", "default-first still chosen on no match");
	`)
}

// --- 5. Fallthrough (implicit / chained / default / empty cases) -----------

func TestSwitchFallthrough(t *testing.T) {
	Expect(t, `
		function count(x) {
			var n = 0;
			switch (x) {
				case 1: n += 1;
				case 2: n += 2;
				case 3: n += 3;
			}
			return n;
		}
		assert.sameValue(count(1), 6, "1 falls through 2 and 3");
		assert.sameValue(count(2), 5, "2 falls through 3");
		assert.sameValue(count(3), 3, "3 alone");
	`)
	// Empty cases chain to the next non-empty body.
	Expect(t, `
		function grp(x) {
			switch (x) {
				case 1:
				case 2:
				case 3: return "low";
				case 4:
				case 5: return "high";
				default: return "other";
			}
		}
		assert.sameValue(grp(1), "low");
		assert.sameValue(grp(3), "low");
		assert.sameValue(grp(5), "high");
		assert.sameValue(grp(9), "other");
	`)
}

// --- 6. break (ordinary / nested / labeled / loop interaction) -------------

func TestSwitchBreak(t *testing.T) {
	Expect(t, `
		function f(x) {
			var out = [];
			switch (x) {
				case 1: out.push("a"); break;
				case 2: out.push("b"); break;
				default: out.push("d");
			}
			return out.join(",");
		}
		assert.sameValue(f(1), "a", "break stops fallthrough");
		assert.sameValue(f(2), "b");
	`)
	// An unlabeled break inside a switch nested in a loop breaks only the switch.
	Expect(t, `
		var out = [];
		for (var i = 0; i < 3; i++) {
			switch (i) { case 1: break; default: out.push(i); }
			out.push("loop" + i);
		}
		assert.sameValue(out.join(","), "0,loop0,loop1,2,loop2",
			"break exits the switch, the for loop keeps going");
	`)
}

func TestSwitchLabeledBreak(t *testing.T) {
	// A labeled break can exit an enclosing labeled loop from inside a switch.
	Expect(t, `
		var out = [];
		outer: for (var i = 0; i < 5; i++) {
			switch (i) {
				case 2: break outer;
				default: out.push(i);
			}
		}
		assert.sameValue(out.join(","), "0,1", "labeled break leaves the loop, not just the switch");
	`)
	// A label on the switch itself: break <label> exits the switch.
	Expect(t, `
		var out = [];
		sw: switch (1) {
			case 1: out.push("a"); break sw; out.push("unreached");
			default: out.push("d");
		}
		assert.sameValue(out.join(","), "a", "labeled break on switch exits it");
	`)
}

// --- 7. continue targeting an enclosing loop -------------------------------

func TestSwitchContinueTargetsLoop(t *testing.T) {
	// continue inside a switch is not caught by the switch; it restarts the loop.
	Expect(t, `
		var out = [];
		for (var i = 0; i < 4; i++) {
			switch (i) {
				case 1:
				case 2: continue;
				default: out.push(i);
			}
			out.push("tail" + i);
		}
		assert.sameValue(out.join(","), "0,tail0,3,tail3",
			"continue skips the loop tail for i=1,2");
	`)
	// Labeled continue from within a switch targets the labeled outer loop.
	Expect(t, `
		var out = [];
		outer: for (var i = 0; i < 3; i++) {
			for (var j = 0; j < 3; j++) {
				switch (j) { case 1: continue outer; default: out.push(i + "" + j); }
			}
		}
		assert.sameValue(out.join(","), "00,10,20", "labeled continue restarts the outer loop");
	`)
}

// --- 8. Nested switches + labels -------------------------------------------

func TestSwitchNested(t *testing.T) {
	Expect(t, `
		function f(a, b) {
			var out = [];
			switch (a) {
				case 1:
					switch (b) {
						case 1: out.push("11"); break;
						default: out.push("1x");
					}
					out.push("after-inner");
					break;
				default: out.push("outer-default");
			}
			return out.join(",");
		}
		assert.sameValue(f(1, 1), "11,after-inner", "inner break does not exit outer switch");
		assert.sameValue(f(1, 9), "1x,after-inner");
		assert.sameValue(f(9, 9), "outer-default");
	`)
}

// --- 9. Completion values (CaseBlockEvaluation + UpdateEmpty) ---------------

func TestSwitchCompletionValue(t *testing.T) {
	Expect(t, `
		assert.sameValue(eval('switch(1){case 1: 42}'), 42, "clause value is the completion");
		assert.sameValue(eval('1; switch("a"){case "a": break; default:}'), undefined, "break is empty");
		assert.sameValue(eval('2; switch("a"){case "a": { 3; break; } default:}'), 3, "block value survives break");
		assert.sameValue(eval('switch(9){}'), undefined, "empty switch -> undefined");
		assert.sameValue(eval('switch(9){case 1: 5}'), undefined, "no match -> undefined");
	`)
	// UpdateEmpty across fallthrough: a non-empty value replaces the previous;
	// an empty completion (break, empty body) does not.
	Expect(t, `
		assert.sameValue(eval('switch("a"){case "a": 2; default: 3}'), 3, "later non-empty replaces");
		assert.sameValue(eval('switch("a"){case "a": default: 5}'), 5, "non-empty replaces empty");
		assert.sameValue(eval('switch("a"){case "a": 7; default:}'), 7, "empty does not replace non-empty");
		assert.sameValue(eval('switch("a"){case "a": 7; case "b": break; default:}'), 7, "break does not replace");
	`)
}

func TestSwitchCompletionThroughLoop(t *testing.T) {
	// A continue inside a switch carries the switch's completion value out to
	// the enclosing loop, which becomes the loop's completion value.
	Expect(t, `
		assert.sameValue(
			eval('do { switch("a"){ case "a": { 6; continue; } default: } } while(false)'), 6);
		assert.sameValue(
			eval('do { switch("a"){ case "a": 14; case "b": continue; default: } } while(false)'), 14,
			"empty continue does not replace prior non-empty value");
	`)
}

// --- 10. Lexical environment: one shared scope across cases ----------------

func TestSwitchOneLexicalScope(t *testing.T) {
	// A let declared in one clause is visible in a later clause's body (same
	// scope), because the whole CaseBlock is a single lexical environment.
	Expect(t, `
		var out = [];
		switch (1) {
			case 1:
				let x = 10;
				out.push("case1:" + x);
			case 2:
				out.push("case2:" + x);
		}
		assert.sameValue(out.join(","), "case1:10,case2:10", "let is visible across clauses");
	`)
}

// --- 11. let / const declarations in cases ---------------------------------

func TestSwitchLetConstInCases(t *testing.T) {
	Expect(t, `
		function f() {
			switch (0) {
				case 0:
					let a = 1;
					const b = 2;
					return a + b;
			}
		}
		assert.sameValue(f(), 3);
	`)
	// const remains immutable within the switch scope.
	Expect(t, `
		var threw = false;
		try { eval('switch(0){ case 0: const c = 1; c = 2; }'); } catch (e) { threw = (e instanceof TypeError); }
		assert.sameValue(threw, true, "assigning to const in a case throws TypeError");
	`)
}

// --- 12. TDZ across cases (const/let ref-before-init) ----------------------

func TestSwitchTDZAcrossCases(t *testing.T) {
	// Reading a let/const declared in a LATER clause from an EARLIER clause is a
	// TDZ access -> ReferenceError (the binding exists in the shared scope but
	// is uninitialized).
	ExpectError(t, `
		switch (1) {
			case 1: x; // TDZ: x is declared below but not yet initialized
			case 2: let x = 5;
		}
	`, "ReferenceError")
	// After initialization, the binding reads normally in a later clause.
	Expect(t, `
		var out;
		switch (1) {
			case 1: let y = 8;
			case 2: out = y;
		}
		assert.sameValue(out, 8);
	`)
}

// --- 13. class declarations in cases ---------------------------------------

func TestSwitchClassInCases(t *testing.T) {
	Expect(t, `
		function f() {
			switch (0) {
				case 0:
					class C { m() { return 42; } }
					return new C().m();
			}
		}
		assert.sameValue(f(), 42);
	`)
	// A class is in the TDZ before its declaration within the shared scope.
	ExpectError(t, `
		switch (1) {
			case 1: new C();
			case 2: class C {}
		}
	`, "ReferenceError")
}

// --- 14. Duplicate lexical bindings across cases -> SyntaxError -------------

func TestSwitchDuplicateLexicalEarlyErrors(t *testing.T) {
	// The CaseBlock is one scope; a name may be lexically declared only once
	// across all clauses, even unreachable ones.
	ExpectError(t, `switch (0) { case 1: let a; case 2: let a; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: let f; default: const f = 0; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: const a = 1; default: const a = 2; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: class A {} default: let A; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: function g() {} default: function g() {} }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: let a; case 2: class a {} }`, "SyntaxError")
}

func TestSwitchDeadCaseStillValidated(t *testing.T) {
	// Even a never-matched (dead) case participates in the early-error check:
	// the duplicate must be reported at parse time regardless of dispatch.
	ExpectError(t, `switch (0) { case 1: let z; case 999: let z; }`, "SyntaxError")
	// Both cases dead relative to the discriminant, still a parse error.
	ExpectError(t, `switch ("nomatch") { case "x": let q; case "y": let q; }`, "SyntaxError")
}

// --- 15. var vs let coexistence and collisions -----------------------------

func TestSwitchVarLexicalCollision(t *testing.T) {
	// A lexical name colliding with a var-declared name in the CaseBlock is an
	// early error (var hoists to the enclosing function, lexical stays in the
	// switch scope, but the spec forbids the overlap).
	ExpectError(t, `switch (0) { case 1: let f; default: var f; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: var f; default: let f; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: function f() {} default: var f; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: var f; default: function f() {} }`, "SyntaxError")
}

func TestSwitchDuplicateVarAllowed(t *testing.T) {
	// Plain var may repeat across cases (and coexist with an outer var); it is
	// NOT an early error.
	Expect(t, `
		switch (0) { case 1: var f; default: var f; }
		assert.sameValue(typeof f, "undefined", "duplicate var across cases is fine");
	`)
	Expect(t, `
		var out;
		switch (1) { case 1: var v = 3; default: out = v; }
		assert.sameValue(out, 3, "var is visible across cases and hoisted");
	`)
}

// --- 16. Mixed declarations ------------------------------------------------

func TestSwitchMixedDeclarations(t *testing.T) {
	Expect(t, `
		function f() {
			var acc = [];
			switch (1) {
				case 1:
					var a = 1;
					let b = 2;
					const c = 3;
					function g() { return 4; }
					acc.push(a, b, c, g());
			}
			return acc.join(",");
		}
		assert.sameValue(f(), "1,2,3,4");
	`)
}

// --- 17. Shadowing via nested blocks in cases (independent env) ------------

func TestSwitchNestedBlockShadowing(t *testing.T) {
	// A brace block inside a case is its own lexical environment, so a let there
	// may reuse a name from the switch scope without conflict, and shadows it.
	Expect(t, `
		var out = [];
		switch (0) {
			case 0:
				let x = 1;
				{ let x = 2; out.push("inner:" + x); }
				out.push("outer:" + x);
		}
		assert.sameValue(out.join(","), "inner:2,outer:1", "inner block shadows the switch-scope let");
	`)
	// The same name declared in an inner block of one case AND at switch level
	// of another case does NOT conflict (different scopes).
	Expect(t, `
		var ok = true;
		switch (0) { case 1: { let n = 1; } default: let n = 2; }
		assert.sameValue(ok, true, "block-local and switch-level names in different clauses do not collide");
	`)
}

// --- 18. Block-wrapped cases (braces = independent env) --------------------

func TestSwitchBlockWrappedCases(t *testing.T) {
	// Wrapping each case body in braces gives each its own scope, so the same
	// let name may appear in multiple cases without an early error.
	Expect(t, `
		function f(x) {
			switch (x) {
				case 1: { let v = "a"; return v; }
				case 2: { let v = "b"; return v; }
				default: { let v = "d"; return v; }
			}
		}
		assert.sameValue(f(1), "a");
		assert.sameValue(f(2), "b");
		assert.sameValue(f(3), "d");
	`)
}

// --- 19. Closures capturing switch-declared bindings -----------------------

func TestSwitchClosuresCaptureBindings(t *testing.T) {
	Expect(t, `
		function make() {
			var fns = [];
			switch (0) {
				case 0:
					let n = 10;
					fns.push(function () { return n; });
					n = 20;
					fns.push(function () { return n; });
			}
			return fns;
		}
		var fns = make();
		assert.sameValue(fns[0](), 20, "closures share the one switch-scope binding");
		assert.sameValue(fns[1](), 20);
	`)
}

// --- 20. Destructuring declarations in cases -------------------------------

func TestSwitchDestructuringInCases(t *testing.T) {
	Expect(t, `
		function f() {
			switch (0) {
				case 0:
					let [a, b = 5] = [1];
					const { p, q: r } = { p: 2, q: 3 };
					return [a, b, p, r].join(",");
			}
		}
		assert.sameValue(f(), "1,5,2,3", "array/object destructuring with defaults in a case");
	`)
	// Duplicate names produced by destructuring across cases are still an early
	// error (they contribute to LexicallyDeclaredNames).
	ExpectError(t, `switch (0) { case 1: let { a } = {}; default: let [a] = []; }`, "SyntaxError")
}

// --- 21. Exceptions unwinding case-body execution --------------------------

func TestSwitchExceptionUnwindsCaseBody(t *testing.T) {
	Expect(t, `
		var out = [];
		try {
			switch (1) {
				case 1:
					out.push("before");
					throw new Error("x");
				case 2:
					out.push("after");
			}
		} catch (e) {
			out.push("caught:" + e.message);
		}
		assert.sameValue(out.join(","), "before,caught:x", "throw skips remaining fallthrough");
	`)
}

// --- 22. Nested lexical envs: block in switch in loop in function -----------

func TestSwitchDeeplyNestedEnvironments(t *testing.T) {
	Expect(t, `
		function run() {
			var total = 0;
			for (let i = 0; i < 3; i++) {
				switch (i) {
					case 0:
					case 1: {
						let bonus = i * 10;
						total += bonus;
						break;
					}
					default:
						total += 100;
				}
			}
			return total;
		}
		assert.sameValue(run(), 110, "0 -> 0, 1 -> 10, 2 -> 100");
	`)
}

// --- 23. Labeled switch statement ------------------------------------------

func TestSwitchLabeledStatement(t *testing.T) {
	Expect(t, `
		var out = [];
		L: switch (1) {
			case 1:
				out.push("a");
				if (true) break L;
				out.push("unreached");
			default:
				out.push("d");
		}
		assert.sameValue(out.join(","), "a", "break L exits the labeled switch");
	`)
}

// --- 24. var hoisting out of the switch ------------------------------------

func TestSwitchVarHoisting(t *testing.T) {
	// A var declared in a case is hoisted to the enclosing function and is
	// visible (as undefined) even before the switch runs, and after.
	Expect(t, `
		function f(x) {
			assert.sameValue(v, undefined, "var hoisted, readable before the switch");
			switch (x) { case 1: var v = "set"; }
			return v;
		}
		assert.sameValue(f(1), "set");
		assert.sameValue(f(2), undefined, "hoisted var stays undefined when its case is skipped");
	`)
}

// --- 25. Function declarations in cases ------------------------------------

func TestSwitchFunctionDeclarationInCase(t *testing.T) {
	// A single function declaration in a case is callable within the switch.
	Expect(t, `
		function f() {
			switch (0) {
				case 0:
					function g() { return 7; }
					return g();
			}
		}
		assert.sameValue(f(), 7);
	`)
}

// --- 26. Duplicate default clause -> SyntaxError ---------------------------

func TestSwitchDuplicateDefaultEarlyError(t *testing.T) {
	ExpectError(t, `switch (0) { default: 1; default: 2; }`, "SyntaxError")
	ExpectError(t, `switch (0) { case 1: default: default: }`, "SyntaxError")
}

// --- 27. Discriminant and case values: no toString/valueOf coercion --------

func TestSwitchNoCoercion(t *testing.T) {
	// Strict equality never triggers valueOf/toString on the operands.
	Expect(t, `
		var called = false;
		var obj = { valueOf: function () { called = true; return 1; } };
		var hit = "none";
		switch (obj) { case 1: hit = "coerced"; break; default: hit = "identity"; }
		assert.sameValue(hit, "identity", "no numeric coercion of the discriminant");
		assert.sameValue(called, false, "valueOf must not be invoked");
	`)
}

// --- 28. Cross-feature: switch inside try/finally --------------------------

func TestSwitchInsideTryFinally(t *testing.T) {
	Expect(t, `
		var out = [];
		function f() {
			try {
				switch (1) { case 1: out.push("body"); return "ret"; }
			} finally {
				out.push("finally");
			}
		}
		assert.sameValue(f(), "ret");
		assert.sameValue(out.join(","), "body,finally", "finally runs on return from a case");
	`)
}

// --- 29. break/continue interaction sanity in while loop -------------------

func TestSwitchInWhileLoop(t *testing.T) {
	Expect(t, `
		var i = 0, out = [];
		while (i < 5) {
			i++;
			switch (i) {
				case 2: continue;      // skip the push for i===2
				case 4: out.push("4"); break;
				default: out.push(String(i));
			}
			out.push("end" + i);
		}
		assert.sameValue(out.join(","), "1,end1,3,end3,4,end4,5,end5",
			"continue skips loop tail for i=2 only");
	`)
}

// --- 30. Empty and single-clause forms -------------------------------------

func TestSwitchEmptyAndSingleClause(t *testing.T) {
	Expect(t, `
		// lone default
		assert.sameValue(eval('switch(1){ default: 5 }'), 5, "lone default runs on any value");
		// lone case, matched and unmatched
		assert.sameValue(eval('switch(1){ case 1: 6 }'), 6);
		assert.sameValue(eval('switch(9){ case 1: 6 }'), undefined);
	`)
}
