// Package parser implements a recursive-descent parser for JavaScript
// (ECMAScript), producing an [ast.Program] from source text.
//
// # Design
//
// The parser first tokenizes the entire input into a slice via the [lexer]
// package, then walks that slice with an index cursor. Buffering the whole
// token stream (rather than pulling one token at a time) makes arbitrary
// lookahead cheap, which the JavaScript grammar needs in several places — most
// notably to distinguish an arrow function `(a, b) => …` from a parenthesized
// expression `(a, b)`, and to tell a destructuring pattern from an object or
// array literal.
//
// Expressions are parsed with precedence climbing (a compact form of Pratt
// parsing); see parse_expr.go. Statements are parsed by recursive descent; see
// parse_stmt.go.
//
// # Error handling
//
// The first error encountered stops the parse and is returned. Errors carry a
// [token.Span] so callers can report a precise source range. Automatic
// Semicolon Insertion (ECMA-262 §12.10) is implemented in expectSemicolon.
//
// ECMA-262 Reference: §13–§16.
package parser

import (
	"fmt"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/lexer"
	"github.com/iceisfun/gojs/token"
)

// maxDepth bounds recursive-descent nesting to protect against stack overflow
// on pathologically nested input.
const maxDepth = 1000

// parser holds the state for a single parse.
type parser struct {
	source string
	toks   []token.Token // fully buffered token stream (always ends with EOF)
	idx    int           // cursor into toks
	err    *token.SyntaxError
	depth  int // current recursion depth

	// noIn disables the `in` operator while parsing the header of a for
	// statement, so `for (x in y)` is not mis-parsed as a relational expr.
	noIn bool
	// inFunction/inLoop/inSwitch track context for validating return, break,
	// and continue. They are simple counters saved/restored across boundaries.
	inFunction int
	inLoop     int
	inSwitch   int
	// classDepth is the class-body nesting depth; private names (#x) are only
	// valid where it is > 0.
	classDepth int
	// privateEnvStack holds one entry per class body currently being parsed,
	// each recording the private names declared in that class. privateRefs
	// records every private-name reference with a snapshot of its enclosing
	// class environments. Because a private name is visible throughout its
	// class — including before its textual declaration and in nested classes —
	// references cannot be resolved until every declaration is known, so they
	// are validated once at the end of the parse (see checkPrivateRefs).
	privateEnvStack []*privateEnv
	privateRefs     []privateRef
	// inFieldInit is true while parsing a class field initializer. A field
	// initializer may not contain `arguments` or a SuperCall (super(...)). It is
	// transparent through arrow functions but reset at a regular function or a
	// method boundary, which introduce their own arguments/super scope.
	inFieldInit bool
	// inGenerator/inAsync track whether the function whose parameters or body is
	// currently being parsed is a generator or async. They gate the reserved-word
	// treatment of `yield` (in a generator) and `await` (in an async function),
	// which may not be used as binding identifiers there. Both are reset at every
	// function boundary to that function's own kind.
	inGenerator bool
	inAsync     bool
	// superCallOK is true only while parsing the body of a derived class
	// constructor, where a SuperCall (super(...)) is allowed. It is transparent
	// through arrow functions but reset at every other function/method boundary
	// and at a class body. classHeritage records whether the class currently
	// being parsed has an extends clause; pendingSuperCall carries the
	// permission from a constructor member to its method body.
	superCallOK      bool
	classHeritage    bool
	pendingSuperCall bool
	// superPropOK/newTargetOK gate super.property and new.target. They default
	// to true for ordinary parsing (which relies on runtime checks); ParseEval
	// sets them from the caller so indirect/global eval rejects super and
	// new.target while a direct eval in a method or function keeps them.
	superPropOK bool
	newTargetOK bool
	// strict reports whether the code currently being parsed is strict-mode
	// code. It is set by a "use strict" directive prologue (at the program or
	// function level), inherited into nested functions, and always true inside a
	// class body. Several early errors are strict-mode sensitive (e.g. duplicate
	// block-level FunctionDeclarations, a FunctionDeclaration in a
	// single-statement position under Annex B, binding `eval`/`arguments`, or a
	// LegacyOctalIntegerLiteral / octal string escape).
	strict bool
}

// isContextualKeyword reports whether t is a soft/contextual keyword (let,
// static, yield, async, await, of, get, set) that may legally serve as a
// binding identifier or plain identifier. Reserved words cannot.
func isContextualKeyword(t token.Type) bool {
	return t >= token.LET && t <= token.SET
}

// Parse parses source into a [*ast.Program]. sourceName is used in error
// messages and stored on the program.
func Parse(sourceName, source string) (*ast.Program, error) {
	p, err := newParser(sourceName, source)
	if err != nil {
		return nil, err
	}
	return p.parseProgram()
}

// EvalContext carries the surrounding lexical context of a direct eval so its
// code is parsed under the same early-error rules: the private names in scope
// (so `this.#x` resolves), whether a SuperCall is permitted (inside a derived
// constructor), and whether the caller is strict-mode code.
type EvalContext struct {
	Strict             bool
	AllowSuperCall     bool
	AllowSuperProperty bool
	AllowNewTarget     bool
	PrivateNames       []string
}

// ParseEval parses the source of a direct eval, seeding the surrounding
// context so context-sensitive early errors match the call site.
func ParseEval(sourceName, source string, ec EvalContext) (*ast.Program, error) {
	p, err := newParser(sourceName, source)
	if err != nil {
		return nil, err
	}
	p.strict = ec.Strict
	p.superCallOK = ec.AllowSuperCall
	p.superPropOK = ec.AllowSuperProperty
	p.newTargetOK = ec.AllowNewTarget
	if len(ec.PrivateNames) > 0 {
		env := &privateEnv{declared: make(map[string]bool, len(ec.PrivateNames))}
		for _, n := range ec.PrivateNames {
			env.declared[n] = true
		}
		p.privateEnvStack = append(p.privateEnvStack, env)
	}
	return p.parseProgram()
}

// newParser tokenizes source and returns a ready parser, or a lexical error.
func newParser(sourceName, source string) (*parser, error) {
	lex := lexer.New(sourceName, source, true)
	var toks []token.Token
	for {
		tk := lex.Next()
		toks = append(toks, tk)
		if tk.Type == token.EOF {
			break
		}
	}
	if err := lex.Err(); err != nil {
		return nil, err
	}
	// super.property and new.target default to permitted; only eval restricts
	// them (see ParseEval).
	return &parser{source: sourceName, toks: toks, superPropOK: true, newTargetOK: true}, nil
}

// ---------------------------------------------------------------------------
// Cursor primitives
// ---------------------------------------------------------------------------

// cur returns the current token.
func (p *parser) cur() token.Token { return p.toks[p.idx] }

// peek returns the token n positions ahead of the cursor (peek(0) == cur),
// clamped to the trailing EOF.
func (p *parser) peek(n int) token.Token {
	i := p.idx + n
	if i >= len(p.toks) {
		return p.toks[len(p.toks)-1] // EOF
	}
	return p.toks[i]
}

// at reports whether the current token is of type t.
func (p *parser) at(t token.Type) bool { return p.cur().Type == t }

// next advances the cursor and returns the token that was current.
func (p *parser) next() token.Token {
	tk := p.toks[p.idx]
	if p.idx < len(p.toks)-1 {
		p.idx++
	}
	return tk
}

// accept consumes and reports whether the current token is of type t.
func (p *parser) accept(t token.Type) bool {
	if p.at(t) {
		p.next()
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// errorf records a syntax error at the current token (only the first is kept).
func (p *parser) errorf(format string, args ...any) {
	if p.err == nil {
		p.err = &token.SyntaxError{Pos: p.cur().Pos, Msg: fmt.Sprintf(format, args...)}
	}
}

// errorAt records a syntax error at a specific position.
func (p *parser) errorAt(pos token.Pos, format string, args ...any) {
	if p.err == nil {
		p.err = &token.SyntaxError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
	}
}

// expect consumes a token of type t, or records an error and returns the
// current (unconsumed) token.
func (p *parser) expect(t token.Type) token.Token {
	if p.at(t) {
		return p.next()
	}
	p.errorf("expected %s but got %s", t, p.cur().Type)
	return p.cur()
}

// enter increments the recursion-depth guard, recording an error if the limit
// is exceeded. Callers pair it with a deferred leave.
func (p *parser) enter() bool {
	p.depth++
	if p.depth > maxDepth {
		p.errorf("maximum nesting depth exceeded")
		return false
	}
	return true
}

func (p *parser) leave() { p.depth-- }

// ---------------------------------------------------------------------------
// Automatic Semicolon Insertion
// ---------------------------------------------------------------------------

// expectSemicolon consumes a statement-terminating semicolon, applying the ASI
// rules: a semicolon may be omitted before '}', at end of input, or when the
// current token began on a new line.
func (p *parser) expectSemicolon() {
	if p.accept(token.SEMICOLON) {
		return
	}
	switch {
	case p.at(token.RBRACE), p.at(token.EOF):
		return
	case p.cur().NewlineBefore:
		return
	default:
		p.errorf("expected ';' but got %s", p.cur().Type)
	}
}

// ---------------------------------------------------------------------------
// Program
// ---------------------------------------------------------------------------

// parseProgram parses the whole token stream into a program node.
func (p *parser) parseProgram() (*ast.Program, error) {
	prog := &ast.Program{Source: p.source}

	// A "use strict" directive anywhere in the program's leading directive
	// prologue makes the whole program strict. Detect it up front (before
	// parsing any block) so strict-sensitive early errors are applied correctly.
	if p.scanUseStrict(p.idx) {
		p.strict = true
		prog.Strict = true
	}

	// Directive prologue: a leading run of string-literal expression
	// statements, one of which may be "use strict".
	for p.err == nil && !p.at(token.EOF) {
		stmt := p.parseStmt()
		if p.err != nil {
			break
		}
		if stmt == nil {
			continue
		}
		if es, ok := stmt.(*ast.ExprStmt); ok && es.Directive == "use strict" {
			prog.Strict = true
			// A strict script propagates into every nested function.
			p.strict = true
		}
		prog.Body = append(prog.Body, stmt)
	}
	prog.EndPos = p.cur().Pos
	// All private-name declarations are now known; validate every reference.
	p.checkPrivateRefs()
	if p.err != nil {
		return nil, p.err
	}
	return prog, nil
}
