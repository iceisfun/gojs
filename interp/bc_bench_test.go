package interp

import "testing"

// Benchmarks comparing the tree-walker against the bytecode VM on function-heavy
// hot code. Run: go test ./interp -run x -bench 'Bytecode|TreeWalker' -benchmem

var benchPrograms = map[string]string{
	// recursion-heavy
	"fib": `function fib(n){ return n < 2 ? n : fib(n-1) + fib(n-2) } fib(27)`,
	// tight arithmetic loop
	"loop": `function run(){ var s = 0; for (var i = 0; i < 200000; i++){ s += i * 2 - 1 } return s } run()`,
	// nested loops + locals
	"nested": `function run(){ var t = 0; for (var i = 0; i < 400; i++){ for (var j = 0; j < 400; j++){ t += (i ^ j) & 7 } } return t } run()`,
	// method calls + property access on a plain object
	"methods": `function run(){ var o = { n: 0, add(x){ this.n = this.n + x; return this.n } }; var s = 0; for (var i = 0; i < 100000; i++){ s = o.add(i) } return s } run()`,
}

func benchEngine(b *testing.B, src string, bytecode bool) {
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var i *Interpreter
		if bytecode {
			i = New(WithBytecode())
		} else {
			i = New()
		}
		if _, err := i.RunString("bench", src); err != nil {
			b.Fatal(err)
		}
		i.Close()
	}
}

func BenchmarkTreeWalker(b *testing.B) {
	for name, src := range benchPrograms {
		b.Run(name, func(b *testing.B) { benchEngine(b, src, false) })
	}
}

func BenchmarkBytecode(b *testing.B) {
	for name, src := range benchPrograms {
		b.Run(name, func(b *testing.B) { benchEngine(b, src, true) })
	}
}
