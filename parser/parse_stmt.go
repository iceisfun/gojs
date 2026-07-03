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

	// Labeled statement: IDENT ':' Statement. Detected before the concrete
	// statement dispatch so the label chain can accumulate before an iteration
	// statement consumes it (see parseLabeledStmt).
	if tk.Type == token.IDENT && p.peek(1).Type == token.COLON {
		return p.parseLabeledStmt()
	}

	// This is a concrete (non-labelled) statement, so it terminates the current
	// label chain. An IterationStatement (for/while/do) claims the pending labels
	// as iteration labels (legal `continue` targets); every other statement simply
	// clears them. Loop parsers perform the claim via consumePendingLabels(true).
	switch tk.Type {
	case token.FOR, token.WHILE, token.DO:
		// handled in the loop parser
	default:
		p.pendingLabels = nil
	}

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
	case token.WITH:
		return p.parseWith()
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
	case token.EXPORT:
		if p.moduleMode {
			return p.parseExport()
		}
	}

	return p.parseExprStmt()
}

// parseLabeledStmt parses a LabelledStatement (`IDENT : Statement`). The cursor
// is on the label identifier. It records the label in the enclosing label set so
// break/continue targets can be validated, rejecting a duplicate label in the
// same function (ECMA-262 §14.13.1 ContainsDuplicateLabels), and appends it to
// the pending-label chain so an immediately-following IterationStatement can
// treat it as a legal `continue` target.
func (p *parser) parseLabeledStmt() ast.Stmt {
	label := p.next()
	p.checkEscapedReserved(label)
	name := label.Literal
	// A LabelledStatement may not redeclare an enclosing label (within the same
	// function boundary).
	if p.hasBreakLabel(name) {
		p.errorAt(label.Pos, "Label '%s' has already been declared", name)
	}
	p.next() // ':'
	p.labelSet = append(p.labelSet, labelInfo{name: name})
	p.pendingLabels = append(p.pendingLabels, name)
	body := p.parseSubStatement(true)
	p.labelSet = p.labelSet[:len(p.labelSet)-1]
	return &ast.LabeledStmt{Label: &ast.Ident{NamePos: label.Pos, Name: name}, Body: body}
}

// consumePendingLabels ends the current label chain. When iteration is true (an
// IterationStatement is claiming the chain), each pending label is marked as an
// iteration label in the enclosing label set, making it a legal `continue`
// target throughout the loop body. The pending list is cleared either way.
func (p *parser) consumePendingLabels(iteration bool) {
	if iteration {
		for _, name := range p.pendingLabels {
			for i := range p.labelSet {
				if p.labelSet[i].name == name {
					p.labelSet[i].iteration = true
				}
			}
		}
	}
	p.pendingLabels = nil
}

// parseExport parses an ES-module export declaration. The cursor is on the
// `export` keyword. It supports the forms the module loader needs: exported
// declarations (var/let/const/function/class), default exports, and named
// export clauses (`export { a, b as c }`). Re-exports (`export … from …`) and
// `export *` are recognized but not resolved.
func (p *parser) parseExport() ast.Stmt {
	kw := p.next() // export
	es := &ast.ExportStmt{Keyword: kw.Pos, EndPos: p.cur().Pos}

	switch {
	case p.at(token.DEFAULT):
		p.next()
		es.Default = true
		switch {
		case p.at(token.FUNCTION):
			es.Decl = p.parseFunctionDecl(false)
		case p.at(token.ASYNC) && p.peek(1).Type == token.FUNCTION && !p.peek(1).NewlineBefore:
			p.next() // async
			es.Decl = p.parseFunctionDecl(true)
		case p.at(token.CLASS):
			es.Decl = p.parseClassDecl()
		default:
			es.DefaultExpr = p.parseAssignExpr()
			p.expectSemicolon()
		}
	case p.at(token.LBRACE):
		p.next()
		for !p.at(token.RBRACE) && !p.at(token.EOF) {
			local := p.parseModuleExportName()
			exported := local
			if p.at(token.IDENT) && p.cur().Literal == "as" && !p.cur().Escaped {
				p.next() // as
				exported = p.parseModuleExportName()
			}
			es.Specifiers = append(es.Specifiers, &ast.ExportSpecifier{Local: local, Exported: exported})
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(token.RBRACE)
		// An optional `from ModuleSpecifier` re-export clause is consumed but not
		// resolved by the loader.
		if p.at(token.IDENT) && p.cur().Literal == "from" {
			p.next()
			p.accept(token.STRING)
		}
		p.expectSemicolon()
	case p.at(token.STAR):
		// export * [as name] from '…' — consumed but not resolved.
		p.next()
		if p.at(token.IDENT) && p.cur().Literal == "as" {
			p.next()
			p.parseModuleExportName()
		}
		if p.at(token.IDENT) && p.cur().Literal == "from" {
			p.next()
			p.accept(token.STRING)
		}
		p.expectSemicolon()
	case p.at(token.VAR), p.at(token.CONST), p.at(token.LET):
		es.Decl = p.parseVarDecl()
	case p.at(token.FUNCTION):
		es.Decl = p.parseFunctionDecl(false)
	case p.at(token.ASYNC) && p.peek(1).Type == token.FUNCTION && !p.peek(1).NewlineBefore:
		p.next() // async
		es.Decl = p.parseFunctionDecl(true)
	case p.at(token.CLASS):
		es.Decl = p.parseClassDecl()
	default:
		p.errorf("unexpected token %s after export", p.cur().Type)
		p.next()
	}
	if es.Decl != nil {
		es.EndPos = es.Decl.End()
	}
	return es
}

// parseModuleExportName parses an import/export binding name: an identifier or
// any identifier-like keyword.
func (p *parser) parseModuleExportName() string {
	tk := p.cur()
	if tk.Type == token.IDENT || tk.Type.IsKeyword() {
		p.next()
		return identText(tk)
	}
	p.errorf("expected export name but got %s", tk.Type)
	p.next()
	return ""
}

// letIsDeclaration reports whether a `let` at the cursor begins a declaration
// (as opposed to being used as an identifier).
func (p *parser) letIsDeclaration() bool {
	switch p.peek(1).Type {
	case token.IDENT, token.LBRACKET, token.LBRACE:
		return true
	default:
		// `let` followed by a contextual keyword name (e.g. `let of`) is a decl;
		// but `let` followed by a reserved word (e.g. `let in`) is the identifier
		// `let` used in an expression, not a declaration.
		return isContextualKeyword(p.peek(1).Type)
	}
}

// parseBraceBody parses a brace-delimited statement list into a block node
// without applying any block-level early-error checks.
func (p *parser) parseBraceBody() *ast.BlockStmt {
	lb := p.expect(token.LBRACE)
	blk := &ast.BlockStmt{Lbrace: lb.Pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) && p.err == nil {
		blk.Body = append(blk.Body, p.parseStmt())
	}
	rb := p.expect(token.RBRACE)
	blk.Rbrace = rb.Pos
	return blk
}

// parseBlock parses a brace-delimited block statement and enforces the Block
// static-semantics early errors (lexical redeclaration rules). It is used for
// genuine Block productions — block statements and try/catch/finally blocks —
// not function bodies (see parseFunctionBody).
func (p *parser) parseBlock() *ast.BlockStmt {
	blk := p.parseBraceBody()
	p.checkBlockEarlyErrors(blk.Body)
	return blk
}

// parseFunctionBody parses the brace-delimited body of a function, arrow, or
// method. Unlike a block statement, a function body uses top-level semantics
// (FunctionDeclarations are var-scoped, not lexical), so the Block early-error
// check is not applied; instead this is where a strict-mode directive prologue
// takes effect for the code within. It returns the body and whether that body
// runs in strict mode (its own "use strict" prologue or strict inherited from
// enclosing code), so callers can record it on the function definition for the
// runtime (e.g. strict-mode `this` binding).
func (p *parser) parseFunctionBody() (*ast.BlockStmt, bool) {
	prevStrict := p.strict
	// A function body is outside any formal parameter list, so yield/await
	// suspension checks for the parameter context do not reach in.
	prevParams := p.inParams
	p.inParams = false
	// A function body is a fresh break/continue/return boundary: an enclosing
	// loop, switch, or label does not reach across it (ECMA-262 forbids
	// break/continue/return from crossing a function boundary). Save and reset the
	// tracking state, and mark that returns are now permitted.
	prevLoop, prevSwitch := p.inLoop, p.inSwitch
	prevLabels, prevPending := p.labelSet, p.pendingLabels
	p.inLoop, p.inSwitch = 0, 0
	p.labelSet, p.pendingLabels = nil, nil
	p.inFuncBody++
	if p.at(token.LBRACE) && p.scanUseStrict(p.idx+1) {
		p.strict = true
	}
	strict := p.strict
	blk := p.parseBraceBody()
	p.strict = prevStrict
	p.inParams = prevParams
	p.inFuncBody--
	p.inLoop, p.inSwitch = prevLoop, prevSwitch
	p.labelSet, p.pendingLabels = prevLabels, prevPending
	return blk, strict
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
	cons := p.parseSubStatement(true)
	stmt := &ast.IfStmt{Keyword: kw.Pos, Test: test, Consequent: cons}
	if p.accept(token.ELSE) {
		stmt.Alternate = p.parseSubStatement(true)
	}
	return stmt
}

// parseFor parses for, for-in, and for-of statements.
func (p *parser) parseFor() ast.Stmt {
	p.consumePendingLabels(true)
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
			p.checkForInLeft(vd)
			body := p.parseLoopBody()
			p.checkForBodyVarConflict(vd, body)
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
			p.checkForInLeft(expr)
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
	body := p.parseSubStatement(false)
	p.inLoop--
	return body
}

// parseWhile parses a while loop.
func (p *parser) parseWhile() ast.Stmt {
	p.consumePendingLabels(true)
	kw := p.next()
	p.expect(token.LPAREN)
	test := p.parseExpression()
	p.expect(token.RPAREN)
	body := p.parseLoopBody()
	return &ast.WhileStmt{Keyword: kw.Pos, Test: test, Body: body}
}

// parseWith parses `with (Object) Statement`. It is a SyntaxError in strict mode
// (§13.11.1).
func (p *parser) parseWith() ast.Stmt {
	kw := p.next()
	if p.strict {
		p.errorAt(kw.Pos, "'with' statements are not allowed in strict mode")
	}
	p.expect(token.LPAREN)
	obj := p.parseExpression()
	p.expect(token.RPAREN)
	body := p.parseSubStatement(true)
	return &ast.WithStmt{Keyword: kw.Pos, Object: obj, Body: body}
}

// parseDoWhile parses a do/while loop.
func (p *parser) parseDoWhile() ast.Stmt {
	p.consumePendingLabels(true)
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
	sawDefault := false
	for !p.at(token.RBRACE) && !p.at(token.EOF) && p.err == nil {
		c := &ast.SwitchCase{CasePos: p.cur().Pos}
		switch {
		case p.accept(token.CASE):
			c.Test = p.parseExpression()
		case p.accept(token.DEFAULT):
			// A CaseBlock may contain at most one DefaultClause (early error).
			if sawDefault {
				p.errorAt(c.CasePos, "more than one switch default")
				p.inSwitch--
				return stmt
			}
			sawDefault = true
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
	// The CaseBlock as a whole is one lexical scope. checkSwitchEarlyErrors
	// enforces the cross-clause lexical redeclaration rules (a superset of the
	// generic Block check) plus switch-specific early errors (duplicate default).
	if p.err == nil {
		p.checkSwitchEarlyErrors(stmt)
	}
	return stmt
}

// checkSwitchEarlyErrors enforces the Static Semantics: Early Errors for a
// SwitchStatement. The entire CaseBlock is a single lexical scope, so:
//   - the LexicallyDeclaredNames of the CaseBlock must not contain duplicates
//     (a let/const/class/function-declaration name repeated across ANY two
//     clauses, including unreachable ones), and
//   - no lexically-declared name may also be a VarDeclaredName of the CaseBlock.
//
// These are parse-time SyntaxErrors, checked regardless of which case matches.
func (p *parser) checkSwitchEarlyErrors(stmt *ast.SwitchStmt) {
	// Collect the CaseBlock's top-level lexical names across all clauses.
	lex := map[string]token.Pos{}
	dup := false
	var dupPos token.Pos
	for _, c := range stmt.Cases {
		caseLexNames(c.Body, func(name string, pos token.Pos) {
			if _, ok := lex[name]; ok {
				if !dup {
					dup, dupPos = true, pos
				}
			} else {
				lex[name] = pos
			}
		})
	}
	if dup {
		p.errorAt(dupPos, "Identifier has already been declared")
		return
	}
	// A lexical name may not collide with a var-declared name in the same
	// CaseBlock (var hoists through nested blocks, so recurse).
	varNames := map[string]bool{}
	for _, c := range stmt.Cases {
		collectSwitchVarNames(c.Body, varNames)
	}
	for name, pos := range lex {
		if varNames[name] {
			p.errorAt(pos, "Identifier has already been declared")
			return
		}
	}
}

// caseLexNames reports the top-level LexicallyDeclaredNames of a case clause's
// statement list: let/const bindings, class declarations, and block-level
// function declarations declared directly in the clause (not nested blocks).
func caseLexNames(body []ast.Stmt, fn func(name string, pos token.Pos)) {
	for _, s := range body {
		switch st := s.(type) {
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					eachBoundName(d.Target, func(n string) { fn(n, d.Target.Pos()) })
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				fn(st.Def.Name.Name, st.Def.Name.NamePos)
			}
		case *ast.FuncDecl:
			if st.Def.Name != nil {
				fn(st.Def.Name.Name, st.Def.Name.NamePos)
			}
		}
	}
}

// collectSwitchVarNames accumulates the VarDeclaredNames of a statement list:
// names bound by `var`, recursing through nested statements (but not into
// nested functions, which have their own scope).
func collectSwitchVarNames(body []ast.Stmt, into map[string]bool) {
	for _, s := range body {
		collectSwitchVarNamesStmt(s, into)
	}
}

func collectSwitchVarNamesStmt(s ast.Stmt, into map[string]bool) {
	switch st := s.(type) {
	case *ast.VarDecl:
		if st.Kind == token.VAR {
			for _, d := range st.Decls {
				eachBoundName(d.Target, func(n string) { into[n] = true })
			}
		}
	case *ast.BlockStmt:
		collectSwitchVarNames(st.Body, into)
	case *ast.IfStmt:
		collectSwitchVarNamesStmt(st.Consequent, into)
		if st.Alternate != nil {
			collectSwitchVarNamesStmt(st.Alternate, into)
		}
	case *ast.ForStmt:
		if vd, ok := st.Init.(*ast.VarDecl); ok {
			collectSwitchVarNamesStmt(vd, into)
		}
		collectSwitchVarNamesStmt(st.Body, into)
	case *ast.ForInStmt:
		if vd, ok := st.Left.(*ast.VarDecl); ok {
			collectSwitchVarNamesStmt(vd, into)
		}
		collectSwitchVarNamesStmt(st.Body, into)
	case *ast.WhileStmt:
		collectSwitchVarNamesStmt(st.Body, into)
	case *ast.DoWhileStmt:
		collectSwitchVarNamesStmt(st.Body, into)
	case *ast.TryStmt:
		collectSwitchVarNames(st.Block.Body, into)
		if st.Handler != nil {
			collectSwitchVarNames(st.Handler.Body.Body, into)
		}
		if st.Finalizer != nil {
			collectSwitchVarNames(st.Finalizer.Body, into)
		}
	case *ast.SwitchStmt:
		for _, c := range st.Cases {
			collectSwitchVarNames(c.Body, into)
		}
	case *ast.LabeledStmt:
		collectSwitchVarNamesStmt(st.Body, into)
	}
}

// eachBoundName invokes fn for each identifier bound by a binding target
// (identifier or destructuring pattern).
func eachBoundName(target ast.Expr, fn func(string)) {
	switch t := target.(type) {
	case *ast.Ident:
		fn(t.Name)
	case *ast.AssignPattern:
		eachBoundName(t.Target, fn)
	case *ast.RestElement:
		eachBoundName(t.Target, fn)
	case *ast.ArrayLit:
		for _, el := range t.Elements {
			if el != nil {
				eachBoundName(el, fn)
			}
		}
	case *ast.ObjectLit:
		for _, pr := range t.Properties {
			if pr.Value != nil {
				eachBoundName(pr.Value, fn)
			} else if pr.Key != nil {
				eachBoundName(pr.Key, fn)
			}
		}
	case *ast.SpreadElement:
		eachBoundName(t.Argument, fn)
	}
}

// parseReturn parses a return statement, honoring ASI for the optional argument.
func (p *parser) parseReturn() ast.Stmt {
	kw := p.next()
	// A ReturnStatement is only permitted inside a function body (the [+Return]
	// grammar parameter). It is an early SyntaxError in global code, eval code,
	// and — separately — a class static initialization block, none of which is a
	// function body. Nested functions inside such contexts re-enter a function
	// body, so their returns remain legal.
	if p.inStaticBlock {
		p.errorAt(kw.Pos, "'return' is not allowed in a class static initialization block")
	} else if p.inFuncBody == 0 {
		p.errorAt(kw.Pos, "'return' outside of function")
	}
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
	labelName := ""
	// A label must appear on the same line (no ASI newline before it).
	if p.at(token.IDENT) && !p.cur().NewlineBefore {
		id := p.next()
		p.checkEscapedReserved(id)
		labelName = id.Literal
		label = &ast.Ident{NamePos: id.Pos, Name: labelName}
		end = id.End
	}
	// Early errors (ECMA-262 §13.9 / §13.8): break/continue must resolve to an
	// enclosing target within the same function or static-block boundary. An
	// unlabelled continue requires an enclosing IterationStatement; an unlabelled
	// break requires an enclosing IterationStatement or SwitchStatement. A
	// labelled break requires an enclosing label; a labelled continue requires an
	// enclosing label that directly labels an IterationStatement.
	switch {
	case labelName == "" && !isBreak:
		if p.inLoop == 0 {
			p.errorAt(kw.Pos, "Illegal continue statement: no surrounding iteration statement")
		}
	case labelName == "" && isBreak:
		if p.inLoop == 0 && p.inSwitch == 0 {
			p.errorAt(kw.Pos, "Illegal break statement")
		}
	case isBreak:
		if !p.hasBreakLabel(labelName) {
			p.errorAt(kw.Pos, "Undefined label '%s'", labelName)
		}
	default: // labelled continue
		if !p.hasContinueLabel(labelName) {
			p.errorAt(kw.Pos, "Undefined label '%s'", labelName)
		}
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
