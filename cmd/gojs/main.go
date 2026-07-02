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

// safeArgs returns the arguments after "gojs run", or nil.
func safeArgs(a []string) []string {
	if len(a) < 3 {
		return nil
	}
	return a[2:]
}

func main() {
	// usage: gojs run [--permissive] <file> [args…]
	permissive := false
	var file string
	var scriptArgs []string
	for _, a := range safeArgs(os.Args) {
		switch {
		case a == "--permissive" || a == "-p":
			permissive = true
		case file == "":
			file = a
		default:
			scriptArgs = append(scriptArgs, a)
		}
	}
	if len(os.Args) < 2 || os.Args[1] != "run" || file == "" {
		fmt.Fprintln(os.Stderr, "usage: gojs run [--permissive] <file.ts|file.js> [args…]")
		os.Exit(2)
	}

	abs, err := filepath.Abs(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gojs:", err)
		os.Exit(1)
	}

	// The entry's directory is the module root. WithTypeScript transpiles .ts
	// modules on load and installs a source mapper, so error stacks report .ts
	// positions; --permissive tolerates TypeScript syntax errors.
	var tsOpts []ts.Option
	if permissive {
		tsOpts = append(tsOpts, ts.Permissive())
	}
	color := os.Getenv("NO_COLOR") == "" // https://no-color.org
	opts := ts.WithTypeScript(interp.NewDirModuleProvider(filepath.Dir(abs)), tsOpts...)
	opts = append(opts,
		interp.WithPrintProvider(interp.NewDefaultPrintProvider()),
		interp.WithTimeProvider(interp.NewDefaultTimeProvider()),
		interp.WithTimerProvider(interp.NewDefaultTimerProvider()),
		interp.WithOsProvider(interp.NewDefaultOsProvider()),
		interp.WithErrorColor(color),
	)
	vm := interp.New(opts...)

	// A Node-like process global for standalone runs. argv is
	// [ "gojs", <script>, <args…> ] to match Node's argv[0]/argv[1] convention.
	if err := process.Install(vm, process.WithArgs(append([]string{"gojs", abs}, scriptArgs...)...)); err != nil {
		fmt.Fprintln(os.Stderr, "gojs:", err)
		os.Exit(1)
	}

	// Bootstrap by requiring the entry module, so it runs with a module scope
	// (module/exports/require) and its imports resolve.
	if _, err := vm.RunString("<entry>", "require('./"+filepath.Base(abs)+"')"); err != nil {
		if v, ok := interp.ThrownValue(err); ok {
			// Rich, colorized, source-mapped stack + code frame for uncaught errors.
			fmt.Fprintln(os.Stderr, vm.FormatError(v))
			hint := "Hint:"
			if color {
				hint = "\x1b[33mHint:\x1b[0m"
			}
			fmt.Fprintln(os.Stderr, hint, "Uncaught exceptions exit with code 1.")
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
