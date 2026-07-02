# Wontfix / deferred — `language/literals` Test262

Current status: `language/literals` = 1036 pass / 1 fail (99.9% of runnable);
the `language/literals/regexp` subtree is 476 pass / 0 fail (100%). gojs now
executes and validates regular expressions with the pure-Go `jsregexp` engine by
default — RE2 is opt-in only via `interp.WithRegExpEngine(interp.RegExpRE2)` — so
the former RE2-based divergences no longer apply. Regexp literals are validated
against the full ECMAScript RegExp grammar at parse time (via `jsregexp.Parse`),
which resolved the entire parse-time early-error backlog: inline modifiers
`(?ims-ims:...)`, Unicode-mode pattern restrictions, bad/duplicate flags,
malformed quantifiers, and out-of-range `\u{...}` are all reported correctly, and
Annex-B-legal non-Unicode forms are still accepted.

Only one deferred item remains.

## numeric — strict-mode octal via `eval` from strict code (1 file)

File: `numeric/7.8.3-3gs.js` — `eval("a = 0x1;a = 01;")` invoked from strict
code must throw `SyntaxError`, because the eval'd source inherits the caller's
strict mode. The literal-level strict-octal early error is implemented, and the
parser already accepts a strict flag (`parser.EvalContext.Strict`, honored by
`ParseEval`), but `interp.directEval` does not yet compute and pass the caller's
strict-mode state. This needs runtime strict-context tracking threaded into
direct eval; it is a direct-eval subsystem gap, not a literals issue. Deferred.

## Note on astral / surrogate-pair matching under `u`

The `u-astral*` and `u-surrogate-pairs*` regexp-literal tests now pass: Unicode
mode advances a whole code point across a surrogate pair (`jsregexp/vm.go`), and
a string literal written as a surrogate-pair escape is preserved as its astral
code point (`lexer/lexer_literal.go`). A separate, deeper divergence still exists
where a test observes UTF-16 *code-unit* semantics directly — e.g. an exec
result's `match[0].length` for an astral match (`built-ins/RegExp/prototype/exec/
u-captured-value.js`) — because the interpreter represents strings as code
points rather than UTF-16 code units. That is tracked as the UTF-16 string
representation overhaul (see NOTES-divergences.md), not here.
