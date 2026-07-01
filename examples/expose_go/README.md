# Exposing Go to JavaScript

Install Go-backed functions and structured host data as globals, then call them
from a script.

```bash
go run ./examples/expose_go
```

Expected output (the `env` line depends on your environment):

```
HELLO!
host.name = gojs
sum = 6
caught: RangeError: expected a positive number
ok: 42
```

Key points:

- `vm.NewFunction(name, fn)` wraps a Go `HostFunc` (`func([]Value) (Value, error)`)
  as a callable JavaScript value.
- Returning an error from a `HostFunc` throws it into the script. Wrap a value
  with `gojs.NewThrow(...)` to throw a specific JS object (e.g. a `RangeError`).
- `vm.FromGo` converts Go values (maps, slices, scalars) into JavaScript values;
  `vm.ToGo` and `vm.ToString` convert the other way.
