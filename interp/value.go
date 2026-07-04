// Package interp implements a tree-walking interpreter for the JavaScript AST
// produced by the parser. It is the runtime core of gojs — the analogue of a
// bytecode VM, executing [ast] nodes directly.
//
// # Values
//
// JavaScript values are modeled by the [Value] interface. The primitive kinds
// (undefined, null, boolean, number, string, bigint, symbol) are small
// immutable Go types; objects (including arrays and functions) are represented
// by [*Object].
//
// # Execution and cancellation
//
// All evaluation threads a [context.Context] so that a host embedding gojs can
// cancel a running script and shut the interpreter down cleanly, including any
// goroutines started for timers (setTimeout/setInterval). See [Interpreter] and
// its Close method.
//
// # Capabilities
//
// Access to host facilities (console output, wall-clock time, timers) is gated
// behind provider interfaces, mirroring the golua design. Without a provider,
// the corresponding globals are absent or inert, keeping the default sandbox
// closed.
//
// ECMA-262 Reference: §6 (values), §7 (abstract operations).
package interp

import (
	"math"
	"strconv"
	"strings"
)

// Value is the interface implemented by every JavaScript runtime value.
type Value interface {
	// Typeof returns the string that the JavaScript `typeof` operator yields
	// for this value ("undefined", "boolean", "number", "string", "bigint",
	// "symbol", "object", or "function").
	Typeof() string
}

// ---------------------------------------------------------------------------
// Primitive value types
// ---------------------------------------------------------------------------

// Undefined is the type of the single `undefined` value.
type Undefined struct{}

// Null is the type of the single `null` value.
type Null struct{}

// Boolean is a JavaScript boolean.
type Boolean bool

// Number is a JavaScript number (IEEE-754 double).
type Number float64

// String is a JavaScript string. gojs stores strings as Go UTF-8 for
// simplicity; APIs that are sensitive to UTF-16 code units approximate over
// runes. (A future revision may switch to a UTF-16 representation.)
type String string

func (Undefined) Typeof() string { return "undefined" }
func (Null) Typeof() string      { return "object" } // historical JS quirk
func (Boolean) Typeof() string   { return "boolean" }
func (Number) Typeof() string    { return "number" }
func (String) Typeof() string    { return "string" }

// Singleton primitive values. Using package-level values avoids allocating a
// fresh boxed value for every undefined/null result.
var (
	Undef Value = Undefined{}
	Nul   Value = Null{}
	True  Value = Boolean(true)
	False Value = Boolean(false)
)

// Bool returns the interned Boolean value for b.
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// ---------------------------------------------------------------------------
// Type predicates
// ---------------------------------------------------------------------------

// IsUndefined reports whether v is undefined.
func IsUndefined(v Value) bool { _, ok := v.(Undefined); return ok }

// IsNull reports whether v is null.
func IsNull(v Value) bool { _, ok := v.(Null); return ok }

// IsNullish reports whether v is null or undefined.
func IsNullish(v Value) bool { return IsUndefined(v) || IsNull(v) }

// ---------------------------------------------------------------------------
// Abstract operations (ECMA-262 §7.1)
// ---------------------------------------------------------------------------

// ToBoolean implements the abstract ToBoolean conversion (§7.1.2).
func ToBoolean(v Value) bool {
	switch x := v.(type) {
	case Undefined, Null:
		return false
	case Boolean:
		return bool(x)
	case Number:
		f := float64(x)
		return f != 0 && !math.IsNaN(f)
	case String:
		return len(x) > 0
	case *strRope:
		return x.length > 0
	case *Symbol:
		return true
	case *BigInt:
		return x.sign() != 0
	case *Object:
		return true
	default:
		return v != nil
	}
}

// ToNumber implements the abstract ToNumber conversion (§7.1.4) for primitives.
// Object operands must first be reduced with ToPrimitive by the caller (the
// interpreter does this at operator sites, where a ctx is available).
func ToNumber(v Value) float64 {
	switch x := v.(type) {
	case Undefined:
		return math.NaN()
	case Null:
		return 0
	case Boolean:
		if x {
			return 1
		}
		return 0
	case Number:
		return float64(x)
	case String:
		return stringToNumber(string(x))
	case *strRope:
		return stringToNumber(x.build())
	default:
		return math.NaN()
	}
}

// stringToNumber converts a string to a number following the StringNumeric
// literal grammar (§7.1.4.1), including hex/octal/binary prefixes, Infinity,
// and surrounding whitespace. An empty/whitespace-only string is 0; an
// unparsable string is NaN.
func stringToNumber(s string) float64 {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0
	}
	// Numeric separators ('_') are valid only in source NumericLiterals, not in
	// the StringNumericLiteral grammar used by ToNumber(String). Go's strconv
	// accepts them, so reject any input containing '_' up front.
	if strings.IndexByte(t, '_') >= 0 {
		return math.NaN()
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(t) > 2 && t[0] == '0' {
		switch t[1] {
		case 'x', 'X':
			if v, err := strconv.ParseUint(t[2:], 16, 64); err == nil {
				return float64(v)
			}
			return math.NaN()
		case 'o', 'O':
			if v, err := strconv.ParseUint(t[2:], 8, 64); err == nil {
				return float64(v)
			}
			return math.NaN()
		case 'b', 'B':
			if v, err := strconv.ParseUint(t[2:], 2, 64); err == nil {
				return float64(v)
			}
			return math.NaN()
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		// An out-of-range magnitude ("1e400", "1e-400") is not an error in the
		// StringNumericLiteral grammar: it rounds to ±Infinity or ±0.
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	// Go's ParseFloat also accepts "inf", "infinity", and "nan" (case-
	// insensitively), which the StringNumericLiteral grammar does not: the only
	// non-finite spellings are the exact "Infinity" forms handled above.
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return math.NaN()
	}
	return f
}

// ToInteger truncates a number toward zero, mapping NaN to 0 (§7.1.5).
func ToInteger(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	if math.IsInf(f, 0) || f == 0 {
		return f
	}
	return math.Trunc(f)
}

// ToInt32 implements the ToInt32 abstraction (§7.1.6) used by bitwise operators.
func ToInt32(f float64) int32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int32(uint32(int64(math.Trunc(f))))
}

// ToUint32 implements the ToUint32 abstraction (§7.1.7).
func ToUint32(f float64) uint32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return uint32(int64(math.Trunc(f)))
}

// NumberToString implements Number::toString in base 10 (§7.1.12.1), producing
// the shortest round-trippable representation and JavaScript's spellings for
// special values.
func NumberToString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0" // collapses -0 to "0" per spec
	}
	// Integers print without a decimal point or exponent when in range.
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return esNumberToString(f)
}

// esNumberToString formats a finite, non-zero, non-integer-in-range Number per
// the fixed/exponential boundary of Number::toString (§6.1.6.1.20). Go's %g
// switches to exponent notation too early (below 1e-4), whereas ECMAScript uses
// fixed notation down to 1e-6; this routine follows the spec's k/n digit rules
// on the shortest round-trippable digit string.
func esNumberToString(f float64) string {
	sign := ""
	if f < 0 {
		sign = "-"
		f = -f
	}
	// Shortest "d.ddde±XX" form gives the significant digits and the decimal
	// exponent of the leading digit.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mantissa, expStr, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expStr)
	digits := strings.Replace(mantissa, ".", "", 1)
	k := len(digits) // number of significant digits
	n := exp + 1     // 10^(n-1) <= f < 10^n

	switch {
	case n >= k && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	// Exponential form: one digit before the point, exponent n-1.
	expPart := n - 1
	expSign := "+"
	if expPart < 0 {
		expSign = "-"
		expPart = -expPart
	}
	if k == 1 {
		return sign + digits + "e" + expSign + strconv.Itoa(expPart)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + expSign + strconv.Itoa(expPart)
}

// normalizeExponent rewrites a Go-formatted exponent to JavaScript's spelling:
// no leading zeros in the exponent digits and an explicit sign (Go emits
// "1.2e+02"; JS wants "1.2e+2"). A value with no exponent is returned unchanged.
func normalizeExponent(s string) string {
	e := strings.IndexAny(s, "eE")
	if e < 0 {
		return s
	}
	mantissa := s[:e]
	exp := s[e+1:]
	sign := "+"
	if len(exp) > 0 && (exp[0] == '+' || exp[0] == '-') {
		if exp[0] == '-' {
			sign = "-"
		}
		exp = exp[1:]
	}
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	return mantissa + "e" + sign + exp
}
