package interp

import (
	"context"
	"math/big"

	"github.com/iceisfun/gojs/ast"
	"github.com/iceisfun/gojs/token"
)

// This file implements expression evaluation.

// evalExpr evaluates an expression to a value. It converts an optional-chain
// short-circuit sentinel (produced by ?. on a nullish base) into undefined,
// which is the observable result at the top of a chain.
func (i *Interpreter) evalExpr(ctx context.Context, expr ast.Expr, env *Environment) (Value, error) {
	return i.evalExprWithName(ctx, expr, env, "")
}

// evalExprWithName is evalExpr with a NamedEvaluation name for anonymous
// function/class initializers (e.g. a class field `x = () => {}` names the arrow
// "x"). It preserves the optional-chaining short-circuit collapse to undefined.
func (i *Interpreter) evalExprWithName(ctx context.Context, expr ast.Expr, env *Environment, name string) (Value, error) {
	v, err := i.evalExprNamed(ctx, expr, env, name)
	if err != nil {
		return nil, err
	}
	if isShortCircuit(v) {
		return Undef, nil
	}
	return v, nil
}

// evalExprNamed evaluates an expression, passing an inferred name down to
// anonymous function/class expressions (for their .name property).
func (i *Interpreter) evalExprNamed(ctx context.Context, expr ast.Expr, env *Environment, name string) (Value, error) {
	switch e := expr.(type) {
	case *ast.NumberLit:
		return Number(e.Value), nil
	case *ast.StringLit:
		return String(e.Value), nil
	case *ast.BoolLit:
		return Bool(e.Value), nil
	case *ast.NullLit:
		return Nul, nil
	case *ast.BigIntLit:
		return i.evalBigIntLit(ctx, e)
	case *ast.Ident:
		return i.resolveIdent(ctx, e.Name, env)
	case *ast.ThisExpr:
		// In a derived constructor, `this` is in the Temporal Dead Zone until
		// super() has been called.
		return i.getThisBinding(ctx, env)
	case *ast.TemplateLit:
		return i.evalTemplate(ctx, e, env)
	case *ast.TaggedTemplateExpr:
		return i.evalTaggedTemplate(ctx, e, env)
	case *ast.RegexLit:
		return i.evalRegexLit(ctx, e)
	case *ast.ArrayLit:
		return i.evalArrayLit(ctx, e, env)
	case *ast.ObjectLit:
		return i.evalObjectLit(ctx, e, env)
	case *ast.FuncExpr:
		fn := i.makeFunction(e.Def, env, kindNormal, nil, true)
		if e.Def.Name == nil && name != "" {
			setFuncNameProp(fn, name)
		}
		return fn, nil
	case *ast.ArrowFunc:
		return i.evalArrow(ctx, e, env, name), nil
	case *ast.ClassExpr:
		// NamedEvaluation supplies the inferred name to an anonymous class; it is
		// applied inside evalClass before static initializers run, so this.name is
		// observable there. A named class expression ignores the inferred name.
		inferred := ""
		if e.Def.Name == nil {
			inferred = name
		}
		return i.evalClass(ctx, e.Def, env, inferred)
	case *ast.UnaryExpr:
		return i.evalUnary(ctx, e, env)
	case *ast.UpdateExpr:
		return i.evalUpdate(ctx, e, env)
	case *ast.BinaryExpr:
		return i.evalBinary(ctx, e, env)
	case *ast.LogicalExpr:
		return i.evalLogical(ctx, e, env)
	case *ast.ConditionalExpr:
		return i.evalConditional(ctx, e, env)
	case *ast.AssignExpr:
		return i.evalAssign(ctx, e, env)
	case *ast.SequenceExpr:
		return i.evalSequence(ctx, e, env)
	case *ast.MemberExpr:
		v, _, err := i.evalMember(ctx, e, env)
		return v, err
	case *ast.CallExpr:
		return i.evalCall(ctx, e, env)
	case *ast.NewExpr:
		return i.evalNew(ctx, e, env)
	case *ast.ImportCall:
		return i.evalImportCall(ctx, e, env)
	case *ast.SpreadElement:
		return i.evalExpr(ctx, e.Argument, env)
	case *ast.AwaitExpr:
		return i.evalAwait(ctx, e, env)
	case *ast.YieldExpr:
		return i.evalYield(ctx, e, env)
	case *ast.SuperExpr:
		return nil, i.throwError(ctx, "SyntaxError", "'super' keyword unexpected here")
	default:
		return nil, i.throwError(ctx, "SyntaxError", "unsupported expression")
	}
}

// resolveIdent looks up an identifier, falling back to the global object and
// enforcing the Temporal Dead Zone for lexical bindings.
func (i *Interpreter) resolveIdent(ctx context.Context, name string, env *Environment) (Value, error) {
	if name == "undefined" {
		return Undef, nil
	}
	// new.target is a meta-property, parsed as an identifier. It resolves to the
	// nearest constructor context's [[NewTarget]] (undefined in ordinary calls).
	if name == "new.target" {
		return env.newTarget(), nil
	}
	// Walk the scope chain, interleaving declarative bindings with any `with`
	// object environment records so an inner binding shadows an outer with-object
	// and vice versa (§9.1.1.2 / §9.1.1.1).
	for e := env; e != nil; e = e.parent {
		if e.withObj != nil {
			obj, ok, err := i.withHasBinding(ctx, e.withObj, name)
			if err != nil {
				return nil, err
			}
			if ok {
				return i.withGetBindingValue(ctx, obj, name, env.isStrict())
			}
		}
		if b, ok := e.vars[name]; ok {
			if !b.initialized {
				return nil, i.throwError(ctx, "ReferenceError", "Cannot access '"+name+"' before initialization")
			}
			return b.value, nil
		}
	}
	// Fall back to a global-object property (globals like console, Math).
	if i.global.HasOwn(StrKey(name)) || i.global.Has(StrKey(name)) {
		return i.global.GetStr(ctx, name)
	}
	return nil, i.throwError(ctx, "ReferenceError", name+" is not defined")
}

// evalArrow builds an arrow function, whose body may be a block or an
// expression.
func (i *Interpreter) evalArrow(ctx context.Context, e *ast.ArrowFunc, env *Environment, name string) *Object {
	def := &ast.FuncDef{Params: e.Params, Async: e.Async, Strict: e.Strict}
	var body *ast.BlockStmt
	if b, ok := e.Body.(*ast.BlockStmt); ok {
		body = b
	} else {
		// Wrap a concise expression body as `return <expr>;`.
		bodyExpr := e.Body.(ast.Expr)
		body = &ast.BlockStmt{Body: []ast.Stmt{&ast.ReturnStmt{Argument: bodyExpr}}}
	}
	def.Body = body
	fn := i.makeFunction(def, env, kindArrow, nil, false)
	if name != "" {
		setFuncNameProp(fn, name)
	}
	return fn
}

// evalSequence evaluates a comma expression, yielding the last operand.
func (i *Interpreter) evalSequence(ctx context.Context, e *ast.SequenceExpr, env *Environment) (Value, error) {
	var result Value = Undef
	for _, sub := range e.Exprs {
		v, err := i.evalExpr(ctx, sub, env)
		if err != nil {
			return nil, err
		}
		result = v
	}
	return result, nil
}

// evalConditional evaluates the ternary operator.
func (i *Interpreter) evalConditional(ctx context.Context, e *ast.ConditionalExpr, env *Environment) (Value, error) {
	test, err := i.evalExpr(ctx, e.Test, env)
	if err != nil {
		return nil, err
	}
	if ToBoolean(test) {
		return i.evalExpr(ctx, e.Consequent, env)
	}
	return i.evalExpr(ctx, e.Alternate, env)
}

// evalLogical evaluates &&, ||, and ?? with short-circuit semantics.
func (i *Interpreter) evalLogical(ctx context.Context, e *ast.LogicalExpr, env *Environment) (Value, error) {
	left, err := i.evalExpr(ctx, e.Left, env)
	if err != nil {
		return nil, err
	}
	switch e.Op {
	case token.AND:
		if !ToBoolean(left) {
			return left, nil
		}
	case token.OR:
		if ToBoolean(left) {
			return left, nil
		}
	case token.NULLISH:
		if !IsNullish(left) {
			return left, nil
		}
	}
	return i.evalExpr(ctx, e.Right, env)
}

// evalTemplate evaluates a template literal into a string.
func (i *Interpreter) evalTemplate(ctx context.Context, e *ast.TemplateLit, env *Environment) (Value, error) {
	var b []byte
	for idx, quasi := range e.Quasis {
		b = append(b, quasi.Cooked...)
		if idx < len(e.Exprs) {
			v, err := i.evalExpr(ctx, e.Exprs[idx], env)
			if err != nil {
				return nil, err
			}
			s, err := i.ToStringV(ctx, v)
			if err != nil {
				return nil, err
			}
			b = append(b, s...)
		}
	}
	// A substitution boundary may join a high and low surrogate; canonicalize.
	return String(canonicalizeWTF8(string(b))), nil
}

// evalTaggedTemplate evaluates a tagged template, passing the strings array and
// interpolated values to the tag function.
func (i *Interpreter) evalTaggedTemplate(ctx context.Context, e *ast.TaggedTemplateExpr, env *Environment) (Value, error) {
	tag, thisVal, err := i.evalCallee(ctx, e.Tag, env)
	if err != nil {
		return nil, err
	}
	stringsArr := i.getTemplateObject(e.Quasi)
	callArgs := []Value{stringsArr}
	for _, ex := range e.Quasi.Exprs {
		v, err := i.evalExpr(ctx, ex, env)
		if err != nil {
			return nil, err
		}
		callArgs = append(callArgs, v)
	}
	return i.call(ctx, tag, thisVal, callArgs)
}

// getTemplateObject implements GetTemplateObject (§13.2.8.4). It returns the
// realm's canonical frozen strings array for a given TemplateLiteral Parse Node,
// creating and caching it on first use so that evaluating the same source site
// again hands the tag function the identical object. The returned array and its
// non-enumerable "raw" sub-array are both frozen: indices are
// {writable:false, enumerable:true, configurable:false}, "length" is
// non-writable/non-enumerable, and "raw" is
// {writable:false, enumerable:false, configurable:false}.
func (i *Interpreter) getTemplateObject(quasi *ast.TemplateLit) *Object {
	if cached, ok := i.templateCache[quasi]; ok {
		return cached
	}

	strs := make([]Value, len(quasi.Quasis))
	raws := make([]Value, len(quasi.Quasis))
	for idx, q := range quasi.Quasis {
		// A segment with an invalid escape has no cooked value: the tagged
		// template receives undefined for it (ECMA-262 §12.9.6). The raw value is
		// always present.
		if q.CookedInvalid {
			strs[idx] = Undef
		} else {
			strs[idx] = String(q.Cooked)
		}
		raws[idx] = String(q.Raw)
	}
	rawArr := i.newArray(raws)
	rawArr.setIntegrityLevel(true)

	template := i.newArray(strs)
	// Define "raw" as a non-enumerable, non-writable, non-configurable data
	// property (CreateDataPropertyOrThrow then SetIntegrityLevel freezes it).
	template.defineOwn(StrKey("raw"), &Property{
		Value:        rawArr,
		Writable:     false,
		Enumerable:   false,
		Configurable: false,
	})
	template.setIntegrityLevel(true)

	if i.templateCache == nil {
		i.templateCache = map[*ast.TemplateLit]*Object{}
	}
	i.templateCache[quasi] = template
	return template
}

// evalArrayLit evaluates an array literal, expanding spread elements.
func (i *Interpreter) evalArrayLit(ctx context.Context, e *ast.ArrayLit, env *Environment) (Value, error) {
	var elems []Value
	for _, el := range e.Elements {
		if el == nil {
			elems = append(elems, theHole) // elision → hole
			continue
		}
		if sp, ok := el.(*ast.SpreadElement); ok {
			v, err := i.evalExpr(ctx, sp.Argument, env)
			if err != nil {
				return nil, err
			}
			spread, err := i.iterableToSlice(ctx, v)
			if err != nil {
				return nil, err
			}
			elems = append(elems, spread...)
			continue
		}
		v, err := i.evalExpr(ctx, el, env)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	return i.newArray(elems), nil
}

// evalObjectLit evaluates an object literal, handling shorthand, computed keys,
// methods, accessors, and spread.
func (i *Interpreter) evalObjectLit(ctx context.Context, e *ast.ObjectLit, env *Environment) (Value, error) {
	obj := NewObject(i.objectProto)
	for _, prop := range e.Properties {
		switch prop.Kind {
		case ast.PropSpread:
			v, err := i.evalExpr(ctx, prop.Value, env)
			if err != nil {
				return nil, err
			}
			// CopyDataProperties (§7.3.25): a null/undefined source contributes
			// nothing; any other value is boxed via ToObject and each own
			// enumerable property copied in [[OwnPropertyKeys]] order through the
			// Proxy-aware internal-method helpers so a Proxy source's ownKeys,
			// getOwnPropertyDescriptor, and get traps run in the correct order.
			if !IsNullish(v) {
				src, err := i.ToObject(ctx, v)
				if err != nil {
					return nil, err
				}
				if err := i.copyDataProperties(ctx, obj, src, nil); err != nil {
					return nil, err
				}
			}
			continue
		case ast.PropGet, ast.PropSet:
			key, err := i.evalPropKey(ctx, prop, env)
			if err != nil {
				return nil, err
			}
			fnExpr := prop.Value.(*ast.FuncExpr)
			fn := i.makeFunction(fnExpr.Def, env, kindNormal, obj, false)
			prefix := "get"
			if prop.Kind == ast.PropSet {
				prefix = "set"
			}
			i.setFuncName(fn, key, prefix)
			i.defineAccessorFromProp(obj, key, prop.Kind, fn)
			continue
		}

		key, err := i.evalPropKey(ctx, prop, env)
		if err != nil {
			return nil, err
		}
		// A colon-form __proto__ member sets the object's prototype rather than
		// creating an own property (ECMA-262 §13.2.5.5 / Annex B.3.1). This does
		// not apply to shorthand, method, computed, or accessor forms.
		if !prop.Computed && !prop.Shorthand && !prop.Method && key.Sym == nil && key.Str == "__proto__" {
			v, err := i.evalExpr(ctx, prop.Value, env)
			if err != nil {
				return nil, err
			}
			switch pv := v.(type) {
			case *Object:
				obj.proto = pv
			case Null:
				obj.proto = nil
			}
			continue
		}
		var val Value
		if prop.Method {
			fnExpr := prop.Value.(*ast.FuncExpr)
			m := i.makeFunction(fnExpr.Def, env, kindNormal, obj, false)
			i.setFuncName(m, key, "")
			val = m
		} else {
			// NamedEvaluation uses the property key as the inferred name. A Symbol
			// key contributes "[desc]" (or "" when it has no description); a string
			// key contributes itself.
			val, err = i.evalExprNamed(ctx, prop.Value, env, funcNameFromKey(key))
			if err != nil {
				return nil, err
			}
		}
		obj.writeData(key, val)
	}
	return obj, nil
}

// defineAccessorFromProp installs or augments a getter/setter accessor.
func (i *Interpreter) defineAccessorFromProp(obj *Object, key PropertyKey, kind ast.PropertyKind, fn *Object) {
	existing, ok := obj.props[key]
	if !ok || !existing.Accessor {
		existing = &Property{Accessor: true, Enumerable: true, Configurable: true}
		obj.defineOwn(key, existing)
	}
	if kind == ast.PropGet {
		existing.Get = fn
	} else {
		existing.Set = fn
	}
}

// evalPropKey computes the property key for an object/class member.
func (i *Interpreter) evalPropKey(ctx context.Context, prop *ast.Property, env *Environment) (PropertyKey, error) {
	if prop.Computed {
		v, err := i.evalExpr(ctx, prop.Key, env)
		if err != nil {
			return PropertyKey{}, err
		}
		return i.ToPropertyKey(ctx, v)
	}
	switch k := prop.Key.(type) {
	case *ast.Ident:
		return StrKey(k.Name), nil
	case *ast.StringLit:
		return StrKey(k.Value), nil
	case *ast.NumberLit:
		return StrKey(NumberToString(k.Value)), nil
	case *ast.BigIntLit:
		return StrKey(bigIntLitKeyString(k.Digits)), nil
	case *ast.PrivateIdent:
		return StrKey(k.Name), nil
	default:
		return PropertyKey{}, i.throwError(ctx, "SyntaxError", "invalid property key")
	}
}

// bigIntLitKeyString returns the property-key string named by a BigInt literal:
// the BigInt value's canonical decimal representation (e.g. `0x10n` names "16").
func bigIntLitKeyString(digits string) string {
	n := new(big.Int)
	if _, ok := n.SetString(digits, 0); !ok {
		n.SetString(digits, 10)
	}
	return n.String()
}

// evalBigIntLit parses a BigInt literal.
func (i *Interpreter) evalBigIntLit(ctx context.Context, e *ast.BigIntLit) (Value, error) {
	n := new(big.Int)
	if _, ok := n.SetString(e.Digits, 0); !ok {
		// Digits may be plain decimal without a base prefix.
		if _, ok := n.SetString(e.Digits, 10); !ok {
			return nil, i.throwError(ctx, "SyntaxError", "invalid BigInt literal")
		}
	}
	return &BigInt{Int: n}, nil
}

// evalRegexLit builds a RegExp object for a regex literal.
func (i *Interpreter) evalRegexLit(ctx context.Context, e *ast.RegexLit) (Value, error) {
	return i.newRegExp(ctx, e.Pattern, e.Flags)
}
