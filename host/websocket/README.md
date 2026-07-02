# host/websocket

A browser-compatible **WebSocket client** for the
[gojs](https://github.com/iceisfun/gojs) JavaScript engine, implemented as an
optional host package. It adds a standard `WebSocket` global to a VM and speaks
[RFC 6455](https://www.rfc-editor.org/rfc/rfc6455) directly — no third-party
WebSocket library, standard library only.

```go
import "github.com/iceisfun/gojs/host/websocket"

vm := gojs.New(
    gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
    gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
)
defer vm.Close()

if err := websocket.Install(vm); err != nil {
    log.Fatal(err)
}

vm.RunString("app.js", `
    const ws = new WebSocket("ws://localhost:8080/chat", ["chat"]);
    ws.onopen    = () => ws.send("hello");
    ws.onmessage = (e) => console.log("recv", e.data);
    ws.onclose   = (e) => console.log("closed", e.code, e.reason, e.wasClean);
`)
```

See [`examples/websocket`](../../examples/websocket) for a complete, offline,
runnable client + echo server.

## Scripting API

`Install` defines `globalThis.WebSocket`, matching the WHATWG interface:

- **Constructor:** `new WebSocket(url[, protocols])` — `url` is `ws://` or
  `wss://`; `protocols` is a string or array of subprotocol names.
- **Constants** (static **and** instance): `CONNECTING=0`, `OPEN=1`,
  `CLOSING=2`, `CLOSED=3`.
- **Properties:** `url`, `protocol`, `extensions`, `readyState`,
  `bufferedAmount`, `binaryType`.
- **Handlers:** `onopen`, `onmessage`, `onerror`, `onclose`.
- **Events:** `addEventListener` / `removeEventListener` / `dispatchEvent` for
  the `open`, `message`, `error`, and `close` types. Event objects carry
  `type`/`target`; message events add `data`; close events add `code`, `reason`,
  `wasClean`; the error event adds `message`.
- **Methods:** `send(data)` (string → text frame; `ArrayBuffer`/`DataView` →
  binary frame) and `close([code[, reason]])`.

## Install options

```go
websocket.Install(vm,
    websocket.WithMaxFrameSize(1<<20),        // reject a larger single frame → close 1009
    websocket.WithMaxMessageSize(4<<20),      // reject a larger reassembled message → close 1009
    websocket.WithHandshakeTimeout(30*time.Second),
    websocket.WithCloseTimeout(5*time.Second),// wait for the peer's Close echo
    websocket.WithTLSConfig(&tls.Config{...}),// for wss://
    websocket.WithOrigin("https://example.com"),
)
```

| Option | Default | Effect |
| --- | --- | --- |
| `WithMaxFrameSize` | 16 MiB | Max payload of one incoming frame (0 = unlimited). Oversized → fail with close **1009**. |
| `WithMaxMessageSize` | 16 MiB | Max size of a reassembled message. Oversized → fail with close **1009**. |
| `WithHandshakeTimeout` | 30s | Bound on TCP/TLS connect + opening handshake. |
| `WithCloseTimeout` | 5s | How long a client-initiated close waits for the peer's Close echo. |
| `WithTLSConfig` | derived | TLS config for `wss://` (ServerName defaults to the URL host). |
| `WithOrigin` | none | `Origin` header on the opening handshake. |

## Protocol coverage

- Opening handshake over `ws://` / `wss://`: random `Sec-WebSocket-Key`,
  `Sec-WebSocket-Version: 13`, optional `Sec-WebSocket-Protocol`, validation of
  the `101` response and `Sec-WebSocket-Accept`. A server that selects an
  extension we did not offer fails the handshake.
- Frame codec: FIN/opcode, mandatory client masking with a random 32-bit key,
  7/16/64-bit lengths, continuation/fragmentation reassembly.
- Control frames: `ping` → automatic `pong` echoing the payload, `pong`
  (ignored), and the `close` handshake (code + reason, echoed).
- Failure handling ("Fail the WebSocket Connection"): masked server frame,
  reserved bits, or bad opcode → **1002**; invalid UTF-8 in a text frame →
  **1007**; oversized frame/message → **1009**. Each surfaces an `error` event
  followed by a `close` event with `wasClean === false`. An abrupt transport
  drop yields close code **1006**, not clean.

## Server helper

The package also exports a minimal **server** side used by the example and the
tests — `Upgrade(w, r, UpgradeConfig)` returns a `*ServerConn` with
`ReadMessage`/`WriteMessage`/`WriteFrame`/`Ping`/`Close`/`CloseNow`. It reuses
the same frame codec.

## Differences from browsers

- **No `Blob`.** Binary messages are always delivered as an `ArrayBuffer`, and
  `binaryType` defaults to `"arraybuffer"` (browsers default to `"blob"`).
  Setting `binaryType = "blob"` has no effect.
- **No `Uint8Array`.** gojs does not implement the TypedArray family yet, so read
  incoming bytes with a `DataView`, and send binary data as an `ArrayBuffer` (or
  `DataView`).
- **No `DOMException`.** `send` before `OPEN` throws an `Error` named
  `InvalidStateError`; invalid `close` arguments throw an `Error` named
  `InvalidAccessError`/`SyntaxError`.

## Threading model

The VM is single-threaded. Each socket runs a reader and a writer goroutine that
never touch JavaScript values; every `open`/`message`/`error`/`close` event is
delivered onto the VM goroutine via `Enqueue`. `send`/`close` run on the VM
goroutine, copy their payload there, and hand the copy to the writer over a
channel. An open socket pins the event loop (so `RunString`/`RunLoop` stays
alive); closing it releases the pin so the loop can drain and return.

## Implementation notes

This package relies on a small binary bridge added to the gojs embedding API:
`VM.NewArrayBuffer`, `VM.ArrayBufferBytes`, `VM.TypedArrayBytes`, and
`VM.Pin` (the event-loop keepalive). `VM.NewUint8Array` exists for API
completeness but returns an `ArrayBuffer` because gojs has no Uint8Array.
