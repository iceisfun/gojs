package interp

import (
	"context"
	"strings"
	"testing"
)

// capturePrint records console output so a module's side effects (not just its
// exports) are part of the differential comparison.
type capturePrint struct{ b *strings.Builder }

func (c capturePrint) Print(_ context.Context, msg string) { c.b.WriteString(msg + "\n") }
func (c capturePrint) Warn(_ context.Context, msg string)  { c.b.WriteString("W:" + msg + "\n") }

// moduleResult requires "entry.js" from srcs on a fresh interpreter (VM default,
// or tree-walker when treeWalker is true) and returns a normalized string of the
// module's exports plus everything it printed and any thrown error — so the two
// engines can be compared for exact agreement over the module-body path
// (compileTopLevelBody), which Test262 does not exercise.
func moduleResult(treeWalker bool, srcs map[string]string) string {
	var out strings.Builder
	opts := []Option{
		WithModuleProvider(NewMapModuleProvider(srcs)),
		WithPrintProvider(capturePrint{b: &out}),
	}
	if treeWalker {
		opts = append(opts, WithTreeWalker())
	}
	i := New(opts...)
	v, err := i.RunString("main", `JSON.stringify(require("entry.js"))`)
	res := "OUT[" + out.String() + "]"
	if err != nil {
		if t, ok := ThrownValue(err); ok {
			s, _ := i.ToStringV(i.ctx, t)
			return res + " E:" + s
		}
		return res + " E:" + err.Error()
	}
	s, _ := i.ToStringV(i.ctx, v)
	return res + " V:" + s
}

// moduleDiffCases each define an "entry.js" (and sometimes helper modules) that
// exercise a module-specific semantic: the pre-bound free variables (module,
// exports, require, __filename, __dirname), `this` === exports, top-level
// return, and the let/const/var/for-let slot path at module scope.
var moduleDiffCases = []map[string]string{
	// let/const/for-let slot path (the perf target) at module scope.
	{"entry.js": `let s=0; for(let i=0;i<10;i++){ s+=i } module.exports = s`},
	{"entry.js": `const a=2, b=3; exports.p = a*b; exports.q = a+b`},
	{"entry.js": `const xs=[1,2,3,4]; let t=0; for(let i=0;i<xs.length;i++) t+=xs[i]; module.exports=t`},
	// this === exports at module top level.
	{"entry.js": `this.x = 7; this.y = this.x + 1; module.exports = {x: exports.x, y: this.y}`},
	// module.exports reassigned wholesale.
	{"entry.js": `let v = 41; module.exports = { get(){ return v+1 } }; module.exports = module.exports.get()`},
	// top-level return short-circuits the rest of the body.
	{"entry.js": `exports.a = 1; if (true) { module.exports = "early"; return } exports.a = 999`},
	// var at module top level (function-scoped slot).
	{"entry.js": `var n = 0; for (var k=0;k<5;k++) n += k; module.exports = n`},
	// var hoisting: used before its declaration in source order.
	{"entry.js": `module.exports = (function(){ return typeof later })(); var later = 1`},
	// TDZ at module top level (let read before init) — must ReferenceError in both.
	{"entry.js": `try { void before; module.exports = "no-throw" } catch(e){ module.exports = e.constructor.name } let before = 1`},
	// const reassignment at module top level (declines slot mode; still throws).
	{"entry.js": `const c = 1; try { c = 2; module.exports = "no-throw" } catch(e){ module.exports = e.constructor.name }`},
	// block shadowing at module scope.
	{"entry.js": `let x = 1; { let x = 2; exports.inner = x } exports.outer = x; module.exports = {inner: exports.inner, outer: exports.outer}`},
	// __filename / __dirname free variables.
	{"entry.js": `module.exports = (typeof __filename) + ":" + (typeof __dirname)`},
	// a binding shadowing a reserved free var → declines slot mode, still correct.
	{"entry.js": `let require = 5; module.exports = require + 1`},
	// requiring another module (nested require through the compiled body).
	{"entry.js": `const m = require("./dep.js"); module.exports = m.double(21)`, "dep.js": `exports.double = x => x*2`},
	// a module that defines a function (NOT closure-free) → tree-walker fallback path.
	{"entry.js": `function sq(x){ return x*x } let r=0; for(let i=1;i<=4;i++) r+=sq(i); module.exports = r`},
	// throwing at module top level.
	{"entry.js": `let ok=1; throw new TypeError("boom-"+ok)`},
	// globals reachable through the module env chain.
	{"entry.js": `module.exports = Math.max(3, 9) + Array.of(1,2,3).length`},
	// console side effect ordering interleaved with a loop.
	{"entry.js": `for (let i=0;i<3;i++) console.log("i="+i); module.exports = "done"`},
	// while + let block body at module scope.
	{"entry.js": `let n=27, steps=0; while(n!==1){ if(n%2) n=3*n+1; else n=n/2; steps++ } module.exports = steps`},
	// a strict, closure-free module still takes the slot path (compiled) and agrees.
	{"entry.js": `"use strict"; let r=0; for(let i=0;i<4;i++) r+=i*i; module.exports = r`},
	// the CommonJS idiom: closure-free body exporting an object literal (compiled
	// via opNewObject/opDefField) — the prime-sieve-module shape.
	{"entry.js": `const limit=100; let count=0,last=0; for(let i=2;i<=limit;i++){ let p=1; for(let d=2;d*d<=i;d++) if(i%d===0){p=0;break} if(p){count++;last=i} } module.exports = {limit, count, last}`},
	{"entry.js": `let a=1, b=2; module.exports = {a, b, sum:a+b, nested:{x:a*b}}`},
}

func TestModuleBytecodeDiff(t *testing.T) {
	for _, srcs := range moduleDiffCases {
		tw := moduleResult(true, srcs)
		bc := moduleResult(false, srcs)
		if tw != bc {
			t.Errorf("module divergence:\n  entry: %s\n  tree-walker: %s\n  bytecode:    %s", srcs["entry.js"], tw, bc)
		}
	}
}
