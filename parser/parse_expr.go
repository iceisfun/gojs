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

	// `yield` begins a YieldExpression only where it is reserved: inside a
	// generator (the [+Yield] grammar parameter), or in strict-mode code (where it
	// is never a valid identifier and parseYield reports the error). In sloppy
	// non-generator code `yield` is an ordinary IdentifierReference, so fall
	// through to the normal expression path, which yields an Ident.
	if p.at(token.YIELD) && p.yieldIsReserved() {
		return p.parseYield()
	}

	left := p.parseConditional()

	if isAssignOp(p.cur().Type) {
		opTok := p.next()
		// Static Semantics: the LHS must be a valid assignment target. A simple
		// assignment (=) additionally permits a refinable destructuring pattern;
		// a compound assignment permits only a simple target.
		p.validateAssignExprTarget(left, opTok.Type)
		value := p.parseAssignExpr()
		return &ast.AssignExpr{Target: left, OpPos: opTok.Pos, Op: opTok.Type, Value: value}
	}
	return left
}

// parseYield parses a yield or yield* expression.
func (p *parser) parseYield() ast.Expr {
	kw := p.next() // yield
	// A YieldExpression may not appear in a generator's formal parameter list
	// (ECMA-262 UniqueFormalParameters / CreateDynamicFunction).
	if p.inParams && p.inGenerator {
		p.errorAt(kw.Pos, "yield expression is not allowed in formal parameters")
	}
	// In strict-mode code outside a generator, `yield` is a reserved word and may
	// not begin a YieldExpression or be used as an identifier (e.g. in the formal
	// parameters or body of a non-generator class method, which is always strict).
	if p.strict && !p.inGenerator {
		p.errorAt(kw.Pos, "yield is only valid inside a generator")
	}
	y := &ast.YieldExpr{Keyword: kw.Pos}
	// `yield *` is a single token sequence: no LineTerminator may appear between
	// `yield` and `*` (the grammar has no [no LineTerminator here] escape, so ASI
	// after a lone `yield` makes a following `*` a new, invalid statement).
	if p.at(token.STAR) && !p.cur().NewlineBefore {
		p.next()
		y.Delegate = true
	}
	if y.Delegate {
		// `yield * AssignmentExpression`: the operand is mandatory, and unlike the
		// plain `yield` form it is not subject to a [no LineTerminator here]
		// restriction — it may appear on the following line.
		y.Argument = p.parseAssignExpr()
	} else if !p.cur().NewlineBefore && canStartExpr(p.cur().Type) {
		// A plain `yield` with no argument is legal; an argument follows only if one
		// could begin on the same line and the next token can start an expression.
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
	// The base of ** must be an UpdateExpression, not a UnaryExpression
	// (ExponentiationExpression : UpdateExpression ** ExponentiationExpression).
	// So a bare unary operator immediately before ** — e.g. -a ** b, !a ** b,
	// typeof a ** b — is an early SyntaxError; the base must be parenthesized.
	// A parenthesized base starts with '(' rather than the unary operator, and
	// ++a / a++ are UpdateExpressions, so only the prefix unary/await operators
	// qualify.
	startTok := p.cur().Type
	left := p.parseUnary()
	bareUnary := false
	switch startTok {
	case token.NOT, token.BIT_NOT, token.PLUS, token.MINUS,
		token.TYPEOF, token.VOID, token.DELETE:
		bareUnary = true
	case token.AWAIT:
		_, bareUnary = left.(*ast.AwaitExpr)
	}
	// A PrivateIdentifier is only legal as the immediate left operand of `in`
	// (RelationalExpression : PrivateIdentifier in ShiftExpression, §13.10.1).
	// In any other position — a standalone `#x`, the left operand of a different
	// operator, or (caught below) the right operand of `in` — it is a SyntaxError.
	if pi, ok := left.(*ast.PrivateIdent); ok && p.cur().Type != token.IN {
		p.errorAt(pi.NamePos, "private identifier is only valid as the left operand of 'in'")
	}
	for {
		opType := p.cur().Type
		prec := p.binaryPrec(opType)
		if prec == 0 || prec < minPrec {
			break
		}
		if opType == token.EXP && bareUnary {
			p.errorAt(p.cur().Pos, "Unary operator used immediately before exponentiation expression; wrap the base in parentheses")
		}
		bareUnary = false
		opTok := p.next()
		// ** is right-associative; all others are left-associative.
		nextMin := prec + 1
		if opType == token.EXP {
			nextMin = prec
		}
		right := p.parseBinary(nextMin)
		// The RHS of `in` (a ShiftExpression) — and the RHS of every other binary
		// operator — is never a PrivateIdentifier, so `#a in #b in c` is a
		// SyntaxError even though each `#…` sits to the left of an `in`.
		if pi, ok := right.(*ast.PrivateIdent); ok {
			p.errorAt(pi.NamePos, "private identifier is only valid as the left operand of 'in'")
		}
		if opType == token.AND || opType == token.OR || opType == token.NULLISH {
			// CoalesceExpression (§13.13): `??` may not be combined with `||` or `&&`
			// without parentheses. Its operands are BitwiseORExpressions, and a
			// CoalesceExpression is not a valid operand of `||`/`&&`, so the two
			// families never mix at the same unparenthesized level. Parenthesizing an
			// operand severs it (tracked via p.parenthesized), so `(a ?? b) || c` and
			// `a ?? (b && c)` remain legal.
			if opType == token.NULLISH {
				p.checkNotLogicalOperand(left)
				p.checkNotLogicalOperand(right)
			} else {
				p.checkNotCoalesceOperand(left)
				p.checkNotCoalesceOperand(right)
			}
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
			// In strict mode, deleting a bare IdentifierReference is an early
			// SyntaxError (ECMA-262 §13.5.1.1). Parentheses do not change the operand
			// node's type, so `delete x`, `delete (x)`, and `delete ((x))` are all
			// rejected, while `delete x.y`, `delete (x, y)`, and `delete f()` are not.
			if p.strict {
				// new.target is a MetaProperty, not an IdentifierReference, so
				// `delete new.target` is permitted (and evaluates to true) even in
				// strict mode.
				if id, ok := operand.(*ast.Ident); ok && id.Name != "new.target" {
					p.errorAt(id.NamePos, "Delete of an unqualified identifier in strict mode")
				}
			}
		}
		return &ast.UnaryExpr{OpPos: op.Pos, Op: op.Type, Operand: operand}
	case token.INC, token.DEC:
		op := p.next()
		operand := p.parseUnary()
		// The operand of a prefix update expression must be a simple assignment
		// target (Static Semantics: UpdateExpression early errors).
		p.checkSimpleAssignmentTarget(operand)
		return &ast.UpdateExpr{OpPos: op.Pos, Op: op.Type, Operand: operand, Prefix: true}
	case token.AWAIT:
		// `await` is reserved directly inside a class static initialization block:
		// it is neither a valid AwaitExpression (a static block is not async) nor a
		// valid identifier there (ECMA-262 ClassStaticBlockBody is parsed with
		// [+Await]). The reservation does not cross a function/arrow boundary.
		if p.staticBlockAwait {
			op := p.next()
			p.errorAt(op.Pos, "await is not allowed in a class static initialization block")
			operand := p.parseUnary()
			return &ast.AwaitExpr{Keyword: op.Pos, Argument: operand}
		}
		// `await` is the AwaitExpression operator only where the [Await] grammar
		// parameter holds: inside an async function or async arrow (p.inAsync), or
		// at the top level of a module (top-level await). Everywhere else — sync
		// functions and generators, script global code, and any function nested
		// inside an async one — `await` is a plain IdentifierReference, so fall
		// through to parsePrimary, which yields an Ident.
		if p.awaitIsReserved() {
			op := p.next()
			// An AwaitExpression may not appear in an async function's formal
			// parameter list (ECMA-262 CreateDynamicFunction, step for
			// "async"/"async generator").
			if p.inParams && p.inAsync {
				p.errorAt(op.Pos, "await expression is not allowed in formal parameters")
			}
			operand := p.parseUnary()
			return &ast.AwaitExpr{Keyword: op.Pos, Argument: operand}
		}
	}
	return p.parsePostfix()
}

// parsePostfix parses postfix ++ and -- after a left-hand-side expression.
func (p *parser) parsePostfix() ast.Expr {
	expr := p.parseLeftHandSide()
	if (p.at(token.INC) || p.at(token.DEC)) && !p.cur().NewlineBefore {
		op := p.next()
		// The operand of a postfix update expression must be a simple assignment
		// target (Static Semantics: UpdateExpression early errors).
		p.checkSimpleAssignmentTarget(expr)
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
		tgt := p.expect(token.IDENT) // target
		// The `target` of the new.target meta-property is a fixed token, not an
		// IdentifierName, so it may not be written with a Unicode escape (§13.3.12).
		if tgt.Escaped {
			p.errorAt(tgt.Pos, "'new.target' must not contain escaped characters")
		}
		// new.target is only valid inside a function; ordinary parsing leaves it
		// permitted (runtime resolves it), but indirect/global eval forbids it.
		if !p.newTargetOK {
			p.errorAt(kw.Pos, "new.target is only valid inside a function")
		}
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
	// ImportCall is a CallExpression, not a MemberExpression, so a *direct*
	// import() may not be the callee of a `new` expression: `new import(x)` and
	// `new import(x).p` are both SyntaxErrors (ECMA-262 sec-import-calls;
	// NewExpression takes a MemberExpression). A parenthesized import() is a
	// PrimaryExpression, though, so `new (import(x))` is valid — the parentheses
	// make the covered expression a MemberExpression.
	if ic, isImportCall := callee.(*ast.ImportCall); isImportCall && !p.parenthesized[callee] {
		p.errorAt(ic.Pos(), "import() is not a valid callee for a new expression")
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
			p.checkSuperPrivate(expr, prop)
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

// checkSuperPrivate reports the early error for a SuperProperty whose member is
// a private name (`super.#x`), which the grammar does not permit (ECMA-262
// MemberExpression : SuperProperty has no private-name production).
func (p *parser) checkSuperPrivate(obj, prop ast.Expr) {
	if _, isSuper := obj.(*ast.SuperExpr); !isSuper {
		return
	}
	if priv, ok := prop.(*ast.PrivateIdent); ok {
		p.errorAt(priv.Pos(), "Private field '%s' must not be accessed on super", priv.Name)
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
			p.checkSuperPrivate(expr, prop)
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
			// A tagged template's tag may not be an optional chain (§13.3.1 early
			// error): `a?.b`x`` and `a?.fn`x`` are SyntaxErrors. Parenthesizing the
			// chain severs it, so `(a?.b)`x`` remains legal.
			if p.isUnparenthesizedOptionalChain(expr) {
				p.errorAt(p.cur().Pos, "Tagged template may not follow an optional chain")
			}
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

// parseImportCall parses a dynamic import: import(AssignmentExpression) or
// import(AssignmentExpression, Options). The cursor is on the `import` keyword.
func (p *parser) parseImportCall() ast.Expr {
	kw := p.next() // import
	p.expect(token.LPAREN)
	spec := p.parseAssignExpr()
	var opts ast.Expr
	if p.accept(token.COMMA) {
		// A second (options) argument is optional, as is a trailing comma.
		if !p.at(token.RPAREN) {
			opts = p.parseAssignExpr()
			p.accept(token.COMMA)
		}
	}
	rparen := p.expect(token.RPAREN)
	return &ast.ImportCall{Keyword: kw.Pos, Specifier: spec, Options: opts, Rparen: rparen.Pos}
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
		// A SuperCall (super(...)) is permitted only in a derived class
		// constructor; a SuperProperty (super.x / super[x]) only where a
		// [[HomeObject]] is in scope — a method, accessor, constructor, or field
		// initializer (ECMA-262 13.3.7.1). Global script/module top level forbids
		// both; a regular function or method re-enables SuperProperty as
		// appropriate; indirect/global eval seeds the context via ParseEval.
		switch {
		case p.at(token.LPAREN) && !p.superCallOK:
			p.errorAt(tk.Pos, "super() is only valid in a derived class constructor")
		case (p.at(token.DOT) || p.at(token.LBRACKET)) && !p.superPropOK:
			p.errorAt(tk.Pos, "'super' keyword is only valid inside a method")
		}
		return &ast.SuperExpr{Keyword: tk.Pos}
	case token.IDENT:
		p.next()
		p.checkEscapedReserved(tk)
		// A strict future reserved word (implements, public, …) is not a valid
		// IdentifierReference in strict-mode code.
		p.checkReservedIdentifier(tk.Literal, tk.Pos)
		if p.inFieldInit && tk.Literal == "arguments" {
			p.errorAt(tk.Pos, "'arguments' is not allowed in a class field initializer")
		}
		if p.inStaticBlock && tk.Literal == "arguments" {
			p.errorAt(tk.Pos, "'arguments' is not allowed in a class static initialization block")
		}
		return &ast.Ident{NamePos: tk.Pos, Name: tk.Literal}
	case token.REGEX:
		p.next()
		flags := regexFlags(tk.Raw)
		if err := validateRegexpLiteral(tk.Literal, flags); err != nil {
			p.errorAt(tk.Pos, "%s", err.Error())
		}
		return &ast.RegexLit{ValuePos: tk.Pos, Pattern: tk.Literal, Flags: flags, Raw: tk.Raw}
	case token.TEMPLATE_NOSUB, token.TEMPLATE_HEAD:
		tl := p.parseTemplate()
		// An untagged template must have a cooked value for every segment: a legacy
		// octal, \8/\9, or malformed hex/unicode escape is an early SyntaxError
		// with no Annex B leniency (ECMA-262 §12.9.6). (A tagged template is parsed
		// via parseCallMemberTail and tolerates such escapes.)
		for _, q := range tl.Quasis {
			if q.CookedInvalid {
				p.errorAt(q.Pos, "invalid escape sequence in template literal")
				break
			}
		}
		return tl
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
			// Parse with async=true so the parameter list is parsed in an async
			// context (an AwaitExpression in the parameters is an early error).
			return p.parseFunctionExpr(true)
		}
		p.next()
		return &ast.Ident{NamePos: tk.Pos, Name: "async"}
	case token.IMPORT:
		// `import` is a reserved word. In expression position the ONLY valid forms
		// are the dynamic ImportCall `import ( … )` (ES2020) and the `import.meta`
		// meta-property (module code; the runtime enforces the module restriction).
		// A bare `import`, or `import.` followed by anything other than `meta`
		// (e.g. import.source / import.defer / import.UNKNOWN — unsupported proposals
		// or typos), is a SyntaxError (ECMA-262 sec-imports, sec-import-calls).
		if p.peek(1).Type == token.LPAREN {
			return p.parseImportCall()
		}
		// import.meta: return the bare `import` identifier and let the member tail
		// consume `.meta`, preserving the existing AST shape (MemberExpr). The
		// `meta` contextual keyword is a fixed token and may not be escaped.
		if p.peek(1).Type == token.DOT && p.peek(2).Type == token.IDENT &&
			p.peek(2).Literal == "meta" && !p.peek(2).Escaped {
			// It is an early Syntax Error for import.meta to appear unless the
			// syntactic goal symbol is Module (ECMA-262 §13.3.12.1). A Script — and
			// a body/params parsed by the Function constructor — is not a Module.
			if !p.moduleMode {
				p.errorAt(tk.Pos, "import.meta is only allowed in a module")
			}
			p.next() // import
			return &ast.Ident{NamePos: tk.Pos, Name: "import"}
		}
		p.errorAt(tk.Pos, "'import' is only valid as import(...) or import.meta")
		p.next()
		return &ast.Ident{NamePos: tk.Pos, Name: "import"}
	case token.PRIVATE:
		// `#field in obj` ergonomic brand check — only valid inside a class.
		p.next()
		p.recordPrivateRef(tk)
		return &ast.PrivateIdent{NamePos: tk.Pos, Name: tk.Literal}
	default:
		// Contextual keywords (let, of, get, set, yield, await, static) used as
		// plain identifiers. yield/await used here as an identifier reference are
		// still reserved in a generator/async or strict context. A genuine
		// ReservedWord (case, catch, default, else, finally, in, instanceof, …)
		// may never be an IdentifierReference (ECMA-262 §13.1.1, Identifier :
		// IdentifierName but not ReservedWord), so it is an early SyntaxError here.
		if tk.Type.IsKeyword() {
			if !isContextualKeyword(tk.Type) {
				p.errorf("unexpected token %s", tk.Type)
				p.next()
				return &ast.Ident{NamePos: tk.Pos}
			}
			p.next()
			name := identText(tk)
			p.checkReservedIdentifier(name, tk.Pos)
			return &ast.Ident{NamePos: tk.Pos, Name: name}
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
	// Record that this node was parenthesized. The parser keeps no distinct paren
	// AST node, so this is how later early-error checks tell `({})`/`(a?.b)` from a
	// bare `{}`/`a?.b`: parenthesization changes AssignmentTargetType and severs an
	// optional chain for tagged-template and coalesce-mixing purposes.
	p.parenthesized[expr] = true
	// A parenthesized identifier is not an IdentifierRef (its IsIdentifierRef is
	// false), so it must not trigger NamedEvaluation as an assignment target. Mark
	// the node so the interpreter can tell `(fn) = f` from `fn = f`.
	if id, ok := expr.(*ast.Ident); ok {
		id.Parenthesized = true
	}
	return expr
}

// parseArrayLit parses an array literal, allowing holes (elisions) and spread
// elements.
func (p *parser) parseArrayLit() ast.Expr {
	lb := p.next() // [
	// Brackets open a fresh expression context: the `in` operator is permitted
	// inside element expressions (e.g. default initializers) even within a
	// for-statement header, where the top-level `in` is otherwise suppressed.
	saveNoIn := p.noIn
	p.noIn = false
	defer func() { p.noIn = saveNoIn }()
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
		// A comma was consumed; if the next token closes the literal it was a
		// trailing comma (an elision that the ArrayAssignmentPattern refinement
		// forbids after a rest element).
		if p.at(token.RBRACKET) {
			arr.TrailingComma = true
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
		tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: first.Pos, Cooked: first.Literal, Raw: first.Raw, CookedInvalid: first.CookedInvalid})
		tl.EndPos = first.End
		return tl
	}
	// TEMPLATE_HEAD expr (TEMPLATE_MIDDLE expr)* TEMPLATE_TAIL
	head := p.next()
	tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: head.Pos, Cooked: head.Literal, Raw: head.Raw, CookedInvalid: head.CookedInvalid})
	for {
		tl.Exprs = append(tl.Exprs, p.parseExpression())
		seg := p.cur()
		switch seg.Type {
		case token.TEMPLATE_MIDDLE:
			p.next()
			tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: seg.Pos, Cooked: seg.Literal, Raw: seg.Raw, CookedInvalid: seg.CookedInvalid})
		case token.TEMPLATE_TAIL:
			p.next()
			tl.Quasis = append(tl.Quasis, ast.TemplateElement{Pos: seg.Pos, Cooked: seg.Literal, Raw: seg.Raw, CookedInvalid: seg.CookedInvalid})
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
		token.COMMA, token.COLON, token.EOF,
		// A template middle/tail closes an interpolation (`}...${` / `}...` + "`")
		// and can never begin an expression: a bare `yield`/`return`/`throw`
		// directly inside a substitution is argument-less.
		token.TEMPLATE_MIDDLE, token.TEMPLATE_TAIL:
		return false
	}
	return true
}
