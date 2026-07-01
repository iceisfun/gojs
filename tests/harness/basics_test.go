package harness

import "testing"

// These tests exercise language fundamentals via in-script assertions. Each
// case throws (failing the Go test) if any assertion does not hold. When a case
// surfaces a real engine bug, it stays here as a regression guard.

func TestArithmeticAndCoercion(t *testing.T) {
	Expect(t, `
		assert.sameValue(1 + 2, 3);
		assert.sameValue(0.1 + 0.2, 0.30000000000000004);
		assert.sameValue(10 % 3, 1);
		assert.sameValue(2 ** 10, 1024);
		assert.sameValue("5" - 2, 3);
		assert.sameValue("5" + 2, "52");
		assert.sameValue(1 + null, 1);
		assert.sameValue(1 + undefined, NaN);
		assert.sameValue(true + true, 2);
		assert.sameValue([] + [], "");
		assert.sameValue([1,2] + [3], "1,23");
		assert.sameValue(+"  42  ", 42);
		assert.sameValue(-"3", -3);
		assert.sameValue(5 / 0, Infinity);
		assert.sameValue(-5 / 0, -Infinity);
		assert.sameValue(0 / 0, NaN);
	`)
}

func TestEquality(t *testing.T) {
	Expect(t, `
		assert.sameValue(1 == "1", true);
		assert.sameValue(1 === "1", false);
		assert.sameValue(null == undefined, true);
		assert.sameValue(null === undefined, false);
		assert.sameValue(NaN === NaN, false);
		assert.sameValue(0 === -0, true);
		assert.sameValue("" == 0, true);
		assert.sameValue([] == false, true);
		assert.sameValue(null == 0, false);
		var o = {}; assert.sameValue(o == o, true);
		assert.sameValue({} == {}, false);
	`)
}

func TestTypeof(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof undefined, "undefined");
		assert.sameValue(typeof null, "object");
		assert.sameValue(typeof 1, "number");
		assert.sameValue(typeof "s", "string");
		assert.sameValue(typeof true, "boolean");
		assert.sameValue(typeof {}, "object");
		assert.sameValue(typeof [], "object");
		assert.sameValue(typeof function(){}, "function");
		assert.sameValue(typeof Symbol(), "symbol");
		assert.sameValue(typeof 1n, "bigint");
		assert.sameValue(typeof notDefinedAnywhere, "undefined");
	`)
}

func TestStrings(t *testing.T) {
	Expect(t, `
		assert.sameValue("hello".length, 5);
		assert.sameValue("hello".toUpperCase(), "HELLO");
		assert.sameValue("HELLO".toLowerCase(), "hello");
		assert.sameValue("  trim  ".trim(), "trim");
		assert.sameValue("a,b,c".split(",").length, 3);
		assert.sameValue("abc".charAt(1), "b");
		assert.sameValue("abc".charCodeAt(0), 97);
		assert.sameValue("abc".indexOf("b"), 1);
		assert.sameValue("abcabc".lastIndexOf("b"), 4);
		assert.sameValue("abc".includes("b"), true);
		assert.sameValue("abc".startsWith("ab"), true);
		assert.sameValue("abc".endsWith("bc"), true);
		assert.sameValue("abc".slice(1), "bc");
		assert.sameValue("abc".slice(-1), "c");
		assert.sameValue("abc".substring(1, 2), "b");
		assert.sameValue("ab".repeat(3), "ababab");
		assert.sameValue("5".padStart(3, "0"), "005");
		assert.sameValue("5".padEnd(3, "0"), "500");
		assert.sameValue("a".concat("b", "c"), "abc");
		assert.sameValue("a-b-c".replace("-", "+"), "a+b-c");
		assert.sameValue("a-b-c".replaceAll("-", "+"), "a+b+c");
		assert.sameValue("abc"[1], "b");
	`)
}

func TestNumbers(t *testing.T) {
	Expect(t, `
		assert.sameValue((255).toString(16), "ff");
		assert.sameValue((3.14159).toFixed(2), "3.14");
		assert.sameValue(parseInt("42px"), 42);
		assert.sameValue(parseInt("0xff", 16), 255);
		assert.sameValue(parseFloat("3.14xyz"), 3.14);
		assert.sameValue(Number("  42  "), 42);
		assert.sameValue(Number(""), 0);
		assert.sameValue(Number("abc"), NaN);
		assert.sameValue(Number.isInteger(5), true);
		assert.sameValue(Number.isInteger(5.5), false);
		assert.sameValue(Number.isNaN(NaN), true);
		assert.sameValue((1234.5678).toString(), "1234.5678");
		assert.sameValue((0).toString(), "0");
		assert.sameValue((-0).toString(), "0");
		assert.sameValue((1e21).toString(), "1e+21");
	`)
}

func TestArrays(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3];
		assert.sameValue(a.length, 3);
		a.push(4); assert.sameValue(a.length, 4);
		assert.sameValue(a.pop(), 4);
		assert.sameValue(a.shift(), 1);
		a.unshift(0); assert.sameValue(a[0], 0);
		assert.sameValue([1,2,3].map(x => x*2).join(","), "2,4,6");
		assert.sameValue([1,2,3,4].filter(x => x%2===0).join(","), "2,4");
		assert.sameValue([1,2,3].reduce((a,b) => a+b, 0), 6);
		assert.sameValue([1,2,3].find(x => x>1), 2);
		assert.sameValue([1,2,3].findIndex(x => x>1), 1);
		assert.sameValue([1,2,3].some(x => x>2), true);
		assert.sameValue([1,2,3].every(x => x>0), true);
		assert.sameValue([1,2,3].includes(2), true);
		assert.sameValue([1,2,3].indexOf(2), 1);
		assert.sameValue([3,1,2].sort().join(","), "1,2,3");
		assert.sameValue([3,1,2].sort((a,b)=>b-a).join(","), "3,2,1");
		assert.sameValue([1,2,3].reverse().join(","), "3,2,1");
		assert.sameValue([1,[2,[3]]].flat(Infinity).join(","), "1,2,3");
		assert.sameValue([1,2,3].slice(1).join(","), "2,3");
		assert.sameValue(Array.isArray([]), true);
		assert.sameValue(Array.isArray({}), false);
		assert.sameValue(Array.from("abc").join(","), "a,b,c");
		assert.sameValue(Array.of(1,2,3).length, 3);
		var sp = [...[1,2], ...[3,4]]; assert.sameValue(sp.length, 4);
	`)
}

func TestObjects(t *testing.T) {
	Expect(t, `
		var o = { a: 1, b: 2 };
		assert.sameValue(Object.keys(o).join(","), "a,b");
		assert.sameValue(Object.values(o).join(","), "1,2");
		assert.sameValue(Object.entries(o).length, 2);
		assert.sameValue(o.hasOwnProperty("a"), true);
		assert.sameValue(o.hasOwnProperty("z"), false);
		var merged = Object.assign({}, o, { c: 3 });
		assert.sameValue(Object.keys(merged).length, 3);
		var spread = { ...o, c: 3 };
		assert.sameValue(spread.c, 3);
		var frozen = Object.freeze({ x: 1 });
		frozen.x = 2;
		assert.sameValue(frozen.x, 1);
		var proto = { greet() { return "hi"; } };
		var child = Object.create(proto);
		assert.sameValue(child.greet(), "hi");
		assert.sameValue(Object.getPrototypeOf(child), proto);
		var computed = { ["a" + "b"]: 5 };
		assert.sameValue(computed.ab, 5);
	`)
}

func TestClosuresAndThis(t *testing.T) {
	Expect(t, `
		function makeAdder(x) { return y => x + y; }
		assert.sameValue(makeAdder(3)(4), 7);
		var counter = (function () { var n = 0; return () => ++n; })();
		counter(); counter();
		assert.sameValue(counter(), 3);
		var obj = { v: 10, get() { return this.v; } };
		assert.sameValue(obj.get(), 10);
		var fn = obj.get;
		assert.sameValue(typeof fn(), "undefined");
		var bound = obj.get.bind({ v: 99 });
		assert.sameValue(bound(), 99);
		assert.sameValue(obj.get.call({ v: 42 }), 42);
		assert.sameValue(obj.get.apply({ v: 7 }), 7);
	`)
}

func TestPrototypesAndClasses(t *testing.T) {
	Expect(t, `
		class Animal {
			constructor(name) { this.name = name; }
			speak() { return this.name + " noises"; }
		}
		class Dog extends Animal {
			speak() { return this.name + " barks"; }
			superSpeak() { return super.speak(); }
		}
		var d = new Dog("Rex");
		assert.sameValue(d.speak(), "Rex barks");
		assert.sameValue(d.superSpeak(), "Rex noises");
		assert.sameValue(d instanceof Dog, true);
		assert.sameValue(d instanceof Animal, true);
		assert.sameValue(d.constructor.name, "Dog");
		class Counter {
			#count = 0;
			inc() { this.#count++; return this; }
			get value() { return this.#count; }
			static create() { return new Counter(); }
		}
		var c = Counter.create();
		c.inc().inc();
		assert.sameValue(c.value, 2);
	`)
}

func TestControlFlow(t *testing.T) {
	Expect(t, `
		var sum = 0;
		for (var i = 0; i < 5; i++) sum += i;
		assert.sameValue(sum, 10);
		var out = [];
		for (const x of [1, 2, 3]) out.push(x);
		assert.sameValue(out.join(","), "1,2,3");
		var keys = [];
		for (const k in { a: 1, b: 2 }) keys.push(k);
		assert.sameValue(keys.join(","), "a,b");
		var n = 0; while (n < 3) n++;
		assert.sameValue(n, 3);
		var m = 0; do { m++; } while (m < 2);
		assert.sameValue(m, 2);
		function classify(x) {
			switch (x) {
				case 1: return "one";
				case 2: return "two";
				default: return "other";
			}
		}
		assert.sameValue(classify(2), "two");
		assert.sameValue(classify(9), "other");
		outer: for (var a = 0; a < 3; a++) {
			for (var b = 0; b < 3; b++) { if (b === 1) continue outer; }
		}
		assert.sameValue(a, 3);
	`)
}

func TestErrorHandling(t *testing.T) {
	Expect(t, `
		var caught = null;
		try { throw new TypeError("boom"); } catch (e) { caught = e; }
		assert.sameValue(caught instanceof TypeError, true);
		assert.sameValue(caught instanceof Error, true);
		assert.sameValue(caught.message, "boom");
		assert.sameValue(caught.name, "TypeError");
		var order = [];
		try { order.push("try"); throw 1; } catch (e) { order.push("catch"); } finally { order.push("finally"); }
		assert.sameValue(order.join(","), "try,catch,finally");
		assert.throws(RangeError, function () { throw new RangeError("r"); });
		assert.throws(TypeError, function () { null.x; });
		assert.throws(ReferenceError, function () { doesNotExist; });
	`)
}

func TestDestructuring(t *testing.T) {
	Expect(t, `
		var [a, b, c = 3] = [1, 2];
		assert.sameValue(a + b + c, 6);
		var [x, , z] = [1, 2, 3];
		assert.sameValue(x + z, 4);
		var [head, ...tail] = [1, 2, 3, 4];
		assert.sameValue(head, 1);
		assert.sameValue(tail.length, 3);
		var { p, q = 9, r: renamed } = { p: 1, r: 5 };
		assert.sameValue(p, 1);
		assert.sameValue(q, 9);
		assert.sameValue(renamed, 5);
		var { nested: { deep } } = { nested: { deep: 42 } };
		assert.sameValue(deep, 42);
		function f({ a, b } = { a: 1, b: 2 }) { return a + b; }
		assert.sameValue(f(), 3);
		assert.sameValue(f({ a: 10, b: 20 }), 30);
	`)
}

func TestJSON(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify({ a: 1, b: [2, 3] }), '{"a":1,"b":[2,3]}');
		assert.sameValue(JSON.stringify([1, "two", true, null]), '[1,"two",true,null]');
		assert.sameValue(JSON.stringify({ a: undefined, b: function(){}, c: 1 }), '{"c":1}');
		assert.sameValue(JSON.parse('{"x":5}').x, 5);
		assert.sameValue(JSON.parse('[1,2,3]').length, 3);
		assert.sameValue(JSON.parse('"hello"'), "hello");
		assert.sameValue(JSON.parse('true'), true);
		assert.sameValue(JSON.parse('null'), null);
		var round = JSON.parse(JSON.stringify({ nested: { arr: [1, 2] } }));
		assert.sameValue(round.nested.arr[1], 2);
		assert.throws(SyntaxError, function () { JSON.parse("{bad"); });
	`)
}

func TestGeneratorsAndIterators(t *testing.T) {
	Expect(t, `
		function* gen() { yield 1; yield 2; yield 3; }
		assert.sameValue([...gen()].join(","), "1,2,3");
		var it = gen();
		assert.sameValue(it.next().value, 1);
		assert.sameValue(it.next().value, 2);
		assert.sameValue(it.next().done, false);
		it.next();
		assert.sameValue(it.next().done, true);
		function* echo() { var x = yield 1; return x; }
		var e = echo(); e.next();
		assert.sameValue(e.next(42).value, 42);
		var m = new Map([["a", 1]]);
		assert.sameValue([...m.keys()].join(","), "a");
		var s = new Set([1, 2, 2, 3]);
		assert.sameValue([...s].join(","), "1,2,3");
	`)
}

func TestMapSet(t *testing.T) {
	Expect(t, `
		var m = new Map();
		m.set("a", 1).set("b", 2);
		assert.sameValue(m.size, 2);
		assert.sameValue(m.get("a"), 1);
		assert.sameValue(m.has("b"), true);
		m.delete("a");
		assert.sameValue(m.has("a"), false);
		var objKey = {};
		m.set(objKey, "obj");
		assert.sameValue(m.get(objKey), "obj");
		var s = new Set();
		s.add(1).add(1).add(2);
		assert.sameValue(s.size, 2);
		var sum = 0;
		s.forEach(v => sum += v);
		assert.sameValue(sum, 3);
	`)
}

func TestTemplateAndSpread(t *testing.T) {
	Expect(t, `
		var name = "world", n = 3;
		assert.sameValue(`+"`hi ${name}, ${n + 1}`"+`, "hi world, 4");
		function sum(...xs) { return xs.reduce((a, b) => a + b, 0); }
		assert.sameValue(sum(1, 2, 3, 4), 10);
		assert.sameValue(sum(...[1, 2], ...[3, 4]), 10);
		function tag(strings, ...values) { return strings.join("|") + "#" + values.join(","); }
		assert.sameValue(tag`+"`a${1}b${2}c`"+`, "a|b|c#1,2");
	`)
}

func TestOptionalChainingAndNullish(t *testing.T) {
	Expect(t, `
		var o = { a: { b: { c: 42 } } };
		assert.sameValue(o?.a?.b?.c, 42);
		assert.sameValue(o?.x?.y?.z, undefined);
		assert.sameValue(o?.x?.y ?? "default", "default");
		assert.sameValue(null ?? "fallback", "fallback");
		assert.sameValue(0 ?? "fallback", 0);
		assert.sameValue("" ?? "fallback", "");
		var fn = null;
		assert.sameValue(fn?.(), undefined);
		var arr = null;
		assert.sameValue(arr?.[0], undefined);
		var x = null; x ??= 5; assert.sameValue(x, 5);
		var y = 1; y ??= 9; assert.sameValue(y, 1);
	`)
}

func TestMath(t *testing.T) {
	Expect(t, `
		assert.sameValue(Math.max(1, 5, 3), 5);
		assert.sameValue(Math.min(1, 5, 3), 1);
		assert.sameValue(Math.abs(-5), 5);
		assert.sameValue(Math.floor(3.7), 3);
		assert.sameValue(Math.ceil(3.2), 4);
		assert.sameValue(Math.round(3.5), 4);
		assert.sameValue(Math.trunc(3.9), 3);
		assert.sameValue(Math.sign(-3), -1);
		assert.sameValue(Math.sqrt(16), 4);
		assert.sameValue(Math.pow(2, 8), 256);
		assert.sameValue(Math.hypot(3, 4), 5);
		var r = Math.random();
		assert(r >= 0 && r < 1, "random in range");
	`)
}
