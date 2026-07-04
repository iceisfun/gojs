package interp

import (
	"context"
	"math"
	"math/big"
	"math/bits"
)

// initMath installs the Math namespace object with its constants and functions.
func (i *Interpreter) initMath() {
	m := NewObject(i.objectProto)
	m.class = "Math"

	// The Math constants have the attributes { [[Writable]]: false,
	// [[Enumerable]]: false, [[Configurable]]: false } (§21.3.1).
	constant := func(name string, v float64) {
		m.defineOwn(StrKey(name), &Property{Value: Number(v), Writable: false, Enumerable: false, Configurable: false})
	}
	constant("PI", math.Pi)
	constant("E", math.E)
	constant("LN2", math.Ln2)
	constant("LN10", math.Log(10))
	constant("LOG2E", 1/math.Ln2)
	constant("LOG10E", 1/math.Log(10))
	constant("SQRT2", math.Sqrt2)
	constant("SQRT1_2", math.Sqrt(0.5))

	// Math[Symbol.toStringTag] = "Math" with attributes { [[Writable]]: false,
	// [[Enumerable]]: false, [[Configurable]]: true } (§21.3.1.9).
	m.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("Math"), Writable: false, Enumerable: false, Configurable: true})

	// Unary functions of one argument that delegate directly to a Go math
	// function whose special-value behaviour already matches the spec.
	unary := map[string]func(float64) float64{
		"abs":    math.Abs,
		"ceil":   math.Ceil,
		"floor":  math.Floor,
		"round":  jsRound,
		"trunc":  math.Trunc,
		"sign":   jsSign,
		"sqrt":   math.Sqrt,
		"cbrt":   math.Cbrt,
		"exp":    math.Exp,
		"expm1":  math.Expm1,
		"log":    math.Log,
		"log2":   math.Log2,
		"log10":  math.Log10,
		"log1p":  math.Log1p,
		"sin":    math.Sin,
		"cos":    math.Cos,
		"tan":    math.Tan,
		"asin":   math.Asin,
		"acos":   math.Acos,
		"atan":   math.Atan,
		"sinh":   math.Sinh,
		"cosh":   math.Cosh,
		"tanh":   math.Tanh,
		"asinh":  math.Asinh,
		"acosh":  math.Acosh,
		"atanh":  math.Atanh,
		"fround": func(f float64) float64 { return float64(float32(f)) },
		// clz32: leading zero bits of ToUint32(x); bits.LeadingZeros32(0) == 32.
		"clz32": func(f float64) float64 {
			return float64(bits.LeadingZeros32(ToUint32(f)))
		},
		"f16round": jsF16Round,
	}
	for name, fn := range unary {
		fn := fn
		i.defineMethod(m, name, 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
			x, err := i.argNum(ctx, args, 0)
			if err != nil {
				return nil, err
			}
			return Number(fn(x)), nil
		})
	}

	i.defineMethod(m, "pow", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		base, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		exp, err := i.argNum(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		return Number(jsPow(base, exp)), nil
	})
	i.defineMethod(m, "imul", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		a, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		b, err := i.argNum(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		// ToUint32 both operands, multiply modulo 2^32, reinterpret as int32.
		product := ToUint32(a) * ToUint32(b)
		return Number(float64(int32(product))), nil
	})
	i.defineMethod(m, "atan2", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		y, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		x, err := i.argNum(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		return Number(math.Atan2(y, x)), nil
	})
	i.defineMethod(m, "hypot", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// §21.3.2.18: coerce every argument first, then let ±Infinity take
		// precedence over NaN, and finally sum the squares.
		coerced := make([]float64, len(args))
		for idx, a := range args {
			f, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			coerced[idx] = f
		}
		for _, f := range coerced {
			if math.IsInf(f, 0) {
				return Number(math.Inf(1)), nil
			}
		}
		for _, f := range coerced {
			if math.IsNaN(f) {
				return Number(math.NaN()), nil
			}
		}
		sum := 0.0
		for _, f := range coerced {
			sum += f * f
		}
		return Number(math.Sqrt(sum)), nil
	})
	i.defineMethod(m, "max", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// §21.3.2.24: ToNumber every argument before comparing, so side
		// effects of coercion always run. +0 is considered larger than -0.
		coerced := make([]float64, len(args))
		for idx, a := range args {
			f, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			coerced[idx] = f
		}
		highest := math.Inf(-1)
		for _, f := range coerced {
			if math.IsNaN(f) {
				return Number(math.NaN()), nil
			}
			if f == 0 && !math.Signbit(f) && highest == 0 && math.Signbit(highest) {
				highest = 0 // +0 beats a current -0
			}
			if f > highest {
				highest = f
			}
		}
		return Number(highest), nil
	})
	i.defineMethod(m, "min", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// §21.3.2.25: ToNumber every argument before comparing. -0 is
		// considered smaller than +0.
		coerced := make([]float64, len(args))
		for idx, a := range args {
			f, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			coerced[idx] = f
		}
		lowest := math.Inf(1)
		for _, f := range coerced {
			if math.IsNaN(f) {
				return Number(math.NaN()), nil
			}
			if f == 0 && math.Signbit(f) && lowest == 0 && !math.Signbit(lowest) {
				lowest = math.Copysign(0, -1) // -0 beats a current +0
			}
			if f < lowest {
				lowest = f
			}
		}
		return Number(lowest), nil
	})
	// Math.sumPrecise (sec-math.sumprecise) returns the correctly-rounded
	// (round-to-nearest, ties-to-even) sum of an iterable of Numbers, as if the
	// addition were carried out with unbounded precision and rounded once to a
	// binary64 at the end. Only Numbers are accepted — a non-Number element
	// throws a TypeError with no coercion, and the iterator is closed.
	i.defineMethod(m, "sumPrecise", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		var items Value = Undef
		if len(args) > 0 {
			items = args[0]
		}
		rec, err := i.getIterator(ctx, items)
		if err != nil {
			return nil, err
		}

		// Exact accumulation of the finite summands as a rational: float64 →
		// big.Rat is exact, so the running total is exact and a single final
		// rounding yields the correctly-rounded binary64 result.
		sum := new(big.Rat)
		term := new(big.Rat)
		sawNaN := false
		hasPosInf := false
		hasNegInf := false
		// allNegZero stays true only while every finite element seen is -0 (or
		// there are none); that is the sole case whose exact-zero sum is -0.
		allNegZero := true

		for {
			v, done, sErr := i.iteratorStepValue(ctx, rec)
			if sErr != nil {
				return nil, sErr
			}
			if done {
				break
			}
			num, ok := v.(Number)
			if !ok {
				terr := i.throwError(ctx, "TypeError", "Math.sumPrecise: expected a Number")
				return nil, i.iteratorClose(ctx, rec, terr)
			}
			f := float64(num)
			switch {
			case math.IsNaN(f):
				sawNaN = true
			case math.IsInf(f, 1):
				hasPosInf = true
			case math.IsInf(f, -1):
				hasNegInf = true
			default:
				if !(f == 0 && math.Signbit(f)) {
					allNegZero = false
				}
				if f != 0 {
					sum.Add(sum, term.SetFloat64(f))
				}
			}
		}

		switch {
		case sawNaN:
			return Number(math.NaN()), nil
		case hasPosInf && hasNegInf:
			return Number(math.NaN()), nil
		case hasPosInf:
			return Number(math.Inf(1)), nil
		case hasNegInf:
			return Number(math.Inf(-1)), nil
		}
		if sum.Sign() == 0 {
			if allNegZero {
				return Number(math.Copysign(0, -1)), nil
			}
			return Number(0), nil
		}
		f, _ := sum.Float64()
		return Number(f), nil
	})

	// Math.random uses a per-interpreter deterministic PRNG so that one
	// interpreter's sequence does not depend on any other's.
	i.defineMethod(m, "random", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Number(i.rng.next()), nil
	})

	i.setGlobalHidden("Math", m)
}

// jsRound implements Math.round (§21.3.2.28): round half toward +Infinity,
// preserving -0 for inputs in [-0.5, -0].
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == math.Trunc(x) {
		return x
	}
	if x > 0 && x < 0.5 {
		return 0
	}
	if x < 0 && x >= -0.5 {
		return math.Copysign(0, -1)
	}
	return math.Floor(x + 0.5)
}

// jsSign implements Math.sign.
func jsSign(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return math.NaN()
	case x > 0:
		return 1
	case x < 0:
		return -1
	default:
		return x // preserves +0/-0
	}
}

// jsPow implements Number::exponentiate (§6.1.6.1.3), which differs from IEEE
// 754 (and from Go's math.Pow) in that an ±Infinity exponent with a base of
// magnitude 1, and any NaN exponent, both yield NaN.
func jsPow(base, exp float64) float64 {
	if math.IsNaN(exp) {
		return math.NaN()
	}
	if math.IsInf(exp, 0) && math.Abs(base) == 1 {
		return math.NaN()
	}
	return math.Pow(base, exp)
}

// jsF16Round implements Math.f16round (§21.3.2.16): round x to IEEE 754-2019
// binary16 (roundTiesToEven) and return the corresponding binary64 value. The
// conversion is a single binary64→binary16 rounding (see float64ToFloat16), so
// it is free of the double-rounding hazard the spec warns about.
func jsF16Round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == 0 {
		return x
	}
	return float16ToFloat64(float64ToFloat16(x))
}
