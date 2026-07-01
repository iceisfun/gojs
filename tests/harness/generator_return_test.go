package harness

import "testing"

// Resuming a generator that is still suspended at its start with an abrupt
// completion (return or throw) completes it immediately, without ever executing
// the body (ECMA-262 27.5.3.3/27.5.3.4: suspendedStart goes straight to
// completed). A normal resume (next) still starts the body.

func TestGeneratorReturnBeforeStart(t *testing.T) {
	Expect(t, `
		var ran = 0;
		function* g() { ran += 1; yield 1; }
		var it = g();
		var r = it.return(42);
		assert.sameValue(ran, 0, "body did not run");
		assert.sameValue(r.value, 42, "return value forwarded");
		assert.sameValue(r.done, true, "generator is done");
		var r2 = it.next();
		assert.sameValue(r2.done, true, "stays done");
		assert.sameValue(r2.value, undefined, "no further values");
	`)
}

func TestGeneratorThrowBeforeStart(t *testing.T) {
	Expect(t, `
		var ran = 0;
		function* g() { ran += 1; yield 1; }
		var it = g();
		var caught;
		try { it.throw(new TypeError("boom")); } catch (e) { caught = e; }
		assert.sameValue(ran, 0, "body did not run");
		assert.sameValue(caught instanceof TypeError, true, "thrown value propagates");
		assert.sameValue(it.next().done, true, "generator is done after throw");
	`)
}

func TestGeneratorReturnAfterStartRunsFinally(t *testing.T) {
	Expect(t, `
		var log = "";
		function* g() { try { yield 1; yield 2; } finally { log += "f"; } }
		var it = g();
		assert.sameValue(it.next().value, 1, "first yield");
		var r = it.return(9);
		assert.sameValue(log, "f", "finally ran");
		assert.sameValue(r.value, 9, "return value");
		assert.sameValue(r.done, true, "done");
	`)
}

// An empty array destructuring pattern must obtain the iterator and close it
// without advancing it; over an unstarted generator this must not run the body.
func TestEmptyPatternDoesNotAdvanceGenerator(t *testing.T) {
	Expect(t, `
		var iterations = 0;
		var iter = (function* () { iterations += 1; })();
		var callCount = 0;
		class C { method([]) { callCount += 1; } }
		new C().method(iter);
		assert.sameValue(iterations, 0, "generator body never ran");
		assert.sameValue(callCount, 1, "method ran once");
	`)
}
