package interp

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
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

// ---------------------------------------------------------------------------
// OsProvider — operating-system facilities (environment, cwd, exit, identity)
// ---------------------------------------------------------------------------

// OsProvider gates a script's access to the host operating system: the
// environment, the working directory, process termination, and host identity
// (platform / architecture / pid). Without one, none of these exist — a script
// cannot read an env var, learn what OS it is on, or terminate the process. This
// is the wall that keeps an embedded VM (a game server, a plugin host) from
// leaking or touching the host: install a provider to grant exactly what you
// intend, and route it wherever you like. Environment visibility is further
// narrowable with NewFilteredOsProvider.
type OsProvider interface {
	// Getenv returns the value of the named environment variable and whether it
	// is visible. A filtered provider reports (", false) for hidden names.
	Getenv(ctx context.Context, name string) (string, bool)
	// Environ returns the full set of visible environment variables.
	Environ(ctx context.Context) map[string]string
	// Cwd returns the current working directory.
	Cwd(ctx context.Context) (string, error)
	// Exit terminates the program with the given status code. An embedder that
	// must not let a script kill the host should implement this to record the
	// code and cancel the VM's context instead of calling os.Exit.
	Exit(ctx context.Context, code int)
	// Platform is the host OS as Node names it ("linux", "darwin", "win32", …).
	Platform() string
	// Arch is the host architecture as Node names it ("x64", "arm64", …).
	Arch() string
	// Pid is the process id.
	Pid() int
}

// DefaultOsProvider grants ordinary host OS access, optionally filtering which
// environment variables are visible.
type DefaultOsProvider struct {
	envFilter func(name string) bool // nil admits every variable
}

// NewDefaultOsProvider returns an OsProvider backed by the real OS, with the
// full environment visible.
func NewDefaultOsProvider() *DefaultOsProvider { return &DefaultOsProvider{} }

// NewFilteredOsProvider is like NewDefaultOsProvider but only exposes the
// environment variables for which allow(name) is true; all others are invisible
// (Getenv reports not-present and they are omitted from Environ). Everything
// else — cwd, exit, platform, arch, pid — behaves as the default.
func NewFilteredOsProvider(allow func(name string) bool) *DefaultOsProvider {
	return &DefaultOsProvider{envFilter: allow}
}

func (p *DefaultOsProvider) allow(name string) bool {
	return p.envFilter == nil || p.envFilter(name)
}

// Getenv reports the value of name, subject to the environment filter.
func (p *DefaultOsProvider) Getenv(_ context.Context, name string) (string, bool) {
	if !p.allow(name) {
		return "", false
	}
	return os.LookupEnv(name)
}

// Environ returns every visible environment variable.
func (p *DefaultOsProvider) Environ(_ context.Context) map[string]string {
	m := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 && p.allow(kv[:i]) {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// Cwd returns the current working directory.
func (*DefaultOsProvider) Cwd(context.Context) (string, error) { return os.Getwd() }

// Exit terminates the program via os.Exit.
func (*DefaultOsProvider) Exit(_ context.Context, code int) { os.Exit(code) }

// Platform maps runtime.GOOS to Node's platform names.
func (*DefaultOsProvider) Platform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS // linux, darwin, freebsd, … already match Node
}

// Arch maps runtime.GOARCH to Node's architecture names.
func (*DefaultOsProvider) Arch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return runtime.GOARCH // arm64, arm, … already match Node
	}
}

// Pid returns the process id.
func (*DefaultOsProvider) Pid() int { return os.Getpid() }

// ---------------------------------------------------------------------------
// NetProvider — outbound network dialing (the single egress wall)
// ---------------------------------------------------------------------------

// NetProvider is the one choke point for outbound connections made by the
// networking host packages (host/fetch, host/sse, host/websocket). Because every
// dial — and therefore every DNS resolution — passes through DialContext, the
// host owns network egress: it can allowlist destinations, pin addresses, use a
// custom resolver or proxy, meter, log, or deny outright.
//
// It is opt-in and orthogonal to the per-package client/dialer options: when a
// NetProvider is installed, a net package that builds its own default client
// routes dialing through it; a client the host supplied explicitly (e.g.
// fetch.WithClient) is left untouched, since that is the host taking direct
// control. Without a NetProvider, packages dial normally — but they are opt-in to
// begin with, so nothing reaches the network unless the host installed them.
type NetProvider interface {
	// DialContext connects to addr ("host:port") over network ("tcp", "tcp4", …).
	// The signature matches net.Dialer.DialContext so it drops into
	// http.Transport.DialContext and direct dialers alike.
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// DefaultNetProvider is a pass-through NetProvider backed by net.Dialer — the
// "just enable the wall" default. Wrap or replace its DialContext to enforce a
// policy (allowlist, custom resolver, proxy, deny).
type DefaultNetProvider struct {
	// Dialer is the underlying dialer; the zero value is a plain net.Dialer.
	Dialer net.Dialer
}

// NewDefaultNetProvider returns a pass-through NetProvider that dials normally.
func NewDefaultNetProvider() *DefaultNetProvider { return &DefaultNetProvider{} }

// DialContext dials through the underlying net.Dialer.
func (p *DefaultNetProvider) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return p.Dialer.DialContext(ctx, network, addr)
}
