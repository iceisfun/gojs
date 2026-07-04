package interp

import "context"

// This file implements WeakRef (§26.1) and FinalizationRegistry (§26.2).
//
// # No collection ever happens
//
// gojs has no garbage-collection finalization hook, so a WeakRef target is held
// strongly for the lifetime of the WeakRef and deref() always returns it: the
// referent is never cleared. Likewise a FinalizationRegistry never observes a
// target being reclaimed, so registered cleanup callbacks never fire. Both are
// conforming behaviors — ECMA-262 explicitly permits an implementation that
// never reclaims (§26.1 note, §9.10.3: "not all objects... must be part of the
// live set" and cleanup is "unpredictable"). Only the bookkeeping and argument
// validation are observable, and those are implemented precisely. See
// wontfix/weak-references-never-cleared for the rationale.

// weakRefTarget extracts the stored target from a WeakRef instance, reporting
// whether this is a WeakRef with the internal slot present.
func weakRefTarget(this Value) (Value, bool) {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil, false
	}
	t, ok := o.internal["WeakRefTarget"].(Value)
	return t, ok
}

// initWeakRef installs the WeakRef constructor and WeakRef.prototype (§26.1).
func (i *Interpreter) initWeakRef() {
	proto := NewObject(i.objectProto)
	proto.class = "WeakRef"
	i.weakRefProto = proto

	wrCall := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor WeakRef requires 'new'")
	}
	wrConstruct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		target := arg(args, 0)
		if !canBeHeldWeakly(target) {
			return nil, i.throwError(ctx, "TypeError", "WeakRef: target cannot be held weakly")
		}
		proto0, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.weakRefProto })
		if err != nil {
			return nil, err
		}
		obj := NewObject(proto0)
		obj.class = "WeakRef"
		// Store the target as a strong reference: gojs never clears it.
		obj.internal = map[string]any{"WeakRefTarget": target}
		return obj, nil
	}

	ctor := i.newNativeCtor("WeakRef", 1, wrCall, wrConstruct)
	linkCtor(ctor, proto)

	// deref() → the target (never undefined here, since we never collect). §26.1.3.2
	i.defineMethod(proto, "deref", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		target, ok := weakRefTarget(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "WeakRef.prototype.deref called on incompatible receiver")
		}
		return target, nil
	})

	// WeakRef.prototype[Symbol.toStringTag] = "WeakRef" (§26.1.3.3).
	proto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value:        String("WeakRef"),
		Writable:     false,
		Enumerable:   false,
		Configurable: true,
	})

	i.setGlobalHidden("WeakRef", ctor)
}

// finalizationCell is one registration in a FinalizationRegistry: a weakly-held
// target, a held value passed to the cleanup callback, and an optional
// unregister token. gojs never fires cleanup, so cells only ever leave the list
// via unregister().
type finalizationCell struct {
	target          Value
	heldValue       Value
	hasToken        bool
	unregisterToken Value
}

// finalizationRegistry is the internal state of a FinalizationRegistry: the
// cleanup callback (never invoked) and the list of live registrations.
type finalizationRegistry struct {
	cleanup Value
	cells   []finalizationCell
}

// finRegistrySlot extracts the *finalizationRegistry backing an instance.
func finRegistrySlot(this Value) *finalizationRegistry {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	r, _ := o.internal["FinalizationRegistry"].(*finalizationRegistry)
	return r
}

// initFinalizationRegistry installs FinalizationRegistry and its prototype
// (§26.2).
func (i *Interpreter) initFinalizationRegistry() {
	proto := NewObject(i.objectProto)
	proto.class = "FinalizationRegistry"
	i.finalizationRegistryProto = proto

	frCall := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor FinalizationRegistry requires 'new'")
	}
	frConstruct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		cb := arg(args, 0)
		if co, ok := cb.(*Object); !ok || !co.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "FinalizationRegistry: cleanup callback is not callable")
		}
		proto0, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.finalizationRegistryProto })
		if err != nil {
			return nil, err
		}
		obj := NewObject(proto0)
		obj.class = "FinalizationRegistry"
		obj.internal = map[string]any{"FinalizationRegistry": &finalizationRegistry{cleanup: cb}}
		return obj, nil
	}

	ctor := i.newNativeCtor("FinalizationRegistry", 1, frCall, frConstruct)
	linkCtor(ctor, proto)

	// register(target, heldValue [, unregisterToken]) → undefined (§26.2.3.1).
	i.defineMethod(proto, "register", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		r := finRegistrySlot(this)
		if r == nil {
			return nil, i.throwError(ctx, "TypeError", "FinalizationRegistry.prototype.register called on incompatible receiver")
		}
		target := arg(args, 0)
		if !canBeHeldWeakly(target) {
			return nil, i.throwError(ctx, "TypeError", "FinalizationRegistry.register: target cannot be held weakly")
		}
		heldValue := arg(args, 1)
		if sameValue(target, heldValue) {
			return nil, i.throwError(ctx, "TypeError", "FinalizationRegistry.register: target and heldValue must not be the same")
		}
		token := arg(args, 2)
		hasToken := false
		if !IsUndefined(token) {
			if !canBeHeldWeakly(token) {
				return nil, i.throwError(ctx, "TypeError", "FinalizationRegistry.register: unregisterToken cannot be held weakly")
			}
			hasToken = true
		}
		r.cells = append(r.cells, finalizationCell{
			target:          target,
			heldValue:       heldValue,
			hasToken:        hasToken,
			unregisterToken: token,
		})
		return Undef, nil
	})

	// unregister(unregisterToken) → boolean (§26.2.3.2). Removes every cell whose
	// token is SameValue to the argument.
	i.defineMethod(proto, "unregister", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		r := finRegistrySlot(this)
		if r == nil {
			return nil, i.throwError(ctx, "TypeError", "FinalizationRegistry.prototype.unregister called on incompatible receiver")
		}
		token := arg(args, 0)
		if !canBeHeldWeakly(token) {
			return nil, i.throwError(ctx, "TypeError", "FinalizationRegistry.unregister: unregisterToken cannot be held weakly")
		}
		removed := false
		kept := r.cells[:0]
		for _, c := range r.cells {
			if c.hasToken && sameValue(c.unregisterToken, token) {
				removed = true
				continue
			}
			kept = append(kept, c)
		}
		r.cells = kept
		return Bool(removed), nil
	})

	// FinalizationRegistry.prototype[Symbol.toStringTag] = "FinalizationRegistry"
	// (§26.2.3.4).
	proto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value:        String("FinalizationRegistry"),
		Writable:     false,
		Enumerable:   false,
		Configurable: true,
	})

	i.setGlobalHidden("FinalizationRegistry", ctor)
}
