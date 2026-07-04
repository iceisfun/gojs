package jsregexp

import "sort"

// charSetV is the compiled form of a v-mode class: a set of single code points
// plus a list of multi-code-point (or empty) strings the class may also match.
type charSetV struct {
	set     *runeSet
	strings [][]rune // each entry has length != 1 (length-1 folded into set)
}

func dedupStrings(in [][]rune) [][]rune {
	seen := map[string]bool{}
	var out [][]rune
	for _, s := range in {
		key := string(s)
		if !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	return out
}

func stringInList(list [][]rune, s []rune) bool {
	for _, t := range list {
		if string(t) == string(s) {
			return true
		}
	}
	return false
}

// matchStringAt reports whether the code points of s match the input starting at
// sp, honoring case folding when ic. Returns the position after the match.
func matchStringAt(m *machine, sp int, s []rune, ic, u bool) (int, bool) {
	i := sp
	for _, r := range s {
		if i >= len(m.input) {
			return 0, false
		}
		ch, w := m.codePointAt(i)
		if ch != r && !(ic && canonicalize(ch, u) == canonicalize(r, u)) {
			return 0, false
		}
		i += w
	}
	return i, true
}

// classStringMatcher builds a matcher for a v-mode class that can match strings.
// It tries each string alternative longest-first, then the single-code-point set.
func (c *compiler) classStringMatcher(cs charSetV) matcher {
	strings := append([][]rune(nil), cs.strings...)
	// Longest-first so the greedy alternative is tried before shorter ones.
	sort.SliceStable(strings, func(i, j int) bool { return len(strings[i]) > len(strings[j]) })
	set := cs.set
	ic, u := c.ic, c.u
	return func(m *machine, sp int, k cont) bool {
		if m.err != nil || !m.step() {
			return false
		}
		for _, s := range strings {
			if np, ok := matchStringAt(m, sp, s, ic, u); ok {
				if k(np) {
					return true
				}
			}
		}
		if sp < len(m.input) {
			r, w := m.codePointAt(sp)
			if classContainsFold(set, r, ic, u) {
				return k(sp + w)
			}
		}
		return false
	}
}

// propertyOfStrings resolves a v-mode "property of strings" to its member set
// (single code points folded into set, multi-code-point sequences as strings).
// ok is false when name is not a supported property of strings.
func propertyOfStrings(name string) (charSetV, bool) {
	switch name {
	case "Emoji_Keycap_Sequence":
		var strs [][]rune
		for _, base := range []rune{'#', '*', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9'} {
			strs = append(strs, []rune{base, 0xFE0F, 0x20E3})
		}
		return charSetV{set: &runeSet{}, strings: strs}, true
	case "RGI_Emoji":
		var b setBuilder
		for _, r := range rgiEmojiSingleRanges {
			b.addRange(r[0], r[1])
		}
		strs := make([][]rune, 0, len(rgiEmojiSequences))
		for _, s := range rgiEmojiSequences {
			strs = append(strs, []rune(s))
		}
		return charSetV{set: b.build(), strings: strs}, true
	}
	return charSetV{}, false
}
