package interp

import "testing"

func TestModuleProviderRequire(t *testing.T) {
	vm := New(WithModuleProvider(NewMapModuleProvider(map[string]string{
		"math.js":  `exports.add = (a, b) => a + b; exports.pi = 3.14;`,
		"greet.js": `const m = require("./math.js"); module.exports = (n) => "hi " + n + " " + m.pi;`,
	})))
	defer vm.Close()

	v, err := vm.RunString("main", `
		const g = require("greet.js");
		const m = require("math.js");
		g("x") + "|" + m.add(2, 3);
	`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got, _ := vm.ToString(v)
	if got != "hi x 3.14|5" {
		t.Errorf("require result = %q", got)
	}

	// Caching: the same module object is returned each time.
	v2, err := vm.RunString("main2", `require("math.js") === require("math.js")`)
	if err != nil {
		t.Fatal(err)
	}
	if got2, _ := vm.ToString(v2); got2 != "true" {
		t.Errorf("module not cached: %q", got2)
	}
}

func TestRequireAbsentWithoutProvider(t *testing.T) {
	vm := New()
	defer vm.Close()
	// Without a ModuleProvider, require is not defined.
	if _, err := vm.RunString("t", `typeof require`); err != nil {
		t.Fatal(err)
	}
	v, _ := vm.RunString("t2", `typeof require`)
	if s, _ := vm.ToString(v); s != "undefined" {
		t.Errorf("require should be undefined without a provider, got %q", s)
	}
}
