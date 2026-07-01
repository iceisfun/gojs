package interp

import (
	"context"

	"github.com/iceisfun/gojs/parser"
)

// evalSource implements the global eval function: it parses a string of source
// text and runs it in the global scope (indirect-eval semantics), returning the
// completion value. Non-string arguments are returned unchanged, per spec.
//
// This is "indirect" eval — it does not capture the caller's local scope. That
// covers the overwhelming majority of real-world and Test262 uses (which eval
// at global scope); true direct eval with lexical access to the calling
// function's locals is intentionally not implemented.
//
// When Security.DisableEval is set, eval throws instead of executing — an
// explicit, observable refusal for locked-down embeddings.
func (i *Interpreter) evalSource(ctx context.Context, code Value) (Value, error) {
	str, ok := code.(String)
	if !ok {
		return code, nil
	}
	if i.security.DisableEval {
		return nil, i.throwError(ctx, "EvalError", "eval is disabled in this sandbox")
	}

	prog, err := parser.Parse("<eval>", string(str))
	if err != nil {
		// A parse failure in eval surfaces as a SyntaxError thrown value.
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	env := i.globalEnv
	if err := i.hoistDeclarations(ctx, prog.Body, env, true); err != nil {
		return nil, err
	}
	result, err := i.execStmts(ctx, prog.Body, env)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// directEval implements a direct call to eval (the callee is the identifier
// `eval` resolving to the %eval% intrinsic). Unlike indirect eval, the code runs
// in the caller's lexical context: this, super, new.target, and private names
// all resolve as in the surrounding code (ECMA-262 19.2.1.1 PerformEval with
// direct = true and a non-strict caller sharing its variable environment).
func (i *Interpreter) directEval(ctx context.Context, code Value, env *Environment) (Value, error) {
	str, ok := code.(String)
	if !ok {
		return code, nil
	}
	if i.security.DisableEval {
		return nil, i.throwError(ctx, "EvalError", "eval is disabled in this sandbox")
	}

	prog, err := parser.ParseEval("<eval>", string(str), parser.EvalContext{
		AllowSuperCall: env.inDerivedConstructor(),
		PrivateNames:   env.privateNamesInScope(),
	})
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	// A fresh declarative scope holds the eval's own lexical (let/const)
	// bindings; var/function declarations hoist to the caller's variable scope,
	// and this/super/#private resolve up the parent chain.
	evalEnv := NewEnvironment(env, false)
	if err := i.hoistDeclarations(ctx, prog.Body, evalEnv, true); err != nil {
		return nil, err
	}
	return i.execStmts(ctx, prog.Body, evalEnv)
}
