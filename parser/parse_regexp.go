package parser

import (
	"github.com/iceisfun/gojs/jsregexp"
)

// This file implements the parse-time early-error check for regular-expression
// literals. A RegularExpressionLiteral is an early SyntaxError if its flags are
// invalid/duplicated, or if its BodyText cannot be recognized using the goal
// symbol Pattern of the ECMAScript RegExp grammar (ECMA-262 §22.2). Both checks
// are grammar-defined and independent of which matching engine executes the
// pattern at runtime, so they are validated here.
//
// The validation delegates to the pure-Go jsregexp engine, which implements the
// full ECMAScript RegExp grammar (named groups and backreferences, u/v Unicode
// modes, inline modifiers, \p{...}, and Annex-B leniency in non-Unicode mode).

// validateRegexpLiteral reports an early error for a regular-expression literal,
// or nil when it is grammatically valid. flags is the raw flag string (the text
// following the closing '/').
func validateRegexpLiteral(pattern, flags string) error {
	// Compile (not just Parse) so the parse-time check matches everything the
	// runtime engine rejects — including compile-only early errors such as an
	// unknown \p{...} property or a reference to an undefined named group — which
	// §22.2 makes early SyntaxErrors of the literal.
	_, err := jsregexp.Compile(pattern, flags)
	return err
}
