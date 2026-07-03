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
	// Private member access (#name) resolves against per-object private storage
	// with a brand check, not the ordinary property chain.
	if priv, ok := e.Property.(*ast.PrivateIdent); ok && !e.Computed {
		val, err := i.getPrivateMember(ctx, base, env.resolvePrivate(priv.Name), priv.Name)
		if err != nil {
			return nil, nil, err
		}
		return val, base, nil
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

// assignSuperMember implements `super.x = v`: an inherited accessor's setter is
// invoked with `this` as the receiver; otherwise the value is written as an own
// property of `this`.
func (i *Interpreter) assignSuperMember(ctx context.Context, e *ast.MemberExpr, value Value, env *Environment) error {
	home := env.homeObject()
	if home == nil || home.proto == nil {
		return i.throwError(ctx, "SyntaxError", "'super' keyword unexpected here")
	}
	key, err := i.memberKey(ctx, e, env)
	if err != nil {
		return err
	}
	thisVal, _ := env.thisBinding()
	// Look up an accessor on the super chain; if a setter exists, run it with the
	// current `this` as the receiver.
	for cur := home.proto; cur != nil; cur = cur.proto {
		p, ok := cur.getOwn(key)
		if !ok {
			continue
		}
		if p.Accessor {
			if p.Set == nil {
				return nil // accessor without a setter: ignore (non-strict)
			}
			_, err := p.Set.fn.call(ctx, thisVal, []Value{value})
			return err
		}
		break
	}
	if obj, ok := thisVal.(*Object); ok {
		return obj.Set(ctx, key, value)
	}
	return nil
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
	// A direct eval — the callee is the identifier `eval` resolving to the
	// %eval% intrinsic — runs in the caller's lexical context.
	if fnObj == i.evalFn {
		if id, ok := e.Callee.(*ast.Ident); ok && id.Name == "eval" {
			return i.directEval(ctx, arg(args, 0), env)
		}
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

	// Plain assignment keeps the existing target path.
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

	// Compound and logical assignment must resolve the LeftHandSideExpression to
	// a single Reference, read through it, then write back through that same
	// reference — never re-resolving the target after the right-hand side runs
	// (§13.15.2). Re-resolution would consult a binding the RHS introduced or a
	// `with`-property it deleted.
	ref, err := i.evalRef(ctx, e.Target, env)
	if err != nil {
		return nil, err
	}
	cur, err := i.getRefValue(ctx, ref)
	if err != nil {
		return nil, err
	}

	// Logical assignment operators short-circuit on the current value.
	switch e.Op {
	case token.AND_ASSIGN, token.OR_ASSIGN, token.NULLISH_ASSIGN:
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
		if err := i.putRefValue(ctx, ref, val); err != nil {
			return nil, err
		}
		return val, nil
	}

	// Arithmetic compound assignment.
	rhs, err := i.evalExpr(ctx, e.Value, env)
	if err != nil {
		return nil, err
	}
	result, err := i.applyBinary(ctx, compoundBaseOp(e.Op), cur, rhs)
	if err != nil {
		return nil, err
	}
	if err := i.putRefValue(ctx, ref, result); err != nil {
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
		// super.x = v assigns through the home object's chain with `this` as the
		// receiver (so an inherited setter runs, else an own property is set).
		if _, ok := t.Object.(*ast.SuperExpr); ok {
			return i.assignSuperMember(ctx, t, value, env)
		}
		base, err := i.evalExpr(ctx, t.Object, env)
		if err != nil {
			return err
		}
		// Private member assignment (#name) routes to private storage with a
		// brand check.
		if priv, ok := t.Property.(*ast.PrivateIdent); ok && !t.Computed {
			return i.setPrivateMember(ctx, base, env.resolvePrivate(priv.Name), priv.Name, value)
		}
		key, err := i.memberKey(ctx, t, env)
		if err != nil {
			return err
		}
		obj, ok := base.(*Object)
		if !ok {
			if IsNullish(base) {
				return i.throwError(ctx, "TypeError", "Cannot set properties of "+briefValue(base))
			}
			// Writes to a primitive receiver never take effect; in strict mode
			// that failed [[Set]] is a TypeError (§13.15.2 / PutValue).
			if env.isStrict() {
				return i.throwError(ctx, "TypeError", "Cannot create property "+keyName(key)+" on "+briefValue(base))
			}
			return nil
		}
		// A failed [[Set]] (non-writable data property, accessor without a setter,
		// non-extensible object, or a rejecting Proxy trap) is silently ignored in
		// sloppy mode but is a TypeError in strict mode (§13.15.2 / PutValue).
		wrote, err := obj.setStatus(ctx, key, value)
		if err != nil {
			return err
		}
		if !wrote && env.isStrict() {
			return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+keyName(key)+" of "+briefValue(base))
		}
		return nil
	case *ast.ArrayLit, *ast.ObjectLit:
		return i.destructureAssign(ctx, target, value, env)
	default:
		return i.throwError(ctx, "SyntaxError", "invalid assignment target")
	}
}

// assignIdent assigns to an existing binding, or creates a global on implicit
// assignment (non-strict semantics).
func (i *Interpreter) assignIdent(ctx context.Context, name string, value Value, env *Environment) error {
	// Interleave `with` object environment records with declarative bindings so
	// a write targets the innermost binder of name (§9.1.1.1 / §9.1.1.2).
	for e := env; e != nil; e = e.parent {
		if e.withObj != nil {
			obj, ok, err := i.withHasBinding(ctx, e.withObj, name)
			if err != nil {
				return err
			}
			if ok {
				wrote, err := obj.setStatus(ctx, StrKey(name), value)
				if err != nil {
					return err
				}
				if !wrote && env.isStrict() {
					return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+keyName(StrKey(name)))
				}
				return nil
			}
		}
		if _, ok := e.vars[name]; ok {
			break
		}
	}
	if b := env.lookup(name); b != nil {
		// A write to a lexical binding still in its Temporal Dead Zone throws.
		if !b.initialized {
			return i.throwError(ctx, "ReferenceError", "Cannot access '"+name+"' before initialization")
		}
		if !b.mutable {
			return i.throwError(ctx, "TypeError", "Assignment to constant variable.")
		}
		b.value = value
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
	case *ast.AssignExpr:
		// A default in a destructuring-assignment pattern, e.g. [a = 1], parses
		// as a plain assignment expression rather than an AssignPattern.
		if t.Op == token.ASSIGN {
			if IsUndefined(value) {
				def, err := i.evalExpr(ctx, t.Value, env)
				if err != nil {
					return err
				}
				value = def
			}
			return i.assignPattern(ctx, t.Target, value, env, bindName)
		}
		return i.throwError(ctx, "SyntaxError", "invalid assignment target")
	case *ast.ArrayLit:
		return i.iterArrayPattern(ctx, t.Elements, value,
			func(el ast.Expr, v Value) error { return i.assignPattern(ctx, el, v, env, bindName) },
			func(target ast.Expr, rest []Value) error {
				return i.assignPattern(ctx, target, i.newArray(rest), env, bindName)
			})
	case *ast.ObjectLit:
		obj, err := i.ToObject(ctx, value)
		if err != nil {
			return err
		}
		taken := map[string]bool{}
		for _, prop := range t.Properties {
			if prop.Kind == ast.PropSpread {
				rest := NewObject(i.objectProto)
				for _, name := range obj.OwnKeys() {
					if taken[name] {
						continue
					}
					if p, ok := obj.getOwn(StrKey(name)); ok && p.Enumerable {
						v, err := obj.GetStr(ctx, name)
						if err != nil {
							return err
						}
						rest.SetData(name, v)
					}
				}
				if err := i.assignPattern(ctx, prop.Value, rest, env, bindName); err != nil {
					return err
				}
				continue
			}
			key, err := i.propertyKeyName(ctx, prop, env)
			if err != nil {
				return err
			}
			taken[key] = true
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

// restTargetOf returns the underlying target of a rest/spread element, or nil
// when el is not a rest element.
func restTargetOf(el ast.Expr) ast.Expr {
	switch e := el.(type) {
	case *ast.SpreadElement:
		return e.Argument
	case *ast.RestElement:
		return e.Target
	}
	return nil
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
