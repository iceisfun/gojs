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
	// slots is non-nil in slot mode: a local name → frame slot index for the
	// function-scope bindings (parameters and hoisted `var`s, assigned by
	// planSlots). In slot mode the compiler emits slot opcodes for locals and
	// ABORTS (c.fail) the moment it would otherwise emit a fallback, reference a
	// captured/nested binding, or use `arguments` — the exact conditions under
	// which a slot local would be unreachable. That makes "the slot compile
	// succeeded" the single source of truth for slot-eligibility (no separate
	// analysis to keep in sync).
	slots map[string]int
	// lexScopes is the block-scoped lexical (let/const) environment, innermost
	// last. Because slot mode forbids nested closures (a FuncExpr/ArrowFunc aborts
	// it), no lexical binding can ever be captured, so per-iteration environments
	// are unobservable and every lexical is a plain frame slot. Each distinct
	// binding gets its own slot (no reuse) so slotNames stays unambiguous for TDZ
	// error messages; nextSlot is the next free index past the function-scope
	// slots.
	lexScopes []map[string]lexInfo
	nextSlot  int32
}

// lexInfo records a lexical binding's frame slot and whether it is const (a
// const reassignment aborts slot mode — see the assignment/update compilers).
type lexInfo struct {
	slot  int32
	konst bool
}

// compileFunctionBody compiles def's body to a codeObject, or returns (nil, false)
// if even the name-based compile hits a whole-function fallback. It first attempts
// slot mode (locals in frame slots); if that aborts, it falls back to the
// name-based compile, which uses the environment for every binding.
func (i *Interpreter) compileFunctionBody(def *ast.FuncDef, strict bool) (*codeObject, bool) {
	if def.Body == nil {
		return nil, false
	}
	if sp := planSlots(def); sp != nil {
		c := &bcCompiler{i: i, slots: sp.byName, nextSlot: int32(len(sp.slotName)),
			code: &codeObject{name: funcName(def), strict: strict, numSlots: len(sp.slotName),
				slotNames: sp.slotName, paramSlots: sp.paramSlot, numParams: sp.numParams}}
		if c.compileBody(def) {
			// Lexical bindings allocated slots past the function-scope ones; grow the
			// frame to cover them.
			c.code.numSlots = int(c.nextSlot)
			return c.code, true
		}
	}
	c := &bcCompiler{i: i, code: &codeObject{name: funcName(def), strict: strict}}
	if c.compileBody(def) {
		return c.code, true
	}
	return nil, false
}

// compileBody compiles the statement list and the implicit trailing `return
// undefined`, reporting success (no fallback aborted the compile).
func (c *bcCompiler) compileBody(def *ast.FuncDef) bool {
	return c.compileStmtList(def.Body.Body)
}

// compileTopLevelBody compiles a module's top-level statement list to a
// slot-mode codeObject, or returns (nil, false) to leave it on the tree-walker.
// A module top level is function-scoped, so it slots exactly like a function
// body — with two guards: it must be slot-eligible (closure-free etc., enforced
// by the compiler's c.fail on nested functions), and no top-level binding may
// shadow a pre-bound free variable (module/exports/require/__filename/__dirname
// in `reserved`), which lives in the env rather than a slot. Name mode is not
// attempted here: a module that declines simply runs on the tree-walker, keeping
// the new path's blast radius to closure-free bodies only.
func (i *Interpreter) compileTopLevelBody(body []ast.Stmt, strict bool, reserved map[string]bool) (*codeObject, bool) {
	if topLevelBindingCollides(body, reserved) {
		return nil, false
	}
	sp := planSlotsBody(body)
	if sp == nil {
		return nil, false
	}
	c := &bcCompiler{i: i, slots: sp.byName, nextSlot: int32(len(sp.slotName)),
		code: &codeObject{name: "<module>", strict: strict, numSlots: len(sp.slotName),
			slotNames: sp.slotName, paramSlots: sp.paramSlot, numParams: sp.numParams}}
	if c.compileStmtList(body) {
		c.code.numSlots = int(c.nextSlot)
		return c.code, true
	}
	return nil, false
}

// topLevelBindingCollides reports whether any module-scope binding shadows a
// reserved free variable. `var` names are function-scoped (collected through
// nested blocks); a top-level let/const is module-scoped too. A let/const in a
// NESTED block that shadows a reserved name is fine — the compiler's lexical
// scope stack resolves it correctly — so only the direct top-level ones count.
func topLevelBindingCollides(body []ast.Stmt, reserved map[string]bool) bool {
	if len(reserved) == 0 {
		return false
	}
	vars := map[string]bool{}
	collectVarNames(body, vars)
	for n := range vars {
		if reserved[n] {
			return true
		}
	}
	for _, s := range body {
		if vd, ok := s.(*ast.VarDecl); ok && vd.Kind != token.VAR {
			for _, d := range vd.Decls {
				if id, ok := d.Target.(*ast.Ident); ok && reserved[id.Name] {
					return true
				}
			}
		}
	}
	return false
}

// compileStmtList compiles a statement list as a body (a function body or a
// module top level), followed by the implicit trailing `return undefined`. The
// list is its own lexical scope: its top-level let/const are hoisted to slots (in
// slot mode) before compiling, so a use-before-init hits the TDZ.
func (c *bcCompiler) compileStmtList(body []ast.Stmt) bool {
	if c.slots != nil {
		c.pushLexScope()
		c.hoistLexicals(body)
	}
	for _, s := range body {
		c.stmt(s)
		if c.err != nil {
			return false
		}
	}
	if c.slots != nil {
		c.popLexScope()
	}
	c.emit(opPushUndef, 0, 0)
	c.emit(opReturn, 0, 0)
	return c.err == nil
}

// treeWalkExpr / treeWalkStmt emit the escape hatch to run a subtree on the
// tree-walker — but in slot mode they abort instead, because a slot local is
// invisible to the tree-walker's name-based resolution.
func (c *bcCompiler) treeWalkExpr(e ast.Expr) {
	if c.slots != nil {
		c.fail()
		return
	}
	c.emit(opEvalNode, c.code.nodeIndex(e), 0)
}

func (c *bcCompiler) treeWalkStmt(s ast.Stmt) {
	if c.slots != nil {
		c.fail()
		return
	}
	c.emit(opEvalStmt, c.code.nodeIndex(s), 0)
}

// localSlot returns (slot, true) if name is a function-scope (param/var) frame
// slot in slot mode. It does NOT see lexical bindings; callers that must handle
// let/const use resolveLocal.
func (c *bcCompiler) localSlot(name string) (int32, bool) {
	if c.slots == nil {
		return 0, false
	}
	s, ok := c.slots[name]
	return int32(s), ok
}

// resolveLocal resolves name to a frame slot in slot mode, searching the lexical
// scopes from innermost out (so an inner let shadows an outer binding or a
// parameter) before the function-scope param/var slots. lexical is true for a
// let/const binding (its reads/writes are TDZ-checked); konst marks a const.
func (c *bcCompiler) resolveLocal(name string) (slot int32, lexical, konst, ok bool) {
	if c.slots == nil {
		return 0, false, false, false
	}
	for j := len(c.lexScopes) - 1; j >= 0; j-- {
		if li, found := c.lexScopes[j][name]; found {
			return li.slot, true, li.konst, true
		}
	}
	if s, found := c.slots[name]; found {
		return int32(s), false, false, true
	}
	return 0, false, false, false
}

// pushLexScope / popLexScope bracket a block, for-head, or function body's
// lexical environment. No runtime opcode is needed on exit — the slots simply
// fall out of the compiler's name resolution (they are not reused).
func (c *bcCompiler) pushLexScope() { c.lexScopes = append(c.lexScopes, map[string]lexInfo{}) }
func (c *bcCompiler) popLexScope()  { c.lexScopes = c.lexScopes[:len(c.lexScopes)-1] }

// allocLexical assigns a fresh frame slot to a lexical binding in the innermost
// scope and records its name for TDZ diagnostics. The caller emits opHoleLocal
// to put the slot in its Temporal Dead Zone.
func (c *bcCompiler) allocLexical(name string, konst bool) int32 {
	slot := c.nextSlot
	c.nextSlot++
	c.code.slotNames = append(c.code.slotNames, name)
	c.lexScopes[len(c.lexScopes)-1][name] = lexInfo{slot: slot, konst: konst}
	return slot
}

// hoistLexicals block-hoists the let/const declarations that appear directly in
// stmts (not in nested blocks): it allocates each a slot in the current lexical
// scope and hole-initializes it, so a use before the initializer runs resolves
// to the (TDZ) binding and throws — matching the tree-walker. A destructuring or
// non-identifier lexical target is not modeled by slots, so it aborts to name
// mode.
func (c *bcCompiler) hoistLexicals(stmts []ast.Stmt) {
	for _, s := range stmts {
		vd, ok := s.(*ast.VarDecl)
		if !ok || vd.Kind == token.VAR {
			continue
		}
		konst := vd.Kind == token.CONST
		for _, decl := range vd.Decls {
			id, ok := decl.Target.(*ast.Ident)
			if !ok {
				c.fail() // destructuring lexical target ⇒ whole-function fallback
				return
			}
			slot := c.allocLexical(id.Name, konst)
			c.emit(opHoleLocal, slot, 0)
		}
	}
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
func (c *bcCompiler) fail()                        { c.err = errBCUnsupported }

// --- statements -------------------------------------------------------------

func (c *bcCompiler) stmt(s ast.Stmt) {
	if c.err != nil {
		return
	}
	// Record the statement's source position so a throw inside it reports an
	// accurate call frame, matching the tree-walker (see eval_stmt.go). One opLine
	// per statement mirrors the tree-walker's per-statement granularity.
	if p := s.Pos(); p.Line > 0 {
		c.emit(opLine, c.code.posIndex(p), 0)
	}
	switch st := s.(type) {
	case *ast.ExprStmt:
		c.expr(st.X)
		c.emit(opPop, 0, 0)
	case *ast.EmptyStmt, *ast.DebuggerStmt:
		// no-op
	case *ast.FuncDecl:
		// In name mode the function object is created by env hoisting (empty
		// completion here). Slot mode skips that hoisting and has no env binding for
		// it, so a nested function declaration aborts slot mode.
		if c.slots != nil {
			c.fail()
		}
	case *ast.VarDecl:
		c.varDecl(st)
	case *ast.BlockStmt:
		// In slot mode a block needs no environment: its let/const bindings are
		// frame slots (hoisted to the TDZ here) and there are no nested function
		// declarations, so there is nothing to put in a child env — skipping
		// opEnterScope also avoids the per-block env allocation.
		if c.slots != nil {
			c.pushLexScope()
			c.hoistLexicals(st.Body)
			for _, sub := range st.Body {
				c.stmt(sub)
			}
			c.popLexScope()
			return
		}
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
		c.treeWalkStmt(s)
	default:
		c.treeWalkStmt(s)
	}
}

func (c *bcCompiler) varDecl(d *ast.VarDecl) {
	// Only simple identifier targets are compiled natively; a destructuring
	// target runs the whole declaration on the tree-walker.
	for _, decl := range d.Decls {
		if _, ok := decl.Target.(*ast.Ident); !ok {
			c.treeWalkStmt(d)
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
			if slot, ok := c.localSlot(name); ok {
				c.emit(opSetLocal, slot, 0)
			} else {
				c.emit(opDeclareVar, c.code.nameIndex(name), 0)
			}
		default: // let / const
			if c.slots != nil {
				// Slot mode: the name was block-hoisted to a hole-initialized slot by
				// hoistLexicals. Evaluate the initializer (or undefined) and store it
				// with a plain opSetLocal — the declaration is what CLEARS the TDZ hole,
				// so it must not be a TDZ-checked write.
				slot, lexical, _, ok := c.resolveLocal(name)
				if !ok || !lexical {
					c.fail() // should not happen: hoistLexicals ran first
					return
				}
				if decl.Init != nil {
					c.exprNamed(decl.Init, name)
				} else {
					c.emit(opPushUndef, 0, 0)
				}
				c.emit(opSetLocal, slot, 0)
				continue
			}
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
	lexHead := false
	if vd, ok := s.Init.(*ast.VarDecl); ok && vd.Kind != token.VAR {
		if c.slots == nil {
			// Name mode: a lexical for-head needs per-iteration environment semantics
			// (CreatePerIterationEnvironment). The tree-walker implements the copy
			// faithfully, so defer the whole statement to it.
			c.treeWalkStmt(s)
			return
		}
		// Slot mode forbids nested closures, so nothing can capture the loop
		// variable — the per-iteration copy is unobservable and one set of slots is
		// correct. Scope the head's let/const to the loop (visible in test, update,
		// and body; gone afterward) and hole-init them before the init runs.
		lexHead = true
		c.pushLexScope()
		c.hoistLexicals([]ast.Stmt{vd})
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
		c.treeWalkStmt(s)
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
	if lexHead {
		c.popLexScope()
	}
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
		if slot, lexical, _, ok := c.resolveLocal(ex.Name); ok {
			if lexical {
				c.emit(opGetLocalTDZ, slot, 0) // ReferenceError if read before init
			} else {
				c.emit(opGetLocal, slot, 0)
			}
			return
		}
		// `arguments` in slot mode would need the (skipped) arguments object; abort.
		if c.slots != nil && ex.Name == "arguments" {
			c.fail()
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
		// x++ / ++x / x-- / --x on a simple identifier: resolve once, read-modify-
		// write through the same reference (opIncDec). Member targets keep the
		// tree-walker (single-evaluation of base/key).
		if id, ok := ex.Operand.(*ast.Ident); ok {
			prefix := int32(0)
			if ex.Prefix {
				prefix = 1
			}
			dec := int32(0)
			if ex.Op == token.DEC {
				dec = 1
			}
			if slot, lexical, konst, ok := c.resolveLocal(id.Name); ok {
				if konst {
					c.fail() // ++/-- on a const ⇒ TypeError; let name mode handle it
					return
				}
				if lexical {
					c.emit(opIncDecLocalTDZ, slot, prefix|dec<<1) // ReferenceError if in TDZ
				} else {
					c.emit(opIncDecLocal, slot, prefix|dec<<1)
				}
				return
			}
			c.emit(opResolveName, c.code.nameIndex(id.Name), 0)
			c.emit(opIncDec, prefix, dec)
			return
		}
		// Member target (obj.x++) needs single-evaluation of base/key via the
		// tree-walker; not available in slot mode.
		if c.slots != nil {
			c.fail()
			return
		}
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
	case *ast.ObjectLit:
		c.objectLit(ex)
	case *ast.TemplateLit:
		c.template(ex)
	case *ast.FuncExpr, *ast.ArrowFunc, *ast.ClassExpr:
		// A nested function/class captures the enclosing environment; a slot-mode
		// function has none of its locals in the env, so it must not create one.
		if c.slots != nil {
			c.fail()
			return
		}
		c.emit(opClosure, c.code.nodeIndex(e), c.code.nameIndex(name))
	case *ast.YieldExpr, *ast.AwaitExpr:
		c.fail() // must not appear in a plain-function body; be safe
	default:
		// Objects, tagged templates, optional chaining, regex, bigint, spread,
		// super, import() — run this subtree on the tree-walker.
		c.treeWalkExpr(e)
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
		c.treeWalkExpr(ex)
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
		c.treeWalkExpr(ex)
		return
	}
	c.expr(ex.Right)
	c.patchTarget(j, c.here())
}

func (c *bcCompiler) unary(ex *ast.UnaryExpr) {
	switch ex.Op {
	case token.TYPEOF:
		if id, ok := ex.Operand.(*ast.Ident); ok && id.Name != "new.target" {
			// typeof reads a slot local's value like any other; only a non-local
			// uses the unresolved-safe opTypeofName. A lexical read still honors the
			// TDZ (typeof of a let/const before init is a ReferenceError, not
			// "undefined"), so it uses the checked opcode.
			if slot, lexical, _, ok := c.resolveLocal(id.Name); ok {
				if lexical {
					c.emit(opGetLocalTDZ, slot, 0)
				} else {
					c.emit(opGetLocal, slot, 0)
				}
				c.emit(opTypeofVal, 0, 0)
				return
			}
			if c.slots != nil && id.Name == "arguments" {
				c.fail()
				return
			}
			c.emit(opTypeofName, c.code.nameIndex(id.Name), 0)
			return
		}
		c.expr(ex.Operand)
		c.emit(opTypeofVal, 0, 0)
	case token.DELETE:
		// delete of a bare identifier consults the environment; a slot local is not
		// there. Member deletes are fine but go through the tree-walker anyway.
		if c.slots != nil {
			c.fail()
			return
		}
		c.emit(opDelete, c.code.nodeIndex(ex), 0)
	case token.VOID:
		c.expr(ex.Operand)
		c.emit(opPop, 0, 0)
		c.emit(opPushUndef, 0, 0)
	case token.MINUS, token.PLUS, token.NOT, token.BIT_NOT:
		c.expr(ex.Operand)
		c.emit(opUnop, int32(ex.Op), 0)
	default:
		c.treeWalkExpr(ex)
	}
}

func (c *bcCompiler) member(ex *ast.MemberExpr) {
	if _, ok := ex.Object.(*ast.SuperExpr); ok {
		c.treeWalkExpr(ex)
		return
	}
	if ex.Optional {
		c.treeWalkExpr(ex)
		return
	}
	if _, ok := ex.Property.(*ast.PrivateIdent); ok {
		c.treeWalkExpr(ex)
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
		c.treeWalkExpr(ex)
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
		c.treeWalkExpr(ex)
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
		c.treeWalkExpr(ex)
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
		c.treeWalkExpr(ex)
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
			c.treeWalkExpr(ex) // holes ⇒ tree-walker
			return
		}
		if _, ok := el.(*ast.SpreadElement); ok {
			c.treeWalkExpr(ex)
			return
		}
	}
	for _, el := range ex.Elements {
		c.expr(el)
	}
	c.emit(opNewArray, int32(len(ex.Elements)), 0)
}

// objectLit compiles an object literal built only from plain data properties
// with static string keys (`{a: e, b, 0: e}`) — the common case, and the
// CommonJS `module.exports = {…}` idiom. Spread, getters/setters, concise
// methods, computed keys, and a colon-form `__proto__` (which sets the
// prototype, not a property) all fall back to the tree-walker, and thus abort
// slot mode — those either need machinery not lowered here or capture the env.
func (c *bcCompiler) objectLit(ex *ast.ObjectLit) {
	for _, p := range ex.Properties {
		if p.Kind != ast.PropInit || p.Computed || p.Method {
			c.treeWalkExpr(ex)
			return
		}
		name, ok := staticKeyName(p.Key)
		if !ok || name == "__proto__" {
			c.treeWalkExpr(ex)
			return
		}
	}
	c.emit(opNewObject, 0, 0)
	for _, p := range ex.Properties {
		name, _ := staticKeyName(p.Key)
		// NamedEvaluation names an anonymous function/class initializer after its
		// key (harmless for other values; such closures abort slot mode anyway).
		c.exprNamed(p.Value, name)
		c.emit(opDefField, c.code.nameIndex(name), 0)
	}
}

// staticKeyName returns the property-key string for a non-computed literal key
// (identifier, string, or numeric), matching evalPropKey's coercion. It reports
// false for a key that is not a compile-time-known string (e.g. a BigInt key).
func staticKeyName(key ast.Expr) (string, bool) {
	switch k := key.(type) {
	case *ast.Ident:
		return k.Name, true
	case *ast.StringLit:
		return k.Value, true
	case *ast.NumberLit:
		return NumberToString(k.Value), true
	default:
		return "", false
	}
}

func (c *bcCompiler) template(ex *ast.TemplateLit) {
	// Evaluate each interpolation, then opTemplate applies ToString to each and
	// interleaves the cooked quasis into a flat string — matching evalTemplate
	// exactly. (A `+`-chain would instead use ToPrimitive(default) and yield a
	// rope, both observably different.)
	for _, sub := range ex.Exprs {
		c.expr(sub)
	}
	c.emit(opTemplate, c.code.nodeIndex(ex), 0)
}

func (c *bcCompiler) assign(ex *ast.AssignExpr, _ string) {
	// Compound assignment to a simple identifier: resolve the target once, read
	// through the same reference, apply the base op, write back (§13.15.2 single
	// reference). Logical assignment (&&= ||= ??=) short-circuits and a member
	// target needs single-evaluation of base/key — both fall back for now.
	if ex.Op != token.ASSIGN {
		if id, ok := ex.Target.(*ast.Ident); ok && !isLogicalAssign(ex.Op) {
			// Slot local: read slot, op, write slot, leave result.
			if slot, lexical, konst, ok := c.resolveLocal(id.Name); ok {
				if konst {
					c.fail() // compound-assign to a const ⇒ TypeError; let name mode handle it
					return
				}
				// A lexical read honors the TDZ; the subsequent write is unchecked
				// because a successful read proves the binding is initialized.
				if lexical {
					c.emit(opGetLocalTDZ, slot, 0)
				} else {
					c.emit(opGetLocal, slot, 0)
				}
				c.expr(ex.Value)
				c.emit(opBinop, int32(compoundBaseOp(ex.Op)), 0)
				c.emit(opDup, 0, 0)
				c.emit(opSetLocal, slot, 0)
				return
			}
			c.emit(opResolveName, c.code.nameIndex(id.Name), 0)
			c.emit(opRefLoad, 0, 0)
			c.expr(ex.Value)
			c.emit(opBinop, int32(compoundBaseOp(ex.Op)), 0)
			c.emit(opPutRef, 0, 0)
			return
		}
		c.treeWalkExpr(ex)
		return
	}
	switch tgt := ex.Target.(type) {
	case *ast.Ident:
		// Resolve the target Reference BEFORE the RHS (§13.15.2): a with-record in
		// the runtime scope chain (not lexically in this function) can have its
		// binding deleted by the RHS, and a resolve-first reference then throws the
		// correct ReferenceError on PutValue. NamedEvaluation applies only to a bare
		// identifier target, not a covered `(x) = fn`, so suppress the name hint when
		// parenthesized.
		inferName := tgt.Name
		if tgt.Parenthesized {
			inferName = ""
		}
		// Slot local: no reference needed — evaluate RHS, dup the result (the
		// assignment's value), store into the slot. A const target aborts to name
		// mode (the RHS still runs there before the TypeError); a let target uses a
		// TDZ-checked write so `x = 1; let x;` throws ReferenceError.
		if slot, lexical, konst, ok := c.resolveLocal(tgt.Name); ok {
			if konst {
				c.fail()
				return
			}
			c.exprNamed(ex.Value, inferName)
			c.emit(opDup, 0, 0)
			if lexical {
				c.emit(opSetLocalTDZ, slot, 0)
			} else {
				c.emit(opSetLocal, slot, 0)
			}
			return
		}
		c.emit(opResolveName, c.code.nameIndex(tgt.Name), 0)
		c.exprNamed(ex.Value, inferName)
		c.emit(opPutRef, 0, 0)
	case *ast.MemberExpr:
		_, super := tgt.Object.(*ast.SuperExpr)
		_, priv := tgt.Property.(*ast.PrivateIdent)
		if super || priv || tgt.Optional {
			c.treeWalkExpr(ex)
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
		c.treeWalkExpr(ex)
	}
}

// --- helpers ----------------------------------------------------------------

func isLogicalAssign(op token.Type) bool {
	return op == token.AND_ASSIGN || op == token.OR_ASSIGN || op == token.NULLISH_ASSIGN
}

func hasSpreadArg(args []ast.Expr) bool {
	for _, a := range args {
		if _, ok := a.(*ast.SpreadElement); ok {
			return true
		}
	}
	return false
}
