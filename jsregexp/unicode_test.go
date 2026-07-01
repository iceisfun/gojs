package jsregexp

import "testing"

func TestUnicodeAstral(t *testing.T) {
	// In u mode '.' and \u{...} span a whole astral code point (2 code units).
	if _, ok := matchStr(t, `^.$`, "u", "😀"); !ok {
		t.Error("/^.$/u should match a single astral char")
	}
	if got, _ := matchStr(t, `\u{1F600}`, "u", "x😀y"); got != "😀" {
		t.Errorf(`\u{1F600} = %q; want 😀`, got)
	}
	// In non-u mode the same char is two code units.
	if _, ok := matchStr(t, `^.$`, "", "😀"); ok {
		t.Error("/^.$/ (non-u) must NOT match an astral char as one unit")
	}
	if _, ok := matchStr(t, `^..$`, "", "😀"); !ok {
		t.Error("/^..$/ (non-u) should match an astral char as two units")
	}
	// A literal astral char in a non-u pattern matches its surrogate pair.
	if _, ok := matchStr(t, `😀`, "", "a😀b"); !ok {
		t.Error("non-u literal astral char should match its surrogate pair")
	}
	// Explicit surrogate escapes.
	if got, _ := matchStr(t, `😀`, "", "😀"); got != "😀" {
		t.Errorf("surrogate escapes = %q; want 😀", got)
	}
}

func TestUnicodeCaseFolding(t *testing.T) {
	// Greek final/medial sigma and capital sigma share a fold orbit.
	if _, ok := matchStr(t, `ς`, "iu", "Σ"); !ok {
		t.Error("/ς/iu should match Σ")
	}
	// Kelvin sign (U+212A) and long s (U+017F) fold into [a-z] under iu.
	if _, ok := matchStr(t, `[a-z]`, "iu", "K"); !ok {
		t.Error("Kelvin sign should match [a-z] under iu")
	}
	if _, ok := matchStr(t, `[a-z]`, "iu", "ſ"); !ok {
		t.Error("long s should match [a-z] under iu")
	}
	// The ASCII-boundary guard: without u, non-ASCII must not fold into ASCII.
	if _, ok := matchStr(t, `[a-z]`, "i", "K"); ok {
		t.Error("Kelvin sign must NOT match [a-z] under non-u i")
	}
}

func TestPropertyEscapes(t *testing.T) {
	cases := []struct {
		pat, flags, input, want string
		ok                      bool
	}{
		{`\p{L}+`, "u", "abcДЕ123", "abcДЕ", true},
		{`\p{Lu}+`, "u", "abCD", "CD", true},
		{`\p{Nd}+`, "u", "abc123", "123", true},
		{`\P{L}+`, "u", "ab..cd", "..", true},
		{`\p{Script=Greek}+`, "u", "abαβγz", "αβγ", true},
		{`\p{ASCII}+`, "u", "ab€cd", "ab", true},
		{`\p{White_Space}`, "u", "x y", " ", true},
	}
	for _, c := range cases {
		got, ok := matchStr(t, c.pat, c.flags, c.input)
		if ok != c.ok || got != c.want {
			t.Errorf("/%s/%s on %q = (%q,%v); want (%q,%v)", c.pat, c.flags, c.input, got, ok, c.want, c.ok)
		}
	}
}

func TestVModeSetOperations(t *testing.T) {
	// Intersection: ASCII letters only.
	if got, _ := matchStr(t, `[\p{L}&&\p{ASCII}]+`, "v", "abДЕcd"); got != "ab" {
		t.Errorf("intersection = %q; want ab", got)
	}
	// Difference: lowercase consonants.
	if got, _ := matchStr(t, `[[a-z]--[aeiou]]+`, "v", "xyzaei"); got != "xyz" {
		t.Errorf("difference = %q; want xyz", got)
	}
	// Nested negation inside intersection.
	if got, _ := matchStr(t, `[\w&&[^_]]+`, "v", "ab_cd"); got != "ab" {
		t.Errorf("nested negation = %q; want ab", got)
	}
}

func TestInvalidPropertyRejected(t *testing.T) {
	if _, err := Compile(`\p{NotARealProperty}`, "u"); err == nil {
		t.Error("unknown property should be a SyntaxError")
	}
}
