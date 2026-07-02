package websocket

import (
	"crypto/sha1"
	"encoding/base64"
	"unicode/utf8"
)

// websocketGUID is the RFC 6455 §1.3 magic value appended to Sec-WebSocket-Key
// when computing Sec-WebSocket-Accept.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// acceptKey computes the Sec-WebSocket-Accept value for a client key: the
// base64 of SHA-1(key + GUID).
func acceptKey(clientKey string) string {
	h := sha1.New()
	h.Write([]byte(clientKey))
	h.Write([]byte(websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// validUTF8 reports whether b is well-formed UTF-8.
func validUTF8(b []byte) bool { return utf8.Valid(b) }
