package harness

import "testing"

// asyncCase wraps an async body so that any assertion failure inside it (which
// rejects the async function's promise) is re-thrown from a rejection handler,
// propagating out of RunString as an uncaught error that fails the Go test.
func asyncCase(body string) string {
	return "(async function(){\n" + body + "\n})().then(function(){}, function(e){ throw e; });"
}

func TestAsyncReturnsPromise(t *testing.T) {
	Expect(t, asyncCase(`
		async function f() { return 42; }
		var p = f();
		assert.sameValue(typeof p.then, "function", "async returns a thenable");
		assert.sameValue(await p, 42);
	`))
}

func TestAsyncAwaitValues(t *testing.T) {
	Expect(t, asyncCase(`
		assert.sameValue(await 5, 5);                       // await a non-promise
		assert.sameValue(await Promise.resolve(10), 10);    // await a promise
		var a = await Promise.resolve(2);
		var b = await Promise.resolve(3);
		assert.sameValue(a + b, 5);
	`))
}

func TestAsyncTryCatch(t *testing.T) {
	Expect(t, asyncCase(`
		var caught = null;
		try {
			await Promise.reject(new TypeError("boom"));
		} catch (e) {
			caught = e;
		}
		assert.sameValue(caught instanceof TypeError, true);
		assert.sameValue(caught.message, "boom");
	`))
}

func TestAsyncLoopSequencing(t *testing.T) {
	Expect(t, asyncCase(`
		var out = [];
		for (var i = 0; i < 4; i++) {
			out.push(await Promise.resolve(i * i));
		}
		assert.sameValue(out.join(","), "0,1,4,9");
	`))
}

func TestAsyncOrdering(t *testing.T) {
	// Synchronous code runs before any await continuation resumes.
	out := Expect(t, `
		var log = [];
		async function f() { log.push("a"); await 0; log.push("c"); console.log(log.join(",")); }
		f();
		log.push("b");
	`)
	if len(out) != 1 || out[0] != "a,b,c" {
		t.Errorf("async ordering = %v, want [a,b,c]", out)
	}
}

func TestAsyncAwaitThenable(t *testing.T) {
	Expect(t, asyncCase(`
		var thenable = { then: function(resolve) { resolve(99); } };
		assert.sameValue(await thenable, 99);
	`))
}
