package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iceisfun/gojs"
)

// newWSServer starts an httptest server that upgrades every request and hands
// the accepted ServerConn to handler (run on its own goroutine). It returns the
// ws:// URL and a cleanup func.
func newWSServer(t *testing.T, handler func(*ServerConn)) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r, UpgradeConfig{Subprotocols: []string{"chat", "echo"}})
		if err != nil {
			return
		}
		handler(conn)
	}))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	return wsURL, srv.Close
}

// runClient installs WebSocket on a fresh VM, runs js (which must eventually let
// the socket close so the loop drains), and returns the recorded `log` array as
// Go values. It fails the test if the run does not complete within the timeout.
func runClient(t *testing.T, js string, opts ...Option) []any {
	t.Helper()
	vm := gojs.New()
	if err := Install(vm, opts...); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Helpers available to every test script.
	const helpers = `
		globalThis.log = [];
		function rec(o) { globalThis.log.push(o); }
		function abBytes(ab) {
			var dv = new DataView(ab);
			var a = [];
			for (var i = 0; i < ab.byteLength; i++) a.push(dv.getUint8(i));
			return a;
		}
		function bytesToAB(arr) {
			var ab = new ArrayBuffer(arr.length);
			var dv = new DataView(ab);
			for (var i = 0; i < arr.length; i++) dv.setUint8(i, arr[i]);
			return ab;
		}
	`

	done := make(chan error, 1)
	go func() {
		_, err := vm.RunString("test.js", helpers+js)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunString: %v", err)
		}
	case <-time.After(10 * time.Second):
		vm.Close()
		t.Fatal("timed out waiting for WebSocket run to complete")
	}

	logVal := vm.GetGlobal("log")
	out, _ := vm.ToGo(logVal).([]any)
	vm.Close()
	return out
}

// findEvent returns the first logged record whose "type" equals typ.
func findEvent(log []any, typ string) map[string]any {
	for _, e := range log {
		if m, ok := e.(map[string]any); ok {
			if m["type"] == typ {
				return m
			}
		}
	}
	return nil
}

func TestOpenAndCleanClientClose(t *testing.T) {
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		// Echo until the client sends Close, then echo the Close.
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				c.Close(closeNormalClosure, "")
				return
			}
		}
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		rec({ type: "ctor", state: ws.readyState });
		ws.onopen = function() {
			rec({ type: "open", state: ws.readyState, proto: ws.protocol });
			ws.close(1000, "bye");
		};
		ws.onclose = function(e) {
			rec({ type: "close", state: ws.readyState, code: e.code, reason: e.reason, clean: e.wasClean });
		};
	`)

	if m := findEvent(log, "ctor"); m == nil || m["state"].(float64) != 0 {
		t.Fatalf("expected CONNECTING(0) at construction, got %v", m)
	}
	open := findEvent(log, "open")
	if open == nil || open["state"].(float64) != 1 {
		t.Fatalf("expected OPEN(1) at onopen, got %v", open)
	}
	cl := findEvent(log, "close")
	if cl == nil {
		t.Fatal("no close event")
	}
	if cl["state"].(float64) != 3 {
		t.Fatalf("expected CLOSED(3) at onclose, got %v", cl["state"])
	}
	if cl["code"].(float64) != 1000 || cl["reason"] != "bye" || cl["clean"] != true {
		t.Fatalf("bad close event: %v", cl)
	}
}

func TestSendReceiveText(t *testing.T) {
	url, cleanup := newWSServer(t, echoHandler)
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onopen = function() { ws.send("hello, server"); };
		ws.onmessage = function(e) {
			rec({ type: "message", binary: (typeof e.data !== "string"), text: e.data });
			ws.close(1000, "done");
		};
		ws.onclose = function(e) { rec({ type: "close", clean: e.wasClean }); };
	`)

	m := findEvent(log, "message")
	if m == nil {
		t.Fatal("no message event")
	}
	if m["binary"] != false || m["text"] != "hello, server" {
		t.Fatalf("bad text message: %v", m)
	}
}

func TestSendReceiveBinary(t *testing.T) {
	url, cleanup := newWSServer(t, echoHandler)
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.binaryType = "arraybuffer";
		ws.onopen = function() { ws.send(bytesToAB([1, 2, 3, 254, 255])); };
		ws.onmessage = function(e) {
			rec({ type: "message", binary: (typeof e.data !== "string"), bytes: abBytes(e.data) });
			ws.close();
		};
	`)

	m := findEvent(log, "message")
	if m == nil {
		t.Fatal("no message event")
	}
	if m["binary"] != true {
		t.Fatalf("expected binary message, got %v", m)
	}
	got := m["bytes"].([]any)
	want := []float64{1, 2, 3, 254, 255}
	if len(got) != len(want) {
		t.Fatalf("byte length mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].(float64) != want[i] {
			t.Fatalf("byte %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestServerPingAutoPong(t *testing.T) {
	pongPayload := make(chan []byte, 1)
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		_ = c.Ping([]byte("heartbeat"))
		// Read raw frames until we see the client's Pong.
		for {
			fr, err := readFrame(c.reader, true, 0)
			if err != nil {
				pongPayload <- nil
				return
			}
			if fr.opcode == opPong {
				pongPayload <- fr.payload
				_ = c.WriteMessage(TextMessage, []byte("pong-observed"))
				continue
			}
			if fr.opcode == opClose {
				c.Close(closeNormalClosure, "")
				return
			}
		}
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onmessage = function(e) {
			rec({ type: "message", text: e.data });
			ws.close(1000, "ok");
		};
	`)

	select {
	case p := <-pongPayload:
		if string(p) != "heartbeat" {
			t.Fatalf("pong payload = %q, want %q", p, "heartbeat")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never observed a pong")
	}
	if m := findEvent(log, "message"); m == nil || m["text"] != "pong-observed" {
		t.Fatalf("expected post-pong message, got %v", m)
	}
}

func TestFragmentedMessageReassembly(t *testing.T) {
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		_ = c.WriteFrame(false, opText, []byte("Hel"))
		_ = c.WriteFrame(false, opContinuation, []byte("lo, "))
		_ = c.WriteFrame(true, opContinuation, []byte("World"))
		// Wait for the client's close, then finish.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				c.Close(closeNormalClosure, "")
				return
			}
		}
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onmessage = function(e) { rec({ type: "message", text: e.data }); ws.close(); };
	`)

	if m := findEvent(log, "message"); m == nil || m["text"] != "Hello, World" {
		t.Fatalf("reassembly failed: %v", m)
	}
}

func TestLargeFrameWithinLimit(t *testing.T) {
	const size = 200000
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		big := make([]byte, size)
		for i := range big {
			big[i] = byte(i)
		}
		_ = c.WriteMessage(BinaryMessage, big)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				c.Close(closeNormalClosure, "")
				return
			}
		}
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onmessage = function(e) { rec({ type: "message", len: e.data.byteLength }); ws.close(); };
	`, WithMaxFrameSize(1<<20), WithMaxMessageSize(1<<20))

	m := findEvent(log, "message")
	if m == nil || int(m["len"].(float64)) != size {
		t.Fatalf("large frame not received intact: %v", m)
	}
}

func TestOversizedFrameRejected(t *testing.T) {
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		_ = c.WriteMessage(BinaryMessage, make([]byte, 4096))
		// Drain until the client fails the connection.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				c.CloseNow()
				return
			}
		}
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onerror = function(e) { rec({ type: "error", message: e.message }); };
		ws.onclose = function(e) { rec({ type: "close", code: e.code, clean: e.wasClean }); };
	`, WithMaxFrameSize(1024))

	cl := findEvent(log, "close")
	if cl == nil || cl["code"].(float64) != 1009 || cl["clean"] != false {
		t.Fatalf("expected close 1009 not-clean, got %v", cl)
	}
	if findEvent(log, "error") == nil {
		t.Fatal("expected an error event on oversized frame")
	}
}

func TestInvalidUTF8Rejected(t *testing.T) {
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		_ = c.WriteFrame(true, opText, []byte{0x48, 0x69, 0xff, 0xfe}) // "Hi" + invalid
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				c.CloseNow()
				return
			}
		}
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onclose = function(e) { rec({ type: "close", code: e.code, clean: e.wasClean }); };
	`)

	cl := findEvent(log, "close")
	if cl == nil || cl["code"].(float64) != 1007 {
		t.Fatalf("expected close 1007, got %v", cl)
	}
}

func TestServerInitiatedCleanClose(t *testing.T) {
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		_ = c.Close(1000, "server-bye")
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onclose = function(e) { rec({ type: "close", code: e.code, reason: e.reason, clean: e.wasClean }); };
	`)

	cl := findEvent(log, "close")
	if cl == nil {
		t.Fatal("no close event")
	}
	if cl["code"].(float64) != 1000 || cl["reason"] != "server-bye" || cl["clean"] != true {
		t.Fatalf("bad server-initiated close: %v", cl)
	}
}

func TestAbruptDisconnect(t *testing.T) {
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		c.CloseNow() // drop without a Close handshake
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`);
		ws.onerror = function(e) { rec({ type: "error" }); };
		ws.onclose = function(e) { rec({ type: "close", code: e.code, clean: e.wasClean }); };
	`)

	cl := findEvent(log, "close")
	if cl == nil || cl["code"].(float64) != 1006 || cl["clean"] != false {
		t.Fatalf("expected abnormal close 1006 not-clean, got %v", cl)
	}
	if findEvent(log, "error") == nil {
		t.Fatal("expected an error event on abrupt disconnect")
	}
}

func TestSubprotocolNegotiation(t *testing.T) {
	url, cleanup := newWSServer(t, func(c *ServerConn) {
		_ = c.Close(1000, "")
	})
	defer cleanup()

	log := runClient(t, `
		var ws = new WebSocket(`+jsStr(url)+`, ["superchat", "echo"]);
		ws.onopen = function() { rec({ type: "open", proto: ws.protocol }); };
		ws.onclose = function() { rec({ type: "close" }); };
	`)

	open := findEvent(log, "open")
	if open == nil || open["proto"] != "echo" {
		t.Fatalf("expected negotiated subprotocol 'echo', got %v", open)
	}
}

// echoHandler echoes every data message back and mirrors the close handshake.
func echoHandler(c *ServerConn) {
	for {
		typ, data, err := c.ReadMessage()
		if err != nil {
			c.Close(closeNormalClosure, "")
			return
		}
		if err := c.WriteMessage(typ, data); err != nil {
			c.CloseNow()
			return
		}
	}
}

// jsStr renders s as a JS string literal.
func jsStr(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}
