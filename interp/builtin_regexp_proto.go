package interp

import (
	"context"
	"strconv"
	"strings"

	"github.com/iceisfun/gojs/jsregexp"
)

// This file implements the ECMAScript RegExp.prototype accessor properties and
// the well-known-symbol methods (@@match, @@replace, @@search, @@split,
// @@matchAll), plus the RegExpExec / GetSubstitution / AdvanceStringIndex
// machinery they share (§22.2.6, §22.2.7). Flag state lives in the instance's
// compiled engine ([[OriginalFlags]]/[[OriginalSource]] analogue); the prototype
// exposes it through getters.

// initRegExpAccessors installs the get-only accessor properties on
// RegExp.prototype: source, flags, and one per flag letter.
func (i *Interpreter) initRegExpAccessors(proto *Object) {
	flag := func(name string, pick func(jsregexp.Flags) bool) {
		get := i.newNativeFunc("get "+name, 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
			o, ok := this.(*Object)
			if !ok {
				return nil, i.throwError(ctx, "TypeError", "RegExp.prototype."+name+" getter called on non-object")
			}
			re, ok := regexpOf(o)
			if !ok {
				if o == proto {
					return Undef, nil // get on %RegExp.prototype% itself
				}
				return nil, i.throwError(ctx, "TypeError", "RegExp.prototype."+name+" getter called on a non-RegExp object")
			}
			return Bool(pick(re.Flags())), nil
		})
		proto.DefineAccessor(name, get, nil, false)
	}

	flag("global", func(f jsregexp.Flags) bool { return f.Global })
	flag("ignoreCase", func(f jsregexp.Flags) bool { return f.IgnoreCase })
	flag("multiline", func(f jsregexp.Flags) bool { return f.Multiline })
	flag("dotAll", func(f jsregexp.Flags) bool { return f.DotAll })
	flag("unicode", func(f jsregexp.Flags) bool { return f.Unicode })
	flag("unicodeSets", func(f jsregexp.Flags) bool { return f.UnicodeSets })
	flag("sticky", func(f jsregexp.Flags) bool { return f.Sticky })
	flag("hasIndices", func(f jsregexp.Flags) bool { return f.HasIndices })

	// get source (§22.2.6.13): EscapeRegExpPattern of [[OriginalSource]], or
	// "(?:)" for %RegExp.prototype% / an empty pattern.
	proto.DefineAccessor("source", i.newNativeFunc("get source", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "RegExp.prototype.source getter called on non-object")
		}
		re, ok := regexpOf(o)
		if !ok {
			if o == proto {
				return String("(?:)"), nil
			}
			return nil, i.throwError(ctx, "TypeError", "RegExp.prototype.source getter called on a non-RegExp object")
		}
		return String(escapeRegExpPattern(re.Source())), nil
	}), nil, false)

	// get flags (§22.2.6.4): assemble the flag string by reading each individual
	// flag accessor through [[Get]], so subclasses that override them are honored.
	proto.DefineAccessor("flags", i.newNativeFunc("get flags", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "RegExp.prototype.flags getter called on non-object")
		}
		var b strings.Builder
		for _, fl := range []struct {
			prop string
			ch   byte
		}{
			{"hasIndices", 'd'}, {"global", 'g'}, {"ignoreCase", 'i'}, {"multiline", 'm'},
			{"dotAll", 's'}, {"unicode", 'u'}, {"unicodeSets", 'v'}, {"sticky", 'y'},
		} {
			v, err := o.GetStr(ctx, fl.prop)
			if err != nil {
				return nil, err
			}
			if ToBoolean(v) {
				b.WriteByte(fl.ch)
			}
		}
		return String(b.String()), nil
	}), nil, false)
}

// escapeRegExpPattern implements EscapeRegExpPattern (§22.2.6.13.1): it renders a
// pattern so that "/" + result + "/" is a valid RegularExpressionLiteral. An
// empty pattern becomes "(?:)"; an unescaped "/" and LF/CR are escaped.
func escapeRegExpPattern(src string) string {
	if src == "" {
		return "(?:)"
	}
	var b strings.Builder
	inClass := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '\\':
			b.WriteByte(c)
			if i+1 < len(src) {
				i++
				b.WriteByte(src[i])
			}
			continue
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				b.WriteString(`\/`)
				continue
			}
		case '\n':
			b.WriteString(`\n`)
			continue
		case '\r':
			b.WriteString(`\r`)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// --- well-known symbol methods (§22.2.6) ---

// initRegExpSymbols installs RegExp.prototype[@@match/@@matchAll/@@replace/
// @@search/@@split].
func (i *Interpreter) initRegExpSymbols(proto *Object) {
	def := func(sym *Symbol, name string, length int, fn CallFn) {
		m := i.newNativeFunc(name, length, fn)
		proto.defineOwn(SymKey(sym), &Property{Value: m, Writable: true, Configurable: true})
	}
	def(i.symSearch, "[Symbol.search]", 1, i.regexpSymbolSearch)
	def(i.symMatch, "[Symbol.match]", 1, i.regexpSymbolMatch)
	def(i.symMatchAll, "[Symbol.matchAll]", 1, i.regexpSymbolMatchAll)
	def(i.symReplace, "[Symbol.replace]", 2, i.regexpSymbolReplace)
	def(i.symSplit, "[Symbol.split]", 2, i.regexpSymbolSplit)
}

// regexpBuiltinExec is the intrinsic RegExp exec behavior (§22.2.7.2): it runs
// the compiled matcher against s honoring lastIndex, returning a match array or
// null. It backs both RegExp.prototype.exec and the RegExpExec fallback.
func (i *Interpreter) regexpBuiltinExec(ctx context.Context, o *Object, s string) (Value, error) {
	re, ok := regexpOf(o)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.prototype.exec called on a non-RegExp object")
	}
	units := jsregexp.ToUnits(s)
	m, err := i.regexExec(ctx, o, re, units)
	if err != nil {
		return nil, i.regexErr(ctx, err)
	}
	if m == nil {
		return Nul, nil
	}
	return i.submatchToArray(re, units, m), nil
}

// regExpExec is the abstract RegExpExec (§22.2.7.1): dispatch through the
// object's own "exec" property (so a user override participates), falling back to
// the builtin behavior when "exec" is not callable.
func (i *Interpreter) regExpExec(ctx context.Context, r *Object, s string) (Value, error) {
	execV, err := r.GetStr(ctx, "exec")
	if err != nil {
		return nil, err
	}
	if fn, ok := execV.(*Object); ok && fn.IsCallable() {
		res, err := i.call(ctx, fn, r, []Value{String(s)})
		if err != nil {
			return nil, err
		}
		if _, isObj := res.(*Object); !isObj && !IsNull(res) {
			return nil, i.throwError(ctx, "TypeError", "RegExp exec method returned a non-object, non-null value")
		}
		return res, nil
	}
	return i.regexpBuiltinExec(ctx, r, s)
}

// getStrProp is ToString(? Get(o, name)).
func (i *Interpreter) getStrProp(ctx context.Context, o *Object, name string) (string, error) {
	v, err := o.GetStr(ctx, name)
	if err != nil {
		return "", err
	}
	return i.ToStringV(ctx, v)
}

// lengthOfArrayLike is ToLength(? Get(o, "length")).
func (i *Interpreter) lengthOfArrayLike(ctx context.Context, o *Object) (int, error) {
	v, err := o.GetStr(ctx, "length")
	if err != nil {
		return 0, err
	}
	n := ToInteger(ToNumber(v))
	if n < 0 {
		n = 0
	}
	return int(n), nil
}

func (i *Interpreter) lastIndexInt(ctx context.Context, o *Object) (int, error) {
	v, err := o.GetStr(ctx, "lastIndex")
	if err != nil {
		return 0, err
	}
	return int(ToInteger(ToNumber(v))), nil
}

func (i *Interpreter) regexpSymbolSearch(ctx context.Context, this Value, args []Value) (Value, error) {
	rx, ok := this.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.prototype[Symbol.search] called on non-object")
	}
	s, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	prev, err := rx.GetStr(ctx, "lastIndex")
	if err != nil {
		return nil, err
	}
	if ToNumber(prev) != 0 {
		rx.SetData("lastIndex", Number(0))
	}
	res, err := i.regExpExec(ctx, rx, s)
	if err != nil {
		return nil, err
	}
	cur, err := rx.GetStr(ctx, "lastIndex")
	if err != nil {
		return nil, err
	}
	if ToNumber(cur) != ToNumber(prev) {
		rx.SetData("lastIndex", prev)
	}
	if IsNull(res) {
		return Number(-1), nil
	}
	return res.(*Object).GetStr(ctx, "index")
}

func (i *Interpreter) regexpSymbolMatch(ctx context.Context, this Value, args []Value) (Value, error) {
	rx, ok := this.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.prototype[Symbol.match] called on non-object")
	}
	s, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	flags, err := i.getStrProp(ctx, rx, "flags")
	if err != nil {
		return nil, err
	}
	if !strings.Contains(flags, "g") {
		return i.regExpExec(ctx, rx, s)
	}
	fullUnicode := strings.ContainsAny(flags, "uv")
	rx.SetData("lastIndex", Number(0))
	units := jsregexp.ToUnits(s)
	var results []Value
	for {
		res, err := i.regExpExec(ctx, rx, s)
		if err != nil {
			return nil, err
		}
		if IsNull(res) {
			if len(results) == 0 {
				return Nul, nil
			}
			return i.newArray(results), nil
		}
		matchStr, err := i.getStrProp(ctx, res.(*Object), "0")
		if err != nil {
			return nil, err
		}
		results = append(results, String(matchStr))
		if matchStr == "" {
			li, _ := i.lastIndexInt(ctx, rx)
			rx.SetData("lastIndex", Number(float64(advanceStringIndex(units, li, fullUnicode))))
		}
	}
}

func (i *Interpreter) regexpSymbolMatchAll(ctx context.Context, this Value, args []Value) (Value, error) {
	rx, ok := this.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.prototype[Symbol.matchAll] called on non-object")
	}
	s, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	// Clone into a fresh matcher so iteration does not disturb rx's lastIndex.
	matcher, err := i.cloneRegExp(ctx, rx)
	if err != nil {
		return nil, err
	}
	li, _ := i.lastIndexInt(ctx, rx)
	matcher.SetData("lastIndex", Number(float64(li)))
	flags, _ := i.getStrProp(ctx, rx, "flags")
	global := strings.Contains(flags, "g")
	fullUnicode := strings.ContainsAny(flags, "uv")
	units := jsregexp.ToUnits(s)
	done := false
	return i.newIterator(func() (Value, bool) {
		if done {
			return Undef, false
		}
		res, err := i.regExpExec(ctx, matcher, s)
		if err != nil || IsNull(res) {
			done = true
			return Undef, false
		}
		if !global {
			done = true
			return res, true
		}
		matchStr, _ := i.getStrProp(ctx, res.(*Object), "0")
		if matchStr == "" {
			mli, _ := i.lastIndexInt(ctx, matcher)
			matcher.SetData("lastIndex", Number(float64(advanceStringIndex(units, mli, fullUnicode))))
		}
		return res, true
	}), nil
}

// cloneRegExp builds a fresh RegExp with the same source and flags as rx.
func (i *Interpreter) cloneRegExp(ctx context.Context, rx *Object) (*Object, error) {
	if re, ok := regexpOf(rx); ok {
		v, err := i.newRegExp(ctx, re.Source(), re.Flags().String())
		if err != nil {
			return nil, err
		}
		return v.(*Object), nil
	}
	src, err := i.getStrProp(ctx, rx, "source")
	if err != nil {
		return nil, err
	}
	flags, err := i.getStrProp(ctx, rx, "flags")
	if err != nil {
		return nil, err
	}
	v, err := i.newRegExp(ctx, src, flags)
	if err != nil {
		return nil, err
	}
	return v.(*Object), nil
}

func (i *Interpreter) regexpSymbolReplace(ctx context.Context, this Value, args []Value) (Value, error) {
	rx, ok := this.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.prototype[Symbol.replace] called on non-object")
	}
	s, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	units := jsregexp.ToUnits(s)
	stringLength := len(units)
	replaceValue := arg(args, 1)
	replFn, functionalReplace := replaceValue.(*Object)
	functionalReplace = functionalReplace && replFn.IsCallable()
	var replTemplate string
	if !functionalReplace {
		replTemplate, err = i.ToStringV(ctx, replaceValue)
		if err != nil {
			return nil, err
		}
	}
	flags, err := i.getStrProp(ctx, rx, "flags")
	if err != nil {
		return nil, err
	}
	global := strings.Contains(flags, "g")
	fullUnicode := strings.ContainsAny(flags, "uv")
	if global {
		rx.SetData("lastIndex", Number(0))
	}
	var results []*Object
	for {
		res, err := i.regExpExec(ctx, rx, s)
		if err != nil {
			return nil, err
		}
		if IsNull(res) {
			break
		}
		ro := res.(*Object)
		results = append(results, ro)
		if !global {
			break
		}
		matchStr, err := i.getStrProp(ctx, ro, "0")
		if err != nil {
			return nil, err
		}
		if matchStr == "" {
			li, _ := i.lastIndexInt(ctx, rx)
			rx.SetData("lastIndex", Number(float64(advanceStringIndex(units, li, fullUnicode))))
		}
	}

	var accumulated []uint16
	nextSourcePosition := 0
	for _, ro := range results {
		resultLength, err := i.lengthOfArrayLike(ctx, ro)
		if err != nil {
			return nil, err
		}
		capturesCount := resultLength - 1
		if capturesCount < 0 {
			capturesCount = 0
		}
		matched, err := i.getStrProp(ctx, ro, "0")
		if err != nil {
			return nil, err
		}
		matchedUnits := jsregexp.ToUnits(matched)
		posV, err := ro.GetStr(ctx, "index")
		if err != nil {
			return nil, err
		}
		position := int(ToInteger(ToNumber(posV)))
		if position < 0 {
			position = 0
		}
		if position > stringLength {
			position = stringLength
		}
		captures := make([]Value, 0, capturesCount)
		for n := 1; n <= capturesCount; n++ {
			capV, err := ro.GetStr(ctx, strconv.Itoa(n))
			if err != nil {
				return nil, err
			}
			if IsUndefined(capV) {
				captures = append(captures, Undef)
			} else {
				cs, err := i.ToStringV(ctx, capV)
				if err != nil {
					return nil, err
				}
				captures = append(captures, String(cs))
			}
		}
		namedCaptures, err := ro.GetStr(ctx, "groups")
		if err != nil {
			return nil, err
		}
		var replacement string
		if functionalReplace {
			callArgs := make([]Value, 0, len(captures)+3)
			callArgs = append(callArgs, String(matched))
			callArgs = append(callArgs, captures...)
			callArgs = append(callArgs, Number(float64(position)), String(s))
			if !IsUndefined(namedCaptures) {
				callArgs = append(callArgs, namedCaptures)
			}
			rv, err := i.call(ctx, replFn, Undef, callArgs)
			if err != nil {
				return nil, err
			}
			replacement, err = i.ToStringV(ctx, rv)
			if err != nil {
				return nil, err
			}
		} else {
			replacement, err = i.getSubstitution(ctx, matchedUnits, units, position, captures, namedCaptures, replTemplate)
			if err != nil {
				return nil, err
			}
		}
		if position >= nextSourcePosition {
			accumulated = append(accumulated, units[nextSourcePosition:position]...)
			accumulated = append(accumulated, jsregexp.ToUnits(replacement)...)
			nextSourcePosition = position + len(matchedUnits)
		}
	}
	if nextSourcePosition < stringLength {
		accumulated = append(accumulated, units[nextSourcePosition:]...)
	}
	return String(jsregexp.FromUnits(accumulated)), nil
}

// getSubstitution implements GetSubstitution (§22.1.3.20.1) over UTF-16 units.
func (i *Interpreter) getSubstitution(ctx context.Context, matched, str []uint16, position int, captures []Value, namedCaptures Value, template string) (string, error) {
	t := jsregexp.ToUnits(template)
	stringLength := len(str)
	var out []uint16
	for j := 0; j < len(t); {
		if t[j] != '$' || j+1 >= len(t) {
			out = append(out, t[j])
			j++
			continue
		}
		c := t[j+1]
		switch {
		case c == '$':
			out = append(out, '$')
			j += 2
		case c == '&':
			out = append(out, matched...)
			j += 2
		case c == '`':
			out = append(out, str[:position]...)
			j += 2
		case c == '\'':
			tail := position + len(matched)
			if tail > stringLength {
				tail = stringLength
			}
			out = append(out, str[tail:]...)
			j += 2
		case c >= '0' && c <= '9':
			digitCount := 1
			if j+2 < len(t) && t[j+2] >= '0' && t[j+2] <= '9' {
				digitCount = 2
			}
			idx := int(c - '0')
			if digitCount == 2 {
				idx = idx*10 + int(t[j+2]-'0')
			}
			if idx > len(captures) && digitCount == 2 {
				digitCount = 1
				idx = int(c - '0')
			}
			if idx >= 1 && idx <= len(captures) {
				if cv, ok := captures[idx-1].(String); ok {
					out = append(out, jsregexp.ToUnits(string(cv))...)
				}
				j += 1 + digitCount
			} else {
				out = append(out, '$')
				j++
			}
		case c == '<':
			if IsUndefined(namedCaptures) || namedCaptures == nil {
				out = append(out, '$', '<')
				j += 2
				continue
			}
			end := -1
			for k := j + 2; k < len(t); k++ {
				if t[k] == '>' {
					end = k
					break
				}
			}
			if end < 0 {
				out = append(out, '$', '<')
				j += 2
				continue
			}
			name := jsregexp.FromUnits(t[j+2 : end])
			if no, ok := namedCaptures.(*Object); ok {
				capV, err := no.GetStr(ctx, name)
				if err != nil {
					return "", err
				}
				if !IsUndefined(capV) {
					cs, err := i.ToStringV(ctx, capV)
					if err != nil {
						return "", err
					}
					out = append(out, jsregexp.ToUnits(cs)...)
				}
			}
			j = end + 1
		default:
			out = append(out, '$')
			j++
		}
	}
	return jsregexp.FromUnits(out), nil
}

// speciesConstructor implements SpeciesConstructor (§7.3.22): the constructor to
// use for derived objects, honoring a @@species override, defaulting to def.
func (i *Interpreter) speciesConstructor(ctx context.Context, o *Object, def *Object) (*Object, error) {
	c, err := o.GetStr(ctx, "constructor")
	if err != nil {
		return nil, err
	}
	if IsUndefined(c) {
		return def, nil
	}
	co, ok := c.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "constructor is not an object")
	}
	s, err := co.Get(ctx, SymKey(i.symSpecies))
	if err != nil {
		return nil, err
	}
	if IsNullish(s) {
		return def, nil
	}
	if so, ok := s.(*Object); ok && so.IsConstructor() {
		return so, nil
	}
	return nil, i.throwError(ctx, "TypeError", "Symbol.species value is not a constructor")
}

func (i *Interpreter) regexpSymbolSplit(ctx context.Context, this Value, args []Value) (Value, error) {
	rx, ok := this.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp.prototype[Symbol.split] called on non-object")
	}
	s, err := i.argStr(ctx, args, 0)
	if err != nil {
		return nil, err
	}
	// C = SpeciesConstructor(rx, %RegExp%); splitter = Construct(C, «rx, newFlags»).
	defV, _ := i.global.GetStr(ctx, "RegExp")
	defCtor, _ := defV.(*Object)
	c, err := i.speciesConstructor(ctx, rx, defCtor)
	if err != nil {
		return nil, err
	}
	flags, err := i.getStrProp(ctx, rx, "flags")
	if err != nil {
		return nil, err
	}
	unicodeMatching := strings.ContainsAny(flags, "uv")
	newFlags := flags
	if !strings.Contains(flags, "y") {
		newFlags += "y"
	}
	splitterV, err := c.fn.construct(ctx, c, []Value{rx, String(newFlags)})
	if err != nil {
		return nil, err
	}
	splitter, ok := splitterV.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "RegExp splitter is not an object")
	}

	lim := int64(1)<<32 - 1
	if !IsUndefined(arg(args, 1)) {
		n, err := i.ToNumberV(ctx, arg(args, 1)) // ToUint32 begins with ToNumber, which may throw
		if err != nil {
			return nil, err
		}
		lim = int64(ToUint32(n))
	}
	if lim == 0 {
		return i.newArray(nil), nil
	}

	units := jsregexp.ToUnits(s)
	size := len(units)
	var out []Value
	push := func(v Value) bool {
		out = append(out, v)
		return int64(len(out)) < lim
	}

	if size == 0 {
		z, err := i.regExpExec(ctx, splitter, s)
		if err != nil {
			return nil, err
		}
		if !IsNull(z) {
			return i.newArray(nil), nil
		}
		return i.newArray([]Value{String("")}), nil
	}

	p := 0
	q := p
	for q < size {
		if err := splitter.SetStr(ctx, "lastIndex", Number(float64(q))); err != nil {
			return nil, err
		}
		z, err := i.regExpExec(ctx, splitter, s)
		if err != nil {
			return nil, err
		}
		if IsNull(z) {
			q = advanceStringIndex(units, q, unicodeMatching)
			continue
		}
		liV, err := splitter.GetStr(ctx, "lastIndex")
		if err != nil {
			return nil, err
		}
		liN, err := i.ToNumberV(ctx, liV)
		if err != nil {
			return nil, err
		}
		e := int(ToInteger(liN))
		if e < 0 {
			e = 0
		}
		if e > size {
			e = size
		}
		if e == p {
			q = advanceStringIndex(units, q, unicodeMatching)
			continue
		}
		if !push(String(jsregexp.FromUnits(units[p:q]))) {
			return i.newArray(out), nil
		}
		p = e
		zo := z.(*Object)
		lenV, err := zo.GetStr(ctx, "length")
		if err != nil {
			return nil, err
		}
		lenN, err := i.ToNumberV(ctx, lenV)
		if err != nil {
			return nil, err
		}
		numCaptures := int(ToInteger(lenN)) - 1
		if numCaptures < 0 {
			numCaptures = 0
		}
		for n := 1; n <= numCaptures; n++ {
			capV, err := zo.GetStr(ctx, strconv.Itoa(n))
			if err != nil {
				return nil, err
			}
			if !push(capV) {
				return i.newArray(out), nil
			}
		}
		q = p
	}
	push(String(jsregexp.FromUnits(units[p:])))
	return i.newArray(out), nil
}
