package interp

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/iceisfun/gojs/parser"
)

// ModuleProvider is the capability interface a host implements to control how a
// script loads other scripts via require(specifier). It is the JavaScript
// analogue of golua's LuaCodeProvider: without a provider, require is not
// available at all, so a script cannot pull in code the host has not sanctioned.
//
// A game embedding gojs, for example, implements ModuleProvider to serve module
// source out of its own asset/data files (a pak archive, a virtual filesystem,
// a database) — the engine never touches the real filesystem unless the provider
// does.
type ModuleProvider interface {
	// Resolve maps a specifier (exactly as written in require(specifier)) to a
	// canonical module id, relative to the module doing the require. referrer is
	// the id of the requiring module, or "" for the top-level program. The
	// returned id is used as the module cache key and passed to Load.
	Resolve(ctx context.Context, specifier, referrer string) (string, error)

	// Load returns the source text of the module with the given canonical id.
	Load(ctx context.Context, id string) (string, error)
}

// WithModuleProvider enables CommonJS-style require(specifier) backed by p.
func WithModuleProvider(p ModuleProvider) Option {
	return func(i *Interpreter) { i.moduleProvider = p }
}

// initModules installs the global require function when a ModuleProvider is
// configured. It is called during bootstrap.
func (i *Interpreter) initModules() {
	if i.moduleProvider == nil {
		return
	}
	i.modules = map[string]*Object{}
	i.setGlobalHidden("require", i.makeRequire(""))
}

// makeRequire builds a require function bound to a referrer module id, so that
// relative specifiers resolve against the requiring module.
func (i *Interpreter) makeRequire(referrer string) *Object {
	return i.newNativeFunc("require", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		spec, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return i.requireModule(ctx, spec, referrer)
	})
}

// requireModule resolves, loads (once), evaluates, and caches a module, then
// returns its exports. Modules are cached by canonical id before evaluation so
// that circular dependencies see a partially-populated exports object rather
// than looping.
func (i *Interpreter) requireModule(ctx context.Context, specifier, referrer string) (Value, error) {
	id, err := i.moduleProvider.Resolve(ctx, specifier, referrer)
	if err != nil {
		return nil, i.throwError(ctx, "Error", "Cannot find module '"+specifier+"': "+err.Error())
	}
	if mod, ok := i.modules[id]; ok {
		return mod.GetStr(ctx, "exports")
	}

	src, err := i.moduleProvider.Load(ctx, id)
	if err != nil {
		return nil, i.throwError(ctx, "Error", "Cannot load module '"+id+"': "+err.Error())
	}
	prog, err := parser.Parse(id, src)
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}

	// Build the CommonJS module record and cache it before evaluation.
	moduleObj := i.NewPlainObject()
	exportsObj := i.NewPlainObject()
	moduleObj.SetData("id", String(id))
	moduleObj.SetData("exports", exportsObj)
	i.modules[id] = moduleObj

	// A fresh function-scoped environment provides the module's free variables:
	// module, exports, require (bound to this module), __filename, __dirname.
	env := NewEnvironment(i.globalEnv, true)
	env.setThis(exportsObj)
	define := func(name string, v Value) {
		env.vars[name] = &binding{value: v, mutable: true, initialized: true}
	}
	define("module", moduleObj)
	define("exports", exportsObj)
	define("require", i.makeRequire(id))
	define("__filename", String(id))
	define("__dirname", String(moduleDir(id)))

	i.hoistDeclarations(ctx, prog.Body, env, true)
	if _, err := i.execStmts(ctx, prog.Body, env); err != nil {
		if _, ok := err.(*returnSignal); !ok {
			// Evaluation failed; drop the half-built module so a retry re-runs it.
			delete(i.modules, id)
			return nil, err
		}
	}
	// module.exports may have been reassigned wholesale (module.exports = ...).
	return moduleObj.GetStr(ctx, "exports")
}

// moduleDir returns the directory portion of a module id, for __dirname.
func moduleDir(id string) string {
	if d := path.Dir(id); d != "." {
		return d
	}
	return ""
}

// ---------------------------------------------------------------------------
// Default providers
// ---------------------------------------------------------------------------

// MapModuleProvider serves module source from an in-memory map of id -> source.
// It is ideal for embeddings that already hold their scripts in memory (game
// data files, bundled assets) and never want to touch the filesystem. Specifier
// resolution is by exact key, with a leading "./" stripped.
type MapModuleProvider struct {
	// Sources maps a module id to its JavaScript source.
	Sources map[string]string
}

// NewMapModuleProvider returns a MapModuleProvider over the given sources.
func NewMapModuleProvider(sources map[string]string) *MapModuleProvider {
	return &MapModuleProvider{Sources: sources}
}

// Resolve normalizes a specifier to a map key: a relative specifier is joined
// against the referrer's directory; otherwise it is used as-is. A ".js" suffix
// is optional.
func (p *MapModuleProvider) Resolve(_ context.Context, specifier, referrer string) (string, error) {
	id := specifier
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
		id = path.Join(moduleDir(referrer), specifier)
	}
	if _, ok := p.Sources[id]; ok {
		return id, nil
	}
	if _, ok := p.Sources[id+".js"]; ok {
		return id + ".js", nil
	}
	return "", os.ErrNotExist
}

// Load returns the stored source for id.
func (p *MapModuleProvider) Load(_ context.Context, id string) (string, error) {
	src, ok := p.Sources[id]
	if !ok {
		return "", os.ErrNotExist
	}
	return src, nil
}

// DirModuleProvider serves modules from a directory on the real filesystem,
// confined to that root (a specifier cannot escape it via ".."). Use it for the
// CLI or trusted local development; prefer a custom provider or
// MapModuleProvider for untrusted embeddings.
type DirModuleProvider struct {
	root string
}

// NewDirModuleProvider returns a provider rooted at dir.
func NewDirModuleProvider(dir string) *DirModuleProvider {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return &DirModuleProvider{root: abs}
}

// Resolve joins the specifier against the referrer directory (for relative
// specifiers) or the root, appending ".js" if needed, and verifies the result
// stays within the root.
func (p *DirModuleProvider) Resolve(_ context.Context, specifier, referrer string) (string, error) {
	var id string
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
		id = path.Join(moduleDir(referrer), specifier)
	} else {
		id = specifier
	}
	id = path.Clean("/" + id)[1:] // normalize, strip leading slash
	full := filepath.Join(p.root, filepath.FromSlash(id))
	if !strings.HasPrefix(full, p.root) {
		return "", os.ErrPermission
	}
	if _, err := os.Stat(full); err == nil {
		return id, nil
	}
	if _, err := os.Stat(full + ".js"); err == nil {
		return id + ".js", nil
	}
	return "", os.ErrNotExist
}

// Load reads the module file under the root.
func (p *DirModuleProvider) Load(_ context.Context, id string) (string, error) {
	full := filepath.Join(p.root, filepath.FromSlash(id))
	if !strings.HasPrefix(full, p.root) {
		return "", os.ErrPermission
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
