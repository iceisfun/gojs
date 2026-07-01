// Example: choosing the RegExp backend.
//
// gojs ships two regular-expression engines and lets the host pick per VM:
//
//   - RegExpCompat (default) — the pure-Go jsregexp engine, a full ECMAScript
//     implementation: backreferences, lookahead/lookbehind, named groups, and
//     u/v Unicode modes, with a step budget that bounds catastrophic
//     backtracking. Correct and sandbox-safe; the right default for real or
//     untrusted JavaScript.
//
//   - RegExpRE2 — Go's regexp package (RE2). Faster and linear-time, but NOT
//     ECMAScript-conformant: backreferences and lookaround do not compile, and
//     capture/flag/Unicode semantics follow RE2. An opt-in for performance over
//     simple, trusted patterns.
package main

import (
	"fmt"

	"github.com/iceisfun/gojs"
)

func main() {
	// A pattern that uses a backreference — valid ECMAScript, but inexpressible
	// in RE2.
	const backref = `/(['"]).*?\1/.exec('say "hi" there')[0]`

	// A simple pattern both engines handle.
	const simple = `'order-42-9000'.match(/[0-9]+/)[0]`

	for _, tc := range []struct {
		name   string
		engine gojs.RegExpEngine
	}{
		{"compat (default)", gojs.RegExpCompat},
		{"re2 (opt-in)", gojs.RegExpRE2},
	} {
		vm := gojs.New(gojs.WithRegExpEngine(tc.engine))

		fmt.Printf("== %s ==\n", tc.name)
		report(vm, "simple  /[0-9]+/", simple)
		report(vm, "backref /(['\"]).*?\\1/", backref)
		fmt.Println()

		vm.Close()
	}

	// Output:
	// == compat (default) ==
	// simple  /[0-9]+/           => 42
	// backref /(['"]).*?\1/      => "hi"
	//
	// == re2 (opt-in) ==
	// simple  /[0-9]+/           => 42
	// backref /(['"]).*?\1/      => uncaught SyntaxError: Invalid regular expression: invalid escape sequence: `\1`
}

func report(vm *gojs.VM, label, src string) {
	v, err := vm.RunString("example.js", src)
	if err != nil {
		fmt.Printf("%-26s => %v\n", label, err)
		return
	}
	fmt.Printf("%-26s => %v\n", label, gojs.BriefValue(v))
}
