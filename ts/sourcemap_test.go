package ts

import (
	"strings"
	"testing"

	"github.com/iceisfun/gojs/interp"
)

// TestSourceMappedStack runs a TypeScript module that throws and checks the
// error's stack reports the original .ts line (not the transpiled-JS line).
func TestSourceMappedStack(t *testing.T) {
	src := map[string]string{
		// throw is on TS line 2.
		"app.ts": "function boom(): void {\n  throw new Error('kaboom');\n}\nboom();\n",
	}
	base := interp.NewMapModuleProvider(src)
	vm := interp.New(WithTypeScript(base)...)
	defer vm.Close()

	v, err := vm.RunString("<entry>", `
		let s = "";
		try { require("./app.ts"); } catch (e) { s = e.stack; }
		s;
	`)
	if err != nil {
		t.Fatal(err)
	}
	stack, _ := vm.ToString(v)
	if !strings.Contains(stack, "Error: kaboom") {
		t.Fatalf("stack missing message:\n%s", stack)
	}
	// The frame should point at the .ts source, line 2 — not the transpiled JS.
	if !strings.Contains(stack, "app.ts:2:") {
		t.Fatalf("stack not source-mapped to app.ts:2:\n%s", stack)
	}
	t.Logf("stack:\n%s", stack)
}

// TestFormatErrorTS renders a rich, source-mapped, multi-frame stack with a code
// frame for an uncaught TypeScript error.
func TestFormatErrorTS(t *testing.T) {
	src := map[string]string{
		"app.ts": "function b(): void {\n  throw new Error(\"boom\");\n}\nfunction a(): void { b(); }\na();\n",
	}
	vm := interp.New(append(WithTypeScript(interp.NewMapModuleProvider(src)), interp.WithErrorColor(false))...)
	defer vm.Close()
	_, err := vm.RunString("<entry>", `require("./app.ts")`)
	v, ok := interp.ThrownValue(err)
	if !ok {
		t.Fatalf("no thrown value: %v", err)
	}
	out := vm.FormatError(v)
	for _, want := range []string{"Error: boom", "at b (app.ts:2:3)", "at a (app.ts:4", "> 2 |", "throw new Error"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
