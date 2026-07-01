// Example: calling JavaScript functions from Go.
//
// The host loads a script that defines functions, then invokes them from Go
// with converted arguments and reads the results back.
package main

import (
	"fmt"
	"log"

	"github.com/iceisfun/gojs"
)

func main() {
	vm := gojs.New()
	defer vm.Close()

	// Define script-side logic. The functions become globals.
	if _, err := vm.RunString("lib.js", `
		function greet(name) { return "Hello, " + name + "!"; }
		function stats(nums) {
			const total = nums.reduce((a, b) => a + b, 0);
			return { count: nums.length, total, mean: total / nums.length };
		}
	`); err != nil {
		log.Fatal(err)
	}

	// Call greet(name) from Go.
	greet := vm.GetGlobal("greet")
	msg, err := vm.Call(greet, gojs.Undefined, gojs.String("world"))
	if err != nil {
		log.Fatal(err)
	}
	s, _ := vm.ToString(msg)
	fmt.Println(s)

	// Call stats([...]) with a Go slice and read back the object result.
	stats := vm.GetGlobal("stats")
	arg := vm.FromGo([]any{2.0, 4.0, 6.0, 8.0})
	result, err := vm.Call(stats, gojs.Undefined, arg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("stats: %v\n", vm.ToGo(result))
}
