package interp

import (
	"context"
	"strconv"
	"strings"
)

// inspect renders a value for console output, following Node-ish conventions:
// top-level strings print verbatim, but strings nested inside objects/arrays are
// quoted. seen guards against cyclic structures.
func (i *Interpreter) inspect(ctx context.Context, v Value, seen map[*Object]bool, quoteStrings bool) (string, error) {
	switch x := flattenRope(v).(type) {
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
	case String:
		if quoteStrings {
			return strconv.Quote(string(x)), nil
		}
		return string(x), nil
	case *BigInt:
		return x.Int.String() + "n", nil
	case *Symbol:
		return "Symbol(" + x.Desc + ")", nil
	case *Object:
		return i.inspectObject(ctx, x, seen)
	default:
		return "", nil
	}
}

// inspectObject renders arrays, functions, errors, and plain objects.
func (i *Interpreter) inspectObject(ctx context.Context, o *Object, seen map[*Object]bool) (string, error) {
	if seen[o] {
		return "[Circular]", nil
	}
	if o.IsCallable() {
		name := o.fn.name
		if name == "" {
			return "[Function (anonymous)]", nil
		}
		if o.fn.ctor {
			return "[class " + name + "]", nil
		}
		return "[Function: " + name + "]", nil
	}
	// Error objects render as "Name: message".
	if i.isErrorObject(o) {
		return briefValue(o), nil
	}
	seen[o] = true
	defer delete(seen, o)

	if o.isArray {
		parts := make([]string, 0, len(o.elems))
		for _, e := range o.elems {
			if isHole(e) {
				parts = append(parts, "<empty>")
				continue
			}
			s, err := i.inspect(ctx, e, seen, true)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[ " + strings.Join(parts, ", ") + " ]", nil
	}

	var parts []string
	for _, name := range o.OwnKeys() {
		p, ok := o.getOwn(StrKey(name))
		if !ok || !p.Enumerable {
			continue
		}
		val, err := o.GetStr(ctx, name)
		if err != nil {
			return "", err
		}
		vs, err := i.inspect(ctx, val, seen, true)
		if err != nil {
			return "", err
		}
		parts = append(parts, formatKey(name)+": "+vs)
	}
	if len(parts) == 0 {
		return "{}", nil
	}
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

// isErrorObject reports whether o inherits from Error.prototype.
func (i *Interpreter) isErrorObject(o *Object) bool {
	for p := o.proto; p != nil; p = p.proto {
		if p == i.errorProto {
			return true
		}
	}
	return false
}

// formatKey renders an object key, quoting it only when it is not a valid
// identifier.
func formatKey(k string) string {
	if isIdentifierName(k) {
		return k
	}
	return strconv.Quote(k)
}

// isIdentifierName reports whether k is a plain identifier (no quoting needed).
func isIdentifierName(k string) bool {
	if k == "" {
		return false
	}
	for idx, r := range k {
		if r == '$' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if idx > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
