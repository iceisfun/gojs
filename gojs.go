// The gojs package overview and the layered-package map live in doc.go. This
// file holds the re-exported public surface (types and constructors) that lets
// simple embeddings depend only on the root package.
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

// Limits bounds script resource usage (call depth, step budget). Alias for
// [interp.Limits].
type Limits = interp.Limits

// WithLimits sets resource limits at construction.
var WithLimits = interp.WithLimits

// ModuleProvider is the interface a host implements to intercept require() and
// serve module source (e.g. from game data files). Alias for
// [interp.ModuleProvider].
type ModuleProvider = interp.ModuleProvider

// Module provider option and default implementations, re-exported.
var (
	WithModuleProvider   = interp.WithModuleProvider
	NewMapModuleProvider = interp.NewMapModuleProvider
	NewDirModuleProvider = interp.NewDirModuleProvider
)

// OsProvider gates host OS access (env, cwd, exit, platform/arch/pid) — the
// capability backing the `process` global. Alias for [interp.OsProvider].
type OsProvider = interp.OsProvider

// OS provider option and default implementations, re-exported.
var (
	WithOsProvider        = interp.WithOsProvider
	NewDefaultOsProvider  = interp.NewDefaultOsProvider
	NewFilteredOsProvider = interp.NewFilteredOsProvider
)

// NetProvider is the single wall for outbound network dialing by the networking
// host packages (fetch/sse/websocket). Alias for [interp.NetProvider].
type NetProvider = interp.NetProvider

// Net provider option and default (pass-through) implementation, re-exported.
var (
	WithNetProvider      = interp.WithNetProvider
	NewDefaultNetProvider = interp.NewDefaultNetProvider
)

// SourceMapper maps transpiled positions in error stacks back to their original
// source (e.g. TypeScript). Alias for [interp.SourceMapper].
type SourceMapper = interp.SourceMapper

// WithSourceMapper installs a source mapper for error-stack positions.
var WithSourceMapper = interp.WithSourceMapper

// WithErrorColor toggles ANSI color in FormatError's rich stack rendering
// (default on).
var WithErrorColor = interp.WithErrorColor

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
	WithRegExpEngine  = interp.WithRegExpEngine

	NewDefaultPrintProvider = interp.NewDefaultPrintProvider
	NewDefaultTimeProvider  = interp.NewDefaultTimeProvider
	NewDefaultTimerProvider = interp.NewDefaultTimerProvider
)

// RegExpEngine selects the RegExp backend passed to [WithRegExpEngine].
type RegExpEngine = interp.RegExpEngine

// RegExp backend choices. RegExpCompat (the default) is the ECMAScript-conformant
// jsregexp engine; RegExpRE2 is the faster, non-conformant RE2 engine.
const (
	RegExpCompat = interp.RegExpCompat
	RegExpRE2    = interp.RegExpRE2
)

// ThrownValue extracts the JavaScript value from an uncaught-exception error
// returned by RunString/RunProgram.
func ThrownValue(err error) (Value, bool) { return interp.ThrownValue(err) }

// BriefValue renders a value for host-facing display (e.g. an uncaught
// exception) without running user toString methods.
func BriefValue(v Value) string { return interp.BriefValue(v) }
