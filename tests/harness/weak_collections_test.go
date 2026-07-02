package harness

import "testing"

// These tests pin the ES2023+ semantics of the weak-collection family:
// WeakMap, WeakSet, WeakRef, and FinalizationRegistry.

func TestWeakMapSymbolKeys(t *testing.T) {
	Expect(t, `
		var m = new WeakMap();
		var s = Symbol("k");
		assert.sameValue(m.has(s), false);
		assert.sameValue(m.get(s), undefined);
		assert.sameValue(m.set(s, 42), m);
		assert.sameValue(m.has(s), true);
		assert.sameValue(m.get(s), 42);
		// Well-known symbols may be held weakly.
		m.set(Symbol.iterator, 7);
		assert.sameValue(m.get(Symbol.iterator), 7);
		assert.sameValue(m.delete(s), true);
		assert.sameValue(m.has(s), false);
	`)
}

func TestWeakMapRegisteredSymbolRejected(t *testing.T) {
	// A registered symbol (Symbol.for) is NOT CanBeHeldWeakly.
	ExpectError(t, `new WeakMap().set(Symbol.for("x"), 1);`, "TypeError")
}

func TestWeakMapPrimitiveKey(t *testing.T) {
	ExpectError(t, `new WeakMap().set(1, 1);`, "TypeError")
	// has/get/delete with a primitive key do not throw; they report absence.
	Expect(t, `
		var m = new WeakMap();
		assert.sameValue(m.has(1), false);
		assert.sameValue(m.get("a"), undefined);
		assert.sameValue(m.delete(true), false);
	`)
}

func TestWeakMapToStringTag(t *testing.T) {
	Expect(t, `
		assert.sameValue(WeakMap.prototype[Symbol.toStringTag], "WeakMap");
		assert.sameValue(Object.prototype.toString.call(new WeakMap()), "[object WeakMap]");
		var d = Object.getOwnPropertyDescriptor(WeakMap.prototype, Symbol.toStringTag);
		assert.sameValue(d.writable, false);
		assert.sameValue(d.enumerable, false);
		assert.sameValue(d.configurable, true);
	`)
}

func TestWeakMapRequiresNew(t *testing.T) {
	ExpectError(t, `WeakMap();`, "TypeError")
}

func TestWeakMapGetOrInsert(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof WeakMap.prototype.getOrInsert, "function");
		assert.sameValue(WeakMap.prototype.getOrInsert.length, 2);
		var m = new WeakMap();
		var k = {};
		assert.sameValue(m.getOrInsert(k, 1), 1);
		assert.sameValue(m.getOrInsert(k, 2), 1);
		var sym = Symbol("s");
		assert.sameValue(m.getOrInsert(sym, 9), 9);
		assert.sameValue(m.get(sym), 9);
		var called = 0;
		var k2 = {};
		assert.sameValue(m.getOrInsertComputed(k2, function(key){ called++; return 5; }), 5);
		assert.sameValue(m.getOrInsertComputed(k2, function(){ called++; return 6; }), 5);
		assert.sameValue(called, 1);
	`)
	ExpectError(t, `new WeakMap().getOrInsert(1, 1);`, "TypeError")
}

func TestWeakMapConstructorInvokesAdder(t *testing.T) {
	Expect(t, `
		var count = 0;
		var orig = WeakMap.prototype.set;
		WeakMap.prototype.set = function(k, v){ count++; return orig.call(this, k, v); };
		var a = {}, b = {};
		var m = new WeakMap([[a, 1], [b, 2]]);
		assert.sameValue(count, 2);
		assert.sameValue(m.get(a), 1);
		WeakMap.prototype.set = orig;
	`)
	// Non-callable adder throws.
	ExpectError(t, `
		WeakMap.prototype.set = 42;
		try { new WeakMap([]); } finally { delete WeakMap.prototype.set; }
	`, "TypeError")
}

func TestWeakSetSymbolValues(t *testing.T) {
	Expect(t, `
		var s = new WeakSet();
		var sym = Symbol("v");
		assert.sameValue(s.has(sym), false);
		assert.sameValue(s.add(sym), s);
		assert.sameValue(s.has(sym), true);
		assert.sameValue(s.delete(sym), true);
		assert.sameValue(s.has(sym), false);
	`)
	ExpectError(t, `new WeakSet().add(Symbol.for("x"));`, "TypeError")
}

func TestWeakSetToStringTagAndNew(t *testing.T) {
	Expect(t, `
		assert.sameValue(WeakSet.prototype[Symbol.toStringTag], "WeakSet");
		assert.sameValue(Object.prototype.toString.call(new WeakSet()), "[object WeakSet]");
	`)
	ExpectError(t, `WeakSet();`, "TypeError")
	ExpectError(t, `new WeakSet().add(1);`, "TypeError")
}

func TestWeakRef(t *testing.T) {
	Expect(t, `
		var o = {};
		var r = new WeakRef(o);
		assert.sameValue(r.deref(), o);
		assert.sameValue(WeakRef.prototype[Symbol.toStringTag], "WeakRef");
		assert.sameValue(Object.prototype.toString.call(r), "[object WeakRef]");
		var sym = Symbol("s");
		assert.sameValue(new WeakRef(sym).deref(), sym);
		assert.sameValue(WeakRef.prototype.deref.length, 0);
		assert.sameValue(WeakRef.length, 1);
	`)
	ExpectError(t, `new WeakRef(1);`, "TypeError")
	ExpectError(t, `new WeakRef(Symbol.for("x"));`, "TypeError")
	ExpectError(t, `WeakRef({});`, "TypeError")
}

func TestFinalizationRegistry(t *testing.T) {
	Expect(t, `
		var fr = new FinalizationRegistry(function(){});
		assert.sameValue(FinalizationRegistry.prototype[Symbol.toStringTag], "FinalizationRegistry");
		assert.sameValue(Object.prototype.toString.call(fr), "[object FinalizationRegistry]");
		var o = {}, token = {};
		fr.register(o, "held", token);
		assert.sameValue(fr.unregister(token), true);
		assert.sameValue(fr.unregister(token), false);
		assert.sameValue(FinalizationRegistry.length, 1);
		assert.sameValue(FinalizationRegistry.prototype.register.length, 2);
	`)
	ExpectError(t, `new FinalizationRegistry(42);`, "TypeError")
	ExpectError(t, `FinalizationRegistry(function(){});`, "TypeError")
	// target must be CanBeHeldWeakly
	ExpectError(t, `new FinalizationRegistry(function(){}).register(1, "held");`, "TypeError")
	// target === heldValue is a TypeError
	ExpectError(t, `var o = {}; new FinalizationRegistry(function(){}).register(o, o);`, "TypeError")
	// invalid unregister token
	ExpectError(t, `new FinalizationRegistry(function(){}).unregister(1);`, "TypeError")
}
