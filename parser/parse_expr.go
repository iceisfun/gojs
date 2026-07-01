package parser

import (
	"strings"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements expression parsing. The entry point is parseExpression
// (a comma sequence); assignment, conditional, and binary operators are handled
// with precedence climbing, and unary/postfix/call/member forms by recursive
// descent down to parsePrimary.

// binaryPrec returns the binding precedence of a binary/logical operator, or 0
// if t is not one. Higher numbers bind tighter. The `in` operator is suppressed
// when p.noIn is set (inside a for-statement header).
func (p *parser) binaryPrec(t token.Type) int {
	switch t {
	case token.NULLISH, token.OR:
		return 1
	case token.AND:
		return 2
	case token.BIT_OR:
		return 3
	case token.BIT_XOR:
		return 4
	case token.BIT_AND:
		return 5
	case token.EQ, token.NE, token.STRICT_EQ, token.STRICT_NE:
		return 6
	case token.LT, token.GT, token.LE, token.GE, token.INSTANCEOF:
		return 7
	case token.IN:
		if p.noIn {
			return 0
		}
		return 7
	case token.SHL, token.SHR, token.USHR:
		return 8
	case token.PLUS, token.MINUS:
		return 9
	case token.STAR, token.SLASH, token.PERCENT:
		return 10
	case token.EXP:
		return 11
	default:
		return 0
	}
}

// isAssignOp reports whether t is a simple or compound assignment operator.
func isAssignOp(t token.Type) bool {
	switch t {
	case token.ASSIGN, token.PLUS_ASSIGN, token.MINUS_ASSIGN, token.STAR_ASSIGN,
		token.SLASH_ASSIGN, token.PERCENT_ASSIGN, token.EXP_ASSIGN,
		token.SHL_ASSIGN, token.SHR_ASSIGN, token.USHR_ASSIGN,
		token.BIT_AND_ASSIGN, token.BIT_OR_ASSIGN, token.BIT_XOR_ASSIGN,
		token.AND_ASSIGN, token.OR_ASSIGN, token.NULLISH_ASSIGN:
		return true
	}
	return false
}

// parseExpression parses a full expression, including the comma operator.
func (p *parser) parseExpression() ast.Expr {
	first := p.parseAssignExpr()
	if !p.at(token.COMMA) {
		return first
	}
	exprs := []ast.Expr{first}
	for p.accept(token.COMMA) {
		exprs = append(exprs, p.parseAssignExpr())
	}
	return &ast.SequenceExpr{Exprs: exprs}
}

// parseAssignExpr parses an assignment expression. This is the level at which
// arrow functions and yield expressions are recognized.
func (p *parser) parseAssignExpr() ast.Expr {
	if !p.enter() {
		return &ast.Ident{NamePos: p.cur().Pos}
	}
	defer p.leave()

	// Arrow functions require lookahead to distinguish from a parenthesized
	// expression; detect and parse them up front.
	if arrow := p.tryParseArrow(); arrow != nil {
		return arrow
	}

	// yield expression (only meaningful inside a generator, but we parse it
	// wherever `yield` appears as a leading keyword).
	if p.at(token.YIELD) {
		return p.parseYield()
	}

	left := p.parseConditional()

	if isAssignOp(p.cur().Type) {
		opTok := p.next()
		value := p.parseAssignExpr()
		return &ast.AssignExpr{Target: left, OpPos: opTok.Pos, Op: opTok.Type, Value: value}
	}
	return left
}

// parseYield parses a yield or yield* expression.
func (p *parser) parseYield() ast.Expr {
	kw := p.next() // yield
	y := &ast.YieldExpr{Keyword: kw.Pos}
	if p.accept(token.STAR) {
		y.Delegate = true
	}
	// A yield with no argument is legal; an argument follows only if one could
	// begin on the same line and the next token can start an expression.
	if !p.cur().NewlineBefore && canStartExpr(p.cur().Type) {
		y.Argument = p.parseAssignExpr()
	}
	return y
}

// parseConditional parses the ternary operator, falling through to binary.
func (p *parser) parseConditional() ast.Expr {
	cond := p.parseBinary(0)
	if !p.at(token.QUESTION) {
		return cond
	}
	p.next() // ?
	// The consequent is parsed without the `in` restriction and cannot itself
	// be a bare comma sequence.
	saveNoIn := p.noIn
	p.noIn = false
	cons := p.parseAssignExpr()
	p.noIn = saveNoIn
	p.expect(token.COLON)
	alt := p.parseAssignExpr()
	return &ast.ConditionalExpr{Test: cond, Consequent: cons, Alternate: alt}
}

// parseBinary implements precedence climbing over binary and logical operators.
func (p *parser) parseBinary(minPrec int) ast.Expr {
	left := p.parseUnary()
	for {
		opType := p.cur().Type
		prec := p.binaryPrec(opType)
		if prec == 0 || prec < minPrec {
			break
		}
		opTok := p.next()
		// ** is right-associative; all others are left-associative.
		nextMin := prec + 1
		if opType == token.EXP {
			nextMin = prec
		}
		right := p.parseBinary(nextMin)
		if opType == token.AND || opType == token.OR || opType == token.NULLISH {
			left = &ast.LogicalExpr{Left: left, OpPos: opTok.Pos, Op: opType, Right: right}
		} else {
			left = &ast.BinaryExpr{Left: left, OpPos: opTok.Pos, Op: opType, Right: right}
		}
	}
	return left
}

// parseUnary parses prefix unary and update operators.
func (p *parser) parseUnary() ast.Expr {
	t := p.cur().Type
	switch t {
	case token.NOT, token.BIT_NOT, token.PLUS, token.MINUS,
		token.TYPEOF, token.VOID, token.DELETE:
		op := p.next()
		operand := p.parseUnary()
		// `delete obj.#priv` is an early SyntaxError: private members cannot be
		// deleted.
		if op.Type == token.DELETE {
			if m, ok := operand.(*ast.MemberExpr); ok {
				if _, ok := m.Property.(*ast.PrivateIdent); ok {
					p.errorAt(m.Property.Pos(), "Private fields can not be deleted")
				}
			}
		}
		return &ast.UnaryExpr{OpPos: op.Pos, Op: op.Type, Operand: operand}
	case token.INC, token.DEC:
		op := p.next()
		operand := p.parseUnary()
		return &ast.UpdateExpr{OpPos: op.Pos, Op: op.Type, Operand: operand, Prefix: true}
	case token.AWAIT:
		op := p.next()
		operand := p.parseUnary()
		return &ast.AwaitExpr{Keyword: op.Pos, Argument: operand}
	}
	return p.parsePostfix()
}

// parsePostfix parses postfix ++ and -- after a left-hand-side expression.
func (p *parser) parsePostfix() ast.Expr {
	expr := p.parseLeftHandSide()
	if (p.at(token.INC) || p.at(token.DEC)) && !p.cur().NewlineBefore {
		op := p.next()
		return &ast.UpdateExpr{OpPos: op.Pos, Op: op.Type, Operand: expr, Prefix: false}
	}
	return expr
}

// parseLeftHandSide parses member access, calls, and new-expressions.
func (p *parser) parseLeftHandSide() ast.Expr {
	var expr ast.Expr
	if p.at(token.NEW) {
		expr = p.parseNew()
	} else {
		expr = p.parsePrimary()
	}
	return p.parseCallMemberTail(expr)
}

// parseNew parses a new-expression. `new.target` is recognized as a special
// meta-property.
func (p *parser) parseNew() ast.Expr {
	kw := p.next() // new
	if p.at(token.DOT) {
		p.next()
		p.expect(token.IDENT) // target
		return &ast.Ident{NamePos: kw.Pos, Name: "new.target"}
	}
	// The callee is a member expression but NOT a call (arguments bind to the
	// new-expression itself).
	var callee ast.Expr
	if p.at(token.NEW) {
		callee = p.parseNew()
	} else {
		callee = p.parsePrimary()
	}
	callee = p.parseMemberTail(callee)

	ne := &ast.NewExpr{Keyword: kw.Pos, Callee: callee, EndPos: callee.End()}
	if p.at(token.LPAREN) {
		args, rparen := p.parseArguments()
		ne.Arguments = args
		ne.EndPos = rparen.End
	}
	return ne
}

// parseMemberTail parses only member accesses (. and []), not calls. Used for a
// new-expression callee.
func (p *parser) parseMemberTail(expr ast.Expr) ast.Expr {
	for {
		switch p.cur().Type {
		case token.DOT:
			p.next()
			prop := p.parseMemberName()
			expr = &ast.MemberExpr{Object: expr, Property: prop, EndPos: prop.End()}
		case token.LBRACKET:
			p.next()
			idx := p.parseExpression()
			rb := p.expect(token.RBRACKET)
			expr = &ast.MemberExpr{Object: expr, Property: idx, Computed: true, EndPos: rb.End}
		default:
			return expr
		}
	}
}

// parseCallMemberTail parses the full call/member/optional-chain/tagged-template
// suffix chain after a primary expression.
func (p *parser) parseCallMemberTail(expr ast.Expr) ast.Expr {
	for {
		switch p.cur().Type {
		case token.DOT:
			p.next()
			prop := p.parseMemberName()
			expr = &ast.MemberExpr{Object: expr, Property: prop, EndPos: prop.End()}
		case token.OPTIONAL:
			p.next()
			switch {
			case p.at(token.LPAREN):
				args, rparen := p.parseArguments()
				expr = &ast.CallExpr{Callee: expr, Arguments: args, Rparen: rparen.Pos, Optional: true}
			case p.at(token.LBRACKET):
				p.next()
				idx := p.parseExpression()
				rb := p.expect(token.RBRACKET)
				expr = &ast.MemberExpr{Object: expr, Property: idx, Computed: true, Optional: true, EndPos: rb.End}
			default:
				prop := p.parseMemberName()
				expr = &ast.MemberExpr{Object: expr, Property: prop, Optional: true, EndPos: prop.End()}
			}
		case token.LBRACKET:
			p.next()
			idx := p.parseExpression()
			rb := p.expect(token.RBRACKET)
			expr = &ast.MemberExpr{Object: expr, Property: idx, Computed: true, EndPos: rb.End}
		case token.LPAREN:
			args, rparen := p.parseArguments()
			expr = &ast.CallExpr{Callee: expr, Arguments: args, Rparen: rparen.Pos}
		case token.TEMPLATE_NOSUB, token.TEMPLATE_HEAD:
			quasi := p.parseTemplate()
			expr = &ast.TaggedTemplateExpr{Tag: expr, Quasi: quasi}
		default:
			return expr
		}
	}
}

// parseMemberName parses the property name after a dot (an identifier, any
// keyword used as a name, or a private name).
func (p *parser) parseMemberName() ast.Expr {
	tk := p.cur()
	if tk.Type == token.PRIVATE {
		p.next()
		p.recordPrivateRef(tk)
		return &ast.PrivateIdent{NamePos: tk.Pos, Name: tk.Literal}
	}
	// Any identifier-like token (including reserved words) is a valid property
	// name after a dot.
	if tk.Type == token.IDENT || tk.Type.IsKeyword() {
		p.next()
		return &ast.Ident{NamePos: tk.Pos, Name: identText(tk)}
	}
	p.errorf("expected property name but got %s", tk.Type)
	return &ast.Ident{NamePos: tk.Pos}
}

// parseArguments parses a parenthesized, comma-separated argument list,
// allowing spread elements. It returns the arguments and the closing paren.
func (p *parser) parseArguments() ([]ast.Expr, token.Token) {
	p.expect(token.LPAREN)
	var args []ast.Expr
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		if p.at(token.ELLIPSIS) {
			ell := p.next()
			arg := p.parseAssignExpr()
			args = append(args, &ast.SpreadElement{Ellipsis: ell.Pos, Argument: arg})
		} else {
			args = append(args, p.parseAssignExpr())
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	rparen := p.expect(token.RPAREN)
	return args, rparen
}

// ---------------------------------------------------------------------------
// Primary expressions
// ---------------------------------------------------------------------------

// parsePrimary parses a primary expression: a literal, identifier, grouping,
// array/object literal, function/class expression, template, or regex.
func (p *parser) parsePrimary() ast.Expr {
	tk := p.cur()
	switch tk.Type {
	case token.NUMBER:
		p.next()
		if p.strict && tk.StrictError != "" {
			p.errorAt(tk.Pos, "%s", tk.StrictError)
		}
		return &ast.NumberLit{ValuePos: tk.Pos, Value: parseNumber(tk.Literal), Raw: tk.Raw}
	case token.BIGINT:
		p.next()
		return &ast.BigIntLit{ValuePos: tk.Pos, Raw: tk.Raw, Digits: tk.Literal}
	case token.STRING:
		p.next()
		if p.strict && tk.StrictError != "" {
			p.errorAt(tk.Pos, "%s", tk.StrictError)
		}
		return &ast.StringLit{ValuePos: tk.Pos, Value: tk.Literal, Raw: tk.Raw}
	case token.TRUE, token.FALSE:
		p.next()
		return &ast.BoolLit{ValuePos: tk.Pos, Value: tk.Type == token.TRUE}
	case token.NULL:
		p.next()
		return &ast.NullLit{ValuePos: tk.Pos}
	case token.THIS:
		p.next()
		return &ast.ThisExpr{Keyword: tk.Pos}
	case token.SUPER:
		p.next()
		return &ast.SuperExpr{Keyword: tk.Pos}
	case token.IDENT:
		p.next()
		return &ast.Ident{NamePos: tk.Pos, Name: tk.Literal}
	case token.REGEX:
		p.next()
		flags := regexFlags(tk.Raw)
		unicode := strings.ContainsRune(flags, 'u') || strings.ContainsRune(flags, 'v')
		if err := validateRegexpLiteral(tk.Literal, unicode); err != nil {
			p.errorAt(tk.Pos, "%s", err.Error())
		}
		return &ast.RegexLit{ValuePos: tk.Pos, Pattern: tk.Literal, Flags: flags, Raw: tk.Raw}
	case token.TEMPLATE_NOSUB, token.TEMPLATE_HEAD:
		return p.parseTemplate()
	case token.LPAREN:
		return p.parseParenExpr()
	case token.LBRACKET:
		return p.parseArrayLit()
	case token.LBRACE:
		return p.parseObjectLit()
	case token.FUNCTION:
		return p.parseFunctionExpr(false)
	case token.CLASS:
		return p.parseClassExpr()
	case token.ASYNC:
		// `async function …` expression; a bare `async` is otherwise an
		// identifier (arrow forms are handled earlier in parseAssignExpr).
		if p.peek(1).Type == token.FUNCTION && !p.peek(1).NewlineBefore {
			p.next() // async
			fn := p.parseFunctionExpr(false)
			fn.Def.Async = true
			return fn
		}
		p.next()
		return &ast.Ident{NamePos: tk.Pos, Name: "async"}
	case token.PRIVATE:
		// `#field in obj` ergonomic brand check — only valid inside a class.
		p.next()
		p.recordPrivateRef(tk)
		return &ast.PrivateIdent{NamePos: tk.Pos, Name: tk.Literal}
	default:
		// Contextual keywords (let, of, get, set, yield, await, static) used as
		// plain identifiers.
		if tk.Type.IsKeyword() {
			p.next()
			return &ast.Ident{NamePos: tk.Pos, Name: identText(tk)}
		}
		p.errorf("unexpected token %s", tk.Type)
		p.next()
		return &ast.Ident{NamePos: tk.Pos}
	}
}

// parseParenExpr parses a parenthesized expression. (Arrow functions with a
// parenthesized parameter list are handled earlier by tryParseArrow.)
func (p *parser) parseParenExpr() ast.Expr {
	p.expect(token.LPAREN)
	saveNoIn := p.noIn
	p.noIn = false
	expr := p.parseExpression()
	p.noIn = saveNoIn
	p.expect(token.RPAREN)
	return expr
}

// parseArrayLit parses an array literal, allowing holes (elisions) and spread
// elements.
func (p *parser) parseArrayLit() ast.Expr {
	lb := p.next() // [
	arr := &ast.ArrayLit{Lbracket: lb.Pos}
	for !p.at(token.RBRACKET) && !p.at(token.EOF) {
		if p.at(token.COMMA) {
			// A hole: elided element.
			p.next()
			arr.Elements = append(arr.Elements, nil)
			continue
		}
		if p.at(token.ELLIPSIS) {
			ell := p.next()
			arg := p.parseAssignExpr()
			arr.Elements = append(arr.Elements, &ast.SpreadElement{Ellipsis: ell.Pos, Argument: arg})
		} else {
			arr.Elements = append(arr.Elements, p.parseAssignExpr())
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	rb := p.expect(token.RBRACKET)
	arr.Rbracket = rb.Pos
	return arr
}

// parseTemplate parses a template literal, reassembling its quasi segments and
// interpolated expressions.
func (p *parser) parseTemplate() *ast.TemplateLit {
	first := p.cur()
	tl := &ast.TemplateLit{Start: first.Pos}
	if first.Type == token.TEMPLATE_NOSUB {
		p.next()
		tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: first.Pos, Cooked: first.Literal, Raw: first.Raw})
		tl.EndPos = first.End
		return tl
	}
	// TEMPLATE_HEAD expr (TEMPLATE_MIDDLE expr)* TEMPLATE_TAIL
	head := p.next()
	tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: head.Pos, Cooked: head.Literal, Raw: head.Raw})
	for {
		tl.Exprs = append(tl.Exprs, p.parseExpression())
		seg := p.cur()
		switch seg.Type {
		case token.TEMPLATE_MIDDLE:
			p.next()
			tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: seg.Pos, Cooked: seg.Literal, Raw: seg.Raw})
		case token.TEMPLATE_TAIL:
			p.next()
			tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: seg.Pos, Cooked: seg.Literal, Raw: seg.Raw})
			tl.EndPos = seg.End
			return tl
		default:
			p.errorf("unterminated template literal")
			tl.EndPos = seg.Pos
			return tl
		}
	}
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// identText returns the source text to use for a token used as an identifier
// name (its literal for IDENT, or the keyword spelling otherwise).
func identText(tk token.Token) string {
	if tk.Literal != "" {
		return tk.Literal
	}
	return tk.Type.String()
}

// regexFlags extracts the flag suffix from a regex literal's raw text
// /pattern/flags.
func regexFlags(raw string) string {
	i := strings.LastIndexByte(raw, '/')
	if i < 0 || i+1 >= len(raw) {
		return ""
	}
	return raw[i+1:]
}

// canStartExpr reports whether a token type can begin an expression. Used for
// optional-argument forms (return, yield, throw) under ASI.
func canStartExpr(t token.Type) bool {
	switch t {
	case token.SEMICOLON, token.RPAREN, token.RBRACE, token.RBRACKET,
		token.COMMA, token.COLON, token.EOF:
		return false
	}
	return true
}
