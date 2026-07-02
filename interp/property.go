package interp

import "context"

// This file implements ordinary [[Get]] / [[Set]] / [[HasProperty]] /
// [[Delete]] over the prototype chain, including accessor (getter/setter)
// invocation.

// Get returns the value of the property key on the object, walking the
// prototype chain and invoking a getter if the property is an accessor.
func (o *Object) Get(ctx context.Context, key PropertyKey) (Value, error) {
	return o.getWithReceiver(ctx, key, o)
}

// GetStr is Get for a string key.
func (o *Object) GetStr(ctx context.Context, name string) (Value, error) {
	return o.getWithReceiver(ctx, StrKey(name), o)
}

// getWithReceiver resolves key starting at o but binds getters' `this` to
// receiver (which matters for inherited accessors).
func (o *Object) getWithReceiver(ctx context.Context, key PropertyKey, receiver Value) (Value, error) {
	for cur := o; cur != nil; cur = cur.proto {
		// A Proxy anywhere on the chain intercepts [[Get]] for the whole
		// remaining chain (its target may itself continue the lookup).
		if cur.proxy != nil {
			return cur.proxy.get(ctx, key, receiver)
		}
		if p, ok := cur.getOwn(key); ok {
			if p.Accessor {
				if p.Get == nil {
					return Undef, nil
				}
				return p.Get.fn.call(ctx, receiver, nil)
			}
			return p.Value, nil
		}
	}
	return Undef, nil
}

// Has reports whether key exists anywhere on the prototype chain.
func (o *Object) Has(key PropertyKey) bool {
	for cur := o; cur != nil; cur = cur.proto {
		if _, ok := cur.getOwn(key); ok {
			return true
		}
	}
	return false
}

// HasOwn reports whether key is an own property of o.
func (o *Object) HasOwn(key PropertyKey) bool {
	_, ok := o.getOwn(key)
	return ok
}

// Set assigns v to key, honoring inherited setters and non-writable data
// properties. In this (non-strict-by-default) implementation, writes that the
// spec would silently ignore are silently ignored.
func (o *Object) Set(ctx context.Context, key PropertyKey, v Value) error {
	_, err := o.setStatus(ctx, key, v)
	return err
}

// setStatus performs the ordinary [[Set]] and reports whether the write took
// effect. It returns ok=false (with a nil error) when the assignment is
// silently dropped by sloppy semantics — a non-writable own or inherited data
// property, an accessor with no setter, or a non-extensible object. That is
// exactly the condition under which the spec's Set(O, P, V, true) — a write
// with the Throw flag set, e.g. RegExpBuiltinExec assigning lastIndex — must
// raise a TypeError; see (*Interpreter).setThrow.
func (o *Object) setStatus(ctx context.Context, key PropertyKey, v Value) (bool, error) {
	// Search the prototype chain for an accessor or a non-writable data
	// property that governs the assignment.
	for cur := o; cur != nil; cur = cur.proto {
		// A Proxy anywhere on the chain governs the whole write via its set
		// trap, with the original object as the receiver.
		if cur.proxy != nil {
			return cur.proxy.set(ctx, key, v, o)
		}
		p, ok := cur.getOwn(key)
		if !ok {
			continue
		}
		if p.Accessor {
			if p.Set == nil {
				return false, nil // no setter: drop (would throw in strict mode)
			}
			_, err := p.Set.fn.call(ctx, o, []Value{v})
			return err == nil, err
		}
		if cur == o {
			// Own data property: update in place if writable.
			if !p.Writable {
				return false, nil
			}
			o.writeData(key, v)
			return true, nil
		}
		if !p.Writable {
			return false, nil // inherited read-only data property blocks the write
		}
		break // inherited writable data property: create an own property
	}
	if !o.extensible {
		return false, nil
	}
	o.writeData(key, v)
	return true, nil
}

// SetStr is Set for a string key.
func (o *Object) SetStr(ctx context.Context, name string, v Value) error {
	return o.Set(ctx, StrKey(name), v)
}

// writeData creates or updates an own data property, routing array elements and
// length through the array-aware path.
func (o *Object) writeData(key PropertyKey, v Value) {
	if !key.IsSymbol() && o.isArray {
		if key.Str == "length" {
			o.setArrayLength(v)
			return
		}
		if idx, ok := arrayIndex(key.Str); ok {
			o.ensureLen(idx + 1)
			o.elems[idx] = v
			return
		}
	}
	if p, ok := o.props[key]; ok && !p.Accessor {
		p.Value = v
		return
	}
	o.defineOwn(key, &Property{Value: v, Writable: true, Enumerable: true, Configurable: true})
}

// Delete removes an own property, returning whether the object no longer has it.
func (o *Object) Delete(key PropertyKey) bool {
	if p, ok := o.props[key]; ok && !p.Configurable {
		return false
	}
	o.deleteOwn(key)
	return true
}

// createDataProperty implements CreateDataProperty (§7.3.5): it defines key as a
// {writable, enumerable, configurable} data property. It fails (returning false)
// without throwing when the object is not extensible or an existing property is
// non-configurable and cannot accept the new value.
func (o *Object) createDataProperty(key PropertyKey, v Value) bool {
	if cur, ok := o.getOwn(key); ok {
		// The full {W,E,C:true} descriptor conflicts with any non-configurable
		// existing property (it would flip [[Configurable]] back to true), so
		// ValidateAndApplyPropertyDescriptor rejects it.
		if !cur.Configurable {
			return false
		}
	} else if !o.extensible {
		return false
	}
	o.defineOwn(key, &Property{Value: v, Writable: true, Enumerable: true, Configurable: true})
	return true
}

// getPrivateMember reads a private class element (#name) off base, enforcing the
// brand check: base must be an object that carries the private name, or a
// TypeError is thrown. Private getters are invoked with base as the receiver.
func (i *Interpreter) getPrivateMember(ctx context.Context, base Value, pn *PrivateName, name string) (Value, error) {
	obj, ok := base.(*Object)
	if !ok || pn == nil {
		return nil, i.throwError(ctx, "TypeError",
			"Cannot read private member "+name+" from an object whose class did not declare it")
	}
	p, ok := obj.getPrivate(pn)
	if !ok {
		return nil, i.throwError(ctx, "TypeError",
			"Cannot read private member "+name+" from an object whose class did not declare it")
	}
	if p.Accessor {
		if p.Get == nil {
			return nil, i.throwError(ctx, "TypeError",
				"'"+name+"' was defined without a getter")
		}
		return p.Get.fn.call(ctx, obj, nil)
	}
	return p.Value, nil
}

// setPrivateMember writes a private class element (#name) on base, enforcing the
// brand check. Assigning to a private method throws, and a private setter is
// invoked with base as the receiver.
func (i *Interpreter) setPrivateMember(ctx context.Context, base Value, pn *PrivateName, name string, v Value) error {
	obj, ok := base.(*Object)
	if !ok || pn == nil {
		return i.throwError(ctx, "TypeError",
			"Cannot write private member "+name+" to an object whose class did not declare it")
	}
	p, ok := obj.getPrivate(pn)
	if !ok {
		return i.throwError(ctx, "TypeError",
			"Cannot write private member "+name+" to an object whose class did not declare it")
	}
	if p.Accessor {
		if p.Set == nil {
			return i.throwError(ctx, "TypeError",
				"'"+name+"' was defined without a setter")
		}
		_, err := p.Set.fn.call(ctx, obj, []Value{v})
		return err
	}
	if !p.Writable {
		return i.throwError(ctx, "TypeError",
			"Cannot write to private method "+name)
	}
	p.Value = v
	return nil
}

// DefineAccessor installs a getter/setter accessor property.
func (o *Object) DefineAccessor(name string, get, set *Object, enumerable bool) {
	o.defineOwn(StrKey(name), &Property{
		Get:          get,
		Set:          set,
		Accessor:     true,
		Enumerable:   enumerable,
		Configurable: true,
	})
}
