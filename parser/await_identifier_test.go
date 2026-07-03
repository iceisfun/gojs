package parser

import "testing"

// `await` is the AwaitExpression operator only where the [Await] grammar
// parameter holds (inside an async function/arrow, or at a module's top level).
// Everywhere else it is an ordinary identifier: in sync functions and
// generators, in script global code, and in any function nested inside an async
// one. A function *declaration*'s BindingIdentifier uses the enclosing [Await],
// so a sync `function await(){}` nested in async code is an early error.

func TestAwaitAsIdentifierAllowed(t *testing.T) {
	good := []string{
		`function foo(await) { return await; }`,                    // param + reference in a sync function
		`function* foo(await) { yield await; }`,                    // sync generator
		`var await = 1; await;`,                                    // script global scope
		`async function outer() { function bar() { await = 1; } }`, // sync fn nested in async
		`async function await() { return 1; }`,                     // async fn *named* await at global
		`function* await() {}`,                                     // generator named await at global
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("expected %q to parse, got error: %v", src, err)
		}
	}
}

func TestAwaitAsIdentifierRejected(t *testing.T) {
	bad := []string{
		`async function f() { var await = 1; }`,      // await reserved in async body
		`async function f() { function await() {} }`, // decl name in async scope
		`async function f() { await; }`,              // bare await operator needs operand
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError for %q", src)
		}
	}
}

// Top-level await remains the operator under the Module goal, and reverts to an
// identifier inside a (sync) function nested at module scope.
func TestModuleTopLevelAwait(t *testing.T) {
	if _, err := ParseModule("test", `var x = await Promise.resolve(1);`); err != nil {
		t.Errorf("top-level await should parse in a module: %v", err)
	}
	if _, err := ParseModule("test", `function f() { var await = 1; return await; }`); err != nil {
		t.Errorf("await is an identifier inside a sync function in a module: %v", err)
	}
}
