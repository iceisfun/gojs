package sse_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iceisfun/gojs"
	"github.com/iceisfun/gojs/host/sse"
)

// newVM builds a VM with a timer provider and a `record(...)` global that
// appends "arg0|arg1|..." to the shared slice. record runs on the VM goroutine
// (the same goroutine RunString drives), so the slice needs no locking.
func newVM(t *testing.T, records *[]string, opts ...sse.Option) *gojs.VM {
	t.Helper()
	vm := gojs.New(
		gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
		gojs.WithTimeProvider(gojs.NewDefaultTimeProvider()),
	)
	vm.SetGlobal("record", vm.NewFunction("record", func(args []gojs.Value) (gojs.Value, error) {
		parts := make([]string, len(args))
		for i, a := range args {
			s, _ := vm.ToString(a)
			parts[i] = s
		}
		*records = append(*records, strings.Join(parts, "|"))
		return gojs.Undefined, nil
	}))
	if err := sse.Install(vm, opts...); err != nil {
		t.Fatalf("Install: %v", err)
	}
	return vm
}

// writeSSE writes a chunk and flushes so the client sees it immediately.
func writeSSE(w http.ResponseWriter, s string) {
	io.WriteString(w, s)
	w.(http.Flusher).Flush()
}

// beginStream sends the SSE response headers and flushes.
func beginStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
}

func run(t *testing.T, vm *gojs.VM, script string) {
	t.Helper()
	if _, err := vm.RunString("test.js", script); err != nil {
		t.Fatalf("RunString: %v", err)
	}
}

func TestOpenAndMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beginStream(w)
		writeSSE(w, "data: hello\n\n")
		<-r.Context().Done() // keep the stream open until the client closes it
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(20*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onopen = () => record("open", es.readyState);
		es.onmessage = (e) => { record("message", e.data, e.origin); es.close(); };
	`)

	want := []string{"open|1", "message|hello|" + srv.URL}
	assertRecords(t, records, want)
}

func TestMultilineDataJoined(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beginStream(w)
		writeSSE(w, "data: line one\ndata: line two\ndata: line three\n\n")
		<-r.Context().Done()
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(20*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onmessage = (e) => { record(e.data); es.close(); };
	`)

	assertRecords(t, records, []string{"line one\nline two\nline three"})
}

func TestNamedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beginStream(w)
		writeSSE(w, "event: greeting\ndata: hi there\n\n")
		<-r.Context().Done()
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(20*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onmessage = () => record("UNEXPECTED_MESSAGE");
		es.addEventListener("greeting", (e) => { record("greeting", e.data); es.close(); });
	`)

	assertRecords(t, records, []string{"greeting|hi there"})
}

func TestCommentsIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beginStream(w)
		writeSSE(w, ": this is a comment\n: keep-alive\ndata: after comments\n\n")
		<-r.Context().Done()
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(20*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onmessage = (e) => { record(e.data); es.close(); };
	`)

	assertRecords(t, records, []string{"after comments"})
}

func TestNoDataNotDispatched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beginStream(w)
		// An event with a type but no data must NOT be dispatched.
		writeSSE(w, "event: ping\n\n")
		// An empty block (blank line only) must NOT be dispatched.
		writeSSE(w, "\n")
		writeSSE(w, "data: real\n\n")
		<-r.Context().Done()
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(20*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.addEventListener("ping", () => record("UNEXPECTED_PING"));
		es.onmessage = (e) => { record("message", e.data); es.close(); };
	`)

	assertRecords(t, records, []string{"message|real"})
}

func TestMalformedLinesTolerated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beginStream(w)
		writeSSE(w, "a line with no colon\n")
		writeSSE(w, "unknownfield: value\n")
		writeSSE(w, "data:no-space-after-colon\n\n")
		<-r.Context().Done()
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(20*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onmessage = (e) => { record(e.data); es.close(); };
	`)

	// "data:no-space-after-colon" -> no leading space to strip.
	assertRecords(t, records, []string{"no-space-after-colon"})
}

func TestReconnectSendsLastEventID(t *testing.T) {
	var mu sync.Mutex
	var connects int
	var secondLastEventID string
	gotSecond := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connects++
		n := connects
		mu.Unlock()

		beginStream(w)
		if n == 1 {
			writeSSE(w, "id: 42\ndata: first\n\n")
			return // disconnect -> client should reconnect
		}
		mu.Lock()
		secondLastEventID = r.Header.Get("Last-Event-ID")
		mu.Unlock()
		close(gotSecond)
		writeSSE(w, "data: second\n\n")
		<-r.Context().Done()
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(15*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onmessage = (e) => {
			record(e.data, e.lastEventId);
			if (e.data === "second") es.close();
		};
	`)

	select {
	case <-gotSecond:
	default:
		t.Fatal("second connection never happened")
	}
	mu.Lock()
	defer mu.Unlock()
	if secondLastEventID != "42" {
		t.Fatalf("Last-Event-ID on reconnect = %q, want %q", secondLastEventID, "42")
	}
	assertRecords(t, records, []string{"first|42", "second|42"})
}

func TestRetryFieldUpdatesDelay(t *testing.T) {
	var mu sync.Mutex
	var connects int
	var gap time.Duration
	var firstEnd time.Time

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connects++
		n := connects
		mu.Unlock()

		beginStream(w)
		if n == 1 {
			// Override the (large) base retry with a tiny one.
			writeSSE(w, "retry: 20\ndata: one\n\n")
			mu.Lock()
			firstEnd = time.Now()
			mu.Unlock()
			return
		}
		mu.Lock()
		gap = time.Since(firstEnd)
		mu.Unlock()
		writeSSE(w, "data: two\n\n")
		<-r.Context().Done()
	}))
	defer srv.Close()

	var records []string
	// Base retry is huge; only the retry: field can make the reconnect prompt.
	vm := newVM(t, &records, sse.WithRetry(30*time.Second))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onmessage = (e) => { if (e.data === "two") es.close(); };
	`)

	mu.Lock()
	defer mu.Unlock()
	if gap == 0 || gap > 5*time.Second {
		t.Fatalf("reconnect gap = %v; expected the retry: 20ms override to apply (much less than 30s base)", gap)
	}
}

func TestCloseStopsReconnection(t *testing.T) {
	var mu sync.Mutex
	var connects int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connects++
		mu.Unlock()
		beginStream(w)
		writeSSE(w, "data: once\n\n")
		return // disconnect; a non-closed client would reconnect
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(30*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onmessage = (e) => { record(e.data); es.close(); };
	`)

	// Give any erroneous reconnection ample time to occur.
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if connects != 1 {
		t.Fatalf("connects = %d, want 1 (close must stop reconnection)", connects)
	}
	assertRecords(t, records, []string{"once"})
}

func TestFatalStatusNoReconnect(t *testing.T) {
	var mu sync.Mutex
	var connects int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connects++
		mu.Unlock()
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	var records []string
	vm := newVM(t, &records, sse.WithRetry(20*time.Millisecond))
	defer vm.Close()

	run(t, vm, `
		const es = new EventSource(`+"`"+srv.URL+"`"+`);
		es.onerror = () => record("error", es.readyState);
	`)

	assertRecords(t, records, []string{"error|2"}) // CLOSED, no reconnect
	mu.Lock()
	defer mu.Unlock()
	if connects != 1 {
		t.Fatalf("connects = %d, want 1 (non-200 is fatal)", connects)
	}
}

func TestMaxReconnectsCap(t *testing.T) {
	// A server that flaps (connects, then drops) reconnects forever, like a
	// browser: a successful connection resets the failure streak. The cap only
	// bounds CONSECUTIVE failed connects, so we point at a closed listener to
	// make every attempt fail, then verify the cap lets RunString return.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // the address now refuses connections

	var records []string
	vm := newVM(t, &records,
		sse.WithRetry(5*time.Millisecond),
		sse.WithMaxReconnects(2),
	)
	defer vm.Close()

	// No close(): the cap must let RunString return on its own.
	run(t, vm, `
		const es = new EventSource(`+"`"+url+"`"+`);
		es.onerror = () => record("error", es.readyState);
	`)

	// 1 initial + 2 reconnect attempts, the last of which gives up:
	// two reconnecting errors (readyState CONNECTING=0) then a terminal
	// error (readyState CLOSED=2).
	assertRecords(t, records, []string{"error|0", "error|0", "error|2"})
}

func TestConstantsExposed(t *testing.T) {
	var records []string
	vm := newVM(t, &records)
	defer vm.Close()

	run(t, vm, `
		record(EventSource.CONNECTING, EventSource.OPEN, EventSource.CLOSED);
	`)
	assertRecords(t, records, []string{"0|1|2"})
}

func assertRecords(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("records = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record[%d] = %q, want %q (full: %#v)", i, got[i], want[i], got)
		}
	}
}
