package interp

import "math"

// Boxing a Number into a Value interface heap-allocates 8 bytes: Go's static
// interface table only covers integer *bit patterns* below 256, and a float64's
// bit pattern is never that small (1.0 is 0x3FF0…). Every arithmetic result in a
// hot loop therefore allocates. We can't make float results free without changing
// the value representation, but the overwhelmingly common result in real code is
// a small integer — an array index, a length, a byte value, a loop counter, a
// character code — so interning those removes most of the churn.
//
// The table covers [smallIntMin, smallIntMax). numberValue returns a shared boxed
// Value for an integral result in range and allocates a fresh box for everything
// else (non-integral floats, large integers, NaN, ±Inf, and -0). Primitives have
// no observable identity, so sharing a box is invisible to the language; the -0
// exclusion is what keeps Object.is(-0,0) and 1/-0 correct.
const (
	smallIntMin = -128
	smallIntMax = 1024
)

var smallInts [smallIntMax - smallIntMin]Value

func init() {
	for k := range smallInts {
		smallInts[k] = Number(float64(smallIntMin + k))
	}
}

// numberValue boxes f, interning small integral results. Callers use it wherever
// they would otherwise write Number(f) for an arithmetic result.
func numberValue(f float64) Value {
	// The range test also filters NaN and ±Inf (all comparisons with NaN are
	// false; ±Inf is out of range), so they fall through to a fresh box.
	if f >= smallIntMin && f < smallIntMax {
		if n := int64(f); float64(n) == f { // integral?
			if n != 0 || !math.Signbit(f) { // exclude -0, which must stay distinct
				return smallInts[n-smallIntMin]
			}
		}
	}
	return Number(f)
}
