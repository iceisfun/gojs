package harness

import "testing"

// A direct call to eval (the callee is the identifier `eval`) runs its code in
// the caller's lexical context: this, super, private names, new.target, and
// local variables all resolve as in the surrounding code (ECMA-262 PerformEval,
// direct = true). An indirect call still runs in the global scope.
func TestDirectEvalInheritsContext(t *testing.T) {
	Expect(t, `
		class Base { greet() { return "base"; } }
		class Derived extends Base {
			#secret = 42;
			#hidden() { return "priv-method"; }
			constructor() {
				eval("super()");            // SuperCall through eval
			}
			viaThis() { return eval("this.#secret"); }
			viaMethod() { return eval("this.#hidden()"); }
			viaSuper() { return eval("super.greet()"); }
			brand(o) { return eval("#secret in o"); }
		}
		var d = new Derived();
		assert.sameValue(d.viaThis(), 42, "private field via direct eval");
		assert.sameValue(d.viaMethod(), "priv-method", "private method via direct eval");
		assert.sameValue(d.viaSuper(), "base", "super property via direct eval");
		assert.sameValue(d.brand(d), true, "private brand check via direct eval");
		assert.sameValue(d.brand({}), false, "brand check is false for a plain object");
	`)
}

func TestDirectEvalLocalsAndThis(t *testing.T) {
	Expect(t, `
		function f() { var y = 3; return eval("y + 1"); }
		assert.sameValue(f(), 4, "direct eval sees the caller's locals");

		var obj = { tag: "T", read() { return eval("this.tag"); } };
		assert.sameValue(obj.read(), "T", "direct eval sees the method's this");

		eval("var leaked = 5;");
		assert.sameValue(leaked, 5, "non-strict direct eval var leaks to the enclosing scope");
	`)
}

func TestIndirectEvalIsGlobal(t *testing.T) {
	Expect(t, `
		var g = 17;
		var e = eval;                 // indirect: not the identifier "eval"
		assert.sameValue(e("g"), 17, "indirect eval resolves globals");
		assert.sameValue((0, eval)("1 + 2"), 3, "the comma form is indirect too");
	`)
}
