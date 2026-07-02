package interp

import (
	"strings"
	"testing"
)

func TestFormatErrorPlain(t *testing.T) {
	vm := New(WithErrorColor(false))
	_, err := vm.RunString("app.js", "\nfunction b() { throw new Error(\"deep\"); }\nfunction a() { b(); }\na();\n")
	v, ok := ThrownValue(err)
	if !ok {
		t.Fatalf("no thrown value: %v", err)
	}
	out := vm.FormatError(v)
	for _, want := range []string{
		"Error: deep",
		"at b (app.js:2:16)",
		"at a (app.js:3:16)",
		"at <module> (app.js:4:1)",
		"> 2 |", // code-frame marker at the throw line
		"^",     // caret
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("unexpected ANSI in plain output:\n%s", out)
	}
}

func TestFormatErrorColor(t *testing.T) {
	vm := New() // color on by default
	_, err := vm.RunString("a.js", `throw new Error("x");`)
	v, _ := ThrownValue(err)
	if !strings.Contains(vm.FormatError(v), "\x1b[1;31mError") {
		t.Errorf("expected ANSI color header, got:\n%q", vm.FormatError(v))
	}
}
