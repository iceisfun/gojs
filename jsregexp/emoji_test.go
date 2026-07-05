package jsregexp

import (
	"context"
	"testing"
)

// \p{RGI_Emoji} is a v-mode property of strings: the union of Basic_Emoji and
// the RGI keycap/flag/modifier/tag/ZWJ sequence sets (Unicode Emoji 15.1). It
// matches whole multi-code-point emoji sequences, so /^\p{RGI_Emoji}+$/v anchors
// the entire string. Members added after 15.1 are out of scope (newer data).
func TestRGIEmojiProperty(t *testing.T) {
	re := MustCompile(`^\p{RGI_Emoji}+$`, "v")
	match := func(s string) bool {
		loc, err := re.FindStringSubmatchIndex(context.Background(), s, 0)
		if err != nil {
			t.Fatalf("match %q: %v", s, err)
		}
		return loc != nil
	}
	shouldMatch := []string{
		"⌚",                                // Basic_Emoji single code point (watch)
		"#️⃣",                              // keycap #
		"\U0001F1E7\U0001F1EA",             // flag (BE)
		"\U0001F385\U0001F3FB",             // modifier sequence (Santa + skin tone)
		"\U0001F469‍\U0001F469‍\U0001F467", // ZWJ family sequence
		"⌚\U0001F1E7\U0001F1EA",            // several in a row (the + quantifier)
	}
	for _, s := range shouldMatch {
		if !match(s) {
			t.Errorf(`\p{RGI_Emoji} should match %q`, s)
		}
	}
	shouldNotMatch := []string{
		"a",          // not emoji
		"\U0001F1E7", // a lone regional indicator is not an RGI flag
		"#",          // '#' without the keycap sequence
	}
	for _, s := range shouldNotMatch {
		if match(s) {
			t.Errorf(`\p{RGI_Emoji} should not match %q`, s)
		}
	}
}
