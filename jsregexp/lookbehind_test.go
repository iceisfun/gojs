package jsregexp

import (
	"context"
	"testing"
)

// These cases are lifted verbatim from Test262 built-ins/RegExp/lookBehind
// (captures.js, back-references-to-captures.js, do-not-backtrack.js, misc.js).
// They pin lookbehind's right-to-left matching semantics: a capture inside a
// lookbehind reflects the last *reverse* iteration, and backreferences /
// quantifiers inside it are matched leaning leftward. See §22.2.2.

const undef = "\x00undef" // sentinel for a group that did not participate

// matchStrs runs re against input at position 0 and returns the exec-style match
// (whole match followed by each capture), using undef for non-participating
// groups. ok is false when there is no match.
func matchStrs(t *testing.T, pattern, flags, input string) (groups []string, ok bool) {
	t.Helper()
	re, err := Compile(pattern, flags)
	if err != nil {
		t.Fatalf("Compile(/%s/%s): %v", pattern, flags, err)
	}
	units := ToUnits(input)
	loc, err := re.FindSubmatchIndex(context.Background(), units, 0)
	if err != nil {
		t.Fatalf("match /%s/%s on %q: %v", pattern, flags, input, err)
	}
	if loc == nil {
		return nil, false
	}
	for g := 0; g*2 < len(loc); g++ {
		s, e := loc[2*g], loc[2*g+1]
		if s < 0 {
			groups = append(groups, undef)
		} else {
			groups = append(groups, FromUnits(units[s:e]))
		}
	}
	return groups, true
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLookbehindCaptures(t *testing.T) {
	cases := []struct {
		pattern, flags, input string
		want                  []string // nil => expect no match
	}{
		// built-ins/RegExp/lookBehind/captures.js
		{`(?<=(c))def`, "", "abcdef", []string{"def", "c"}},              // #1
		{`(?<=(\w{2}))def`, "", "abcdef", []string{"def", "bc"}},         // #2
		{`(?<=(\w(\w)))def`, "", "abcdef", []string{"def", "bc", "c"}},   // #3
		{`(?<=(\w){3})def`, "", "abcdef", []string{"def", "a"}},          // #4 (reverse iteration)
		{`(?<=(bc)|(cd)).`, "", "abcdef", []string{"d", "bc", undef}},    // #5
		{`(?<=([ab]{1,2})\D|(abc))\w`, "", "abcdef", []string{"c", "a", undef}}, // #6
		{`\D(?<=([ab]+))(\w)`, "", "abcdef", []string{"ab", "a", "b"}},   // #7

		// built-ins/RegExp/lookBehind/back-references-to-captures.js
		{`(?<=\1(\w))d`, "i", "abcCd", []string{"d", "C"}},   // #1
		{`(?<=\1([abx]))d`, "", "abxxd", []string{"d", "x"}}, // #2
		{`(?<=\1(\w+))c`, "", "ababc", []string{"c", "ab"}},  // #3
		{`(?<=\1(\w+))c`, "", "ababbc", []string{"c", "b"}},  // #4
		{`(?<=\1(\w+))c`, "", "ababdc", nil},                 // #5
		{`(?<=(\w+)\1)c`, "", "ababc", []string{"c", "abab"}}, // #6

		// built-ins/RegExp/lookBehind/do-not-backtrack.js
		{`(?<=([abc]+)).\1`, "", "abcdbc", nil},

		// built-ins/RegExp/lookBehind/misc.js — must not regress
		{`^foo(?<=foo)$`, "", "foo", []string{"foo"}},        // #5
		{`^f.o(?<=foo)$`, "", "foo", []string{"foo"}},        // #6
		{`^f.o(?<!foo)$`, "", "fno", []string{"fno"}},        // #7
		{`^foooo(?<=fo+)$`, "", "foooo", []string{"foooo"}},  // #8
		{`^foooo(?<=fo*)$`, "", "foooo", []string{"foooo"}},  // #9
		{`^foo(?<!foo)$`, "", "foo", nil},                    // #3
	}
	for _, c := range cases {
		got, ok := matchStrs(t, c.pattern, c.flags, c.input)
		if c.want == nil {
			if ok {
				t.Errorf("/%s/%s on %q: expected no match, got %v", c.pattern, c.flags, c.input, got)
			}
			continue
		}
		if !ok {
			t.Errorf("/%s/%s on %q: expected %v, got no match", c.pattern, c.flags, c.input, c.want)
			continue
		}
		if !eqStrs(got, c.want) {
			t.Errorf("/%s/%s on %q: got %v, want %v", c.pattern, c.flags, c.input, got, c.want)
		}
	}
}
