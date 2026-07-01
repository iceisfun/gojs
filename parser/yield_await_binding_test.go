package parser

import "testing"

// `yield` is a reserved word in strict-mode code and in a generator body;
// `await` is reserved in an async function body. None may be used as a binding
// identifier there (ECMA-262 BindingIdentifier static semantics). Both stay
// usable as ordinary identifiers in sloppy, non-generator/non-async code.

func TestYieldAwaitBindingReserved(t *testing.T) {
	bad := []string{
		`"use strict"; var yield;`,
		`function* g() { var yield; }`,
		`function* g() { let yield = 1; }`,
		`class C { *g() { var yield; } }`,
		`class C { static async *g() { var yield; } }`,
		`async function f() { var await; }`,
		`async function f() { let await = 1; }`,
		`class C { async m() { var await; } }`,
		`async () => { var await; };`,
		`function f() { async function h() { var await; } }`, // inner async reserves await
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for reserved binding: %s", src)
		}
	}
}

// yield used as an identifier reference (not the yield operator) is likewise
// reserved in a generator or strict context.
func TestYieldAsIdentifierReferenceReserved(t *testing.T) {
	bad := []string{
		`function* g() { void yield; }`,
		`class C { *g() { void yield; } }`,
		`class C { static async *g() { void yield; } }`,
		`"use strict"; void yield;`,
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for yield identifier reference: %s", src)
		}
	}
	good := []string{
		`function f() { void yield; }`,       // sloppy, non-generator
		`void yield;`,                        // sloppy top level
		`function* g() { yield; }`,           // yield operator (statement)
		`function* g() { var x = yield 1; }`, // yield operator
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("valid yield use wrongly rejected: %s -> %v", src, err)
		}
	}
}

func TestYieldAwaitUsableAsIdentifiers(t *testing.T) {
	good := []string{
		`function f() { var yield; }`,                        // sloppy, non-generator
		`function f() { var await = 1; }`,                    // sloppy, non-async
		`var yield = 1;`,                                     // sloppy top level
		`var await = 1;`,                                     // sloppy top level
		`function* g() { yield 1; }`,                         // yield as operator
		`function* g() { var x = yield; }`,                   // yield operator, no argument
		`async function f() { await 1; }`,                    // await as operator
		`function* g() { function h() { var yield; } }`,      // nested non-generator function
		`function* g() { function h(x, yield) {} }`,          // nested non-generator parameter
		`async function f() { function h() { var await; } }`, // nested non-async function
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("valid identifier use wrongly rejected: %s -> %v", src, err)
		}
	}
}
