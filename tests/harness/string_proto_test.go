package harness

import "testing"

// The dispatch to a well-known-symbol method (Symbol.split/@@replace/etc.) on
// the argument of String.prototype.{split,search,replace,replaceAll,match,
// matchAll} is only performed when that argument is an Object. A primitive
// argument must never have its symbol property accessed (2025 spec change:
// "If separator/searchValue/regexp is an Object, then ...").

func TestStringSymbolMethodNotAccessedOnPrimitiveArg(t *testing.T) {
	Expect(t, `
		function trap(proto, sym) {
			Object.defineProperty(proto, sym, {
				configurable: true,
				get: function() { throw new Error('should not be called'); },
			});
		}
		trap(Number.prototype, Symbol.split);
		trap(Boolean.prototype, Symbol.replace);
		trap(String.prototype, Symbol.search);
		trap(Number.prototype, Symbol.match);
		trap(Boolean.prototype, Symbol.matchAll);

		assert.sameValue('a1b1c'.split(1).join(','), 'a,b,c');
		assert.sameValue('aXb'.replace(true, '-'), 'aXb');
		assert.sameValue('abc'.search('b'), 1);
		assert.sameValue('a1b'.match(1)[0], '1');
		var it = 'a1b'.matchAll(true);
		assert.sameValue(typeof it[Symbol.iterator], 'function');
	`)
}

// The symbol method on an Object argument receives the ORIGINAL receiver
// unchanged (not ToString'd), and is looked up even when the receiver is a
// String wrapper object.

func TestStringSymbolMethodReceivesRawReceiver(t *testing.T) {
	Expect(t, `
		var searchValue = /./g;
		var recv = new String('Leo');
		var repl = {};
		var seen;
		Object.defineProperty(searchValue, Symbol.replace, {
			value: function(O, replaceValue) {
				assert.sameValue(this, searchValue);
				assert.sameValue(O, recv, 'raw receiver');
				assert.sameValue(replaceValue, repl, 'raw replaceValue');
				seen = true;
				return 42;
			},
		});
		assert.sameValue(recv.replaceAll(searchValue, repl), 42);
		assert.sameValue(seen, true);
	`)
}

// search/match/matchAll create an internal RegExp and Invoke its symbol method
// via a fresh property lookup, so a user-overridden RegExp.prototype method is
// honored, and a removed one is a TypeError.

func TestStringSearchInvokesRegExpPrototypeSymbol(t *testing.T) {
	Expect(t, `
		var original = RegExp.prototype[Symbol.search];
		var sentinel = {};
		var thisVal, arg0;
		RegExp.prototype[Symbol.search] = function() {
			thisVal = this;
			arg0 = arguments[0];
			return sentinel;
		};
		try {
			var result = 'target'.search('string source');
			assert(thisVal instanceof RegExp, 'this is a RegExp');
			assert.sameValue(thisVal.source, 'string source');
			assert.sameValue(arg0, 'target');
			assert.sameValue(result, sentinel);
		} finally {
			RegExp.prototype[Symbol.search] = original;
		}
	`)
}

func TestStringMatchAllRemovedRegExpSymbolThrows(t *testing.T) {
	Expect(t, `
		var original = Object.getOwnPropertyDescriptor(RegExp.prototype, Symbol.matchAll);
		delete RegExp.prototype[Symbol.matchAll];
		try {
			var threw = false;
			try { 'abc'.matchAll(/\w/g); } catch (e) { threw = e instanceof TypeError; }
			assert.sameValue(threw, true, 'TypeError when @@matchAll is absent');
		} finally {
			Object.defineProperty(RegExp.prototype, Symbol.matchAll, original);
		}
	`)
}

// isWellFormed / toWellFormed exist with the correct shape and coerce `this`.

func TestStringWellFormedMethodShape(t *testing.T) {
	Expect(t, `
		['isWellFormed', 'toWellFormed'].forEach(function(name) {
			var d = Object.getOwnPropertyDescriptor(String.prototype, name);
			assert.sameValue(typeof d.value, 'function', name + ' is a function');
			assert.sameValue(d.writable, true, name + ' writable');
			assert.sameValue(d.enumerable, false, name + ' non-enumerable');
			assert.sameValue(d.configurable, true, name + ' configurable');
			assert.sameValue(d.value.length, 0, name + ' length');
			assert.sameValue(d.value.name, name, name + ' name');
		});
		assert.sameValue('abc'.isWellFormed(), true);
		assert.sameValue('a▨c'.isWellFormed(), true);
		assert.sameValue('a💩c'.toWellFormed(), 'a💩c');
	`)
}

func TestStringWellFormedCoercesThis(t *testing.T) {
	ExpectError(t, `String.prototype.isWellFormed.call(null);`, "TypeError")
	ExpectError(t, `String.prototype.toWellFormed.call(undefined);`, "TypeError")
	Expect(t, `
		var thrown = {};
		var receiver = { toString: function() { throw thrown; } };
		var got;
		try { String.prototype.isWellFormed.call(receiver); } catch (e) { got = e; }
		assert.sameValue(got, thrown, 'abrupt ToString(this) propagates');
	`)
}
