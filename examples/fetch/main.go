// Example: the Fetch API (host/fetch).
//
// This program is fully self-contained and offline: it starts a tiny local HTTP
// server on a random loopback port, builds a VM with a print provider and the
// Fetch API installed, and runs main.js — which performs a GET (text), a GET
// (JSON), a POST with a JSON body and custom headers, and demonstrates error
// handling for a bad URL. RunString drains the event loop, so every async fetch
// completes before the program exits.
//
// Run it from the repository root:
//
//	go run ./examples/fetch
package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/iceisfun/gojs"
	"github.com/iceisfun/gojs/host/fetch"
)

//go:embed main.js
var script string

// localServer returns a demo HTTP handler with a few routes the script calls.
func localServer() http.Handler {
	mux := http.NewServeMux()

	// Plain text.
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello from the local server!"))
	})

	// JSON payload.
	mux.HandleFunc("/time", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"service": "demo",
			"version": 1,
			"tags":    []string{"gojs", "fetch"},
		})
	})

	// Echoes the request method, a custom header, and the posted JSON body back.
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed any
		_ = json.Unmarshal(body, &parsed)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"method":   r.Method,
			"apiKey":   r.Header.Get("X-Api-Key"),
			"received": parsed,
		})
	})

	return mux
}

func main() {
	// Start the demo server on a random loopback port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: localServer()}
	go srv.Serve(ln)
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	// Build a VM: console output enabled, plus the timer provider so the script
	// could use AbortSignal.timeout. Then install the Fetch API.
	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
		gojs.WithTimeProvider(gojs.NewDefaultTimeProvider()),
		gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
	)
	defer vm.Close()

	if err := fetch.Install(vm); err != nil {
		log.Fatal(err)
	}
	// Hand the script the base URL of the local server.
	vm.SetGlobal("BASE", gojs.String(base))

	// RunString runs the top level and then drains the event loop, so all the
	// fetch promises settle before it returns.
	if _, err := vm.RunString("main.js", script); err != nil {
		log.Fatalf("script error: %v", err)
	}
}
