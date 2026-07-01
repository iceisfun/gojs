# Wontfix / deferred — `language/literals` Test262

These remaining `language/literals` failures are left unaddressed because a
correct fix would require either a full ECMAScript RegExp engine/grammar (gojs
executes patterns with Go's RE2, which has different semantics by design) or
features gojs does not implement. They are documented here so the remaining
fail count is understood, not mysterious.

## regexp — inline modifiers `(?ims-ims:...)` (largest remaining cluster)

Files: `early-err-modifiers-*`, `early-err-arithmetic-modifiers-*`
(~150 test results).

These are negative tests for the ES2023 *RegExp modifiers* proposal
(`(?i:...)`, `(?ims-ms:...)`). gojs does not implement inline modifiers at all,
and RE2 uses a different inline-flag syntax. Validating the invalid forms as
early errors would first require implementing the modifiers grammar. Deferred.

## regexp — unicode-mode (`u` / `v`) pattern restrictions

Files: `u-invalid-*`, `u-unicode-esc-*`, `u-surrogate-pairs-*`,
`unicode-escape-nls-err`, `invalid-identity-escape-in-capture-u`, etc.

In Unicode mode many constructs that are legal in Annex-B (non-unicode) mode
become early errors: identity escapes of non-syntax characters (`/\a/u`),
legacy octal escapes, non-empty class ranges with the wrong bounds, out-of-range
`\u{...}`, lone/paired surrogate handling, etc. Correctly validating these
requires an ECMAScript-specific Unicode-mode pattern parser. gojs additionally
uses rune-indexed UTF-8 strings and RE2, so astral-plane / surrogate semantics
are intentionally divergent (see NOTES-divergences.md). Deferred.

Note: the named-group subset of Unicode-mode errors *is* handled — a malformed
or dangling `\k<name>` is rejected under the `u`/`v` flag even without a declared
group (`parser/parse_regexp.go`).

## regexp — RE2 semantic divergences (backreferences, `\u` escape form)

Files: `named-groups/forward-reference.js` (positive: `/\k<a>(?<a>x)/`),
`S7.8.5_A1.1_T1`, `S7.8.5_A2.1_T1`, `S7.8.5_A1.4_T2`, `S7.8.5_A2.4_T2`.

RE2 does not support backreferences at all, so even a syntactically valid named
backreference cannot execute. RE2 also rejects some ECMAScript escape forms
(e.g. a bare `\` or `\u` sequences in certain positions). These are runtime
divergences inherent to the RE2 backend, not lexer/parser gaps. Deferred.

## numeric — strict-mode octal via `eval` from strict code

File: `numeric/7.8.3-3gs.js` — `eval("a = 01;")` invoked from `onlyStrict`
code must throw `SyntaxError`. This needs `eval` to propagate the caller's
strict-mode context into the parser (direct strict eval). The literal-level
strict octal early error is already implemented; only the eval-strictness
plumbing is missing. Out of scope for the literals work. Deferred.
