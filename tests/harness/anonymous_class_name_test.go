package harness

import "testing"

// Named evaluation gives an anonymous class expression the name of the binding
// it is assigned to, but a static member named "name" (part of the class body)
// takes precedence, and a named class expression keeps its own name
// (ECMA-262 ClassDefinitionEvaluation + SetFunctionName).
func TestAnonymousClassNamedEvaluation(t *testing.T) {
	Expect(t, `
		class C {
			method([cls = class {}, named = class X {}, withStatic = class { static name() {} }]) {
				assert.sameValue(cls.name, "cls", "anonymous class takes the binding name");
				assert.sameValue(named.name, "X", "a named class keeps its own name");
				assert.sameValue(typeof withStatic.name, "function",
					"a static name member is not overwritten by named evaluation");
			}
		}
		new C().method([]);
	`)
}

func TestAnonymousClassNameContexts(t *testing.T) {
	Expect(t, `
		assert.sameValue((class {}).name, "", "bare anonymous class has empty name");
		assert.sameValue((class X {}).name, "X", "named class expression");
		var a = class {};
		assert.sameValue(a.name, "a", "assignment to a variable");
		let [b = class {}] = [];
		assert.sameValue(b.name, "b", "array destructuring default");
		let { c = class {} } = {};
		assert.sameValue(c.name, "c", "object destructuring default");
		function f(p = class {}) { return p.name; }
		assert.sameValue(f(), "p", "parameter default");
	`)
}
