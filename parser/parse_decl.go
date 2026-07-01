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
	if v, err := strconv.ParseFloat(s, 64); err == nil {
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

	switch p.peek(base).Type {
	case token.IDENT:
		// single-identifier arrow: x => …
		if p.peek(base+1).Type != token.ARROW || p.peek(base+1).NewlineBefore {
			return nil
		}
	case token.LPAREN:
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
	if p.at(token.IDENT) {
		id := p.next()
		arrow.Params = []ast.Expr{&ast.Ident{NamePos: id.Pos, Name: id.Literal}}
	} else {
		arrow.Params = p.parseParams()
	}
	p.expect(token.ARROW)
	if p.at(token.LBRACE) {
		bodyUseStrict := p.scanUseStrict(p.idx + 1)
		arrow.Body, _ = p.parseFunctionBody()
		p.checkStrictSimpleParams(start.Pos, bodyUseStrict, arrow.Params)
	} else {
		arrow.Expression = true
		arrow.Body = p.parseAssignExpr()
	}
	// Arrow functions never permit duplicate parameter names.
	p.checkParamDuplicates(arrow.Params, true)
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

// simpleParamList reports whether every parameter is a plain identifier.
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

// parseParams parses a parenthesized formal parameter list.
func (p *parser) parseParams() []ast.Expr {
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
		return &ast.Ident{NamePos: id.Pos, Name: id.Literal}
	default:
		// Contextual keywords are valid binding names.
		if p.cur().Type.IsKeyword() {
			id := p.next()
			return &ast.Ident{NamePos: id.Pos, Name: identText(id)}
		}
		p.errorf("expected binding name but got %s", p.cur().Type)
		return &ast.Ident{NamePos: p.cur().Pos}
	}
}

// ---------------------------------------------------------------------------
// Functions
// ---------------------------------------------------------------------------

// parseFuncDef parses the star/name/params/body of a function following the
// `function` keyword (already consumed by the caller position-wise via kwPos).
func (p *parser) parseFuncDef(requireName bool) *ast.FuncDef {
	def := &ast.FuncDef{}
	if p.accept(token.STAR) {
		def.Generator = true
	}
	if p.at(token.IDENT) || (p.cur().Type.IsKeyword() && !p.at(token.LPAREN)) {
		if p.at(token.IDENT) {
			id := p.next()
			def.Name = &ast.Ident{NamePos: id.Pos, Name: id.Literal}
		} else if requireName {
			id := p.next()
			def.Name = &ast.Ident{NamePos: id.Pos, Name: identText(id)}
		}
	}
	if requireName && def.Name == nil {
		p.errorf("function declaration requires a name")
	}
	// A regular function establishes its own arguments/super scope, so a field
	// initializer's restrictions do not reach into its parameters or body.
	prevField := p.inFieldInit
	p.inFieldInit = false
	paramsPos := p.cur().Pos
	def.Params = p.parseParams()
	p.inFunction++
	bodyUseStrict := p.at(token.LBRACE) && p.scanUseStrict(p.idx+1)
	def.Body, def.Strict = p.parseFunctionBody()
	p.inFunction--
	p.inFieldInit = prevField
	p.checkStrictSimpleParams(paramsPos, bodyUseStrict, def.Params)
	p.checkParamDuplicates(def.Params, def.Strict)
	return def
}

// parseFunctionExpr parses a function expression. The async flag is applied by
// the caller for `async function`.
func (p *parser) parseFunctionExpr(async bool) *ast.FuncExpr {
	kw := p.expect(token.FUNCTION)
	def := p.parseFuncDef(false)
	def.Async = async
	return &ast.FuncExpr{Keyword: kw.Pos, Def: def}
}

// parseFunctionDecl parses a function declaration statement.
func (p *parser) parseFunctionDecl(async bool) *ast.FuncDecl {
	kw := p.expect(token.FUNCTION)
	def := p.parseFuncDef(true)
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
	obj := &ast.ObjectLit{Lbrace: lb.Pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		obj.Properties = append(obj.Properties, p.parseProperty())
		if !p.accept(token.COMMA) {
			break
		}
	}
	rb := p.expect(token.RBRACE)
	obj.Rbrace = rb.Pos
	return obj
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
			fn := p.parseMethodBody(false, false)
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
		// Method definition.
		fn := p.parseMethodBody(async, generator)
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: fn, Kind: ast.PropInit, Computed: computed, Method: true}
	case p.accept(token.COLON):
		val := p.parseAssignExpr()
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: val, Kind: ast.PropInit, Computed: computed}
	case p.at(token.ASSIGN):
		// Shorthand with default, only valid in destructuring: { x = 1 }.
		p.next()
		def := p.parseAssignExpr()
		val := &ast.AssignPattern{Target: key, Default: def}
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: val, Kind: ast.PropInit, Shorthand: true}
	default:
		// Shorthand: { x }.
		return &ast.Property{KeyPos: tk.Pos, Key: key, Value: key, Kind: ast.PropInit, Shorthand: true}
	}
}

// parsePropertyKey parses an object/class member key, returning the key
// expression and whether it was computed ([expr]).
func (p *parser) parsePropertyKey() (ast.Expr, bool) {
	tk := p.cur()
	switch tk.Type {
	case token.LBRACKET:
		p.next()
		expr := p.parseAssignExpr()
		p.expect(token.RBRACKET)
		return expr, true
	case token.STRING:
		p.next()
		return &ast.StringLit{ValuePos: tk.Pos, Value: tk.Literal, Raw: tk.Raw}, false
	case token.NUMBER:
		p.next()
		return &ast.NumberLit{ValuePos: tk.Pos, Value: parseNumber(tk.Literal), Raw: tk.Raw}, false
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
	// A method establishes its own arguments/super scope (see parseFuncDef).
	prevField := p.inFieldInit
	p.inFieldInit = false
	paramsPos := p.cur().Pos
	def.Params = p.parseParams()
	p.inFunction++
	bodyUseStrict := p.at(token.LBRACE) && p.scanUseStrict(p.idx+1)
	def.Body, def.Strict = p.parseFunctionBody()
	p.inFunction--
	p.inFieldInit = prevField
	p.checkStrictSimpleParams(paramsPos, bodyUseStrict, def.Params)
	// A concise method's parameter list must never contain duplicates.
	p.checkParamDuplicates(def.Params, true)
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
	if p.at(token.IDENT) {
		id := p.next()
		def.Name = &ast.Ident{NamePos: id.Pos, Name: id.Literal}
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
	prevStrict := p.strict
	p.strict = true
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		if p.accept(token.SEMICOLON) {
			continue // stray semicolons between members are allowed
		}
		def.Members = append(def.Members, p.parseClassMember())
	}
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
	for _, m := range def.Members {
		if !m.Field && !m.Static {
			if id, ok := m.Key.(*ast.Ident); ok && id.Name == "constructor" {
				ctorCount++
				if ctorCount > 1 {
					p.errorAt(m.KeyPos, "A class may only have one constructor")
				}
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
		}
		privateKinds[name] = prev | bit
	}
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
	}

	async := false
	generator := false
	kind := ast.PropInit

	if (p.at(token.GET) || p.at(token.SET)) && !isMemberEnd(p.peek(1).Type) {
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
		// Method or accessor.
		fn := p.parseMethodBody(async, generator)
		m.Value = fn
		return m
	}

	// Field definition (with optional initializer). A field initializer may not
	// contain `arguments` or a SuperCall (tracked via inFieldInit).
	m.Field = true
	if p.accept(token.ASSIGN) {
		prev := p.inFieldInit
		p.inFieldInit = true
		m.Value = p.parseAssignExpr()
		p.inFieldInit = prev
	}
	p.expectSemicolon()
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
