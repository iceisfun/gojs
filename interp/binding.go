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
func (i *Interpreter) bindArrayPattern(ctx context.Context, pat *ast.ArrayLit, value Value, env *Environment, bind func(string, Value)) error {
	items, err := i.iterableToSlice(ctx, value)
	if err != nil {
		return err
	}
	for idx, el := range pat.Elements {
		if el == nil {
			continue // elision / hole
		}
		if rest, ok := el.(*ast.SpreadElement); ok {
			var remaining []Value
			if idx < len(items) {
				remaining = append(remaining, items[idx:]...)
			}
			return i.bindPattern(ctx, rest.Argument, i.newArray(remaining), env, bind)
		}
		if restEl, ok := el.(*ast.RestElement); ok {
			var remaining []Value
			if idx < len(items) {
				remaining = append(remaining, items[idx:]...)
			}
			return i.bindPattern(ctx, restEl.Target, i.newArray(remaining), env, bind)
		}
		var v Value = Undef
		if idx < len(items) {
			v = items[idx]
		}
		if err := i.bindPattern(ctx, el, v, env, bind); err != nil {
			return err
		}
	}
	return nil
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
