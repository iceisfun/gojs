package interp

import (
	"context"
	"strings"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/parser"
)

// createDynamicFunction implements CreateDynamicFunction for ~normal~ functions
// (ECMA-262 sec-createdynamicfunction), backing `new Function(...)` and a plain
// `Function(...)` call. The last argument is the function body; any preceding
// arguments are formal parameters (each stringified, then comma-joined). The
// resulting function closes over the global environment and is named
// "anonymous".
//
// Parameters and body are parsed separately as well as combined, so that an
// injection such as `new Function("/*", "*/ ) {")` is rejected: each piece must
// be individually well-formed.
func (i *Interpreter) createDynamicFunction(ctx context.Context, args []Value) (Value, error) {
	if i.security.DisableFunctionCtor {
		return nil, i.throwError(ctx, "EvalError", "Function constructor is disabled in this sandbox")
	}

	// Split arguments into parameter strings and the body string.
	var paramStrings []string
	bodyString := ""
	if n := len(args); n > 0 {
		for k := 0; k < n-1; k++ {
			s, err := i.ToStringV(ctx, args[k])
			if err != nil {
				return nil, err
			}
			paramStrings = append(paramStrings, s)
		}
		s, err := i.ToStringV(ctx, args[n-1])
		if err != nil {
			return nil, err
		}
		bodyString = s
	}
	paramString := strings.Join(paramStrings, ",")

	// Validate the parameter list and body independently so that neither can
	// smuggle grammar into the other.
	if _, err := parser.Parse("<anonymous>", "(function anonymous("+paramString+"\n){})"); err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}
	bodyParseString := "\n" + bodyString + "\n"
	if _, err := parser.Parse("<anonymous>", "(function anonymous(){"+bodyParseString+"})"); err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	// Parse the assembled source. The exact shape matches the spec so that
	// early errors on the whole FunctionExpression are enforced too.
	sourceString := "function anonymous(" + paramString + "\n) {" + bodyParseString + "}"
	prog, err := parser.Parse("<anonymous>", sourceString)
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}
	if len(prog.Body) != 1 {
		return nil, i.throwError(ctx, "SyntaxError", "invalid dynamic function source")
	}
	decl, ok := prog.Body[0].(*ast.FuncDecl)
	if !ok || decl.Def == nil {
		return nil, i.throwError(ctx, "SyntaxError", "invalid dynamic function source")
	}

	// The dynamic function closes over the global environment.
	fn := i.makeFunction(decl.Def, i.globalEnv, kindNormal, nil)
	return fn, nil
}
