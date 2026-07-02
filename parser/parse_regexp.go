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
	f, err := jsregexp.ParseFlags(flags)
	if err != nil {
		return err
	}
	_, err = jsregexp.Parse(pattern, f)
	return err
}
