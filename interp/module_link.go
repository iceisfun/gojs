package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/parser"
)

// This file implements the slice of ES-module linking that gojs needs to make
// dynamic import() report the module errors the spec mandates: an indirect
// export (`export { x } from 'src'` / `export * as ns from 'src'`) whose target
// binding is ambiguous or does not exist is a SyntaxError raised before the
// module is evaluated (ECMA-262 §16.2.1.6.3 ResolveExport, applied by
// InitializeEnvironment). gojs otherwise evaluates modules through a flattened
// CommonJS-style path (see eval_import.go); this layer only validates the export
// graph, it does not wire cross-module bindings.

// linkedModule is the export structure extracted from one parsed module.
type linkedModule struct {
	id       string
	local    map[string]string // export name -> local binding name (incl. "default")
	indirect []indirectExport  // `export { importName as exportName } from moduleID`
	stars    []string          // resolved ids of `export * from …`
	ok       bool              // false when the module could not be loaded/parsed
}

// indirectExport is one re-exported binding: exportName is the name this module
// exposes, importName is the name looked up in the source module (the sentinel
// starNamespace for `export * as ns`), and moduleID is the source module's id.
type indirectExport struct {
	exportName string
	importName string
	moduleID   string
}

// starNamespace marks an indirect export whose bound value is the source
// module's whole namespace object (`export * as ns from 'src'`).
const starNamespace = "*namespace*"

// resolvedBinding is a non-null, non-ambiguous ResolveExport result.
type resolvedBinding struct {
	moduleID    string
	bindingName string
}

// validateModuleLinks loads the module graph rooted at id and validates every
// reachable module's indirect exports. It returns a thrown SyntaxError when any
// indirect export resolves to null (missing / circular) or "ambiguous"; a
// dependency that cannot be loaded or parsed is treated leniently (skipped) so
// linking never introduces a failure the evaluator would not also hit.
func (i *Interpreter) validateModuleLinks(ctx context.Context, id string) error {
	if i.linkedModules == nil {
		i.linkedModules = map[string]*linkedModule{}
	}
	seen := map[string]bool{}
	var walk func(mid string) error
	walk = func(mid string) error {
		if seen[mid] {
			return nil
		}
		seen[mid] = true
		lm := i.loadLinked(ctx, mid)
		if !lm.ok {
			return nil
		}
		for _, ie := range lm.indirect {
			if ie.importName == starNamespace {
				continue // a namespace binding always resolves
			}
			res, ambiguous, err := i.resolveExport(ctx, ie.moduleID, ie.importName, nil)
			if err != nil {
				return err
			}
			if ambiguous || res == nil {
				return i.throwError(ctx, "SyntaxError",
					"The requested module does not provide an export named '"+ie.importName+"'")
			}
		}
		// Recurse into every referenced module so the whole graph is validated.
		for _, ie := range lm.indirect {
			if err := walk(ie.moduleID); err != nil {
				return err
			}
		}
		for _, s := range lm.stars {
			if err := walk(s); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(id)
}

// loadLinked returns the extracted export structure for the module id, loading
// and parsing it (once, cached) through the module provider. A load or parse
// failure yields a record with ok=false rather than an error, so the linker can
// skip it.
func (i *Interpreter) loadLinked(ctx context.Context, id string) *linkedModule {
	if lm, ok := i.linkedModules[id]; ok {
		return lm
	}
	lm := &linkedModule{id: id, local: map[string]string{}}
	i.linkedModules[id] = lm // cache before extracting so cycles terminate

	src, err := i.moduleProvider.Load(ctx, id)
	if err != nil {
		return lm
	}
	prog, perr := parser.ParseModule(id, src)
	if perr != nil {
		return lm
	}
	i.extractExportEntries(ctx, id, prog.Body, lm)
	lm.ok = true
	return lm
}

// extractExportEntries records the export structure of one module's top-level
// statements into lm, resolving each re-export's source specifier to a module id.
func (i *Interpreter) extractExportEntries(ctx context.Context, id string, stmts []ast.Stmt, lm *linkedModule) {
	for _, s := range stmts {
		es, ok := s.(*ast.ExportStmt)
		if !ok {
			continue
		}
		switch {
		case es.Source == "":
			// A local export: a declaration, a default, or `export { a as b }`
			// naming local bindings.
			switch {
			case es.Default:
				// `export default` binds *default* unless it is a *named* function
				// or class declaration, whose own name is the local binding — the
				// same mapping flattenModuleExports uses when evaluating the body.
				local := defaultLocal
				if es.Decl != nil {
					if n := declBindingName(es.Decl); n != "" {
						local = n
					}
				}
				lm.local["default"] = local
			case es.Decl != nil:
				for _, n := range declBindingNames(es.Decl) {
					lm.local[n] = n
				}
			default:
				for _, sp := range es.Specifiers {
					lm.local[sp.Exported] = sp.Local
				}
			}
		case es.Star && es.StarName == "":
			// export * from 'src'
			if mid, ok := i.resolveModuleID(ctx, es.Source, id); ok {
				lm.stars = append(lm.stars, mid)
			}
		case es.Star: // export * as name from 'src'
			if mid, ok := i.resolveModuleID(ctx, es.Source, id); ok {
				lm.indirect = append(lm.indirect, indirectExport{es.StarName, starNamespace, mid})
			}
		default: // export { a as b } from 'src'
			if mid, ok := i.resolveModuleID(ctx, es.Source, id); ok {
				for _, sp := range es.Specifiers {
					lm.indirect = append(lm.indirect, indirectExport{sp.Exported, sp.Local, mid})
				}
			}
		}
	}
}

// resolveModuleID resolves a re-export's source specifier against its referrer,
// returning the canonical module id and whether resolution succeeded.
func (i *Interpreter) resolveModuleID(ctx context.Context, specifier, referrer string) (string, bool) {
	mid, err := i.moduleProvider.Resolve(ctx, specifier, referrer)
	if err != nil {
		return "", false
	}
	return mid, true
}

// getExportedNames implements GetExportedNames (§16.2.1.6.2): the set of names a
// module exports, following `export *` re-exports (which never contribute
// "default"). starSet breaks cycles among star re-exports. The result is
// de-duplicated but unordered; the caller sorts it.
func (i *Interpreter) getExportedNames(ctx context.Context, id string, starSet map[string]bool) []string {
	if starSet[id] {
		return nil
	}
	starSet[id] = true
	lm := i.loadLinked(ctx, id)
	if !lm.ok {
		return nil
	}
	set := map[string]bool{}
	var names []string
	add := func(n string) {
		if !set[n] {
			set[n] = true
			names = append(names, n)
		}
	}
	for name := range lm.local {
		add(name)
	}
	for _, ie := range lm.indirect {
		add(ie.exportName)
	}
	for _, s := range lm.stars {
		for _, n := range i.getExportedNames(ctx, s, starSet) {
			if n != "default" {
				add(n)
			}
		}
	}
	return names
}

// resolveExport implements ResolveExport (§16.2.1.6.3): it resolves exportName as
// seen by module id, returning the resolved binding, or ambiguous=true when the
// name is provided by two different star re-exports, or (nil,false,nil) when the
// name is not exported or the resolution is circular. resolveSet records the
// (module, name) pairs already visited to break cycles.
func (i *Interpreter) resolveExport(ctx context.Context, id, exportName string, resolveSet []resolvedBinding) (*resolvedBinding, bool, error) {
	// A (module, exportName) already on the path is a circular import: return null.
	for _, r := range resolveSet {
		if r.moduleID == id && r.bindingName == exportName {
			return nil, false, nil
		}
	}
	resolveSet = append(resolveSet, resolvedBinding{id, exportName})

	lm := i.loadLinked(ctx, id)
	if !lm.ok {
		return nil, false, nil
	}
	// A locally-bound export resolves to this module's local binding.
	if localName, ok := lm.local[exportName]; ok {
		return &resolvedBinding{id, localName}, false, nil
	}
	// An indirect export forwards to the source module (or is a namespace binding).
	for _, ie := range lm.indirect {
		if ie.exportName != exportName {
			continue
		}
		if ie.importName == starNamespace {
			return &resolvedBinding{ie.moduleID, starNamespace}, false, nil
		}
		return i.resolveExport(ctx, ie.moduleID, ie.importName, resolveSet)
	}
	// "default" is never resolved through a star re-export (§16.2.1.6.3 step 8).
	if exportName == "default" {
		return nil, false, nil
	}
	// Otherwise gather star re-exports: a name provided by two distinct bindings
	// is ambiguous.
	var star *resolvedBinding
	for _, s := range lm.stars {
		res, ambiguous, err := i.resolveExport(ctx, s, exportName, resolveSet)
		if err != nil {
			return nil, false, err
		}
		if ambiguous {
			return nil, true, nil
		}
		if res != nil {
			if star == nil {
				star = res
			} else if star.moduleID != res.moduleID || star.bindingName != res.bindingName {
				return nil, true, nil
			}
		}
	}
	return star, false, nil
}
