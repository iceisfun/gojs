# WebSocket client

A self-contained, offline demo of the browser-compatible `WebSocket` global that
[`host/websocket`](../../host/websocket) installs.

`main.go` starts a localhost echo server (built on the package's server helper),
installs the WebSocket global on a VM, and runs `main.js`. The script uses only
the standard `WebSocket` API to:

- connect (with a subprotocol) and log the `open`/`readyState` transition,
- send a **text** message and a **binary** message (`ArrayBuffer`),
- handle the echoed replies via `onmessage` / `addEventListener`,
- **reconnect** automatically after the server abruptly drops the socket
  (`onclose` with `wasClean === false`), using a `setTimeout` backoff,
- **shut down gracefully** with `ws.close(1000, "bye")` on the second session.

```bash
go run ./examples/websocket
```

Expected output:

```
[client] connecting (attempt 1) to ws://127.0.0.1:PORT/echo
[client] open; protocol="echo" readyState=1
[client] text echo: "hello, server"
[client] binary echo: [103, 111, 106, 115]
[client] asking server to drop the connection...
[client] error: connection closed abnormally
[client] close code=1006 reason="" wasClean=false
[client] reconnecting in 100ms
[client] connecting (attempt 2) to ws://127.0.0.1:PORT/echo
[client] open; protocol="echo" readyState=1
[client] text echo: "hello, server"
[client] binary echo: [103, 111, 106, 115]
[client] done; closing cleanly
[client] close code=1000 reason="bye" wasClean=true
[client] shutdown complete
```

## Notes

- `RunString` blocks until the event loop drains. An open socket keeps the loop
  alive (via the interpreter's `Pin` mechanism), and `setTimeout` keeps it alive
  during the reconnect backoff, so the program exits only after the final clean
  close.
- gojs has no `Blob`, so binary frames always arrive as an `ArrayBuffer`. The
  script reads bytes with a `DataView` because gojs has no `Uint8Array` sugar
  yet.
