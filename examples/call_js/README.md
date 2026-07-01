# Calling JavaScript from Go

Load a script that defines functions, then drive it from Go: fetch a function
global, call it with converted arguments, and read the result back.

```bash
go run ./examples/call_js
```

Expected output:

```
Hello, world!
stats: map[count:4 mean:5 total:20]
```

Key points:

- `vm.GetGlobal(name)` fetches a value (e.g. a function) defined by the script.
- `vm.Call(fn, this, args...)` invokes a callable JS value from Go. Pass
  `gojs.Undefined` for `this` unless calling a method.
- Convert across the boundary with `vm.FromGo` (Go → JS) and `vm.ToGo` /
  `vm.ToString` (JS → Go).
