package interp

import (
	"context"
	"fmt"
	"os"
	"time"
)

// This file defines the capability provider interfaces that gate the
// interpreter's access to host facilities, mirroring the golua provider design.
// A nil provider means the capability is unavailable (the sandbox stays closed);
// a Default* implementation grants ordinary host access.

// ---------------------------------------------------------------------------
// PrintProvider — console output
// ---------------------------------------------------------------------------

// PrintProvider routes console output through a host-defined sink. All writes
// that a user script makes to stdout/stderr (console.log, console.error, and
// friends) pass through here, so an embedder can capture, redirect, or silence
// them. Without a provider, console methods produce no output.
type PrintProvider interface {
	// Print receives normal console output (console.log/info/debug). msg is the
	// fully formatted line without a trailing newline; the provider adds one if
	// desired.
	Print(ctx context.Context, msg string)
	// Warn receives diagnostic output (console.warn/error).
	Warn(ctx context.Context, msg string)
}

// DefaultPrintProvider writes normal output to stdout and warnings to stderr.
type DefaultPrintProvider struct{}

// NewDefaultPrintProvider returns a PrintProvider backed by os.Stdout/os.Stderr.
func NewDefaultPrintProvider() *DefaultPrintProvider { return &DefaultPrintProvider{} }

// Print writes msg and a newline to stdout.
func (*DefaultPrintProvider) Print(_ context.Context, msg string) {
	fmt.Fprintln(os.Stdout, msg)
}

// Warn writes msg and a newline to stderr.
func (*DefaultPrintProvider) Warn(_ context.Context, msg string) {
	fmt.Fprintln(os.Stderr, msg)
}

// ---------------------------------------------------------------------------
// TimeProvider — wall clock and monotonic time
// ---------------------------------------------------------------------------

// TimeProvider supplies the notion of "now" to Date and performance.now. Gating
// it lets an embedder present a fixed or virtual clock for deterministic tests.
type TimeProvider interface {
	// Now returns the current wall-clock time (backs Date.now and new Date()).
	Now(ctx context.Context) time.Time
	// Monotonic returns a monotonically increasing millisecond timestamp for
	// performance.now(); the zero point is arbitrary.
	Monotonic(ctx context.Context) float64
}

// DefaultTimeProvider reads the host clock via the time package.
type DefaultTimeProvider struct {
	start time.Time
}

// NewDefaultTimeProvider returns a TimeProvider backed by the host clock.
func NewDefaultTimeProvider() *DefaultTimeProvider {
	return &DefaultTimeProvider{start: time.Now()}
}

// Now returns the current host time.
func (*DefaultTimeProvider) Now(context.Context) time.Time { return time.Now() }

// Monotonic returns milliseconds elapsed since the provider was created.
func (p *DefaultTimeProvider) Monotonic(context.Context) float64 {
	return float64(time.Since(p.start).Nanoseconds()) / 1e6
}

// ---------------------------------------------------------------------------
// TimerProvider — deferred and repeating callbacks
// ---------------------------------------------------------------------------

// TimerProvider backs setTimeout/setInterval/setImmediate. It gates the ability
// of a script to schedule future work (and thereby to keep the process alive).
// The interpreter guarantees fn runs on its own event-loop goroutine, so
// implementations only need to arrange for fn to be invoked after the delay.
type TimerProvider interface {
	// AfterFunc arranges for fn to be called once after delay. The returned
	// cancel function stops the timer if it has not yet fired. Implementations
	// should stop the timer when ctx is cancelled.
	AfterFunc(ctx context.Context, delay time.Duration, fn func()) (cancel func())
}

// DefaultTimerProvider schedules callbacks with time.AfterFunc.
type DefaultTimerProvider struct{}

// NewDefaultTimerProvider returns a TimerProvider backed by time.AfterFunc.
func NewDefaultTimerProvider() *DefaultTimerProvider { return &DefaultTimerProvider{} }

// AfterFunc schedules fn using a runtime timer.
func (*DefaultTimerProvider) AfterFunc(_ context.Context, delay time.Duration, fn func()) func() {
	t := time.AfterFunc(delay, fn)
	return func() { t.Stop() }
}
