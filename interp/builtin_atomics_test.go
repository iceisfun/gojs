package interp

import "testing"

// TestAtomicsOps exercises the Atomics operations on ordinary (non-shared)
// integer TypedArrays. test262's Atomics coverage is gated on the
// SharedArrayBuffer feature and is skipped, so these self-checks are the
// verification that the read-modify-write, load/store, compareExchange,
// isLockFree and pause behaviour is correct on a single-agent buffer.
func TestAtomicsOps(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"add-returns-old", `const a = new Int32Array(4); a[1] = 5; const old = Atomics.add(a, 1, 3); ` + "`${old},${a[1]}`", "5,8"},
		{"sub", `const a = new Int32Array(1); a[0] = 10; const old = Atomics.sub(a, 0, 4); ` + "`${old},${a[0]}`", "10,6"},
		{"and", `const a = new Uint8Array(1); a[0] = 0b1100; Atomics.and(a, 0, 0b1010); ` + "`${a[0]}`", "8"},
		{"or", `const a = new Uint8Array(1); a[0] = 0b1100; Atomics.or(a, 0, 0b0011); ` + "`${a[0]}`", "15"},
		{"xor", `const a = new Uint8Array(1); a[0] = 0b1100; Atomics.xor(a, 0, 0b1010); ` + "`${a[0]}`", "6"},
		{"exchange", `const a = new Int16Array(1); a[0] = 42; const old = Atomics.exchange(a, 0, 7); ` + "`${old},${a[0]}`", "42,7"},
		{"cx-hit", `const a = new Int32Array(1); a[0] = 5; const old = Atomics.compareExchange(a, 0, 5, 9); ` + "`${old},${a[0]}`", "5,9"},
		{"cx-miss", `const a = new Int32Array(1); a[0] = 5; const old = Atomics.compareExchange(a, 0, 4, 9); ` + "`${old},${a[0]}`", "5,5"},
		{"load", `const a = new Uint32Array(2); a[1] = 123; ` + "`${Atomics.load(a, 1)}`", "123"},
		{"store-returns-value", `const a = new Int8Array(1); ` + "`${Atomics.store(a, 0, 3)},${a[0]}`", "3,3"},
		{"store-truncates-element-but-returns-int", `const a = new Int8Array(1); const r = Atomics.store(a, 0, 300); ` + "`${r},${a[0]}`", "300,44"},
		{"uint8-wraparound-on-add", `const a = new Uint8Array(1); a[0] = 250; Atomics.add(a, 0, 10); ` + "`${a[0]}`", "4"},

		// BigInt element types.
		{"bigint-add", `const a = new BigInt64Array(1); a[0] = 5n; const old = Atomics.add(a, 0, 3n); ` + "`${old},${a[0]}`", "5,8"},
		{"bigint-cx-hit", `const a = new BigUint64Array(1); a[0] = 7n; Atomics.compareExchange(a, 0, 7n, 20n); ` + "`${a[0]}`", "20"},
		{"bigint-cx-miss", `const a = new BigInt64Array(1); a[0] = 7n; Atomics.compareExchange(a, 0, 6n, 20n); ` + "`${a[0]}`", "7"},

		// isLockFree / pause / string tag.
		{"isLockFree", `` + "`${Atomics.isLockFree(1)},${Atomics.isLockFree(2)},${Atomics.isLockFree(4)},${Atomics.isLockFree(8)},${Atomics.isLockFree(3)}`", "true,true,true,true,false"},
		{"pause-undefined", `` + "`${Atomics.pause()}`", "undefined"},
		{"pause-with-int", `` + "`${Atomics.pause(10)}`", "undefined"},
		{"toStringTag", `Object.prototype.toString.call(Atomics)`, "[object Atomics]"},

		// Error paths.
		{"non-typedarray", `try { Atomics.add([], 0, 1); "no" } catch (e) { e.constructor.name }`, "TypeError"},
		{"float-array-rejected", `try { Atomics.add(new Float64Array(1), 0, 1); "no" } catch (e) { e.constructor.name }`, "TypeError"},
		{"clamped-rejected", `try { Atomics.add(new Uint8ClampedArray(1), 0, 1); "no" } catch (e) { e.constructor.name }`, "TypeError"},
		{"index-out-of-range", `try { Atomics.load(new Int32Array(2), 5); "no" } catch (e) { e.constructor.name }`, "RangeError"},
		{"pause-non-integer", `try { Atomics.pause(1.5); "no" } catch (e) { e.constructor.name }`, "TypeError"},
		{"wait-non-shared-throws", `try { Atomics.wait(new Int32Array(1), 0, 0); "no" } catch (e) { e.constructor.name }`, "TypeError"},
		{"wait-wrong-kind", `try { Atomics.wait(new Uint8Array(1), 0, 0); "no" } catch (e) { e.constructor.name }`, "TypeError"},
		{"notify-non-shared-zero", `` + "`${Atomics.notify(new Int32Array(1), 0)}`", "0"},
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
