package harness

import "testing"

// Each evaluation of a class body mints fresh PrivateName identities for the
// private elements it declares. Two instances produced by separate evaluations
// of the same class therefore carry distinct private brands, and each fails the
// other's brand check even though the textual name is identical (ECMA-262 uses
// a PrivateName value, not the source text, as the key).

func TestPrivateBrandDistinctPerClassEvaluation(t *testing.T) {
	Expect(t, `
		function makeC() {
			class C {
				#m() { return "test262"; }
				access(o) { return o.#m(); }
			}
			return new C();
		}
		var c1 = makeC();
		var c2 = makeC();
		assert.sameValue(c1.access(c1), "test262", "an instance accesses its own brand");
		assert.throws(TypeError, function () { c1.access(c2); },
			"c1's method cannot read c2's like-named private (different evaluation)");
		assert.throws(TypeError, function () { c2.access(c1); },
			"and vice versa");
	`)
}

func TestPrivateBrandInOperatorPerEvaluation(t *testing.T) {
	Expect(t, `
		function makeC() {
			class C { #x = 1; has(o) { return #x in o; } }
			return new C();
		}
		var c1 = makeC(), c2 = makeC();
		assert.sameValue(c1.has(c1), true, "own brand present");
		assert.sameValue(c1.has(c2), false, "other evaluation's brand absent");
		assert.sameValue(c1.has({}), false, "plain object has no brand");
	`)
}

// The refactor must preserve ordinary private semantics.
func TestPrivateNamesStillWork(t *testing.T) {
	Expect(t, `
		class A { #a = 1; getA() { return this.#a; } }
		class B extends A { #b = 2; getB() { return this.#b; } }
		var b = new B();
		assert.sameValue(b.getA(), 1, "inherited private via base method");
		assert.sameValue(b.getB(), 2, "own private");

		class S { static #s = 9; static read() { return S.#s; } }
		assert.sameValue(S.read(), 9, "static private");

		class G {
			#v = 0;
			get #p() { return this.#v; }
			set #p(x) { this.#v = x; }
			bump() { this.#p = this.#p + 5; return this.#p; }
		}
		assert.sameValue(new G().bump(), 5, "private accessor pair");

		class Outer {
			#o = 8;
			run() { var self = this; class Inner { f() { return self.#o; } } return new Inner().f(); }
		}
		assert.sameValue(new Outer().run(), 8, "inner class reads outer private");
	`)
}
