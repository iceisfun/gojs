package interp

import (
	"context"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements binding-pattern destructuring, shared by parameter
// binding, variable declarations, and for-of/for-in loop heads.

// bindPattern binds value to the binding target (an identifier or a
// destructuring pattern), invoking bind(name, v) for each simple name it
// resolves. It is used wherever a new binding is introduced.
func (i *Interpreter) bindPattern(ctx context.Context, target ast.Expr, value Value, env *Environment, bind func(string, Value)) error {
	switch t := target.(type) {
	case *ast.Ident:
		bind(t.Name, value)
		return nil
	case *ast.AssignPattern:
		if IsUndefined(value) {
			// Infer the name for an anonymous default (e.g. `{f = () => {}}`
			// gives the function the name "f").
			def, err := i.evalExprNamed(ctx, t.Default, env, bindingName(t.Target))
			if err != nil {
				return err
			}
			value = def
		}
		return i.bindPattern(ctx, t.Target, value, env, bind)
	case *ast.AssignExpr:
		// A default written inside an array/parameter pattern (e.g. `[c = 3]`)
		// parses as an assignment expression; treat `=` as a defaulting binding.
		if t.Op == token.ASSIGN {
			if IsUndefined(value) {
				def, err := i.evalExprNamed(ctx, t.Value, env, bindingName(t.Target))
				if err != nil {
					return err
				}
				value = def
			}
			return i.bindPattern(ctx, t.Target, value, env, bind)
		}
		return i.throwError(ctx, "SyntaxError", "invalid binding target")
	case *ast.ArrayLit:
		return i.bindArrayPattern(ctx, t, value, env, bind)
	case *ast.ObjectLit:
		return i.bindObjectPattern(ctx, t, value, env, bind)
	default:
		return i.throwError(ctx, "SyntaxError", "invalid binding target")
	}
}

// bindArrayPattern destructures an iterable into an array pattern's elements.
//
// It consumes the iterator lazily, one step per pattern element, and closes the
// iterator (calling its return method) when the pattern is satisfied before the
// iterator is exhausted, or when binding an element throws. This matters for a
// non-exhausting pattern over an infinite or side-effecting iterator: draining
// it eagerly would hang or skip the required IteratorClose (ECMA-262
// 8.5.2/8.6.2, IteratorBindingInitialization + IteratorClose).
func (i *Interpreter) bindArrayPattern(ctx context.Context, pat *ast.ArrayLit, value Value, env *Environment, bind func(string, Value)) error {
	return i.iterArrayPattern(ctx, pat.Elements, value,
		func(el ast.Expr, v Value) error { return i.bindPattern(ctx, el, v, env, bind) },
		func(target ast.Expr, rest []Value) error {
			return i.bindPattern(ctx, target, i.newArray(rest), env, bind)
		})
}

// iterArrayPattern drives lazy consumption of an array pattern's elements,
// shared by binding-context (bindArrayPattern) and assignment-context
// (assignPattern) destructuring. It calls onElem for each non-rest element with
// the value pulled from the iterator, and onRest for a rest element with the
// collected remaining values. It closes the iterator (calling return()) when
// the pattern is satisfied before the iterator is exhausted, or when onElem
// completes abruptly — the required IteratorClose that an eager drain would skip
// (and which would hang on an infinite iterator). See ECMA-262 8.5.2/8.6.2.
func (i *Interpreter) iterArrayPattern(ctx context.Context, elements []ast.Expr, value Value, onElem func(el ast.Expr, v Value) error, onRest func(target ast.Expr, rest []Value) error) error {
	step, closeIter, err := i.patternIterator(ctx, value)
	if err != nil {
		return err
	}
	done := false
	var stepErr error
	// pull advances the iterator once, latching completion and any error so that
	// a failed IteratorStep leaves the iterator marked done (never closed).
	pull := func() Value {
		if done {
			return Undef
		}
		v, d, err := step()
		if err != nil {
			stepErr = err
			done = true
			return Undef
		}
		if d {
			done = true
			return Undef
		}
		return v
	}
	for _, el := range elements {
		if restTgt := restTargetOf(el); restTgt != nil {
			var rest []Value
			for {
				v := pull()
				if done {
					break
				}
				rest = append(rest, v)
			}
			if stepErr != nil {
				return stepErr
			}
			return onRest(restTgt, rest)
		}
		v := pull()
		if stepErr != nil {
			return stepErr
		}
		if el == nil {
			continue // elision / hole: the value is consumed but discarded
		}
		if err := onElem(el, v); err != nil {
			if !done {
				// Abrupt completion: close the iterator, but the original error
				// takes precedence over any error from return().
				_ = closeIter()
			}
			return err
		}
	}
	if !done {
		if err := closeIter(); err != nil {
			return err
		}
	}
	return nil
}

// patternIterator returns a stepping function and a close function for
// destructuring value as an array pattern. Arrays and strings are stepped
// directly from their backing storage (finite, and their native iterators have
// no observable return), so only a generic object drives the full
// Symbol.iterator protocol with IteratorClose support.
func (i *Interpreter) patternIterator(ctx context.Context, value Value) (step func() (Value, bool, error), closeIter func() error, err error) {
	noClose := func() error { return nil }
	switch v := value.(type) {
	case *Object:
		if v.isArray {
			idx := 0
			return func() (Value, bool, error) {
				if idx >= len(v.elems) {
					return Undef, true, nil
				}
				val := undefIfHole(v.elems[idx])
				idx++
				return val, false, nil
			}, noClose, nil
		}
		return i.protocolIterator(ctx, v)
	case String:
		runes := []rune(string(v))
		idx := 0
		return func() (Value, bool, error) {
			if idx >= len(runes) {
				return Undef, true, nil
			}
			val := String(string(runes[idx]))
			idx++
			return val, false, nil
		}, noClose, nil
	case Undefined, Null:
		return nil, nil, i.throwError(ctx, "TypeError", briefValue(value)+" is not iterable")
	default:
		return nil, nil, i.throwError(ctx, "TypeError", briefValue(value)+" is not iterable")
	}
}

// protocolIterator obtains an iterator via Symbol.iterator and returns step and
// close functions driving next() and return() per the iteration protocol.
func (i *Interpreter) protocolIterator(ctx context.Context, obj *Object) (step func() (Value, bool, error), closeIter func() error, err error) {
	itFn, ok := i.methodBySymbol(obj, i.symIterator)
	if !ok {
		return nil, nil, i.throwError(ctx, "TypeError", briefValue(obj)+" is not iterable")
	}
	iterator, err := itFn.fn.call(ctx, obj, nil)
	if err != nil {
		return nil, nil, err
	}
	itObj, ok := iterator.(*Object)
	if !ok {
		return nil, nil, i.throwError(ctx, "TypeError", "iterator is not an object")
	}
	nextV, err := itObj.GetStr(ctx, "next")
	if err != nil {
		return nil, nil, err
	}
	next, ok := nextV.(*Object)
	if !ok || !next.IsCallable() {
		return nil, nil, i.throwError(ctx, "TypeError", "iterator.next is not a function")
	}
	step = func() (Value, bool, error) {
		if err := i.checkContext(); err != nil {
			return Undef, true, err
		}
		resV, err := next.fn.call(ctx, itObj, nil)
		if err != nil {
			return Undef, true, err
		}
		res, ok := resV.(*Object)
		if !ok {
			return Undef, true, i.throwError(ctx, "TypeError", "iterator result is not an object")
		}
		doneV, err := res.GetStr(ctx, "done")
		if err != nil {
			return Undef, true, err
		}
		if ToBoolean(doneV) {
			return Undef, true, nil
		}
		val, err := res.GetStr(ctx, "value")
		if err != nil {
			return Undef, true, err
		}
		return val, false, nil
	}
	closeIter = func() error {
		retV, err := itObj.GetStr(ctx, "return")
		if err != nil {
			return err
		}
		ret, ok := retV.(*Object)
		if !ok || !ret.IsCallable() {
			// A missing or null return method means there is nothing to close.
			return nil
		}
		_, err = ret.fn.call(ctx, itObj, nil)
		return err
	}
	return step, closeIter, nil
}

// bindObjectPattern destructures an object into an object pattern's properties.
func (i *Interpreter) bindObjectPattern(ctx context.Context, pat *ast.ObjectLit, value Value, env *Environment, bind func(string, Value)) error {
	obj, err := i.ToObject(ctx, value)
	if err != nil {
		return err
	}
	taken := map[string]bool{}
	for _, prop := range pat.Properties {
		if prop.Kind == ast.PropSpread {
			// Rest: collect remaining own enumerable properties.
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
			if err := i.bindPattern(ctx, prop.Value, rest, env, bind); err != nil {
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
		targetExpr := prop.Value
		if targetExpr == nil {
			targetExpr = prop.Key
		}
		if err := i.bindPattern(ctx, targetExpr, v, env, bind); err != nil {
			return err
		}
	}
	return nil
}

// propertyKeyName computes the string key of an object-pattern property.
func (i *Interpreter) propertyKeyName(ctx context.Context, prop *ast.Property, env *Environment) (string, error) {
	if prop.Computed {
		k, err := i.evalExpr(ctx, prop.Key, env)
		if err != nil {
			return "", err
		}
		return i.ToStringV(ctx, k)
	}
	switch k := prop.Key.(type) {
	case *ast.Ident:
		return k.Name, nil
	case *ast.StringLit:
		return k.Value, nil
	case *ast.NumberLit:
		return NumberToString(k.Value), nil
	case *ast.BigIntLit:
		return bigIntLitKeyString(k.Digits), nil
	default:
		return "", i.throwError(ctx, "SyntaxError", "invalid property key in pattern")
	}
}

// countParams returns the arity (number of parameters before the first default
// or rest), matching Function.prototype.length semantics.
func countParams(params []ast.Expr) int {
	n := 0
	for _, p := range params {
		switch p.(type) {
		case *ast.RestElement, *ast.AssignPattern:
			return n
		}
		n++
	}
	return n
}
