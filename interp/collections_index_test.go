package interp

import (
	"math"
	"strconv"
	"testing"
)

// These tests cover the orderedMap hash index that engages past
// mapIndexThreshold: hkey must faithfully encode SameValueZero equivalence
// classes, the linear→indexed transition must be transparent, and tombstone /
// re-insert / iterator semantics must be identical in both modes.

// --- hkey faithfulness: SameValueZero-equal iff hkey == -------------------

func TestHashKeyFaithful(t *testing.T) {
	bi := func(n int64) Value { return NewBigInt(n) }
	obj1, obj2 := NewObject(nil), NewObject(nil)
	sym1, sym2 := &Symbol{Desc: "s"}, &Symbol{Desc: "s"}
	groups := [][]Value{
		{Number(1), Number(1)},
		{Number(0), Number(math.Copysign(0, -1))}, // +0 and -0 are one key
		{Number(math.NaN()), Number(math.NaN())},  // all NaNs are one key
		{String("x"), newComputedString("x" + "")},
		{Boolean(true)},
		{Boolean(false)},
		{Undef},
		{Nul},
		{bi(1), bi(1)},
		{obj1}, {obj2},
		{sym1}, {sym2},
		{Number(1.5)},
		{String("1")}, // distinct from Number(1) and BigInt 1n
	}
	// Every value in a group shares one hkey; values in different groups differ.
	for gi := range groups {
		h0 := hashKey(groups[gi][0])
		for _, v := range groups[gi] {
			if hashKey(v) != h0 {
				t.Errorf("group %d: %v hashed differently from its group", gi, v)
			}
		}
		for gj := gi + 1; gj < len(groups); gj++ {
			if h0 == hashKey(groups[gj][0]) {
				t.Errorf("groups %d and %d collide: %v vs %v", gi, gj, groups[gi][0], groups[gj][0])
			}
		}
	}
}

// --- linear and indexed modes agree on a mixed workload --------------------

func TestOrderedMapIndexMatchesLinear(t *testing.T) {
	var m orderedMap
	// Insert well past the threshold with keys of every kind.
	keys := []Value{Undef, Nul, Boolean(true), Boolean(false), Number(math.NaN())}
	for n := 0; n < 40; n++ {
		keys = append(keys, Number(float64(n)))
		keys = append(keys, String("k"+strconv.Itoa(n)))
	}
	for idx, k := range keys {
		m.set(k, Number(float64(idx)))
	}
	if m.index == nil {
		t.Fatal("index should have been built past the threshold")
	}
	if m.size() != len(keys) {
		t.Fatalf("size = %d, want %d", m.size(), len(keys))
	}
	// Every key resolves to the value it was inserted with (last write wins;
	// here each key is unique so it's the insert index).
	for idx, k := range keys {
		if v, ok := m.get(k); !ok || v != Value(Number(float64(idx))) {
			t.Errorf("get(%v) = %v,%v; want %d", k, v, ok, idx)
		}
	}
	// A NaN key added once must dedupe and be findable (Go maps can't key on NaN
	// directly — the hkNaN class is what makes this work).
	m.set(Number(math.NaN()), String("nan2"))
	if v, ok := m.get(Number(math.NaN())); !ok || v != Value(String("nan2")) {
		t.Errorf("NaN key not updated in place: %v,%v", v, ok)
	}
	// -0 and +0 collapse.
	m.set(Number(math.Copysign(0, -1)), String("zero"))
	if v, ok := m.get(Number(0)); !ok || v != Value(String("zero")) {
		t.Errorf("-0/+0 not unified: %v,%v", v, ok)
	}
}

// --- tombstone + re-insert keeps the index consistent ----------------------

func TestOrderedMapIndexDeleteReinsert(t *testing.T) {
	var m orderedMap
	for n := 0; n < 30; n++ { // cross the threshold
		m.set(Number(float64(n)), Number(float64(n)))
	}
	// Delete a spread of keys, then confirm they're gone and the rest remain.
	for n := 0; n < 30; n += 3 {
		if !m.delete(Number(float64(n))) {
			t.Errorf("delete(%d) reported absent", n)
		}
	}
	for n := 0; n < 30; n++ {
		_, ok := m.get(Number(float64(n)))
		wantPresent := n%3 != 0
		if ok != wantPresent {
			t.Errorf("get(%d) present=%v, want %v", n, ok, wantPresent)
		}
	}
	// Re-insert a deleted key: it takes a fresh position (end) with a new value.
	m.set(Number(0), String("back"))
	if v, ok := m.get(Number(0)); !ok || v != Value(String("back")) {
		t.Errorf("re-inserted key = %v,%v", v, ok)
	}
	if m.size() != 20+1 { // 30 - 10 deleted + 1 re-added
		t.Errorf("size = %d, want 21", m.size())
	}
}

// --- through the engine: a large Map/Set behaves correctly -----------------

func TestLargeCollectionThroughEngine(t *testing.T) {
	cases := []struct{ src, want string }{
		// 1000 numeric keys, then random-access reads and a delete.
		{`let m=new Map(); for(let k=0;k<1000;k++) m.set(k,k*k);
		  m.get(999)===998001 && m.get(0)===0 && m.size===1000 && m.delete(500) && m.size===999 && !m.has(500)`, "true"},
		// string keys with a duplicate insert (must not grow).
		{`let m=new Map(); for(let k=0;k<500;k++) m.set("k"+k, k); m.set("k5", 999);
		  m.size===500 && m.get("k5")===999`, "true"},
		// Set dedupe + membership across the threshold, including NaN and -0.
		{`let s=new Set(); for(let k=0;k<300;k++){ s.add(k); s.add(k) } s.add(NaN); s.add(NaN); s.add(-0);
		  s.size===301 && s.has(NaN) && s.has(0) && s.has(150)`, "true"},
		// object identity keys.
		{`let m=new Map(); let ks=[]; for(let k=0;k<100;k++){let o={};ks.push(o);m.set(o,k)}
		  m.get(ks[42])===42 && !m.has({})`, "true"},
		// insertion order preserved after the index engages.
		{`let m=new Map(); for(let k=0;k<50;k++) m.set(k,k); [...m.keys()].join(",")===Array.from({length:50},(_,i)=>i).join(",")`, "true"},
	}
	for _, c := range cases {
		i := New()
		v, err := i.RunString("t", c.src)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if got, _ := i.ToStringV(i.ctx, v); got != c.want {
			t.Errorf("%q = %q, want %q", c.src, got, c.want)
		}
	}
}

// --- benchmark: N inserts+lookups is linear, not quadratic ------------------

func benchMapFill(b *testing.B, n int) {
	b.ReportAllocs()
	for iter := 0; iter < b.N; iter++ {
		var m orderedMap
		for k := 0; k < n; k++ {
			m.set(Number(float64(k)), Number(float64(k)))
		}
		for k := 0; k < n; k++ {
			m.get(Number(float64(k)))
		}
	}
}

func BenchmarkMapFill100(b *testing.B)  { benchMapFill(b, 100) }
func BenchmarkMapFill1000(b *testing.B) { benchMapFill(b, 1000) }
