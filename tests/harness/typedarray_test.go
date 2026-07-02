package harness

import "testing"

// TestTypedArrayConstruction covers the four construction forms and the shared
// backing buffer.
func TestTypedArrayConstruction(t *testing.T) {
	Expect(t, `
		var a = new Uint8Array(4);
		assert.sameValue(a.length, 4);
		assert.sameValue(a.byteLength, 4);
		assert.sameValue(a.byteOffset, 0);
		assert.sameValue(a.BYTES_PER_ELEMENT, 1);
		assert.sameValue(Uint8Array.BYTES_PER_ELEMENT, 1);
		assert.sameValue(a[0], 0);

		var b = new Int32Array([1, 2, 3]);
		assert.sameValue(b.length, 3);
		assert.sameValue(b.byteLength, 12);
		assert.sameValue(b[2], 3);

		var c = new Float64Array(b);
		assert.sameValue(c.length, 3);
		assert.sameValue(c[1], 2);

		var ab = new ArrayBuffer(16);
		var d = new Uint16Array(ab, 4, 2);
		assert.sameValue(d.buffer, ab);
		assert.sameValue(d.byteOffset, 4);
		assert.sameValue(d.length, 2);
		assert.sameValue(d.byteLength, 4);

		var e = new Uint8Array([10, 20, 30]);
		assert.sameValue(e.toString(), "10,20,30");
		assert.sameValue(e[Symbol.iterator], e.values);
	`)
}

// TestTypedArrayNames covers the name / prototype / toStringTag wiring.
func TestTypedArrayNames(t *testing.T) {
	Expect(t, `
		var kinds = ["Int8Array","Uint8Array","Uint8ClampedArray","Int16Array",
			"Uint16Array","Int32Array","Uint32Array","Float32Array","Float64Array",
			"BigInt64Array","BigUint64Array"];
		for (var i = 0; i < kinds.length; i++) {
			var C = globalThis[kinds[i]];
			assert.sameValue(C.name, kinds[i]);
			assert.sameValue(C.length, 3);
			assert.sameValue(Object.getPrototypeOf(C.prototype)[Symbol.toStringTag], undefined);
		}
		var a = new Int16Array(2);
		assert.sameValue(a[Symbol.toStringTag], "Int16Array");
		assert.sameValue(Object.prototype.toString.call(a), "[object Int16Array]");
		// The abstract %TypedArray% is the [[Prototype]] of every constructor.
		var TA = Object.getPrototypeOf(Int8Array);
		assert.sameValue(TA, Object.getPrototypeOf(Uint8Array));
		assert.throws(TypeError, function () { new TA(3); });
	`)
}

// TestTypedArrayIndexing covers the integer-indexed exotic behavior.
func TestTypedArrayIndexing(t *testing.T) {
	Expect(t, `
		var a = new Uint8Array(3);
		a[0] = 255;
		a[1] = 256;   // wraps to 0
		a[2] = -1;    // wraps to 255
		assert.sameValue(a[0], 255);
		assert.sameValue(a[1], 0);
		assert.sameValue(a[2], 255);

		a[5] = 9;      // out of bounds write: no-op
		assert.sameValue(a[5], undefined);
		assert.sameValue(5 in a, false);
		assert.sameValue(0 in a, true);
		assert.sameValue("1.5" in a, false);
		assert.sameValue(a["-0"], undefined);

		assert.sameValue(delete a[0], false);   // valid index: cannot delete
		assert.sameValue(delete a[9], true);    // invalid index: vacuous success
		assert.sameValue(a[0], 255);

		var keys = Object.keys(a);
		assert.sameValue(keys.length, 3);
		assert.sameValue(keys[0], "0");

		var c = new Uint8ClampedArray(3);
		c[0] = 300;  // clamps to 255
		c[1] = -5;   // clamps to 0
		c[2] = 2.5;  // rounds to even -> 2
		assert.sameValue(c[0], 255);
		assert.sameValue(c[1], 0);
		assert.sameValue(c[2], 2);
	`)
}

// TestTypedArrayBigInt covers BigInt64/BigUint64 semantics.
func TestTypedArrayBigInt(t *testing.T) {
	Expect(t, `
		var a = new BigInt64Array(2);
		a[0] = 10n;
		a[1] = -1n;
		assert.sameValue(a[0], 10n);
		assert.sameValue(a[1], -1n);
		assert.sameValue(typeof a[0], "bigint");
		assert.throws(TypeError, function () { a[0] = 1; });
		var u = new BigUint64Array([0n]);
		u[0] = -1n;
		assert.sameValue(u[0], 18446744073709551615n);
	`)
	ExpectError(t, `new Int8Array(new BigInt64Array(1))`, "TypeError")
}

// TestTypedArrayMethods covers the prototype methods.
func TestTypedArrayMethods(t *testing.T) {
	Expect(t, `
		var a = new Int32Array([5, 3, 1, 4, 2]);
		assert.sameValue(a.at(-1), 2);
		assert.sameValue(a.indexOf(4), 3);
		assert.sameValue(a.includes(3), true);
		assert.sameValue(a.join("-"), "5-3-1-4-2");

		var doubled = a.map(function (x) { return x * 2; });
		assert.sameValue(doubled instanceof Int32Array, true);
		assert.sameValue(doubled[0], 10);

		var evens = a.filter(function (x) { return x % 2 === 0; });
		assert.sameValue(evens.length, 2);
		assert.sameValue(evens[0], 4);

		var sum = a.reduce(function (acc, x) { return acc + x; }, 0);
		assert.sameValue(sum, 15);

		a.sort();
		assert.sameValue(a.join(","), "1,2,3,4,5");
		a.reverse();
		assert.sameValue(a.join(","), "5,4,3,2,1");

		var s = a.slice(1, 3);
		assert.sameValue(s.join(","), "4,3");

		var sub = a.subarray(1, 3);
		assert.sameValue(sub.buffer, a.buffer);
		assert.sameValue(sub.length, 2);
		sub[0] = 99;
		assert.sameValue(a[1], 99);

		var f = new Uint8Array(4);
		f.fill(7, 1, 3);
		assert.sameValue(f.join(","), "0,7,7,0");

		var cw = new Uint8Array([1, 2, 3, 4, 5]);
		cw.copyWithin(0, 3);
		assert.sameValue(cw.join(","), "4,5,3,4,5");

		assert.sameValue(Uint8Array.of(1, 2, 3).join(","), "1,2,3");
		assert.sameValue(Uint8Array.from([1, 2, 3], function (x) { return x + 1; }).join(","), "2,3,4");
		assert.sameValue(Uint8Array.from("abc").length, 3);

		var w = new Int8Array([1, 2, 3]).with(1, 9);
		assert.sameValue(w.join(","), "1,9,3");
		var ts = new Int8Array([3, 1, 2]).toSorted();
		assert.sameValue(ts.join(","), "1,2,3");
		var tr = new Int8Array([1, 2, 3]).toReversed();
		assert.sameValue(tr.join(","), "3,2,1");
	`)
}

// TestTypedArraySet covers prototype.set with both source forms.
func TestTypedArraySet(t *testing.T) {
	Expect(t, `
		var a = new Uint8Array(5);
		a.set([1, 2, 3], 1);
		assert.sameValue(a.join(","), "0,1,2,3,0");
		var b = new Uint8Array([9, 8]);
		a.set(b, 0);
		assert.sameValue(a.join(","), "9,8,2,3,0");
	`)
	ExpectError(t, `new Uint8Array(2).set([1, 2, 3])`, "RangeError")
}

// TestTypedArrayConstructorErrors covers new-requirement and abstract ctor.
func TestTypedArrayConstructorErrors(t *testing.T) {
	ExpectError(t, `Uint8Array(3)`, "TypeError")
	ExpectError(t, `new Int32Array(new ArrayBuffer(6))`, "RangeError")
	ExpectError(t, `new Int32Array(new ArrayBuffer(16), 6)`, "RangeError")
}
