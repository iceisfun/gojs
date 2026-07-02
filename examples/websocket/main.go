// Example: a browser-compatible WebSocket client in gojs.
//
// This program is fully self-contained and runs offline. It starts a localhost
// echo server (built on host/websocket's server helper), then runs main.js,
// which uses the standard WebSocket API to connect, exchange text and binary
// messages, transparently reconnect after a dropped connection, and finally
// shut down cleanly.
//
//	go run ./examples/websocket
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/iceisfun/gojs"
	"github.com/iceisfun/gojs/host/websocket"
)

func main() {
	// 1. Start a localhost echo server on an ephemeral port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	wsURL := fmt.Sprintf("ws://%s/echo", ln.Addr().String())

	srv := &http.Server{Handler: http.HandlerFunc(echoServer)}
	go srv.Serve(ln)
	defer srv.Close()

	// 2. Build a VM with console output and timers (the reconnect backoff uses
	//    setTimeout), then install the WebSocket global.
	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
		gojs.WithTimeProvider(gojs.NewDefaultTimeProvider()),
		gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
		// The WebSocket client dials through this too. Pass-through here; wrap
		// DialContext to allowlist hosts or deny egress.
		gojs.WithNetProvider(gojs.NewDefaultNetProvider()),
	)
	defer vm.Close()

	if err := websocket.Install(vm); err != nil {
		log.Fatal(err)
	}
	vm.SetGlobal("SERVER_URL", gojs.String(wsURL))

	// 3. Run the client script. RunString blocks until the event loop drains —
	//    i.e. until every socket has closed and no timers remain pending.
	src, err := os.ReadFile("examples/websocket/main.js")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := vm.RunString("main.js", string(src)); err != nil {
		log.Fatal(err)
	}
}

// echoServer upgrades an incoming request and echoes every message back. When it
// receives the text "please-drop" it closes the socket abruptly, letting the
// client demonstrate its reconnect logic.
func echoServer(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Upgrade(w, r, websocket.UpgradeConfig{
		Subprotocols: []string{"echo"},
	})
	if err != nil {
		return
	}
	for {
		typ, data, err := conn.ReadMessage()
		if err != nil {
			// Client closed (or protocol error): mirror the close handshake.
			conn.Close(1000, "")
			return
		}
		if typ == websocket.TextMessage && string(data) == "please-drop" {
			conn.CloseNow() // simulate a mid-session disconnect
			return
		}
		if err := conn.WriteMessage(typ, data); err != nil {
			conn.CloseNow()
			return
		}
	}
}
