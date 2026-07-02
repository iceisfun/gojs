package harness

import "testing"

// TestReflectApply covers Reflect.apply delegating [[Call]].
func TestReflectApply(t *testing.T) {
	Expect(t, `
		function f(a, b) { return this.tag + ":" + a + b; }
		var r = Reflect.apply(f, { tag: "T" }, [1, 2]);
		assert.sameValue(r, "T:12", "apply forwards this and args");
		assert.sameValue(Reflect.apply(Math.max, undefined, [4, 9, 2]), 9, "apply spreads args");
	`)
}

// TestReflectConstruct covers construction and a distinct new.target.
func TestReflectConstruct(t *testing.T) {
	Expect(t, `
		function C(a) { this.a = a; }
		var o = Reflect.construct(C, [7]);
		assert.sameValue(o.a, 7, "construct runs [[Construct]]");
		assert.sameValue(o instanceof C, true, "construct default proto");

		function D() {}
		D.prototype.kind = "D";
		var o2 = Reflect.construct(C, [1], D);
		assert.sameValue(Object.getPrototypeOf(o2), D.prototype, "newTarget supplies prototype");

		var seen;
		function E() { seen = new.target; }
		Reflect.construct(E, [], D);
		assert.sameValue(seen, D, "new.target is the third argument");
	`)
}

// TestReflectConstructThrows covers non-constructor arguments.
func TestReflectConstructThrows(t *testing.T) {
	ExpectError(t, `Reflect.construct(function () {}.bind(null), []); Reflect.construct(Math.max, [])`, "TypeError")
	ExpectError(t, `Reflect.construct(function () {}, [], Math.max)`, "TypeError")
	ExpectError(t, `Reflect.apply(42, null, [])`, "TypeError")
	ExpectError(t, `Reflect.construct(function C() {}, 5)`, "TypeError")
}

// TestReflectDefineDeleteHas covers the boolean-returning traps.
func TestReflectDefineDeleteHas(t *testing.T) {
	Expect(t, `
		var o = {};
		assert.sameValue(Reflect.defineProperty(o, "x", { value: 1, configurable: true }), true, "define ok");
		assert.sameValue(o.x, 1, "defined value");
		Object.freeze(o);
		assert.sameValue(Reflect.defineProperty(o, "y", { value: 2 }), false, "define on frozen returns false");
		assert.sameValue(Reflect.has(o, "x"), true, "has own");
		assert.sameValue(Reflect.has(o, "toString"), true, "has inherited");
		assert.sameValue(Reflect.has(o, "nope"), false, "has absent");

		var d = { a: 1 };
		assert.sameValue(Reflect.deleteProperty(d, "a"), true, "delete ok");
		assert.sameValue("a" in d, false, "deleted");
	`)
}

// TestReflectGetSet covers get/set with receiver plumbing.
func TestReflectGetSet(t *testing.T) {
	Expect(t, `
		var o = { get x() { return this.v; }, v: 5 };
		assert.sameValue(Reflect.get(o, "x"), 5, "get accessor");
		assert.sameValue(Reflect.get(o, "x", { v: 99 }), 99, "get with receiver");
		assert.sameValue(Reflect.set(o, "v", 8), true, "set data");
		assert.sameValue(o.v, 8, "set applied");

		var target = {};
		Object.defineProperty(target, "ro", { value: 1, writable: false });
		assert.sameValue(Reflect.set(target, "ro", 2), false, "set non-writable returns false");
	`)
}

// TestReflectOwnKeysAndProto covers ownKeys ordering and prototype ops.
func TestReflectOwnKeysAndProto(t *testing.T) {
	Expect(t, `
		var s = Symbol("s");
		var o = {};
		o[2] = "b"; o[0] = "a"; o.foo = 1; o[s] = 2; o.bar = 3;
		var keys = Reflect.ownKeys(o);
		assert.sameValue(keys.length, 5, "all keys");
		assert.sameValue(keys[0], "0", "index 0 first");
		assert.sameValue(keys[1], "2", "index 2 second");
		assert.sameValue(keys[2], "foo", "string keys next");
		assert.sameValue(keys[3], "bar", "string keys insertion order");
		assert.sameValue(keys[4], s, "symbols last");

		var proto = { p: 1 };
		var c = {};
		assert.sameValue(Reflect.setPrototypeOf(c, proto), true, "setProto ok");
		assert.sameValue(Reflect.getPrototypeOf(c), proto, "getProto");
		assert.sameValue(Reflect.isExtensible(c), true, "extensible");
		assert.sameValue(Reflect.preventExtensions(c), true, "preventExtensions");
		assert.sameValue(Reflect.isExtensible(c), false, "no longer extensible");
	`)
}

// TestReflectGetOwnPropertyDescriptor covers descriptor retrieval.
func TestReflectGetOwnPropertyDescriptor(t *testing.T) {
	Expect(t, `
		var o = { x: 1 };
		var d = Reflect.getOwnPropertyDescriptor(o, "x");
		assert.sameValue(d.value, 1, "descriptor value");
		assert.sameValue(d.writable, true, "descriptor writable");
		assert.sameValue(Reflect.getOwnPropertyDescriptor(o, "nope"), undefined, "absent is undefined");
	`)
}

// TestReflectShape covers the Reflect object's own shape.
func TestReflectShape(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof Reflect, "object", "Reflect is a namespace object");
		assert.sameValue(Object.prototype.toString.call(Reflect), "[object Reflect]", "toStringTag");
		assert.sameValue(Reflect.get.length, 2, "get.length");
		assert.sameValue(Reflect.get.name, "get", "get.name");
	`)
}
