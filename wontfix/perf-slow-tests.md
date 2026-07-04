# Performance: disabled slow Test262 suites

Some Test262 suites are **disabled by default** in the mining harness — not
because gojs gets them wrong, but because their wall-clock is dominated by the
tree-walking interpreter processing very large strings, which made a general
conformance pass take ~25 minutes on a single domain and stalled iteration on
everything else.

They are listed in `slowSkip` in `tests/test262/test262_test.go` and excluded by
default. Run them with `GOJS_T262_SLOW=1`, or add ad-hoc exclusions with
`GOJS_T262_SKIP=<substr,substr>`.

## Currently disabled

- `built-ins/RegExp/property-escapes/generated/` (469 files)

  Each generated file builds a string containing *every* code point of a Unicode
  property (e.g. `\p{Alphabetic}` ≈ 130k code points) via
  `String.fromCodePoint.apply(null, hugeArray)` and then tests a regex against it
  (and, on any mismatch, loops over every code point individually). With the
  cons-string (rope) fix the string building is no longer O(n²), and with the
  Unicode 17.0 property tables the *results are correct* — but building and
  matching 130k-element arrays across 469 files in a tree-walker still costs
  ~25 min. The fast top-level `property-escapes/*` validation tests (`\p{…}`
  syntax / unknown-property SyntaxErrors) still run and pass.

## When this can be revisited

This is purely a runtime-speed limit. It becomes unnecessary if/when the
interpreter gets materially faster on large-array/large-string workloads (e.g. a
bytecode VM, or fast paths for `String.fromCodePoint` spread and `for…of` over
large strings). At that point, drop the entry from `slowSkip` and the suite
should pass at full speed. The conformance data is already correct today — this
is a "don't wait 25 minutes for it" switch, not a "we can't pass it" one.
