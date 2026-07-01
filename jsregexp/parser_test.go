package jsregexp

import "testing"

// accept lists patterns (with flags) that must parse successfully.
var acceptCases = []struct{ pat, flags string }{
	{`abc`, ""},
	{`a|b|c`, ""},
	{`a*b+c?`, ""},
	{`a{2}`, ""},
	{`a{2,}`, ""},
	{`a{2,5}`, ""},
	{`a{2,5}?`, ""},
	{`(abc)`, ""},
	{`(?:abc)`, ""},
	{`(?<name>abc)`, ""},
	{`(?<name>a)\k<name>`, ""},
	{`(a)\1`, ""},
	{`\1(a)`, ""}, // forward numeric backreference
	{`^abc$`, ""},
	{`\bword\B`, ""},
	{`[a-z]`, ""},
	{`[^a-z]`, ""},
	{`[abc-]`, ""},
	{`[-abc]`, ""},
	{`[\d\w\s]`, ""},
	{`[\b]`, ""}, // backspace inside class
	{`a.b`, "s"},
	{`(?=ahead)`, ""},
	{`(?!ahead)`, ""},
	{`(?<=behind)`, ""},
	{`(?<!behind)`, ""},
	{`A`, ""},
	{`\u{1F600}`, "u"},
	{`\x41`, ""},
	{`\cA`, ""},
	{`\p{Letter}`, "u"},
	{`\P{Letter}`, "u"},
	{`[\p{L}]`, "u"},
	{`(?i:abc)`, ""},
	{`(?i-s:abc)`, ""},
	{`\0`, ""},
	{`{`, ""},      // Annex B: lone '{' is a literal
	{`}`, ""},      // Annex B: lone '}' is a literal
	{`]`, ""},      // Annex B: lone ']' is a literal
	{`a{`, ""},     // Annex B: not a quantifier, literal '{'
	{`\q`, ""},     // Annex B: identity escape 'q'
	{`(?=x)*`, ""}, // Annex B: quantifiable assertion
	{`[a&&b]`, "v"},
	{`[[a-z]--[aeiou]]`, "v"},
	{`😀`, "u"}, // surrogate pair
}

// reject lists patterns (with flags) that must be rejected as SyntaxError.
var rejectCases = []struct{ pat, flags string }{
	{`a**`, ""},
	{`*a`, ""},
	{`+a`, ""},
	{`?a`, ""},
	{`(abc`, ""},
	{`abc)`, ""},
	{`a{2,1}`, ""},                 // out of order
	{`[z-a]`, ""},                  // range out of order
	{`(?<a>a)(?<a>a)`, ""},         // duplicate name, same path
	{`(?<a>a)(?<b>b)(?<a>a)`, "u"}, // duplicate name across three groups
	{`(?<a>(?<a>x))`, ""},          // duplicate name, nested
	{`(?<n>a)\k<x>`, ""},           // named backref to unknown name
	{`\1`, "u"},                    // backref with no groups, unicode mode
	{`{`, "u"},                     // lone '{' in unicode mode
	{`}`, "u"},                     // lone '}' in unicode mode
	{`]`, "u"},                     // lone ']' in unicode mode
	{`\u{110000}`, "u"},            // code point out of range
	{`(?=x)*`, "u"},                // assertion not quantifiable in unicode mode
	{`(?<=x)*`, ""},                // lookbehind never quantifiable
	{`\x`, "u"},                    // incomplete hex escape in unicode mode
	{`\`, ""},                      // trailing backslash
}

func TestParserAccept(t *testing.T) {
	for _, c := range acceptCases {
		if _, err := Compile(c.pat, c.flags); err != nil {
			t.Errorf("Compile(%q, %q) = error %v; want ok", c.pat, c.flags, err)
		}
	}
}

func TestParserReject(t *testing.T) {
	for _, c := range rejectCases {
		if _, err := Compile(c.pat, c.flags); err == nil {
			t.Errorf("Compile(%q, %q) = ok; want SyntaxError", c.pat, c.flags)
		}
	}
}

func TestFlagsParsing(t *testing.T) {
	if _, err := ParseFlags("gg"); err == nil {
		t.Error("duplicate flag gg should fail")
	}
	if _, err := ParseFlags("uv"); err == nil {
		t.Error("u and v are mutually exclusive")
	}
	if _, err := ParseFlags("q"); err == nil {
		t.Error("unknown flag q should fail")
	}
	f, err := ParseFlags("dgimsy")
	if err != nil {
		t.Fatalf("valid flags: %v", err)
	}
	if !f.HasIndices || !f.Global || !f.IgnoreCase || !f.Multiline || !f.DotAll || !f.Sticky {
		t.Errorf("flag fields not all set: %+v", f)
	}
}
