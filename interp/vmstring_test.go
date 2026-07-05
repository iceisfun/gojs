package interp

import (
	"strings"
	"testing"
)

// These tests exercise the vmString representations directly (construction and
// upconversion), then through the engine (so the boundaries — equality, keys,
// property access, coercion — all see each kind). The invariant under test: a
// string's VALUE is independent of its representation, so every kind must be
// indistinguishable from the equivalent bare String at every boundary.

// --- direct construction of each kind ---------------------------------------

func TestVMStringKinds(t *testing.T) {
	long := strings.Repeat("abcd", 40) // 160 bytes, over metaStrThreshold
	cases := []struct {
		name string
		make func() Value
		want string
	}{
		{"cons", func() Value { return concatStrings(String("foo"), String("bar")) }, "foobar"},
		{"cons-nested", func() Value {
			return concatStrings(concatStrings(String("a"), String("b")), concatStrings(String("c"), String("d")))
		}, "abcd"},
		{"flat-boxed", func() Value { return newComputedString(long) }, long},
		{"external-copy", func() Value { return NewReadOnlyString([]byte("héllo world")) }, "héllo world"},
		{"external-borrow", func() Value { return NewBorrowedString([]byte("borrowed bytes here")) }, "borrowed bytes here"},
		{"slice-share", func() Value { return newSliceString(String(long), 4, 120) }, long[4 : 4+120]},
		{"slice-copy-small", func() Value { return newSliceString(String(long), 0, 8) }, long[0:8]},
	}
	for _, c := range cases {
		v := c.make()
		if got := stringValue(v); got != c.want {
			t.Errorf("%s: value = %q, want %q", c.name, got, c.want)
		}
		if v.Typeof() != "string" {
			t.Errorf("%s: Typeof = %q, want string", c.name, v.Typeof())
		}
		if !isStringish(v) {
			t.Errorf("%s: isStringish = false", c.name)
		}
		if got := stringByteLen(v); got != len(c.want) {
			t.Errorf("%s: byteLen = %d, want %d", c.name, got, len(c.want))
		}
		if got := codeUnitLenValue(v); got != codeUnitLen(c.want) {
			t.Errorf("%s: codeUnitLen = %d, want %d", c.name, got, codeUnitLen(c.want))
		}
	}
}

// --- flatten memoization ----------------------------------------------------

func TestVMStringFlattenMemoizes(t *testing.T) {
	r := concatStrings(String("hello "), String("world")).(*vmString)
	first := r.build()
	if r.flags&fFlattened == 0 {
		t.Fatal("build() did not set fFlattened")
	}
	if r.left != nil || r.right != nil {
		t.Error("build() did not release the cons children")
	}
	if second := r.build(); second != first {
		t.Errorf("memoized build differs: %q vs %q", first, second)
	}
}

// --- upconversion chains: value survives representation changes --------------

func TestVMStringUpconversion(t *testing.T) {
	// flat -> cons (via +) -> flat, with an astral character forcing non-ASCII.
	base := newComputedString(strings.Repeat("x", 100)) // boxed flat
	rope := concatStrings(base, String("\U0001F600"))   // cons(flat, astral)
	rope = concatStrings(rope, String("tail"))          // deeper cons
	want := strings.Repeat("x", 100) + "\U0001F600" + "tail"
	if got := stringValue(rope); got != want {
		t.Fatalf("cons chain value = %q, want %q", got, want)
	}
	// After flatten the utf16 length must count the astral char as 2 units.
	rs := rope.(*vmString)
	if got := rs.codeUnitLen(); got != codeUnitLen(want) {
		t.Errorf("utf16 len = %d, want %d", got, codeUnitLen(want))
	}
	if rs.isASCII() {
		t.Error("string with an astral char reported ASCII")
	}
	// slice of a cons: parent flattens, span is correct.
	sl := newSliceString(rope, 100, 4) // the astral char is 4 bytes at offset 100
	if got := stringValue(sl); got != "\U0001F600" {
		t.Errorf("slice of cons = %q, want astral face", got)
	}
}

// --- ASCII flag + cached view correctness -----------------------------------

func TestVMStringCachedView(t *testing.T) {
	asciiBoxed := newComputedString(strings.Repeat("ab", 50)).(*vmString)
	if !asciiBoxed.isASCII() {
		t.Error("ASCII string not flagged ASCII")
	}
	if asciiBoxed.units != nil {
		t.Error("ASCII string should never materialize a units slice")
	}
	// Non-ASCII: the units view is built once and reused (identity check).
	nonASCII := newComputedString(strings.Repeat("é", 100)).(*vmString)
	v1 := nonASCII.view()
	if nonASCII.units == nil {
		t.Fatal("non-ASCII view did not cache units")
	}
	v2 := nonASCII.view()
	if &v1.units[0] != &v2.units[0] {
		t.Error("non-ASCII view rebuilt the units slice instead of reusing the cache")
	}
}

// --- every kind is indistinguishable through the engine boundaries ----------

func TestVMStringThroughEngine(t *testing.T) {
	i := New()
	// Install builder functions returning each kind, all denoting "gojs-rocks!!".
	target := "gojs-rocks!!"
	i.SetGlobal("mkFlat", i.NewFunction("mkFlat", func(_ []Value) (Value, error) {
		return newComputedString(strings.Repeat(target, 6)[:len(target)]), nil // boxed flat, exact bytes
	}))
	i.SetGlobal("mkExt", i.NewFunction("mkExt", func(_ []Value) (Value, error) {
		return NewReadOnlyString([]byte(target)), nil
	}))
	i.SetGlobal("mkBorrow", i.NewFunction("mkBorrow", func(_ []Value) (Value, error) {
		return NewBorrowedString([]byte(target)), nil
	}))
	// A cons is produced by + in-script. Compare all forms for equality, use as a
	// property key, index, and length — each must match a plain literal.
	src := `
		const lit = "gojs-rocks!!";
		const forms = [mkFlat(), mkExt(), mkBorrow(), "gojs" + "-" + "rocks" + "!!"];
		let out = [];
		for (const f of forms) {
			out.push(f === lit);                       // strict equality
			out.push(f.length === lit.length);         // cached length
			out.push(f[0] === "g" && f[f.length-1] === "!"); // indexing
			out.push(({[f]: 1})[lit] === 1);           // property-key round-trip
			out.push((f + "X") === lit + "X");         // concat
		}
		out.every(Boolean);
	`
	v, err := i.RunString("main", src)
	if err != nil {
		t.Fatal(err)
	}
	if v != Value(Boolean(true)) {
		t.Errorf("some string kind was distinguishable from a literal: %#v", v)
	}
}

// --- the .length / charCodeAt loops that were O(n^2) now scale ---------------

func TestVMStringHotLoopsCorrect(t *testing.T) {
	i := New()
	// join -> big boxed flat string, then length + charCodeAt walk.
	src := `
		let a = new Array(5000); for (let k=0;k<a.length;k++) a[k]='z';
		const s = a.join('');            // boxed flat, cached length
		let acc = 0;
		for (let i=0;i<s.length;i++) acc = (acc + s.charCodeAt(i)) >>> 0;
		[s.length, acc];
	`
	v, err := i.RunString("main", src)
	if err != nil {
		t.Fatal(err)
	}
	arr := v.(*Object)
	n, _ := arr.GetStr(i.ctx, "0")
	acc, _ := arr.GetStr(i.ctx, "1")
	if n != Value(Number(5000)) {
		t.Errorf("length = %v, want 5000", n)
	}
	if acc != Value(Number(5000*122)) { // 'z' == 122
		t.Errorf("charCodeAt sum = %v, want %d", acc, 5000*122)
	}
}
