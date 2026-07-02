// Package fetch installs a WHATWG Fetch API — fetch, Headers, Request, Response,
// AbortController/AbortSignal — into a gojs VM. It is opt-in: nothing is added
// to a VM unless the host calls [Install]. The VM engine has no dependency on
// this package; only this package imports gojs.
//
// # Design
//
// The web-facing API is a JavaScript prelude (fetch.js, embedded) that
// implements the WHATWG semantics — header normalization and iteration, body
// extraction and single-consumption guards, the promise-returning body
// accessors (.text/.json/.arrayBuffer/.bytes) — in terms of a few hidden native
// primitives. The native primitives, installed under globalThis.__gojs_fetch,
// perform the actual net/http round-trip on a worker goroutine and settle a
// promise on the VM event loop, plus a small UTF-8 codec.
//
// # Threading
//
// A native function invoked from JavaScript runs on the VM goroutine. send reads
// its arguments and copies the request body there, then does the blocking HTTP
// on its own goroutine (never touching VM state from it). It keeps the event
// loop alive with VM.Pin for the duration and delivers the result back via
// VM.Enqueue, building the response value and settling the promise on the VM
// goroutine.
//
// # Capability gating
//
// Fetch is a network capability. A plain Install(vm) uses a sensible default
// *http.Client. Hosts can pass [WithClient] to supply their own (proxy, TLS
// config, cookie jar, timeout) and [WithAllowlist] to vet or block every
// outgoing request.
package fetch

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/iceisfun/gojs"
)

//go:embed fetch.js
var preludeSource string

// errUseLastResponse is returned by the redirect policy for redirect: "manual"
// so net/http hands back the 3xx response instead of following it.
var errUseLastResponse = http.ErrUseLastResponse

// Option configures [Install].
type Option func(*config)

type config struct {
	client    *http.Client
	allowlist func(*http.Request) error
}

// WithClient sets the *http.Client used for the round-trip, letting the host
// control transport, TLS, proxy, cookie jar, and any overall timeout. The
// client's redirect policy (CheckRedirect) is overridden per request to honor
// the fetch init's redirect mode.
func WithClient(c *http.Client) Option {
	return func(cfg *config) { cfg.client = c }
}

// WithAllowlist installs a hook called on the VM goroutine with each fully
// built *http.Request just before it is sent. Returning a non-nil error blocks
// the request; the fetch promise rejects with a TypeError carrying the message.
func WithAllowlist(fn func(*http.Request) error) Option {
	return func(cfg *config) { cfg.allowlist = fn }
}

// Install adds the Fetch API to vm. It is safe to call once per VM.
func Install(vm *gojs.VM, opts ...Option) error {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.client == nil {
		// A default client: default transport (transparent gzip/deflate), follow
		// redirects, no overall timeout (abort provides cancellation). When the VM
		// has a NetProvider installed, route dialing (and thus DNS) through it so
		// the host controls network egress; a client supplied via WithClient is
		// left untouched (the host is already in control there).
		cfg.client = &http.Client{}
		if np := vm.NetProvider(); np != nil {
			tr := http.DefaultTransport.(*http.Transport).Clone()
			tr.DialContext = np.DialContext
			cfg.client.Transport = tr
		}
	}

	native := vm.NewPlainObject()
	native.SetData("send", cfg.newSend(vm))
	native.SetData("utf8Encode", vm.NewFunction("utf8Encode", func(args []gojs.Value) (gojs.Value, error) {
		s, err := vm.ToString(argAt(args, 0))
		if err != nil {
			return nil, err
		}
		return vm.NewUint8Array([]byte(s)), nil
	}))
	native.SetData("utf8Decode", vm.NewFunction("utf8Decode", func(args []gojs.Value) (gojs.Value, error) {
		b, ok := bytesOf(vm, argAt(args, 0))
		if !ok {
			return gojs.String(""), nil
		}
		return gojs.String(string(b)), nil
	}))
	vm.SetGlobal("__gojs_fetch", native)

	if _, err := vm.RunString("gojs:fetch", preludeSource); err != nil {
		return fmt.Errorf("fetch: installing prelude: %w", err)
	}
	return nil
}

// newSend builds the native send(method, url, headerPairs, body, redirect)
// primitive. It returns { promise, cancel }.
func (cfg *config) newSend(vm *gojs.VM) *gojs.Object {
	return vm.NewFunction("send", func(args []gojs.Value) (gojs.Value, error) {
		method, _ := vm.ToString(argAt(args, 0))
		rawURL, _ := vm.ToString(argAt(args, 1))
		headerPairs := readPairs(vm, argAt(args, 2))
		redirect, _ := vm.ToString(argAt(args, 4))

		// Copy the body bytes out on the VM goroutine before the worker starts.
		var body []byte
		if b, ok := bytesOf(vm, argAt(args, 3)); ok {
			body = append([]byte(nil), b...)
		}

		cap := vm.NewPromiseCapability()
		ctx, cancel := context.WithCancel(context.Background())

		result := vm.NewPlainObject()
		result.SetData("promise", cap.Promise)
		result.SetData("cancel", vm.NewFunction("cancel", func([]gojs.Value) (gojs.Value, error) {
			cancel()
			return gojs.Undefined, nil
		}))

		// Reject helpers build error values on the VM goroutine.
		reject := func(name, msg string) {
			cap.Reject(vm.NewError(name, msg))
		}

		var reader io.Reader
		if body != nil {
			reader = strings.NewReader(string(body))
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
		if err != nil {
			cancel()
			reject("TypeError", "Failed to parse URL from "+rawURL)
			return result, nil
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			cancel()
			reject("TypeError", "Cannot fetch URL with scheme "+quote(req.URL.Scheme))
			return result, nil
		}
		applyHeaders(req, headerPairs)
		if body != nil {
			req.ContentLength = int64(len(body))
		}

		if cfg.allowlist != nil {
			if err := cfg.allowlist(req); err != nil {
				cancel()
				reject("TypeError", "Request blocked: "+err.Error())
				return result, nil
			}
		}

		client := cfg.clientForRedirect(redirect)
		redirected := &boolBox{}
		client.CheckRedirect = redirectPolicy(redirect, redirected)

		release := vm.Pin()
		go func() {
			resp, err := client.Do(req)
			var data []byte
			if err == nil {
				data, err = io.ReadAll(resp.Body)
				resp.Body.Close()
			}
			vm.Enqueue(func() error {
				defer release()
				defer cancel()
				if err != nil {
					if ctx.Err() != nil {
						// Cancelled via abort: the prelude rejects with the abort
						// reason, but settle the native promise too so nothing leaks.
						cap.Reject(vm.NewError("AbortError", "The operation was aborted."))
						return nil
					}
					cap.Reject(vm.NewError("TypeError", networkErrorMessage(err)))
					return nil
				}
				cap.Resolve(buildResponse(vm, req, resp, redirected.v, data))
				return nil
			})
		}()
		return result, nil
	})
}

type boolBox struct{ v bool }

// clientForRedirect returns a client that shares the configured client's
// transport/jar/timeout but gets a per-request redirect policy.
func (cfg *config) clientForRedirect(_ string) *http.Client {
	base := cfg.client
	return &http.Client{
		Transport: base.Transport,
		Jar:       base.Jar,
		Timeout:   base.Timeout,
	}
}

// redirectPolicy maps a fetch redirect mode to a net/http CheckRedirect.
func redirectPolicy(mode string, redirected *boolBox) func(*http.Request, []*http.Request) error {
	switch mode {
	case "manual":
		return func(*http.Request, []*http.Request) error { return errUseLastResponse }
	case "error":
		return func(*http.Request, []*http.Request) error {
			return errors.New("redirect encountered with redirect mode \"error\"")
		}
	default: // "follow"
		return func(_ *http.Request, via []*http.Request) error {
			if len(via) > 0 {
				redirected.v = true
			}
			if len(via) >= 20 {
				return errors.New("too many redirects")
			}
			return nil
		}
	}
}

// buildResponse constructs the { status, statusText, url, redirected, headers,
// body } descriptor the prelude turns into a Response. Runs on the VM goroutine.
func buildResponse(vm *gojs.VM, req *http.Request, resp *http.Response, redirected bool, data []byte) gojs.Value {
	obj := vm.NewPlainObject()
	obj.SetData("status", gojs.Number(float64(resp.StatusCode)))
	obj.SetData("statusText", gojs.String(statusText(resp.Status)))
	finalURL := req.URL.String()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	obj.SetData("url", gojs.String(finalURL))
	obj.SetData("redirected", gojs.Bool(redirected))
	obj.SetData("body", vm.NewUint8Array(data))

	var pairs []any
	for name, values := range resp.Header {
		for _, v := range values {
			pairs = append(pairs, []any{name, v})
		}
	}
	obj.SetData("headers", vm.FromGo(pairs))
	return obj
}

// applyHeaders sets each [name, value] pair on the request. Host is applied via
// req.Host so it reaches the wire.
func applyHeaders(req *http.Request, pairs [][2]string) {
	for _, p := range pairs {
		name, value := p[0], p[1]
		if strings.EqualFold(name, "host") {
			req.Host = value
			continue
		}
		req.Header.Add(http.CanonicalHeaderKey(name), value)
	}
}

// readPairs reads a JS array of [name, value] arrays into Go.
func readPairs(vm *gojs.VM, v gojs.Value) [][2]string {
	raw, ok := vm.ToGo(v).([]any)
	if !ok {
		return nil
	}
	out := make([][2]string, 0, len(raw))
	for _, item := range raw {
		pair, ok := item.([]any)
		if !ok || len(pair) != 2 {
			continue
		}
		name, _ := pair[0].(string)
		value, _ := pair[1].(string)
		out = append(out, [2]string{name, value})
	}
	return out
}

// bytesOf returns the bytes of a Uint8Array or ArrayBuffer value.
func bytesOf(vm *gojs.VM, v gojs.Value) ([]byte, bool) {
	if b, ok := vm.TypedArrayBytes(v); ok {
		return b, true
	}
	return vm.ArrayBufferBytes(v)
}

func statusText(status string) string {
	// resp.Status is like "200 OK"; the text is everything after the code.
	if i := strings.IndexByte(status, ' '); i >= 0 {
		return status[i+1:]
	}
	return ""
}

// networkErrorMessage produces a browser-ish "Failed to fetch"-style message
// while keeping the underlying cause for diagnosis.
func networkErrorMessage(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return "Failed to fetch: " + uerr.Err.Error()
	}
	return "Failed to fetch: " + err.Error()
}

func quote(s string) string {
	if s == "" {
		return "\"\""
	}
	return "\"" + s + "\""
}

func argAt(args []gojs.Value, n int) gojs.Value {
	if n < 0 || n >= len(args) {
		return gojs.Undefined
	}
	return args[n]
}
