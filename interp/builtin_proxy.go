package interp

import "context"

// This file implements the Proxy exotic object (ECMA-262 §10.5) and the Proxy
// constructor / Proxy.revocable (§28.2). A Proxy routes each essential internal
// method through a trap on its handler, falling back to the target when the trap
// is absent, and enforces the core invariants plus the revoked-proxy TypeError.

// proxyState holds a Proxy's [[ProxyTarget]] and [[ProxyHandler]] slots. A
// revoked proxy has handler == nil (and target == nil), so every trap dispatch
// first raises a TypeError.
type proxyState struct {
	i       *Interpreter
	target  *Object
	handler *Object
}

// revoked reports whether the proxy has been revoked.
func (p *proxyState) revoked() bool { return p.handler == nil }

// checkRevoked throws a TypeError when the proxy has been revoked.
func (p *proxyState) checkRevoked(ctx context.Context) error {
	if p.revoked() {
		return p.i.throwError(ctx, "TypeError", "Cannot perform operation on a revoked proxy")
	}
	return nil
}

// trap returns the named handler trap, or nil when it is absent (undefined or
// null). A present but non-callable trap is a TypeError. Assumes not revoked.
func (p *proxyState) trap(ctx context.Context, name string) (*Object, error) {
	v, err := p.handler.GetStr(ctx, name)
	if err != nil {
		return nil, err
	}
	if IsNullish(v) {
		return nil, nil
	}
	fn, ok := v.(*Object)
	if !ok || !fn.IsCallable() {
		return nil, p.i.throwError(ctx, "TypeError", "'"+name+"' trap is not a function")
	}
	return fn, nil
}

// ---------------------------------------------------------------------------
// Essential internal methods
// ---------------------------------------------------------------------------

// get implements [[Get]] (§10.5.8).
func (p *proxyState) get(ctx context.Context, key PropertyKey, receiver Value) (Value, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return nil, err
	}
	i := p.i
	tr, err := p.trap(ctx, "get")
	if err != nil {
		return nil, err
	}
	if tr == nil {
		return i.getV(ctx, p.target, key, receiver)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, keyToValue(key), receiver})
	if err != nil {
		return nil, err
	}
	// Invariant: a non-configurable, non-writable data property must report its
	// exact value; a non-configurable accessor with no getter must report
	// undefined.
	if td, ok := p.target.getOwn(key); ok && !td.Configurable {
		if !td.Accessor && !td.Writable && !sameValue(res, td.Value) {
			return nil, i.throwError(ctx, "TypeError", "proxy get: inconsistent non-configurable non-writable property")
		}
		if td.Accessor && td.Get == nil && !IsUndefined(res) {
			return nil, i.throwError(ctx, "TypeError", "proxy get: non-configurable accessor without getter must report undefined")
		}
	}
	return res, nil
}

// set implements [[Set]] (§10.5.9), returning whether the write succeeded.
func (p *proxyState) set(ctx context.Context, key PropertyKey, v, receiver Value) (bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "set")
	if err != nil {
		return false, err
	}
	if tr == nil {
		return i.setV(ctx, p.target, key, v, receiver)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, keyToValue(key), v, receiver})
	if err != nil {
		return false, err
	}
	if !ToBoolean(res) {
		return false, nil
	}
	if td, ok := p.target.getOwn(key); ok && !td.Configurable {
		if !td.Accessor && !td.Writable && !sameValue(v, td.Value) {
			return false, i.throwError(ctx, "TypeError", "proxy set: cannot change a non-configurable non-writable property")
		}
		if td.Accessor && td.Set == nil {
			return false, i.throwError(ctx, "TypeError", "proxy set: cannot set through a non-configurable accessor without a setter")
		}
	}
	return true, nil
}

// has implements [[HasProperty]] (§10.5.7).
func (p *proxyState) has(ctx context.Context, key PropertyKey) (bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "has")
	if err != nil {
		return false, err
	}
	if tr == nil {
		return i.hasV(ctx, p.target, key)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, keyToValue(key)})
	if err != nil {
		return false, err
	}
	if !ToBoolean(res) {
		if td, ok := p.target.getOwn(key); ok {
			if !td.Configurable {
				return false, i.throwError(ctx, "TypeError", "proxy has: cannot report a non-configurable property as absent")
			}
			if !p.target.extensible {
				return false, i.throwError(ctx, "TypeError", "proxy has: cannot report a property of a non-extensible target as absent")
			}
		}
	}
	return ToBoolean(res), nil
}

// deleteProperty implements [[Delete]] (§10.5.10).
func (p *proxyState) deleteProperty(ctx context.Context, key PropertyKey) (bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "deleteProperty")
	if err != nil {
		return false, err
	}
	if tr == nil {
		return i.deleteV(ctx, p.target, key)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, keyToValue(key)})
	if err != nil {
		return false, err
	}
	if !ToBoolean(res) {
		return false, nil
	}
	if td, ok := p.target.getOwn(key); ok && !td.Configurable {
		return false, i.throwError(ctx, "TypeError", "proxy deleteProperty: cannot delete a non-configurable property")
	}
	return true, nil
}

// getOwnProperty implements [[GetOwnProperty]] (§10.5.5).
func (p *proxyState) getOwnProperty(ctx context.Context, key PropertyKey) (*Property, bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return nil, false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "getOwnPropertyDescriptor")
	if err != nil {
		return nil, false, err
	}
	if tr == nil {
		return i.getOwnPropertyV(ctx, p.target, key)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, keyToValue(key)})
	if err != nil {
		return nil, false, err
	}
	targetDesc, hasTarget := p.target.getOwn(key)
	if IsUndefined(res) {
		if hasTarget {
			if !targetDesc.Configurable {
				return nil, false, i.throwError(ctx, "TypeError", "proxy getOwnPropertyDescriptor: cannot report a non-configurable property as non-existent")
			}
			if !p.target.extensible {
				return nil, false, i.throwError(ctx, "TypeError", "proxy getOwnPropertyDescriptor: cannot report a property of a non-extensible target as non-existent")
			}
		}
		return nil, false, nil
	}
	descObj, ok := res.(*Object)
	if !ok {
		return nil, false, i.throwError(ctx, "TypeError", "proxy getOwnPropertyDescriptor: trap must return an object or undefined")
	}
	desc, err := i.toPropertyDescriptor(ctx, descObj)
	if err != nil {
		return nil, false, err
	}
	if !desc.Configurable {
		if !hasTarget || targetDesc.Configurable {
			return nil, false, i.throwError(ctx, "TypeError", "proxy getOwnPropertyDescriptor: cannot report a non-existent or configurable property as non-configurable")
		}
	}
	return desc, true, nil
}

// ownKeys implements [[OwnPropertyKeys]] (§10.5.11).
func (p *proxyState) ownKeys(ctx context.Context) ([]PropertyKey, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return nil, err
	}
	i := p.i
	tr, err := p.trap(ctx, "ownKeys")
	if err != nil {
		return nil, err
	}
	if tr == nil {
		return i.ownKeysV(ctx, p.target)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target})
	if err != nil {
		return nil, err
	}
	arr, ok := res.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "proxy ownKeys: trap must return an array-like object")
	}
	list, err := i.createListFromArrayLike(ctx, arr)
	if err != nil {
		return nil, err
	}
	keys := make([]PropertyKey, 0, len(list))
	seen := map[PropertyKey]bool{}
	for _, v := range list {
		switch k := v.(type) {
		case String:
			pk := StrKey(string(k))
			if seen[pk] {
				return nil, i.throwError(ctx, "TypeError", "proxy ownKeys: trap returned duplicate keys")
			}
			seen[pk] = true
			keys = append(keys, pk)
		case *Symbol:
			pk := SymKey(k)
			if seen[pk] {
				return nil, i.throwError(ctx, "TypeError", "proxy ownKeys: trap returned duplicate keys")
			}
			seen[pk] = true
			keys = append(keys, pk)
		default:
			return nil, i.throwError(ctx, "TypeError", "proxy ownKeys: trap returned a non-key value")
		}
	}
	// Invariant: every non-configurable own key of the target must appear, and
	// when the target is non-extensible the key set must match the target's
	// exactly.
	for _, tk := range p.target.ownPropertyKeys() {
		td, ok := p.target.getOwn(tk)
		if !ok {
			continue
		}
		if !td.Configurable && !seen[tk] {
			return nil, i.throwError(ctx, "TypeError", "proxy ownKeys: trap result omits a non-configurable key")
		}
		if !p.target.extensible && !seen[tk] {
			return nil, i.throwError(ctx, "TypeError", "proxy ownKeys: trap result omits a key of a non-extensible target")
		}
	}
	return keys, nil
}

// defineProperty implements [[DefineOwnProperty]] (§10.5.6).
func (p *proxyState) defineProperty(ctx context.Context, key PropertyKey, descObj *Object) (bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "defineProperty")
	if err != nil {
		return false, err
	}
	if tr == nil {
		return i.definePropertyV(ctx, p.target, key, descObj)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, keyToValue(key), descObj})
	if err != nil {
		return false, err
	}
	return ToBoolean(res), nil
}

// defineDataValue supports OrdinarySet's CreateDataProperty step when the
// receiver is a proxy: it invokes the defineProperty trap with a complete data
// descriptor.
func (p *proxyState) defineDataValue(ctx context.Context, key PropertyKey, v Value) (bool, error) {
	d := NewObject(p.i.objectProto)
	d.SetData("value", v)
	d.SetData("writable", True)
	d.SetData("enumerable", True)
	d.SetData("configurable", True)
	return p.defineProperty(ctx, key, d)
}

// getPrototypeOf implements [[GetPrototypeOf]] (§10.5.1).
func (p *proxyState) getPrototypeOf(ctx context.Context) (Value, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return nil, err
	}
	i := p.i
	tr, err := p.trap(ctx, "getPrototypeOf")
	if err != nil {
		return nil, err
	}
	if tr == nil {
		return i.getProtoV(ctx, p.target)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target})
	if err != nil {
		return nil, err
	}
	if !IsNull(res) {
		if _, ok := res.(*Object); !ok {
			return nil, i.throwError(ctx, "TypeError", "proxy getPrototypeOf: trap must return an object or null")
		}
	}
	// Invariant: a non-extensible target must report its actual prototype.
	if !p.target.extensible {
		actual, _ := i.getProtoV(ctx, p.target)
		if !sameValue(res, actual) {
			return nil, i.throwError(ctx, "TypeError", "proxy getPrototypeOf: inconsistent prototype for a non-extensible target")
		}
	}
	return res, nil
}

// setPrototypeOf implements [[SetPrototypeOf]] (§10.5.2).
func (p *proxyState) setPrototypeOf(ctx context.Context, proto Value) (bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "setPrototypeOf")
	if err != nil {
		return false, err
	}
	if tr == nil {
		return i.setProtoV(ctx, p.target, proto)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, proto})
	if err != nil {
		return false, err
	}
	if !ToBoolean(res) {
		return false, nil
	}
	if !p.target.extensible {
		actual, _ := i.getProtoV(ctx, p.target)
		if !sameValue(proto, actual) {
			return false, i.throwError(ctx, "TypeError", "proxy setPrototypeOf: cannot change the prototype of a non-extensible target")
		}
	}
	return true, nil
}

// isExtensible implements [[IsExtensible]] (§10.5.3).
func (p *proxyState) isExtensible(ctx context.Context) (bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "isExtensible")
	if err != nil {
		return false, err
	}
	if tr == nil {
		return i.isExtensibleV(ctx, p.target)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target})
	if err != nil {
		return false, err
	}
	if ToBoolean(res) != p.target.extensible {
		return false, i.throwError(ctx, "TypeError", "proxy isExtensible: result must match the target's extensibility")
	}
	return ToBoolean(res), nil
}

// preventExtensions implements [[PreventExtensions]] (§10.5.4).
func (p *proxyState) preventExtensions(ctx context.Context) (bool, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return false, err
	}
	i := p.i
	tr, err := p.trap(ctx, "preventExtensions")
	if err != nil {
		return false, err
	}
	if tr == nil {
		return i.preventExtensionsV(ctx, p.target)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target})
	if err != nil {
		return false, err
	}
	if ToBoolean(res) && p.target.extensible {
		return false, i.throwError(ctx, "TypeError", "proxy preventExtensions: cannot report success while the target is still extensible")
	}
	return ToBoolean(res), nil
}

// callTrap implements [[Call]] (§10.5.12) for a callable proxy.
func (p *proxyState) callTrap(ctx context.Context, thisArg Value, args []Value) (Value, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return nil, err
	}
	i := p.i
	tr, err := p.trap(ctx, "apply")
	if err != nil {
		return nil, err
	}
	if tr == nil {
		return p.target.fn.call(ctx, thisArg, args)
	}
	return tr.fn.call(ctx, p.handler, []Value{p.target, thisArg, i.newArray(append([]Value{}, args...))})
}

// constructTrap implements [[Construct]] (§10.5.13) for a constructor proxy.
func (p *proxyState) constructTrap(ctx context.Context, newTarget Value, args []Value) (Value, error) {
	if err := p.checkRevoked(ctx); err != nil {
		return nil, err
	}
	i := p.i
	tr, err := p.trap(ctx, "construct")
	if err != nil {
		return nil, err
	}
	if tr == nil {
		return p.target.fn.construct(ctx, newTarget, args)
	}
	res, err := tr.fn.call(ctx, p.handler, []Value{p.target, i.newArray(append([]Value{}, args...)), newTarget})
	if err != nil {
		return nil, err
	}
	if _, ok := res.(*Object); !ok {
		return nil, i.throwError(ctx, "TypeError", "proxy construct: trap must return an object")
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

// newProxy builds a Proxy exotic object over target and handler.
func (i *Interpreter) newProxy(ctx context.Context, target, handler Value) (*Object, error) {
	t, ok := target.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot create proxy with a non-object as target")
	}
	h, ok := handler.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot create proxy with a non-object as handler")
	}
	st := &proxyState{i: i, target: t, handler: h}
	pobj := &Object{
		props:      map[PropertyKey]*Property{},
		extensible: true,
		class:      "Proxy",
		proxy:      st,
	}
	// A proxy over a callable/constructor target is itself callable/constructable
	// so `typeof` reports "function" and it responds to () and `new`.
	if t.IsCallable() {
		pobj.fn = &functionData{
			call:   st.callTrap,
			name:   "",
			length: 0,
		}
		if t.IsConstructor() {
			pobj.fn.construct = st.constructTrap
			pobj.fn.ctor = true
		}
	}
	return pobj, nil
}

// initProxy installs the Proxy constructor and Proxy.revocable.
func (i *Interpreter) initProxy() {
	ctor := i.newNativeCtor("Proxy", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor Proxy requires 'new'")
	}, func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		return i.newProxy(ctx, arg(args, 0), arg(args, 1))
	})
	// Proxy has no "prototype" property (a proxy has no [[Prototype]]-bearing
	// instances of its own), so newNativeCtor's implicit wiring is left as-is;
	// the spec gives Proxy no prototype property, so remove any default.
	ctor.deleteOwn(StrKey("prototype"))

	i.defineMethod(ctor, "revocable", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		pobj, err := i.newProxy(ctx, arg(args, 0), arg(args, 1))
		if err != nil {
			return nil, err
		}
		st := pobj.proxy
		revoke := i.newNativeFunc("", 0, func(ctx context.Context, _ Value, _ []Value) (Value, error) {
			st.target = nil
			st.handler = nil
			return Undef, nil
		})
		result := NewObject(i.objectProto)
		result.SetData("proxy", pobj)
		result.SetData("revoke", revoke)
		return result, nil
	})

	i.setGlobalHidden("Proxy", ctor)
}

// keyToValue renders a property key as the Value passed to a trap (a string or a
// symbol).
func keyToValue(k PropertyKey) Value {
	if k.IsSymbol() {
		return k.Sym
	}
	return String(k.Str)
}

// toPropertyDescriptor implements ToPropertyDescriptor + CompletePropertyDescriptor
// (§6.2.6.5 / §6.2.6.6): it parses a descriptor object into a fully populated
// Property. A malformed accessor/value combination or a non-callable
// getter/setter throws a TypeError.
func (i *Interpreter) toPropertyDescriptor(ctx context.Context, desc *Object) (*Property, error) {
	hasEnum := desc.HasOwn(StrKey("enumerable"))
	hasConf := desc.HasOwn(StrKey("configurable"))
	hasValue := desc.HasOwn(StrKey("value"))
	hasWritable := desc.HasOwn(StrKey("writable"))
	hasGet := desc.HasOwn(StrKey("get"))
	hasSet := desc.HasOwn(StrKey("set"))
	if (hasGet || hasSet) && (hasValue || hasWritable) {
		return nil, i.throwError(ctx, "TypeError", "Invalid property descriptor: accessors with a value or writable")
	}
	p := &Property{}
	if hasEnum {
		v, _ := desc.GetStr(ctx, "enumerable")
		p.Enumerable = ToBoolean(v)
	}
	if hasConf {
		v, _ := desc.GetStr(ctx, "configurable")
		p.Configurable = ToBoolean(v)
	}
	if hasGet || hasSet {
		p.Accessor = true
		if hasGet {
			v, _ := desc.GetStr(ctx, "get")
			if fn, ok := v.(*Object); ok && fn.IsCallable() {
				p.Get = fn
			} else if !IsUndefined(v) {
				return nil, i.throwError(ctx, "TypeError", "Getter must be a function")
			}
		}
		if hasSet {
			v, _ := desc.GetStr(ctx, "set")
			if fn, ok := v.(*Object); ok && fn.IsCallable() {
				p.Set = fn
			} else if !IsUndefined(v) {
				return nil, i.throwError(ctx, "TypeError", "Setter must be a function")
			}
		}
		return p, nil
	}
	p.Value = Undef
	if hasValue {
		p.Value, _ = desc.GetStr(ctx, "value")
	}
	if hasWritable {
		v, _ := desc.GetStr(ctx, "writable")
		p.Writable = ToBoolean(v)
	}
	return p, nil
}
