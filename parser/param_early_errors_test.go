package parser

import "testing"

// A function whose parameter list is not simple (it contains a destructuring
// pattern, a default, or a rest element) may not have a "use strict" directive
// in its body (ECMA-262 15.2.1: ContainsUseStrict && !IsSimpleParameterList).
func TestUseStrictWithNonSimpleParams(t *testing.T) {
	bad := []string{
		`function f([a]) { "use strict"; }`,
		`function f({a}) { "use strict"; }`,
		`function f(a, ...r) { "use strict"; }`,
		`function f(a = 1) { "use strict"; }`,
		`var o = { m([a]) { "use strict"; } };`,
		`class C { m(a, ...r) { "use strict"; } }`,
		`([a]) => { "use strict"; };`,
		`(a = 1) => { "use strict"; };`,
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for use-strict with non-simple params: %s", src)
		}
	}
	good := []string{
		`function f(a, b) { "use strict"; }`, // simple params
		`function f([a]) { return a; }`,      // non-simple, but no directive
		`"use strict"; function f([a]) {}`,   // strict context, no own directive
		`class C { m([a]) {} }`,              // methods are strict, but no directive
		`(a, b) => { "use strict"; };`,       // simple arrow
		`([a]) => a;`,                        // expression body: no directive possible
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("valid function wrongly rejected: %s -> %v", src, err)
		}
	}
}

// A rest parameter must be the final formal parameter: neither another
// parameter nor a trailing comma may follow it (ECMA-262 15.1.1).
func TestRestParameterMustBeLast(t *testing.T) {
	bad := []string{
		`function f(...a,) {}`,
		`function f(a, ...b,) {}`,
		`function f(...a, b) {}`,
		`var o = { m(...a,) {} };`,
		`class C { m(...a,) {} }`,
		`(...a,) => 0;`,
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for rest parameter not last: %s", src)
		}
	}
	good := []string{
		`function f(...a) {}`,
		`function f(a, ...b) {}`,
		`function f(a, b,) {}`, // trailing comma after a simple param is fine
		`(...a) => 0;`,
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("valid rest parameter wrongly rejected: %s -> %v", src, err)
		}
	}
}
