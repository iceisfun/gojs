package websocket

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iceisfun/gojs"
)

// clientConn is one live client-side WebSocket. The read and write loops run on
// their own goroutines and never touch gojs.Value/*gojs.Object state; every JS
// event is delivered through vm.Enqueue so it runs on the VM goroutine.
type clientConn struct {
	vm      *gojs.VM
	handle  int
	onEvent gojs.Value // JS callback (type, a, b, c); VM-goroutine only
	release func()     // event-loop Pin release; call exactly once
	remove  func()     // deregister the handle; call on the VM goroutine
	opts    options

	conn       net.Conn
	writeCh    chan wsFrame
	writerStop chan struct{}
	sockOnce   sync.Once

	mu          sync.Mutex
	closeSent   bool
	finished    bool
	closeCode   int
	closeReason string
	wasClean    bool
	errMsg      string
}

// wsFrame is a frame handed to the writer goroutine.
type wsFrame struct {
	opcode  byte
	payload []byte
	final   bool // close the socket after writing this frame
}

// start dials, performs the opening handshake, then runs the read loop. It is
// launched on its own goroutine by connect.
func (cc *clientConn) start(rawURL, protocols string) {
	ctx, cancel := context.WithTimeout(context.Background(), cc.opts.handshakeTimeout)
	defer cancel()

	conn, reader, protocol, extensions, err := dial(ctx, rawURL, protocols, cc.opts)
	if err != nil {
		cc.setClose(closeAbnormalClosure, "", false)
		cc.setError(err.Error())
		cc.finish()
		return
	}
	cc.conn = conn

	go cc.writeLoop()

	cc.vm.Enqueue(func() error {
		return cc.emit("open", gojs.String(protocol), gojs.String(extensions), nil)
	})

	cc.readLoop(reader)
}

// writeLoop owns all writes to the socket. It exits when the socket is closed or
// a frame flagged final has been written.
func (cc *clientConn) writeLoop() {
	for {
		select {
		case <-cc.writerStop:
			return
		case f := <-cc.writeCh:
			if err := writeFrame(cc.conn, true, f.opcode, f.payload, true); err != nil {
				cc.closeSocket()
				return
			}
			if f.final {
				cc.closeSocket()
				return
			}
		}
	}
}

// readLoop reads frames, reassembles messages, answers control frames, and
// delivers message/close/error events. It always calls finish before returning.
func (cc *clientConn) readLoop(reader *bufio.Reader) {
	defer cc.finish()

	var (
		fragOpcode byte
		fragBuf    []byte
		fragged    bool
	)

	for {
		fr, err := readFrame(reader, false, cc.opts.maxFrameSize)
		if err != nil {
			cc.handleReadError(err)
			return
		}

		switch fr.opcode {
		case opPing:
			cc.enqueueFrame(wsFrame{opcode: opPong, payload: fr.payload})

		case opPong:
			// Unsolicited or heartbeat pong: nothing to do.

		case opClose:
			cc.handlePeerClose(fr.payload)
			return

		case opText, opBinary:
			if fragged {
				cc.failConnection(closeProtocolError, "expected continuation frame")
				return
			}
			if fr.fin {
				if !cc.deliverMessage(fr.opcode, fr.payload) {
					return
				}
			} else {
				fragged = true
				fragOpcode = fr.opcode
				fragBuf = append(fragBuf[:0], fr.payload...)
				if len(fragBuf) > cc.opts.maxMessageSize {
					cc.failConnection(closeMessageTooBig, "message exceeds limit")
					return
				}
			}

		case opContinuation:
			if !fragged {
				cc.failConnection(closeProtocolError, "unexpected continuation frame")
				return
			}
			fragBuf = append(fragBuf, fr.payload...)
			if len(fragBuf) > cc.opts.maxMessageSize {
				cc.failConnection(closeMessageTooBig, "message exceeds limit")
				return
			}
			if fr.fin {
				fragged = false
				if !cc.deliverMessage(fragOpcode, fragBuf) {
					return
				}
				fragBuf = nil
			}
		}
	}
}

// deliverMessage validates and dispatches a fully assembled data message. It
// returns false when the connection was failed (invalid UTF-8) and the read
// loop must stop.
func (cc *clientConn) deliverMessage(opcode byte, payload []byte) bool {
	if opcode == opText {
		if !validUTF8(payload) {
			cc.failConnection(closeInvalidPayload, "text message is not valid UTF-8")
			return false
		}
		text := string(payload)
		cc.vm.Enqueue(func() error {
			return cc.emit("message", gojs.String(text), gojs.Bool(false), nil)
		})
		return true
	}
	// Binary: copy out so the VM goroutine owns its own bytes.
	data := make([]byte, len(payload))
	copy(data, payload)
	cc.vm.Enqueue(func() error {
		return cc.emit("message", cc.vm.NewArrayBuffer(data), gojs.Bool(true), nil)
	})
	return true
}

// handlePeerClose processes a Close frame received from the server.
func (cc *clientConn) handlePeerClose(payload []byte) {
	code, reason, err := decodeClosePayload(payload)
	if err != nil {
		// Malformed close: fail with the code the decoder chose.
		we := err.(*wsError)
		cc.failConnection(we.code, we.text)
		return
	}
	cc.setClose(code, reason, true)

	cc.mu.Lock()
	alreadySent := cc.closeSent
	cc.closeSent = true
	cc.mu.Unlock()

	if alreadySent {
		// This is the echo of our own Close: the handshake is complete.
		cc.closeSocket()
		return
	}
	// Peer initiated: echo the Close, then close the socket.
	cc.enqueueFrame(wsFrame{opcode: opClose, payload: encodeClosePayload(code, reason), final: true})
}

// handleReadError maps a read error onto a close/error event.
func (cc *clientConn) handleReadError(err error) {
	if we, ok := err.(*wsError); ok {
		cc.failConnection(we.code, we.text)
		return
	}
	// EOF or transport error with no Close handshake: abnormal closure.
	cc.mu.Lock()
	initiated := cc.closeSent
	cc.mu.Unlock()
	if initiated {
		// We sent a Close and the peer dropped without echoing; still treat as
		// a completed close from our side but not clean.
		cc.setClose(closeAbnormalClosure, "", false)
	} else {
		cc.setClose(closeAbnormalClosure, "", false)
		cc.setError("connection closed abnormally")
	}
	cc.closeSocket()
}

// failConnection fails the connection with a protocol error: send a Close frame
// with code, record an error, then tear down. Used for the RFC "Fail the
// WebSocket Connection" cases (bad UTF-8, oversized, protocol violations).
func (cc *clientConn) failConnection(code int, text string) {
	cc.setClose(code, "", false)
	cc.setError(text)
	cc.mu.Lock()
	cc.closeSent = true
	cc.mu.Unlock()
	cc.enqueueFrame(wsFrame{opcode: opClose, payload: encodeClosePayload(code, ""), final: true})
}

// ---- helpers called from the VM goroutine (native send/close) ----

// sendText queues a text message. Called on the VM goroutine.
func (cc *clientConn) sendText(s string) {
	cc.enqueueFrame(wsFrame{opcode: opText, payload: []byte(s)})
}

// sendBinary queues a binary message. The bytes are already a VM-goroutine copy.
func (cc *clientConn) sendBinary(b []byte) {
	cc.enqueueFrame(wsFrame{opcode: opBinary, payload: b})
}

// initiateClose starts a clean close handshake. Called on the VM goroutine.
func (cc *clientConn) initiateClose(code int, reason string) {
	cc.mu.Lock()
	if cc.closeSent {
		cc.mu.Unlock()
		return
	}
	cc.closeSent = true
	cc.mu.Unlock()
	cc.setClose(code, reason, true)
	cc.enqueueFrame(wsFrame{opcode: opClose, payload: encodeClosePayload(code, reason)})

	// Guard against a peer that never echoes the Close.
	go func() {
		t := time.NewTimer(cc.opts.closeTimeout)
		defer t.Stop()
		select {
		case <-t.C:
			cc.setClose(0, "", false) // downgrade wasClean if not yet finished
			cc.closeSocket()
		case <-cc.writerStop:
		}
	}()
}

// ---- shared plumbing ----

// enqueueFrame hands a frame to the writer goroutine, dropping it if the socket
// is already closing.
func (cc *clientConn) enqueueFrame(f wsFrame) {
	select {
	case cc.writeCh <- f:
	case <-cc.writerStop:
	}
}

// closeSocket closes the transport and stops the writer, exactly once.
func (cc *clientConn) closeSocket() {
	cc.sockOnce.Do(func() {
		if cc.conn != nil {
			cc.conn.Close()
		}
		close(cc.writerStop)
	})
}

// setClose records the close code/reason/clean flag; the first record wins so a
// clean handshake result is not overwritten by the subsequent transport EOF.
func (cc *clientConn) setClose(code int, reason string, clean bool) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closeCode == 0 {
		cc.closeCode = code
		cc.closeReason = reason
		cc.wasClean = clean
	} else if !clean {
		// A later abnormal signal can only clear wasClean, never set it.
		cc.wasClean = cc.wasClean && clean
	}
}

// setError records a message to surface as an error event before close.
func (cc *clientConn) setError(msg string) {
	cc.mu.Lock()
	if cc.errMsg == "" {
		cc.errMsg = msg
	}
	cc.mu.Unlock()
}

// finish delivers the terminal error/close events, releases the Pin, and
// deregisters the handle — exactly once.
func (cc *clientConn) finish() {
	cc.mu.Lock()
	if cc.finished {
		cc.mu.Unlock()
		return
	}
	cc.finished = true
	code, reason, clean, errMsg := cc.closeCode, cc.closeReason, cc.wasClean, cc.errMsg
	cc.mu.Unlock()

	// The socket is closed either explicitly by the terminal path that reached
	// here or by the writer after it flushes a queued final Close frame. We must
	// not close it here: doing so could race the writer and truncate the Close
	// handshake before our Close frame is sent.

	if code == 0 {
		code = closeAbnormalClosure
	}

	cc.vm.Enqueue(func() error {
		if errMsg != "" {
			if err := cc.emit("error", gojs.String(errMsg), nil, nil); err != nil {
				return err
			}
		}
		if err := cc.emit("close", gojs.Number(float64(code)), gojs.String(reason), gojs.Bool(clean)); err != nil {
			return err
		}
		cc.remove()
		cc.release()
		return nil
	})
}

// emit calls the JS onEvent(type, a, b, c) callback. Runs on the VM goroutine.
func (cc *clientConn) emit(typ string, a, b, c gojs.Value) error {
	args := []gojs.Value{gojs.String(typ)}
	if a != nil {
		args = append(args, a)
	}
	if b != nil {
		args = append(args, b)
	}
	if c != nil {
		args = append(args, c)
	}
	_, err := cc.vm.Call(cc.onEvent, gojs.Undefined, args...)
	return err
}

// dial performs the TCP/TLS connect and the RFC 6455 opening handshake. On
// success it returns the raw connection, a buffered reader positioned at the
// first frame byte, and the negotiated subprotocol and extensions.
func dial(ctx context.Context, rawURL, protocols string, opts options) (net.Conn, *bufio.Reader, string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("invalid url: %w", err)
	}
	var secure bool
	switch strings.ToLower(u.Scheme) {
	case "ws":
		secure = false
	case "wss":
		secure = true
	default:
		return nil, nil, "", "", fmt.Errorf("unsupported scheme %q (want ws or wss)", u.Scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if secure {
			port = "443"
		} else {
			port = "80"
		}
	}
	address := net.JoinHostPort(host, port)

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, "", "", err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if secure {
		tlsConf := opts.tlsConfig
		if tlsConf == nil {
			tlsConf = &tls.Config{}
		}
		tlsConf = tlsConf.Clone()
		if tlsConf.ServerName == "" {
			tlsConf.ServerName = host
		}
		tconn := tls.Client(conn, tlsConf)
		if err := tconn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, nil, "", "", err
		}
		conn = tconn
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, nil, "", "", err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	var req strings.Builder
	fmt.Fprintf(&req, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&req, "Host: %s\r\n", u.Host)
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&req, "Sec-WebSocket-Key: %s\r\n", key)
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	if protocols != "" {
		fmt.Fprintf(&req, "Sec-WebSocket-Protocol: %s\r\n", protocols)
	}
	if opts.origin != "" {
		fmt.Fprintf(&req, "Origin: %s\r\n", opts.origin)
	}
	req.WriteString("\r\n")

	if _, err := conn.Write([]byte(req.String())); err != nil {
		conn.Close()
		return nil, nil, "", "", err
	}

	reader := bufio.NewReader(conn)
	httpReq := &http.Request{Method: "GET", URL: u, Header: make(http.Header)}
	resp, err := http.ReadResponse(reader, httpReq)
	if err != nil {
		conn.Close()
		return nil, nil, "", "", fmt.Errorf("reading handshake response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, "", "", fmt.Errorf("handshake failed: unexpected status %d", resp.StatusCode)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		conn.Close()
		return nil, nil, "", "", fmt.Errorf("handshake failed: bad Upgrade header")
	}
	if !headerContainsToken(resp.Header.Get("Connection"), "upgrade") {
		conn.Close()
		return nil, nil, "", "", fmt.Errorf("handshake failed: bad Connection header")
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != acceptKey(key) {
		conn.Close()
		return nil, nil, "", "", fmt.Errorf("handshake failed: bad Sec-WebSocket-Accept")
	}
	extensions := resp.Header.Get("Sec-WebSocket-Extensions")
	if extensions != "" {
		// We offer no extensions, so the server must not select any.
		conn.Close()
		return nil, nil, "", "", fmt.Errorf("handshake failed: server selected unsupported extension %q", extensions)
	}
	protocol := resp.Header.Get("Sec-WebSocket-Protocol")

	// Clear the handshake deadline; framing has its own lifetime.
	_ = conn.SetDeadline(time.Time{})
	return conn, reader, protocol, extensions, nil
}

// headerContainsToken reports whether a comma-separated header value contains
// token (case-insensitive).
func headerContainsToken(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}
