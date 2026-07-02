# Wontfix / deferred — generator/async function-family Test262

The generator/async function-family intrinsics — `%GeneratorFunction%` (§27.3),
`%AsyncGeneratorFunction%` (§27.4), `%AsyncFunction%` (§27.7) and their
`.prototype` objects — are implemented in `interp/builtin_genfunc.go`, wired into
the function object prototype chains by `interp/function_make.go`, and reachable
via `Object.getPrototypeOf(function*(){}).constructor` etc. Their dynamic
constructors reuse `CreateDynamicFunction` (`interp/builtin_function_ctor.go`).

After the push these dirs pass at:

* `built-ins/GeneratorFunction` — ~91.3% of runnable
* `built-ins/AsyncGeneratorFunction` — ~91.3% of runnable
* `built-ins/AsyncFunction` — ~94.4% of runnable

## `$262.createRealm` (cross-realm tests) — the only remaining failures

Files: `built-ins/GeneratorFunction/proto-from-ctor-realm{,-prototype}.js`,
`built-ins/AsyncGeneratorFunction/proto-from-ctor-realm{,-prototype}.js`,
`built-ins/AsyncFunction/proto-from-ctor-realm.js`.

These use `$262.createRealm()` to build a second realm and assert that
`Reflect.construct(GeneratorFunction, args, newTargetFromRealmB)` derives the new
function's prototype from realm B's `%GeneratorFunction.prototype%`. gojs has a
single realm per `Interpreter` and the test262 host `$262` object does not
provide `createRealm` (same limitation already documented in
`wontfix/typedarray.md` and `wontfix/weak-references-never-cleared.md`).
Deferred.
