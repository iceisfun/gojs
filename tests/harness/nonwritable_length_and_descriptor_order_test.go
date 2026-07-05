package harness

import "testing"

// TestArrayIndexPastNonWritableLength covers Array [[DefineOwnProperty]] /
// OrdinarySet for a new index at or beyond a non-writable "length"
// (§10.4.2.1 step 3.b): the element must NOT be created — a sloppy assignment is
// dropped, and Array.prototype.push (a Set-with-Throw) raises a TypeError before
// writing anything.
func TestArrayIndexPastNonWritableLength(t *testing.T) {
	Expect(t, `
		// push must throw WITHOUT having mutated the array.
		var a = [1, 2, 3];
		Object.defineProperty(a, "length", { writable: false });
		var threw = false;
		try { a.push(4); } catch (e) { threw = (e instanceof TypeError); }
		assert.sameValue(threw, true, "push throws TypeError");
		assert.sameValue(a.length, 3, "length unchanged after failed push");
		assert.sameValue(a.join(","), "1,2,3", "elements unchanged after failed push");
		assert.sameValue(a.hasOwnProperty(3), false, "no index 3 created by push");

		// Sloppy indexed assignment past the end is silently dropped.
		var b = [1, 2, 3];
		Object.defineProperty(b, "length", { writable: false });
		b[3] = 4;
		assert.sameValue(b.length, 3, "length unchanged after dropped assignment");
		assert.sameValue(b[3], undefined, "index 3 not created by assignment");

		// Strict assignment past the end throws.
		var c = [1, 2, 3];
		Object.defineProperty(c, "length", { writable: false });
		assert.throws(TypeError, function () { "use strict"; c[3] = 4; }, "strict assignment throws");
		assert.sameValue(c.length, 3, "length unchanged after strict throw");

		// A frozen array behaves the same (length is non-writable too).
		var f = Object.freeze([1, 2, 3]);
		assert.throws(TypeError, function () { f.push(4); }, "push on frozen array throws");
		assert.sameValue(f.length, 3, "frozen length unchanged");

		// Writing WITHIN a sparse tail below a non-writable length is still allowed
		// (it does not grow length).
		var s = [1, 2, 3];
		s.length = 10;
		Object.defineProperty(s, "length", { writable: false });
		s[5] = 99;
		assert.sameValue(s[5], 99, "in-bounds sparse write allowed");
		assert.sameValue(s.length, 10, "length still 10");
		// But an index at/after length is still refused.
		s[10] = 1;
		assert.sameValue(s[10], undefined, "index at length refused");
		assert.sameValue(s.length, 10, "length unchanged");
	`)
}

// TestDefinePropertyDescriptorGetterOrder covers ToPropertyDescriptor
// (§6.2.6.5): every present descriptor field is Get in order (enumerable,
// configurable, value, writable, get, set) BEFORE the "cannot specify both
// accessors and a value/writable attribute" TypeError is raised. So a descriptor
// whose "value" and "get" are themselves getters runs both getters first.
func TestDefinePropertyDescriptorGetterOrder(t *testing.T) {
	Expect(t, `
		var log = [];
		var desc = {};
		Object.defineProperty(desc, "value", { get: function () { log.push("value"); return 1; } });
		Object.defineProperty(desc, "get",   { get: function () { log.push("get"); return function () { return 2; }; } });
		var threw = false;
		try { Object.defineProperty({}, "x", desc); } catch (e) { threw = (e instanceof TypeError); }
		assert.sameValue(threw, true, "both accessor and data => TypeError");
		assert.sameValue(log.join(","), "value,get", "value and get getters both ran, in order");

		// The get-must-be-callable TypeError (raised while Get-ing "get") still
		// precedes the accessor+data check.
		assert.throws(TypeError, function () {
			Object.defineProperty({}, "y", { value: 1, get: 123 });
		}, "non-callable getter throws");

		// A well-formed accessor descriptor with getters for enumerable/get works,
		// and the fields are read in order.
		var order = [];
		var d2 = {};
		Object.defineProperty(d2, "enumerable",   { get: function () { order.push("enumerable"); return true; } });
		Object.defineProperty(d2, "configurable", { get: function () { order.push("configurable"); return true; } });
		Object.defineProperty(d2, "get",          { get: function () { order.push("get"); return function () { return 42; }; } });
		var obj = {};
		Object.defineProperty(obj, "p", d2);
		assert.sameValue(order.join(","), "enumerable,configurable,get", "fields read in spec order");
		assert.sameValue(obj.p, 42, "accessor installed");
	`)
}
