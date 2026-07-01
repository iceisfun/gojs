package interp

// This file defines the error and control-flow signal types used throughout
// evaluation. Evaluation functions return a Go error whose concrete type may
// be one of:
//
//   - *Throw          — a JavaScript exception carrying a thrown Value
//   - *returnSignal   — unwinds to the enclosing function (return statement)
//   - *breakSignal    — unwinds to the enclosing/labeled loop or switch
//   - *continueSignal — restarts the enclosing/labeled loop
//   - any other error — a host-side Go error, surfaced to the embedder
//
// Using typed errors (rather than panic/recover) keeps control flow explicit
// and cheap, and lets native functions participate by returning errors.

// Throw is a JavaScript exception. Value is the thrown value (commonly an Error
// object, but any value may be thrown).
type Throw struct {
	Value Value
}

// Error implements the error interface with a short, host-facing message. The
// full JS value is available via Value.
func (t *Throw) Error() string {
	return "uncaught " + briefValue(t.Value)
}

// NewThrow wraps a value as a throwable error.
func NewThrow(v Value) *Throw { return &Throw{Value: v} }

// BriefValue renders a value for host-facing display (e.g. printing an uncaught
// exception). It does not run user toString methods; for full formatting use an
// Interpreter's ToStringV.
func BriefValue(v Value) string { return briefValue(v) }

// returnSignal unwinds evaluation to the nearest enclosing function call.
type returnSignal struct {
	value Value
}

func (*returnSignal) Error() string { return "return outside function" }

// breakSignal unwinds to the nearest enclosing loop/switch, or to the loop
// carrying the matching label.
type breakSignal struct {
	label string // empty for an unlabeled break
}

func (*breakSignal) Error() string { return "break outside loop" }

// continueSignal restarts the nearest enclosing loop, or the loop carrying the
// matching label.
type continueSignal struct {
	label string // empty for an unlabeled continue
}

func (*continueSignal) Error() string { return "continue outside loop" }

// isSignal reports whether err is one of the internal control-flow signals
// (rather than a Throw or host error).
func isSignal(err error) bool {
	switch err.(type) {
	case *returnSignal, *breakSignal, *continueSignal:
		return true
	}
	return false
}

// ownDataOnChain returns the value of a data property found on obj or its
// prototype chain, without invoking accessors or a context.
func ownDataOnChain(obj *Object, name string) (Value, bool) {
	for cur := obj; cur != nil; cur = cur.proto {
		if p, ok := cur.props[StrKey(name)]; ok && !p.Accessor {
			return p.Value, true
		}
	}
	return nil, false
}

// briefValue renders a value for a host-facing Go error string without needing
// a context. It is intentionally simple; user-facing formatting goes through
// the interpreter's ToString.
func briefValue(v Value) string {
	switch x := v.(type) {
	case String:
		return string(x)
	case Number:
		return NumberToString(float64(x))
	case Boolean:
		if bool(x) {
			return "true"
		}
		return "false"
	case Undefined:
		return "undefined"
	case Null:
		return "null"
	case *Object:
		// Look up name/message as data properties along the prototype chain
		// (Error instances inherit "name" from their prototype).
		if name, ok := ownDataOnChain(x, "name"); ok {
			msg := ""
			if m, ok := ownDataOnChain(x, "message"); ok {
				if s, ok := m.(String); ok {
					msg = string(s)
				}
			}
			if ns, ok := name.(String); ok {
				if msg != "" {
					return string(ns) + ": " + msg
				}
				return string(ns)
			}
		}
		return "[object " + x.class + "]"
	default:
		return "value"
	}
}
