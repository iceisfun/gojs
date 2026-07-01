# for-in — remaining Test262 failures (deferred)

After the early-error and semantics work, `language/statements/for-in` sits at
179 pass / 19 fail (90.4%). The remaining 19 failures fall into these clusters,
deferred because each requires machinery outside the scope of for-in itself.

## 1. Per-iteration lexical bindings + TDZ during head evaluation (10 results)

Files: `head-const-bound-names-fordecl-tdz.js`, `head-let-bound-names-fordecl-tdz.js`,
`scope-head-lex-open.js`, `scope-head-lex-close.js`, `scope-body-lex-open.js`.

These exercise ForIn/OfHeadEvaluation §14.7.5.6: the bound names of a
`for (let/const … in …)` head must be created in a *temporal dead zone*
declarative environment while the right-hand side expression is evaluated, and
then a *fresh per-iteration* lexical environment is created for each iteration so
closures over the loop variable capture distinct bindings. gojs currently
evaluates the RHS in the enclosing scope and binds the loop variable into a
single per-iteration environment without a TDZ phase. Implementing this
correctly touches the shared head-evaluation/scope machinery (also used by
for-of and the C-style for loop) and is a larger, cross-cutting change.

## 2. Trailing elision after a rest element (4 results)

Files: `dstr/array-rest-before-elision.js` (`[...x, ,]`),
`dstr/array-rest-elision-invalid.js` (`[...x,]`).

`[...x,]` is a SyntaxError in a destructuring **pattern** but legal in an array
**literal**. gojs's `parseArrayLit` silently drops the trailing comma, so the
information needed to reject it is gone by the time the for-in head is validated
post-parse. Detecting this requires threading a "destructuring context" flag
into `parseArrayLit` (or recording trailing-comma provenance on the AST), which
would perturb the shared array-literal parser for one edge case.

## 3. `yield` in a generator-context for-in head (1 result)

File: `dstr/obj-id-identifier-yield-expr.js` — `(function*(){ for ({ yield } in [{}]) ; })`.

`yield` as an IdentifierReference is an early error inside a generator body. The
strict-mode variant of this rule is handled (`checkNoYieldInStrict`), but the
generator-context variant needs the parser to track whether it is inside a
generator (`p.inGenerator`), which is a general parser feature rather than a
for-in concern.

## 4. Object.defineProperty attribute preservation (2 results)

File: `order-after-define-property.js`.

Redefining an existing property with a partial descriptor (e.g. only `get`) must
preserve the previously-set `enumerable` attribute. gojs resets it, so the
property drops out of the for-in enumeration. This is an `Object.defineProperty`
attribute-merge bug in the shared property machinery, not a for-in bug.

## 5. TypedArray feature (2 results)

File: `resizable-buffer.js` — uses `Uint8Array` / resizable ArrayBuffers, which
gojs does not implement (the test is not tagged with a `TypedArray` feature the
runner skips on, so it surfaces as a failure).
