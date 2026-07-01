package parser

import (
	"errors"
	"unicode"
)

// This file implements a focused set of parse-time early-error checks for
// regular-expression literals: named capture group specifiers ((?<name>...))
// and named backreferences (\k<name>). These are pure syntactic checks defined
// by the ECMAScript RegExp grammar (ECMA-262 §22.2) and do not depend on the
// underlying regex engine's matching semantics, so they are validated here even
// though gojs executes the pattern with RE2.
//
// Only well-defined, engine-independent early errors are reported. Broader
// unicode-mode (`u`/`v`) restrictions and the inline-modifiers grammar are not
// validated (see wontfix/literals.md).

// validateRegexpLiteral reports an early error for a regular-expression literal
// pattern, or nil when the checks it performs pass. unicode indicates whether
// the literal carries the `u` or `v` flag, under which named backreferences are
// validated even when the pattern declares no named groups.
func validateRegexpLiteral(pattern string, unicode bool) error {
	if err := validateRegexpNamedGroups(pattern, unicode); err != nil {
		return err
	}
	return validateRegexpDuplicateNames(pattern)
}

// validateRegexpNamedGroups verifies named group specifiers and named
// backreferences within a regexp pattern:
//
//   - a group name must be a non-empty RegExpIdentifierName;
//   - group names must be unique;
//   - when the pattern declares at least one named group, every \k must be a
//     well-formed \k<name> that references a declared group.
//
// It correctly skips character classes ([...]), where '(' and '\k' are literal,
// and lookbehind assertions ((?<= and (?<!), which are not named groups.
func validateRegexpNamedGroups(pattern string, unicode bool) error {
	rs := []rune(pattern)
	inClass := false
	declared := map[string]bool{}

	type backref struct {
		name       string
		wellFormed bool
	}
	var backrefs []backref

	for i := 0; i < len(rs); {
		c := rs[i]
		switch {
		case c == '\\':
			// A named backreference \k<name> outside a character class. The name
			// is a RegExpIdentifierName terminated by '>', read non-greedily so a
			// following group specifier is not swallowed (e.g. \k<a(?<a>a)).
			if i+1 < len(rs) && rs[i+1] == 'k' && !inClass {
				if i+2 < len(rs) && rs[i+2] == '<' {
					name, end, ok := readGroupSpecifierName(rs, i+3)
					backrefs = append(backrefs, backref{name: name, wellFormed: ok})
					if ok {
						i = end
						continue
					}
				} else {
					// \k not followed by '<' is a malformed named backreference.
					backrefs = append(backrefs, backref{wellFormed: false})
				}
				i += 2
				continue
			}
			// Any other escape consumes the following character verbatim.
			i += 2
		case c == '[':
			inClass = true
			i++
		case c == ']':
			inClass = false
			i++
		case c == '(' && !inClass && i+2 < len(rs) && rs[i+1] == '?' && rs[i+2] == '<':
			// (?<= and (?<! are lookbehind assertions, not named groups.
			if i+3 < len(rs) && (rs[i+3] == '=' || rs[i+3] == '!') {
				i += 4
				continue
			}
			name, end, ok := readGroupSpecifierName(rs, i+3)
			if !ok {
				return errors.New("Invalid regular expression: invalid group specifier name")
			}
			declared[name] = true
			i = end
		default:
			i++
		}
	}

	// A \k backreference must resolve to a declared group name when the pattern
	// declares any named group, or (regardless) under the unicode `u`/`v` flag,
	// where \k is not a legacy identity escape.
	if len(declared) > 0 || unicode {
		for _, br := range backrefs {
			if !br.wellFormed || !declared[br.name] {
				return errors.New("Invalid regular expression: invalid named backreference")
			}
		}
	}
	return nil
}

// validateRegexpDuplicateNames reports an early error when two named capture
// groups share a name and could both participate in a single match. Under the
// duplicate-named-capturing-groups semantics (ES2025), a name may be reused
// across the branches of a disjunction (they are mutually exclusive), but not
// within the same alternative (sequence) or across nested groups on the same
// path. It parses the pattern's group/disjunction structure to decide.
//
// The pattern is assumed to have already passed named-group syntax validation.
func validateRegexpDuplicateNames(pattern string) error {
	d := &reDupChecker{rs: []rune(pattern)}
	_, err := d.disjunction()
	return err
}

// reDupChecker is a minimal recursive-descent walker over a regexp pattern that
// tracks only the set of named groups reachable on a match path, ignoring
// matching semantics.
type reDupChecker struct {
	rs  []rune
	pos int
}

// disjunction walks Alternative ('|' Alternative)* and returns the union of the
// names declared in any branch (branches are mutually exclusive, so duplicates
// across them are permitted).
func (d *reDupChecker) disjunction() (map[string]bool, error) {
	names, err := d.alternative()
	if err != nil {
		return nil, err
	}
	for d.pos < len(d.rs) && d.rs[d.pos] == '|' {
		d.pos++
		more, err := d.alternative()
		if err != nil {
			return nil, err
		}
		for n := range more {
			names[n] = true
		}
	}
	return names, nil
}

// alternative walks a sequence of atoms, requiring their name sets to be
// pairwise disjoint (two occurrences in a sequence can both match), and returns
// the union.
func (d *reDupChecker) alternative() (map[string]bool, error) {
	names := map[string]bool{}
	for d.pos < len(d.rs) {
		c := d.rs[d.pos]
		if c == '|' || c == ')' {
			break
		}
		atom, err := d.atom()
		if err != nil {
			return nil, err
		}
		for n := range atom {
			if names[n] {
				return nil, errors.New("Invalid regular expression: duplicate capture group name")
			}
			names[n] = true
		}
	}
	return names, nil
}

// atom walks a single atom (a group, character class, escape, or literal) and
// returns the names it declares.
func (d *reDupChecker) atom() (map[string]bool, error) {
	c := d.rs[d.pos]
	switch c {
	case '\\':
		d.pos += 2
		return nil, nil
	case '[':
		// Character class: skip to the matching ']' honoring escapes.
		d.pos++
		for d.pos < len(d.rs) && d.rs[d.pos] != ']' {
			if d.rs[d.pos] == '\\' {
				d.pos++
			}
			d.pos++
		}
		if d.pos < len(d.rs) {
			d.pos++ // ']'
		}
		return nil, nil
	case '(':
		return d.group()
	default:
		d.pos++
		return nil, nil
	}
}

// group walks a parenthesized group and returns the names within, adding the
// group's own name when it is a named capture. A name that duplicates one in the
// group's own body (same path) is an error.
func (d *reDupChecker) group() (map[string]bool, error) {
	d.pos++ // '('
	self := ""
	if d.pos+1 < len(d.rs) && d.rs[d.pos] == '?' {
		switch d.rs[d.pos+1] {
		case ':', '=', '!':
			d.pos += 2 // non-capturing or lookahead
		case '<':
			if d.pos+2 < len(d.rs) && (d.rs[d.pos+2] == '=' || d.rs[d.pos+2] == '!') {
				d.pos += 3 // lookbehind
			} else {
				// Named capture: read the specifier name up to '>'.
				name, end, ok := readGroupSpecifierName(d.rs, d.pos+2)
				if !ok {
					// Should not happen after syntax validation; be defensive.
					d.pos += 2
				} else {
					self = name
					d.pos = end
				}
			}
		default:
			// Inline modifiers such as (?i:...) — not validated here; treat the
			// remainder as a subpattern.
			d.pos += 1
		}
	}
	inner, err := d.disjunction()
	if err != nil {
		return nil, err
	}
	if inner == nil {
		inner = map[string]bool{}
	}
	if self != "" {
		if inner[self] {
			return nil, errors.New("Invalid regular expression: duplicate capture group name")
		}
		inner[self] = true
	}
	if d.pos < len(d.rs) && d.rs[d.pos] == ')' {
		d.pos++ // ')'
	}
	return inner, nil
}

// readGroupSpecifierName reads and validates a named group specifier's name
// starting at index i (just after "(?<"). It returns the decoded name, the
// index just past the closing '>', and whether the name is valid: non-empty,
// terminated by '>', and a legal RegExpIdentifierName (with \u escapes decoded).
func readGroupSpecifierName(rs []rune, i int) (string, int, bool) {
	var raw []rune
	j := i
	for j < len(rs) && rs[j] != '>' {
		if rs[j] == '\\' {
			raw = append(raw, rs[j])
			j++
			if j >= len(rs) {
				return "", j, false
			}
		}
		raw = append(raw, rs[j])
		j++
	}
	if j >= len(rs) {
		return "", j, false // unterminated: no closing '>'
	}
	name, ok := decodeIdentifierName(raw)
	if !ok {
		return "", j, false
	}
	return name, j + 1, true
}

// decodeIdentifierName decodes any \uXXXX / \u{...} escapes in raw and reports
// whether the result is a valid RegExpIdentifierName (a non-empty run whose
// first code point is an identifier start and the rest identifier parts).
func decodeIdentifierName(raw []rune) (string, bool) {
	var cps []rune
	for i := 0; i < len(raw); {
		if raw[i] == '\\' {
			cp, next, ok := decodeUnicodeEscape(raw, i)
			if !ok {
				return "", false
			}
			cps = append(cps, cp)
			i = next
			continue
		}
		cps = append(cps, raw[i])
		i++
	}
	if len(cps) == 0 {
		return "", false
	}
	if !isRegexIDStart(cps[0]) {
		return "", false
	}
	for _, r := range cps[1:] {
		if !isRegexIDPart(r) {
			return "", false
		}
	}
	return string(cps), true
}

// decodeUnicodeEscape decodes a \uXXXX or \u{...} escape starting at raw[i]
// (raw[i] == '\\'), returning the code point and the index past the escape.
func decodeUnicodeEscape(raw []rune, i int) (rune, int, bool) {
	if i+1 >= len(raw) || raw[i+1] != 'u' {
		return 0, i, false
	}
	j := i + 2
	if j < len(raw) && raw[j] == '{' {
		j++
		val := 0
		start := j
		for j < len(raw) && raw[j] != '}' {
			d := hexDigit(raw[j])
			if d < 0 {
				return 0, i, false
			}
			val = val*16 + d
			j++
		}
		if j >= len(raw) || j == start || val > 0x10FFFF {
			return 0, i, false
		}
		return rune(val), j + 1, true // past '}'
	}
	if j+4 > len(raw) {
		return 0, i, false
	}
	val := 0
	for k := 0; k < 4; k++ {
		d := hexDigit(raw[j+k])
		if d < 0 {
			return 0, i, false
		}
		val = val*16 + d
	}
	return rune(val), j + 4, true
}

func hexDigit(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10
	}
	return -1
}

// isRegexIDStart reports whether r may start a RegExpIdentifierName.
func isRegexIDStart(r rune) bool {
	return r == '$' || r == '_' ||
		unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}

// isRegexIDPart reports whether r may continue a RegExpIdentifierName.
func isRegexIDPart(r rune) bool {
	return isRegexIDStart(r) ||
		unicode.IsDigit(r) ||
		unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) ||
		unicode.Is(unicode.Nd, r) || unicode.Is(unicode.Pc, r) ||
		r == 0x200C || r == 0x200D // ZWNJ, ZWJ
}
