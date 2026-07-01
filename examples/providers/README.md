# Capability providers

gojs gates every host capability behind a provider. This example installs a
**custom** `PrintProvider` that captures and tags console output instead of
writing to stdout, plus the default timer and time providers so
`setTimeout`/`setInterval` and `Date` work.

```bash
go run ./examples/providers
```

Expected output (order reflects the event loop: sync code, then microtasks,
then timers by delay):

```
captured output:
  [log] starting
  [log] synchronous end
  [log] microtask: promised
  [log] tick 1
  [log] tick 2
  [log] tick 3
  [log] timeout fired
```

Key points:

- A `PrintProvider` receives **all** `console.*` output. Implement the
  interface (`Print`/`Warn`) to capture, redirect, filter, or silence it.
- With a `TimerProvider`, `setTimeout`/`setInterval`/`queueMicrotask` schedule
  callbacks onto the interpreter's single-threaded event loop.
- `RunString` drains that loop before returning, so all pending callbacks run.
- Omit a provider to disable the capability: no `PrintProvider` means console
  output is silently dropped; no `TimerProvider` means timers are unavailable.
