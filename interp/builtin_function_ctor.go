package interp

import (
	"context"
	"strings"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/parser"
)

// dynFuncKind selects which flavour of function CreateDynamicFunction builds,
// controlling both the syntactic prefix (function / function* / async function
// / async function*) used to parse the source and the resulting function's
// semantics.
type dynFuncKind int

const (
	dynNormal dynFuncKind = iota
	dynGenerator
	dynAsync
	dynAsyncGenerator
)

// prefix returns the source prefix that precedes the "anonymous" name for this
// kind (e.g. "async function* ").
func (k dynFuncKind) prefix() string {
	switch k {
	case dynGenerator:
		return "function* "
	case dynAsync:
		return "async function "
	case dynAsyncGenerator:
		return "async function* "
	default:
		return "function "
	}
}

// createDynamicFunction implements CreateDynamicFunction for a normal function
// (ECMA-262 sec-createdynamicfunction), backing `new Function(...)` and a plain
// `Function(...)` call.
func (i *Interpreter) createDynamicFunction(ctx context.Context, args []Value) (Value, error) {
	return i.createDynamicFunctionKind(ctx, dynNormal, args)
}

// createDynamicFunctionKind implements CreateDynamicFunction for any of the
// function flavours. The last argument is the function body; any preceding
// arguments are formal parameters (each stringified, then comma-joined). The
// resulting function closes over the global environment and is named
// "anonymous".
//
// Parameters and body are parsed separately as well as combined, so that an
// injection such as `new Function("/*", "*/ ) {")` is rejected: each piece must
// be individually well-formed.
func (i *Interpreter) createDynamicFunctionKind(ctx context.Context, kind dynFuncKind, args []Value) (Value, error) {
	if i.security.DisableFunctionCtor {
		return nil, i.throwError(ctx, "EvalError", "Function constructor is disabled in this sandbox")
	}
	prefix := kind.prefix()

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
	if _, err := parser.Parse("<anonymous>", "("+prefix+"anonymous("+paramString+"\n){})"); err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}
	bodyParseString := "\n" + bodyString + "\n"
	if _, err := parser.Parse("<anonymous>", "("+prefix+"anonymous(){"+bodyParseString+"})"); err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	// Parse the assembled source. The exact shape matches the spec so that
	// early errors on the whole FunctionExpression are enforced too.
	sourceString := prefix + "anonymous(" + paramString + "\n) {" + bodyParseString + "}"
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
