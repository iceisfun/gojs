package interp

import "github.com/iceisfun/gojs/ast"

// This file defines the bytecode instruction set and the compiled code object
// consumed by the stack VM (bc_vm.go). The VM is an OPTIONAL execution engine,
// gated by WithBytecode; when it is off, nothing here runs and the tree-walker is
// used exactly as before.
//
// Design: the compiler (bc_compiler.go) lowers the *hot structural* AST — control
// flow, operators, calls, common expressions — to real opcodes, and emits the
// opEvalNode / opEvalStmt escape hatch for any subtree it does not yet compile
// natively. Those escape opcodes re-enter the tree-walker for that one subtree
// using the frame's live *Environment, so a compiled function is ALWAYS correct:
// unimplemented nodes fall back per-subtree rather than failing the whole
// function. Native coverage can then grow one node type at a time, each change
// validated differentially against the tree-walker.
//
// v1 keeps scopes name-based (the same *Environment operations the tree-walker
// uses), so every scope-touching opcode reuses a proven interp helper. Slot-based
// locals are a later pass (bc_resolver.go) layered on top without changing this
// instruction set's observable behavior.

// bcOp is a single bytecode operation.
type bcOp uint8

const (
	opNop bcOp = iota

	// Stack shuffling.
	opPop // discard top of stack
	opDup // duplicate top of stack

	// Constants / nullary loads.
	opPushConst     // a=const index → push consts[a]
	opPushUndef     //
	opPushNull      //
	opPushTrue      //
	opPushFalse     //
	opPushThis      // GetThisBinding(env)
	opPushNewTarget // env.newTarget()

	// Identifier reads/writes (name-based; reuse resolveIdent / assignIdent).
	opLoadName   // a=name index → push resolveIdent(name)
	opStoreName  // a=name index → assignIdent(name, pop) (leaves nothing)
	opTypeofName // a=name index → typeof, but undefined-safe for an unresolved name

	// Operators.
	opBinop     // a=token.Type → push applyBinary(op, a:=pop2, b:=pop1)
	opUnop      // a=token.Type → push unary(op, pop) for - + ! ~ void
	opTypeofVal // typeof of a value already on the stack
	opInstOf    // push evalInstanceof(left:=pop2, right:=pop1)

	// Property access (static string key in names[a], or computed on stack).
	opGetProp // a=name index → push getProperty(base:=pop, key)
	opGetElem // pop key, base → push getProperty(base, ToPropertyKey(key))
	opSetProp // a=name index → PutValue(base:=pop2 . key = value:=pop1); leaves value
	opSetElem // pop value, key, base → PutValue; leaves value

	// Control flow (targets are instruction indices).
	opJump          // a=target
	opJumpIfFalse   // a=target; pop, jump when falsy
	opJumpIfTrue    // a=target; pop, jump when truthy
	opJumpIfFalseKp // a=target; peek, jump when falsy, else pop (&&)
	opJumpIfTrueKp  // a=target; peek, jump when truthy, else pop (|| / ??)
	opJumpIfNotNull // a=target; peek, jump when NOT null/undefined, else pop (??)

	// Calls / construction.
	opCall   // a=argc; stack: fn, this, arg0..argN-1 → push result
	opMethod // a=name index; [base] → [fn=base.name, this=base] (fetch before args)
	opNew    // a=argc; stack: ctor, arg0.. → push result

	// Loops (runtime loop stack for break/continue + scope/stack restoration).
	opEnterLoop // a=breakIP, b=contIP → push a loop record
	opExitLoop  // pop the loop record
	opBreak     // jump to innermost loop's breakIP (restoring env + stack)
	opContinue  // jump to innermost loop's contIP (restoring env + stack)

	// Function completion.
	opReturn // pop → return value
	opThrow  // pop → *Throw

	// Aggregate literals.
	opNewArray // a=count; pop count values (in order) → push array

	// Declarations (simple identifier targets only; patterns fall back).
	opDeclareVar // a=name index; pop value → var-scope assign (declareVarBinding path)
	opDeclareLex // a=name index, b=mutable(0/1); pop value → init lexical binding

	// Scopes.
	opEnterScope // a=node index (*ast.BlockStmt) → push child env + hoist its decls
	opExitScope  // restore parent env

	// Closures / templates.
	opClosure  // a=node index (FuncExpr/ArrowFunc/ClassExpr), b=name index → push function object
	opTemplate // a=count → concatenate 2*count-1 alternating quasi/expr parts on stack

	// Escape hatches: run one AST subtree on the tree-walker with the live env.
	opEvalNode // a=node index (ast.Expr) → push evalExpr(node, env)
	opEvalStmt // a=node index (ast.Stmt) → evalStmt(node, env); completion handled by VM
	opUpdate   // a=node index (*ast.UpdateExpr) → push evalUpdate(node, env)
	opDelete   // a=node index (*ast.UnaryExpr delete) → push evalDelete(operand, env)
)

// bcInstr is one decoded instruction. Operands are int32 so a codeObject is a
// flat, contiguous slice — cache-friendly, unlike the AST pointer graph. (Byte
// packing is a later optimization; the instruction stream's semantics do not
// depend on the physical encoding.)
type bcInstr struct {
	op bcOp
	a  int32
	b  int32
}

// codeObject is the compiled form of one function body (or, later, a script).
type codeObject struct {
	name   string
	instrs []bcInstr

	// Pools referenced by instruction operands.
	consts []Value     // opPushConst
	names  []string    // opLoadName/opStoreName/opGetProp/... and declarations
	nodes  []ast.Node  // opEvalNode/opEvalStmt/opClosure/opEnterScope/opUpdate/opDelete

	strict bool
}

// constIndex interns a constant value into the pool, returning its index. Small
// value pools with pointer-free primitives are cheap to scan; the compiler only
// calls this for literal nodes, so pools stay small.
func (c *codeObject) constIndex(v Value) int32 {
	c.consts = append(c.consts, v)
	return int32(len(c.consts) - 1)
}

// nameIndex interns a name (identifier or static property key), deduplicating so
// repeated references to the same name share one pool slot.
func (c *codeObject) nameIndex(name string) int32 {
	for i, n := range c.names {
		if n == name {
			return int32(i)
		}
	}
	c.names = append(c.names, name)
	return int32(len(c.names) - 1)
}

// nodeIndex stores an AST node for an escape-hatch / deferred opcode.
func (c *codeObject) nodeIndex(n ast.Node) int32 {
	c.nodes = append(c.nodes, n)
	return int32(len(c.nodes) - 1)
}

// emit appends an instruction and returns its index (for later jump patching).
func (c *codeObject) emit(op bcOp, a, b int32) int {
	c.instrs = append(c.instrs, bcInstr{op: op, a: a, b: b})
	return len(c.instrs) - 1
}
