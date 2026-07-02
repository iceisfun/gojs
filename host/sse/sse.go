// Package sse installs a WHATWG-compatible Server-Sent Events client
// (the EventSource web API) into a gojs VM.
//
// # Overview
//
// SSE is a one-way "server pushes text to client" protocol layered on a long
// lived HTTP response with content type text/event-stream. This package is an
// OPTIONAL host add-on: the VM has no dependency on it. Call [Install] to expose
// a browser-shaped EventSource constructor to scripts:
//
//	vm := gojs.New(gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()))
//	if err := sse.Install(vm); err != nil { ... }
//	vm.RunString("app.js", `
//	    const es = new EventSource("http://localhost:8080/events");
//	    es.onopen    = () => console.log("open");
//	    es.onmessage = (e) => console.log("message", e.data);
//	    es.addEventListener("tick", (e) => console.log("tick", e.data));
//	    es.onerror   = () => console.log("error/reconnecting");
//	`)
//
// # Architecture
//
// The public EventSource surface is defined as a JavaScript prelude (see
// prelude.go) that [Install] loads with vm.RunString. The prelude delegates the
// actual networking to two hidden natives under globalThis.__gojs_sse:
//
//	__gojs_sse.connect(url, opts, onEvent) -> handle
//	__gojs_sse.close(handle)
//
// connect opens the HTTP stream on its own goroutine and reports every state
// change back to the prelude by calling onEvent(kind, payload) via
// vm.Enqueue, where kind is "open", "event", or "error". The prelude turns
// those into DOM-style events (open/message/named/error) and dispatches them to
// the matching handler property (onopen/onmessage/onerror) and any listeners
// registered with addEventListener.
//
// # Threading
//
// The VM is single-threaded. The blocking HTTP read runs on a background
// goroutine that never touches any JavaScript value directly; it only marshals
// results onto the VM goroutine with vm.Enqueue. While a stream is open the
// package holds the event loop alive with vm.Pin so RunString/RunLoop do not
// return prematurely; close() and giving up on reconnection release the hold so
// the loop can end.
//
// # Reconnection
//
// On a network error or a stream that ends, the client waits the reconnection
// time (default 3s, overridable per-stream by a retry: field), fires an error
// event, and reconnects — sending Last-Event-ID when an id: has been seen. A
// non-200 status or a wrong content type is a fatal error: it fires a
// non-reconnecting error and stops. close() stops permanently. To bound
// automatic reconnection (and guarantee the process can exit even if a script
// never calls close), set a cap with [WithMaxReconnects].
package sse

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/iceisfun/gojs"
)

// DefaultRetry is the initial reconnection delay used when no [WithRetry]
// option is given and the server has not sent a retry: field. It matches the
// value browsers commonly default to.
const DefaultRetry = 3 * time.Second

// Option configures [Install].
type Option func(*config)

type config struct {
	retry       time.Duration
	maxAttempts int
	client      *http.Client
}

// WithRetry sets the base reconnection delay. It is the wait before the first
// (and each subsequent) reconnect attempt until the server overrides it with a
// retry: field. Tests use a very small value for determinism. A non-positive
// value is ignored.
func WithRetry(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.retry = d
		}
	}
}

// WithMaxReconnects caps the number of consecutive failed reconnection attempts
// before the client gives up, fires a final (non-reconnecting) error, and
// releases the event loop. The counter resets whenever a connection succeeds.
// n <= 0 means unlimited reconnection (browser behavior); with unlimited
// reconnection a stream stays alive until the script calls close().
func WithMaxReconnects(n int) Option {
	return func(c *config) { c.maxAttempts = n }
}

// WithHTTPClient supplies a custom *http.Client (for a custom transport, TLS
// config, proxy, etc.). The client must not impose a per-request Timeout, since
// that would abort long-lived streams; cancellation is handled via request
// contexts instead. If unset, a fresh client with no timeout is used.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client != nil {
			c.client = client
		}
	}
}

// registry owns the hidden natives and the set of live streams for one VM.
type registry struct {
	vm  *gojs.VM
	cfg config

	mu      sync.Mutex
	streams map[int64]*stream
	nextID  int64
}

// Install exposes the EventSource constructor to scripts running in vm. It is
// opt-in and safe to call once per VM. Options tune the reconnection behavior
// and HTTP client; a plain Install(vm) uses sensible defaults (see
// [DefaultRetry], unlimited reconnects, a timeout-free http.Client).
func Install(vm *gojs.VM, opts ...Option) error {
	cfg := config{
		retry:       DefaultRetry,
		maxAttempts: 0,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.client == nil {
		// Default client (no Timeout: streams are long-lived). Route dialing
		// through the VM's NetProvider when one is installed, so the host controls
		// egress; WithHTTPClient (a host-supplied client) is left untouched.
		cfg.client = &http.Client{}
		if np := vm.NetProvider(); np != nil {
			tr := http.DefaultTransport.(*http.Transport).Clone()
			tr.DialContext = np.DialContext
			cfg.client.Transport = tr
		}
	}

	reg := &registry{vm: vm, cfg: cfg, streams: map[int64]*stream{}}

	ns := vm.NewPlainObject()
	ns.SetData("connect", vm.NewFunction("connect", reg.connect))
	ns.SetData("close", vm.NewFunction("close", reg.close))
	vm.SetGlobal("__gojs_sse", ns)

	_, err := vm.RunString("<sse-prelude>", prelude)
	return err
}

// connect implements __gojs_sse.connect(url, opts, onEvent) -> handle. It
// validates arguments, starts the reader goroutine, and returns an integer
// handle the prelude later passes to close.
func (r *registry) connect(args []gojs.Value) (gojs.Value, error) {
	rawURL, err := r.vm.ToString(arg(args, 0))
	if err != nil {
		return nil, err
	}
	onEvent, ok := arg(args, 2).(*gojs.Object)
	if !ok || !onEvent.IsCallable() {
		return nil, gojs.NewThrow(r.vm.NewError("TypeError", "EventSource: missing event callback"))
	}

	origin := ""
	if u, perr := url.Parse(rawURL); perr == nil && u.Scheme != "" && u.Host != "" {
		origin = u.Scheme + "://" + u.Host
	}

	r.mu.Lock()
	r.nextID++
	id := r.nextID
	s := &stream{
		reg:     r,
		id:      id,
		url:     rawURL,
		origin:  origin,
		onEvent: onEvent,
		retry:   r.cfg.retry,
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	// Hold the loop open for the lifetime of the stream so RunString/RunLoop do
	// not return while it is live. The reader goroutine releases it on exit.
	s.release = r.vm.Pin()
	r.streams[id] = s
	r.mu.Unlock()

	go s.run()

	return gojs.Number(float64(id)), nil
}

// close implements __gojs_sse.close(handle). It stops the stream permanently.
func (r *registry) close(args []gojs.Value) (gojs.Value, error) {
	id := int64(toNumber(arg(args, 0)))
	r.mu.Lock()
	s := r.streams[id]
	delete(r.streams, id)
	r.mu.Unlock()
	if s != nil {
		s.stop()
	}
	return gojs.Undefined, nil
}

// remove drops a stream from the registry (called by the reader goroutine when
// it exits on its own, e.g. after giving up on reconnection).
func (r *registry) remove(id int64) {
	r.mu.Lock()
	delete(r.streams, id)
	r.mu.Unlock()
}

// arg returns args[i] or undefined when out of range.
func arg(args []gojs.Value, i int) gojs.Value {
	if i < len(args) {
		return args[i]
	}
	return gojs.Undefined
}

// toNumber best-effort converts a value to float64 for the handle id.
func toNumber(v gojs.Value) float64 {
	if n, ok := v.(gojs.Number); ok {
		return float64(n)
	}
	return 0
}
