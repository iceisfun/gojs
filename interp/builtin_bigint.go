package interp

import (
	"context"
	"math"
	"math/big"
	"strconv"
	"strings"
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

	// BigInt.prototype.toString ( [ radix ] ) — §21.2.3.3. Its length is 0
	// because the radix argument is optional.
	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		v, ok := bigOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "BigInt.prototype.toString called on non-BigInt")
		}
		radix := 10.0
		if !IsUndefined(arg(args, 0)) {
			// ToIntegerOrInfinity begins with ToNumber, which throws a
			// TypeError for Symbol/BigInt radix values (§7.1.5, §7.1.4).
			f, err := i.ToNumberV(ctx, arg(args, 0))
			if err != nil {
				return nil, err
			}
			radix = ToInteger(f)
		}
		if radix < 2 || radix > 36 {
			return nil, i.throwError(ctx, "RangeError", "toString() radix must be between 2 and 36")
		}
		return String(v.Text(int(radix))), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		v, ok := bigOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "BigInt.prototype.valueOf called on non-BigInt")
		}
		return &BigInt{Int: v}, nil
	})
	// BigInt.prototype.toLocaleString ( [ locales [ , options ] ] ) — §21.2.3.2.
	// Like Number's, this is locale-formatted through x/text (see
	// locale_format.go) rather than returning a bare toString.
	i.defineMethod(proto, "toLocaleString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		v, ok := bigOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "BigInt.prototype.toLocaleString called on non-BigInt")
		}
		s, err := i.formatLocaleBigInt(ctx, v, arg(args, 0), arg(args, 1))
		if err != nil {
			return nil, err
		}
		return String(s), nil
	})

	// BigInt.prototype[Symbol.toStringTag] = "BigInt" — §21.2.3.5.
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("BigInt"), Writable: false, Enumerable: false, Configurable: true})

	ctor := i.newNativeCtor("BigInt", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.bigIntConstructorValue(ctx, arg(args, 0))
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "BigInt is not a constructor")
	})
	ctor.fn.ctor = false
	linkCtor(ctor, proto)

	// BigInt.asIntN / asUintN wrap to N bits. bits is coerced via ToIndex
	// (§7.1.22): negatives and values > 2^53-1 throw RangeError, while
	// Symbol/BigInt throw TypeError. ToIndex runs before ToBigInt (§21.2.2.1).
	i.defineMethod(ctor, "asIntN", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		bits, err := i.toIndex(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		b, err := i.toBigInt(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return wrapBits(b.(*BigInt).Int, bits, true), nil
	})
	i.defineMethod(ctor, "asUintN", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		bits, err := i.toIndex(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		b, err := i.toBigInt(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return wrapBits(b.(*BigInt).Int, bits, false), nil
	})

	i.setGlobalHidden("BigInt", ctor)
}

// bigIntConstructorValue implements the BigInt(value) coercion (§21.2.1.1): the
// value is reduced to a primitive with hint Number; a Number primitive is
// converted via NumberToBigInt (RangeError on non-integers), otherwise the
// primitive is fed to ToBigInt.
func (i *Interpreter) bigIntConstructorValue(ctx context.Context, v Value) (Value, error) {
	prim, err := i.ToPrimitive(ctx, v, "number")
	if err != nil {
		return nil, err
	}
	if n, ok := prim.(Number); ok {
		return i.numberToBigInt(ctx, float64(n))
	}
	return i.primitiveToBigInt(ctx, prim)
}

// toBigInt implements ToBigInt (§7.1.13): it reduces v to a primitive with hint
// Number, then converts. A Number primitive throws a TypeError (unlike the
// BigInt constructor, which accepts integral Numbers).
func (i *Interpreter) toBigInt(ctx context.Context, v Value) (Value, error) {
	prim, err := i.ToPrimitive(ctx, v, "number")
	if err != nil {
		return nil, err
	}
	if n, ok := prim.(Number); ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot convert "+NumberToString(float64(n))+" to a BigInt because it is not a BigInt")
	}
	return i.primitiveToBigInt(ctx, prim)
}

// numberToBigInt implements NumberToBigInt (§21.2.1.1.1): the Number must be an
// integral value, else a RangeError is thrown.
func (i *Interpreter) numberToBigInt(ctx context.Context, f float64) (Value, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return nil, i.throwError(ctx, "RangeError", "The number "+NumberToString(f)+" cannot be converted to a BigInt because it is not an integer")
	}
	bi, _ := big.NewFloat(f).Int(nil)
	return &BigInt{Int: bi}, nil
}

// primitiveToBigInt converts a non-Number primitive to a BigInt following the
// type dispatch of ToBigInt (§7.1.13). Number is handled by the callers.
func (i *Interpreter) primitiveToBigInt(ctx context.Context, prim Value) (Value, error) {
	switch x := prim.(type) {
	case *BigInt:
		return x, nil
	case Boolean:
		if bool(x) {
			return NewBigInt(1), nil
		}
		return NewBigInt(0), nil
	case String:
		n, ok := stringToBigInt(string(x))
		if !ok {
			return nil, i.throwError(ctx, "SyntaxError", "Cannot convert "+strconv.Quote(string(x))+" to a BigInt")
		}
		return &BigInt{Int: n}, nil
	case *Symbol:
		return nil, i.throwError(ctx, "TypeError", "Cannot convert a Symbol value to a BigInt")
	default: // Undefined, Null
		return nil, i.throwError(ctx, "TypeError", "Cannot convert "+briefValue(prim)+" to a BigInt")
	}
}

// stringToBigInt implements StringToBigInt (§7.1.14): leading/trailing
// whitespace and line terminators are ignored; an empty (or whitespace-only)
// string is 0n. The numeric part is a StrIntegerLiteral — a signed decimal
// integer or an unsigned 0b/0o/0x non-decimal integer literal (no decimal
// points, exponents, or "Infinity"). ok is false when the string is not a valid
// literal.
func stringToBigInt(s string) (*big.Int, bool) {
	t := strings.TrimFunc(s, isJSWhiteSpaceOrLineTerminator)
	if t == "" {
		return big.NewInt(0), true
	}
	n := new(big.Int)
	// Non-decimal integer literals: a "0b"/"0o"/"0x" prefix, no sign allowed.
	if len(t) > 2 && t[0] == '0' {
		base := 0
		switch t[1] {
		case 'b', 'B':
			base = 2
		case 'o', 'O':
			base = 8
		case 'x', 'X':
			base = 16
		}
		if base != 0 {
			rest := t[2:]
			if rest[0] == '+' || rest[0] == '-' {
				return nil, false
			}
			if _, ok := n.SetString(rest, base); ok {
				return n, true
			}
			return nil, false
		}
	}
	// Signed decimal integer literal (optional + or -, decimal digits only).
	if _, ok := n.SetString(t, 10); ok {
		return n, true
	}
	return nil, false
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
