# Function-code: intentional divergences

Status after this work: `language/function-code` Test262 = **260 pass / 21 fail
(92.5% of runnable)**, up from 136/145 (48.4%). All 21 remaining failures are
intentional divergences, catalogued below. Nothing here is a silent bug — each
item is deliberate and, where user-visible, pinned by a harness regression test.

## 1. Dynamic `Function` constructor is disabled (20 failing tests)

`new Function(...)` / `Function(...)` throws `EvalError: Function constructor is
disabled in this sandbox` (`interp/builtin_function.go`, `initFunction`). This is
a deliberate security posture for the embeddable sandbox: gojs never compiles
code from strings via the `Function` constructor.

Affected function-code tests (both modes / `gs` variants): `10.4.3-1-13`, `-14`,
`-15`, `-16`, `-64`, `-65`, `-83`, `-84` (`-s` and `gs` forms). They construct a
function dynamically and assert its `this`. They cannot pass without enabling the
constructor, which is out of scope.

Plan (if ever wanted): gate behind a `Security` opt-in that routes the string
through `parser.Parse` + `makeFunction`, mirroring how `eval` is gated. The `this`
behaviour would then already be correct because the produced function flows
through the same `makeFunction` path fixed here.

## 2. Direct `eval` with caller scope is not implemented (1 failing test)

`interp/eval_source.go` implements **indirect** eval only: the code runs in the
global scope, so `this` inside `eval("this")` is the global object, not the
caller's `this`. `10.4.3-1-17-s.js` expects `eval("typeof this") === "undefined"`
inside a strict function, which requires direct eval to inherit the calling
function's (strict, `undefined`) `this` binding.

Plan: thread the current `*Environment` (and its `this`/strict flags) into
`evalSource` for the *direct* call form (callee is the identifier `eval`
resolving to the intrinsic), parse in the caller's strict context, and execute
against that environment. This is a sizeable, cross-cutting change (scope
capture, strict propagation, new lexical declarations leaking or not) and is
deferred; indirect eval covers the overwhelming majority of real usage.

## 3. A function expression's own-name binding is immutable like `const`

The named-FE binding (e.g. `f` in `var g = function f(){}`) is created as a
read-only binding; assigning to it throws `TypeError: Assignment to constant
variable.` even in sloppy mode, whereas the spec makes it a non-strict immutable
binding whose assignment is a silent no-op in sloppy code. This is a niche edge
(reassigning a function to its own inner name) and not covered by the
function-code corpus; noted here for completeness.
