package harness

import "testing"

// TestJSONStringifyPrimitives checks that JSON.stringify correctly serializes
// primitive values and that non-serializable top-level values (undefined,
// function, symbol) produce the JS undefined value, not a string.
func TestJSONStringifyPrimitives(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify(1), '1');
		assert.sameValue(JSON.stringify(-5), '-5');
		assert.sameValue(JSON.stringify(3.14), '3.14');
		assert.sameValue(JSON.stringify(0), '0');
		assert.sameValue(JSON.stringify("a"), '"a"');
		assert.sameValue(JSON.stringify("hello"), '"hello"');
		assert.sameValue(JSON.stringify(""), '""');
		assert.sameValue(JSON.stringify(true), 'true');
		assert.sameValue(JSON.stringify(false), 'false');
		assert.sameValue(JSON.stringify(null), 'null');
		assert.sameValue(JSON.stringify(undefined), undefined);
		assert.sameValue(JSON.stringify(function(){}), undefined);
		assert.sameValue(JSON.stringify(Symbol('s')), undefined);
	`)
}

// TestJSONStringifySpecialNumbers checks that NaN and the Infinity values
// serialize as the JSON literal "null" (they are not representable in JSON).
func TestJSONStringifySpecialNumbers(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify(NaN), 'null');
		assert.sameValue(JSON.stringify(Infinity), 'null');
		assert.sameValue(JSON.stringify(-Infinity), 'null');
		assert.sameValue(JSON.stringify(1/0), 'null');
		assert.sameValue(JSON.stringify(-1/0), 'null');
		assert.sameValue(JSON.stringify(0/0), 'null');
	`)
}

// TestJSONStringifyStringEscaping checks that special characters inside strings
// are escaped correctly per the JSON spec: the six named escapes (\", \\, \n,
// \r, \t, \b, \f) and \u00XX for remaining control characters below U+0020.
func TestJSONStringifyStringEscaping(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify('"'), '"\\""');
		assert.sameValue(JSON.stringify('\\'), '"\\\\"');
		assert.sameValue(JSON.stringify('\n'), '"\\n"');
		assert.sameValue(JSON.stringify('\r'), '"\\r"');
		assert.sameValue(JSON.stringify('\t'), '"\\t"');
		assert.sameValue(JSON.stringify('\b'), '"\\b"');
		assert.sameValue(JSON.stringify('\f'), '"\\f"');
		assert.sameValue(JSON.stringify('\x01'), '"\\u0001"');
		assert.sameValue(JSON.stringify('\x1f'), '"\\u001f"');
		assert.sameValue(JSON.stringify('\x00'), '"\\u0000"');
		assert.sameValue(JSON.stringify('a"b\\c'), '"a\\"b\\\\c"');
		assert.sameValue(JSON.stringify('line1\nline2'), '"line1\\nline2"');
	`)
}

// TestJSONStringifyObjects checks serialization of plain objects: insertion-
// order key ordering for string keys, ascending ordering for integer-index
// keys (which come before string keys), and omission of non-serializable values
// (undefined, function, symbol) from the output.
func TestJSONStringifyObjects(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify({a: 1, b: 2}), '{"a":1,"b":2}');
		assert.sameValue(JSON.stringify({b: 2, a: 1}), '{"b":2,"a":1}');
		assert.sameValue(JSON.stringify({a: {b: {c: 3}}}), '{"a":{"b":{"c":3}}}');
		assert.sameValue(JSON.stringify({x: null, y: true, z: 'hi'}), '{"x":null,"y":true,"z":"hi"}');
		assert.sameValue(JSON.stringify({a: [1, 2], b: {c: 3}}), '{"a":[1,2],"b":{"c":3}}');

		var o = {};
		o['b'] = 'bee';
		o['1'] = 'one';
		o['0'] = 'zero';
		assert.sameValue(JSON.stringify(o), '{"0":"zero","1":"one","b":"bee"}');

		assert.sameValue(JSON.stringify({a: undefined, b: function(){}, c: 1}), '{"c":1}');
		assert.sameValue(JSON.stringify({a: undefined, b: undefined}), '{}');
		assert.sameValue(JSON.stringify({a: Symbol('x'), b: 2}), '{"b":2}');
	`)
}

// TestJSONStringifyArrays checks serialization of arrays: nested arrays, mixed
// element types, and the rule that undefined/function/symbol elements serialize
// as the JSON literal "null".
func TestJSONStringifyArrays(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify([1, 2, 3]), '[1,2,3]');
		assert.sameValue(JSON.stringify([1, 'two', true, null]), '[1,"two",true,null]');
		assert.sameValue(JSON.stringify([[1, 2], [3, 4]]), '[[1,2],[3,4]]');
		assert.sameValue(JSON.stringify([{a: 1}, {b: 2}]), '[{"a":1},{"b":2}]');
		assert.sameValue(JSON.stringify([undefined, function(){}, Symbol('s'), 1]), '[null,null,null,1]');
		assert.sameValue(JSON.stringify([undefined]), '[null]');
		assert.sameValue(JSON.stringify([NaN, Infinity, -Infinity]), '[null,null,null]');
	`)
}

// TestJSONStringifyEmptyContainers checks that empty objects and empty arrays
// produce their compact representations, including when nested.
func TestJSONStringifyEmptyContainers(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify({}), '{}');
		assert.sameValue(JSON.stringify([]), '[]');
		assert.sameValue(JSON.stringify({a: {}, b: []}), '{"a":{},"b":[]}');
		assert.sameValue(JSON.stringify([{}, []]), '[{},[]]');
	`)
}

// TestJSONStringifyToJSON checks that when an object has a toJSON method, that
// method is called and its return value is serialized in place of the object.
// Per spec, toJSON receives the current key as its first argument; the key at
// the top level is the empty string.
func TestJSONStringifyToJSON(t *testing.T) {
	Expect(t, `
		var o = { x: 1, toJSON: function() { return 'custom'; } };
		assert.sameValue(JSON.stringify(o), '"custom"');

		var o2 = { val: 42, toJSON: function() { return { serialized: this.val }; } };
		assert.sameValue(JSON.stringify(o2), '{"serialized":42}');

		var o3 = { toJSON: function() { return undefined; } };
		assert.sameValue(JSON.stringify(o3), undefined);

		var o4 = { items: [1, 2, 3], toJSON: function() { return this.items; } };
		assert.sameValue(JSON.stringify(o4), '[1,2,3]');

		var keyCaptures = [];
		var o5 = {
			a: 1,
			toJSON: function(key) { keyCaptures.push(key); return 99; }
		};
		JSON.stringify(o5);
		assert.sameValue(keyCaptures[0], '');
	`)
}

// TestJSONStringifySpaceIndent checks the third argument (space) for numeric
// indentation and string indentation, including the spec max-10 cap for both
// forms and boundary values of zero and negative numbers.
func TestJSONStringifySpaceIndent(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.stringify({a: 1}, null, 2), '{\n  "a": 1\n}');
		assert.sameValue(JSON.stringify([1, 2, 3], null, 2), '[\n  1,\n  2,\n  3\n]');
		assert.sameValue(JSON.stringify({a: 1, b: 2}, null, 4), '{\n    "a": 1,\n    "b": 2\n}');
		assert.sameValue(JSON.stringify({a: 1}, null, '\t'), '{\n\t"a": 1\n}');
		assert.sameValue(JSON.stringify({a: 1}, null, '--'), '{\n--"a": 1\n}');
		assert.sameValue(JSON.stringify({a: 1}, null, 0), '{"a":1}');
		assert.sameValue(JSON.stringify({a: 1}, null, -1), '{"a":1}');
		assert.sameValue(JSON.stringify({a: 1}, null, 11), '{\n          "a": 1\n}');
		assert.sameValue(JSON.stringify({a: 1}, null, '12345678901'), '{\n1234567890"a": 1\n}');
		assert.sameValue(JSON.stringify({}, null, 2), '{}');
		assert.sameValue(JSON.stringify([], null, 2), '[]');
	`)
}

// TestJSONStringifyCircularReference checks that attempting to serialize an
// object that participates in a reference cycle throws a TypeError.
func TestJSONStringifyCircularReference(t *testing.T) {
	Expect(t, `
		assert.throws(TypeError, function() {
			var o = {};
			o.self = o;
			JSON.stringify(o);
		}, 'direct self-reference');

		assert.throws(TypeError, function() {
			var a = [];
			a.push(a);
			JSON.stringify(a);
		}, 'array self-reference');

		assert.throws(TypeError, function() {
			var a = {};
			var b = { ref: a };
			a.ref = b;
			JSON.stringify(a);
		}, 'indirect circular reference');
	`)
}

// TestJSONParseBasic checks that JSON.parse correctly parses all JSON value
// types: primitives, objects, arrays, and nested structures.
func TestJSONParseBasic(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.parse('true'), true);
		assert.sameValue(JSON.parse('false'), false);
		assert.sameValue(JSON.parse('null'), null);
		assert.sameValue(JSON.parse('1'), 1);
		assert.sameValue(JSON.parse('"hello"'), 'hello');
		assert.sameValue(JSON.parse('""'), '');

		var obj = JSON.parse('{"a":1,"b":2}');
		assert.sameValue(obj.a, 1);
		assert.sameValue(obj.b, 2);

		var arr = JSON.parse('[1,2,3]');
		assert.sameValue(arr.length, 3);
		assert.sameValue(arr[0], 1);
		assert.sameValue(arr[2], 3);

		var nested = JSON.parse('{"x":{"y":[1,2]}}');
		assert.sameValue(nested.x.y[1], 2);

		var mixed = JSON.parse('[{"a":1},{"b":2}]');
		assert.sameValue(mixed[0].a, 1);
		assert.sameValue(mixed[1].b, 2);

		var empty = JSON.parse('{}');
		assert.sameValue(Object.keys(empty).length, 0);

		var emptyArr = JSON.parse('[]');
		assert.sameValue(emptyArr.length, 0);
	`)
}

// TestJSONParseStrings checks that JSON.parse correctly decodes the full set of
// JSON string escape sequences, including the six named escapes, \/ (allowed
// by the spec), and \uXXXX Unicode escapes.
func TestJSONParseStrings(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.parse('"hello"'), 'hello');
		assert.sameValue(JSON.parse('""'), '');
		assert.sameValue(JSON.parse('"a\\"b"'), 'a"b');
		assert.sameValue(JSON.parse('"a\\\\b"'), 'a\\b');
		assert.sameValue(JSON.parse('"a\\nb"'), 'a\nb');
		assert.sameValue(JSON.parse('"a\\tb"'), 'a\tb');
		assert.sameValue(JSON.parse('"a\\rb"'), 'a\rb');
		assert.sameValue(JSON.parse('"a\\bb"'), 'a\bb');
		assert.sameValue(JSON.parse('"a\\fb"'), 'a\fb');
		assert.sameValue(JSON.parse('"a\\/b"'), 'a/b');
		assert.sameValue(JSON.parse('"\\u0041"'), 'A');
		assert.sameValue(JSON.parse('"\\u0000"'), '\x00');
		assert.sameValue(JSON.parse('"\\u00e9"'), 'é');
		assert.sameValue(JSON.parse('"\\u4e2d\\u6587"'), '中文');
	`)
}

// TestJSONParseNumbers checks that JSON.parse correctly parses the full range
// of valid JSON number syntax: integers, negative numbers, floating-point, and
// exponent forms with both E and e, and with +/- exponent signs.
func TestJSONParseNumbers(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.parse('0'), 0);
		assert.sameValue(JSON.parse('42'), 42);
		assert.sameValue(JSON.parse('-1'), -1);
		assert.sameValue(JSON.parse('3.14'), 3.14);
		assert.sameValue(JSON.parse('-3.14'), -3.14);
		assert.sameValue(JSON.parse('1e2'), 100);
		assert.sameValue(JSON.parse('1E2'), 100);
		assert.sameValue(JSON.parse('1e+2'), 100);
		assert.sameValue(JSON.parse('1e-2'), 0.01);
		assert.sameValue(JSON.parse('2.5e3'), 2500);
		assert.sameValue(JSON.parse('100'), 100);
		assert.sameValue(JSON.parse('0.5'), 0.5);
	`)
}

// TestJSONParseWhitespace checks that JSON.parse tolerates ASCII whitespace
// (space, tab, newline, carriage return) before and after the JSON value.
func TestJSONParseWhitespace(t *testing.T) {
	Expect(t, `
		assert.sameValue(JSON.parse('  1  '), 1);
		assert.sameValue(JSON.parse('\t42\t'), 42);
		assert.sameValue(JSON.parse('\n"hi"\n'), 'hi');
		assert.sameValue(JSON.parse('\r\n{"a":1}\r\n').a, 1);
		assert.sameValue(JSON.parse('   true   '), true);
		assert.sameValue(JSON.parse(' null '), null);
		assert.sameValue(JSON.parse(' [] ').length, 0);
	`)
}

// TestJSONParseMalformed checks that JSON.parse throws SyntaxError for every
// form of invalid JSON input covered by the spec.
func TestJSONParseMalformed(t *testing.T) {
	Expect(t, `
		assert.throws(SyntaxError, function() { JSON.parse('{bad'); }, 'unquoted key');
		assert.throws(SyntaxError, function() { JSON.parse('{"a":1,}'); }, 'trailing comma in object');
		assert.throws(SyntaxError, function() { JSON.parse('[1,2,]'); }, 'trailing comma in array');
		assert.throws(SyntaxError, function() { JSON.parse("'hello'"); }, 'single-quoted string');
		assert.throws(SyntaxError, function() { JSON.parse('"unterminated'); }, 'unterminated string');
		assert.throws(SyntaxError, function() { JSON.parse(''); }, 'empty input');
		assert.throws(SyntaxError, function() { JSON.parse('undefined'); }, 'undefined literal');
		assert.throws(SyntaxError, function() { JSON.parse('{a:1}'); }, 'unquoted object key');
		assert.throws(SyntaxError, function() { JSON.parse('[1 2]'); }, 'missing comma in array');
		assert.throws(SyntaxError, function() { JSON.parse('1 2'); }, 'extra tokens after value');
		assert.throws(SyntaxError, function() { JSON.parse('+1'); }, 'leading plus sign');
		assert.throws(SyntaxError, function() { JSON.parse('.5'); }, 'leading decimal point');
	`)
}

// TestJSONRoundTrip verifies that JSON.parse(JSON.stringify(x)) produces a
// value that deeply equals the original for all JSON-representable data types.
func TestJSONRoundTrip(t *testing.T) {
	Expect(t, `
		function deepEqual(a, b) {
			if (a === b) return true;
			if (a === null || b === null) return false;
			if (typeof a !== 'object' || typeof b !== 'object') return false;
			var ka = Object.keys(a).sort();
			var kb = Object.keys(b).sort();
			if (ka.join(',') !== kb.join(',')) return false;
			for (var i = 0; i < ka.length; i++) {
				if (!deepEqual(a[ka[i]], b[ka[i]])) return false;
			}
			return true;
		}

		var simple = {a: 1, b: 'str', c: true, d: null, e: [1, 2, 3]};
		assert(deepEqual(JSON.parse(JSON.stringify(simple)), simple), 'simple object round-trip');

		var nested = {x: {y: {z: [1, 2, {w: false}]}}};
		assert(deepEqual(JSON.parse(JSON.stringify(nested)), nested), 'nested round-trip');

		assert.sameValue(JSON.parse(JSON.stringify(42)), 42);
		assert.sameValue(JSON.parse(JSON.stringify('hello')), 'hello');
		assert.sameValue(JSON.parse(JSON.stringify(true)), true);
		assert.sameValue(JSON.parse(JSON.stringify(false)), false);
		assert.sameValue(JSON.parse(JSON.stringify(null)), null);

		var arr = [1, 'two', null, false, [3, 4]];
		var rt = JSON.parse(JSON.stringify(arr));
		assert.sameValue(rt.length, 5);
		assert.sameValue(rt[0], 1);
		assert.sameValue(rt[1], 'two');
		assert.sameValue(rt[2], null);
		assert.sameValue(rt[3], false);
		assert.sameValue(rt[4][0], 3);
		assert.sameValue(rt[4][1], 4);
	`)
}

// TestJSONStringifyReplacerFunction checks the second argument to
// JSON.stringify when it is a function. The replacer receives each key/value
// pair and may return undefined to omit that property.
//
// NOTE: may be unimplemented — the gojs engine ignores the replacer argument.
func TestJSONStringifyReplacerFunction(t *testing.T) {
	Expect(t, `
		var result = JSON.stringify({a: 1, b: 2, c: 3}, function(key, value) {
			if (key === 'b') return undefined;
			return value;
		});
		assert.sameValue(result, '{"a":1,"c":3}');

		var nums = JSON.stringify([1, 2, 3, 4], function(key, value) {
			if (typeof value === 'number' && value % 2 === 0) return null;
			return value;
		});
		assert.sameValue(nums, '[1,null,3,null]');

		var doubled = JSON.stringify({x: 5}, function(key, value) {
			if (typeof value === 'number') return value * 2;
			return value;
		});
		assert.sameValue(doubled, '{"x":10}');
	`)
}

// TestJSONStringifyReplacerArray checks the second argument to JSON.stringify
// when it is an array, acting as a property allowlist: only keys named in the
// array (and their values) are included in the output.
//
// NOTE: may be unimplemented — the gojs engine ignores the replacer argument.
func TestJSONStringifyReplacerArray(t *testing.T) {
	Expect(t, `
		var result = JSON.stringify({a: 1, b: 2, c: 3}, ['a', 'c']);
		assert.sameValue(result, '{"a":1,"c":3}');

		var nested = JSON.stringify({a: 1, b: {c: 2, d: 3}}, ['a', 'b', 'c']);
		assert.sameValue(nested, '{"a":1,"b":{"c":2}}');

		var empty = JSON.stringify({a: 1, b: 2}, []);
		assert.sameValue(empty, '{}');
	`)
}

// TestJSONParseReviver checks the second argument to JSON.parse when it is a
// function. The reviver is called bottom-up for every parsed key/value pair and
// can transform or delete properties by returning a new value or undefined.
//
// NOTE: may be unimplemented — the gojs engine ignores the reviver argument.
func TestJSONParseReviver(t *testing.T) {
	Expect(t, `
		var result = JSON.parse('{"a":1,"b":2}', function(key, value) {
			if (key === '') return value;
			return value * 2;
		});
		assert.sameValue(result.a, 2);
		assert.sameValue(result.b, 4);

		var tagged = JSON.parse('{"created":"2020-01-01"}', function(key, value) {
			if (key === 'created') return 'parsed:' + value;
			return value;
		});
		assert.sameValue(tagged.created, 'parsed:2020-01-01');

		var omit = JSON.parse('{"a":1,"secret":2,"b":3}', function(key, value) {
			if (key === 'secret') return undefined;
			return value;
		});
		assert.sameValue(omit.a, 1);
		assert.sameValue(omit.b, 3);
		assert.sameValue(omit.hasOwnProperty('secret'), false);
	`)
}
