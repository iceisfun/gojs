// Package gojs is an embeddable, sandbox-first JavaScript (ECMAScript) runtime
// for Go applications. It is pure Go with no cgo and no external dependencies.
//
// gojs is organized as a layered pipeline, mirroring a conventional engine.
// Each stage lives in its own sub-package:
//
//		Source → lexer → parser → ast → interp (tree-walking evaluator)
//
//	  - [github.com/iceisfun/gojs/token] — token kinds, source positions, and spans
//	  - [github.com/iceisfun/gojs/lexer] — lexical scanner producing tokens
//	  - [github.com/iceisfun/gojs/parser] — recursive-descent parser producing an AST
//	  - [github.com/iceisfun/gojs/ast] — abstract syntax tree node types
//	  - [github.com/iceisfun/gojs/interp] — tree-walking evaluator, value model, providers, and the host API
//
// # Capability model
//
// Host access (console output, wall-clock time, timers, module loading) is
// gated behind capability providers, so the default configuration is a closed
// sandbox: scripts cannot print, read the clock, schedule timers, or require
// modules unless the embedder opts in. A caller enables each capability
// explicitly by passing a provider at construction:
//
//	vm := gojs.New(
//	    gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
//	)
//	defer vm.Close()
//	if _, err := vm.RunString("example.js", `console.log(1 + 2)`); err != nil {
//	    log.Fatal(err)
//	}
//
// # Execution model
//
// A VM runs JavaScript on a single logical thread with an event loop. Host
// operations may execute concurrently, but they hand results back to the VM by
// enqueuing exactly one continuation (see [github.com/iceisfun/gojs/interp]'s
// Enqueue, QueueMicrotask, ResolvePromise, and RejectPromise), preserving the
// single-threaded invariant that JavaScript itself never runs on two
// goroutines at once. [VM.Close] cancels the underlying context and drains any
// outstanding timers and coroutines.
//
// # Resource limits
//
// Untrusted code can be bounded with [Limits] (maximum call depth, evaluation
// steps, and more) and with a context deadline, so a hostile or buggy script
// cannot exhaust host resources.
//
// The root package re-exports the most common surface of the interp package so
// simple embeddings need only import gojs; reach into the sub-packages directly
// for lower-level access such as custom AST walking or provider implementations.
package gojs
