package interp

import (
	"context"
	"math"
)

// initMath installs the Math namespace object with its constants and functions.
func (i *Interpreter) initMath() {
	m := NewObject(i.objectProto)
	m.class = "Math"

	m.SetHidden("PI", Number(math.Pi))
	m.SetHidden("E", Number(math.E))
	m.SetHidden("LN2", Number(math.Ln2))
	m.SetHidden("LN10", Number(math.Log(10)))
	m.SetHidden("LOG2E", Number(1/math.Ln2))
	m.SetHidden("LOG10E", Number(1/math.Log(10)))
	m.SetHidden("SQRT2", Number(math.Sqrt2))
	m.SetHidden("SQRT1_2", Number(math.Sqrt(0.5)))

	// Unary functions of one argument.
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
		return Number(math.Pow(base, exp)), nil
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
		sum := 0.0
		for _, a := range args {
			f, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			sum += f * f
		}
		return Number(math.Sqrt(sum)), nil
	})
	i.defineMethod(m, "max", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		res := math.Inf(-1)
		for _, a := range args {
			f, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			if math.IsNaN(f) {
				return Number(math.NaN()), nil
			}
			if f > res {
				res = f
			}
		}
		return Number(res), nil
	})
	i.defineMethod(m, "min", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		res := math.Inf(1)
		for _, a := range args {
			f, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			if math.IsNaN(f) {
				return Number(math.NaN()), nil
			}
			if f < res {
				res = f
			}
		}
		return Number(res), nil
	})
	// Math.random uses a per-interpreter deterministic PRNG so that one
	// interpreter's sequence does not depend on any other's.
	i.defineMethod(m, "random", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Number(i.rng.next()), nil
	})

	i.setGlobalHidden("Math", m)
}

// jsRound implements Math.round (round half up toward +Infinity).
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
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
