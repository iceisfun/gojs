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
	base = flattenRope(base)
	switch b := base.(type) {
	case *Object:
		return b.Get(ctx, key)
	case String:
		if !key.IsSymbol() {
			if key.Str == "length" {
				return Number(float64(codeUnitLen(string(b)))), nil
			}
			if idx, ok := arrayIndex(key.Str); ok {
				s := string(b)
				// ASCII fast path: byte index == code-unit index.
				if isASCIIStr(s) {
					if idx < len(s) {
						return String(s[idx : idx+1]), nil
					}
					return Undef, nil
				}
				units := codeUnits(s)
				if idx < len(units) {
					return String(unitsToString(units[idx : idx+1])), nil
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
	// Resolve through a property Reference so a nullish base rejects (ToObject)
	// BEFORE a computed key is coerced (ToPropertyKey) — §13.3.3 / GetValue. So
	// `null[{toString(){throw}}]` throws TypeError, not the key's toString error.
	ref := &reference{kind: refProp, strict: env.isStrict(), base: base}
	if e.Computed {
		kv, err := i.evalExpr(ctx, e.Property, env)
		if err != nil {
			return nil, nil, err
		}
		ref.keyVal = kv
	} else {
		ref.key, ref.keyDone = StrKey(e.Property.(*ast.Ident).Name), true
	}
	val, err := i.getRefValue(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	return val, base, nil
}

// getThisBinding implements the Function Environment Record GetThisBinding
// (§9.1.1.3.4): it returns the effective `this`, but a derived constructor's
// binding is uninitialized until super() runs, and reading it then is a
// ReferenceError.
func (i *Interpreter) getThisBinding(ctx context.Context, env *Environment) (Value, error) {
	if env.thisUninitialized() {
		return nil, i.throwError(ctx, "ReferenceError",
			"Must call super constructor in derived class before accessing 'this' or returning from derived constructor")
	}
	v, _ := env.thisBinding()
	return v, nil
}

// superBase implements MakeSuperPropertyReference's GetThisBinding + GetSuperBase
// (§13.3.7.1, §9.1.1.3.5). It returns actualThis and the super base value
// (home.[[GetPrototypeOf]]()). The base is captured before ToPropertyKey runs on
// any computed key, per the "GetSuperBase before ToPropertyKey" ordering.
func (i *Interpreter) superBase(ctx context.Context, env *Environment) (thisVal Value, base *Object, err error) {
	home := env.homeObject()
	if home == nil {
		// HasSuperBinding() is false: no [[HomeObject]] in scope.
		return nil, nil, i.throwError(ctx, "ReferenceError", "'super' keyword unexpected here")
	}
	thisVal, err = i.getThisBinding(ctx, env)
	if err != nil {
		return nil, nil, err
	}
	return thisVal, home.proto, nil
}

// evalSuperMember handles super.x property access inside a method.
func (i *Interpreter) evalSuperMember(ctx context.Context, e *ast.MemberExpr, env *Environment) (Value, Value, error) {
	// GetThisBinding precedes evaluation of a computed key (§13.3.7.1 steps 1-2).
	thisVal, base, err := i.superBase(ctx, env)
	if err != nil {
		return nil, nil, err
	}
	// Evaluate a computed key to a raw value, capturing the super base before
	// ToPropertyKey may run user code that mutates the home prototype.
	var keyVal Value
	if e.Computed {
		if keyVal, err = i.evalExpr(ctx, e.Property, env); err != nil {
			return nil, nil, err
		}
	}
	// RequireObjectCoercible on the super base (ToObject in GetValue): a null base
	// (class extends null) is a TypeError, not a SyntaxError.
	if base == nil {
		return nil, nil, i.throwError(ctx, "TypeError", "Cannot read properties of null")
	}
	key, err := i.superKey(ctx, e, keyVal)
	if err != nil {
		return nil, nil, err
	}
	val, err := base.getWithReceiver(ctx, key, thisVal)
	if err != nil {
		return nil, nil, err
	}
	return val, thisVal, nil
}

// superKey resolves a super member's property key: a literal IdentifierName, or
// ToPropertyKey of an already-evaluated computed key value.
func (i *Interpreter) superKey(ctx context.Context, e *ast.MemberExpr, keyVal Value) (PropertyKey, error) {
	if e.Computed {
		return i.ToPropertyKey(ctx, keyVal)
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

// assignSuperMember implements `super.x = v` as PutValue on a Super Reference
// (§6.2.5.6): [[Set]] with `this` as the receiver, and a failed write is a
// TypeError in strict mode.
func (i *Interpreter) assignSuperMember(ctx context.Context, e *ast.MemberExpr, value Value, env *Environment) error {
	thisVal, base, err := i.superBase(ctx, env)
	if err != nil {
		return err
	}
	var keyVal Value
	if e.Computed {
		if keyVal, err = i.evalExpr(ctx, e.Property, env); err != nil {
			return err
		}
	}
	if base == nil {
		return i.throwError(ctx, "TypeError", "Cannot set properties of null")
	}
	key, err := i.superKey(ctx, e, keyVal)
	if err != nil {
		return err
	}
	succeeded, err := i.setV(ctx, base, key, value, thisVal)
	if err != nil {
		return err
	}
	if !succeeded && env.isStrict() {
		return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+keyName(key))
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
	// EvaluateNew (§13.3.5.1): ArgumentListEvaluation happens BEFORE the
	// IsConstructor check, so `new x(x = Array)` evaluates the argument (mutating
	// x) and only then throws for the original non-constructor value.
	args, err := i.evalArgs(ctx, e.Arguments, env)
	if err != nil {
		return nil, err
	}
	ctor, ok := callee.(*Object)
	if !ok || !ctor.IsConstructor() {
		return nil, i.throwError(ctx, "TypeError", i.calleeName(e.Callee)+" is not a constructor")
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

	// Plain assignment to a simple identifier resolves the target Reference
	// before evaluating the right-hand side (§13.15.2): a `with` object's
	// HasBinding is captured up front, so an RHS that deletes the bound property
	// still writes through the same (now strict-erroring) reference.
	if e.Op == token.ASSIGN {
		if id, ok := e.Target.(*ast.Ident); ok {
			ref, err := i.resolveIdentRef(ctx, id.Name, env)
			if err != nil {
				return nil, err
			}
			// NamedEvaluation applies only when the target IsIdentifierRef — a bare
			// identifier, not a parenthesized `(fn)` — so a covered identifier
			// target leaves an anonymous RHS unnamed (§13.15.2).
			inferName := id.Name
			if id.Parenthesized {
				inferName = ""
			}
			val, err := i.evalExprNamed(ctx, e.Value, env, inferName)
			if err != nil {
				return nil, err
			}
			if err := i.putRefValue(ctx, ref, val); err != nil {
				return nil, err
			}
			return val, nil
		}
	}

	// Plain assignment to a member target resolves the LeftHandSideExpression to
	// a Reference — evaluating the base and any computed property-name expression
	// — *before* the right-hand side, with ToPropertyKey deferred to PutValue
	// (§13.15.2 AssignmentExpression : LeftHandSideExpression = AssignmentExpression,
	// and the note that ToPropertyKey on a computed key runs after both operands).
	// So `base[prop()] = expr()` reports prop()'s throw before expr() runs, and
	// `base[keyObj] = expr()` never coerces keyObj when expr() throws first.
	if e.Op == token.ASSIGN {
		ref, err := i.evalRef(ctx, e.Target, env)
		if err != nil {
			return nil, err
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
				return i.withSetMutableBinding(ctx, obj, name, value, env.isStrict())
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
			// A non-strict immutable binding (named function expression's own name)
			// swallows the assignment in sloppy code and throws only in strict code.
			if b.weakImmutable && !env.isStrict() {
				return nil
			}
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
// each leaf is an assignment target). It implements DestructuringAssignmentEvaluation
// (§13.15.5): unlike binding destructuring, each leaf's target reference is
// resolved through the Reference machinery (so `for ({x} of …)` on an
// unresolvable name in strict mode throws a ReferenceError, and an assignment to
// a read-only property is rejected), and array patterns resolve each element's
// reference before stepping the iterator, closing it on any abrupt completion.
func (i *Interpreter) destructureAssign(ctx context.Context, pattern ast.Expr, value Value, env *Environment) error {
	return i.assignPattern(ctx, pattern, value, env)
}

// assignPattern assigns value to an assignment-context destructuring target: a
// simple reference (identifier or member), a defaulted target, or a nested
// array/object pattern.
func (i *Interpreter) assignPattern(ctx context.Context, target ast.Expr, value Value, env *Environment) error {
	switch t := target.(type) {
	case *ast.Ident, *ast.MemberExpr:
		ref, err := i.evalRef(ctx, t, env)
		if err != nil {
			return err
		}
		return i.putRefValue(ctx, ref, value)
	case *ast.AssignPattern:
		if IsUndefined(value) {
			def, err := i.evalExprNamed(ctx, t.Default, env, bindingName(t.Target))
			if err != nil {
				return err
			}
			value = def
		}
		return i.assignPattern(ctx, t.Target, value, env)
	case *ast.AssignExpr:
		// A default in a destructuring-assignment pattern, e.g. [a = 1], parses
		// as a plain assignment expression rather than an AssignPattern.
		if t.Op == token.ASSIGN {
			if IsUndefined(value) {
				def, err := i.evalExprNamed(ctx, t.Value, env, bindingName(t.Target))
				if err != nil {
					return err
				}
				value = def
			}
			return i.assignPattern(ctx, t.Target, value, env)
		}
		return i.throwError(ctx, "SyntaxError", "invalid assignment target")
	case *ast.ArrayLit:
		return i.assignArrayPattern(ctx, t, value, env)
	case *ast.ObjectLit:
		return i.assignObjectPattern(ctx, t, value, env)
	default:
		return i.throwError(ctx, "SyntaxError", "invalid assignment target")
	}
}

// assignArrayPattern implements IteratorDestructuringAssignmentEvaluation for an
// ArrayAssignmentPattern (§13.15.5.3). Each non-nested element resolves its
// target Reference *before* the iterator is stepped, and any abrupt completion
// while the iterator is not done triggers IteratorClose — matching the observable
// next()/return() call counts the spec prescribes.
func (i *Interpreter) assignArrayPattern(ctx context.Context, pat *ast.ArrayLit, value Value, env *Environment) error {
	rec, err := i.getIterator(ctx, value)
	if err != nil {
		return err
	}
	// stepValue pulls the next value, reporting done once the iterator is
	// exhausted. A step that itself throws leaves the record done (never closed).
	stepValue := func() (v Value, done bool, err error) {
		if rec.done {
			return Undef, true, nil
		}
		if err := i.checkContext(); err != nil {
			rec.done = true
			return nil, true, err
		}
		return i.iteratorStepValue(ctx, rec)
	}
	// closeOnAbrupt runs IteratorClose for an abrupt completion, preserving the
	// original error and skipping the close when the iterator is already done.
	closeOnAbrupt := func(pending error) error {
		if rec.done {
			return pending
		}
		return i.iteratorClose(ctx, rec, pending)
	}

	for _, el := range pat.Elements {
		if el == nil {
			if _, _, err := stepValue(); err != nil {
				return err
			}
			continue
		}
		if restTgt := restTargetOf(el); restTgt != nil {
			return i.assignRestElement(ctx, restTgt, stepValue, closeOnAbrupt, env)
		}
		tgt, def := splitAssignElement(el)
		if isDestructuringPattern(tgt) {
			// A nested pattern: the value is pulled first, then destructured.
			v, done, err := stepValue()
			if err != nil {
				return err
			}
			if done {
				v = Undef
			}
			if def != nil && IsUndefined(v) {
				dv, derr := i.evalExprNamed(ctx, def, env, "")
				if derr != nil {
					return closeOnAbrupt(derr)
				}
				v = dv
			}
			if aerr := i.assignPattern(ctx, tgt, v, env); aerr != nil {
				return closeOnAbrupt(aerr)
			}
			continue
		}
		// A simple reference: resolve it before stepping the iterator.
		ref, err := i.evalRef(ctx, tgt, env)
		if err != nil {
			return closeOnAbrupt(err)
		}
		v, done, err := stepValue()
		if err != nil {
			return err
		}
		if done {
			v = Undef
		}
		if def != nil && IsUndefined(v) {
			dv, derr := i.evalExprNamed(ctx, def, env, bindingName(tgt))
			if derr != nil {
				return closeOnAbrupt(derr)
			}
			v = dv
		}
		if perr := i.putRefValue(ctx, ref, v); perr != nil {
			return closeOnAbrupt(perr)
		}
	}
	if !rec.done {
		return i.iteratorClose(ctx, rec, nil)
	}
	return nil
}

// assignRestElement implements the AssignmentRestElement case: for a non-pattern
// target the Reference is resolved before the iterator is drained; the remaining
// values are collected into an array and assigned (or destructured for a nested
// pattern). Abrupt completions close the iterator when it is not done.
func (i *Interpreter) assignRestElement(ctx context.Context, target ast.Expr, stepValue func() (Value, bool, error), closeOnAbrupt func(error) error, env *Environment) error {
	if isDestructuringPattern(target) {
		rest, err := drainRest(stepValue)
		if err != nil {
			return err
		}
		if aerr := i.assignPattern(ctx, target, i.newArray(rest), env); aerr != nil {
			return closeOnAbrupt(aerr)
		}
		return nil
	}
	// Simple target: resolve the Reference before draining the iterator.
	ref, err := i.evalRef(ctx, target, env)
	if err != nil {
		return closeOnAbrupt(err)
	}
	rest, err := drainRest(stepValue)
	if err != nil {
		return err
	}
	if perr := i.putRefValue(ctx, ref, i.newArray(rest)); perr != nil {
		return closeOnAbrupt(perr)
	}
	return nil
}

// drainRest collects the remaining iterator values into a slice, stopping once
// the iterator reports done. A step that throws propagates (leaving the record
// done); no IteratorClose is required in that case.
func drainRest(stepValue func() (Value, bool, error)) ([]Value, error) {
	var rest []Value
	for {
		v, done, err := stepValue()
		if err != nil {
			return nil, err
		}
		if done {
			return rest, nil
		}
		rest = append(rest, v)
	}
}

// assignObjectPattern implements the assignment-context ObjectAssignmentPattern
// (§13.15.5.4/5): each property's value is read via [[Get]] and assigned to its
// (reference-resolved) target, and a rest target receives a fresh object of the
// remaining own enumerable properties (including symbol keys).
func (i *Interpreter) assignObjectPattern(ctx context.Context, pat *ast.ObjectLit, value Value, env *Environment) error {
	if IsNullish(value) {
		return i.throwError(ctx, "TypeError", "Cannot destructure "+briefValue(value))
	}
	obj, err := i.ToObject(ctx, value)
	if err != nil {
		return err
	}
	taken := map[PropertyKey]bool{}
	for _, prop := range pat.Properties {
		if prop.Kind == ast.PropSpread {
			// BindingRestProperty / AssignmentRestProperty: CopyDataProperties
			// (§7.3.25) with the already-bound names excluded, so the excluded
			// keys' descriptors and getters are never touched.
			rest := NewObject(i.objectProto)
			if err := i.copyDataProperties(ctx, rest, obj, taken); err != nil {
				return err
			}
			if aerr := i.assignPattern(ctx, prop.Value, rest, env); aerr != nil {
				return aerr
			}
			continue
		}
		pk, err := i.patternPropertyKey(ctx, prop, env)
		if err != nil {
			return err
		}
		taken[pk] = true
		tgt := prop.Value
		if tgt == nil {
			tgt = prop.Key
		}
		if err := i.assignKeyedProperty(ctx, obj, pk, tgt, env); err != nil {
			return err
		}
	}
	return nil
}

// assignKeyedProperty implements KeyedDestructuringAssignmentEvaluation
// (§13.15.5.6). For a non-pattern DestructuringAssignmentTarget the target
// Reference is evaluated *before* GetV(value, propertyName) pulls the property
// value (step 1 precedes step 2), and an Initializer is evaluated only
// afterwards, when the pulled value is undefined (step 3). The deferred
// PutValue coerces a computed member key at that point, so a target like
// `obj[key()]` reports `key`'s side effects (base, key expression) before the
// get and its ToPropertyKey only after. A nested array/object pattern skips the
// reference step (step 1's guard) and recurses on the pulled value.
func (i *Interpreter) assignKeyedProperty(ctx context.Context, obj Value, pk PropertyKey, tgt ast.Expr, env *Environment) error {
	target, def := splitAssignElement(tgt)
	if isDestructuringPattern(target) {
		v, err := i.getProperty(ctx, obj, pk)
		if err != nil {
			return err
		}
		if def != nil && IsUndefined(v) {
			dv, derr := i.evalExprNamed(ctx, def, env, "")
			if derr != nil {
				return derr
			}
			v = dv
		}
		return i.assignPattern(ctx, target, v, env)
	}
	ref, err := i.evalRef(ctx, target, env)
	if err != nil {
		return err
	}
	v, err := i.getProperty(ctx, obj, pk)
	if err != nil {
		return err
	}
	if def != nil && IsUndefined(v) {
		dv, derr := i.evalExprNamed(ctx, def, env, bindingName(target))
		if derr != nil {
			return derr
		}
		v = dv
	}
	return i.putRefValue(ctx, ref, v)
}

// splitAssignElement separates a destructuring array element into its target and
// optional default initializer.
func splitAssignElement(el ast.Expr) (target ast.Expr, def ast.Expr) {
	switch e := el.(type) {
	case *ast.AssignPattern:
		return e.Target, e.Default
	case *ast.AssignExpr:
		if e.Op == token.ASSIGN {
			return e.Target, e.Value
		}
	}
	return el, nil
}

// isDestructuringPattern reports whether target is itself a nested pattern.
func isDestructuringPattern(target ast.Expr) bool {
	switch target.(type) {
	case *ast.ArrayLit, *ast.ObjectLit:
		return true
	}
	return false
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

// funcNameFromKey renders a property key as the inferred function name for
// NamedEvaluation: a Symbol with a description yields "[desc]"; a Symbol without
// one yields "" (no name applied); a string key yields itself.
func funcNameFromKey(k PropertyKey) string {
	if k.IsSymbol() {
		if k.Sym.Desc != "" {
			return "[" + k.Sym.Desc + "]"
		}
		return ""
	}
	return k.Str
}

// keyName renders a property key for error messages.
func keyName(k PropertyKey) string {
	if k.IsSymbol() {
		return "Symbol(" + k.Sym.Desc + ")"
	}
	return k.Str
}
