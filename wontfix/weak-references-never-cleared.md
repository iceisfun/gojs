# Weak references are never cleared — intentional divergence

gojs runs on the Go runtime and exposes no garbage-collection finalization
hook, so the weak-collection family cannot observe when a referent becomes
unreachable. Every referent is therefore held **strongly** for the lifetime of
the holding object and is never reclaimed. This affects:

- `WeakMap` / `WeakSet` entries — kept forever; keys/values are never dropped.
- `WeakRef.prototype.deref()` — always returns the original target; it never
  returns `undefined` due to collection.
- `FinalizationRegistry` cleanup callbacks — **never fire**, because no target
  is ever noticed to have been reclaimed.

## Why this is spec-conforming

ECMA-262 explicitly permits an implementation that never reclaims:

- §9.10.3 (liveness) makes the set of live objects observed by the engine an
  implementation choice, and notes that "the notion of what is live is defined
  in terms of ... the implementation".
- §9.10.4.1 states cleanup callback timing is unpredictable and that an engine
  may choose **never** to call a `FinalizationRegistry` cleanup callback.
- The `WeakRef`/`FinalizationRegistry` design notes emphasize that reclamation
  is never guaranteed; correct programs must not depend on it.

A conforming program cannot distinguish "collection never happens" from
"collection has not happened yet". All observable, deterministic behavior —
argument validation (`CanBeHeldWeakly`, `target === heldValue`), the
registration/unregistration bookkeeping of `FinalizationRegistry`, property
descriptors, `Symbol.toStringTag`, and subclassing — is implemented precisely
(see `interp/builtin_collections.go` and `interp/builtin_weakref.go`).

The only Test262 cases in this family that still fail are those requiring the
`$262.createRealm` host hook (`proto-from-ctor-realm.js`), which the gojs
Test262 harness does not provide, and two `WeakMap` constructor tests
(`iterator-item-{first,second}-entry-returns-abrupt.js`) that fail for an
unrelated reason described below.

## Unrelated: array index access bypasses accessor descriptors

`WeakMap/iterator-item-{first,second}-entry-returns-abrupt.js` install a getter
on an array index (`Object.defineProperty(arr, 0, { get })`) and expect reading
`arr[0]` to invoke it. In gojs, indexed reads on a dense array return the backing
element without consulting an accessor property defined for that index, so the
getter never runs and its abrupt completion is not observed. This is a
pre-existing Array/property-descriptor limitation (it equally affects the `Map`
constructor), not a weak-collection issue, and is out of scope for this pass.
