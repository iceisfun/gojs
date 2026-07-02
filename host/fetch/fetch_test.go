package fetch

import (
	"compress/gzip"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iceisfun/gojs"
)

// testServer returns an httptest server exercising the behaviors the JS tests
// drive. All handlers are local and hermetic.
func testServer() *httptest.Server {
	mux := http.NewServeMux()

	// Echoes method, selected headers, and body back as text.
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Method", r.Method)
		w.Header().Set("X-Content-Type", r.Header.Get("Content-Type"))
		w.Header().Set("X-Custom-In", r.Header.Get("X-Custom"))
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Body-Len", itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		w.Write(body)
	})

	// Returns JSON.
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hello":"world","n":42,"arr":[1,2,3]}`))
	})

	// Returns a fixed status/statusText and a small text body.
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418 I'm a teapot
		w.Write([]byte("short and stout"))
	})

	// Sends multiple values for one header name.
	mux.HandleFunc("/multi", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Multi", "a")
		w.Header().Add("X-Multi", "b")
		w.Write([]byte("ok"))
	})

	// gzip-encoded body; Go's transport transparently decompresses it.
	mux.HandleFunc("/gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		gz := gzip.NewWriter(w)
		gz.Write([]byte("compressed payload"))
		gz.Close()
	})

	// Redirects to /echo.
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/echo", http.StatusFound)
	})

	// Binary body (bytes 0..255).
	mux.HandleFunc("/binary", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		b := make([]byte, 256)
		for i := range b {
			b[i] = byte(i)
		}
		w.Write(b)
	})

	// Slow endpoint for abort tests: blocks until the request context is done.
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			w.Write([]byte("late"))
		}
	})

	return httptest.NewServer(mux)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// newVM builds a VM with fetch installed and BASE set to srv.URL.
func newVM(t *testing.T, srv *httptest.Server, opts ...Option) *gojs.VM {
	t.Helper()
	vm := gojs.New()
	if err := Install(vm, opts...); err != nil {
		t.Fatalf("Install: %v", err)
	}
	vm.SetGlobal("BASE", gojs.String(srv.URL))
	return vm
}

// runAsync runs an async IIFE that assigns its result string to globalThis.OUT,
// drains the event loop, and returns OUT. Any thrown error is captured as
// "ERR:<name>:<message>".
func runAsync(t *testing.T, vm *gojs.VM, body string) string {
	t.Helper()
	src := `globalThis.OUT = "";
(async () => {
  try {
    globalThis.OUT = String(await (async () => { ` + body + ` })());
  } catch (e) {
    globalThis.OUT = "ERR:" + e.name + ":" + e.message;
  }
})();`
	if _, err := vm.RunString("test", src); err != nil {
		t.Fatalf("RunString: %v", err)
	}
	out, err := vm.ToString(vm.GetGlobal("OUT"))
	if err != nil {
		t.Fatalf("ToString(OUT): %v", err)
	}
	return out
}

func want(t *testing.T, got, expect string) {
	t.Helper()
	if got != expect {
		t.Errorf("got %q, want %q", got, expect)
	}
}

func TestMethods(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		out := runAsync(t, vm, `
			const r = await fetch(BASE + "/echo", { method: "`+m+`" });
			return r.headers.get("x-method");`)
		want(t, out, m)
	}
}

func TestCustomHeadersEchoed(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/echo", { headers: { "X-Custom": "hi-there" } });
		return r.headers.get("x-custom-in");`)
	want(t, out, "hi-there")
}

func TestPostJSONRoundTrip(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const payload = { a: 1, b: [2, 3], c: "x" };
		const r = await fetch(BASE + "/echo", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(payload),
		});
		const echoed = await r.json();
		return echoed.a + "," + echoed.b.join("-") + "," + echoed.c + "," + r.headers.get("x-content-type");`)
	want(t, out, "1,2-3,x,application/json")
}

func TestStringBodyDefaultContentType(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/echo", { method: "POST", body: "plain body" });
		return (await r.text()) + "|" + r.headers.get("x-content-type");`)
	want(t, out, "plain body|text/plain;charset=UTF-8")
}

func TestTextAndStatus(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/status");
		return r.status + "|" + r.statusText + "|" + r.ok + "|" + (await r.text());`)
	want(t, out, "418|I'm a teapot|false|short and stout")
}

func TestOkStatus(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/json");
		return r.ok + "|" + r.status;`)
	want(t, out, "true|200")
}

func TestJSONBody(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/json");
		const j = await r.json();
		return j.hello + "|" + j.n + "|" + j.arr.length;`)
	want(t, out, "world|42|3")
}

func TestArrayBufferAndBytes(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)

	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/binary");
		const buf = await r.arrayBuffer();
		const u = new Uint8Array(buf);
		return u.length + "|" + u[0] + "|" + u[255];`)
	want(t, out, "256|0|255")

	out = runAsync(t, vm, `
		const r = await fetch(BASE + "/binary");
		const u = await r.bytes();
		return (u instanceof Uint8Array) + "|" + u.length + "|" + u[10];`)
	want(t, out, "true|256|10")
}

func TestRedirectFollow(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/redirect");
		return r.status + "|" + r.redirected + "|" + r.url.endsWith("/echo");`)
	want(t, out, "200|true|true")
}

func TestRedirectManual(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/redirect", { redirect: "manual" });
		return r.status + "|" + (r.headers.get("location") !== null);`)
	want(t, out, "302|true")
}

func TestRedirectError(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		try {
			await fetch(BASE + "/redirect", { redirect: "error" });
			return "no-error";
		} catch (e) {
			return e.name;
		}`)
	want(t, out, "TypeError")
}

func TestGzipDecompression(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/gzip");
		return await r.text();`)
	want(t, out, "compressed payload")
}

func TestAbort(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const ac = new AbortController();
		const p = fetch(BASE + "/slow", { signal: ac.signal });
		ac.abort();
		try {
			await p;
			return "no-abort";
		} catch (e) {
			return e.name + "|" + ac.signal.aborted;
		}`)
	want(t, out, "AbortError|true")
}

func TestAbortAlreadyAborted(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const ac = new AbortController();
		ac.abort();
		try {
			await fetch(BASE + "/echo", { signal: ac.signal });
			return "no-abort";
		} catch (e) {
			return e.name;
		}`)
	want(t, out, "AbortError")
}

func TestBadURL(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		try {
			await fetch("not a url");
			return "no-error";
		} catch (e) {
			return e.name;
		}`)
	want(t, out, "TypeError")
}

func TestUnsupportedScheme(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		try {
			await fetch("ftp://example.invalid/x");
			return "no-error";
		} catch (e) {
			return e.name;
		}`)
	want(t, out, "TypeError")
}

func TestConnectionRefused(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	// Port 1 on localhost refuses connections.
	out := runAsync(t, vm, `
		try {
			await fetch("http://127.0.0.1:1/nope");
			return "no-error";
		} catch (e) {
			return e.name;
		}`)
	want(t, out, "TypeError")
}

func TestBodyConsumedTwice(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/json");
		await r.text();
		if (!r.bodyUsed) return "not-used";
		try {
			await r.text();
			return "no-error";
		} catch (e) {
			return e.name;
		}`)
	want(t, out, "TypeError")
}

func TestResponseHeadersMultiValue(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv)
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/multi");
		return r.headers.get("x-multi");`)
	want(t, out, "a, b")
}

func TestAllowlistBlocks(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	vm := newVM(t, srv, WithAllowlist(func(r *http.Request) error {
		if strings.Contains(r.URL.Path, "/json") {
			return errBlocked
		}
		return nil
	}))
	out := runAsync(t, vm, `
		try {
			await fetch(BASE + "/json");
			return "allowed";
		} catch (e) {
			return e.name;
		}`)
	want(t, out, "TypeError")

	out = runAsync(t, vm, `
		const r = await fetch(BASE + "/echo");
		return r.status;`)
	want(t, out, "200")
}

func TestHTTPSWithCustomClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secure hello"))
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	vm := gojs.New()
	if err := Install(vm, WithClient(client)); err != nil {
		t.Fatal(err)
	}
	vm.SetGlobal("BASE", gojs.String(srv.URL))
	out := runAsync(t, vm, `
		const r = await fetch(BASE + "/");
		return (await r.text()) + "|" + BASE.startsWith("https");`)
	want(t, out, "secure hello|true")
}

func TestHeadersClass(t *testing.T) {
	vm := gojs.New()
	if err := Install(vm); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		// Case-insensitive get/has/set/delete.
		{`const h = new Headers(); h.set("Content-Type", "text/plain"); return h.get("content-type");`, "text/plain"},
		{`const h = new Headers({ "X-A": "1" }); return "" + h.has("x-a") + h.has("X-A") + h.has("x-b");`, "truetruefalse"},
		{`const h = new Headers(); h.append("A", "x"); h.append("a", "y"); return h.get("a");`, "x, y"},
		{`const h = new Headers([["a","1"],["b","2"]]); h.delete("A"); return h.get("a") + "/" + h.get("b");`, "null/2"},
		{`const h = new Headers({a:"1"}); return h.get("missing");`, "null"},
		{`const h = new Headers({ "Set-Cookie": "id=1" }); return h.get("set-cookie");`, "id=1"},
		// Iteration is sorted and lowercased.
		{`const h = new Headers(); h.set("Z", "1"); h.set("A", "2"); return [...h.keys()].join(",");`, "a,z"},
		{`const h = new Headers({ B: "2", A: "1" }); return [...h.entries()].map(e => e[0]+"="+e[1]).join("&");`, "a=1&b=2"},
		{`const h = new Headers({ A: "1", B: "2" }); return [...h.values()].join(",");`, "1,2"},
		{`const h = new Headers({ A: "1", B: "2" }); let s=""; h.forEach((v,k) => s += k+":"+v+";"); return s;`, "a:1;b:2;"},
		// Symbol.iterator yields entries.
		{`const h = new Headers({ A: "1" }); const [[k,v]] = [...h]; return k+"="+v;`, "a=1"},
		// Value whitespace is trimmed.
		{`const h = new Headers(); h.set("a", "  spaced  "); return h.get("a");`, "spaced"},
		// Copy-construct from another Headers.
		{`const a = new Headers({ x: "1" }); const b = new Headers(a); return b.get("x");`, "1"},
	}
	for _, c := range cases {
		out := runAsync(t, vm, c.src)
		want(t, out, c.want)
	}
}

func TestRequestResponseConstructors(t *testing.T) {
	vm := gojs.New()
	if err := Install(vm); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		{`const r = new Request("http://x/y", { method: "post" }); return r.method + "|" + r.url;`, "POST|http://x/y"},
		{`const r = new Request("http://x/", { headers: { A: "1" } }); return r.headers.get("a");`, "1"},
		{`const r = new Response("hi", { status: 201, statusText: "Created" }); return r.status + "|" + r.statusText + "|" + r.ok;`, "201|Created|true"},
		{`const r = new Response("ok"); return r.status + "|" + r.ok;`, "200|true"},
		{`const r = new Response("body text"); return await r.text();`, "body text"},
		{`const r = Response.json({ a: 1 }); return (await r.json()).a + "|" + r.headers.get("content-type");`, "1|application/json"},
		{`let threw = false; try { new Request("http://x/", { method: "GET", body: "x" }); } catch (e) { threw = e.name; } return threw;`, "TypeError"},
		{`let threw = false; try { new Response("x", { status: 99 }); } catch (e) { threw = e.name; } return threw;`, "RangeError"},
		// Request body round-trips through the body mixin.
		{`const r = new Request("http://x/", { method: "POST", body: "payload" }); return await r.text();`, "payload"},
	}
	for _, c := range cases {
		out := runAsync(t, vm, c.src)
		want(t, out, c.want)
	}
}

var errBlocked = &blockErr{}

type blockErr struct{}

func (*blockErr) Error() string { return "blocked by allowlist" }
