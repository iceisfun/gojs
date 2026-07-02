// Package websocket provides a browser-compatible WebSocket client for the gojs
// JavaScript engine. Calling [Install] adds a global WebSocket class to a VM,
// implemented against a hand-written RFC 6455 client that runs the socket on its
// own goroutines and delivers open/message/error/close events back onto the VM
// event loop.
//
// The package depends only on the Go standard library and the gojs embedding
// API; it pulls in no third-party WebSocket library and implements the framing
// protocol directly.
//
// # Scripting surface
//
// After Install, scripts use the familiar Web API:
//
//	const ws = new WebSocket("ws://localhost:8080/chat");
//	ws.binaryType = "arraybuffer";
//	ws.onopen    = () => ws.send("hello");
//	ws.onmessage = (e) => console.log("recv", e.data);
//	ws.onclose   = (e) => console.log("closed", e.code, e.reason, e.wasClean);
//	ws.addEventListener("error", (e) => console.log("error", e.message));
//
// # Browser differences
//
// gojs has no Blob type, so binary messages are always delivered as an
// ArrayBuffer regardless of binaryType, and binaryType defaults to "arraybuffer"
// (the DOM default is "blob"). Everything else — readyState constants, the
// handler properties, addEventListener/removeEventListener/dispatchEvent for the
// open/message/error/close types, and the close handshake with code/reason/
// wasClean — matches the browser API.
package websocket

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/iceisfun/gojs"
)

// Default limits and timeouts.
const (
	defaultMaxFrameSize     = 16 << 20 // 16 MiB
	defaultMaxMessageSize   = 16 << 20 // 16 MiB
	defaultHandshakeTimeout = 30 * time.Second
	defaultCloseTimeout     = 5 * time.Second
)

// options holds the resolved Install configuration.
type options struct {
	maxFrameSize     int
	maxMessageSize   int
	handshakeTimeout time.Duration
	closeTimeout     time.Duration
	tlsConfig        *tls.Config
	origin           string
	// dial, when set, establishes the underlying TCP connection — the VM's
	// NetProvider. nil means dial directly with net.Dialer.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func defaultOptions() options {
	return options{
		maxFrameSize:     defaultMaxFrameSize,
		maxMessageSize:   defaultMaxMessageSize,
		handshakeTimeout: defaultHandshakeTimeout,
		closeTimeout:     defaultCloseTimeout,
	}
}

// Option configures the WebSocket installation.
type Option func(*options)

// WithMaxFrameSize bounds the payload of a single incoming frame. A frame larger
// than the limit fails the connection with close code 1009. Zero means
// unlimited. The default is 16 MiB.
func WithMaxFrameSize(n int) Option {
	return func(o *options) { o.maxFrameSize = n }
}

// WithMaxMessageSize bounds the total size of a reassembled (possibly
// fragmented) message. Exceeding it fails the connection with close code 1009.
// The default is 16 MiB.
func WithMaxMessageSize(n int) Option {
	return func(o *options) { o.maxMessageSize = n }
}

// WithHandshakeTimeout bounds the time allowed to establish the TCP/TLS
// connection and complete the opening handshake. The default is 30 seconds.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(o *options) { o.handshakeTimeout = d }
}

// WithCloseTimeout bounds how long a client-initiated close waits for the peer
// to echo the Close frame before dropping the socket. The default is 5 seconds.
func WithCloseTimeout(d time.Duration) Option {
	return func(o *options) { o.closeTimeout = d }
}

// WithTLSConfig sets the TLS configuration used for wss:// connections. When
// nil, a default configuration is used with ServerName derived from the URL.
func WithTLSConfig(c *tls.Config) Option {
	return func(o *options) { o.tlsConfig = c }
}

// WithOrigin sets an Origin header sent with the opening handshake.
func WithOrigin(origin string) Option {
	return func(o *options) { o.origin = origin }
}

// registry maps opaque handles to live connections. Every access happens on the
// VM goroutine (connect/send/close natives and the enqueued deregistration), so
// it needs no lock.
type registry struct {
	vm    *gojs.VM
	opts  options
	next  int
	conns map[int]*clientConn
}

// Install adds the WebSocket global to vm and wires up the native bridge. It is
// safe to call once per VM, before or after other RunString calls; the global
// persists across runs. Options tune frame/message limits, timeouts, TLS, and
// the handshake Origin.
func Install(vm *gojs.VM, opts ...Option) error {
	cfg := defaultOptions()
	for _, o := range opts {
		o(&cfg)
	}
	// Route the underlying dial through the VM's NetProvider when installed, so
	// the host controls WebSocket egress (and DNS) too.
	if np := vm.NetProvider(); np != nil {
		cfg.dial = np.DialContext
	}
	reg := &registry{vm: vm, opts: cfg, conns: map[int]*clientConn{}}

	bridge := vm.NewPlainObject()
	bridge.SetHidden("connect", vm.NewFunction("connect", reg.connect))
	bridge.SetHidden("send", vm.NewFunction("send", reg.send))
	bridge.SetHidden("close", vm.NewFunction("close", reg.close))
	vm.SetGlobal("__gojs_ws", bridge)

	if _, err := vm.RunString("<websocket-prelude>", preludeJS); err != nil {
		return err
	}
	return nil
}

// connect(url, protocols, onEvent) -> handle. Opens the socket asynchronously
// and returns immediately, mirroring the WebSocket constructor.
func (r *registry) connect(args []gojs.Value) (gojs.Value, error) {
	url, err := r.vm.ToString(arg(args, 0))
	if err != nil {
		return nil, err
	}
	protocols, err := r.vm.ToString(arg(args, 1))
	if err != nil {
		return nil, err
	}
	if protocols == "undefined" {
		protocols = ""
	}
	onEvent := arg(args, 2)

	r.next++
	handle := r.next
	cc := &clientConn{
		vm:         r.vm,
		handle:     handle,
		onEvent:    onEvent,
		release:    r.vm.Pin(),
		remove:     func() { delete(r.conns, handle) },
		opts:       r.opts,
		writeCh:    make(chan wsFrame, 64),
		writerStop: make(chan struct{}),
	}
	r.conns[handle] = cc
	go cc.start(url, protocols)
	return gojs.Number(float64(handle)), nil
}

// send(handle, data) queues an outgoing message. String data becomes a text
// frame; an ArrayBuffer or DataView becomes a binary frame.
func (r *registry) send(args []gojs.Value) (gojs.Value, error) {
	cc := r.lookup(arg(args, 0))
	if cc == nil {
		return gojs.Undefined, nil
	}
	data := arg(args, 1)

	if s, ok := data.(gojs.String); ok {
		cc.sendText(string(s))
		return gojs.Undefined, nil
	}
	if b, ok := r.vm.ArrayBufferBytes(data); ok {
		cc.sendBinary(cloneBytes(b))
		return gojs.Undefined, nil
	}
	if b, ok := r.vm.TypedArrayBytes(data); ok {
		cc.sendBinary(cloneBytes(b))
		return gojs.Undefined, nil
	}
	return nil, gojs.NewThrow(r.vm.NewError("TypeError", "WebSocket.send: data must be a string, ArrayBuffer, or DataView"))
}

// close(handle, code, reason) starts a clean close handshake.
func (r *registry) close(args []gojs.Value) (gojs.Value, error) {
	cc := r.lookup(arg(args, 0))
	if cc == nil {
		return gojs.Undefined, nil
	}
	code := closeNormalClosure
	if c := arg(args, 1); !isUndefined(c) {
		if n, ok := c.(gojs.Number); ok {
			code = int(n)
		}
	}
	reason := ""
	if rv := arg(args, 2); !isUndefined(rv) {
		s, err := r.vm.ToString(rv)
		if err != nil {
			return nil, err
		}
		reason = s
	}
	cc.initiateClose(code, reason)
	return gojs.Undefined, nil
}

func (r *registry) lookup(h gojs.Value) *clientConn {
	n, ok := h.(gojs.Number)
	if !ok {
		return nil
	}
	return r.conns[int(n)]
}

// arg returns the i-th argument or undefined.
func arg(args []gojs.Value, i int) gojs.Value {
	if i < len(args) {
		return args[i]
	}
	return gojs.Undefined
}

func isUndefined(v gojs.Value) bool {
	return v == gojs.Undefined || v == nil
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
