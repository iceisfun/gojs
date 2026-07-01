# Block scope — intentional divergences (out of scope for this pass)

This pass focused on ECMAScript **early errors** (phase: parse) for block scope:
lexical redeclaration in blocks/switch, and `FunctionDeclaration`/lexical
declarations appearing in single-statement positions. See
`parser/early_errors.go`. The `language/block-scope` Test262 slice is now at
100% (287/287).

The following are runtime-semantics gaps discovered while writing the harness
suite (`tests/harness/block_scope_test.go`). They are unrelated to the
block-scope early-error work and are documented here rather than fixed, to keep
the change focused and low-risk.

## 1. `eval` uses indirect (global-scope) semantics

`interp` implements `eval` as an **indirect eval**: the argument is parsed and
run as a program in the global environment. Consequences:

- A direct `eval("x")` does **not** resolve `x` against the caller's lexical
  environment (e.g. it cannot read a `let` in the enclosing block). Per spec, a
  *direct* eval should evaluate in the caller's variable/lexical environment.
- A top-level `let`/`const`/`class` declared **directly** in the eval'd source
  leaks into the global scope instead of a dedicated per-eval lexical
  environment. (`var` leaking to the global object is correct for sloppy eval.)

Block scoping *within* the eval'd program is correct: `eval("{ let y = 1; } typeof y")`
yields `"undefined"`, and lexical redeclaration inside eval'd source is still a
`SyntaxError`. Only the *boundary* between the eval'd program and its caller is
non-conforming. Fixing this requires threading the caller's environment into a
direct-eval code path and giving eval its own lexical environment record — a
change to the `eval` builtin and call machinery, out of scope for block-scope
early errors.

## 2. Annex B legacy runtime hoisting of block-level function declarations

The early-error rules for block-level function declarations are implemented
(e.g. a block function whose name collides with a `let`/`var` is a
`SyntaxError`, and duplicate plain function declarations in one block are legal
in sloppy code but not strict). The separate **Annex B.3.3 legacy runtime
semantics** — whereby a sloppy-mode block-level `function f(){}` also creates
and updates a `var`-scoped `f` in the enclosing function environment — is a
runtime hoisting behavior, not an early error, and was not part of this pass.
No `language/block-scope` Test262 case depends on it.
