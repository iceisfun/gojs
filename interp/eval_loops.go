package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// This file implements loop evaluation with correct labeled break/continue
// semantics. Each loop is passed the label (if any) of an enclosing labeled
// statement so that `break label` / `continue label` target it.

// loopControl classifies how a control-flow signal affects the current loop.
type loopControl int

const (
	loopContinue  loopControl = iota // continue this loop's next iteration
	loopBreak                        // stop this loop
	loopPropagate                    // signal targets an outer construct
	loopNormal                       // no signal
)

// classifyLoopSignal decides what a signal means for a loop carrying label.
func classifyLoopSignal(err error, label string) loopControl {
	switch e := err.(type) {
	case nil:
		return loopNormal
	case *breakSignal:
		if e.label == "" || e.label == label {
			return loopBreak
		}
		return loopPropagate
	case *continueSignal:
		if e.label == "" || e.label == label {
			return loopContinue
		}
		return loopPropagate
	default:
		return loopPropagate
	}
}

// evalWhile evaluates a while loop.
func (i *Interpreter) evalWhile(ctx context.Context, s *ast.WhileStmt, env *Environment) (Value, error) {
	return i.runWhile(ctx, s, env, "")
}

func (i *Interpreter) runWhile(ctx context.Context, s *ast.WhileStmt, env *Environment, label string) (Value, error) {
	var completion Value = Undef
	for {
		test, err := i.evalExpr(ctx, s.Test, env)
		if err != nil {
			return nil, err
		}
		if !ToBoolean(test) {
			return completion, nil
		}
		bodyVal, err := i.evalStmt(ctx, s.Body, env)
		if bodyVal != nil {
			completion = bodyVal
		}
		switch classifyLoopSignal(err, label) {
		case loopBreak:
			return completion, nil
		case loopContinue, loopNormal:
			continue
		default:
			return completion, err
		}
	}
}

// evalDoWhile evaluates a do/while loop.
func (i *Interpreter) evalDoWhile(ctx context.Context, s *ast.DoWhileStmt, env *Environment) (Value, error) {
	return i.runDoWhile(ctx, s, env, "")
}

func (i *Interpreter) runDoWhile(ctx context.Context, s *ast.DoWhileStmt, env *Environment, label string) (Value, error) {
	var completion Value = Undef
	for {
		bodyVal, err := i.evalStmt(ctx, s.Body, env)
		if bodyVal != nil {
			completion = bodyVal
		}
		switch classifyLoopSignal(err, label) {
		case loopBreak:
			return completion, nil
		case loopContinue, loopNormal:
			// fall through to the test
		default:
			return completion, err
		}
		test, err := i.evalExpr(ctx, s.Test, env)
		if err != nil {
			return nil, err
		}
		if !ToBoolean(test) {
			return completion, nil
		}
	}
}

// evalFor evaluates a C-style for loop.
func (i *Interpreter) evalFor(ctx context.Context, s *ast.ForStmt, env *Environment) (Value, error) {
	return i.runFor(ctx, s, env, "")
}

func (i *Interpreter) runFor(ctx context.Context, s *ast.ForStmt, env *Environment, label string) (Value, error) {
	// The loop header gets its own scope so `let` bindings are per-loop.
	loopEnv := NewEnvironment(env, false)
	if s.Init != nil {
		switch init := s.Init.(type) {
		case *ast.VarDecl:
			if err := i.hoistDeclarations(ctx, []ast.Stmt{init}, loopEnv, false); err != nil {
				return nil, err
			}
			if err := i.evalVarDecl(ctx, init, loopEnv); err != nil {
				return nil, err
			}
		case ast.Expr:
			if _, err := i.evalExpr(ctx, init, loopEnv); err != nil {
				return nil, err
			}
		}
	}
	var completion Value = Undef
	for {
		if s.Test != nil {
			test, err := i.evalExpr(ctx, s.Test, loopEnv)
			if err != nil {
				return nil, err
			}
			if !ToBoolean(test) {
				return completion, nil
			}
		}
		// Each iteration runs in a copy so closures capture per-iteration lets.
		iterEnv := i.copyLoopScope(loopEnv, env)
		bodyVal, err := i.evalStmt(ctx, s.Body, iterEnv)
		if bodyVal != nil {
			completion = bodyVal
		}
		switch classifyLoopSignal(err, label) {
		case loopBreak:
			return completion, nil
		case loopContinue, loopNormal:
			// proceed to update
		default:
			return completion, err
		}
		i.writeBackLoopScope(loopEnv, iterEnv)
		if s.Update != nil {
			if _, err := i.evalExpr(ctx, s.Update, loopEnv); err != nil {
				return nil, err
			}
		}
	}
}

// copyLoopScope creates a per-iteration environment seeded with the current
// loop-variable values, so closures created in the body capture distinct
// bindings per iteration (matching `let` semantics in for loops).
func (i *Interpreter) copyLoopScope(loopEnv, parent *Environment) *Environment {
	iter := NewEnvironment(parent, false)
	for name, b := range loopEnv.vars {
		iter.vars[name] = &binding{value: b.value, mutable: b.mutable, initialized: b.initialized}
	}
	return iter
}

// writeBackLoopScope copies iteration-local variable values back so the update
// clause and next test see the latest values.
func (i *Interpreter) writeBackLoopScope(loopEnv, iterEnv *Environment) {
	for name, b := range loopEnv.vars {
		if ib, ok := iterEnv.vars[name]; ok {
			b.value = ib.value
		}
	}
}

// evalForIn evaluates for-in and for-of loops.
func (i *Interpreter) evalForIn(ctx context.Context, s *ast.ForInStmt, env *Environment) (Value, error) {
	return i.runForIn(ctx, s, env, "")
}

func (i *Interpreter) runForIn(ctx context.Context, s *ast.ForInStmt, env *Environment, label string) (Value, error) {
	rhs, err := i.evalExpr(ctx, s.Right, env)
	if err != nil {
		return nil, err
	}

	// completion accumulates the loop's completion value: the last non-empty
	// body completion value (UpdateEmpty semantics), preserved across break.
	var completion Value = Undef

	// runBody binds one value to the loop target and evaluates the body,
	// returning the classified loop signal alongside the raw error.
	runBody := func(item Value) (loopControl, error) {
		iterEnv := NewEnvironment(env, false)
		if err := i.bindForTarget(ctx, s.Left, item, iterEnv, env); err != nil {
			return loopPropagate, err
		}
		bodyVal, err := i.evalStmt(ctx, s.Body, iterEnv)
		if bodyVal != nil {
			completion = bodyVal
		}
		return classifyLoopSignal(err, label), err
	}

	if s.Of {
		// for-of drives the iteration protocol directly so that it can close the
		// iterator (call its return method) on any abrupt completion — break, a
		// signal targeting an enclosing construct, a throw, or a target-binding
		// error (ECMA-262 14.7.5.7, ForIn/OfBodyEvaluation + IteratorClose).
		step, closeIter, err := i.patternIterator(ctx, rhs)
		if err != nil {
			return nil, err
		}
		for {
			v, done, err := step()
			if err != nil {
				// A throwing IteratorStep leaves the iterator done; do not close.
				return completion, err
			}
			if done {
				return completion, nil
			}
			sig, bodyErr := runBody(v)
			switch sig {
			case loopContinue, loopNormal:
				continue
			case loopBreak:
				// Normal break: close, surfacing any error from return().
				if e := closeIter(); e != nil {
					return completion, e
				}
				return completion, nil
			default:
				// Throw, or a break/continue targeting an outer loop: close the
				// iterator but let the original abrupt completion take precedence.
				_ = closeIter()
				return completion, bodyErr
			}
		}
	}

	// for-in enumerates own+inherited enumerable string keys.
	if IsNullish(rhs) {
		return Undef, nil
	}
	obj, err := i.ToObject(ctx, rhs)
	if err != nil {
		return nil, err
	}
	// Keys are collected once up front, but a property deleted before it is
	// visited must not be visited, so existence is re-checked at each step.
	keys := i.enumerateKeys(obj)
	for _, k := range keys {
		if !i.stillEnumerable(ctx, obj, k) {
			continue
		}
		sig, bodyErr := runBody(String(k))
		switch sig {
		case loopContinue, loopNormal:
			continue
		case loopBreak:
			return completion, nil
		default:
			return completion, bodyErr
		}
	}
	return completion, nil
}

// stillEnumerable reports whether name is still an enumerable property somewhere
// on obj's prototype chain, so a for-in enumeration skips properties deleted
// (or made non-enumerable) before they are visited.
func (i *Interpreter) stillEnumerable(ctx context.Context, obj *Object, name string) bool {
	for cur := obj; cur != nil; cur = cur.proto {
		if p, ok := cur.getOwn(StrKey(name)); ok {
			return p.Enumerable
		}
	}
	return false
}

// bindForTarget binds a for-in/of loop value to the loop's left-hand side,
// which is either a declaration or an existing assignment target.
func (i *Interpreter) bindForTarget(ctx context.Context, left ast.Node, v Value, iterEnv, assignEnv *Environment) error {
	switch l := left.(type) {
	case *ast.VarDecl:
		target := l.Decls[0].Target
		bind := func(name string, val Value) {
			iterEnv.vars[name] = &binding{value: val, mutable: true, initialized: true}
		}
		return i.bindPattern(ctx, target, v, iterEnv, bind)
	case ast.Expr:
		return i.assignTo(ctx, l, v, assignEnv)
	default:
		return i.throwError(ctx, "SyntaxError", "invalid for-loop binding")
	}
}

// enumerateKeys returns the enumerable string keys of obj and its prototype
// chain, de-duplicated, in insertion order (for for-in).
func (i *Interpreter) enumerateKeys(obj *Object) []string {
	seen := map[string]bool{}
	var out []string
	for cur := obj; cur != nil; cur = cur.proto {
		for _, name := range cur.OwnKeys() {
			if seen[name] {
				continue
			}
			seen[name] = true
			if p, ok := cur.getOwn(StrKey(name)); ok && p.Enumerable {
				out = append(out, name)
			}
		}
	}
	return out
}

// errStopIteration is a sentinel used to break out of the iterate helper.
var errStopIteration = &sentinelError{"stop iteration"}

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }
