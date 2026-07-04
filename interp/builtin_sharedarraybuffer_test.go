package interp

import "testing"

// TestSharedArrayBuffer exercises the Phase-1 SharedArrayBuffer implementation:
// construction, views, Atomics interop, growable buffers and slice. test262's
// SAB coverage is gated on the SharedArrayBuffer feature; these self-checks are
// the verification of the single-agent behaviour.
func TestSharedArrayBuffer(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// Construction and identity.
		{"byteLength", `new SharedArrayBuffer(8).byteLength`, "8"},
		{"toStringTag", `Object.prototype.toString.call(new SharedArrayBuffer(8))`, "[object SharedArrayBuffer]"},
		{"ctor-name", `SharedArrayBuffer.name`, "SharedArrayBuffer"},
		{"is-global", `typeof SharedArrayBuffer`, "function"},
		{"proto-chain", `Object.getPrototypeOf(new SharedArrayBuffer(4)) === SharedArrayBuffer.prototype`, "true"},
		{"not-arraybuffer", `new SharedArrayBuffer(4) instanceof ArrayBuffer`, "false"},
		{"isView-false", `ArrayBuffer.isView(new SharedArrayBuffer(4))`, "false"},

		// Requires new.
		{"no-new-throws", `try { SharedArrayBuffer(8); "no" } catch (e) { e.constructor.name }`, "TypeError"},

		// TypedArray / DataView over a SharedArrayBuffer.
		{"typedarray-view", `const sab = new SharedArrayBuffer(8); const a = new Int32Array(sab); a[0] = 42; a[1] = 7; ` + "`${a[0]},${a[1]},${a.buffer === sab}`", "42,7,true"},
		{"dataview-view", `const sab = new SharedArrayBuffer(8); const dv = new DataView(sab); dv.setUint32(0, 0xdeadbeef); ` + "`${dv.getUint32(0).toString(16)}`", "deadbeef"},
		{"typedarray-byteLength", `new Uint8Array(new SharedArrayBuffer(16)).byteLength`, "16"},

		// Atomics work identically over a shared buffer.
		{"atomics-add", `const a = new Int32Array(new SharedArrayBuffer(16)); a[1] = 5; const old = Atomics.add(a, 1, 3); ` + "`${old},${a[1]}`", "5,8"},
		{"atomics-load-store", `const a = new Int32Array(new SharedArrayBuffer(16)); Atomics.store(a, 2, 99); ` + "`${Atomics.load(a, 2)}`", "99"},
		{"atomics-cx", `const a = new Int32Array(new SharedArrayBuffer(8)); a[0] = 5; Atomics.compareExchange(a, 0, 5, 9); ` + "`${a[0]}`", "9"},

		// Atomics.wait on a shared Int32Array.
		{"wait-not-equal", `const a = new Int32Array(new SharedArrayBuffer(8)); a[0] = 1; Atomics.wait(a, 0, 0)`, "not-equal"},
		{"wait-timed-out", `const a = new Int32Array(new SharedArrayBuffer(8)); Atomics.wait(a, 0, 0, 0)`, "timed-out"},
		{"wait-non-shared-throws", `try { Atomics.wait(new Int32Array(1), 0, 0); "no" } catch (e) { e.constructor.name }`, "TypeError"},
		{"wait-bigint-not-equal", `const a = new BigInt64Array(new SharedArrayBuffer(8)); a[0] = 1n; Atomics.wait(a, 0, 0n)`, "not-equal"},

		// Atomics.notify with no waiters.
		{"notify-zero", `const a = new Int32Array(new SharedArrayBuffer(8)); Atomics.notify(a, 0)`, "0"},
		{"notify-zero-count", `const a = new Int32Array(new SharedArrayBuffer(8)); Atomics.notify(a, 0, 5)`, "0"},

		// Atomics.waitAsync result shape.
		{"waitAsync-not-equal", `const a = new Int32Array(new SharedArrayBuffer(8)); a[0] = 1; const r = Atomics.waitAsync(a, 0, 0); ` + "`${r.async},${r.value}`", "false,not-equal"},
		{"waitAsync-timed-out", `const a = new Int32Array(new SharedArrayBuffer(8)); const r = Atomics.waitAsync(a, 0, 0, 0); ` + "`${r.async},${r.value}`", "false,timed-out"},
		{"waitAsync-pending-promise", `const a = new Int32Array(new SharedArrayBuffer(8)); const r = Atomics.waitAsync(a, 0, 0, 100); ` + "`${r.async},${r.value instanceof Promise}`", "true,true"},
		{"waitAsync-non-shared-throws", `try { Atomics.waitAsync(new Int32Array(1), 0, 0); "no" } catch (e) { e.constructor.name }`, "TypeError"},

		// Growable SharedArrayBuffer.
		{"growable-flags", `const sab = new SharedArrayBuffer(4, {maxByteLength: 8}); ` + "`${sab.growable},${sab.maxByteLength},${sab.byteLength}`", "true,8,4"},
		{"non-growable-flags", `const sab = new SharedArrayBuffer(4); ` + "`${sab.growable},${sab.maxByteLength}`", "false,4"},
		{"grow-bumps-length", `const sab = new SharedArrayBuffer(4, {maxByteLength: 8}); sab.grow(8); sab.byteLength`, "8"},
		{"grow-view-sees-more", `const sab = new SharedArrayBuffer(4, {maxByteLength: 8}); const a = new Int32Array(sab); const before = a.length; sab.grow(8); ` + "`${before},${a.length}`", "1,2"},
		{"grow-shrink-throws", `const sab = new SharedArrayBuffer(4, {maxByteLength: 8}); try { sab.grow(2); "no" } catch (e) { e.constructor.name }`, "RangeError"},
		{"grow-over-max-throws", `const sab = new SharedArrayBuffer(4, {maxByteLength: 8}); try { sab.grow(16); "no" } catch (e) { e.constructor.name }`, "RangeError"},
		{"grow-non-growable-throws", `const sab = new SharedArrayBuffer(4); try { sab.grow(8); "no" } catch (e) { e.constructor.name }`, "TypeError"},

		// slice.
		{"slice-bytes", `const sab = new SharedArrayBuffer(8); const a = new Uint8Array(sab); a[2] = 11; a[3] = 22; const b = new Uint8Array(sab.slice(2, 4)); ` + "`${b.length},${b[0]},${b[1]}`", "2,11,22"},
		{"slice-returns-shared", `const s = new SharedArrayBuffer(8).slice(0, 4); ` + "`${s instanceof SharedArrayBuffer},${s.byteLength}`", "true,4"},
		{"slice-negative", `const sab = new SharedArrayBuffer(8); const a = new Uint8Array(sab); a[7] = 99; const b = new Uint8Array(sab.slice(-1)); ` + "`${b.length},${b[0]}`", "1,99"},

		// species.
		{"species-is-ctor", `SharedArrayBuffer[Symbol.species] === SharedArrayBuffer`, "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := New()
			v, err := i.RunString(tc.name, tc.src)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			got, _ := i.ToStringV(i.ctx, v)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
