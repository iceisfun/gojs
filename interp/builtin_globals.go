package interp

import (
	"context"
	"math"
	"strconv"
	"strings"
)

// initGlobals installs the global value bindings and free functions
// (globalThis, NaN, Infinity, parseInt/parseFloat, isNaN/isFinite, eval, and
// the number/string encoding helpers).
func (i *Interpreter) initGlobals() {
	i.global.defineOwn(StrKey("globalThis"), &Property{Value: i.global, Writable: true, Enumerable: false, Configurable: true})
	i.global.defineOwn(StrKey("undefined"), &Property{Value: Undef, Writable: false, Enumerable: false, Configurable: false})
	i.global.defineOwn(StrKey("NaN"), &Property{Value: Number(math.NaN()), Writable: false, Enumerable: false, Configurable: false})
	i.global.defineOwn(StrKey("Infinity"), &Property{Value: Number(math.Inf(1)), Writable: false, Enumerable: false, Configurable: false})

	// parseInt and parseFloat are shared: Number.parseInt === parseInt and
	// Number.parseFloat === parseFloat are the very same function objects.
	parseIntFn := i.newNativeFunc("parseInt", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		radix := 0
		if !IsUndefined(arg(args, 1)) {
			radix, _ = i.argInt(ctx, args, 1)
		}
		return Number(parseIntImpl(s, radix)), nil
	})
	parseFloatFn := i.newNativeFunc("parseFloat", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(parseFloatImpl(s)), nil
	})
	i.setGlobalHidden("parseInt", parseIntFn)
	i.setGlobalHidden("parseFloat", parseFloatFn)
	if numCtor, ok := i.GetGlobal("Number").(*Object); ok {
		numCtor.SetHidden("parseInt", parseIntFn)
		numCtor.SetHidden("parseFloat", parseFloatFn)
	}
	i.setGlobalFunc("isNaN", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(math.IsNaN(f)), nil
	})
	i.setGlobalFunc("isFinite", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		f, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(!math.IsNaN(f) && !math.IsInf(f, 0)), nil
	})

	// eval is not implemented; it either throws (when hardened) or reports the
	// missing capability. Dynamic code evaluation is intentionally unsupported.
	i.setGlobalFunc("eval", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.evalSource(ctx, arg(args, 0))
	})

	// URI helpers (thin wrappers over Go's URL escaping semantics).
	i.setGlobalFunc("encodeURIComponent", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return String(encodeURI(s, uriComponentUnreserved)), nil
	})
	i.setGlobalFunc("encodeURI", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return String(encodeURI(s, uriUnreserved)), nil
	})
}

// setGlobalFunc defines a non-enumerable global function.
func (i *Interpreter) setGlobalFunc(name string, length int, fn CallFn) {
	i.setGlobalHidden(name, i.newNativeFunc(name, length, fn))
}

// parseIntImpl implements the parseInt algorithm (§19.2.5).
func parseIntImpl(s string, radix int) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return math.NaN()
	}
	sign := 1.0
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	if radix == 0 {
		if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
			radix = 16
			s = s[2:]
		} else {
			radix = 10
		}
	} else if radix == 16 {
		if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
			s = s[2:]
		}
	}
	if radix < 2 || radix > 36 {
		return math.NaN()
	}
	var result float64
	consumed := 0
	for _, c := range s {
		d := digitValue(c)
		if d < 0 || d >= radix {
			break
		}
		result = result*float64(radix) + float64(d)
		consumed++
	}
	if consumed == 0 {
		return math.NaN()
	}
	return sign * result
}

// parseFloatImpl implements the parseFloat algorithm, reading the longest valid
// numeric prefix.
func parseFloatImpl(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "Infinity") || strings.HasPrefix(s, "+Infinity") {
		return math.Inf(1)
	}
	if strings.HasPrefix(s, "-Infinity") {
		return math.Inf(-1)
	}
	// Find the longest parseable prefix.
	end := 0
	for end < len(s) {
		if _, err := strconv.ParseFloat(s[:end+1], 64); err != nil {
			// Allow trailing exponent/decimal build-up.
			if end+1 < len(s) && (s[end] == 'e' || s[end] == 'E' || s[end] == '.' || s[end] == '+' || s[end] == '-') {
				end++
				continue
			}
			break
		}
		end++
	}
	if end == 0 {
		return math.NaN()
	}
	// Trim any invalid trailing build-up back to a parseable value.
	for end > 0 {
		if f, err := strconv.ParseFloat(s[:end], 64); err == nil {
			return f
		}
		end--
	}
	return math.NaN()
}

// digitValue returns the value of a base-36 digit, or -1.
func digitValue(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

// URI escaping helpers.
const (
	uriUnreserved          = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'();/?:@&=+$,#"
	uriComponentUnreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
)

func encodeURI(s, unreserved string) string {
	var b strings.Builder
	for _, by := range []byte(s) {
		if strings.IndexByte(unreserved, by) >= 0 {
			b.WriteByte(by)
		} else {
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[by>>4])
			b.WriteByte(hex[by&0xF])
		}
	}
	return b.String()
}
