package interp

import "testing"

// TestTypedArrayBufferCacheStale guards the abCache pointer on typedArrayData
// (builtin_typedarray.go): after an element access populates the cache, a resize
// or detach of the backing buffer — which mutates the same arrayBufferData in
// place — must still be observed on the next access.
func TestTypedArrayBufferCacheStale(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// grow, then read the newly in-bounds tail: cache must see new length/data.
		{`var b=new ArrayBuffer(4,{maxByteLength:16}); var v=new Uint8Array(b);
		  v[0]=1; var before=v[8]; b.resize(16); v[8]=42; "" + before + "," + v[8]`, "undefined,42"},
		// shrink, then read the now-out-of-bounds index: must return undefined.
		{`var b=new ArrayBuffer(16,{maxByteLength:16}); var v=new Uint8Array(b);
		  v[8]=7; var before=v[8]; b.resize(4); "" + before + "," + v[8]`, "7,undefined"},
		// detach after a cached access: reads become undefined.
		{`var b=new ArrayBuffer(8); var v=new Uint8Array(b);
		  v[0]=9; var before=v[0]; b.transfer(); "" + before + "," + v[0]`, "9,undefined"},
	}
	for _, tc := range cases {
		i := New(WithBytecode())
		v, err := i.RunString("t", tc.src)
		if err != nil {
			t.Errorf("%s: error %v", tc.src, err)
			continue
		}
		got, _ := asString(v)
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
}
