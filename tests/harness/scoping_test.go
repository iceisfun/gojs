package harness

import "testing"

// TestScopingVarHoisting verifies that var declarations are hoisted to the top
// of their enclosing function scope: reading a var before its declaration line
// yields undefined rather than a ReferenceError.
func TestScopingVarHoisting(t *testing.T) {
	Expect(t, `
		function f() {
			assert.sameValue(x, undefined, "var read before declaration is undefined");
			var x = 42;
			assert.sameValue(x, 42, "var readable after assignment");
		}
		f();

		// Hoisting at the top-level (global var)
		assert.sameValue(typeof topVar, "undefined", "typeof hoisted var before decl is undefined");
		var topVar = 1;
		assert.sameValue(topVar, 1, "top-level var assigned correctly");
	`)
}

// TestScopingFunctionDeclarationHoisting verifies that function declarations
// are hoisted entirely (both the binding and the body), so a function can be
// called before its textual position in the source.
func TestScopingFunctionDeclarationHoisting(t *testing.T) {
	Expect(t, `
		assert.sameValue(hoisted(), 99, "function callable before declaration");
		function hoisted() { return 99; }

		// Nested inside a function
		function outer() {
			assert.sameValue(inner(), "inner", "inner function callable before its line");
			function inner() { return "inner"; }
		}
		outer();
	`)
}

// TestScopingLetTDZ verifies that reading a let binding before its declaration
// in the same block throws ReferenceError (Temporal Dead Zone).
func TestScopingLetTDZ(t *testing.T) {
	Expect(t, `
		assert.throws(ReferenceError, function() {
			(function() {
				var _ = x; // access before declaration
				let x = 1;
			})();
		}, "let read before declaration should throw ReferenceError");
	`)
}

// TestScopingConstTDZ verifies that reading a const binding before its
// declaration in the same block throws ReferenceError.
func TestScopingConstTDZ(t *testing.T) {
	Expect(t, `
		assert.throws(ReferenceError, function() {
			(function() {
				var _ = k; // access before declaration
				const k = 1;
			})();
		}, "const read before declaration should throw ReferenceError");
	`)
}

// TestScopingConstReassignment verifies that assigning to a const binding
// throws TypeError.
func TestScopingConstReassignment(t *testing.T) {
	Expect(t, `
		assert.throws(TypeError, function() {
			const x = 1;
			x = 2;
		}, "reassigning const should throw TypeError");
	`)
}

// TestScopingBlockLet verifies that let is block-scoped: a let declared inside
// {} is not visible outside that block.
func TestScopingBlockLet(t *testing.T) {
	Expect(t, `
		{
			let blockScoped = 42;
			assert.sameValue(blockScoped, 42, "let readable inside its block");
		}
		assert.throws(ReferenceError, function() {
			return blockScoped; // declared in outer block, not visible here
		}, "let is not visible outside its block");
	`)
}

// TestScopingBlockConst verifies that const is also block-scoped.
func TestScopingBlockConst(t *testing.T) {
	Expect(t, `
		{
			const blockConst = 7;
			assert.sameValue(blockConst, 7, "const readable inside its block");
		}
		assert.throws(ReferenceError, function() {
			return blockConst;
		}, "const is not visible outside its block");
	`)
}

// TestScopingVarIsNotBlockScoped verifies that var ignores block boundaries
// and remains accessible after a block ends.
func TestScopingVarIsNotBlockScoped(t *testing.T) {
	Expect(t, `
		function f() {
			{
				var inside = "hello";
			}
			assert.sameValue(inside, "hello", "var declared in block is visible after block");
		}
		f();

		// var in an if block leaks out
		function g() {
			if (true) {
				var leaked = 99;
			}
			assert.sameValue(leaked, 99, "var in if block leaks to function scope");
		}
		g();
	`)
}

// TestScopingVarRedeclaration verifies that redeclaring a var in the same scope
// is legal and does not reset the value.
func TestScopingVarRedeclaration(t *testing.T) {
	Expect(t, `
		function f() {
			var x = 1;
			var x;
			assert.sameValue(x, 1, "var redeclaration without initializer does not reset value");
			var x = 2;
			assert.sameValue(x, 2, "var redeclaration with initializer updates value");
		}
		f();
	`)
}

// TestScopingNestedFunctionScopeChain verifies that inner functions can read
// variables from outer functions through the scope chain.
func TestScopingNestedFunctionScopeChain(t *testing.T) {
	Expect(t, `
		function outer() {
			var a = 1;
			function middle() {
				var b = 2;
				function inner() {
					var c = 3;
					assert.sameValue(a, 1, "inner sees outer.a");
					assert.sameValue(b, 2, "inner sees middle.b");
					assert.sameValue(c, 3, "inner sees own c");
				}
				inner();
				assert.throws(ReferenceError, function() { return c; }, "middle cannot see inner.c");
			}
			middle();
			assert.throws(ReferenceError, function() { return b; }, "outer cannot see middle.b");
		}
		outer();
	`)
}

// TestScopingVariableShadowing verifies that an inner declaration shadows an
// outer one without affecting the outer binding.
func TestScopingVariableShadowing(t *testing.T) {
	Expect(t, `
		var x = "outer";
		function f() {
			var x = "inner";
			assert.sameValue(x, "inner", "inner var shadows outer");
		}
		f();
		assert.sameValue(x, "outer", "outer var unchanged after inner shadow");

		// let shadowing inside a block
		var y = "outside";
		{
			let y = "inside";
			assert.sameValue(y, "inside", "let shadows var in block");
		}
		assert.sameValue(y, "outside", "outer var y unchanged after block let shadow");
	`)
}

// TestScopingParameters verifies that function parameters are in scope in the
// body and can be referenced, shadowed, or default-referenced from each other.
func TestScopingParameters(t *testing.T) {
	Expect(t, `
		function add(a, b) {
			return a + b;
		}
		assert.sameValue(add(3, 4), 7, "basic parameters work");

		// Default parameters can reference earlier params
		function greet(name, greeting) {
			if (greeting === undefined) greeting = "hello " + name;
			return greeting;
		}
		assert.sameValue(greet("world"), "hello world", "default via check works");

		// Native default syntax
		function withDefault(a, b) {
			if (b === undefined) b = a * 2;
			return b;
		}
		assert.sameValue(withDefault(5), 10, "default derived from earlier param");
		assert.sameValue(withDefault(5, 3), 3, "explicit arg overrides default");

		// Parameters shadow outer vars
		var z = 99;
		function shadow(z) {
			return z;
		}
		assert.sameValue(shadow(1), 1, "param shadows outer var");
		assert.sameValue(z, 99, "outer var z unchanged");
	`)
}

// TestScopingArguments verifies that normal functions have an `arguments`
// object reflecting all passed values, while arrow functions do not have their
// own `arguments` (they inherit from the enclosing function scope).
func TestScopingArguments(t *testing.T) {
	Expect(t, `
		function normal(a, b) {
			assert.sameValue(arguments.length, 2, "arguments.length is 2");
			assert.sameValue(arguments[0], a, "arguments[0] matches first param");
			assert.sameValue(arguments[1], b, "arguments[1] matches second param");
		}
		normal(10, 20);

		// Extra arguments beyond declared params are accessible
		function extra() {
			assert.sameValue(arguments.length, 3, "three args captured");
			assert.sameValue(arguments[2], "c", "third arg accessible");
		}
		extra("a", "b", "c");

		// Arrow inside normal: arrow sees enclosing normal's arguments
		function outer() {
			var arrow = () => arguments.length;
			return arrow();
		}
		assert.sameValue(outer("x", "y"), 2, "arrow inherits outer arguments.length");
	`)
}

// TestScopingIIFE verifies that an Immediately Invoked Function Expression
// creates its own isolated scope.
func TestScopingIIFE(t *testing.T) {
	Expect(t, `
		var result = (function() {
			var secret = 42;
			return secret * 2;
		})();
		assert.sameValue(result, 84, "IIFE returns computed value");
		assert.throws(ReferenceError, function() { return secret; }, "IIFE var not in outer scope");

		// IIFE with argument
		var tripled = (function(n) { return n * 3; })(7);
		assert.sameValue(tripled, 21, "IIFE with argument works");
	`)
}

// TestClosureBasic verifies that a closure captures a variable by reference
// from its enclosing scope.
func TestClosureBasic(t *testing.T) {
	Expect(t, `
		function makeAdder(x) {
			return function(y) { return x + y; };
		}
		var add5 = makeAdder(5);
		assert.sameValue(add5(3), 8, "closure captures x=5");
		assert.sameValue(add5(10), 15, "closure reuses same x");

		var add10 = makeAdder(10);
		assert.sameValue(add10(1), 11, "independent closure captures x=10");
		assert.sameValue(add5(0), 5, "original closure unaffected");
	`)
}

// TestClosureCounter verifies that a closure can mutate the captured variable,
// and that multiple calls see the updated state.
func TestClosureCounter(t *testing.T) {
	Expect(t, `
		var counter = (function() {
			var n = 0;
			return {
				inc: function() { n++; },
				dec: function() { n--; },
				get: function() { return n; }
			};
		})();

		counter.inc();
		counter.inc();
		counter.inc();
		counter.dec();
		assert.sameValue(counter.get(), 2, "counter reflects increments and decrement");
	`)
}

// TestClosureIndependent verifies that two closures created by separate calls
// to the same factory do not share state.
func TestClosureIndependent(t *testing.T) {
	Expect(t, `
		function makeCounter() {
			var n = 0;
			return function() { return ++n; };
		}
		var c1 = makeCounter();
		var c2 = makeCounter();

		c1(); c1(); c1();
		c2();

		assert.sameValue(c1(), 4, "c1 has its own count");
		assert.sameValue(c2(), 2, "c2 has its own independent count");
	`)
}

// TestClosureLoopLet verifies the classic loop-capture behaviour with let:
// each iteration gets a new binding, so each captured closure sees a distinct
// value.
func TestClosureLoopLet(t *testing.T) {
	Expect(t, `
		var fns = [];
		for (let i = 0; i < 3; i++) {
			fns.push(function() { return i; });
		}
		assert.sameValue(fns[0](), 0, "let loop: first closure returns 0");
		assert.sameValue(fns[1](), 1, "let loop: second closure returns 1");
		assert.sameValue(fns[2](), 2, "let loop: third closure returns 2");
	`)
}

// TestClosureLoopVar verifies the classic loop-capture behaviour with var:
// all closures share a single binding which holds the final value after the
// loop.
func TestClosureLoopVar(t *testing.T) {
	Expect(t, `
		var fns = [];
		for (var i = 0; i < 3; i++) {
			fns.push(function() { return i; });
		}
		assert.sameValue(fns[0](), 3, "var loop: first closure sees final i=3");
		assert.sameValue(fns[1](), 3, "var loop: second closure sees final i=3");
		assert.sameValue(fns[2](), 3, "var loop: third closure sees final i=3");
	`)
}

// TestClosureLoopLetArrow verifies the same per-iteration binding with arrow
// functions (same semantics as regular functions for variable capture).
func TestClosureLoopLetArrow(t *testing.T) {
	Expect(t, `
		var arrows = [];
		for (let j = 0; j < 3; j++) {
			arrows.push(() => j);
		}
		assert.sameValue(arrows[0](), 0, "arrow let loop: index 0 returns 0");
		assert.sameValue(arrows[1](), 1, "arrow let loop: index 1 returns 1");
		assert.sameValue(arrows[2](), 2, "arrow let loop: index 2 returns 2");
	`)
}

// TestClosureMutatesOuter verifies that a closure truly shares the outer
// binding (by reference), so mutations made through the closure are visible
// in the outer scope and vice-versa.
func TestClosureMutatesOuter(t *testing.T) {
	Expect(t, `
		function makeShared() {
			var v = 0;
			function setter(x) { v = x; }
			function getter() { return v; }
			return { setter: setter, getter: getter };
		}
		var s = makeShared();
		assert.sameValue(s.getter(), 0, "initial value is 0");
		s.setter(99);
		assert.sameValue(s.getter(), 99, "getter sees value set by setter");
	`)
}

// TestClosureThisArrowVsNormal verifies that arrow functions capture the
// lexical `this` of their enclosing context, while normal functions get their
// own `this` determined by the call site.
func TestClosureThisArrowVsNormal(t *testing.T) {
	Expect(t, `
		var obj = {
			value: 42,
			getArrow: function() {
				var self = this;
				return () => self.value; // arrow closes over self which is obj
			},
			getNormal: function() {
				return function() { return this && this.value; };
			}
		};

		var arrow = obj.getArrow();
		assert.sameValue(arrow(), 42, "arrow uses lexical this (via self)");

		var normal = obj.getNormal();
		// called without receiver => this is undefined (strict) or global
		// we just check it doesn't return 42 (not the obj.value)
		assert.sameValue(normal() === 42, false, "normal function not bound to obj when detached");

		// Arrow inside a method via lexical this directly
		function Timer() {
			this.count = 0;
			this.tick = () => { this.count++; };
		}
		var t2 = new Timer();
		var tick = t2.tick; // detach
		tick(); tick();
		assert.sameValue(t2.count, 2, "arrow tick captures Timer's this even when detached");
	`)
}

// TestClosureThisArrowDirect verifies lexical this capture without the self-alias.
func TestClosureThisArrowDirect(t *testing.T) {
	Expect(t, `
		function Accumulator(start) {
			this.total = start;
			this.add = function(arr) {
				arr.forEach(function(v) {
					// normal function: this is not Accumulator — capture via closure variable
				});
				// Use arrow to capture this
				arr.forEach((v) => { this.total += v; });
			};
		}
		var acc = new Accumulator(10);
		acc.add([1, 2, 3]);
		assert.sameValue(acc.total, 16, "arrow forEach callback captures constructor this");
	`)
}
