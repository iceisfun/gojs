package harness

import "testing"

// Async generators: next/return/throw return promises, `await` suspends
// transparently, and `yield` delivers { value, done } through a request queue.

func TestAsyncGeneratorNextReturnsPromise(t *testing.T) {
	Expect(t, asyncCase(`
		async function* ag() { yield 1; }
		var g = ag();
		var p = g.next();
		assert.sameValue(typeof p.then, "function", "next() returns a promise");
		var r = await p;
		assert.sameValue(r.value, 1);
		assert.sameValue(r.done, false);
	`))
}

func TestAsyncGeneratorYieldAwaitReturn(t *testing.T) {
	Expect(t, asyncCase(`
		async function* ag() {
			yield 1;
			yield await Promise.resolve(2);   // await inside the body is transparent
			return 3;                          // return value ends iteration
		}
		var g = ag();
		assert.sameValue((await g.next()).value, 1);
		var second = await g.next();
		assert.sameValue(second.value, 2);
		assert.sameValue(second.done, false);
		var third = await g.next();
		assert.sameValue(third.value, 3);
		assert.sameValue(third.done, true, "return value is done:true");
		var after = await g.next();
		assert.sameValue(after.value, undefined);
		assert.sameValue(after.done, true, "exhausted generator stays done");
	`))
}

func TestAsyncGeneratorThrowInto(t *testing.T) {
	Expect(t, asyncCase(`
		var caught = null;
		async function* ag() {
			try { yield 1; } catch (e) { caught = e; yield 2; }
		}
		var g = ag();
		assert.sameValue((await g.next()).value, 1);
		var r = await g.throw(new TypeError("boom"));   // injected at the yield
		assert.sameValue(caught instanceof TypeError, true);
		assert.sameValue(caught.message, "boom");
		assert.sameValue(r.value, 2);
	`))
}

func TestAsyncGeneratorReturnMethod(t *testing.T) {
	Expect(t, asyncCase(`
		async function* ag() { yield 1; yield 2; }
		var g = ag();
		assert.sameValue((await g.next()).value, 1);
		var r = await g.return(42);      // early return
		assert.sameValue(r.value, 42);
		assert.sameValue(r.done, true);
		assert.sameValue((await g.next()).done, true);
	`))
}

func TestAsyncGeneratorIsAsyncIterable(t *testing.T) {
	Expect(t, asyncCase(`
		async function* ag() { yield 1; }
		var g = ag();
		assert.sameValue(typeof g[Symbol.asyncIterator], "function");
		assert.sameValue(g[Symbol.asyncIterator](), g, "@@asyncIterator returns this");
	`))
}

func TestAsyncGeneratorPrototypeShape(t *testing.T) {
	// next/return/throw live on the shared %AsyncGeneratorPrototype%, not the
	// instance, and the prototype carries the AsyncGenerator toStringTag. An
	// instance's [[Prototype]] is its function's own .prototype (per
	// OrdinaryCreateFromConstructor), which in turn inherits from
	// %AsyncGeneratorPrototype% — so the methods are two levels up.
	Expect(t, `
		async function* ag() {}
		var proto = Object.getPrototypeOf(Object.getPrototypeOf(ag()));
		assert.sameValue(proto.hasOwnProperty("next"), true);
		assert.sameValue(proto.hasOwnProperty("return"), true);
		assert.sameValue(proto.hasOwnProperty("throw"), true);
		assert.sameValue(ag().hasOwnProperty("next"), false, "methods are on the prototype");
		assert.sameValue(proto[Symbol.toStringTag], "AsyncGenerator");
		assert.sameValue(Object.getPrototypeOf(ag.prototype), proto, "func .prototype inherits from %AsyncGeneratorPrototype%");
	`)
}

func TestForAwaitOfAsyncGenerator(t *testing.T) {
	Expect(t, asyncCase(`
		async function* ag() { yield 1; yield 2; yield 3; }
		var out = [];
		for await (var x of ag()) { out.push(x); }
		assert.sameValue(out.join(","), "1,2,3");
	`))
}

func TestForAwaitOfSyncIterableOfPromises(t *testing.T) {
	// A sync iterable is wrapped like AsyncFromSyncIterator: each value is awaited.
	Expect(t, asyncCase(`
		var out = [];
		for await (var v of [Promise.resolve("a"), Promise.resolve("b"), "c"]) { out.push(v); }
		assert.sameValue(out.join(","), "a,b,c");
	`))
}

func TestForAwaitOfBreakClosesIterator(t *testing.T) {
	Expect(t, asyncCase(`
		var closed = false;
		var iter = {
			[Symbol.asyncIterator]() { return this; },
			i: 0,
			next() { return Promise.resolve({ value: this.i++, done: false }); },
			return(v) { closed = true; return Promise.resolve({ value: v, done: true }); }
		};
		var seen = [];
		for await (var x of iter) { seen.push(x); if (x === 2) break; }
		assert.sameValue(seen.join(","), "0,1,2");
		assert.sameValue(closed, true, "break awaits the iterator's return()");
	`))
}

func TestAsyncGeneratorBrandCheck(t *testing.T) {
	// Called on a non-async-generator receiver, next rejects (never throws).
	Expect(t, asyncCase(`
		async function* ag() {}
		var next = Object.getPrototypeOf(ag()).next;
		var threw = false;
		try { await next.call({}); } catch (e) { threw = e instanceof TypeError; }
		assert.sameValue(threw, true, "next on a bad receiver rejects with TypeError");
	`))
}
