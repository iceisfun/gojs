// Example: a hardened sandbox running untrusted code.
//
// Runs a snippet with output captured, dynamic code evaluation disabled, and a
// wall-clock timeout via context cancellation — the posture you would use for
// evaluating untrusted user scripts.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/iceisfun/gojs"
)

func main() {
	// A short deadline: a runaway loop is interrupted at the next statement
	// check when the context expires.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	vm := gojs.New(
		gojs.WithContext(ctx),
		// No print provider: the script cannot write to the host's stdout.
		// Hardening: refuse dynamic code and prototype mutation.
		gojs.WithSecurity(gojs.Security{
			DisableEval:          true,
			DisableFunctionCtor:  true,
			DisableProtoMutation: true,
		}),
	)
	defer vm.Close()

	run := func(label, src string) {
		_, err := vm.RunString(label, src)
		if err != nil {
			if v, ok := gojs.ThrownValue(err); ok {
				fmt.Printf("%s -> threw %s\n", label, gojs.BriefValue(v))
			} else {
				fmt.Printf("%s -> %v\n", label, err)
			}
			return
		}
		fmt.Printf("%s -> completed\n", label)
	}

	// Pure computation is allowed.
	run("compute", `let s = 0; for (let i = 0; i < 1000; i++) s += i; s;`)

	// eval is refused.
	run("eval", `eval("1 + 1")`)

	// The Function constructor is refused.
	run("Function", `Function("return 1")()`)

	// A runaway loop is stopped by the context deadline.
	run("runaway", `while (true) {}`)
}
