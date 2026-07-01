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
		re, ok := regexpOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Method RegExp.prototype.exec called on incompatible receiver")
		}
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		m := re.FindStringSubmatchIndex(s)
		if m == nil {
			return Nul, nil
		}
		groups := re.FindStringSubmatch(s)
		vals := make([]Value, len(groups))
		for idx, g := range groups {
			vals[idx] = String(g)
		}
		result := i.newArray(vals)
		result.SetData("index", Number(float64(len([]rune(s[:m[0]])))))
		result.SetData("input", String(s))
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
	o.SetHidden("flags", String(flags))
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
