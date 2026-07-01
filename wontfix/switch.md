# Switch — intentional divergences / out-of-scope items

These are known, deliberate gaps left after driving
`language/statements/switch` to 100% of runnable Test262 (213 pass, 0 fail,
3 skip). None are exercised by the switch corpus; each is noted here rather than
worked around.

## 1. Annex B.3.5 sloppy function-in-case hoisting

In sloppy mode, a function declaration inside a switch case is (per Annex B.3.5)
supposed to *also* create a function-scoped `var` binding, so the name is
visible after the switch:

```js
function f() { switch (1) { case 1: function g() { return 9; } } return typeof g; }
f(); // spec (sloppy): "function"
```

gojs treats a function declaration in a case as purely lexical (block-scoped to
the CaseBlock), so `typeof g` is `"undefined"` here. This matches strict-mode
semantics and keeps the one-shared-CaseBlock-scope model simple. No Test262
switch test covers this Annex B nuance, and the primary early-error requirement
(a duplicate `function` name across cases is a SyntaxError) is fully enforced.

## 2. Completion-value emptiness of plain (non-switch) StatementLists

The switch code path (`execCaseBody` in `interp/eval_tryswitch.go`) implements
StatementList completion + `UpdateEmpty` precisely: declarations, empty
statements, and `break`/`continue` are empty completions that never overwrite a
prior non-empty value. This makes every switch completion-value case correct
(the `cptn-*` Test262 files all pass).

The general-purpose `execStmts` path still uses the pre-existing REPL model in
which a bare declaration collapses the running value to `undefined`:

```js
eval('5; var x;'); // gojs: undefined   spec: 5
```

Making `execStmts` fully spec-accurate for empty completions is a cross-cutting
change to every block/program/function body and is out of scope for the switch
work. It is not needed for any switch test, and loop completion values (which
*are* needed, e.g. `do { switch … continue } while(false)`) are handled
correctly.
