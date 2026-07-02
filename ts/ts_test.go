package ts

import (
	"strings"
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

// TestEnum runs a TypeScript enum end to end, exercising both forward and
// reverse (Level[n]) mappings after enum lowering.
func TestEnum(t *testing.T) {
	vm := interp.New()
	v, err := RunString(vm, "e.ts",
		"enum Level { Low, Medium, High }\nLevel.High + ':' + Level[Level.High];\n")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := vm.ToString(v)
	if s != "2:High" {
		t.Fatalf("got %q, want %q", s, "2:High")
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

// TestStrictVsPermissive: malformed TS is rejected by default but tolerated
// under Permissive().
func TestStrictVsPermissive(t *testing.T) {
	src := map[string]string{"bad.ts": "const x: number = 1;\n,oops!!!\n"}
	base := interp.NewMapModuleProvider(src)

	_, errStrict := interp.New(interp.WithModuleProvider(Provider(base))).
		RunString("<e>", `require("./bad.ts")`)
	if errStrict == nil || !strings.Contains(errStrict.Error(), "bad.ts:2") {
		t.Fatalf("strict should reject with bad.ts:2, got: %v", errStrict)
	}

	_, errPerm := interp.New(interp.WithModuleProvider(Provider(base, Permissive()))).
		RunString("<e>", `require("./bad.ts")`)
	if errPerm != nil && strings.Contains(errPerm.Error(), "bad.ts:2:") {
		t.Fatalf("permissive should not reject at transpile, got: %v", errPerm)
	}
}
