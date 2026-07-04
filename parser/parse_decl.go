package parser

import (
	"math"
	"strconv"
	"strings"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements parsing of functions, arrow functions, classes, object
// literals, and binding patterns, plus numeric-literal decoding. These forms
// are shared between the expression and statement parsers.

// ---------------------------------------------------------------------------
// Numeric literals
// ---------------------------------------------------------------------------

// parseNumber decodes a numeric literal's source text into a float64. It
// understands decimal, hex (0x), octal (0o), binary (0b), exponent forms, and
// numeric separators ('_'). Invalid input yields NaN, which the lexer's own
// validation makes unreachable in practice.
func parseNumber(raw string) float64 {
	s := strings.ReplaceAll(raw, "_", "")
	if len(s) >= 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X':
			if v, err := strconv.ParseUint(s[2:], 16, 64); err == nil {
				return float64(v)
			}
			// Fall back to big-int-free parse for values above 64 bits.
			return parseRadix(s[2:], 16)
		case 'o', 'O':
			return parseRadix(s[2:], 8)
		case 'b', 'B':
			return parseRadix(s[2:], 2)
		}
		// LegacyOctalIntegerLiteral: a leading 0 followed only by octal digits
		// evaluates in base 8 (e.g. 0777 === 511). A leading zero followed by a
		// digit 8 or 9 (NonOctalDecimalIntegerLiteral, e.g. 08) falls through to
		// the decimal parse below.
		if isOctalDigits(s[1:]) {
			return parseRadix(s[1:], 8)
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err == nil {
		return v
	}
	// A DecimalLiteral whose magnitude overflows float64 (e.g. 10e10000) is not a
	// syntax error: it rounds to +Infinity (and a tiny magnitude rounds to 0).
	if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
		return v
	}
	return math.NaN()
}

// isOctalDigits reports whether s is non-empty and consists solely of octal
// digits (0-7).
func isOctalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '7' {
			return false
		}
	}
	return true
}

// parseRadix converts a digit string in the given base to a float64 without
// overflow (accumulating in float space for very large values).
func parseRadix(digits string, base int) float64 {
	var v float64
	for _, c := range digits {
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			continue
		}
		v = v*float64(base) + float64(d)
	}
	return v
}

// ---------------------------------------------------------------------------
// Arrow functions
// ---------------------------------------------------------------------------

// tryParseArrow detects and parses an arrow function starting at the current
// token, returning nil (without consuming input) when the upcoming tokens are
// not an arrow function.
func (p *parser) tryParseArrow() ast.Expr {
	start := p.cur()
	async := false
	base := 0

	if start.Type == token.ASYNC && !p.peek(1).NewlineBefore {
		switch p.peek(1).Type {
		case token.IDENT:
			// async x => …
			if p.peek(2).Type == token.ARROW && !p.peek(2).NewlineBefore {
				async = true
				base = 1
			} else {
				return nil
			}
		case token.LPAREN:
			async = true
			base = 1
		default:
			return nil
		}
	}

	switch {
	case p.peek(base).Type == token.IDENT || isContextualKeyword(p.peek(base).Type):
		// single-identifier arrow: x => … . A contextual keyword (yield, await,
		// let, of, get, set, static) may serve as the sole BindingIdentifier
		// parameter (`yield => 1` in sloppy code); its context-sensitive
		// reservation is enforced when the parameter is committed below.
		if p.peek(base+1).Type != token.ARROW || p.peek(base+1).NewlineBefore {
			return nil
		}
	case p.peek(base).Type == token.LPAREN:
		// (params) => … — verify the matching paren is followed by =>.
		close := p.matchParen(p.idx + base)
		if close < 0 {
			return nil
		}
		after := close + 1
		if after >= len(p.toks) || p.toks[after].Type != token.ARROW || p.toks[after].NewlineBefore {
			return nil
		}
	default:
		return nil
	}

	// Commit: this is an arrow function.
	if async {
		p.next() // async
	}
	arrow := &ast.ArrowFunc{Start: start.Pos, Async: async}
	// An arrow inherits [Yield]/[Await] from its context (it is transparent to
	// yield/await like it is to `this`), except that an async arrow is itself
	// async, so `await` is reserved within it.
	prevAsync := p.inAsync
	if async {
		p.inAsync = true
	}
	// ArrowParameters are still parsed with the enclosing [Await] parameter, so
	// the `await` reservation imposed by an enclosing class static initialization
	// block reaches into the arrow's *parameter list* (both a BindingIdentifier
	// named `await` and an `await` reference inside a default initializer are
	// early errors). The reservation is a function boundary only for the arrow's
	// *body* — a ConciseBody carries no [Await] parameter — so staticBlockAwait is
	// cleared just before the body below.
	prevStaticAwait := p.staticBlockAwait
	if p.at(token.IDENT) || isContextualKeyword(p.cur().Type) {
		id := p.next()
		name := identText(id)
		p.checkReservedIdentifier(name, id.Pos)
		p.checkStrictBindingName(name, id.Pos, p.strict)
		arrow.Params = []ast.Expr{&ast.Ident{NamePos: id.Pos, Name: name}}
	} else {
		arrow.Params = p.parseParams()
	}
	p.staticBlockAwait = false
	p.expect(token.ARROW)
	if p.at(token.LBRACE) {
		bodyUseStrict := p.scanUseStrict(p.idx + 1)
		arrow.Body, arrow.Strict = p.parseFunctionBody()
		p.checkStrictSimpleParams(start.Pos, bodyUseStrict, arrow.Params)
	} else {
		arrow.Expression = true
		// A concise body carries no directive prologue, so its strictness is
		// exactly the enclosing lexical context's.
		arrow.Strict = p.strict
		// A concise body is outside the parameter list.
		prevParams := p.inParams
		p.inParams = false
		arrow.Body = p.parseAssignExpr()
		p.inParams = prevParams
	}
	p.inAsync = prevAsync
	p.staticBlockAwait = prevStaticAwait
	// Arrow functions never permit duplicate parameter names.
	p.checkParamDuplicates(arrow.Params, true)
	// The strict BindingIdentifier restriction (no `eval`/`arguments`) applies
	// under the arrow's effective strictness, which a block body's own "use
	// strict" directive can turn on even in sloppy surrounding code.
	p.checkStrictParamNames(arrow.Params, arrow.Strict)
	if !arrow.Expression {
		p.checkParamBodyLexicalConflict(arrow.Params, arrow.Body.(*ast.BlockStmt))
	}
	return arrow
}

// matchParen returns the index of the RPAREN that matches the LPAREN at index
// openIdx, or -1 if unbalanced. It counts nesting across (), [], and {}.
func (p *parser) matchParen(openIdx int) int {
	depth := 0
	for i := openIdx; i < len(p.toks); i++ {
		switch p.toks[i].Type {
		case token.LPAREN, token.LBRACKET, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACKET, token.RBRACE:
			depth--
			if depth == 0 {
				if p.toks[i].Type == token.RPAREN {
					return i
				}
				return -1
			}
		case token.EOF:
			return -1
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Parameter lists and binding patterns
// ---------------------------------------------------------------------------

// checkStrictSimpleParams enforces the early error forbidding a "use strict"
// directive in the body of a function whose parameter list is not simple — i.e.
// contains a destructuring pattern, a default, or a rest element (ECMA-262
// 15.2.1: a Syntax Error if ContainsUseStrict of the body is true and
// IsSimpleParameterList of the parameters is false).
func (p *parser) checkStrictSimpleParams(pos token.Pos, bodyHasUseStrict bool, params []ast.Expr) {
	if bodyHasUseStrict && !simpleParamList(params) {
		p.errorAt(pos, "Illegal 'use strict' directive in function with non-simple parameter list")
	}
}

func simpleParamList(params []ast.Expr) bool {
	for _, p := range params {
		if _, ok := p.(*ast.Ident); !ok {
			return false
		}
	}
	return true
}

// paramBoundNames collects every identifier bound by a formal parameter list,
// recursing through defaults, rest elements, and destructuring patterns.
func paramBoundNames(params []ast.Expr) []string {
	var out []string
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch t := e.(type) {
		case *ast.Ident:
			out = append(out, t.Name)
		case *ast.RestElement:
			walk(t.Target)
		case *ast.AssignPattern:
			walk(t.Target)
		case *ast.SpreadElement:
			walk(t.Argument)
		case *ast.ArrayLit:
			for _, el := range t.Elements {
				if el != nil {
					walk(el)
				}
			}
		case *ast.ObjectLit:
			for _, pr := range t.Properties {
				if pr.Value != nil {
					walk(pr.Value)
				} else if pr.Key != nil {
					walk(pr.Key)
				}
			}
		}
	}
	for _, p := range params {
		walk(p)
	}
	return out
}

// checkStrictParamNames enforces the strict-mode early error that a formal
// parameter may not be bound to the name `eval` or `arguments` (ECMA-262
// BindingIdentifier static semantics in strict code / StrictFormalParameters).
func (p *parser) checkStrictParamNames(params []ast.Expr, strict bool) {
	if !strict || p.err != nil {
		return
	}
	for _, name := range paramBoundNames(params) {
		if name == "eval" || name == "arguments" {
			p.errorf("'%s' may not be used as a parameter name in strict mode", name)
			return
		}
	}
}

// checkParamDuplicates enforces the early error for duplicate parameter names.
// Duplicates are permitted only in a sloppy-mode function whose parameter list
// is simple (identifiers only); a strict-mode function or any non-simple list
// (defaults, rest, or destructuring) makes a repeated binding a SyntaxError.
func (p *parser) checkParamDuplicates(params []ast.Expr, strict bool) {
	if p.err != nil {
		return
	}
	if !strict && simpleParamList(params) {
		return
	}
	seen := map[string]bool{}
	for _, name := range paramBoundNames(params) {
		if seen[name] {
			p.errorf("duplicate parameter name '%s' not allowed in this context", name)
			return
		}
		seen[name] = true
	}
}

// checkParamBodyLexicalConflict enforces the early error that a name bound by a
// function's FormalParameters may not also be lexically declared at the top
// level of its FunctionBody (ECMA-262 §15.2.1 and the analogous method/generator
// static semantics: BoundNames of the parameters ∩ LexicallyDeclaredNames of the
// body must be empty). Only let/const/class contribute lexical names; a top-level
// FunctionDeclaration in a function body is var-scoped, not lexical.
func (p *parser) checkParamBodyLexicalConflict(params []ast.Expr, body *ast.BlockStmt) {
	if p.err != nil || body == nil {
		return
	}
	lex := map[string]bool{}
	for _, s := range body.Body {
		switch st := s.(type) {
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					forEachBindingName(d.Target, func(n string, _ token.Pos) { lex[n] = true })
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				lex[st.Def.Name.Name] = true
			}
		}
	}
	if len(lex) == 0 {
		return
	}
	for _, name := range paramBoundNames(params) {
		if lex[name] {
			p.errorAt(body.Pos(), "Identifier '%s' has already been declared", name)
			return
		}
	}
}

// parseParams parses a parenthesized formal parameter list.
func (p *parser) parseParams() []ast.Expr {
	prevParams := p.inParams
	p.inParams = true
	defer func() { p.inParams = prevParams }()
	p.expect(token.LPAREN)
	var params []ast.Expr
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		el := p.parseBindingElement()
		params = append(params, el)
		if _, isRest := el.(*ast.RestElement); isRest {
			// A rest parameter must be the final parameter: neither another
			// parameter nor a trailing comma may follow it (ECMA-262 15.1.1).
			if p.at(token.COMMA) {
				p.errorAt(p.cur().Pos, "Rest parameter must be last formal parameter")
			}
			break
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN)
	return params
}

// parseBindingElement parses a single binding element: a rest element, a
// pattern, or a target with an optional default value.
func (p *parser) parseBindingElement() ast.Expr {
	if p.at(token.ELLIPSIS) {
		ell := p.next()
		return &ast.RestElement{Ellipsis: ell.Pos, Target: p.parseBindingTarget()}
	}
	target := p.parseBindingTarget()
	if p.accept(token.ASSIGN) {
		def := p.parseAssignExpr()
		return &ast.AssignPattern{Target: target, Default: def}
	}
	return target
}

// parseBindingTarget parses an identifier or a destructuring pattern (array or
// object). Patterns are represented by the same ArrayLit/ObjectLit nodes used
// for literals; the interpreter distinguishes them by context.
func (p *parser) parseBindingTarget() ast.Expr {
	switch p.cur().Type {
	case token.LBRACKET:
		pat := p.parseArrayLit()
		p.checkBindingPattern(pat)
		return pat
	case token.LBRACE:
		pat := p.parseObjectLit()
		p.checkBindingPattern(pat)
		return pat
	case token.IDENT:
		id := p.next()
		p.checkReservedIdentifier(id.Literal, id.Pos)
		p.checkStrictBindingName(id.Literal, id.Pos, p.strict)
		p.checkEscapedReserved(id)
		return &ast.Ident{NamePos: id.Pos, Name: id.Literal}
	default:
		// Only the contextual keywords (let, static, yield, async, await, of,
		// get, set) may serve as a BindingIdentifier. An always-reserved word
		// (break, for, if, this, enum, ...) is not an Identifier, so using it
		// where a binding name is required is an early SyntaxError
		// (ECMA-262 §12.7.2).
		if p.cur().Type.IsKeyword() {
			id := p.next()
			name := identText(id)
			if token.IsReservedWord(name) {
				p.errorAt(id.Pos, "'%s' is a reserved word and may not be used as an identifier", name)
				return &ast.Ident{NamePos: id.Pos, Name: name}
			}
			p.checkReservedIdentifier(name, id.Pos)
			return &ast.Ident{NamePos: id.Pos, Name: name}
		}
		p.errorf("expected binding name but got %s", p.cur().Type)
		return &ast.Ident{NamePos: p.cur().Pos}
	}
}

// checkEscapedReserved reports the early error for an IdentifierName written
// with a UnicodeEscapeSequence whose StringValue is a reserved word, used where
// an Identifier — a BindingIdentifier, IdentifierReference, or LabelIdentifier —
// is required. Such a token is a valid IdentifierName (so it remains legal as a
// property name), but Identifier excludes ReservedWord, and a reserved word's
// code points may not be expressed by an escape (ECMA-262 §12.7.2, §13.1.1).
func (p *parser) checkEscapedReserved(tk token.Token) {
	if !tk.Escaped {
		return
	}
	name := tk.Literal
	if token.IsReservedWord(name) {
		p.errorAt(tk.Pos, "keyword '%s' must not contain escaped characters", name)
		return
	}
	// Context-dependent reservations (ECMA-262 §12.7.2): the strict future
	// reserved words, plus yield in a generator/strict context and await in an
	// async context, are not valid Identifiers where they are reserved. We only
	// apply these to an escaped form; the corresponding unescaped early errors
	// are handled by the keyword-token paths and their own checks.
	switch name {
	case "implements", "interface", "package", "private", "protected", "public", "let", "static":
		if p.strict {
			p.errorAt(tk.Pos, "'%s' may not be used as an identifier in strict mode", name)
		}
	case "yield":
		if p.strict || p.inGenerator {
			p.errorAt(tk.Pos, "'yield' may not be used as an identifier in this context")
		}
	case "await":
		if p.inAsync {
			p.errorAt(tk.Pos, "'await' may not be used as an identifier in this context")
		}
	}
}

// checkStrictBindingName reports the strict-mode BindingIdentifier early error
// (ECMA-262 §13.1.1): a name that is *bound* — a var/let/const binding, a
// function/arrow parameter, a catch parameter, or a function declaration name —
// may not be `eval` or `arguments` in strict-mode code. Unlike an
// IdentifierReference (`eval(...)`, `arguments[0]`), which remains legal in
// strict code, this restriction applies only where the name is introduced as a
// new binding, so it is deliberately separate from checkReservedIdentifier
// (which is shared with reference contexts). The strict flag is passed
// explicitly because a binding's effective strictness may come from a
// function/arrow body's own "use strict" directive, which is not yet reflected
// in p.strict while the enclosing parameter list or name is being parsed.
func (p *parser) checkStrictBindingName(name string, pos token.Pos, strict bool) {
	if strict && (name == "eval" || name == "arguments") {
		p.earlyError(pos, "'"+name+"' may not be used as a binding identifier in strict mode")
	}
}

// checkReservedIdentifier reports the early errors for a name used as a binding
// identifier or an identifier reference: `yield` is a reserved word in
// strict-mode or generator code, and `await` is reserved in async-function
// code, so neither may be used as an identifier there.
func (p *parser) checkReservedIdentifier(name string, pos token.Pos) {
	switch name {
	case "yield":
		if p.strict || p.inGenerator {
			p.earlyError(pos, "'yield' may not be used as an identifier in this context")
		}
	case "await":
		if p.inAsync {
			p.earlyError(pos, "'await' may not be used as an identifier in this context")
		}
		// Inside a class static initialization block `await` is reserved (the block
		// is parsed with [+Await]) even though the block is not itself async. This
		// reservation does not cross a function/arrow boundary.
		if p.staticBlockAwait {
			p.earlyError(pos, "'await' may not be used as an identifier in a class static initialization block")
		}
	case "implements", "interface", "package", "private", "protected", "public", "let", "static":
		// The strict future reserved words are not valid Identifiers in strict-mode
		// code (ECMA-262 §12.7.2, Identifier static semantics).
		if p.strict {
			p.earlyError(pos, "'"+name+"' may not be used as an identifier in strict mode")
		}
	}
}

// checkFuncExprName enforces the reserved-word early errors for a function
// expression's BindingIdentifier, which is parsed in the function's own scope:
// `yield` is reserved iff the function is a generator, `await` iff it is async,
// and the strict future reserved words are reserved in strict-mode code.
func (p *parser) checkFuncExprName(name string, pos token.Pos, generator, async bool) {
	switch name {
	case "yield":
		if p.strict || generator {
			p.earlyError(pos, "'yield' may not be used as an identifier in this context")
		}
	case "await":
		if async {
			p.earlyError(pos, "'await' may not be used as an identifier in this context")
		}
	case "eval", "arguments":
		// A BindingIdentifier may not be `eval` or `arguments` in strict-mode code
		// (ECMA-262 13.1.1: BindingIdentifier : Identifier early error).
		if p.strict {
			p.earlyError(pos, "'"+name+"' may not be used as a binding identifier in strict mode")
		}
	case "implements", "interface", "package", "private", "protected", "public", "let", "static":
		if p.strict {
			p.earlyError(pos, "'"+name+"' may not be used as an identifier in strict mode")
		}
	}
}

// ---------------------------------------------------------------------------
// Functions
// ---------------------------------------------------------------------------

// parseFuncDef parses the star/name/params/body of a function following the
// `function` keyword (already consumed by the caller position-wise via kwPos).
func (p *parser) parseFuncDef(requireName, async bool) *ast.FuncDef {
	def := &ast.FuncDef{}
	if p.accept(token.STAR) {
		def.Generator = true
	}
	if p.at(token.IDENT) || (p.cur().Type.IsKeyword() && !p.at(token.LPAREN)) {
		switch {
		case p.at(token.IDENT):
			id := p.next()
			p.checkEscapedReserved(id)
			def.Name = &ast.Ident{NamePos: id.Pos, Name: id.Literal}
		case requireName || isContextualKeyword(p.cur().Type):
			// A function-expression name binds in the function's own scope, so a
			// contextual keyword (e.g. `function yield(){}` inside a generator, in
			// sloppy code) is a valid name there; its own-scope reservation is
			// checked below.
			id := p.next()
			def.Name = &ast.Ident{NamePos: id.Pos, Name: identText(id)}
		}
	}
	if requireName && def.Name == nil {
		p.errorf("function declaration requires a name")
	}
	// A function *declaration*'s BindingIdentifier is evaluated with the
	// enclosing [Yield]/[Await] parameters (still in effect here, before the new
	// function scope overrides p.inGenerator/p.inAsync below). So `function
	// await(){}` nested inside async code, or `function yield(){}` inside a
	// generator, is an early error — while the same name is legal at the top
	// level of a script (`async function await(){}`). A function *expression*'s
	// name binds in its own scope under different parameters, so it is excluded.
	if requireName && def.Name != nil {
		p.checkReservedIdentifier(def.Name.Name, def.Name.NamePos)
	} else if def.Name != nil {
		// A function *expression*'s name binds in the function's own scope, so
		// `yield` is reserved only if this function is itself a generator and
		// `await` only if it is async; the strict future reserved words remain
		// reserved in strict-mode code.
		p.checkFuncExprName(def.Name.Name, def.Name.NamePos, def.Generator, async)
	}
	// A regular function establishes its own arguments/super scope (so a field
	// initializer's restrictions do not reach in) and its own yield/await
	// reservation determined by whether it is a generator or async.
	prevField, prevGen, prevAsync := p.inFieldInit, p.inGenerator, p.inAsync
	prevSuper, prevProp, prevNT := p.superCallOK, p.superPropOK, p.newTargetOK
	prevStatic, prevStaticAwait := p.inStaticBlock, p.staticBlockAwait
	p.inFieldInit = false
	p.inStaticBlock = false // a nested function has its own arguments scope
	p.staticBlockAwait = false
	p.inGenerator, p.inAsync = def.Generator, async
	p.superCallOK = false // a nested regular function never permits super()
	p.superPropOK = false // nor super.property (only valid inside a method)
	p.newTargetOK = true  // but new.target is valid in any function
	paramsPos := p.cur().Pos
	def.Params = p.parseParams()
	p.inFunction++
	bodyUseStrict := p.at(token.LBRACE) && p.scanUseStrict(p.idx+1)
	def.Body, def.Strict = p.parseFunctionBody()
	p.inFunction--
	p.inFieldInit, p.inGenerator, p.inAsync = prevField, prevGen, prevAsync
	p.superCallOK, p.superPropOK, p.newTargetOK = prevSuper, prevProp, prevNT
	p.inStaticBlock, p.staticBlockAwait = prevStatic, prevStaticAwait
	p.checkStrictSimpleParams(paramsPos, bodyUseStrict, def.Params)
	p.checkParamDuplicates(def.Params, def.Strict)
	p.checkStrictParamNames(def.Params, def.Strict)
	p.checkParamBodyLexicalConflict(def.Params, def.Body)
	// A function named `eval`/`arguments` is an early error when the function is
	// strict-mode code — which includes strictness inherited from a "use strict"
	// directive in its own body (§13.1.1's BindingIdentifier restriction sees the
	// whole function's strictness). This is checked here, after the body has
	// determined def.Strict, for both declarations and named expressions;
	// checkFuncExprName above only observes the enclosing strictness.
	if def.Name != nil {
		p.checkStrictBindingName(def.Name.Name, def.Name.NamePos, def.Strict)
	}
	return def
}

// parseFunctionExpr parses a function expression. The async flag is applied by
// the caller for `async function`.
func (p *parser) parseFunctionExpr(async bool) *ast.FuncExpr {
	kw := p.expect(token.FUNCTION)
	def := p.parseFuncDef(false, async)
	def.Async = async
	return &ast.FuncExpr{Keyword: kw.Pos, Def: def}
}

// parseFunctionDecl parses a function declaration statement.
func (p *parser) parseFunctionDecl(async bool) *ast.FuncDecl {
	kw := p.expect(token.FUNCTION)
	def := p.parseFuncDef(true, async)
	def.Async = async
	return &ast.FuncDecl{Keyword: kw.Pos, Def: def}
}

// ---------------------------------------------------------------------------
// Object literals
// ---------------------------------------------------------------------------

// parseObjectLit parses an object literal or (in binding context) an object
// destructuring pattern.
func (p *parser) parseObjectLit() ast.Expr {
	lb := p.expect(token.LBRACE)
	// Braces open a fresh expression context: the `in` operator is permitted
	// inside property-value expressions (e.g. default initializers) even within a
	// for-statement header, where the top-level `in` is otherwise suppressed.
	saveNoIn := p.noIn
	p.noIn = false
	defer func() { p.noIn = saveNoIn }()
	obj := &ast.ObjectLit{Lbrace: lb.Pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		obj.Properties = append(obj.Properties, p.parseProperty())
		if !p.accept(token.COMMA) {
			break
		}
	}
	rb := p.expect(token.RBRACE)
	obj.Rbrace = rb.Pos
	p.recordObjectLitEarlyErrors(obj)
	return obj
}

// recordObjectLitEarlyErrors registers the ObjectLiteral early errors that are
// suppressed when the object is refined into a destructuring pattern: a
// CoverInitializedName (`{ a = 1 }`) and duplicate `__proto__:` data properties
// (ECMA-262 §13.2.5.1, §B.3.1). They are deferred (keyed by the opening brace)
// so a subsequent `= ...` assignment or binding-pattern use can clear them.
func (p *parser) recordObjectLitEarlyErrors(obj *ast.ObjectLit) {
	protoCount := 0
	for _, prop := range obj.Properties {
		// CoverInitializedName: a shorthand written with an initializer.
		if prop.Shorthand {
			if _, ok := prop.Value.(*ast.AssignPattern); ok {
				p.recordDeferredObjErr(obj.Lbrace, prop.KeyPos,
					"Invalid shorthand property initializer")
			}
		}
		// A `__proto__:` special form is only the data property PropertyName :
		// AssignmentExpression — not a shorthand, method, accessor, or computed key.
		if prop.Kind == ast.PropInit && !prop.Method && !prop.Shorthand && !prop.Computed {
			if isProtoKey(prop.Key) {
				protoCount++
			}
		}
	}
	if protoCount >= 2 {
		p.recordDeferredObjErr(obj.Lbrace, obj.Lbrace,
			"Duplicate __proto__ fields are not allowed in object literals")
	}
}

// isProtoKey reports whether a (non-computed) property key is the literal name
// `__proto__`, whether written as an identifier or a string literal.
func isProtoKey(key ast.Expr) bool {
	switch k := key.(type) {
	case *ast.Ident:
		return k.Name == "__proto__"
	case *ast.StringLit:
		return k.Value == "__proto__"
	}
	return false
}

// recordDeferredObjErr notes an ObjectLiteral early error that is suppressed if
// the owning object is later refined into a destructuring pattern (see
// deferredObjErrs).
func (p *parser) recordDeferredObjErr(owner, pos token.Pos, msg string) {
	p.deferredObjErrs = append(p.deferredObjErrs, deferredObjErr{owner: owner, pos: pos, msg: msg})
}

// clearDeferredObjErrs discards the pending ObjectLiteral early errors owned by
// the object whose opening brace is at owner. It is called when that object is
// validated as a destructuring assignment/binding pattern, where a
// CoverInitializedName and a duplicate `__proto__` are both permitted.
func (p *parser) clearDeferredObjErrs(owner token.Pos) {
	if len(p.deferredObjErrs) == 0 {
		return
	}
	kept := p.deferredObjErrs[:0]
	for _, e := range p.deferredObjErrs {
		if e.owner != owner {
			kept = append(kept, e)
		}
	}
	p.deferredObjErrs = kept
}

// flushDeferredObjErrs reports the first surviving deferred ObjectLiteral early
// error (one that was never cleared by pattern refinement).
func (p *parser) flushDeferredObjErrs() {
	if p.err != nil {
		return
	}
	for _, e := range p.deferredObjErrs {
		p.errorAt(e.pos, "%s", e.msg)
		return
	}
}

// parseProperty parses one object-literal member: spread, shorthand, key:value,
// method, or get/set accessor.
func (p *parser) parseProperty() *ast.Property {
	tk := p.cur()

	// Spread: ...expr
	if tk.Type == token.ELLIPSIS {
		p.next()
		arg := p.parseAssignExpr()
		return &ast.Property{KeyPos: tk.Pos, Value: arg, Kind: ast.PropSpread}
	}

	// Accessors and async/generator methods: get/set/async prefixes are only
	// treated specially when followed by a property key (not ':' or '(').
	if tk.Type == token.GET || tk.Type == token.SET {
		next := p.peek(1).Type
		if next != token.COLON && next != token.COMMA && next != token.RBRACE &&
			next != token.LPAREN && next != token.ASSIGN {
			p.next() // get/set
			kind := ast.PropGet
			if tk.Type == token.SET {
				kind = ast.PropSet
			}
			key, computed := p.parsePropertyKey()
			p.checkNoPrivateKey(key)
			fn := p.parseMethodBody(false, false)
			p.checkObjectAccessorArity(kind, key.Pos(), fn)
			return &ast.Property{KeyPos: tk.Pos, Key: key, Value: fn, Kind: kind, Computed: computed, Method: true}
		}
	}
	async := false
	generator := false
	if tk.Type == token.ASYNC && p.peek(1).Type != token.COLON &&
		p.peek(1).Type != token.COMMA && p.peek(1).Type != token.LPAREN &&
		p.peek(1).Type != token.RBRACE && !p.peek(1).NewlineBefore {
		p.next()
		async = true
	}
	if p.at(token.STAR) {
		p.next()
		generator = true
	}

	key, computed := p.parsePropertyKey()

	switch {
	case p.at(token.LPAREN):
		// Method definition. A private name (#x) is never a valid method key in an
		// object literal (ECMA-262 §13.2.5.1: PrivateBoundNames must be empty).
		p.checkNoPrivateKey(key)
		fn := p.parseMethodBody(async, generator)
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: fn, Kind: ast.PropInit, Computed: computed, Method: true}
	case p.accept(token.COLON):
		// `async`/`*` are only valid as a method-definition prefix; before a
		// PropertyName : AssignmentExpression they are a SyntaxError.
		p.checkNoMethodPrefix(tk, async, generator)
		p.checkNoPrivateKey(key)
		val := p.parseAssignExpr()
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: val, Kind: ast.PropInit, Computed: computed}
	case p.at(token.ASSIGN):
		// Shorthand with default (CoverInitializedName), only valid when the object
		// is later refined into a destructuring pattern: { x = 1 }. The name is an
		// IdentifierReference, so a non-identifier key, an escaped reserved word, or
		// a strict-reserved word is an early error, as is a stray method prefix.
		p.checkNoMethodPrefix(tk, async, generator)
		p.checkShorthandIdentifier(tk, key, computed)
		p.next()
		def := p.parseAssignExpr()
		val := &ast.AssignPattern{Target: key, Default: def}
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: val, Kind: ast.PropInit, Shorthand: true}
	default:
		// Shorthand: { x } — the name is both key and IdentifierReference, so it
		// must be a plain identifier (not a number, string, computed, or reserved
		// word) and no method prefix may precede it.
		p.checkNoMethodPrefix(tk, async, generator)
		p.checkShorthandIdentifier(tk, key, computed)
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: key, Kind: ast.PropInit, Shorthand: true}
	}
}

// checkNoMethodPrefix reports a SyntaxError when an `async` or `*` (generator)
// prefix was consumed but the member is not a method definition. Such a prefix
// is only valid immediately before a MethodDefinition's PropertyName ( ... ).
func (p *parser) checkNoMethodPrefix(tk token.Token, async, generator bool) {
	if async || generator {
		p.errorAt(tk.Pos, "Unexpected token in object literal")
	}
}

// checkNoPrivateKey reports the early error for a private name (#x) used as an
// object-literal property key, which is never permitted (private names exist
// only in class bodies).
func (p *parser) checkNoPrivateKey(key ast.Expr) {
	if priv, ok := key.(*ast.PrivateIdent); ok {
		p.errorAt(priv.Pos(), "Private names are not allowed in object literals")
	}
}

// checkShorthandIdentifier validates the key of a shorthand property
// (`{ x }` or `{ x = 1 }`): it must be an IdentifierReference — a plain
// (non-computed) identifier, not a number/string literal or a reserved word.
func (p *parser) checkShorthandIdentifier(tk token.Token, key ast.Expr, computed bool) {
	if computed {
		p.errorAt(tk.Pos, "Unexpected token in object literal shorthand")
		return
	}
	if _, ok := key.(*ast.Ident); !ok {
		p.errorAt(tk.Pos, "Unexpected token in object literal shorthand")
		return
	}
	// A hard ReservedWord (if, for, function, true, …) is never an
	// IdentifierReference; the contextual keywords (let, static, yield, …) are,
	// subject to their own strict/generator/async reservations below.
	if tk.Type.IsKeyword() && !isContextualKeyword(tk.Type) {
		p.errorAt(tk.Pos, "Unexpected reserved word in object literal shorthand")
		return
	}
	p.checkReservedIdentifier(identText(tk), tk.Pos)
	p.checkEscapedReserved(tk)
}

// checkObjectAccessorArity enforces the getter/setter parameter-count early
// errors on an object-literal accessor: a getter declares no parameters and a
// setter declares exactly one non-rest parameter (ECMA-262 §13.2.5.1).
func (p *parser) checkObjectAccessorArity(kind ast.PropertyKind, pos token.Pos, fn *ast.FuncExpr) {
	if fn == nil {
		return
	}
	params := fn.Def.Params
	if kind == ast.PropGet {
		if len(params) != 0 {
			p.errorAt(pos, "getter functions must have no arguments")
		}
		return
	}
	if len(params) != 1 {
		p.errorAt(pos, "setter functions must have exactly one argument")
		return
	}
	if _, isRest := params[0].(*ast.RestElement); isRest {
		p.errorAt(pos, "setter function argument must not be a rest parameter")
	}
}

// parsePropertyKey parses an object/class member key, returning the key
// expression and whether it was computed ([expr]).
func (p *parser) parsePropertyKey() (ast.Expr, bool) {
	tk := p.cur()
	switch tk.Type {
	case token.LBRACKET:
		p.next()
		// A ComputedPropertyName is a full AssignmentExpression; the `in` operator
		// is always permitted inside it, even when the object literal appears in a
		// for-statement header (where the surrounding noIn restriction applies).
		saveNoIn := p.noIn
		p.noIn = false
		expr := p.parseAssignExpr()
		p.noIn = saveNoIn
		p.expect(token.RBRACKET)
		return expr, true
	case token.STRING:
		p.next()
		return &ast.StringLit{ValuePos: tk.Pos, Value: tk.Literal, Raw: tk.Raw}, false
	case token.NUMBER:
		p.next()
		return &ast.NumberLit{ValuePos: tk.Pos, Value: parseNumber(tk.Literal), Raw: tk.Raw}, false
	case token.BIGINT:
		// A BigInt literal is a valid LiteralPropertyName; its property key is the
		// BigInt's decimal string value (e.g. `1n` names the property "1").
		p.next()
		return &ast.BigIntLit{ValuePos: tk.Pos, Raw: tk.Raw, Digits: tk.Literal}, false
	case token.PRIVATE:
		p.next()
		return &ast.PrivateIdent{NamePos: tk.Pos, Name: tk.Literal}, false
	case token.IDENT:
		p.next()
		return &ast.Ident{NamePos: tk.Pos, Name: tk.Literal}, false
	default:
		if tk.Type.IsKeyword() {
			p.next()
			return &ast.Ident{NamePos: tk.Pos, Name: identText(tk)}, false
		}
		p.errorf("expected property key but got %s", tk.Type)
		p.next()
		return &ast.Ident{NamePos: tk.Pos}, false
	}
}

// parseMethodBody parses the parameter list and body of a concise method,
// returning it as a function expression.
func (p *parser) parseMethodBody(async, generator bool) *ast.FuncExpr {
	start := p.cur()
	def := &ast.FuncDef{Async: async, Generator: generator}
	// A method establishes its own arguments/super scope and yield/await
	// reservation (see parseFuncDef).
	prevField, prevGen, prevAsync := p.inFieldInit, p.inGenerator, p.inAsync
	prevSuper, prevProp, prevNT := p.superCallOK, p.superPropOK, p.newTargetOK
	prevStatic, prevStaticAwait := p.inStaticBlock, p.staticBlockAwait
	p.inFieldInit = false
	p.inStaticBlock = false // a method has its own arguments scope
	p.staticBlockAwait = false
	p.inGenerator, p.inAsync = generator, async
	// A SuperCall is permitted only in the derived constructor; parseClassMember
	// signals that via pendingSuperCall for exactly that one method body. Super
	// property and new.target are valid in any method.
	p.superCallOK = p.pendingSuperCall
	p.pendingSuperCall = false
	p.superPropOK, p.newTargetOK = true, true
	paramsPos := p.cur().Pos
	def.Params = p.parseParams()
	p.inFunction++
	bodyUseStrict := p.at(token.LBRACE) && p.scanUseStrict(p.idx+1)
	def.Body, def.Strict = p.parseFunctionBody()
	p.inFunction--
	p.inFieldInit, p.inGenerator, p.inAsync = prevField, prevGen, prevAsync
	p.superCallOK, p.superPropOK, p.newTargetOK = prevSuper, prevProp, prevNT
	p.inStaticBlock, p.staticBlockAwait = prevStatic, prevStaticAwait
	p.checkStrictSimpleParams(paramsPos, bodyUseStrict, def.Params)
	// A concise method's parameter list must never contain duplicates.
	p.checkParamDuplicates(def.Params, true)
	p.checkStrictParamNames(def.Params, def.Strict)
	p.checkParamBodyLexicalConflict(def.Params, def.Body)
	return &ast.FuncExpr{Keyword: start.Pos, Def: def}
}

// ---------------------------------------------------------------------------
// Classes
// ---------------------------------------------------------------------------

// parseClassExpr parses a class expression.
func (p *parser) parseClassExpr() ast.Expr {
	kw := p.expect(token.CLASS)
	def := p.parseClassDef()
	return &ast.ClassExpr{Keyword: kw.Pos, Def: def}
}

// parseClassDecl parses a class declaration statement.
func (p *parser) parseClassDecl() *ast.ClassDecl {
	kw := p.expect(token.CLASS)
	def := p.parseClassDef()
	if def.Name == nil {
		p.errorAt(kw.Pos, "class declaration requires a name")
	}
	return &ast.ClassDecl{Keyword: kw.Pos, Def: def}
}

// parseClassDef parses the shared body of a class (name, extends, members).
func (p *parser) parseClassDef() *ast.ClassDef {
	def := &ast.ClassDef{}
	// All class code — including the name BindingIdentifier and the heritage
	// expression — is strict-mode code, so enter strict mode before reading the
	// name (which may then not be a strict reserved word such as an escaped
	// `yield`/`let`/`static`).
	prevStrict := p.strict
	p.strict = true
	if p.at(token.IDENT) {
		id := p.next()
		p.checkEscapedReserved(id)
		def.Name = &ast.Ident{NamePos: id.Pos, Name: id.Literal}
	} else if p.cur().Type.IsKeyword() && !p.at(token.EXTENDS) {
		// A class BindingIdentifier may be spelled with a contextual keyword
		// (e.g. `class await {}` in script code, or `yield` outside a
		// generator/strict context). An always-reserved word, or one reserved
		// in the current context, is a SyntaxError.
		id := p.next()
		name := identText(id)
		if token.IsReservedWord(name) {
			p.errorAt(id.Pos, "'%s' is a reserved word and may not be used as an identifier", name)
		}
		p.checkReservedIdentifier(name, id.Pos)
		p.checkEscapedReserved(id)
		def.Name = &ast.Ident{NamePos: id.Pos, Name: name}
	}
	if p.accept(token.EXTENDS) {
		def.SuperClass = p.parseLeftHandSide()
	}
	lb := p.expect(token.LBRACE)
	def.Lbrace = lb.Pos
	// Private names (#x) are only valid inside a class body; track nesting so
	// their use elsewhere is a SyntaxError (see parsePrivateName). A class body
	// is always strict-mode code.
	p.classDepth++
	env := &privateEnv{declared: map[string]bool{}}
	p.privateEnvStack = append(p.privateEnvStack, env)
	// Entering a class body: a SuperCall is permitted only in this class's own
	// derived constructor, not in any construct inherited from an outer scope.
	prevHeritage, prevSuper := p.classHeritage, p.superCallOK
	p.classHeritage = def.SuperClass != nil
	p.superCallOK = false
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		if p.accept(token.SEMICOLON) {
			continue // stray semicolons between members are allowed
		}
		def.Members = append(def.Members, p.parseClassMember())
	}
	p.classHeritage, p.superCallOK = prevHeritage, prevSuper
	// Record this class's declared private names before popping so that
	// references captured anywhere (including in nested classes, or textually
	// before the declaration) can still resolve to them.
	for _, m := range def.Members {
		if priv, ok := m.Key.(*ast.PrivateIdent); ok {
			env.declared[priv.Name] = true
		}
	}
	p.strict = prevStrict
	p.classDepth--
	p.privateEnvStack = p.privateEnvStack[:len(p.privateEnvStack)-1]
	rb := p.expect(token.RBRACE)
	def.Rbrace = rb.Pos
	p.checkClassMembers(def)
	return def
}

// privateEnv records the private names (#x) declared in a single class body.
type privateEnv struct {
	declared map[string]bool
}

// privateRef is a use of a private name captured with the class environments
// enclosing it, so that "declared in an enclosing class" can be validated after
// the whole program is parsed and every declaration is known.
type privateRef struct {
	name string
	pos  token.Pos
	envs []*privateEnv
}

// recordPrivateRef notes a reference to a private name for later validation. A
// reference outside any class body is an immediate SyntaxError; inside one, the
// enclosing environments are snapshotted (their declarations may still be
// pending) and checked once parsing completes.
func (p *parser) recordPrivateRef(tk token.Token) {
	if len(p.privateEnvStack) == 0 {
		p.errorAt(tk.Pos, "Private field '%s' must be declared in an enclosing class", tk.Literal)
		return
	}
	envs := make([]*privateEnv, len(p.privateEnvStack))
	copy(envs, p.privateEnvStack)
	p.privateRefs = append(p.privateRefs, privateRef{name: tk.Literal, pos: tk.Pos, envs: envs})
}

// checkPrivateRefs reports the first reference to a private name that is not
// declared in any of its enclosing classes (ECMA-262 AllPrivateIdentifiersValid).
func (p *parser) checkPrivateRefs() {
	if p.err != nil {
		return
	}
	for _, ref := range p.privateRefs {
		found := false
		for _, env := range ref.envs {
			if env.declared[ref.name] {
				found = true
				break
			}
		}
		if !found {
			p.errorAt(ref.pos, "Private field '%s' must be declared in an enclosing class", ref.name)
			return
		}
	}
}

// checkClassMembers enforces early (static-semantic) errors on a class body:
// at most one constructor, and no duplicate private names (a get/set pair for
// the same private name being the only permitted repeat).
func (p *parser) checkClassMembers(def *ast.ClassDef) {
	ctorCount := 0
	// privateKinds records, per private name, the accessor halves already seen
	// (bit 1 = get, bit 2 = set) or a sentinel for a field/method.
	const kindOther = 4
	privateKinds := map[string]int{}
	// privateStatic records, per private name, whether the first-seen half was
	// static; a complementary get/set half must agree (a private accessor pair
	// may not mix static and non-static).
	privateStatic := map[string]bool{}
	for _, m := range def.Members {
		// A getter takes no parameters; a setter takes exactly one (and it may
		// not be a rest parameter). This is an early (parse-phase) error.
		p.checkAccessorArity(m)
		if priv, ok := m.Key.(*ast.PrivateIdent); ok && priv.Name == "#constructor" {
			p.errorAt(m.KeyPos, "Classes may not declare a private element named '#constructor'")
		}
		if name, named := classMemberName(m); named {
			switch {
			case name == "constructor" && m.Field:
				// A field (static or not) may never be named "constructor".
				p.errorAt(m.KeyPos, "Classes may not have a field named 'constructor'")
			case name == "constructor" && !m.Static && isSpecialClassMethod(m):
				// The constructor must be a plain method, not an accessor,
				// generator, or async method.
				p.errorAt(m.KeyPos, "Class constructor may not be an accessor, generator, or async method")
			case name == "constructor" && !m.Static && !m.Field:
				ctorCount++
				if ctorCount > 1 {
					p.errorAt(m.KeyPos, "A class may only have one constructor")
				}
			case name == "prototype" && m.Static:
				// A static member may never be named "prototype".
				p.errorAt(m.KeyPos, "Classes may not have a static member named 'prototype'")
			}
		}
		priv, ok := m.Key.(*ast.PrivateIdent)
		if !ok {
			continue
		}
		name := priv.Name
		var bit int
		switch m.Kind {
		case ast.PropGet:
			bit = 1
		case ast.PropSet:
			bit = 2
		default:
			bit = kindOther
		}
		prev := privateKinds[name]
		// A duplicate is an error unless it is the complementary accessor half.
		if prev != 0 && !(bit != kindOther && prev != kindOther && prev&bit == 0) {
			p.errorAt(priv.Pos(), "Duplicate private name %s", name)
		} else if prev != 0 && privateStatic[name] != m.Static {
			// A private get/set accessor pair may not mix static and non-static.
			p.errorAt(priv.Pos(), "Private accessor %s must be all static or all non-static", name)
		}
		if prev == 0 {
			privateStatic[name] = m.Static
		}
		privateKinds[name] = prev | bit
	}
}

// checkAccessorArity enforces the early error that a getter declares no
// parameters and a setter declares exactly one non-rest parameter (ECMA-262
// class MethodDefinition static semantics).
func (p *parser) checkAccessorArity(m *ast.ClassMember) {
	if m.Field || (m.Kind != ast.PropGet && m.Kind != ast.PropSet) {
		return
	}
	fe, ok := m.Value.(*ast.FuncExpr)
	if !ok {
		return
	}
	params := fe.Def.Params
	if m.Kind == ast.PropGet {
		if len(params) != 0 {
			p.errorAt(m.KeyPos, "getter functions must have no arguments")
		}
		return
	}
	// setter
	if len(params) != 1 {
		p.errorAt(m.KeyPos, "setter functions must have exactly one argument")
		return
	}
	if _, isRest := params[0].(*ast.RestElement); isRest {
		p.errorAt(m.KeyPos, "setter function argument must not be a rest parameter")
	}
}

// classMemberName returns the static property name of a class member and true,
// or ("", false) when the name is computed or a private name (neither of which
// participates in the constructor/prototype static-name early errors).
func classMemberName(m *ast.ClassMember) (string, bool) {
	if m.Computed {
		return "", false
	}
	switch k := m.Key.(type) {
	case *ast.Ident:
		return k.Name, true
	case *ast.StringLit:
		return k.Value, true
	}
	return "", false
}

// isSpecialClassMethod reports whether a class method is an accessor, a
// generator, or async — the forms a "constructor" method may not take.
func isSpecialClassMethod(m *ast.ClassMember) bool {
	if m.Field {
		return false
	}
	if m.Kind == ast.PropGet || m.Kind == ast.PropSet {
		return true
	}
	if fe, ok := m.Value.(*ast.FuncExpr); ok {
		return fe.Def.Generator || fe.Def.Async
	}
	return false
}

// parseClassMember parses a single class body element: a method, accessor,
// field, or their static variants.
func (p *parser) parseClassMember() *ast.ClassMember {
	start := p.cur()
	m := &ast.ClassMember{KeyPos: start.Pos}

	// `static` modifier (unless `static` is itself the member name, e.g.
	// `static() {}` or `static = 1`).
	if p.at(token.STATIC) && p.peek(1).Type != token.LPAREN &&
		p.peek(1).Type != token.ASSIGN && p.peek(1).Type != token.SEMICOLON {
		p.next()
		m.Static = true

		// A `static { ... }` initialization block. Its body is a statement list
		// evaluated at class-definition time with `this` bound to the constructor.
		if p.at(token.LBRACE) {
			return p.parseStaticBlock(m)
		}
	}

	async := false
	generator := false
	kind := ast.PropInit

	if (p.at(token.GET) || p.at(token.SET)) && !isMemberEnd(p.peek(1).Type) && p.peek(1).Type != token.STAR {
		if p.at(token.SET) {
			kind = ast.PropSet
		} else {
			kind = ast.PropGet
		}
		p.next()
	} else {
		if p.at(token.ASYNC) && !p.peek(1).NewlineBefore && !isMemberEnd(p.peek(1).Type) {
			p.next()
			async = true
		}
		if p.at(token.STAR) {
			p.next()
			generator = true
		}
	}

	key, computed := p.parsePropertyKey()
	m.Key = key
	m.Computed = computed
	m.Kind = kind

	if p.at(token.LPAREN) {
		// Method or accessor. A SuperCall is permitted in its body only if this
		// is the class's own derived constructor: a non-static, non-computed,
		// plain method named "constructor" in a class with a heritage.
		if name, named := classMemberName(m); named && name == "constructor" &&
			!m.Static && !async && !generator && kind == ast.PropInit {
			p.pendingSuperCall = p.classHeritage
		}
		fn := p.parseMethodBody(async, generator)
		m.Value = fn
		return m
	}

	// Field definition (with optional initializer). A field initializer may not
	// contain `arguments` or a SuperCall (tracked via inFieldInit).
	m.Field = true
	if p.accept(token.ASSIGN) {
		prev, prevProp := p.inFieldInit, p.superPropOK
		p.inFieldInit = true
		p.superPropOK = true // super.x is valid in a field initializer
		m.Value = p.parseAssignExpr()
		p.inFieldInit, p.superPropOK = prev, prevProp
	}
	p.expectSemicolon()
	return m
}

// parseStaticBlock parses a `static { ... }` class initialization block. Its
// body is a statement list evaluated at class-definition time. Like a field
// initializer it may not reference `arguments` or contain a SuperCall, but it
// may use super.property; it establishes its own function-like scope so nested
// functions inside it are unrestricted.
func (p *parser) parseStaticBlock(m *ast.ClassMember) *ast.ClassMember {
	m.Field = false
	m.Key = nil
	// Save and set the boundary flags: this block is its own arguments/super
	// scope (super() forbidden, super.property allowed, new.target allowed) and
	// its own generator/async context.
	prevField, prevStatic := p.inFieldInit, p.inStaticBlock
	prevStaticAwait := p.staticBlockAwait
	prevGen, prevAsync, prevParams := p.inGenerator, p.inAsync, p.inParams
	prevSuperCall, prevSuperProp := p.superCallOK, p.superPropOK
	prevNT := p.newTargetOK
	// A static initialization block is a break/continue/return boundary just like
	// a function body: an enclosing loop, switch, or label does not reach across
	// it. Save and reset the tracking state.
	prevLoop, prevSwitch := p.inLoop, p.inSwitch
	prevLabels, prevPending := p.labelSet, p.pendingLabels
	p.inFieldInit = false
	p.inStaticBlock = true
	p.staticBlockAwait = true
	p.inGenerator, p.inAsync, p.inParams = false, false, false
	p.superCallOK = false
	p.superPropOK = true
	p.newTargetOK = true // new.target is valid (and evaluates to undefined) in a static block
	p.inLoop, p.inSwitch = 0, 0
	p.labelSet, p.pendingLabels = nil, nil
	m.StaticBlock = p.parseBlock()
	p.inFieldInit, p.inStaticBlock = prevField, prevStatic
	p.staticBlockAwait = prevStaticAwait
	p.inGenerator, p.inAsync, p.inParams = prevGen, prevAsync, prevParams
	p.newTargetOK = prevNT
	p.superCallOK, p.superPropOK = prevSuperCall, prevSuperProp
	p.inLoop, p.inSwitch = prevLoop, prevSwitch
	p.labelSet, p.pendingLabels = prevLabels, prevPending
	return m
}

// isMemberEnd reports whether t terminates a class-member modifier position, so
// that get/set/async can be recognized as plain member names when appropriate.
func isMemberEnd(t token.Type) bool {
	switch t {
	case token.LPAREN, token.ASSIGN, token.SEMICOLON, token.RBRACE:
		return true
	}
	return false
}
