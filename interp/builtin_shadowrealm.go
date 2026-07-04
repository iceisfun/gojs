package interp

import (
	"context"
	"math"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/parser"
)

// This file implements the ShadowRealm builtin (the TC39 ShadowRealm proposal).
// A ShadowRealm owns a fresh, fully isolated realm — a second *Interpreter with
// its own global object and complete set of intrinsics. Only primitives and
// callables cross the realm boundary: a callable becomes a "wrapped function", a
// fresh exotic function in the receiving realm that forwards calls to the source
// realm; a non-callable object throws a TypeError. This models §GetWrappedValue,
// WrappedFunctionCreate, and PerformShadowRealmEval from the proposal spec.

// shadowRealmInner returns the inner realm interpreter stored on a ShadowRealm
// instance, or nil when this is not a ShadowRealm object.
func shadowRealmInner(this Value) *Interpreter {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	inner, _ := o.internal["shadowRealm"].(*Interpreter)
	return inner
}

// initShadowRealm installs the ShadowRealm constructor and prototype.
func (i *Interpreter) initShadowRealm() {
	proto := NewObject(i.objectProto)
	proto.class = "ShadowRealm"
	i.shadowRealmProto = proto

	call := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor ShadowRealm requires 'new'")
	}
	construct := func(ctx context.Context, newTarget Value, _ []Value) (Value, error) {
		proto0, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.shadowRealmProto })
		if err != nil {
			return nil, err
		}
		obj := NewObject(proto0)
		obj.class = "ShadowRealm"
		obj.internal = map[string]any{"shadowRealm": i.newInnerRealm()}
		return obj, nil
	}
	ctor := i.newNativeCtor("ShadowRealm", 0, call, construct)
	linkCtor(ctor, proto)

	i.defineMethod(proto, "evaluate", 1, i.shadowRealmEvaluate)
	i.defineMethod(proto, "importValue", 2, i.shadowRealmImportValue)
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("ShadowRealm"), Writable: false, Enumerable: false, Configurable: true})

	i.setGlobalHidden("ShadowRealm", ctor)
}

// newInnerRealm builds the isolated realm backing one ShadowRealm instance: a
// second interpreter sharing this realm's global Symbol registry (so Symbol.for
// crosses realms) and inheriting its module provider (so importValue resolves
// the same modules). It is registered for cleanup when this realm is closed.
func (i *Interpreter) newInnerRealm() *Interpreter {
	inner := New(func(child *Interpreter) {
		child.symByKey = i.symByKey
		child.symBySym = i.symBySym
		if i.moduleProvider != nil {
			child.moduleProvider = i.moduleProvider
		}
	})
	i.childRealms = append(i.childRealms, inner)
	return inner
}

// shadowRealmEvaluate implements ShadowRealm.prototype.evaluate: it runs the
// source text (which must be a string) as global code in the inner realm and
// returns the wrapped completion value. A parse or runtime error, or a
// non-wrappable result, is reported as a TypeError in the *caller* realm (i).
func (i *Interpreter) shadowRealmEvaluate(ctx context.Context, this Value, args []Value) (Value, error) {
	inner := shadowRealmInner(this)
	if inner == nil {
		return nil, i.throwError(ctx, "TypeError", "ShadowRealm.prototype.evaluate called on a non-ShadowRealm object")
	}
	srcV := arg(args, 0)
	src, ok := srcV.(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "ShadowRealm.prototype.evaluate expects a string")
	}
	// A parse failure of the source is a SyntaxError in the caller realm
	// (PerformShadowRealmEval), distinct from a runtime error which becomes a
	// caller-realm TypeError.
	inner.registerSource("<shadowrealm>", string(src))
	prog, perr := parser.Parse("<shadowrealm>", string(src))
	if perr != nil {
		return nil, i.throwError(ctx, "SyntaxError", perr.Error())
	}
	result, rerr := inner.shadowRun(prog)
	if rerr != nil {
		return nil, i.wrapCrossRealmError(ctx, rerr)
	}
	return i.wrapForRealm(ctx, i, inner, result)
}

// shadowRun evaluates a parsed program in this (inner) realm using the
// PerformShadowRealmEval environment setup: a fresh declarative lexical
// environment whose outer scope is the global environment, so top-level
// let/const declarations do NOT persist across evaluate calls (they cannot
// conflict between successive evaluations), while non-strict var/function
// declarations still target the global environment. The event loop is not
// drained — evaluate is synchronous.
func (inner *Interpreter) shadowRun(prog *ast.Program) (Value, error) {
	lexEnv := NewEnvironment(inner.globalEnv, false)
	lexEnv.strict = prog.Strict
	varEnv := inner.globalEnv
	if prog.Strict {
		varEnv = lexEnv
	}
	inner.steps = 0
	if err := inner.evalDeclarationInstantiation(inner.ctx, prog.Body, varEnv, lexEnv, prog.Strict); err != nil {
		return nil, err
	}
	return inner.execStmts(inner.ctx, prog.Body, lexEnv)
}

// wrapCrossRealmError converts an error raised in another realm into a TypeError
// in realm i: the original error object must not cross the boundary, so only a
// fresh caller-realm TypeError (with a descriptive message) is surfaced.
func (i *Interpreter) wrapCrossRealmError(ctx context.Context, err error) error {
	if v, ok := ThrownValue(err); ok {
		msg := "ShadowRealm evaluation failed"
		if s, e := i.ToString(v); e == nil && s != "" {
			msg = s
		}
		return i.throwError(ctx, "TypeError", msg)
	}
	return err
}

// wrapForRealm implements GetWrappedValue: a value produced in realm `home` is
// prepared for use in realm i. A primitive passes through unchanged; a callable
// becomes a wrapped function whose [[Prototype]] comes from realm `dest`'s
// %Function.prototype% and that invokes the callable back in `home`; any other
// object throws a TypeError in realm i (the realm performing the crossing).
func (i *Interpreter) wrapForRealm(ctx context.Context, dest, home *Interpreter, value Value) (Value, error) {
	if isPrimitive(value) {
		return value, nil
	}
	if o, ok := value.(*Object); ok && o.IsCallable() {
		w, err := dest.wrappedFunctionCreate(home, o)
		if err != nil {
			// A throwing "length"/"name" accessor (or a revoked/throwing proxy)
			// makes WrappedFunctionCreate abrupt, surfacing as a TypeError in the
			// realm that performed the crossing.
			return nil, i.wrapCrossRealmError(ctx, err)
		}
		return w, nil
	}
	return nil, i.throwError(ctx, "TypeError", "ShadowRealm boundary: value must be a primitive or a callable")
}

// wrappedFunctionCreate builds a wrapped function in realm dest (== the receiver)
// over target, a callable living in realm home. The wrapper belongs to dest, so
// dest is its [[Realm]]: every TypeError it raises (a non-wrappable argument or
// return, or a thrown target) is a dest-realm TypeError. Calls forward through
// the boundary: arguments are wrapped from dest into home, the return value is
// wrapped from home back into dest. It fails when copying the target's
// name/length is abrupt (a throwing accessor or proxy trap).
func (dest *Interpreter) wrappedFunctionCreate(home *Interpreter, target *Object) (*Object, error) {
	w := dest.newNativeFunc("", 0, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		wrapped := make([]Value, len(args))
		for k, a := range args {
			// Arguments flow from dest (where the wrapper runs) into home; a
			// non-wrappable argument throws dest's TypeError.
			wa, err := dest.wrapForRealm(ctx, home, dest, a)
			if err != nil {
				return nil, err
			}
			wrapped[k] = wa
		}
		ret, cerr := target.fn.call(home.ctx, Undef, wrapped)
		if cerr != nil {
			return nil, dest.wrapCrossRealmError(ctx, cerr)
		}
		return dest.wrapForRealm(ctx, dest, home, ret)
	})
	// CopyNameAndLength: mirror the target's "length" and "name". A throwing
	// accessor or proxy trap propagates (WrappedFunctionCreate then throws).
	if err := dest.copyWrappedNameAndLength(w, home, target); err != nil {
		return nil, err
	}
	return w, nil
}

// copyWrappedNameAndLength copies target's own "length" and "name" onto the
// wrapper w (CopyNameAndLength, §GetWrappedValue). "length" is set to the
// target's length as an integer or +Infinity (SetFunctionLength); "name" to its
// string name (or ""). A [[Get]] that throws is propagated to the caller.
func (dest *Interpreter) copyWrappedNameAndLength(w *Object, home *Interpreter, target *Object) error {
	length := Number(0)
	// HasOwnProperty(target, "length") — this triggers a proxy's
	// getOwnPropertyDescriptor trap, whose throw must propagate (§SetFunctionLength
	// step, reached via CopyNameAndLength).
	desc, hasLen, err := home.getOwnPropertyV(home.ctx, target, StrKey("length"))
	if err != nil {
		return err
	}
	if hasLen && desc != nil {
		lv, err := target.GetStr(home.ctx, "length")
		if err != nil {
			return err
		}
		if n, e := home.ToNumberV(home.ctx, lv); e == nil {
			f := float64(n)
			switch {
			case math.IsInf(f, 1):
				length = Number(f) // +Infinity is preserved
			case f > 0:
				length = Number(ToInteger(n))
			}
		}
	}
	nv, err := target.GetStr(home.ctx, "name")
	if err != nil {
		return err
	}
	name := ""
	if s, ok := nv.(String); ok {
		name = string(s)
	}
	w.defineOwn(StrKey("length"), &Property{Value: length, Writable: false, Enumerable: false, Configurable: true})
	w.defineOwn(StrKey("name"), &Property{Value: String(name), Writable: false, Enumerable: false, Configurable: true})
	if w.fn != nil {
		w.fn.name = name
	}
	return nil
}

// shadowRealmImportValue implements ShadowRealm.prototype.importValue: it imports
// specifier as a module in the inner realm and returns a caller-realm promise
// resolving to the wrapped value of the module's exportName binding.
func (i *Interpreter) shadowRealmImportValue(ctx context.Context, this Value, args []Value) (Value, error) {
	inner := shadowRealmInner(this)
	if inner == nil {
		return nil, i.throwError(ctx, "TypeError", "ShadowRealm.prototype.importValue called on a non-ShadowRealm object")
	}
	specifier, err := i.ToStringV(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	exportNameV := arg(args, 1)
	exportName, ok := exportNameV.(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "ShadowRealm.prototype.importValue expects a string export name")
	}

	pObj, resolve, reject := i.newPromise()
	nsV, ierr := inner.importModuleNamespace(inner.ctx, specifier)
	if ierr != nil {
		reject(i.crossRealmErrorValue(ctx, ierr))
		return pObj, nil
	}
	ns, _ := nsV.(*Object)
	if ns == nil {
		reject(i.newError("TypeError", "ShadowRealm importValue: module has no namespace"))
		return pObj, nil
	}
	if has, herr := inner.hasV(inner.ctx, ns, StrKey(string(exportName))); herr != nil || !has {
		reject(i.newError("TypeError", "ShadowRealm importValue: module has no export named '"+string(exportName)+"'"))
		return pObj, nil
	}
	val, gerr := ns.GetStr(inner.ctx, string(exportName))
	if gerr != nil {
		reject(i.crossRealmErrorValue(ctx, gerr))
		return pObj, nil
	}
	wrapped, werr := i.wrapForRealm(ctx, i, inner, val)
	if werr != nil {
		reject(i.crossRealmErrorValue(ctx, werr))
		return pObj, nil
	}
	resolve(wrapped)
	return pObj, nil
}

// crossRealmErrorValue renders a cross-realm error as a caller-realm TypeError
// value (for rejecting a promise), never letting the foreign error object cross.
func (i *Interpreter) crossRealmErrorValue(ctx context.Context, err error) Value {
	msg := "ShadowRealm importValue failed"
	if v, ok := ThrownValue(err); ok {
		if s, e := i.ToString(v); e == nil && s != "" {
			msg = s
		}
	}
	return i.newError("TypeError", msg)
}
