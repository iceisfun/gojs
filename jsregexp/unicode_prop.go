package jsregexp

import "unicode"

// resolveProperty resolves a Unicode property escape \p{Name} or \p{Name=Value}
// to a runeSet. This is an interim resolver covering General_Category, Script,
// Script_Extensions (approximated by Script), and a few binary properties; the
// full UnicodePropertyValue set and case-insensitive/string properties are
// completed in the unicode phase. Unknown properties yield a SyntaxError so
// callers fail loudly rather than mismatching silently.
func resolveProperty(name, value string) (*runeSet, error) {
	// General_Category with an explicit value, e.g. \p{General_Category=Letter}.
	switch name {
	case "General_Category", "gc":
		if rt := categoryTable(value); rt != nil {
			return rangeTableToSet(rt), nil
		}
		return nil, propErr(name, value)
	case "Script", "sc", "Script_Extensions", "scx":
		if rt := unicode.Scripts[canonicalScript(value)]; rt != nil {
			return rangeTableToSet(rt), nil
		}
		return nil, propErr(name, value)
	}

	if value != "" {
		return nil, propErr(name, value)
	}

	// Lone property: a General_Category shorthand, a binary property, or the
	// "Any"/"ASCII"/"Assigned" specials.
	if rt := categoryTable(name); rt != nil {
		return rangeTableToSet(rt), nil
	}
	if rt := binaryProperty(name); rt != nil {
		return rangeTableToSet(rt), nil
	}
	switch name {
	case "Any":
		return &runeSet{ranges: []rrange{{0, 0x10FFFF}}}, nil
	case "ASCII":
		return &runeSet{ranges: []rrange{{0, 0x7F}}}, nil
	case "Assigned":
		// Complement of the Cn (unassigned) category.
		var b setBuilder
		b.addComplement(rangeTableToSet(unicode.Categories["Cn"]).ranges)
		return b.build(), nil
	}
	return nil, propErr(name, value)
}

func propErr(name, value string) *SyntaxError {
	msg := "invalid Unicode property " + name
	if value != "" {
		msg += "=" + value
	}
	return &SyntaxError{Msg: msg, Pos: -1}
}

// categoryTable maps a General_Category long or short name to its RangeTable.
func categoryTable(v string) *unicode.RangeTable {
	if short, ok := categoryAlias[v]; ok {
		v = short
	}
	return unicode.Categories[v]
}

func binaryProperty(name string) *unicode.RangeTable {
	switch name {
	case "White_Space", "space":
		return unicode.White_Space
	case "Alphabetic", "Alpha":
		return unicode.Letter // approximation (excludes Nl/other Alphabetic)
	case "Uppercase", "Upper":
		return unicode.Upper
	case "Lowercase", "Lower":
		return unicode.Lower
	case "Math":
		return unicode.Other_Math
	case "Dash":
		return unicode.Dash
	case "Hex_Digit", "Hex":
		return unicode.Hex_Digit
	case "Ideographic":
		return unicode.Ideographic
	case "Noncharacter_Code_Point":
		return unicode.Noncharacter_Code_Point
	}
	return nil
}

// categoryAlias maps General_Category long names to Go's short keys.
var categoryAlias = map[string]string{
	"Letter":                "L",
	"Uppercase_Letter":      "Lu",
	"Lowercase_Letter":      "Ll",
	"Titlecase_Letter":      "Lt",
	"Modifier_Letter":       "Lm",
	"Other_Letter":          "Lo",
	"Mark":                  "M",
	"Nonspacing_Mark":       "Mn",
	"Spacing_Mark":          "Mc",
	"Enclosing_Mark":        "Me",
	"Number":                "N",
	"Decimal_Number":        "Nd",
	"Letter_Number":         "Nl",
	"Other_Number":          "No",
	"Punctuation":           "P",
	"Connector_Punctuation": "Pc",
	"Dash_Punctuation":      "Pd",
	"Open_Punctuation":      "Ps",
	"Close_Punctuation":     "Pe",
	"Initial_Punctuation":   "Pi",
	"Final_Punctuation":     "Pf",
	"Other_Punctuation":     "Po",
	"Symbol":                "S",
	"Math_Symbol":           "Sm",
	"Currency_Symbol":       "Sc",
	"Modifier_Symbol":       "Sk",
	"Other_Symbol":          "So",
	"Separator":             "Z",
	"Space_Separator":       "Zs",
	"Line_Separator":        "Zl",
	"Paragraph_Separator":   "Zp",
	"Other":                 "C",
	"Control":               "Cc",
	"Format":                "Cf",
	"Surrogate":             "Cs",
	"Private_Use":           "Co",
	"Unassigned":            "Cn",
}

// canonicalScript maps a few common script aliases to Go's unicode.Scripts keys;
// most names already match.
func canonicalScript(v string) string {
	switch v {
	case "Grek":
		return "Greek"
	case "Latn":
		return "Latin"
	case "Cyrl":
		return "Cyrillic"
	case "Hani":
		return "Han"
	}
	return v
}

// rangeTableToSet converts a unicode.RangeTable to a runeSet.
func rangeTableToSet(rt *unicode.RangeTable) *runeSet {
	if rt == nil {
		return &runeSet{}
	}
	var b setBuilder
	for _, r := range rt.R16 {
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			// Coalesce stride-1 runs into ranges; strided runs add singletons.
			if r.Stride == 1 {
				b.addRange(rune(r.Lo), rune(r.Hi))
				break
			}
			b.addRune(c)
		}
	}
	for _, r := range rt.R32 {
		if r.Stride == 1 {
			b.addRange(rune(r.Lo), rune(r.Hi))
			continue
		}
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			b.addRune(c)
		}
	}
	return b.build()
}
