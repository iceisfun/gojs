# Choosing the RegExp engine

gojs ships **two** regular-expression backends and lets the host select one per
VM with `WithRegExpEngine`:

| Engine | Option | Conformance | Speed | Backrefs / lookaround | ReDoS safety |
|---|---|---|---|---|---|
| **Compat** (default) | `gojs.RegExpCompat` | Full ECMAScript | Backtracking | ✅ supported | Step budget bounds it |
| **RE2** (opt-in) | `gojs.RegExpRE2` | **Not** conformant | Linear-time | ❌ compile error | Linear by construction |

```go
// Default: the ECMAScript-conformant engine. Nothing to configure.
vm := gojs.New()

// Opt in to the faster RE2 engine.
vm := gojs.New(gojs.WithRegExpEngine(gojs.RegExpRE2))
```

## When to use which

**Use `RegExpCompat` (the default) for anything real or untrusted.** It is a
complete ECMAScript regex implementation — backreferences, lookahead/lookbehind,
named groups, and `u`/`v` Unicode modes — and it bounds catastrophic
backtracking with a step budget wired to the VM's `context.Context`, so a
malicious pattern fails with a `RangeError` instead of hanging the host.

**Opt into `RegExpRE2` only for performance over simple, trusted patterns.** It
is backed by Go's `regexp` (RE2): linear-time and fast, but deliberately *not*
ECMAScript-conformant:

- Patterns using **backreferences** (`\1`, `\k<name>`) or **lookaround**
  (`(?=)`, `(?!)`, `(?<=)`, `(?<!)`) throw a `SyntaxError` at construction — RE2
  cannot express them.
- Capture-priority, flag, and Unicode semantics follow **RE2**, not the spec, so
  some matches differ from a conformant engine.
- It is a good fit for a plugin/config/scripting host that only runs simple
  patterns and wants RE2's speed and linear-time guarantee — *not* for running a
  pre-existing JavaScript codebase that expects standard regex behavior.

## Run it

```
go run ./examples/regexp_engine
```

```
== compat (default) ==
simple  /[0-9]+/           => 42
backref /(['"]).*?\1/      => "hi"

== re2 (opt-in) ==
simple  /[0-9]+/           => 42
backref /(['"]).*?\1/      => uncaught SyntaxError: Invalid regular expression: invalid escape sequence: `\1`
```

The default engine matches the backreference; the RE2 engine rejects the same
pattern because RE2 has no backreferences — exactly the trade-off you opt into.
