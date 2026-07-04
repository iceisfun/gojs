package interp

import "testing"

// bcResult runs src and returns a normalized "V:<value>" / "E:<error>" string,
// so the tree-walker and the bytecode VM can be compared for exact agreement.
func bcResult(bytecode bool, src string) string {
	var i *Interpreter
	if bytecode {
		i = New(WithBytecode())
	} else {
		i = New()
	}
	v, err := i.RunString("diff", src)
	if err != nil {
		if t, ok := ThrownValue(err); ok {
			s, _ := i.ToStringV(i.ctx, t)
			return "E:" + s
		}
		return "E:" + err.Error()
	}
	s, _ := i.ToStringV(i.ctx, v)
	return "V:" + s
}

// bcDiffCases exercise features the compiler lowers natively AND the escape-hatch
// fallbacks (object literals, destructuring, for-of, switch, try, compound/logical
// assignment), especially where a fallback subtree is mixed with compiled control
// flow. Each script's last expression statement is its completion value.
var bcDiffCases = []string{
	// arithmetic / precedence / coercion
	`function f(){ return 1 + 2 * 3 - 4 / 2 } f()`,
	`function f(){ return 2 ** 10 % 7 } f()`,
	`function f(){ return "a" + 1 + true + null + undefined } f()`,
	`function f(){ return 10 & 6, 10 | 1, 10 ^ 3, 1 << 4, -8 >> 1, -8 >>> 28 } f()`,
	`function f(){ return -(-5) + +"3" + ~0 + (!false) } f()`,
	`function f(){ return 3n * 4n - 1n } f()`,
	`function f(){ return -5n } f()`,

	// comparisons / equality
	`function f(){ return (1 < 2) + (2 <= 2) + (3 > 4) + ("a" == "a") + (1 === "1") + (null == undefined) } f()`,
	`function f(a){ return a instanceof Array } f([1])`,

	// control flow: if / while / do / for / break / continue
	`function f(){ let s=0; for(var i=0;i<5;i++) s+=i; return s } f()`,
	`function f(){ let s=0,i=0; while(i<10){ if(i==5) break; s+=i; i++ } return s } f()`,
	`function f(){ let s=0,i=0; do { i++; if(i%2) continue; s+=i } while(i<6); return s } f()`,
	`function f(){ let s=0; for(var i=0;i<4;i++){ for(var j=0;j<4;j++){ if(j==2) break; s+=j } } return s } f()`,
	`function f(){ let s=0; outer: for(var i=0;i<3;i++){ s+=i } return s } f()`, // no real label use
	`function f(n){ return n<=1 ? 1 : n*arguments.callee(n-1) } f(5)`,

	// recursion + closures
	`function fib(n){ return n<2 ? n : fib(n-1)+fib(n-2) } fib(15)`,
	`function mk(){ let c=0; return function(){ return ++c } } var g=mk(); g(); g(); g()`,
	`function f(){ var out=[]; for(var i=0;i<3;i++){ out.push(function(){return i}) } return out[0]()+out[1]()+out[2]() } f()`,
	`function f(){ var out=[]; for(let i=0;i<3;i++){ out.push(function(){return i}) } return out[0]()+out[1]()+out[2]() } f()`,

	// arrows, this, methods
	`function f(){ return ((a,b)=>a*b)(6,7) } f()`,
	`var o={x:10, get(){ return this.x }}; o.get()`,
	`var o={x:5, make(){ return ()=>this.x }}; o.make()()`,
	`function f(){ return [1,2,3].map(x=>x*x).join(",") } f()`,

	// strings / templates / typeof
	"function f(x){ return `v=${x+1} ${typeof x}` } f(41)",
	`function f(){ return typeof notDefined + " " + typeof f } f()`,
	`function f(){ let a="x"; return typeof a } f()`,

	// logical short-circuit with side effects
	`function f(){ let hits=0; let g=()=>{hits++;return 0}; let r = g() || g() || 7; return r + ":" + hits } f()`,
	`function f(){ let a=null; return (a ?? "d") + (a?.x ?? "e") } f()`,
	`function f(){ return (true && "y") + (false || "z") } f()`,

	// member get/set on objects & arrays
	`function f(){ var a=[0,0,0]; a[1]=9; a["2"]=8; return a[0]+a[1]+a[2] } f()`,
	`function f(){ var o={}; o.a=1; o.b=o.a+1; return o.a+o.b } f()`,
	`function f(){ var a=[1,2,3]; return a.length + a.indexOf(2) } f()`,

	// new / constructors
	`function P(x){ this.x=x } var p=new P(7); p.x`,
	`function f(){ return new Array(1,2,3).length } f()`,

	// --- escape-hatch fallbacks mixed with compiled control flow ---
	`function f(){ let o={a:1,b:2,c:3}; let s=0; for(const k in o) s+=o[k]; return s } f()`,
	`function f(){ let s=0; for(const v of [1,2,3,4]) { if(v==3) break; s+=v } return s } f()`,
	`function f(x){ switch(x){ case 1: return "one"; case 2: return "two"; default: return "other" } } f(2)`,
	`function f(){ let s=0; for(var i=0;i<5;i++){ try { if(i==3) break; s+=i } finally { s+=100 } } return s } f()`,
	`function f(){ let s=0; for(var i=0;i<5;i++){ try { if(i==2) continue; s+=i } finally { s+=1 } } return s } f()`,
	`function f(){ let x=10; x+=5; x*=2; x-=3; return x } f()`, // compound assign (fallback)
	`function f(){ let a=0; a ||= 5; a &&= 9; return a } f()`,   // logical assign (fallback)
	`function f(){ let {p, q=2} = {p:1}; return p+q } f()`,      // destructuring (fallback)
	`function f(){ let [a,,c] = [1,2,3]; return a+c } f()`,
	`function f(){ let o={x:1}; let {x, y="d"} = o; return x+y } f()`,
	`function f(){ return [...[1,2],...[3,4]].length } f()`, // spread (fallback)
	`function f(){ let s=0; [10,20,30].forEach(v=>{s+=v}); return s } f()`,

	// throw / catch across the compile boundary
	`function f(){ try { throw new Error("boom") } catch(e){ return e.message } } f()`,
	`function f(){ try { let x = null; return x.y } catch(e){ return e.name } } f()`,
	`function inner(){ throw new TypeError("t") } function f(){ try { inner() } catch(e){ return e.constructor.name } } f()`,

	// nested functions / hoisting
	`function f(){ return g()+2; function g(){ return 40 } } f()`,
	`function f(){ var r=""; function a(){r+="a"} function b(){r+="b"} a();b(); return r } f()`,

	// exceptions that should propagate identically
	`function f(){ return undefinedFn() } f()`,
	`function f(){ null.prop = 1 } f()`,
	`function f(){ const c=1; c=2 } f()`,

	// regression: strict assignment to an undeclared name throws ReferenceError
	`"use strict"; function f(){ undeclaredX = 1 } try { f() } catch(e){ e.name }`,
	`"use strict"; function f(){ try { undeclaredX = 1; return "no" } catch(e){ return e.name } } f()`,
	// regression: method callee property is fetched BEFORE arguments are evaluated
	`function f(){ var log=0; var o={}; try { o.a.b(log=1) } catch(e){} return log } f()`,
	`function f(){ var seq=[]; var o={m(){seq.push("m")}}; o.m(seq.push("arg")); return seq.join(",") } f()`,
	// regression: super() reached from within an arrow inside a derived constructor
	`class A{constructor(){this.x=1}} class B extends A{constructor(){ (()=>{ super() })(); this.y=2 }} function f(){ var b=new B(); return b.x+b.y } f()`,
	// regression: parenthesized assignment target suppresses NamedEvaluation
	`function f(){ var g; (g) = function(){}; return g.name } f()`,
	// regression: template must ToString each part into a flat string (not a
	// ToPrimitive-default `+`-chain rope) — object with asymmetric valueOf/toString
	"function f(){ var o={valueOf(){return 1}, toString(){return 's'}}; return `${o}` } f()",
	// regression: a template result is a real string accepted by String-strict
	// builtins (RegExp.escape rejected a rope)
	"function f(){ return RegExp.escape(`.${'a'}`) } f()",
	"function f(){ return `n=${1+2} b=${true} u=${undefined} nil=${null}` } f()",
	// regression: new evaluates args before the isConstructor check
	`var x = {}; function f(){ try { new x(x = Array) } catch(e){} return x === Array } f()`,
	// regression: null[key] throws TypeError before the key's toString runs
	`function f(){ var hit=0; var k={toString(){hit=1;return "p"}}; try { (null)[k] } catch(e){} return hit + ":" + (typeof null) } f()`,
}

func TestBytecodeDiff(t *testing.T) {
	for _, src := range bcDiffCases {
		tw := bcResult(false, src)
		bc := bcResult(true, src)
		if tw != bc {
			t.Errorf("divergence:\n  src: %s\n  tree-walker: %s\n  bytecode:    %s", src, tw, bc)
		}
	}
}
