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
	"encoding/json"
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

// parseRunArgs parses the tokens after "gojs run" into the entry file, the
// --permissive flag, and the script arguments. Flags are only recognized BEFORE
// the entry file (the conventional `run [flags] <file> [args…]` shape), so a
// `--permissive` after <file> is passed through to the script, not consumed as a
// runner flag (matching Node/Deno-style argument handling).
func parseRunArgs(args []string) (file string, permissive bool, scriptArgs []string) {
	for _, a := range args {
		switch {
		case file == "" && (a == "--permissive" || a == "-p"):
			permissive = true
		case file == "":
			file = a
		default:
			scriptArgs = append(scriptArgs, a)
		}
	}
	return file, permissive, scriptArgs
}

// entryRequireSource builds the bootstrap program that requires the entry module
// by its base name. The name is JSON-encoded so any character valid in a
// filename — quotes, backslashes, non-ASCII — produces a well-formed JS string
// literal instead of breaking the generated source (a JSON string is a valid JS
// string literal).
func entryRequireSource(base string) string {
	spec, _ := json.Marshal("./" + base)
	return "require(" + string(spec) + ")"
}

func main() {
	// usage: gojs run [--permissive] <file> [args…]
	file, permissive, scriptArgs := parseRunArgs(safeArgs(os.Args))
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
	proc, err := process.Install(vm, process.WithArgs(append([]string{"gojs", abs}, scriptArgs...)...))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gojs:", err)
		os.Exit(1)
	}

	// Bootstrap by requiring the entry module, so it runs with a module scope
	// (module/exports/require) and its imports resolve.
	_, runErr := vm.RunString("<entry>", entryRequireSource(filepath.Base(abs)))
	// The program (and its drained event loop) has finished: flush any buffered
	// trailing partial line from process.stdout/stderr.write so it is not lost on
	// normal completion (process.exit flushes on its own path).
	proc.Flush()
	if runErr != nil {
		err := runErr
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

	// A promise that rejected and was never handled is an error the program
	// swallowed silently; report each and exit non-zero, as Node does.
	if rejections := vm.TakeUnhandledRejections(); len(rejections) > 0 {
		for _, reason := range rejections {
			label := "Uncaught (in promise)"
			if color {
				label = "\x1b[31mUncaught (in promise)\x1b[0m"
			}
			fmt.Fprintln(os.Stderr, label, vm.FormatError(reason))
		}
		os.Exit(1)
	}
}
