package interp

import (
	"context"
	"strings"

	"github.com/iceisfun/gojs/jsregexp"
)

// This file backs RegExp with the pure-Go jsregexp engine (a full ECMAScript
// regex implementation with backreferences, lookaround, Unicode/u/v modes, and
// a step budget that bounds catastrophic backtracking). The engine operates on
// UTF-16 code units, so match offsets, lastIndex, and .index are code-unit
// indices as ECMAScript requires.

// initRegExp installs the RegExp constructor and prototype.
func (i *Interpreter) initRegExp() {
	proto := i.regexpProto

	i.defineMethod(proto, "test", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		reObj, re, ok := regexpReceiver(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.test called on incompatible receiver")
		}
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		m, err := i.regexExec(ctx, reObj, re, jsregexp.ToUnits(s))
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		return Bool(m != nil), nil
	})

	i.defineMethod(proto, "exec", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		reObj, re, ok := regexpReceiver(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.exec called on incompatible receiver")
		}
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		units := jsregexp.ToUnits(s)
		m, err := i.regexExec(ctx, reObj, re, units)
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		if m == nil {
			return Nul, nil
		}
		return i.submatchToArray(re, units, m), nil
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
// RegExp argument in addition to a string.
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
	coerceRegExp := func(ctx context.Context, v Value, extraFlags string) (*Object, reEngine, error) {
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
		units := jsregexp.ToUnits(s)
		m, err := re.FindSubmatchIndex(ctx, units, 0)
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		if m == nil {
			return Number(-1), nil
		}
		return Number(float64(m[0])), nil
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
		units := jsregexp.ToUnits(s)
		if regexpIsGlobal(reObj) {
			reObj.SetData("lastIndex", Number(0))
			all, err := i.regexFindAll(ctx, re, units)
			if err != nil {
				return nil, i.regexErr(ctx, err)
			}
			if len(all) == 0 {
				return Nul, nil
			}
			vals := make([]Value, len(all))
			for j, m := range all {
				vals[j] = String(jsregexp.FromUnits(units[m[0]:m[1]]))
			}
			return i.newArray(vals), nil
		}
		m, err := i.regexExec(ctx, reObj, re, units)
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		if m == nil {
			return Nul, nil
		}
		return i.submatchToArray(re, units, m), nil
	})

	i.defineMethod(sp, "matchAll", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := strOf(ctx, this)
		if err != nil {
			return nil, err
		}
		// A RegExp argument must carry the global flag (§22.1.3.14).
		if reObj, ok := arg(args, 0).(*Object); ok {
			if _, isRe := regexpOf(reObj); isRe && !regexpIsGlobal(reObj) {
				return nil, i.throwError(ctx, "TypeError", "String.prototype.matchAll called with a non-global RegExp argument")
			}
		}
		_, re, err := coerceRegExp(ctx, arg(args, 0), "g")
		if err != nil {
			return nil, err
		}
		units := jsregexp.ToUnits(s)
		matches, err := i.regexFindAll(ctx, re, units)
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(matches) {
				return Undef, false
			}
			m := matches[idx]
			idx++
			return i.submatchToArray(re, units, m), true
		}), nil
	})

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
				return i.regexSplit(ctx, s, re, limit)
			}
		}
		return i.stringSplitString(ctx, s, args)
	})
}

// regexExec runs one match honoring lastIndex for global/sticky regexes, updating
// lastIndex as ECMAScript's RegExpBuiltinExec specifies. It returns the code-unit
// submatch offsets or nil for no match.
func (i *Interpreter) regexExec(ctx context.Context, reObj *Object, re reEngine, units []uint16) ([]int, error) {
	f := re.Flags()
	useLast := f.Global || f.Sticky
	start := 0
	if useLast {
		liV, _ := reObj.GetStr(ctx, "lastIndex")
		start = int(ToInteger(ToNumber(liV)))
		if start < 0 || start > len(units) {
			reObj.SetData("lastIndex", Number(0))
			return nil, nil
		}
	}
	m, err := re.FindSubmatchIndex(ctx, units, start)
	if err != nil {
		return nil, err
	}
	if m == nil {
		if useLast {
			reObj.SetData("lastIndex", Number(0))
		}
		return nil, nil
	}
	if useLast {
		reObj.SetData("lastIndex", Number(float64(m[1])))
	}
	return m, nil
}

// regexFindAll collects every match from the start of units, advancing past
// zero-width matches by one code point (AdvanceStringIndex). It is independent of
// lastIndex; callers that must reset lastIndex do so themselves.
func (i *Interpreter) regexFindAll(ctx context.Context, re reEngine, units []uint16) ([][]int, error) {
	unicode := re.Flags().UnicodeMode()
	var out [][]int
	pos := 0
	for pos <= len(units) {
		m, err := re.FindSubmatchIndex(ctx, units, pos)
		if err != nil {
			return nil, err
		}
		if m == nil {
			break
		}
		out = append(out, m)
		if m[1] == m[0] {
			pos = advanceStringIndex(units, m[1], unicode)
		} else {
			pos = m[1]
		}
	}
	return out, nil
}

// advanceStringIndex returns the next index after i, stepping over a surrogate
// pair in Unicode mode.
func advanceStringIndex(units []uint16, i int, unicode bool) int {
	if unicode && i < len(units) && i+1 < len(units) &&
		units[i] >= 0xD800 && units[i] <= 0xDBFF && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
		return i + 2
	}
	return i + 1
}

// submatchToArray builds a JS match array from code-unit submatch offsets,
// including .index, .input, and the named-capture .groups object.
func (i *Interpreter) submatchToArray(re reEngine, units []uint16, m []int) *Object {
	n := len(m) / 2
	vals := make([]Value, n)
	for g := 0; g < n; g++ {
		s, e := m[2*g], m[2*g+1]
		if s < 0 {
			vals[g] = Undef // unmatched optional group
		} else {
			vals[g] = String(jsregexp.FromUnits(units[s:e]))
		}
	}
	arr := i.newArray(vals)
	arr.SetData("index", Number(float64(m[0])))
	arr.SetData("input", String(jsregexp.FromUnits(units)))

	names := re.GroupNames()
	if len(names) > 0 {
		groups := NewObject(i.objectProto)
		for name, idx := range names {
			s, e := m[2*idx], m[2*idx+1]
			if idx >= n || s < 0 {
				groups.SetData(name, Undef)
			} else {
				groups.SetData(name, String(jsregexp.FromUnits(units[s:e])))
			}
		}
		arr.SetData("groups", groups)
	} else {
		arr.SetData("groups", Undef)
	}
	return arr
}

// regexSplit implements String.prototype.split with a RegExp separator, including
// interspersed separator capture groups (§22.1.3.21).
func (i *Interpreter) regexSplit(ctx context.Context, s string, re reEngine, limit int) (Value, error) {
	if limit == 0 {
		return i.newArray(nil), nil
	}
	units := jsregexp.ToUnits(s)
	var out []Value
	push := func(v Value) bool {
		out = append(out, v)
		return !(limit >= 0 && len(out) >= limit)
	}

	if len(units) == 0 {
		m, err := re.FindSubmatchIndex(ctx, units, 0)
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		if m != nil {
			return i.newArray(nil), nil
		}
		return i.newArray([]Value{String("")}), nil
	}

	matches, err := i.regexFindAll(ctx, re, units)
	if err != nil {
		return nil, i.regexErr(ctx, err)
	}
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if start >= len(units) {
			break // no match position at end of string
		}
		if end == last {
			continue // empty match at the current boundary
		}
		if !push(String(jsregexp.FromUnits(units[last:start]))) {
			return i.newArray(out), nil
		}
		for g := 1; g < len(m)/2; g++ {
			if m[2*g] < 0 {
				if !push(Undef) {
					return i.newArray(out), nil
				}
			} else if !push(String(jsregexp.FromUnits(units[m[2*g]:m[2*g+1]]))) {
				return i.newArray(out), nil
			}
		}
		last = end
	}
	push(String(jsregexp.FromUnits(units[last:])))
	return i.newArray(out), nil
}

// stringSplitString implements String.prototype.split with a string separator.
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

// regexReplace implements String.prototype.replace with a RegExp pattern,
// supporting a function replacer and $-substitutions ($&, $1..$99, $<name>, $$,
// $`, $').
func (i *Interpreter) regexReplace(ctx context.Context, s string, reObj *Object, re reEngine, repl Value, global bool) (Value, error) {
	units := jsregexp.ToUnits(s)
	var matches [][]int
	if global {
		reObj.SetData("lastIndex", Number(0))
		ms, err := i.regexFindAll(ctx, re, units)
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		matches = ms
	} else {
		m, err := re.FindSubmatchIndex(ctx, units, 0)
		if err != nil {
			return nil, i.regexErr(ctx, err)
		}
		if m != nil {
			matches = [][]int{m}
		}
	}
	if len(matches) == 0 {
		return String(s), nil
	}

	names := re.GroupNames()
	replFn, isFn := repl.(*Object)
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(jsregexp.FromUnits(units[last:m[0]]))
		ng := len(m) / 2
		groups := make([]Value, ng)
		groupStr := make([]string, ng)
		for g := 0; g < ng; g++ {
			if m[2*g] >= 0 {
				gs := jsregexp.FromUnits(units[m[2*g]:m[2*g+1]])
				groupStr[g] = gs
				groups[g] = String(gs)
			} else {
				groups[g] = Undef
			}
		}
		if isFn && replFn.IsCallable() {
			callArgs := make([]Value, 0, ng+3)
			callArgs = append(callArgs, groups...)
			callArgs = append(callArgs, Number(float64(m[0])), String(s))
			if len(names) > 0 {
				callArgs = append(callArgs, i.namedGroupsObject(names, units, m))
			}
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
			b.WriteString(expandDollar(rs, units, groupStr, groups, m[0], m[1], names))
		}
		last = m[1]
	}
	b.WriteString(jsregexp.FromUnits(units[last:]))
	return String(b.String()), nil
}

// namedGroupsObject builds the groups object passed to a replacer function.
func (i *Interpreter) namedGroupsObject(names map[string]int, units []uint16, m []int) *Object {
	o := NewObject(i.objectProto)
	for name, idx := range names {
		if 2*idx+1 < len(m) && m[2*idx] >= 0 {
			o.SetData(name, String(jsregexp.FromUnits(units[m[2*idx]:m[2*idx+1]])))
		} else {
			o.SetData(name, Undef)
		}
	}
	return o
}

// expandDollar performs $-substitution in a regex replacement string.
func expandDollar(repl string, units []uint16, groupStr []string, groups []Value, matchStart, matchEnd int, names map[string]int) string {
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
			b.WriteString(groupStr[0])
			j++
		case next == '`':
			b.WriteString(jsregexp.FromUnits(units[:matchStart]))
			j++
		case next == '\'':
			b.WriteString(jsregexp.FromUnits(units[matchEnd:]))
			j++
		case next == '<' && len(names) > 0:
			// $<name> named-group substitution.
			end := strings.IndexByte(repl[j+2:], '>')
			if end < 0 {
				b.WriteByte('$')
				continue
			}
			name := repl[j+2 : j+2+end]
			if idx, ok := names[name]; ok && idx < len(groups) {
				if s, isStr := groups[idx].(String); isStr {
					b.WriteString(string(s))
				}
			}
			j += 2 + end
		case next >= '0' && next <= '9':
			num := int(next - '0')
			consumed := 1
			if j+2 < len(repl) && repl[j+2] >= '0' && repl[j+2] <= '9' {
				two := num*10 + int(repl[j+2]-'0')
				if two < len(groupStr) {
					num = two
					consumed = 2
				}
			}
			if num > 0 && num < len(groupStr) {
				b.WriteString(groupStr[num])
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

// regexpIsGlobal reports whether a RegExp object carries the global flag.
func regexpIsGlobal(o *Object) bool {
	if re, ok := regexpOf(o); ok {
		return re.Flags().Global
	}
	return false
}

// regexpFromArgs builds a RegExp from (pattern, flags) arguments.
func (i *Interpreter) regexpFromArgs(ctx context.Context, args []Value) (Value, error) {
	pattern := ""
	flags := ""
	if src, fl, ok := regexpSourceFlags(arg(args, 0)); ok {
		pattern = src
		flags = fl
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

// newRegExp compiles a RegExp object with the interpreter's configured backend
// (jsregexp by default, RE2 when RegExpRE2 is selected).
func (i *Interpreter) newRegExp(ctx context.Context, pattern, flags string) (Value, error) {
	re, err := i.compileRegExp(pattern, flags)
	if err != nil {
		return nil, i.throwError(ctx, "SyntaxError", err.Error())
	}
	f := re.Flags()
	o := NewObject(i.regexpProto)
	o.class = "RegExp"
	o.internal = map[string]any{"regexp": re}
	source := pattern
	if source == "" {
		source = "(?:)"
	}
	o.SetHidden("source", String(source))
	o.SetHidden("flags", String(canonicalFlags(flags)))
	o.SetHidden("global", Bool(f.Global))
	o.SetHidden("ignoreCase", Bool(f.IgnoreCase))
	o.SetHidden("multiline", Bool(f.Multiline))
	o.SetHidden("dotAll", Bool(f.DotAll))
	o.SetHidden("unicode", Bool(f.Unicode))
	o.SetHidden("unicodeSets", Bool(f.UnicodeSets))
	o.SetHidden("sticky", Bool(f.Sticky))
	o.SetHidden("hasIndices", Bool(f.HasIndices))
	o.SetData("lastIndex", Number(0))
	return o, nil
}

// regexErr maps a jsregexp runtime error (step budget or context cancellation)
// to something the host can act on: the context error propagates as-is; a budget
// overflow becomes a catchable RangeError.
func (i *Interpreter) regexErr(ctx context.Context, err error) error {
	if err == jsregexp.ErrBudget {
		return i.throwError(ctx, "RangeError", "regular expression step budget exceeded")
	}
	return err
}

// regexpOf extracts the compiled reEngine from a RegExp object.
func regexpOf(v Value) (reEngine, bool) {
	o, ok := v.(*Object)
	if !ok || o.internal == nil {
		return nil, false
	}
	re, ok := o.internal["regexp"].(reEngine)
	return re, ok
}

// regexpReceiver validates that this is a RegExp object and returns it with its
// compiled engine.
func regexpReceiver(this Value) (*Object, reEngine, bool) {
	o, ok := this.(*Object)
	if !ok {
		return nil, nil, false
	}
	re, ok := regexpOf(o)
	if !ok {
		return nil, nil, false
	}
	return o, re, true
}

// regexpSourceFlags returns the source pattern and flags if v is a RegExp object.
func regexpSourceFlags(v Value) (string, string, bool) {
	o, ok := v.(*Object)
	if !ok || o.class != "RegExp" {
		return "", "", false
	}
	re, ok := regexpOf(o)
	if !ok {
		return "", "", false
	}
	return re.Source(), re.Flags().String(), true
}
