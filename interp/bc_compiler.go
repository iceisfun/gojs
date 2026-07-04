package interp

import (
	"errors"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file compiles a function body's AST to a codeObject (bc_opcodes.go) for
// the stack VM (bc_vm.go). It lowers the hot structural nodes to real opcodes and
// emits opEvalNode/opEvalStmt for anything not yet handled natively, so a compiled
// body is always correct. A handful of whole-function-unsafe constructs (labels,
// direct eval, yield/await) abort compilation via errBCUnsupported, and the
// caller falls back to the tree-walker for the entire function.

// errBCUnsupported aborts compilation of a whole function; makeFunction then uses
// the tree-walker for it. Used only for constructs that can't be safely mixed
// with compiled control flow in v1 (see scanning in the compiler).
var errBCUnsupported = errors.New("bytecode: unsupported construct")

// bcCompiler lowers one function body to a codeObject.
type bcCompiler struct {
	i    *Interpreter
	code *codeObject
	err  error
}

// compileFunctionBody attempts to compile def's body to a codeObject. It returns
// (nil, false) when the body contains a construct that forces a whole-function
// fallback to the tree-walker.
func (i *Interpreter) compileFunctionBody(def *ast.FuncDef, strict bool) (*codeObject, bool) {
	if def.Body == nil {
		return nil, false
	}
	c := &bcCompiler{i: i, code: &codeObject{name: funcName(def), strict: strict}}
	for _, s := range def.Body.Body {
		c.stmt(s)
		if c.err != nil {
			return nil, false
		}
	}
	// Falling off the end of a function returns undefined.
	c.emit(opPushUndef, 0, 0)
	c.emit(opReturn, 0, 0)
	if c.err != nil {
		return nil, false
	}
	return c.code, true
}

func funcName(def *ast.FuncDef) string {
	if def.Name != nil {
		return def.Name.Name
	}
	return ""
}

// --- emit helpers -----------------------------------------------------------

func (c *bcCompiler) emit(op bcOp, a, b int32) int { return c.code.emit(op, a, b) }
func (c *bcCompiler) here() int                    { return len(c.code.instrs) }
func (c *bcCompiler) patchTarget(idx, target int)  { c.code.instrs[idx].a = int32(target) }
func (c *bcCompiler) fail()                         { c.err = errBCUnsupported }

// --- statements -------------------------------------------------------------

func (c *bcCompiler) stmt(s ast.Stmt) {
	if c.err != nil {
		return
	}
	switch st := s.(type) {
	case *ast.ExprStmt:
		c.expr(st.X)
		c.emit(opPop, 0, 0)
	case *ast.EmptyStmt, *ast.DebuggerStmt:
		// no-op
	case *ast.FuncDecl:
		// Already created by hoisting; empty completion (see evalStmt FuncDecl).
	case *ast.VarDecl:
		c.varDecl(st)
	case *ast.BlockStmt:
		c.emit(opEnterScope, c.code.nodeIndex(st), 0)
		for _, sub := range st.Body {
			c.stmt(sub)
		}
		c.emit(opExitScope, 0, 0)
	case *ast.IfStmt:
		c.ifStmt(st)
	case *ast.WhileStmt:
		c.whileStmt(st)
	case *ast.DoWhileStmt:
		c.doWhileStmt(st)
	case *ast.ForStmt:
		c.forStmt(st)
	case *ast.ReturnStmt:
		if st.Argument != nil {
			c.expr(st.Argument)
		} else {
			c.emit(opPushUndef, 0, 0)
		}
		c.emit(opReturn, 0, 0)
	case *ast.BreakStmt:
		if st.Label != nil {
			c.fail() // labeled control flow ⇒ whole-function fallback
			return
		}
		c.emit(opBreak, 0, 0)
	case *ast.ContinueStmt:
		if st.Label != nil {
			c.fail()
			return
		}
		c.emit(opContinue, 0, 0)
	case *ast.ThrowStmt:
		c.expr(st.Argument)
		c.emit(opThrow, 0, 0)
	case *ast.LabeledStmt:
		// Labeled break/continue could target a compiled loop from within a
		// fallback subtree; keep the whole function on the tree-walker instead.
		c.fail()
	case *ast.ClassDecl, *ast.WithStmt, *ast.ForInStmt, *ast.TryStmt, *ast.SwitchStmt:
		// Not yet compiled natively: run the whole statement on the tree-walker.
		c.emit(opEvalStmt, c.code.nodeIndex(s), 0)
	default:
		c.emit(opEvalStmt, c.code.nodeIndex(s), 0)
	}
}

func (c *bcCompiler) varDecl(d *ast.VarDecl) {
	// Only simple identifier targets are compiled natively; a destructuring
	// target runs the whole declaration on the tree-walker.
	for _, decl := range d.Decls {
		if _, ok := decl.Target.(*ast.Ident); !ok {
			c.emit(opEvalStmt, c.code.nodeIndex(d), 0)
			return
		}
	}
	for _, decl := range d.Decls {
		name := decl.Target.(*ast.Ident).Name
		switch d.Kind {
		case token.VAR:
			if decl.Init == nil {
				continue // `var x;` must not reset an existing hoisted binding
			}
			c.exprNamed(decl.Init, name)
			c.emit(opDeclareVar, c.code.nameIndex(name), 0)
		default: // let / const
			if decl.Init != nil {
				c.exprNamed(decl.Init, name)
			} else {
				c.emit(opPushUndef, 0, 0)
			}
			mutable := int32(0)
			if d.Kind == token.LET {
				mutable = 1
			}
			c.emit(opDeclareLex, c.code.nameIndex(name), mutable)
		}
	}
}

func (c *bcCompiler) ifStmt(s *ast.IfStmt) {
	c.expr(s.Test)
	jElse := c.emit(opJumpIfFalse, 0, 0)
	c.stmt(s.Consequent)
	if s.Alternate != nil {
		jEnd := c.emit(opJump, 0, 0)
		c.patchTarget(jElse, c.here())
		c.stmt(s.Alternate)
		c.patchTarget(jEnd, c.here())
	} else {
		c.patchTarget(jElse, c.here())
	}
}

// Loop layout (see bc_vm.go for the runtime loop stack that opEnterLoop pushes):
//
//	opEnterLoop(breakIP, contIP)
//	Ltop: <test> ; opJumpIfFalse breakIP
//	      <body>
//	Lcont:<update?>
//	      opJump Ltop
//	breakIP: opExitLoop
func (c *bcCompiler) whileStmt(s *ast.WhileStmt) {
	enter := c.emit(opEnterLoop, 0, 0)
	top := c.here()
	c.expr(s.Test)
	jFalse := c.emit(opJumpIfFalse, 0, 0)
	c.stmt(s.Body)
	cont := c.here()
	c.emit(opJump, int32(top), 0)
	brk := c.here()
	c.emit(opExitLoop, 0, 0)
	c.patchTarget(jFalse, brk)
	c.code.instrs[enter].a = int32(brk)
	c.code.instrs[enter].b = int32(cont)
}

func (c *bcCompiler) doWhileStmt(s *ast.DoWhileStmt) {
	enter := c.emit(opEnterLoop, 0, 0)
	top := c.here()
	c.stmt(s.Body)
	cont := c.here()
	c.expr(s.Test)
	c.emit(opJumpIfTrue, int32(top), 0)
	brk := c.here()
	c.emit(opExitLoop, 0, 0)
	c.code.instrs[enter].a = int32(brk)
	c.code.instrs[enter].b = int32(cont)
}

func (c *bcCompiler) forStmt(s *ast.ForStmt) {
	// A lexical (let/const) for-head needs per-iteration environment semantics
	// (CreatePerIterationEnvironment); defer that to the tree-walker for now.
	if vd, ok := s.Init.(*ast.VarDecl); ok && vd.Kind != token.VAR {
		c.emit(opEvalStmt, c.code.nodeIndex(s), 0)
		return
	}
	// init
	switch init := s.Init.(type) {
	case nil:
		// none
	case *ast.VarDecl:
		c.varDecl(init)
	case ast.Expr:
		c.expr(init)
		c.emit(opPop, 0, 0)
	default:
		c.emit(opEvalStmt, c.code.nodeIndex(s), 0)
		return
	}
	enter := c.emit(opEnterLoop, 0, 0)
	top := c.here()
	var jFalse int = -1
	if s.Test != nil {
		c.expr(s.Test)
		jFalse = c.emit(opJumpIfFalse, 0, 0)
	}
	c.stmt(s.Body)
	cont := c.here()
	if s.Update != nil {
		c.expr(s.Update)
		c.emit(opPop, 0, 0)
	}
	c.emit(opJump, int32(top), 0)
	brk := c.here()
	c.emit(opExitLoop, 0, 0)
	if jFalse >= 0 {
		c.patchTarget(jFalse, brk)
	}
	c.code.instrs[enter].a = int32(brk)
	c.code.instrs[enter].b = int32(cont)
}

// --- expressions ------------------------------------------------------------

// expr compiles e, leaving exactly one value on the operand stack.
func (c *bcCompiler) expr(e ast.Expr) { c.exprNamed(e, "") }

// exprNamed is expr with a NamedEvaluation hint used when e is an anonymous
// function/class initializer (so `let f = () => {}` names the function "f").
func (c *bcCompiler) exprNamed(e ast.Expr, name string) {
	if c.err != nil {
		return
	}
	switch ex := e.(type) {
	case *ast.NumberLit:
		c.emit(opPushConst, c.code.constIndex(Number(ex.Value)), 0)
	case *ast.StringLit:
		c.emit(opPushConst, c.code.constIndex(String(ex.Value)), 0)
	case *ast.BoolLit:
		if ex.Value {
			c.emit(opPushTrue, 0, 0)
		} else {
			c.emit(opPushFalse, 0, 0)
		}
	case *ast.NullLit:
		c.emit(opPushNull, 0, 0)
	case *ast.Ident:
		if ex.Name == "new.target" {
			c.emit(opPushNewTarget, 0, 0)
			return
		}
		c.emit(opLoadName, c.code.nameIndex(ex.Name), 0)
	case *ast.ThisExpr:
		c.emit(opPushThis, 0, 0)
	case *ast.BinaryExpr:
		c.binary(ex)
	case *ast.LogicalExpr:
		c.logical(ex)
	case *ast.UnaryExpr:
		c.unary(ex)
	case *ast.UpdateExpr:
		c.emit(opUpdate, c.code.nodeIndex(ex), 0)
	case *ast.AssignExpr:
		c.assign(ex, name)
	case *ast.ConditionalExpr:
		c.expr(ex.Test)
		jElse := c.emit(opJumpIfFalse, 0, 0)
		c.expr(ex.Consequent)
		jEnd := c.emit(opJump, 0, 0)
		c.patchTarget(jElse, c.here())
		c.expr(ex.Alternate)
		c.patchTarget(jEnd, c.here())
	case *ast.SequenceExpr:
		for idx, sub := range ex.Exprs {
			c.expr(sub)
			if idx != len(ex.Exprs)-1 {
				c.emit(opPop, 0, 0)
			}
		}
	case *ast.MemberExpr:
		c.member(ex)
	case *ast.CallExpr:
		c.call(ex)
	case *ast.NewExpr:
		c.newExpr(ex)
	case *ast.ArrayLit:
		c.arrayLit(ex)
	case *ast.TemplateLit:
		c.template(ex)
	case *ast.FuncExpr, *ast.ArrowFunc, *ast.ClassExpr:
		c.emit(opClosure, c.code.nodeIndex(e), c.code.nameIndex(name))
	case *ast.YieldExpr, *ast.AwaitExpr:
		c.fail() // must not appear in a plain-function body; be safe
	default:
		// Objects, tagged templates, optional chaining, regex, bigint, spread,
		// super, import() — run this subtree on the tree-walker.
		c.emit(opEvalNode, c.code.nodeIndex(e), 0)
	}
}

func (c *bcCompiler) binary(ex *ast.BinaryExpr) {
	switch ex.Op {
	case token.INSTANCEOF:
		c.expr(ex.Left)
		c.expr(ex.Right)
		c.emit(opInstOf, 0, 0)
	case token.IN:
		// `#priv in obj` and `key in obj` — defer to the tree-walker.
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
	default:
		c.expr(ex.Left)
		c.expr(ex.Right)
		c.emit(opBinop, int32(ex.Op), 0)
	}
}

func (c *bcCompiler) logical(ex *ast.LogicalExpr) {
	c.expr(ex.Left)
	var j int
	switch ex.Op {
	case token.AND:
		j = c.emit(opJumpIfFalseKp, 0, 0)
	case token.OR:
		j = c.emit(opJumpIfTrueKp, 0, 0)
	case token.NULLISH:
		j = c.emit(opJumpIfNotNull, 0, 0)
	default:
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	c.expr(ex.Right)
	c.patchTarget(j, c.here())
}

func (c *bcCompiler) unary(ex *ast.UnaryExpr) {
	switch ex.Op {
	case token.TYPEOF:
		if id, ok := ex.Operand.(*ast.Ident); ok && id.Name != "new.target" {
			c.emit(opTypeofName, c.code.nameIndex(id.Name), 0)
			return
		}
		c.expr(ex.Operand)
		c.emit(opTypeofVal, 0, 0)
	case token.DELETE:
		c.emit(opDelete, c.code.nodeIndex(ex), 0)
	case token.VOID:
		c.expr(ex.Operand)
		c.emit(opPop, 0, 0)
		c.emit(opPushUndef, 0, 0)
	case token.MINUS, token.PLUS, token.NOT, token.BIT_NOT:
		c.expr(ex.Operand)
		c.emit(opUnop, int32(ex.Op), 0)
	default:
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
	}
}

func (c *bcCompiler) member(ex *ast.MemberExpr) {
	if _, ok := ex.Object.(*ast.SuperExpr); ok {
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	if ex.Optional {
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	if _, ok := ex.Property.(*ast.PrivateIdent); ok {
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	c.expr(ex.Object)
	if ex.Computed {
		c.expr(ex.Property)
		c.emit(opGetElem, 0, 0)
		return
	}
	name := ex.Property.(*ast.Ident).Name
	c.emit(opGetProp, c.code.nameIndex(name), 0)
}

func (c *bcCompiler) call(ex *ast.CallExpr) {
	if ex.Optional || hasSpreadArg(ex.Arguments) {
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	// Direct eval alters the whole function scope; keep such functions on the
	// tree-walker entirely.
	if id, ok := ex.Callee.(*ast.Ident); ok && id.Name == "eval" {
		c.fail()
		return
	}
	// A super() call needs the derived-constructor machinery; delegate the whole
	// call to the tree-walker (a bare SuperExpr is not a valid standalone value).
	if _, ok := ex.Callee.(*ast.SuperExpr); ok {
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	// Method call: preserve `this` = the base object. Only a static, non-super,
	// non-optional member callee is handled natively. The method property is
	// fetched (opMethod) BEFORE the arguments are evaluated, matching the spec
	// order (GetValue of the callee reference precedes ArgumentListEvaluation).
	if m, ok := ex.Callee.(*ast.MemberExpr); ok {
		_, super := m.Object.(*ast.SuperExpr)
		_, priv := m.Property.(*ast.PrivateIdent)
		if !super && !priv && !m.Optional && !m.Computed {
			c.expr(m.Object)
			c.emit(opMethod, c.code.nameIndex(m.Property.(*ast.Ident).Name), 0)
			for _, a := range ex.Arguments {
				c.expr(a)
			}
			c.emit(opCall, int32(len(ex.Arguments)), 0)
			return
		}
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	// Plain call: `this` is undefined.
	c.expr(ex.Callee)
	c.emit(opPushUndef, 0, 0)
	for _, a := range ex.Arguments {
		c.expr(a)
	}
	c.emit(opCall, int32(len(ex.Arguments)), 0)
}

func (c *bcCompiler) newExpr(ex *ast.NewExpr) {
	if hasSpreadArg(ex.Arguments) {
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	c.expr(ex.Callee)
	for _, a := range ex.Arguments {
		c.expr(a)
	}
	c.emit(opNew, int32(len(ex.Arguments)), 0)
}

func (c *bcCompiler) arrayLit(ex *ast.ArrayLit) {
	for _, el := range ex.Elements {
		if el == nil {
			c.emit(opEvalNode, c.code.nodeIndex(ex), 0) // holes ⇒ tree-walker
			return
		}
		if _, ok := el.(*ast.SpreadElement); ok {
			c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
			return
		}
	}
	for _, el := range ex.Elements {
		c.expr(el)
	}
	c.emit(opNewArray, int32(len(ex.Elements)), 0)
}

func (c *bcCompiler) template(ex *ast.TemplateLit) {
	// Build ((((q0 + e0) + q1) + e1) ... ) — string-prefixed `+` matches the
	// ToString-and-concatenate semantics of an untagged template.
	c.emit(opPushConst, c.code.constIndex(String(ex.Quasis[0].Cooked)), 0)
	for idx, sub := range ex.Exprs {
		c.expr(sub)
		c.emit(opBinop, int32(token.PLUS), 0)
		c.emit(opPushConst, c.code.constIndex(String(ex.Quasis[idx+1].Cooked)), 0)
		c.emit(opBinop, int32(token.PLUS), 0)
	}
}

func (c *bcCompiler) assign(ex *ast.AssignExpr, _ string) {
	// Only simple `=` to an identifier or a static/computed member is native;
	// compound and logical assignment, and destructuring targets, fall back.
	if ex.Op != token.ASSIGN {
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
		return
	}
	switch tgt := ex.Target.(type) {
	case *ast.Ident:
		// NamedEvaluation applies only to a bare identifier target, not a covered
		// `(x) = fn` (§13.15.2), so suppress the name hint when parenthesized.
		inferName := tgt.Name
		if tgt.Parenthesized {
			inferName = ""
		}
		c.exprNamed(ex.Value, inferName)
		c.emit(opDup, 0, 0)
		c.emit(opStoreName, c.code.nameIndex(tgt.Name), 0)
	case *ast.MemberExpr:
		_, super := tgt.Object.(*ast.SuperExpr)
		_, priv := tgt.Property.(*ast.PrivateIdent)
		if super || priv || tgt.Optional {
			c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
			return
		}
		c.expr(tgt.Object)
		if tgt.Computed {
			c.expr(tgt.Property)
			c.expr(ex.Value)
			c.emit(opSetElem, 0, 0)
			return
		}
		c.expr(ex.Value)
		c.emit(opSetProp, c.code.nameIndex(tgt.Property.(*ast.Ident).Name), 0)
	default:
		// Array/object destructuring assignment target.
		c.emit(opEvalNode, c.code.nodeIndex(ex), 0)
	}
}

// --- helpers ----------------------------------------------------------------

func hasSpreadArg(args []ast.Expr) bool {
	for _, a := range args {
		if _, ok := a.(*ast.SpreadElement); ok {
			return true
		}
	}
	return false
}
