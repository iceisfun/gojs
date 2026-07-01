// Package interp is the heart of gojs: a tree-walking evaluator for the AST
// produced by [github.com/iceisfun/gojs/parser], together with the JavaScript
// value model, the intrinsic prototypes (the "realm"), the capability
// providers, and the host-facing embedding API.
//
// An [Interpreter] runs script on a single logical thread with an event loop.
// Host code drives it through the embedding API — Enqueue, QueueMicrotask,
// ResolvePromise, RejectPromise, and RunLoop — which is how concurrent Go work
// hands results back without ever running JavaScript on two goroutines at once.
// Capabilities (printing, time, timers, module loading) are supplied by
// providers and are absent by default, and execution can be bounded with
// [Limits] and a context deadline.
//
// Most embedders do not import this package directly; the root gojs package
// re-exports the common surface. Import interp when you need lower-level access
// such as building custom host functions, providers, or values.
package interp

import (
	"context"
	"sync"
)

// Interpreter is a single JavaScript runtime instance: a global object and
// environment, the set of intrinsic prototypes (the "realm"), the capability
// providers, and the execution context used for cancellation.
//
// An Interpreter is not safe for concurrent use by multiple goroutines running
// scripts; however, timer callbacks scheduled via a [TimerProvider] are run on
// the interpreter's own event loop, serialized with script execution.
type Interpreter struct {
	ctx    context.Context
	cancel context.CancelFunc

	// global object and the global (outermost) lexical environment.
	global    *Object
	globalEnv *Environment

	// intrinsics holds the well-known prototype and constructor objects.
	intrinsics

	// Capability providers. A nil provider disables the corresponding feature.
	printer PrintProvider
	timer   TimerProvider
	clock   TimeProvider

	// security holds opt-in hardening switches (see Security / WithSecurity).
	security Security

	// moduleProvider gates require(); modules caches evaluated modules by id.
	moduleProvider ModuleProvider
	modules        map[string]*Object

	// rng is the per-interpreter PRNG backing Math.random.
	rng *prng

	// evalFn is the intrinsic %eval% function object. A call whose callee is the
	// identifier `eval` resolving to this object is a direct eval, which runs in
	// the caller's lexical context rather than the global scope.
	evalFn *Object

	// pendingNewTarget carries the [[NewTarget]] value from an ordinary
	// function's [[Construct]] to the [[Call]] that runs its body. It is set
	// immediately before the call and consumed at the top of the call, so no
	// other JS runs in between (the VM is single-threaded).
	pendingNewTarget Value

	// callDepth counts active JS function invocations; it bounds recursion so
	// runaway/infinite recursion raises a RangeError instead of overflowing the
	// Go goroutine stack (which would crash the host).
	callDepth int

	// limits holds the resource limits (call depth, step budget); steps is the
	// running evaluation-step count checked against Limits.MaxSteps.
	limits Limits
	steps  int64

	// regexpEngine selects the RegExp backend (the ECMAScript-conformant
	// jsregexp engine by default, or the faster RE2 engine as an opt-in).
	regexpEngine RegExpEngine

	// wellKnownSymbols
	symIterator      *Symbol
	symAsyncIterator *Symbol
	symToPrimitive   *Symbol
	symToStringTag   *Symbol
	symHasInstance   *Symbol

	// event loop / timers
	loop    *eventLoop
	wg      sync.WaitGroup
	closed  bool
	closeMu sync.Mutex
}

// intrinsics holds the realm's built-in prototype and constructor objects. They
// are created once during bootstrap and shared by all objects of a kind.
type intrinsics struct {
	objectProto    *Object
	functionProto  *Object
	arrayProto     *Object
	stringProto    *Object
	numberProto    *Object
	booleanProto   *Object
	symbolProto    *Object
	bigintProto    *Object
	errorProto     *Object
	regexpProto    *Object
	mapProto       *Object
	setProto       *Object
	promiseProto   *Object
	iteratorProto  *Object
	generatorProto *Object
	dateProto      *Object

	objectCtor   *Object
	functionCtor *Object
	arrayCtor    *Object

	// nativeErrorProtos maps an error name (TypeError, RangeError, ...) to its
	// prototype, so runtime code can raise the right error kind.
	nativeErrorProtos map[string]*Object
	nativeErrorCtors  map[string]*Object
}

// Option configures an [Interpreter] at construction time.
type Option func(*Interpreter)

// WithContext sets the parent context used for cancellation. When the context
// is cancelled, running scripts observe the cancellation at the next
// interruption check and timer goroutines are stopped.
func WithContext(ctx context.Context) Option {
	return func(i *Interpreter) { i.ctx = ctx }
}

// WithPrintProvider sets the provider that receives console output. Without one,
// console methods are inert (nothing is written).
func WithPrintProvider(p PrintProvider) Option {
	return func(i *Interpreter) { i.printer = p }
}

// WithTimeProvider sets the wall-clock provider backing Date.now and Date.
func WithTimeProvider(p TimeProvider) Option {
	return func(i *Interpreter) { i.clock = p }
}

// WithTimerProvider enables setTimeout/setInterval/setImmediate backed by p.
func WithTimerProvider(p TimerProvider) Option {
	return func(i *Interpreter) { i.timer = p }
}

// RegExpEngine selects which regular-expression backend the VM installs.
type RegExpEngine int

const (
	// RegExpCompat is the default: the pure-Go jsregexp engine, a full
	// ECMAScript implementation (backreferences, lookahead/lookbehind, named
	// groups, u/v Unicode modes) with a step budget that bounds catastrophic
	// backtracking. Correct, sandbox-safe, and the right choice for running real
	// or untrusted JavaScript.
	RegExpCompat RegExpEngine = iota

	// RegExpRE2 backs RegExp with Go's regexp package (RE2). It is faster and
	// linear-time, but it is NOT ECMAScript-conformant: patterns using
	// backreferences or lookaround fail to compile (SyntaxError), and capture,
	// flag, and Unicode semantics follow RE2 rather than the spec. Use it only
	// for performance-sensitive scripting over simple, trusted patterns where
	// full conformance is not required.
	RegExpRE2
)

// WithRegExpEngine selects the RegExp backend. The default (RegExpCompat) is the
// ECMAScript-conformant jsregexp engine; RegExpRE2 opts into the faster,
// non-conformant RE2 engine. See RegExpEngine for the trade-offs.
func WithRegExpEngine(e RegExpEngine) Option {
	return func(i *Interpreter) { i.regexpEngine = e }
}

// New creates an Interpreter with the standard global environment installed.
func New(opts ...Option) *Interpreter {
	i := &Interpreter{limits: defaultLimits()}
	for _, opt := range opts {
		opt(i)
	}
	if i.ctx == nil {
		i.ctx = context.Background()
	}
	i.ctx, i.cancel = context.WithCancel(i.ctx)
	i.loop = newEventLoop()
	if i.rng == nil {
		i.rng = newPRNG(0)
	}

	i.symIterator = &Symbol{Desc: "Symbol.iterator"}
	i.symAsyncIterator = &Symbol{Desc: "Symbol.asyncIterator"}
	i.symToPrimitive = &Symbol{Desc: "Symbol.toPrimitive"}
	i.symToStringTag = &Symbol{Desc: "Symbol.toStringTag"}
	i.symHasInstance = &Symbol{Desc: "Symbol.hasInstance"}

	i.bootstrap()
	return i
}

// Context returns the interpreter's execution context.
func (i *Interpreter) Context() context.Context { return i.ctx }

// Global returns the global object.
func (i *Interpreter) Global() *Object { return i.global }

// Close cancels the interpreter's context, stops the timer event loop, and
// waits for any in-flight timer goroutines to finish. It is safe to call
// multiple times.
func (i *Interpreter) Close() error {
	i.closeMu.Lock()
	if i.closed {
		i.closeMu.Unlock()
		return nil
	}
	i.closed = true
	i.closeMu.Unlock()

	i.cancel()
	if i.loop != nil {
		i.loop.stop()
	}
	i.wg.Wait()
	return nil
}

// enterCall increments the recursion counter, returning a RangeError once
// Limits.MaxCallDepth is exceeded. Pair with leaveCall via defer.
func (i *Interpreter) enterCall() error {
	i.callDepth++
	if i.limits.MaxCallDepth > 0 && i.callDepth > i.limits.MaxCallDepth {
		return i.throwError(i.ctx, "RangeError", "Maximum call stack size exceeded")
	}
	return nil
}

func (i *Interpreter) leaveCall() { i.callDepth-- }

// checkContext returns a Throw-free error if the context has been cancelled, so
// long-running evaluation can abort cooperatively.
func (i *Interpreter) checkContext() error {
	select {
	case <-i.ctx.Done():
		return i.ctx.Err()
	default:
		return nil
	}
}
