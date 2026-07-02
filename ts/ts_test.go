package ts

import (
	"testing"

	"github.com/iceisfun/gojs/interp"
)

// TestRunString runs a self-contained TypeScript program and checks its
// completion value — proving type-stripping + execution end to end.
func TestRunString(t *testing.T) {
	vm := interp.New()
	v, err := RunString(vm, "prog.ts",
		"const x: number = 40;\nconst y: number = 2;\nx + y;\n")
	if err != nil {
		t.Fatal(err)
	}
	if v != interp.Number(42) {
		t.Fatalf("got %v, want 42", v)
	}
}

// TestRequireTypeScript proves the Provider transpiles TypeScript modules on
// load: a JS entry requires a .ts module whose ES export/typed API is lowered to
// CommonJS and executed.
func TestRequireTypeScript(t *testing.T) {
	src := map[string]string{
		"math.ts":  "export function add(a: number, b: number): number { return a + b; }\n",
		"main.ts":  "import { add } from './math';\nconst r: number = add(40, 2);\nmodule.exports = r;\n",
	}
	base := interp.NewMapModuleProvider(src)
	vm := interp.New(interp.WithModuleProvider(Provider(base)))

	v, err := vm.RunString("<entry>", "require('./main.ts')")
	if err != nil {
		t.Fatal(err)
	}
	if v != interp.Number(42) {
		t.Fatalf("got %v, want 42", v)
	}
}
