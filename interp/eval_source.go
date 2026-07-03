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

	// Indirect eval runs in the global scope: super, super property, and
	// new.target are all invalid, so parse with an empty context.
	prog, err := parser.ParseEval("<eval>", string(str), parser.EvalContext{})
	if err != nil {
		// A parse failure in eval surfaces as a SyntaxError thrown value.
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	env := i.globalEnv
	// Indirect eval is never strict on account of its caller: its strictness comes
	// solely from its own Directive Prologue (§19.2.1.1). The global environment is
	// shared, so save and restore the flag around this synchronous run — mirroring
	// evalProgram — rather than letting a strict enclosing program leak in.
	savedStrict := env.strict
	env.strict = prog.Strict
	defer func() { env.strict = savedStrict }()
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
		// A direct eval inherits the strictness of the calling context, so
		// strict-only early errors (e.g. a `with` statement) fire on its code.
		Strict:             env.isStrict(),
		AllowSuperCall:     env.inDerivedConstructor(),
		AllowSuperProperty: env.homeObject() != nil,
		AllowNewTarget:     env.functionScope() != i.globalEnv,
		PrivateNames:       env.privateNamesInScope(),
	})
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	// A fresh declarative scope holds the eval's own lexical (let/const)
	// bindings; var/function declarations hoist to the caller's variable scope,
	// and this/super/#private resolve up the parent chain. Direct-eval code is
	// strict when its caller is strict or its own Directive Prologue opts in.
	evalEnv := NewEnvironment(env, false)
	evalEnv.strict = env.isStrict() || prog.Strict
	if err := i.hoistDeclarations(ctx, prog.Body, evalEnv, true); err != nil {
		return nil, err
	}
	return i.execStmts(ctx, prog.Body, evalEnv)
}
