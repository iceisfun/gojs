package jsregexp

// scanGroups performs a linear pre-scan over the pattern to count capturing
// groups and collect named-group indices. This must happen before the main
// parse because numeric and named backreferences may refer to groups that
// appear later in the source. It only needs paren structure, so it skips escaped
// characters and character-class interiors (where '(' is a literal).
func scanGroups(src []rune) (int, map[string][]int, error) {
	names := map[string][]int{}
	count := 0
	inClass := false
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\\':
			i += 2 // skip the escaped code point
		case c == '[' && !inClass:
			inClass = true
			i++
		case c == ']' && inClass:
			inClass = false
			i++
		case c == '(' && !inClass:
			if i+1 < len(src) && src[i+1] == '?' {
				// (?<name>...) is a named capture unless it is a lookbehind.
				if i+2 < len(src) && src[i+2] == '<' &&
					(i+3 >= len(src) || (src[i+3] != '=' && src[i+3] != '!')) {
					count++
					name, end, ok := scanGroupName(src, i+3)
					if !ok {
						return 0, nil, errAt(i, "invalid capture group name")
					}
					// Record every occurrence's index; a name duplicated across
					// alternatives (ES2025) has several, and \k<name> / .groups
					// resolve to whichever one participated in the match.
					names[name] = append(names[name], count)
					i = end
					continue
				}
				i++ // non-capturing group / assertion / modifier
			} else {
				count++
				i++
			}
		default:
			i++
		}
	}
	return count, names, nil
}

// scanGroupName reads a group name (starting after the '<') up to the '>',
// decoding \u escapes. It performs no identifier validation — that is the main
// parser's job — it only needs the textual name and the index past '>'.
func scanGroupName(src []rune, i int) (string, int, bool) {
	var name []rune
	for i < len(src) {
		c := src[i]
		if c == '>' {
			if len(name) == 0 {
				return "", 0, false
			}
			return string(name), i + 1, true
		}
		if c == '\\' {
			if i+1 < len(src) && src[i+1] == 'u' {
				r, ni, ok := scanUnicodeEscape(src, i+2)
				if !ok {
					return "", 0, false
				}
				// Combine a \uHHHH high surrogate with a following \uHHHH low
				// surrogate into the astral code point they denote, so an escaped
				// group name matches the raw astral one (must agree with the main
				// parser's readGroupName).
				if r >= 0xD800 && r <= 0xDBFF && ni+1 < len(src) && src[ni] == '\\' && src[ni+1] == 'u' {
					if lo, ni2, ok2 := scanUnicodeEscape(src, ni+2); ok2 && lo >= 0xDC00 && lo <= 0xDFFF {
						r = (r-0xD800)<<10 + (lo - 0xDC00) + 0x10000
						ni = ni2
					}
				}
				name = append(name, r)
				i = ni
				continue
			}
			return "", 0, false
		}
		name = append(name, c)
		i++
	}
	return "", 0, false
}

// scanUnicodeEscape decodes a \u escape body (after the 'u') for the pre-scan,
// accepting either \u{...} or exactly four hex digits.
func scanUnicodeEscape(src []rune, i int) (rune, int, bool) {
	if i < len(src) && src[i] == '{' {
		i++
		v := 0
		start := i
		for i < len(src) && isHexDigit(src[i]) {
			v = v*16 + hexVal(src[i])
			i++
		}
		if i == start || i >= len(src) || src[i] != '}' || v > 0x10FFFF {
			return 0, 0, false
		}
		return rune(v), i + 1, true
	}
	if i+3 >= len(src) {
		return 0, 0, false
	}
	v := 0
	for k := 0; k < 4; k++ {
		if !isHexDigit(src[i+k]) {
			return 0, 0, false
		}
		v = v*16 + hexVal(src[i+k])
	}
	return rune(v), i + 4, true
}
