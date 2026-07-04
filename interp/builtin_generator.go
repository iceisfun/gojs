package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// Generators are implemented with a dedicated goroutine per generator instance.
// The generator body runs in that goroutine; a `yield` blocks it while handing a
// value back to the caller, and the consumer's next() call resumes it. Because
// next() and the body never run at the same instant (each blocks while the other
// runs), no interpreter state is touched concurrently — this is cooperative
// coroutining, not parallelism.
//
// The goroutine also selects on the interpreter context so that Close (context
// cancellation) unblocks and terminates any generators left suspended, avoiding
// leaks. Each generator goroutine is tracked by the interpreter WaitGroup so
// Close waits for them.

// generatorState holds the channels connecting a suspended generator body to
// its consumer.
type generatorState struct {
	resume chan resumeMsg // consumer → body: value/behavior for the pending yield
	out    chan yieldMsg  // body → consumer: a yielded/returned/thrown result
	done   bool
	// executing is true while the body is running between a resume and the next
	// suspension/completion. A re-entrant resume observed during this window is
	// the "executing" state of GeneratorValidate (ECMA-262 §27.5.3.2 step 5).
	executing bool
	// asyncGen is true when this coroutine drives an async generator, so a
	// `yield` first awaits its operand before delivering it to the consumer.
	asyncGen bool
	ctx      context.Context // cancelled when the interpreter closes
}

// resumeMsg is sent into the generator by next/return/throw.
type resumeMsg struct {
	value Value
	// mode selects how the pending yield resolves: continue with value, force a
	// return, or throw value.
	mode resumeMode
}

type resumeMode int

const (
	resumeNext resumeMode = iota
	resumeReturn
	resumeThrow
)

// yieldMsg is produced by the body at a yield point or on completion.
type yieldMsg struct {
	value Value
	done  bool
	err   error
	// rawResult, when non-nil, is an iterator-result object that the sync
	// generator driver must hand back to the consumer verbatim (without
	// re-wrapping it via CreateIterResultObject). It models the sync
	// GeneratorYield(innerResult) step of yield* delegation (§14.4.14), where the
	// exact { value, done } object produced by the inner iterator is re-yielded —
	// so a missing `done` property is observably preserved. It is meaningless for
	// async generators and async functions.
	rawResult *Object
	// awaited marks a suspension produced by `await` (as opposed to `yield`):
	// the async-generator driver resolves the value and resumes the body without
	// delivering an iterator result to the consumer. It is meaningless for sync
	// generators (which never await) and async functions (whose every suspension
	// is an await).
	awaited bool
}

// startCoroutine sets up a suspendable coroutine over a function body: it binds
// parameters, prepares the body environment (with env.gen wired so yield/await
// can suspend), and returns the shared state plus an advance function. The body
// runs lazily on a dedicated goroutine on first advance; advance sends a resume
// message and blocks until the body next suspends (yield/await) or completes.
//
// Both generators (makeGenerator) and async functions (asyncRun) are built on
// this. Because advance blocks the caller while the body runs and the body
// blocks at each suspension point, only one goroutine touches interpreter state
// at a time — cooperative coroutining, not parallelism.
func (i *Interpreter) startCoroutine(fnObj *Object, def *ast.FuncDef, closure *Environment, homeObj *Object, this Value, args []Value, arrow bool) (*generatorState, func(resumeMsg) yieldMsg, error) {
	gs := &generatorState{
		resume:   make(chan resumeMsg),
		out:      make(chan yieldMsg),
		asyncGen: def.Async && def.Generator,
		ctx:      i.ctx,
	}

	env := NewEnvironment(closure, true)
	env.strict = def.Strict || i.forceStrict()
	// An async arrow inherits this/arguments lexically; a normal function/
	// generator establishes its own.
	if !arrow {
		env.setThis(this)
		if homeObj != nil {
			env.homeObj = homeObj
		}
	}
	env.gen = gs
	// A named generator/async(-generator) function expression can refer to itself
	// by name through an immutable binding in its body scope (mirroring the plain
	// named-function-expression case in makeFunction). Methods (homeObj != nil) and
	// arrows never create this binding.
	if def.Name != nil && !arrow && homeObj == nil && fnObj != nil {
		if _, exists := closure.vars[def.Name.Name]; !exists {
			env.vars[def.Name.Name] = &binding{value: fnObj, mutable: false, weakImmutable: true, initialized: true}
		}
	}
	// Establish the arguments object before binding parameters so it is visible
	// to default-value initializers (gojs uses an unmapped snapshot, so there is
	// no aliasing to defer); a parameter named "arguments" shadows it, which
	// bindParams applies by overwriting the binding below.
	if !arrow {
		env.vars["arguments"] = &binding{value: i.makeArguments(args, nil, env.strict || !simpleParameterList(def.Params)), mutable: true, initialized: true}
	}
	if err := i.bindParams(i.ctx, def.Params, args, env); err != nil {
		return nil, nil, err
	}
	// A non-simple parameter list gives the body its own VariableEnvironment
	// (§10.2.11 step 27). It is a fresh function scope, so the generator state —
	// which generator() locates by walking to the nearest function scope — must be
	// carried onto it, or a `yield` in the body would find no generator.
	bodyEnv := bodyVarEnv(def.Params, def.Body, env)
	if bodyEnv != env {
		bodyEnv.gen = gs
	}

	started := false
	start := func() {
		started = true
		i.wg.Add(1)
		go func() {
			defer i.wg.Done()
			select {
			case <-gs.resume: // first resume value is ignored, per spec
			case <-gs.ctx.Done():
				return
			}
			_, err := i.runGeneratorBody(gs.ctx, defName(def), def.Body, bodyEnv)
			var final yieldMsg
			switch e := err.(type) {
			case nil:
				final = yieldMsg{value: Undef, done: true}
			case *returnSignal:
				final = yieldMsg{value: e.value, done: true}
			case *genReturn:
				final = yieldMsg{value: e.value, done: true}
			default:
				final = yieldMsg{err: err, done: true}
			}
			select {
			case gs.out <- final:
			case <-gs.ctx.Done():
			}
		}()
	}

	advance := func(msg resumeMsg) yieldMsg {
		if gs.done {
			return yieldMsg{value: Undef, done: true}
		}
		if !started {
			// An abrupt resume (return or throw) of a generator still suspended at
			// its start completes it immediately, without ever executing the body
			// (ECMA-262 27.5.3.3/27.5.3.4: a suspendedStart generator goes straight
			// to completed). Only a normal resume (next) starts the body.
			switch msg.mode {
			case resumeReturn:
				gs.done = true
				return yieldMsg{value: msg.value, done: true}
			case resumeThrow:
				gs.done = true
				return yieldMsg{done: true, err: NewThrow(msg.value)}
			}
			start()
		}
		gs.executing = true
		select {
		case gs.resume <- msg:
		case <-gs.ctx.Done():
			gs.executing = false
			gs.done = true
			return yieldMsg{done: true, err: gs.ctx.Err()}
		}
		select {
		case res := <-gs.out:
			gs.executing = false
			if res.done {
				gs.done = true
			}
			return res
		case <-gs.ctx.Done():
			gs.executing = false
			gs.done = true
			return yieldMsg{done: true, err: gs.ctx.Err()}
		}
	}
	return gs, advance, nil
}

// generatorInstance carries the coroutine state backing a generator object. It
// is stored in the object's internal "Generator" slot (the [[GeneratorState]]/
// [[GeneratorContext]] pair of ECMA-262 §27.5) so that the shared
// %GeneratorPrototype% methods can locate it from their `this` receiver.
type generatorInstance struct {
	gs      *generatorState
	advance func(resumeMsg) yieldMsg
}

// generatorInstanceOf returns the generator backing receiver this, or nil when
// this is not a generator object (GeneratorValidate §27.5.3.2 steps 1–2).
func generatorInstanceOf(this Value) *generatorInstance {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	g, _ := o.internal["Generator"].(*generatorInstance)
	return g
}

// makeGenerator builds the generator-object factory: calling a generator
// function returns a fresh generator (iterator) object without running the body.
// The next/return/throw methods live on %GeneratorPrototype% (see initGenerator);
// the object only carries the coroutine state in its internal slot.
func (i *Interpreter) makeGenerator(fnObj *Object, def *ast.FuncDef, closure *Environment, homeObj *Object, this Value, args []Value) (Value, error) {
	gs, advance, err := i.startCoroutine(fnObj, def, closure, homeObj, this, args, false)
	if err != nil {
		return nil, err
	}

	// OrdinaryCreateFromConstructor(fnObj, "%GeneratorPrototype%"): the instance's
	// [[Prototype]] is the generator function's own .prototype object (which itself
	// inherits from %GeneratorPrototype%), falling back to the intrinsic when that
	// property is not an object.
	instProto := i.generatorProto
	if fnObj != nil {
		if p, ok := fnObj.getOwn(StrKey("prototype")); ok && !p.Accessor {
			if po, isObj := p.Value.(*Object); isObj {
				instProto = po
			}
		}
	}
	genObj := NewObject(instProto)
	genObj.class = "Generator"
	genObj.internal = map[string]any{"Generator": &generatorInstance{gs: gs, advance: advance}}
	return genObj, nil
}

// initGenerator installs the own properties of %GeneratorPrototype% (ECMA-262
// §27.5.1): the shared next/return/throw methods, the constructor back-reference
// to %Generator% (%GeneratorFunction.prototype%), and the @@toStringTag. The
// [ @@iterator ] () method is inherited from %IteratorPrototype%.
func (i *Interpreter) initGenerator() {
	proto := i.generatorProto

	// resume performs GeneratorValidate + GeneratorResume/GeneratorResumeAbrupt
	// (§27.5.3.2–27.5.3.4) for the given mode.
	resume := func(ctx context.Context, name string, this Value, val Value, mode resumeMode) (Value, error) {
		inst := generatorInstanceOf(this)
		if inst == nil {
			return nil, i.throwError(ctx, "TypeError", "Generator.prototype."+name+" called on a non-generator object")
		}
		gs := inst.gs
		// GeneratorValidate step 5: resuming while "executing" is a TypeError and
		// leaves the generator completed.
		if gs.executing {
			gs.done = true
			return nil, i.throwError(ctx, "TypeError", "generator is already running")
		}
		if gs.done {
			switch mode {
			case resumeReturn:
				return i.createIterResult(val, true), nil
			case resumeThrow:
				return nil, NewThrow(val)
			default: // resumeNext
				return i.createIterResult(Undef, true), nil
			}
		}
		res := inst.advance(resumeMsg{value: val, mode: mode})
		if res.err != nil {
			return nil, res.err
		}
		if res.rawResult != nil {
			// yield* re-yields the inner iterator's result object verbatim.
			return res.rawResult, nil
		}
		return i.createIterResult(res.value, res.done), nil
	}

	i.defineMethod(proto, "next", 1, func(ctx context.Context, this Value, a []Value) (Value, error) {
		return resume(ctx, "next", this, arg(a, 0), resumeNext)
	})
	i.defineMethod(proto, "return", 1, func(ctx context.Context, this Value, a []Value) (Value, error) {
		return resume(ctx, "return", this, arg(a, 0), resumeReturn)
	})
	i.defineMethod(proto, "throw", 1, func(ctx context.Context, this Value, a []Value) (Value, error) {
		return resume(ctx, "throw", this, arg(a, 0), resumeThrow)
	})

	// %GeneratorPrototype%.constructor is %Generator% (i.e. %GeneratorFunction%'s
	// prototype), non-writable/non-enumerable/configurable.
	proto.defineOwn(StrKey("constructor"), &Property{Value: i.genFuncProto, Writable: false, Enumerable: false, Configurable: true})
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("Generator"), Writable: false, Enumerable: false, Configurable: true})
}

// runGeneratorBody executes a generator body, hoisting declarations first. It is
// separate from runFunctionBody only so future generator-specific handling has a
// home.
func (i *Interpreter) runGeneratorBody(ctx context.Context, name string, body *ast.BlockStmt, env *Environment) (Value, error) {
	defer i.enterFrame(name)()
	// Clear any enclosing parameter-default context (see runFunctionBody).
	savedParamEnv := i.paramDefaultEnv
	i.paramDefaultEnv = nil
	defer func() { i.paramDefaultEnv = savedParamEnv }()
	if err := i.hoistDeclarations(ctx, body.Body, env, true); err != nil {
		return Undef, err
	}
	_, err := i.execStmts(ctx, body.Body, env)
	return Undef, err
}

// evalYield implements a yield / yield* expression inside a generator body. It
// runs on the generator goroutine, so it may block on the resume channel.
func (i *Interpreter) evalYield(ctx context.Context, e *ast.YieldExpr, env *Environment) (Value, error) {
	gs := env.generator()
	if gs == nil {
		return nil, i.throwError(ctx, "SyntaxError", "yield is only valid inside a generator")
	}

	if e.Delegate {
		return i.evalYieldDelegate(ctx, e, env, gs)
	}

	var val Value = Undef
	if e.Argument != nil {
		v, err := i.evalExpr(ctx, e.Argument, env)
		if err != nil {
			return nil, err
		}
		val = v
	}
	// In an async generator, `yield x` first awaits x (AsyncGeneratorYield,
	// §27.6.3.8 step 5) and then delivers the settled value to the consumer.
	if gs.asyncGen {
		awaited, err := i.doAwait(gs, val)
		if err != nil {
			return nil, err
		}
		return i.doYield(gs, awaited)
	}
	return i.doYield(gs, val)
}

// suspend hands a message to the consumer/driver and blocks until resumed,
// translating a return()/throw() resume into the appropriate control signal.
// awaited distinguishes an `await` suspension (transparent to an async
// generator's consumer) from a `yield` suspension (which delivers a result).
func (i *Interpreter) suspend(gs *generatorState, out yieldMsg) (Value, error) {
	select {
	case gs.out <- out:
	case <-gs.ctx.Done():
		return nil, gs.ctx.Err()
	}
	select {
	case msg := <-gs.resume:
		switch msg.mode {
		case resumeReturn:
			// AsyncGeneratorUnwrapYieldResumption (§27.6.3.7): a return completion
			// resuming an async generator's `yield` awaits its value before closing
			// the generator. PromiseResolve may itself throw (broken promise), and a
			// rejection propagates as a throw into the body. An `await` suspension
			// (out.awaited) is never resumed with a return, so it is excluded.
			if gs.asyncGen && !out.awaited {
				promise, err := i.promiseResolve(i.ctx, i.promiseCtor, msg.value)
				if err != nil {
					return nil, err
				}
				awaited, err := i.doAwait(gs, promise)
				if err != nil {
					return nil, err
				}
				return nil, &genReturn{value: awaited}
			}
			return nil, &genReturn{value: msg.value}
		case resumeThrow:
			return nil, NewThrow(msg.value)
		default:
			return msg.value, nil
		}
	case <-gs.ctx.Done():
		return nil, gs.ctx.Err()
	}
}

// doYield hands a yielded value to the consumer and blocks until resumed.
func (i *Interpreter) doYield(gs *generatorState, value Value) (Value, error) {
	return i.suspend(gs, yieldMsg{value: value, done: false})
}

// doYieldRaw hands the inner iterator's result object to the consumer verbatim
// (sync GeneratorYield(innerResult) for yield*), blocking until resumed.
func (i *Interpreter) doYieldRaw(gs *generatorState, result *Object) (Value, error) {
	return i.suspend(gs, yieldMsg{rawResult: result})
}

// doAwait suspends for an awaited value; the async driver resolves it to a
// promise and resumes with the settled value (or a thrown rejection).
func (i *Interpreter) doAwait(gs *generatorState, value Value) (Value, error) {
	return i.suspend(gs, yieldMsg{value: value, done: false, awaited: true})
}

// createIterResult builds a { value, done } iterator-result object.
func (i *Interpreter) createIterResult(value Value, done bool) *Object {
	o := NewObject(i.objectProto)
	o.SetData("value", value)
	o.SetData("done", Bool(done))
	return o
}

// evalYieldDelegate implements `yield*`: it drives an inner iterable, yielding
// each of its values, and evaluates to the inner iterator's return value.
func (i *Interpreter) evalYieldDelegate(ctx context.Context, e *ast.YieldExpr, env *Environment, gs *generatorState) (Value, error) {
	iterable, err := i.evalExpr(ctx, e.Argument, env)
	if err != nil {
		return nil, err
	}
	if gs.asyncGen {
		return i.evalYieldDelegateAsync(ctx, iterable, gs)
	}

	// GetIterator(value, sync) (§14.4.14 step 4).
	rec, err := i.getIterator(ctx, iterable)
	if err != nil {
		return nil, err
	}
	iter := rec.iterator

	// received models the resumption driving each loop turn (§14.4.14 step 6):
	// nil = normal (next), *genReturn = a consumer return(), *Throw = throw().
	// recVal carries the value sent by a normal resumption.
	var recVal Value = Undef
	var received error

	for {
		switch rc := received.(type) {
		case nil:
			// step a.i: innerResult = IteratorNext(iterator, received.[[Value]]).
			innerResult, err := i.call(ctx, rec.nextMethod, iter, []Value{recVal})
			if err != nil {
				return nil, err
			}
			ro, ok := innerResult.(*Object)
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "iterator result is not an object")
			}
			done, err := iterResultDone(ctx, ro)
			if err != nil {
				return nil, err
			}
			if done {
				// step a.iii.1: Return IteratorValue(innerResult).
				return ro.GetStr(ctx, "value")
			}
			// step a.vii: GeneratorYield(innerResult) — re-yield the raw object.
			recVal, received = i.doYieldRaw(gs, ro)
		case *genReturn:
			// step c: received is a return completion.
			retMethod, err := i.getMethodStr(ctx, iter, "return")
			if err != nil {
				return nil, err
			}
			if retMethod == nil {
				// step c.iii: no return method — forward the return completion.
				return nil, &genReturn{value: rc.value}
			}
			innerResult, err := i.call(ctx, retMethod, iter, []Value{rc.value})
			if err != nil {
				return nil, err
			}
			ro, ok := innerResult.(*Object)
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "iterator result is not an object")
			}
			done, err := iterResultDone(ctx, ro)
			if err != nil {
				return nil, err
			}
			if done {
				// step c.viii: complete the yield* with a return of IteratorValue.
				value, err := ro.GetStr(ctx, "value")
				if err != nil {
					return nil, err
				}
				return nil, &genReturn{value: value}
			}
			recVal, received = i.doYieldRaw(gs, ro)
		case *Throw:
			// step b: received is a throw completion.
			throwMethod, err := i.getMethodStr(ctx, iter, "throw")
			if err != nil {
				return nil, err
			}
			if throwMethod == nil {
				// step b.iii: no throw method — close the iterator, then report the
				// protocol violation as a TypeError.
				if cerr := i.iteratorClose(ctx, rec, nil); cerr != nil {
					return nil, cerr
				}
				return nil, i.throwError(ctx, "TypeError", "The iterator does not provide a throw method")
			}
			innerResult, err := i.call(ctx, throwMethod, iter, []Value{rc.Value})
			if err != nil {
				return nil, err
			}
			ro, ok := innerResult.(*Object)
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "iterator result is not an object")
			}
			done, err := iterResultDone(ctx, ro)
			if err != nil {
				return nil, err
			}
			if done {
				// step b.ii.6: Return IteratorValue(innerResult).
				return ro.GetStr(ctx, "value")
			}
			recVal, received = i.doYieldRaw(gs, ro)
		default:
			// A host error (e.g. context cancellation) from a prior suspension.
			return nil, received
		}
	}
}

// iterResultDone implements IteratorComplete (§7.4.5): Get(result, "done") then
// ToBoolean. It deliberately does not read "value", so a consumer that only
// observes an incomplete step never triggers a "value" getter.
func iterResultDone(ctx context.Context, result *Object) (bool, error) {
	doneV, err := result.GetStr(ctx, "done")
	if err != nil {
		return false, err
	}
	return ToBoolean(doneV), nil
}

// evalYieldDelegateAsync implements `yield* value` inside an async generator
// (ECMA-262 §14.4.14, YieldExpression : yield * with generatorKind async). It
// obtains an async iterator (GetIterator(value, async)), awaits each step, and
// forwards the consumer's next/return/throw resumptions to the inner iterator.
func (i *Interpreter) evalYieldDelegateAsync(ctx context.Context, iterable Value, gs *generatorState) (Value, error) {
	rec, err := i.getAsyncIterator(ctx, iterable)
	if err != nil {
		return nil, err
	}
	iter := rec.iterator

	// received models the resumption completion driving each turn: nil = normal
	// (next), *genReturn = return(), *Throw = throw(). recVal is the normal value.
	var recVal Value = Undef
	var received error

	// step calls an inner iterator method, awaits the result, and returns its
	// { done, value } — matching the async yield* loop's IteratorNext/Await shape.
	step := func(method Value, argVal Value) (done bool, value Value, err error) {
		res, err := i.call(ctx, method, iter, []Value{argVal})
		if err != nil {
			return false, nil, err
		}
		settled, err := i.doAwait(gs, res)
		if err != nil {
			return false, nil, err
		}
		ro, ok := settled.(*Object)
		if !ok {
			return false, nil, i.throwError(ctx, "TypeError", "iterator result is not an object")
		}
		doneV, err := ro.GetStr(ctx, "done")
		if err != nil {
			return false, nil, err
		}
		value, err = ro.GetStr(ctx, "value")
		if err != nil {
			return false, nil, err
		}
		return ToBoolean(doneV), value, nil
	}

	// asyncYield performs AsyncGeneratorYield: await the value, then deliver it to
	// the consumer, returning the resumption completion.
	asyncYield := func(v Value) {
		awaited, aerr := i.doAwait(gs, v)
		if aerr != nil {
			recVal, received = nil, aerr
			return
		}
		recVal, received = i.doYield(gs, awaited)
	}

	for {
		switch rc := received.(type) {
		case nil:
			done, value, err := step(rec.nextMethod, recVal)
			if err != nil {
				return nil, err
			}
			if done {
				return value, nil
			}
			asyncYield(value)
		case *genReturn:
			retMethod, err := i.getMethodStr(ctx, iter, "return")
			if err != nil {
				return nil, err
			}
			if retMethod == nil {
				awaited, aerr := i.doAwait(gs, rc.value)
				if aerr != nil {
					return nil, aerr
				}
				return nil, &genReturn{value: awaited}
			}
			done, value, err := step(retMethod, rc.value)
			if err != nil {
				return nil, err
			}
			if done {
				return nil, &genReturn{value: value}
			}
			asyncYield(value)
		case *Throw:
			throwMethod, err := i.getMethodStr(ctx, iter, "throw")
			if err != nil {
				return nil, err
			}
			if throwMethod == nil {
				// The iterator has no throw: close it with a normal completion,
				// then report the protocol violation as a TypeError (§15.5.5). An
				// abrupt result from return() propagates ahead of the TypeError.
				if cerr := i.asyncIteratorClose(ctx, gs, iter, nil); cerr != nil {
					return nil, cerr
				}
				return nil, i.throwError(ctx, "TypeError", "The iterator does not provide a throw method")
			}
			done, value, err := step(throwMethod, rc.Value)
			if err != nil {
				return nil, err
			}
			if done {
				return value, nil
			}
			asyncYield(value)
		default:
			// A host error (e.g. context cancellation) from a prior suspension.
			return nil, received
		}
	}
}

// genReturn unwinds a generator body when the consumer calls generator.return().
type genReturn struct{ value Value }

func (*genReturn) Error() string { return "generator return" }
