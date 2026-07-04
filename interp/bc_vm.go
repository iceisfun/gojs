package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file is the stack VM that executes a codeObject (bc_compiler.go). It is an
// optional engine gated by WithBytecode; disabling it changes nothing. Every
// scope-, property-, and call-touching opcode delegates to the same interp helper
// the tree-walker uses (resolveIdent/assignIdent, getRefValue/putRefValue,
// applyBinary, i.call, ToBoolean, strictEquals via applyBinary), so the VM's
// observable behavior matches the tree-walker exactly. v1 keeps scopes name-based;
// slot-based locals are a later layer that will not change these semantics.

// bcFrame is one activation of a compiled function on the VM.
type bcFrame struct {
	code  *codeObject
	ip    int
	stack []Value
	env   *Environment
	loops []bcLoopRT
}

// bcLoopRT records a live loop so break/continue (native, or an unlabeled signal
// escaping a fallback subtree) can restore the environment and operand-stack
// depth before jumping — the flat instruction stream can't rely on the Go stack
// for scope cleanup the way the tree-walker does.
type bcLoopRT struct {
	breakIP  int
	contIP   int
	env      *Environment
	stackLen int
}

func (f *bcFrame) push(v Value) { f.stack = append(f.stack, v) }

func (f *bcFrame) pop() Value {
	n := len(f.stack) - 1
	v := f.stack[n]
	f.stack = f.stack[:n]
	return v
}

// popN removes the top n values and returns them in source (bottom-to-top) order,
// i.e. args[0] is the deepest of the n.
func (f *bcFrame) popN(n int) []Value {
	if n == 0 {
		return nil
	}
	base := len(f.stack) - n
	out := make([]Value, n)
	copy(out, f.stack[base:])
	f.stack = f.stack[:base]
	return out
}

// execCode runs a compiled function body in env and returns its completion value.
// Control flow (return/throw) surfaces exactly as in the tree-walker: a *Throw or
// context error propagates; the value of a `return` is returned directly.
func (i *Interpreter) execCode(ctx context.Context, code *codeObject, env *Environment) (Value, error) {
	fr := &bcFrame{code: code, env: env, stack: make([]Value, 0, 8)}
	instrs := code.instrs
	for fr.ip < len(instrs) {
		if err := i.checkContext(); err != nil {
			return nil, err
		}
		if err := i.step(); err != nil {
			return nil, err
		}
		in := instrs[fr.ip]
		fr.ip++
		switch in.op {
		case opNop:
		case opPop:
			fr.pop()
		case opDup:
			fr.push(fr.stack[len(fr.stack)-1])

		case opPushConst:
			fr.push(code.consts[in.a])
		case opPushUndef:
			fr.push(Undef)
		case opPushNull:
			fr.push(Nul)
		case opPushTrue:
			fr.push(True)
		case opPushFalse:
			fr.push(False)
		case opPushThis:
			v, err := i.getThisBinding(ctx, fr.env)
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opPushNewTarget:
			fr.push(fr.env.newTarget())

		case opLoadName:
			v, err := i.resolveIdent(ctx, code.names[in.a], fr.env)
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opStoreName:
			v := fr.pop()
			if err := i.assignIdent(ctx, code.names[in.a], v, fr.env); err != nil {
				return nil, err
			}
		case opTypeofName:
			v, err := i.typeofName(ctx, code.names[in.a], fr.env)
			if err != nil {
				return nil, err
			}
			fr.push(v)

		case opBinop:
			b := fr.pop()
			a := fr.pop()
			v, err := i.applyBinary(ctx, token.Type(in.a), a, b)
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opUnop:
			v, err := i.applyUnaryValue(ctx, token.Type(in.a), fr.pop())
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opTypeofVal:
			fr.push(String(fr.pop().Typeof()))
		case opInstOf:
			r := fr.pop()
			l := fr.pop()
			v, err := i.evalInstanceof(ctx, l, r)
			if err != nil {
				return nil, err
			}
			fr.push(v)

		case opGetProp:
			base := fr.pop()
			v, err := i.getRefValue(ctx, &reference{kind: refProp, strict: fr.env.isStrict(), base: base, key: StrKey(code.names[in.a]), keyDone: true})
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opGetElem:
			keyVal := fr.pop()
			base := fr.pop()
			v, err := i.getRefValue(ctx, &reference{kind: refProp, strict: fr.env.isStrict(), base: base, keyVal: keyVal})
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opSetProp:
			value := fr.pop()
			base := fr.pop()
			if err := i.putRefValue(ctx, &reference{kind: refProp, strict: fr.env.isStrict(), base: base, key: StrKey(code.names[in.a]), keyDone: true}, value); err != nil {
				return nil, err
			}
			fr.push(value)
		case opSetElem:
			value := fr.pop()
			keyVal := fr.pop()
			base := fr.pop()
			if err := i.putRefValue(ctx, &reference{kind: refProp, strict: fr.env.isStrict(), base: base, keyVal: keyVal}, value); err != nil {
				return nil, err
			}
			fr.push(value)

		case opJump:
			fr.ip = int(in.a)
		case opJumpIfFalse:
			if !ToBoolean(fr.pop()) {
				fr.ip = int(in.a)
			}
		case opJumpIfTrue:
			if ToBoolean(fr.pop()) {
				fr.ip = int(in.a)
			}
		case opJumpIfFalseKp:
			if !ToBoolean(fr.stack[len(fr.stack)-1]) {
				fr.ip = int(in.a)
			} else {
				fr.pop()
			}
		case opJumpIfTrueKp:
			if ToBoolean(fr.stack[len(fr.stack)-1]) {
				fr.ip = int(in.a)
			} else {
				fr.pop()
			}
		case opJumpIfNotNull:
			if !IsNullish(fr.stack[len(fr.stack)-1]) {
				fr.ip = int(in.a)
			} else {
				fr.pop()
			}

		case opCall:
			args := fr.popN(int(in.a))
			this := fr.pop()
			fn := fr.pop()
			v, err := i.call(ctx, fn, this, args)
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opCallMethod:
			args := fr.popN(int(in.b))
			base := fr.pop()
			fn, err := i.getRefValue(ctx, &reference{kind: refProp, strict: fr.env.isStrict(), base: base, key: StrKey(code.names[in.a]), keyDone: true})
			if err != nil {
				return nil, err
			}
			v, err := i.call(ctx, fn, base, args)
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opNew:
			args := fr.popN(int(in.a))
			callee := fr.pop()
			ctor, ok := callee.(*Object)
			if !ok || !ctor.IsConstructor() {
				return nil, i.throwError(ctx, "TypeError", briefValue(callee)+" is not a constructor")
			}
			v, err := ctor.fn.construct(ctx, ctor, args)
			if err != nil {
				return nil, err
			}
			fr.push(v)

		case opReturn:
			return fr.pop(), nil
		case opThrow:
			return nil, NewThrow(fr.pop())

		case opNewArray:
			fr.push(i.newArray(fr.popN(int(in.a))))

		case opDeclareVar:
			v := fr.pop()
			if err := i.assignIdent(ctx, code.names[in.a], v, fr.env); err != nil {
				return nil, err
			}
		case opDeclareLex:
			v := fr.pop()
			name := code.names[in.a]
			if b, ok := fr.env.vars[name]; ok {
				b.value = v
				b.initialized = true
			} else {
				fr.env.vars[name] = &binding{value: v, mutable: in.b == 1, initialized: true}
			}

		case opEnterScope:
			block := code.nodes[in.a].(*ast.BlockStmt)
			scope := NewEnvironment(fr.env, false)
			if err := i.hoistDeclarations(ctx, block.Body, scope, false); err != nil {
				return nil, err
			}
			fr.env = scope
		case opExitScope:
			fr.env = fr.env.parent

		case opEnterLoop:
			fr.loops = append(fr.loops, bcLoopRT{breakIP: int(in.a), contIP: int(in.b), env: fr.env, stackLen: len(fr.stack)})
		case opExitLoop:
			fr.loops = fr.loops[:len(fr.loops)-1]
		case opBreak:
			lp := fr.loops[len(fr.loops)-1]
			fr.env = lp.env
			fr.stack = fr.stack[:lp.stackLen]
			fr.ip = lp.breakIP
		case opContinue:
			lp := fr.loops[len(fr.loops)-1]
			fr.env = lp.env
			fr.stack = fr.stack[:lp.stackLen]
			fr.ip = lp.contIP

		case opClosure:
			node := code.nodes[in.a].(ast.Expr)
			v, err := i.evalExprNamed(ctx, node, fr.env, code.names[in.b])
			if err != nil {
				return nil, err
			}
			fr.push(v)

		case opEvalNode:
			v, err := i.evalExpr(ctx, code.nodes[in.a].(ast.Expr), fr.env)
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opEvalStmt:
			_, err := i.evalStmt(ctx, code.nodes[in.a].(ast.Stmt), fr.env)
			if err != nil {
				if ret, ok := err.(*returnSignal); ok {
					return ret.value, nil
				}
				if handled := i.bcLoopSignal(fr, err); handled {
					continue
				}
				return nil, err
			}
		case opUpdate:
			v, err := i.evalUpdate(ctx, code.nodes[in.a].(*ast.UpdateExpr), fr.env)
			if err != nil {
				return nil, err
			}
			fr.push(v)
		case opDelete:
			node := code.nodes[in.a].(*ast.UnaryExpr)
			v, err := i.evalDelete(ctx, node.Operand, fr.env)
			if err != nil {
				return nil, err
			}
			fr.push(v)

		default:
			return nil, i.throwError(ctx, "SyntaxError", "bytecode: bad opcode")
		}
	}
	return Undef, nil
}

// bcLoopSignal handles an unlabeled break/continue that escaped a fallback
// (opEvalStmt) subtree, targeting the nearest enclosing compiled loop. It mirrors
// opBreak/opContinue. Returns false (leaving err to propagate) for a labeled
// signal or when there is no enclosing loop — neither can occur in a function the
// compiler accepted, but propagating is the safe default.
func (i *Interpreter) bcLoopSignal(fr *bcFrame, err error) bool {
	if len(fr.loops) == 0 {
		return false
	}
	lp := fr.loops[len(fr.loops)-1]
	switch s := err.(type) {
	case *breakSignal:
		if s.label != "" {
			return false
		}
		fr.env = lp.env
		fr.stack = fr.stack[:lp.stackLen]
		fr.ip = lp.breakIP
		return true
	case *continueSignal:
		if s.label != "" {
			return false
		}
		fr.env = lp.env
		fr.stack = fr.stack[:lp.stackLen]
		fr.ip = lp.contIP
		return true
	}
	return false
}

// typeofName implements typeof on a bare identifier, yielding "undefined" for an
// unresolved name rather than throwing (mirrors evalTypeof's identifier path).
func (i *Interpreter) typeofName(ctx context.Context, name string, env *Environment) (Value, error) {
	bound := false
	for e := env; e != nil && !bound; e = e.parent {
		if e.withObj != nil {
			if _, ok, err := i.withHasBinding(ctx, e.withObj, name); err != nil {
				return nil, err
			} else if ok {
				bound = true
			}
		}
		if _, ok := e.vars[name]; ok {
			bound = true
		}
	}
	if !bound && !i.global.Has(StrKey(name)) && name != "undefined" {
		return String("undefined"), nil
	}
	v, err := i.resolveIdent(ctx, name, env)
	if err != nil {
		return nil, err
	}
	return String(v.Typeof()), nil
}
