package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/parser"
)

// This file provides the top-level entry points for running JavaScript source:
// parse, evaluate the top-level program, then drain the event loop so scheduled
// timers and microtasks run to completion.

// RunString parses and executes JavaScript source, returning the completion
// value of the program (the value of its last expression statement) after the
// event loop has drained. sourceName appears in error messages.
func (i *Interpreter) RunString(sourceName, source string) (Value, error) {
	prog, err := parser.Parse(sourceName, source)
	if err != nil {
		// A parse failure is a SyntaxError; surface it as a thrown JS value so
		// embedders (and try/catch-style harnesses) see a proper error object.
		return nil, NewThrow(i.newError("SyntaxError", err.Error()))
	}
	return i.RunProgram(prog)
}

// RunProgram executes an already-parsed program.
func (i *Interpreter) RunProgram(prog *ast.Program) (Value, error) {
	// Each top-level run (including the timer/microtask tail it drains) gets a
	// fresh step budget.
	i.steps = 0
	result, err := i.evalProgram(i.ctx, prog)
	if err != nil {
		return nil, i.normalizeError(err)
	}
	// Drain queued microtasks and timers scheduled by the program.
	if loopErr := i.loop.run(); loopErr != nil {
		return nil, i.normalizeError(loopErr)
	}
	return result, nil
}

// evalProgram runs the top-level statements of a program in the global scope.
func (i *Interpreter) evalProgram(ctx context.Context, prog *ast.Program) (Value, error) {
	env := i.globalEnv
	if prog.Strict || i.forceStrict() {
		// Strict mode currently affects only a few checks; recorded for future
		// use. The global environment is shared across RunString calls.
	}
	i.hoistDeclarations(ctx, prog.Body, env, true)
	result, err := i.execStmts(ctx, prog.Body, env)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// normalizeError converts internal control-flow signals that escaped to the top
// level into host errors, and leaves *Throw and context errors intact.
func (i *Interpreter) normalizeError(err error) error {
	switch err.(type) {
	case *returnSignal:
		return &Throw{Value: i.newError("SyntaxError", "Illegal return statement")}
	case *breakSignal:
		return &Throw{Value: i.newError("SyntaxError", "Illegal break statement")}
	case *continueSignal:
		return &Throw{Value: i.newError("SyntaxError", "Illegal continue statement")}
	default:
		return err
	}
}

// ThrownValue returns the JavaScript value carried by a *Throw error, or nil if
// err is not a thrown exception. Embedders use it to inspect uncaught errors.
func ThrownValue(err error) (Value, bool) {
	if t, ok := err.(*Throw); ok {
		return t.Value, true
	}
	return nil, false
}
