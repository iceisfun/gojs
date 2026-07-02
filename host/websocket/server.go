package websocket

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// This file provides a small server-side helper built on the same frame codec
// as the client. It is enough to run an echo/relay server for the example and
// is reused by the package tests. It is intentionally minimal: one goroutine per
// connection, blocking reads and writes.

// MessageType identifies the kind of a WebSocket data message.
type MessageType int

const (
	// TextMessage is a UTF-8 text message (opcode 0x1).
	TextMessage MessageType = 1
	// BinaryMessage is a binary message (opcode 0x2).
	BinaryMessage MessageType = 2
)

// ServerConn is an accepted server-side WebSocket connection. Its methods are
// not safe for concurrent use by multiple goroutines except that Close may be
// called concurrently with Read/Write to unblock them.
type ServerConn struct {
	conn      net.Conn
	reader    *bufio.Reader
	Subproto  string
	maxFrame  int
	maxMsg    int
	closeSent bool
}

// UpgradeConfig tunes an Upgrade. The zero value is valid.
type UpgradeConfig struct {
	// Subprotocols lists protocols the server supports, in preference order. The
	// first client-offered protocol that appears here is selected.
	Subprotocols []string
	// MaxFrameSize and MaxMessageSize bound incoming frames/messages (0 =
	// unlimited / 16 MiB default respectively).
	MaxFrameSize   int
	MaxMessageSize int
}

// Upgrade performs the server side of the RFC 6455 opening handshake on an
// incoming HTTP request, hijacks the connection, and returns a ServerConn. The
// caller owns the connection and must Close it.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg UpgradeConfig) (*ServerConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, fmt.Errorf("websocket: request missing Upgrade: websocket")
	}
	if !headerContainsToken(r.Header.Get("Connection"), "upgrade") {
		return nil, fmt.Errorf("websocket: request missing Connection: Upgrade")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, fmt.Errorf("websocket: unsupported Sec-WebSocket-Version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("websocket: missing Sec-WebSocket-Key")
	}

	subproto := selectSubprotocol(r.Header.Get("Sec-WebSocket-Protocol"), cfg.Subprotocols)

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("websocket: ResponseWriter does not support hijacking")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	var resp strings.Builder
	resp.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	resp.WriteString("Upgrade: websocket\r\n")
	resp.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&resp, "Sec-WebSocket-Accept: %s\r\n", acceptKey(key))
	if subproto != "" {
		fmt.Fprintf(&resp, "Sec-WebSocket-Protocol: %s\r\n", subproto)
	}
	resp.WriteString("\r\n")
	if _, err := brw.WriteString(resp.String()); err != nil {
		conn.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}

	maxFrame := cfg.MaxFrameSize
	maxMsg := cfg.MaxMessageSize
	if maxMsg == 0 {
		maxMsg = defaultMaxMessageSize
	}
	return &ServerConn{
		conn:     conn,
		reader:   brw.Reader,
		Subproto: subproto,
		maxFrame: maxFrame,
		maxMsg:   maxMsg,
	}, nil
}

// ReadMessage reads the next complete data message, transparently answering
// Ping frames with a Pong and reassembling fragments. It returns
// (0, nil, err) when the peer sends a Close frame or the connection ends; the
// error is io.EOF-like or a *wsError describing a protocol violation.
func (s *ServerConn) ReadMessage() (MessageType, []byte, error) {
	var (
		fragOpcode byte
		fragBuf    []byte
		fragged    bool
	)
	for {
		fr, err := readFrame(s.reader, true, s.maxFrame)
		if err != nil {
			return 0, nil, err
		}
		switch fr.opcode {
		case opPing:
			if err := s.writeControl(opPong, fr.payload); err != nil {
				return 0, nil, err
			}
		case opPong:
			// ignore
		case opClose:
			return 0, nil, errClosed
		case opText, opBinary:
			if fragged {
				return 0, nil, protoErr(closeProtocolError, "expected continuation frame")
			}
			if fr.fin {
				if len(fr.payload) > s.maxMsg {
					return 0, nil, protoErr(closeMessageTooBig, "message exceeds limit")
				}
				return msgType(fr.opcode), fr.payload, nil
			}
			fragged = true
			fragOpcode = fr.opcode
			fragBuf = append([]byte(nil), fr.payload...)
		case opContinuation:
			if !fragged {
				return 0, nil, protoErr(closeProtocolError, "unexpected continuation frame")
			}
			fragBuf = append(fragBuf, fr.payload...)
			if len(fragBuf) > s.maxMsg {
				return 0, nil, protoErr(closeMessageTooBig, "message exceeds limit")
			}
			if fr.fin {
				return msgType(fragOpcode), fragBuf, nil
			}
		}
	}
}

// WriteMessage sends a complete unfragmented data message.
func (s *ServerConn) WriteMessage(typ MessageType, data []byte) error {
	opcode := opText
	if typ == BinaryMessage {
		opcode = opBinary
	}
	return writeFrame(s.conn, true, opcode, data, false)
}

// WriteFrame writes a single raw frame (unmasked, as a server must). It exposes
// FIN/opcode so callers — including tests — can send fragments or malformed
// frames deliberately.
func (s *ServerConn) WriteFrame(fin bool, opcode byte, payload []byte) error {
	return writeFrame(s.conn, fin, opcode, payload, false)
}

// Ping sends a Ping frame with the given payload.
func (s *ServerConn) Ping(payload []byte) error { return s.writeControl(opPing, payload) }

// Close sends a Close frame with the given code and reason and then closes the
// transport.
func (s *ServerConn) Close(code int, reason string) error {
	if !s.closeSent {
		s.closeSent = true
		_ = writeFrame(s.conn, true, opClose, encodeClosePayload(code, reason), false)
	}
	return s.conn.Close()
}

// CloseNow closes the transport without a Close frame, simulating an abrupt
// disconnect.
func (s *ServerConn) CloseNow() error { return s.conn.Close() }

func (s *ServerConn) writeControl(opcode byte, payload []byte) error {
	return writeFrame(s.conn, true, opcode, payload, false)
}

func msgType(opcode byte) MessageType {
	if opcode == opBinary {
		return BinaryMessage
	}
	return TextMessage
}

// errClosed is returned by ReadMessage when the peer sends a Close frame.
var errClosed = fmt.Errorf("websocket: peer closed connection")

// selectSubprotocol picks the first offered protocol that the server supports.
func selectSubprotocol(offered string, supported []string) string {
	if offered == "" || len(supported) == 0 {
		return ""
	}
	for _, want := range strings.Split(offered, ",") {
		want = strings.TrimSpace(want)
		for _, have := range supported {
			if strings.EqualFold(want, have) {
				return have
			}
		}
	}
	return ""
}
