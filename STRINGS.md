# gojs — string representation

A JavaScript string is a sequence of UTF-16 code units. gojs stores strings as
**WTF-8 bytes** (standard UTF-8 for scalar values; a lone surrogate as its 3-byte
`0xED …` encoding — `interp/utf16.go`) and presents the UTF-16 code-unit view the
spec requires on top of those bytes.

Most strings are the bare value type `String` (`type String string`,
`interp/value.go:62`) — no indirection, usable directly as a property-map key.
But a string that is *built* lazily, *carries* cached metadata, *shares* a
parent's bytes, or *borrows* host bytes takes a richer representation: the
pointer type **`*vmString`** (`interp/vmstring.go`). Both are string primitives —
`Typeof` reports `"string"`, `isStringish` accepts either — and every
string-consuming abstract operation flattens a `*vmString` to a `String` at the
boundary.

The design rule: **a string's VALUE is immutable, but its REPRESENTATION may be
refined.** Flattening a cons, caching a length, materializing a code-unit view —
none change the value denoted, so every cached field is safe to populate lazily.
The interpreter runs each string on a single goroutine (a SharedArrayBuffer
shares *bytes* between agents, never string objects), so the caches need no locks.

---

## 1. Why a rich type at all

Two idiomatic patterns were **O(n²)** while strings were only ever a flat Go
string, and neither is fixable without somewhere to cache:

| Pattern | Cost before | Cause | After |
|---|---|---|---|
| `arr.join(sep)` | 400k elems → ~240 s | result built with `out += …` (whole accumulator copied each step) | `[]byte` builder → 26 ms |
| `for (i<s.length)` | 200k chars → 7.4 s | `s.length` re-scans all bytes (`codeUnitLen`) every read | cached length → 67 ms |
| `for (i…) s.charCodeAt(i)` | 2.4 MB → non-terminating | `charCodeAt` rebuilds the code-unit view (`viewOf`) every call | cached view → ~0.8 s (linear) |
| `s += chunk` (accumulate) | already O(1)/step | cons node from `+` | unchanged |

The first is a single-implementation bug (join re-implemented string-building and
it rotted). The last three need a place to remember length / ASCII-ness / the
code-unit view *across calls* — which a value type cannot provide. That place is
`*vmString`.

---

## 2. The representations (`strKind`)

A `*vmString` is a **kind-tagged struct**, not a Go interface — an interface
method is a non-inlinable dispatched call, and strings are on the hottest paths
(`===`, property keys, `.length`). The tag lets the flat/common arm inline.

| kind | produced by | payload | flatten |
|---|---|---|---|
| `strCons` | `+`, and any lazy concat | `left, right Value` (each `String` or `*vmString`) | walk the tree (iteratively) once, memoize |
| `strFlat` | `newComputedString` for a materialized result ≥ 64 bytes | `flat string` + cached metadata | return `flat` |
| `strSlice` | `newSliceString` (a large substring window) | shares a parent string's backing, or a detached copy | return the span |
| `strExternal` | `NewBorrowedString` (host `[]byte`, zero-copy) | borrowed `flat string` | return `flat` |

`build()` (`interp/vmstring.go`) flattens any kind to its Go string and
**memoizes** the result into `flat` (setting `fFlattened`), then releases the cons
tree / parent so it can be collected. Because the value is immutable the memo can
never be stale. Cons flattening uses an explicit stack, so an
`((a+b)+c)+…`-deep chain — the shape `s += chunk` produces — cannot overflow the
Go stack.

### Metadata cache

`strFlat`/`strExternal`/`strSlice` and a flattened `strCons` carry, computed once
and cached on first observation:

- `length` — flattened **byte** length (always known; keeps concat O(1)).
- `utf16Len` — the ECMAScript `.length`; `-1` until first read
  (`codeUnitLen()`), then O(1).
- `fASCII` / `fASCIIKnown` — pure-ASCII flag; an ASCII string needs no code-unit
  materialization (byte index == code-unit index) and has `utf16Len == length`.
- `units []uint16` — the code-unit view of a *non-ASCII* string, built once on
  first indexing (`view()`) so a `charCodeAt` walk is O(1)/step instead of
  re-materializing the slice each call. Never allocated for an ASCII string.
- `hash` — reserved for a future cached property-key hash (§5).

### Where the cache is consulted

- `getProperty` (`interp/eval_call.go`) reads `.length` and `s[i]` from the
  `*vmString` cache **before** flattening, so a hot loop is O(1) per read.
- `charAt`/`charCodeAt`/`codePointAt`/`at` receive the string-primitive receiver
  (via `stringReceiver`, not a pre-flattened Go string) and reuse the cached
  view (`viewOfValue`).
- Producers `Array.prototype.join`, `TypedArray.prototype.join`,
  `String.prototype.concat`, and template literals (tree-walker **and** the
  bytecode `opTemplate`) return `newComputedString`, so their large results carry
  the cache from birth. Small results (< 64 bytes) stay a bare `String` — boxing
  would only add an allocation, and a short string's length scan is free.

---

## 3. The `+` operator: cons strings

`a + b` on two strings builds a `strCons` node in O(1) (`concatStrings`) rather
than copying — `s += chunk` in a loop is O(1) per step and one flatten at the end,
not O(n²). A string operand stays lazy through `evalAdd`
(`interp/eval_ops.go`): it is its own ToPrimitive, so the node is never flattened
mid-accumulation. Concatenation may abut a high and a low surrogate across the
join, so flattening canonicalizes WTF-8 (`canonicalizeWTF8`) to keep byte
equality == code-unit equality.

**Rejected: eager / background flatten.** Flattening a cons chain "when it gets
deep" was considered and rejected: it *reintroduces* O(n²) for the accumulation
pattern (flattening every *D* appends makes total work O(n²/D)), whereas
flatten-once-at-first-observation + memoize is O(n) for the whole accumulation.
Deep ropes need no eager collapse — `build()` is iterative (no overflow) and
flattening later uses the same bytes. Offloading the flatten to a goroutine is
also rejected: it would race the shared metadata cache (forcing locks on the
hottest path) to move a memcpy the main goroutine usually needs *now* (it
flattened *because* it needs the bytes). The lazy + memoized design already
serves both accumulate-then-use and accumulate-while-observing optimally.

---

## 4. Host strings and the "dangerous primitive"

Go strings are **immutable** — there is no write path, copying one copies only the
2-word header, and slicing shares the backing safely because nothing can mutate
it. A Go `[]byte` is **mutable** and its backing is not copy-on-write. That
asymmetry defines the one hazard.

- **`NewReadOnlyString(b []byte)`** — the **safe default**. It *copies* `b`
  (`string(b)`), so the host may reuse or mutate `b` afterward. Result is a
  metadata-bearing flat string.
- **`NewBorrowedString(b []byte)`** — the explicit **unsafe** zero-copy path. It
  borrows `b`'s backing via `unsafe.String`, for a large buffer whose lifetime
  the host controls. The caller must not mutate or free `b` while the string (or
  anything derived from it) is reachable, and `b` must be valid UTF-8.

A borrowed external string is the only *dangerous primitive* that can enter the
VM: because the engine never writes to a string, reads are always safe; the
hazard is solely the host mutating the shared backing. It is dangerous precisely
when it **escapes into durable state** — a property key, a `Map`/`Set` key, an
object slot — because a Go map stores the string header and does **not** copy its
bytes, so the durable reference would alias the mutable buffer. The safe default
copies at the door so this cannot happen; the borrow variant makes escape safety
the host's documented contract. (A future refinement can taint borrowed strings
and copy-on-escape at the durable sinks; today the copy-at-construction default
covers correctness.)

`newSliceString` (a large substring) shares its parent string's immutable
backing — no hazard, only a memory-retention concern, so a *small* window into a
*large* parent is copied (`strings.Clone`) rather than pinning the parent alive.

---

## 5. Not yet done

- **Property-key hashing.** `props map[PropertyKey]*Property` re-hashes the key's
  bytes on every access, and `Map`/`Set` currently *linear-scan* their entries
  (`interp/builtin_collections.go`). A cached `hash` on `*vmString` (field
  reserved) plus a hashed collection would make both O(1). Independent of the
  work above, unblocked by it.
- **Non-ASCII index cost.** A bare (unboxed) `String` read by `charCodeAt` in a
  loop still rescans; only `*vmString` receivers cache the view. Producers box
  large results, so this bites only large *literals* indexed in a loop — rare.
- **`slice`/`substring` boxing.** These already zero-copy an ASCII substring via
  Go slicing; routing them through `newSliceString`/`newComputedString` for a
  cached length is a small future win.

---

## 6. Tests

- `interp/vmstring_test.go` — each kind's value/`Typeof`/byte-length/UTF-16
  length; flatten memoization + tree release; upconversion chains
  (flat → cons → flat with an astral char; slice-of-cons); cached-view identity;
  and **indistinguishability through the engine** — every kind compared for `===`,
  used as a property key, indexed, length-read, and concatenated must match a
  plain literal.
- `interp/rope_test.go` — cons-is-a-string-primitive, the host-boundary
  (`ToGo`) flatten, and `join` correctness including surrogate coalescing.
- `interp/vmstring_bench_test.go` — `join` / length-loop / `charCodeAt` /
  concat-chain regression benchmarks.
- Full **Test262** is the correctness gate (`./run-test262.sh`); the string work
  lands with zero conformance change.
