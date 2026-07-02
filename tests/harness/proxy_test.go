package harness

import "testing"

// TestProxyGetTrap covers the get trap and fallthrough to the target.
func TestProxyGetTrap(t *testing.T) {
	Expect(t, `
		var log = [];
		var target = { a: 1, b: 2 };
		var p = new Proxy(target, {
			get: function (t, key, receiver) {
				log.push(key);
				if (key === "a") return 42;
				return t[key];
			}
		});
		assert.sameValue(p.a, 42, "get trap intercepts");
		assert.sameValue(p.b, 2, "get trap can defer to target");
		assert.sameValue(log.length, 2, "trap called per access");

		// Absent trap falls through to the target.
		var p2 = new Proxy(target, {});
		assert.sameValue(p2.a, 1, "no trap -> target");
	`)
}

// TestProxySetTrap covers the set trap.
func TestProxySetTrap(t *testing.T) {
	Expect(t, `
		var target = {};
		var p = new Proxy(target, {
			set: function (t, key, value, receiver) {
				t[key] = value * 2;
				return true;
			}
		});
		p.x = 5;
		assert.sameValue(target.x, 10, "set trap intercepts");

		var plain = new Proxy({}, {});
		plain.y = 7;
		assert.sameValue(plain.y, 7, "no set trap -> target");
	`)
}

// TestProxyHasDeleteTrap covers has and deleteProperty traps.
func TestProxyHasDeleteTrap(t *testing.T) {
	Expect(t, `
		var target = { visible: 1, _hidden: 2 };
		var p = new Proxy(target, {
			has: function (t, key) { return key[0] !== "_" && key in t; },
			deleteProperty: function (t, key) { delete t[key]; return true; }
		});
		assert.sameValue("visible" in p, true, "has trap true");
		assert.sameValue("_hidden" in p, false, "has trap hides");
		assert.sameValue(delete p.visible, true, "delete trap");
		assert.sameValue("visible" in target, false, "delete applied");
	`)
}

// TestProxyOwnKeysTrap covers ownKeys via Object.keys/Reflect.ownKeys.
func TestProxyOwnKeysTrap(t *testing.T) {
	Expect(t, `
		var target = { a: 1, b: 2, c: 3 };
		var p = new Proxy(target, {
			ownKeys: function (t) { return ["a", "c"]; },
			getOwnPropertyDescriptor: function (t, key) {
				return Object.getOwnPropertyDescriptor(t, key);
			}
		});
		var keys = Reflect.ownKeys(p);
		assert.sameValue(keys.length, 2, "ownKeys trap length");
		assert.sameValue(keys[0], "a", "ownKeys trap[0]");
		assert.sameValue(keys[1], "c", "ownKeys trap[1]");
	`)
}

// TestProxyGetOwnPropertyDescriptorTrap covers the descriptor trap.
func TestProxyGetOwnPropertyDescriptorTrap(t *testing.T) {
	Expect(t, `
		var p = new Proxy({}, {
			getOwnPropertyDescriptor: function (t, key) {
				return { value: 99, writable: true, enumerable: true, configurable: true };
			}
		});
		var d = Object.getOwnPropertyDescriptor(p, "anything");
		assert.sameValue(d.value, 99, "descriptor trap value");
	`)
}

// TestProxyDefinePropertyTrap covers the defineProperty trap.
func TestProxyDefinePropertyTrap(t *testing.T) {
	Expect(t, `
		var log = [];
		var p = new Proxy({}, {
			defineProperty: function (t, key, desc) {
				log.push(key);
				return Reflect.defineProperty(t, key, desc);
			}
		});
		Object.defineProperty(p, "z", { value: 1, configurable: true });
		assert.sameValue(log[0], "z", "defineProperty trap called");
		assert.sameValue(Reflect.defineProperty(p, "w", { value: 2, configurable: true }), true, "Reflect.defineProperty via trap");
	`)
}

// TestProxyPrototypeTraps covers getPrototypeOf/setPrototypeOf/isExtensible/preventExtensions.
func TestProxyPrototypeTraps(t *testing.T) {
	Expect(t, `
		var proto = { p: 1 };
		var p = new Proxy({}, {
			getPrototypeOf: function (t) { return proto; }
		});
		assert.sameValue(Object.getPrototypeOf(p), proto, "getPrototypeOf trap");
		assert.sameValue(Reflect.getPrototypeOf(p), proto, "Reflect.getPrototypeOf trap");

		var ext = new Proxy({}, {
			isExtensible: function (t) { return Reflect.isExtensible(t); },
			preventExtensions: function (t) { Object.preventExtensions(t); return true; }
		});
		assert.sameValue(Reflect.isExtensible(ext), true, "isExtensible trap");
		assert.sameValue(Reflect.preventExtensions(ext), true, "preventExtensions trap");
		assert.sameValue(Reflect.isExtensible(ext), false, "now non-extensible");
	`)
}

// TestProxyApplyTrap covers a callable proxy's apply trap.
func TestProxyApplyTrap(t *testing.T) {
	Expect(t, `
		function target(a, b) { return a + b; }
		var p = new Proxy(target, {
			apply: function (t, thisArg, args) { return t.apply(thisArg, args) * 10; }
		});
		assert.sameValue(typeof p, "function", "callable proxy is a function");
		assert.sameValue(p(2, 3), 50, "apply trap");
		assert.sameValue(Reflect.apply(p, undefined, [1, 1]), 20, "Reflect.apply via trap");

		var noTrap = new Proxy(target, {});
		assert.sameValue(noTrap(4, 5), 9, "no apply trap -> target call");
	`)
}

// TestProxyConstructTrap covers a constructor proxy's construct trap and new.target.
func TestProxyConstructTrap(t *testing.T) {
	Expect(t, `
		function Target(v) { this.v = v; }
		var p = new Proxy(Target, {
			construct: function (t, args, newTarget) {
				return Reflect.construct(t, [args[0] + 1], newTarget);
			}
		});
		var o = new p(10);
		assert.sameValue(o.v, 11, "construct trap");
		assert.sameValue(o instanceof Target, true, "construct trap prototype");

		var noTrap = new Proxy(Target, {});
		assert.sameValue(new noTrap(3).v, 3, "no construct trap -> target");
	`)
}

// TestProxyRevocable covers Proxy.revocable and post-revocation TypeErrors.
func TestProxyRevocable(t *testing.T) {
	Expect(t, `
		var r = Proxy.revocable({ a: 1 }, {});
		assert.sameValue(r.proxy.a, 1, "revocable proxy works before revoke");
		assert.sameValue(typeof r.revoke, "function", "revoke is a function");
		r.revoke();
		assert.throws(TypeError, function () { return r.proxy.a; }, "get after revoke throws");
		assert.throws(TypeError, function () { r.proxy.a = 2; }, "set after revoke throws");
		assert.throws(TypeError, function () { return "a" in r.proxy; }, "has after revoke throws");
		r.revoke(); // idempotent
	`)
}

// TestProxyConstructorErrors covers construction argument validation.
func TestProxyConstructorErrors(t *testing.T) {
	ExpectError(t, `Proxy({}, {})`, "TypeError")    // requires new
	ExpectError(t, `new Proxy(1, {})`, "TypeError") // target must be object
	ExpectError(t, `new Proxy({}, 1)`, "TypeError") // handler must be object
	ExpectError(t, `new Proxy(1, 2)`, "TypeError")
}

// TestProxyRevokedInvariant covers the revoked-target TypeError on many ops.
func TestProxyRevokedInvariant(t *testing.T) {
	Expect(t, `
		var r = Proxy.revocable({}, {});
		r.revoke();
		var count = 0;
		[
			function () { Reflect.get(r.proxy, "x"); },
			function () { Reflect.has(r.proxy, "x"); },
			function () { Reflect.ownKeys(r.proxy); },
			function () { Reflect.getPrototypeOf(r.proxy); }
		].forEach(function (f) {
			try { f(); } catch (e) { if (e instanceof TypeError) count++; }
		});
		assert.sameValue(count, 4, "all ops throw TypeError after revoke");
	`)
}

// A handler trap that revokes its own proxy mid-operation must not crash the
// host: the internal method binds target before invoking the trap, so the
// post-trap invariant checks still see a valid target. Constructing through a
// revoked-proxy new.target throws a TypeError (GetFunctionRealm on a revoked
// proxy). Regression for a nil-target panic.
func TestProxyRevokeDuringTrap(t *testing.T) {
	ExpectError(t, `
		var handle = Proxy.revocable(function () {}, {
			get: function () { handle.revoke(); }
		});
		new handle.proxy();
	`, "TypeError")
	// A get that revokes mid-read must not panic; it returns the trap result.
	Expect(t, `
		var handle = Proxy.revocable({}, { get: function () { handle.revoke(); return 42; } });
		assert.sameValue(handle.proxy.x, 42);
		var threw = false;
		try { handle.proxy.y; } catch (e) { threw = (e instanceof TypeError); }
		assert(threw, "operating on a revoked proxy throws TypeError");
	`)
}
