package websocket

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

// This file is the RFC 6455 frame codec, shared by the client and by the
// server-side helper (and reused by the package tests). It deliberately depends
// only on the standard library.

// Opcodes (RFC 6455 §5.2).
const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA
)

// Close codes used by this implementation (RFC 6455 §7.4.1).
const (
	closeNormalClosure   = 1000
	closeNoStatusRcvd    = 1005 // reserved: no status code present in the frame
	closeAbnormalClosure = 1006 // reserved: connection dropped without a Close frame
	closeProtocolError   = 1002
	closeInvalidPayload  = 1007 // e.g. invalid UTF-8 in a text frame
	closePolicyViolation = 1008
	closeMessageTooBig   = 1009
	closeInternalError   = 1011
)

// controlFrameMaxPayload is the largest allowed control-frame payload (§5.5).
const controlFrameMaxPayload = 125

// wsError is a protocol error carrying the WebSocket close code that should be
// used to fail the connection.
type wsError struct {
	code int
	text string
}

func (e *wsError) Error() string { return fmt.Sprintf("websocket: %s (close %d)", e.text, e.code) }

func protoErr(code int, text string) *wsError { return &wsError{code: code, text: text} }

// frame is a single decoded WebSocket frame.
type frame struct {
	fin     bool
	opcode  byte
	payload []byte
}

func isControl(opcode byte) bool { return opcode&0x8 != 0 }

// readFrame decodes one frame from r. expectMasked states whether the peer's
// frames must be masked (true for a server reading client frames, false for a
// client reading server frames), enforcing the RFC 6455 masking rules. maxFrame
// bounds an individual frame's payload length; 0 means unlimited. Protocol
// violations are returned as *wsError with the close code to use.
func readFrame(r *bufio.Reader, expectMasked bool, maxFrame int) (frame, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return frame{}, err
	}

	fin := hdr[0]&0x80 != 0
	rsv := hdr[0] & 0x70
	opcode := hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := int(hdr[1] & 0x7F)

	if rsv != 0 {
		// No extensions are negotiated, so any reserved bit is a violation.
		return frame{}, protoErr(closeProtocolError, "reserved bits set")
	}
	if masked != expectMasked {
		if expectMasked {
			return frame{}, protoErr(closeProtocolError, "client frame is not masked")
		}
		return frame{}, protoErr(closeProtocolError, "server frame must not be masked")
	}

	switch opcode {
	case opContinuation, opText, opBinary, opClose, opPing, opPong:
	default:
		return frame{}, protoErr(closeProtocolError, fmt.Sprintf("unknown opcode 0x%x", opcode))
	}
	if isControl(opcode) {
		if !fin {
			return frame{}, protoErr(closeProtocolError, "fragmented control frame")
		}
		if length > controlFrameMaxPayload {
			return frame{}, protoErr(closeProtocolError, "control frame payload too large")
		}
	}

	// Extended payload length.
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return frame{}, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return frame{}, err
		}
		u := binary.BigEndian.Uint64(ext[:])
		if u > 0x7FFFFFFFFFFFFFFF {
			return frame{}, protoErr(closeProtocolError, "payload length has high bit set")
		}
		length = int(u)
	}

	if maxFrame > 0 && length > maxFrame {
		return frame{}, protoErr(closeMessageTooBig, "frame payload exceeds limit")
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return frame{}, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return frame{}, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i&3]
		}
	}
	return frame{fin: fin, opcode: opcode, payload: payload}, nil
}

// writeFrame encodes a single frame to w. When mask is true (client → server)
// the payload is masked with a fresh random 32-bit key; the caller's payload
// slice is never mutated.
func writeFrame(w io.Writer, fin bool, opcode byte, payload []byte, mask bool) error {
	var header [14]byte
	header[0] = opcode
	if fin {
		header[0] |= 0x80
	}

	n := len(payload)
	hlen := 2
	switch {
	case n <= 125:
		header[1] = byte(n)
	case n <= 0xFFFF:
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:4], uint16(n))
		hlen = 4
	default:
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:10], uint64(n))
		hlen = 10
	}

	var maskKey [4]byte
	if mask {
		header[1] |= 0x80
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		copy(header[hlen:hlen+4], maskKey[:])
		hlen += 4
	}

	if _, err := w.Write(header[:hlen]); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if !mask {
		_, err := w.Write(payload)
		return err
	}
	// Mask into a scratch buffer so the caller's slice is untouched.
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ maskKey[i&3]
	}
	_, err := w.Write(masked)
	return err
}

// encodeClosePayload builds a Close-frame payload from a status code and reason
// (§5.5.1). A code of 0 or closeNoStatusRcvd yields an empty payload.
func encodeClosePayload(code int, reason string) []byte {
	if code == 0 || code == closeNoStatusRcvd {
		return nil
	}
	buf := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(buf[:2], uint16(code))
	copy(buf[2:], reason)
	return buf
}

// decodeClosePayload parses a Close-frame payload into a status code and reason.
// An empty payload means "no status code" (1005). A single byte is a protocol
// error.
func decodeClosePayload(payload []byte) (code int, reason string, err error) {
	switch {
	case len(payload) == 0:
		return closeNoStatusRcvd, "", nil
	case len(payload) == 1:
		return 0, "", protoErr(closeProtocolError, "close frame with 1-byte payload")
	default:
		code = int(binary.BigEndian.Uint16(payload[:2]))
		reason = string(payload[2:])
		if !validCloseCode(code) {
			return code, reason, protoErr(closeProtocolError, "invalid close code")
		}
		if !validUTF8(payload[2:]) {
			return code, reason, protoErr(closeInvalidPayload, "close reason is not valid UTF-8")
		}
		return code, reason, nil
	}
}

// validCloseCode reports whether code is a status code a peer is allowed to send
// in a Close frame (§7.4).
func validCloseCode(code int) bool {
	switch {
	case code >= 3000 && code <= 4999:
		return true
	case code >= 1000 && code <= 1003:
		return true
	case code >= 1007 && code <= 1011:
		return true
	default:
		return false
	}
}
