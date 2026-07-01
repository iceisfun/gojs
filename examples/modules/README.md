# Intercepting require() with a ModuleProvider

A host controls how scripts pull in other scripts by implementing
`ModuleProvider`. Without one, `require` is unavailable — a script cannot load
code the host has not sanctioned. This is the JavaScript analogue of golua's
code provider.

```bash
go run ./examples/modules
```

Expected output:

```
start distance: 5
after move: 6 4 -> 7.211102550927978
cached module identity: true
```

Key points:

- `WithModuleProvider(p)` enables CommonJS-style `require(specifier)`. Each
  module runs in its own scope with `module`, `exports`, `require`,
  `__filename`, and `__dirname`.
- A module's `require` resolves relative specifiers (`./lib/vec.js`) against
  that module — the provider's `Resolve(specifier, referrer)` does the mapping.
- Modules are cached by canonical id: a module's body runs once and the same
  `exports` is returned on subsequent `require`s (circular deps see a partial
  `exports` rather than looping).
- `NewMapModuleProvider` serves from an in-memory map — ideal when the host
  already holds scripts in memory (game data files, bundled assets). Implement
  the two-method `ModuleProvider` interface yourself to serve from a pak
  archive, database, or network. `NewDirModuleProvider(dir)` serves from a
  directory, confined to that root.
