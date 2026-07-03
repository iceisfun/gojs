package harness

import "testing"

// Await must cost exactly one microtask tick: Await(v) does
// PromiseResolve(%Promise%, v) — which returns a *native* promise unchanged
// rather than re-wrapping it — followed by PerformPromiseThen. Re-wrapping a
// native promise would interpose an extra thenable-adoption tick and desync the
// interleaving of async/await with plain promise chains.

func TestAwaitInterleavesOneTick(t *testing.T) {
	Expect(t, asyncCase(`
		var actual = [];
		async function pushAwait(v) { actual.push('Await: ' + v); }
		async function callAsync() {
			await pushAwait(1);
			await pushAwait(2);
		}
		callAsync();
		await new Promise(function (resolve) {
			actual.push('Promise: 1');
			resolve();
		}).then(function () {
			actual.push('Promise: 2');
		});
		assert.compareArray(actual,
			['Await: 1', 'Promise: 1', 'Await: 2', 'Promise: 2'],
			'async/await and promises interleave one tick at a time');
	`))
}

// Awaiting a native promise must not observe a monkey-patched `then`: the
// internal PerformPromiseThen is used, not the value's `then` method.
func TestAwaitNativePromiseIgnoresPatchedThen(t *testing.T) {
	Expect(t, asyncCase(`
		var thenCalls = 0;
		var patched = Promise.resolve(42);
		patched.then = function () { thenCalls++; return Promise.prototype.then.apply(this, arguments); };
		var actual = [];
		async function trigger() { actual.push('Await: ' + await patched); }
		trigger();
		await new Promise(function (resolve) {
			actual.push('Promise: 1');
			resolve();
		}).then(function () {
			actual.push('Promise: 2');
		});
		assert.compareArray(actual, ['Promise: 1', 'Await: 42', 'Promise: 2'],
			'await on a native promise interleaves in one tick');
		assert.sameValue(thenCalls, 0, 'monkey-patched then on a native promise is not called');
	`))
}
