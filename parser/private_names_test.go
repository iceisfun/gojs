package parser

import "testing"

// A reference to a private name (#x) must resolve to a private name declared in
// an enclosing class; otherwise it is an early SyntaxError (ECMA-262
// AllPrivateIdentifiersValid). Because private names are visible throughout
// their class — before their textual declaration and inside nested classes —
// this is validated after the whole program is parsed.

func TestPrivateNameUndeclaredIsSyntaxError(t *testing.T) {
	bad := []string{
		`class C { f = (() => {})().#x }`,      // undeclared, in a field initializer
		`class C { m() { return this.#missing; } }`,
		`class C { m(o) { return #missing in o; } }`, // brand check on undeclared
		`var o = {}; o.#x;`,                          // outside any class
		`this.#x;`,                                   // outside any class
		`class C { #x = 1; } class D { m(o) { return o.#x; } }`, // sibling class
		`class C { #a = 1; m() { return this.#b; } }`,           // wrong name
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for undeclared private name: %s", src)
		}
	}
}

func TestPrivateNameDeclaredIsValid(t *testing.T) {
	good := []string{
		`class C { #x = 1; m() { return this.#x; } }`,
		`class C { m() { return this.#x; } #x = 1; }`, // reference before declaration
		`class C { #x = 1; m(o) { return #x in o; } }`,
		`class O { #o = 1; m() { class I { f() { return this.#o; } } } }`, // outer class
		`class C { #m() {} n() { return this.#m(); } }`,                   // private method
		`class C { get #g() { return 1; } n() { return this.#g; } }`,      // private getter
		`class C { static #s = 1; static read() { return C.#s; } }`,       // static private
		`class Outer { #o = 1; m() { class Inner { #i = 2; f() { return this.#o + this.#i; } } } }`,
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("declared private name wrongly rejected: %s -> %v", src, err)
		}
	}
}
