// Example: basic script execution.
//
// The simplest embedding: create a VM with a print provider, run a script, and
// read back its completion value.
package main

import (
	"fmt"
	"log"

	"github.com/iceisfun/gojs"
)

func main() {
	// A VM with no providers is a closed sandbox (no console output). Install
	// the default print provider so console.log reaches stdout.
	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
	)
	defer vm.Close()

	source := `
		function factorial(n) {
			return n <= 1 ? 1 : n * factorial(n - 1);
		}
		console.log("factorial(10) =", factorial(10));
		factorial(10); // the program's completion value
	`

	result, err := vm.RunString("factorial.js", source)
	if err != nil {
		log.Fatalf("run error: %v", err)
	}

	// Convert the JS completion value to a Go value.
	fmt.Printf("Go sees: %v\n", vm.ToGo(result))
}
