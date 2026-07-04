package ts

import (
	"testing"

	"github.com/iceisfun/gojs/interp"
)

// TestLowerDifferential is the oracle: for each self-contained TypeScript program
// the AST path (Lower) and the text path (Transpile + parser) must produce the
// same completion value, or both throw. Any divergence is a Lower bug.
func TestLowerDifferential(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"arith", `const a = 2 + 3 * 4 - (5 % 3) ** 2; a;`},
		{"ternary_logical", `const x = 5; (x > 3 && x < 10 ? "mid" : "no") + (0 || "d") + (null ?? "n");`},
		{"template", "const n = 3; `n=${n} sq=${n*n}`;"},
		{"string_ops", `"abc".toUpperCase() + [1,2,3].map(v => v*2).join(",");`},
		{"fib", `function fib(n: number): number { return n < 2 ? n : fib(n-1) + fib(n-2); } fib(15);`},
		{"closure", `function mk(){ let c = 0; return () => ++c; } const f = mk(); f(); f(); f();`},
		{"let_const_block", `let s = 0; { const s = 10; } for (let i=0;i<5;i++){ s += i; } s;`},
		{"forof_forin", `let t = 0; for (const v of [10,20,30]) t += v; for (const k in {a:1,b:2}) t += k.length; t;`},
		{"while_do", `let i = 0, acc = 1; while (i < 4) { acc *= 2; i++; } do { acc += 1; } while (false); acc;`},
		{"switch", `function g(n){ switch(n){ case 1: return "one"; case 2: return "two"; default: return "many"; } } g(2) + g(9);`},
		{"trycatch", `let out = ""; try { throw new Error("boom"); } catch (e) { out = (e as Error).message; } finally { out += "!"; } out;`},
		{"class_basic", `class A { x: number; constructor(x: number){ this.x = x; } get d(){ return this.x*2; } inc(){ return ++this.x; } } const o = new A(5); o.inc(); o.d;`},
		{"class_extends", `class B { greet(){ return "b"; } } class C extends B { greet(){ return super.greet() + "c"; } } new C().greet();`},
		{"class_static_field", `class K { static count = 0; v = 7; static make(){ K.count++; return new K(); } } K.make(); K.make(); K.count * 100 + new K().v;`},
		{"destructure_array", `const [a, , b, ...rest] = [1,2,3,4,5]; a + b + rest.length;`},
		{"destructure_object", `const { p, q: r = 9, ...others } = { p: 1, s: 2, t: 3 }; p + r + Object.keys(others).length;`},
		{"spread", `const xs = [1,2,3]; const ys = [0, ...xs, 4]; Math.max(...ys) + ys.length;`},
		{"object_methods", `const k = "dyn"; const obj = { a: 1, [k]: 2, m(){ return this.a + this.dyn; }, get g(){ return 100; } }; obj.m() + obj.g;`},
		{"enum", `enum Color { Red = 1, Green, Blue } Color.Blue * 10 + Color[Color.Green].length;`},
		{"namespace", `namespace N { export const v = 42; export function f(){ return v + 1; } } N.f();`},
		{"typeof_instanceof", `const r = []; (typeof r) + ":" + (r instanceof Array) + ":" + ("length" in r);`},
		{"optional_chain", `const o: any = { a: { b: 3 } }; (o?.a?.b ?? 0) + (o?.x?.y ?? 7);`},
		{"generator", `function* gen(){ yield 1; yield 2; yield 3; } let s = 0; for (const v of gen()) s += v; s;`},
		{"regex", `const m = "2026-07-04".match(/(\d+)-(\d+)-(\d+)/); m ? Number(m[1]) + Number(m[3]) : -1;`},
		{"exp_bitops", `(2 ** 10) + (0xff & 0x0f) + (1 << 4) + (255 >>> 4);`},
		{"update_seq", `let i = 5; const r = (i++, ++i, i--); r + ":" + i;`},
		{"default_rest_params", `function f(a: number, b = 10, ...rest: number[]) { return a + b + rest.reduce((s,v)=>s+v, 0); } f(1) + ":" + f(1,2,3,4);`},
		{"private_fields", `class Counter { #n = 0; bump(){ this.#n++; return this.#n; } has(){ return #n in this; } } const c = new Counter(); c.bump(); c.bump() + ":" + c.has();`},
		{"getter_setter", `class Temp { #c = 0; get f(){ return this.#c * 9/5 + 32; } set f(v){ this.#c = (v-32)*5/9; } } const t = new Temp(); t.f = 212; Math.round(t.f);`},
		{"computed_assign", `const o: any = { arr: [1,2,3] }; const k = "arr"; o[k][1] += 40; o.arr[1] *= 2; o.arr.join(",");`},
		{"labeled_break", `let count = 0; outer: for (let i=0;i<5;i++){ for (let j=0;j<5;j++){ if (i*j > 6) break outer; count++; } } count;`},
		{"labeled_continue", `let s = 0; loop: for (let i=0;i<4;i++){ for (let j=0;j<4;j++){ if (j===2) continue loop; s += 1; } } s;`},
		{"tagged_template", `function tag(strings: TemplateStringsArray, ...vals: number[]){ return strings.join("|") + "#" + vals.join(","); } const a=1,b=2; tag` + "`x${a}y${b}z`" + `;`},
		{"nested_destructure", `const { a: { b }, c: [d, e] } = { a: { b: 5 }, c: [6, 7] }; b + d + e;`},
		{"do_continue", `let i = 0, s = 0; do { i++; if (i % 2 === 0) continue; s += i; } while (i < 6); s;`},
		{"void_delete", `const o: any = { x: 1, y: 2 }; delete o.x; (void 0) === undefined ? Object.keys(o).join("") : "no";`},
		{"chained_optional_call", `const o: any = { f(){ return { g: () => 42 }; } }; (o.f?.().g?.() ?? -1) + (o.none?.() ?? 7);`},
		{"array_methods", `[1,2,3,4,5,6].filter(x=>x%2===0).map(x=>x*x).reduce((a,b)=>a+b,0);`},
		{"exponent_assoc", `2 ** 3 ** 2;`}, // right-assoc => 512
		{"comma_in_for", `let out = ""; for (let i=0, j=10; i<j; i++, j--){ out += i + "" + j + " "; } out.trim();`},
		// The prime.ts sieve (corrected & bounded): TypedArray, Math, nested loops,
		// labeled-free control flow, typed array literal, template strings.
		{"sieve", `
const limit = 1000;
const composite = new Uint8Array(limit + 1);
const sqrt = Math.floor(Math.sqrt(limit));
for (let p = 2; p <= sqrt; p++) {
    if (composite[p]) continue;
    for (let n = p * p; n <= limit; n += p) composite[n] = 1;
}
const primes: number[] = [];
for (let i = 2; i <= limit; i++) {
    if (!composite[i]) primes.push(i);
}
` + "`count=${primes.length} last=${primes[primes.length - 1]}`" + `;`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			textVM := interp.New()
			astVM := interp.New()

			textVal, textErr := RunString(textVM, tc.name+".ts", tc.src)
			astVal, astErr := RunStringAST(astVM, tc.name+".ts", tc.src)

			switch {
			case textErr != nil && astErr != nil:
				// Both threw/failed — acceptable, but the messages should agree so
				// we don't mask a Lower bug behind a coincidental error.
				if textErr.Error() != astErr.Error() {
					t.Fatalf("both erred but differently:\n text: %v\n  ast: %v", textErr, astErr)
				}
				return
			case textErr != nil:
				t.Fatalf("text path erred, ast path ok:\n text: %v\n  ast value: %v", textErr, astVal)
			case astErr != nil:
				t.Fatalf("ast path erred, text path ok:\n  ast: %v\n text value: %v", astErr, textVal)
			}

			ts, _ := textVM.ToString(textVal)
			as, _ := astVM.ToString(astVal)
			if ts != as {
				t.Fatalf("value mismatch: text=%q ast=%q", ts, as)
			}
		})
	}
}
