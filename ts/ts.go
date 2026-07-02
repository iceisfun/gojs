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

	"github.com/iceisfun/gojs/interp"
	"github.com/iceisfun/typescript/core"
	"github.com/iceisfun/typescript/sourcemap"
	"github.com/iceisfun/typescript/transpiler"
)

// Transpile converts TypeScript source to CommonJS JavaScript, the module form
// gojs's require()/module system evaluates. fileName is used for diagnostics and
// selects TSX parsing for a .tsx name.
func Transpile(fileName, src string) (js string, err error) {
	// The transpiler runs upstream typescript-go transforms; guard against a
	// panic in a transform so it surfaces as an error rather than crashing the
	// host process (transforms lean on unimplemented type-checker corners).
	defer func() {
		if r := recover(); r != nil {
			js, err = "", fmt.Errorf("transpile %s: %v", fileName, r)
		}
	}()
	return transpiler.Module(src, transpiler.Options{
		FileName: fileName,
		Module:   core.ModuleKindCommonJS,
		JSX:      strings.HasSuffix(fileName, ".tsx"),
	})
}

// transpileWithMap is Transpile plus a v3 source map (generated JS positions ->
// original TypeScript), used when a Mapper is recording maps for error stacks.
func transpileWithMap(fileName, src string) (js string, raw *sourcemap.RawSourceMap, err error) {
	defer func() {
		if r := recover(); r != nil {
			js, raw, err = "", nil, fmt.Errorf("transpile %s: %v", fileName, r)
		}
	}()
	return transpiler.ModuleWithSourceMap(src, transpiler.Options{
		FileName: fileName,
		Module:   core.ModuleKindCommonJS,
		JSX:      strings.HasSuffix(fileName, ".tsx"),
	})
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
func Provider(base interp.ModuleProvider) interp.ModuleProvider {
	return &provider{base: base}
}

// WithTypeScript returns the VM options for running TypeScript with
// source-mapped error stacks: a transpiling module provider over base, plus the
// matching source mapper so a thrown error's stack reports the original .ts
// line/column. Compose it with other options:
//
//	opts := append(ts.WithTypeScript(base), gojs.WithPrintProvider(pp))
//	vm := gojs.New(opts...)
func WithTypeScript(base interp.ModuleProvider) []interp.Option {
	m := NewMapper()
	return []interp.Option{
		interp.WithModuleProvider(&provider{base: base, mapper: m}),
		interp.WithSourceMapper(m),
	}
}

type provider struct {
	base   interp.ModuleProvider
	mapper *Mapper // when set, transpile with a source map and record it
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
	if p.mapper != nil {
		js, raw, err := transpileWithMap(id, src)
		if err != nil {
			return "", err
		}
		p.mapper.record(id, raw)
		return js, nil
	}
	return Transpile(id, src)
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
