// Command gojs runs JavaScript or TypeScript files on the gojs VM with no
// external tooling: TypeScript is transpiled in-process via the embedded
// typescript-go hoisting (see package github.com/iceisfun/gojs/ts).
//
//	gojs run app.ts
//
// The entry file's directory is the module root; require()/import resolve
// against it, and .ts/.tsx modules are transpiled on load.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/iceisfun/gojs/host/process"
	"github.com/iceisfun/gojs/interp"
	"github.com/iceisfun/gojs/ts"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "usage: gojs run <file.ts|file.js>")
		os.Exit(2)
	}

	abs, err := filepath.Abs(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "gojs:", err)
		os.Exit(1)
	}

	vm := interp.New(
		// The entry's directory is the module root; the ts.Provider transpiles
		// TypeScript modules on load (this is the file-inclusion hook — swap the
		// base provider to serve modules from anywhere).
		interp.WithModuleProvider(ts.Provider(interp.NewDirModuleProvider(filepath.Dir(abs)))),
		interp.WithPrintProvider(interp.NewDefaultPrintProvider()),
		interp.WithTimeProvider(interp.NewDefaultTimeProvider()),
		interp.WithTimerProvider(interp.NewDefaultTimerProvider()),
		interp.WithOsProvider(interp.NewDefaultOsProvider()),
	)

	// A Node-like process global for standalone runs. argv is
	// [ "gojs", <script>, <args…> ] to match Node's argv[0]/argv[1] convention.
	if err := process.Install(vm, process.WithArgs(append([]string{"gojs", abs}, os.Args[3:]...)...)); err != nil {
		fmt.Fprintln(os.Stderr, "gojs:", err)
		os.Exit(1)
	}

	// Bootstrap by requiring the entry module, so it runs with a module scope
	// (module/exports/require) and its imports resolve.
	if _, err := vm.RunString("<entry>", "require('./"+filepath.Base(abs)+"')"); err != nil {
		if v, ok := interp.ThrownValue(err); ok {
			fmt.Fprintln(os.Stderr, interp.BriefValue(v))
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
