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

// checkTopLevelEarlyErrors enforces the LexicallyDeclaredNames static-semantics
// early errors for a StatementList that uses top-level semantics: a Script's
// top level (ECMA-262 §16.1.1) or a function/arrow/method body (§15.2.1 and the
// FunctionBody productions). Unlike a Block, at this level a
// FunctionDeclaration contributes a var-declared name rather than a lexical one,
// so the Annex B relaxation for duplicate FunctionDeclarations does not apply
// here. The two early errors are: the LexicallyDeclaredNames must contain no
// duplicates, and must not intersect the VarDeclaredNames.
func (p *parser) checkTopLevelEarlyErrors(body []ast.Stmt) {
	if p.err != nil {
		return
	}

	// TopLevelLexicallyDeclaredNames: names bound by a `let`/`const` or a class
	// declaration appearing directly in this StatementList. A top-level
	// FunctionDeclaration is deliberately excluded (it is var-scoped).
	type lexInfo struct {
		pos   token.Pos
		count int
	}
	lex := map[string]*lexInfo{}
	addLex := func(name string, pos token.Pos) {
		li := lex[name]
		if li == nil {
			li = &lexInfo{pos: pos}
			lex[name] = li
		}
		li.count++
	}
	for _, s := range body {
		switch st := s.(type) {
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					forEachBindingName(d.Target, func(n string, pos token.Pos) {
						addLex(n, pos)
					})
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				addLex(st.Def.Name.Name, st.Def.Name.NamePos)
			}
		}
	}

	// Duplicate lexically declared names are always an error at this level.
	for name, li := range lex {
		if li.count > 1 {
			p.errorAt(li.pos, "Identifier '%s' has already been declared", name)
			return
		}
	}

	// A lexically declared name may not also be a VarDeclaredName. The var names
	// comprise the hoisted `var` declarations of the whole StatementList plus the
	// names of the (var-scoped) top-level FunctionDeclarations.
	varNames := map[string]bool{}
	collectBlockVarNames(body, varNames)
	collectTopLevelFuncNames(body, varNames)
	for name, li := range lex {
		if varNames[name] {
			p.errorAt(li.pos, "Identifier '%s' has already been declared", name)
			return
		}
	}
}

// collectTopLevelFuncNames records the names of the FunctionDeclarations that
// appear directly in a top-level StatementList — including through a chain of
// labels (`l: function f(){}`), whose TopLevelVarDeclaredNames also contribute a
// var name — but not those nested inside blocks or other statements, which have
// their own (block-level) scope.
func collectTopLevelFuncNames(body []ast.Stmt, into map[string]bool) {
	for _, s := range body {
		st := s
		for {
			if lbl, ok := st.(*ast.LabeledStmt); ok {
				st = lbl.Body
				continue
			}
			break
		}
		if fd, ok := st.(*ast.FuncDecl); ok && fd.Def.Name != nil {
			into[fd.Def.Name.Name] = true
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
	// `let` in a single-statement position is handled by parseSubStatement (it is
	// the identifier `let` unless the restricted `let [` lookahead applies), so it
	// is intentionally not treated as a lexical declaration here.
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
// position, rejecting declarations the grammar forbids there. annexBFunc is
// true for the if/else and labelled-statement bodies where Annex B (B.3.2/B.3.4)
// relaxes a plain FunctionDeclaration in sloppy code, and false for iteration
// bodies where no such relaxation applies.
func (p *parser) parseSubStatement(annexBFunc bool) ast.Stmt {
	// A leading `let` in a Statement-only position is the identifier `let` used
	// as an ExpressionStatement, never a LexicalDeclaration. The one exception is
	// the lookahead-restricted `let [` form, which is an early error here.
	if p.at(token.LET) {
		if p.peek(1).Type == token.LBRACKET {
			p.errorAt(p.cur().Pos, "Lexical declaration cannot appear in a single-statement context")
		}
		// A concrete statement ends the enclosing label chain (it is not a loop).
		p.pendingLabels = nil
		return p.parseExprStmt()
	}
	p.checkStatementPosition(annexBFunc)
	stmt := p.parseStmt()
	// In an iteration-statement body a LabelledFunctionDeclaration is a Syntax
	// Error (ECMA-262 B.3.2); annexBFunc is false only for those loop bodies.
	if !annexBFunc && isLabeledFunction(stmt) {
		p.errorAt(stmt.Pos(), "Labelled function declaration cannot appear in a single-statement context")
	}
	return stmt
}

// isLabeledFunction reports whether stmt is a (possibly multiply) labelled
// FunctionDeclaration, e.g. `l1: l2: function f(){}`.
func isLabeledFunction(stmt ast.Stmt) bool {
	inner := stmt
	for {
		lbl, ok := inner.(*ast.LabeledStmt)
		if !ok {
			return false
		}
		if _, ok := lbl.Body.(*ast.FuncDecl); ok {
			return true
		}
		inner = lbl.Body
	}
}

// checkCatchParam enforces the CatchParameter static-semantics early errors
// (ECMA-262 §14.15.1):
//
//   - BoundNames of CatchParameter must contain no duplicate elements (a
//     destructuring pattern such as `catch ([x, x])`).
//   - No element of BoundNames of CatchParameter may also occur in the
//     LexicallyDeclaredNames of the catch Block (`catch (x) { let x; }`, or a
//     directly-nested FunctionDeclaration `catch (e) { function e(){} }`, which
//     is a lexically declared name of the block).
//
// Both hold in strict and sloppy code. The separate restriction against
// intersecting the block's VarDeclaredNames (with the Annex B §B.3.5 relaxation
// for a single-identifier CatchParameter) is not enforced here.
func (p *parser) checkCatchParam(cc *ast.CatchClause) {
	if p.err != nil || cc == nil || cc.Param == nil {
		return
	}
	seen := map[string]bool{}
	var dupName string
	var dupPos token.Pos
	dup := false
	p.collectBindingNames(cc.Param, func(name string, pos token.Pos) {
		if seen[name] && !dup {
			dup, dupName, dupPos = true, name, pos
		}
		seen[name] = true
	})
	if dup {
		p.earlyError(dupPos, "Duplicate binding '"+dupName+"' in catch parameter")
		return
	}
	if cc.Body == nil {
		return
	}
	// LexicallyDeclaredNames of the catch Block: let/const bindings, class
	// declarations, and (unlike a function body's top level) block-level
	// FunctionDeclarations, which are lexically scoped within a Block.
	lex := map[string]token.Pos{}
	addLex := func(name string, pos token.Pos) {
		if _, ok := lex[name]; !ok {
			lex[name] = pos
		}
	}
	for _, s := range cc.Body.Body {
		switch st := s.(type) {
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					forEachBindingName(d.Target, addLex)
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				addLex(st.Def.Name.Name, st.Def.Name.NamePos)
			}
		case *ast.FuncDecl:
			if st.Def.Name != nil {
				addLex(st.Def.Name.Name, st.Def.Name.NamePos)
			}
		}
	}
	for name := range seen {
		if pos, ok := lex[name]; ok {
			p.earlyError(pos, "Identifier '"+name+"' has already been declared")
			return
		}
	}
}

// earlyError records a SyntaxError at pos (only the first is kept).
func (p *parser) earlyError(pos token.Pos, msg string) {
	if p.err == nil {
		p.errorAt(pos, "%s", msg)
	}
}

// checkForInLeft validates the left-hand side of a for-in / for-of statement.
// The LHS is either a ForDeclaration (var/let/const binding) or an
// assignment-target expression (possibly a destructuring assignment pattern).
func (p *parser) checkForInLeft(left ast.Node) {
	switch l := left.(type) {
	case *ast.VarDecl:
		p.checkForDeclaration(l)
		// Inside a generator, `yield` in the head is a valid YieldExpression, so
		// the reserved-word check applies only in strict non-generator code.
		if p.strict && !p.inGenerator && len(l.Decls) > 0 {
			p.checkNoYieldInStrict(l.Decls[0].Target)
		}
	case ast.Expr:
		p.checkAssignmentTarget(l)
		if p.strict && !p.inGenerator {
			p.checkNoYieldInStrict(l)
		}
	}
}

// checkNoYieldInStrict reports a SyntaxError if a for-in/of head pattern uses a
// YieldExpression in strict mode (where `yield` is a reserved word and may not
// serve as an IdentifierReference outside a generator). The walk descends
// through the pattern's structure — computed keys, defaults, and nested
// targets — but not into nested function bodies, which have their own scope.
func (p *parser) checkNoYieldInStrict(expr ast.Expr) {
	if expr == nil || p.err != nil {
		return
	}
	switch e := expr.(type) {
	case *ast.YieldExpr:
		p.earlyError(e.Keyword, "'yield' expression is not allowed in strict mode")
	case *ast.Ident:
		if e.Name == "yield" {
			p.earlyError(e.NamePos, "'yield' may not be used as an identifier in strict mode")
		}
	case *ast.MemberExpr:
		p.checkNoYieldInStrict(e.Object)
		if e.Computed {
			p.checkNoYieldInStrict(e.Property)
		}
	case *ast.CallExpr:
		p.checkNoYieldInStrict(e.Callee)
		for _, a := range e.Arguments {
			p.checkNoYieldInStrict(a)
		}
	case *ast.ArrayLit:
		for _, el := range e.Elements {
			p.checkNoYieldInStrict(el)
		}
	case *ast.ObjectLit:
		for _, prop := range e.Properties {
			if prop.Computed {
				p.checkNoYieldInStrict(prop.Key)
			}
			p.checkNoYieldInStrict(prop.Value)
		}
	case *ast.SpreadElement:
		p.checkNoYieldInStrict(e.Argument)
	case *ast.RestElement:
		p.checkNoYieldInStrict(e.Target)
	case *ast.AssignExpr:
		p.checkNoYieldInStrict(e.Target)
		p.checkNoYieldInStrict(e.Value)
	case *ast.AssignPattern:
		p.checkNoYieldInStrict(e.Target)
		p.checkNoYieldInStrict(e.Default)
	}
}

// checkForDeclaration validates the bound names and binding pattern of a
// for-in/of ForDeclaration head (for (let/const/var <target> in ...)).
func (p *parser) checkForDeclaration(vd *ast.VarDecl) {
	if len(vd.Decls) == 0 {
		return
	}
	target := vd.Decls[0].Target
	lexical := vd.Kind == token.LET || vd.Kind == token.CONST
	seen := map[string]bool{}
	var walk func(name string, pos token.Pos)
	walk = func(name string, pos token.Pos) {
		if lexical && name == "let" {
			p.earlyError(pos, "'let' is not a valid binding name in a lexical declaration")
			return
		}
		if p.strict && (name == "eval" || name == "arguments") {
			p.earlyError(pos, "Binding '"+name+"' in strict mode")
			return
		}
		if lexical && seen[name] {
			p.earlyError(pos, "Duplicate binding '"+name+"' in destructuring declaration")
			return
		}
		seen[name] = true
	}
	p.collectBindingNames(target, walk)
	// The binding pattern itself must be structurally valid (rest last, etc.).
	p.checkBindingPattern(target)
}

// checkForBodyVarConflict reports a SyntaxError when a lexical ForDeclaration
// (for (let/const <target> in ...)) binds a name that also appears among the
// VarDeclaredNames of the loop body (ECMA-262 13.7.5.1).
func (p *parser) checkForBodyVarConflict(head *ast.VarDecl, body ast.Stmt) {
	if head == nil || (head.Kind != token.LET && head.Kind != token.CONST) || len(head.Decls) == 0 {
		return
	}
	bound := map[string]bool{}
	p.collectBindingNames(head.Decls[0].Target, func(name string, _ token.Pos) { bound[name] = true })
	if len(bound) == 0 {
		return
	}
	varNames := map[string]token.Pos{}
	collectBodyVarNames(body, varNames)
	for name, pos := range varNames {
		if bound[name] {
			p.earlyError(pos, "Identifier '"+name+"' has already been declared")
			return
		}
	}
}

// collectBodyVarNames gathers the var-declared names within a statement, without
// descending into nested function or class bodies.
func collectBodyVarNames(s ast.Stmt, into map[string]token.Pos) {
	switch st := s.(type) {
	case *ast.VarDecl:
		if st.Kind == token.VAR {
			for _, d := range st.Decls {
				collectPatternNamesPos(d.Target, into)
			}
		}
	case *ast.BlockStmt:
		for _, b := range st.Body {
			collectBodyVarNames(b, into)
		}
	case *ast.IfStmt:
		collectBodyVarNames(st.Consequent, into)
		if st.Alternate != nil {
			collectBodyVarNames(st.Alternate, into)
		}
	case *ast.ForStmt:
		if vd, ok := st.Init.(*ast.VarDecl); ok {
			collectBodyVarNames(vd, into)
		}
		collectBodyVarNames(st.Body, into)
	case *ast.ForInStmt:
		if vd, ok := st.Left.(*ast.VarDecl); ok {
			collectBodyVarNames(vd, into)
		}
		collectBodyVarNames(st.Body, into)
	case *ast.WhileStmt:
		collectBodyVarNames(st.Body, into)
	case *ast.DoWhileStmt:
		collectBodyVarNames(st.Body, into)
	case *ast.TryStmt:
		for _, b := range st.Block.Body {
			collectBodyVarNames(b, into)
		}
		if st.Handler != nil {
			for _, b := range st.Handler.Body.Body {
				collectBodyVarNames(b, into)
			}
		}
		if st.Finalizer != nil {
			for _, b := range st.Finalizer.Body {
				collectBodyVarNames(b, into)
			}
		}
	case *ast.SwitchStmt:
		for _, c := range st.Cases {
			for _, b := range c.Body {
				collectBodyVarNames(b, into)
			}
		}
	case *ast.LabeledStmt:
		collectBodyVarNames(st.Body, into)
	}
}

// collectPatternNamesPos records each bound name of a binding target with its
// position.
func collectPatternNamesPos(target ast.Expr, into map[string]token.Pos) {
	switch t := target.(type) {
	case *ast.Ident:
		if _, ok := into[t.Name]; !ok {
			into[t.Name] = t.NamePos
		}
	case *ast.AssignPattern:
		collectPatternNamesPos(t.Target, into)
	case *ast.AssignExpr:
		if t.Op == token.ASSIGN {
			collectPatternNamesPos(t.Target, into)
		}
	case *ast.RestElement:
		collectPatternNamesPos(t.Target, into)
	case *ast.SpreadElement:
		collectPatternNamesPos(t.Argument, into)
	case *ast.ArrayLit:
		for _, el := range t.Elements {
			if el != nil {
				collectPatternNamesPos(el, into)
			}
		}
	case *ast.ObjectLit:
		for _, prop := range t.Properties {
			if prop.Value != nil {
				collectPatternNamesPos(prop.Value, into)
			}
		}
	}
}

// collectBindingNames invokes emit for each BoundName in a binding target
// (Ident or destructuring pattern).
func (p *parser) collectBindingNames(target ast.Expr, emit func(name string, pos token.Pos)) {
	switch t := target.(type) {
	case *ast.Ident:
		emit(t.Name, t.NamePos)
	case *ast.AssignPattern:
		p.collectBindingNames(t.Target, emit)
	case *ast.RestElement:
		p.collectBindingNames(t.Target, emit)
	case *ast.SpreadElement:
		p.collectBindingNames(t.Argument, emit)
	case *ast.ArrayLit:
		for _, el := range t.Elements {
			if el == nil {
				continue
			}
			p.collectBindingNames(el, emit)
		}
	case *ast.ObjectLit:
		for _, prop := range t.Properties {
			if prop.Kind == ast.PropSpread {
				p.collectBindingNames(prop.Value, emit)
				continue
			}
			if prop.Value != nil {
				p.collectBindingNames(prop.Value, emit)
			}
		}
	case *ast.AssignExpr:
		if t.Op == token.ASSIGN {
			p.collectBindingNames(t.Target, emit)
		}
	}
}

// checkBindingPattern validates the structure of a binding pattern (used in a
// var/let/const ForDeclaration): a rest element must be the final element and
// may not carry a default initializer.
func (p *parser) checkBindingPattern(target ast.Expr) {
	switch t := target.(type) {
	case *ast.ArrayLit:
		for idx, el := range t.Elements {
			if el == nil {
				continue
			}
			if isRest(el) {
				if idx != len(t.Elements)-1 {
					p.earlyError(el.Pos(), "Rest element must be last element")
				}
				if hasDefault(el) {
					p.earlyError(el.Pos(), "Rest element may not have a default")
				}
				continue
			}
			p.checkBindingPattern(stripDefault(el))
		}
	case *ast.ObjectLit:
		// Refining this object into a binding pattern clears its deferred
		// ObjectLiteral early errors (CoverInitializedName, duplicate __proto__).
		p.clearDeferredObjErrs(t.Lbrace)
		for idx, prop := range t.Properties {
			if prop.Kind == ast.PropSpread {
				if idx != len(t.Properties)-1 {
					p.earlyError(prop.Pos(), "Rest element must be last element")
				}
				continue
			}
			if prop.Method || prop.Kind == ast.PropGet || prop.Kind == ast.PropSet {
				p.earlyError(prop.Pos(), "Invalid destructuring binding target")
				continue
			}
			if prop.Value != nil {
				p.checkBindingPattern(stripDefault(prop.Value))
			}
		}
	}
}

// validateAssignExprTarget enforces the AssignmentExpression static-semantics
// early errors on the left-hand side of an assignment. For a simple assignment
// (=) the LHS must be a simple assignment target (IdentifierReference or
// MemberExpression) or a refinable destructuring AssignmentPattern (array or
// object literal). For a compound assignment (+=, **=, &&=, …) destructuring
// patterns are not permitted, so the LHS must be a simple assignment target.
func (p *parser) validateAssignExprTarget(target ast.Expr, op token.Type) {
	if p.err != nil {
		return
	}
	if op == token.ASSIGN {
		p.checkAssignmentTarget(target)
		return
	}
	p.checkSimpleAssignmentTarget(target)
}

// checkSimpleAssignmentTarget reports an early error unless expr is a simple
// assignment target: an IdentifierReference or a (non-optional) MemberExpression.
// It is used for compound assignment operators and prefix/postfix update
// expressions (++/--), where an array/object destructuring pattern is not a
// legal target.
func (p *parser) checkSimpleAssignmentTarget(expr ast.Expr) {
	if p.err != nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if p.strict && (e.Name == "eval" || e.Name == "arguments") {
			p.earlyError(e.NamePos, "Assignment to '"+e.Name+"' in strict mode")
		}
	case *ast.MemberExpr:
		if isImportMeta(e) {
			p.earlyError(e.Pos(), "import.meta is not a valid assignment target")
		} else if containsOptional(e) {
			p.earlyError(e.Pos(), "Optional chain may not be an assignment target")
		}
	default:
		p.earlyError(expr.Pos(), "Invalid left-hand side in assignment")
	}
}

// checkAssignmentTarget validates an expression used as a for-in/of assignment
// target: it must be a simple assignment target (Ident, non-optional member
// access) or a valid destructuring AssignmentPattern (array/object literal).
func (p *parser) checkAssignmentTarget(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.Ident:
		if p.strict && (e.Name == "eval" || e.Name == "arguments") {
			p.earlyError(e.NamePos, "Assignment to '"+e.Name+"' in strict mode")
		}
	case *ast.MemberExpr:
		if isImportMeta(e) {
			p.earlyError(e.Pos(), "import.meta is not a valid assignment target")
		} else if containsOptional(e) {
			p.earlyError(e.Pos(), "Optional chain may not be an assignment target")
		}
	case *ast.ArrayLit:
		// A parenthesized array literal is a ParenthesizedExpression, not a
		// refinable AssignmentPattern: `([]) = x` is a Syntax Error, whereas the
		// bare `[] = x` refines into a destructuring pattern.
		if p.parenthesized[e] {
			p.earlyError(e.Pos(), "Invalid left-hand side in assignment")
			return
		}
		p.checkArrayAssignmentPattern(e)
	case *ast.ObjectLit:
		if p.parenthesized[e] {
			p.earlyError(e.Pos(), "Invalid left-hand side in assignment")
			return
		}
		p.checkObjectAssignmentPattern(e)
	default:
		p.earlyError(expr.Pos(), "Invalid left-hand side in for-loop")
	}
}

// isImportMeta reports whether e is the `import.meta` meta-property. The parser
// represents it as a MemberExpr whose object is the reserved-word Ident `import`
// (which can never be an ordinary IdentifierReference), so this is unambiguous.
func isImportMeta(e *ast.MemberExpr) bool {
	obj, ok := e.Object.(*ast.Ident)
	return ok && obj.Name == "import"
}

// checkArrayAssignmentPattern validates an array destructuring assignment
// pattern used as a for-in/of target.
func (p *parser) checkArrayAssignmentPattern(arr *ast.ArrayLit) {
	for idx, el := range arr.Elements {
		if el == nil {
			continue
		}
		if isRest(el) {
			if idx != len(arr.Elements)-1 {
				p.earlyError(el.Pos(), "Rest element must be last element")
			} else if arr.TrailingComma {
				// `[...x,]`: an elision may not follow an AssignmentRestElement.
				p.earlyError(el.Pos(), "Rest element may not be followed by a comma")
			}
			if hasDefault(el) {
				p.earlyError(el.Pos(), "Rest element may not have a default initializer")
			}
			// The rest target must itself be a simple/pattern target, never a
			// pattern with a default, call, or (a, b).
			p.checkAssignmentElement(restArg(el), true)
			continue
		}
		p.checkAssignmentElement(el, false)
	}
}

// checkObjectAssignmentPattern validates an object destructuring assignment
// pattern used as a for-in/of target.
func (p *parser) checkObjectAssignmentPattern(obj *ast.ObjectLit) {
	// This object is being refined into a destructuring pattern, so its deferred
	// ObjectLiteral early errors (CoverInitializedName, duplicate __proto__) do
	// not apply.
	p.clearDeferredObjErrs(obj.Lbrace)
	for idx, prop := range obj.Properties {
		if prop.Kind == ast.PropSpread {
			if idx != len(obj.Properties)-1 {
				p.earlyError(prop.Pos(), "Rest element must be last element")
			}
			p.checkAssignmentElement(prop.Value, true)
			continue
		}
		if prop.Method || prop.Kind == ast.PropGet || prop.Kind == ast.PropSet {
			p.earlyError(prop.Pos(), "Invalid destructuring assignment target")
			continue
		}
		if prop.Value != nil {
			p.checkAssignmentElement(prop.Value, false)
		}
	}
}

// checkAssignmentElement validates one element of a destructuring assignment
// pattern. When rest is true the element is a rest target (no default allowed).
func (p *parser) checkAssignmentElement(el ast.Expr, rest bool) {
	// Peel a default initializer (target = default), which is legal for a
	// non-rest element.
	switch e := el.(type) {
	case *ast.AssignExpr:
		if e.Op == token.ASSIGN {
			if rest {
				p.earlyError(e.Pos(), "Rest element may not have a default initializer")
			}
			p.checkAssignmentElement(e.Target, false)
			return
		}
		p.earlyError(el.Pos(), "Invalid destructuring assignment target")
		return
	case *ast.AssignPattern:
		if rest {
			p.earlyError(e.Pos(), "Rest element may not have a default initializer")
		}
		p.checkAssignmentElement(e.Target, false)
		return
	}
	// Otherwise the element must itself be a valid assignment target.
	p.checkAssignmentTarget(el)
}

// --- small structural helpers ----------------------------------------------

func isRest(el ast.Expr) bool {
	switch el.(type) {
	case *ast.SpreadElement, *ast.RestElement:
		return true
	}
	return false
}

func restArg(el ast.Expr) ast.Expr {
	switch e := el.(type) {
	case *ast.SpreadElement:
		return e.Argument
	case *ast.RestElement:
		return e.Target
	}
	return el
}

// hasDefault reports whether a rest element carries a default initializer
// (which is always an early error).
func hasDefault(el ast.Expr) bool {
	arg := restArg(el)
	switch a := arg.(type) {
	case *ast.AssignPattern:
		return true
	case *ast.AssignExpr:
		return a.Op == token.ASSIGN
	}
	return false
}

// stripDefault removes a default initializer wrapper, returning the underlying
// binding target.
func stripDefault(el ast.Expr) ast.Expr {
	switch e := el.(type) {
	case *ast.AssignPattern:
		return e.Target
	case *ast.AssignExpr:
		if e.Op == token.ASSIGN {
			return e.Target
		}
	}
	return el
}

// containsOptional reports whether a member-access chain includes an optional
// (?.) link, which disqualifies it as an assignment target.
// checkNotLogicalOperand reports the §13.13 early error when a `??` operand is
// an unparenthesized `||` or `&&` expression (e.g. `a ?? b || c`, `a ?? b && c`).
func (p *parser) checkNotLogicalOperand(e ast.Expr) {
	if p.parenthesized[e] {
		return
	}
	if le, ok := e.(*ast.LogicalExpr); ok && (le.Op == token.OR || le.Op == token.AND) {
		p.earlyError(le.OpPos, "Cannot mix '??' with '||' or '&&' without parentheses")
	}
}

// checkNotCoalesceOperand reports the §13.13 early error when a `||`/`&&` operand
// is an unparenthesized `??` expression (e.g. `a || b ?? c`, `a && b ?? c`).
func (p *parser) checkNotCoalesceOperand(e ast.Expr) {
	if p.parenthesized[e] {
		return
	}
	if le, ok := e.(*ast.LogicalExpr); ok && le.Op == token.NULLISH {
		p.earlyError(le.OpPos, "Cannot mix '??' with '||' or '&&' without parentheses")
	}
}

// isUnparenthesizedOptionalChain reports whether e is an OptionalExpression whose
// optional link is not severed by parentheses. It walks the member/call spine,
// stopping at any node that was parenthesized (which begins a fresh
// PrimaryExpression, e.g. `(a?.b).c` is a plain — non-optional — MemberExpression).
func (p *parser) isUnparenthesizedOptionalChain(e ast.Expr) bool {
	for {
		if p.parenthesized[e] {
			return false
		}
		switch n := e.(type) {
		case *ast.MemberExpr:
			if n.Optional {
				return true
			}
			e = n.Object
		case *ast.CallExpr:
			if n.Optional {
				return true
			}
			e = n.Callee
		default:
			return false
		}
	}
}

func containsOptional(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if e.Optional {
			return true
		}
		return containsOptional(e.Object)
	case *ast.CallExpr:
		if e.Optional {
			return true
		}
		return containsOptional(e.Callee)
	}
	return false
}
