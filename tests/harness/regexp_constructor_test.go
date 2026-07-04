package harness

import "testing"

// The RegExp constructor's §22.2.4.1 handling of RegExp and "regexp-like"
// arguments, extracted from Test262 built-ins/RegExp/{S15.10.3.1_A1_*,
// from-regexp-like*,call_with_non_regexp_same_constructor}.
func TestRegExpConstructorFromRegExpLike(t *testing.T) {
	Expect(t, `
		// RegExp(re) called (no 'new'), no flags, same constructor -> returns re itself.
		var re = /x/i;
		var same = RegExp(re);
		re.indicator = 1;
		assert.sameValue(same, re, "RegExp(re) returns the same object");
		assert.sameValue(same.indicator, 1, "same object, so indicator is visible");

		// new RegExp(re) DOES create a distinct object (NewTarget defined).
		var copy = new RegExp(re);
		assert.sameValue(copy === re, false, "new RegExp(re) is a fresh object");
		assert.sameValue(copy.source, "x");
		assert.sameValue(copy.flags, "i");

		// A regexp-like object (Symbol.match truthy) is read via source/flags.
		var obj = { source: "source text", flags: "i" };
		obj[Symbol.match] = true;
		var result = new RegExp(obj);
		assert.sameValue(Object.getPrototypeOf(result), RegExp.prototype);
		assert.sameValue(result.source, "source text");
		assert.sameValue(result.flags, "i");

		// Same-constructor short-circuit also applies to a regexp-like object.
		obj.constructor = RegExp;
		assert.sameValue(RegExp(obj), obj, "regexp-like with constructor===RegExp returns itself");

		// Explicit flags override the source object's flags.
		var over = new RegExp(obj, "g");
		assert.sameValue(over.flags, "g");
	`)
}

func TestRegExpConstructorGetterErrors(t *testing.T) {
	// A throwing constructor/source/flags getter on a regexp-like object must
	// propagate out of the RegExp constructor.
	ExpectError(t, `
		var obj = {};
		obj[Symbol.match] = true;
		Object.defineProperty(obj, 'source', { get: function(){ throw new TypeError('src'); } });
		RegExp(obj);
	`, "TypeError")
	ExpectError(t, `
		var obj = { source: "x" };
		obj[Symbol.match] = true;
		Object.defineProperty(obj, 'flags', { get: function(){ throw new TypeError('flg'); } });
		new RegExp(obj);
	`, "TypeError")
	ExpectError(t, `
		var obj = {};
		obj[Symbol.match] = true;
		Object.defineProperty(obj, 'constructor', { get: function(){ throw new TypeError('ctor'); } });
		RegExp(obj);
	`, "TypeError")
}
