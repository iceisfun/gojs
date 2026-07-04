package interp

import (
	"context"
	"math"
	"math/big"
)

// initAtomics installs the Atomics namespace object (§25.4).
//
// gojs runs a single agent — one VM has one thread of control — so the
// read-modify-write operations are atomic by construction: nothing can
// interleave between the read and the write within an agent. SharedArrayBuffer,
// the cross-agent shared memory these operations exist to coordinate, is not
// implemented, so Atomics.wait (which requires a shared buffer) always throws a
// TypeError and Atomics.notify always reports zero woken agents. Every other
// operation — add/sub/and/or/xor, exchange, compareExchange, load, store,
// isLockFree and pause — is fully functional on an ordinary integer TypedArray,
// which is the primitive a host would build cross-VM coordination on if it ever
// shares a buffer between agents.
func (i *Interpreter) initAtomics() {
	a := NewObject(i.objectProto)
	a.class = "Atomics"
	// Atomics[Symbol.toStringTag] = "Atomics", { w:false, e:false, c:true } (§25.4.15).
	a.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("Atomics"), Writable: false, Enumerable: false, Configurable: true})

	// The binary read-modify-write operations. numOp acts on integer element
	// types (computing in a wide domain and letting the element write mask to
	// width); bigOp acts on BigInt element types.
	rmw := func(name string, numOp func(old, val int64) int64, bigOp func(old, val *big.Int) *big.Int) {
		i.defineMethod(a, name, 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
			td, idx, err := i.atomicAccess(ctx, args, false)
			if err != nil {
				return nil, err
			}
			if taKinds[td.kind].bigInt {
				bv, err := i.toBigInt(ctx, arg(args, 2))
				if err != nil {
					return nil, err
				}
				if _, ok := td.validIndex(float64(idx)); !ok {
					return nil, i.throwError(ctx, "TypeError", "Atomics."+name+" on an out-of-bounds TypedArray")
				}
				old := td.getElement(idx).(*BigInt)
				td.setElementBig(idx, bigOp(new(big.Int).Set(old.Int), bv.(*BigInt).Int))
				return old, nil
			}
			val, err := i.atomicIntArg(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics."+name+" on an out-of-bounds TypedArray")
			}
			old := td.getElement(idx).(Number)
			td.setElementNum(idx, float64(numOp(int64(float64(old)), val)))
			return old, nil
		})
	}
	rmw("add", func(o, v int64) int64 { return o + v }, func(o, v *big.Int) *big.Int { return o.Add(o, v) })
	rmw("sub", func(o, v int64) int64 { return o - v }, func(o, v *big.Int) *big.Int { return o.Sub(o, v) })
	rmw("and", func(o, v int64) int64 { return o & v }, func(o, v *big.Int) *big.Int { return o.And(o, v) })
	rmw("or", func(o, v int64) int64 { return o | v }, func(o, v *big.Int) *big.Int { return o.Or(o, v) })
	rmw("xor", func(o, v int64) int64 { return o ^ v }, func(o, v *big.Int) *big.Int { return o.Xor(o, v) })

	// exchange(ta, index, value): store value, return the previous value.
	i.defineMethod(a, "exchange", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		if taKinds[td.kind].bigInt {
			bv, err := i.toBigInt(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics.exchange on an out-of-bounds TypedArray")
			}
			old := td.getElement(idx)
			td.setElementBig(idx, bv.(*BigInt).Int)
			return old, nil
		}
		val, err := i.atomicNumArg(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		if _, ok := td.validIndex(float64(idx)); !ok {
			return nil, i.throwError(ctx, "TypeError", "Atomics.exchange on an out-of-bounds TypedArray")
		}
		old := td.getElement(idx)
		td.setElementNum(idx, val)
		return old, nil
	})

	// compareExchange(ta, index, expected, replacement): store replacement only
	// if the current value equals expected (compared in element representation);
	// return the previous value.
	i.defineMethod(a, "compareExchange", 4, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		if taKinds[td.kind].bigInt {
			expBV, err := i.toBigInt(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			repBV, err := i.toBigInt(ctx, arg(args, 3))
			if err != nil {
				return nil, err
			}
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics.compareExchange on an out-of-bounds TypedArray")
			}
			old := td.getElement(idx).(*BigInt)
			// Compare in the element's stored bit-pattern (mod 2^64).
			if td.bigBits(old.Int) == td.bigBits(expBV.(*BigInt).Int) {
				td.setElementBig(idx, repBV.(*BigInt).Int)
			}
			return old, nil
		}
		expected, err := i.atomicNumArg(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		replacement, err := i.atomicNumArg(ctx, arg(args, 3))
		if err != nil {
			return nil, err
		}
		if _, ok := td.validIndex(float64(idx)); !ok {
			return nil, i.throwError(ctx, "TypeError", "Atomics.compareExchange on an out-of-bounds TypedArray")
		}
		old := td.getElement(idx).(Number)
		if td.numBits(float64(old)) == td.numBits(expected) {
			td.setElementNum(idx, replacement)
		}
		return old, nil
	})

	// load(ta, index): return the current value.
	i.defineMethod(a, "load", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		return td.getElement(idx), nil
	})

	// store(ta, index, value): store value, returning the integer value stored
	// (not the previous value).
	i.defineMethod(a, "store", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		td, idx, err := i.atomicAccess(ctx, args, false)
		if err != nil {
			return nil, err
		}
		if taKinds[td.kind].bigInt {
			bv, err := i.toBigInt(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			if _, ok := td.validIndex(float64(idx)); !ok {
				return nil, i.throwError(ctx, "TypeError", "Atomics.store on an out-of-bounds TypedArray")
			}
			td.setElementBig(idx, bv.(*BigInt).Int)
			return bv, nil
		}
		// The spec converts with ToIntegerOrInfinity and returns that integer
		// Number, independent of what the element write truncates it to.
		f, err := i.ToNumberV(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		v := integerOrInfinity(f)
		if _, ok := td.validIndex(float64(idx)); !ok {
			return nil, i.throwError(ctx, "TypeError", "Atomics.store on an out-of-bounds TypedArray")
		}
		td.setElementNum(idx, v)
		return Number(v), nil
	})

	// isLockFree(size): whether an atomic op on an element of the given byte size
	// is lock-free. gojs treats 1, 2, 4 and 8 as lock-free.
	i.defineMethod(a, "isLockFree", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		n := integerOrInfinity(f)
		return Boolean(n == 1 || n == 2 || n == 4 || n == 8), nil
	})

	// wait(ta, index, value, timeout): only valid on an Int32Array/BigInt64Array
	// over a SharedArrayBuffer. gojs has no shared buffers, so — after the same
	// validation a conforming engine performs — this always throws a TypeError.
	i.defineMethod(a, "wait", 4, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if _, _, err := i.atomicAccess(ctx, args, true); err != nil {
			return nil, err
		}
		return nil, i.throwError(ctx, "TypeError", "Atomics.wait cannot be used on a non-shared ArrayBuffer")
	})

	// notify(ta, index, count): wake agents waiting on the location. With no
	// shared memory there are never any waiters, so this reports 0.
	i.defineMethod(a, "notify", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if _, _, err := i.atomicAccess(ctx, args, true); err != nil {
			return nil, err
		}
		return Number(0), nil
	})

	// pause(N): a hint that the caller is in a spin-wait loop (§25.4.14). N, when
	// given, must be an integral Number; the value is otherwise ignored. Returns
	// undefined.
	i.defineMethod(a, "pause", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if v := arg(args, 0); v != Undef {
			n, ok := v.(Number)
			if !ok || math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) || float64(n) != math.Trunc(float64(n)) {
				return nil, i.throwError(ctx, "TypeError", "Atomics.pause: iterationNumber must be an integer")
			}
		}
		return Undef, nil
	})

	i.setGlobalHidden("Atomics", a)
}

// atomicAccess implements ValidateIntegerTypedArray followed by the index part
// of ValidateAtomicAccess (§25.4.3): it checks that args[0] is an integer (or,
// when waitable, an Int32Array/BigInt64Array) TypedArray on an in-bounds,
// non-detached buffer, and that args[1] converts to an in-range access index.
// Value coercion (which may run user code and shrink the buffer) is left to the
// caller, which re-validates the index with validIndex before the access.
func (i *Interpreter) atomicAccess(ctx context.Context, args []Value, waitable bool) (*typedArrayData, int, error) {
	td, ok := typedArrayOf(arg(args, 0))
	if !ok {
		return nil, 0, i.throwError(ctx, "TypeError", "Atomics operation called on a non-TypedArray")
	}
	switch {
	case waitable:
		if td.kind != taInt32 && td.kind != taBigInt64 {
			return nil, 0, i.throwError(ctx, "TypeError", "Atomics.wait/notify requires an Int32Array or BigInt64Array")
		}
	case td.kind == taUint8Clamped || td.kind == taFloat32 || td.kind == taFloat64:
		return nil, 0, i.throwError(ctx, "TypeError", "Atomics operation requires an integer TypedArray")
	}
	oob, length := td.outOfBounds()
	if oob {
		return nil, 0, i.throwError(ctx, "TypeError", "Atomics operation called on an out-of-bounds TypedArray")
	}
	idx, err := i.toIndex(ctx, arg(args, 1))
	if err != nil {
		return nil, 0, err
	}
	if idx >= length {
		return nil, 0, i.throwError(ctx, "RangeError", "Atomics access index "+NumberToString(float64(idx))+" is out of bounds")
	}
	return td, idx, nil
}

// atomicNumArg coerces an Atomics value argument for a non-BigInt element to the
// float the element write expects (ToIntegerOrInfinity, per the spec's numeric
// read-modify-write conversion).
func (i *Interpreter) atomicNumArg(ctx context.Context, v Value) (float64, error) {
	f, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	return integerOrInfinity(f), nil
}

// atomicIntArg is atomicNumArg reduced to an int64 for the integer bitwise/
// arithmetic ops; the element write masks the result to the element width.
func (i *Interpreter) atomicIntArg(ctx context.Context, v Value) (int64, error) {
	f, err := i.atomicNumArg(ctx, v)
	if err != nil {
		return 0, err
	}
	switch {
	case f >= 9.223372036854776e18:
		return math.MaxInt64, nil
	case f <= -9.223372036854776e18:
		return math.MinInt64, nil
	}
	return int64(f), nil
}

// numBits returns the element-width bit pattern taWriteNum would store for f, so
// two values can be compared the way the memory does (compareExchange).
func (td *typedArrayData) numBits(f float64) uint64 {
	var buf [8]byte
	size := taKinds[td.kind].size
	taWriteNum(td.kind, buf[:size], f)
	var b uint64
	for k := size - 1; k >= 0; k-- {
		b = b<<8 | uint64(buf[k])
	}
	return b
}

// bigBits returns the low 64 bits of v, the pattern setElementBig stores.
func (td *typedArrayData) bigBits(v *big.Int) uint64 {
	return bigIntToUint64(v)
}

// integerOrInfinity implements ToIntegerOrInfinity on an already-computed Number:
// NaN becomes 0, ±Infinity is preserved, and any finite value is truncated
// toward zero.
func integerOrInfinity(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	if math.IsInf(f, 0) {
		return f
	}
	return math.Trunc(f)
}
