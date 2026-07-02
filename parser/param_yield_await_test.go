package parser

import "testing"

// A YieldExpression may not appear in a generator's formal parameter list, and
// an AwaitExpression may not appear in an async function's formal parameter
// list (ECMA-262 UniqueFormalParameters / CreateDynamicFunction). Both remain
// legal in the corresponding function body.

func TestYieldAwaitInParametersRejected(t *testing.T) {
	bad := []string{
		`function* g(a = yield) {}`,
		`function* g(a = yield 1) {}`,
		`(function*(a = yield) {})`,
		`async function* g(a = yield) {}`,
		`async function f(a = await 1) {}`,
		`(async function(a = await 1) {})`,
		`async function* g(a = await 1) {}`,
		`({ *m(a = yield) {} })`,
		`({ async m(a = await 1) {} })`,
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for yield/await in parameters: %s", src)
		}
	}
}

func TestYieldAwaitInBodyAllowed(t *testing.T) {
	good := []string{
		`function* g(a) { yield a; }`,
		`async function f(a) { return await a; }`,
		`async function* g(a) { yield await a; }`,
		// A nested (non-generator/non-async) function in a default value has its
		// own parameter context, so yield/await there is an identifier, not an
		// operator, and must not trip the parameter early error.
		`function* g(a = function() { var yield; }) {}`,
		`async function f(a = function() { var await; }) {}`,
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("unexpected SyntaxError for %s: %v", src, err)
		}
	}
}
