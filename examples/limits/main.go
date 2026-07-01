// Example: resource limits for untrusted code.
//
// Limits bound how much a script may consume so a host can run untrusted or
// buggy code without a crash or an unbounded loop:
//   - MaxCallDepth  caps recursion (a catchable RangeError, as in real engines).
//   - MaxSteps      caps evaluation steps (an UNCATCHABLE abort — try/catch
//     cannot swallow it — so a busy loop always terminates).
package main

import (
	"fmt"

	"github.com/iceisfun/gojs"
)

func main() {
	vm := gojs.New(
		gojs.WithLimits(gojs.Limits{
			MaxCallDepth: 200,     // deep recursion -> RangeError
			MaxSteps:     500_000, // busy loops / CPU -> hard stop
		}),
	)
	defer vm.Close()

	run := func(label, src string) {
		_, err := vm.RunString(label, src)
		switch {
		case err == nil:
			fmt.Printf("%-18s completed\n", label)
		default:
			fmt.Printf("%-18s stopped: %v\n", label, err)
		}
	}

	// Ordinary work stays well under the limits.
	run("normal", `let s = 0; for (let i = 0; i < 1000; i++) s += i;`)

	// Infinite recursion trips MaxCallDepth as a RangeError. It is a normal JS
	// exception, so the script itself can catch it and recover.
	run("deep recursion", `
		function f(n) { return f(n + 1); }
		try { f(0); } catch (e) { /* e is a RangeError */ }
	`)

	// A busy loop trips MaxSteps. This abort is UNCATCHABLE: the try/catch here
	// does not swallow it, and execution stops.
	run("busy loop", `
		try {
			while (true) { /* burn steps */ }
		} catch (e) {
			// never reached — the step limit is not a catchable exception
		}
	`)

	fmt.Println("host still in control after all three")
}
