package harness

import "testing"

// TestArrayBufferBasics exercises the ArrayBuffer constructor, byteLength, and
// the ArrayBuffer.isView / species / toStringTag surface.
func TestArrayBufferBasics(t *testing.T) {
	Expect(t, `
		var ab = new ArrayBuffer(8);
		assert.sameValue(ab.byteLength, 8);
		assert.sameValue(new ArrayBuffer().byteLength, 0);
		assert.sameValue(Object.getPrototypeOf(ab), ArrayBuffer.prototype);
		assert.sameValue(ArrayBuffer.name, "ArrayBuffer");
		assert.sameValue(ArrayBuffer.length, 1);
		assert.sameValue(ArrayBuffer.isView(ab), false);
		assert.sameValue(ArrayBuffer.isView(new DataView(ab)), true);
		assert.sameValue(ArrayBuffer.isView({}), false);
		assert.sameValue(ArrayBuffer.isView(1), false);
		assert.sameValue(ArrayBuffer[Symbol.species], ArrayBuffer);
		assert.sameValue(Object.prototype.toString.call(ab), "[object ArrayBuffer]");
		assert.sameValue(ArrayBuffer.prototype[Symbol.toStringTag], "ArrayBuffer");
	`)
}

// TestArrayBufferConstructorErrors covers the `new`, negative, and too-large
// requirements of the constructor.
func TestArrayBufferConstructorErrors(t *testing.T) {
	ExpectError(t, `ArrayBuffer(8)`, "TypeError")
	ExpectError(t, `new ArrayBuffer(-1)`, "RangeError")
	ExpectError(t, `new ArrayBuffer(-Infinity)`, "RangeError")
	ExpectError(t, `new ArrayBuffer(9007199254740992)`, "RangeError")
	ExpectError(t, `new ArrayBuffer(Infinity)`, "RangeError")
}

// TestArrayBufferDescriptors checks that the accessor and constructor property
// descriptors match the spec.
func TestArrayBufferDescriptors(t *testing.T) {
	Expect(t, `
		var d = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, "byteLength");
		assert.sameValue(typeof d.get, "function");
		assert.sameValue(d.set, undefined);
		assert.sameValue(d.enumerable, false);
		assert.sameValue(d.configurable, true);
		assert.sameValue(d.get.name, "get byteLength");

		var nd = Object.getOwnPropertyDescriptor(ArrayBuffer, "name");
		assert.sameValue(nd.writable, false);
		assert.sameValue(nd.enumerable, false);
		assert.sameValue(nd.configurable, true);

		var sd = Object.getOwnPropertyDescriptor(ArrayBuffer, Symbol.species);
		assert.sameValue(sd.set, undefined);
		assert.sameValue(typeof sd.get, "function");
	`)
}

// TestArrayBufferSlice covers slice bounds, species, and copying.
func TestArrayBufferSlice(t *testing.T) {
	Expect(t, `
		var ab = new ArrayBuffer(8);
		var dv = new DataView(ab);
		for (var i = 0; i < 8; i++) dv.setUint8(i, i + 1);

		var s = ab.slice(2, 5);
		assert.sameValue(s.byteLength, 3);
		var sv = new DataView(s);
		assert.sameValue(sv.getUint8(0), 3);
		assert.sameValue(sv.getUint8(1), 4);
		assert.sameValue(sv.getUint8(2), 5);

		assert.sameValue(ab.slice(-2).byteLength, 2);
		assert.sameValue(ab.slice(5, 2).byteLength, 0);
		assert.sameValue(ab.slice().byteLength, 8);
		assert.sameValue(ab.slice(0, 100).byteLength, 8);
	`)
}

// TestArrayBufferResizable covers the resizable-buffer surface.
func TestArrayBufferResizable(t *testing.T) {
	Expect(t, `
		var ab = new ArrayBuffer(4, {maxByteLength: 8});
		assert.sameValue(ab.resizable, true);
		assert.sameValue(ab.maxByteLength, 8);
		assert.sameValue(ab.byteLength, 4);

		var dv = new DataView(ab);
		dv.setUint8(0, 42);
		ab.resize(8);
		assert.sameValue(ab.byteLength, 8);
		assert.sameValue(dv.getUint8(0), 42, "bytes preserved across resize");
		ab.resize(2);
		assert.sameValue(ab.byteLength, 2);

		var fixed = new ArrayBuffer(4);
		assert.sameValue(fixed.resizable, false);
		assert.sameValue(fixed.maxByteLength, 4);
	`)
	ExpectError(t, `new ArrayBuffer(8, {maxByteLength: 4})`, "RangeError")
	ExpectError(t, `new ArrayBuffer(4).resize(2)`, "TypeError")
	ExpectError(t, `new ArrayBuffer(4, {maxByteLength: 8}).resize(16)`, "RangeError")
}

// TestArrayBufferTransfer covers transfer / transferToFixedLength and detach.
func TestArrayBufferTransfer(t *testing.T) {
	Expect(t, `
		var ab = new ArrayBuffer(4);
		new DataView(ab).setUint8(1, 99);
		assert.sameValue(ab.detached, false);

		var t1 = ab.transfer();
		assert.sameValue(ab.detached, true, "source detached after transfer");
		assert.sameValue(ab.byteLength, 0);
		assert.sameValue(t1.byteLength, 4);
		assert.sameValue(new DataView(t1).getUint8(1), 99);

		var big = t1.transfer(6);
		assert.sameValue(big.byteLength, 6);
		assert.sameValue(t1.detached, true);

		var rz = new ArrayBuffer(4, {maxByteLength: 16});
		assert.sameValue(rz.transfer().resizable, true, "transfer preserves resizability");
		var fx = new ArrayBuffer(4, {maxByteLength: 16}).transferToFixedLength();
		assert.sameValue(fx.resizable, false);
	`)
	ExpectError(t, `
		var ab = new ArrayBuffer(4);
		ab.transfer();
		ab.transfer();
	`, "TypeError")
	ExpectError(t, `
		var ab = new ArrayBuffer(4);
		ab.transfer();
		ab.slice(0);
	`, "TypeError")
}
