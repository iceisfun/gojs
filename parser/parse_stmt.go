package parser

import (
	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements statement parsing by recursive descent.

// parseStmt parses a single statement.
func (p *parser) parseStmt() ast.Stmt {
	if !p.enter() {
		return &ast.EmptyStmt{Semicolon: p.cur().Pos}
	}
	defer p.leave()

	tk := p.cur()
	switch tk.Type {
	case token.LBRACE:
		return p.parseBlock()
	case token.SEMICOLON:
		p.next()
		return &ast.EmptyStmt{Semicolon: tk.Pos}
	case token.VAR, token.CONST:
		return p.parseVarDecl()
	case token.LET:
		// `let` is a declaration when followed by a binding start; otherwise it
		// is an identifier used as an expression statement.
		if p.letIsDeclaration() {
			return p.parseVarDecl()
		}
	case token.FUNCTION:
		return p.parseFunctionDecl(false)
	case token.ASYNC:
		if p.peek(1).Type == token.FUNCTION && !p.peek(1).NewlineBefore {
			p.next() // async
			return p.parseFunctionDecl(true)
		}
	case token.CLASS:
		return p.parseClassDecl()
	case token.IF:
		return p.parseIf()
	case token.FOR:
		return p.parseFor()
	case token.WHILE:
		return p.parseWhile()
	case token.DO:
		return p.parseDoWhile()
	case token.SWITCH:
		return p.parseSwitch()
	case token.RETURN:
		return p.parseReturn()
	case token.BREAK:
		return p.parseBreakContinue(true)
	case token.CONTINUE:
		return p.parseBreakContinue(false)
	case token.THROW:
		return p.parseThrow()
	case token.TRY:
		return p.parseTry()
	case token.DEBUGGER:
		p.next()
		end := p.cur().Pos
		p.expectSemicolon()
		return &ast.DebuggerStmt{Keyword: tk.Pos, EndPos: end}
	}

	// Labeled statement: IDENT ':' Statement.
	if tk.Type == token.IDENT && p.peek(1).Type == token.COLON {
		label := p.next()
		p.next() // ':'
		body := p.parseStmt()
		return &ast.LabeledStmt{Label: &ast.Ident{NamePos: label.Pos, Name: label.Literal}, Body: body}
	}

	return p.parseExprStmt()
}

// letIsDeclaration reports whether a `let` at the cursor begins a declaration
// (as opposed to being used as an identifier).
func (p *parser) letIsDeclaration() bool {
	switch p.peek(1).Type {
	case token.IDENT, token.LBRACKET, token.LBRACE:
		return true
	default:
		// let followed by a contextual keyword name (e.g. `let of`) is a decl.
		return p.peek(1).Type.IsKeyword()
	}
}

// parseBlock parses a brace-delimited block statement.
func (p *parser) parseBlock() *ast.BlockStmt {
	lb := p.expect(token.LBRACE)
	blk := &ast.BlockStmt{Lbrace: lb.Pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) && p.err == nil {
		blk.Body = append(blk.Body, p.parseStmt())
	}
	rb := p.expect(token.RBRACE)
	blk.Rbrace = rb.Pos
	return blk
}

// parseExprStmt parses an expression statement, recognizing directive-prologue
// string literals (e.g. "use strict").
func (p *parser) parseExprStmt() ast.Stmt {
	startTok := p.cur()
	expr := p.parseExpression()
	stmt := &ast.ExprStmt{X: expr}
	// A lone string-literal statement is a directive.
	if sl, ok := expr.(*ast.StringLit); ok && startTok.Type == token.STRING {
		stmt.Directive = sl.Value
	}
	p.expectSemicolon()
	return stmt
}

// parseVarDecl parses a var/let/const declaration statement.
func (p *parser) parseVarDecl() *ast.VarDecl {
	kw := p.next() // var/let/const
	decl := &ast.VarDecl{Keyword: kw.Pos, Kind: kw.Type}
	for {
		d := p.parseVarDeclarator()
		// A const declaration must have an initializer (ECMA-262 §14.3.1.1).
		if kw.Type == token.CONST && d.Init == nil {
			p.errorAt(d.Target.Pos(), "Missing initializer in const declaration")
		}
		decl.Decls = append(decl.Decls, d)
		if !p.accept(token.COMMA) {
			break
		}
	}
	decl.EndPos = p.cur().Pos
	p.expectSemicolon()
	return decl
}

// parseVarDeclarator parses a single `target = init` binding without consuming
// a terminating separator.
func (p *parser) parseVarDeclarator() *ast.VarDeclarator {
	target := p.parseBindingTarget()
	d := &ast.VarDeclarator{Target: target}
	if p.accept(token.ASSIGN) {
		d.Init = p.parseAssignExpr()
	}
	return d
}

// parseIf parses an if/else statement.
func (p *parser) parseIf() ast.Stmt {
	kw := p.next() // if
	p.expect(token.LPAREN)
	test := p.parseExpression()
	p.expect(token.RPAREN)
	cons := p.parseStmt()
	stmt := &ast.IfStmt{Keyword: kw.Pos, Test: test, Consequent: cons}
	if p.accept(token.ELSE) {
		stmt.Alternate = p.parseStmt()
	}
	return stmt
}

// parseFor parses for, for-in, and for-of statements.
func (p *parser) parseFor() ast.Stmt {
	kw := p.next() // for
	await := false
	if p.at(token.AWAIT) {
		p.next()
		await = true
	}
	p.expect(token.LPAREN)

	// Parse the initializer part, which may be a declaration or an expression,
	// while suppressing the `in` operator so for-in is not misparsed.
	var initNode ast.Node
	switch {
	case p.at(token.SEMICOLON):
		// no init
	case p.at(token.VAR) || p.at(token.CONST) || (p.at(token.LET) && p.letIsDeclaration()):
		kwTok := p.next()
		vd := &ast.VarDecl{Keyword: kwTok.Pos, Kind: kwTok.Type}
		p.noIn = true
		target := p.parseBindingTarget()
		p.noIn = false
		// for-in / for-of with a single declarator and no initializer.
		if p.at(token.IN) || p.at(token.OF) {
			isOf := p.at(token.OF)
			p.next()
			right := p.forRight(isOf)
			p.expect(token.RPAREN)
			vd.Decls = []*ast.VarDeclarator{{Target: target}}
			body := p.parseLoopBody()
			return &ast.ForInStmt{Keyword: kw.Pos, Left: vd, Right: right, Body: body, Of: isOf, Await: await}
		}
		// Otherwise a C-style declaration list.
		d := &ast.VarDeclarator{Target: target}
		p.noIn = true
		if p.accept(token.ASSIGN) {
			d.Init = p.parseAssignExpr()
		}
		vd.Decls = append(vd.Decls, d)
		for p.accept(token.COMMA) {
			vd.Decls = append(vd.Decls, p.parseVarDeclarator())
		}
		p.noIn = false
		initNode = vd
	default:
		// Expression initializer, possibly a for-in/of left-hand side.
		p.noIn = true
		expr := p.parseExpression()
		p.noIn = false
		if p.at(token.IN) || p.at(token.OF) {
			isOf := p.at(token.OF)
			p.next()
			right := p.forRight(isOf)
			p.expect(token.RPAREN)
			body := p.parseLoopBody()
			return &ast.ForInStmt{Keyword: kw.Pos, Left: expr, Right: right, Body: body, Of: isOf, Await: await}
		}
		initNode = expr
	}

	// C-style for(;;).
	p.expect(token.SEMICOLON)
	stmt := &ast.ForStmt{Keyword: kw.Pos, Init: initNode}
	if !p.at(token.SEMICOLON) {
		stmt.Test = p.parseExpression()
	}
	p.expect(token.SEMICOLON)
	if !p.at(token.RPAREN) {
		stmt.Update = p.parseExpression()
	}
	p.expect(token.RPAREN)
	stmt.Body = p.parseLoopBody()
	return stmt
}

// forRight parses the right-hand side of a for-in/for-of loop. for-of takes an
// AssignmentExpression; for-in takes a full Expression.
func (p *parser) forRight(isOf bool) ast.Expr {
	if isOf {
		return p.parseAssignExpr()
	}
	return p.parseExpression()
}

// parseLoopBody parses a loop body while tracking loop context for break/continue.
func (p *parser) parseLoopBody() ast.Stmt {
	p.inLoop++
	body := p.parseStmt()
	p.inLoop--
	return body
}

// parseWhile parses a while loop.
func (p *parser) parseWhile() ast.Stmt {
	kw := p.next()
	p.expect(token.LPAREN)
	test := p.parseExpression()
	p.expect(token.RPAREN)
	body := p.parseLoopBody()
	return &ast.WhileStmt{Keyword: kw.Pos, Test: test, Body: body}
}

// parseDoWhile parses a do/while loop.
func (p *parser) parseDoWhile() ast.Stmt {
	kw := p.next()
	body := p.parseLoopBody()
	p.expect(token.WHILE)
	p.expect(token.LPAREN)
	test := p.parseExpression()
	rp := p.expect(token.RPAREN)
	p.accept(token.SEMICOLON) // optional trailing semicolon
	return &ast.DoWhileStmt{Keyword: kw.Pos, Body: body, Test: test, EndPos: rp.End}
}

// parseSwitch parses a switch statement.
func (p *parser) parseSwitch() ast.Stmt {
	kw := p.next()
	p.expect(token.LPAREN)
	disc := p.parseExpression()
	p.expect(token.RPAREN)
	p.expect(token.LBRACE)
	stmt := &ast.SwitchStmt{Keyword: kw.Pos, Discriminant: disc}
	p.inSwitch++
	for !p.at(token.RBRACE) && !p.at(token.EOF) && p.err == nil {
		c := &ast.SwitchCase{CasePos: p.cur().Pos}
		switch {
		case p.accept(token.CASE):
			c.Test = p.parseExpression()
		case p.accept(token.DEFAULT):
			// default clause: Test stays nil.
		default:
			p.errorf("expected 'case' or 'default' but got %s", p.cur().Type)
			p.inSwitch--
			return stmt
		}
		p.expect(token.COLON)
		for !p.at(token.CASE) && !p.at(token.DEFAULT) && !p.at(token.RBRACE) && !p.at(token.EOF) && p.err == nil {
			c.Body = append(c.Body, p.parseStmt())
		}
		stmt.Cases = append(stmt.Cases, c)
	}
	p.inSwitch--
	rb := p.expect(token.RBRACE)
	stmt.Rbrace = rb.Pos
	return stmt
}

// parseReturn parses a return statement, honoring ASI for the optional argument.
func (p *parser) parseReturn() ast.Stmt {
	kw := p.next()
	stmt := &ast.ReturnStmt{Keyword: kw.Pos, EndPos: kw.End}
	if !p.at(token.SEMICOLON) && !p.at(token.RBRACE) && !p.at(token.EOF) && !p.cur().NewlineBefore {
		stmt.Argument = p.parseExpression()
		stmt.EndPos = stmt.Argument.End()
	}
	p.expectSemicolon()
	return stmt
}

// parseBreakContinue parses a break or continue statement with an optional label.
func (p *parser) parseBreakContinue(isBreak bool) ast.Stmt {
	kw := p.next()
	var label *ast.Ident
	end := kw.End
	// A label must appear on the same line (no ASI newline before it).
	if p.at(token.IDENT) && !p.cur().NewlineBefore {
		id := p.next()
		label = &ast.Ident{NamePos: id.Pos, Name: id.Literal}
		end = id.End
	}
	p.expectSemicolon()
	if isBreak {
		return &ast.BreakStmt{Keyword: kw.Pos, Label: label, EndPos: end}
	}
	return &ast.ContinueStmt{Keyword: kw.Pos, Label: label, EndPos: end}
}

// parseThrow parses a throw statement. A newline after `throw` is a syntax
// error (a throw argument is mandatory and cannot be ASI-terminated).
func (p *parser) parseThrow() ast.Stmt {
	kw := p.next()
	if p.cur().NewlineBefore {
		p.errorf("illegal newline after throw")
	}
	arg := p.parseExpression()
	stmt := &ast.ThrowStmt{Keyword: kw.Pos, Argument: arg, EndPos: arg.End()}
	p.expectSemicolon()
	return stmt
}

// parseTry parses a try/catch/finally statement.
func (p *parser) parseTry() ast.Stmt {
	kw := p.next()
	stmt := &ast.TryStmt{Keyword: kw.Pos, Block: p.parseBlock()}
	if p.at(token.CATCH) {
		cat := p.next()
		cc := &ast.CatchClause{Keyword: cat.Pos}
		if p.accept(token.LPAREN) {
			cc.Param = p.parseBindingTarget()
			p.expect(token.RPAREN)
		}
		cc.Body = p.parseBlock()
		stmt.Handler = cc
	}
	if p.accept(token.FINALLY) {
		stmt.Finalizer = p.parseBlock()
	}
	if stmt.Handler == nil && stmt.Finalizer == nil {
		p.errorf("missing catch or finally after try")
	}
	return stmt
}
