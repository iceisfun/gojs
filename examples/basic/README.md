# Basic execution

The minimal gojs embedding: create a VM, install a print provider so
`console.log` has somewhere to go, run a script, and read back the completion
value as a Go value.

```bash
go run ./examples/basic
```

Expected output:

```
factorial(10) = 3628800
Go sees: 3.6288e+06
```

Key points:

- `gojs.New()` with no options is a **closed sandbox** — `console.log` produces
  nothing until you install a `PrintProvider`.
- `RunString` returns the program's completion value (the value of its last
  expression statement).
- `vm.ToGo` converts a JavaScript value into an idiomatic Go value
  (numbers become `float64`).
