# Running TypeScript

gojs runs TypeScript with **no external tooling**. The
[`ts`](../../ts) package transpiles TypeScript to JavaScript in-process — it
embeds a hoisting of Microsoft's [typescript-go](https://github.com/microsoft/typescript-go)
compiler — and hands the result to the VM.

```bash
go run ./examples/typescript
```

Expected output:

```
a: distance 5
b: distance 10
c: distance 1.4142135623730951
nearest to origin: c
```

The same two `.ts` files also run through the CLI:

```bash
go run ./cmd/gojs run ./examples/typescript/main.ts
```

Key points:

- **`ts.Provider(base)`** wraps any `ModuleProvider` so that `.ts`/`.tsx`
  modules are transpiled to JavaScript on load. Everything else about module
  loading — how a specifier resolves, where source comes from — stays with the
  base provider (here an in-memory map; use `NewDirModuleProvider` for a
  directory, or your own provider for a database, archive, or network). The
  provider *is* the file-inclusion hook.
- **`import "./geometry"`** resolves through the provider; the `.ts` extension
  is retried automatically, so extensionless TypeScript imports work.
- Transpilation is **checker-free** (the `isolatedModules` model): type
  annotations, `interface`s, generics, and class visibility modifiers are
  erased/lowered, but the program is **not type-checked** — type errors are
  ignored. The goal is to *run* TypeScript, not validate it.
- For a self-contained script (no imports), `ts.RunString(vm, name, src)`
  transpiles and runs in one call.

> `enum`, `const enum`, and `namespace` lower and run. Not yet wired: error
> stack traces report transpiled-JavaScript positions rather than original `.ts`
> lines (source-map support is planned).
