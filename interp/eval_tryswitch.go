package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// evalTry evaluates a try/catch/finally statement. A finally block runs on every
// exit path and can override the pending completion (e.g. a return inside
// finally supersedes an exception from the try block).
func (i *Interpreter) evalTry(ctx context.Context, s *ast.TryStmt, env *Environment) (Value, error) {
	result, err := i.evalBlock(ctx, s.Block, env)

	// A thrown JS exception (not a control-flow signal) may be caught.
	if err != nil && s.Handler != nil {
		if thr, ok := err.(*Throw); ok {
			result, err = i.evalCatch(ctx, s.Handler, thr.Value, env)
		}
	}

	if s.Finalizer != nil {
		finResult, finErr := i.evalBlock(ctx, s.Finalizer, env)
		// If finally completes abruptly, it wins.
		if finErr != nil {
			return finResult, finErr
		}
	}
	return result, err
}

// evalCatch runs a catch clause, binding the caught value to its parameter.
func (i *Interpreter) evalCatch(ctx context.Context, handler *ast.CatchClause, caught Value, env *Environment) (Value, error) {
	scope := NewEnvironment(env, false)
	if handler.Param != nil {
		bind := func(name string, v Value) {
			scope.vars[name] = &binding{value: v, mutable: true, initialized: true}
		}
		if err := i.bindPattern(ctx, handler.Param, caught, scope, bind); err != nil {
			return nil, err
		}
	}
	i.hoistDeclarations(ctx, handler.Body.Body, scope, false)
	return i.execStmts(ctx, handler.Body.Body, scope)
}

// evalSwitch evaluates a switch statement. Matching uses strict equality; once a
// clause matches, execution falls through subsequent clauses until a break.
func (i *Interpreter) evalSwitch(ctx context.Context, s *ast.SwitchStmt, env *Environment) (Value, error) {
	disc, err := i.evalExpr(ctx, s.Discriminant, env)
	if err != nil {
		return nil, err
	}
	scope := NewEnvironment(env, false)
	// Hoist lexical declarations from all case bodies into the switch scope.
	for _, c := range s.Cases {
		i.hoistDeclarations(ctx, c.Body, scope, false)
	}

	matched := -1
	for idx, c := range s.Cases {
		if c.Test == nil {
			continue // handle default after searching the case labels
		}
		test, err := i.evalExpr(ctx, c.Test, scope)
		if err != nil {
			return nil, err
		}
		if strictEquals(disc, test) {
			matched = idx
			break
		}
	}
	if matched == -1 {
		for idx, c := range s.Cases {
			if c.Test == nil {
				matched = idx
				break
			}
		}
	}
	if matched == -1 {
		return Undef, nil
	}

	for idx := matched; idx < len(s.Cases); idx++ {
		_, err := i.execStmts(ctx, s.Cases[idx].Body, scope)
		if err != nil {
			if b, ok := err.(*breakSignal); ok && b.label == "" {
				return Undef, nil
			}
			return nil, err
		}
	}
	return Undef, nil
}
