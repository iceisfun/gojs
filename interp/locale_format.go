package interp

import (
	"context"
	"math/big"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Locale-sensitive number formatting for Number.prototype.toLocaleString and
// BigInt.prototype.toLocaleString. gojs ships no ECMA-402 (Intl) object, but its
// existing golang.org/x/text dependency carries the CLDR locale database, so the
// language leaves this method implementation-defined (ECMA-262 §21.1.3.4: "it is
// permissible ... to return the same thing as toString") and we choose to honor
// the caller's locale rather than fall back to a bare toString. This covers the
// `decimal` style only: grouping, min/max fraction digits, and minimum integer
// digits, with the requested locale's separators. Currency/percent/unit styles,
// compact notation, and signDisplay would need a real Intl.NumberFormat and are
// not attempted here.

// formatLocaleNumber renders a Number for Number.prototype.toLocaleString using
// the locale from the `locales` argument and the `options` object (both may be
// undefined). NaN and ±Infinity flow through x/text, which renders them as "NaN"
// and "∞"/"-∞" — matching a full-ICU host.
func (i *Interpreter) formatLocaleNumber(ctx context.Context, f float64, locales, options Value) (string, error) {
	p, err := i.resolveNumberLocale(ctx, locales)
	if err != nil {
		return "", err
	}
	opts, _, err := i.resolveNumberFormatOptions(ctx, options, false)
	if err != nil {
		return "", err
	}
	return p.Sprint(number.Decimal(f, opts...)), nil
}

// formatLocaleBigInt renders a BigInt for BigInt.prototype.toLocaleString. Values
// within int64 range go through x/text (so exotic grouping such as the Indian
// lakh/crore system is honored); larger magnitudes — which x/text's number
// package cannot ingest as a *big.Int — are grouped in threes using the locale's
// detected group separator.
func (i *Interpreter) formatLocaleBigInt(ctx context.Context, v *big.Int, locales, options Value) (string, error) {
	p, err := i.resolveNumberLocale(ctx, locales)
	if err != nil {
		return "", err
	}
	opts, grouping, err := i.resolveNumberFormatOptions(ctx, options, true)
	if err != nil {
		return "", err
	}
	if v.IsInt64() {
		return p.Sprint(number.Decimal(v.Int64(), opts...)), nil
	}
	digits := v.Text(10)
	neg := strings.HasPrefix(digits, "-")
	if neg {
		digits = digits[1:]
	}
	if grouping {
		digits = groupDigits(digits, localeGroupSeparator(p))
	}
	if neg {
		digits = "-" + digits
	}
	return digits, nil
}

// resolveNumberLocale turns a toLocaleString `locales` argument into an x/text
// printer. undefined selects the default (English); a string, or the first
// element of an array-like, is parsed as a BCP-47 tag. A structurally malformed
// tag throws a RangeError, mirroring ECMA-402 CanonicalizeLocaleList; passing
// null throws a TypeError via ToObject, likewise matching the spec.
func (i *Interpreter) resolveNumberLocale(ctx context.Context, locales Value) (*message.Printer, error) {
	tag, ok, err := i.firstLocaleTag(ctx, locales)
	if err != nil {
		return nil, err
	}
	if !ok {
		return message.NewPrinter(language.English), nil
	}
	t, perr := language.Parse(tag)
	if perr != nil {
		return nil, i.throwError(ctx, "RangeError", "Incorrect locale information provided")
	}
	return message.NewPrinter(t), nil
}

// firstLocaleTag extracts the first candidate BCP-47 tag from a `locales`
// argument: (nil,false) for undefined (use the default), (tag,true) for a string
// or an array-like's first element.
func (i *Interpreter) firstLocaleTag(ctx context.Context, locales Value) (string, bool, error) {
	if IsUndefined(locales) {
		return "", false, nil
	}
	if s, ok := asString(locales); ok {
		return s, true, nil
	}
	// Array-like (or any other object): ToObject then read element 0. null and
	// undefined already handled / rejected by ToObject.
	obj, err := i.ToObject(ctx, locales)
	if err != nil {
		return "", false, err
	}
	lv, err := i.getProperty(ctx, obj, StrKey("length"))
	if err != nil {
		return "", false, err
	}
	ln, err := i.ToNumberV(ctx, lv)
	if err != nil {
		return "", false, err
	}
	if integerOrInfinity(ln) < 1 {
		return "", false, nil
	}
	el, err := i.getProperty(ctx, obj, StrKey("0"))
	if err != nil {
		return "", false, err
	}
	s, err := i.ToStringV(ctx, el)
	if err != nil {
		return "", false, err
	}
	return s, true, nil
}

// resolveNumberFormatOptions reads the subset of Intl.NumberFormat options we
// support from a toLocaleString `options` object and returns the x/text options
// plus whether grouping is enabled. Defaults follow ECMA-402 for the decimal
// style: minimumFractionDigits 0, maximumFractionDigits max(min,3), grouping on.
// isBigInt suppresses the fraction-digit options (BigInts are integers).
func (i *Interpreter) resolveNumberFormatOptions(ctx context.Context, optionsArg Value, isBigInt bool) ([]number.Option, bool, error) {
	grouping := true
	minFrac := 0
	maxFrac := 3
	minInt := 1

	if !IsUndefined(optionsArg) {
		obj, err := i.ToObject(ctx, optionsArg)
		if err != nil {
			return nil, false, err
		}
		// useGrouping: honor the common {useGrouping:false}. ECMA-402 also accepts
		// the strings "auto"/"always"/"min2" (all keep grouping on here) and true.
		ug, err := i.getProperty(ctx, obj, StrKey("useGrouping"))
		if err != nil {
			return nil, false, err
		}
		if b, ok := ug.(Boolean); ok && !bool(b) {
			grouping = false
		}
		if v, set, err := i.intOption(ctx, obj, "minimumIntegerDigits"); err != nil {
			return nil, false, err
		} else if set {
			minInt = clampInt(v, 1, 21)
		}
		if !isBigInt {
			mnfSet, mxfSet := false, false
			if v, set, err := i.intOption(ctx, obj, "minimumFractionDigits"); err != nil {
				return nil, false, err
			} else if set {
				minFrac = clampInt(v, 0, 100)
				mnfSet = true
			}
			if v, set, err := i.intOption(ctx, obj, "maximumFractionDigits"); err != nil {
				return nil, false, err
			} else if set {
				maxFrac = clampInt(v, 0, 100)
				mxfSet = true
			}
			if !mnfSet {
				minFrac = 0
			}
			if !mxfSet {
				maxFrac = minFrac
				if maxFrac < 3 {
					maxFrac = 3
				}
			}
			if maxFrac < minFrac {
				maxFrac = minFrac
			}
		}
	}

	opts := make([]number.Option, 0, 4)
	if minInt > 1 {
		opts = append(opts, number.MinIntegerDigits(minInt))
	}
	if !isBigInt {
		opts = append(opts, number.MinFractionDigits(minFrac), number.MaxFractionDigits(maxFrac))
	}
	if !grouping {
		opts = append(opts, number.NoSeparator())
	}
	return opts, grouping, nil
}

// intOption reads an integer-valued option: (0,false,nil) when absent/undefined,
// otherwise the ToIntegerOrInfinity-truncated value. ToNumber may throw (e.g. a
// Symbol or a BigInt option), which propagates.
func (i *Interpreter) intOption(ctx context.Context, obj *Object, name string) (int, bool, error) {
	v, err := i.getProperty(ctx, obj, StrKey(name))
	if err != nil {
		return 0, false, err
	}
	if IsUndefined(v) {
		return 0, false, nil
	}
	f, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, false, err
	}
	return int(integerOrInfinity(f)), true, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// localeGroupSeparator recovers a locale's thousands separator by formatting a
// 7-digit probe (large enough that every locale groups it) and returning the
// first non-ASCII-digit rune. Used only for out-of-int64 BigInts; locales that
// render non-ASCII digit glyphs are not handled here (their in-range values
// still format correctly through x/text).
func localeGroupSeparator(p *message.Printer) string {
	s := p.Sprint(number.Decimal(int64(1111111)))
	for _, r := range s {
		if r < '0' || r > '9' {
			return string(r)
		}
	}
	return ""
}

// groupDigits inserts sep every three digits from the right of an unsigned
// decimal digit string.
func groupDigits(s, sep string) string {
	if sep == "" || len(s) <= 3 {
		return s
	}
	n := len(s)
	first := n % 3
	var b strings.Builder
	if first > 0 {
		b.WriteString(s[:first])
	}
	for j := first; j < n; j += 3 {
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s[j : j+3])
	}
	return b.String()
}
