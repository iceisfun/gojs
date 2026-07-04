package jsregexp

import "testing"

// Duplicate named capture groups (ES2025, regexp-duplicate-named-groups): the
// same name may appear in mutually exclusive alternatives, each with its own
// index, and \k<name> resolves to whichever occurrence participated. These are
// the engine-observable cases from Test262 built-ins/RegExp/named-groups/
// duplicate-names-{exec,match}.js (the .groups object and $<name> substitution
// are exercised by the interpreter-level suite).
func TestDuplicateNamedGroups(t *testing.T) {
	cases := []struct {
		pattern, input string
		want           []string // nil => no match
	}{
		{`(?<x>a)|(?<x>b)`, "bab", []string{"b", undef, "b"}},
		{`(?<x>b)|(?<x>a)`, "bab", []string{"b", "b", undef}},
		{`(?:(?<x>a)|(?<x>b))\k<x>`, "aa", []string{"aa", "a", undef}},
		{`(?:(?<x>a)|(?<x>b))\k<x>`, "bb", []string{"bb", undef, "b"}},
		{`(?:(?:(?<x>a)|(?<x>b))\k<x>){2}`, "aabb", []string{"aabb", undef, "b"}},
		{`(?:(?:(?<x>a)|(?<x>b))\k<x>){2}`, "abab", nil},
		{`(?:(?<x>a)|(?<x>b))\k<x>`, "abab", nil},
		{`^(?:(?<a>x)|(?<a>y)|z)\k<a>$`, "xx", []string{"xx", "x", undef}},
	}
	for _, c := range cases {
		got, ok := matchStrs(t, c.pattern, "", c.input)
		if c.want == nil {
			if ok {
				t.Errorf("/%s/ on %q: expected no match, got %v", c.pattern, c.input, got)
			}
			continue
		}
		if !ok {
			t.Errorf("/%s/ on %q: expected %v, got no match", c.pattern, c.input, c.want)
		} else if !eqStrs(got, c.want) {
			t.Errorf("/%s/ on %q: got %v, want %v", c.pattern, c.input, got, c.want)
		}
	}
}
