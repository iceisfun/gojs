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
func (i *Interpreter) startCoroutine(def *ast.FuncDef, closure *Environment, homeObj *Object, this Value, args []Value, arrow bool) (*generatorState, func(resumeMsg) yieldMsg, error) {
	gs := &generatorState{
		resume:   make(chan resumeMsg),
		out:      make(chan yieldMsg),
		asyncGen: def.Async && def.Generator,
		ctx:      i.ctx,
	}

	env := NewEnvironment(closure, true)
	// An async arrow inherits this/arguments lexically; a normal function/
	// generator establishes its own.
	if !arrow {
		env.setThis(this)
		if homeObj != nil {
			env.homeObj = homeObj
		}
	}
	env.gen = gs
	if err := i.bindParams(i.ctx, def.Params, args, env); err != nil {
		return nil, nil, err
	}
	// Establish the arguments object after binding parameters (so a mapped
	// arguments object can alias them); a parameter named "arguments" shadows it.
	if !arrow {
		if _, exists := env.vars["arguments"]; !exists {
			env.vars["arguments"] = &binding{value: i.makeArguments(args), mutable: true, initialized: true}
		}
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
			_, err := i.runGeneratorBody(gs.ctx, def.Body, env)
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
		select {
		case gs.resume <- msg:
		case <-gs.ctx.Done():
			gs.done = true
			return yieldMsg{done: true, err: gs.ctx.Err()}
		}
		select {
		case res := <-gs.out:
			if res.done {
				gs.done = true
			}
			return res
		case <-gs.ctx.Done():
			gs.done = true
			return yieldMsg{done: true, err: gs.ctx.Err()}
		}
	}
	return gs, advance, nil
}

// makeGenerator builds the generator-object factory: calling a generator
// function returns a fresh generator (iterator) object without running the body.
func (i *Interpreter) makeGenerator(def *ast.FuncDef, closure *Environment, homeObj *Object, this Value, args []Value) (Value, error) {
	gs, advance, err := i.startCoroutine(def, closure, homeObj, this, args, false)
	if err != nil {
		return nil, err
	}

	genObj := NewObject(i.generatorProto)
	genObj.class = "Generator"

	result := func(value Value, done bool) *Object {
		o := NewObject(i.objectProto)
		o.SetData("value", value)
		o.SetData("done", Bool(done))
		return o
	}
	step := func(msg resumeMsg) (Value, error) {
		res := advance(msg)
		if res.err != nil {
			return nil, res.err
		}
		return result(res.value, res.done), nil
	}

	i.defineMethod(genObj, "next", 1, func(ctx context.Context, _ Value, a []Value) (Value, error) {
		return step(resumeMsg{value: arg(a, 0), mode: resumeNext})
	})
	i.defineMethod(genObj, "return", 1, func(ctx context.Context, _ Value, a []Value) (Value, error) {
		if gs.done {
			return result(arg(a, 0), true), nil
		}
		return step(resumeMsg{value: arg(a, 0), mode: resumeReturn})
	})
	i.defineMethod(genObj, "throw", 1, func(ctx context.Context, _ Value, a []Value) (Value, error) {
		if gs.done {
			return nil, NewThrow(arg(a, 0))
		}
		return step(resumeMsg{value: arg(a, 0), mode: resumeThrow})
	})
	// A generator is its own iterator.
	genObj.defineOwn(SymKey(i.symIterator), &Property{
		Value:        i.newNativeFunc("[Symbol.iterator]", 0, func(ctx context.Context, this Value, a []Value) (Value, error) { return this, nil }),
		Writable:     true,
		Configurable: true,
	})

	return genObj, nil
}

// runGeneratorBody executes a generator body, hoisting declarations first. It is
// separate from runFunctionBody only so future generator-specific handling has a
// home.
func (i *Interpreter) runGeneratorBody(ctx context.Context, body *ast.BlockStmt, env *Environment) (Value, error) {
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
func (i *Interpreter) suspend(gs *generatorState, value Value, awaited bool) (Value, error) {
	select {
	case gs.out <- yieldMsg{value: value, done: false, awaited: awaited}:
	case <-gs.ctx.Done():
		return nil, gs.ctx.Err()
	}
	select {
	case msg := <-gs.resume:
		switch msg.mode {
		case resumeReturn:
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
	return i.suspend(gs, value, false)
}

// doAwait suspends for an awaited value; the async driver resolves it to a
// promise and resumes with the settled value (or a thrown rejection).
func (i *Interpreter) doAwait(gs *generatorState, value Value) (Value, error) {
	return i.suspend(gs, value, true)
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
	var last Value = Undef
	err = i.iterate(ctx, iterable, func(v Value) error {
		last = v
		_, yErr := i.doYield(gs, v)
		return yErr
	})
	if err != nil {
		return nil, err
	}
	return last, nil
}

// genReturn unwinds a generator body when the consumer calls generator.return().
type genReturn struct{ value Value }

func (*genReturn) Error() string { return "generator return" }
