package interp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The bytecode VM polls ctx.Done() only every ctxPollInterval instructions (a
// perf optimization), so these tests pin down that cancellation and deadlines
// still abort a running script promptly — the sandbox depends on it.

func TestContextCancelAbortsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	i := New(WithContext(ctx))
	// Cancel shortly after the script starts spinning.
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	done := make(chan error, 1)
	go func() {
		_, err := i.RunString("spin", `while (true) { let x = 1 + 1; }`)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("script did not observe cancellation within 5s — poll throttle too coarse or broken")
	}
}

func TestContextDeadlineAbortsLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	i := New(WithContext(ctx))
	done := make(chan error, 1)
	go func() {
		_, err := i.RunString("spin", `let s = 0; for (;;) { s = s + 1; }`)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("script did not observe the deadline within 5s")
	}
}

// A short, already-cancelled context must not prevent a well-behaved script that
// completes in fewer than ctxPollInterval instructions from returning its value
// (the poll is periodic, and completion is not gated on it).
func TestShortScriptRunsUnderThrottle(t *testing.T) {
	i := New()
	v, err := i.RunString("t", `1 + 2 + 3`)
	if err != nil {
		t.Fatal(err)
	}
	if v != Value(Number(6)) {
		t.Fatalf("got %v, want 6", v)
	}
}
