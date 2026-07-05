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
		// If finally completes abruptly, it replaces the pending completion
		// (§14.15.8 step 3), and UpdateEmpty(F, undefined) still applies.
		if finErr != nil {
			return orUndef(finResult), finErr
		}
	}
	// TryStatement completion is UpdateEmpty(C, undefined) (§14.15.8), applied to
	// normal AND abrupt completions: an empty completion value (an empty try/
	// catch block, or an empty-valued break/continue out of it) surfaces as
	// undefined; a non-empty accumulated value is preserved.
	return orUndef(result), err
}

// evalCatch runs a catch clause, binding the caught value to its parameter.
func (i *Interpreter) evalCatch(ctx context.Context, handler *ast.CatchClause, caught Value, env *Environment) (Value, error) {
	// CatchClauseEvaluation (§14.15.3): the catch parameter is bound in its own
	// declarative environment (catchEnv), and its BindingInitialization — which
	// may run default-value initializers that create closures — happens there.
	catchEnv := NewEnvironment(env, false)
	if handler.Param != nil {
		bind := func(name string, v Value) {
			catchEnv.bind(name, &binding{value: v, mutable: true, initialized: true})
		}
		if err := i.bindPattern(ctx, handler.Param, caught, catchEnv, bind); err != nil {
			return nil, err
		}
	}
	// The Block then evaluates in a *fresh* environment nested inside catchEnv
	// (Block : { StatementList }), so a let/const in the body is neither observed
	// by nor colliding with the catch parameter's initializer closures.
	blockEnv := NewEnvironment(catchEnv, false)
	if err := i.hoistDeclarations(ctx, handler.Body.Body, blockEnv, false); err != nil {
		return nil, err
	}
	return i.execStmts(ctx, handler.Body.Body, blockEnv)
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
		if err := i.hoistDeclarations(ctx, c.Body, scope, false); err != nil {
			return nil, err
		}
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

	// Track the switch's completion value V per CaseBlockEvaluation +
	// UpdateEmpty: V accumulates the completion value of each executed clause,
	// but an empty completion (an empty clause body, a break/continue, a bare
	// declaration) never replaces a previous non-empty value.
	var v Value // nil == empty
	for idx := matched; idx < len(s.Cases); idx++ {
		cv, err := i.execCaseBody(ctx, s.Cases[idx].Body, scope)
		if cv != nil {
			v = cv
		}
		if err != nil {
			// UpdateEmpty(R, V): an unlabeled break completes the switch with V.
			if b, ok := err.(*breakSignal); ok && b.label == "" {
				return orUndef(v), nil
			}
			// Other abrupt completions (continue, labeled break, return, throw)
			// propagate outward carrying V as the completion value.
			return orUndef(v), err
		}
	}
	return orUndef(v), nil
}

// execCaseBody runs a case clause's StatementList, returning the completion
// value per StatementListEvaluation + UpdateEmpty. The returned value is nil
// when the list's completion is empty (an empty list, or a list of only
// empty-completion statements such as declarations, break/continue, or empty
// statements). On an abrupt completion the accumulated value is returned
// alongside the control-flow error.
func (i *Interpreter) execCaseBody(ctx context.Context, stmts []ast.Stmt, env *Environment) (Value, error) {
	var v Value // nil == empty
	for _, s := range stmts {
		// A nested block is its own lexical scope; recurse so its completion
		// emptiness is tracked correctly rather than collapsing to undefined.
		if b, ok := s.(*ast.BlockStmt); ok {
			scope := NewEnvironment(env, false)
			if err := i.hoistDeclarations(ctx, b.Body, scope, false); err != nil {
				return v, err
			}
			cv, err := i.execCaseBody(ctx, b.Body, scope)
			if cv != nil {
				v = cv
			}
			if err != nil {
				return v, err
			}
			continue
		}
		cv, err := i.evalStmt(ctx, s, env)
		if err != nil {
			// return/throw carry their own value in the signal; break/continue
			// return a nil (empty) value here. Preserve any non-empty value.
			if cv != nil {
				v = cv
			}
			return v, err
		}
		// Declarations and empty/debugger statements are empty completions and
		// must not overwrite a previous non-empty value.
		if cv != nil && !stmtIsEmptyCompletion(s) {
			v = cv
		}
	}
	return v, nil
}

// stmtIsEmptyCompletion reports whether a statement's normal completion is
// always empty (produces no value), per the spec's StatementList semantics.
func stmtIsEmptyCompletion(s ast.Stmt) bool {
	switch s.(type) {
	case *ast.VarDecl, *ast.FuncDecl, *ast.ClassDecl, *ast.EmptyStmt, *ast.DebuggerStmt:
		return true
	}
	return false
}

// orUndef maps an empty (nil) completion value to undefined.
func orUndef(v Value) Value {
	if v == nil {
		return Undef
	}
	return v
}
