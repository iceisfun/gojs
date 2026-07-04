package interp

import (
	"context"
	"math"
	"math/big"
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
		return String(symbolDescriptiveString(sym)), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		sym, ok := thisSymbol(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Symbol.prototype.valueOf called on non-symbol")
		}
		return sym, nil
	})
	// Symbol.prototype.description is an accessor whose getter throws a
	// TypeError on a non-symbol this and reports undefined when the symbol
	// was created without a description (§20.4.3.2).
	proto.DefineAccessor("description",
		i.newNativeFunc("get description", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
			sym, ok := thisSymbol(this)
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "Symbol.prototype.description getter called on non-symbol")
			}
			if !sym.HasDesc {
				return Undef, nil
			}
			return String(sym.Desc), nil
		}), nil, false)

	// Symbol.prototype[Symbol.toPrimitive] returns the underlying symbol,
	// with attributes {[[Writable]]: false, ..., [[Configurable]]: true}.
	toPrim := i.newNativeFunc("[Symbol.toPrimitive]", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		sym, ok := thisSymbol(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Symbol.prototype[Symbol.toPrimitive] called on non-symbol")
		}
		return sym, nil
	})
	proto.defineOwn(SymKey(i.symToPrimitive), &Property{Value: toPrim, Writable: false, Enumerable: false, Configurable: true})

	// Symbol.prototype[Symbol.toStringTag] is the string "Symbol".
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("Symbol"), Writable: false, Enumerable: false, Configurable: true})

	ctor := i.newNativeCtor("Symbol", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if IsUndefined(arg(args, 0)) {
			return &Symbol{}, nil
		}
		desc, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return &Symbol{Desc: desc, HasDesc: true}, nil
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Symbol is not a constructor")
	})
	ctor.fn.ctor = false
	linkCtor(ctor, proto)

	// Well-known symbols are non-writable, non-enumerable, non-configurable
	// data properties on the Symbol constructor (§20.4.2).
	wellKnown := func(name string, s *Symbol) {
		ctor.defineOwn(StrKey(name), &Property{Value: s, Writable: false, Enumerable: false, Configurable: false})
	}
	wellKnown("iterator", i.symIterator)
	wellKnown("asyncIterator", i.symAsyncIterator)
	wellKnown("toPrimitive", i.symToPrimitive)
	wellKnown("toStringTag", i.symToStringTag)
	wellKnown("hasInstance", i.symHasInstance)
	wellKnown("match", i.symMatch)
	wellKnown("matchAll", i.symMatchAll)
	wellKnown("replace", i.symReplace)
	wellKnown("search", i.symSearch)
	wellKnown("split", i.symSplit)
	wellKnown("species", i.symSpecies)
	wellKnown("unscopables", i.symUnscopables)
	wellKnown("isConcatSpreadable", i.symIsConcatSpreadable)

	// The GlobalSymbolRegistry backs Symbol.for/Symbol.keyFor. It maps a
	// string key to its registered symbol and back (§20.4.2.2, §20.4.2.6). The
	// registry is shared by every realm in an agent (a ShadowRealm's inner realm
	// inherits its parent's maps), so Symbol.for observes registrations made in
	// another realm — hence it lives on the Interpreter rather than in a local.
	if i.symByKey == nil {
		i.symByKey = map[string]*Symbol{}
		i.symBySym = map[*Symbol]string{}
	}
	byKey := i.symByKey
	bySym := i.symBySym
	i.defineMethod(ctor, "for", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		key, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		if s, ok := byKey[key]; ok {
			return s, nil
		}
		s := &Symbol{Desc: key, HasDesc: true, Registered: true}
		byKey[key] = s
		bySym[s] = key
		return s, nil
	})
	i.defineMethod(ctor, "keyFor", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		sym, ok := arg(args, 0).(*Symbol)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Symbol.keyFor requires a symbol argument")
		}
		if key, ok := bySym[sym]; ok {
			return String(key), nil
		}
		return Undef, nil
	})

	i.setGlobalHidden("Symbol", ctor)
}

// symbolDescriptiveString renders a symbol as "Symbol(desc)" per SymbolDescriptiveString
// (§20.4.3.3.1): a symbol with no description yields "Symbol()".
func symbolDescriptiveString(sym *Symbol) string {
	if !sym.HasDesc {
		return "Symbol()"
	}
	return "Symbol(" + sym.Desc + ")"
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

// numberValueOf implements the numeric coercion the Number constructor uses
// (§21.1.1.1): ToNumeric, then map a BigInt to the Number nearest its value
// (BigInt is the one input the ordinary ToNumber rejects with a TypeError).
func (i *Interpreter) numberValueOf(ctx context.Context, v Value) (float64, error) {
	prim, err := i.ToPrimitive(ctx, v, "number")
	if err != nil {
		return 0, err
	}
	if b, ok := prim.(*BigInt); ok {
		f, _ := new(big.Float).SetInt(b.Int).Float64()
		return f, nil
	}
	return i.ToNumberV(ctx, prim)
}

func (i *Interpreter) initNumber() {
	proto := i.numberProto
	// Number.prototype is itself a Number wrapper with [[NumberData]] 0, so
	// Number.prototype.valueOf() and .toString() work on it directly.
	proto.class = "Number"
	proto.primitive = Number(0)

	// num extracts thisNumberValue (§21.1.3.1): the [[NumberData]] slot of a
	// Number, either a primitive or a Number wrapper object. A wrapper is
	// identified by its [[Class]] being "Number"; other exotic objects that store
	// a Number primitive (e.g. Date, whose [[DateValue]] is a Number) are not
	// Number objects and must be rejected with a TypeError by the caller.
	num := func(this Value) (float64, bool) {
		switch x := this.(type) {
		case Number:
			return float64(x), true
		case *Object:
			if x.class == "Number" {
				if n, ok := x.primitive.(Number); ok {
					return float64(n), true
				}
			}
		}
		return 0, false
	}

	i.defineMethod(proto, "toString", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.toString called on non-number")
		}
		// Per §21.1.3.6 the radix is converted (ToIntegerOrInfinity) before the
		// range check, and both precede the radix-10 shortcut, so a poisoned
		// radix argument throws even for NaN/Infinity receivers.
		radixF := 10.0
		if !IsUndefined(arg(args, 0)) {
			rf, err := i.argNum(ctx, args, 0)
			if err != nil {
				return nil, err
			}
			radixF = ToInteger(rf)
		}
		if radixF < 2 || radixF > 36 {
			return nil, i.throwError(ctx, "RangeError", "toString() radix must be between 2 and 36")
		}
		radix := int(radixF)
		if radix == 10 {
			return String(NumberToString(f)), nil
		}
		if f == math.Trunc(f) && !math.IsInf(f, 0) &&
			f >= math.MinInt64 && f <= math.MaxInt64 {
			return String(strconv.FormatInt(int64(f), radix)), nil
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return String(NumberToString(f)), nil
		}
		return String(numberToRadixString(f, radix)), nil
	})
	i.defineMethod(proto, "valueOf", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.valueOf called on non-number")
		}
		return Number(f), nil
	})
	i.defineMethod(proto, "toFixed", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.toFixed called on non-number")
		}
		// §21.1.3.3: convert fractionDigits (may throw for a Symbol/BigInt or a
		// poisoned valueOf) before the range check; ±Infinity is out of range.
		fdF, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		fd := ToInteger(fdF)
		if fd < 0 || fd > 100 {
			return nil, i.throwError(ctx, "RangeError", "toFixed() digits argument must be between 0 and 100")
		}
		digits := int(fd)
		if math.IsNaN(f) {
			return String("NaN"), nil
		}
		if math.Abs(f) >= 1e21 {
			return String(NumberToString(f)), nil
		}
		return String(toFixedString(f, digits)), nil
	})
	i.defineMethod(proto, "toPrecision", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.toPrecision called on non-number")
		}
		// §21.1.3.5: if precision is undefined, return ToString(x). Otherwise
		// ToIntegerOrInfinity(precision) is evaluated (and may throw for a
		// Symbol/BigInt or a poisoned valueOf) before the NaN/Infinity handling
		// and the range check.
		if IsUndefined(arg(args, 0)) {
			return String(NumberToString(f)), nil
		}
		pF, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		p := int(ToInteger(pF))
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
	i.defineMethod(proto, "toExponential", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.toExponential called on non-number")
		}
		// §21.1.3.2: ToIntegerOrInfinity(fractionDigits) is evaluated (and may
		// throw) before the non-finite check, so a poisoned argument throws even
		// for a NaN/Infinity receiver.
		fdUndefined := IsUndefined(arg(args, 0))
		fdF, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		fc := ToInteger(fdF)
		if math.IsNaN(f) {
			return String("NaN"), nil
		}
		if math.IsInf(f, 0) {
			if f < 0 {
				return String("-Infinity"), nil
			}
			return String("Infinity"), nil
		}
		if fc < 0 || fc > 100 {
			return nil, i.throwError(ctx, "RangeError", "toExponential() argument must be between 0 and 100")
		}
		return String(toExponentialString(f, int(fc), fdUndefined)), nil
	})
	i.defineMethod(proto, "toLocaleString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// Without an Intl implementation, Number.prototype.toLocaleString returns
		// the same String as Number.prototype.toString with no radix (§19).
		f, ok := num(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Number.prototype.toLocaleString called on non-number")
		}
		return String(NumberToString(f)), nil
	})

	ctor := i.newNativeCtor("Number", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if len(args) == 0 {
			return Number(0), nil
		}
		f, err := i.numberValueOf(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		return Number(f), nil
	}, func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		f := 0.0
		if len(args) > 0 {
			var err error
			f, err = i.numberValueOf(ctx, arg(args, 0))
			if err != nil {
				return nil, err
			}
		}
		p, err := i.protoFromNewTarget(ctx, newTarget, i.numberProto)
		if err != nil {
			return nil, err
		}
		o := NewObject(p)
		o.class = "Number"
		o.primitive = Number(f)
		return o, nil
	})
	linkCtor(ctor, proto)

	// The Number "value properties" (§21.1.2) are all
	// { [[Writable]]: false, [[Enumerable]]: false, [[Configurable]]: false }.
	numConst := func(name string, v float64) {
		ctor.defineOwn(StrKey(name), &Property{Value: Number(v), Writable: false, Enumerable: false, Configurable: false})
	}
	numConst("MAX_SAFE_INTEGER", 9007199254740991)
	numConst("MIN_SAFE_INTEGER", -9007199254740991)
	numConst("MAX_VALUE", math.MaxFloat64)
	numConst("MIN_VALUE", math.SmallestNonzeroFloat64)
	numConst("EPSILON", 2.220446049250313e-16)
	numConst("POSITIVE_INFINITY", math.Inf(1))
	numConst("NEGATIVE_INFINITY", math.Inf(-1))
	numConst("NaN", math.NaN())

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

// toExponentialString formats a finite float64 in decimal exponential notation
// per Number.prototype.toExponential (§21.1.3.2): one digit before the point and
// `frac` digits after it, with the exponent written as e±d (no leading zeros).
// When shortest is true (fractionDigits was undefined) it emits the fewest
// significand digits that round-trip. Callers handle NaN, ±Infinity, and the
// RangeError on frac before reaching here.
func toExponentialString(f float64, frac int, shortest bool) string {
	sign := ""
	// ℝ(-0) is 0, so only a genuinely negative value gets a "-" sign.
	if f < 0 {
		sign = "-"
		f = -f
	}
	if f == 0 {
		if shortest || frac == 0 {
			return sign + "0e+0"
		}
		return sign + "0." + strings.Repeat("0", frac) + "e+0"
	}
	if shortest {
		// The shortest round-tripping significand is unique; Go's 'e' with prec
		// -1 produces exactly it. Only the exponent needs reformatting (e+02 →
		// e+2) to match the spec's no-leading-zero rule.
		return sign + normalizeExponent(strconv.FormatFloat(f, 'e', -1, 64))
	}
	// Exact rounding: work with the value's exact rational form so the spec's
	// round-half-up ("pick the larger intSignificand") is honored, unlike Go's
	// round-half-to-even. So (25).toExponential(0) === "3e+1".
	xr := new(big.Rat).SetFloat64(f)
	// Normalize the decimal exponent so that 10^e <= f < 10^(e+1), starting from
	// a float estimate and correcting any off-by-one from log10 rounding.
	e := int(math.Floor(math.Log10(f)))
	for xr.Cmp(pow10Rat(e)) < 0 {
		e--
	}
	for xr.Cmp(pow10Rat(e+1)) >= 0 {
		e++
	}
	// scaled = f * 10^(frac-e) lies in [10^frac, 10^(frac+1)); round half up.
	scaled := new(big.Rat).Mul(xr, pow10Rat(frac-e))
	scaled.Add(scaled, big.NewRat(1, 2))
	n := new(big.Int).Quo(scaled.Num(), scaled.Denom()) // floor, scaled > 0
	// A carry across the power-of-ten boundary bumps the exponent.
	tenFracP1 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(frac+1)), nil)
	if n.Cmp(tenFracP1) >= 0 {
		n.Div(n, big.NewInt(10))
		e++
	}
	sig := n.String() // exactly frac+1 digits, no leading zero
	if frac != 0 {
		sig = sig[:1] + "." + sig[1:]
	}
	expSign := "+"
	if e < 0 {
		expSign = "-"
		e = -e
	}
	return sign + sig + "e" + expSign + strconv.Itoa(e)
}

// pow10Rat returns 10^k as an exact rational for any (possibly negative) k.
func pow10Rat(k int) *big.Rat {
	n := k
	if n < 0 {
		n = -n
	}
	p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	r := new(big.Rat)
	if k >= 0 {
		r.SetInt(p)
	} else {
		r.SetFrac(big.NewInt(1), p)
	}
	return r
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

// radixDigits maps a digit value (0..35) to its character for Number.toString.
const radixDigits = "0123456789abcdefghijklmnopqrstuvwxyz"

// numberToRadixString renders a finite, non-integral float64 in the given radix
// (2..36, radix != 10), producing both integer and fractional parts in that
// radix. It ports V8's DoubleToRadixCString: fractional digits are emitted only
// up to the input's binary precision (bounded by a ULP-derived delta) with
// round-to-even, and low-order integer digits beyond the double's integer
// precision are filled with zeros. Callers handle integral values, ±Infinity,
// and NaN before reaching here.
func numberToRadixString(f float64, radix int) string {
	r := float64(radix)
	negative := f < 0
	if negative {
		f = -f
	}
	integer := math.Floor(f)
	fraction := f - integer

	// Fractional part. delta is half a ULP of f: the largest error we ignore.
	var frac []byte
	delta := 0.5 * (math.Nextafter(f, math.Inf(1)) - f)
	if smallest := math.Nextafter(0, math.Inf(1)); delta < smallest {
		delta = smallest
	}
	if fraction >= delta {
		frac = append(frac, '.')
	emit:
		for {
			fraction *= r
			delta *= r
			digit := int(fraction)
			frac = append(frac, radixDigits[digit])
			fraction -= float64(digit)
			// Round to even, carrying into earlier digits (and the integer part)
			// when the rounded value would overflow the current position.
			if fraction > 0.5 || (fraction == 0.5 && digit&1 == 1) {
				if fraction+delta > 1 {
					for {
						// frac[0] is the '.'; a carry past it bumps the integer.
						if len(frac) == 1 {
							integer++
							break
						}
						last := frac[len(frac)-1]
						var d int
						if last >= 'a' {
							d = int(last-'a') + 10
						} else {
							d = int(last - '0')
						}
						if d+1 < radix {
							frac[len(frac)-1] = radixDigits[d+1]
							break
						}
						frac = frac[:len(frac)-1]
					}
					break emit
				}
			}
			if fraction < delta {
				break
			}
		}
		// A fraction that rounded entirely away leaves just the '.'; drop it.
		if len(frac) == 1 {
			frac = frac[:0]
		}
	}

	// Integer part, least-significant digit first. For magnitudes beyond the
	// double's integer precision, the low digits are unrepresentable and filled
	// with zeros (matching V8), which we detect via the binary exponent.
	var intDigits []byte
	for {
		if _, e := math.Frexp(integer / r); e <= 53 {
			break
		}
		integer /= r
		intDigits = append(intDigits, '0')
	}
	for {
		remainder := math.Mod(integer, r)
		intDigits = append(intDigits, radixDigits[int(remainder)])
		integer = (integer - remainder) / r
		if integer <= 0 {
			break
		}
	}
	for l, h := 0, len(intDigits)-1; l < h; l, h = l+1, h-1 {
		intDigits[l], intDigits[h] = intDigits[h], intDigits[l]
	}

	var b strings.Builder
	if negative {
		b.WriteByte('-')
	}
	b.Write(intDigits)
	b.Write(frac)
	return b.String()
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
	}, func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		p, err := i.protoFromNewTarget(ctx, newTarget, i.booleanProto)
		if err != nil {
			return nil, err
		}
		o := NewObject(p)
		o.class = "Boolean"
		o.primitive = Bool(ToBoolean(arg(args, 0)))
		return o, nil
	})
	linkCtor(ctor, proto)
	i.setGlobalHidden("Boolean", ctor)
}
