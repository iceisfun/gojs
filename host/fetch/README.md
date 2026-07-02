# host/fetch — WHATWG Fetch API for gojs

An **opt-in** Fetch API for the [gojs](../../) JavaScript engine. Nothing is
added to a VM unless the host calls `fetch.Install(vm)`. The engine has no
dependency on this package — only this package imports `gojs`.

```go
import (
    "github.com/iceisfun/gojs"
    "github.com/iceisfun/gojs/host/fetch"
)

vm := gojs.New(gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()))
if err := fetch.Install(vm); err != nil {
    log.Fatal(err)
}
_, err := vm.RunString("app.js", `
    (async () => {
        const r = await fetch("https://example.com/api", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ hi: "there" }),
        });
        console.log(r.status, await r.text());
    })();
`)
```

`RunString` runs the top level and then drains the event loop, so the async
fetches complete before it returns.

## What you get

Installed on `globalThis`:

- **`fetch(input, init?)`** returning a real `Promise`.
  - Methods: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS.
  - `init`: `method`, `headers`, `body`, `redirect`, `signal`.
  - Request bodies: `string`, `ArrayBuffer`, `Uint8Array`. A string body defaults
    the `Content-Type` to `text/plain;charset=UTF-8`.
- **`Response`**: `status`, `statusText`, `ok`, `headers`, `url`, `redirected`,
  `type`, `bodyUsed`, plus body accessors `.text()`, `.json()`,
  `.arrayBuffer()`, and `.bytes()` (→ `Uint8Array`). Bodies are single-use: a
  second read rejects with a `TypeError`. `Response.json(data, init?)` and
  `Response.error()` statics are provided.
- **`Headers`**: `append`, `set`, `get`, `has`, `delete`, `forEach`, `entries`,
  `keys`, `values`, and `[Symbol.iterator]`. Names are case-insensitive;
  multiple values combine into a comma-joined string; iteration is sorted and
  lowercased.
- **`Request`**: constructed from a URL string or another `Request`, with the
  same body accessors as `Response`.
- **`AbortController` / `AbortSignal`**: `controller.abort(reason?)`,
  `signal.aborted`, `signal.reason`, `throwIfAborted()`,
  `addEventListener("abort", …)`, plus `AbortSignal.abort()` and
  `AbortSignal.timeout(ms)`. Aborting an in-flight fetch rejects its promise with
  an `AbortError` (a `DOMException`-like value whose `name` is `"AbortError"`)
  and cancels the underlying HTTP request.
- **`DOMException`** (minimal): defined only if not already present.

## Behavior

- **Redirects**: `redirect: "follow"` (default) follows up to 20 redirects and
  sets `response.redirected` / `response.url` to the final hop. `"error"` rejects
  with a `TypeError`. `"manual"` returns the raw 3xx response (see gaps below).
- **Compression**: gzip/deflate response bodies are transparently decompressed
  by Go's HTTP transport.
- **HTTPS**: supported. Supply a custom client via `WithClient` to control TLS.
- **Errors** map to a rejected promise: invalid URL, unsupported scheme, DNS
  failure, connection refused, and TLS failure → `TypeError`; abort → the
  signal's reason (an `AbortError` by default).

## Configuring / gating the network

`Install` takes functional options. A plain `Install(vm)` uses a sensible
default `*http.Client`.

```go
// Supply your own client (proxy, TLS config, cookie jar, overall timeout).
fetch.Install(vm, fetch.WithClient(myClient))

// Vet or block every outgoing request on the VM goroutine.
fetch.Install(vm, fetch.WithAllowlist(func(r *http.Request) error {
    if r.URL.Hostname() != "api.internal" {
        return fmt.Errorf("host %q not allowed", r.URL.Hostname())
    }
    return nil
}))
```

`WithAllowlist`'s hook is called with the fully built `*http.Request` just before
it is sent; returning an error rejects the fetch with a `TypeError`.

## Implementation notes

- The web API is a JavaScript prelude (`fetch.js`, embedded via `go:embed`)
  layered over a few hidden native primitives under `globalThis.__gojs_fetch`:
  `send(...)` (the `net/http` round-trip) and a `utf8Encode`/`utf8Decode` codec.
  All WHATWG semantics live in the prelude; only the round-trip and the codec are
  in Go.
- **Threading.** `send` runs on the VM goroutine, copies the request body out
  there, then performs the blocking HTTP on a worker goroutine (which never
  touches VM state). It holds the event loop open with `VM.Pin` for the
  duration and delivers the result via `VM.Enqueue`, building the response value
  and settling the promise back on the VM goroutine.
- **Binary bridge.** This package relies on the engine's `Uint8Array` and the
  `VM.NewArrayBuffer` / `VM.NewUint8Array` / `VM.ArrayBufferBytes` /
  `VM.TypedArrayBytes` helpers.

## Browser differences and intentional gaps

- **Streaming bodies** are not implemented: a response body is fully buffered in
  memory before the promise resolves, and `response.body` (a `ReadableStream`) is
  absent.
- **`Blob`, `FormData`, `URLSearchParams`** are not implemented; a non-string,
  non-binary body is coerced with `String(body)`.
- **`redirect: "manual"`** returns the actual 3xx response (with its `Location`
  header readable), rather than a browser "opaqueredirect" response with status
  `0`. `"follow"` and `"error"` behave as in browsers.
- **CORS, credentials/cookies mode, `Request.cache`, `referrer`, `integrity`,
  and `keepalive`** are not modeled. Cookies work only if the supplied client has
  a `Jar`.
- **`AbortSignal.timeout`** requires a timer provider on the VM
  (`gojs.WithTimerProvider(...)`); without one it never fires.
- **UTF-8 decoding** (`.text()`, `.json()`) reinterprets the response bytes as
  UTF-8 without the U+FFFD replacement a browser `TextDecoder` performs on
  invalid sequences.
- The engine's `Uint8Array` is a minimal subset (indexing, `length`, iteration,
  `from`/`of`/`set`/`subarray`/`slice`, buffer aliasing); it is not the full
  `%TypedArray%` method surface.

## Tests

`fetch_test.go` is hermetic — it uses `net/http/httptest` (HTTP and TLS) and
loopback only, never the public internet. It covers every method, custom headers
echoed back, POST JSON round-trips, text/JSON/arrayBuffer/bytes reads, status and
`ok`, redirect follow/manual/error, transparent gzip, abort (already-aborted and
in-flight), body-consumption guards, multi-value response headers, the allowlist
hook, HTTPS via a custom client, and error mapping for bad URLs, unsupported
schemes, and refused connections. `Headers`, `Request`, and `Response`
constructors have their own unit tests.
