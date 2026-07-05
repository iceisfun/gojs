package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
)

// A reference models an ECMA-262 Reference Record (§6.2.5): the resolved target
// of an assignment. It exists so that a compound or logical assignment can
// resolve its LeftHandSideExpression exactly once — evaluating the base and any
// computed property key up front — and then perform GetValue and PutValue on
// that same reference. Re-resolving the target between the read and the write
// would consult the wrong binding when the right-hand side mutates the
// environment (e.g. `x *= (eval("var x = 2;"), 4)` or a `with`-scope getter that
// deletes the bound property).
type refKind int

const (
	refIdentBinding    refKind = iota // a declarative environment binding
	refIdentWith                      // an object environment record (a `with` object)
	refIdentGlobal                    // a resolvable name on the global object
	refIdentUnresolved                // a name bound nowhere
	refProp                           // an ordinary property reference base[key]
	refSuperProp                      // super.key
	refPrivate                        // base.#name
)

type reference struct {
	kind   refKind
	strict bool

	// Identifier references.
	name    string
	binding *binding
	obj     *Object // the with/global binding object

	// Property references.
	base    Value
	keyVal  Value       // computed property-name value, before ToPropertyKey
	key     PropertyKey // resolved key (valid once keyDone is true)
	keyDone bool

	// super.key references: base is GetSuperBase (the home object's prototype, or
	// null), thisVal is the actualThis receiver.
	thisVal Value

	// Private references.
	priv *PrivateName
}

// evalRef resolves an assignment target to a reference, evaluating the base and
// (for a computed member) the property-name expression once, left to right.
// ToPropertyKey on a computed key is deferred to GetValue/PutValue so that a
// nullish base rejects (via ToObject) before the key is coerced (§13.15.2, and
// the note at EvaluatePropertyAccessWithExpressionKey).
func (i *Interpreter) evalRef(ctx context.Context, target ast.Expr, env *Environment) (*reference, error) {
	switch t := target.(type) {
	case *ast.Ident:
		return i.resolveIdentRef(ctx, t.Name, env)
	case *ast.MemberExpr:
		if _, ok := t.Object.(*ast.SuperExpr); ok {
			home := env.homeObject()
			if home == nil {
				return nil, i.throwError(ctx, "ReferenceError", "'super' keyword unexpected here")
			}
			// GetThisBinding (ReferenceError when uninitialized) precedes evaluation
			// of a computed key (§13.3.7.1 steps 1-3).
			thisVal, err := i.getThisBinding(ctx, env)
			if err != nil {
				return nil, err
			}
			ref := &reference{kind: refSuperProp, strict: env.isStrict(), thisVal: thisVal}
			if err := i.setRefKey(ctx, ref, t, env); err != nil {
				return nil, err
			}
			// GetSuperBase snapshot (§9.1.1.3.5), captured after the key expression
			// but before the deferred ToPropertyKey. A null base (extends null) is a
			// TypeError only once GetValue/PutValue coerces it via ToObject.
			if home.proto != nil {
				ref.base = home.proto
			} else {
				ref.base = Nul
			}
			return ref, nil
		}
		base, err := i.evalExpr(ctx, t.Object, env)
		if err != nil {
			return nil, err
		}
		if priv, ok := t.Property.(*ast.PrivateIdent); ok && !t.Computed {
			return &reference{
				kind:   refPrivate,
				strict: env.isStrict(),
				base:   base,
				priv:   env.resolvePrivate(priv.Name),
				name:   priv.Name,
			}, nil
		}
		ref := &reference{kind: refProp, strict: env.isStrict(), base: base}
		if err := i.setRefKey(ctx, ref, t, env); err != nil {
			return nil, err
		}
		return ref, nil
	default:
		return nil, i.throwError(ctx, "SyntaxError", "invalid assignment target")
	}
}

// setRefKey evaluates a member expression's property key onto ref. A computed
// key is left as a raw value (keyDone false) for lazy ToPropertyKey; a literal
// key is stored resolved.
func (i *Interpreter) setRefKey(ctx context.Context, ref *reference, e *ast.MemberExpr, env *Environment) error {
	if e.Computed {
		v, err := i.evalExpr(ctx, e.Property, env)
		if err != nil {
			return err
		}
		ref.keyVal = v
		ref.keyDone = false
		return nil
	}
	switch p := e.Property.(type) {
	case *ast.Ident:
		ref.key, ref.keyDone = StrKey(p.Name), true
	case *ast.PrivateIdent:
		ref.key, ref.keyDone = StrKey(p.Name), true
	default:
		return i.throwError(ctx, "SyntaxError", "invalid member expression")
	}
	return nil
}

// resolveKey performs ToPropertyKey once, memoizing the result so a compound
// assignment's GetValue and PutValue observe a single coercion (§13.15.2 A7_T4).
func (i *Interpreter) resolveKey(ctx context.Context, ref *reference) (PropertyKey, error) {
	if ref.keyDone {
		return ref.key, nil
	}
	k, err := i.ToPropertyKey(ctx, ref.keyVal)
	if err != nil {
		return PropertyKey{}, err
	}
	ref.key, ref.keyDone = k, true
	return k, nil
}

// resolveIdentRef resolves an identifier to a reference, walking the scope chain
// exactly as resolveIdent/assignIdent do: object environment records (`with`)
// are interleaved with declarative bindings so the innermost binder wins.
func (i *Interpreter) resolveIdentRef(ctx context.Context, name string, env *Environment) (*reference, error) {
	strict := env.isStrict()
	for e := env; e != nil; e = e.parent {
		if e.withObj != nil {
			obj, ok, err := i.withHasBinding(ctx, e.withObj, name)
			if err != nil {
				return nil, err
			}
			if ok {
				return &reference{kind: refIdentWith, strict: strict, name: name, obj: obj}, nil
			}
		}
		if b, ok := e.vars[name]; ok {
			return &reference{kind: refIdentBinding, strict: strict, name: name, binding: b}, nil
		}
	}
	if i.global.HasOwn(StrKey(name)) || i.global.Has(StrKey(name)) {
		return &reference{kind: refIdentGlobal, strict: strict, name: name, obj: i.global}, nil
	}
	return &reference{kind: refIdentUnresolved, strict: strict, name: name}, nil
}

// getRefValue implements GetValue(ref) (§6.2.5.5).
func (i *Interpreter) getRefValue(ctx context.Context, ref *reference) (Value, error) {
	switch ref.kind {
	case refIdentBinding:
		b := ref.binding
		if !b.initialized {
			return nil, i.throwError(ctx, "ReferenceError", "Cannot access '"+ref.name+"' before initialization")
		}
		return b.value, nil
	case refIdentWith:
		return i.withGetBindingValue(ctx, ref.obj, ref.name, ref.strict)
	case refIdentGlobal:
		return i.global.GetStr(ctx, ref.name)
	case refIdentUnresolved:
		return nil, i.throwError(ctx, "ReferenceError", ref.name+" is not defined")
	case refPrivate:
		return i.getPrivateMember(ctx, ref.base, ref.priv, ref.name)
	case refSuperProp:
		// GetValue on a Super Reference: ToObject(base) rejects a null base before
		// ToPropertyKey (§6.2.5.5), then [[Get]] runs with `this` as the receiver.
		base, ok := ref.base.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Cannot read properties of "+briefValue(ref.base))
		}
		key, err := i.resolveKey(ctx, ref)
		if err != nil {
			return nil, err
		}
		return base.getWithReceiver(ctx, key, ref.thisVal)
	case refProp:
		// ToObject rejects a nullish base before the computed key is coerced, so
		// resolve the key only after the base is known object-coercible.
		if IsNullish(ref.base) {
			return nil, i.throwError(ctx, "TypeError", "Cannot read properties of "+briefValue(ref.base)+i.refKeyHint(ref, "reading"))
		}
		// Fast path: TypedArray element read with a numeric index. A Number key is
		// always a canonical numeric index (ToString(n) round-trips), so [[Get]]
		// never consults the prototype — it returns the element or undefined. This
		// avoids ToString on the index and CanonicalNumericIndexString's string↔
		// float round-trip, the dominant cost of tight typed-array loops.
		if !ref.keyDone {
			if n, ok := ref.keyVal.(Number); ok {
				if o, ok := ref.base.(*Object); ok && o.typedArray != nil {
					if idx, valid := o.typedArray.validIndex(normalizeIndex(float64(n))); valid {
						return o.typedArray.getElement(idx), nil
					}
					return Undef, nil
				}
			}
		}
		key, err := i.resolveKey(ctx, ref)
		if err != nil {
			return nil, err
		}
		return i.getProperty(ctx, ref.base, key)
	default:
		return nil, i.throwError(ctx, "SyntaxError", "invalid assignment target")
	}
}

// putRefValue implements PutValue(ref, value) (§6.2.5.6).
func (i *Interpreter) putRefValue(ctx context.Context, ref *reference, value Value) error {
	switch ref.kind {
	case refIdentBinding:
		b := ref.binding
		if !b.initialized {
			return i.throwError(ctx, "ReferenceError", "Cannot access '"+ref.name+"' before initialization")
		}
		if !b.mutable {
			// A non-strict immutable binding (a named function expression's own
			// name) swallows the assignment in sloppy code and throws only in
			// strict code; const's immutable binding is strict and always throws.
			if b.weakImmutable && !ref.strict {
				return nil
			}
			return i.throwError(ctx, "TypeError", "Assignment to constant variable.")
		}
		b.value = value
		return nil
	case refIdentWith, refIdentGlobal:
		// Object environment record SetMutableBinding (§9.1.1.2.5), which also
		// backs the global object record: if the bound property no longer exists
		// and the reference is strict, throw a ReferenceError; otherwise write
		// through.
		has, err := i.hasV(ctx, ref.obj, StrKey(ref.name))
		if err != nil {
			return err
		}
		if !has && ref.strict {
			return i.throwError(ctx, "ReferenceError", ref.name+" is not defined")
		}
		wrote, err := ref.obj.setStatus(ctx, StrKey(ref.name), value)
		if err != nil {
			return err
		}
		if !wrote && ref.strict {
			return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+ref.name)
		}
		return nil
	case refIdentUnresolved:
		if ref.strict {
			return i.throwError(ctx, "ReferenceError", ref.name+" is not defined")
		}
		return i.global.SetStr(ctx, ref.name, value)
	case refPrivate:
		return i.setPrivateMember(ctx, ref.base, ref.priv, ref.name, value)
	case refSuperProp:
		return i.putSuperRef(ctx, ref, value)
	case refProp:
		if IsNullish(ref.base) {
			return i.throwError(ctx, "TypeError", "Cannot set properties of "+briefValue(ref.base)+i.refKeyHint(ref, "setting"))
		}
		// Fast path mirroring the read side: a TypedArray [[Set]] with a numeric
		// index writes through TypedArraySetElement (which still coerces the value,
		// possibly running user code) and always "succeeds", never touching the
		// prototype. Skips index ToString + CanonicalNumericIndexString.
		if !ref.keyDone {
			if n, ok := ref.keyVal.(Number); ok {
				if o, ok := ref.base.(*Object); ok && o.typedArray != nil {
					_, err := i.typedArraySetElement(ctx, o.typedArray, normalizeIndex(float64(n)), value)
					return err
				}
			}
		}
		key, err := i.resolveKey(ctx, ref)
		if err != nil {
			return err
		}
		obj, ok := ref.base.(*Object)
		if !ok {
			// §6.2.5.6 PutValue step 6a: with a primitive base, set base to
			// ToObject(base) and invoke the wrapper's [[Set]] with the original
			// primitive as the receiver, so an inherited setter or a Proxy on the
			// wrapper prototype fires. The write itself never sticks (the receiver
			// is not an Object), so a failed [[Set]] is a TypeError only in strict
			// mode.
			wrapper, err := i.ToObject(ctx, ref.base)
			if err != nil {
				return err
			}
			wrote, err := i.setV(ctx, wrapper, key, value, ref.base)
			if err != nil {
				return err
			}
			if !wrote && ref.strict {
				return i.throwError(ctx, "TypeError", "Cannot create property "+keyName(key)+" on "+briefValue(ref.base))
			}
			return nil
		}
		wrote, err := obj.setStatus(ctx, key, value)
		if err != nil {
			return err
		}
		if !wrote && ref.strict {
			return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+keyName(key)+" of "+briefValue(ref.base))
		}
		return nil
	default:
		return i.throwError(ctx, "SyntaxError", "invalid assignment target")
	}
}

// getPropByName is getRefValue's refProp read path specialized for a static,
// already-resolved property name (a member expression like `o.foo`, never a
// computed `o[x]`). It reproduces the nullish-base check and [[Get]] exactly but
// takes the base and name directly, so the hot bytecode opGetProp opcode avoids
// heap-allocating a reference on every property read.
func (i *Interpreter) getPropByName(ctx context.Context, base Value, name string) (Value, error) {
	if IsNullish(base) {
		return nil, i.throwError(ctx, "TypeError", "Cannot read properties of "+briefValue(base)+" (reading '"+name+"')")
	}
	return i.getProperty(ctx, base, StrKey(name))
}

// setPropByName is putRefValue's refProp write path specialized for a static
// property name, avoiding the per-write reference allocation. It mirrors the
// primitive-base wrapper, setStatus, and strict-mode read-only handling of the
// reference path exactly.
func (i *Interpreter) setPropByName(ctx context.Context, base Value, name string, value Value, strict bool) error {
	if IsNullish(base) {
		return i.throwError(ctx, "TypeError", "Cannot set properties of "+briefValue(base)+" (setting '"+name+"')")
	}
	key := StrKey(name)
	obj, ok := base.(*Object)
	if !ok {
		wrapper, err := i.ToObject(ctx, base)
		if err != nil {
			return err
		}
		wrote, err := i.setV(ctx, wrapper, key, value, base)
		if err != nil {
			return err
		}
		if !wrote && strict {
			return i.throwError(ctx, "TypeError", "Cannot create property "+keyName(key)+" on "+briefValue(base))
		}
		return nil
	}
	wrote, err := obj.setStatus(ctx, key, value)
	if err != nil {
		return err
	}
	if !wrote && strict {
		return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+keyName(key)+" of "+briefValue(base))
	}
	return nil
}

// putSuperRef implements PutValue on a Super Reference (§6.2.5.6): ToObject(base)
// rejects a null base, then [[Set]] runs with `this` as the receiver (so an
// inherited setter fires, else the value becomes an own property of `this`); a
// failed write is a TypeError in strict mode.
func (i *Interpreter) putSuperRef(ctx context.Context, ref *reference, value Value) error {
	base, ok := ref.base.(*Object)
	if !ok {
		return i.throwError(ctx, "TypeError", "Cannot set properties of "+briefValue(ref.base))
	}
	key, err := i.resolveKey(ctx, ref)
	if err != nil {
		return err
	}
	succeeded, err := i.setV(ctx, base, key, value, ref.thisVal)
	if err != nil {
		return err
	}
	if !succeeded && ref.strict {
		return i.throwError(ctx, "TypeError", "Cannot assign to read-only property "+keyName(key))
	}
	return nil
}

// refKeyHint produces the " (reading 'x')" / " (setting 'x')" suffix for a
// nullish-base property error, but only when the key is already known; a
// still-uncoerced computed key must not be forced through ToPropertyKey here.
func (i *Interpreter) refKeyHint(ref *reference, verb string) string {
	if !ref.keyDone {
		return ""
	}
	return " (" + verb + " '" + keyName(ref.key) + "')"
}
