package harness

import "testing"

// TestJSONStringifyWrappersAndReplacer covers primitive-wrapper unwrapping,
// array-replacer ordering/allow-list, and BigInt rejection.
func TestJSONStringifyWrappersAndReplacer(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify(new Number(8.5)), "8.5", "Number wrapper");
		assert.sameValue(JSON.stringify(new String("str")), '"str"', "String wrapper");
		assert.sameValue(JSON.stringify(new Boolean(true)), "true", "Boolean wrapper");
		assert.sameValue(JSON.stringify({a: new Number(1)}), '{"a":1}', "nested wrapper");

		// Array replacer drives key order and acts as an allow-list.
		var o = {a: 2, b: 1, c: 3};
		assert.sameValue(JSON.stringify(o, ["c", "b", "a"]), '{"c":3,"b":1,"a":2}', "order");
		assert.sameValue(JSON.stringify(o, []), "{}", "empty allow-list");

		// space as a wrapper object is unwrapped.
		assert.sameValue(JSON.stringify({a:1}, null, new Number(2)), '{\n  "a": 1\n}', "space wrapper");
	`)
	ExpectError(t, `JSON.stringify(1n)`, "TypeError")
	ExpectError(t, `JSON.stringify({a: 1n})`, "TypeError")
}

// TestJSONRawJSON covers JSON.rawJSON and JSON.isRawJSON.
func TestJSONRawJSON(t *testing.T) {
	Expect(t, `
		var raw = JSON.rawJSON("1.5");
		assert.sameValue(JSON.isRawJSON(raw), true, "isRawJSON true");
		assert.sameValue(JSON.isRawJSON({}), false, "isRawJSON false");
		assert.sameValue(Object.isFrozen(raw), true, "frozen");
		assert.sameValue(raw.rawJSON, "1.5", "rawJSON prop");

		// rawJSON is emitted verbatim, bypassing Number precision limits.
		assert.sameValue(JSON.stringify({x: JSON.rawJSON("12345678901234567890")}),
			'{"x":12345678901234567890}', "raw big int text");
		assert.sameValue(JSON.stringify([JSON.rawJSON("true"), JSON.rawJSON('"a"')]),
			'[true,"a"]', "raw array");
	`)
	ExpectError(t, `JSON.rawJSON("")`, "SyntaxError")
	ExpectError(t, `JSON.rawJSON("{}")`, "SyntaxError")  // objects not allowed
	ExpectError(t, `JSON.rawJSON(" 1 ")`, "SyntaxError") // surrounding whitespace
	ExpectError(t, `JSON.rawJSON("tru")`, "SyntaxError") // not valid JSON
}

// TestJSONParseWithSource covers the reviver context/source third argument.
func TestJSONParseWithSource(t *testing.T) {
	Expect(t, `
		var sources = [];
		JSON.parse('{"a": 1, "b": [2, 3]}', function(k, v, ctx) {
			if (typeof v !== "object") sources.push(k + "=" + ctx.source);
			return v;
		});
		assert.sameValue(sources.join(","), "a=1,0=2,1=3", "primitive sources");

		// Non-primitive values get a context with no source.
		var hadSource = true;
		JSON.parse('[1]', function(k, v, ctx) {
			if (k === "") hadSource = "source" in ctx;
			return v;
		});
		assert.sameValue(hadSource, false, "root object has no source");

		// The reviver always receives a context object (3rd arg).
		var ctxType;
		JSON.parse('5', function(k, v, ctx) { ctxType = typeof ctx; return v; });
		assert.sameValue(ctxType, "object", "context is an object");
	`)
}

// TestJSONParseControlChars covers rejection of unescaped control characters and
// Object.hasOwn.
func TestJSONParseControlChars(t *testing.T) {
	ExpectError(t, `JSON.parse('""')`, "SyntaxError")
	Expect(t, `
		assert.sameValue(JSON.parse('"\\u0001"').length, 1, "escaped control ok");
		assert.sameValue(Object.hasOwn({a:1}, "a"), true, "hasOwn true");
		assert.sameValue(Object.hasOwn({a:1}, "b"), false, "hasOwn false");
		var d = Object.getOwnPropertyDescriptor(JSON, Symbol.toStringTag);
		assert.sameValue(d.value, "JSON", "toStringTag");
	`)
}
