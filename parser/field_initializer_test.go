package parser

import "testing"

// A class field initializer may not contain `arguments` or a SuperCall
// (super(...)). Both restrictions look through arrow functions but stop at a
// regular function or method boundary, which introduce their own arguments and
// super bindings. SuperProperty (super.x) remains allowed (ECMA-262 15.7.1,
// FieldDefinition static semantics).

func TestFieldInitializerEarlyErrors(t *testing.T) {
	bad := []string{
		`class C { x = () => super(); }`,
		`class C { x = () => arguments; }`,
		`class C { #x = super(); }`,
		`class C { #x = () => arguments; }`,
		`class C { static #x = arguments; }`,
		`class C { static #x = super(); }`,
		`class C { x = typeof super(); }`,
		`class C { x = false ? {} : arguments; }`,
		`class C { static [y] = () => arguments; }`,
		`class C { x = () => { var t = () => super(); }; }`, // through nested arrows and a block
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for field initializer: %s", src)
		}
	}
}

func TestFieldInitializerAllowed(t *testing.T) {
	good := []string{
		`class C extends B { constructor() { super(); } }`,   // super() in constructor
		`class C extends B { m() { return super.foo; } }`,    // super property in method
		`class C { x = super.foo; }`,                         // super property in field initializer
		`class C { x = function () { return arguments; }; }`, // regular function's own arguments
		`class C { m() { return arguments; } }`,              // arguments in a method body
		`class C { x = this.arguments; }`,                    // a property named "arguments"
		`class C { x = () => this.foo(); }`,                  // ordinary arrow initializer
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("valid field initializer wrongly rejected: %s -> %v", src, err)
		}
	}
}
