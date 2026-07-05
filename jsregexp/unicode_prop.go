package jsregexp

// resolveProperty resolves a Unicode property escape \p{Name} or \p{Name=Value}
// to the POSITIVE runeSet of code points it matches. The caller (compileClassSet)
// is responsible for complementing the set for \P{...}, so this function never
// pre-negates.
//
// It implements the ECMAScript UnicodeMatchProperty / UnicodeMatchPropertyValue
// abstract operations (§22.2): the non-binary properties General_Category (gc),
// Script (sc) and Script_Extensions (scx) with an explicit value, plus the fixed
// set of "lone" binary properties. Matching is case-sensitive and exact except
// for the aliases enumerated below (per PropertyAliases.txt /
// PropertyValueAliases.txt); no loose underscore/hyphen stripping is performed.
//
// Unknown property names/values yield a SyntaxError (via propErr) so callers fail
// loudly rather than mismatching silently.
func resolveProperty(name, value string) (*runeSet, error) {
	// Non-binary properties: \p{Name=Value}. Resolved against the generated
	// Unicode 17.0 tables (unicode_ucd17.go) so \p{} matches the same version the
	// conformance suite targets, rather than Go's older unicode package.
	if value != "" {
		switch canonicalPropertyName(name) {
		case "General_Category":
			if rs := lookupCategory(value); rs != nil {
				return rs, nil
			}
		case "Script":
			if rs := lookupScript(ucd17Script, resolveScriptName(value)); rs != nil {
				return rs, nil
			}
		case "Script_Extensions":
			if rs := lookupScript(ucd17ScriptExt, resolveScriptName(value)); rs != nil {
				return rs, nil
			}
		}
		return nil, propErr(name, value)
	}

	// Lone name: a General_Category shorthand (\p{Lu}, \p{Letter}), a binary
	// property, or one of the Any/ASCII/Assigned specials.
	if rs := lookupCategory(name); rs != nil {
		return rs, nil
	}
	if set := binaryProperty(name); set != nil {
		return set, nil
	}
	switch name {
	case "Any":
		return &runeSet{ranges: []rrange{{0, 0x10FFFF}}}, nil
	case "ASCII":
		return &runeSet{ranges: []rrange{{0, 0x7F}}}, nil
	case "Assigned":
		// Complement of the Cn (unassigned) category.
		var b setBuilder
		b.addComplement(ucd17Category["Cn"])
		return b.build(), nil
	}
	return nil, propErr(name, value)
}

// lookupScript resolves a Script or Script_Extensions value against a generated
// table. The special value "Unknown" (alias "Zzzz") is not stored: per UAX #24 it
// denotes the code points assigned to no explicit script, i.e. the complement of
// the union of every script's ranges. Both sc=Unknown and scx=Unknown reduce to
// this same complement, so it is computed on demand rather than tabulated.
func lookupScript(m map[string][]rrange, key string) *runeSet {
	if key == "Unknown" {
		var union setBuilder
		for _, rs := range m {
			union.addRanges(rs)
		}
		var b setBuilder
		b.addComplement(union.build().ranges)
		return b.build()
	}
	return ucdLookup(m, key)
}

// ucdLookup returns a fresh runeSet for key in a generated UCD 17.0 table, or
// nil when the key is absent. The copy keeps callers free to mutate the result.
func ucdLookup(m map[string][]rrange, key string) *runeSet {
	rs, ok := m[key]
	if !ok {
		return nil
	}
	return &runeSet{ranges: append([]rrange(nil), rs...)}
}

// lookupCategory resolves a General_Category long name or alias to its 17.0 set.
func lookupCategory(v string) *runeSet {
	if short, ok := categoryAlias[v]; ok {
		v = short
	}
	return ucdLookup(ucd17Category, v)
}

// resolveScriptName maps a 4-letter ISO 15924 alias to its long Script name.
func resolveScriptName(v string) string {
	if long, ok := scriptAlias[v]; ok {
		return long
	}
	return v
}

func propErr(name, value string) *SyntaxError {
	msg := "invalid Unicode property " + name
	if value != "" {
		msg += "=" + value
	}
	return &SyntaxError{Msg: msg, Pos: -1}
}

// canonicalPropertyName maps the accepted aliases of the three non-binary
// property names to their canonical long form.
func canonicalPropertyName(name string) string {
	switch name {
	case "General_Category", "gc":
		return "General_Category"
	case "Script", "sc":
		return "Script"
	case "Script_Extensions", "scx":
		return "Script_Extensions"
	}
	return name
}

// binaryProperty resolves a lone binary property name (long form or alias) to
// its positive runeSet from the generated Unicode 17.0 tables, or nil if the
// name is not a supported binary property.
func binaryProperty(name string) *runeSet {
	if canon, ok := binaryAlias[name]; ok {
		name = canon
	}
	return ucdLookup(ucd17Binary, name)
}

// binaryAlias maps every accepted short/alias form of a binary property to its
// canonical long name (per PropertyAliases.txt). The canonical names are then
// resolved by binaryProperty either via directBinary or its switch.
var binaryAlias = map[string]string{
	"AHex":    "ASCII_Hex_Digit",
	"Alpha":   "Alphabetic",
	"Bidi_C":  "Bidi_Control",
	"Bidi_M":  "Bidi_Mirrored",
	"CI":      "Case_Ignorable",
	"CWCF":    "Changes_When_Casefolded",
	"CWCM":    "Changes_When_Casemapped",
	"CWL":     "Changes_When_Lowercased",
	"CWKCF":   "Changes_When_NFKC_Casefolded",
	"CWT":     "Changes_When_Titlecased",
	"CWU":     "Changes_When_Uppercased",
	"DI":      "Default_Ignorable_Code_Point",
	"Dep":     "Deprecated",
	"Dia":     "Diacritic",
	"EComp":   "Emoji_Component",
	"EMod":    "Emoji_Modifier",
	"EBase":   "Emoji_Modifier_Base",
	"EPres":   "Emoji_Presentation",
	"ExtPict": "Extended_Pictographic",
	"Ext":     "Extender",
	"Gr_Base": "Grapheme_Base",
	"Gr_Ext":  "Grapheme_Extend",
	"Hex":     "Hex_Digit",
	"IDSB":    "IDS_Binary_Operator",
	"IDST":    "IDS_Trinary_Operator",
	"IDC":     "ID_Continue",
	"IDS":     "ID_Start",
	"Ideo":    "Ideographic",
	"Join_C":  "Join_Control",
	"LOE":     "Logical_Order_Exception",
	"Lower":   "Lowercase",
	"NChar":   "Noncharacter_Code_Point",
	"Pat_Syn": "Pattern_Syntax",
	"Pat_WS":  "Pattern_White_Space",
	"QMark":   "Quotation_Mark",
	"RI":      "Regional_Indicator",
	"STerm":   "Sentence_Terminal",
	"SD":      "Soft_Dotted",
	"Term":    "Terminal_Punctuation",
	"UIdeo":   "Unified_Ideograph",
	"Upper":   "Uppercase",
	"VS":      "Variation_Selector",
	"space":   "White_Space",
	"WSpace":  "White_Space",
	"XIDC":    "XID_Continue",
	"XIDS":    "XID_Start",
}

// categoryAlias maps General_Category long names (and the umbrella "Cased_Letter"
// group) to Go's short keys. Go already registers the short keys and the group
// keys (LC and the L/M/N/P/S/Z/C umbrellas), so only the long forms need mapping.
var categoryAlias = map[string]string{
	"Cased_Letter":          "LC",
	"Letter":                "L",
	"Uppercase_Letter":      "Lu",
	"Lowercase_Letter":      "Ll",
	"Titlecase_Letter":      "Lt",
	"Modifier_Letter":       "Lm",
	"Other_Letter":          "Lo",
	"Mark":                  "M",
	"Combining_Mark":        "M",
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
	"cntrl":                 "Cc",
	"Format":                "Cf",
	"Surrogate":             "Cs",
	"Private_Use":           "Co",
	"Unassigned":            "Cn",
}

// scriptAlias maps 4-letter ISO 15924 script codes to Go's unicode.Scripts long
// keys. Only aliases whose long form differs from the code need listing; the
// long names themselves already match unicode.Scripts keys.
var scriptAlias = map[string]string{
	"Adlm": "Adlam",
	"Aghb": "Caucasian_Albanian",
	"Ahom": "Ahom",
	"Arab": "Arabic",
	"Armi": "Imperial_Aramaic",
	"Armn": "Armenian",
	"Avst": "Avestan",
	"Bali": "Balinese",
	"Bamu": "Bamum",
	"Bass": "Bassa_Vah",
	"Batk": "Batak",
	"Beng": "Bengali",
	"Bhks": "Bhaiksuki",
	"Bopo": "Bopomofo",
	"Brah": "Brahmi",
	"Brai": "Braille",
	"Bugi": "Buginese",
	"Buhd": "Buhid",
	"Cakm": "Chakma",
	"Cans": "Canadian_Aboriginal",
	"Cari": "Carian",
	"Cham": "Cham",
	"Cher": "Cherokee",
	"Chrs": "Chorasmian",
	"Copt": "Coptic",
	"Cpmn": "Cypro_Minoan",
	"Cprt": "Cypriot",
	"Cyrl": "Cyrillic",
	"Deva": "Devanagari",
	"Diak": "Dives_Akuru",
	"Dogr": "Dogra",
	"Dsrt": "Deseret",
	"Dupl": "Duployan",
	"Egyp": "Egyptian_Hieroglyphs",
	"Elba": "Elbasan",
	"Elym": "Elymaic",
	"Ethi": "Ethiopic",
	"Geor": "Georgian",
	"Glag": "Glagolitic",
	"Gong": "Gunjala_Gondi",
	"Gonm": "Masaram_Gondi",
	"Goth": "Gothic",
	"Gran": "Grantha",
	"Grek": "Greek",
	"Gujr": "Gujarati",
	"Guru": "Gurmukhi",
	"Hang": "Hangul",
	"Hani": "Han",
	"Hano": "Hanunoo",
	"Hatr": "Hatran",
	"Hebr": "Hebrew",
	"Hira": "Hiragana",
	"Hluw": "Anatolian_Hieroglyphs",
	"Hmng": "Pahawh_Hmong",
	"Hmnp": "Nyiakeng_Puachue_Hmong",
	"Hung": "Old_Hungarian",
	"Ital": "Old_Italic",
	"Java": "Javanese",
	"Kali": "Kayah_Li",
	"Kana": "Katakana",
	"Kawi": "Kawi",
	"Khar": "Kharoshthi",
	"Khmr": "Khmer",
	"Khoj": "Khojki",
	"Kits": "Khitan_Small_Script",
	"Knda": "Kannada",
	"Kthi": "Kaithi",
	"Lana": "Tai_Tham",
	"Laoo": "Lao",
	"Latn": "Latin",
	"Lepc": "Lepcha",
	"Limb": "Limbu",
	"Lina": "Linear_A",
	"Linb": "Linear_B",
	"Lisu": "Lisu",
	"Lyci": "Lycian",
	"Lydi": "Lydian",
	"Mahj": "Mahajani",
	"Maka": "Makasar",
	"Mand": "Mandaic",
	"Mani": "Manichaean",
	"Marc": "Marchen",
	"Medf": "Medefaidrin",
	"Mend": "Mende_Kikakui",
	"Merc": "Meroitic_Cursive",
	"Mero": "Meroitic_Hieroglyphs",
	"Mlym": "Malayalam",
	"Modi": "Modi",
	"Mong": "Mongolian",
	"Mroo": "Mro",
	"Mtei": "Meetei_Mayek",
	"Mult": "Multani",
	"Mymr": "Myanmar",
	"Nagm": "Nag_Mundari",
	"Nand": "Nandinagari",
	"Narb": "Old_North_Arabian",
	"Nbat": "Nabataean",
	"Newa": "Newa",
	"Nkoo": "Nko",
	"Nshu": "Nushu",
	"Ogam": "Ogham",
	"Olck": "Ol_Chiki",
	"Orkh": "Old_Turkic",
	"Orya": "Oriya",
	"Osge": "Osage",
	"Osma": "Osmanya",
	"Ougr": "Old_Uyghur",
	"Palm": "Palmyrene",
	"Pauc": "Pau_Cin_Hau",
	"Perm": "Old_Permic",
	"Phag": "Phags_Pa",
	"Phli": "Inscriptional_Pahlavi",
	"Phlp": "Psalter_Pahlavi",
	"Phnx": "Phoenician",
	"Plrd": "Miao",
	"Prti": "Inscriptional_Parthian",
	"Rjng": "Rejang",
	"Rohg": "Hanifi_Rohingya",
	"Runr": "Runic",
	"Samr": "Samaritan",
	"Sarb": "Old_South_Arabian",
	"Saur": "Saurashtra",
	"Sgnw": "SignWriting",
	"Shaw": "Shavian",
	"Shrd": "Sharada",
	"Sidd": "Siddham",
	"Sind": "Khudawadi",
	"Sinh": "Sinhala",
	"Sogd": "Sogdian",
	"Sogo": "Old_Sogdian",
	"Sora": "Sora_Sompeng",
	"Soyo": "Soyombo",
	"Sund": "Sundanese",
	"Sylo": "Syloti_Nagri",
	"Syrc": "Syriac",
	"Tagb": "Tagbanwa",
	"Takr": "Takri",
	"Tale": "Tai_Le",
	"Talu": "New_Tai_Lue",
	"Taml": "Tamil",
	"Tang": "Tangut",
	"Tavt": "Tai_Viet",
	"Telu": "Telugu",
	"Tfng": "Tifinagh",
	"Tglg": "Tagalog",
	"Thaa": "Thaana",
	"Thai": "Thai",
	"Tibt": "Tibetan",
	"Tirh": "Tirhuta",
	"Tnsa": "Tangsa",
	"Toto": "Toto",
	"Ugar": "Ugaritic",
	"Vaii": "Vai",
	"Vith": "Vithkuqi",
	"Wara": "Warang_Citi",
	"Wcho": "Wancho",
	"Xpeo": "Old_Persian",
	"Xsux": "Cuneiform",
	"Yezi": "Yezidi",
	"Yiii": "Yi",
	"Zanb": "Zanabazar_Square",
	"Zinh": "Inherited",
	"Zyyy": "Common",
	"Zzzz": "Unknown",
}
