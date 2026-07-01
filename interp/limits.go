package interp

// Limits bounds the resources a script may consume, so a host can run untrusted
// or buggy code without risking a crash or an unbounded loop. It mirrors the
// spirit of golua's vm.Limits, adapted to a tree-walking JavaScript engine.
//
// A zero value for a field means "use the default"; set a field explicitly to
// override. Apply limits at construction with [WithLimits] or at runtime with
// [Interpreter.SetLimits].
type Limits struct {
	// MaxCallDepth bounds nested JavaScript function invocations. Exceeding it
	// raises a catchable RangeError ("Maximum call stack size exceeded"), just
	// as engines do for runaway recursion. It also keeps recursion well below
	// the point at which the Go goroutine stack would overflow.
	// Default: 6000. A negative value disables the limit (not recommended).
	MaxCallDepth int

	// MaxSteps bounds the number of evaluation steps (statements executed, loop
	// iterations, and function entries). Exceeding it aborts execution with an
	// uncatchable [*LimitError] — try/catch cannot swallow it — so a tight loop
	// like `while (true) {}` terminates deterministically even without a context
	// deadline. Default: 0, meaning unlimited (rely on the context deadline).
	MaxSteps int64
}

// defaultLimits returns the limits applied when none are configured.
func defaultLimits() Limits {
	return Limits{MaxCallDepth: 6000, MaxSteps: 0}
}

// WithLimits sets the resource limits for the interpreter.
func WithLimits(l Limits) Option {
	return func(i *Interpreter) { i.applyLimits(l) }
}

// SetLimits updates the resource limits at runtime.
func (i *Interpreter) SetLimits(l Limits) { i.applyLimits(l) }

// applyLimits merges l over the current limits, treating zero fields as
// "keep the existing/default value".
func (i *Interpreter) applyLimits(l Limits) {
	if l.MaxCallDepth != 0 {
		i.limits.MaxCallDepth = l.MaxCallDepth
	}
	if l.MaxSteps != 0 {
		i.limits.MaxSteps = l.MaxSteps
	}
}

// LimitError is returned when an execution resource limit is exceeded. Unlike a
// JavaScript exception it is NOT a thrown value: try/catch cannot catch it and
// it unwinds all the way out of RunString, guaranteeing the script stops.
type LimitError struct {
	// Kind identifies the limit that tripped ("steps").
	Kind string
	Msg  string
}

// Error implements the error interface.
func (e *LimitError) Error() string { return e.Msg }

// step accounts for one unit of evaluation work and enforces MaxSteps. It is
// called at coarse checkpoints (each statement, loop iteration, and call), which
// is enough to bound CPU for the loops and recursion that untrusted code uses to
// burn time.
func (i *Interpreter) step() error {
	if i.limits.MaxSteps == 0 {
		return nil
	}
	i.steps++
	if i.steps > i.limits.MaxSteps {
		return &LimitError{Kind: "steps", Msg: "execution step limit exceeded"}
	}
	return nil
}
