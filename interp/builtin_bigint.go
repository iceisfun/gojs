package interp

import (
	"context"
	"math"
	"math/big"
	"strconv"
)

// initBigInt installs the BigInt function and BigInt.prototype methods. BigInt
// is not a constructor (calling it with `new` throws), matching the spec.
func (i *Interpreter) initBigInt() {
	proto := i.bigintProto

	bigOf := func(this Value) (*big.Int, bool) {
		switch x := this.(type) {
		case *BigInt:
			return x.Int, true
		case *Object:
			if b, ok := x.primitive.(*BigInt); ok {
				return b.Int, true
			}
		}
		return nil, false
	}

	i.defineMethod(proto, "toString", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		v, ok := bigOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "BigInt.prototype.toString called on non-BigInt")
		}
		radix := 10
		if !IsUndefined(arg(args, 0)) {
			radix, _ = i.argInt(ctx, args, 0)
		}
		if radix < 2 || radix > 36 {
			return nil, i.throwError(ctx, "RangeError", "toString() radix must be between 2 and 36")
		}
		return String(v.Text(radix)), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		v, ok := bigOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "BigInt.prototype.valueOf called on non-BigInt")
		}
		return &BigInt{Int: v}, nil
	})

	ctor := i.newNativeCtor("BigInt", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.toBigInt(ctx, arg(args, 0))
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "BigInt is not a constructor")
	})
	ctor.fn.ctor = false
	linkCtor(ctor, proto)

	// BigInt.asIntN / asUintN wrap to N bits.
	i.defineMethod(ctor, "asIntN", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		bits, _ := i.argInt(ctx, args, 0)
		b, err := i.toBigInt(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return wrapBits(b.(*BigInt).Int, bits, true), nil
	})
	i.defineMethod(ctor, "asUintN", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		bits, _ := i.argInt(ctx, args, 0)
		b, err := i.toBigInt(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return wrapBits(b.(*BigInt).Int, bits, false), nil
	})

	i.setGlobalHidden("BigInt", ctor)
}

// toBigInt converts a value to a BigInt following the BigInt() coercion rules.
func (i *Interpreter) toBigInt(ctx context.Context, v Value) (Value, error) {
	switch x := v.(type) {
	case *BigInt:
		return x, nil
	case Boolean:
		if bool(x) {
			return NewBigInt(1), nil
		}
		return NewBigInt(0), nil
	case Number:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
			return nil, i.throwError(ctx, "RangeError", "The number "+NumberToString(f)+" cannot be converted to a BigInt because it is not an integer")
		}
		bi, _ := big.NewFloat(f).Int(nil)
		return &BigInt{Int: bi}, nil
	case String:
		s := string(x)
		n := new(big.Int)
		if s == "" {
			return NewBigInt(0), nil
		}
		if _, ok := n.SetString(s, 0); !ok {
			if _, ok := n.SetString(s, 10); !ok {
				return nil, i.throwError(ctx, "SyntaxError", "Cannot convert "+strconv.Quote(s)+" to a BigInt")
			}
		}
		return &BigInt{Int: n}, nil
	default:
		return nil, i.throwError(ctx, "TypeError", "Cannot convert "+briefValue(v)+" to a BigInt")
	}
}

// wrapBits reduces v modulo 2^bits, interpreting the result as signed or
// unsigned (BigInt.asIntN / asUintN).
func wrapBits(v *big.Int, bits int, signed bool) Value {
	if bits <= 0 {
		return NewBigInt(0)
	}
	mod := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	r := new(big.Int).Mod(v, mod)
	if signed {
		half := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
		if r.Cmp(half) >= 0 {
			r.Sub(r, mod)
		}
	}
	return &BigInt{Int: r}
}
