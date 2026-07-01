package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// compoundBaseOp maps a compound-assignment operator to its underlying binary
// operator (e.g. += -> +).
func compoundBaseOp(op token.Type) token.Type {
	switch op {
	case token.PLUS_ASSIGN:
		return token.PLUS
	case token.MINUS_ASSIGN:
		return token.MINUS
	case token.STAR_ASSIGN:
		return token.STAR
	case token.SLASH_ASSIGN:
		return token.SLASH
	case token.PERCENT_ASSIGN:
		return token.PERCENT
	case token.EXP_ASSIGN:
		return token.EXP
	case token.SHL_ASSIGN:
		return token.SHL
	case token.SHR_ASSIGN:
		return token.SHR
	case token.USHR_ASSIGN:
		return token.USHR
	case token.BIT_AND_ASSIGN:
		return token.BIT_AND
	case token.BIT_OR_ASSIGN:
		return token.BIT_OR
	case token.BIT_XOR_ASSIGN:
		return token.BIT_XOR
	default:
		return op
	}
}

// This file implements property access, function calls, construction (new),
// assignment, and optional chaining.

// shortCircuit is an internal sentinel produced when an optional-chain link
// (?.) is applied to a nullish base. It propagates up through the remaining
// member/call links of the chain and is converted to undefined at the top of
// the expression (see evalExpr).
type shortCircuit struct{}

func (shortCircuit) Typeof() string { return "undefined" }

var scSentinel Value = shortCircuit{}

func isShortCircuit(v Value) bool { _, ok := v.(shortCircuit); return ok }

// getProperty reads key from any value, boxing primitives to their prototype so
// e.g. "abc".length and (5).toString() work. A nullish base throws.
func (i *Interpreter) getProperty(ctx context.Context, base Value, key PropertyKey) (Value, error) {
	switch b := base.(type) {
	case *Object:
		return b.Get(ctx, key)
	case String:
		if !key.IsSymbol() {
			if key.Str == "length" {
				return Number(float64(len([]rune(string(b))))), nil
			}
			if idx, ok := arrayIndex(key.Str); ok {
				rs := []rune(string(b))
				if idx < len(rs) {
					return String(string(rs[idx])), nil
				}
				return Undef, nil
			}
		}
		return i.stringProto.getWithReceiver(ctx, key, base)
	case Number:
		return i.numberProto.getWithReceiver(ctx, key, base)
	case Boolean:
		return i.booleanProto.getWithReceiver(ctx, key, base)
	case *Symbol:
		return i.symbolProto.getWithReceiver(ctx, key, base)
	case *BigInt:
		return i.bigintProto.getWithReceiver(ctx, key, base)
	default:
		return nil, i.throwError(ctx, "TypeError",
			"Cannot read properties of "+briefValue(base)+" (reading '"+keyName(key)+"')")
	}
}

// memberKey computes the property key of a member expression.
func (i *Interpreter) memberKey(ctx context.Context, e *ast.MemberExpr, env *Environment) (PropertyKey, error) {
	if e.Computed {
		v, err := i.evalExpr(ctx, e.Property, env)
		if err != nil {
			return PropertyKey{}, err
		}
		return i.ToPropertyKey(ctx, v)
	}
	switch p := e.Property.(type) {
	case *ast.Ident:
		return StrKey(p.Name), nil
	case *ast.PrivateIdent:
		return StrKey(p.Name), nil
	default:
		return PropertyKey{}, i.throwError(ctx, "SyntaxError", "invalid member expression")
	}
}

// evalMember evaluates a member access, returning the value and the base object
// (the `this` binding for a subsequent method call). It participates in
// optional chaining.
func (i *Interpreter) evalMember(ctx context.Context, e *ast.MemberExpr, env *Environment) (Value, Value, error) {
	// super.member resolves against the home object's prototype and must be
	// handled before evaluating the base (evaluating a bare `super` throws).
	if _, ok := e.Object.(*ast.SuperExpr); ok {
		return i.evalSuperMember(ctx, e, env)
	}
	base, err := i.evalExprNamed(ctx, e.Object, env, "")
	if err != nil {
		return nil, nil, err
	}
	if isShortCircuit(base) {
		return scSentinel, Undef, nil
	}
	if e.Optional && IsNullish(base) {
		return scSentinel, Undef, nil
	}
	key, err := i.memberKey(ctx, e, env)
	if err != nil {
		return nil, nil, err
	}
	val, err := i.getProperty(ctx, base, key)
	if err != nil {
		return nil, nil, err
	}
	return val, base, nil
}

// evalSuperMember handles super.x property access inside a method.
func (i *Interpreter) evalSuperMember(ctx context.Context, e *ast.MemberExpr, env *Environment) (Value, Value, error) {
	home := env.homeObject()
	if home == nil || home.proto == nil {
		return nil, nil, i.throwError(ctx, "SyntaxError", "'super' keyword unexpected here")
	}
	key, err := i.memberKey(ctx, e, env)
	if err != nil {
		return nil, nil, err
	}
	thisVal, _ := env.thisBinding()
	val, err := home.proto.getWithReceiver(ctx, key, thisVal)
	if err != nil {
		return nil, nil, err
	}
	return val, thisVal, nil
}

// evalCallee resolves a call target to (function, thisArg), supporting method
// calls and optional chaining.
func (i *Interpreter) evalCallee(ctx context.Context, callee ast.Expr, env *Environment) (Value, Value, error) {
	if member, ok := callee.(*ast.MemberExpr); ok {
		val, base, err := i.evalMember(ctx, member, env)
		return val, base, err
	}
	// super(...) call.
	if _, ok := callee.(*ast.SuperExpr); ok {
		return i.resolveSuperCall(ctx, env)
	}
	fn, err := i.evalExprNamed(ctx, callee, env, "")
	return fn, Undef, err
}

// evalCall evaluates a function call, expanding spread arguments and honoring
// optional calls.
func (i *Interpreter) evalCall(ctx context.Context, e *ast.CallExpr, env *Environment) (Value, error) {
	fn, thisArg, err := i.evalCallee(ctx, e.Callee, env)
	if err != nil {
		return nil, err
	}
	if isShortCircuit(fn) {
		return scSentinel, nil
	}
	if e.Optional && IsNullish(fn) {
		return scSentinel, nil
	}
	args, err := i.evalArgs(ctx, e.Arguments, env)
	if err != nil {
		return nil, err
	}
	fnObj, ok := fn.(*Object)
	if !ok || !fnObj.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", i.calleeName(e.Callee)+" is not a function")
	}
	return fnObj.fn.call(ctx, thisArg, args)
}

// evalArgs evaluates a call/new argument list, expanding spreads.
func (i *Interpreter) evalArgs(ctx context.Context, argExprs []ast.Expr, env *Environment) ([]Value, error) {
	var args []Value
	for _, a := range argExprs {
		if sp, ok := a.(*ast.SpreadElement); ok {
			v, err := i.evalExpr(ctx, sp.Argument, env)
			if err != nil {
				return nil, err
			}
			spread, err := i.iterableToSlice(ctx, v)
			if err != nil {
				return nil, err
			}
			args = append(args, spread...)
			continue
		}
		v, err := i.evalExpr(ctx, a, env)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	return args, nil
}

// evalNew evaluates a `new` expression.
func (i *Interpreter) evalNew(ctx context.Context, e *ast.NewExpr, env *Environment) (Value, error) {
	callee, err := i.evalExpr(ctx, e.Callee, env)
	if err != nil {
		return nil, err
	}
	ctor, ok := callee.(*Object)
	if !ok || !ctor.IsConstructor() {
		return nil, i.throwError(ctx, "TypeError", i.calleeName(e.Callee)+" is not a constructor")
	}
	args, err := i.evalArgs(ctx, e.Arguments, env)
	if err != nil {
		return nil, err
	}
	return ctor.fn.construct(ctx, ctor, args)
}

// ---------------------------------------------------------------------------
// Assignment
// ---------------------------------------------------------------------------

// evalAssign evaluates simple and compound assignment expressions, including
// destructuring assignment and logical-assignment short-circuits.
func (i *Interpreter) evalAssign(ctx context.Context, e *ast.AssignExpr, env *Environment) (Value, error) {
	// Destructuring assignment: target is an array/object pattern.
	if e.Op == token.ASSIGN {
		switch e.Target.(type) {
		case *ast.ArrayLit, *ast.ObjectLit:
			value, err := i.evalExpr(ctx, e.Value, env)
			if err != nil {
				return nil, err
			}
			if err := i.destructureAssign(ctx, e.Target, value, env); err != nil {
				return nil, err
			}
			return value, nil
		}
	}

	// Logical assignment operators short-circuit on the current value.
	switch e.Op {
	case token.AND_ASSIGN, token.OR_ASSIGN, token.NULLISH_ASSIGN:
		cur, err := i.evalExpr(ctx, e.Target, env)
		if err != nil {
			return nil, err
		}
		short := (e.Op == token.AND_ASSIGN && !ToBoolean(cur)) ||
			(e.Op == token.OR_ASSIGN && ToBoolean(cur)) ||
			(e.Op == token.NULLISH_ASSIGN && !IsNullish(cur))
		if short {
			return cur, nil
		}
		val, err := i.evalExprNamed(ctx, e.Value, env, bindingName(e.Target))
		if err != nil {
			return nil, err
		}
		if err := i.assignTo(ctx, e.Target, val, env); err != nil {
			return nil, err
		}
		return val, nil
	}

	// Plain and arithmetic compound assignment.
	if e.Op == token.ASSIGN {
		val, err := i.evalExprNamed(ctx, e.Value, env, bindingName(e.Target))
		if err != nil {
			return nil, err
		}
		if err := i.assignTo(ctx, e.Target, val, env); err != nil {
			return nil, err
		}
		return val, nil
	}
	cur, err := i.evalExpr(ctx, e.Target, env)
	if err != nil {
		return nil, err
	}
	rhs, err := i.evalExpr(ctx, e.Value, env)
	if err != nil {
		return nil, err
	}
	result, err := i.applyBinary(ctx, compoundBaseOp(e.Op), cur, rhs)
	if err != nil {
		return nil, err
	}
	if err := i.assignTo(ctx, e.Target, result, env); err != nil {
		return nil, err
	}
	return result, nil
}

// assignTo writes value to an assignment target (identifier or member).
func (i *Interpreter) assignTo(ctx context.Context, target ast.Expr, value Value, env *Environment) error {
	switch t := target.(type) {
	case *ast.Ident:
		return i.assignIdent(ctx, t.Name, value, env)
	case *ast.MemberExpr:
		base, err := i.evalExpr(ctx, t.Object, env)
		if err != nil {
			return err
		}
		key, err := i.memberKey(ctx, t, env)
		if err != nil {
			return err
		}
		obj, ok := base.(*Object)
		if !ok {
			// Writes to primitive receivers are silently dropped (non-strict).
			if IsNullish(base) {
				return i.throwError(ctx, "TypeError", "Cannot set properties of "+briefValue(base))
			}
			return nil
		}
		return obj.Set(ctx, key, value)
	case *ast.ArrayLit, *ast.ObjectLit:
		return i.destructureAssign(ctx, target, value, env)
	default:
		return i.throwError(ctx, "SyntaxError", "invalid assignment target")
	}
}

// assignIdent assigns to an existing binding, or creates a global on implicit
// assignment (non-strict semantics).
func (i *Interpreter) assignIdent(ctx context.Context, name string, value Value, env *Environment) error {
	if b := env.lookup(name); b != nil {
		if !b.mutable && b.initialized {
			return i.throwError(ctx, "TypeError", "Assignment to constant variable.")
		}
		b.value = value
		b.initialized = true
		return nil
	}
	// Implicit global.
	return i.global.SetStr(ctx, name, value)
}

// assignBinding sets a binding known to exist (used for class declarations).
func (i *Interpreter) assignBinding(env *Environment, name string, value Value) {
	if b := env.lookup(name); b != nil {
		b.value = value
		b.initialized = true
		return
	}
	env.vars[name] = &binding{value: value, mutable: true, initialized: true}
}

// destructureAssign performs assignment-context destructuring (no new bindings;
// each leaf is an assignment target).
func (i *Interpreter) destructureAssign(ctx context.Context, pattern ast.Expr, value Value, env *Environment) error {
	assign := func(name string, v Value) {
		_ = i.assignIdent(ctx, name, v, env)
	}
	// For member targets inside the pattern we need the full assignTo path;
	// bindPattern only handles name binding, so we special-case simple names
	// and fall back to assignTo for members via a wrapper pattern walk.
	return i.assignPattern(ctx, pattern, value, env, assign)
}

// assignPattern is like bindPattern but assigns to existing references and
// supports member targets.
func (i *Interpreter) assignPattern(ctx context.Context, target ast.Expr, value Value, env *Environment, bindName func(string, Value)) error {
	switch t := target.(type) {
	case *ast.Ident:
		return i.assignIdent(ctx, t.Name, value, env)
	case *ast.MemberExpr:
		return i.assignTo(ctx, t, value, env)
	case *ast.AssignPattern:
		if IsUndefined(value) {
			def, err := i.evalExpr(ctx, t.Default, env)
			if err != nil {
				return err
			}
			value = def
		}
		return i.assignPattern(ctx, t.Target, value, env, bindName)
	case *ast.ArrayLit:
		items, err := i.iterableToSlice(ctx, value)
		if err != nil {
			return err
		}
		for idx, el := range t.Elements {
			if el == nil {
				continue
			}
			if rest, ok := el.(*ast.RestElement); ok {
				var remaining []Value
				if idx < len(items) {
					remaining = append(remaining, items[idx:]...)
				}
				return i.assignPattern(ctx, rest.Target, i.newArray(remaining), env, bindName)
			}
			var v Value = Undef
			if idx < len(items) {
				v = items[idx]
			}
			if err := i.assignPattern(ctx, el, v, env, bindName); err != nil {
				return err
			}
		}
		return nil
	case *ast.ObjectLit:
		obj, err := i.ToObject(ctx, value)
		if err != nil {
			return err
		}
		for _, prop := range t.Properties {
			key, err := i.propertyKeyName(ctx, prop, env)
			if err != nil {
				return err
			}
			v, err := obj.GetStr(ctx, key)
			if err != nil {
				return err
			}
			tgt := prop.Value
			if tgt == nil {
				tgt = prop.Key
			}
			if err := i.assignPattern(ctx, tgt, v, env, bindName); err != nil {
				return err
			}
		}
		return nil
	default:
		return i.throwError(ctx, "SyntaxError", "invalid assignment target")
	}
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// calleeName produces a readable name for a call target, for error messages.
func (i *Interpreter) calleeName(callee ast.Expr) string {
	switch c := callee.(type) {
	case *ast.Ident:
		return c.Name
	case *ast.MemberExpr:
		if p, ok := c.Property.(*ast.Ident); ok {
			return i.calleeName(c.Object) + "." + p.Name
		}
	}
	return "expression"
}

// keyName renders a property key for error messages.
func keyName(k PropertyKey) string {
	if k.IsSymbol() {
		return "Symbol(" + k.Sym.Desc + ")"
	}
	return k.Str
}
