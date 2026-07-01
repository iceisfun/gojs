package parser

import (
	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements ECMAScript static-semantics "early errors" that must be
// reported at parse time (phase: parse), namely:
//
//   - Block static semantics (ECMA-262 §14.2.1): the LexicallyDeclaredNames of a
//     block's StatementList must contain no duplicates, and must not intersect
//     its VarDeclaredNames. A block-level FunctionDeclaration contributes a
//     lexical name; Annex B (§B.3.2) relaxes the duplicate rule for two plain
//     FunctionDeclarations in non-strict code.
//   - The grammar refinement (ECMA-262 §14.1) that a Declaration may not appear
//     where only a Statement is permitted (the body of if/else, a loop, or a
//     labelled statement), with the Annex B (§B.3.3) exception that a plain
//     FunctionDeclaration may be the body of an if/else clause or a labelled
//     statement in non-strict code.

// scanUseStrict reports whether the run of statements beginning at token index
// from constitutes a directive prologue containing a "use strict" directive. It
// walks a leading sequence of string-literal statements (each optionally
// terminated by a semicolon) without consuming input.
func (p *parser) scanUseStrict(from int) bool {
	i := from
	for i < len(p.toks) {
		t := p.toks[i]
		if t.Type != token.STRING {
			return false
		}
		// A directive is a StringLiteral whose source text (ignoring the quotes)
		// is exactly "use strict". Using the decoded literal is a close-enough
		// approximation for our purposes.
		if t.Literal == "use strict" {
			return true
		}
		i++
		if i < len(p.toks) && p.toks[i].Type == token.SEMICOLON {
			i++
		}
		// The prologue continues only while the next statement is another string
		// literal; anything else ends it.
	}
	return false
}

// checkBlockEarlyErrors enforces the Block static-semantics early errors on a
// StatementList that forms a genuine Block (a block statement, a try/catch/
// finally block, or a switch CaseBlock). It must not be used for a function
// body or the script top level, where FunctionDeclarations are var-scoped.
func (p *parser) checkBlockEarlyErrors(body []ast.Stmt) {
	if p.err != nil {
		return
	}

	// lexInfo tracks, for one lexically declared name, where it was first
	// declared, how many times, and whether every declaration is a plain
	// FunctionDeclaration (relevant to the Annex B duplicate relaxation).
	type lexInfo struct {
		pos      token.Pos
		count    int
		onlyFunc bool
	}
	lex := map[string]*lexInfo{}
	addLex := func(name string, pos token.Pos, plainFunc bool) {
		li := lex[name]
		if li == nil {
			li = &lexInfo{pos: pos, onlyFunc: true}
			lex[name] = li
		}
		li.count++
		if !plainFunc {
			li.onlyFunc = false
		}
	}

	for _, s := range body {
		switch st := s.(type) {
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					forEachBindingName(d.Target, func(n string, pos token.Pos) {
						addLex(n, pos, false)
					})
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				addLex(st.Def.Name.Name, st.Def.Name.NamePos, false)
			}
		case *ast.FuncDecl:
			if st.Def.Name != nil {
				plain := !st.Def.Async && !st.Def.Generator
				addLex(st.Def.Name.Name, st.Def.Name.NamePos, plain)
			}
		}
	}

	// Duplicate lexically declared names are an error, unless (Annex B, in
	// non-strict code) every declaration of that name is a plain
	// FunctionDeclaration.
	for name, li := range lex {
		if li.count > 1 && !(li.onlyFunc && !p.strict) {
			p.errorAt(li.pos, "Identifier '%s' has already been declared", name)
			return
		}
	}

	// A lexically declared name may not also be a VarDeclaredName of the block.
	varNames := map[string]bool{}
	collectBlockVarNames(body, varNames)
	for name, li := range lex {
		if varNames[name] {
			p.errorAt(li.pos, "Identifier '%s' has already been declared", name)
			return
		}
	}
}

// forEachBindingName invokes fn for each identifier bound by a binding target
// (a plain identifier or a destructuring pattern), along with its position.
func forEachBindingName(target ast.Expr, fn func(string, token.Pos)) {
	switch t := target.(type) {
	case *ast.Ident:
		fn(t.Name, t.NamePos)
	case *ast.AssignPattern:
		forEachBindingName(t.Target, fn)
	case *ast.RestElement:
		forEachBindingName(t.Target, fn)
	case *ast.ArrayLit:
		for _, el := range t.Elements {
			if el != nil {
				forEachBindingName(el, fn)
			}
		}
	case *ast.ObjectLit:
		for _, pr := range t.Properties {
			if pr.Value != nil {
				forEachBindingName(pr.Value, fn)
			} else {
				forEachBindingName(pr.Key, fn)
			}
		}
	case *ast.SpreadElement:
		forEachBindingName(t.Argument, fn)
	}
}

// collectBlockVarNames gathers the VarDeclaredNames of a StatementList,
// descending through nested statements (blocks, if, loops, try, switch, labels)
// but stopping at function and class boundaries.
func collectBlockVarNames(stmts []ast.Stmt, into map[string]bool) {
	for _, s := range stmts {
		collectBlockVarNamesStmt(s, into)
	}
}

func collectBlockVarNamesStmt(s ast.Stmt, into map[string]bool) {
	switch st := s.(type) {
	case *ast.VarDecl:
		if st.Kind == token.VAR {
			for _, d := range st.Decls {
				forEachBindingName(d.Target, func(n string, _ token.Pos) { into[n] = true })
			}
		}
	case *ast.BlockStmt:
		collectBlockVarNames(st.Body, into)
	case *ast.IfStmt:
		collectBlockVarNamesStmt(st.Consequent, into)
		if st.Alternate != nil {
			collectBlockVarNamesStmt(st.Alternate, into)
		}
	case *ast.ForStmt:
		if vd, ok := st.Init.(*ast.VarDecl); ok {
			collectBlockVarNamesStmt(vd, into)
		}
		collectBlockVarNamesStmt(st.Body, into)
	case *ast.ForInStmt:
		if vd, ok := st.Left.(*ast.VarDecl); ok {
			collectBlockVarNamesStmt(vd, into)
		}
		collectBlockVarNamesStmt(st.Body, into)
	case *ast.WhileStmt:
		collectBlockVarNamesStmt(st.Body, into)
	case *ast.DoWhileStmt:
		collectBlockVarNamesStmt(st.Body, into)
	case *ast.TryStmt:
		collectBlockVarNames(st.Block.Body, into)
		if st.Handler != nil {
			collectBlockVarNames(st.Handler.Body.Body, into)
		}
		if st.Finalizer != nil {
			collectBlockVarNames(st.Finalizer.Body, into)
		}
	case *ast.SwitchStmt:
		for _, c := range st.Cases {
			collectBlockVarNames(c.Body, into)
		}
	case *ast.LabeledStmt:
		collectBlockVarNamesStmt(st.Body, into)
	}
}

// checkStatementPosition reports an early error when a Declaration appears in a
// position where the grammar permits only a Statement (the body of if/else, a
// loop, or a labelled statement). annexBFunc reports whether a plain
// FunctionDeclaration is permitted here under Annex B (true for if/else clauses
// and labelled statements in non-strict code; false for iteration bodies).
func (p *parser) checkStatementPosition(annexBFunc bool) {
	if p.err != nil {
		return
	}
	tk := p.cur()
	switch tk.Type {
	case token.CONST, token.CLASS:
		p.errorAt(tk.Pos, "Lexical declaration cannot appear in a single-statement context")
	case token.LET:
		if p.letIsDeclaration() {
			p.errorAt(tk.Pos, "Lexical declaration cannot appear in a single-statement context")
		}
	case token.FUNCTION:
		// A generator declaration is never permitted; a plain FunctionDeclaration
		// is permitted only under the Annex B relaxation in non-strict code.
		if p.peek(1).Type == token.STAR || p.strict || !annexBFunc {
			p.errorAt(tk.Pos, "Function declaration cannot appear in a single-statement context")
		}
	case token.ASYNC:
		if p.peek(1).Type == token.FUNCTION && !p.peek(1).NewlineBefore {
			p.errorAt(tk.Pos, "Function declaration cannot appear in a single-statement context")
		}
	}
}

// parseSubStatement parses a statement that appears in a single-statement
// position, rejecting declarations that the grammar forbids there.
func (p *parser) parseSubStatement(annexBFunc bool) ast.Stmt {
	p.checkStatementPosition(annexBFunc)
	return p.parseStmt()
}
