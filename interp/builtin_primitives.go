package interp

import (
	"context"
	"math"
	"strconv"
	"strings"
)

// This file installs the Symbol, Number, and Boolean intrinsics. String lives
// in builtin_string.go and Math/JSON in their own files.

// ---------------------------------------------------------------------------
// Symbol
// ---------------------------------------------------------------------------

func (i *Interpreter) initSymbol() {
	proto := i.symbolProto
	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		sym, ok := thisSymbol(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Symbol.prototype.toString called on non-symbol")
		}
		return String("Symbol(" + sym.Desc + ")"), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		sym, ok := thisSymbol(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Symbol.prototype.valueOf called on non-symbol")
		}
		return sym, nil
	})
	proto.DefineAccessor("description",
		i.newNativeFunc("get description", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
			sym, ok := thisSymbol(this)
			if !ok {
				return Undef, nil
			}
			return String(sym.Desc), nil
		}), nil, false)

	ctor := i.newNativeCtor("Symbol", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		desc := ""
		if !IsUndefined(arg(args, 0)) {
			var err error
			desc, err = i.argStr(ctx, args, 0)
			if err != nil {
				return nil, err
			}
		}
		return &Symbol{Desc: desc}, nil
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Symbol is not a constructor")
	})
	ctor.fn.ctor = false
	linkCtor(ctor, proto)

	// Well-known symbols.
	ctor.SetHidden("iterator", i.symIterator)
	ctor.SetHidden("asyncIterator", i.symAsyncIterator)
	ctor.SetHidden("toPrimitive", i.symToPrimitive)
	ctor.SetHidden("toStringTag", i.symToStringTag)
	ctor.SetHidden("hasInstance", i.symHasInstance)

	// A tiny registry for Symbol.for/keyFor.
	registry := map[string]*Symbol{}
	i.defineMethod(ctor, "for", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		key, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		if s, ok := registry[key]; ok {
			return s, nil
		}
		s := &Symbol{Desc: key}
		registry[key] = s
		return s, nil
	})

	i.setGlobalHidden("Symbol", ctor)
}

func thisSymbol(this Value) (*Symbol, bool) {
	switch x := this.(type) {
	case *Symbol:
		return x, true
	case *Object:
		if s, ok := x.primitive.(*Symbol); ok {
			return s, true
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Number
// ---------------------------------------------------------------------------

func (i *Interpreter) initNumber() {
	proto := i.numberProto
	// Number.prototype is itself a Number wrapper with [[NumberData]] 0, so
	// Number.prototype.valueOf() and .toString() work on it directly.
	proto.class = "Number"
	proto.primitive = Number(0)

	num := func(this Value) (float64, bool) {
		switch x := this.(type) {
		case Number:
			return float64(x), true
		case *Object:
			if n, ok := x.primitive.(Number); ok {
				return float64(n), true
			}
		}
		return 0, false
	}

	i.defineMethod(proto, "toString", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.toString called on non-number")
		}
		radix := 10
		if !IsUndefined(arg(args, 0)) {
			radix, _ = i.argInt(ctx, args, 0)
		}
		if radix == 10 {
			return String(NumberToString(f)), nil
		}
		if radix < 2 || radix > 36 {
			return nil, i.throwError(ctx, "RangeError", "toString() radix must be between 2 and 36")
		}
		if f == math.Trunc(f) && !math.IsInf(f, 0) {
			return String(strconv.FormatInt(int64(f), radix)), nil
		}
		return String(strconv.FormatFloat(f, 'g', -1, 64)), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.valueOf called on non-number")
		}
		return Number(f), nil
	})
	i.defineMethod(proto, "toFixed", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, _ := num(this)
		digits, _ := i.argInt(ctx, args, 0)
		if digits < 0 || digits > 100 {
			return nil, i.throwError(ctx, "RangeError", "toFixed() digits argument must be between 0 and 100")
		}
		if math.IsNaN(f) {
			return String("NaN"), nil
		}
		if math.Abs(f) >= 1e21 {
			return String(NumberToString(f)), nil
		}
		return String(toFixedString(f, digits)), nil
	})
	i.defineMethod(proto, "toPrecision", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, _ := num(this)
		if IsUndefined(arg(args, 0)) {
			return String(NumberToString(f)), nil
		}
		p, _ := i.argInt(ctx, args, 0)
		if math.IsNaN(f) {
			return String("NaN"), nil
		}
		if math.IsInf(f, 0) {
			return String(NumberToString(f)), nil
		}
		if p < 1 || p > 100 {
			return nil, i.throwError(ctx, "RangeError", "toPrecision() argument must be between 1 and 100")
		}
		return String(toPrecisionString(f, p)), nil
	})

	ctor := i.newNativeCtor("Number", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if len(args) == 0 {
			return Number(0), nil
		}
		f, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(f), nil
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f := 0.0
		if len(args) > 0 {
			var err error
			f, err = i.argNum(ctx, args, 0)
			if err != nil {
				return nil, err
			}
		}
		o := NewObject(i.numberProto)
		o.class = "Number"
		o.primitive = Number(f)
		return o, nil
	})
	linkCtor(ctor, proto)

	ctor.SetHidden("MAX_SAFE_INTEGER", Number(9007199254740991))
	ctor.SetHidden("MIN_SAFE_INTEGER", Number(-9007199254740991))
	ctor.SetHidden("MAX_VALUE", Number(math.MaxFloat64))
	ctor.SetHidden("MIN_VALUE", Number(math.SmallestNonzeroFloat64))
	ctor.SetHidden("EPSILON", Number(2.220446049250313e-16))
	ctor.SetHidden("POSITIVE_INFINITY", Number(math.Inf(1)))
	ctor.SetHidden("NEGATIVE_INFINITY", Number(math.Inf(-1)))
	ctor.SetHidden("NaN", Number(math.NaN()))

	i.defineMethod(ctor, "isInteger", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		n, ok := arg(args, 0).(Number)
		return Bool(ok && !math.IsInf(float64(n), 0) && float64(n) == math.Trunc(float64(n))), nil
	})
	i.defineMethod(ctor, "isFinite", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		n, ok := arg(args, 0).(Number)
		return Bool(ok && !math.IsInf(float64(n), 0) && !math.IsNaN(float64(n))), nil
	})
	i.defineMethod(ctor, "isNaN", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		n, ok := arg(args, 0).(Number)
		return Bool(ok && math.IsNaN(float64(n))), nil
	})
	i.defineMethod(ctor, "isSafeInteger", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		n, ok := arg(args, 0).(Number)
		f := float64(n)
		return Bool(ok && f == math.Trunc(f) && math.Abs(f) <= 9007199254740991), nil
	})
	i.defineMethod(ctor, "parseFloat", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(parseFloatImpl(s)), nil
	})
	i.defineMethod(ctor, "parseInt", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		radix, _ := i.argInt(ctx, args, 1)
		return Number(parseIntImpl(s, radix)), nil
	})

	i.setGlobalHidden("Number", ctor)
}

// toPrecisionString formats f with exactly p significant digits, choosing fixed
// or exponential notation per Number.prototype.toPrecision (§21.1.3.5): if the
// decimal exponent e satisfies -6 <= e < p, use fixed notation; otherwise
// exponential. Trailing zeros are preserved.
func toPrecisionString(f float64, p int) string {
	if f == 0 {
		if p == 1 {
			return "0"
		}
		return "0." + strings.Repeat("0", p-1)
	}
	// Determine the decimal exponent e of the leading significant digit.
	e := int(math.Floor(math.Log10(math.Abs(f))))
	if e < -6 || e >= p {
		// Exponential notation with p-1 fractional digits.
		return normalizeExponent(strconv.FormatFloat(f, 'e', p-1, 64))
	}
	// Fixed notation with (p - 1 - e) fractional digits.
	frac := p - 1 - e
	if frac < 0 {
		frac = 0
	}
	return strconv.FormatFloat(f, 'f', frac, 64)
}

// toFixedString formats f with exactly `digits` fractional digits, rounding
// half toward +Infinity as Number.prototype.toFixed specifies ("if there are
// two such values pick the larger n"), which differs from Go's round-half-to-
// even. So (2.5).toFixed(0) === "3" and (-2.5).toFixed(0) === "-2".
func toFixedString(f float64, digits int) string {
	scale := math.Pow(10, float64(digits))
	rounded := math.Floor(f*scale+0.5) / scale
	return strconv.FormatFloat(rounded, 'f', digits, 64)
}

// ---------------------------------------------------------------------------
// Boolean
// ---------------------------------------------------------------------------

func (i *Interpreter) initBoolean() {
	proto := i.booleanProto
	// Boolean.prototype is itself a Boolean wrapper with [[BooleanData]] false.
	proto.class = "Boolean"
	proto.primitive = Boolean(false)

	boolOf := func(this Value) (bool, bool) {
		switch x := this.(type) {
		case Boolean:
			return bool(x), true
		case *Object:
			if b, ok := x.primitive.(Boolean); ok {
				return bool(b), true
			}
		}
		return false, false
	}

	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		b, ok := boolOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Boolean.prototype.toString called on non-boolean")
		}
		if b {
			return String("true"), nil
		}
		return String("false"), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		b, ok := boolOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Boolean.prototype.valueOf called on non-boolean")
		}
		return Bool(b), nil
	})

	ctor := i.newNativeCtor("Boolean", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Bool(ToBoolean(arg(args, 0))), nil
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o := NewObject(i.booleanProto)
		o.class = "Boolean"
		o.primitive = Bool(ToBoolean(arg(args, 0)))
		return o, nil
	})
	linkCtor(ctor, proto)
	i.setGlobalHidden("Boolean", ctor)
}
