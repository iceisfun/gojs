// Package ts adds TypeScript support to gojs. It transpiles TypeScript to
// JavaScript using github.com/iceisfun/typescript — a hoisting of Microsoft's
// typescript-go compiler — and runs the result on a gojs VM.
//
// Importing this package is what pulls the typescript-go dependency into a
// build; the gojs core (module github.com/iceisfun/gojs) stays dependency-free,
// so embeddings that only run JavaScript pay nothing for it.
//
// Transpilation is checker-free (the isolatedModules model): types are erased
// and TypeScript-only syntax is lowered, but the program is not type-checked.
// Type errors are ignored — the goal is to *run* TypeScript, not validate it.
package ts

import (
	"context"
	"fmt"
	"strings"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/interp"
	tsast "github.com/iceisfun/typescript/ast"
	"github.com/iceisfun/typescript/core"
	"github.com/iceisfun/typescript/sourcemap"
	"github.com/iceisfun/typescript/transpiler"
)

// Transpile converts TypeScript source to CommonJS JavaScript, the module form
// gojs's require()/module system evaluates. fileName is used for diagnostics and
// selects TSX parsing for a .tsx name.
func Transpile(fileName, src string) (string, error) {
	js, _, err := transpileWith(fileName, src, false, false)
	return js, err
}

// config holds per-provider transpilation settings.
type config struct {
	permissive  bool
	astDisabled bool
}

// Option configures TypeScript transpilation for a Provider / WithTypeScript.
type Option func(*config)

// Permissive transpiles TypeScript even when it has syntax errors (matching
// ts.transpileModule) instead of rejecting it. The default is strict: malformed
// TypeScript is reported as an error rather than run.
func Permissive() Option { return func(c *config) { c.permissive = true } }

// DisableAST turns off the direct TypeScript-AST -> gojs-AST module frontend, so
// every TypeScript module is transpiled to JavaScript text and re-parsed (the
// original behavior). By default a Provider lowers TypeScript modules straight to
// the gojs AST — skipping the text emit + re-parse — and falls back to the text
// path per module for anything the lowerer does not yet handle. Use this to force
// the text path everywhere (for debugging, or to compare the two paths).
func DisableAST() Option { return func(c *config) { c.astDisabled = true } }

// UnsupportedSyntaxError reports TypeScript syntax that gojs deliberately does
// not run. gojs is an embedded, syscall-firewalled scripting engine — not a build
// tool — so frontend-only features that would need a code transform (lowering JSX
// elements to factory calls, desugaring decorators) are out of scope. The
// isolatedModules transpiler PRESERVES such syntax verbatim into JavaScript,
// which the gojs parser then rejects with a confusing low-level message;
// detecting it up front turns that into a clear, actionable error.
type UnsupportedSyntaxError struct {
	Feature  string // human-readable name, e.g. "JSX" or "decorators"
	FileName string
}

func (e *UnsupportedSyntaxError) Error() string {
	return fmt.Sprintf("gojs/ts: %s: %s is not supported and cannot be run "+
		"(gojs runs TypeScript as a scripting language, not a build tool)", e.FileName, e.Feature)
}

// unsupportedSyntax scans a parsed TypeScript source for constructs gojs cannot
// run because the transpiler preserves them verbatim (JSX, decorators), returning
// the first one found. Everything else (types, enums, namespaces, parameter
// properties, module syntax) is erased or lowered to runnable JavaScript.
func unsupportedSyntax(sf *tsast.SourceFile, fileName string) *UnsupportedSyntaxError {
	var found *UnsupportedSyntaxError
	var visit tsast.Visitor
	visit = func(n *tsast.Node) bool {
		if n == nil || found != nil {
			return found != nil
		}
		switch n.Kind {
		case tsast.KindJsxElement, tsast.KindJsxSelfClosingElement, tsast.KindJsxFragment:
			found = &UnsupportedSyntaxError{Feature: "JSX", FileName: fileName}
			return true
		case tsast.KindDecorator:
			found = &UnsupportedSyntaxError{Feature: "decorators (@…)", FileName: fileName}
			return true
		}
		return n.ForEachChild(visit)
	}
	if sf.Statements != nil {
		for _, stmt := range sf.Statements.Nodes {
			if visit(stmt) {
				break
			}
		}
	}
	return found
}

// transpileWith converts TypeScript to CommonJS JavaScript, optionally producing
// a source map and/or tolerating syntax errors. It recovers a transform panic
// (unimplemented type-checker corners) into an error rather than crashing.
func transpileWith(fileName, src string, permissive, withMap bool) (js string, raw *sourcemap.RawSourceMap, err error) {
	defer func() {
		if r := recover(); r != nil {
			js, raw, err = "", nil, fmt.Errorf("transpile %s: %v", fileName, r)
		}
	}()
	o := transpiler.Options{
		FileName:           fileName,
		Module:             core.ModuleKindCommonJS,
		JSX:                strings.HasSuffix(fileName, ".tsx"),
		IgnoreSyntaxErrors: permissive,
	}
	// Reject JSX/decorators with a clear error before the transpiler preserves
	// them into JavaScript the gojs parser would choke on. Only pay for the extra
	// detection parse when the syntax is even possible: JSX requires a .tsx file,
	// and a decorator requires an "@" somewhere in the source. A parse failure here
	// is ignored — the emit below reports it (or, when permissive, recovers).
	if o.JSX || strings.ContainsRune(src, '@') {
		if sf, perr := transpiler.ModuleAST(src, o); perr == nil {
			if ue := unsupportedSyntax(sf, fileName); ue != nil {
				return "", nil, ue
			}
		}
	}
	if withMap {
		return transpiler.ModuleWithSourceMap(src, o)
	}
	js, err = transpiler.Module(src, o)
	return js, nil, err
}

// IsTypeScript reports whether a module id names a TypeScript source file.
func IsTypeScript(id string) bool {
	switch {
	case strings.HasSuffix(id, ".ts"),
		strings.HasSuffix(id, ".tsx"),
		strings.HasSuffix(id, ".mts"),
		strings.HasSuffix(id, ".cts"):
		return true
	}
	return false
}

// Provider wraps a base [interp.ModuleProvider] (the "CodeProvider" that decides
// how files are included — from disk, memory, network, game data, etc.) so that
// any module whose id is a TypeScript file is transpiled to JavaScript before
// gojs evaluates it. Non-TypeScript modules pass through unchanged.
//
// Resolution is delegated entirely to base, so host behavior over what a
// specifier means is preserved.
func Provider(base interp.ModuleProvider, opts ...Option) interp.ModuleProvider {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &provider{base: base, permissive: c.permissive, astDisabled: c.astDisabled}
}

// WithTypeScript returns the VM options for running TypeScript with
// source-mapped error stacks: a transpiling module provider over base, plus the
// matching source mapper so a thrown error's stack reports the original .ts
// line/column. Compose it with other options:
//
//	opts := append(ts.WithTypeScript(base), gojs.WithPrintProvider(pp))
//	vm := gojs.New(opts...)
func WithTypeScript(base interp.ModuleProvider, opts ...Option) []interp.Option {
	var c config
	for _, o := range opts {
		o(&c)
	}
	m := NewMapper()
	return []interp.Option{
		interp.WithModuleProvider(&provider{base: base, mapper: m, permissive: c.permissive, astDisabled: c.astDisabled}),
		interp.WithSourceMapper(m),
	}
}

type provider struct {
	base        interp.ModuleProvider
	mapper      *Mapper // when set, transpile with a source map and record it
	permissive  bool
	astDisabled bool
}

func (p *provider) Resolve(ctx context.Context, specifier, referrer string) (string, error) {
	id, err := p.base.Resolve(ctx, specifier, referrer)
	if err == nil {
		return id, nil
	}
	// A TypeScript import is written without an extension (import "./util");
	// retry the base resolver with the TypeScript extensions so such specifiers
	// resolve to their source file. Base resolution (and thus host policy) is
	// still what decides whether the candidate exists.
	if !hasModuleExt(specifier) {
		for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
			if id, e := p.base.Resolve(ctx, specifier+ext, referrer); e == nil {
				return id, nil
			}
		}
	}
	return "", err
}

func hasModuleExt(specifier string) bool {
	switch {
	case IsTypeScript(specifier),
		strings.HasSuffix(specifier, ".js"),
		strings.HasSuffix(specifier, ".mjs"),
		strings.HasSuffix(specifier, ".cjs"),
		strings.HasSuffix(specifier, ".json"):
		return true
	}
	return false
}

func (p *provider) Load(ctx context.Context, id string) (string, error) {
	src, err := p.base.Load(ctx, id)
	if err != nil || !IsTypeScript(id) {
		return src, err
	}
	js, raw, err := transpileWith(id, src, p.permissive, p.mapper != nil)
	if err != nil {
		return "", err
	}
	if p.mapper != nil {
		p.mapper.record(id, src, raw)
	}
	return js, nil
}

// LoadProgram implements [interp.ProgramLoader]: for a TypeScript module it
// lowers the source straight to a gojs AST (transpiler.ModuleAST + [Lower]),
// skipping the emit-JavaScript-text-then-reparse round trip [Load] performs.
//
// It defers to the text Load path (handled=false) when the AST frontend is
// disabled, when a source mapper is configured (the AST path is not yet recorded
// in the source map), for non-TypeScript ids, and — crucially — whenever the
// transpile or the lowering fails, including a construct the lowerer does not yet
// handle (*UnsupportedNodeError). The text path therefore stays the correctness
// backstop: turning the AST frontend on can only skip work, never change results.
func (p *provider) LoadProgram(ctx context.Context, id string) (*ast.Program, string, bool, error) {
	if p.astDisabled || p.mapper != nil || !IsTypeScript(id) {
		return nil, "", false, nil
	}
	src, err := p.base.Load(ctx, id)
	if err != nil {
		return nil, "", false, err
	}
	sf, err := transpiler.ModuleAST(src, transpiler.Options{
		FileName:           id,
		Module:             core.ModuleKindCommonJS,
		JSX:                strings.HasSuffix(id, ".tsx"),
		IgnoreSyntaxErrors: p.permissive,
	})
	if err != nil {
		// Let the text path re-run and surface the transpile/syntax error uniformly.
		return nil, "", false, nil
	}
	// JSX/decorators are preserved verbatim by the transpiler and cannot run;
	// surface a clear error here as a HARD failure rather than falling back to the
	// text path (which would only reproduce the confusing low-level parse error).
	if ue := unsupportedSyntax(sf, id); ue != nil {
		return nil, "", false, ue
	}
	prog, err := Lower(sf, id, src)
	if err != nil {
		return nil, "", false, nil
	}
	return prog, src, true, nil
}

// RunString transpiles a self-contained TypeScript program and runs it on vm as
// a top-level script. It is for scripts that do not use top-level import/export
// (those belong to a module and should be reached through a TypeScript-aware
// [Provider] via require); name appears in diagnostics.
func RunString(vm *interp.Interpreter, name, tsSrc string) (interp.Value, error) {
	js, err := Transpile(name, tsSrc)
	if err != nil {
		return nil, err
	}
	return vm.RunString(name, js)
}
