<p align="center">
  <img src="docs/contrib/logo_gojs.png" alt="gojs" width="640">
</p>

# gojs

[![Go Reference](https://pkg.go.dev/badge/github.com/iceisfun/gojs.svg)](https://pkg.go.dev/github.com/iceisfun/gojs)

An embeddable, sandbox-first **JavaScript (ECMAScript) runtime for Go**. Pure
Go, zero dependencies, no cgo.

gojs is a companion to [GoLua](https://github.com/iceisfun/golua): the same
capability-gated, provider-based sandboxing philosophy, applied to JavaScript.
Good fits include plugin systems, user scripting, configuration runtimes,
automation, and running untrusted snippets under tight host control.

> **Status:** first-pass implementation. The lexer, parser, AST, and a
> tree-walking interpreter run a large subset of modern JavaScript. Some
> features are approximated or not yet implemented — see
> [Limitations](#limitations).

## Features

- **Modern JS syntax** — `let`/`const`, arrow functions, classes (with
  `extends`/`super`, private `#fields`, `static`, getters/setters, instance
  fields), destructuring with defaults and rest, spread, template &
  tagged-template literals, optional chaining (`?.`), nullish coalescing
  (`??`), logical assignment (`&&=` `||=` `??=`), exponentiation, BigInt
  literals, computed keys, and labeled `break`/`continue`.
- **Full statement set** — `if`/`else`, `for`, `for-in`, `for-of`, `while`,
  `do-while`, `switch`, `try`/`catch`/`finally` (incl. optional catch
  binding), `throw`, and Automatic Semicolon Insertion.
- **Built-ins** — `Object`, `Function`, `Array`, `String`, `Number`,
  `Boolean`, `Symbol` (well-known symbols), `Math`, `JSON`, `RegExp` (RE2),
  the `Error` hierarchy, and global helpers (`parseInt`, `parseFloat`,
  `isNaN`, `isFinite`, `encodeURIComponent`, …).
- **Event loop & timers** — `setTimeout`, `setInterval`, `clearTimeout`,
  `clearInterval`, `setImmediate`, and `queueMicrotask`, all serialized on a
  single event-loop goroutine so callbacks never race with script code.
- **Context cancellation** — every evaluation threads a `context.Context`;
  `Close()` cancels it and drains all timer goroutines.
- **Precise diagnostics** — every token and AST node carries a source span
  (line/column/offset) for underline-quality error messages.
- **No cgo, no C dependencies, single static binary.**

## Capability model

Like GoLua, gojs is **closed by default**. Access to host facilities is granted
by installing *providers*; a `New()` with no options cannot print, read the
clock, or schedule timers.

| Provider          | Controls                                   | Default implementation      |
| ----------------- | ------------------------------------------ | --------------------------- |
| `PrintProvider`   | all `console.*` output                     | `NewDefaultPrintProvider()` |
| `TimeProvider`    | `Date` / `performance.now` clock source    | `NewDefaultTimeProvider()`  |
| `TimerProvider`   | `setTimeout` / `setInterval` scheduling    | `NewDefaultTimerProvider()` |

Each provider is a small Go interface, so you can supply your own — route
`console.*` through your logger, present a fixed clock for deterministic tests,
or back timers with your own scheduler:

```go
type PrintProvider interface {
	Print(ctx context.Context, msg string) // console.log/info/debug
	Warn(ctx context.Context, msg string)  // console.warn/error
}
```

Additional hardening is available through `WithSecurity(Security{…})`:
`DisableEval`, `DisableFunctionCtor`, `DisableProtoMutation`,
`StrictModulesOnly`.

### Host async integration

Concurrent host work (HTTP, filesystem, DB, subprocess) integrates through a
single-threaded event-loop model — the interpreter is never touched from more
than one goroutine. A provider does its blocking work on its own goroutine, then
posts exactly one continuation back onto the VM goroutine:

```go
cap := vm.NewPromiseCapability()   // create a pending promise on the VM goroutine
go func() {
	body, err := http.Get(url)     // blocking work off the VM goroutine
	if err != nil {
		cap.Reject(vm.NewError("Error", err.Error())) // marshals back to the loop
		return
	}
	cap.Resolve(gojs.String(body))
}()
return cap.Promise                 // hand the promise to the script
```

`Enqueue`, `QueueMicrotask`, `ResolvePromise`/`RejectPromise`, and `RunLoop`
round out the API. All JavaScript — the initial program and every callback —
runs on one goroutine, so objects, environments, and closures never need locks.

## Installation

```bash
go get github.com/iceisfun/gojs
# CLI
go install github.com/iceisfun/gojs/cmd/gojs@latest
```

## Quick start

```go
package main

import (
	"log"

	"github.com/iceisfun/gojs"
)

func main() {
	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
		gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
	)
	defer vm.Close()

	_, err := vm.RunString("example.js", `
		const nums = [1, 2, 3, 4];
		const evens = nums.filter(n => n % 2 === 0);
		console.log("sum of evens:", evens.reduce((a, b) => a + b, 0));
		setTimeout(() => console.log("done"), 10);
	`)
	if err != nil {
		log.Fatal(err)
	}
}
```

The embedding flow is `gojs.New` → `RunString` → `Close`. `RunString` parses,
evaluates the top level, then drains the event loop (so pending timers and
promise microtasks run) before returning.

## CLI

```bash
gojs script.js            # run a file
gojs -e "console.log(1)"  # evaluate a snippet
gojs < script.js          # read from stdin

gojs --sandbox script.js  # run with the closed sandbox (no providers)
gojs --no-timers ...       # disable setTimeout/setInterval
gojs --timeout 500 ...     # cancel after 500ms
```

By default the CLI installs the `Default*` providers (the standalone-runner
trust level). `--sandbox` runs with no providers at all. `--ast` prints the
parsed syntax tree instead of executing.

## Go interop

Move values and control across the Go/JavaScript boundary with a small API.

**Expose a Go function to scripts:**

```go
vm.SetGlobal("shout", vm.NewFunction("shout", func(args []gojs.Value) (gojs.Value, error) {
	s, _ := vm.ToString(args[0])
	return gojs.String(strings.ToUpper(s)), nil
}))
vm.RunString("s.js", `console.log(shout("hi"))`) // HI
```

Returning an error from a `HostFunc` throws it into the script; wrap a value
with `gojs.NewThrow(...)` to throw a specific JS object.

**Call a script function from Go:**

```go
vm.RunString("lib.js", `function add(a, b) { return a + b; }`)
sum, _ := vm.Call(vm.GetGlobal("add"), gojs.Undefined, gojs.Number(2), gojs.Number(3))
fmt.Println(vm.ToGo(sum)) // 5
```

**Convert values both ways** with `vm.FromGo` (Go → JS: scalars, `[]any`,
`map[string]any`) and `vm.ToGo` / `vm.ToString` (JS → Go).

## Examples

Runnable programs live under [`examples/`](examples), each with its own README:

| Example                                  | Shows                                                    |
| ---------------------------------------- | ------------------------------------------------------- |
| [`basic`](examples/basic)               | Run a script and read back its result                   |
| [`expose_go`](examples/expose_go)       | Expose Go functions and data to JavaScript              |
| [`call_js`](examples/call_js)           | Call script functions from Go                           |
| [`providers`](examples/providers)       | Custom `PrintProvider` + timers on the event loop       |
| [`sandbox`](examples/sandbox)           | Hardened sandbox: no I/O, no eval, timeout on runaway    |

```bash
go run ./examples/basic
go run ./examples/expose_go
go run ./examples/providers
```

## Architecture

gojs is organized into layered packages with no import cycles:

```
Source → lexer → parser → ast → interp (tree-walking evaluator)
                                    ↑
                                Providers
```

| Package  | Purpose                                                              |
| -------- | ------------------------------------------------------------------- |
| `token`  | Lexical token types, categories, keywords, source spans             |
| `lexer`  | Tokenizes source (regex/division disambiguation, templates, ASI)    |
| `ast`    | AST node definitions (`Expr`/`Stmt`) with positions                 |
| `parser` | Recursive-descent + precedence-climbing parser                      |
| `interp` | The runtime: values, objects, environments, evaluator, built-ins, providers, event loop |

The root package `gojs` re-exports the common surface of `interp`.

## Limitations

This is a first pass. Notable gaps and approximations:

- **Generators** (`function*`, `yield`, `yield*`) are fully functional —
  each instance runs on its own goroutine as a cooperative coroutine, and
  `Close()` cleans up suspended generators.
- **`async`/`await`** parse but run synchronously (await unwraps its
  operand); they are not yet backed by the microtask queue.
- **Strings** are stored as Go UTF-8 and indexed by rune, not UTF-16 code
  units — an approximation for characters outside the BMP.
- **Modules** (`import`/`export`) are not yet executed.
- **`eval` / `Function(...)`** dynamic code is intentionally unsupported
  (sandbox posture).
- **`RegExp`** is backed by RE2, so backreferences and lookaround are not
  supported.
- No `Proxy`/`Reflect`, `TypedArray`/`ArrayBuffer`, or `Intl` yet.

## License

See [LICENSE](LICENSE).
