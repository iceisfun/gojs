package test262

import (
	"context"
	"sync"
	"time"

	"github.com/iceisfun/gojs/interp"
)

// This file implements the $262.agent host API (Test262 INTERPRETING.md) that
// drives multi-agent SharedArrayBuffer / Atomics tests. An "agent" is a separate
// gojs Interpreter running on its own goroutine; agents share memory through a
// SharedArrayBuffer and coordinate through Atomics.wait/notify. The engine-level
// shared state (the cross-agent Atomics waiter registry and the Atomics big
// lock) lives in an interp.AgentCluster; everything here is the host-policy
// plumbing layered on top: a report FIFO, a broadcast rendezvous, and the child
// factory that spawns new agents.
//
// Only the primitive members are provided here; the richer helpers the tests use
// (safeBroadcast, waitUntil, tryYield, getReportAsync, timeouts) are defined in
// pure JS by harness/atomicsHelper.js on top of these primitives.

// agentHost is the shared state for one test's agent cluster. It is created in
// the parent (main) agent and shared by pointer with every child agent it
// starts.
type agentHost struct {
	cluster *interp.AgentCluster
	ctx     context.Context    // derived from the test deadline; cancelled by drain
	cancel  context.CancelFunc // unblocks parked children at teardown
	start0  time.Time          // epoch for monotonicNow

	mu      sync.Mutex
	reports []string // FIFO of report() messages, drained by getReport()

	recvMu    sync.Mutex
	receivers []chan interp.SharedBacking // one per started child, fed by broadcast()

	wg sync.WaitGroup // running child agents, awaited by drain()
}

func newAgentHost(parent context.Context) *agentHost {
	ctx, cancel := context.WithCancel(parent)
	return &agentHost{
		cluster: interp.NewAgentCluster(),
		ctx:     ctx,
		cancel:  cancel,
		start0:  time.Now(),
	}
}

// report records a message for the parent to collect via getReport.
func (h *agentHost) report(msg string) {
	h.mu.Lock()
	h.reports = append(h.reports, msg)
	h.mu.Unlock()
}

// getReport pops the oldest reported message; ok is false when none are queued
// (the JS wrapper then returns null and the test spins).
func (h *agentHost) getReport() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.reports) == 0 {
		return "", false
	}
	s := h.reports[0]
	h.reports = h.reports[1:]
	return s, true
}

// broadcast delivers a shared backing to every started agent's receiver channel.
// Each receiver is buffered (size 1), so this never blocks waiting for a child to
// reach receiveBroadcast; the child picks it up when it does.
func (h *agentHost) broadcast(b interp.SharedBacking) {
	h.recvMu.Lock()
	recv := append([]chan interp.SharedBacking(nil), h.receivers...)
	h.recvMu.Unlock()
	for _, ch := range recv {
		select {
		case ch <- b:
		case <-h.ctx.Done():
			return
		default:
			// Receiver already holds a pending broadcast (buffer full); a second
			// broadcast to the same agent is unusual in the corpus — drop it rather
			// than block.
		}
	}
}

// start registers a fresh receiver for a new child agent, then launches the child
// on its own goroutine running scriptSrc. newChild builds a child Interpreter
// wired to this host and the given receiver.
func (h *agentHost) start(scriptSrc string, newChild func(rc chan interp.SharedBacking) *interp.Interpreter) {
	rc := make(chan interp.SharedBacking, 1)
	h.recvMu.Lock()
	h.receivers = append(h.receivers, rc)
	h.recvMu.Unlock()

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		child := newChild(rc)
		defer child.Close()
		// A child's uncaught error is not a test failure by itself — the parent
		// observes success/failure through the shared memory and reports — so run
		// it best-effort and let the parent's assertions be the judge.
		_, _ = child.RunString("<agent>", scriptSrc)
	}()
}

// drain tears the cluster down at the end of a test: it cancels the host context
// (waking any child parked in Atomics.wait or receiveBroadcast), then waits for
// the child goroutines to exit, with a hard cap so a stuck agent leaks rather
// than hanging the whole suite.
func (h *agentHost) drain() {
	h.cancel()
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// installAgentHost installs the $262.agent object onto an existing $262 host.
// selfRecv is this agent's broadcast receiver (nil for the parent, which never
// receives). host is the $262 object returned by installT262Host.
func installAgentHost(vm *interp.Interpreter, h *agentHost, selfRecv chan interp.SharedBacking, host *interp.Object) {
	agent := vm.NewPlainObject()

	// newChild builds a child agent: a fresh Interpreter joined to the same
	// cluster and host, with $262 and $262.agent installed and its own receiver.
	newChild := func(rc chan interp.SharedBacking) *interp.Interpreter {
		child := interp.New(
			interp.WithContext(h.ctx),
			interp.WithTimeProvider(interp.NewDefaultTimeProvider()),
			interp.WithTimerProvider(interp.NewDefaultTimerProvider()),
			interp.WithAgentCluster(h.cluster),
		)
		childHost := installT262Host(child)
		installAgentHost(child, h, rc, childHost)
		return child
	}

	// start(scriptSource): spawn a new agent running scriptSource.
	agent.SetData("start", vm.NewFunction("start", func(args []interp.Value) (interp.Value, error) {
		src, err := vm.ToString(agentArg(args, 0))
		if err != nil {
			return interp.Undef, err
		}
		h.start(src, newChild)
		return interp.Undef, nil
	}))

	// broadcast(sab): send a SharedArrayBuffer to every started agent. Returns
	// undefined; the async helpers await it (await on a non-promise is a no-op).
	agent.SetData("broadcast", vm.NewFunction("broadcast", func(args []interp.Value) (interp.Value, error) {
		if len(args) > 0 {
			if b, ok := interp.SharedBackingOf(args[0]); ok {
				h.broadcast(b)
			}
		}
		return interp.Undef, nil
	}))

	// getReport(): the oldest message reported by any agent, or null if none.
	agent.SetData("getReport", vm.NewFunction("getReport", func(args []interp.Value) (interp.Value, error) {
		if s, ok := h.getReport(); ok {
			return interp.String(s), nil
		}
		return interp.Nul, nil
	}))

	// report(msg): (child) report a string back to the parent.
	agent.SetData("report", vm.NewFunction("report", func(args []interp.Value) (interp.Value, error) {
		s, err := vm.ToString(agentArg(args, 0))
		if err != nil {
			return interp.Undef, err
		}
		h.report(s)
		return interp.Undef, nil
	}))

	// sleep(ms): block this agent for ms milliseconds (or until teardown).
	agent.SetData("sleep", vm.NewFunction("sleep", func(args []interp.Value) (interp.Value, error) {
		ms := agentNum(vm, agentArg(args, 0))
		if ms > 0 {
			t := time.NewTimer(time.Duration(ms) * time.Millisecond)
			defer t.Stop()
			select {
			case <-t.C:
			case <-vm.Context().Done():
			}
		}
		return interp.Undef, nil
	}))

	// monotonicNow(): a monotonically non-decreasing timestamp in milliseconds.
	agent.SetData("monotonicNow", vm.NewFunction("monotonicNow", func(args []interp.Value) (interp.Value, error) {
		return interp.Number(float64(time.Since(h.start0).Nanoseconds()) / 1e6), nil
	}))

	// leaving(): (child) a hint that the agent is done; nothing to do here.
	agent.SetData("leaving", vm.NewFunction("leaving", func(args []interp.Value) (interp.Value, error) {
		return interp.Undef, nil
	}))

	// receiveBroadcast(fn): (child) block until the parent broadcasts a SAB, then
	// call fn with a SharedArrayBuffer over the same bytes in this agent's realm.
	if selfRecv != nil {
		agent.SetData("receiveBroadcast", vm.NewFunction("receiveBroadcast", func(args []interp.Value) (interp.Value, error) {
			fn := agentArg(args, 0)
			var b interp.SharedBacking
			select {
			case b = <-selfRecv:
			case <-vm.Context().Done():
				return interp.Undef, nil
			}
			sab := vm.NewSharedArrayBufferFromBacking(b)
			_, err := vm.Call(fn, interp.Undef, sab)
			return interp.Undef, err
		}))
	}

	host.SetData("agent", agent)
}

// agentArg returns args[i] or undefined.
func agentArg(args []interp.Value, i int) interp.Value {
	if i < len(args) {
		return args[i]
	}
	return interp.Undef
}

// agentNum coerces v to a float64 for the agent API's numeric arguments (sleep
// milliseconds). Non-numeric or error coercions yield 0.
func agentNum(vm *interp.Interpreter, v interp.Value) float64 {
	if f, ok := vm.ToGo(v).(float64); ok {
		return f
	}
	return 0
}
