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
	i.hoistDeclarations(ctx, prog.Body, env, true)
	result, err := i.execStmts(ctx, prog.Body, env)
	if err != nil {
		return nil, err
	}
	return result, nil
}
