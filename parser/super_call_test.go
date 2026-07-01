package parser

import "testing"

// A SuperCall (super(...)) is a SyntaxError anywhere but a derived class
// constructor: an ordinary method, an accessor, a generator/async method, a
// base-class constructor (no heritage), a field initializer, a nested regular
// function, and top-level code all forbid it (ECMA-262 13.3.7.1). It is
// transparent through arrow functions, and super.property is unaffected.
func TestSuperCallOnlyInDerivedConstructor(t *testing.T) {
	bad := []string{
		`class C extends B { method() { super(); } }`,
		`class C extends B { get x() { super(); } }`,
		`class C extends B { *g() { super(); } }`,
		`class C extends B { async m() { super(); } }`,
		`class C extends Function { async *method() { super(); } }`,
		`class C { constructor() { super(); } }`, // base class: no heritage
		`class C extends B { x = super(); }`,     // field initializer
		`class C extends B { constructor() { function f() { super(); } } }`,
		`super();`, // top level
		`class C extends B { static m() { super(); } }`,
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for misplaced super(): %s", src)
		}
	}
	good := []string{
		`class C extends B { constructor() { super(); } }`,
		`class C extends B { constructor() { super(1, 2); this.x = 1; } }`,
		`class C extends B { constructor() { (() => super())(); } }`, // arrow is transparent
		`class C extends B { m() { super.foo(); } }`,                 // super property, not a call
		`class C extends B { constructor() { if (true) super(); } }`,
		`class C extends B { constructor() { super(); class D extends E { constructor() { super(); } } } }`,
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("valid super() wrongly rejected: %s -> %v", src, err)
		}
	}
}
