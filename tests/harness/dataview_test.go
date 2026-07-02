package harness

import "testing"

// TestDataViewBasics covers the constructor, buffer/byteLength/byteOffset
// accessors, and toStringTag.
func TestDataViewBasics(t *testing.T) {
	Expect(t, `
		var ab = new ArrayBuffer(16);
		var dv = new DataView(ab, 4, 8);
		assert.sameValue(dv.buffer, ab);
		assert.sameValue(dv.byteOffset, 4);
		assert.sameValue(dv.byteLength, 8);
		assert.sameValue(DataView.name, "DataView");
		assert.sameValue(DataView.length, 1);
		assert.sameValue(Object.getPrototypeOf(dv), DataView.prototype);
		assert.sameValue(DataView.prototype[Symbol.toStringTag], "DataView");
		assert.sameValue(Object.prototype.toString.call(dv), "[object DataView]");

		var full = new DataView(ab);
		assert.sameValue(full.byteOffset, 0);
		assert.sameValue(full.byteLength, 16);
	`)
}

// TestDataViewConstructorErrors covers new-requirement, non-buffer arg, and
// out-of-range offsets/lengths.
func TestDataViewConstructorErrors(t *testing.T) {
	ExpectError(t, `DataView(new ArrayBuffer(8))`, "TypeError")
	ExpectError(t, `new DataView({})`, "TypeError")
	ExpectError(t, `new DataView(new ArrayBuffer(8), 9)`, "RangeError")
	ExpectError(t, `new DataView(new ArrayBuffer(8), 4, 5)`, "RangeError")
	ExpectError(t, `new DataView(new ArrayBuffer(8), -1)`, "RangeError")
}

// TestDataViewIntegers covers signed/unsigned integer round-trips and
// endianness.
func TestDataViewIntegers(t *testing.T) {
	Expect(t, `
		var dv = new DataView(new ArrayBuffer(8));

		dv.setInt8(0, -1);
		assert.sameValue(dv.getInt8(0), -1);
		assert.sameValue(dv.getUint8(0), 255);

		// Big-endian is the default.
		dv.setUint16(0, 0x1234);
		assert.sameValue(dv.getUint8(0), 0x12);
		assert.sameValue(dv.getUint8(1), 0x34);
		assert.sameValue(dv.getUint16(0), 0x1234);
		assert.sameValue(dv.getUint16(0, true), 0x3412, "little-endian read");

		dv.setUint16(0, 0x1234, true);
		assert.sameValue(dv.getUint8(0), 0x34);
		assert.sameValue(dv.getUint8(1), 0x12);

		dv.setInt32(0, -2);
		assert.sameValue(dv.getInt32(0), -2);
		assert.sameValue(dv.getUint32(0), 4294967294);

		// Wrap-around on store.
		dv.setUint8(0, 256 + 5);
		assert.sameValue(dv.getUint8(0), 5);
		dv.setInt16(0, 0x1FFFF);
		assert.sameValue(dv.getUint16(0), 0xFFFF);
	`)
}

// TestDataViewFloats covers float round-trips including specials.
func TestDataViewFloats(t *testing.T) {
	Expect(t, `
		var dv = new DataView(new ArrayBuffer(8));

		dv.setFloat64(0, 3.14159);
		assert.sameValue(dv.getFloat64(0), 3.14159);
		dv.setFloat64(0, 3.14159, true);
		assert.sameValue(dv.getFloat64(0, true), 3.14159);

		dv.setFloat32(0, 1.5);
		assert.sameValue(dv.getFloat32(0), 1.5);

		dv.setFloat64(0, Infinity);
		assert.sameValue(dv.getFloat64(0), Infinity);
		dv.setFloat64(0, -Infinity);
		assert.sameValue(dv.getFloat64(0), -Infinity);
		dv.setFloat64(0, NaN);
		assert.sameValue(dv.getFloat64(0), NaN);
		dv.setFloat32(0, NaN);
		assert.sameValue(dv.getFloat32(0), NaN);

		// Float16 round-trips (values exactly representable in binary16).
		dv.setFloat16(0, 1.5);
		assert.sameValue(dv.getFloat16(0), 1.5);
		dv.setFloat16(0, -544);
		assert.sameValue(dv.getFloat16(0), -544);
		dv.setFloat16(0, Infinity);
		assert.sameValue(dv.getFloat16(0), Infinity);
		dv.setFloat16(0, 100000); // overflows binary16 -> Infinity
		assert.sameValue(dv.getFloat16(0), Infinity);
	`)
}

// TestDataViewBigInt covers 64-bit BigInt accessors.
func TestDataViewBigInt(t *testing.T) {
	Expect(t, `
		var dv = new DataView(new ArrayBuffer(8));
		dv.setBigInt64(0, -1n);
		assert.sameValue(dv.getBigInt64(0), -1n);
		assert.sameValue(dv.getBigUint64(0), 18446744073709551615n);

		dv.setBigUint64(0, 0x0102030405060708n);
		assert.sameValue(dv.getUint8(0), 1);
		assert.sameValue(dv.getUint8(7), 8);
		dv.setBigUint64(0, 0x0102030405060708n, true);
		assert.sameValue(dv.getUint8(0), 8);
	`)
	ExpectError(t, `new DataView(new ArrayBuffer(8)).setBigInt64(0, 1)`, "TypeError")
}

// TestDataViewBounds covers RangeError on out-of-range indices.
func TestDataViewBounds(t *testing.T) {
	ExpectError(t, `new DataView(new ArrayBuffer(4)).getInt32(1)`, "RangeError")
	ExpectError(t, `new DataView(new ArrayBuffer(4)).getUint8(4)`, "RangeError")
	ExpectError(t, `new DataView(new ArrayBuffer(4)).setInt32(1, 0)`, "RangeError")
	ExpectError(t, `new DataView(new ArrayBuffer(8)).getFloat64(1)`, "RangeError")
	// Infinity index -> ToIndex RangeError.
	ExpectError(t, `new DataView(new ArrayBuffer(8)).getInt8(Infinity)`, "RangeError")
	// Negative index -> ToIndex RangeError.
	ExpectError(t, `new DataView(new ArrayBuffer(8)).getInt8(-1)`, "RangeError")
}

// TestDataViewDetached covers TypeError on a detached backing buffer, and that
// numeric conversion of the value argument happens before that check.
func TestDataViewDetached(t *testing.T) {
	ExpectError(t, `
		var ab = new ArrayBuffer(8);
		var dv = new DataView(ab);
		ab.transfer();
		dv.getInt8(0);
	`, "TypeError")
	ExpectError(t, `
		var ab = new ArrayBuffer(8);
		var dv = new DataView(ab);
		ab.transfer();
		dv.setInt8(0, 1);
	`, "TypeError")
	// The value argument is coerced (and may throw) before the detached check.
	Expect(t, `
		var ab = new ArrayBuffer(8);
		var dv = new DataView(ab);
		ab.transfer();
		var order = [];
		try {
			dv.setInt8(0, { valueOf: function() { order.push("value"); return 1; } });
		} catch (e) {
			order.push("threw");
		}
		assert.sameValue(order[0], "value");
		assert.sameValue(order[1], "threw");
	`)
}

// TestDataViewGetterDescriptors checks accessor descriptor attributes.
func TestDataViewGetterDescriptors(t *testing.T) {
	Expect(t, `
		["buffer", "byteLength", "byteOffset"].forEach(function(name) {
			var d = Object.getOwnPropertyDescriptor(DataView.prototype, name);
			assert.sameValue(typeof d.get, "function", name);
			assert.sameValue(d.set, undefined, name);
			assert.sameValue(d.enumerable, false, name);
			assert.sameValue(d.configurable, true, name);
			assert.sameValue(d.get.name, "get " + name, name);
		});
		var m = Object.getOwnPropertyDescriptor(DataView.prototype, "getInt8");
		assert.sameValue(typeof m.value, "function");
		assert.sameValue(m.value.length, 1);
		assert.sameValue(m.value.name, "getInt8");
		assert.sameValue(Object.getOwnPropertyDescriptor(DataView.prototype, "setInt8").value.length, 2);
	`)
}
