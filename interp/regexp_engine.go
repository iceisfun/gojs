package interp

import (
	"context"
	"fmt"
	"regexp"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/iceisfun/gojs/jsregexp"
)

// reEngine is the backend a RegExp object dispatches to. Both the conformant
// jsregexp engine and the RE2 adapter satisfy it, so the RegExp built-in methods
// are written once against this interface. All offsets are UTF-16 code-unit
// indices, matching ECMAScript string indexing.
type reEngine interface {
	// FindSubmatchIndex returns 2*(NumGroups+1) code-unit offsets for the first
	// match at or after start (pair 0 is the whole match, -1/-1 for groups that
	// did not participate), or nil for no match.
	FindSubmatchIndex(ctx context.Context, units []uint16, start int) ([]int, error)
	Source() string
	Flags() jsregexp.Flags
	GroupNames() map[string]int
}

// compileRegExp compiles a pattern/flags pair with the interpreter's configured
// backend (jsregexp by default, RE2 when RegExpRE2 is selected).
func (i *Interpreter) compileRegExp(pattern, flags string) (reEngine, error) {
	if i.regexpEngine == RegExpRE2 {
		return compileRE2(pattern, flags)
	}
	return jsregexp.Compile(pattern, flags)
}

// re2Engine adapts Go's regexp (RE2) to reEngine. It is faster and linear-time
// but not ECMAScript-conformant: patterns using backreferences or lookaround do
// not compile, and capture/flag/Unicode semantics follow RE2.
type re2Engine struct {
	re     *regexp.Regexp
	source string
	flags  jsregexp.Flags
	names  map[string]int
}

func compileRE2(pattern, flags string) (reEngine, error) {
	f, err := jsregexp.ParseFlags(flags)
	if err != nil {
		return nil, err
	}
	// Translate the flags RE2 understands to inline form; g/y/d/u/v affect
	// runtime/semantics only. RE2 treats the pattern as UTF-8 (rune-oriented),
	// which approximates Unicode mode.
	var inline string
	if f.IgnoreCase {
		inline += "i"
	}
	if f.Multiline {
		inline += "m"
	}
	if f.DotAll {
		inline += "s"
	}
	goPat := pattern
	if inline != "" {
		goPat = "(?" + inline + ")" + pattern
	}
	re, err := regexp.Compile(goPat)
	if err != nil {
		// RE2 rejects backreferences and lookaround; surface as a SyntaxError
		// with the conformant prefix the RegExp constructor expects.
		return nil, fmt.Errorf("Invalid regular expression: %s", re2Reason(err))
	}
	names := map[string]int{}
	for idx, n := range re.SubexpNames() {
		if n != "" {
			names[n] = idx
		}
	}
	return &re2Engine{re: re, source: pattern, flags: f, names: names}, nil
}

func re2Reason(err error) string {
	msg := err.Error()
	// Go's regexp errors are "error parsing regexp: <reason>"; keep the reason.
	const pfx = "error parsing regexp: "
	if len(msg) > len(pfx) && msg[:len(pfx)] == pfx {
		return msg[len(pfx):]
	}
	return msg
}

func (e *re2Engine) Source() string             { return e.source }
func (e *re2Engine) Flags() jsregexp.Flags      { return e.flags }
func (e *re2Engine) GroupNames() map[string]int { return e.names }

// FindSubmatchIndex runs RE2 and maps its byte offsets back to code-unit
// offsets. RE2 is linear-time, so it needs no step budget; ctx is not consulted
// because a match cannot be interrupted mid-run.
func (e *re2Engine) FindSubmatchIndex(_ context.Context, units []uint16, start int) ([]int, error) {
	if start < 0 || start > len(units) {
		return nil, nil
	}
	s, byteAtUnit, unitAtByte := unitByteMaps(units)
	startByte := byteAtUnit[start]
	m := e.re.FindStringSubmatchIndex(s[startByte:])
	if m == nil {
		return nil, nil
	}
	out := make([]int, len(m))
	for i, b := range m {
		if b < 0 {
			out[i] = -1
		} else {
			out[i] = unitAtByte[b+startByte]
		}
	}
	// Emulate the sticky flag: the match must begin exactly at start.
	if e.flags.Sticky && out[0] != start {
		return nil, nil
	}
	return out, nil
}

// unitByteMaps decodes a UTF-16 code-unit slice into a UTF-8 string plus the two
// offset maps needed to translate between code-unit and byte positions.
func unitByteMaps(units []uint16) (string, []int, []int) {
	runes := utf16.Decode(units)
	var buf []byte
	byteAtUnit := make([]int, len(units)+1)
	ui := 0
	for _, r := range runes {
		span := 1
		if r > 0xFFFF {
			span = 2 // came from a surrogate pair (two code units)
		}
		byteAtUnit[ui] = len(buf)
		if span == 2 {
			byteAtUnit[ui+1] = len(buf)
		}
		buf = utf8.AppendRune(buf, r)
		ui += span
	}
	byteAtUnit[len(units)] = len(buf)

	unitAtByte := make([]int, len(buf)+1)
	for u := 0; u < len(units); u++ {
		for b := byteAtUnit[u]; b < byteAtUnit[u+1]; b++ {
			unitAtByte[b] = u
		}
	}
	unitAtByte[len(buf)] = len(units)
	return string(buf), byteAtUnit, unitAtByte
}
