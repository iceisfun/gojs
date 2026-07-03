package harness

import "testing"

// These regression tests guard two systemic Object-builtin fixes: the
// higher-level Object methods must visit symbol-keyed properties (not just
// string keys), and Object.freeze/seal (with isFrozen/isSealed) must apply to
// an array's dense indexed elements.

// TestObjectAssignCopiesSymbols checks that Object.assign copies own enumerable
// symbol-keyed properties (CopyDataProperties, §7.3.25), and that a non-enumerable
// symbol property is skipped just like a non-enumerable string property.
func TestObjectAssignCopiesSymbols(t *testing.T) {
	Expect(t, `
		var s = Symbol('x');
		var hidden = Symbol('h');
		var src = { a: 1 };
		src[s] = 42;
		Object.defineProperty(src, hidden, { value: 99, enumerable: false });
		var dst = {};
		var ret = Object.assign(dst, src);
		assert.sameValue(ret, dst);
		assert.sameValue(dst.a, 1);
		assert.sameValue(dst[s], 42);
		assert.sameValue(Object.prototype.hasOwnProperty.call(dst, hidden), false);
	`)
}

// TestGetOwnPropertyDescriptorsIncludesSymbols checks that
// Object.getOwnPropertyDescriptors returns descriptors keyed by symbols as well
// as strings (§20.1.2.9).
func TestGetOwnPropertyDescriptorsIncludesSymbols(t *testing.T) {
	Expect(t, `
		var s = Symbol('x');
		var o = { a: 1 };
		o[s] = 2;
		var d = Object.getOwnPropertyDescriptors(o);
		assert.sameValue(Object.getOwnPropertySymbols(d).length, 1);
		assert.sameValue(d[s].value, 2);
		assert.sameValue(d[s].writable, true);
		assert.sameValue(d[s].enumerable, true);
		assert.sameValue(d[s].configurable, true);
		assert.sameValue(d.a.value, 1);
	`)
}

// TestFreezeArrayFreezesElements checks that Object.freeze on an array makes its
// dense indexed elements non-writable and non-configurable, makes "length"
// non-writable, and that Object.isFrozen and getOwnPropertyDescriptor report the
// frozen state (SetIntegrityLevel/TestIntegrityLevel, §7.3.15/16).
func TestFreezeArrayFreezesElements(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3];
		var ret = Object.freeze(a);
		assert.sameValue(ret, a);
		assert.sameValue(Object.isFrozen(a), true);
		assert.sameValue(Object.isSealed(a), true);

		var d0 = Object.getOwnPropertyDescriptor(a, 0);
		assert.sameValue(d0.writable, false);
		assert.sameValue(d0.configurable, false);
		assert.sameValue(d0.value, 1);

		var dl = Object.getOwnPropertyDescriptor(a, "length");
		assert.sameValue(dl.writable, false);

		// A frozen element cannot be reassigned (silently ignored in sloppy mode).
		a[0] = 99;
		assert.sameValue(a[0], 1);
	`)
}

// TestSealArraySealsElements checks that Object.seal on an array makes its dense
// elements non-configurable while leaving them writable, and that isSealed
// (but not isFrozen) reports the sealed state.
func TestSealArraySealsElements(t *testing.T) {
	Expect(t, `
		var a = [1, 2, 3];
		Object.seal(a);
		assert.sameValue(Object.isSealed(a), true);
		assert.sameValue(Object.isFrozen(a), false);

		var d0 = Object.getOwnPropertyDescriptor(a, 0);
		assert.sameValue(d0.writable, true);
		assert.sameValue(d0.configurable, false);

		// A sealed (but not frozen) element is still writable.
		a[0] = 7;
		assert.sameValue(a[0], 7);
		// It cannot be deleted (non-configurable).
		assert.sameValue(delete a[0], false);
		assert.sameValue(a[0], 7);
	`)
}
