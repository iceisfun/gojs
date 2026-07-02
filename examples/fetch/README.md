# Fetch API example

A self-contained, offline demo of [`host/fetch`](../../host/fetch). `main.go`
starts a tiny HTTP server on a random loopback port, builds a VM with a print
provider and the Fetch API installed, hands the script the server's base URL, and
runs `main.js`.

Run it from the repository root:

```sh
go run ./examples/fetch
```

Expected output:

```
GET /hello -> 200 OK
  body: Hello from the local server!
GET /time -> service=demo version=1 tags=gojs,fetch
  content-type: application/json
POST /echo -> 200
  method seen by server: POST
  api key seen by server: secret-123
  echoed payload: {"hello":"world","items":[1,2,3]}
Headers demo -> accept: text/plain, application/json
Error handling -> caught TypeError: Failed to fetch: dial tcp 127.0.0.1:1: connect: connection refused
Abort demo -> caught AbortError
Done.
```

`main.js` demonstrates:

1. **GET + text** — `await fetch(...)` then `await res.text()`.
2. **GET + JSON** — `await res.json()` and reading a response header.
3. **POST JSON with custom headers** — a JSON body and an `X-Api-Key` header
   echoed back by the server.
4. **Headers** — case-insensitive, comma-combined values.
5. **Error handling** — a refused connection rejects with a `TypeError`.
6. **Abort** — an `AbortController` rejects the pending fetch with an
   `AbortError`.

Because the demo server runs on `127.0.0.1`, no network access is required.

## Walling off the network — `NetProvider`

Every dial made by `fetch` (and `sse`/`websocket`) — and therefore every DNS
lookup — passes through the VM's `NetProvider`, the single egress wall. This
example installs the pass-through default; wrap `DialContext` to enforce a
policy:

```go
// Allowlist: the script may only reach api.mygame.com.
type allowlist struct{ d net.Dialer }
func (a allowlist) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
    host, _, _ := net.SplitHostPort(addr)
    if host != "api.mygame.com" {
        return nil, fmt.Errorf("egress to %s denied", host)
    }
    return a.d.DialContext(ctx, network, addr)
}

vm := gojs.New(gojs.WithNetProvider(allowlist{}))
```

The same seam makes network tests hermetic: a `NetProvider` that dials every
address to a local `httptest.Server` lets a script request
`https://api.example.com` and actually reach your loopback server — no real DNS,
no outbound sockets (see `netprovider_test.go`).
