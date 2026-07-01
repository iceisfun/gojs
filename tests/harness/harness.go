// Package harness provides reusable infrastructure for running JavaScript
// snippets and fixtures against the gojs engine under test.
//
// Every run gets a fully-provisioned VM (a capturing print provider, a
// deterministic time provider, and a real timer provider) plus a small
// test262-style `assert` object injected as a prologue, so test scripts can
// self-report failures by throwing. Runs are bounded by a context timeout and a
// wall-clock watchdog so a runaway or hung script fails the test instead of
// wedging the whole suite.
package harness

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iceisfun/gojs/interp"
)

// DefaultTimeout bounds a single script run. Loops and generator steps check the
// interpreter context between statements, so a runaway synchronous loop is
// interrupted when the context deadline elapses.
const DefaultTimeout = 5 * time.Second

// FixedEpoch is the wall-clock time reported by the deterministic time provider,
// so Date-dependent tests are reproducible.
var FixedEpoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// fixedTime is a TimeProvider that always reports FixedEpoch (with a monotonic
// clock advancing from zero) for deterministic Date/performance behavior.
type fixedTime struct{ start time.Time }

func (fixedTime) Now(context.Context) time.Time { return FixedEpoch }
func (f fixedTime) Monotonic(context.Context) float64 {
	return float64(time.Since(f.start).Milliseconds())
}

// capture records console output line by line.
type capture struct {
	mu    sync.Mutex
	lines []string
}

func (c *capture) Print(_ context.Context, msg string) {
	c.mu.Lock()
	c.lines = append(c.lines, msg)
	c.mu.Unlock()
}
func (c *capture) Warn(ctx context.Context, msg string) { c.Print(ctx, msg) }

// Result is the outcome of a script run.
type Result struct {
	Output []string // captured console lines, in order
	Value  interp.Value
	Err    error
}

// assertPrologue is a minimal, test262-compatible assertion library injected
// before every harness script. Scripts throw (via assert failures) to signal a
// test failure; the harness turns any uncaught throw into a Go test failure.
const assertPrologue = `
function Test262Error(message) { this.message = message || ""; }
Test262Error.prototype.toString = function () { return "Test262Error: " + this.message; };
function assert(mustBeTrue, message) {
  if (mustBeTrue === true) return;
  throw new Test262Error("Expected true but got " + String(mustBeTrue) +
    (message ? ": " + message : ""));
}
assert._isSameValue = function (a, b) {
  if (a === b) return a !== 0 || 1 / a === 1 / b; // +0 vs -0
  return a !== a && b !== b;                        // NaN
};
assert.sameValue = function (actual, expected, message) {
  if (assert._isSameValue(actual, expected)) return;
  throw new Test262Error("Expected SameValue(" + String(actual) + ", " +
    String(expected) + ") to be true" + (message ? ": " + message : ""));
};
assert.notSameValue = function (actual, unexpected, message) {
  if (!assert._isSameValue(actual, unexpected)) return;
  throw new Test262Error("Expected SameValue(" + String(actual) + ", " +
    String(unexpected) + ") to be false" + (message ? ": " + message : ""));
};
assert.throws = function (expectedErrorConstructor, func, message) {
  if (typeof func !== "function") throw new Test262Error("assert.throws requires a function");
  try {
    func();
  } catch (thrown) {
    if (thrown instanceof expectedErrorConstructor) return;
    throw new Test262Error("Threw " + (thrown && thrown.name) +
      " instead of " + expectedErrorConstructor.name +
      (message ? ": " + message : ""));
  }
  throw new Test262Error("Expected a " + expectedErrorConstructor.name +
    " to be thrown but no exception was thrown" + (message ? ": " + message : ""));
};
`

// New builds a VM provisioned with all providers plus a capture sink. The
// returned capture exposes the console output collected during the run.
func New(ctx context.Context) (*interp.Interpreter, *capture) {
	cap := &capture{}
	vm := interp.New(
		interp.WithContext(ctx),
		interp.WithPrintProvider(cap),
		interp.WithTimeProvider(fixedTime{start: FixedEpoch}),
		interp.WithTimerProvider(interp.NewDefaultTimerProvider()),
	)
	return vm, cap
}

// Run executes source (prefixed with the assertion prologue) under a timeout and
// returns the result. It never blocks the caller indefinitely: if the run does
// not finish within DefaultTimeout+1s, Run reports a timeout error.
func Run(source string) Result {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	vm, cap := New(ctx)
	defer vm.Close()

	done := make(chan Result, 1)
	go func() {
		v, err := vm.RunString("<harness>", assertPrologue+"\n"+source)
		done <- Result{Value: v, Err: err}
	}()

	select {
	case r := <-done:
		r.Output = cap.lines
		return r
	case <-time.After(DefaultTimeout + time.Second):
		// The context deadline should already have unwound execution; if we get
		// here the run is genuinely stuck (e.g. a deadlocked host op).
		return Result{Output: cap.lines, Err: context.DeadlineExceeded}
	}
}

// Expect runs source and fails t if it produces an uncaught error (i.e. any
// assert failure or thrown exception). It returns the captured output.
func Expect(t *testing.T, source string) []string {
	t.Helper()
	r := Run(source)
	if r.Err != nil {
		if v, ok := interp.ThrownValue(r.Err); ok {
			t.Fatalf("uncaught exception: %s", interp.BriefValue(v))
		}
		t.Fatalf("run error: %v", r.Err)
	}
	return r.Output
}

// ExpectError runs source and fails t unless it throws. When wantName is
// non-empty, the thrown value's error name must match. Returns the thrown value.
func ExpectError(t *testing.T, source, wantName string) {
	t.Helper()
	r := Run(source)
	if r.Err == nil {
		t.Fatalf("expected an error but the script completed")
	}
	v, ok := interp.ThrownValue(r.Err)
	if !ok {
		t.Fatalf("expected a thrown JS value, got host error: %v", r.Err)
	}
	if wantName != "" && !strings.Contains(interp.BriefValue(v), wantName) {
		t.Fatalf("expected a %s, got: %s", wantName, interp.BriefValue(v))
	}
}
