# Hardened sandbox

The posture for running untrusted code: no output capability, dynamic code
evaluation disabled, prototype mutation disabled, and a wall-clock timeout that
interrupts runaway loops.

```bash
go run ./examples/sandbox
```

Expected output:

```
compute -> completed
eval -> threw EvalError: eval is disabled in this sandbox
Function -> threw EvalError: Function constructor is disabled in this sandbox
runaway -> context deadline exceeded
```

Key points:

- `gojs.New()` with **no providers** cannot print, read the clock, or schedule
  timers — the default sandbox is closed.
- `WithSecurity(Security{...})` disables language escape hatches: `DisableEval`,
  `DisableFunctionCtor`, `DisableProtoMutation`, `StrictModulesOnly`.
- `WithContext(ctx)` with a timeout bounds execution: the interpreter checks the
  context between statements and loop iterations, so a `while (true) {}` is
  interrupted rather than hanging.
- `Close()` cancels the context and drains any spawned goroutines (e.g. from
  generators or timers).
