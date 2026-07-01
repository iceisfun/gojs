# Wontfix / deferred — `built-ins/{decodeURI,decodeURIComponent,encodeURI,encodeURIComponent}` Test262

`decodeURI`/`decodeURIComponent` are now implemented (ECMA-262 §19.2.6 Decode)
and `encodeURI`/`encodeURIComponent` were already present; the four functions
now pass ~96%/74% of their runnable Test262. The handful of remaining failures
fall into two buckets that cannot be fixed without changes that are out of scope
(a UTF-16 string overhaul) or that would contradict the current spec.

## Lone / paired UTF-16 surrogates (the encode cluster)

Files: `encodeURI/S15.1.3.3_A1.1_T*`, `A1.2_T*`, `A1.3_T*`, `A2.4_T*`
(and the identical `encodeURIComponent/S15.1.3.4_*`), plus
`decodeURI/S15.1.3.1_A2.5_T1` and `decodeURIComponent/S15.1.3.2_A2.5_T1`.

These tests construct strings from raw UTF-16 code units with
`String.fromCharCode(0xD800)` etc. and require:

* `encodeURI`/`encodeURIComponent` to throw a **URIError** on an unpaired
  surrogate, and to emit 4-byte UTF-8 for a valid surrogate **pair**; and
* `decodeURI("%F0%90%80%80")` to equal `String.fromCharCode(0xD800, 0xDC00)`
  (a two-code-unit surrogate pair).

gojs represents JS strings as Go (UTF-8, code-point-indexed) strings, not
UTF-16 code-unit arrays — `"𐀀".length === 1`, and `String.fromCharCode(0xD800)`
yields U+FFFD because Go's `WriteRune` cannot encode a lone surrogate. So the
engine cannot *represent* an unpaired surrogate to reject it, nor produce a
surrogate-pair string to compare against. Astral code points do round-trip
correctly as single code points (`decodeURIComponent("%F0%90%80%80")` yields the
U+10000 character), they simply don't compare equal to a `fromCharCode(H, L)`
pair. Fixing this requires reworking the string type to UTF-16 semantics
throughout (length, indexing, charCodeAt, iteration, …) — a large cross-cutting
change tracked separately, not a URI-function bug. `A2.5` additionally times out
(a ~1.3M-iteration exhaustive sweep over the astral plane).

## Outdated ES5.1 "length is DontDelete" tests

Files: `*/S15.1.3.{1,2,3,4}_A5.2.js`.

Each does `delete fn.length; if (fn.length === undefined) throw`. Under
ECMA-262 §20.2.4 a function's `length` is `configurable: true`, so the delete
succeeds and `length` becomes `undefined` — these pre-ES2015 tests fail on any
compliant engine (they assume the old `{DontDelete}` attribute) and are in the
official Test262 excludelist. Our runner does not consult the excludelist, so
they show up as failures. The modern equivalents (`name.js`, `prop-desc.js`,
`A5.3` with `verifyNotWritable`) all pass.
