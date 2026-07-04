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

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
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
	os      OsProvider
	net     NetProvider

	// useBytecode enables the optional bytecode VM (bc_*.go): eligible function
	// bodies are compiled and run on the stack VM instead of the tree-walker.
	// Off by default; the tree-walker remains the reference engine.
	useBytecode bool

	// sourceMapper, when set, maps generated (transpiled) positions in error
	// stacks back to their original source (e.g. TypeScript). curPos is the
	// position of the top-level statement executing; callStack holds one frame
	// per active function call (innermost last), each tracking the position of
	// the statement currently executing within it — together they form the
	// captured stack trace of an error.
	sourceMapper SourceMapper
	curPos       token.Pos
	callStack    []stackFrame
	// sources retains parsed source text by name, so error rendering can show a
	// code frame (the offending line with a caret). errorColor enables ANSI color
	// in FormatError's rendered stacks.
	sources    map[string]string
	errorColor bool

	// security holds opt-in hardening switches (see Security / WithSecurity).
	security Security

	// moduleProvider gates require(); modules caches evaluated modules by id.
	moduleProvider ModuleProvider
	modules        map[string]*Object
	// moduleNamespaces caches the module namespace object produced for each id
	// imported via dynamic import(), so a module is evaluated at most once and
	// repeated imports share the same namespace.
	moduleNamespaces map[string]*Object
	// linkedModules caches the export structure extracted from each parsed module
	// (by id), so the linker can run ResolveExport for indirect-export validation
	// without re-parsing; see module_link.go.
	linkedModules map[string]*linkedModule
	// moduleEnvs holds each evaluated module's top-level scope by id, so a
	// re-exported binding served through another module's namespace can read the
	// live local value from the module that actually declares it.
	moduleEnvs map[string]*Environment

	// symByKey / symBySym are the agent-wide GlobalSymbolRegistry backing
	// Symbol.for / Symbol.keyFor. A ShadowRealm's inner realm shares its parent's
	// maps so a registered symbol is the same value across realms.
	symByKey map[string]*Symbol
	symBySym map[*Symbol]string
	// childRealms are the inner realms created by ShadowRealm constructors on this
	// realm; they are closed when this interpreter is closed so their event-loop
	// goroutines do not leak.
	childRealms []*Interpreter

	// templateCache is the realm's [[TemplateMap]] (§13.2.8.4 GetTemplateObject).
	// It canonicalizes tagged-template objects by source location: each distinct
	// TemplateLiteral Parse Node maps to the single frozen strings array handed to
	// the tag function, so evaluating the same source site again returns the same
	// object. Keyed by AST-node identity (a fresh parse — e.g. each eval call —
	// yields a distinct node, while a loop reuses one node).
	templateCache map[*ast.TemplateLit]*Object

	// rng is the per-interpreter PRNG backing Math.random.
	rng *prng

	// evalFn is the intrinsic %eval% function object. A call whose callee is the
	// identifier `eval` resolving to this object is a direct eval, which runs in
	// the caller's lexical context rather than the global scope.
	evalFn *Object

	// paramDefaultEnv is non-nil only while a function's formal-parameter default
	// value is being evaluated; it points at that function's parameter
	// environment (holding the parameters and, when present, the arguments
	// object). A direct eval running here has its VariableEnvironment set to the
	// enclosing (outer) scope rather than the parameter environment, so a var/
	// function declaration in the eval whose name is already bound in the
	// parameter environment is a SyntaxError (ECMA-262 EvalDeclarationInstantiation
	// — a var may not hoist over a like-named binding in an intervening
	// declarative environment). Cleared when a function body begins executing so a
	// nested function body's eval is unaffected.
	paramDefaultEnv *Environment

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
	symIterator           *Symbol
	symAsyncIterator      *Symbol
	symToPrimitive        *Symbol
	symToStringTag        *Symbol
	symHasInstance        *Symbol
	symMatch              *Symbol
	symMatchAll           *Symbol
	symReplace            *Symbol
	symSearch             *Symbol
	symSplit              *Symbol
	symSpecies            *Symbol
	symUnscopables        *Symbol
	symIsConcatSpreadable *Symbol

	// unitsKey/unitsVal memoize the most recent UTF-16 encoding of a RegExp
	// subject string (see (*Interpreter).toUnits), so match/replace/split/
	// matchAll and repeated .test() do not re-encode the same subject per call.
	unitsKey string
	unitsVal []uint16

	// event loop / timers
	loop    *eventLoop
	wg      sync.WaitGroup
	closed  bool
	closeMu sync.Mutex
}

// intrinsics holds the realm's built-in prototype and constructor objects. They
// are created once during bootstrap and shared by all objects of a kind.
type intrinsics struct {
	objectProto               *Object
	functionProto             *Object
	arrayProto                *Object
	stringProto               *Object
	numberProto               *Object
	booleanProto              *Object
	symbolProto               *Object
	bigintProto               *Object
	errorProto                *Object
	regexpProto               *Object
	regexpCtor                *Object // %RegExp% constructor (SpeciesConstructor default)
	regexpStringIteratorProto *Object // %RegExpStringIteratorPrototype%
	mapProto                  *Object
	setProto                  *Object
	weakMapProto              *Object // %WeakMap.prototype%
	weakSetProto              *Object // %WeakSet.prototype%
	weakRefProto              *Object // %WeakRef.prototype%
	finalizationRegistryProto *Object // %FinalizationRegistry.prototype%
	shadowRealmProto          *Object // %ShadowRealm.prototype%
	promiseProto              *Object
	promiseCtor               *Object // %Promise%, for SpeciesConstructor defaults
	aggregateErrorProto       *Object // %AggregateError.prototype%, for Promise.any
	iteratorProto             *Object
	arrayIteratorProto        *Object // %ArrayIteratorPrototype%
	mapIteratorProto          *Object // %MapIteratorPrototype%
	setIteratorProto          *Object // %SetIteratorPrototype%
	iteratorHelperProto       *Object // %IteratorHelperPrototype%
	wrapForValidIterProto     *Object // %WrapForValidIteratorPrototype%
	stringIteratorProto       *Object // %StringIteratorPrototype%
	iteratorCtor              *Object // %Iterator%
	generatorProto            *Object
	asyncIteratorProto        *Object // %AsyncIteratorPrototype%
	asyncGeneratorProto       *Object // %AsyncGeneratorPrototype%
	asyncFromSyncIterProto    *Object // %AsyncFromSyncIteratorPrototype%

	// The generator/async function-family intrinsics. These constructors are not
	// global bindings; they are reachable only through the prototype chains of
	// generator/async functions (e.g. Object.getPrototypeOf(function*(){}).constructor).
	genFuncProto      *Object // %GeneratorFunction.prototype% (aka %Generator%)
	genFuncCtor       *Object // %GeneratorFunction%
	asyncGenFuncProto *Object // %AsyncGeneratorFunction.prototype% (aka %AsyncGenerator%)
	asyncGenFuncCtor  *Object // %AsyncGeneratorFunction%
	asyncFuncProto    *Object // %AsyncFunction.prototype%
	asyncFuncCtor     *Object // %AsyncFunction%
	arrayValuesFn     *Object // %Array.prototype.values% (== Array.prototype[Symbol.iterator])
	dateProto         *Object
	arrayBufferProto  *Object // %ArrayBuffer.prototype%
	dataViewProto     *Object // %DataView.prototype%
	typedArrayProto   *Object // %TypedArray.prototype%
	typedArrayCtor    *Object // %TypedArray% (the abstract intrinsic)
	// typedArrayKindProtos / typedArrayKindCtors map each concrete kind to its
	// per-kind %TypedArray.prototype% subclass and constructor.
	typedArrayKindProtos map[taKind]*Object
	typedArrayKindCtors  map[taKind]*Object

	// legacyNullGetter backs the Annex B "caller"/"arguments" own accessors on
	// sloppy plain functions: it always returns null (never a strict function).
	legacyNullGetter *Object

	// throwTypeError is the shared %ThrowTypeError% intrinsic (§10.2.4): a single
	// anonymous, frozen, non-constructor function that unconditionally throws a
	// TypeError. It backs the poison-pill get AND set accessors for "caller"/
	// "arguments" on %Function.prototype% and the "callee" accessor on unmapped
	// (strict) arguments objects.
	throwTypeError *Object

	objectCtor      *Object
	functionCtor    *Object
	arrayCtor       *Object
	arrayBufferCtor *Object // %ArrayBuffer%
	dataViewCtor    *Object // %DataView%

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

// WithBytecode enables the optional bytecode VM: eligible (non-generator,
// non-async) function bodies are compiled to bytecode and executed on the stack
// VM (bc_*.go) instead of the tree-walker. Any construct the compiler does not
// yet handle falls back to the tree-walker automatically — per-subtree where
// possible, or for the whole function otherwise — so behavior is unchanged. This
// is an experimental performance path; the tree-walker remains the reference.
func WithBytecode() Option {
	return func(i *Interpreter) { i.useBytecode = true }
}

// WithTimerProvider enables setTimeout/setInterval/setImmediate backed by p.
func WithTimerProvider(p TimerProvider) Option {
	return func(i *Interpreter) { i.timer = p }
}

// WithOsProvider grants access to host OS facilities (environment, cwd, exit,
// platform/arch/pid) — see [OsProvider]. Without one, those facilities are
// unavailable. It backs the `process` global installed by host/process.
func WithOsProvider(p OsProvider) Option {
	return func(i *Interpreter) { i.os = p }
}

// WithNetProvider routes outbound dialing done by the networking host packages
// (host/fetch, host/sse, host/websocket) through p — the single egress wall (and
// a convenient test seam; point it at a loopback server). See [NetProvider].
func WithNetProvider(p NetProvider) Option {
	return func(i *Interpreter) { i.net = p }
}

// NetProvider returns the configured outbound-dial provider, or nil. Networking
// host packages consult it when building their default client/dialer.
func (i *Interpreter) NetProvider() NetProvider { return i.net }

// SourceMapper translates a generated (transpiled) source position back to its
// original position, so error stacks can report original .ts line/column for
// code that was transpiled to JavaScript (see the ts package). line and column
// are 1-based; ok is false when the position is not mapped.
type SourceMapper interface {
	MapPosition(source string, line, column int) (origSource string, origLine, origColumn int, ok bool)
	// SourceText returns the original source text for a mapped source name (used
	// to render code frames), or ok=false if unavailable.
	SourceText(origSource string) (string, bool)
}

// WithSourceMapper installs a SourceMapper used to rewrite positions in error
// stacks back to their original source.
func WithSourceMapper(m SourceMapper) Option {
	return func(i *Interpreter) { i.sourceMapper = m }
}

// WithErrorColor enables (default) or disables ANSI color in the rich error
// rendering produced by FormatError. Disable it when output goes to a log or web
// sink rather than a terminal.
func WithErrorColor(on bool) Option {
	return func(i *Interpreter) { i.errorColor = on }
}

// SourceMapper returns the configured source mapper, or nil.
func (i *Interpreter) SourceMapper() SourceMapper { return i.sourceMapper }

// PrintProvider returns the configured console-output sink, or nil. It lets host
// packages (e.g. host/process's process.stdout) route their output through the
// same capability as console.
func (i *Interpreter) PrintProvider() PrintProvider { return i.printer }

// TimeProvider returns the configured clock, or nil.
func (i *Interpreter) TimeProvider() TimeProvider { return i.clock }

// OsProvider returns the configured OS-facilities provider, or nil.
func (i *Interpreter) OsProvider() OsProvider { return i.os }

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
	i := &Interpreter{limits: defaultLimits(), errorColor: true}
	for _, opt := range opts {
		opt(i)
	}
	if i.ctx == nil {
		i.ctx = context.Background()
	}
	i.ctx, i.cancel = context.WithCancel(i.ctx)
	// Tag the base context with this realm so realm-sensitive operations reached
	// through the realm-agnostic object layer (chiefly Proxy internal methods)
	// can select the *running* realm's intrinsics/error constructors. A child
	// realm's own tag shadows the parent's inherited one.
	i.ctx = context.WithValue(i.ctx, currentRealmKey{}, i)
	i.loop = newEventLoop()
	if i.rng == nil {
		i.rng = newPRNG(0)
	}

	// Well-known symbols are shared by every realm in one agent (they are agent-
	// level values, not per-realm). A child realm (NewChildRealm) pre-populates
	// these fields from its parent, so only mint fresh ones when unset.
	if i.symIterator == nil {
		i.symIterator = &Symbol{Desc: "Symbol.iterator", HasDesc: true}
		i.symAsyncIterator = &Symbol{Desc: "Symbol.asyncIterator", HasDesc: true}
		i.symToPrimitive = &Symbol{Desc: "Symbol.toPrimitive", HasDesc: true}
		i.symToStringTag = &Symbol{Desc: "Symbol.toStringTag", HasDesc: true}
		i.symHasInstance = &Symbol{Desc: "Symbol.hasInstance", HasDesc: true}
		i.symMatch = &Symbol{Desc: "Symbol.match", HasDesc: true}
		i.symMatchAll = &Symbol{Desc: "Symbol.matchAll", HasDesc: true}
		i.symReplace = &Symbol{Desc: "Symbol.replace", HasDesc: true}
		i.symSearch = &Symbol{Desc: "Symbol.search", HasDesc: true}
		i.symSplit = &Symbol{Desc: "Symbol.split", HasDesc: true}
		i.symSpecies = &Symbol{Desc: "Symbol.species", HasDesc: true}
		i.symUnscopables = &Symbol{Desc: "Symbol.unscopables", HasDesc: true}
		i.symIsConcatSpreadable = &Symbol{Desc: "Symbol.isConcatSpreadable", HasDesc: true}
	}

	i.bootstrap()
	return i
}

// Context returns the interpreter's execution context.
func (i *Interpreter) Context() context.Context { return i.ctx }

// currentRealmKey is the context key under which the running realm is stored.
// The running execution context's Realm (§9.4) governs which realm's intrinsics
// and error constructors abstract operations use — most visibly for the
// TypeErrors a Proxy raises, which come from the *current* realm rather than the
// realm that created the proxy.
type currentRealmKey struct{}

// currentRealm returns the running realm recorded in ctx, or nil when unset.
func currentRealm(ctx context.Context) *Interpreter {
	r, _ := ctx.Value(currentRealmKey{}).(*Interpreter)
	return r
}

// withCurrentRealm returns ctx tagged with i as the running realm. It reuses ctx
// unchanged when it already names i, so same-realm calls — the overwhelming
// majority — allocate nothing; only a genuine cross-realm boundary wraps ctx.
func (i *Interpreter) withCurrentRealm(ctx context.Context) context.Context {
	if currentRealm(ctx) == i {
		return ctx
	}
	return context.WithValue(ctx, currentRealmKey{}, i)
}

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
	// Close any inner realms (ShadowRealm) so their event-loop goroutines stop.
	for _, child := range i.childRealms {
		child.Close()
	}
	return nil
}

// NewChildRealm creates a second, fully independent realm (a fresh Interpreter
// with its own global object and complete set of intrinsics) that nonetheless
// belongs to the same agent as i: it shares i's GlobalSymbolRegistry (so
// Symbol.for is consistent across realms), inherits i's cancellation context,
// clock/timer providers, module provider, and bytecode setting, and is closed
// automatically when i is closed. Objects flow between the two realms directly
// as ordinary values (no wrapping) — each retains the [[Prototype]] and, for
// built-ins, the [[Realm]] of the realm that created it. This backs multi-realm
// host hooks such as Test262's $262.createRealm.
func (i *Interpreter) NewChildRealm() *Interpreter {
	child := New(func(c *Interpreter) {
		c.ctx = i.ctx // wrapped with its own cancel by New; parent cancel propagates
		c.useBytecode = i.useBytecode
		c.clock = i.clock
		c.timer = i.timer
		if i.moduleProvider != nil {
			c.moduleProvider = i.moduleProvider
		}
		// Agent-level Symbol state is shared across realms: the GlobalSymbolRegistry
		// (Symbol.for/keyFor) and every well-known symbol (Symbol.iterator, ...), so
		// e.g. otherRealm.Symbol.iterator === Symbol.iterator. Pre-populating before
		// bootstrap makes initSymbol and every intrinsic key off the shared symbols.
		c.symByKey = i.symByKey
		c.symBySym = i.symBySym
		c.symIterator = i.symIterator
		c.symAsyncIterator = i.symAsyncIterator
		c.symToPrimitive = i.symToPrimitive
		c.symToStringTag = i.symToStringTag
		c.symHasInstance = i.symHasInstance
		c.symMatch = i.symMatch
		c.symMatchAll = i.symMatchAll
		c.symReplace = i.symReplace
		c.symSearch = i.symSearch
		c.symSplit = i.symSplit
		c.symSpecies = i.symSpecies
		c.symUnscopables = i.symUnscopables
		c.symIsConcatSpreadable = i.symIsConcatSpreadable
	})
	i.childRealms = append(i.childRealms, child)
	return child
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
