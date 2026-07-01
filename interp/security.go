package interp

// Security collects opt-in hardening switches for an interpreter. They restrict
// language and runtime features that are common footguns or escape hatches in
// an embedded/sandboxed setting. All default to false (feature enabled); set a
// field to true to disable the corresponding capability.
//
// This mirrors golua's capability-gating philosophy, but because JavaScript's
// dangerous surfaces are language features (not just host APIs), the knobs live
// in one struct applied at construction via [WithSecurity].
type Security struct {
	// DisableProtoMutation blocks mutation of an object's prototype through the
	// __proto__ accessor and Object.setPrototypeOf / Reflect.setPrototypeOf.
	// Prototype pollution is a frequent sandbox-escape and gadget vector.
	DisableProtoMutation bool

	// DisableEval makes the global eval() throw instead of executing code from
	// a string. (gojs does not implement dynamic eval regardless; this makes
	// the refusal explicit and observable.)
	DisableEval bool

	// DisableFunctionCtor makes the Function constructor throw, preventing
	// construction of functions from strings (another dynamic-code path).
	DisableFunctionCtor bool

	// DisableWith rejects the `with` statement at parse/eval time. gojs does
	// not support `with` at all, so this is always effectively on; the flag is
	// retained for parity and forward compatibility.
	DisableWith bool

	// StrictModulesOnly forces every program to be evaluated in strict mode,
	// regardless of a "use strict" directive.
	StrictModulesOnly bool
}

// WithSecurity applies the given hardening options to the interpreter.
func WithSecurity(s Security) Option {
	return func(i *Interpreter) { i.security = s }
}

// forceStrict reports whether all code must run in strict mode.
func (i *Interpreter) forceStrict() bool { return i.security.StrictModulesOnly }
