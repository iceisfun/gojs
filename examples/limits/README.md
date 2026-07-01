# Resource limits

`Limits` bound how much CPU and stack a script may consume, so a host can run
untrusted or buggy code without a crash or an unbounded loop.

```bash
go run ./examples/limits
```

Expected output:

```
normal             completed
deep recursion     completed
busy loop          stopped: execution step limit exceeded
host still in control after all three
```

Key points:

- `MaxCallDepth` caps nested calls. Exceeding it raises a **catchable**
  `RangeError` ("Maximum call stack size exceeded") — a normal JS exception the
  script can catch and recover from (so the "deep recursion" case *completes*).
  It also keeps recursion below the point where the Go stack would overflow.
- `MaxSteps` caps evaluation steps (statements, loop iterations, calls).
  Exceeding it aborts with an **uncatchable** `*LimitError`: `try/catch` cannot
  swallow it, and control returns to the host. This guarantees a busy loop like
  `while (true) {}` terminates even without a context deadline.
- Both default sensibly (`MaxCallDepth` 6000, `MaxSteps` unlimited). Combine
  with `WithContext(ctx)` (a wall-clock deadline) and `WithSecurity(...)` for a
  fully locked-down sandbox — see the `sandbox` example.
