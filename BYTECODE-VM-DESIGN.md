# gojs — Bytecode VM design report

Status: design study + working prototype, 2026-07-03. The `bytecode` branch now
carries a functioning opt-in VM (see "Prototype status" below); the rest of this
document is the design analysis it was built from — what a bytecode VM for gojs
looks like, whether it could host `tsc`, and what it buys us. Grounded in the
current tree-walker (files cited inline).

---

## Prototype status (bytecode branch, 2026-07-03)

A first working slice exists behind `WithBytecode()` — off by default, tree-walker
unchanged as the reference. Files: `interp/bc_opcodes.go`, `bc_compiler.go`,
`bc_vm.go`, `bc_vm_test.go`, `bc_bench_test.go`.

**What it does.** Eligible (non-generator, non-async) function bodies are compiled
once in `makeFunction` to a ~55-opcode stack machine and executed by `execCode`.
The compiler lowers the hot structural nodes — control flow, operators, calls,
member access, closures, templates, `new` — to real opcodes and emits an
`opEvalNode`/`opEvalStmt` escape hatch that runs any not-yet-native subtree on the
tree-walker with the frame's live env. So a compiled body is **always correct**:
unimplemented nodes fall back per-subtree, and labels / direct `eval` fall back the
whole function. Goroutine coroutines are untouched (generators/async never
compile). Scopes are still name-based in this slice (the same `*Environment` ops
the tree-walker uses) — slot-based locals are the next layer.

**Correctness.** Validated differentially: a 60-case unit test asserts bytecode
output == tree-walker output, and a Test262 harness toggle (`GOJS_T262_BYTECODE=1`)
runs the whole suite on the VM. A ~70k-test broad pass (all of `language/*` +
23 built-in domains) reduced to **zero divergence** after fixing the handful the
diff surfaced — three of which were latent **tree-walker** bugs the VM exposed and
that are now fixed for both engines (`new` arg-vs-isConstructor order §13.3.5.1;
`null[key]` ToObject-before-ToPropertyKey §13.3.3; and templates now flat-string +
ToString-correct).

**Slot-based locals — DONE (bc_resolver.go).** A fully-native function (no
fallback, no let/const, no nested closure, no `arguments`-object need) now gets
frame slots for its parameters and function-scope vars instead of env-map
bindings. Eligibility is decided by the compiler itself: it attempts a slot
compile and aborts to name mode the instant it would emit a fallback or touch a
binding a slot can't model, so "the slot compile succeeded" is the single source
of truth (no separate analysis to drift). Local access becomes an array index;
compound/`++`/`--`/assignment on a slot skip the reference machinery entirely.

**Performance (tree-walker vs slot-mode VM, same programs):**

| workload | speedup | allocations (TW → VM) |
|---|---|---|
| `fib(27)` (recursion) | ~3.8× | 17.4M → 5.3M (−70%) |
| 200k arithmetic loop | ~4.4× | 3.4M → 2.0M (−41%) |
| 400×400 nested loop | ~4.3× | 2.5M → 1.6M (−38%) |
| 100k method calls | ~2.3× | 3.2M → 1.3M (−59%) |

(CPU ratios carry machine noise; the **allocation reductions are the robust,
machine-independent metric**.) This is exactly the §0 prediction realized: the
first slice's 1.3–1.6× was pure dispatch with allocs unchanged; slotting locals
then attacked the allocation count and roughly doubled the win again. The residual
`fib` allocations are the per-call env (still created for `this`/global lookups)
plus the locals array and boxed return value — eliminating the env for
pure-computational functions is the next increment.

---

## 0. Build order — resolver → bytecode → profile → optimize (do NOT gate on profiling)

"Profile before you optimize" is the right default for *most* software. It is the
wrong gate *here*, because for an interpreter the structural evidence for dispatch
cost is already conclusive without a profiler:

- every AST node is a Go **interface** (dynamic dispatch per node),
- every evaluation returns **`(Value, error)`** up a deep Go recursion,
- **repeated type switches** on node kind (`eval_expr.go:37`, `eval_stmt.go:279`),
- **name-based identifier lookup** walking `map[string]*binding` chains per access
  (`environment.go:184`),
- **poor instruction locality** — the "program" is a pointer graph, not a stream.

None of that needs `pprof` to confirm it's expensive. And a subtler point makes
"profile first" actively misleading here: **a profile of the tree-walker measures
the tree-walker, not the VM.** Its hot spots may not transfer, so optimizing them
first is partly wasted motion. The resolver pass and a minimal bytecode backend
are moves every serious interpreter makes *regardless* of data — they are not
speculative bets that need a profile to justify.

So the order is:

```
resolver  →  minimal bytecode backend  →  PROFILE  →  optimize what actually shows up
```

Profiling is not skipped — it is *repositioned*. Once bytecode exists, the profiler
is finally pointed at the artifact we will ship, and it becomes dramatically more
informative (it will tell us whether the next win is inline caches, SMI, allocation
reduction, or something unexpected). Profile to *choose the optimization*, not to
*justify the VM*.

## 0. TL;DR

- **Today gojs is a pure tree-walker.** The parser emits `*ast.Program` (55 sealed
  node types + 9 aux structs, no IR); `interp` type-switches over the AST and
  evaluates nodes directly (`eval_expr.go:37`, `eval_stmt.go:279`). There is **no
  existing bytecode/IR** for JS — the only compile→opcode→VM in the repo is the
  isolated `jsregexp/` engine (a useful in-house reference for the pattern).
- **Build the resolver + a minimal bytecode backend *before* profiling, not
  after (§0).** For an interpreter, the structural evidence for dispatch cost
  (interface-typed nodes, `(Value, error)` recursion, per-node type switches,
  name-based lookup, poor locality) is already conclusive — don't gate the VM on a
  profile. Profile *after* bytecode exists, when the profiler measures the artifact
  you'll ship and tells you which optimization (IC / SMI / allocation) to build
  next. Highest-confidence single win: **scope-slot resolution**.
- **Keep the dedicated-goroutine coroutine model — maybe forever; do not reify
  generators.** Go's runtime already gives stack management, suspension,
  cancellation, cleanup, and scheduling for free, and the model already terminates
  cleanly on close (ctx-cancel + `i.wg`), which puts gojs *ahead* of many engines
  on simplicity. Reifying would hand-roll all five and *forecloses real
  parallelism* for ShadowRealm / worker-style isolation. Simpler now, strategic
  later (§5a, §5a′).
- **Running `tsc.js` is mostly a *host-shim + performance* problem, not a
  language problem.** gojs is at 98.28% Test262 — close enough on language. What
  it lacks is `fs`/`path` host modules and the *speed* to typecheck a real
  project in tolerable time. The bytecode VM is precisely the speed enabler.
- **"tsc emits bytecode directly" is real and has prior art** (Microsoft's Static
  TypeScript, AssemblyScript) — its unique payoff is **types survive to codegen**,
  enabling type-specialized opcodes a plain JS engine can't emit. But it only
  helps first-party code you compile through your own toolchain, and couples an
  external emitter to gojs's internal bytecode ABI. Treat it as a phase-2
  research track, not the way to "run TypeScript."

---

## 1. Where gojs is today (the baseline a VM replaces)

| Concern | Current tree-walker mechanism | Ref |
|---|---|---|
| Dispatch | Go type switch on AST node pointers; no visitor, no IR | `eval_expr.go:37`, `eval_stmt.go:279` |
| Eval signature | `(ctx, node, env) → (Value, error)` | `eval_stmt.go:261`, `eval_expr.go:36` |
| Completions | typed Go **error sentinels**: `*Throw`, `*returnSignal`, `*breakSignal`, `*continueSignal`, `*genReturn`, `*LimitError` (no panic/recover) | `errors.go:18-57` |
| Values | Go interface, single `Typeof()` method; value-typed primitives + pointer `*Object`/`*Symbol`/`*BigInt`/`*strRope`. **No NaN-boxing, no SMI.** | `value.go:36` |
| Numbers | uniformly `float64`; BigInt separate `*big.Int` | `value.go:57`, `object.go:49` |
| Objects | `map[PropertyKey]*Property` + ordered `keys` slice + dense `[]Value` array fast path; **no shared shapes/hidden classes** | `object.go:130` |
| Functions | one `CallFn` closure for native **and** JS; JS closure captures its defining `*Environment` by pointer | `object.go:107`, `function_make.go:26` |
| Scopes | parent-linked chain of **`map[string]*binding`**; every variable access is a name→binding map lookup up the chain; **no slot indices** | `environment.go:12,105,184` |
| JS frame = Go frame | yes — recursion depth is Go recursion depth, bounded by `MaxCallDepth`=6000 to avoid crashing the host goroutine stack | `interp.go:400`, `limits.go:16` |
| **Generators** | **one goroutine per instance**, `resume`/`out` channels, cooperative rendezvous | `builtin_generator.go:9,84` |
| **async/await** | same `startCoroutine` machinery; resumption via microtask | `async.go:25,196` |
| **async generators** | coroutine + request-queue state machine | `async_generator.go:38` |
| try/catch/finally | Go error-return propagation; `err.(*Throw)` filter lets signals pass | `eval_tryswitch.go:12` |
| Event loop | `eventLoop` micro+macro queues, drained after top-level program | `eventloop.go`, `run.go:38` |

Two facts dominate the VM design:

1. **Variables are resolved by name at runtime.** The parser produces an
   *unresolved* AST — no lexical addressing. Hoisting, TDZ, and closure capture
   are all done dynamically in `interp` against map-based environments
   (`run.go:29`→`evalProgram`→`hoistDeclarations`→`execStmts`). This is the single
   biggest departure from any real VM and the biggest single speedup available.

2. **Suspension is solved with goroutines.** Generators, async functions, and
   async generators all run on `startCoroutine` (`builtin_generator.go:84`): the
   body runs on a dedicated goroutine, and `yield`/`await` block on a channel
   rendezvous. This is elegant and correct, and it works *because the Go stack is
   the JS stack* — suspending is just "block the goroutine." A bytecode VM can
   either keep this or replace it (see §5).

---

## 2. What a bytecode VM is (and which flavor fits gojs)

A bytecode VM adds a **compile step** (AST → linear instruction stream in a
`CodeObject`) and an **execution loop** (a tight `for` over `[]Instr` with a
`switch` on the opcode). It separates *analysis done once* (scope resolution,
constant pooling, control-flow lowering) from *execution done many times*.

**Stack vs register.** Recommend a **stack machine** for v1 (like CPython/JVM):
no register allocator, trivial to compile expression trees to
(post-order-emit → operands land on an operand stack). A register/accumulator
design (like V8 Ignition, Lua) is faster but needs allocation and is a later
optimization. The opcode *surface* barely changes between the two, so starting
stack-based does not paint you into a corner.

**A `CodeObject` (per function/script):**

```
type CodeObject struct {
    Code       []byte        // packed opcodes + operands
    Consts     []Value       // literal pool: numbers, strings, bigints
    Templates  []*CodeObject // nested function/class bodies
    NumLocals  int           // frame slot count (from the resolver)
    NumUpvals  int           // captured-variable count
    UpvalDesc  []UpvalDesc   // {fromParentLocal bool; index int}
    Handlers   []HandlerSpan // try/catch/finally table: {start,end,catchIP,finallyIP,scopeDepth}
    LineTable  []LinePC      // IP → source pos (for stacks; reuses token.Pos)
    Flags      // strict, generator, async, arrow, hasParamExprs, needsArguments, ...
}
```

Frame at runtime:

```
type Frame struct {
    code   *CodeObject
    ip     int
    locals []Value      // NumLocals slots (params + let/const/var/temps)
    stack  []Value      // operand stack
    upvals []*Upvalue   // captured bindings (shared *binding-equivalent)
    this   Value
    newTgt Value
    home   *Object      // [[HomeObject]] for super
}
```

`Upvalue` is the boxed-binding analogue of today's `*binding` — a heap cell so
closures share a captured variable by pointer (mirrors the current
`map[string]*binding` sharing, `environment.go:105`).

---

## 3. Proposed opcode set for gojs (~100 opcodes)

Grounded in the 33 expression + 21 statement AST nodes. Operators are discriminated
by `token.Type` on `BinaryExpr`/`UnaryExpr`/`AssignExpr`/`UpdateExpr` today, so the
compiler switches on `token.Type` to pick the opcode.

**Constants / literals**
```
NIL UNDEF TRUE FALSE
CONST k            ; push Consts[k]  (NumberLit, StringLit, BigIntLit)
REGEXP k           ; fresh RegExp object each execution (spec-required)
```

**Composite literals**
```
NEW_ARRAY n        ; ArrayLit; then ELEM/HOLE/SPREAD_ARRAY per element
NEW_OBJECT
DEF_FIELD k        ; ObjectLit data prop           DEF_COMPUTED
DEF_GETTER k       DEF_SETTER k                     SPREAD_OBJECT   ; CopyDataProperties, Proxy-aware
TEMPLATE n         ; concat n parts (rope-friendly) TAGGED_TEMPLATE k
```

**Variables (post-resolver)**
```
GET_LOCAL s   SET_LOCAL s          ; frame slot
GET_UPVAL u   SET_UPVAL u          ; captured
GET_GLOBAL k  SET_GLOBAL k         ; late-bound global by name (inline-cache site)
GET_NAME k    SET_NAME k           ; DYNAMIC fallback: with-scope / sloppy-eval / unresolved
TDZ_CHECK s                        ; let/const before init (or folded into GET_LOCAL for lexicals)
INIT_LEX s                         ; complete a lexical binding
```

**Operators** (`eval_ops.go` today)
```
ADD SUB MUL DIV MOD POW
BAND BOR BXOR SHL SHR USHR
EQ NEQ SEQ SNEQ LT LE GT GE
INSTANCEOF IN
NEG NOT BNOT TYPEOF VOID TO_NUMERIC
INC DEC                            ; UpdateExpr
```
Short-circuit `&& || ??` compile to **jumps**, not opcodes:
```
JUMP_IF_FALSY_KEEP off   JUMP_IF_TRUTHY_KEEP off   JUMP_IF_NULLISH_KEEP off
```

**Property access / references** (`eval_call.go`, `MemberExpr`)
```
GET_PROP k   GET_ELEM             GET_PROP_OPT k   GET_ELEM_OPT
SET_PROP k   SET_ELEM
GET_SUPER_PROP k   SET_SUPER_PROP k
GET_PRIVATE p      SET_PRIVATE p           ; #private brand check
HAS_PRIVATE p                              ; #x in obj
DELETE_PROP k      DELETE_ELEM
```

**Calls**
```
CALL argc                ; plain call
CALL_METHOD k argc       ; keeps `this` (a.b() without re-evaluating a)
CALL_SPREAD              ; args include spread
NEW argc     NEW_SPREAD
SUPER_CALL argc          ; derived-ctor this-rebind lives here
CALL_OPT argc            ; fn?.()
```

**Control flow**
```
JUMP off   JUMP_IF_TRUE off   JUMP_IF_FALSE off
RETURN     THROW
ENTER_TRY handlerIdx   LEAVE_TRY               ; push/pop handler span
ENTER_FINALLY   END_FINALLY                    ; finally completion state machine
; break/continue → JUMP (backpatched); labeled break crossing finally → JUMP_FINALLY
SWITCH ...                                     ; comparison chain or jump table
```

**Iteration**
```
GET_ITERATOR   ITER_NEXT   ITER_RESULT_DONE   ITER_VALUE   ITER_CLOSE   ; for-of
FORIN_ENUM     FORIN_NEXT                                              ; for-in
GET_ASYNC_ITERATOR   AWAIT_ITER_NEXT                                   ; for await
```

**Functions / classes**
```
CLOSURE k        ; build fn object from Templates[k], capture upvals
ARROW k          ; lexical this/arguments
MAKE_CLASS k
```

**Coroutine suspension** — the design fork (see §5)
```
INITIAL_YIELD    ; generator start
YIELD   YIELD_STAR   AWAIT
```

**Scope / misc**
```
PUSH_BLOCK / POP_BLOCK    ; block scope for let/const (or eliminated by flat slots + TDZ)
PUSH_WITH / POP_WITH      ; WithStmt (forces dynamic name lookup inside)
COPY_DATA_PROPS           ; rest element in destructuring
```

Count: ~100–110 opcodes, in line with CPython (~120) and below Ignition (~200
with type variants). Everything hard-won in the tree-walker maps onto exactly one
of these — e.g. derived-class `this` rebind is `SUPER_CALL`, `new.target` is a
frame field read, TDZ is `TDZ_CHECK`, Proxy-aware object spread is
`SPREAD_OBJECT`/`COPY_DATA_PROPS`.

---

## 4. The mandatory prerequisite: a scope-resolver pass

This is not optional and it is where most of the value is. Before/while compiling,
a static pass over the AST assigns every `Ident` a resolution:

- **local slot** `s` — declared in the current function frame (params, `var`,
  `let`, `const`, temps),
- **upvalue** `u` — free variable captured from an enclosing function,
- **global** — resolved late by name (`GET_GLOBAL`, inline-cacheable),
- **dynamic** — inside a `with` scope or a sloppy-mode direct `eval`: must fall
  back to `GET_NAME`/`SET_NAME` (runtime chain walk, exactly like today).

TDZ (`let`/`const` before initialization) becomes a per-slot `initialized`
tracking or, where the compiler can prove initialization dominates use, is
elided. The current per-scope `superInit`, `fieldInit`, `homeObj`, `newTgt`
(`environment.go:12`) become frame fields or compile-time facts.

The de-opt triggers — `with` and direct sloppy `eval` — are detected
syntactically by the compiler, which marks affected scopes "dynamic" and emits
name-based ops there. Real engines do exactly this. Note gojs's `with`
(`WithStmt`) and sloppy `eval` (`eval_source.go`) both already exist and must be
honored.

**This pass alone could speed up even the existing tree-walker** (resolve idents
to `*binding` pointers once instead of walking `map[string]*binding` every
access), so it is a low-risk first deliverable independent of any bytecode.

---

## 5. The hard cases, and how they map

### 5a. Generators / async — keep goroutines, or reify frames?

This is *the* decision. Today all suspension is `startCoroutine`
(`builtin_generator.go:84`): a goroutine per coroutine, channel rendezvous on
`yield`/`await`. Two VM options:

- **Option A — keep the goroutine model (recommended; likely the keeper, maybe
  forever).** Each coroutine runs the VM loop on its own goroutine; `YIELD`/`AWAIT`
  do the same channel handoff. The reason this is the *right* design and not just
  the easy one: **Go's runtime already gives you, for free, the five hardest parts
  of coroutine support** — stack management, suspension, cancellation, cleanup, and
  scheduling. Reifying frames means reimplementing all five by hand. And the
  correctness property that usually breaks first is already satisfied: the current
  model **terminates on close** — every generator goroutine selects on
  `gs.ctx.Done()` and is tracked by `i.wg`, so `Close` cancels the context and
  waits out every suspended coroutine (`builtin_generator.go:9-19`). No orphans.
  That already puts gojs *ahead* of many JS engines on implementation simplicity.
  *Cost:* a few KB of goroutine stack per **simultaneously-suspended** generator,
  and the Go-stack recursion ceiling stays. Both are non-issues for realistic
  workloads.

- **Option B — reify frames (do not pursue for generators).** A bytecode `Frame`
  is an explicit heap object, so a coroutine *could* suspend by saving `ip` and
  stashing the frame — no goroutine. The only scenario where this wins is millions
  of *concurrently-suspended* generators, where goroutine-stack memory adds up —
  a pathological workload. Pursuing it means hand-rolling the five things Go gives
  you above, and it *forecloses* the parallelism angle below. **Don't reify
  generators.** If step-3 profiling (§9) ever flags per-generator memory on a real
  workload, revisit *then* — but plan on keeping goroutines.

**Recommendation: start with Option A and keep it.** It is simpler, already
reliable, and — unlike reified frames — it leaves the door open to *real
parallelism* (next). If profiling later shows per-generator goroutine memory is a
genuine bottleneck, migrate hot paths to Option B; the opcode set (`YIELD`/`AWAIT`)
is identical, only the runtime backing changes.

### 5a′. Goroutines as a parallelism substrate (ShadowRealm, workers)

Today the coroutine goroutines are deliberately *cooperative, never parallel* —
only one touches interpreter state at a time (`builtin_generator.go:12-14`). But
the goroutine model is the right foundation for genuine parallelism *where there
is no shared mutable state*:

- **ShadowRealm** is the natural fit: a realm has its own global object and its own
  intrinsics, and objects cannot cross the callable boundary (only primitives +
  wrapped callables). The object graphs are disjoint, so two realms *could* run on
  separate goroutines truly in parallel.
- **What blocks it today:** the `*Interpreter` holds shared *mutable* state that
  would race — the UTF-16 units memo (`unitsKey`/`unitsVal`, `interp.go:144`), the
  symbol registry/interner, the single `eventLoop`, and the `callDepth`/`steps`
  counters. True parallel realms therefore means giving each realm its own
  interpreter-state island (own intrinsics, own event loop, own caches) with only
  *immutable* data shared, plus thread-safe marshalling across the realm boundary.
  That's real work, but it's a clean, well-bounded design — and it's *only*
  reachable if we keep goroutines rather than reifying onto one shared VM stack.

So: dedicated goroutines aren't just the simpler start — they're the strategic
choice if parallel realms / workers are on the roadmap.

### 5b. The Go-stack ceiling

Today a JS frame is a Go frame, capped at `MaxCallDepth`=6000 (`interp.go:400`,
`limits.go:16`) to avoid crashing the host goroutine. With reified frames, a
pure-JS call chain is a push onto an **explicit VM call stack** (heap), so JS
recursion is bounded by heap, not the Go stack — the 6000 ceiling can rise
dramatically. *Caveat:* the moment JS calls a native builtin that calls back into
JS (a JS `sort` comparator, a `valueOf`, a Proxy trap), you re-enter Go
recursion. So you get a **hybrid**: VM frames for pure-JS chains, Go stack across
JS↔native↔JS. Generators are unaffected by this caveat (see 5a).

### 5c. try/catch/finally

Replace error-return propagation with a per-`CodeObject` **handler table**
(`Handlers []HandlerSpan`). On `THROW` (or a native error surfacing into the VM),
the loop scans the current frame's spans for one covering `ip`; if none, it pops
the frame and continues up the VM call stack. This mirrors today's
`err.(*Throw)` filter that lets `*returnSignal`/`*breakSignal` pass through
(`eval_tryswitch.go:12`) — the table only catches throws.

`finally` is the subtle part: it must run on *every* exit (fallthrough, throw,
return, and break/continue that jump across it). Use a **completion register**: the
`finally` block runs with a pending completion `{normal|throw|return|break k}` and
re-dispatches it at `END_FINALLY`. This is the direct bytecode analogue of the
current signal machinery (`genReturn`, labeled signals).

### 5d. Closures

`CLOSURE k` builds a function object from `Templates[k]` and captures upvalues per
`UpvalDesc` (from a parent local slot, or re-captured from a parent upvalue).
Captured cells are shared heap boxes — the pointer-sharing that today falls out of
`map[string]*binding` (`environment.go:105`). Per-iteration `let` capture
(`createPerIterationEnvironment`, `eval_loops.go:186`) becomes "fresh upvalue cells
per loop iteration," a well-understood compiler idiom (Lua's approach).

### 5e. Numbers (SMI) and inline caches — deliberately deferred

The two biggest *additional* wins each depend on a **separate** large change:

- **SMI / tagged small integers**: needs a value-representation change (tagged
  `Value` or type-specialized opcodes). Do **not** bundle this with the bytecode,
  and especially do not bundle it with the pending UTF-16 string rework — ship the
  VM float64-only behind the same opcodes and specialize later.
- **Inline caches** (per-site shape→slot memo on `GET_PROP`/`GET_GLOBAL`/`CALL`):
  need **hidden classes / shapes**, which the current `map[PropertyKey]*Property`
  object model (`object.go:130`) does not have. The bytecode gives you the *site*
  to hang a cache on; the shapes are a follow-on project.

So the bytecode alone delivers **dispatch + scope-slotting + coroutine** wins. The
"now it's V8-fast" story needs these follow-ons — be honest about that.

---

## 6. Can a bytecode VM directly run `tsc`?

Two genuinely different questions are hiding here.

### Q1 — Run the shipped `tsc.js` on gojs

`tsc` ships as JavaScript (`typescript` npm package: `lib/typescript.js` ~8–10 MB,
`lib/tsc.js`), CommonJS, ~ES2018. What it takes:

1. **Language conformance — mostly there.** gojs is at 98.28% Test262. `tsc`
   deliberately avoids exotic features, so the language surface is very likely
   covered. Known-risk gaps:
   - the **UTF-16 vs code-point** string divergence (`NOTES-divergences.md`): the
     scanner uses `charCodeAt`/length arithmetic; astral characters in *source*
     could mis-scan. In practice tsc source is overwhelmingly ASCII/BMP, so this
     is a low-probability bite, but it's the most likely correctness hazard.
   - RegExp: `tsc` uses regexes; the in-house `jsregexp` backtracking engine
     should cover them (verify against tsc's actual patterns).
2. **Host environment — the real gap, but the scaffolding exists.** `tsc` needs
   `require` (CommonJS), `fs`, `path`, `os`, `process` (argv/cwd/exit), and a bit
   of `Buffer`/`performance`. gojs *already* has the capability model for this:
   `ModuleProvider`/`require` interception (`WithModuleProvider`,
   `NewDirModuleProvider`), `OsProvider` (the `process` global), and a
   `SourceMapper` that exists specifically to map transpiled-TS stack frames
   (`gojs.go`). You'd supply Go-backed `fs`/`path` shims — moderate, well-trodden
   work inside the existing provider walls.
3. **Performance — where the bytecode VM earns its keep.** Typechecking a real
   project allocates and traverses millions of nodes. On a tree-walker this is
   *unusably* slow (order 50–200× V8). A bytecode VM with scope-slotting is the
   difference between "runs but you wait minutes" and "usable." **The VM does not
   *enable* running tsc — conformance + host shims do — but it is what makes it
   practical.**

**Verdict:** running `tsc.js` is a host-shim + performance project, not a language
project. gojs is close on language and already has the provider architecture; the
bytecode VM is the speed enabler. That the codebase already ships a TS
`SourceMapper` shows this is an intended use case.

### Q2 — Have the tsc *transpile step emit gojs bytecode directly*

Interpretation: instead of `TS → JS text → gojs-parse → gojs-bytecode`, write a
custom backend that walks the (typechecked, down-levelled) TypeScript AST and
emits gojs bytecode, skipping the JS-text round trip.

- **Feasible?** Yes. The TypeScript compiler API exposes the fully-typed,
  post-transform AST (`ts.createProgram` → `getTypeChecker` → transformer pipeline).
  You reuse `tsc` as a *front end* (parse + typecheck + down-level) and replace only
  the final print-to-JS step with a bytecode emitter. This is essentially writing a
  `TS-AST → gojs-bytecode` compiler.
- **Prior art:** Microsoft **Static TypeScript** (STS, the MakeCode engine) does
  exactly this — a TS *subset* compiled to a compact bytecode VM for
  microcontrollers. **AssemblyScript** compiles a TS subset to WASM. Both prove
  the model; both also demonstrate its constraint: they support a *subset*, not
  arbitrary TypeScript.
- **The unique payoff — types survive to codegen.** A normal JS engine throws
  types away at parse time; here you'd have them at emit time. That enables
  **type-specialized opcodes**: known-`number` operands → `ADD_NUMBER` with no
  `ToNumeric` dispatch, monomorphic member access → a pre-seeded inline cache /
  resolved slot, typed arrays/fields → specialized storage. This is a genuine
  advantage a plain JS engine *cannot* reproduce.
- **Costs / caveats:**
  - It only helps code you compile through *your* toolchain — not arbitrary npm
    JS, and not `tsc.js` itself.
  - It couples an external emitter to gojs's internal bytecode ABI — you now have a
    versioned bytecode format to keep stable across engine changes.
  - TypeScript's type system is **unsound** (`any`, casts, `as`), so
    type-driven opcodes still need runtime guards + de-opt paths; the types are
    hints, not proofs.

**Verdict:** promising as a **phase-2 research track** for hot *first-party* code
where you own the source and want type-specialized bytecode (à la Static
TypeScript). It is *not* the path to "run the TypeScript ecosystem" — that's Q1.
Do Q1 first.

---

## 7. Benefits of a bytecode VM (honest accounting)

Ordered by confidence, with the current mechanism each one displaces:

1. **Dispatch locality.** Tree-walk = a Go interface type-switch + method call +
   `(Value, error)` return per node, chasing pointers with poor I-cache behavior
   (`eval_expr.go:37`). Bytecode = a tight loop over a flat `[]byte` with one
   opcode switch — better branch prediction and cache behavior. Typical
   tree→bytecode speedups: **2–5×** on straight-line code before any other change.
2. **Scope slotting (biggest single win).** Replace the per-access
   `map[string]*binding` chain walk (`environment.go:184`) with an array index.
   Variable access is the most frequent operation in real code; this alone is
   often **2–10×** on variable-heavy workloads. Available even without bytecode
   (see §4).
3. **No goroutine per coroutine.** Reified frames (§5a) turn generators/async into
   cheap heap objects — removes per-instance goroutine stacks (KBs each), the
   WaitGroup/`ctx.Done()` leak plumbing, and channel-rendezvous latency per
   `yield`/`await`. Large for generator- and async-heavy code.
4. **Raised recursion ceiling.** Explicit VM call stack lifts the Go-stack-bound
   `MaxCallDepth`=6000 for pure-JS recursion (§5b).
5. **Compile-once, run-many.** Loops/functions pay parse+resolve+lowering *once*;
   the tree-walker re-walks and re-resolves names on every iteration.
6. **A substrate for optimizations that have nowhere to live in an AST walk:**
   inline caches, hidden classes, SMI specialization, peephole/const-folding, dead
   code elimination, and eventually a JIT tier (bytecode is the natural JIT input).
   None are expressible over a bare tree-walk.
7. **Serializable code objects.** Bytecode can be cached to disk (skip re-parse on
   startup) and is the substrate for the Q2 "tsc emits bytecode" idea.

---

## 8. Costs & risks (equally honest)

- **Large surface, second engine to maintain.** A resolver, a compiler for all 55
  AST nodes + 9 aux structs, the VM loop, the handler-table mechanism, frame
  reification, and re-plumbing every builtin that re-enters JS. Multi-month, and it
  must be kept in lockstep with the spec alongside the tree-walker.
- **Conformance regression risk.** You're at 98.28% with a *lot* of hard-won
  correctness (derived-ctor `this` rebind, TDZ, early errors, eval semantics,
  Proxy-aware ops). A fresh engine risks re-introducing fixed bugs. **Mitigation:
  keep the tree-walker, run BOTH against Test262 (differential testing), gate the
  VM behind a flag until parity.**
- **The headline wins depend on follow-ons.** Inline caches need hidden classes;
  SMI needs tagged values. Bytecode alone buys dispatch + slotting + coroutine
  wins — not automatic "V8 parity."
- **Coupling risk in Q2.** A TS→bytecode emitter pins a public bytecode ABI.

---

## 9. Recommended path (incremental, low-risk)

1. **Resolver pass** — static scope analysis annotating idents with
   slot/upvalue/global/dynamic. Independent of bytecode; can speed up the current
   tree-walker on its own; de-opt `with`/sloppy-`eval` scopes. Lowest risk,
   immediate value, and it's the prerequisite for everything else.
2. **CodeObject + compiler + minimal stack VM** for a language *subset*
   (expressions, calls, control flow, closures), behind a flag, falling back to the
   tree-walker for unsupported nodes. **Differential-test against Test262 every
   step** — the tree-walker is the oracle.
3. **Profile now (not before).** With bytecode in place, `pprof` CPU + mem on a
   real workload (a chunk of `tsc`, loop/recursion/alloc benches). The profiler now
   measures the artifact you'll ship and tells you which optimization is next.
4. **Optimize whatever actually shows up** — peephole/const-fold, then (if the data
   says) hidden classes → inline caches, then SMI. Each independent, each
   data-driven.
5. **Keep goroutine coroutines — likely forever** (§5a). Do **not** reify
   generators/async: Go already gives stack management, suspension, cancellation,
   cleanup, and scheduling for free, and `Close()` already kills every coroutine.
   Reify *plain* call frames only if step 3 shows the Go-stack path is a real cost.
   Treat parallel realms (§5a′) as its own track.
6. **In parallel, pursue Q1 (`run tsc.js`)** — `fs`/`path` host shims on the
   existing provider model. It's the killer app that *justifies* the perf work and
   doesn't require the VM to be complete, only fast enough. Defer Q2 (typed
   bytecode) to a research spike once Q1 works.

The through-line: **profile first, then the scope resolver is the real prize; keep
the goroutines; the opcodes are the easy part; frame reification, the
optimizations (IC/SMI), and the typed-bytecode tsc backend are separate later
bets gated on data.**
