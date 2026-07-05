package interp

import "testing"

// Allocation-focused benchmarks for the value-boxing / call-frame hot paths.
// Unlike BenchmarkBytecode (which recreates the interpreter each iteration and
// so measures bootstrap too), these reuse ONE interpreter and call a prebuilt
// `run` function, so -benchmem reports the steady-state per-call and per-op
// allocation of the VM itself. Regression guard for the Number-interning, lazy
// scope-map, and frame-pool work.
//
//	go test ./interp -run x -bench Perf -benchmem
var perfPrograms = map[string]string{
	// recursion + integer arithmetic: exercises call-frame/env/args churn.
	"fib": `function fib(n){ return n < 2 ? n : fib(n-1) + fib(n-2) } function run(){ return fib(30) }`,
	// tight arithmetic loop: operand boxing with large (un-interned) results.
	"loop": `function run(){ var s = 0; for (var i = 0; i < 1000000; i++){ s += i * 2 - 1 } return s }`,
	// escape-time inner loop: float arithmetic plus a small (interned) counter.
	"mandel": `function run(){var sum=0;for(var y=0;y<200;y++){for(var x=0;x<200;x++){var cr=x/100-1.5,ci=y/100-1,zr=0,zi=0,n=0;while(n<100){var zr2=zr*zr,zi2=zi*zi;if(zr2+zi2>4)break;zi=2*zr*zi+ci;zr=zr2-zi2+cr;n++}sum+=n}}return sum}`,
	// property read+write heavy: exercises opGetProp/opSetProp on a fixed shape.
	"props": `function run(){var o={a:0,b:0,c:0};for(var i=0;i<500000;i++){o.a=o.b+o.c+i;o.b=o.a-o.c;o.c=o.a+o.b}return o.a+o.b+o.c}`,
}

func benchPerf(b *testing.B, name string) {
	i := New(WithBytecode())
	if _, err := i.RunString("setup", perfPrograms[name]); err != nil {
		b.Fatal(err)
	}
	run := i.GetGlobal("run")
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := i.Call(run, Undef); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPerfFib(b *testing.B)    { benchPerf(b, "fib") }
func BenchmarkPerfLoop(b *testing.B)   { benchPerf(b, "loop") }
func BenchmarkPerfMandel(b *testing.B) { benchPerf(b, "mandel") }

func BenchmarkPerfProps(b *testing.B) { benchPerf(b, "props") }
