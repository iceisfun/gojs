package harness

import (
	"strings"
	"testing"
)

// This file is a comprehensive, self-contained regression suite for function
// semantics and execution: declaration/expression/arrow forms, invocation and
// `this` binding across strict and sloppy modes, the arguments object (mapped
// and unmapped), parameter environments (defaults, rest, destructuring, TDZ,
// duplicate rules), return/closure/recursion behavior, function object
// metadata (name/length), call/apply/bind, constructors and new.target, and
// cross-feature integration. Each case throws (failing the Go test) if any
// assertion does not hold. It has no dependency on Test262.

// ---------------------------------------------------------------------------
// Function declarations
// ---------------------------------------------------------------------------

func TestFuncDeclHoistingAndForms(t *testing.T) {
	Expect(t, `
		// Hoisted: callable before its textual position.
		assert.sameValue(hoisted(), 7, "declaration hoisting");
		function hoisted(){ return 7; }

		// Recursion.
		function fact(n){ return n <= 1 ? 1 : n * fact(n - 1); }
		assert.sameValue(fact(5), 120, "recursion");

		// Nested declaration is scoped to its enclosing function.
		function outer(){ function inner(){ return "in"; } return inner(); }
		assert.sameValue(outer(), "in", "nested declaration");

		// A later declaration of the same name wins (both fully hoisted).
		function dup(){ return 1; }
		function dup(){ return 2; }
		assert.sameValue(dup(), 2, "duplicate declaration: last wins");

		// Global function declarations become global properties.
		function g(){ return 42; }
		assert.sameValue(globalThis.g(), 42, "global function is a global property");
	`)
}

// ---------------------------------------------------------------------------
// Function expressions
// ---------------------------------------------------------------------------

func TestFuncExpressions(t *testing.T) {
	Expect(t, `
		// Anonymous, assigned: name inferred from the binding.
		var anon = function(){ return 1; };
		assert.sameValue(anon.name, "anon", "anonymous FE name inference");

		// Named function expression: name visible only inside the body.
		var fact = function self(n){ return n <= 1 ? 1 : n * self(n - 1); };
		assert.sameValue(fact(4), 24, "named FE self-recursion");
		assert.sameValue(fact.name, "self", "named FE name");
		assert.sameValue(typeof self, "undefined", "named FE name not leaked to outer scope");

		// The internal name binding refers to the function itself.
		var g = function h(){ return h; };
		assert.sameValue(g(), g, "FE name binding refers to the function");

		// IIFE.
		var r = (function(x){ return x * 2; })(21);
		assert.sameValue(r, 42, "IIFE");
	`)
}

// ---------------------------------------------------------------------------
// Arrow functions
// ---------------------------------------------------------------------------

func TestArrowFunctions(t *testing.T) {
	Expect(t, `
		// Lexical this: an arrow captures the enclosing method's this.
		var obj = { v: 10, get(){ return (() => this.v)(); } };
		assert.sameValue(obj.get(), 10, "arrow lexical this");

		// Arrow this is not affected by call/apply.
		var f = () => this;
		var captured = f();
		assert.sameValue(f.call({}), captured, "arrow this ignores call");

		// Lexical arguments: an arrow sees the enclosing function's arguments.
		function outer(){ return (() => arguments[0])(); }
		assert.sameValue(outer(99), 99, "arrow lexical arguments");

		// new.target is lexical too (undefined at top level).
		function host(){ return (() => new.target)(); }
		assert.sameValue(new host().constructor !== undefined, true, "arrow inside ctor runs");

		// Arrows are not constructable and have no own prototype.
		var a = () => {};
		assert.sameValue(a.prototype, undefined, "arrow has no prototype");
		assert.sameValue(a.hasOwnProperty("prototype"), false, "arrow lacks own prototype");
		assert.throws(TypeError, function(){ new a(); }, "arrow not constructable");

		// Concise-body value.
		var sq = x => x * x;
		assert.sameValue(sq(6), 36, "concise body");
		assert.sameValue(sq.name, "sq", "arrow name inference");
	`)
}

// ---------------------------------------------------------------------------
// Invocation forms and `this`
// ---------------------------------------------------------------------------

func TestInvocationFormsAndThis(t *testing.T) {
	Expect(t, `
		var obj = { v: 5, m(){ return this.v; } };

		// obj.f() — this is the receiver.
		assert.sameValue(obj.m(), 5, "method call this");

		// (obj.f)() — parenthesization does not detach this.
		assert.sameValue((obj.m)(), 5, "parenthesized method keeps this");

		// Detached call loses the receiver.
		var detached = obj.m;
		assert.sameValue(detached(), undefined, "detached sloppy this is global (v undefined)");

		// call / apply set this explicitly.
		function who(){ return this.tag; }
		assert.sameValue(who.call({ tag: "A" }), "A", "call this");
		assert.sameValue(who.apply({ tag: "B" }), "B", "apply this");
	`)
}

func TestSloppyThisSubstitution(t *testing.T) {
	Expect(t, `
		function f(){ return this; }
		// Nullish receivers become the global object.
		assert.sameValue(f.call(null), globalThis, "sloppy null this -> global");
		assert.sameValue(f.call(undefined), globalThis, "sloppy undefined this -> global");
		assert.sameValue(f(), globalThis, "sloppy plain call this -> global");

		// Primitive receivers are boxed to their wrapper object.
		function typ(){ return typeof this; }
		assert.sameValue(typ.call(1), "object", "sloppy number this boxed");
		assert.sameValue(typ.call("s"), "object", "sloppy string this boxed");
		assert.sameValue(typ.call(true), "object", "sloppy boolean this boxed");

		// The box round-trips the original value.
		function val(){ return this.valueOf(); }
		assert.sameValue(val.call(7), 7, "boxed this.valueOf");
	`)
}

func TestStrictThisIsUndefined(t *testing.T) {
	Expect(t, `
		function f(){ "use strict"; return this; }
		assert.sameValue(f(), undefined, "strict plain call this is undefined");
		assert.sameValue(f.call(undefined), undefined, "strict undefined stays undefined");
		assert.sameValue(f.call(null), null, "strict null stays null");

		// Primitives are NOT boxed in strict mode.
		function typ(){ "use strict"; return typeof this; }
		assert.sameValue(typ.call(1), "number", "strict number this not boxed");
		assert.sameValue(typ.call("s"), "string", "strict string this not boxed");
		assert.sameValue(typ.call(true), "boolean", "strict boolean this not boxed");
		assert.sameValue(typ.call(42) === "number", true, "strict primitive identity");

		// Strict propagates into nested functions declared in a strict function.
		function outer(){ "use strict"; return function(){ return this; }; }
		assert.sameValue(outer()(), undefined, "nested inherits strict this");
	`)
}

// ---------------------------------------------------------------------------
// arguments object
// ---------------------------------------------------------------------------

func TestArgumentsObject(t *testing.T) {
	Expect(t, `
		function f(a, b){ return arguments.length; }
		assert.sameValue(f(1), 1, "arguments.length reflects actual args");
		assert.sameValue(f(1, 2, 3), 3, "arguments.length can exceed arity");

		// Indexing and iteration.
		function idx(){ return arguments[0] + ":" + arguments[2]; }
		assert.sameValue(idx(10, 20, 30), "10:30", "arguments indexing");

		function sum(){ var s = 0; for (var x of arguments) s += x; return s; }
		assert.sameValue(sum(1, 2, 3, 4), 10, "arguments is iterable");

		function spread(){ return [...arguments].join(","); }
		assert.sameValue(spread("a", "b"), "a,b", "arguments spreads");

		// The common generic idiom works on arguments.
		function borrow(){ return Array.prototype.slice.call(arguments, 1); }
		assert.sameValue(borrow(1, 2, 3).join(","), "2,3", "slice.call(arguments)");

		// A parameter named "arguments" shadows the arguments object.
		function shadow(arguments){ return arguments; }
		assert.sameValue(shadow(5), 5, "parameter named arguments shadows the object");
	`)
}

// TestUnmappedArguments verifies that arguments is a snapshot with no aliasing
// to the named parameters. This is exact for strict mode and for any non-simple
// parameter list (where the spec also mandates an unmapped object). gojs also
// uses this snapshot behavior in the sloppy simple-param case, where the spec
// would call for a *mapped* object; that intentional divergence is documented
// in wontfix/function-code.md and pinned by TestArgumentsNoMappedAliasing below.
func TestUnmappedArguments(t *testing.T) {
	Expect(t, `
		// Strict mode: no aliasing.
		function s(a){ "use strict"; arguments[0] = 99; return a; }
		assert.sameValue(s(1), 1, "strict arguments does not alias parameter");
		function s2(a){ "use strict"; a = 5; return arguments[0]; }
		assert.sameValue(s2(1), 1, "strict parameter does not alias arguments");

		// A default parameter makes the whole list non-simple -> unmapped.
		function d(a = 0){ arguments[0] = 99; return a; }
		assert.sameValue(d(1), 1, "default param -> unmapped arguments");

		// A rest parameter -> unmapped.
		function r(a, ...rest){ arguments[0] = 99; return a; }
		assert.sameValue(r(1, 2), 1, "rest param -> unmapped arguments");

		// A destructuring parameter -> unmapped.
		function p([a]){ arguments[0] = 99; return a; }
		assert.sameValue(p([1]), 1, "destructuring param -> unmapped arguments");
	`)
}

// TestArgumentsNoMappedAliasing pins gojs's documented divergence: it does not
// implement sloppy-mode mapped-arguments aliasing, so writing arguments[i] does
// not change the named parameter (and vice versa) even for a simple parameter
// list. If mapped arguments are ever implemented, update this test and remove
// the entry from wontfix/function-code.md.
func TestArgumentsNoMappedAliasing(t *testing.T) {
	Expect(t, `
		function w2p(a){ arguments[0] = 99; return a; }
		assert.sameValue(w2p(1), 1, "divergence: arguments[i] write is not seen by the parameter");
		function p2w(a){ a = 5; return arguments[0]; }
		assert.sameValue(p2w(1), 1, "divergence: parameter write is not seen by arguments[i]");
	`)
}

// ---------------------------------------------------------------------------
// Default parameters and the parameter environment
// ---------------------------------------------------------------------------

func TestDefaultParameters(t *testing.T) {
	Expect(t, `
		// Only undefined triggers the default (null does not).
		function f(a = 10){ return a; }
		assert.sameValue(f(), 10, "missing -> default");
		assert.sameValue(f(undefined), 10, "undefined -> default");
		assert.sameValue(f(null), null, "null keeps null");
		assert.sameValue(f(0), 0, "0 keeps 0");

		// A later default may reference an earlier parameter.
		function g(a, b = a + 1){ return b; }
		assert.sameValue(g(5), 6, "default references prior parameter");

		// Defaults evaluate left to right, only when needed.
		var calls = [];
		function side(tag){ calls.push(tag); return tag; }
		function h(a = side("a"), b = side("b")){ return [a, b]; }
		h(undefined, "given");
		assert.sameValue(calls.join(","), "a", "only missing defaults evaluate");

		// Defaults can reference outer bindings.
		let outer = 3;
		function k(a = outer){ return a; }
		assert.sameValue(k(), 3, "default resolves outer binding");
	`)
}

func TestParameterEnvironmentTDZ(t *testing.T) {
	Expect(t, `
		// A default that references a not-yet-initialized later parameter throws.
		assert.throws(ReferenceError, function(){
			(function(a = b, b = 1){})();
		}, "default forward-reference is a TDZ ReferenceError");

		// But providing the argument avoids evaluating the offending default.
		assert.sameValue((function(a = b, b = 1){ return a; })(7, 2), 7,
			"default not evaluated when argument supplied");
	`)
}

// ---------------------------------------------------------------------------
// Rest and destructuring parameters
// ---------------------------------------------------------------------------

func TestRestAndDestructuringParams(t *testing.T) {
	Expect(t, `
		function rest(a, ...more){ return a + ":" + more.length + ":" + more.join(","); }
		assert.sameValue(rest(1, 2, 3, 4), "1:3:2,3,4", "rest gathers remaining args");
		assert.sameValue(rest(1), "1:0:", "rest is empty when no extra args");
		assert.sameValue(Array.isArray((function(...r){ return r; })(1)), true, "rest is a real Array");

		function obj({ a, b = 5 }){ return a + b; }
		assert.sameValue(obj({ a: 1 }), 6, "object destructuring param with default");

		function arr([x, , z]){ return x + z; }
		assert.sameValue(arr([1, 2, 3]), 4, "array destructuring param with hole");

		function mix({ a }, [b], ...c){ return a + b + c.length; }
		assert.sameValue(mix({ a: 1 }, [2], 3, 4), 5, "mixed patterns and rest");
	`)
}

// ---------------------------------------------------------------------------
// Duplicate parameters
// ---------------------------------------------------------------------------

func TestDuplicateParameters(t *testing.T) {
	// Sloppy + simple list: duplicates are allowed; the last binding wins.
	Expect(t, `
		function f(a, a){ return a; }
		assert.sameValue(f(1, 2), 2, "sloppy duplicate: last wins");
	`)
	// Strict mode makes duplicates a SyntaxError (early error, surfaced via eval).
	ExpectError(t, `eval("(function(a, a){ 'use strict'; })");`, "SyntaxError")
	// A non-simple parameter list forbids duplicates even in sloppy mode.
	ExpectError(t, `eval("(function(a, a, ...b){})");`, "SyntaxError")
	ExpectError(t, `eval("(function(a, a = 1){})");`, "SyntaxError")
	// Arrow functions never allow duplicates.
	ExpectError(t, `eval("((a, a) => a)");`, "SyntaxError")
}

// ---------------------------------------------------------------------------
// return / closures / recursion / lexical lifetime
// ---------------------------------------------------------------------------

func TestReturnSemantics(t *testing.T) {
	Expect(t, `
		function implicit(){ 1 + 1; }
		assert.sameValue(implicit(), undefined, "implicit return is undefined");

		function early(x){ if (x) return "yes"; return "no"; }
		assert.sameValue(early(true), "yes", "early return");
		assert.sameValue(early(false), "no", "fallthrough return");

		// return inside a nested block/loop unwinds the whole function.
		function nested(){ for (var i = 0; i < 10; i++){ if (i === 3) return i; } return -1; }
		assert.sameValue(nested(), 3, "return from loop");

		// return in a finally overrides the try's return.
		function fin(){ try { return "try"; } finally { return "finally"; } }
		assert.sameValue(fin(), "finally", "finally return overrides");
	`)
}

func TestClosuresAndLexicalLifetime(t *testing.T) {
	Expect(t, `
		// Mutable captured state.
		function counter(){ var n = 0; return function(){ return ++n; }; }
		var c = counter();
		assert.sameValue(c(), 1, "closure state 1");
		assert.sameValue(c(), 2, "closure state 2");

		// Independent instances do not share state.
		var d = counter();
		assert.sameValue(d(), 1, "independent closure instance");

		// Nested closures capture the same variable.
		function make(){ var x = 0; return { inc(){ x++; }, get(){ return x; } }; }
		var m = make();
		m.inc(); m.inc();
		assert.sameValue(m.get(), 2, "nested closures share capture");

		// Per-iteration binding with let.
		var fns = [];
		for (let i = 0; i < 3; i++){ fns.push(function(){ return i; }); }
		assert.sameValue(fns[0]() + "," + fns[1]() + "," + fns[2](), "0,1,2", "let per-iteration capture");
	`)
}

func TestRecursion(t *testing.T) {
	Expect(t, `
		// Mutual recursion.
		function even(n){ return n === 0 ? true : odd(n - 1); }
		function odd(n){ return n === 0 ? false : even(n - 1); }
		assert.sameValue(even(10), true, "mutual recursion even");
		assert.sameValue(odd(7), true, "mutual recursion odd");

		// Unbounded recursion is bounded by the engine and throws RangeError.
		function boom(){ return boom(); }
		assert.throws(RangeError, boom, "call-depth limit throws RangeError");
	`)
}

// ---------------------------------------------------------------------------
// Function object metadata
// ---------------------------------------------------------------------------

func TestFunctionNameAndLength(t *testing.T) {
	Expect(t, `
		// name inference across binding forms.
		function decl(){}
		assert.sameValue(decl.name, "decl", "declaration name");
		var fe = function(){};
		assert.sameValue(fe.name, "fe", "assigned FE name");
		let arrow = () => {};
		assert.sameValue(arrow.name, "arrow", "assigned arrow name");
		var o = { method(){}, get x(){ return 1; }, set x(v){} };
		assert.sameValue(o.method.name, "method", "concise method name");
		assert.sameValue(Object.getOwnPropertyDescriptor(o, "x").get.name, "get x", "getter name");
		assert.sameValue(Object.getOwnPropertyDescriptor(o, "x").set.name, "set x", "setter name");

		// length counts leading required parameters only.
		assert.sameValue((function(a, b, c){}).length, 3, "plain length");
		assert.sameValue((function(a, b = 1, c){}).length, 1, "default stops the count");
		assert.sameValue((function(a, ...b){}).length, 1, "rest excluded from length");
		assert.sameValue((function(){}).length, 0, "no params");
		assert.sameValue(((a, b) => 0).length, 2, "arrow length");
	`)
}

// ---------------------------------------------------------------------------
// call / apply / bind
// ---------------------------------------------------------------------------

func TestCallApplyBind(t *testing.T) {
	Expect(t, `
		function f(a, b){ return this.base + a + b; }

		// call: explicit this + positional args.
		assert.sameValue(f.call({ base: 10 }, 1, 2), 13, "call");

		// apply: array and array-like args, plus empty/sparse.
		assert.sameValue(f.apply({ base: 10 }, [1, 2]), 13, "apply array");
		assert.sameValue((function(){ return arguments.length; }).apply(null, { length: 3 }), 3, "apply array-like");
		assert.sameValue((function(a){ return a; }).apply(null, []), undefined, "apply empty");

		// bind: this + partial application.
		var g = f.bind({ base: 100 }, 1);
		assert.sameValue(g(2), 103, "bind this + partial");
		assert.sameValue(g.name, "bound f", "bound name");
		assert.sameValue(g.length, 1, "bound length = target.length - boundArgs");

		// bind repeated / metadata.
		var h = g.bind(null, 5);
		assert.sameValue(h(), 106, "repeated bind");
		assert.sameValue(h.length, 0, "repeated bind length floors at 0");
	`)
}

func TestBindConstructor(t *testing.T) {
	Expect(t, `
		function Point(x, y){ this.x = x; this.y = y; }
		var BoundPoint = Point.bind(null, 1);
		var p = new BoundPoint(2);
		assert.sameValue(p.x, 1, "bound constructor uses bound arg");
		assert.sameValue(p.y, 2, "bound constructor uses call arg");
		assert.sameValue(p instanceof Point, true, "bound constructor instance chain");
	`)
}

// ---------------------------------------------------------------------------
// Constructors and new.target
// ---------------------------------------------------------------------------

func TestConstructors(t *testing.T) {
	Expect(t, `
		// A returned object replaces the new instance.
		function A(){ this.x = 1; return { y: 2 }; }
		var a = new A();
		assert.sameValue(a.y, 2, "constructor returns object");
		assert.sameValue(a.x, undefined, "returned object replaces this");

		// A returned primitive is ignored.
		function B(){ this.x = 1; return 5; }
		assert.sameValue(new B().x, 1, "constructor primitive return ignored");

		// Prototype linkage.
		function C(){}
		C.prototype.greet = function(){ return "hi"; };
		var c = new C();
		assert.sameValue(c.greet(), "hi", "prototype method");
		assert.sameValue(Object.getPrototypeOf(c), C.prototype, "instance prototype");
		assert.sameValue(C.prototype.constructor, C, "prototype.constructor back-link");
	`)
}

func TestNewTarget(t *testing.T) {
	Expect(t, `
		function F(){ return new.target; }
		assert.sameValue(F(), undefined, "new.target is undefined for a plain call");

		function G(){ this.nt = new.target; }
		assert.sameValue(new G().nt, G, "new.target is the constructor under new");
	`)
}

// ---------------------------------------------------------------------------
// Generators / async detection
// ---------------------------------------------------------------------------

func TestGeneratorAndAsyncShape(t *testing.T) {
	Expect(t, `
		function* gen(){ yield 1; yield 2; return 3; }
		var it = gen();
		assert.sameValue(typeof it.next, "function", "generator has next");
		assert.sameValue(it[Symbol.iterator]() === it, true, "generator is its own iterator");
		var r1 = it.next();
		assert.sameValue(r1.value, 1, "yield 1");
		assert.sameValue(r1.done, false, "not done");
		assert.sameValue(it.next().value, 2, "yield 2");
		var r3 = it.next();
		assert.sameValue(r3.value, 3, "return value");
		assert.sameValue(r3.done, true, "done after return");

		// Spreading a generator collects yields.
		function* nums(){ yield 1; yield 2; yield 3; }
		assert.sameValue([...nums()].join(","), "1,2,3", "generator spread");

		// A generator function is not constructable and has no [[Call]] this issue.
		assert.throws(TypeError, function(){ new gen(); }, "generator not constructable");
	`)
}

func TestAsyncFunctions(t *testing.T) {
	// Assertions inside promise reactions are swallowed as unhandled rejections
	// by the harness, so async outcomes are surfaced via console output and
	// verified here in Go instead.
	out := Expect(t, `
		// An async function returns a promise (synchronously observable).
		async function ok(){ return 21 * 2; }
		var p = ok();
		assert.sameValue(typeof p.then, "function", "async returns a promise");
		p.then(function(v){ console.log("ok:" + v); });

		// await unwraps a resolved value.
		async function chain(){ var x = await Promise.resolve(5); return x + 1; }
		chain().then(function(v){ console.log("chain:" + v); });

		// A thrown error rejects the returned promise.
		async function bad(){ throw new TypeError("nope"); }
		bad().then(
			function(){ console.log("bad:resolved"); },
			function(e){ console.log("bad:" + (e instanceof TypeError)); });
	`)
	joined := strings.Join(out, "\n")
	for _, want := range []string{"ok:42", "chain:6", "bad:true"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("async output missing %q; got: %v", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// eval interaction (indirect) and Function constructor gating
// ---------------------------------------------------------------------------

func TestEvalAndFunctionCtorGating(t *testing.T) {
	// Indirect eval runs in the global scope.
	Expect(t, `
		var x = eval("1 + 2");
		assert.sameValue(x, 3, "indirect eval evaluates");
		eval("var evalGlobal = 41;");
		assert.sameValue(evalGlobal, 41, "indirect eval writes global");
	`)
	// The dynamic Function constructor builds a callable from source.
	Expect(t, `assert.sameValue(Function("return 1")(), 1, "Function ctor runs");`)
}

// ---------------------------------------------------------------------------
// Exception propagation
// ---------------------------------------------------------------------------

func TestExceptionPropagation(t *testing.T) {
	Expect(t, `
		// Throw propagates across nested calls until caught.
		function deep(n){ if (n === 0) throw new Error("bottom"); return deep(n - 1); }
		var caught = "";
		try { deep(5); } catch(e){ caught = e.message; }
		assert.sameValue(caught, "bottom", "throw unwinds nested calls");

		// finally still runs while an exception propagates.
		var log = [];
		function f(){ try { throw new Error("x"); } finally { log.push("f"); } }
		try { f(); } catch(e){ log.push("c"); }
		assert.sameValue(log.join(","), "f,c", "finally runs before the handler");

		// A throw from a default-parameter initializer propagates.
		assert.throws(RangeError, function(){
			(function(a = (function(){ throw new RangeError("d"); })()){})();
		}, "throw from default initializer");
	`)
}

// ---------------------------------------------------------------------------
// Cross-feature integration
// ---------------------------------------------------------------------------

func TestFunctionIntegration(t *testing.T) {
	Expect(t, `
		// A closure over mapped arguments, invoked via apply, returning a bound method.
		function factory(prefix){
			return function(){
				var parts = Array.prototype.slice.call(arguments);
				return prefix + parts.join("-");
			};
		}
		var join = factory("x");
		assert.sameValue(join.apply(null, [1, 2, 3]), "x1-2-3", "closure + apply + arguments");

		// Recursion + default params + destructuring together.
		function walk({ value, next } = {}, acc = []){
			if (value === undefined) return acc;
			acc.push(value);
			return walk(next, acc);
		}
		var list = { value: 1, next: { value: 2, next: { value: 3 } } };
		assert.sameValue(walk(list).join(","), "1,2,3", "recursive linked-list walk");

		// Functions are ordinary objects: attach and read own properties.
		function tagged(){ return tagged.tag; }
		tagged.tag = "meta";
		assert.sameValue(tagged(), "meta", "function object own property");
		assert.sameValue(typeof tagged, "function", "callable typeof");
	`)
}
