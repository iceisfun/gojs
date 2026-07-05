package process

import (
	"context"
	"strings"
	"testing"

	"github.com/iceisfun/gojs"
)

// TestNextTickOrdering covers process.nextTick priority: a nextTick callback runs
// before an already-queued Promise reaction (Node's ordering), not after it.
func TestNextTickOrdering(t *testing.T) {
	var out strings.Builder
	vm := gojs.New(gojs.WithPrintProvider(capturePrinter{&out}))
	defer vm.Close()
	if _, err := Install(vm); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.RunString("t.js", `
		Promise.resolve().then(() => console.log("promise"));
		process.nextTick(() => console.log("tick"));
		console.log("sync");
	`); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "sync\ntick\npromise\n" {
		t.Errorf("ordering = %q, want %q", got, "sync\ntick\npromise\n")
	}
}

// TestNextTickChainedBeforePromise covers that a nextTick scheduled from within a
// nextTick still runs before pending Promise reactions.
func TestNextTickChainedBeforePromise(t *testing.T) {
	var out strings.Builder
	vm := gojs.New(gojs.WithPrintProvider(capturePrinter{&out}))
	defer vm.Close()
	if _, err := Install(vm); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.RunString("t.js", `
		Promise.resolve().then(() => console.log("promise"));
		process.nextTick(() => {
			console.log("tick1");
			process.nextTick(() => console.log("tick2"));
		});
	`); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "tick1\ntick2\npromise\n" {
		t.Errorf("ordering = %q, want %q", got, "tick1\ntick2\npromise\n")
	}
}

// TestNextTickValidatesCallbackSynchronously covers that process.nextTick with a
// missing or non-callable first argument throws a TypeError synchronously (from
// the nextTick call), so a surrounding try/catch catches it — rather than queuing
// an invalid job that fails later as an uncaught async error.
func TestNextTickValidatesCallbackSynchronously(t *testing.T) {
	vm := gojs.New(gojs.WithPrintProvider(capturePrinter{&strings.Builder{}}))
	defer vm.Close()
	if _, err := Install(vm); err != nil {
		t.Fatal(err)
	}
	v, err := vm.RunString("t.js", `
		var results = [];
		[123, undefined, "x", null, {}].forEach(function (bad) {
			try { process.nextTick(bad); results.push("no throw"); }
			catch (e) { results.push(e.name); }
		});
		// A valid callback does not throw synchronously.
		try { process.nextTick(function () {}); results.push("ok"); }
		catch (e) { results.push("unexpected:" + e.name); }
		results.join(",");
	`)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := vm.ToString(v)
	if want := "TypeError,TypeError,TypeError,TypeError,TypeError,ok"; s != want {
		t.Errorf("nextTick validation = %q, want %q", s, want)
	}
}

// rawPrinter records exactly the strings emitted through Print, with no added
// newline, so a test can observe whether a trailing partial line was flushed.
type rawPrinter struct{ out *strings.Builder }

func (p rawPrinter) Print(_ context.Context, msg string) { p.out.WriteString(msg) }
func (p rawPrinter) Warn(_ context.Context, msg string)  { p.out.WriteString(msg) }

// TestFlushPartialLineOnCompletion covers Process.Flush: a process.stdout.write
// with no trailing newline is buffered by the line-oriented writer and would be
// lost on normal completion; Flush emits it (the standalone runner calls Flush
// once the program finishes).
func TestFlushPartialLineOnCompletion(t *testing.T) {
	var out strings.Builder
	vm := gojs.New(gojs.WithPrintProvider(rawPrinter{&out}))
	defer vm.Close()
	proc, err := Install(vm)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.RunString("t.js", `process.stdout.write("partial");`); err != nil {
		t.Fatal(err)
	}
	// Before Flush the partial line is still buffered (not emitted).
	if got := out.String(); got != "" {
		t.Errorf("before Flush = %q, want empty (line buffered)", got)
	}
	proc.Flush()
	if got := out.String(); got != "partial" {
		t.Errorf("after Flush = %q, want %q (partial line emitted)", got, "partial")
	}
	// Flush is idempotent — a second call emits nothing more.
	proc.Flush()
	if got := out.String(); got != "partial" {
		t.Errorf("after second Flush = %q, want %q", got, "partial")
	}
}
