package interp

import "unicode"

// Unicode full case conversion for String.prototype.{toLowerCase,toUpperCase}
// and their toLocale* variants (which, absent an ICU locale layer, use the same
// locale-insensitive Default Case Conversion algorithm; ECMA-262 §22.1.3.29/.30).
//
// Go's strings.ToLower/ToUpper apply only the simple (1:1) case mappings from
// UnicodeData.txt. The spec additionally requires the locale-insensitive full
// mappings from SpecialCasing.txt, including one-to-many expansions (ß→SS,
// ﬀ→FF, İ→i+◌̇, …) and the context-sensitive Final_Sigma rule (Σ→ς at word end,
// otherwise Σ→σ). The lookup tables live in unicode_casing_tables.go.

// rangesContain reports whether r is covered by a sorted, non-overlapping list
// of inclusive rune ranges, via binary search.
func rangesContain(ranges [][2]rune, r rune) bool {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case r < ranges[mid][0]:
			hi = mid - 1
		case r > ranges[mid][1]:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

func isCasedRune(r rune) bool         { return rangesContain(casedRanges, r) }
func isCaseIgnorableRune(r rune) bool { return rangesContain(caseIgnorableRanges, r) }

// finalSigma implements the Unicode Final_Sigma context test for a Σ (U+03A3)
// at position i within runes. It holds when Σ is preceded by a cased letter
// (ignoring intervening case-ignorable code points) and is not followed by a
// cased letter (again ignoring case-ignorable code points).
func finalSigma(runes []rune, i int) bool {
	// Before: scan back over case-ignorable code points, then require a cased one.
	before := false
	for j := i - 1; j >= 0; j-- {
		if isCaseIgnorableRune(runes[j]) {
			continue
		}
		before = isCasedRune(runes[j])
		break
	}
	if !before {
		return false
	}
	// After: scan forward over case-ignorable code points; Final_Sigma fails if a
	// cased code point follows.
	for j := i + 1; j < len(runes); j++ {
		if isCaseIgnorableRune(runes[j]) {
			continue
		}
		if isCasedRune(runes[j]) {
			return false
		}
		break
	}
	return true
}

// toLowerCaseFull returns the full Unicode lowercase of s (locale-insensitive).
func toLowerCaseFull(s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		switch {
		case r == 0x03A3: // GREEK CAPITAL LETTER SIGMA — conditional Final_Sigma.
			if finalSigma(runes, i) {
				out = append(out, 0x03C2) // ς
			} else {
				out = append(out, 0x03C3) // σ
			}
		default:
			if m, ok := specialLowerMap[r]; ok {
				out = append(out, m...)
			} else {
				out = append(out, unicode.ToLower(r))
			}
		}
	}
	return string(out)
}

// toUpperCaseFull returns the full Unicode uppercase of s (locale-insensitive).
func toUpperCaseFull(s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for _, r := range runes {
		if m, ok := specialUpperMap[r]; ok {
			out = append(out, m...)
		} else {
			out = append(out, unicode.ToUpper(r))
		}
	}
	return string(out)
}
