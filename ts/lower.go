package ts

import (
	"fmt"
	"strconv"
	"strings"

	gast "github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/interp"
	"github.com/iceisfun/gojs/token"
	tsast "github.com/iceisfun/typescript/ast"
	"github.com/iceisfun/typescript/core"
	"github.com/iceisfun/typescript/transpiler"
)

// RunStringAST runs a self-contained TypeScript program on vm via the AST path:
// it lowers the source to a typescript-go AST (transpiler.ModuleAST), translates
// that directly into a gojs *ast.Program ([Lower]), and executes it — skipping
// the emit-JavaScript-text-then-reparse round trip that [RunString] performs.
//
// It is for scripts that do not use top-level import/export (see [RunString]).
// A node the lowerer does not yet handle returns an *UnsupportedNodeError; a
// caller that needs total coverage can fall back to [RunString] on that error.
func RunStringAST(vm *interp.Interpreter, name, tsSrc string) (interp.Value, error) {
	sf, err := transpiler.ModuleAST(tsSrc, transpiler.Options{FileName: name, Module: core.ModuleKindCommonJS})
	if err != nil {
		return nil, err
	}
	prog, err := Lower(sf, name, tsSrc)
	if err != nil {
		return nil, err
	}
	return vm.RunProgram(prog)
}

// lower.go translates a post-transform typescript-go AST (types erased, all
// TypeScript-only syntax lowered — see transpiler.ModuleAST) into a gojs
// *ast.Program. The result feeds gojs's existing execution engines unchanged:
// the tree-walker consumes it directly, and the bytecode compiler compiles
// eligible function bodies from it. This is the "TS AST -> gojs AST" frontend
// that skips emitting JavaScript text and re-parsing it.
//
// Only JavaScript syntax is handled; anything TypeScript-only should already be
// gone by the time we see the tree. An unrecognized node aborts the whole lower
// with an *UnsupportedNodeError, which lets a caller fall back to the text path.

// UnsupportedNodeError reports a TypeScript AST node kind that the lowerer does
// not yet translate. A caller can treat it as a signal to fall back to
// transpile-to-text plus the gojs parser.
type UnsupportedNodeError struct {
	Kind string
	Msg  string
}

func (e *UnsupportedNodeError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("gojs/ts: unsupported node %s: %s", e.Kind, e.Msg)
	}
	return fmt.Sprintf("gojs/ts: unsupported node %s", e.Kind)
}

// Lower translates a lowered TypeScript SourceFile (from transpiler.ModuleAST)
// into a gojs *ast.Program. name identifies the module in diagnostics; srcText is
// the original TypeScript source, used to resolve node byte offsets into 1-based
// line/column positions for stack traces (pass "" to skip position mapping).
func Lower(sf *tsast.SourceFile, name, srcText string) (prog *gast.Program, err error) {
	l := &lowerer{name: name, tab: newPosTable(srcText)}
	defer func() {
		if r := recover(); r != nil {
			if ue, ok := r.(*UnsupportedNodeError); ok {
				prog, err = nil, ue
				return
			}
			panic(r)
		}
	}()
	body := l.stmtList(sf.Statements.Nodes)
	return &gast.Program{
		Source: name,
		Body:   body,
		Strict: l.strict,
	}, nil
}

type lowerer struct {
	name string
	tab  *posTable
	// strict tracks lexical strict-mode nesting: once a directive prologue turns
	// strict on (or we enter a class), inner functions inherit it.
	strict bool
}

func (l *lowerer) fail(n *tsast.Node, msg string) {
	kind := "<nil>"
	if n != nil {
		kind = n.Kind.String()
	}
	panic(&UnsupportedNodeError{Kind: kind, Msg: msg})
}

func (l *lowerer) pos(n *tsast.Node) token.Pos {
	if n == nil {
		return token.Pos{Source: l.name}
	}
	off := int(n.Pos())
	line, col := l.tab.lineCol(off)
	return token.Pos{Source: l.name, Offset: off, Line: line, Column: col}
}

// posTable maps a byte offset in the module source to a 1-based line and column.
// Node positions from typescript-go include leading trivia, so lineCol skips
// whitespace and comments first to land on the token itself.
type posTable struct {
	src   string
	lines []int // byte offset of the start of each line
}

func newPosTable(src string) *posTable {
	lines := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lines = append(lines, i+1)
		}
	}
	return &posTable{src: src, lines: lines}
}

func (t *posTable) lineCol(off int) (int, int) {
	if t == nil || off < 0 || off > len(t.src) {
		return 0, 0
	}
	off = t.skipTrivia(off)
	// Largest line-start <= off.
	lo, hi := 0, len(t.lines)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.lines[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1, off - t.lines[lo] + 1
}

func (t *posTable) skipTrivia(off int) int {
	s := t.src
	for off < len(s) {
		switch c := s[off]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f':
			off++
		case c == '/' && off+1 < len(s) && s[off+1] == '/':
			off += 2
			for off < len(s) && s[off] != '\n' {
				off++
			}
		case c == '/' && off+1 < len(s) && s[off+1] == '*':
			off += 2
			for off+1 < len(s) && !(s[off] == '*' && s[off+1] == '/') {
				off++
			}
			if off+1 < len(s) {
				off += 2
			} else {
				off = len(s)
			}
		default:
			return off
		}
	}
	return off
}

// ---------------------------------------------------------------------------
// Statement lists (with directive-prologue detection)
// ---------------------------------------------------------------------------

// stmtList lowers a run of statements, marking a leading directive prologue and
// flipping strict mode on for the remainder of the current scope when it sees a
// "use strict" directive. It restores strict on return, so callers must lower a
// whole function/program scope through one stmtList call.
func (l *lowerer) stmtList(nodes []*tsast.Node) []gast.Stmt {
	out := make([]gast.Stmt, 0, len(nodes))
	inPrologue := true
	savedStrict := l.strict
	// First pass over the prologue only, so nested statements lowered below
	// already observe the correct strict flag.
	for _, n := range nodes {
		if inPrologue {
			if dir, ok := directiveString(n); ok {
				if dir == "use strict" {
					l.strict = true
				}
			} else {
				inPrologue = false
			}
		}
	}
	inPrologue = true
	for _, n := range nodes {
		if skipStatement(n) {
			continue
		}
		s := l.stmt(n)
		if s == nil {
			continue
		}
		if inPrologue {
			if dir, ok := directiveString(n); ok {
				if es, isExpr := s.(*gast.ExprStmt); isExpr {
					es.Directive = dir
				}
			} else {
				inPrologue = false
			}
		}
		out = append(out, s)
	}
	l.strict = savedStrict
	return out
}

// directiveString returns the string value of a statement that is a bare string
// literal expression statement (a directive-prologue entry), and whether it is
// one.
func directiveString(n *tsast.Node) (string, bool) {
	if n.Kind != tsast.KindExpressionStatement {
		return "", false
	}
	x := n.AsExpressionStatement().Expression
	if x != nil && x.Kind == tsast.KindStringLiteral {
		return x.Text(), true
	}
	return "", false
}

// skipStatement reports nodes the printer would drop (not-emitted markers left
// by the transforms, and stray semicolons we needn't preserve).
func skipStatement(n *tsast.Node) bool {
	switch n.Kind {
	case tsast.KindNotEmittedStatement, tsast.KindNotEmittedTypeElement:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

func (l *lowerer) stmt(n *tsast.Node) gast.Stmt {
	switch n.Kind {
	case tsast.KindBlock:
		return l.block(n)
	case tsast.KindEmptyStatement:
		return &gast.EmptyStmt{Semicolon: l.pos(n)}
	case tsast.KindExpressionStatement:
		return &gast.ExprStmt{X: l.expr(n.AsExpressionStatement().Expression)}
	case tsast.KindVariableStatement:
		return l.varDecl(n.AsVariableStatement().DeclarationList)
	case tsast.KindFunctionDeclaration:
		return &gast.FuncDecl{Keyword: l.pos(n), Def: l.funcDef(n, n.Name())}
	case tsast.KindClassDeclaration:
		return &gast.ClassDecl{Keyword: l.pos(n), Def: l.classDef(n)}
	case tsast.KindIfStatement:
		s := n.AsIfStatement()
		return &gast.IfStmt{
			Keyword:    l.pos(n),
			Test:       l.expr(s.Expression),
			Consequent: l.stmt(s.ThenStatement),
			Alternate:  l.stmtOrNil(s.ElseStatement),
		}
	case tsast.KindWhileStatement:
		s := n.AsWhileStatement()
		return &gast.WhileStmt{Keyword: l.pos(n), Test: l.expr(s.Expression), Body: l.stmt(s.Statement)}
	case tsast.KindDoStatement:
		s := n.AsDoStatement()
		return &gast.DoWhileStmt{Keyword: l.pos(n), Body: l.stmt(s.Statement), Test: l.expr(s.Expression)}
	case tsast.KindForStatement:
		return l.forStmt(n)
	case tsast.KindForInStatement, tsast.KindForOfStatement:
		return l.forInOf(n)
	case tsast.KindReturnStatement:
		return &gast.ReturnStmt{Keyword: l.pos(n), Argument: l.exprOrNil(n.AsReturnStatement().Expression)}
	case tsast.KindBreakStatement:
		return &gast.BreakStmt{Keyword: l.pos(n), Label: l.labelOrNil(n.AsBreakStatement().Label)}
	case tsast.KindContinueStatement:
		return &gast.ContinueStmt{Keyword: l.pos(n), Label: l.labelOrNil(n.AsContinueStatement().Label)}
	case tsast.KindThrowStatement:
		return &gast.ThrowStmt{Keyword: l.pos(n), Argument: l.expr(n.AsThrowStatement().Expression)}
	case tsast.KindTryStatement:
		return l.tryStmt(n)
	case tsast.KindSwitchStatement:
		return l.switchStmt(n)
	case tsast.KindLabeledStatement:
		s := n.AsLabeledStatement()
		return &gast.LabeledStmt{Label: l.ident(s.Label), Body: l.stmt(s.Statement)}
	case tsast.KindWithStatement:
		s := n.AsWithStatement()
		return &gast.WithStmt{Keyword: l.pos(n), Object: l.expr(s.Expression), Body: l.stmt(s.Statement)}
	case tsast.KindDebuggerStatement:
		return &gast.DebuggerStmt{Keyword: l.pos(n)}
	default:
		l.fail(n, "statement")
		return nil
	}
}

func (l *lowerer) stmtOrNil(n *tsast.Node) gast.Stmt {
	if n == nil {
		return nil
	}
	return l.stmt(n)
}

func (l *lowerer) block(n *tsast.Node) *gast.BlockStmt {
	return &gast.BlockStmt{
		Lbrace: l.pos(n),
		Body:   l.stmtList(n.AsBlock().Statements.Nodes),
	}
}

func (l *lowerer) varDecl(list *tsast.Node) *gast.VarDecl {
	dl := list.AsVariableDeclarationList()
	kind := token.VAR
	flags := list.Flags
	switch {
	case flags&tsast.NodeFlagsLet != 0:
		kind = token.LET
	case flags&tsast.NodeFlagsConst != 0:
		kind = token.CONST
	}
	decls := make([]*gast.VarDeclarator, 0, len(dl.Declarations.Nodes))
	for _, d := range dl.Declarations.Nodes {
		vd := d.AsVariableDeclaration()
		decls = append(decls, &gast.VarDeclarator{
			Target: l.bindingTarget(vd.Name()),
			Init:   l.exprOrNil(vd.Initializer),
		})
	}
	return &gast.VarDecl{Keyword: l.pos(list), Kind: kind, Decls: decls}
}

func (l *lowerer) forStmt(n *tsast.Node) gast.Stmt {
	s := n.AsForStatement()
	var init gast.Node
	if s.Initializer != nil {
		if s.Initializer.Kind == tsast.KindVariableDeclarationList {
			init = l.varDecl(s.Initializer)
		} else {
			init = l.expr(s.Initializer)
		}
	}
	return &gast.ForStmt{
		Keyword: l.pos(n),
		Init:    init,
		Test:    l.exprOrNil(s.Condition),
		Update:  l.exprOrNil(s.Incrementor),
		Body:    l.stmt(s.Statement),
	}
}

func (l *lowerer) forInOf(n *tsast.Node) gast.Stmt {
	s := n.AsForInOrOfStatement()
	var left gast.Node
	if s.Initializer.Kind == tsast.KindVariableDeclarationList {
		left = l.varDecl(s.Initializer)
	} else {
		left = l.expr(s.Initializer)
	}
	return &gast.ForInStmt{
		Keyword: l.pos(n),
		Left:    left,
		Right:   l.expr(s.Expression),
		Body:    l.stmt(s.Statement),
		Of:      n.Kind == tsast.KindForOfStatement,
		Await:   s.AwaitModifier != nil,
	}
}

func (l *lowerer) tryStmt(n *tsast.Node) gast.Stmt {
	s := n.AsTryStatement()
	out := &gast.TryStmt{Keyword: l.pos(n), Block: l.block(s.TryBlock)}
	if s.CatchClause != nil {
		cc := s.CatchClause.AsCatchClause()
		var param gast.Expr
		if cc.VariableDeclaration != nil {
			param = l.bindingTarget(cc.VariableDeclaration.AsVariableDeclaration().Name())
		}
		out.Handler = &gast.CatchClause{Keyword: l.pos(s.CatchClause), Param: param, Body: l.block(cc.Block)}
	}
	if s.FinallyBlock != nil {
		out.Finalizer = l.block(s.FinallyBlock)
	}
	return out
}

func (l *lowerer) switchStmt(n *tsast.Node) gast.Stmt {
	s := n.AsSwitchStatement()
	cb := s.CaseBlock.AsCaseBlock()
	cases := make([]*gast.SwitchCase, 0, len(cb.Clauses.Nodes))
	for _, c := range cb.Clauses.Nodes {
		clause := c.AsCaseOrDefaultClause()
		sc := &gast.SwitchCase{CasePos: l.pos(c)}
		if clause.Expression != nil { // case; default has no test
			sc.Test = l.expr(clause.Expression)
		}
		sc.Body = l.stmtList(clause.Statements.Nodes)
		cases = append(cases, sc)
	}
	return &gast.SwitchStmt{Keyword: l.pos(n), Discriminant: l.expr(s.Expression), Cases: cases}
}

func (l *lowerer) labelOrNil(n *tsast.Node) *gast.Ident {
	if n == nil {
		return nil
	}
	return l.ident(n)
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

func (l *lowerer) exprOrNil(n *tsast.Node) gast.Expr {
	if n == nil {
		return nil
	}
	return l.expr(n)
}

func (l *lowerer) expr(n *tsast.Node) gast.Expr {
	switch n.Kind {
	case tsast.KindIdentifier:
		return l.ident(n)
	case tsast.KindPrivateIdentifier:
		return &gast.PrivateIdent{NamePos: l.pos(n), Name: n.Text()}
	case tsast.KindThisKeyword:
		return &gast.ThisExpr{Keyword: l.pos(n)}
	case tsast.KindSuperKeyword:
		return &gast.SuperExpr{Keyword: l.pos(n)}
	case tsast.KindTrueKeyword:
		return &gast.BoolLit{ValuePos: l.pos(n), Value: true}
	case tsast.KindFalseKeyword:
		return &gast.BoolLit{ValuePos: l.pos(n), Value: false}
	case tsast.KindNullKeyword:
		return &gast.NullLit{ValuePos: l.pos(n)}
	case tsast.KindNumericLiteral:
		return l.numberLit(n)
	case tsast.KindBigIntLiteral:
		return l.bigintLit(n)
	case tsast.KindStringLiteral:
		return l.stringLit(n)
	case tsast.KindRegularExpressionLiteral:
		return l.regexLit(n)
	case tsast.KindNoSubstitutionTemplateLiteral:
		return &gast.TemplateLit{
			Start:  l.pos(n),
			Quasis: []gast.TemplateElement{{Pos: l.pos(n), Cooked: n.Text(), Raw: n.Text()}},
		}
	case tsast.KindTemplateExpression:
		return l.templateExpr(n)
	case tsast.KindTaggedTemplateExpression:
		return l.taggedTemplate(n)
	case tsast.KindArrayLiteralExpression:
		return l.arrayLit(n)
	case tsast.KindObjectLiteralExpression:
		return l.objectLit(n)
	case tsast.KindParenthesizedExpression:
		return l.parenthesized(n)
	case tsast.KindPropertyAccessExpression:
		return l.propertyAccess(n)
	case tsast.KindElementAccessExpression:
		return l.elementAccess(n)
	case tsast.KindCallExpression:
		return l.callExpr(n)
	case tsast.KindNewExpression:
		return l.newExpr(n)
	case tsast.KindBinaryExpression:
		return l.binaryExpr(n)
	case tsast.KindPrefixUnaryExpression:
		return l.prefixUnary(n)
	case tsast.KindPostfixUnaryExpression:
		u := n.AsPostfixUnaryExpression()
		return &gast.UpdateExpr{OpPos: l.pos(n), Op: incDecTok(u.Operator), Operand: l.expr(u.Operand), Prefix: false}
	case tsast.KindConditionalExpression:
		c := n.AsConditionalExpression()
		return &gast.ConditionalExpr{Test: l.expr(c.Condition), Consequent: l.expr(c.WhenTrue), Alternate: l.expr(c.WhenFalse)}
	case tsast.KindDeleteExpression:
		return &gast.UnaryExpr{OpPos: l.pos(n), Op: token.DELETE, Operand: l.expr(n.AsDeleteExpression().Expression)}
	case tsast.KindTypeOfExpression:
		return &gast.UnaryExpr{OpPos: l.pos(n), Op: token.TYPEOF, Operand: l.expr(n.AsTypeOfExpression().Expression)}
	case tsast.KindVoidExpression:
		return &gast.UnaryExpr{OpPos: l.pos(n), Op: token.VOID, Operand: l.expr(n.AsVoidExpression().Expression)}
	case tsast.KindAwaitExpression:
		return &gast.AwaitExpr{Keyword: l.pos(n), Argument: l.expr(n.AsAwaitExpression().Expression)}
	case tsast.KindYieldExpression:
		y := n.AsYieldExpression()
		return &gast.YieldExpr{Keyword: l.pos(n), Argument: l.exprOrNil(y.Expression), Delegate: y.AsteriskToken != nil}
	case tsast.KindFunctionExpression:
		return &gast.FuncExpr{Keyword: l.pos(n), Def: l.funcDef(n, n.Name())}
	case tsast.KindArrowFunction:
		return l.arrowFunc(n)
	case tsast.KindClassExpression:
		return &gast.ClassExpr{Keyword: l.pos(n), Def: l.classDef(n)}
	case tsast.KindSpreadElement:
		return &gast.SpreadElement{Ellipsis: l.pos(n), Argument: l.expr(n.AsSpreadElement().Expression)}
	case tsast.KindOmittedExpression:
		return nil // an array hole
	// Type-carrying wrappers that the transforms normally erase; unwrap
	// defensively in case one survives.
	case tsast.KindNonNullExpression:
		return l.expr(n.AsNonNullExpression().Expression)
	case tsast.KindAsExpression:
		return l.expr(n.AsAsExpression().Expression)
	case tsast.KindSatisfiesExpression:
		return l.expr(n.AsSatisfiesExpression().Expression)
	case tsast.KindTypeAssertionExpression:
		return l.expr(n.AsTypeAssertion().Expression)
	case tsast.KindPartiallyEmittedExpression:
		// Left by the type-eraser after stripping `as T` / `satisfies T` etc.;
		// it preserves source range but emits only its inner expression.
		return l.expr(n.AsPartiallyEmittedExpression().Expression)
	default:
		l.fail(n, "expression")
		return nil
	}
}

func (l *lowerer) ident(n *tsast.Node) *gast.Ident {
	return &gast.Ident{NamePos: l.pos(n), Name: n.Text()}
}

func (l *lowerer) parenthesized(n *tsast.Node) gast.Expr {
	inner := l.expr(n.AsParenthesizedExpression().Expression)
	// Preserve the CoverParenthesizedExpression distinction gojs tracks on a bare
	// parenthesized identifier (affects NamedEvaluation).
	if id, ok := inner.(*gast.Ident); ok {
		id.Parenthesized = true
	}
	return inner
}

func (l *lowerer) propertyAccess(n *tsast.Node) gast.Expr {
	pa := n.AsPropertyAccessExpression()
	return &gast.MemberExpr{
		Object:   l.expr(pa.Expression),
		Property: l.expr(n.Name()),
		EndPos:   l.pos(n),
		Computed: false,
		Optional: pa.QuestionDotToken != nil,
	}
}

func (l *lowerer) elementAccess(n *tsast.Node) gast.Expr {
	ea := n.AsElementAccessExpression()
	return &gast.MemberExpr{
		Object:   l.expr(ea.Expression),
		Property: l.expr(ea.ArgumentExpression),
		EndPos:   l.pos(n),
		Computed: true,
		Optional: ea.QuestionDotToken != nil,
	}
}

func (l *lowerer) callExpr(n *tsast.Node) gast.Expr {
	c := n.AsCallExpression()
	// import(...) parses as a CallExpression whose callee is the import keyword.
	if c.Expression.Kind == tsast.KindImportKeyword {
		var spec, opts gast.Expr
		args := c.Arguments.Nodes
		if len(args) > 0 {
			spec = l.expr(args[0])
		}
		if len(args) > 1 {
			opts = l.expr(args[1])
		}
		return &gast.ImportCall{Keyword: l.pos(n), Specifier: spec, Options: opts, Rparen: l.pos(n)}
	}
	return &gast.CallExpr{
		Callee:    l.expr(c.Expression),
		Arguments: l.exprs(c.Arguments.Nodes),
		Rparen:    l.pos(n),
		Optional:  c.QuestionDotToken != nil,
	}
}

func (l *lowerer) newExpr(n *tsast.Node) gast.Expr {
	ne := n.AsNewExpression()
	var args []gast.Expr
	if ne.Arguments != nil {
		args = l.exprs(ne.Arguments.Nodes)
	}
	return &gast.NewExpr{Keyword: l.pos(n), Callee: l.expr(ne.Expression), Arguments: args, EndPos: l.pos(n)}
}

func (l *lowerer) exprs(nodes []*tsast.Node) []gast.Expr {
	out := make([]gast.Expr, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, l.expr(n))
	}
	return out
}

func (l *lowerer) binaryExpr(n *tsast.Node) gast.Expr {
	b := n.AsBinaryExpression()
	opk := b.OperatorToken.Kind
	if opk == tsast.KindCommaToken {
		return l.sequence(b)
	}
	if op, ok := assignOps[opk]; ok {
		return &gast.AssignExpr{Target: l.expr(b.Left), OpPos: l.pos(b.OperatorToken), Op: op, Value: l.expr(b.Right)}
	}
	if op, ok := logicalOps[opk]; ok {
		return &gast.LogicalExpr{Left: l.expr(b.Left), OpPos: l.pos(b.OperatorToken), Op: op, Right: l.expr(b.Right)}
	}
	if op, ok := binaryOps[opk]; ok {
		return &gast.BinaryExpr{Left: l.expr(b.Left), OpPos: l.pos(b.OperatorToken), Op: op, Right: l.expr(b.Right)}
	}
	l.fail(n, "binary operator "+opk.String())
	return nil
}

// sequence flattens a comma expression tree into a single gojs SequenceExpr.
func (l *lowerer) sequence(b *tsast.BinaryExpression) gast.Expr {
	var flat []gast.Expr
	var walk func(n *tsast.Node)
	walk = func(n *tsast.Node) {
		if n.Kind == tsast.KindBinaryExpression {
			inner := n.AsBinaryExpression()
			if inner.OperatorToken.Kind == tsast.KindCommaToken {
				walk(inner.Left)
				walk(inner.Right)
				return
			}
		}
		flat = append(flat, l.expr(n))
	}
	walk(b.Left)
	walk(b.Right)
	return &gast.SequenceExpr{Exprs: flat}
}

func (l *lowerer) prefixUnary(n *tsast.Node) gast.Expr {
	u := n.AsPrefixUnaryExpression()
	switch u.Operator {
	case tsast.KindPlusPlusToken, tsast.KindMinusMinusToken:
		return &gast.UpdateExpr{OpPos: l.pos(n), Op: incDecTok(u.Operator), Operand: l.expr(u.Operand), Prefix: true}
	}
	op, ok := prefixOps[u.Operator]
	if !ok {
		l.fail(n, "prefix operator "+u.Operator.String())
	}
	return &gast.UnaryExpr{OpPos: l.pos(n), Op: op, Operand: l.expr(u.Operand)}
}

// ---------------------------------------------------------------------------
// Literals
// ---------------------------------------------------------------------------

func (l *lowerer) numberLit(n *tsast.Node) gast.Expr {
	raw := n.Text()
	return &gast.NumberLit{ValuePos: l.pos(n), Value: parseJSNumber(raw), Raw: raw}
}

func (l *lowerer) bigintLit(n *tsast.Node) gast.Expr {
	raw := n.Text()
	digits := strings.TrimSuffix(raw, "n")
	digits = strings.ReplaceAll(digits, "_", "")
	return &gast.BigIntLit{ValuePos: l.pos(n), Raw: raw, Digits: digits}
}

func (l *lowerer) stringLit(n *tsast.Node) gast.Expr {
	v := n.Text()
	return &gast.StringLit{ValuePos: l.pos(n), Value: v, Raw: strconv.Quote(v)}
}

func (l *lowerer) regexLit(n *tsast.Node) gast.Expr {
	raw := n.Text()
	pattern, flags := raw, ""
	if i := strings.LastIndexByte(raw, '/'); i > 0 {
		pattern = raw[1:i]
		flags = raw[i+1:]
	}
	return &gast.RegexLit{ValuePos: l.pos(n), Pattern: pattern, Flags: flags, Raw: raw}
}

func (l *lowerer) templateExpr(n *tsast.Node) gast.Expr {
	te := n.AsTemplateExpression()
	tl := &gast.TemplateLit{Start: l.pos(n)}
	tl.Quasis = append(tl.Quasis, gast.TemplateElement{Pos: l.pos(te.Head), Cooked: te.Head.Text(), Raw: te.Head.RawText()})
	for _, span := range te.TemplateSpans.Nodes {
		sp := span.AsTemplateSpan()
		tl.Exprs = append(tl.Exprs, l.expr(sp.Expression))
		tl.Quasis = append(tl.Quasis, gast.TemplateElement{Pos: l.pos(sp.Literal), Cooked: sp.Literal.Text(), Raw: sp.Literal.RawText()})
	}
	return tl
}

func (l *lowerer) taggedTemplate(n *tsast.Node) gast.Expr {
	tt := n.AsTaggedTemplateExpression()
	var quasi *gast.TemplateLit
	switch tl := l.expr(tt.Template).(type) {
	case *gast.TemplateLit:
		quasi = tl
	default:
		l.fail(n, "tagged template quasi")
	}
	return &gast.TaggedTemplateExpr{Tag: l.expr(tt.Tag), Quasi: quasi}
}

func (l *lowerer) arrayLit(n *tsast.Node) gast.Expr {
	a := n.AsArrayLiteralExpression()
	elems := make([]gast.Expr, 0, len(a.Elements.Nodes))
	for _, e := range a.Elements.Nodes {
		elems = append(elems, l.exprOrNil(e)) // OmittedExpression -> nil hole
	}
	return &gast.ArrayLit{Lbracket: l.pos(n), Elements: elems}
}

func (l *lowerer) objectLit(n *tsast.Node) gast.Expr {
	o := n.AsObjectLiteralExpression()
	props := make([]*gast.Property, 0, len(o.Properties.Nodes))
	for _, p := range o.Properties.Nodes {
		props = append(props, l.objectMember(p))
	}
	return &gast.ObjectLit{Lbrace: l.pos(n), Properties: props}
}

func (l *lowerer) objectMember(n *tsast.Node) *gast.Property {
	switch n.Kind {
	case tsast.KindPropertyAssignment:
		key, computed := l.propKey(n.Name())
		return &gast.Property{KeyPos: l.pos(n), Key: key, Value: l.expr(n.AsPropertyAssignment().Initializer), Kind: gast.PropInit, Computed: computed}
	case tsast.KindShorthandPropertyAssignment:
		sp := n.AsShorthandPropertyAssignment()
		key, _ := l.propKey(n.Name())
		val := gast.Expr(l.ident(n.Name()))
		if sp.ObjectAssignmentInitializer != nil {
			// `{ x = default }` destructuring shorthand.
			val = &gast.AssignPattern{Target: l.ident(n.Name()), Default: l.expr(sp.ObjectAssignmentInitializer)}
		}
		return &gast.Property{KeyPos: l.pos(n), Key: key, Value: val, Kind: gast.PropInit, Shorthand: true}
	case tsast.KindSpreadAssignment:
		return &gast.Property{KeyPos: l.pos(n), Value: l.expr(n.AsSpreadAssignment().Expression), Kind: gast.PropSpread}
	case tsast.KindMethodDeclaration:
		key, computed := l.propKey(n.Name())
		return &gast.Property{KeyPos: l.pos(n), Key: key, Value: l.methodFunc(n), Kind: gast.PropInit, Computed: computed, Method: true}
	case tsast.KindGetAccessor:
		key, computed := l.propKey(n.Name())
		return &gast.Property{KeyPos: l.pos(n), Key: key, Value: l.methodFunc(n), Kind: gast.PropGet, Computed: computed}
	case tsast.KindSetAccessor:
		key, computed := l.propKey(n.Name())
		return &gast.Property{KeyPos: l.pos(n), Key: key, Value: l.methodFunc(n), Kind: gast.PropSet, Computed: computed}
	default:
		l.fail(n, "object member")
		return nil
	}
}

// propKey lowers a property/member name, reporting whether it was computed.
func (l *lowerer) propKey(name *tsast.Node) (gast.Expr, bool) {
	switch name.Kind {
	case tsast.KindIdentifier:
		return l.ident(name), false
	case tsast.KindPrivateIdentifier:
		return &gast.PrivateIdent{NamePos: l.pos(name), Name: name.Text()}, false
	case tsast.KindStringLiteral:
		return l.stringLit(name), false
	case tsast.KindNumericLiteral:
		return l.numberLit(name), false
	case tsast.KindComputedPropertyName:
		return l.expr(name.AsComputedPropertyName().Expression), true
	default:
		l.fail(name, "property key")
		return nil, false
	}
}

// ---------------------------------------------------------------------------
// Functions
// ---------------------------------------------------------------------------

// funcDef lowers a function-like node (declaration, expression, method,
// accessor, constructor) into a gojs FuncDef with the given name.
func (l *lowerer) funcDef(n *tsast.Node, name *tsast.Node) *gast.FuncDef {
	async := tsast.HasSyntacticModifier(n, tsast.ModifierFlagsAsync)
	gen := hasAsterisk(n)
	savedStrict := l.strict
	def := &gast.FuncDef{
		Name:      l.identOrNil(name),
		Params:    l.params(n.Parameters()),
		Async:     async,
		Generator: gen,
	}
	def.Body = l.functionBody(n.Body())
	def.Strict = l.strict // set by the body's directive prologue, if any
	l.strict = savedStrict
	return def
}

// methodFunc wraps a method/accessor body in a gojs FuncExpr (anonymous).
func (l *lowerer) methodFunc(n *tsast.Node) gast.Expr {
	return &gast.FuncExpr{Keyword: l.pos(n), Def: l.funcDef(n, nil)}
}

// functionBody lowers a function body (always a block for non-arrow functions).
func (l *lowerer) functionBody(body *tsast.Node) *gast.BlockStmt {
	if body == nil {
		return &gast.BlockStmt{} // overload signature / abstract — empty body
	}
	return l.block(body)
}

func (l *lowerer) arrowFunc(n *tsast.Node) gast.Expr {
	a := n.AsArrowFunction()
	async := tsast.HasSyntacticModifier(n, tsast.ModifierFlagsAsync)
	savedStrict := l.strict
	arrow := &gast.ArrowFunc{
		Start:  l.pos(n),
		Params: l.params(n.Parameters()),
		Async:  async,
	}
	if a.Body.Kind == tsast.KindBlock {
		arrow.Body = l.block(a.Body)
	} else {
		arrow.Body = l.expr(a.Body)
		arrow.Expression = true
	}
	arrow.Strict = l.strict
	l.strict = savedStrict
	return arrow
}

func (l *lowerer) params(nodes []*tsast.Node) []gast.Expr {
	out := make([]gast.Expr, 0, len(nodes))
	for _, p := range nodes {
		pd := p.AsParameterDeclaration()
		target := l.bindingTarget(pd.Name())
		switch {
		case pd.DotDotDotToken != nil:
			out = append(out, &gast.RestElement{Ellipsis: l.pos(p), Target: target})
		case pd.Initializer != nil:
			out = append(out, &gast.AssignPattern{Target: target, Default: l.expr(pd.Initializer)})
		default:
			out = append(out, target)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Binding targets (identifiers and destructuring patterns)
// ---------------------------------------------------------------------------

func (l *lowerer) bindingTarget(n *tsast.Node) gast.Expr {
	switch n.Kind {
	case tsast.KindIdentifier:
		return l.ident(n)
	case tsast.KindObjectBindingPattern:
		return l.objectBindingPattern(n)
	case tsast.KindArrayBindingPattern:
		return l.arrayBindingPattern(n)
	default:
		l.fail(n, "binding target")
		return nil
	}
}

func (l *lowerer) objectBindingPattern(n *tsast.Node) gast.Expr {
	bp := n.AsBindingPattern()
	props := make([]*gast.Property, 0, len(bp.Elements.Nodes))
	for _, e := range bp.Elements.Nodes {
		be := e.AsBindingElement()
		target := l.bindingTarget(be.Name())
		if be.DotDotDotToken != nil {
			props = append(props, &gast.Property{KeyPos: l.pos(e), Value: target, Kind: gast.PropSpread})
			continue
		}
		value := target
		if be.Initializer != nil {
			value = &gast.AssignPattern{Target: target, Default: l.expr(be.Initializer)}
		}
		var key gast.Expr
		computed := false
		shorthand := be.PropertyName == nil
		if be.PropertyName != nil {
			key, computed = l.propKey(be.PropertyName)
		} else {
			key = l.bindingName(be.Name())
		}
		props = append(props, &gast.Property{KeyPos: l.pos(e), Key: key, Value: value, Kind: gast.PropInit, Computed: computed, Shorthand: shorthand})
	}
	return &gast.ObjectLit{Lbrace: l.pos(n), Properties: props}
}

func (l *lowerer) arrayBindingPattern(n *tsast.Node) gast.Expr {
	bp := n.AsBindingPattern()
	elems := make([]gast.Expr, 0, len(bp.Elements.Nodes))
	for _, e := range bp.Elements.Nodes {
		if e.Kind == tsast.KindOmittedExpression {
			elems = append(elems, nil) // hole
			continue
		}
		be := e.AsBindingElement()
		if be.Name() == nil { // elision represented as an empty binding element
			elems = append(elems, nil)
			continue
		}
		target := l.bindingTarget(be.Name())
		switch {
		case be.DotDotDotToken != nil:
			elems = append(elems, &gast.RestElement{Ellipsis: l.pos(e), Target: target})
		case be.Initializer != nil:
			elems = append(elems, &gast.AssignPattern{Target: target, Default: l.expr(be.Initializer)})
		default:
			elems = append(elems, target)
		}
	}
	return &gast.ArrayLit{Lbracket: l.pos(n), Elements: elems}
}

// bindingName returns an Ident for a shorthand object-pattern key. The binding
// itself may be a nested pattern, but the *key* of a shorthand entry is always a
// plain identifier.
func (l *lowerer) bindingName(n *tsast.Node) *gast.Ident {
	if n.Kind == tsast.KindIdentifier {
		return l.ident(n)
	}
	l.fail(n, "shorthand binding key")
	return nil
}

func (l *lowerer) identOrNil(n *tsast.Node) *gast.Ident {
	if n == nil {
		return nil
	}
	return l.ident(n)
}

// ---------------------------------------------------------------------------
// Classes
// ---------------------------------------------------------------------------

func (l *lowerer) classDef(n *tsast.Node) *gast.ClassDef {
	// A class body is always strict code.
	savedStrict := l.strict
	l.strict = true
	def := &gast.ClassDef{
		Name:   l.identOrNil(n.Name()),
		Lbrace: l.pos(n),
		Rbrace: l.pos(n),
	}
	if sc := l.extendsClause(n); sc != nil {
		def.SuperClass = sc
	}
	for _, m := range n.Members() {
		if cm := l.classMember(m); cm != nil {
			def.Members = append(def.Members, cm)
		}
	}
	l.strict = savedStrict
	return def
}

func (l *lowerer) extendsClause(n *tsast.Node) gast.Expr {
	clauses := n.ClassLikeData().HeritageClauses
	if clauses == nil {
		return nil
	}
	for _, hc := range clauses.Nodes {
		h := hc.AsHeritageClause()
		if h.Token == tsast.KindExtendsKeyword && len(h.Types.Nodes) > 0 {
			return l.expr(h.Types.Nodes[0].AsExpressionWithTypeArguments().Expression)
		}
	}
	return nil
}

func (l *lowerer) classMember(n *tsast.Node) *gast.ClassMember {
	static := tsast.IsStatic(n)
	switch n.Kind {
	case tsast.KindPropertyDeclaration:
		key, computed := l.propKey(n.Name())
		return &gast.ClassMember{KeyPos: l.pos(n), Key: key, Value: l.exprOrNil(n.AsPropertyDeclaration().Initializer), Kind: gast.PropInit, Static: static, Computed: computed, Field: true}
	case tsast.KindMethodDeclaration:
		key, computed := l.propKey(n.Name())
		return &gast.ClassMember{KeyPos: l.pos(n), Key: key, Value: l.methodFunc(n), Kind: gast.PropInit, Static: static, Computed: computed}
	case tsast.KindGetAccessor:
		key, computed := l.propKey(n.Name())
		return &gast.ClassMember{KeyPos: l.pos(n), Key: key, Value: l.methodFunc(n), Kind: gast.PropGet, Static: static, Computed: computed}
	case tsast.KindSetAccessor:
		key, computed := l.propKey(n.Name())
		return &gast.ClassMember{KeyPos: l.pos(n), Key: key, Value: l.methodFunc(n), Kind: gast.PropSet, Static: static, Computed: computed}
	case tsast.KindConstructor:
		return &gast.ClassMember{KeyPos: l.pos(n), Key: &gast.Ident{NamePos: l.pos(n), Name: "constructor"}, Value: l.methodFunc(n)}
	case tsast.KindClassStaticBlockDeclaration:
		return &gast.ClassMember{KeyPos: l.pos(n), Static: true, StaticBlock: l.block(n.AsClassStaticBlockDeclaration().Body)}
	case tsast.KindSemicolonClassElement:
		return nil
	default:
		l.fail(n, "class member")
		return nil
	}
}

// ---------------------------------------------------------------------------
// Helpers: operator tables, number parsing, modifier probes
// ---------------------------------------------------------------------------

func hasAsterisk(n *tsast.Node) bool {
	switch n.Kind {
	case tsast.KindFunctionDeclaration:
		return n.AsFunctionDeclaration().AsteriskToken != nil
	case tsast.KindFunctionExpression:
		return n.AsFunctionExpression().AsteriskToken != nil
	case tsast.KindMethodDeclaration:
		return n.AsMethodDeclaration().AsteriskToken != nil
	}
	return false
}

func incDecTok(k tsast.Kind) token.Type {
	if k == tsast.KindMinusMinusToken {
		return token.DEC
	}
	return token.INC
}

var prefixOps = map[tsast.Kind]token.Type{
	tsast.KindPlusToken:        token.PLUS,
	tsast.KindMinusToken:       token.MINUS,
	tsast.KindExclamationToken: token.NOT,
	tsast.KindTildeToken:       token.BIT_NOT,
}

var logicalOps = map[tsast.Kind]token.Type{
	tsast.KindAmpersandAmpersandToken: token.AND,
	tsast.KindBarBarToken:             token.OR,
	tsast.KindQuestionQuestionToken:   token.NULLISH,
}

var assignOps = map[tsast.Kind]token.Type{
	tsast.KindEqualsToken:                                  token.ASSIGN,
	tsast.KindPlusEqualsToken:                              token.PLUS_ASSIGN,
	tsast.KindMinusEqualsToken:                             token.MINUS_ASSIGN,
	tsast.KindAsteriskEqualsToken:                          token.STAR_ASSIGN,
	tsast.KindSlashEqualsToken:                             token.SLASH_ASSIGN,
	tsast.KindPercentEqualsToken:                           token.PERCENT_ASSIGN,
	tsast.KindAsteriskAsteriskEqualsToken:                  token.EXP_ASSIGN,
	tsast.KindLessThanLessThanEqualsToken:                  token.SHL_ASSIGN,
	tsast.KindGreaterThanGreaterThanEqualsToken:            token.SHR_ASSIGN,
	tsast.KindGreaterThanGreaterThanGreaterThanEqualsToken: token.USHR_ASSIGN,
	tsast.KindAmpersandEqualsToken:                         token.BIT_AND_ASSIGN,
	tsast.KindBarEqualsToken:                               token.BIT_OR_ASSIGN,
	tsast.KindCaretEqualsToken:                             token.BIT_XOR_ASSIGN,
	tsast.KindAmpersandAmpersandEqualsToken:                token.AND_ASSIGN,
	tsast.KindBarBarEqualsToken:                            token.OR_ASSIGN,
	tsast.KindQuestionQuestionEqualsToken:                  token.NULLISH_ASSIGN,
}

var binaryOps = map[tsast.Kind]token.Type{
	tsast.KindPlusToken:                              token.PLUS,
	tsast.KindMinusToken:                             token.MINUS,
	tsast.KindAsteriskToken:                          token.STAR,
	tsast.KindSlashToken:                             token.SLASH,
	tsast.KindPercentToken:                           token.PERCENT,
	tsast.KindAsteriskAsteriskToken:                  token.EXP,
	tsast.KindEqualsEqualsToken:                      token.EQ,
	tsast.KindExclamationEqualsToken:                 token.NE,
	tsast.KindEqualsEqualsEqualsToken:                token.STRICT_EQ,
	tsast.KindExclamationEqualsEqualsToken:           token.STRICT_NE,
	tsast.KindLessThanToken:                          token.LT,
	tsast.KindGreaterThanToken:                       token.GT,
	tsast.KindLessThanEqualsToken:                    token.LE,
	tsast.KindGreaterThanEqualsToken:                 token.GE,
	tsast.KindAmpersandToken:                         token.BIT_AND,
	tsast.KindBarToken:                               token.BIT_OR,
	tsast.KindCaretToken:                             token.BIT_XOR,
	tsast.KindLessThanLessThanToken:                  token.SHL,
	tsast.KindGreaterThanGreaterThanToken:            token.SHR,
	tsast.KindGreaterThanGreaterThanGreaterThanToken: token.USHR,
	tsast.KindInKeyword:                              token.IN,
	tsast.KindInstanceOfKeyword:                      token.INSTANCEOF,
}

// parseJSNumber parses a JavaScript numeric-literal spelling (decimal, hex,
// octal, binary, with underscore separators and exponents) into its IEEE-754
// value.
func parseJSNumber(text string) float64 {
	s := strings.ReplaceAll(text, "_", "")
	if len(s) > 2 {
		switch s[0:2] {
		case "0x", "0X":
			if v, err := strconv.ParseUint(s[2:], 16, 64); err == nil {
				return float64(v)
			}
		case "0o", "0O":
			if v, err := strconv.ParseUint(s[2:], 8, 64); err == nil {
				return float64(v)
			}
		case "0b", "0B":
			if v, err := strconv.ParseUint(s[2:], 2, 64); err == nil {
				return float64(v)
			}
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return 0
}
