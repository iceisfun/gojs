package interp

import (
	"context"
	"math"
	"math/big"
)

// This file implements the abstract equality, relational, and arithmetic
// operations shared by the evaluator and the built-in library.

// strictEquals implements the Strict Equality Comparison (===, §7.2.16). No
// coercion is performed.
func strictEquals(a, b Value) bool {
	switch x := a.(type) {
	case Undefined:
		_, ok := b.(Undefined)
		return ok
	case Null:
		_, ok := b.(Null)
		return ok
	case Boolean:
		y, ok := b.(Boolean)
		return ok && x == y
	case Number:
		y, ok := b.(Number)
		return ok && float64(x) == float64(y) // NaN != NaN falls out naturally
	case String:
		y, ok := b.(String)
		return ok && x == y
	case *Symbol:
		y, ok := b.(*Symbol)
		return ok && x == y
	case *BigInt:
		y, ok := b.(*BigInt)
		return ok && x.Int.Cmp(y.Int) == 0
	case *Object:
		y, ok := b.(*Object)
		return ok && x == y
	default:
		return false
	}
}

// sameValue implements the SameValue comparison (§7.2.10): like strict equality
// but NaN equals NaN and +0 is distinct from -0. It backs Object.is.
func sameValue(a, b Value) bool {
	if xa, ok := a.(Number); ok {
		if xb, ok := b.(Number); ok {
			fa, fb := float64(xa), float64(xb)
			if math.IsNaN(fa) && math.IsNaN(fb) {
				return true
			}
			if fa == 0 && fb == 0 {
				return math.Signbit(fa) == math.Signbit(fb)
			}
			return fa == fb
		}
	}
	return strictEquals(a, b)
}

// sameValueZero is strictEquals except that NaN is considered equal to NaN
// (§7.2.11). It backs Array.prototype.includes and Map/Set keys.
func sameValueZero(a, b Value) bool {
	if xa, ok := a.(Number); ok {
		if xb, ok := b.(Number); ok {
			fa, fb := float64(xa), float64(xb)
			if math.IsNaN(fa) && math.IsNaN(fb) {
				return true
			}
			return fa == fb
		}
	}
	return strictEquals(a, b)
}

// looseEquals implements the Abstract Equality Comparison (==, §7.2.15),
// including type coercion (which may call user methods, hence ctx).
func (i *Interpreter) looseEquals(ctx context.Context, a, b Value) (bool, error) {
	// Same type: defer to strict equality.
	if sameTypeCategory(a, b) {
		return strictEquals(a, b), nil
	}
	switch {
	case IsNullish(a) && IsNullish(b):
		return true, nil
	case IsNullish(a) || IsNullish(b):
		return false, nil
	}
	// Number/String and Boolean coercions.
	switch x := a.(type) {
	case Number:
		if _, ok := b.(String); ok {
			return float64(x) == ToNumber(b), nil
		}
	case String:
		if _, ok := b.(Number); ok {
			return ToNumber(a) == float64(b.(Number)), nil
		}
	case Boolean:
		return i.looseEquals(ctx, Number(ToNumber(a)), b)
	case *BigInt:
		switch b.(type) {
		case Number, String:
			return bigEqualsNumeric(x, b), nil
		}
	}
	if _, ok := b.(Boolean); ok {
		return i.looseEquals(ctx, a, Number(ToNumber(b)))
	}
	// Object vs primitive: coerce the object to a primitive and retry.
	if ao, ok := a.(*Object); ok {
		if isPrimitive(b) {
			prim, err := i.ToPrimitive(ctx, ao, "")
			if err != nil {
				return false, err
			}
			return i.looseEquals(ctx, prim, b)
		}
	}
	if bo, ok := b.(*Object); ok {
		if isPrimitive(a) {
			prim, err := i.ToPrimitive(ctx, bo, "")
			if err != nil {
				return false, err
			}
			return i.looseEquals(ctx, a, prim)
		}
	}
	return false, nil
}

// sameTypeCategory reports whether a and b share the same loose-equality type
// category (both numbers, both strings, both objects, ...).
func sameTypeCategory(a, b Value) bool {
	switch a.(type) {
	case Undefined:
		_, ok := b.(Undefined)
		return ok
	case Null:
		_, ok := b.(Null)
		return ok
	case Boolean:
		_, ok := b.(Boolean)
		return ok
	case Number:
		_, ok := b.(Number)
		return ok
	case String:
		_, ok := b.(String)
		return ok
	case *Symbol:
		_, ok := b.(*Symbol)
		return ok
	case *BigInt:
		_, ok := b.(*BigInt)
		return ok
	case *Object:
		_, ok := b.(*Object)
		return ok
	}
	return false
}

// bigEqualsNumeric compares a BigInt with a Number or numeric String by value.
func bigEqualsNumeric(x *BigInt, other Value) bool {
	f := ToNumber(other)
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return false
	}
	xf := new(big.Float).SetInt(x.Int)
	of := big.NewFloat(f)
	return xf.Cmp(of) == 0
}
