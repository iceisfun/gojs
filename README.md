<p align="center">
  <img src="docs/contrib/logo_gojs.png" alt="gojs" width="640">
</p>

# gojs

[![Go Reference](https://pkg.go.dev/badge/github.com/iceisfun/gojs.svg)](https://pkg.go.dev/github.com/iceisfun/gojs)

> [!IMPORTANT]
> **gojs is not ready for a release and is not production-ready.** It is an
> incomplete, actively evolving implementation: language and library coverage
> has gaps, and the **public API surface will change** (types, options, and
> function signatures may break between commits without notice).
>
> It is suitable for experiments, prototypes, learning, and other non-serious
> projects — not for anything where stability or completeness matters. Pin a
> specific commit if you depend on it, and expect to update your code as things
> move.

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
  `Boolean`, `Symbol` (well-known symbols), `Map`, `Set`, `WeakMap`, `WeakSet`,
  `Promise`, `Math`, `JSON`, `RegExp` (RE2), the `Error` hierarchy, and global
  helpers (`parseInt`, `parseFloat`, `isNaN`, `isFinite`,
  `encodeURIComponent`, …).
- **Generators & async/await** — `function*`, `yield`, `yield*`, `async`
  functions, `await`, and async arrows, all driven cooperatively on the single
  VM thread (an `await` is a `yield` to a promise-driven runner), so ordering
  matches real engines.
- **Event loop & timers** — `setTimeout`, `setInterval`, `clearTimeout`,
  `clearInterval`, `setImmediate`, and `queueMicrotask`, all serialized on a
  single event-loop goroutine so callbacks never race with script code.
- **Context cancellation** — every evaluation threads a `context.Context`;
  `Close()` cancels it and drains all timer goroutines.
- **Precise diagnostics** — every token and AST node carries a source span
  (line/column/offset) for underline-quality error messages.
- **TypeScript** — run `.ts`/`.tsx` directly. The optional
  [`ts`](ts) package transpiles TypeScript to JavaScript in-process (embedding a
  hoisting of Microsoft's [typescript-go](https://github.com/microsoft/typescript-go)),
  so `gojs run app.ts` and embedded TypeScript just work. Type-stripping is
  checker-free; the gojs core stays dependency-free (only importing `ts` pulls
  the compiler in). See the [`typescript`](examples/typescript) example.
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
| `ModuleProvider`  | `require(specifier)` module loading        | `NewMapModuleProvider`, `NewDirModuleProvider` |
| `OsProvider`      | `process` env / cwd / exit / platform / arch / pid | `NewDefaultOsProvider()`, `NewFilteredOsProvider(filter)` |
| `NetProvider`     | outbound dialing/DNS for `fetch`/`sse`/`websocket` | `NewDefaultNetProvider()` (pass-through; wrap to allowlist/deny) |

Resource use is bounded with `WithLimits(Limits{MaxCallDepth, MaxSteps})`:
recursion raises a catchable `RangeError`, and the step budget is an
uncatchable abort that stops runaway loops. See the `limits` example.

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
go install github.com/iceisfun/gojs/cmd/gojs@latest

gojs run app.js          # run a JavaScript file
gojs run app.ts          # TypeScript, transpiled in-process
```

The runner resolves `require()`/imports against the entry file's directory
(TypeScript modules are transpiled on load) and installs the standalone-runner
capabilities: `console`, timers, and a Node-like **`process`** (`argv`, `env`,
`stdout.write`, `exit`, `hrtime`, `nextTick`). These are ordinary host
capabilities — `process` is the [`host/process`](host/process) package, which an
embedder installs (and sandboxes) explicitly per VM.

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

## Security model

gojs is designed to run **untrusted** JavaScript. The posture is defense in
depth, and every layer is off by default:

- **No ambient authority.** A bare `gojs.New()` has no way to print, read the
  clock, schedule timers, or load modules. Scripts see only pure-computation
  built-ins. Capabilities are granted one provider at a time, and each provider
  is an interface you can implement to mediate, log, or refuse individual
  operations.
- **No dynamic code by default.** `eval` and the `Function` constructor are
  gated by `Security{DisableEval, DisableFunctionCtor}`; the CLI `--sandbox`
  mode turns them off entirely.
- **Prototype-pollution guard.** `DisableProtoMutation` freezes mutation of
  built-in prototypes so untrusted code can't tamper with the shared realm.
- **Bounded resources.** `WithLimits(Limits{MaxCallDepth, MaxSteps})` caps
  recursion (catchable `RangeError`) and total evaluation steps (an uncatchable
  abort), and every evaluation honors a `context.Context` deadline. A hostile
  `while (true) {}` is stopped by the step budget or the context, not by luck.
- **Single-threaded by construction.** JavaScript never runs on two goroutines
  at once, so a script cannot exploit host-side data races. `Close()` cancels
  the context and drains outstanding timers and coroutines.

```go
vm := gojs.New(
	gojs.WithSecurity(gojs.Security{
		DisableEval:          true,
		DisableFunctionCtor:  true,
		DisableProtoMutation: true,
	}),
	gojs.WithLimits(gojs.Limits{MaxCallDepth: 512, MaxSteps: 5_000_000}),
)
```

See the [`sandbox`](examples/sandbox) and [`limits`](examples/limits) examples
for end-to-end setups.

## Standard library

Implemented intrinsics and globals (see [Limitations](#limitations) for gaps):

| Area          | Coverage                                                                    |
| ------------- | --------------------------------------------------------------------------- |
| `Object`      | literals, descriptors, `keys`/`values`/`entries`, `assign`, `freeze`/`seal`, `create`, `getPrototypeOf`, `is`, `groupBy` |
| `Array`       | literals, iteration (`map`/`filter`/`reduce`/…), `sort`, `flat`, ES2023 `toSorted`/`toReversed`/`toSpliced`/`with`, `from`/`of`, `Array.isArray` |
| `String`      | full method set incl. `match`/`matchAll`/`replace`/`replaceAll`/`split` (regex-aware), templates, `padStart`/`padEnd` |
| `Number`/`Math` | numeric methods, `toFixed`/`toPrecision`/`toString(radix)`, the `Math.*` surface |
| `JSON`        | `parse`/`stringify` with replacer (function + array), reviver, `toJSON`, cycle detection |
| `RegExp`      | RE2-backed `exec`/`test`, flags, capture groups (no backrefs/lookaround/named groups — see [Limitations](#limitations)) |
| Collections   | `Map`, `Set`, `WeakMap`, `WeakSet`, and `Promise` (with the microtask queue) |
| `Symbol`      | well-known symbols (`iterator`, `asyncIterator`, `hasInstance`, …)          |
| Errors        | full `Error` hierarchy, subclassable                                        |
| Globals       | `parseInt`, `parseFloat`, `isNaN`, `isFinite`, `encodeURIComponent`, `Date`, `Promise`, timers |

## TypeScript

gojs can run TypeScript with no external tooling. The [`ts`](ts) package
transpiles `.ts`/`.tsx` to JavaScript in-process — it embeds
[`github.com/iceisfun/typescript`](https://github.com/iceisfun/typescript), a
hoisting of Microsoft's typescript-go compiler out of its `internal/` packages —
and runs the result on the VM. Importing `ts` is what pulls that dependency into
a build; embeddings that only run JavaScript keep the zero-dependency core.

```go
import "github.com/iceisfun/gojs/ts"

// Self-contained script:
vm := gojs.New(gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()))
ts.RunString(vm, "app.ts", `const n: number = 21; console.log(n * 2);`) // 42

// Multi-file: ts.Provider wraps any ModuleProvider so .ts modules are
// transpiled on load; import/require between them resolve through it.
vm = gojs.New(
    gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
    gojs.WithModuleProvider(ts.Provider(gojs.NewDirModuleProvider("./src"))),
)
vm.RunString("<entry>", `require("./main.ts")`)
```

From the CLI:

```bash
gojs run app.ts        # transpile + run a TypeScript entry file
```

Transpilation is **checker-free** (the `isolatedModules` model): type
annotations, `interface`s, generics, and class visibility are erased/lowered,
but the program is **not type-checked** — the goal is to *run* TypeScript. `enum`
/ `namespace` lowering and `.ts`-origin line numbers in stack traces are not yet
wired. See the [`typescript`](examples/typescript) example.

## Examples

Runnable programs live under [`examples/`](examples), each with its own README:

| Example                                  | Shows                                                    |
| ---------------------------------------- | ------------------------------------------------------- |
| [`basic`](examples/basic)               | Run a script and read back its result                   |
| [`expose_go`](examples/expose_go)       | Expose Go functions and data to JavaScript              |
| [`call_js`](examples/call_js)           | Call script functions from Go                           |
| [`providers`](examples/providers)       | Custom `PrintProvider` + timers on the event loop       |
| [`modules`](examples/modules)           | Intercept `require()` with a `ModuleProvider`           |
| [`typescript`](examples/typescript)     | Run TypeScript (transpiled in-process) with imports     |
| [`limits`](examples/limits)             | Bound recursion and CPU with `Limits`                   |
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

## Project structure

```
gojs/
├── doc.go            package overview (pkg.go.dev landing)
├── gojs.go           root package: re-exported public surface
├── token/            token kinds, source positions, spans
├── lexer/            lexical scanner
├── ast/              AST node types
├── parser/           recursive-descent parser
├── interp/           evaluator, values, built-ins, providers, event loop, host API
├── ts/               optional TypeScript support (transpile + run; pulls in typescript-go)
├── cmd/gojs/         command-line runner (runs .js and .ts)
├── examples/         runnable embedding examples (each with a README)
└── tests/
    ├── harness/      behavioral JS conformance suite (self-asserting programs)
    ├── doctest/      documentation examples run as tests
    └── test262/      optional Test262 runner (gated behind GOJS_T262)
```

## Running tests

```bash
go test ./...              # full suite (fast; Test262 is skipped)
go test ./... -race        # race detector across the event loop and coroutines
go test ./tests/harness    # just the JS behavioral suite
```

The behavioral suite runs real JavaScript programs that assert their own
results, so a regression surfaces as a failing Go test with the thrown value.
The optional [Test262](https://github.com/tc39/test262) runner is gated behind
the `GOJS_T262` environment variable (pointing at a local checkout) so the
default `go test ./...` stays fast and hermetic.

## Contributing

The workflow is find-fix-test: when you hit a behavior that diverges from the
spec, **first reproduce it as a failing test** in `tests/harness`, then fix the
engine until it passes. Keep `go test ./... -race` green. Intentional
divergences from the spec (e.g. RE2 regex semantics, rune-indexed strings) are
recorded so they aren't mistaken for bugs. New built-ins and language features
should land with harness coverage.

## Limitations

This is a first pass. Notable gaps and approximations:

- **Top-level `await`** outside an `async` function is a best-effort
  synchronous unwrap rather than a full module-graph suspension.
- **Strings** are stored as Go UTF-8 and indexed by rune, not UTF-16 code
  units — an approximation for characters outside the BMP.
- **Modules** — CommonJS `require()` works through a `ModuleProvider` (see the
  `modules` and `typescript` examples). Native ES `import`/`export` is not
  executed directly; TypeScript's ES modules run by transpiling to CommonJS.
- **`eval` / `Function(...)`** dynamic code is intentionally unsupported
  (sandbox posture).
- **`RegExp`** is backed by RE2, so backreferences, lookaround, and named
  capture groups (`(?<name>…)` / `match.groups`) are not supported.
- No `Proxy`/`Reflect`, `TypedArray`/`ArrayBuffer`, or `Intl` yet.

## License

See [LICENSE](LICENSE).
