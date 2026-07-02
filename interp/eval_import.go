package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/parser"
	"github.com/iceisfun/gojs/token"
)

// defaultLocal is the synthetic local binding name for an anonymous default
// export (ECMA-262 uses "*default*"). It cannot collide with a real binding
// because it is not a valid identifier.
const defaultLocal = "*default*"

// evalImportCall implements the runtime semantics of a dynamic import()
// (ECMA-262 sec-import-call-runtime-semantics-evaluation). It always returns a
// Promise: the specifier expression, its ToString, and the module's resolution
// and evaluation all reject the promise on failure rather than throwing
// synchronously.
func (i *Interpreter) evalImportCall(ctx context.Context, e *ast.ImportCall, env *Environment) (Value, error) {
	// Evaluating the specifier expression and the options argument (steps that
	// precede NewPromiseCapability) uses `?`: an abrupt completion here throws
	// synchronously rather than rejecting a promise.
	specV, err := i.evalExpr(ctx, e.Specifier, env)
	if err != nil {
		return nil, err
	}
	if e.Options != nil {
		// The options argument is evaluated for its side effects and abrupt
		// completions, but its contents (import attributes) are not honored.
		if _, oerr := i.evalExpr(ctx, e.Options, env); oerr != nil {
			return nil, oerr
		}
	}

	// From here on failures reject the returned promise (IfAbruptRejectPromise).
	pObj, resolve, reject := i.newPromise()
	rejectAbrupt := func(err error) (Value, error) {
		if v, ok := ThrownValue(err); ok {
			reject(v)
			return pObj, nil
		}
		return nil, err
	}

	spec, serr := i.ToStringV(ctx, specV)
	if serr != nil {
		return rejectAbrupt(serr)
	}

	ns, lerr := i.importModuleNamespace(ctx, spec)
	if lerr != nil {
		return rejectAbrupt(lerr)
	}
	resolve(ns)
	return pObj, nil
}

// importModuleNamespace resolves, loads (once), evaluates, and caches an ES
// module, returning its namespace object. The namespace exposes each export as
// a live accessor binding onto the module's top-level scope, so a later
// mutation of an exported variable is observed through the namespace.
func (i *Interpreter) importModuleNamespace(ctx context.Context, specifier string) (Value, error) {
	if i.moduleProvider == nil {
		return nil, i.throwError(ctx, "TypeError", "Cannot import module '"+specifier+"': no module provider is configured")
	}
	id, err := i.moduleProvider.Resolve(ctx, specifier, "")
	if err != nil {
		return nil, i.throwError(ctx, "TypeError", "Cannot find module '"+specifier+"': "+err.Error())
	}
	if ns, ok := i.moduleNamespaces[id]; ok {
		return ns, nil
	}

	src, err := i.moduleProvider.Load(ctx, id)
	if err != nil {
		return nil, i.throwError(ctx, "TypeError", "Cannot load module '"+id+"': "+err.Error())
	}
	i.registerSource(id, src)
	prog, err := parser.ParseModule(id, src)
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	// Flatten export declarations into ordinary statements, recording the
	// exported-name -> local-name mapping so the namespace can bind live getters.
	body, exports := i.flattenModuleExports(prog.Body)

	// Evaluate the module body in its own function-scoped environment. Top-level
	// var/let/const/function bindings live here, giving the namespace getters a
	// stable target that reflects later mutations.
	env := NewEnvironment(i.globalEnv, true)
	env.strict = true
	if i.moduleProvider != nil {
		env.vars["require"] = &binding{value: i.makeRequire(id), mutable: true, initialized: true}
	}

	if err := i.hoistDeclarations(ctx, body, env, true); err != nil {
		return nil, err
	}
	if _, err := i.execStmts(ctx, body, env); err != nil {
		if _, ok := err.(*returnSignal); !ok {
			return nil, err
		}
	}

	// Build the namespace object with a live accessor for each export.
	ns := i.NewPlainObject()
	for _, ex := range exports {
		local := ex.local
		getter := i.newNativeFunc("get "+ex.exported, 0, func(gctx context.Context, _ Value, _ []Value) (Value, error) {
			if b := env.lookup(local); b != nil {
				if !b.initialized {
					return nil, i.throwError(gctx, "ReferenceError", "Cannot access '"+local+"' before initialization")
				}
				return b.value, nil
			}
			return Undef, nil
		})
		ns.DefineAccessor(ex.exported, getter, nil, true)
	}
	i.moduleNamespaces[id] = ns
	return ns, nil
}

// moduleExport records one export binding: the name seen through the namespace
// and the module-scope local it reads.
type moduleExport struct {
	exported string
	local    string
}

// flattenModuleExports rewrites a module's top-level statement list, replacing
// each ExportStmt with the plain declaration it wraps (if any), and returns the
// resulting statements together with the collected export bindings.
func (i *Interpreter) flattenModuleExports(stmts []ast.Stmt) ([]ast.Stmt, []moduleExport) {
	var body []ast.Stmt
	var exports []moduleExport
	for _, s := range stmts {
		es, ok := s.(*ast.ExportStmt)
		if !ok {
			body = append(body, s)
			continue
		}
		switch {
		case es.Default && es.DefaultExpr != nil:
			// export default <expr>; -> const *default* = <expr>;
			body = append(body, &ast.VarDecl{
				Kind: token.CONST,
				Decls: []*ast.VarDeclarator{{
					Target: &ast.Ident{Name: defaultLocal},
					Init:   es.DefaultExpr,
				}},
			})
			exports = append(exports, moduleExport{"default", defaultLocal})
		case es.Default && es.Decl != nil:
			local := declBindingName(es.Decl)
			if local == "" {
				local = defaultLocal
				setDeclName(es.Decl, defaultLocal)
			}
			body = append(body, es.Decl)
			exports = append(exports, moduleExport{"default", local})
		case es.Decl != nil:
			body = append(body, es.Decl)
			for _, n := range declBindingNames(es.Decl) {
				exports = append(exports, moduleExport{n, n})
			}
		default:
			for _, sp := range es.Specifiers {
				exports = append(exports, moduleExport{sp.Exported, sp.Local})
			}
		}
	}
	return body, exports
}

// declBindingName returns the single declared name of a function or class
// declaration, or "" when it is anonymous (an anonymous default export).
func declBindingName(s ast.Stmt) string {
	switch d := s.(type) {
	case *ast.FuncDecl:
		if d.Def != nil && d.Def.Name != nil {
			return d.Def.Name.Name
		}
	case *ast.ClassDecl:
		if d.Def != nil && d.Def.Name != nil {
			return d.Def.Name.Name
		}
	}
	return ""
}

// setDeclName assigns a synthetic name to an anonymous default function/class
// declaration so it produces a bindable local.
func setDeclName(s ast.Stmt, name string) {
	switch d := s.(type) {
	case *ast.FuncDecl:
		if d.Def != nil && d.Def.Name == nil {
			d.Def.Name = &ast.Ident{Name: name}
		}
	case *ast.ClassDecl:
		if d.Def != nil && d.Def.Name == nil {
			d.Def.Name = &ast.Ident{Name: name}
		}
	}
}

// declBindingNames returns every name bound by an exported declaration
// (var/let/const patterns, or a function/class name).
func declBindingNames(s ast.Stmt) []string {
	var names []string
	switch d := s.(type) {
	case *ast.VarDecl:
		for _, dec := range d.Decls {
			forEachPatternName(dec.Target, func(n string) { names = append(names, n) })
		}
	case *ast.FuncDecl:
		if d.Def != nil && d.Def.Name != nil {
			names = append(names, d.Def.Name.Name)
		}
	case *ast.ClassDecl:
		if d.Def != nil && d.Def.Name != nil {
			names = append(names, d.Def.Name.Name)
		}
	}
	return names
}
