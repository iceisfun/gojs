package interp

import (
	"strconv"
	"testing"
)

// Benchmarks for the string-construction and metadata-cache paths that a
// first-class vmString targets. Run: go test ./interp -bench VMString -benchmem
//
// The regressions these guard against were all O(n²): Array.join built with
// Go-string concat, and `s.length` / `s.charCodeAt(i)` re-scanning the string on
// every read inside a loop. Each benchmark drives the pattern through the engine.

func benchRun(b *testing.B, body string) {
	b.Helper()
	i := New()
	// Wrap in an IIFE so the top-level lexicals don't persist across RunString
	// on the shared interpreter (they would collide on the second iteration).
	src := "(function(){\n" + body + "\n})()"
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := i.RunString("bench", src); err != nil {
			b.Fatal(err)
		}
	}
}

// Array.join of many elements — the O(n²)-if-naive builder.
func BenchmarkVMStringJoin(b *testing.B) {
	benchRun(b, `
		let a = new Array(20000);
		for (let k=0;k<a.length;k++) a[k]=k;
		a.join(",").length;
	`)
}

// `for (i<s.length)` — length read every iteration; O(n²) without a cached length.
func BenchmarkVMStringLengthLoop(b *testing.B) {
	benchRun(b, `
		let a = new Array(20000); for (let k=0;k<a.length;k++) a[k]='x';
		const s = a.join('');
		let c = 0;
		for (let i=0;i<s.length;i++) c++;
		c;
	`)
}

// charCodeAt walk — per-index view rebuild is O(n²) without a cached view.
func BenchmarkVMStringCharCodeAtLoop(b *testing.B) {
	benchRun(b, `
		let a = new Array(20000); for (let k=0;k<a.length;k++) a[k]='x';
		const s = a.join('');
		let acc = 0;
		for (let i=0;i<s.length;i++) acc = (acc + s.charCodeAt(i)) >>> 0;
		acc;
	`)
}

// Rope accumulation via += — O(1) per append with the cons representation.
func BenchmarkVMStringConcatChain(b *testing.B) {
	benchRun(b, `
		let s = '';
		for (let i=0;i<20000;i++) s += 'ab';
		s.length;
	`)
}

// Direct (no-engine) construction + flatten of each kind, to isolate the
// representation cost from interpreter overhead.
func BenchmarkVMStringBuildKinds(b *testing.B) {
	chunks := make([]Value, 256)
	for i := range chunks {
		chunks[i] = String("chunk-" + strconv.Itoa(i) + ";")
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var acc Value = String("")
		for _, c := range chunks {
			acc = concatStrings(acc, c)
		}
		_ = stringValue(acc) // flatten
	}
}
