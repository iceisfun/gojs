package interp

import (
	"context"
	"sort"
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
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.test called on incompatible receiver")
		}
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		res, err := i.regExpExec(ctx, o, s)
		if err != nil {
			return nil, err
		}
		return Bool(!IsNull(res)), nil
	})

	i.defineMethod(proto, "exec", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.exec called on incompatible receiver")
		}
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return i.regexpBuiltinExec(ctx, o, s)
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
		return i.regexpFromArgs(ctx, args, true) // called as a function: NewTarget undefined
	}, func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		v, err := i.regexpFromArgs(ctx, args, false) // constructed: NewTarget defined
		if err != nil {
			return nil, err
		}
		// GetPrototypeFromConstructor (§22.2.3.1): a subclass instance takes its
		// prototype from new.target rather than %RegExp.prototype%.
		if o, ok := v.(*Object); ok {
			p, err := i.protoFromNewTarget(ctx, newTarget, i.regexpProto)
			if err != nil {
				return nil, err
			}
			if p != i.regexpProto {
				o.SetProto(p)
			}
		}
		return v, nil
	})
	linkCtor(ctor, proto)
	i.regexpCtor = ctor
	i.setGlobalHidden("RegExp", ctor)

	i.initRegExpStatics(ctor)
	i.initRegExpAccessors(proto)
	i.initRegExpSymbols(proto)
	i.initRegExpStringIterator()
	i.initStringRegex()
}

// initStringRegex installs the RegExp-aware String.prototype methods, each of
// which delegates to the regexp argument's well-known-symbol method when present
// (§22.1.3), falling back to a freshly created RegExp (match/search/matchAll) or
// plain string behavior (replace/split).
func (i *Interpreter) initStringRegex() {
	sp := i.stringProto

	requireCoercible := func(ctx context.Context, this Value) error {
		if IsNullish(this) {
			return i.throwError(ctx, "TypeError", "String.prototype method called on null or undefined")
		}
		return nil
	}

	i.defineMethod(sp, "search", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := requireCoercible(ctx, this); err != nil {
			return nil, err
		}
		regexp := arg(args, 0)
		// §22.1.3.13 step 3: the @@search lookup is only performed when the
		// argument is an Object; a primitive never has its @@search accessed.
		if ro, ok := regexp.(*Object); ok {
			if m, err := i.getMethod(ctx, ro, i.symSearch); err != nil {
				return nil, err
			} else if m != nil {
				// The searcher receives the original receiver unchanged.
				return i.call(ctx, m, ro, []Value{this})
			}
		}
		s, err := i.ToStringV(ctx, this)
		if err != nil {
			return nil, err
		}
		rx, err := i.regExpCreate(ctx, regexp, "")
		if err != nil {
			return nil, err
		}
		// Step 8: Invoke(rx, @@search, «S») — a fresh property lookup so a
		// user-overridden RegExp.prototype[@@search] is honored.
		return i.invokeSymbol(ctx, rx, i.symSearch, "@@search", []Value{String(s)})
	})

	i.defineMethod(sp, "match", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := requireCoercible(ctx, this); err != nil {
			return nil, err
		}
		regexp := arg(args, 0)
		if ro, ok := regexp.(*Object); ok {
			if m, err := i.getMethod(ctx, ro, i.symMatch); err != nil {
				return nil, err
			} else if m != nil {
				return i.call(ctx, m, ro, []Value{this})
			}
		}
		s, err := i.ToStringV(ctx, this)
		if err != nil {
			return nil, err
		}
		rx, err := i.regExpCreate(ctx, regexp, "")
		if err != nil {
			return nil, err
		}
		return i.invokeSymbol(ctx, rx, i.symMatch, "@@match", []Value{String(s)})
	})

	i.defineMethod(sp, "matchAll", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := requireCoercible(ctx, this); err != nil {
			return nil, err
		}
		regexp := arg(args, 0)
		if ro, ok := regexp.(*Object); ok {
			isRe, err := i.isRegExpValue(ctx, ro)
			if err != nil {
				return nil, err
			}
			if isRe {
				flags, err := i.getStrProp(ctx, ro, "flags")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(flags, "g") {
					return nil, i.throwError(ctx, "TypeError", "String.prototype.matchAll called with a non-global RegExp argument")
				}
			}
			if m, err := i.getMethod(ctx, ro, i.symMatchAll); err != nil {
				return nil, err
			} else if m != nil {
				return i.call(ctx, m, ro, []Value{this})
			}
		}
		s, err := i.ToStringV(ctx, this)
		if err != nil {
			return nil, err
		}
		rx, err := i.regExpCreate(ctx, regexp, "g")
		if err != nil {
			return nil, err
		}
		// Step 5: Invoke(rx, @@matchAll, «S») — a property lookup so a removed or
		// overridden RegExp.prototype[@@matchAll] is honored (TypeError if absent).
		return i.invokeSymbol(ctx, rx, i.symMatchAll, "@@matchAll", []Value{String(s)})
	})

	i.defineMethod(sp, "replace", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := requireCoercible(ctx, this); err != nil {
			return nil, err
		}
		searchValue := arg(args, 0)
		// §22.1.3.19 step 3: the @@replace lookup is only performed when the
		// searchValue is an Object.
		if sv, ok := searchValue.(*Object); ok {
			if m, err := i.getMethod(ctx, sv, i.symReplace); err != nil {
				return nil, err
			} else if m != nil {
				// The replacer receives the original receiver unchanged.
				return i.call(ctx, m, sv, []Value{this, arg(args, 1)})
			}
		}
		s, err := i.ToStringV(ctx, this)
		if err != nil {
			return nil, err
		}
		return i.stringReplace(ctx, s, args, false)
	})

	i.defineMethod(sp, "replaceAll", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := requireCoercible(ctx, this); err != nil {
			return nil, err
		}
		searchValue := arg(args, 0)
		if sv, ok := searchValue.(*Object); ok {
			isRe, err := i.isRegExpValue(ctx, sv)
			if err != nil {
				return nil, err
			}
			if isRe {
				flags, err := i.getStrProp(ctx, sv, "flags")
				if err != nil {
					return nil, err
				}
				if !strings.Contains(flags, "g") {
					return nil, i.throwError(ctx, "TypeError", "String.prototype.replaceAll called with a non-global RegExp argument")
				}
			}
			if m, err := i.getMethod(ctx, sv, i.symReplace); err != nil {
				return nil, err
			} else if m != nil {
				return i.call(ctx, m, sv, []Value{this, arg(args, 1)})
			}
		}
		s, err := i.ToStringV(ctx, this)
		if err != nil {
			return nil, err
		}
		return i.stringReplace(ctx, s, args, true)
	})

	i.defineMethod(sp, "split", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if err := requireCoercible(ctx, this); err != nil {
			return nil, err
		}
		separator := arg(args, 0)
		// §22.1.3.23 step 3: the @@split lookup is only performed when the
		// separator is an Object; a primitive never has its @@split accessed.
		if sep, ok := separator.(*Object); ok {
			if m, err := i.getMethod(ctx, sep, i.symSplit); err != nil {
				return nil, err
			} else if m != nil {
				// The splitter receives the original receiver unchanged.
				return i.call(ctx, m, sep, []Value{this, arg(args, 1)})
			}
		}
		s, err := i.ToStringV(ctx, this)
		if err != nil {
			return nil, err
		}
		return i.stringSplitString(ctx, s, args)
	})
}

// invokeSymbol implements Invoke (§7.3.20) for a well-known symbol method: it
// re-reads the property from o (so a user-overridden method on the prototype is
// honored) and calls it with o as the receiver. A missing or non-callable
// method is a TypeError.
func (i *Interpreter) invokeSymbol(ctx context.Context, o *Object, sym *Symbol, name string, args []Value) (Value, error) {
	fn, err := o.Get(ctx, SymKey(sym))
	if err != nil {
		return nil, err
	}
	fo, ok := fn.(*Object)
	if !ok || !fo.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "the "+name+" property is not callable")
	}
	return i.call(ctx, fo, o, args)
}

// getMethod implements GetMethod (§7.3.11): Get(V, P); undefined/null → nil;
// otherwise it must be callable.
func (i *Interpreter) getMethod(ctx context.Context, v Value, sym *Symbol) (*Object, error) {
	o, ok := v.(*Object)
	if !ok {
		// GetMethod is defined via GetV (§7.3.11), which boxes primitives with
		// ToObject.  This lets well-known-symbol methods (e.g. a string's
		// @@iterator) be found on the primitive's prototype.  Nullish values
		// have no wrapper and thus no method.
		if IsNullish(v) {
			return nil, nil
		}
		boxed, err := i.ToObject(ctx, v)
		if err != nil {
			return nil, err
		}
		o = boxed
	}
	fn, err := o.Get(ctx, SymKey(sym))
	if err != nil {
		return nil, err
	}
	if IsNullish(fn) {
		return nil, nil
	}
	fo, ok := fn.(*Object)
	if !ok || !fo.IsCallable() {
		return nil, i.throwError(ctx, "TypeError", "the value of a well-known symbol method is not callable")
	}
	return fo, nil
}

// isRegExpValue implements IsRegExp (§22.1.3): an object whose @@match is truthy,
// or (absent @@match) one with a compiled matcher.
func (i *Interpreter) isRegExpValue(ctx context.Context, v Value) (bool, error) {
	o, ok := v.(*Object)
	if !ok {
		return false, nil
	}
	m, err := o.Get(ctx, SymKey(i.symMatch))
	if err != nil {
		return false, err
	}
	if !IsUndefined(m) {
		return ToBoolean(m), nil
	}
	_, isRe := regexpOf(o)
	return isRe, nil
}

// regExpCreate builds a RegExp from a (non-RegExp) pattern value and flag string.
func (i *Interpreter) regExpCreate(ctx context.Context, pattern Value, flags string) (*Object, error) {
	p := ""
	if !IsUndefined(pattern) {
		s, err := i.ToStringV(ctx, pattern)
		if err != nil {
			return nil, err
		}
		p = s
	}
	v, err := i.newRegExp(ctx, p, flags)
	if err != nil {
		return nil, err
	}
	return v.(*Object), nil
}

// toUnits encodes a RegExp subject to UTF-16 code units, memoizing the most
// recent (string -> []uint16) result. RegExp methods re-encode the same subject
// on every RegExpExec call — repeated .test(bigString), or a global
// match/replace/split/matchAll loop that calls exec once per match — so a
// single-entry cache keyed on string identity collapses that to one encode.
// String equality short-circuits on pointer identity, so a hit is O(1). The
// returned slice is shared and treated as read-only (matchers only read it); the
// interpreter is single-threaded, so no aliasing race is possible.
func (i *Interpreter) toUnits(s string) []uint16 {
	if i.unitsVal != nil && s == i.unitsKey {
		return i.unitsVal
	}
	u := jsregexp.ToUnits(s)
	i.unitsKey = s
	i.unitsVal = u
	return u
}

// regexExec runs one match honoring lastIndex for global/sticky regexes, updating
// lastIndex as ECMAScript's RegExpBuiltinExec specifies. It returns the code-unit
// submatch offsets or nil for no match.
func (i *Interpreter) regexExec(ctx context.Context, reObj *Object, re reEngine, units []uint16) ([]int, error) {
	f := re.Flags()
	useLast := f.Global || f.Sticky
	// RegExpBuiltinExec reads and coerces lastIndex unconditionally (step 4),
	// before consulting the global/sticky flags — so the getter and any valueOf
	// fire exactly once even for a plain regex.
	liV, err := reObj.GetStr(ctx, "lastIndex")
	if err != nil {
		return nil, err
	}
	lastIndex, err := i.toLength(ctx, liV)
	if err != nil {
		return nil, err
	}
	start := 0
	if useLast {
		start = lastIndex
		if start > len(units) {
			if err := i.setThrow(ctx, reObj, "lastIndex", Number(0)); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
	m, err := re.FindSubmatchIndex(ctx, units, start)
	if err != nil {
		return nil, err
	}
	if m == nil {
		if useLast {
			if err := i.setThrow(ctx, reObj, "lastIndex", Number(0)); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	if useLast {
		if err := i.setThrow(ctx, reObj, "lastIndex", Number(float64(m[1]))); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// setThrow implements the abstract operation Set(O, P, V, true): it performs an
// ordinary [[Set]] and raises a TypeError if the write cannot take effect
// (non-writable data property, setter-less accessor, or non-extensible object).
// The RegExp exec path and Symbol methods use it because the spec sets
// lastIndex with the Throw flag regardless of the surrounding strictness.
// toLength implements ToLength (§7.1.20): ToIntegerOrInfinity clamped to
// [0, 2^53-1]. The initial ToNumber may throw (e.g. an object with a throwing
// valueOf), which is why it takes a context and returns an error.
func (i *Interpreter) toLength(ctx context.Context, v Value) (int, error) {
	f, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	n := ToInteger(f)
	if n <= 0 {
		return 0, nil
	}
	const maxSafe = 1<<53 - 1
	if n > maxSafe {
		n = maxSafe
	}
	return int(n), nil
}

func (i *Interpreter) setThrow(ctx context.Context, o *Object, name string, v Value) error {
	ok, err := o.setStatus(ctx, StrKey(name), v)
	if err != nil {
		return err
	}
	if !ok {
		return i.throwError(ctx, "TypeError",
			"Cannot assign to read only property '"+name+"' of object")
	}
	return nil
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
	orderedNames := orderedGroupNames(names)
	if len(names) > 0 {
		groups := NewObject(nil)
		for _, name := range orderedNames {
			idx, ok := participatingGroupIndex(m, n, names[name])
			if !ok {
				groups.SetData(name, Undef)
			} else {
				groups.SetData(name, String(jsregexp.FromUnits(units[m[2*idx]:m[2*idx+1]])))
			}
		}
		arr.SetData("groups", groups)
	} else {
		arr.SetData("groups", Undef)
	}

	// The d flag adds an `indices` array of [start,end] pairs (§22.2.7.7,
	// MakeMatchIndicesIndexPairArray), parallel to the match array, with its own
	// named-group `.groups` object.
	if re.Flags().HasIndices {
		pair := func(s, e int) Value {
			if s < 0 {
				return Undef
			}
			return i.newArray([]Value{Number(float64(s)), Number(float64(e))})
		}
		indices := make([]Value, n)
		for g := 0; g < n; g++ {
			indices[g] = pair(m[2*g], m[2*g+1])
		}
		indicesArr := i.newArray(indices)
		if len(names) > 0 {
			ig := NewObject(nil)
			for _, name := range orderedNames {
				idx, ok := participatingGroupIndex(m, n, names[name])
				if !ok {
					ig.SetData(name, Undef)
				} else {
					ig.SetData(name, pair(m[2*idx], m[2*idx+1]))
				}
			}
			indicesArr.SetData("groups", ig)
		} else {
			indicesArr.SetData("groups", Undef)
		}
		arr.SetData("indices", indicesArr)
	}
	return arr
}

// orderedGroupNames returns the named-capture names sorted by capture index, so
// the exec result's `groups` object enumerates in source (left-to-right) order
// rather than the nondeterministic order of the underlying map (§22.2.7.2).
func orderedGroupNames(names map[string][]int) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Slice(out, func(a, b int) bool { return minIndex(names[out[a]]) < minIndex(names[out[b]]) })
	return out
}

// minIndex returns the smallest capture index among idxs (a name's first
// left-to-right occurrence), used to order the `groups` object.
func minIndex(idxs []int) int {
	lo := idxs[0]
	for _, i := range idxs[1:] {
		if i < lo {
			lo = i
		}
	}
	return lo
}

// participatingGroupIndex returns the capture index for a (possibly duplicated)
// group name that actually participated in match m — the one whose start offset
// is set. Duplicate names are mutually exclusive, so at most one qualifies. ok is
// false when none did (the name's value is `undefined`).
func participatingGroupIndex(m []int, n int, idxs []int) (int, bool) {
	for _, idx := range idxs {
		if idx < n && m[2*idx] >= 0 {
			return idx, true
		}
	}
	return 0, false
}

// stringSplitString implements String.prototype.split with a string separator.
func (i *Interpreter) stringSplitString(ctx context.Context, s string, args []Value) (Value, error) {
	// §22.1.3.21: ToUint32(limit) (step 4) is evaluated before ToString of the
	// separator (step 5), and a lim of 0 yields an empty array (step 6).
	var lim int64 = 1<<32 - 1
	if v := arg(args, 1); !IsUndefined(v) {
		f, err := i.ToNumberV(ctx, v)
		if err != nil {
			return nil, err
		}
		lim = int64(ToUint32(f))
	}
	if IsUndefined(arg(args, 0)) {
		if lim == 0 {
			return i.newArray([]Value{}), nil
		}
		return i.newArray([]Value{String(s)}), nil
	}
	sep, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	if lim == 0 {
		return i.newArray([]Value{}), nil
	}
	var parts []string
	if sep == "" {
		for _, r := range s {
			parts = append(parts, string(r))
		}
	} else {
		parts = strings.Split(s, sep)
	}
	out := make([]Value, 0, len(parts))
	for _, p := range parts {
		if int64(len(out)) >= lim {
			break
		}
		out = append(out, String(p))
	}
	return i.newArray(out), nil
}

// regexpFromArgs implements the RegExp constructor (§22.2.4.1). isCall is true
// when RegExp was called as a function (NewTarget undefined), which enables the
// same-object short-circuit for a RegExp/regexp-like argument with no flags.
func (i *Interpreter) regexpFromArgs(ctx context.Context, args []Value, isCall bool) (Value, error) {
	pat := arg(args, 0)
	flagsArg := arg(args, 1)

	// Step 1: patternIsRegExp = IsRegExp(pattern) (reads pattern[@@match] once and
	// propagates a throwing getter).
	patternIsRegExp, err := i.isRegExpValue(ctx, pat)
	if err != nil {
		return nil, err
	}

	// Step 2: when NewTarget is undefined and pattern is RegExp/regexp-like with
	// no explicit flags, return pattern itself iff its .constructor is RegExp.
	if isCall && patternIsRegExp && IsUndefined(flagsArg) {
		po := pat.(*Object)
		pc, err := po.GetStr(ctx, "constructor")
		if err != nil {
			return nil, err
		}
		if pco, ok := pc.(*Object); ok && pco == i.regexpCtor {
			return pat, nil
		}
	}

	pattern := ""
	flags := ""
	haveFlags := false
	switch {
	case func() bool { _, _, ok := regexpSourceFlags(pat); return ok }():
		// Step 4: pattern has [[RegExpMatcher]] — use its OriginalSource/Flags.
		src, fl, _ := regexpSourceFlags(pat)
		pattern = src
		if IsUndefined(flagsArg) {
			flags, haveFlags = fl, true
		}
	case patternIsRegExp:
		// Step 5: a regexp-like object — read source/flags through [[Get]].
		po := pat.(*Object)
		sv, err := po.GetStr(ctx, "source")
		if err != nil {
			return nil, err
		}
		if pattern, err = i.ToStringV(ctx, sv); err != nil {
			return nil, err
		}
		if IsUndefined(flagsArg) {
			fv, err := po.GetStr(ctx, "flags")
			if err != nil {
				return nil, err
			}
			if flags, err = i.ToStringV(ctx, fv); err != nil {
				return nil, err
			}
			haveFlags = true
		}
	case !IsUndefined(pat):
		// Step 6: an ordinary value coerced to the pattern source.
		if pattern, err = i.argStr(ctx, args, 0); err != nil {
			return nil, err
		}
	}

	if !haveFlags && !IsUndefined(flagsArg) {
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
	o := NewObject(i.regexpProto)
	o.class = "RegExp"
	o.internal = map[string]any{"regexp": re}
	// source and the flag booleans are exposed via RegExp.prototype accessors
	// (initRegExpAccessors), which read them from the compiled engine. lastIndex
	// is the sole own data property: RegExpAlloc (§22.2.3.2.1) defines it as
	// writable but non-enumerable and non-configurable.
	o.defineOwn(StrKey("lastIndex"), &Property{Value: Number(0), Writable: true})
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
