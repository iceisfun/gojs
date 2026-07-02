// Example: running TypeScript on the gojs VM, with no external tooling.
//
// TypeScript is transpiled to JavaScript in-process by
// github.com/iceisfun/gojs/ts (which embeds a hoisting of Microsoft's
// typescript-go compiler) and executed by gojs. Transpilation is checker-free:
// types are erased and TypeScript syntax is lowered, but the program is not
// type-checked — the goal is to *run* TypeScript.
//
// ts.Provider wraps any ModuleProvider so that .ts/.tsx modules are transpiled
// on load; file inclusion (resolution + storage) stays under host control. Here
// the two .ts files are embedded and served from an in-memory map, so imports
// between them resolve without touching the filesystem.
//
// Run it from the repository root:
//
//	go run ./examples/typescript
//
// The same files also run through the CLI:
//
//	go run ./cmd/gojs run ./examples/typescript/main.ts
package main

import (
	_ "embed"
	"log"

	"github.com/iceisfun/gojs"
	"github.com/iceisfun/gojs/ts"
)

//go:embed main.ts
var mainTS string

//go:embed geometry.ts
var geometryTS string

func main() {
	// The host's TypeScript "files": module id -> source.
	sources := map[string]string{
		"main.ts":     mainTS,
		"geometry.ts": geometryTS,
	}

	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
		// ts.Provider transpiles any .ts module the base provider serves.
		gojs.WithModuleProvider(ts.Provider(gojs.NewMapModuleProvider(sources))),
	)
	defer vm.Close()

	// Bootstrap by requiring the entry module; its `import "./geometry"` resolves
	// through the same provider (the ".ts" extension is retried automatically).
	if _, err := vm.RunString("<entry>", `require("./main.ts")`); err != nil {
		log.Fatalf("run error: %v", err)
	}
}
