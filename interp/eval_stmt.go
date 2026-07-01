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
	var result Value = Undef
	for _, s := range stmts {
		v, err := i.evalStmt(ctx, s, env)
		if err != nil {
			return result, err
		}
		if v != nil {
			result = v
		}
	}
	return result, nil
}

// hoistDeclarations performs hoisting for a statement list about to run in env.
// When fnLevel is true (function body or global), var and nested function-decl
// names are hoisted to this (function) scope. Lexical declarations (let/const/
// class) and this level's function declarations are always processed.
func (i *Interpreter) hoistDeclarations(ctx context.Context, stmts []ast.Stmt, env *Environment, fnLevel bool) {
	if fnLevel {
		names := map[string]bool{}
		collectVarNames(stmts, names)
		for n := range names {
			env.declareVar(n, nil)
		}
	}
	for _, s := range stmts {
		switch st := s.(type) {
		case *ast.FuncDecl:
			fn := i.makeFunction(st.Def, env, kindNormal, nil)
			name := ""
			if st.Def.Name != nil {
				name = st.Def.Name.Name
			}
			env.vars[name] = &binding{value: fn, mutable: true, initialized: true}
		case *ast.VarDecl:
			if st.Kind == token.LET || st.Kind == token.CONST {
				for _, d := range st.Decls {
					forEachPatternName(d.Target, func(n string) {
						env.declareLexical(n, st.Kind == token.LET)
					})
				}
			}
		case *ast.ClassDecl:
			if st.Def.Name != nil {
				env.declareLexical(st.Def.Name.Name, true)
			}
		}
	}
}

// evalStmt evaluates a single statement.
func (i *Interpreter) evalStmt(ctx context.Context, stmt ast.Stmt, env *Environment) (Value, error) {
	if err := i.checkContext(); err != nil {
		return nil, err
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return i.evalExpr(ctx, s.X, env)
	case *ast.EmptyStmt, *ast.DebuggerStmt:
		return Undef, nil
	case *ast.VarDecl:
		return Undef, i.evalVarDecl(ctx, s, env)
	case *ast.FuncDecl:
		return Undef, nil // already bound during hoisting
	case *ast.ClassDecl:
		cls, err := i.evalClass(ctx, s.Def, env)
		if err != nil {
			return nil, err
		}
		if s.Def.Name != nil {
			i.assignBinding(env, s.Def.Name.Name, cls)
		}
		return Undef, nil
	case *ast.BlockStmt:
		return i.evalBlock(ctx, s, env)
	case *ast.IfStmt:
		return i.evalIf(ctx, s, env)
	case *ast.WhileStmt:
		return i.evalWhile(ctx, s, env)
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
	i.hoistDeclarations(ctx, block.Body, scope, false)
	return i.execStmts(ctx, block.Body, scope)
}

// evalVarDecl evaluates a var/let/const declaration, assigning initializers.
func (i *Interpreter) evalVarDecl(ctx context.Context, decl *ast.VarDecl, env *Environment) error {
	for _, d := range decl.Decls {
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
				// Assign into the pre-hoisted var binding.
				if b := env.lookup(name); b != nil {
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
		if d.Init == nil && decl.Kind != token.VAR {
			// Declared but uninitialized let/const still needs a live binding.
			forEachPatternName(d.Target, func(n string) { bind(n, Undef) })
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
	if ToBoolean(test) {
		return i.evalStmt(ctx, s.Consequent, env)
	}
	if s.Alternate != nil {
		return i.evalStmt(ctx, s.Alternate, env)
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
