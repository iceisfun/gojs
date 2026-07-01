package jsregexp

import (
	"context"
	"testing"
)

// find is a test helper: it compiles pat/flags and returns the submatch offsets
// of the first match in s at or after start.
func find(t *testing.T, pat, flags, s string) []int {
	t.Helper()
	re, err := Compile(pat, flags)
	if err != nil {
		t.Fatalf("Compile(%q,%q): %v", pat, flags, err)
	}
	loc, err := re.FindStringSubmatchIndex(context.Background(), s, 0)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}
	return loc
}

func matchStr(t *testing.T, pat, flags, s string) (string, bool) {
	loc := find(t, pat, flags, s)
	if loc == nil {
		return "", false
	}
	units := ToUnits(s)
	return FromUnits(units[loc[0]:loc[1]]), true
}

func TestMatchBasics(t *testing.T) {
	cases := []struct {
		pat, flags, input, want string
		ok                      bool
	}{
		{`abc`, "", "xxabcxx", "abc", true},
		{`a.c`, "", "a1c", "a1c", true},
		{`a.c`, "", "a\nc", "", false}, // . excludes newline
		{`a.c`, "s", "a\nc", "a\nc", true},
		{`a|bb|ccc`, "", "xccc", "ccc", true},
		{`a*`, "", "aaaa", "aaaa", true},
		{`a*?`, "", "aaaa", "", true}, // lazy
		{`a+`, "", "baaa", "aaa", true},
		{`a{2,3}`, "", "aaaa", "aaa", true},
		{`a{2,3}?`, "", "aaaa", "aa", true},
		{`\d+`, "", "abc123", "123", true},
		{`\w+`, "", "  foo_bar ", "foo_bar", true},
		{`[a-c]+`, "", "xxabcabx", "abcab", true},
		{`[^a-c]+`, "", "abcXYZabc", "XYZ", true},
		{`^abc$`, "", "abc", "abc", true},
		{`^b`, "m", "a\nbc", "b", true},
		{`ABC`, "i", "xabcx", "abc", true},
		{`[a-z]+`, "i", "XYZ", "XYZ", true},
		{`colou?r`, "", "color", "color", true},
		{`(?:ab)+`, "", "ababab", "ababab", true},
	}
	for _, c := range cases {
		got, ok := matchStr(t, c.pat, c.flags, c.input)
		if ok != c.ok || got != c.want {
			t.Errorf("/%s/%s on %q = (%q,%v); want (%q,%v)", c.pat, c.flags, c.input, got, ok, c.want, c.ok)
		}
	}
}

func TestCaptures(t *testing.T) {
	loc := find(t, `(a+)(b+)`, "", "xaaabbbx")
	if loc == nil {
		t.Fatal("no match")
	}
	// whole match, group1, group2
	if got := []int{loc[0], loc[1], loc[2], loc[3], loc[4], loc[5]}; !eqInts(got, []int{1, 7, 1, 4, 4, 7}) {
		t.Errorf("captures = %v; want [1 7 1 4 4 7]", got)
	}
}

func TestNamedGroupsAndBackref(t *testing.T) {
	if got, _ := matchStr(t, `(?<w>\w+)\s+\k<w>`, "", "hi foo foo bar"); got != "foo foo" {
		t.Errorf("named backref = %q; want %q", got, "foo foo")
	}
	if got, _ := matchStr(t, `(['"]).*?\1`, "", `say "hello" now`); got != `"hello"` {
		t.Errorf("backref quote match = %q; want %q", got, `"hello"`)
	}
	if _, ok := matchStr(t, `(a)(b)\2\1`, "", "abba"); !ok {
		t.Error(`(a)(b)\2\1 should match "abba"`)
	}
}

func TestLookaround(t *testing.T) {
	if got, _ := matchStr(t, `\d+(?= dollars)`, "", "100 dollars"); got != "100" {
		t.Errorf("lookahead = %q; want 100", got)
	}
	if got, _ := matchStr(t, `\d+(?! dollars)`, "", "100 euros"); got != "100" {
		t.Errorf("neg lookahead = %q; want 100", got)
	}
	if got, _ := matchStr(t, `(?<=\$)\d+`, "", "$42"); got != "42" {
		t.Errorf("lookbehind = %q; want 42", got)
	}
	if got, _ := matchStr(t, `(?<!\$)\d+`, "", "€42"); got != "42" {
		t.Errorf("neg lookbehind = %q; want 42", got)
	}
}

func TestNullableQuantifierTerminates(t *testing.T) {
	// A nullable body under a star must not loop forever.
	if got, _ := matchStr(t, `(a*)*b`, "", "aaab"); got != "aaab" {
		t.Errorf("(a*)*b = %q; want aaab", got)
	}
	if _, ok := matchStr(t, `(a*)*`, "", ""); !ok {
		t.Error("(a*)* should match empty")
	}
}

func TestStepBudgetStopsCatastrophicBacktracking(t *testing.T) {
	re, err := Compile(`(a+)+$`, "")
	if err != nil {
		t.Fatal(err)
	}
	re.SetStepBudget(200_000)
	// Classic catastrophic input: many a's then a non-matching char.
	input := ToUnits("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa!")
	_, err = re.FindSubmatchIndex(context.Background(), input, 0)
	if err != ErrBudget {
		t.Fatalf("expected ErrBudget on catastrophic backtracking, got %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	re := MustCompile(`(a+)+$`, "")
	re.SetStepBudget(0) // disable budget so only ctx stops it
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled
	input := ToUnits("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa!")
	_, err := re.FindSubmatchIndex(ctx, input, 0)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func eqInts(a, b []int) bool {
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
