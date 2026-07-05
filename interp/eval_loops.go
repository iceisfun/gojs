package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
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
	// The loop header gets its own scope so `let` bindings are per-loop. When the
	// initializer is a lexical declaration (let/const), its bound names are
	// "perIterationBindings": each iteration runs in a fresh environment seeded
	// with the previous iteration's values (ECMA-262 §14.7.4.7 ForBodyEvaluation
	// and CreatePerIterationEnvironment). Closures created in the test, body, or
	// update thus capture that iteration's binding rather than a single shared one.
	loopEnv := NewEnvironment(env, false)
	var perIterNames []string
	if s.Init != nil {
		switch init := s.Init.(type) {
		case *ast.VarDecl:
			if err := i.hoistDeclarations(ctx, []ast.Stmt{init}, loopEnv, false); err != nil {
				return nil, err
			}
			if err := i.evalVarDecl(ctx, init, loopEnv); err != nil {
				return nil, err
			}
			if init.Kind == token.LET || init.Kind == token.CONST {
				for _, d := range init.Decls {
					forEachPatternName(d.Target, func(n string) {
						perIterNames = append(perIterNames, n)
					})
				}
			}
		case ast.Expr:
			if _, err := i.evalExpr(ctx, init, loopEnv); err != nil {
				return nil, err
			}
		}
	}
	// CreatePerIterationEnvironment before the first test.
	curEnv := loopEnv
	if len(perIterNames) > 0 {
		curEnv = i.createPerIterationEnvironment(curEnv, env, perIterNames)
	}
	var completion Value = Undef
	for {
		if s.Test != nil {
			test, err := i.evalExpr(ctx, s.Test, curEnv)
			if err != nil {
				return nil, err
			}
			if !ToBoolean(test) {
				return completion, nil
			}
		}
		bodyVal, err := i.evalStmt(ctx, s.Body, curEnv)
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
		// CreatePerIterationEnvironment after the body, before the update: the
		// increment runs in the fresh environment so closures captured during the
		// previous iteration keep their own copy of the loop variables.
		if len(perIterNames) > 0 {
			curEnv = i.createPerIterationEnvironment(curEnv, env, perIterNames)
		}
		if s.Update != nil {
			if _, err := i.evalExpr(ctx, s.Update, curEnv); err != nil {
				return nil, err
			}
		}
	}
}

// createPerIterationEnvironment implements CreatePerIterationEnvironment
// (ECMA-262 §14.7.4.4): a new declarative environment whose outer scope is the
// loop's outer scope, with each per-iteration binding copied from the previous
// iteration's environment.
func (i *Interpreter) createPerIterationEnvironment(last, outer *Environment, names []string) *Environment {
	iter := NewEnvironment(outer, false)
	for _, name := range names {
		if b, ok := last.vars[name]; ok {
			iter.bind(name, &binding{value: b.value, mutable: b.mutable, initialized: b.initialized})
		}
	}
	return iter
}

// evalForIn evaluates for-in and for-of loops.
func (i *Interpreter) evalForIn(ctx context.Context, s *ast.ForInStmt, env *Environment) (Value, error) {
	return i.runForIn(ctx, s, env, "")
}

func (i *Interpreter) runForIn(ctx context.Context, s *ast.ForInStmt, env *Environment, label string) (Value, error) {
	// ForIn/OfHeadEvaluation (§14.7.5.11): a lexical ForDeclaration (let/const)
	// puts its bound names in a Temporal Dead Zone — an environment where they are
	// declared but uninitialized — while the iterable expression is evaluated, so
	// the RHS cannot observe the outer binding of the same name, and a closure it
	// creates captures the TDZ binding (accessing it later throws ReferenceError).
	rhsEnv := env
	if vd, ok := s.Left.(*ast.VarDecl); ok && (vd.Kind == token.LET || vd.Kind == token.CONST) {
		tdz := NewEnvironment(env, false)
		mutable := vd.Kind == token.LET
		for _, d := range vd.Decls {
			forEachPatternName(d.Target, func(n string) {
				tdz.bind(n, &binding{mutable: mutable, initialized: false})
			})
		}
		rhsEnv = tdz
	}
	rhs, err := i.evalExpr(ctx, s.Right, rhsEnv)
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

	if s.Await {
		return i.runForAwait(ctx, env, rhs, runBody, &completion)
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
				// A return, or a break/continue targeting an outer loop, or a
				// throw. IteratorClose (§7.4.11) calls return(): if the body's
				// completion is a throw, that throw wins and the return() result is
				// discarded; otherwise (a non-throw abrupt completion) an error from
				// return() supersedes it.
				closeErr := closeIter()
				if closeErr != nil && isAbruptSignal(bodyErr) {
					return completion, closeErr
				}
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
	keys, err := i.enumerateKeys(ctx, obj)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		enum, err := i.stillEnumerable(ctx, obj, k)
		if err != nil {
			return nil, err
		}
		if !enum {
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

// runForAwait evaluates a `for await (... of ...)` loop (§14.7.5.7 with the
// async iteration hint). It obtains the async iterator (@@asyncIterator, or the
// sync iterator with each value awaited, per AsyncFromSyncIterator) and awaits
// every iterator step by suspending the enclosing async coroutine.
func (i *Interpreter) runForAwait(ctx context.Context, env *Environment, rhs Value, runBody func(Value) (loopControl, error), completion *Value) (Value, error) {
	gs := env.generator()
	if gs == nil {
		return nil, i.throwError(ctx, "SyntaxError", "for await is only valid inside an async function or async generator")
	}

	// GetIterator(rhs, async) (§7.4.2): use @@asyncIterator, or wrap the sync
	// iterator in %AsyncFromSyncIteratorPrototype% so each step's value is awaited
	// through AsyncFromSyncIteratorContinuation. Both cases then share one loop
	// that awaits result = iterator.next() (§14.7.5.6 with iteratorKind=async).
	rec, err := i.getAsyncIterator(ctx, rhs)
	if err != nil {
		return nil, err
	}
	iter := rec.iterator
	for {
		nres, err := i.call(ctx, rec.nextMethod, iter, nil)
		if err != nil {
			return *completion, err
		}
		settled, err := i.doAwait(gs, nres)
		if err != nil {
			return *completion, err
		}
		res, ok := settled.(*Object)
		if !ok {
			return *completion, i.throwError(ctx, "TypeError", "iterator result is not an object")
		}
		doneV, err := res.GetStr(ctx, "done")
		if err != nil {
			return *completion, err
		}
		if ToBoolean(doneV) {
			return *completion, nil
		}
		val, err := res.GetStr(ctx, "value")
		if err != nil {
			return *completion, err
		}
		sig, bodyErr := runBody(val)
		switch sig {
		case loopContinue, loopNormal:
			continue
		case loopBreak:
			return *completion, i.asyncIteratorClose(ctx, gs, iter, nil)
		default:
			return *completion, i.asyncIteratorClose(ctx, gs, iter, bodyErr)
		}
	}
}

// asyncIteratorClose implements AsyncIteratorClose (§7.4.8): on abrupt loop
// completion it calls the async iterator's return() (if any) and awaits it.
// completion is the loop's own completion (nil for break/normal, a throw for a
// body error). A throw completion takes precedence over any error from looking
// up, calling, or awaiting return() (steps 5-6); for a non-throw completion the
// GetMethod/Call/Await error, or a non-Object return result, is propagated.
func (i *Interpreter) asyncIteratorClose(ctx context.Context, gs *generatorState, iter *Object, completion error) error {
	ret, err := i.getMethodStr(ctx, iter, "return")
	if err != nil {
		if completion != nil {
			return completion
		}
		return err
	}
	if ret == nil {
		return completion
	}
	res, err := i.call(ctx, ret, iter, nil)
	if err != nil {
		if completion != nil {
			return completion
		}
		return err
	}
	awaited, err := i.doAwait(gs, res)
	if err != nil {
		if completion != nil {
			return completion
		}
		return err
	}
	if completion != nil {
		return completion
	}
	if _, ok := awaited.(*Object); !ok {
		return i.throwError(ctx, "TypeError", "iterator return method returned a non-object")
	}
	return nil
}

// stillEnumerable reports whether name is still an enumerable property somewhere
// on obj's prototype chain, so a for-in enumeration skips properties deleted
// (or made non-enumerable) before they are visited. It routes through the
// object internal methods so a Proxy's traps run.
func (i *Interpreter) stillEnumerable(ctx context.Context, obj *Object, name string) (bool, error) {
	for cur := obj; cur != nil; {
		p, ok, err := i.getOwnPropertyV(ctx, cur, StrKey(name))
		if err != nil {
			return false, err
		}
		if ok {
			return p.Enumerable, nil
		}
		proto, err := i.getProtoV(ctx, cur)
		if err != nil {
			return false, err
		}
		next, ok := proto.(*Object)
		if !ok {
			break
		}
		cur = next
	}
	return false, nil
}

// bindForTarget binds a for-in/of loop value to the loop's left-hand side,
// which is either a declaration or an existing assignment target.
func (i *Interpreter) bindForTarget(ctx context.Context, left ast.Node, v Value, iterEnv, assignEnv *Environment) error {
	switch l := left.(type) {
	case *ast.VarDecl:
		target := l.Decls[0].Target
		// A `const` ForBinding is immutable: assigning to it in the loop body
		// (e.g. `for (const x of a) x++`) is a TypeError (§14.7.5.7). let/var
		// bindings remain mutable.
		mutable := l.Kind != token.CONST
		bind := func(name string, val Value) {
			iterEnv.bind(name, &binding{value: val, mutable: mutable, initialized: true})
		}
		return i.bindPattern(ctx, target, v, iterEnv, bind)
	case ast.Expr:
		return i.assignTo(ctx, l, v, assignEnv)
	default:
		return i.throwError(ctx, "SyntaxError", "invalid for-loop binding")
	}
}

// enumerateKeys returns the enumerable string keys of obj and its prototype
// chain, de-duplicated, in insertion order (for for-in). It routes through the
// object internal methods so a Proxy's ownKeys/getOwnPropertyDescriptor/
// getPrototypeOf traps run.
func (i *Interpreter) enumerateKeys(ctx context.Context, obj *Object) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for cur := obj; cur != nil; {
		keys, err := i.ownKeysV(ctx, cur)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if k.IsSymbol() || seen[k.Str] {
				continue
			}
			seen[k.Str] = true
			p, ok, err := i.getOwnPropertyV(ctx, cur, k)
			if err != nil {
				return nil, err
			}
			if ok && p.Enumerable {
				out = append(out, k.Str)
			}
		}
		proto, err := i.getProtoV(ctx, cur)
		if err != nil {
			return nil, err
		}
		next, ok := proto.(*Object)
		if !ok {
			break
		}
		cur = next
	}
	return out, nil
}

// errStopIteration is a sentinel used to break out of the iterate helper.
var errStopIteration = &sentinelError{"stop iteration"}

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }
