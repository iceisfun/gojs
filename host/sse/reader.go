package sse

import (
	"bufio"
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iceisfun/gojs"
)

// stream is one live EventSource connection. All Value/*Object access happens on
// the VM goroutine (inside functions posted with vm.Enqueue); the reader
// goroutine only touches the plain-Go fields below.
type stream struct {
	reg     *registry
	id      int64
	url     string
	origin  string
	onEvent gojs.Value // JS callback (kind, payload); called only via Enqueue

	// retry and lastEventID are owned by the reader goroutine.
	retry       time.Duration
	lastEventID string

	ctx     context.Context
	cancel  context.CancelFunc
	release func()     // releases the vm.Hold; idempotent
	closed  int32      // atomic: set by stop()
	relOnce atomicOnce // guards release to run at most once
}

// atomicOnce is a tiny sync.Once replacement usable without importing sync here.
type atomicOnce struct{ done int32 }

func (o *atomicOnce) do(fn func()) {
	if atomic.CompareAndSwapInt32(&o.done, 0, 1) {
		fn()
	}
}

func (s *stream) isClosed() bool { return atomic.LoadInt32(&s.closed) == 1 }

// stop marks the stream closed and aborts any in-flight request or retry sleep.
// The reader goroutine observes the cancellation and releases the loop hold.
func (s *stream) stop() {
	atomic.StoreInt32(&s.closed, 1)
	s.cancel()
}

// run is the reader goroutine: (re)connect, parse, reconnect until closed or the
// reconnection cap is hit.
func (s *stream) run() {
	defer s.relOnce.do(s.release)
	defer s.reg.remove(s.id)

	attempts := 0
	for {
		if s.isClosed() {
			return
		}
		opened, fatal := s.connectOnce()
		if s.isClosed() {
			return
		}
		if fatal {
			// Bad status or content type: fire a terminal error and stop.
			s.deliverError(false)
			return
		}
		if opened {
			attempts = 0 // a successful connection resets the failure streak
		}
		if s.reg.cfg.maxAttempts > 0 && attempts >= s.reg.cfg.maxAttempts {
			s.deliverError(false)
			return
		}
		attempts++
		// Transient disconnect (network error or the stream ended): announce the
		// reconnect, wait, and loop to reconnect with Last-Event-ID.
		s.deliverError(true)
		if !s.sleep(s.retry) {
			return // closed during the retry wait
		}
	}
}

// connectOnce performs a single HTTP attempt. It returns opened=true once a
// valid 200 text/event-stream response has begun (after which it parses until
// the stream ends), and fatal=true for a non-retryable response.
func (s *stream) connectOnce() (opened bool, fatal bool) {
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return false, false // malformed URL is treated as transient
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if s.lastEventID != "" {
		req.Header.Set("Last-Event-ID", s.lastEventID)
	}

	resp, err := s.reg.cfg.client.Do(req)
	if err != nil {
		return false, false // network error -> reconnect
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK || !isEventStream(resp.Header.Get("Content-Type")) {
		return false, true // spec: fail the connection permanently
	}

	s.deliverOpen()
	s.parse(resp.Body)
	return true, false
}

// isEventStream reports whether a Content-Type header names text/event-stream
// (ignoring parameters such as charset, and case).
func isEventStream(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return strings.EqualFold(mt, "text/event-stream")
}

// parse consumes the event stream, dispatching each complete event as a blank
// line is reached, until the body ends, a read error occurs, or the stream is
// closed.
func (s *stream) parse(body io.Reader) {
	r := bufio.NewReader(body)

	var data strings.Builder
	hasData := false
	eventType := ""
	idBuffer := s.lastEventID

	dispatch := func() {
		// The last-event-id string tracks the id buffer even across dispatches.
		s.lastEventID = idBuffer
		if !hasData {
			eventType = ""
			return // no data -> no event is fired
		}
		payload := strings.TrimSuffix(data.String(), "\n")
		typ := eventType
		if typ == "" {
			typ = "message"
		}
		s.deliverEvent(typ, payload, s.lastEventID)
		data.Reset()
		hasData = false
		eventType = ""
	}

	for {
		if s.isClosed() {
			return
		}
		line, err := readLine(r)
		if err != nil {
			// A partial final line (no terminator) is ignored per spec.
			return
		}
		switch {
		case line == "":
			dispatch()
		case line[0] == ':':
			// comment line -> ignore
		default:
			field, value := splitField(line)
			switch field {
			case "data":
				data.WriteString(value)
				data.WriteByte('\n')
				hasData = true
			case "event":
				eventType = value
			case "id":
				if !strings.ContainsRune(value, '\x00') {
					idBuffer = value
				}
			case "retry":
				if ms, ok := parseRetry(value); ok {
					s.retry = ms
				}
			default:
				// unknown field -> ignore (tolerate malformed lines)
			}
		}
	}
}

// splitField splits a line into a field name and value at the first colon and
// strips a single leading space from the value, per the SSE line format. A line
// with no colon is a field name with an empty value.
func splitField(line string) (field, value string) {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		field = line[:i]
		value = line[i+1:]
		value = strings.TrimPrefix(value, " ") // remove exactly one leading space
		return field, value
	}
	return line, ""
}

// parseRetry parses a retry: value: ASCII digits only, interpreted as
// milliseconds.
func parseRetry(value string) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	ms, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// readLine reads one line terminated by LF, CR, or CRLF, returning the line
// without its terminator. On EOF or error it returns the error; any partial
// bytes are discarded (the caller treats a partial final line as incomplete).
func readLine(r *bufio.Reader) (string, error) {
	var buf []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return string(buf), err
		}
		switch b {
		case '\n':
			return string(buf), nil
		case '\r':
			// A CR may be followed by LF (CRLF); consume the LF if present.
			if nb, err := r.ReadByte(); err == nil && nb != '\n' {
				_ = r.UnreadByte()
			}
			return string(buf), nil
		default:
			buf = append(buf, b)
		}
	}
}

// sleep waits d, returning false if the stream is closed (context cancelled)
// before the delay elapses.
func (s *stream) sleep(d time.Duration) bool {
	if d < 0 {
		d = 0
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-s.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ---------------------------------------------------------------------------
// Delivery to the VM goroutine
// ---------------------------------------------------------------------------

// deliverOpen reports that the connection is established.
func (s *stream) deliverOpen() {
	s.reg.vm.Enqueue(func() error {
		if s.isClosed() {
			return nil
		}
		_, err := s.reg.vm.Call(s.onEvent, gojs.Undefined, gojs.String("open"), gojs.Undefined)
		return err
	})
}

// deliverEvent reports a dispatched message/named event. The string arguments
// are safe to hand across goroutines; the JavaScript payload object is built on
// the VM goroutine.
func (s *stream) deliverEvent(typ, data, lastID string) {
	origin := s.origin
	s.reg.vm.Enqueue(func() error {
		if s.isClosed() {
			return nil
		}
		vm := s.reg.vm
		payload := vm.NewPlainObject()
		payload.SetData("type", gojs.String(typ))
		payload.SetData("data", gojs.String(data))
		payload.SetData("lastEventId", gojs.String(lastID))
		payload.SetData("origin", gojs.String(origin))
		_, err := vm.Call(s.onEvent, gojs.Undefined, gojs.String("event"), payload)
		return err
	})
}

// deliverError reports a connection error. reconnecting is true when the client
// will retry (readyState becomes CONNECTING) and false for a terminal failure
// (readyState becomes CLOSED).
func (s *stream) deliverError(reconnecting bool) {
	s.reg.vm.Enqueue(func() error {
		if s.isClosed() {
			return nil
		}
		vm := s.reg.vm
		payload := vm.NewPlainObject()
		payload.SetData("reconnecting", gojs.Bool(reconnecting))
		_, err := vm.Call(s.onEvent, gojs.Undefined, gojs.String("error"), payload)
		return err
	})
}
