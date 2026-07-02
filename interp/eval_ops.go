package interp

import (
	"context"
	"math"
	"math/big"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements unary, update (++/--), and binary operator evaluation,
// including the abstract addition/relational/bitwise algorithms.

// evalUnary evaluates prefix unary operators.
func (i *Interpreter) evalUnary(ctx context.Context, e *ast.UnaryExpr, env *Environment) (Value, error) {
	// typeof and delete need special handling of their operand.
	switch e.Op {
	case token.TYPEOF:
		return i.evalTypeof(ctx, e.Operand, env)
	case token.DELETE:
		return i.evalDelete(ctx, e.Operand, env)
	}

	v, err := i.evalExpr(ctx, e.Operand, env)
	if err != nil {
		return nil, err
	}
	switch e.Op {
	case token.NOT:
		return Bool(!ToBoolean(v)), nil
	case token.MINUS:
		if b, ok := v.(*BigInt); ok {
			return &BigInt{Int: new(big.Int).Neg(b.Int)}, nil
		}
		n, err := i.ToNumberV(ctx, v)
		if err != nil {
			return nil, err
		}
		return Number(-n), nil
	case token.PLUS:
		n, err := i.ToNumberV(ctx, v)
		if err != nil {
			return nil, err
		}
		return Number(n), nil
	case token.BIT_NOT:
		if b, ok := v.(*BigInt); ok {
			return &BigInt{Int: new(big.Int).Not(b.Int)}, nil
		}
		n, err := i.ToNumberV(ctx, v)
		if err != nil {
			return nil, err
		}
		return Number(float64(^ToInt32(n))), nil
	case token.VOID:
		return Undef, nil
	default:
		return nil, i.throwError(ctx, "SyntaxError", "unsupported unary operator")
	}
}

// evalTypeof implements the typeof operator, which yields "undefined" for an
// unresolved identifier rather than throwing.
func (i *Interpreter) evalTypeof(ctx context.Context, operand ast.Expr, env *Environment) (Value, error) {
	if id, ok := operand.(*ast.Ident); ok {
		if b := env.lookup(id.Name); b == nil && !i.global.Has(StrKey(id.Name)) && id.Name != "undefined" {
			return String("undefined"), nil
		}
	}
	v, err := i.evalExpr(ctx, operand, env)
	if err != nil {
		return nil, err
	}
	return String(v.Typeof()), nil
}

// evalDelete implements the delete operator on member expressions.
func (i *Interpreter) evalDelete(ctx context.Context, operand ast.Expr, env *Environment) (Value, error) {
	// delete of a bare identifier: a lexical/local binding cannot be deleted
	// (returns false); a global property is deleted subject to its configurable
	// flag (a var-created global is non-configurable, so delete returns false);
	// an unresolved name yields true.
	if id, ok := operand.(*ast.Ident); ok {
		if b := env.lookup(id.Name); b != nil {
			return False, nil
		}
		key := StrKey(id.Name)
		if i.global.HasOwn(key) {
			return Bool(i.global.Delete(key)), nil
		}
		return True, nil
	}
	member, ok := operand.(*ast.MemberExpr)
	if !ok {
		return True, nil // delete of a non-reference is a no-op that returns true
	}
	obj, err := i.evalExpr(ctx, member.Object, env)
	if err != nil {
		return nil, err
	}
	o, err := i.ToObject(ctx, obj)
	if err != nil {
		return nil, err
	}
	key, err := i.memberKey(ctx, member, env)
	if err != nil {
		return nil, err
	}
	return Bool(o.Delete(key)), nil
}

// evalUpdate implements prefix/postfix ++ and --.
func (i *Interpreter) evalUpdate(ctx context.Context, e *ast.UpdateExpr, env *Environment) (Value, error) {
	old, err := i.evalExpr(ctx, e.Operand, env)
	if err != nil {
		return nil, err
	}
	// BigInt increments stay BigInt.
	if b, ok := old.(*BigInt); ok {
		delta := big.NewInt(1)
		nv := new(big.Int)
		if e.Op == token.INC {
			nv.Add(b.Int, delta)
		} else {
			nv.Sub(b.Int, delta)
		}
		res := &BigInt{Int: nv}
		if err := i.assignTo(ctx, e.Operand, res, env); err != nil {
			return nil, err
		}
		if e.Prefix {
			return res, nil
		}
		return b, nil
	}
	n, err := i.ToNumberV(ctx, old)
	if err != nil {
		return nil, err
	}
	var updated float64
	if e.Op == token.INC {
		updated = n + 1
	} else {
		updated = n - 1
	}
	if err := i.assignTo(ctx, e.Operand, Number(updated), env); err != nil {
		return nil, err
	}
	if e.Prefix {
		return Number(updated), nil
	}
	return Number(n), nil
}

// evalBinary evaluates a binary operator expression.
func (i *Interpreter) evalBinary(ctx context.Context, e *ast.BinaryExpr, env *Environment) (Value, error) {
	// Ergonomic brand check: `#field in obj` tests whether obj carries the
	// private field, without evaluating `#field` as a value.
	if e.Op == token.IN {
		if priv, ok := e.Left.(*ast.PrivateIdent); ok {
			right, err := i.evalExpr(ctx, e.Right, env)
			if err != nil {
				return nil, err
			}
			obj, ok := right.(*Object)
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "Cannot use 'in' operator to search in a non-object")
			}
			pn := env.resolvePrivate(priv.Name)
			return Bool(pn != nil && obj.hasPrivate(pn)), nil
		}
	}
	left, err := i.evalExpr(ctx, e.Left, env)
	if err != nil {
		return nil, err
	}
	// `in` and `instanceof` inspect the right operand specially.
	switch e.Op {
	case token.IN:
		return i.evalIn(ctx, left, e.Right, env)
	case token.INSTANCEOF:
		right, err := i.evalExpr(ctx, e.Right, env)
		if err != nil {
			return nil, err
		}
		return i.evalInstanceof(ctx, left, right)
	}
	right, err := i.evalExpr(ctx, e.Right, env)
	if err != nil {
		return nil, err
	}
	return i.applyBinary(ctx, e.Op, left, right)
}

// applyBinary computes the result of a binary operator on two values.
func (i *Interpreter) applyBinary(ctx context.Context, op token.Type, left, right Value) (Value, error) {
	switch op {
	case token.PLUS:
		return i.evalAdd(ctx, left, right)
	case token.MINUS, token.STAR, token.SLASH, token.PERCENT, token.EXP:
		return i.evalArithmetic(ctx, op, left, right)
	case token.EQ:
		eq, err := i.looseEquals(ctx, left, right)
		return Bool(eq), err
	case token.NE:
		eq, err := i.looseEquals(ctx, left, right)
		return Bool(!eq), err
	case token.STRICT_EQ:
		return Bool(strictEquals(left, right)), nil
	case token.STRICT_NE:
		return Bool(!strictEquals(left, right)), nil
	case token.LT, token.GT, token.LE, token.GE:
		return i.evalRelational(ctx, op, left, right)
	case token.BIT_AND, token.BIT_OR, token.BIT_XOR, token.SHL, token.SHR, token.USHR:
		return i.evalBitwise(ctx, op, left, right)
	default:
		return nil, i.throwError(ctx, "SyntaxError", "unsupported binary operator")
	}
}

// evalAdd implements the addition operator, which concatenates when either
// operand is a string after ToPrimitive, and otherwise adds numerically.
func (i *Interpreter) evalAdd(ctx context.Context, left, right Value) (Value, error) {
	lp, err := i.ToPrimitive(ctx, left, "")
	if err != nil {
		return nil, err
	}
	rp, err := i.ToPrimitive(ctx, right, "")
	if err != nil {
		return nil, err
	}
	_, lStr := lp.(String)
	_, rStr := rp.(String)
	if lStr || rStr {
		ls, err := i.ToStringV(ctx, lp)
		if err != nil {
			return nil, err
		}
		rs, err := i.ToStringV(ctx, rp)
		if err != nil {
			return nil, err
		}
		return String(ls + rs), nil
	}
	if lb, ok := lp.(*BigInt); ok {
		if rb, ok := rp.(*BigInt); ok {
			return &BigInt{Int: new(big.Int).Add(lb.Int, rb.Int)}, nil
		}
		return nil, i.throwError(ctx, "TypeError", "Cannot mix BigInt and other types, use explicit conversions")
	}
	ln, err := i.ToNumberV(ctx, lp)
	if err != nil {
		return nil, err
	}
	rn, err := i.ToNumberV(ctx, rp)
	if err != nil {
		return nil, err
	}
	return Number(ln + rn), nil
}

// evalArithmetic implements -, *, /, %, ** for numbers and BigInts.
func (i *Interpreter) evalArithmetic(ctx context.Context, op token.Type, left, right Value) (Value, error) {
	if lb, ok := left.(*BigInt); ok {
		if rb, ok := right.(*BigInt); ok {
			return i.bigArithmetic(ctx, op, lb, rb)
		}
		return nil, i.throwError(ctx, "TypeError", "Cannot mix BigInt and other types, use explicit conversions")
	}
	ln, err := i.ToNumberV(ctx, left)
	if err != nil {
		return nil, err
	}
	rn, err := i.ToNumberV(ctx, right)
	if err != nil {
		return nil, err
	}
	switch op {
	case token.MINUS:
		return Number(ln - rn), nil
	case token.STAR:
		return Number(ln * rn), nil
	case token.SLASH:
		return Number(ln / rn), nil
	case token.PERCENT:
		return Number(math.Mod(ln, rn)), nil
	case token.EXP:
		return Number(math.Pow(ln, rn)), nil
	default:
		return nil, i.throwError(ctx, "SyntaxError", "unsupported arithmetic operator")
	}
}

// bigArithmetic implements BigInt arithmetic.
func (i *Interpreter) bigArithmetic(ctx context.Context, op token.Type, a, b *BigInt) (Value, error) {
	res := new(big.Int)
	switch op {
	case token.MINUS:
		res.Sub(a.Int, b.Int)
	case token.STAR:
		res.Mul(a.Int, b.Int)
	case token.SLASH:
		if b.Int.Sign() == 0 {
			return nil, i.throwError(ctx, "RangeError", "Division by zero")
		}
		res.Quo(a.Int, b.Int)
	case token.PERCENT:
		if b.Int.Sign() == 0 {
			return nil, i.throwError(ctx, "RangeError", "Division by zero")
		}
		res.Rem(a.Int, b.Int)
	case token.EXP:
		res.Exp(a.Int, b.Int, nil)
	default:
		return nil, i.throwError(ctx, "SyntaxError", "unsupported BigInt operator")
	}
	return &BigInt{Int: res}, nil
}

// evalRelational implements <, >, <=, >= following the Abstract Relational
// Comparison (§7.2.13): string/string compares lexicographically, otherwise
// numeric.
func (i *Interpreter) evalRelational(ctx context.Context, op token.Type, left, right Value) (Value, error) {
	lp, err := i.ToPrimitive(ctx, left, "number")
	if err != nil {
		return nil, err
	}
	rp, err := i.ToPrimitive(ctx, right, "number")
	if err != nil {
		return nil, err
	}
	ls, lok := lp.(String)
	rs, rok := rp.(String)
	if lok && rok {
		switch op {
		case token.LT:
			return Bool(ls < rs), nil
		case token.GT:
			return Bool(ls > rs), nil
		case token.LE:
			return Bool(ls <= rs), nil
		case token.GE:
			return Bool(ls >= rs), nil
		}
	}
	ln, err := i.ToNumberV(ctx, lp)
	if err != nil {
		return nil, err
	}
	rn, err := i.ToNumberV(ctx, rp)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(ln) || math.IsNaN(rn) {
		return False, nil
	}
	switch op {
	case token.LT:
		return Bool(ln < rn), nil
	case token.GT:
		return Bool(ln > rn), nil
	case token.LE:
		return Bool(ln <= rn), nil
	case token.GE:
		return Bool(ln >= rn), nil
	}
	return False, nil
}

// evalBitwise implements the bitwise and shift operators over 32-bit integers.
func (i *Interpreter) evalBitwise(ctx context.Context, op token.Type, left, right Value) (Value, error) {
	// BigInt bitwise.
	if lb, ok := left.(*BigInt); ok {
		if rb, ok := right.(*BigInt); ok {
			return i.bigBitwise(ctx, op, lb, rb)
		}
	}
	ln, err := i.ToNumberV(ctx, left)
	if err != nil {
		return nil, err
	}
	rn, err := i.ToNumberV(ctx, right)
	if err != nil {
		return nil, err
	}
	switch op {
	case token.BIT_AND:
		return Number(float64(ToInt32(ln) & ToInt32(rn))), nil
	case token.BIT_OR:
		return Number(float64(ToInt32(ln) | ToInt32(rn))), nil
	case token.BIT_XOR:
		return Number(float64(ToInt32(ln) ^ ToInt32(rn))), nil
	case token.SHL:
		return Number(float64(ToInt32(ln) << (ToUint32(rn) & 31))), nil
	case token.SHR:
		return Number(float64(ToInt32(ln) >> (ToUint32(rn) & 31))), nil
	case token.USHR:
		return Number(float64(ToUint32(ln) >> (ToUint32(rn) & 31))), nil
	default:
		return nil, i.throwError(ctx, "SyntaxError", "unsupported bitwise operator")
	}
}

// bigBitwise implements BigInt bitwise/shift operators.
func (i *Interpreter) bigBitwise(ctx context.Context, op token.Type, a, b *BigInt) (Value, error) {
	res := new(big.Int)
	switch op {
	case token.BIT_AND:
		res.And(a.Int, b.Int)
	case token.BIT_OR:
		res.Or(a.Int, b.Int)
	case token.BIT_XOR:
		res.Xor(a.Int, b.Int)
	case token.SHL:
		return i.bigShift(ctx, a.Int, b.Int, false)
	case token.SHR:
		return i.bigShift(ctx, a.Int, b.Int, true)
	default:
		return nil, i.throwError(ctx, "TypeError", "BigInts have no unsigned right shift, use >> instead")
	}
	return &BigInt{Int: res}, nil
}

// maxBigIntShift bounds a BigInt left shift. math/big allocates the full
// magnitude of the result, so an unbounded or negative-wrapped shift count can
// panic the host (makeslice: len out of range) or exhaust memory. Beyond this
// many bits we report the result as exceeding the maximum BigInt size, matching
// engines that cap BigInt precision. ~1 billion bits is far past any real use.
const maxBigIntShift = 1 << 30

// bigShift implements BigInt << and >> with ECMAScript semantics: a negative
// shift count reverses direction (x << -n === x >> n). It rejects left shifts
// that would exceed the maximum BigInt size rather than letting math/big panic.
func (i *Interpreter) bigShift(ctx context.Context, a, shift *big.Int, right bool) (Value, error) {
	n := new(big.Int).Set(shift)
	if n.Sign() < 0 {
		right = !right
		n.Neg(n)
	}
	if right {
		// A right shift by an out-of-range amount saturates: 0 for a
		// non-negative operand, -1 for a negative one. No allocation risk.
		if !n.IsInt64() || n.Int64() > maxBigIntShift {
			if a.Sign() < 0 {
				return &BigInt{Int: big.NewInt(-1)}, nil
			}
			return &BigInt{Int: big.NewInt(0)}, nil
		}
		return &BigInt{Int: new(big.Int).Rsh(a, uint(n.Int64()))}, nil
	}
	// Shifting zero is always zero, regardless of the (finite) count.
	if a.Sign() == 0 {
		return &BigInt{Int: big.NewInt(0)}, nil
	}
	if !n.IsInt64() || n.Int64() > maxBigIntShift {
		return nil, i.throwError(ctx, "RangeError", "Maximum BigInt size exceeded")
	}
	return &BigInt{Int: new(big.Int).Lsh(a, uint(n.Int64()))}, nil
}

// evalIn implements the `in` operator.
func (i *Interpreter) evalIn(ctx context.Context, left Value, rightExpr ast.Expr, env *Environment) (Value, error) {
	right, err := i.evalExpr(ctx, rightExpr, env)
	if err != nil {
		return nil, err
	}
	obj, ok := right.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot use 'in' operator to search in a non-object")
	}
	key, err := i.ToPropertyKey(ctx, left)
	if err != nil {
		return nil, err
	}
	return Bool(obj.Has(key)), nil
}

// evalInstanceof implements the instanceof operator, honoring Symbol.hasInstance.
func (i *Interpreter) evalInstanceof(ctx context.Context, left, right Value) (Value, error) {
	ctor, ok := right.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Right-hand side of 'instanceof' is not callable")
	}
	if hasInstance, ok := i.methodBySymbol(ctor, i.symHasInstance); ok {
		res, err := hasInstance.fn.call(ctx, ctor, []Value{left})
		if err != nil {
			return nil, err
		}
		return Bool(ToBoolean(res)), nil
	}
	if !ctor.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "Right-hand side of 'instanceof' is not callable")
	}
	res, err := i.ordinaryHasInstance(ctx, ctor, left)
	if err != nil {
		return nil, err
	}
	return Bool(res), nil
}
