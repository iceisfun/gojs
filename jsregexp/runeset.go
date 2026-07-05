package jsregexp

import (
	"sort"
	"unicode"
)

// runeSet is a normalized, sorted set of inclusive code-point ranges with O(log
// n) membership. The compiler lowers each character class into one of these.
type runeSet struct {
	ranges []rrange
}

type rrange struct{ lo, hi rune }

// setBuilder accumulates ranges before normalization.
type setBuilder struct {
	ranges []rrange
}

func (b *setBuilder) addRange(lo, hi rune) {
	if lo > hi {
		lo, hi = hi, lo
	}
	b.ranges = append(b.ranges, rrange{lo, hi})
}

func (b *setBuilder) addRune(r rune) { b.addRange(r, r) }

// addClassEscape adds the code points denoted by \d \D \w \W \s \S. The negated
// forms are represented by complementing the positive set over the full code
// point range, so membership stays a simple range test.
func (b *setBuilder) addClassEscape(k ClassEscKind, foldWord bool) {
	// Under /iu, GetWordCharacters (§22.2.2.7.3) folds ſ (U+017F) and the Kelvin
	// sign (U+212A) into the word set (their canonical forms are word chars), so
	// \W — the complement of that extended set — must exclude them.
	wr := wordRanges
	if foldWord {
		wr = wordFoldRanges
	}
	switch k {
	case EscDigit:
		b.addRange('0', '9')
	case EscNotDigit:
		b.addComplement(digitRanges)
	case EscWord:
		b.addRanges(wr)
	case EscNotWord:
		b.addComplement(wr)
	case EscSpace:
		b.addRanges(spaceRanges)
	case EscNotSpace:
		b.addComplement(spaceRanges)
	}
}

func (b *setBuilder) addRanges(rs []rrange) {
	for _, r := range rs {
		b.addRange(r.lo, r.hi)
	}
}

// addComplement adds the complement of rs over [0, 0x10FFFF].
func (b *setBuilder) addComplement(rs []rrange) {
	norm := normalize(rs)
	var prev rune = 0
	for _, r := range norm {
		if r.lo > prev {
			b.addRange(prev, r.lo-1)
		}
		if r.hi+1 > prev {
			prev = r.hi + 1
		}
	}
	if prev <= 0x10FFFF {
		b.addRange(prev, 0x10FFFF)
	}
}

func (b *setBuilder) build() *runeSet { return &runeSet{ranges: normalize(b.ranges)} }

// normalize sorts and coalesces overlapping/adjacent ranges.
func normalize(in []rrange) []rrange {
	if len(in) == 0 {
		return nil
	}
	rs := make([]rrange, len(in))
	copy(rs, in)
	sort.Slice(rs, func(i, j int) bool { return rs[i].lo < rs[j].lo })
	out := rs[:1]
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.lo <= last.hi+1 {
			if r.hi > last.hi {
				last.hi = r.hi
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}

func (s *runeSet) contains(r rune) bool {
	lo, hi := 0, len(s.ranges)
	for lo < hi {
		mid := (lo + hi) / 2
		switch {
		case r < s.ranges[mid].lo:
			hi = mid
		case r > s.ranges[mid].hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// intersect returns the set of code points in both a and b.
func intersect(a, b *runeSet) *runeSet {
	var out setBuilder
	for _, ra := range a.ranges {
		for _, rb := range b.ranges {
			lo, hi := ra.lo, ra.hi
			if rb.lo > lo {
				lo = rb.lo
			}
			if rb.hi < hi {
				hi = rb.hi
			}
			if lo <= hi {
				out.addRange(lo, hi)
			}
		}
	}
	return out.build()
}

// subtract returns the set of code points in a but not in b.
func subtract(a, b *runeSet) *runeSet {
	var out setBuilder
	for _, ra := range a.ranges {
		lo := ra.lo
		for _, rb := range b.ranges {
			if rb.hi < lo || rb.lo > ra.hi {
				continue
			}
			if rb.lo > lo {
				out.addRange(lo, rb.lo-1)
			}
			if rb.hi+1 > lo {
				lo = rb.hi + 1
			}
			if lo > ra.hi {
				break
			}
		}
		if lo <= ra.hi {
			out.addRange(lo, ra.hi)
		}
	}
	return out.build()
}

// --- code point classes used by escapes and assertions ---

var digitRanges = []rrange{{'0', '9'}}

var wordRanges = []rrange{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}

// wordFoldRanges is wordRanges extended with the two non-ASCII code points whose
// Unicode simple case fold is an ASCII word character; used only under /iu.
var wordFoldRanges = []rrange{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}, {0x017F, 0x017F}, {0x212A, 0x212A}}

// spaceRanges is WhiteSpace ∪ LineTerminator (§22.2.2, \s): tab/LF/VT/FF/CR,
// space, NBSP, the Zs Space_Separator category, LINE/PARAGRAPH SEPARATOR, and
// the BOM (ZWNBSP).
var spaceRanges = []rrange{
	{0x0009, 0x000D}, // tab, LF, VT, FF, CR
	{0x0020, 0x0020}, // SPACE
	{0x00A0, 0x00A0}, // NBSP
	{0x1680, 0x1680}, // OGHAM SPACE MARK
	{0x2000, 0x200A}, // EN QUAD .. HAIR SPACE
	{0x2028, 0x2029}, // LINE / PARAGRAPH SEPARATOR
	{0x202F, 0x202F}, // NARROW NO-BREAK SPACE
	{0x205F, 0x205F}, // MEDIUM MATHEMATICAL SPACE
	{0x3000, 0x3000}, // IDEOGRAPHIC SPACE
	{0xFEFF, 0xFEFF}, // ZERO WIDTH NO-BREAK SPACE (BOM)
}

func isLineTerminator(r rune) bool {
	return r == 0x000A || r == 0x000D || r == 0x2028 || r == 0x2029
}

// isWordChar reports membership in the \w set, used by \b / \B.
func isWordChar(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// canonicalize implements the Canonicalize abstract operation (§22.2.2.7.1) used
// for case-insensitive comparison. In Unicode mode it applies simple case
// folding (the minimum of r's fold orbit); otherwise it upper-cases r, with the
// spec's guard that a non-ASCII code point never folds to an ASCII one.
func canonicalize(r rune, unicodeMode bool) rune {
	if unicodeMode {
		return simpleFold(r)
	}
	up := unicode.ToUpper(r)
	if r >= 128 && up < 128 {
		return r
	}
	return up
}

// simpleFold returns the canonical representative of r's case-fold orbit: the
// smallest code point reachable via unicode.SimpleFold, corrected by the
// CaseFolding.txt "S" (simple) mappings that Go's orbit-based SimpleFold does not
// model (see foldExtra). Unicode-mode Canonicalize (§22.2.2.7.1) uses exactly the
// simple/common CaseFolding.txt mappings, so two code points sharing a simple
// fold must canonicalize identically.
func simpleFold(r rune) rune {
	min := r
	for c := unicode.SimpleFold(r); c != r; c = unicode.SimpleFold(c) {
		if c < min {
			min = c
		}
	}
	for _, c := range foldExtra[r] {
		if c < min {
			min = c
		}
	}
	return min
}

// scfSimplePairs are the CaseFolding.txt entries with status "S" (a simple case
// fold that differs from the character's full fold). Go's unicode.SimpleFold is
// orbit-based and omits the ones whose full fold expands to a multi-code-point
// sequence (e.g. U+1FD3 → U+0390, U+FB05 → U+FB06), so Unicode-mode Canonicalize
// would fail to relate the two. Each pair {src, dst} is an equivalence that must
// hold in both directions; foldExtra records it symmetrically. Pairs Go already
// covers (e.g. U+1E9E ↔ U+00DF) are harmless duplicates.
var scfSimplePairs = [][2]rune{
	{0x1E9E, 0x00DF}, {0x1F88, 0x1F80}, {0x1F89, 0x1F81}, {0x1F8A, 0x1F82},
	{0x1F8B, 0x1F83}, {0x1F8C, 0x1F84}, {0x1F8D, 0x1F85}, {0x1F8E, 0x1F86},
	{0x1F8F, 0x1F87}, {0x1F98, 0x1F90}, {0x1F99, 0x1F91}, {0x1F9A, 0x1F92},
	{0x1F9B, 0x1F93}, {0x1F9C, 0x1F94}, {0x1F9D, 0x1F95}, {0x1F9E, 0x1F96},
	{0x1F9F, 0x1F97}, {0x1FA8, 0x1FA0}, {0x1FA9, 0x1FA1}, {0x1FAA, 0x1FA2},
	{0x1FAB, 0x1FA3}, {0x1FAC, 0x1FA4}, {0x1FAD, 0x1FA5}, {0x1FAE, 0x1FA6},
	{0x1FAF, 0x1FA7}, {0x1FBC, 0x1FB3}, {0x1FCC, 0x1FC3}, {0x1FD3, 0x0390},
	{0x1FE3, 0x03B0}, {0x1FFC, 0x1FF3}, {0xFB05, 0xFB06},
}

// foldExtra maps each code point involved in an scfSimplePairs entry to the other
// code points in its (Go-orbit-augmented) simple-fold class, so simpleFold and
// classContainsFold see the full equivalence Go's SimpleFold misses.
var foldExtra = buildFoldExtra()

func buildFoldExtra() map[rune][]rune {
	m := map[rune]map[rune]bool{}
	link := func(a, b rune) {
		if m[a] == nil {
			m[a] = map[rune]bool{}
		}
		if a != b {
			m[a][b] = true
		}
	}
	// orbit collects every code point reachable from r through Go's SimpleFold.
	orbit := func(r rune) []rune {
		out := []rune{r}
		for c := unicode.SimpleFold(r); c != r; c = unicode.SimpleFold(c) {
			out = append(out, c)
		}
		return out
	}
	for _, p := range scfSimplePairs {
		class := append(orbit(p[0]), orbit(p[1])...)
		for _, a := range class {
			for _, b := range class {
				link(a, b)
			}
		}
	}
	out := map[rune][]rune{}
	for r, set := range m {
		for c := range set {
			out[r] = append(out[r], c)
		}
	}
	return out
}

// classContainsFold reports whether the class set contains r, applying case
// folding when ic is set. Rather than pre-expanding the (possibly huge) set, it
// tests each member of r's fold orbit against the set.
func classContainsFold(set *runeSet, r rune, ic, unicodeMode bool) bool {
	if set.contains(r) {
		return true
	}
	if !ic {
		return false
	}
	if unicodeMode {
		for c := unicode.SimpleFold(r); c != r; c = unicode.SimpleFold(c) {
			if set.contains(c) {
				return true
			}
		}
		// CaseFolding.txt "S" mappings that Go's orbit walk omits (foldExtra).
		for _, c := range foldExtra[r] {
			if set.contains(c) {
				return true
			}
		}
		return false
	}
	for _, v := range [2]rune{unicode.ToUpper(r), unicode.ToLower(r)} {
		// Honor the ASCII-boundary guard: folding must not cross the 128 line.
		if v != r && (r < 128) == (v < 128) && set.contains(v) {
			return true
		}
	}
	return false
}

// wordCharFold reports \w membership. Under /iu the long-s (U+017F) and Kelvin
// sign (U+212A) fold to word characters and so count as such.
func wordCharFold(r rune, fold bool) bool {
	if isWordChar(r) {
		return true
	}
	return fold && (r == 0x017F || r == 0x212A)
}
