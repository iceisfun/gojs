package interp

import (
	"context"
)

// This file implements the context-sensitive abstract operations — those that
// may invoke user-defined methods (valueOf, toString, Symbol.toPrimitive) and
// therefore need the interpreter and a context.

// isPrimitive reports whether v is a non-object value.
func isPrimitive(v Value) bool {
	_, isObj := v.(*Object)
	return !isObj
}

// ToPrimitive converts v to a primitive following §7.1.1. hint is "number",
// "string", or "default". For non-objects, v is returned unchanged.
func (i *Interpreter) ToPrimitive(ctx context.Context, v Value, hint string) (Value, error) {
	obj, ok := v.(*Object)
	if !ok {
		return v, nil
	}
	// Symbol.toPrimitive takes precedence when present. GetMethod performs a
	// real [[Get]] so accessor properties run (and can propagate errors).
	exotic, err := obj.Get(ctx, SymKey(i.symToPrimitive))
	if err != nil {
		return nil, err
	}
	if !IsNullish(exotic) {
		fn, ok := exotic.(*Object)
		if !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Symbol.toPrimitive is not a function")
		}
		h := hint
		if h == "" {
			h = "default"
		}
		res, err := fn.fn.call(ctx, obj, []Value{String(h)})
		if err != nil {
			return nil, err
		}
		if isPrimitive(res) {
			return res, nil
		}
		return nil, i.throwError(ctx, "TypeError", "Cannot convert object to primitive value")
	}

	methods := []string{"valueOf", "toString"}
	if hint == "string" {
		methods = []string{"toString", "valueOf"}
	}
	for _, name := range methods {
		m, err := obj.GetStr(ctx, name)
		if err != nil {
			return nil, err
		}
		if fn, ok := m.(*Object); ok && fn.IsCallable() {
			res, err := fn.fn.call(ctx, obj, nil)
			if err != nil {
				return nil, err
			}
			if isPrimitive(res) {
				return res, nil
			}
		}
	}
	return nil, i.throwError(ctx, "TypeError", "Cannot convert object to primitive value")
}

// ToStringV converts a value to a Go string per §7.1.17, reducing objects via
// ToPrimitive with a string hint.
func (i *Interpreter) ToStringV(ctx context.Context, v Value) (string, error) {
	switch x := v.(type) {
	case String:
		return string(x), nil
	case Undefined:
		return "undefined", nil
	case Null:
		return "null", nil
	case Boolean:
		if bool(x) {
			return "true", nil
		}
		return "false", nil
	case Number:
		return NumberToString(float64(x)), nil
	case *BigInt:
		return x.Int.String(), nil
	case *Symbol:
		return "", i.throwError(ctx, "TypeError", "Cannot convert a Symbol value to a string")
	case *Object:
		prim, err := i.ToPrimitive(ctx, x, "string")
		if err != nil {
			return "", err
		}
		return i.ToStringV(ctx, prim)
	default:
		return "", nil
	}
}

// ToNumberV converts a value to a number per §7.1.4, reducing objects via
// ToPrimitive with a number hint. BigInt operands throw (mixed arithmetic is
// handled at operator sites).
func (i *Interpreter) ToNumberV(ctx context.Context, v Value) (float64, error) {
	switch x := v.(type) {
	case *Object:
		prim, err := i.ToPrimitive(ctx, x, "number")
		if err != nil {
			return 0, err
		}
		return i.ToNumberV(ctx, prim)
	case *Symbol:
		return 0, i.throwError(ctx, "TypeError", "Cannot convert a Symbol value to a number")
	case *BigInt:
		return 0, i.throwError(ctx, "TypeError", "Cannot convert a BigInt value to a number")
	default:
		return ToNumber(v), nil
	}
}

// ToObject boxes a primitive into its wrapper object, or throws a TypeError for
// null/undefined (§7.1.18).
func (i *Interpreter) ToObject(ctx context.Context, v Value) (*Object, error) {
	switch x := v.(type) {
	case *Object:
		return x, nil
	case String:
		return i.newStringObject(x), nil
	case Number:
		o := NewObject(i.numberProto)
		o.class = "Number"
		o.primitive = x
		return o, nil
	case Boolean:
		o := NewObject(i.booleanProto)
		o.class = "Boolean"
		o.primitive = x
		return o, nil
	case *Symbol:
		o := NewObject(i.symbolProto)
		o.class = "Symbol"
		o.primitive = x
		return o, nil
	case *BigInt:
		o := NewObject(i.bigintProto)
		o.class = "BigInt"
		o.primitive = x
		return o, nil
	default: // Undefined, Null
		return nil, i.throwError(ctx, "TypeError", "Cannot convert undefined or null to object")
	}
}

// newStringObject creates a String wrapper object exposing indexed characters
// and a length property.
func (i *Interpreter) newStringObject(s String) *Object {
	o := NewObject(i.stringProto)
	o.class = "String"
	o.primitive = s
	// String exotic objects (§10.4.3) expose each character as an own data
	// property { [[Writable]]: false, [[Enumerable]]: true, [[Configurable]]:
	// false }, and "length" as { [[Writable]]: false, [[Enumerable]]: false,
	// [[Configurable]]: false }.
	runes := []rune(string(s))
	for idx, r := range runes {
		o.defineOwn(StrKey(intToStr(idx)), &Property{Value: String(string(r)), Writable: false, Enumerable: true, Configurable: false})
	}
	o.defineOwn(StrKey("length"), &Property{Value: Number(float64(len(runes))), Writable: false, Enumerable: false, Configurable: false})
	return o
}

// ToPropertyKey converts a value to a property key per §7.1.19: symbols pass
// through; everything else becomes a string key.
func (i *Interpreter) ToPropertyKey(ctx context.Context, v Value) (PropertyKey, error) {
	if sym, ok := v.(*Symbol); ok {
		return SymKey(sym), nil
	}
	s, err := i.ToStringV(ctx, v)
	if err != nil {
		return PropertyKey{}, err
	}
	return StrKey(s), nil
}

// methodBySymbol returns the callable stored under a symbol key on obj's
// prototype chain, if any.
func (i *Interpreter) methodBySymbol(obj *Object, sym *Symbol) (*Object, bool) {
	for cur := obj; cur != nil; cur = cur.proto {
		if p, ok := cur.getOwn(SymKey(sym)); ok && !p.Accessor {
			if fn, ok := p.Value.(*Object); ok && fn.IsCallable() {
				return fn, true
			}
		}
	}
	return nil, false
}
