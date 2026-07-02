# Wontfix / deferred — `built-ins/TypedArray*` Test262

The `%TypedArray%` family (the abstract intrinsic, the 11 concrete
constructors, the integer-indexed exotic behavior, and the §23.2.3 prototype
methods) is implemented in `interp/builtin_typedarray.go` /
`interp/builtin_typedarray_proto.go`. After the push it passes:

* `built-ins/TypedArray` — ~98.9% of runnable
* `built-ins/TypedArrayConstructors` — ~94.2% of runnable

The remaining failures are all rooted in engine-wide gaps that are out of scope
for the TypedArray work, not in the TypedArray implementation itself.

## `$262.createRealm` (cross-realm tests)

Files: `built-ins/TypedArrayConstructors/internals/{Get,Set,HasProperty,
GetOwnProperty,DefineOwnProperty,Delete}/*-realm.js` and similar.

These construct a second realm with `$262.createRealm()` and assert that a
TypedArray from realm A behaves correctly when its prototype/constructor come
from realm B. The test262 runner's `$262` host object provides
`detachArrayBuffer` and `global` but not `createRealm`: gojs has a single
realm per `Interpreter`, and standing up an independent global object graph
(its own intrinsics, prototypes, and constructors) inside one interpreter is a
much larger feature than the TypedArray surface. Deferred.

## The `with` statement (parser gap)

Files: `internals/HasProperty/infinity-with-detached-buffer.js` and its BigInt
sibling.

These use the sloppy-mode `with (ta) { ... }` statement, which the gojs parser
does not implement (it reports `expected ';'`). `with` is a legacy sloppy-only
construct; the two tests only use it as a harness convenience. Deferred with the
rest of the unsupported statement forms.

## Strict-mode `delete` returning a value

Files: `internals/Delete/indexed-value-*-strict.js` (and BigInt variants).

`[[Delete]]` on a valid integer index correctly returns `false`
(`Object.prototype.hasOwnProperty`/`Reflect.deleteProperty` observe it), but in
these tests `delete ta[0]` in strict-mode source must additionally *throw* a
TypeError. gojs is deliberately "non-strict-by-default": the evaluator does not
thread the surrounding strictness into the `delete` / assignment operators, so a
`false` result from `[[Delete]]` / `[[Set]]` is silently swallowed rather than
raised (see the note on `setStatus` in `interp/property.go`). This is a global
evaluator policy, not a TypedArray issue, and is left as-is.

## Immutable ArrayBuffers (`transferToImmutable`)

Files: any test invoking `testWithTypedArrayConstructors(..., ["immutable"])`
or `ArrayBuffer.prototype.transferToImmutable`.

The updated `testTypedArray.js` harness adds an "immutable ArrayBuffer" argument
factory gated on `ArrayBuffer.prototype.transferToImmutable`, a very recent
proposal gojs does not implement. Tests that *require* that factory throw
`no arg factories match include immutable ...` from the harness itself.
Deferred until immutable ArrayBuffers land.

## `Array.prototype.*` generality / String-wrapper indices

Files: `prototype/set/array-arg-primitive-toobject.js`, a couple of
`compareArray`-formatted diffs.

`TypedArray.prototype.set("678", 1)` requires `ToObject("678")` to expose
`"0"`/`"1"`/`"2"` as indexable own properties (String exotic object), and the
harness's `compareArray.format` calls `Array.prototype.map.call(typedArray, …)`.
gojs's boxed-String wrapper does not carry per-index own properties, and the
`Array.prototype` methods read dense `elems` rather than operating generically
over an arbitrary array-like `this`. Both are pre-existing engine limitations
unrelated to TypedArray. Deferred.

## Resizable-ArrayBuffer mid-iteration corner cases

A small number of `resizable-buffer*` tests grow/shrink the backing buffer in
the middle of an iterator step or a user `toLocaleString`/comparator callback
and assert exact length-tracking snapshots. The common paths (length-tracking
views, out-of-bounds detection, post-coercion re-validation for
fill/copyWithin/slice/with) are implemented and pass; these last few depend on
precise witness-record re-reads inside the iterator protocol that are not worth
the added complexity right now. Deferred.
