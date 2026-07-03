package harness

import "testing"

// TestPromiseStringIterableAll verifies that Promise combinators can iterate a
// string argument (each code point is a value). Regression for GetMethod not
// boxing primitive strings, which made strings appear non-iterable.
func TestPromiseStringIterableAll(t *testing.T) {
	Expect(t, asyncCase(`
		var res = await Promise.all("abc");
		assert.sameValue(res.join(","), "a,b,c", "Promise.all over a string");
	`))
}

func TestPromiseStringIterableRace(t *testing.T) {
	Expect(t, asyncCase(`
		var res = await Promise.race("xyz");
		assert.sameValue(res, "x", "Promise.race over a string resolves to first char");
	`))
}

func TestPromiseStringIterableAllSettled(t *testing.T) {
	Expect(t, asyncCase(`
		var res = await Promise.allSettled("ab");
		var out = res.map(function(r){ return r.status + ":" + r.value; });
		assert.sameValue(out.join(","), "fulfilled:a,fulfilled:b", "Promise.allSettled over a string");
	`))
}

// TestPromiseAnyEmptyStringRejectsAggregate verifies Promise.any over an empty
// (but iterable) string rejects with AggregateError, not TypeError.
func TestPromiseAnyEmptyStringRejectsAggregate(t *testing.T) {
	Expect(t, asyncCase(`
		var caught = null;
		try {
			await Promise.any("");
		} catch (e) {
			caught = e;
		}
		assert.sameValue(caught instanceof AggregateError, true, "empty string -> AggregateError");
	`))
}

// TestPromiseResolvingFunctionsAnonymous verifies the resolving functions passed
// to a thenable's then method (from CreateResolvingFunctions) are anonymous with
// length 1, per the spec.
func TestPromiseResolvingFunctionsAnonymous(t *testing.T) {
	Expect(t, asyncCase(`
		var names = [];
		var lengths = [];
		var thenable = {
			then: function(resolve, reject) {
				names.push(resolve.name);
				names.push(reject.name);
				lengths.push(resolve.length);
				lengths.push(reject.length);
				resolve(7);
			}
		};
		var v = await Promise.resolve(thenable);
		assert.sameValue(v, 7, "adopted thenable value");
		assert.sameValue(names.join(","), ",", "resolving function names are empty");
		assert.sameValue(lengths.join(","), "1,1", "resolving function lengths are 1");
	`))
}

// TestPromiseConstructorHonorsNewTargetPrototype verifies the Promise
// constructor reads new.target.prototype (OrdinaryCreateFromConstructor) and
// propagates an abrupt getter throw.
func TestPromiseConstructorHonorsNewTargetPrototype(t *testing.T) {
	Expect(t, `
		var custom = {};
		var bound = (function(){}).bind();
		Object.defineProperty(bound, "prototype", { value: custom });
		var p = Reflect.construct(Promise, [function(){}], bound);
		assert.sameValue(Object.getPrototypeOf(p), custom, "prototype from new.target");

		var threw = false;
		var boom = (function(){}).bind();
		Object.defineProperty(boom, "prototype", { get: function(){ throw new Test262Error(); } });
		try {
			Reflect.construct(Promise, [function(){}], boom);
		} catch (e) {
			threw = e instanceof Test262Error;
		}
		assert.sameValue(threw, true, "abrupt prototype getter propagates");
	`)
}
