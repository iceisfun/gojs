package harness

import "testing"

// Symbol.prototype.description reports undefined when no description was given,
// the empty string for Symbol(""), and throws on a non-symbol receiver.
func TestSymbolDescription(t *testing.T) {
	Expect(t, `
		assert.sameValue(Symbol().description, undefined, "no description");
		assert.sameValue(Symbol(undefined).description, undefined, "undefined description");
		assert.sameValue(Symbol("").description, "", "empty-string description");
		assert.sameValue(Symbol("desc").description, "desc", "string description");
		assert.sameValue(Object(Symbol("w")).description, "w", "wrapper");
		var getter = Object.getOwnPropertyDescriptor(Symbol.prototype, "description").get;
		assert.throws(TypeError, function () { getter.call(null); });
		assert.throws(TypeError, function () { getter.call({}); });
	`)
}

// Symbol.prototype.toString renders "Symbol()" for a descriptionless symbol.
func TestSymbolToString(t *testing.T) {
	Expect(t, `
		assert.sameValue(Symbol().toString(), "Symbol()");
		assert.sameValue(Symbol("").toString(), "Symbol()");
		assert.sameValue(Symbol("x").toString(), "Symbol(x)");
	`)
}

// Symbol.for and Symbol.keyFor share the global registry.
func TestSymbolForKeyFor(t *testing.T) {
	Expect(t, `
		var s = Symbol.for("hello");
		assert.sameValue(Symbol.for("hello"), s, "for returns the same symbol");
		assert.sameValue(Symbol.keyFor(s), "hello", "keyFor returns the key");
		assert.sameValue(Symbol.keyFor(Symbol("nope")), undefined, "unregistered symbol");
		assert.sameValue(Symbol.keyFor.length, 1);
		assert.sameValue(Symbol.keyFor.name, "keyFor");
		assert.throws(TypeError, function () { Symbol.keyFor("not a symbol"); });
	`)
}

// Well-known symbols are non-writable, non-enumerable, non-configurable.
func TestSymbolWellKnownDescriptors(t *testing.T) {
	Expect(t, `
		["iterator","asyncIterator","toPrimitive","toStringTag","hasInstance",
		 "match","matchAll","replace","search","split","species","unscopables",
		 "isConcatSpreadable"].forEach(function (name) {
			var d = Object.getOwnPropertyDescriptor(Symbol, name);
			assert.sameValue(typeof d.value, "symbol", name + " is a symbol");
			assert.sameValue(d.writable, false, name + " writable");
			assert.sameValue(d.enumerable, false, name + " enumerable");
			assert.sameValue(d.configurable, false, name + " configurable");
		});
	`)
}

// Symbol.prototype[Symbol.toPrimitive] and [Symbol.toStringTag].
func TestSymbolPrototypeToPrimitive(t *testing.T) {
	Expect(t, `
		var d = Object.getOwnPropertyDescriptor(Symbol.prototype, Symbol.toPrimitive);
		assert.sameValue(typeof d.value, "function");
		assert.sameValue(d.value.name, "[Symbol.toPrimitive]");
		assert.sameValue(d.value.length, 1);
		assert.sameValue(d.writable, false);
		assert.sameValue(d.enumerable, false);
		assert.sameValue(d.configurable, true);
		var s = Symbol("z");
		assert.sameValue(d.value.call(s), s, "on a symbol");
		assert.sameValue(d.value.call(Object(s)), s, "on a wrapper");
		assert.throws(TypeError, function () { d.value.call(undefined); });

		var t = Object.getOwnPropertyDescriptor(Symbol.prototype, Symbol.toStringTag);
		assert.sameValue(t.value, "Symbol");
		assert.sameValue(t.writable, false);
		assert.sameValue(t.enumerable, false);
		assert.sameValue(t.configurable, true);
	`)
}
