package interp

import (
	"context"
	"strings"
	"unicode"
)

// initString installs the String constructor and String.prototype methods.
// Strings are stored as Go UTF-8; methods that the spec defines over UTF-16
// code units operate over runes here, which is a faithful approximation for the
// Basic Multilingual Plane.
func (i *Interpreter) initString() {
	proto := i.stringProto
	proto.class = "String"
	proto.primitive = String("")

	strOf := func(ctx context.Context, this Value) (string, error) {
		switch x := this.(type) {
		case String:
			return string(x), nil
		case *Object:
			if s, ok := x.primitive.(String); ok {
				return string(s), nil
			}
		}
		if IsNullish(this) {
			return "", i.throwError(ctx, "TypeError", "String.prototype method called on null or undefined")
		}
		return i.ToStringV(ctx, this)
	}

	m := func(name string, n int, fn func(ctx context.Context, s string, args []Value) (Value, error)) {
		i.defineMethod(proto, name, n, func(ctx context.Context, this Value, args []Value) (Value, error) {
			s, err := strOf(ctx, this)
			if err != nil {
				return nil, err
			}
			return fn(ctx, s, args)
		})
	}

	m("toString", 0, func(ctx context.Context, s string, args []Value) (Value, error) { return String(s), nil })
	m("valueOf", 0, func(ctx context.Context, s string, args []Value) (Value, error) { return String(s), nil })
	m("charAt", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		rs := []rune(s)
		idx, _ := i.argInt(ctx, args, 0)
		if idx < 0 || idx >= len(rs) {
			return String(""), nil
		}
		return String(string(rs[idx])), nil
	})
	m("charCodeAt", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		rs := []rune(s)
		idx, _ := i.argInt(ctx, args, 0)
		if idx < 0 || idx >= len(rs) {
			return Number(nan()), nil
		}
		return Number(float64(rs[idx])), nil
	})
	m("codePointAt", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		rs := []rune(s)
		idx, _ := i.argInt(ctx, args, 0)
		if idx < 0 || idx >= len(rs) {
			return Undef, nil
		}
		return Number(float64(rs[idx])), nil
	})
	m("at", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		rs := []rune(s)
		idx, _ := i.argInt(ctx, args, 0)
		if idx < 0 {
			idx += len(rs)
		}
		if idx < 0 || idx >= len(rs) {
			return Undef, nil
		}
		return String(string(rs[idx])), nil
	})
	m("indexOf", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(float64(strings.Index(s, sub))), nil
	})
	m("lastIndexOf", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(float64(strings.LastIndex(s, sub))), nil
	})
	m("includes", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(strings.Contains(s, sub)), nil
	})
	m("startsWith", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(strings.HasPrefix(s, sub)), nil
	})
	m("endsWith", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		sub, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(strings.HasSuffix(s, sub)), nil
	})
	m("slice", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		rs := []rune(s)
		n := len(rs)
		start := relIndex(argIntOr(ctx, i, args, 0, 0), n)
		end := n
		if !IsUndefined(arg(args, 1)) {
			end = relIndex(argIntOr(ctx, i, args, 1, n), n)
		}
		if start > end {
			return String(""), nil
		}
		return String(string(rs[start:end])), nil
	})
	m("substring", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		rs := []rune(s)
		n := len(rs)
		a := clampRange(argIntOr(ctx, i, args, 0, 0), n)
		b := n
		if !IsUndefined(arg(args, 1)) {
			b = clampRange(argIntOr(ctx, i, args, 1, n), n)
		}
		if a > b {
			a, b = b, a
		}
		return String(string(rs[a:b])), nil
	})
	m("substr", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		rs := []rune(s)
		n := len(rs)
		start := argIntOr(ctx, i, args, 0, 0)
		if start < 0 {
			start = max(n+start, 0)
		}
		length := n - start
		if !IsUndefined(arg(args, 1)) {
			length = argIntOr(ctx, i, args, 1, 0)
		}
		if start >= n || length <= 0 {
			return String(""), nil
		}
		end := min(start+length, n)
		return String(string(rs[start:end])), nil
	})
	m("toUpperCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.ToUpper(s)), nil
	})
	m("toLowerCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.ToLower(s)), nil
	})
	m("toLocaleUpperCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.ToUpper(s)), nil
	})
	m("toLocaleLowerCase", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.ToLower(s)), nil
	})
	m("trim", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.TrimSpace(s)), nil
	})
	m("trimStart", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.TrimLeftFunc(s, unicode.IsSpace)), nil
	})
	m("trimEnd", 0, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(strings.TrimRightFunc(s, unicode.IsSpace)), nil
	})
	m("padStart", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(pad(s, argIntOr(ctx, i, args, 0, 0), padStr(ctx, i, args), true)), nil
	})
	m("padEnd", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		return String(pad(s, argIntOr(ctx, i, args, 0, 0), padStr(ctx, i, args), false)), nil
	})
	m("repeat", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		count, _ := i.argInt(ctx, args, 0)
		if count < 0 {
			return nil, i.throwError(ctx, "RangeError", "Invalid count value")
		}
		return String(strings.Repeat(s, count)), nil
	})
	m("concat", 1, func(ctx context.Context, s string, args []Value) (Value, error) {
		var b strings.Builder
		b.WriteString(s)
		for _, a := range args {
			as, err := i.ToStringV(ctx, a)
			if err != nil {
				return nil, err
			}
			b.WriteString(as)
		}
		return String(b.String()), nil
	})
	m("split", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		if IsUndefined(arg(args, 0)) {
			return i.newArray([]Value{String(s)}), nil
		}
		sep, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		var parts []string
		if sep == "" {
			for _, r := range s {
				parts = append(parts, string(r))
			}
		} else {
			parts = strings.Split(s, sep)
		}
		limit := -1
		if !IsUndefined(arg(args, 1)) {
			limit, _ = i.argInt(ctx, args, 1)
		}
		out := make([]Value, 0, len(parts))
		for idx, p := range parts {
			if limit >= 0 && idx >= limit {
				break
			}
			out = append(out, String(p))
		}
		return i.newArray(out), nil
	})
	m("replace", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		return i.stringReplace(ctx, s, args, false)
	})
	m("replaceAll", 2, func(ctx context.Context, s string, args []Value) (Value, error) {
		return i.stringReplace(ctx, s, args, true)
	})

	// Indexed access and length on the prototype fall back to per-instance
	// wrappers; primitive strings are handled directly in member access.

	ctor := i.newNativeCtor("String", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if len(args) == 0 {
			return String(""), nil
		}
		if sym, ok := args[0].(*Symbol); ok {
			return String("Symbol(" + sym.Desc + ")"), nil
		}
		s, err := i.ToStringV(ctx, args[0])
		if err != nil {
			return nil, err
		}
		return String(s), nil
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := ""
		if len(args) > 0 {
			var err error
			s, err = i.ToStringV(ctx, args[0])
			if err != nil {
				return nil, err
			}
		}
		return i.newStringObject(String(s)), nil
	})
	linkCtor(ctor, proto)

	i.defineMethod(ctor, "fromCharCode", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		var b strings.Builder
		for _, a := range args {
			n, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			b.WriteRune(rune(uint16(int64(n))))
		}
		return String(b.String()), nil
	})
	i.defineMethod(ctor, "fromCodePoint", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		var b strings.Builder
		for _, a := range args {
			n, err := i.ToNumberV(ctx, a)
			if err != nil {
				return nil, err
			}
			b.WriteRune(rune(int64(n)))
		}
		return String(b.String()), nil
	})

	i.setGlobalHidden("String", ctor)
}

// stringReplace implements String.prototype.replace/replaceAll for string
// patterns and string or function replacements. (Regex patterns are handled
// by the RegExp integration when present.)
func (i *Interpreter) stringReplace(ctx context.Context, s string, args []Value, all bool) (Value, error) {
	pattern, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	repl := arg(args, 1)
	replFn, isFn := repl.(*Object)
	doReplace := func(match string, idx int) (string, error) {
		if isFn && replFn.IsCallable() {
			r, err := replFn.fn.call(ctx, Undef, []Value{String(match), Number(float64(idx)), String(s)})
			if err != nil {
				return "", err
			}
			return i.ToStringV(ctx, r)
		}
		rs, err := i.ToStringV(ctx, repl)
		if err != nil {
			return "", err
		}
		return strings.ReplaceAll(rs, "$&", match), nil
	}
	if all {
		var b strings.Builder
		rest := s
		offset := 0
		for {
			idx := strings.Index(rest, pattern)
			if idx < 0 || pattern == "" {
				b.WriteString(rest)
				break
			}
			b.WriteString(rest[:idx])
			r, err := doReplace(pattern, offset+idx)
			if err != nil {
				return nil, err
			}
			b.WriteString(r)
			rest = rest[idx+len(pattern):]
			offset += idx + len(pattern)
		}
		return String(b.String()), nil
	}
	idx := strings.Index(s, pattern)
	if idx < 0 {
		return String(s), nil
	}
	r, err := doReplace(pattern, idx)
	if err != nil {
		return nil, err
	}
	return String(s[:idx] + r + s[idx+len(pattern):]), nil
}

// clampRange clamps x into [0, n] (for substring).
func clampRange(x, n int) int {
	if x < 0 {
		return 0
	}
	if x > n {
		return n
	}
	return x
}

// pad implements padStart/padEnd.
func pad(s string, targetLen int, padding string, start bool) string {
	cur := len([]rune(s))
	if cur >= targetLen || padding == "" {
		return s
	}
	need := targetLen - cur
	pr := []rune(padding)
	var b strings.Builder
	for b.Len() < need*4 && len([]rune(b.String())) < need {
		b.WriteString(padding)
		_ = pr
	}
	padRunes := []rune(b.String())
	if len(padRunes) > need {
		padRunes = padRunes[:need]
	}
	if start {
		return string(padRunes) + s
	}
	return s + string(padRunes)
}

// padStr reads the optional pad-string argument (default a single space).
func padStr(ctx context.Context, i *Interpreter, args []Value) string {
	if IsUndefined(arg(args, 1)) {
		return " "
	}
	s, _ := i.ToStringV(ctx, arg(args, 1))
	return s
}
