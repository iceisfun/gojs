package jsregexp

import "unicode"

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
	// Non-binary properties: \p{Name=Value}. Also reachable as \p{name} below is
	// rejected because these require a value.
	if value != "" {
		switch canonicalPropertyName(name) {
		case "General_Category":
			if rt := categoryTable(value); rt != nil {
				return rangeTableToSet(rt), nil
			}
		case "Script":
			if rt := scriptTable(value); rt != nil {
				return rangeTableToSet(rt), nil
			}
		case "Script_Extensions":
			// Go does not ship a Script_Extensions table, so approximate it with
			// the plain Script table (the two differ only for shared code points,
			// which scx additionally includes; Script is a safe subset).
			if rt := scriptTable(value); rt != nil {
				return rangeTableToSet(rt), nil
			}
		}
		return nil, propErr(name, value)
	}

	// Lone name: a General_Category shorthand (\p{Lu}, \p{Letter}), a binary
	// property, or one of the Any/ASCII/Assigned specials.
	if rt := categoryTable(name); rt != nil {
		return rangeTableToSet(rt), nil
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

// categoryTable maps a General_Category long name or short alias to its
// RangeTable, including the group categories (LC and the umbrellas L, M, N, P,
// S, Z, C), which Go already exposes as keys.
func categoryTable(v string) *unicode.RangeTable {
	if short, ok := categoryAlias[v]; ok {
		v = short
	}
	return unicode.Categories[v]
}

// scriptTable maps a Script value — a long name or a 4-letter ISO 15924 alias —
// to its RangeTable.
func scriptTable(v string) *unicode.RangeTable {
	if long, ok := scriptAlias[v]; ok {
		v = long
	}
	return unicode.Scripts[v]
}

// binaryProperty resolves a lone binary property name (long form or alias) to
// its positive runeSet, or nil if the name is not a supported binary property.
// The three kinds of source are: (a) tables Go exposes directly in
// unicode.Properties, (b) tables composed from Go's Categories/Properties, and
// (c) hardcoded tables in unicode_tables.go for properties Go cannot supply.
func binaryProperty(name string) *runeSet {
	// (a) Direct pass-throughs to unicode.Properties. Aliases are normalized to
	// the canonical name first.
	if canon, ok := binaryAlias[name]; ok {
		name = canon
	}
	if rt := directBinary[name]; rt != nil {
		return rangeTableToSet(rt)
	}
	// (b) + (c): composed and hardcoded properties.
	switch name {
	case "Alphabetic":
		return unionTables(cat("L"), cat("Nl"), prop("Other_Alphabetic"))
	case "Uppercase":
		return unionTables(cat("Lu"), prop("Other_Uppercase"))
	case "Lowercase":
		return unionTables(cat("Ll"), prop("Other_Lowercase"))
	case "Cased":
		return unionTables(cat("Lu"), cat("Ll"), cat("Lt"), prop("Other_Uppercase"), prop("Other_Lowercase"))
	case "Math":
		return unionTables(cat("Sm"), prop("Other_Math"))
	case "Grapheme_Extend":
		return graphemeExtend()
	case "Grapheme_Base":
		return graphemeBase()
	case "ID_Start":
		return idStart()
	case "ID_Continue":
		return idContinue()
	case "XID_Start":
		return subtract(idStart(), &runeSet{ranges: normalize(xidStartRemove)})
	case "XID_Continue":
		return subtract(idContinue(), &runeSet{ranges: normalize(xidContinueRemove)})
	case "Default_Ignorable_Code_Point":
		return defaultIgnorable()
	case "Case_Ignorable":
		return &runeSet{ranges: normalize(caseIgnorableRanges)}
	case "Bidi_Mirrored":
		return &runeSet{ranges: normalize(bidiMirroredRanges)}
	case "Emoji":
		return &runeSet{ranges: normalize(emojiRanges)}
	case "Emoji_Component":
		return &runeSet{ranges: normalize(emojiComponentRanges)}
	case "Emoji_Modifier":
		return &runeSet{ranges: normalize(emojiModifierRanges)}
	case "Emoji_Modifier_Base":
		return &runeSet{ranges: normalize(emojiModifierBaseRanges)}
	case "Emoji_Presentation":
		return &runeSet{ranges: normalize(emojiPresentationRanges)}
	case "Extended_Pictographic":
		return &runeSet{ranges: normalize(extendedPictographicRanges)}
	case "Changes_When_Casefolded":
		return &runeSet{ranges: normalize(changesWhenCasefoldedRanges)}
	case "Changes_When_Casemapped":
		return &runeSet{ranges: normalize(changesWhenCasemappedRanges)}
	case "Changes_When_Lowercased":
		return &runeSet{ranges: normalize(changesWhenLowercasedRanges)}
	case "Changes_When_NFKC_Casefolded":
		return &runeSet{ranges: normalize(changesWhenNFKCCasefoldedRanges)}
	case "Changes_When_Titlecased":
		return &runeSet{ranges: normalize(changesWhenTitlecasedRanges)}
	case "Changes_When_Uppercased":
		return &runeSet{ranges: normalize(changesWhenUppercasedRanges)}
	}
	return nil
}

// cat and prop are short accessors for Go's category and property tables.
func cat(k string) *unicode.RangeTable  { return unicode.Categories[k] }
func prop(k string) *unicode.RangeTable { return unicode.Properties[k] }

// unionTables builds the normalized union of one or more RangeTables.
func unionTables(tabs ...*unicode.RangeTable) *runeSet {
	var b setBuilder
	for _, t := range tabs {
		if t == nil {
			continue
		}
		b.ranges = append(b.ranges, rangeTableToSet(t).ranges...)
	}
	return b.build()
}

// idStart implements DerivedCoreProperties ID_Start:
//
//	ID_Start = L ∪ Nl ∪ Other_ID_Start − Pattern_Syntax − Pattern_White_Space
func idStart() *runeSet {
	base := unionTables(cat("L"), cat("Nl"), prop("Other_ID_Start"))
	return subtract(base, unionTables(prop("Pattern_Syntax"), prop("Pattern_White_Space")))
}

// idContinue implements DerivedCoreProperties ID_Continue:
//
//	ID_Continue = ID_Start ∪ Mn ∪ Mc ∪ Nd ∪ Pc ∪ Other_ID_Continue
//	              − Pattern_Syntax − Pattern_White_Space
func idContinue() *runeSet {
	var b setBuilder
	b.ranges = append(b.ranges, idStart().ranges...)
	b.ranges = append(b.ranges, unionTables(cat("Mn"), cat("Mc"), cat("Nd"), cat("Pc"), prop("Other_ID_Continue")).ranges...)
	base := b.build()
	return subtract(base, unionTables(prop("Pattern_Syntax"), prop("Pattern_White_Space")))
}

// graphemeExtend implements DerivedCoreProperties Grapheme_Extend:
//
//	Grapheme_Extend = Mn ∪ Me ∪ Other_Grapheme_Extend
func graphemeExtend() *runeSet {
	return unionTables(cat("Mn"), cat("Me"), prop("Other_Grapheme_Extend"))
}

// graphemeBase implements DerivedCoreProperties Grapheme_Base:
//
//	Grapheme_Base = [0..10FFFF] − Cc − Cf − Cs − Co − Cn − Zl − Zp − Grapheme_Extend
func graphemeBase() *runeSet {
	all := &runeSet{ranges: []rrange{{0, 0x10FFFF}}}
	excl := unionTables(cat("Cc"), cat("Cf"), cat("Cs"), cat("Co"), cat("Cn"), cat("Zl"), cat("Zp"))
	base := subtract(all, excl)
	return subtract(base, graphemeExtend())
}

// defaultIgnorable implements DerivedCoreProperties
// Default_Ignorable_Code_Point:
//
//	DI = Other_Default_Ignorable_Code_Point ∪ Cf ∪ Variation_Selector
//	     − White_Space − FFF9..FFFB − 13430..1343F − Prepended_Concatenation_Mark
func defaultIgnorable() *runeSet {
	base := unionTables(prop("Other_Default_Ignorable_Code_Point"), cat("Cf"), prop("Variation_Selector"))
	excl := unionTables(prop("White_Space"), prop("Prepended_Concatenation_Mark"))
	excl = &runeSet{ranges: normalize(append(append([]rrange{}, excl.ranges...),
		rrange{0xFFF9, 0xFFFB}, rrange{0x13430, 0x1343F}))}
	return subtract(base, excl)
}

// directBinary maps the canonical names of binary properties that Go exposes
// verbatim in unicode.Properties. Aliases are resolved to these names first via
// binaryAlias.
var directBinary = map[string]*unicode.RangeTable{
	"ASCII_Hex_Digit":         unicode.ASCII_Hex_Digit,
	"Bidi_Control":            unicode.Bidi_Control,
	"Dash":                    unicode.Dash,
	"Deprecated":              unicode.Deprecated,
	"Diacritic":               unicode.Diacritic,
	"Extender":                unicode.Extender,
	"Hex_Digit":               unicode.Hex_Digit,
	"IDS_Binary_Operator":     unicode.IDS_Binary_Operator,
	"IDS_Trinary_Operator":    unicode.IDS_Trinary_Operator,
	"Ideographic":             unicode.Ideographic,
	"Join_Control":            unicode.Join_Control,
	"Logical_Order_Exception": unicode.Logical_Order_Exception,
	"Noncharacter_Code_Point": unicode.Noncharacter_Code_Point,
	"Pattern_Syntax":          unicode.Pattern_Syntax,
	"Pattern_White_Space":     unicode.Pattern_White_Space,
	"Quotation_Mark":          unicode.Quotation_Mark,
	"Radical":                 unicode.Radical,
	"Regional_Indicator":      unicode.Regional_Indicator,
	"Sentence_Terminal":       unicode.Sentence_Terminal,
	"Soft_Dotted":             unicode.Soft_Dotted,
	"Terminal_Punctuation":    unicode.Terminal_Punctuation,
	"Unified_Ideograph":       unicode.Unified_Ideograph,
	"Variation_Selector":      unicode.Variation_Selector,
	"White_Space":             unicode.White_Space,
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
