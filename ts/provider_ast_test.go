package ts

import (
	"testing"

	"github.com/iceisfun/gojs/interp"
)

// runGraph evaluates a TypeScript module graph through a Provider and returns the
// entry module's exports as a string. disableAST forces the text path.
func runGraph(t *testing.T, src map[string]string, disableAST bool) string {
	t.Helper()
	base := interp.NewMapModuleProvider(src)
	var opts []Option
	if disableAST {
		opts = append(opts, DisableAST())
	}
	vm := interp.New(interp.WithModuleProvider(Provider(base, opts...)))
	v, err := vm.RunString("<entry>", "require('./main.ts')")
	if err != nil {
		t.Fatalf("run (disableAST=%v): %v", disableAST, err)
	}
	s, _ := vm.ToString(v)
	return s
}

// TestProviderModuleGraph checks that a real multi-module TypeScript program —
// import/export lowered to CommonJS require/exports — produces the same result
// through the AST frontend (default) and the text path (DisableAST). This covers
// the module plumbing the script-level oracle does not.
func TestProviderModuleGraph(t *testing.T) {
	src := map[string]string{
		"math.ts": `
export function add(a: number, b: number): number { return a + b; }
export const K = 10;
export default class Vec { constructor(public x: number, public y: number) {} len() { return Math.hypot(this.x, this.y); } }
`,
		"util.ts": `
import { add } from './math';
export const inc = (x: number): number => add(x, 1);
export function sumAll(...xs: number[]): number { return xs.reduce((a, b) => a + b, 0); }
`,
		"main.ts": `
import Vec, { K, add } from './math';
import { inc, sumAll } from './util';
const v = new Vec(3, 4);
const r = { total: sumAll(inc(K), add(1, 2), v.len()) };
module.exports = ` + "`total=${r.total}`" + `;
`,
	}
	ast := runGraph(t, src, false)
	text := runGraph(t, src, true)
	if ast != text {
		t.Fatalf("AST path %q != text path %q", ast, text)
	}
	// inc(10)=11 + add(1,2)=3 + hypot(3,4)=5 = 19.
	if ast != "total=19" {
		t.Fatalf("result = %q, want %q", ast, "total=19")
	}
}

// TestProviderFallback proves the text path is a working backstop: a module using
// a construct the lowerer does not translate (new.target, a MetaProperty) still
// runs correctly, because LoadProgram returns handled=false and the interpreter
// loads and parses the transpiled text instead.
func TestProviderFallback(t *testing.T) {
	src := map[string]string{
		"main.ts": `
function Widget(this: any) { this.made = new.target ? new.target.name : "plain"; }
const w = new (Widget as any)();
module.exports = w.made;
`,
	}
	got := runGraph(t, src, false) // AST default; must fall back for new.target
	if got != "Widget" {
		t.Fatalf("fallback result = %q, want %q", got, "Widget")
	}
}
