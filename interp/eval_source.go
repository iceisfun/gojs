package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/parser"
	"github.com/iceisfun/gojs/token"
)

// evalSource implements the global eval function: an *indirect* eval (the callee
// is not the identifier `eval`). It parses a string of source text and runs it
// with the global environment as its VariableEnvironment and a fresh
// declarative environment for its lexical declarations (§19.2.1.1 PerformEval
// with direct = false), returning the completion value. Non-string arguments are
// returned unchanged, per spec.
//
// When Security.DisableEval is set, eval throws instead of executing — an
// explicit, observable refusal for locked-down embeddings.
func (i *Interpreter) evalSource(ctx context.Context, code Value) (Value, error) {
	// A rope (produced by string concatenation) is still a String value; flatten
	// it before the "if Type(x) is not String, return x" check (§19.2.1.1 step 2),
	// otherwise `eval("a" + "b")` would return the unevaluated source string.
	code = flattenRope(code)
	str, ok := code.(String)
	if !ok {
		return code, nil
	}
	if i.security.DisableEval {
		return nil, i.throwError(ctx, "EvalError", "eval is disabled in this sandbox")
	}

	// Indirect eval runs in the global scope: super, super property, and
	// new.target are all invalid, so parse with an empty context. Its strictness
	// comes solely from its own Directive Prologue (§19.2.1.1).
	prog, err := parser.ParseEval("<eval>", string(str), parser.EvalContext{})
	if err != nil {
		// A parse failure in eval surfaces as a SyntaxError thrown value.
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	// PerformEval env setup: lexEnv is a fresh declarative environment whose outer
	// scope is the global environment; varEnv is the global environment, except in
	// a strict eval, where var/function declarations are confined to lexEnv so they
	// cannot leak into the global scope.
	lexEnv := NewEnvironment(i.globalEnv, false)
	lexEnv.strict = prog.Strict
	varEnv := i.globalEnv
	if prog.Strict {
		varEnv = lexEnv
	}
	if err := i.evalDeclarationInstantiation(ctx, prog.Body, varEnv, lexEnv, prog.Strict); err != nil {
		return nil, err
	}
	v, err := i.execStmts(ctx, prog.Body, lexEnv)
	if err != nil {
		return nil, err
	}
	// PerformEval / ScriptEvaluation: an empty completion value is undefined.
	return orUndef(v), nil
}

// directEval implements a direct call to eval (the callee is the identifier
// `eval` resolving to the %eval% intrinsic). Unlike indirect eval, the code runs
// in the caller's lexical context: this, super, new.target, and private names
// all resolve as in the surrounding code (§19.2.1.1 PerformEval with direct =
// true). A non-strict direct eval shares the caller's VariableEnvironment, so
// its var/function declarations hoist into the calling function (or global)
// scope; a strict direct eval confines them to its own environment.
func (i *Interpreter) directEval(ctx context.Context, code Value, env *Environment) (Value, error) {
	// See evalSource: a concatenation rope is a String; flatten before the
	// non-String short-circuit so direct eval evaluates it.
	code = flattenRope(code)
	str, ok := code.(String)
	if !ok {
		return code, nil
	}
	if i.security.DisableEval {
		return nil, i.throwError(ctx, "EvalError", "eval is disabled in this sandbox")
	}

	prog, err := parser.ParseEval("<eval>", string(str), parser.EvalContext{
		// A direct eval inherits the strictness of the calling context, so
		// strict-only early errors (e.g. a `with` statement) fire on its code.
		Strict:             env.isStrict(),
		AllowSuperCall:     env.inDerivedConstructor(),
		AllowSuperProperty: env.homeObject() != nil,
		AllowNewTarget:     env.hasNewTargetBinding(),
		InFieldInitializer: env.inFieldInitializer(),
		PrivateNames:       env.privateNamesInScope(),
	})
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	strict := env.isStrict() || prog.Strict

	// EvalDeclarationInstantiation early error: when this direct eval runs while a
	// function's parameter default is being evaluated, its VariableEnvironment is
	// the enclosing scope, not the parameter environment. A var/function
	// declaration in the eval whose name is already bound in that parameter
	// environment (e.g. `eval("var arguments")` inside `f(p = eval(...))`, where
	// the parameter environment holds the arguments object or a parameter named
	// "arguments") may not hoist over it — a SyntaxError (§19.2.1.3). Strict eval
	// declares in its own scope, so the rule does not apply.
	if !strict && i.paramDefaultEnv != nil {
		names := map[string]bool{}
		collectVarNames(prog.Body, names)
		collectTopLevelFuncNames(prog.Body, names)
		for name := range names {
			if _, bound := i.paramDefaultEnv.vars[name]; bound {
				return nil, i.throwError(ctx, "SyntaxError",
					"Identifier '"+name+"' has already been declared in the parameter scope")
			}
		}
	}

	// PerformEval env setup: lexEnv is a fresh declarative environment chained to
	// the caller's scope (holding the eval's own let/const/class bindings, with
	// this/super/#private resolving up the parent chain); varEnv is the caller's
	// VariableEnvironment for a non-strict eval, or lexEnv itself for a strict one.
	lexEnv := NewEnvironment(env, false)
	lexEnv.strict = strict
	varEnv := env.functionScope()
	if strict {
		varEnv = lexEnv
	}
	if err := i.evalDeclarationInstantiation(ctx, prog.Body, varEnv, lexEnv, strict); err != nil {
		return nil, err
	}
	v, err := i.execStmts(ctx, prog.Body, lexEnv)
	if err != nil {
		return nil, err
	}
	// PerformEval / ScriptEvaluation: an empty completion value is undefined.
	return orUndef(v), nil
}

// evalDeclarationInstantiation implements EvalDeclarationInstantiation
// (§19.2.1.3) for a body about to run in lexEnv with VariableEnvironment varEnv.
//
// var/function declarations hoist into varEnv (the global object when varEnv is
// the global environment, a fresh eval scope for a strict eval, or the caller's
// function scope for a non-strict direct eval). Lexical declarations
// (let/const/class) are instantiated (uninitialized) in lexEnv. All can-declare
// validation runs before any binding is created, so a rejected eval leaves the
// target environments untouched.
func (i *Interpreter) evalDeclarationInstantiation(ctx context.Context, stmts []ast.Stmt, varEnv, lexEnv *Environment, strict bool) error {
	global := varEnv == i.globalEnv

	// variableNames = VarDeclaredNames of the body (var names plus top-level
	// function-declaration names), used for the var/lexical hoisting conflict
	// checks below.
	variableNames := map[string]bool{}
	collectVarNames(stmts, variableNames)
	collectTopLevelFuncNames(stmts, variableNames)

	// Step 5 (strict is false): a var may not shadow a like-named lexical binding.
	if !strict {
		// At global scope, `eval` will not create a global var that a global
		// lexical declaration would shadow.
		if global {
			for name := range variableNames {
				if i.hasLexicalDeclaration(name) {
					return i.throwError(ctx, "SyntaxError", "Identifier '"+name+"' has already been declared")
				}
			}
		}
		// A direct eval will not hoist a var over a like-named lexical declaration
		// in an intervening scope: walk from lexEnv up to (but not including)
		// varEnv. Object (`with`) environments hold no lexical bindings and are
		// skipped; every declarative environment in this range is a block/catch
		// scope whose bindings are lexical, so any collision is an early error.
		for e := lexEnv; e != nil && e != varEnv; e = e.parent {
			if e.withObj != nil {
				continue
			}
			for name := range variableNames {
				if _, bound := e.vars[name]; bound {
					return i.throwError(ctx, "SyntaxError", "Identifier '"+name+"' has already been declared")
				}
			}
		}
		// The VariableEnvironment itself may hold a top-level lexical binding: a
		// non-strict function keeps its body's let/const/class in a lexical
		// Environment Record layered on the variable environment (§10.2.11 step
		// "lexEnv = NewDeclarativeEnvironment(varEnv)"), but gojs stores both in the
		// one function-scope record, distinguished by binding.lexical. A hoisted
		// var may not shadow such a lexical binding (`function f(){ let x;
		// eval('var x'); }`). The global scope's lexical names are handled above.
		if !global {
			for name := range variableNames {
				if b, bound := varEnv.vars[name]; bound && b.lexical {
					return i.throwError(ctx, "SyntaxError", "Identifier '"+name+"' has already been declared")
				}
			}
		}
	}

	// funcsToInitialize: the top-level function declarations, deduplicated so that
	// the *last* declaration of each name survives, in source order (§19.2.1.3
	// step 8, reverse iteration inserting first). declaredFuncNames records those
	// surviving names.
	type funcEntry struct {
		name string
		decl *ast.FuncDecl
	}
	var topFuncs []funcEntry
	lastFunc := map[string]int{}
	for _, s := range stmts {
		if fd, ok := s.(*ast.FuncDecl); ok && fd.Def.Name != nil {
			name := fd.Def.Name.Name
			lastFunc[name] = len(topFuncs)
			topFuncs = append(topFuncs, funcEntry{name, fd})
		}
	}
	var funcsToInit []funcEntry
	declaredFuncNames := map[string]bool{}
	for idx, fe := range topFuncs {
		if lastFunc[fe.name] != idx {
			continue // superseded by a later declaration of the same name
		}
		declaredFuncNames[fe.name] = true
		funcsToInit = append(funcsToInit, fe)
	}

	// declaredVarNames: var/ForBinding names that are not also function names.
	varOnly := map[string]bool{}
	collectVarNames(stmts, varOnly)
	var declaredVarNames []string
	seenVar := map[string]bool{}
	for name := range varOnly {
		if declaredFuncNames[name] || seenVar[name] {
			continue
		}
		seenVar[name] = true
		declaredVarNames = append(declaredVarNames, name)
	}

	// Validation phase (§19.2.1.3 steps 8 and 10): at global scope every new
	// function/var must be definable on the global object. No binding is created
	// until every check has passed.
	if global {
		for name := range declaredFuncNames {
			if !i.canDeclareGlobalFunction(name) {
				return i.throwError(ctx, "TypeError", "Cannot declare global function '"+name+"'")
			}
		}
		for _, name := range declaredVarNames {
			if !i.canDeclareGlobalVar(name) {
				return i.throwError(ctx, "TypeError", "Cannot declare global variable '"+name+"'")
			}
		}
	}

	// Instantiate lexical declarations (uninitialized) in lexEnv. A duplicate
	// lexical name, or a lexical name colliding with a var in the same body, is an
	// early error.
	lexNames := map[string]bool{}
	declareLex := func(name string, mutable bool) error {
		if lexNames[name] || variableNames[name] {
			return i.throwError(ctx, "SyntaxError", "Identifier '"+name+"' has already been declared")
		}
		lexNames[name] = true
		lexEnv.declareLexical(name, mutable)
		return nil
	}
	for _, s := range stmts {
		switch st := s.(type) {
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

	// Instantiate function declarations, binding each to a function object closed
	// over lexEnv.
	for _, fe := range funcsToInit {
		fn := i.makeFunction(fe.decl.Def, lexEnv, kindNormal, nil, false)
		if global {
			i.defineGlobalFunction(fe.name, fn, true)
			continue
		}
		if b, ok := varEnv.vars[fe.name]; ok {
			b.value = fn
			b.initialized = true
		} else {
			varEnv.vars[fe.name] = &binding{value: fn, mutable: true, initialized: true, deletable: true}
		}
	}

	// Instantiate declared var names (initialized to undefined; an existing
	// binding is left untouched so `var x` never clobbers a prior value).
	for _, name := range declaredVarNames {
		if global {
			i.defineGlobalVar(name, nil, true)
			continue
		}
		if _, ok := varEnv.vars[name]; !ok {
			varEnv.vars[name] = &binding{value: Undef, mutable: true, initialized: true, deletable: true}
		}
	}
	return nil
}
