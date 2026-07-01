package interp

import (
	"context"
	"regexp"
	"strings"
)

// This file provides a pragmatic RegExp implementation backed by Go's regexp
// package (RE2). RE2 does not support backreferences or lookaround, so those
// patterns will fail to compile; the common cases (character classes, anchors,
// quantifiers, groups, alternation) work.

// initRegExp installs the RegExp constructor and prototype. It is not part of
// the default bootstrap sequence yet; call it from bootstrap when enabling
// regex support.
func (i *Interpreter) initRegExp() {
	proto := i.regexpProto

	i.defineMethod(proto, "test", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		re, ok := regexpOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.test called on incompatible receiver")
		}
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Bool(re.MatchString(s)), nil
	})
	i.defineMethod(proto, "exec", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		reObj, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.exec called on incompatible receiver")
		}
		re, ok := regexpOf(reObj)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.exec called on incompatible receiver")
		}
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		rs := []rune(s)
		// For a global/sticky regex, exec resumes from lastIndex and advances it.
		global := regexpIsGlobal(reObj)
		start := 0
		if global {
			liV, _ := reObj.GetStr(ctx, "lastIndex")
			start = int(ToInteger(ToNumber(liV)))
			if start < 0 || start > len(rs) {
				reObj.SetData("lastIndex", Number(0))
				return Nul, nil
			}
		}
		m := re.FindStringSubmatchIndex(string(rs[start:]))
		if m == nil {
			if global {
				reObj.SetData("lastIndex", Number(0))
			}
			return Nul, nil
		}
		// Shift byte offsets into the sliced string back onto the full string,
		// then produce a rune-indexed match array.
		base := len(string(rs[:start]))
		for k := range m {
			if m[k] >= 0 {
				m[k] += base
			}
		}
		result := i.submatchToArray(s, m)
		if global {
			endRune := len([]rune(s[:m[1]]))
			reObj.SetData("lastIndex", Number(float64(endRune)))
		}
		return result, nil
	})
	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "RegExp.prototype.toString called on non-object")
		}
		src, _ := o.GetStr(ctx, "source")
		flags, _ := o.GetStr(ctx, "flags")
		ss, _ := i.ToStringV(ctx, src)
		fs, _ := i.ToStringV(ctx, flags)
		return String("/" + ss + "/" + fs), nil
	})

	ctor := i.newNativeCtor("RegExp", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.regexpFromArgs(ctx, args)
	}, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.regexpFromArgs(ctx, args)
	})
	linkCtor(ctor, proto)
	i.setGlobalHidden("RegExp", ctor)

	i.initStringRegex()
}

// initStringRegex installs the RegExp-aware String.prototype methods
// (match/matchAll/search) and upgrades replace/replaceAll/split to accept a
// RegExp argument in addition to a string. It runs after both initString and
// the RegExp setup, redefining the string-only versions where needed.
func (i *Interpreter) initStringRegex() {
	sp := i.stringProto

	strOf := func(ctx context.Context, this Value) (string, error) {
		if IsNullish(this) {
			return "", i.throwError(ctx, "TypeError", "String.prototype method called on null or undefined")
		}
		return i.ToStringV(ctx, this)
	}

	// coerceRegExp turns the argument into a compiled RegExp object, wrapping a
	// non-RegExp value with `new RegExp(value)`.
	coerceRegExp := func(ctx context.Context, v Value, extraFlags string) (*Object, *regexp.Regexp, error) {
		if o, ok := v.(*Object); ok {
			if re, ok := regexpOf(o); ok {
				return o, re, nil
			}
		}
		pattern := ""
		if !IsNullish(v) {
			p, err := i.ToStringV(ctx, v)
			if err != nil {
				return nil, nil, err
			}
			pattern = p
		}
		rev, err := i.newRegExp(ctx, pattern, extraFlags)
		if err != nil {
			return nil, nil, err
		}
		o := rev.(*Object)
		re, _ := regexpOf(o)
		return o, re, nil
	}

	i.defineMethod(sp, "search", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := strOf(ctx, this)
		if err != nil {
			return nil, err
		}
		_, re, err := coerceRegExp(ctx, arg(args, 0), "")
		if err != nil {
			return nil, err
		}
		loc := re.FindStringIndex(s)
		if loc == nil {
			return Number(-1), nil
		}
		return Number(float64(len([]rune(s[:loc[0]])))), nil
	})

	i.defineMethod(sp, "match", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := strOf(ctx, this)
		if err != nil {
			return nil, err
		}
		reObj, re, err := coerceRegExp(ctx, arg(args, 0), "")
		if err != nil {
			return nil, err
		}
		if regexpIsGlobal(reObj) {
			all := re.FindAllString(s, -1)
			if all == nil {
				return Nul, nil
			}
			vals := make([]Value, len(all))
			for j, m := range all {
				vals[j] = String(m)
			}
			return i.newArray(vals), nil
		}
		return i.regexMatchResult(s, re), nil
	})

	i.defineMethod(sp, "matchAll", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := strOf(ctx, this)
		if err != nil {
			return nil, err
		}
		// A RegExp argument must carry the global flag (spec §22.1.3.14).
		if reObj, ok := arg(args, 0).(*Object); ok {
			if _, isRe := regexpOf(reObj); isRe && !regexpIsGlobal(reObj) {
				return nil, i.throwError(ctx, "TypeError", "String.prototype.matchAll called with a non-global RegExp argument")
			}
		}
		_, re, err := coerceRegExp(ctx, arg(args, 0), "g")
		if err != nil {
			return nil, err
		}
		matches := re.FindAllStringSubmatchIndex(s, -1)
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(matches) {
				return Undef, false
			}
			m := matches[idx]
			idx++
			return i.submatchToArray(s, m), true
		}), nil
	})

	// Regex-aware replace/replaceAll: dispatch on a RegExp first argument,
	// falling back to the original string-pattern behavior otherwise.
	i.defineMethod(sp, "replace", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := strOf(ctx, this)
		if err != nil {
			return nil, err
		}
		if reObj, ok := arg(args, 0).(*Object); ok {
			if re, ok := regexpOf(reObj); ok {
				return i.regexReplace(ctx, s, reObj, re, arg(args, 1), regexpIsGlobal(reObj))
			}
		}
		return i.stringReplace(ctx, s, args, false)
	})
	i.defineMethod(sp, "replaceAll", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := strOf(ctx, this)
		if err != nil {
			return nil, err
		}
		if reObj, ok := arg(args, 0).(*Object); ok {
			if re, ok := regexpOf(reObj); ok {
				if !regexpIsGlobal(reObj) {
					return nil, i.throwError(ctx, "TypeError", "replaceAll must be called with a global RegExp")
				}
				return i.regexReplace(ctx, s, reObj, re, arg(args, 1), true)
			}
		}
		return i.stringReplace(ctx, s, args, true)
	})

	// Regex-aware split.
	i.defineMethod(sp, "split", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := strOf(ctx, this)
		if err != nil {
			return nil, err
		}
		if reObj, ok := arg(args, 0).(*Object); ok {
			if re, ok := regexpOf(reObj); ok {
				limit := -1
				if !IsUndefined(arg(args, 1)) {
					limit, _ = i.argInt(ctx, args, 1)
				}
				return i.regexSplit(s, re, limit), nil
			}
		}
		return i.stringSplitString(ctx, s, args)
	})
}

// regexSplit implements String.prototype.split with a RegExp separator,
// including ECMAScript's rule that capture groups from the separator are
// interspersed into the result (§22.1.3.21). Zero-width matches are skipped so
// the split does not loop.
func (i *Interpreter) regexSplit(s string, re *regexp.Regexp, limit int) *Object {
	if limit == 0 {
		return i.newArray(nil)
	}
	var out []Value
	push := func(v Value) bool {
		out = append(out, v)
		return !(limit >= 0 && len(out) >= limit)
	}
	// An empty input yields [""] unless the pattern matches the empty string.
	if s == "" {
		if re.MatchString("") {
			return i.newArray(nil)
		}
		return i.newArray([]Value{String("")})
	}
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[0], m[1]
		if end == last && start == last {
			continue // skip zero-width match at the current position
		}
		if start >= len(s) {
			break
		}
		if !push(String(s[last:start])) {
			return i.newArray(out)
		}
		// Intersperse capture groups.
		for g := 1; g < len(m)/2; g++ {
			if m[2*g] < 0 {
				if !push(Undef) {
					return i.newArray(out)
				}
			} else if !push(String(s[m[2*g]:m[2*g+1]])) {
				return i.newArray(out)
			}
		}
		last = end
	}
	push(String(s[last:]))
	return i.newArray(out)
}

// stringSplitString implements String.prototype.split with a string separator
// (the non-regex path used by the RegExp-aware split override).
func (i *Interpreter) stringSplitString(ctx context.Context, s string, args []Value) (Value, error) {
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
}

// regexMatchResult builds the match-array result of a non-global match, or null.
func (i *Interpreter) regexMatchResult(s string, re *regexp.Regexp) Value {
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return Nul
	}
	return i.submatchToArray(s, m)
}

// submatchToArray converts a FindStringSubmatchIndex result into a JS match
// array (full match + capture groups, with .index and .input).
func (i *Interpreter) submatchToArray(s string, m []int) *Object {
	n := len(m) / 2
	vals := make([]Value, n)
	for g := 0; g < n; g++ {
		start, end := m[2*g], m[2*g+1]
		if start < 0 {
			vals[g] = Undef // unmatched optional group
		} else {
			vals[g] = String(s[start:end])
		}
	}
	arr := i.newArray(vals)
	arr.SetData("index", Number(float64(len([]rune(s[:m[0]])))))
	arr.SetData("input", String(s))
	return arr
}

// regexReplace implements String.prototype.replace with a RegExp pattern,
// supporting a function replacer and $-substitutions ($&, $1..$99, $$, $`, $').
func (i *Interpreter) regexReplace(ctx context.Context, s string, reObj *Object, re *regexp.Regexp, repl Value, global bool) (Value, error) {
	replFn, isFn := repl.(*Object)
	var matches [][]int
	if global {
		matches = re.FindAllStringSubmatchIndex(s, -1)
	} else if m := re.FindStringSubmatchIndex(s); m != nil {
		matches = [][]int{m}
	}
	if len(matches) == 0 {
		return String(s), nil
	}

	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(s[last:m[0]])
		groups := make([]string, len(m)/2)
		for g := range groups {
			if m[2*g] >= 0 {
				groups[g] = s[m[2*g]:m[2*g+1]]
			}
		}
		if isFn && replFn.IsCallable() {
			callArgs := make([]Value, 0, len(groups)+2)
			for _, g := range groups {
				callArgs = append(callArgs, String(g))
			}
			callArgs = append(callArgs, Number(float64(len([]rune(s[:m[0]])))), String(s))
			r, err := replFn.fn.call(ctx, Undef, callArgs)
			if err != nil {
				return nil, err
			}
			rs, err := i.ToStringV(ctx, r)
			if err != nil {
				return nil, err
			}
			b.WriteString(rs)
		} else {
			rs, err := i.ToStringV(ctx, repl)
			if err != nil {
				return nil, err
			}
			b.WriteString(expandDollar(rs, s, groups, m[0], m[1]))
		}
		last = m[1]
	}
	b.WriteString(s[last:])
	return String(b.String()), nil
}

// expandDollar performs $-substitution in a regex replacement string.
func expandDollar(repl, src string, groups []string, matchStart, matchEnd int) string {
	var b strings.Builder
	for j := 0; j < len(repl); j++ {
		if repl[j] != '$' || j+1 >= len(repl) {
			b.WriteByte(repl[j])
			continue
		}
		next := repl[j+1]
		switch {
		case next == '$':
			b.WriteByte('$')
			j++
		case next == '&':
			b.WriteString(groups[0])
			j++
		case next == '`':
			b.WriteString(src[:matchStart])
			j++
		case next == '\'':
			b.WriteString(src[matchEnd:])
			j++
		case next >= '0' && next <= '9':
			// Try two-digit group first, then one-digit.
			num := int(next - '0')
			consumed := 1
			if j+2 < len(repl) && repl[j+2] >= '0' && repl[j+2] <= '9' {
				two := num*10 + int(repl[j+2]-'0')
				if two < len(groups) {
					num = two
					consumed = 2
				}
			}
			if num > 0 && num < len(groups) {
				b.WriteString(groups[num])
				j += consumed
			} else {
				b.WriteByte('$')
			}
		default:
			b.WriteByte('$')
		}
	}
	return b.String()
}

// canonicalFlags returns the RegExp flags in the spec-mandated order
// (d, g, i, m, s, u, v, y), deduplicated.
func canonicalFlags(flags string) string {
	var b strings.Builder
	for _, f := range "dgimsuvy" {
		if strings.ContainsRune(flags, f) {
			b.WriteRune(f)
		}
	}
	return b.String()
}

// regexpIsGlobal reports whether a RegExp object has the global flag.
func regexpIsGlobal(o *Object) bool {
	if p, ok := o.props[StrKey("global")]; ok {
		if b, ok := p.Value.(Boolean); ok {
			return bool(b)
		}
	}
	return false
}

// regexpFromArgs builds a RegExp from (pattern, flags) arguments.
func (i *Interpreter) regexpFromArgs(ctx context.Context, args []Value) (Value, error) {
	pattern := ""
	flags := ""
	if re, ok := regexpSource(arg(args, 0)); ok {
		pattern = re
	} else if !IsUndefined(arg(args, 0)) {
		p, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		pattern = p
	}
	if !IsUndefined(arg(args, 1)) {
		f, err := i.argStr(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		flags = f
	}
	return i.newRegExp(ctx, pattern, flags)
}

// newRegExp compiles a RegExp object, translating JS flags to Go regexp flags.
func (i *Interpreter) newRegExp(ctx context.Context, pattern, flags string) (Value, error) {
	goPattern := pattern
	var inline strings.Builder
	if strings.Contains(flags, "i") {
		inline.WriteString("i")
	}
	if strings.Contains(flags, "s") {
		inline.WriteString("s")
	}
	if strings.Contains(flags, "m") {
		inline.WriteString("m")
	}
	if inline.Len() > 0 {
		goPattern = "(?" + inline.String() + ")" + pattern
	}
	re, err := regexp.Compile(goPattern)
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", "Invalid regular expression: "+err.Error())
	}
	o := NewObject(i.regexpProto)
	o.class = "RegExp"
	o.internal = map[string]any{"regexp": re}
	o.SetHidden("source", String(pattern))
	o.SetHidden("flags", String(canonicalFlags(flags)))
	o.SetHidden("global", Bool(strings.Contains(flags, "g")))
	o.SetHidden("ignoreCase", Bool(strings.Contains(flags, "i")))
	o.SetHidden("multiline", Bool(strings.Contains(flags, "m")))
	o.SetData("lastIndex", Number(0))
	return o, nil
}

// regexpOf extracts the compiled *regexp.Regexp from a RegExp object.
func regexpOf(v Value) (*regexp.Regexp, bool) {
	o, ok := v.(*Object)
	if !ok || o.internal == nil {
		return nil, false
	}
	re, ok := o.internal["regexp"].(*regexp.Regexp)
	return re, ok
}

// regexpSource returns the source pattern if v is a RegExp object.
func regexpSource(v Value) (string, bool) {
	o, ok := v.(*Object)
	if !ok || o.class != "RegExp" {
		return "", false
	}
	if p, ok := o.props[StrKey("source")]; ok {
		if s, ok := p.Value.(String); ok {
			return string(s), true
		}
	}
	return "", false
}
