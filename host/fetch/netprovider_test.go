package fetch_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iceisfun/gojs"
	"github.com/iceisfun/gojs/host/fetch"
)

// redirectNet dials every request to target regardless of the hostname the
// script asked for — the loopback test seam. A script that fetches
// https://api.example.com actually reaches our local test server.
type redirectNet struct{ target string }

func (r redirectNet) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, r.target)
}

// denyNet refuses every dial — the egress wall shut.
type denyNet struct{}

func (denyNet) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("network egress denied")
}

func run(t *testing.T, np gojs.NetProvider, script string) string {
	t.Helper()
	vm := gojs.New(
		gojs.WithPrintProvider(gojs.NewDefaultPrintProvider()),
		gojs.WithTimeProvider(gojs.NewDefaultTimeProvider()),
		gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
		gojs.WithNetProvider(np),
	)
	defer vm.Close()
	if err := fetch.Install(vm); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	vm.SetGlobal("report", vm.NewFunction("report", func(args []gojs.Value) (gojs.Value, error) {
		s, _ := vm.ToString(args[0])
		got <- s
		return gojs.Undefined, nil
	}))
	if _, err := vm.RunString("t.js", script); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		return s
	default:
		return "<no report>"
	}
}

// TestNetProviderLoopback: the NetProvider makes a request to an unresolvable
// domain reach a local test server — a hermetic loopback for testing.
func TestNetProviderLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("served host=" + r.Host))
	}))
	defer srv.Close()

	out := run(t, redirectNet{strings.TrimPrefix(srv.URL, "http://")}, `
		fetch("http://api.example.com/x")
			.then(r => r.text())
			.then(report)
			.catch(e => report("ERR:" + e.message));
	`)
	if !strings.Contains(out, "served host=api.example.com") {
		t.Fatalf("expected loopback to serve api.example.com, got %q", out)
	}
}

// TestNetProviderDeny: the NetProvider refuses the dial, so fetch fails — the
// script cannot reach the network even though fetch is installed.
func TestNetProviderDeny(t *testing.T) {
	out := run(t, denyNet{}, `
		fetch("http://api.example.com/x")
			.then(r => report("UNEXPECTED"))
			.catch(e => report("denied"));
	`)
	if out != "denied" {
		t.Fatalf("expected denial, got %q", out)
	}
}
