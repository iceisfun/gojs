package interp

import (
	"strings"
	"testing"
)

// run evaluates src on a fresh VM using the given RegExp engine and returns the
// string form of the result (or the error text).
func runWithEngine(t *testing.T, engine RegExpEngine, src string) (string, error) {
	t.Helper()
	vm := New(WithRegExpEngine(engine))
	defer vm.Close()
	v, err := vm.RunString("test.js", src)
	if err != nil {
		return "", err
	}
	s, _ := vm.ToStringV(vm.ctx, v)
	return s, nil
}

func TestRegExpEngineCompatDefault(t *testing.T) {
	// Backreferences and lookaround work on the default (compat) engine.
	got, err := runWithEngine(t, RegExpCompat, `/(['"]).*?\1/.exec('say "hi" ok')[0]`)
	if err != nil {
		t.Fatalf("compat backref: %v", err)
	}
	if got != `"hi"` {
		t.Errorf("compat backref = %q; want %q", got, `"hi"`)
	}
	got, err = runWithEngine(t, RegExpCompat, `/(?<=\$)\d+/.exec('$42')[0]`)
	if err != nil {
		t.Fatalf("compat lookbehind: %v", err)
	}
	if got != "42" {
		t.Errorf("compat lookbehind = %q; want 42", got)
	}
}

func TestRegExpEngineRE2SimpleWorks(t *testing.T) {
	// A simple pattern works on the RE2 engine and returns code-unit-correct
	// offsets.
	got, err := runWithEngine(t, RegExpRE2, `'abc123def'.match(/[0-9]+/)[0]`)
	if err != nil {
		t.Fatalf("re2 simple: %v", err)
	}
	if got != "123" {
		t.Errorf("re2 simple = %q; want 123", got)
	}
	got, err = runWithEngine(t, RegExpRE2, `'a,b,c'.split(/,/).join('|')`)
	if err != nil {
		t.Fatalf("re2 split: %v", err)
	}
	if got != "a|b|c" {
		t.Errorf("re2 split = %q; want a|b|c", got)
	}
}

func TestRegExpEngineRE2RejectsBackref(t *testing.T) {
	// RE2 cannot express backreferences; the constructor must throw SyntaxError.
	_, err := runWithEngine(t, RegExpRE2, `/(a)\1/.test('aa')`)
	if err == nil {
		t.Fatal("RE2 engine should reject a backreference pattern")
	}
	if !strings.Contains(err.Error(), "SyntaxError") && !strings.Contains(err.Error(), "Invalid regular expression") {
		t.Errorf("expected a SyntaxError, got %v", err)
	}
}

func TestRegExpEngineRE2RejectsLookahead(t *testing.T) {
	_, err := runWithEngine(t, RegExpRE2, `/foo(?=bar)/.test('foobar')`)
	if err == nil {
		t.Fatal("RE2 engine should reject a lookahead pattern")
	}
}
