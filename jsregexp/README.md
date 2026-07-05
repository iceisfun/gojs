# jsregexp

A pure-Go **ECMAScript (JavaScript) regular-expression engine**. It parses a
pattern + flags into an AST, compiles that to a backtracking matcher, and
executes it with a **step budget** so untrusted patterns cannot hang the host.

Unlike Go's standard `regexp` (RE2), jsregexp implements the parts of the
ECMAScript grammar RE2 deliberately cannot: **backreferences**, **lookahead /
lookbehind**, **named groups**, and the **`u` / `v` Unicode modes** — with
matching defined over **UTF-16 code units**, exactly as JavaScript strings are.

It is the default RegExp engine inside [gojs](https://github.com/iceisfun/gojs),
but has no dependency on the interpreter and can be embedded on its own.

> **Status: pre-1.0, actively evolving.** The public API and the exported AST may
> change between commits. **Conformance is high** — measured through gojs, the
> `built-ins/RegExp` Test262 domain passes at ~100% of runnable — with a few known
> edge-case gaps listed in **[Limitations](#limitations-read-this)** below.
> Performance (a backtracking matcher, not a bytecode VM) is the main caveat.

## Install

```go
import "github.com/iceisfun/gojs/jsregexp"
```

Zero third-party dependencies; standard library only; no cgo.

## Quick start

```go
package main

import (
	"context"
	"fmt"

	"github.com/iceisfun/gojs/jsregexp"
)

func main() {
	ctx := context.Background()

	// Compile(pattern, flags). flags is the JS flag string ("gimsuvy d"), not Go's.
	re := jsregexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`, "")

	ok, _ := re.Match(ctx, "date: 2026-07-01")
	fmt.Println(ok) // true
}
```

### Extracting matches and capture groups

Offsets are **UTF-16 code-unit indices** (what JS uses for `.index`,
`lastIndex`, etc.). Slice the code-unit view to get substrings:

```go
input := "date: 2026-07-01"
units := jsregexp.ToUnits(input) // []uint16

loc, err := re.FindSubmatchIndex(ctx, units, 0)
// loc = [6 16  6 10  11 13  14 16]
//        ^whole  ^g1   ^g2    ^g3   (pairs of [start,end); -1/-1 if a group didn't match)
if err != nil {
	// err is ErrBudget (step budget hit) or a context error — see below.
}
if loc != nil {
	year := jsregexp.FromUnits(units[loc[2]:loc[3]]) // "2026"
	fmt.Println(year)
}
```

`FindStringSubmatchIndex(ctx, s, start)` is the same over a Go `string`.

### Named groups

```go
re := jsregexp.MustCompile(`(?<year>\d{4})-(?<month>\d{2})`, "")
units := jsregexp.ToUnits("2026-07")
loc, _ := re.FindSubmatchIndex(ctx, units, 0)

i := re.GroupNames()["year"]              // capture index for "year"
fmt.Println(jsregexp.FromUnits(units[loc[2*i]:loc[2*i+1]])) // "2026"
```

### Backreferences and lookaround (things RE2 can't do)

```go
// Balanced quotes via a backreference.
re := jsregexp.MustCompile(`(['"]).*?\1`, "")
loc, _ := re.FindStringSubmatchIndex(ctx, `say "hi" ok`, 0)
// matches "hi" (with quotes)

// Lookbehind.
re = jsregexp.MustCompile(`(?<=\$)\d+`, "")
loc, _ = re.FindStringSubmatchIndex(ctx, "$42", 0) // matches 42
```

### Global iteration (the engine is stateless)

Like Go's `regexp`, a `*Regexp` holds no `lastIndex` — you drive the position.
Advance past zero-width matches yourself (by one code point):

```go
units := jsregexp.ToUnits(input)
for pos := 0; pos <= len(units); {
	loc, err := re.FindSubmatchIndex(ctx, units, pos)
	if err != nil { /* handle ErrBudget / ctx */ break }
	if loc == nil { break }
	// ... use loc ...
	if loc[1] == loc[0] {
		pos = loc[1] + 1 // zero-width: step forward
	} else {
		pos = loc[1]
	}
}
```

### ReDoS safety: the step budget

Because this is a **backtracking** engine, a pathological pattern can require
exponential work. The engine bounds it and returns `ErrBudget` instead of
hanging:

```go
re := jsregexp.MustCompile(`(a+)+$`, "")
re.SetStepBudget(1_000_000) // default is jsregexp.DefaultStepBudget (10M)

_, err := re.Match(ctx, strings.Repeat("a", 40)+"!")
// err == jsregexp.ErrBudget
```

### Cancellation

Every match honors the `context.Context` (polled periodically), so a deadline or
cancel aborts a long search:

```go
ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()
_, err := re.Match(ctx, veryLargeInput)
// err == context.DeadlineExceeded (if it runs long)
```

## Flags

`Compile`/`MustCompile` take the JavaScript flag string. All are parsed and
exposed via `re.Flags()`:

| Flag | Meaning | Notes |
|---|---|---|
| `i` | ignore case | Unicode simple case folding (see limitations) |
| `m` | multiline `^`/`$` | |
| `s` | dotAll (`.` matches newlines) | |
| `u` | Unicode mode | code-point semantics, `\u{…}`, `\p{…}` |
| `v` | Unicode sets mode | set operations `&&` `--`, nested classes |
| `y` | sticky | anchors the match at `start` |
| `g` | global | **caller-managed** — the engine is stateless |
| `d` | has-indices | the engine returns offsets; building `.indices` is the caller's job |

## Public API

- `Compile(source, flags string) (*Regexp, error)` / `MustCompile(...) *Regexp`
- `(*Regexp).FindSubmatchIndex(ctx, []uint16, start) ([]int, error)`
- `(*Regexp).FindStringSubmatchIndex(ctx, string, start) ([]int, error)`
- `(*Regexp).Match(ctx, string) (bool, error)`
- `(*Regexp).Source() string`, `.Flags() Flags`, `.NumSubexp() int`, `.GroupNames() map[string]int`
- `(*Regexp).SetStepBudget(n int)` — `n <= 0` disables the budget (not recommended for untrusted input)
- `ToUnits(string) []uint16` / `FromUnits([]uint16) string`
- `ParseFlags(string) (Flags, error)`, `Flags{...}` with `.String()` / `.UnicodeMode()`
- `Parse(pattern, Flags) (*Pattern, error)` and the AST node types (`.AST()`) — for tooling; **not stable**
- `ErrBudget`, `DefaultStepBudget`, `SyntaxError{Msg, Pos}`

## Limitations (read this)

We would rather be honest than oversell. Known gaps, roughly by impact:

1. **Performance.** The matcher is a tree of continuation closures, not compiled
   bytecode/JIT. It runs on the order of a few million matcher-steps per second,
   so large inputs and heavy backtracking are **slow**. Performance is an
   explicit non-goal for now; a bytecode VM is future work.

2. **Not linear-time.** This is a genuine backtracking engine. ReDoS is mitigated
   by the **step budget** (bounded work → `ErrBudget`), *not* by a linear-time
   guarantee like RE2. A consequence: a legitimate-but-expensive match over a
   very large input can hit the budget and return `ErrBudget` instead of a
   result. Tune with `SetStepBudget`. If you need a hard linear-time guarantee
   over simple patterns, use Go's `regexp` instead.

3. **Lookbehind is emulated**, not a true right-to-left matcher: it scans
   candidate start positions. Match / no-match and simple captures are correct,
   but capture *values* inside a complex, backtracking lookbehind may differ from
   a spec engine.

4. **Case folding.** The `i` flag applies Unicode **simple** case folding: Go's
   `unicode.SimpleFold` orbits augmented with the `CaseFolding.txt` status-`S`
   pairs that orbit-folding misses (e.g. `ẞ`↔`ß`, the Greek/Latin ligature pairs),
   so it matches the spec's `Canonicalize` for the simple mappings. Full
   *multi-character* case folding (one code point folding to several) is not
   applied — the spec's regex `Canonicalize` is simple-fold only, so this is
   conformant for matching.

5. **`\p{}` property escapes** resolve against a generated **Unicode 17.0**
   Character Database: General_Category, binary properties, **Script**, and
   **Script_Extensions** (properly, not approximated by `Script`). Unsupported
   property names/values return a `SyntaxError` rather than silently mismatching.

6. **`v`-mode "properties of strings"** — `\p{RGI_Emoji}` and the emoji-sequence
   sets (`Basic_Emoji`, `Emoji_Keycap_Sequence`, `RGI_Emoji_Modifier_Sequence`,
   `RGI_Emoji_Flag_Sequence`, `RGI_Emoji_Tag_Sequence`, `RGI_Emoji_ZWJ_Sequence`),
   plus multi-code-point class strings (`\q{abc}`) and set operations (`&&`,
   `--`), are supported, over Unicode Emoji 17.0 data.

7. **Unicode input model.** Matching is over UTF-16 code units (correct), but the
   convenience helpers round-trip through Go (UTF-8) strings, so **lone
   surrogates** cannot survive `ToUnits`/`FromUnits`. Feed `[]uint16` directly if
   you must preserve them.

8. **Stateless / stateful-flag boundary.** `g`, `y`, and `d` are parsed and
   reported, but their *stateful* behavior (`lastIndex` advancement, the `.indices`
   array) is the caller's responsibility — the engine itself only reports offsets
   and honors `y` via the `start` argument.

9. **A few residual edge-case gaps.** Conformance is high (~100% of the runnable
   `built-ins/RegExp` Test262 domain via gojs), but a complex, backtracking
   **lookbehind** can produce wrong capture *values* (item 3), and there are minor
   Annex-B corners. Treat it as "very usable, close to a reference implementation,
   with lookbehind captures the notable caveat."

10. **API + AST are pre-1.0.** Signatures and the exported AST node shapes may
    change without notice.

## Conformance

jsregexp is exercised against the official **Test262** suite through gojs, and
the `built-ins/RegExp` domain passes at **~100% of runnable** (2816 pass, 0 fail
at last full run), as do the `RegExp` `Symbol.{match,matchAll,replace,search,
split}` protocols and the regex-literal parse tests in `language/literals`. The
residual gaps are the ones enumerated under [Limitations](#limitations-read-this)
— chiefly complex-lookbehind capture values. These numbers move as the engine
matures.

## License

Same as the parent [gojs](https://github.com/iceisfun/gojs) repository.
