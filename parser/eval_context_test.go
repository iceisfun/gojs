package parser

import "testing"

// Ordinary parsing leaves super.property and new.target permitted (their
// placement is checked at runtime), so gating them must not reject valid or
// previously-accepted normal code.
func TestNewTargetSuperNormalParsingUnaffected(t *testing.T) {
	ok := []string{
		`function f() { return new.target; }`,
		`class C { m() { return super.x; } }`,
		`class C extends B { constructor() { super(); return super.y; } }`,
		`class C { x = super.y; }`,
		`({ m() { return super.foo; } });`,
		`class C { *m() { return new.target; } }`,
		`class C extends B { constructor() { (() => super.z)(); } }`, // arrow inherits
	}
	for _, src := range ok {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("normal parse wrongly rejected: %s -> %v", src, err)
		}
	}
}

// ParseEval seeds the caller's context: an indirect/global eval forbids super
// and new.target, while a direct eval in a method/function permits them, and a
// nested function or method within the eval source re-enables them.
func TestEvalContextGatesNewTargetAndSuper(t *testing.T) {
	rejected := []struct {
		src string
		ec  EvalContext
	}{
		{`new.target`, EvalContext{}},
		{`super.x`, EvalContext{}},
		{`super()`, EvalContext{}},
		{`super.x`, EvalContext{AllowNewTarget: true}},        // a function, but not a method
		{`new.target`, EvalContext{AllowSuperProperty: true}}, // not a function
	}
	for _, c := range rejected {
		if _, err := ParseEval("eval", c.src, c.ec); err == nil {
			t.Errorf("eval context should reject %q with %+v", c.src, c.ec)
		}
	}
	accepted := []struct {
		src string
		ec  EvalContext
	}{
		{`new.target`, EvalContext{AllowNewTarget: true}},
		{`super.x`, EvalContext{AllowSuperProperty: true}},
		{`super()`, EvalContext{AllowSuperCall: true}},
		{`this.#p`, EvalContext{PrivateNames: []string{"#p"}}},
		{`function g() { return new.target; }`, EvalContext{}}, // nested function re-enables
		{`({ m() { return super.x; } })`, EvalContext{}},       // nested method re-enables
	}
	for _, c := range accepted {
		if _, err := ParseEval("eval", c.src, c.ec); err != nil {
			t.Errorf("eval context should accept %q with %+v -> %v", c.src, c.ec, err)
		}
	}
}
