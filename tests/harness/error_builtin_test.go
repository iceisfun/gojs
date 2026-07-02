package harness

import "testing"

// Error.prototype.toString follows §20.5.3.4: undefined name/message default to
// "Error"/"" and getter/ToString aborts propagate.
func TestErrorToString(t *testing.T) {
	Expect(t, `
		assert.sameValue(Error.prototype.toString.call({}), "Error");
		assert.sameValue(Error.prototype.toString.call({ message: "42" }), "Error: 42");
		assert.sameValue(Error.prototype.toString.call({ name: "24" }), "24");
		assert.sameValue(Error.prototype.toString.call({ name: "24", message: "42" }), "24: 42");
		assert.throws(TypeError, function () {
			Error.prototype.toString.call({ message: Symbol() });
		});
		function Boom() {}
		assert.throws(TypeError, function () {
			Error.prototype.toString.call({ get name() { throw new TypeError(); } });
		});
	`)
}

// Error.isError reports [[ErrorData]] presence.
func TestErrorIsError(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof Error.isError, "function");
		assert.sameValue(Error.isError.length, 1);
		assert.sameValue(Error.isError.name, "isError");
		assert.sameValue(Error.isError(new Error()), true);
		assert.sameValue(Error.isError(new TypeError()), true);
		assert.sameValue(Error.isError(new AggregateError([])), true);
		class E extends Error {}
		assert.sameValue(Error.isError(new E()), true, "subclass");
		assert.sameValue(Error.isError({}), false);
		assert.sameValue(Error.isError(Object.create(Error.prototype)), false, "fake error");
		assert.sameValue(Error.isError(42), false);
		assert.sameValue(Error.isError("x"), false);
	`)
}

// AggregateError constructor semantics (§20.5.7).
func TestAggregateErrorBasics(t *testing.T) {
	Expect(t, `
		assert.sameValue(AggregateError.length, 2);
		assert.sameValue(AggregateError.name, "AggregateError");
		assert.sameValue(Object.getPrototypeOf(AggregateError), Error);
		assert.sameValue(Object.getPrototypeOf(AggregateError.prototype), Error.prototype);
		assert.sameValue(AggregateError.prototype.name, "AggregateError");
		assert.sameValue(AggregateError.prototype.message, "");
		assert.sameValue(AggregateError.prototype.constructor, AggregateError);
		assert.sameValue(AggregateError.prototype.hasOwnProperty("errors"), false);

		var e = new AggregateError([1, 2, 3], "boom");
		assert.sameValue(e instanceof AggregateError, true);
		assert.sameValue(e instanceof Error, true);
		assert.sameValue(e.message, "boom");
		assert.sameValue(Array.isArray(e.errors), true);
		assert.sameValue(e.errors.length, 3);
		assert.sameValue(e.errors[0], 1);
		var md = Object.getOwnPropertyDescriptor(e, "errors");
		assert.sameValue(md.enumerable, false);
		assert.sameValue(md.writable, true);
		assert.sameValue(md.configurable, true);

		var e2 = new AggregateError([]);
		assert.sameValue(e2.hasOwnProperty("message"), false, "no message property when undefined");
	`)
}

// AggregateError cause option and iteration order/abrupt handling.
func TestAggregateErrorCauseAndOrder(t *testing.T) {
	Expect(t, `
		var cause = {};
		var e = new AggregateError([], "m", { cause: cause });
		assert.sameValue(e.cause, cause);
		assert.sameValue(new AggregateError([], "m").hasOwnProperty("cause"), false);
		assert.sameValue(new AggregateError([], "m", { cause: undefined }).hasOwnProperty("cause"), true);

		var seq = [];
		var message = { toString: function () { seq.push(1); return ""; } };
		var errors = { };
		errors[Symbol.iterator] = function () {
			seq.push(2);
			return { next: function () { seq.push(3); return { done: true }; } };
		};
		new AggregateError(errors, message);
		assert.sameValue(seq.join(","), "1,2,3", "message before iteration");

		assert.throws(Test262Error, function () {
			new AggregateError({ get [Symbol.iterator]() { throw new Test262Error(); } });
		});
		assert.throws(TypeError, function () { new AggregateError(); });
	`)
}
