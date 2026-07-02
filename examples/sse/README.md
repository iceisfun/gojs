# Server-Sent Events (EventSource)

A self-contained, **offline** demo of the `host/sse` package. `main.go` starts a
localhost `text/event-stream` server, builds a VM with print + time + timer
providers and `sse.Install`, and runs `main.js`.

```bash
go run ./examples/sse
```

The server pushes an id:'d `message` and a named `tick` event every 300ms. It
drops the **first** connection after two messages to force an automatic
reconnect; the client reconnects with a `Last-Event-ID` header, and the server
resumes the id sequence from there. After ~2.5s the script calls `es.close()`
and the process exits.

`main.js` demonstrates:

- `onopen` and `onmessage` handler properties,
- a named-event listener via `addEventListener("tick", …)`,
- `onerror` firing on the reconnect (with `readyState` back to `CONNECTING`),
- `Last-Event-ID` continuity, visible as a monotonically increasing
  `e.lastEventId`,
- `close()` ending the stream so `RunString` returns.

Example output (ports and exact interleaving vary):

```
SSE server listening at http://127.0.0.1:PORT
connecting to http://127.0.0.1:PORT
[open] readyState = 1 (OPEN = 1)
[message] message #1 | lastEventId = 1 | origin = http://127.0.0.1:PORT
[tick] value = 1
server: dropping first connection to force a reconnect
[message] message #2 | lastEventId = 2 | origin = http://127.0.0.1:PORT
[tick] value = 2
[error] readyState = 0 -> reconnecting
server: client reconnected with Last-Event-ID="2"
[open] readyState = 1 (OPEN = 1)
[message] message #3 | lastEventId = 3 | origin = http://127.0.0.1:PORT
[tick] value = 3
...
closing EventSource
[closed] readyState = 2 (CLOSED = 2)
done
```
