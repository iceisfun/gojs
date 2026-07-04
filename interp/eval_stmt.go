package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements statement evaluation and declaration hoisting.

// execStmts runs a list of statements in env, returning the completion value of
// the last value-producing statement (used by the REPL) and the first error or
// control-flow signal.
func (i *Interpreter) execStmts(ctx context.Context, stmts []ast.Stmt, env *Environment) (Value, error) {
	// result tracks the StatementList completion value with nil == empty
	// (§14.2.2 UpdateEmpty). An empty list, or one whose statements all produce
	// empty completions (declarations, an empty statement, an empty nested
	// block), yields nil — NOT undefined — so an enclosing StatementList
	// preserves a preceding non-empty value. Callers that must surface a JS
	// value (eval/script, and the if/with/try constructs whose spec completion
	// is UpdateEmpty(C, undefined)) map a nil result to undefined via orUndef.
	var result Value = nil
	for _, s := range stmts {
		v, err := i.evalStmt(ctx, s, env)
		// Preserve an abrupt completion's value (e.g. a switch that ends in a
		// `continue` carries its completion value out through the enclosing
		// block) so callers can apply UpdateEmpty correctly.
		if v != nil {
			result = v
		}
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// hoistDeclarations performs hoisting for a statement list about to run in env.
// When fnLevel is true (function body or global), var and nested function-decl
// names are hoisted to this (function) scope. Lexical declarations (let/const/
// class) and this level's function declarations are always processed.
func (i *Interpreter) hoistDeclarations(ctx context.Context, stmts []ast.Stmt, env *Environment, fnLevel bool) error {
	// varNames holds the var/function names hoisted to this scope, used to
	// detect a var-vs-lexical collision in the same scope (an early error).
	varNames := map[string]bool{}
	if fnLevel {
		collectVarNames(stmts, varNames)
		for n := range varNames {
			i.declareVarBinding(env, n, nil)
		}
	}
	// lexNames tracks lexical (let/const/class) names declared directly in this
	// block so a duplicate lexical declaration is rejected.
	lexNames := map[string]bool{}
	declareLex := func(name string, mutable bool) error {
		if lexNames[name] || varNames[name] {
			return i.throwError(ctx, "SyntaxError", "Identifier '"+name+"' has already been declared")
		}
		lexNames[name] = true
		env.declareLexical(name, mutable)
		return nil
	}

	for _, s := range stmts {
		switch st := s.(type) {
		case *ast.FuncDecl:
			fn := i.makeFunction(st.Def, env, kindNormal, nil, false)
			name := ""
			if st.Def.Name != nil {
				name = st.Def.Name.Name
			}
			// A global-scope function declaration becomes a globalThis property.
			if env == i.globalEnv {
				i.defineGlobalFunction(name, fn, false)
			} else {
				env.vars[name] = &binding{value: fn, mutable: true, initialized: true}
			}
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					var derr error
					forEachPatternName(d.Target, func(n string) {
						if derr == nil {
							derr = declareLex(n, st.Kind == token.LET)
						}
					})
					if derr != nil {
						return derr
					}
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				if err := declareLex(st.Def.Name.Name, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// declareVarBinding creates a hoisted var binding, routing global-scope vars
// onto the global object (so they become globalThis properties) rather than the
// declarative environment.
func (i *Interpreter) declareVarBinding(env *Environment, name string, v Value) {
	if env == i.globalEnv {
		i.defineGlobalVar(name, v, false)
		return
	}
	env.declareVar(name, v)
}

// defineGlobalVar creates or updates a global `var`/function binding as a
// property of the global object (CreateGlobalVarBinding, §9.1.1.4.17). A
// top-level script var is non-configurable (configurable=false, so `delete x`
// is a no-op that returns false); a var introduced by eval is configurable
// (configurable=true). Either way the property is writable and enumerable. An
// existing property (a prior var, or a built-in) keeps its descriptor and only
// takes the new value.
func (i *Interpreter) defineGlobalVar(name string, v Value, configurable bool) {
	key := StrKey(name)
	if p, ok := i.global.props[key]; ok {
		if v != nil && !p.Accessor {
			p.Value = v
		}
		return
	}
	init := Value(Undef)
	if v != nil {
		init = v
	}
	i.global.defineOwn(key, &Property{Value: init, Writable: true, Enumerable: true, Configurable: configurable})
}

// defineGlobalFunction implements CreateGlobalFunctionBinding (§9.1.1.4.18) for a
// top-level global function declaration. Unlike a plain var, an absent or
// *configurable* existing property is redefined to a fresh {writable,
// enumerable, non-configurable} data property; a non-configurable existing
// property (a prior global function/var) keeps its descriptor and only takes the
// new value. The name joins the global [[VarNames]] list. configurable is the D
// argument: false for a top-level script function declaration, true for one
// introduced by eval.
func (i *Interpreter) defineGlobalFunction(name string, fn Value, configurable bool) {
	key := StrKey(name)
	if p, ok := i.global.getOwn(key); ok && !p.Configurable {
		if !p.Accessor {
			p.Value = fn
		}
		return
	}
	i.global.defineOwn(key, &Property{Value: fn, Writable: true, Enumerable: true, Configurable: configurable})
}

// checkGlobalDeclarations performs the early-error phase of
// GlobalDeclarationInstantiation (§16.1.7) for a Script about to run at global
// scope. Every top-level declaration is validated BEFORE any binding is created,
// so a rejected script (e.g. one passed to $262.evalScript) leaves the global
// environment untouched. It raises a SyntaxError when a lexical name collides
// with an existing var/lexical/restricted-global binding (or a var name collides
// with an existing lexical), and a TypeError when the global object cannot accept
// a new function/var (an incompatible non-configurable property, or a fresh name
// on a non-extensible global).
func (i *Interpreter) checkGlobalDeclarations(ctx context.Context, stmts []ast.Stmt) error {
	varDeclNames := map[string]bool{} // VariableDeclaration/ForBinding names (excludes functions)
	collectVarNames(stmts, varDeclNames)
	lexNames := map[string]bool{}
	funcNames := map[string]bool{}
	for _, s := range stmts {
		switch st := s.(type) {
		case *ast.FuncDecl:
			if st.Def.Name != nil {
				funcNames[st.Def.Name.Name] = true
			}
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					forEachPatternName(d.Target, func(n string) { lexNames[n] = true })
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				lexNames[st.Def.Name.Name] = true
			}
		}
	}

	dup := func(name string) error {
		return i.throwError(ctx, "SyntaxError", "Identifier '"+name+"' has already been declared")
	}
	// A lexical name must not clash with an existing lexical declaration or a
	// non-configurable ("restricted") global property. Note the spec relies on
	// HasRestrictedGlobalProperty rather than HasVarDeclaration here: ordinary
	// global var/function bindings are non-configurable (hence restricted, and
	// blocked), but a var introduced by a non-strict direct eval is configurable
	// — so a later lexical declaration is permitted to shadow it.
	for name := range lexNames {
		if i.hasLexicalDeclaration(name) || i.hasRestrictedGlobalProperty(name) {
			return dup(name)
		}
	}
	// A var-declared name (including a function) must not clash with an existing
	// lexical declaration.
	for name := range varDeclNames {
		if i.hasLexicalDeclaration(name) {
			return dup(name)
		}
	}
	for name := range funcNames {
		if i.hasLexicalDeclaration(name) {
			return dup(name)
		}
	}
	// The global object must be able to accept each new function/var binding.
	for name := range funcNames {
		if !i.canDeclareGlobalFunction(name) {
			return i.throwError(ctx, "TypeError", "Cannot declare global function '"+name+"'")
		}
	}
	for name := range varDeclNames {
		if funcNames[name] {
			continue
		}
		if !i.canDeclareGlobalVar(name) {
			return i.throwError(ctx, "TypeError", "Cannot declare global variable '"+name+"'")
		}
	}
	return nil
}

// hasLexicalDeclaration reports whether the global lexical environment already
// binds name (a let/const/class binding lives in globalEnv, whereas var and
// function bindings live on the global object).
func (i *Interpreter) hasLexicalDeclaration(name string) bool {
	b, ok := i.globalEnv.vars[name]
	return ok && b != nil && b.lexical
}

// hasRestrictedGlobalProperty implements HasRestrictedGlobalProperty
// (§9.1.1.4.14): name is a non-configurable own property of the global object.
func (i *Interpreter) hasRestrictedGlobalProperty(name string) bool {
	p, ok := i.global.getOwn(StrKey(name))
	return ok && !p.Configurable
}

// canDeclareGlobalVar implements CanDeclareGlobalVar (§9.1.1.4.15).
func (i *Interpreter) canDeclareGlobalVar(name string) bool {
	if i.global.HasOwn(StrKey(name)) {
		return true
	}
	return i.global.extensible
}

// canDeclareGlobalFunction implements CanDeclareGlobalFunction (§9.1.1.4.16).
func (i *Interpreter) canDeclareGlobalFunction(name string) bool {
	p, ok := i.global.getOwn(StrKey(name))
	if !ok {
		return i.global.extensible
	}
	if p.Configurable {
		return true
	}
	return !p.Accessor && p.Writable && p.Enumerable
}

// evalStmt evaluates a single statement.
func (i *Interpreter) evalStmt(ctx context.Context, stmt ast.Stmt, env *Environment) (Value, error) {
	if err := i.checkContext(); err != nil {
		return nil, err
	}
	if err := i.step(); err != nil {
		return nil, err
	}
	// Record the position of the statement being executed, so an error
	// constructed here can report a source frame (mapped back through a
	// SourceMapper for transpiled code). The position updates the innermost
	// active call frame, or the module position when at top level.
	if p := stmt.Pos(); p.Line > 0 {
		if n := len(i.callStack); n > 0 {
			i.callStack[n-1].pos = p
		} else {
			i.curPos = p
		}
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return i.evalExpr(ctx, s.X, env)
	case *ast.EmptyStmt, *ast.DebuggerStmt:
		// Both produce NormalCompletion(empty) (§14.4.1, §14.16.1), so they must
		// not overwrite a preceding statement's completion value — the value of
		// `eval('2;;')` is 2. Return nil (empty) rather than undefined.
		return nil, nil
	case *ast.VarDecl:
		// A VariableStatement and a LexicalDeclaration both produce an empty
		// completion (ECMA-262 §14.3.2.1, §14.3.1.1), so they must not overwrite
		// the completion value of a preceding statement — e.g. the value of
		// `eval('7; let x;')` is 7. Return nil (empty) rather than undefined.
		return nil, i.evalVarDecl(ctx, s, env)
	case *ast.FuncDecl:
		return nil, nil // already bound during hoisting; empty completion
	case *ast.ClassDecl:
		cls, err := i.evalClass(ctx, s.Def, env, "")
		if err != nil {
			return nil, err
		}
		if s.Def.Name != nil {
			i.assignBinding(env, s.Def.Name.Name, cls)
		}
		// A ClassDeclaration produces an empty completion (§15.7), so it must not
		// overwrite a preceding statement's value — `eval('1; class C {}')` is 1.
		return nil, nil
	case *ast.BlockStmt:
		return i.evalBlock(ctx, s, env)
	case *ast.IfStmt:
		return i.evalIf(ctx, s, env)
	case *ast.WhileStmt:
		return i.evalWhile(ctx, s, env)
	case *ast.WithStmt:
		return i.evalWith(ctx, s, env)
	case *ast.DoWhileStmt:
		return i.evalDoWhile(ctx, s, env)
	case *ast.ForStmt:
		return i.evalFor(ctx, s, env)
	case *ast.ForInStmt:
		return i.evalForIn(ctx, s, env)
	case *ast.ReturnStmt:
		return i.evalReturn(ctx, s, env)
	case *ast.BreakStmt:
		label := ""
		if s.Label != nil {
			label = s.Label.Name
		}
		return nil, &breakSignal{label: label}
	case *ast.ContinueStmt:
		label := ""
		if s.Label != nil {
			label = s.Label.Name
		}
		return nil, &continueSignal{label: label}
	case *ast.ThrowStmt:
		v, err := i.evalExpr(ctx, s.Argument, env)
		if err != nil {
			return nil, err
		}
		return nil, NewThrow(v)
	case *ast.TryStmt:
		return i.evalTry(ctx, s, env)
	case *ast.SwitchStmt:
		return i.evalSwitch(ctx, s, env)
	case *ast.LabeledStmt:
		return i.evalLabeled(ctx, s, env)
	default:
		return nil, i.throwError(ctx, "SyntaxError", "unsupported statement")
	}
}

// evalBlock runs a block in a fresh lexical scope.
func (i *Interpreter) evalBlock(ctx context.Context, block *ast.BlockStmt, env *Environment) (Value, error) {
	scope := NewEnvironment(env, false)
	if err := i.hoistDeclarations(ctx, block.Body, scope, false); err != nil {
		return nil, err
	}
	return i.execStmts(ctx, block.Body, scope)
}

// evalWith evaluates a `with` statement (§14.11). Its object becomes an object
// environment record whose binding object is consulted (via [[HasProperty]] and
// @@unscopables) for identifier resolution inside the body.
func (i *Interpreter) evalWith(ctx context.Context, s *ast.WithStmt, env *Environment) (Value, error) {
	objV, err := i.evalExpr(ctx, s.Object, env)
	if err != nil {
		return nil, err
	}
	obj, err := i.ToObject(ctx, objV)
	if err != nil {
		return nil, err
	}
	scope := NewEnvironment(env, false)
	scope.withObj = obj
	// A block body introduces its own inner declarative scope (child of the
	// object environment record) so its lexical declarations shadow the
	// with-object; a single-statement body runs directly in the object scope.
	// WithStatement completion is UpdateEmpty(C, undefined) (§14.11.7): an empty
	// body completion surfaces as undefined.
	var v Value
	if block, ok := s.Body.(*ast.BlockStmt); ok {
		v, err = i.evalBlock(ctx, block, scope)
	} else {
		v, err = i.evalStmt(ctx, s.Body, scope)
	}
	// UpdateEmpty(C, undefined) applies to normal and abrupt completions alike.
	return orUndef(v), err
}

// withHasBinding implements the object environment record HasBinding
// (§9.1.1.2): the binding object must have the property (via [[HasProperty]],
// which runs a Proxy's has trap) and it must not be hidden by the object's
// @@unscopables. It returns the binding object and true when name is bound.
func (i *Interpreter) withHasBinding(ctx context.Context, obj *Object, name string) (*Object, bool, error) {
	key := StrKey(name)
	has, err := i.hasV(ctx, obj, key)
	if err != nil {
		return nil, false, err
	}
	if !has {
		return nil, false, nil
	}
	unV, err := i.getV(ctx, obj, SymKey(i.symUnscopables), obj)
	if err != nil {
		return nil, false, err
	}
	if un, ok := unV.(*Object); ok {
		blockedV, err := i.getV(ctx, un, key, un)
		if err != nil {
			return nil, false, err
		}
		if ToBoolean(blockedV) {
			return nil, false, nil
		}
	}
	return obj, true, nil
}

// withGetBindingValue implements the object environment record GetBindingValue
// (§9.1.1.2.6). HasBinding has already matched, but GetBindingValue performs its
// own [[HasProperty]] re-check first: a Proxy binding object, or a side effect
// during the @@unscopables lookup, may have removed the property in between. If
// it is gone, a strict reference throws a ReferenceError and a non-strict one
// yields undefined; otherwise the value is read via [[Get]].
func (i *Interpreter) withGetBindingValue(ctx context.Context, obj *Object, name string, strict bool) (Value, error) {
	key := StrKey(name)
	has, err := i.hasV(ctx, obj, key)
	if err != nil {
		return nil, err
	}
	if !has {
		if strict {
			return nil, i.throwError(ctx, "ReferenceError", name+" is not defined")
		}
		return Undef, nil
	}
	return i.getV(ctx, obj, key, obj)
}

// withSetMutableBinding implements the object environment record
// SetMutableBinding (§9.1.1.2.5): re-check [[HasProperty]] (a strict reference to
// a binding that vanished throws a ReferenceError) before writing via [[Set]].
func (i *Interpreter) withSetMutableBinding(ctx context.Context, obj *Object, name string, value Value, strict bool) error {
	key := StrKey(name)
	has, err := i.hasV(ctx, obj, key)
	if err != nil {
		return err
	}
	if !has && strict {
		return i.throwError(ctx, "ReferenceError", name+" is not defined")
	}
	wrote, err := obj.setStatus(ctx, key, value)
	if err != nil {
		return err
	}
	if !wrote && strict {
		return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+name)
	}
	return nil
}

// evalVarDecl evaluates a var/let/const declaration, assigning initializers.
func (i *Interpreter) evalVarDecl(ctx context.Context, decl *ast.VarDecl, env *Environment) error {
	for _, d := range decl.Decls {
		// A simple `var x [= init]` assigns via PutValue(ResolveBinding(x), value)
		// (§14.3.2.1): ResolveBinding runs BEFORE the initializer, so an enclosing
		// `with` object that owns x at resolve time receives the write — even if
		// the initializer (e.g. `delete obj.x`) removes that property in between.
		if id, ok := d.Target.(*ast.Ident); ok && decl.Kind == token.VAR {
			if d.Init == nil {
				continue // bare `var x;` keeps any existing hoisted value
			}
			base, err := i.identWithBase(ctx, id.Name, env)
			if err != nil {
				return err
			}
			value, err := i.evalExprNamed(ctx, d.Init, env, id.Name)
			if err != nil {
				return err
			}
			if obj, ok := base.(*Object); ok {
				if err := i.withSetMutableBinding(ctx, obj, id.Name, value, env.isStrict()); err != nil {
					return err
				}
			} else if err := i.assignIdent(ctx, id.Name, value, env); err != nil {
				return err
			}
			continue
		}
		var value Value = Undef
		if d.Init != nil {
			v, err := i.evalExprNamed(ctx, d.Init, env, bindingName(d.Target))
			if err != nil {
				return err
			}
			value = v
		}
		bind := func(name string, v Value) {
			switch decl.Kind {
			case token.VAR:
				// Global-scope vars live on the global object; others assign
				// into their pre-hoisted binding.
				if env == i.globalEnv {
					i.defineGlobalVar(name, v, false)
				} else if b := env.lookup(name); b != nil {
					b.value = v
					b.initialized = true
				} else {
					env.declareVar(name, v)
				}
			default: // let / const
				if b, ok := env.vars[name]; ok {
					b.value = v
					b.initialized = true
				} else {
					env.vars[name] = &binding{value: v, mutable: decl.Kind == token.LET, initialized: true}
				}
			}
		}
		if d.Init == nil {
			// A `var x;` with no initializer must NOT reset an existing hoisted
			// binding (var x = 1; var x; leaves x === 1). let/const still need a
			// live binding initialized to undefined.
			if decl.Kind != token.VAR {
				forEachPatternName(d.Target, func(n string) { bind(n, Undef) })
			}
			continue
		}
		if err := i.bindPattern(ctx, d.Target, value, env, bind); err != nil {
			return err
		}
	}
	return nil
}

// evalIf evaluates an if/else statement.
func (i *Interpreter) evalIf(ctx context.Context, s *ast.IfStmt, env *Environment) (Value, error) {
	test, err := i.evalExpr(ctx, s.Test, env)
	if err != nil {
		return nil, err
	}
	// IfStatement completion is UpdateEmpty(stmtCompletion, undefined)
	// (§14.6.2), applied to normal AND abrupt completions: a taken branch that
	// completes empty (e.g. `if(x){}` or `if(x) break;`, where the break carries
	// an empty value) yields undefined, and an untaken if with no else yields
	// undefined. orUndef maps an empty (nil) completion value to undefined while
	// preserving the control-flow signal in err.
	if ToBoolean(test) {
		v, err := i.evalStmt(ctx, s.Consequent, env)
		return orUndef(v), err
	}
	if s.Alternate != nil {
		v, err := i.evalStmt(ctx, s.Alternate, env)
		return orUndef(v), err
	}
	return Undef, nil
}

// evalReturn evaluates a return statement, producing a return signal.
func (i *Interpreter) evalReturn(ctx context.Context, s *ast.ReturnStmt, env *Environment) (Value, error) {
	var v Value = Undef
	if s.Argument != nil {
		val, err := i.evalExpr(ctx, s.Argument, env)
		if err != nil {
			return nil, err
		}
		v = val
		// `return Expression` in an async generator awaits the value (§13.10.1
		// step 3, GetGeneratorKind() is async), which costs one microtask tick.
		// A bare `return;` and an implicit fall-off return carry no expression and
		// are not awaited.
		if gs := env.generator(); gs != nil && gs.asyncGen {
			awaited, err := i.doAwait(gs, v)
			if err != nil {
				return nil, err
			}
			v = awaited
		}
	}
	return nil, &returnSignal{value: v}
}

// evalLabeled evaluates a labeled statement. When the label decorates a loop,
// the label is threaded into the loop so that `break label` / `continue label`
// target it directly. For any other labeled statement, only `break label` is
// meaningful and is caught here.
func (i *Interpreter) evalLabeled(ctx context.Context, s *ast.LabeledStmt, env *Environment) (Value, error) {
	label := s.Label.Name
	var v Value
	var err error
	switch body := s.Body.(type) {
	case *ast.WhileStmt:
		v, err = i.runWhile(ctx, body, env, label)
	case *ast.DoWhileStmt:
		v, err = i.runDoWhile(ctx, body, env, label)
	case *ast.ForStmt:
		v, err = i.runFor(ctx, body, env, label)
	case *ast.ForInStmt:
		v, err = i.runForIn(ctx, body, env, label)
	default:
		v, err = i.evalStmt(ctx, s.Body, env)
	}
	if err != nil {
		if b, ok := err.(*breakSignal); ok && b.label == label {
			// §14.13.4 step 4a: a break targeting this label yields
			// NormalCompletion(stmtResult.[[Value]]). gojs's break carries an empty
			// value and execStmts hands out the LabelledItem's accumulated value as
			// v (already UpdateEmpty-folded), so v is that completion value; an
			// empty (nil) value becomes undefined.
			if v != nil {
				return v, nil
			}
			return Undef, nil
		}
		return v, err
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Hoisting helpers
// ---------------------------------------------------------------------------

// collectVarNames gathers all var-declared names within stmts, descending into
// nested statements but not into nested function or class bodies.
func collectVarNames(stmts []ast.Stmt, into map[string]bool) {
	for _, s := range stmts {
		collectVarNamesStmt(s, into)
	}
}

// collectTopLevelFuncNames records the names of function declarations that
// appear directly in a statement list (eval/script top level). These are
// var-scoped (hoisted like `var`), so they participate in the
// EvalDeclarationInstantiation var-hoisting early error alongside var names.
func collectTopLevelFuncNames(stmts []ast.Stmt, into map[string]bool) {
	for _, s := range stmts {
		if fd, ok := s.(*ast.FuncDecl); ok && fd.Def.Name != nil {
			into[fd.Def.Name.Name] = true
		}
	}
}

func collectVarNamesStmt(s ast.Stmt, into map[string]bool) {
	switch st := s.(type) {
	case *ast.VarDecl:
		if st.Kind == token.VAR {
			for _, d := range st.Decls {
				forEachPatternName(d.Target, func(n string) { into[n] = true })
			}
		}
	case *ast.BlockStmt:
		collectVarNames(st.Body, into)
	case *ast.IfStmt:
		collectVarNamesStmt(st.Consequent, into)
		if st.Alternate != nil {
			collectVarNamesStmt(st.Alternate, into)
		}
	case *ast.ForStmt:
		if vd, ok := st.Init.(*ast.VarDecl); ok {
			collectVarNamesStmt(vd, into)
		}
		collectVarNamesStmt(st.Body, into)
	case *ast.ForInStmt:
		if vd, ok := st.Left.(*ast.VarDecl); ok {
			collectVarNamesStmt(vd, into)
		}
		collectVarNamesStmt(st.Body, into)
	case *ast.WhileStmt:
		collectVarNamesStmt(st.Body, into)
	case *ast.WithStmt:
		collectVarNamesStmt(st.Body, into)
	case *ast.DoWhileStmt:
		collectVarNamesStmt(st.Body, into)
	case *ast.TryStmt:
		collectVarNames(st.Block.Body, into)
		if st.Handler != nil {
			collectVarNames(st.Handler.Body.Body, into)
		}
		if st.Finalizer != nil {
			collectVarNames(st.Finalizer.Body, into)
		}
	case *ast.SwitchStmt:
		for _, c := range st.Cases {
			collectVarNames(c.Body, into)
		}
	case *ast.LabeledStmt:
		collectVarNamesStmt(st.Body, into)
	}
}

// forEachPatternName invokes fn for each identifier name bound by a binding
// target (identifier or destructuring pattern).
func forEachPatternName(target ast.Expr, fn func(string)) {
	switch t := target.(type) {
	case *ast.Ident:
		fn(t.Name)
	case *ast.AssignPattern:
		forEachPatternName(t.Target, fn)
	case *ast.RestElement:
		forEachPatternName(t.Target, fn)
	case *ast.ArrayLit:
		for _, el := range t.Elements {
			if el != nil {
				forEachPatternName(el, fn)
			}
		}
	case *ast.ObjectLit:
		for _, p := range t.Properties {
			if p.Value != nil {
				forEachPatternName(p.Value, fn)
			} else {
				forEachPatternName(p.Key, fn)
			}
		}
	case *ast.SpreadElement:
		forEachPatternName(t.Argument, fn)
	}
}

// bindingName returns the simple name of a binding target, or "" for patterns.
// Used to give anonymous function/class expressions an inferred name.
func bindingName(target ast.Expr) string {
	if id, ok := target.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
