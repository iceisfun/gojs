// Example: exposing Go functions and data to JavaScript.
//
// The host installs Go-backed functions and a config object as globals; the
// script calls into them as if they were ordinary JavaScript.
package main

import (
	"log"
	"os"
	"strings"

	"github.com/iceisfun/gojs"
)

func main() {
	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
	)
	defer vm.Close()

	// Expose a Go function. A HostFunc receives the JS call arguments and
	// returns a value (or an error, which is thrown into the script).
	vm.SetGlobal("shout", vm.NewFunction("shout", func(args []gojs.Value) (gojs.Value, error) {
		s, err := vm.ToString(args[0])
		if err != nil {
			return nil, err
		}
		return gojs.String(strings.ToUpper(s) + "!"), nil
	}))

	// Expose a Go function that can throw a JavaScript exception.
	vm.SetGlobal("mustPositive", vm.NewFunction("mustPositive", func(args []gojs.Value) (gojs.Value, error) {
		n := float64(args[0].(gojs.Number))
		if n <= 0 {
			return nil, gojs.NewThrow(vm.NewError("RangeError", "expected a positive number"))
		}
		return gojs.Number(n), nil
	}))

	// Expose structured host data. FromGo converts Go values to JS.
	vm.SetGlobal("host", vm.FromGo(map[string]any{
		"name": "gojs",
		"env":  os.Getenv("USER"),
		"nums": []any{1, 2, 3},
	}))

	source := `
		console.log(shout("hello"));
		console.log("host.name =", host.name);
		console.log("sum =", host.nums.reduce((a, b) => a + b, 0));
		try {
			mustPositive(-5);
		} catch (e) {
			console.log("caught:", e.name + ":", e.message);
		}
		console.log("ok:", mustPositive(42));
	`
	if _, err := vm.RunString("expose.js", source); err != nil {
		log.Fatalf("run error: %v", err)
	}
}
