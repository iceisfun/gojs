package interp

import "github.com/iceisfun/gojs/ast"

// EvalKind identifies which dynamic-code entry point compiled a source string.
type EvalKind string

const (
	EvalIndirect EvalKind = "eval"        // an indirect eval(str)
	EvalDirect   EvalKind = "eval:direct" // a direct eval(str)
	EvalFunction EvalKind = "Function"    // Function(...) / new Function(...) / fn.constructor(...)
)

// EvalInfo describes one dynamic compilation delivered to a WithDumpEval observer.
type EvalInfo struct {
	Kind    EvalKind     // which entry point compiled this source
	Source  string       // the exact source text handed to the parser
	Program *ast.Program // the parsed AST, or nil when Err is non-nil
	Err     error        // non-nil when the source failed to parse
}

// EvalObserver receives every dynamic compilation (eval / Function) on the VM
// goroutine, before the compiled code runs — a lens for unwinding obfuscated,
// self-generating payloads one stage at a time.
type EvalObserver func(EvalInfo)

// WithDumpEval installs an observer called for every eval() and Function()
// compilation, handing it the source text and parsed AST. It is a debugging /
// reverse-engineering aid; it does not change execution.
func WithDumpEval(obs EvalObserver) Option {
	return func(i *Interpreter) { i.dumpEval = obs }
}

// reportEval delivers one compilation to the observer, if installed.
func (i *Interpreter) reportEval(kind EvalKind, source string, prog *ast.Program, err error) {
	if i.dumpEval != nil {
		i.dumpEval(EvalInfo{Kind: kind, Source: source, Program: prog, Err: err})
	}
}
