// Example: Server-Sent Events (EventSource) in gojs.
//
// This program is fully self-contained and runs OFFLINE. It starts a localhost
// SSE server that pushes a periodic "message" event, a custom "tick" event, and
// an ever-increasing id:, then deliberately drops the connection once so the
// client demonstrates automatic reconnection with Last-Event-ID. A gojs VM with
// a print provider, a timer provider, and the sse host package loads main.js,
// which wires up onopen/onmessage, a named-event listener, and onerror, and
// closes the stream after a few seconds so the process exits cleanly.
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/iceisfun/gojs"
	"github.com/iceisfun/gojs/host/sse"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(streamEvents))
	defer srv.Close()

	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
		gojs.WithTimeProvider(gojs.NewDefaultTimeProvider()),
		gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
		// Route the SSE stream's dial (and DNS) through the egress wall.
		// Pass-through here; wrap DialContext to allowlist or deny.
		gojs.WithNetProvider(gojs.NewDefaultNetProvider()),
	)
	defer vm.Close()

	if err := sse.Install(vm, sse.WithRetry(500*time.Millisecond)); err != nil {
		log.Fatal(err)
	}

	// Expose the server URL to the script.
	vm.SetGlobal("SERVER_URL", gojs.String(srv.URL))

	source, err := os.ReadFile("examples/sse/main.js")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("SSE server listening at", srv.URL)
	// RunString stays alive while the EventSource is open (the host holds the
	// event loop); it returns once the script calls es.close().
	if _, err := vm.RunString("main.js", string(source)); err != nil {
		log.Fatal(err)
	}
	fmt.Println("done")
}

// streamEvents is the SSE endpoint. It sends an id:'d message and a named
// "tick" event once per 300ms. On its FIRST connection it drops the stream
// after two messages to exercise reconnection; the client reconnects with a
// Last-Event-ID header, which the server logs and honors by resuming the ids.
func streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Resume the id counter from Last-Event-ID when the client reconnects.
	id := 0
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if n, err := strconv.Atoi(last); err == nil {
			id = n
			fmt.Printf("server: client reconnected with Last-Event-ID=%q\n", last)
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	firstConn := isFirstConnection()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	sent := 0
	for {
		select {
		case <-r.Context().Done():
			return // client closed the stream
		case <-ticker.C:
			id++
			sent++
			fmt.Fprintf(w, "id: %d\n", id)
			fmt.Fprintf(w, "data: message #%d\n\n", id)
			fmt.Fprintf(w, "event: tick\ndata: %d\n\n", id)
			flusher.Flush()

			// On the very first connection, drop after two messages so the
			// client demonstrates automatic reconnection.
			if firstConn && sent >= 2 {
				fmt.Println("server: dropping first connection to force a reconnect")
				return
			}
		}
	}
}

var connOnce sync.Once

// isFirstConnection reports true exactly once, for the first HTTP connection.
func isFirstConnection() bool {
	firstConn := false
	connOnce.Do(func() { firstConn = true })
	return firstConn
}
