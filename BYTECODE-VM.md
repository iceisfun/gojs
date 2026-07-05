# gojs — Bytecode VM architecture

gojs executes JavaScript on a **bytecode virtual machine**, on by default. The
parser emits an unresolved AST; a per-function compiler lowers eligible bodies to
a stack-machine `CodeObject`, and a tight execution loop runs it. The original
**tree-walking interpreter is retained** — as the fallback for constructs the
compiler declines, and as the differential oracle the VM is validated against.

This document describes how the VM works and why it is built the way it is. It
covers the engine as shipped on `master`; file/line citations point at the code.

- **Enable/disable.** The VM is enabled by `New` automatically. `WithBytecode()`
  is a no-op kept for compatibility; `WithTreeWalker()` forces the tree-walker,
  either as a differential reference or to sidestep a suspected VM regression
  (`interp/interp.go:305-318`).
- **Two engines, one behavior.** The compiler falls back to the tree-walker for
  any subtree it does not yet lower, so a compiled body is *always* correct — the
  two engines are behaviorally identical by construction (see "Correctness").

---

## 1. Baseline: what the VM sits on top of

The tree-walker is still the semantic reference, and the VM inherits its value
model, object model, and coroutine machinery unchanged.

| Concern | Mechanism | Ref |
|---|---|---|
| Values | Go interface, single `Typeof()`; value-typed primitives + pointer `*Object`/`*Symbol`/`*BigInt`/`*strRope`. No NaN-boxing, no SMI. | `value.go:36` |
| Numbers | uniformly `float64`; BigInt separate `*big.Int` | `value.go:57`, `object.go:49` |
| Objects | `map[PropertyKey]*Property` + ordered `keys` slice + dense `[]Value` array fast path; no shared shapes/hidden classes | `object.go:130` |
| Functions | one `CallFn` closure for native **and** JS; JS closure captures its defining `*Environment` by pointer | `object.go:107`, `function_make.go:26` |
| Scopes | parent-linked chain of `map[string]*binding`; name→binding lookup up the chain | `environment.go:12,105,184` |
| Completions | typed Go **error sentinels**: `*Throw`, `*returnSignal`, `*breakSignal`, `*continueSignal`, `*genReturn`, `*LimitError` (no panic/recover) | `errors.go:18-57` |
| **Generators** | **one goroutine per instance**, `resume`/`out` channels, cooperative rendezvous | `builtin_generator.go:9,84` |
| **async/await** | same `startCoroutine` machinery; resumption via microtask | `async.go:25,196` |
| **async generators** | coroutine + request-queue state machine | `async_generator.go:38` |
| Event loop | `eventLoop` micro+macro queues, drained after top-level program | `eventloop.go`, `run.go:38` |

Two facts shaped the VM design:

1. **The AST is unresolved.** The parser produces no lexical addressing; hoisting,
   TDZ, and closure capture are done dynamically against map-based environments.
   Turning name lookups into slot indices is the single biggest speedup, and it
   is the job of the resolver (§4).
2. **Suspension is solved with goroutines.** Generators, async functions, and
   async generators all run on `startCoroutine` (`builtin_generator.go:84`): the
   body runs on a dedicated goroutine and `yield`/`await` block on a channel
   rendezvous. This works *because the Go stack is the JS stack*. The VM keeps
   this model rather than reifying frames (§5a).

---

## 2. Compile + execute

Compilation adds a step that is done **once per function**: AST → a linear
instruction stream in a `CodeObject`, separating analysis (scope resolution,
constant pooling, control-flow lowering) from execution done many times.

The VM is a **stack machine** (like CPython/JVM): expression trees compile by
post-order emit, operands land on an operand stack. There is no register
allocator. This keeps the compiler simple without painting the opcode surface
into a corner — a register/accumulator redesign would reuse most of it.

**`CodeObject`** (per function/script) carries the packed code, a literal pool,
nested function/class bodies, the frame slot count from the resolver, upvalue
descriptors, a try/catch/finally handler table, an IP→source-pos line table, and
flags (strict, generator, async, arrow, …).

**`Frame`** (per activation) holds `code`, `ip`, a `locals` slot array
(params + let/const/var/temps), the operand `stack`, captured `upvals`, and
`this`/`new.target`/`[[HomeObject]]`. An upvalue is a boxed heap cell so closures
share a captured variable by pointer — the analogue of today's `*binding`
sharing (`environment.go:105`).

Compilation happens in `makeFunction`: eligible (non-generator, non-async)
bodies are compiled to a stack machine and executed by the VM; generators and
async functions stay on the coroutine machinery and are never compiled
(`function_make.go:64`).

---

## 3. Opcode set

The VM defines ~54 opcodes (`interp/bc_opcodes.go`), in the range of CPython
(~120) and well below V8 Ignition (~200 with type variants). Operators are
discriminated by `token.Type` on `BinaryExpr`/`UnaryExpr`/`AssignExpr`/
`UpdateExpr`, so the compiler switches on `token.Type` to select the opcode.

Broadly, the opcodes cover:

- **Constants / literals** — push pooled numbers/strings/bigints; fresh `RegExp`
  per execution (spec-required).
- **Composite literals** — array/object construction, data/getter/setter/computed
  properties, Proxy-aware object spread, template concatenation.
- **Variables (post-resolver)** — `GET_LOCAL`/`SET_LOCAL` (frame slot),
  `GET_UPVAL`/`SET_UPVAL` (captured), `GET_GLOBAL`/`SET_GLOBAL` (late-bound by
  name), `GET_NAME`/`SET_NAME` (dynamic fallback for `with` / sloppy `eval`), and
  TDZ/lexical-init.
- **Operators** — the arithmetic/bitwise/compare/logical set; `&& || ??`
  short-circuit compiles to **jumps** rather than opcodes.
- **Property access** — get/set prop and elem, optional-chaining variants,
  super-property access, `#private` get/set with brand checks, delete.
- **Calls** — plain call, method call (keeps `this` without re-evaluating the
  receiver), spread, `new`, `super(...)`, optional call.
- **Control flow** — jumps, `RETURN`, `THROW`, try/finally spans, switch.
- **Iteration** — for-of iterator protocol, for-in enumeration, for-await.
- **Functions / classes** — build closures/arrows (capturing upvalues) and
  classes from nested code objects.
- **Coroutine suspension** — `YIELD`/`YIELD_STAR`/`AWAIT` (used only inside the
  coroutine machinery).

Everything hard-won in the tree-walker maps onto exactly one of these:
derived-class `this` rebind is the `super(...)` opcode, `new.target` is a frame
field read, TDZ is a per-slot check, Proxy-aware object spread is its own opcode.

**The escape hatch.** The compiler emits `opEvalNode`/`opEvalStmt` for any subtree
it does not (yet) lower to native opcodes: that node runs on the tree-walker with
the frame's live environment, and execution resumes in the VM. Labeled statements
and direct `eval` fall the whole function back to the tree-walker. This is what
makes a compiled body always correct — unimplemented constructs degrade
per-subtree, never wrongly.

---

## 4. The scope resolver

The resolver (`interp/bc_resolver.go`) is where most of the VM's value lives. A
static pass assigns every identifier a resolution:

- **local slot** `s` — declared in the current function frame (params, `var`,
  `let`, `const`, temps),
- **upvalue** `u` — a free variable captured from an enclosing function,
- **global** — resolved late by name (`GET_GLOBAL`),
- **dynamic** — inside a `with` scope or a sloppy-mode direct `eval`: falls back
  to `GET_NAME`/`SET_NAME`, a runtime chain walk exactly like the tree-walker.

A fully-native function — no fallback, no nested-closure or `arguments`-object
need a slot can't model — gets frame slots for its parameters and function-scope
vars instead of env-map bindings. **Eligibility is decided by the compiler
itself**: it attempts a slot compile and aborts to name mode the instant it would
emit a fallback or touch a binding a slot can't model, so "the slot compile
succeeded" is the single source of truth — there is no separate analysis pass to
drift out of sync. Local access becomes an array index; compound/`++`/`--`/
assignment on a slot skip the reference machinery entirely.

The de-opt triggers — `with` (`WithStmt`) and direct sloppy `eval`
(`eval_source.go`) — are detected syntactically and their scopes marked dynamic.
Real engines do exactly this.

---

## 5. The hard cases

### 5a. Generators / async — goroutines, not reified frames

All suspension runs on `startCoroutine` (`builtin_generator.go:84`): a goroutine
per coroutine, channel rendezvous on `yield`/`await`. The VM keeps this rather
than reifying frames onto an explicit stack, and that is a deliberate choice, not
just the easy one: **Go's runtime already provides the five hardest parts of
coroutine support** — stack management, suspension, cancellation, cleanup, and
scheduling. The correctness property that usually breaks first is already
satisfied: every generator goroutine selects on `gs.ctx.Done()` and is tracked by
`i.wg`, so `Close` cancels the context and waits out every suspended coroutine
(`builtin_generator.go:9-19`) — no orphans.

The cost is a few KB of goroutine stack per *simultaneously-suspended* generator
and the Go-stack recursion ceiling. Both are non-issues for realistic workloads.
Reifying frames would win only for millions of concurrently-suspended
generators — and would foreclose the parallelism angle below — so generators are
not reified.

### 5a′. Goroutines as a parallelism substrate

The coroutine goroutines today are cooperative, never parallel — only one touches
interpreter state at a time (`builtin_generator.go:12-14`). But the goroutine
model is the right foundation for genuine parallelism *where there is no shared
mutable state*, e.g. **ShadowRealm**: disjoint object graphs (only primitives and
wrapped callables cross the boundary) could run on separate goroutines truly in
parallel. What stands in the way today is shared *mutable* interpreter state — the
UTF-16 units memo (`interp.go:144`), the symbol registry/interner, the single
`eventLoop`, and the `callDepth`/`steps` counters. True parallel realms means
giving each realm its own interpreter-state island with only immutable data
shared. That is bounded, real work — and it is reachable *only* because the engine
keeps goroutines rather than reifying onto one shared VM stack.

### 5b. The Go-stack ceiling

A JS frame is a Go frame, capped at `MaxCallDepth` = 6000 (`limits.go:28`) to
avoid crashing the host goroutine. Reifying *plain call frames* (distinct from
generators) onto an explicit VM stack would bound pure-JS recursion by heap
instead, lifting that ceiling — but the moment JS calls a native builtin that
calls back into JS (a `sort` comparator, a `valueOf`, a Proxy trap) it re-enters
Go recursion, so the result is a hybrid. This is a possible future increment, not
a current property.

### 5c. try/catch/finally

Error-return propagation is replaced by a per-`CodeObject` **handler table**. On a
throw, the loop scans the current frame's spans for one covering `ip`; if none, it
pops the frame and continues up the VM call stack. This mirrors the tree-walker's
`err.(*Throw)` filter that lets `*returnSignal`/`*breakSignal` pass through
(`eval_tryswitch.go:12`) — the table only catches throws. `finally` runs on
*every* exit via a **completion register**: the block runs with a pending
completion `{normal|throw|return|break k}` and re-dispatches it at the end — the
direct analogue of the signal machinery (`genReturn`, labeled signals).

### 5d. Closures

A closure opcode builds a function object from a nested code object and captures
upvalues per the upvalue descriptors (from a parent local slot, or re-captured
from a parent upvalue). Captured cells are shared heap boxes — the pointer-sharing
that falls out of `map[string]*binding` today (`environment.go:105`).
Per-iteration `let` capture (`createPerIterationEnvironment`,
`eval_loops.go:186`) becomes fresh upvalue cells per loop iteration.

### 5e. Deliberately out of scope: SMI and inline caches

The two biggest *additional* wins each need a separate large change and are not
part of the VM as it stands:

- **SMI / tagged small integers** need a value-representation change (tagged
  `Value` or type-specialized opcodes). The VM runs float64-only behind its
  opcodes; specialization is a later, independent step.
- **Inline caches** (per-site shape→slot memo on property/global/call sites) need
  hidden classes / shapes, which the `map[PropertyKey]*Property` object model
  (`object.go:130`) does not have. The bytecode provides the *site* to hang a
  cache on; the shapes are a follow-on project.

So the VM delivers **dispatch + scope-slotting + coroutine** wins. The
"V8-fast" story would need these follow-ons.

---

## 6. Correctness

The VM is validated **differentially against the tree-walker**, which is the
oracle:

- A unit test asserts bytecode output == tree-walker output across a suite of
  cases (`interp/bc_vm_test.go`).
- A Test262 harness toggle (`GOJS_T262_BYTECODE=1`) runs the whole conformance
  suite on the VM.

A broad differential pass (all of `language/*` plus the built-in domains) reduced
to **zero divergence** between the engines. Three of the discrepancies it surfaced
were latent **tree-walker** bugs the VM exposed, now fixed for both engines:
`new` arg-vs-`isConstructor` ordering (§13.3.5.1), `null[key]`
ToObject-before-ToPropertyKey (§13.3.3), and template flattening/`ToString`
ordering. The differential harness is the standing guarantee that the two engines
stay behaviorally identical; see the top-level README for the current Test262
figure.

---

## 7. Performance

Measured tree-walker vs slot-mode VM on the same programs:

| workload | speedup | allocations (TW → VM) |
|---|---|---|
| `fib(27)` (recursion) | ~3.8× | 17.4M → 5.3M (−70%) |
| 200k arithmetic loop | ~4.4× | 3.4M → 2.0M (−41%) |
| 400×400 nested loop | ~4.3× | 2.5M → 1.6M (−38%) |
| 100k method calls | ~2.3× | 3.2M → 1.3M (−59%) |

CPU ratios carry machine noise; the **allocation reductions are the robust,
machine-independent metric**. Dispatch alone (before slotting) bought ~1.3–1.6×
with allocations unchanged; slotting locals then attacked the allocation count and
roughly doubled the win again. The residual `fib` allocations are the per-call env
(still created for `this`/global lookups), the locals array, and the boxed return
value — eliminating the env for pure-computational functions is a candidate next
increment. Benchmarks live in `interp/bc_bench_test.go`.

---

## 8. What the VM enables next

Ordered by confidence, each independent and data-driven:

1. **Peephole / const-folding** over the linear instruction stream — nowhere to
   live in an AST walk.
2. **Hidden classes / shapes → inline caches** — the object-model change that
   unlocks per-site property/global/call caching.
3. **SMI / tagged integers** — value-representation specialization.
4. **Raised recursion ceiling** — reify plain call frames (§5b) if the Go-stack
   path ever shows up as a real cost.
5. **Serializable code objects** — cache bytecode to skip re-parse on startup.

The through-line: the scope resolver was the real prize; the goroutine coroutine
model stays; the optimizations above (IC/SMI, frame reification) are separate
later bets, gated on profiling the shipped VM rather than the tree-walker.

---

## 9. Related: running `tsc`

A recurring question is whether the engine can run the shipped `tsc.js`. That is
primarily a **host-shim + performance** problem, not a language one: gojs's
language conformance is high (see README), and `tsc` deliberately avoids exotic
features. What it needs is Go-backed `fs`/`path`/`os` shims on the existing
provider model (`ModuleProvider`/`require`, `OsProvider`, and the TS
`SourceMapper` already present in `gojs.go`), plus enough *speed* to typecheck a
real project in tolerable time. The bytecode VM is precisely that speed enabler —
it does not *enable* running tsc (conformance + host shims do), but it is what
makes it practical. A separate, more speculative track is emitting gojs bytecode
directly from the typechecked TS AST (à la Microsoft Static TypeScript /
AssemblyScript) so types survive to codegen and enable type-specialized opcodes;
that helps only first-party code compiled through your own toolchain and couples
an external emitter to the internal bytecode ABI, so it stays a research spike.
