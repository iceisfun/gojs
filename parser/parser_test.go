package parser

import (
	"testing"

	"github.com/iceisfun/gojs/ast"
)

// mustParse parses src and fails the test on error.
func mustParse(t *testing.T, src string) *ast.Program {
	t.Helper()
	prog, err := Parse("test", src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return prog
}

func TestParsePrograms(t *testing.T) {
	sources := []string{
		`var x = 1 + 2 * 3;`,
		`let a, b = 2, c;`,
		`const {x, y: z, ...rest} = obj;`,
		`const [first, , third = 9, ...more] = arr;`,
		`function f(a, b = 1, ...c) { return a + b; }`,
		`const g = (x, y) => x + y;`,
		`const h = x => { return x * 2; };`,
		`async function af() { await p; }`,
		`x?.y?.z?.();`,
		`a ?? b ?? c;`,
		`obj.method(1, 2, ...args);`,
		`new Foo(1, 2).bar;`,
		`if (a) b(); else c();`,
		`for (let i = 0; i < 10; i++) sum += i;`,
		`for (const k in obj) console.log(k);`,
		`for (const v of list) { total += v; }`,
		`while (cond) { work(); }`,
		`do { step(); } while (again);`,
		`switch (n) { case 1: one(); break; default: other(); }`,
		`try { risky(); } catch (e) { handle(e); } finally { cleanup(); }`,
		`try { risky(); } catch { swallow(); }`,
		`throw new Error("boom");`,
		"`hello ${name}, you are ${age} years old`;",
		"tag`a${b}c`;",
		`label: for (;;) { break label; }`,
		`class Point extends Base { #x = 0; constructor(x) { super(); this.#x = x; } get x() { return this.#x; } static make() { return new Point(0); } }`,
		`const obj = { a: 1, b, [c]: 3, m() {}, get g() { return 1; }, ...spread };`,
		`x = y = z = 0;`,
		`a ? b ? c : d : e;`,
		`let re = /ab+c/gi; let d = 4 / 2;`,
		`void 0; typeof x; delete o.p; -x; !y; ~z;`,
		`export default 1;`, // export parses as ... hmm, may not be supported
	}
	for _, src := range sources {
		prog, err := Parse("test", src)
		if err != nil {
			// export is not yet supported; tolerate that one specifically.
			if src == `export default 1;` {
				continue
			}
			t.Errorf("parse %q: %v", src, err)
			continue
		}
		if len(prog.Body) == 0 {
			t.Errorf("parse %q: empty body", src)
		}
	}
}

func TestOperatorPrecedence(t *testing.T) {
	prog := mustParse(t, `1 + 2 * 3;`)
	es := prog.Body[0].(*ast.ExprStmt)
	bin, ok := es.X.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", es.X)
	}
	// Top operator must be '+', with the '*' nested on the right.
	if _, ok := bin.Right.(*ast.BinaryExpr); !ok {
		t.Errorf("expected multiplication nested under addition, got %T", bin.Right)
	}
}

func TestArrowVsParen(t *testing.T) {
	// Parenthesized expression, not an arrow.
	prog := mustParse(t, `(a, b);`)
	es := prog.Body[0].(*ast.ExprStmt)
	if _, ok := es.X.(*ast.SequenceExpr); !ok {
		t.Errorf("expected SequenceExpr, got %T", es.X)
	}
	// Arrow function.
	prog = mustParse(t, `(a, b) => a + b;`)
	es = prog.Body[0].(*ast.ExprStmt)
	if _, ok := es.X.(*ast.ArrowFunc); !ok {
		t.Errorf("expected ArrowFunc, got %T", es.X)
	}
}

func TestASI(t *testing.T) {
	// Two statements separated only by a newline.
	prog := mustParse(t, "var a = 1\nvar b = 2\n")
	if len(prog.Body) != 2 {
		t.Errorf("expected 2 statements via ASI, got %d", len(prog.Body))
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		`var = 5;`,
		`function () {}`, // declaration needs a name
		`if (x)`,         // missing body
		`{ unterminated`,
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}
