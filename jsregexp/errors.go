// Package jsregexp implements an ECMAScript-conformant regular-expression
// engine in pure Go. It is deliberately independent of the gojs interpreter:
// it parses a pattern and flags into an immutable AST (this file group), lowers
// that AST to a backtracking bytecode program, and executes it with a matcher
// bounded by a caller-supplied step budget (so untrusted patterns cannot hang
// the host — the ReDoS guarantee RE2 gave us, without RE2's inability to
// express backreferences and lookaround).
//
// The grammar and semantics follow ECMA-262 §22.2 (RegExp). Flag-dependent
// behavior (the u and v Unicode modes in particular) is threaded through the
// parser rather than approximated.
package jsregexp

import "strconv"

// SyntaxError is returned for any pattern or flags string that is not a valid
// ECMAScript regular expression. The interpreter maps this to a JavaScript
// SyntaxError. Msg is a human-readable reason; Pos is the rune offset into the
// pattern where the error was detected, or -1 when not applicable (e.g. a bad
// flags string).
type SyntaxError struct {
	Msg string
	Pos int
}

func (e *SyntaxError) Error() string {
	if e.Pos < 0 {
		return "Invalid regular expression: " + e.Msg
	}
	return "Invalid regular expression: " + e.Msg + " at offset " + strconv.Itoa(e.Pos)
}

// errAt is a small constructor used throughout the parser.
func errAt(pos int, msg string) *SyntaxError { return &SyntaxError{Msg: msg, Pos: pos} }
