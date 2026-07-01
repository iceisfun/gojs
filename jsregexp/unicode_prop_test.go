package jsregexp

import "testing"

// wantMember is one membership expectation for a resolved property set.
type wantMember struct {
	r  rune
	in bool
}

// propCase drives a single \p{name} or \p{name=value} resolution and asserts
// which representative code points are (not) members of the resulting set.
type propCase struct {
	name  string
	value string
	want  []wantMember
}

func runPropCases(t *testing.T, cases []propCase) {
	t.Helper()
	for _, c := range cases {
		label := c.name
		if c.value != "" {
			label = c.name + "=" + c.value
		}
		set, err := resolveProperty(c.name, c.value)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", label, err)
			continue
		}
		for _, w := range c.want {
			if got := set.contains(w.r); got != w.in {
				t.Errorf("%s: contains(U+%04X) = %v, want %v", label, w.r, got, w.in)
			}
		}
	}
}

func TestResolvePropertyGeneralCategory(t *testing.T) {
	runPropCases(t, []propCase{
		{"Lu", "", []wantMember{{'A', true}, {'a', false}, {'5', false}}},
		{"Ll", "", []wantMember{{'a', true}, {'A', false}}},
		{"Lt", "", []wantMember{{0x01C5, true}, {'A', false}}}, // Dž
		{"Nd", "", []wantMember{{'5', true}, {'a', false}}},
		{"L", "", []wantMember{{'a', true}, {'A', true}, {0x00E9, true}, {'5', false}}},
		{"Letter", "", []wantMember{{'a', true}, {'5', false}}},
		{"LC", "", []wantMember{{'A', true}, {'a', true}, {'5', false}}},
		{"Cased_Letter", "", []wantMember{{'A', true}, {'a', true}, {0x01C5, true}, {'5', false}}},
		{"N", "", []wantMember{{'5', true}, {0x00BD, true}, {'a', false}}}, // ½ is No
		{"P", "", []wantMember{{',', true}, {'!', true}, {'a', false}}},
		{"S", "", []wantMember{{'+', true}, {'$', true}, {'a', false}}},
		{"Zs", "", []wantMember{{0x20, true}, {0xA0, true}, {'a', false}}},
		{"Cc", "", []wantMember{{0x00, true}, {0x1F, true}, {'a', false}}},
		{"Space_Separator", "", []wantMember{{0x20, true}, {'a', false}}},
		{"Other_Punctuation", "", []wantMember{{'!', true}, {',', true}, {'(', false}}}, // '(' is Ps, not Po
		// gc= form and aliases.
		{"General_Category", "Letter", []wantMember{{'a', true}, {'5', false}}},
		{"gc", "Lu", []wantMember{{'A', true}, {'a', false}}},
		{"General_Category", "Decimal_Number", []wantMember{{'5', true}, {'a', false}}},
	})
}

func TestResolvePropertyScript(t *testing.T) {
	runPropCases(t, []propCase{
		{"Script", "Greek", []wantMember{{0x03B1, true}, {'a', false}}},
		{"Script", "Latin", []wantMember{{'a', true}, {0x03B1, false}}},
		{"Script", "Cyrillic", []wantMember{{0x0410, true}, {'a', false}}},
		{"Script", "Han", []wantMember{{0x4E00, true}, {'a', false}}},
		{"sc", "Greek", []wantMember{{0x03B1, true}}},
		// ISO 15924 4-letter aliases.
		{"Script", "Grek", []wantMember{{0x03B1, true}, {'a', false}}},
		{"Script", "Latn", []wantMember{{'a', true}}},
		{"Script", "Cyrl", []wantMember{{0x0410, true}}},
		{"Script", "Hani", []wantMember{{0x4E00, true}}},
		// Script_Extensions is approximated by Script; it must at least contain the
		// script's own code points.
		{"Script_Extensions", "Greek", []wantMember{{0x03B1, true}, {'a', false}}},
		{"scx", "Latin", []wantMember{{'a', true}}},
	})
}

func TestResolvePropertyBinaryDirect(t *testing.T) {
	runPropCases(t, []propCase{
		{"White_Space", "", []wantMember{{0x20, true}, {0x09, true}, {0x2028, true}, {'a', false}}},
		{"Hex_Digit", "", []wantMember{{'F', true}, {'0', true}, {0xFF26, true}, {'G', false}}},
		{"ASCII_Hex_Digit", "", []wantMember{{'F', true}, {0xFF26, false}, {'G', false}}},
		{"Dash", "", []wantMember{{'-', true}, {'a', false}}},
		{"Ideographic", "", []wantMember{{0x4E00, true}, {'a', false}}},
		{"Noncharacter_Code_Point", "", []wantMember{{0xFFFE, true}, {0xFFFF, true}, {'a', false}}},
		{"Variation_Selector", "", []wantMember{{0xFE00, true}, {'a', false}}},
		{"Join_Control", "", []wantMember{{0x200C, true}, {0x200D, true}, {'a', false}}},
		{"Quotation_Mark", "", []wantMember{{'"', true}, {'\'', true}, {'a', false}}},
		{"Regional_Indicator", "", []wantMember{{0x1F1E6, true}, {'a', false}}},
		{"Sentence_Terminal", "", []wantMember{{'.', true}, {'!', true}, {'a', false}}},
		{"Pattern_White_Space", "", []wantMember{{0x20, true}, {0x09, true}, {'a', false}}},
	})
}

func TestResolvePropertyDerived(t *testing.T) {
	runPropCases(t, []propCase{
		{"Alphabetic", "", []wantMember{{'a', true}, {0x00E9, true}, {0x00AA, true}, {'1', false}}},
		{"Uppercase", "", []wantMember{{'A', true}, {'a', false}, {'1', false}}},
		{"Lowercase", "", []wantMember{{'a', true}, {'A', false}}},
		{"Cased", "", []wantMember{{'a', true}, {'A', true}, {0x01C5, true}, {'5', false}}},
		{"Math", "", []wantMember{{'+', true}, {0x2200, true}, {'a', false}}},
		{"Grapheme_Extend", "", []wantMember{{0x0300, true}, {'a', false}}},
		{"Grapheme_Base", "", []wantMember{{'a', true}, {0x0300, false}, {0x00, false}}},
		{"ID_Start", "", []wantMember{{'a', true}, {'A', true}, {'1', false}, {'_', false}}},
		{"ID_Continue", "", []wantMember{{'a', true}, {'1', true}, {'_', true}, {' ', false}}},
		{"XID_Start", "", []wantMember{{'a', true}, {0x037A, false}}},
		{"XID_Continue", "", []wantMember{{'a', true}, {'1', true}, {0x037A, false}}},
		{"Default_Ignorable_Code_Point", "", []wantMember{{0x00AD, true}, {0x200B, true}, {0x13430, false}, {'a', false}}},
	})
}

func TestResolvePropertyHardcoded(t *testing.T) {
	runPropCases(t, []propCase{
		{"Case_Ignorable", "", []wantMember{{'.', true}, {'\'', true}, {0x0300, true}, {'a', false}}},
		{"Bidi_Mirrored", "", []wantMember{{'(', true}, {')', true}, {'[', true}, {'a', false}}},
		{"Emoji", "", []wantMember{{0x1F600, true}, {'#', true}, {'a', false}}},
		{"Emoji_Presentation", "", []wantMember{{0x1F600, true}, {'#', false}, {'a', false}}},
		{"Emoji_Modifier", "", []wantMember{{0x1F3FB, true}, {0x1F600, false}}},
		{"Emoji_Modifier_Base", "", []wantMember{{0x1F466, true}, {0x261D, true}, {'a', false}}},
		{"Emoji_Component", "", []wantMember{{0x200D, true}, {0x1F3FB, true}, {'a', false}}},
		{"Extended_Pictographic", "", []wantMember{{0x1F600, true}, {'a', false}}},
		{"Changes_When_Uppercased", "", []wantMember{{'a', true}, {'A', false}, {'1', false}}},
		{"Changes_When_Lowercased", "", []wantMember{{'A', true}, {'a', false}}},
		{"Changes_When_Casefolded", "", []wantMember{{'A', true}, {'a', false}}},
		{"Changes_When_Titlecased", "", []wantMember{{'a', true}, {0x01C5, false}}},
		{"Changes_When_Casemapped", "", []wantMember{{'a', true}, {'A', true}, {'1', false}}},
		{"Changes_When_NFKC_Casefolded", "", []wantMember{{'A', true}, {0x00B5, true}, {'a', false}}}, // micro sign folds
	})
}

func TestResolvePropertySpecials(t *testing.T) {
	runPropCases(t, []propCase{
		{"Any", "", []wantMember{{0x00, true}, {0x10FFFF, true}}},
		{"ASCII", "", []wantMember{{0x00, true}, {0x7F, true}, {0x80, false}}},
		{"Assigned", "", []wantMember{{'a', true}, {0x0378, false}}}, // U+0378 is unassigned
	})
}

func TestResolvePropertyAliases(t *testing.T) {
	runPropCases(t, []propCase{
		{"Alpha", "", []wantMember{{'a', true}, {'1', false}}},
		{"WSpace", "", []wantMember{{0x20, true}, {'a', false}}},
		{"space", "", []wantMember{{0x20, true}, {'a', false}}},
		{"AHex", "", []wantMember{{'F', true}, {'G', false}}},
		{"Hex", "", []wantMember{{'F', true}, {0xFF26, true}}},
		{"IDS", "", []wantMember{{'a', true}, {'1', false}}},
		{"IDC", "", []wantMember{{'a', true}, {'1', true}}},
		{"XIDS", "", []wantMember{{'a', true}, {0x037A, false}}},
		{"Gr_Ext", "", []wantMember{{0x0300, true}, {'a', false}}},
		{"Gr_Base", "", []wantMember{{'a', true}, {0x0300, false}}},
		{"VS", "", []wantMember{{0xFE00, true}, {'a', false}}},
		{"RI", "", []wantMember{{0x1F1E6, true}, {'a', false}}},
		{"STerm", "", []wantMember{{'.', true}, {'a', false}}},
		{"Upper", "", []wantMember{{'A', true}, {'a', false}}},
		{"Lower", "", []wantMember{{'a', true}, {'A', false}}},
		{"CI", "", []wantMember{{0x0300, true}, {'a', false}}},
		{"DI", "", []wantMember{{0x00AD, true}, {'a', false}}},
		{"Ideo", "", []wantMember{{0x4E00, true}, {'a', false}}},
		{"EPres", "", []wantMember{{0x1F600, true}, {'a', false}}},
		{"ExtPict", "", []wantMember{{0x1F600, true}, {'a', false}}},
	})
}

// TestResolvePropertyErrors verifies that unsupported or bogus property names and
// values fail loudly with a SyntaxError instead of resolving to a silent set.
func TestResolvePropertyErrors(t *testing.T) {
	bad := []struct{ name, value string }{
		{"Bogus", ""},                        // unknown lone name
		{"Klingon", ""},                      // unknown script-like name
		{"General_Category", "Bogus"},        // unknown gc value
		{"Script", "Klingon"},                // unknown script value
		{"Script_Extensions", "Nope"},        // unknown scx value
		{"Block", "Latin"},                   // Blocks are not supported by ECMAScript
		{"gc", ""},                           // gc requires a value
		{"Lu", "Foo"},                        // a lone GC name takes no value
		{"Hyphen", ""},                       // deprecated: not an ECMAScript binary property
		{"Other_Alphabetic", ""},             // Other_* are building blocks, not \p{} names
		{"Other_Uppercase", ""},              // ditto
		{"Prepended_Concatenation_Mark", ""}, // not an ECMAScript binary property
		{"Line_Break", ""},                   // enumerated, not binary; unsupported
		{"General_Category", ""},             // handled as lone below, but empty value string here
	}
	for _, b := range bad {
		if set, err := resolveProperty(b.name, b.value); err == nil {
			t.Errorf("resolveProperty(%q,%q) = %v, want error", b.name, b.value, set.ranges)
		}
	}
}
