# host/sse — Server-Sent Events (EventSource) for gojs

An **optional** host package that installs a WHATWG-compatible
[`EventSource`](https://html.spec.whatwg.org/multipage/server-sent-events.html)
client into a gojs VM. Scripts get the familiar browser API; the network I/O is
handled by Go on a background goroutine and marshaled back onto the single VM
goroutine safely.

The core VM has **no dependency** on this package — it is purely additive.

## Usage

```go
import (
	"github.com/iceisfun/gojs"
	"github.com/iceisfun/gojs/host/sse"
)

vm := gojs.New(
	gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
	gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()), // needed only if the script uses setTimeout/setInterval
)
defer vm.Close()

if err := sse.Install(vm); err != nil {
	log.Fatal(err)
}

vm.RunString("app.js", `
	const es = new EventSource("http://localhost:8080/events");
	es.onopen    = ()  => console.log("open");
	es.onmessage = (e) => console.log("message:", e.data);
	es.addEventListener("tick", (e) => console.log("tick:", e.data));
	es.onerror   = ()  => console.log("error / reconnecting, readyState =", es.readyState);
	// es.close(); // stop and let RunString return
`)
```

`RunString`/`RunLoop` stay alive while an `EventSource` is open (the package
holds the event loop the way a pending timer would). Calling `es.close()` — or
exhausting the reconnect cap — releases the loop so the call can return.

## `Install`

```go
func Install(vm *gojs.VM, opts ...Option) error
```

Options:

| Option | Default | Meaning |
| --- | --- | --- |
| `WithRetry(d time.Duration)` | `3s` (`DefaultRetry`) | Base reconnection delay, until a server `retry:` field overrides it. |
| `WithMaxReconnects(n int)` | `0` (unlimited) | Cap on **consecutive failed** connection attempts before giving up. A successful connection resets the streak. |
| `WithHTTPClient(c *http.Client)` | fresh, timeout-free client | Custom transport/TLS/proxy. Must not set a per-request `Timeout` (it would abort long-lived streams). |

## JavaScript API

`globalThis.EventSource` is a class with:

- Constructor `new EventSource(url[, { withCredentials }])`. `url` must be an
  absolute HTTP(S) URL.
- Properties: `url`, `withCredentials`, `readyState`.
- Ready-state constants `CONNECTING (0)`, `OPEN (1)`, `CLOSED (2)` — both static
  (`EventSource.OPEN`) and instance (`es.OPEN`).
- Handler properties: `onopen`, `onmessage`, `onerror`.
- `addEventListener(type, fn)` / `removeEventListener(type, fn)` /
  `dispatchEvent(event)` so named events (`event: foo`) reach
  `addEventListener("foo", …)`.
- `close()`.

Delivered events are plain objects shaped like a DOM `MessageEvent`:
`{ type, data, lastEventId, origin, target }`.

## Stream parsing (per the SSE spec)

- Request sends `Accept: text/event-stream`, `Cache-Control: no-cache`, and
  `Last-Event-ID` once an `id:` has been seen.
- A `200` response with a `text/event-stream` content type fires `open`.
- Lines split on `\n`, `\r`, or `\r\n`. `field: value` strips **one** leading
  space after the colon. A line starting with `:` is a comment (ignored). A
  blank line dispatches the accumulated event.
- Fields: `data` (multiple lines join with `\n`; a trailing `\n` is stripped
  before dispatch), `event` (event type, default `message`), `id` (persisted for
  reconnection; ignored if it contains a NUL), `retry` (digits only → new
  reconnection delay in ms). Unknown fields are ignored.
- An event with **no data** is not dispatched (an `event:`/`id:`/`retry:`-only
  block fires nothing, though `id`/`retry` still take effect).

## Reconnection

On a network error or a stream that simply ends, the client sets `readyState` to
`CONNECTING`, fires `error`, waits the reconnection time, and reconnects with
`Last-Event-ID`. `close()` sets `CLOSED` and stops permanently. A non-`200`
status or a wrong content type is a **fatal** error: it fires a non-reconnecting
`error` (`readyState` → `CLOSED`) and does not retry.

`WithMaxReconnects` bounds **consecutive** failures, guaranteeing the process can
exit even if a server is unreachable and the script never calls `close()`. With
the default (unlimited), a live-but-flapping server reconnects forever, exactly
like a browser tab — rely on `close()` to stop it.

## Threading

The blocking HTTP read runs on a dedicated goroutine that never touches any
`gojs.Value`/`*gojs.Object`. Every open/message/error is delivered to JS through
`vm.Enqueue`, which runs the continuation on the VM goroutine. The package keeps
the loop alive for the stream's lifetime with `vm.Pin()` and releases it on
`close()` / give-up, so the loop blocks idle (no busy-polling) and can exit
cleanly. `close()` cancels the request context, unblocking the reader.

## Differences from a browser `EventSource`

- URLs must be absolute; there is no document base URL for relative resolution.
- `withCredentials` is accepted and exposed but has no cookie/CORS effect (there
  is no browser security context on the host).
- No same-origin/CORS enforcement — the host decides what a script may reach via
  the injected URL and the `*http.Client`.
- An exception thrown by an event handler propagates to the embedder (it surfaces
  from `RunString`), rather than being swallowed and merely reported to a
  developer console.
