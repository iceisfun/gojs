// Package gojs is an embeddable, sandbox-first JavaScript (ECMAScript) runtime
// for Go applications. It is pure Go with no cgo and no external dependencies.
//
// gojs is organized as a layered pipeline, mirroring a conventional engine:
//
//	Source → lexer → parser → ast → interp (tree-walking evaluator)
//
// Host access (console output, wall-clock time, timers) is gated behind
// capability providers, so the default configuration is a closed sandbox. A
// caller opts into each capability explicitly.
//
// # Quick start
//
//	vm := gojs.New(
//	    gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
//	)
//	defer vm.Close()
//	if _, err := vm.RunString("example.js", `console.log(1 + 2)`); err != nil {
//	    log.Fatal(err)
//	}
//
// The root package re-exports the most common surface of the interp package so
// simple embeddings need only import gojs.
package gojs

import "github.com/iceisfun/gojs/interp"

// VM is a JavaScript runtime instance. It is an alias for [interp.Interpreter]
// so callers can use the root package without importing interp directly.
type VM = interp.Interpreter

// Value is a JavaScript runtime value. Alias for [interp.Value].
type Value = interp.Value

// Option configures a [VM] at construction. Alias for [interp.Option].
type Option = interp.Option

// Security holds the opt-in hardening switches. Alias for [interp.Security].
type Security = interp.Security

// Value type aliases for building/inspecting JavaScript values from Go.
type (
	// Object is a JavaScript object (also arrays and functions).
	Object = interp.Object
	// String is a JavaScript string.
	String = interp.String
	// Number is a JavaScript number (float64).
	Number = interp.Number
	// Boolean is a JavaScript boolean.
	Boolean = interp.Boolean
	// HostFunc is the signature for a Go function exposed to scripts.
	HostFunc = interp.HostFunc
)

// Interned primitive values.
var (
	// Undefined is the JavaScript undefined value.
	Undefined = interp.Undef
	// Null is the JavaScript null value.
	Null = interp.Nul
	// True and False are the JavaScript boolean values.
	True  = interp.True
	False = interp.False
)

// Bool returns the JavaScript boolean for b.
func Bool(b bool) Value { return interp.Bool(b) }

// NewThrow wraps a value so returning it from a HostFunc throws it as a JS
// exception.
func NewThrow(v Value) error { return interp.NewThrow(v) }

// New creates a VM. With no options it is a closed sandbox: no console output,
// no clock, and no timers. Add providers to grant capabilities.
func New(opts ...Option) *VM { return interp.New(opts...) }

// Provider constructors and options, re-exported for convenience.
var (
	WithContext       = interp.WithContext
	WithPrintProvider = interp.WithPrintProvider
	WithTimeProvider  = interp.WithTimeProvider
	WithTimerProvider = interp.WithTimerProvider
	WithSecurity      = interp.WithSecurity

	NewDefaultPrintProvider = interp.NewDefaultPrintProvider
	NewDefaultTimeProvider  = interp.NewDefaultTimeProvider
	NewDefaultTimerProvider = interp.NewDefaultTimerProvider
)

// ThrownValue extracts the JavaScript value from an uncaught-exception error
// returned by RunString/RunProgram.
func ThrownValue(err error) (Value, bool) { return interp.ThrownValue(err) }

// BriefValue renders a value for host-facing display (e.g. an uncaught
// exception) without running user toString methods.
func BriefValue(v Value) string { return interp.BriefValue(v) }
