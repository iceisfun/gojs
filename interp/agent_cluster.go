package interp

import "sync"

// An "agent cluster" (§9.7) is a set of agents — here, gojs Interpreters running
// on separate goroutines — that can share memory through a SharedArrayBuffer and
// coordinate through Atomics.wait/notify. Each agent has its own heap and runs
// its own event loop; the ONLY state they share is a SharedArrayBuffer's backing
// bytes and the Atomics waiter registry.
//
// AgentCluster carries exactly that shared, engine-level state:
//
//   - waiters: one waiter registry for the whole cluster, so an Atomics.notify in
//     one agent wakes an Atomics.wait/waitAsync parked in another. Because a
//     waiterKey is keyed by the shared *arrayBufferData (whose pointer every
//     agent holds identically for the same SAB) plus the element index, a single
//     shared registry Just Works across agents.
//   - atomicsMu: a big lock serialising Atomics read-modify-write on shared
//     memory so a cross-agent RMW (add, compareExchange, exchange, ...) is
//     indivisible. It is taken only for shared buffers in a cluster, so a
//     single-agent VM pays nothing.
//
// Host-level agent plumbing (the $262.agent report queue, broadcast, and
// start/receiveBroadcast rendezvous) is deliberately NOT here: that is embedder
// policy, built on top of this engine primitive by whoever installs the host
// object (see the Test262 runner's installAgentHost).
type AgentCluster struct {
	waiters   *waiterList
	atomicsMu sync.Mutex
}

// NewAgentCluster creates an empty cluster. Attach it to each participating
// Interpreter with WithAgentCluster; interpreters sharing one AgentCluster form
// one agent cluster.
func NewAgentCluster() *AgentCluster {
	return &AgentCluster{waiters: &waiterList{m: map[waiterKey][]*sabWaiter{}}}
}

// WithAgentCluster attaches i to a shared agent cluster so its Atomics.wait /
// notify / waitAsync coordinate with the other agents in the cluster.
func WithAgentCluster(c *AgentCluster) Option {
	return func(i *Interpreter) {
		i.cluster = c
		i.sabWaiters = c.waiters
	}
}

// SharedBacking is an opaque handle to a SharedArrayBuffer's backing store. It
// lets an embedder hand the SAME bytes to another agent: extract it from a SAB
// value in one agent with SharedBackingOf, then rewrap it in another agent with
// NewSharedArrayBufferFromBacking. The wrapper objects differ per realm; the
// bytes are one block.
type SharedBacking struct{ ab *arrayBufferData }

// SharedBackingOf returns the shared backing of a SharedArrayBuffer value, or
// ok=false if v is not a SharedArrayBuffer.
func SharedBackingOf(v Value) (SharedBacking, bool) {
	ab, ok := arrayBufferOf(v)
	if !ok || !ab.shared {
		return SharedBacking{}, false
	}
	return SharedBacking{ab}, true
}

// NewSharedArrayBufferFromBacking wraps an existing shared backing in a fresh
// SharedArrayBuffer object in i's realm — same bytes, new wrapper object. Used
// to deliver a broadcast SAB into a receiving agent.
func (i *Interpreter) NewSharedArrayBufferFromBacking(b SharedBacking) Value {
	obj := NewObject(i.sharedArrayBufferProto)
	obj.class = "SharedArrayBuffer"
	obj.internal = map[string]any{"ArrayBuffer": b.ab}
	return obj
}

// atomicsLock takes the cluster's Atomics lock when td's buffer is shared and
// this interpreter is part of a cluster, returning the unlock function to defer.
// For a non-shared buffer or a single-agent VM it is a no-op. Callers MUST have
// finished all user-code coercions before calling this — the lock must never be
// held across code that could re-enter the interpreter.
func (i *Interpreter) atomicsLock(td *typedArrayData) func() {
	if i.cluster == nil {
		return func() {}
	}
	ab, ok := arrayBufferOf(td.buffer)
	if !ok || !ab.shared {
		return func() {}
	}
	i.cluster.atomicsMu.Lock()
	return i.cluster.atomicsMu.Unlock
}
