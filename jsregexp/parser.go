package jsregexp

import "unicode"

// parser is a recursive-descent parser for the ECMAScript RegExp pattern
// grammar (§22.2.1). It operates on a rune slice so code-point offsets are
// natural, and reports errors by panicking with a *SyntaxError that Parse
// recovers. Flag-dependent grammar (chiefly the u/v Unicode modes) is threaded
// through as p.flags.
type parser struct {
	src   []rune
	pos   int
	flags Flags

	numGroups int            // total capturing groups (from the pre-scan)
	names     map[string]int // group name -> capture index (from the pre-scan)
	capIndex  int            // capture counter during the main parse
}

// Parse parses pattern under the given flags into an immutable *Pattern, or
// returns a *SyntaxError. It performs a group pre-scan first so that numeric and
// named backreferences — which may refer forward — can be validated.
func Parse(pattern string, flags Flags) (pat *Pattern, err error) {
	src := []rune(pattern)
	n, names, serr := scanGroups(src)
	if serr != nil {
		return nil, serr
	}
	p := &parser{src: src, flags: flags, numGroups: n, names: names}

	defer func() {
		if r := recover(); r != nil {
			if se, ok := r.(*SyntaxError); ok {
				pat, err = nil, se
				return
			}
			panic(r)
		}
	}()

	body := p.parseDisjunction()
	if p.pos != len(p.src) {
		// The only way to stop early is an unmatched ')'.
		p.fail("unmatched ')'")
	}
	if _, derr := dupCheckNames(body); derr != nil {
		return nil, derr
	}
	return &Pattern{Body: body, GroupCount: n, GroupNames: names, Flags: flags}, nil
}

// --- lexer-ish helpers ---

func (p *parser) eof() bool { return p.pos >= len(p.src) }
func (p *parser) peek() rune {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return -1
}
func (p *parser) peekAt(k int) rune {
	if p.pos+k < len(p.src) {
		return p.src[p.pos+k]
	}
	return -1
}
func (p *parser) advance() rune   { r := p.src[p.pos]; p.pos++; return r }
func (p *parser) fail(msg string) { panic(errAt(p.pos, msg)) }
func (p *parser) expect(r rune) {
	if p.peek() != r {
		p.fail("expected '" + string(r) + "'")
	}
	p.pos++
}

// --- grammar ---

// Disjunction :: Alternative ( '|' Alternative )*
func (p *parser) parseDisjunction() Node {
	first := p.parseAlternative()
	if p.peek() != '|' {
		return first
	}
	alts := []Node{first}
	for p.peek() == '|' {
		p.advance()
		alts = append(alts, p.parseAlternative())
	}
	return &Disjunction{Alternatives: alts}
}

// Alternative :: Term*   (terminated by '|' or ')' or EOF)
func (p *parser) parseAlternative() Node {
	var terms []Node
	for !p.eof() && p.peek() != '|' && p.peek() != ')' {
		terms = append(terms, p.parseTerm())
	}
	switch len(terms) {
	case 0:
		return &Empty{}
	case 1:
		return terms[0]
	default:
		return &Concat{Terms: terms}
	}
}

// Term :: Assertion | Atom Quantifier?
//
// quantifiable reports whether the parsed node may take a quantifier under the
// current flags: plain atoms always may; ^ $ \b \B and lookahead may only in
// Annex B (non-Unicode) mode; lookbehind never may.
func (p *parser) parseTerm() Node {
	node, quantifiable := p.parseAssertionOrAtom()
	if q, ok := p.parseQuantifier(); ok {
		if !quantifiable {
			p.fail("nothing to repeat")
		}
		return &Quantifier{Min: q.min, Max: q.max, Greedy: q.greedy, Body: node}
	}
	return node
}

// parseAssertionOrAtom returns the next assertion or atom and whether it is
// quantifiable.
func (p *parser) parseAssertionOrAtom() (Node, bool) {
	switch c := p.peek(); c {
	case '^':
		p.advance()
		return &Assertion{Kind: AssertBOL}, false
	case '$':
		p.advance()
		return &Assertion{Kind: AssertEOL}, false
	case '\\':
		if n := p.peekAt(1); n == 'b' || n == 'B' {
			p.advance()
			p.advance()
			kind := AssertWordB
			if n == 'B' {
				kind = AssertNotWordB
			}
			return &Assertion{Kind: kind}, false
		}
		return p.parseAtom(), true
	case '(':
		return p.parseGroup()
	default:
		return p.parseAtom(), true
	}
}

type quant struct {
	min, max int
	greedy   bool
}

// parseQuantifier parses *, +, ?, or {n[,[m]]}, with an optional lazy '?'.
// It returns ok=false (without consuming, except a rewound brace) when the next
// token is not a quantifier.
func (p *parser) parseQuantifier() (quant, bool) {
	var q quant
	switch p.peek() {
	case '*':
		q.min, q.max = 0, -1
	case '+':
		q.min, q.max = 1, -1
	case '?':
		q.min, q.max = 0, 1
	case '{':
		return p.parseBraceQuantifier()
	default:
		return q, false
	}
	p.advance()
	q.greedy = true
	if p.peek() == '?' {
		p.advance()
		q.greedy = false
	}
	return q, true
}

// parseBraceQuantifier parses {n}, {n,}, or {n,m}. On a malformed brace it
// rewinds and returns ok=false, so the caller can treat '{' as a literal in
// Annex B mode. A well-formed but out-of-order {n,m} (m<n) is a hard error.
func (p *parser) parseBraceQuantifier() (quant, bool) {
	var q quant
	save := p.pos
	p.advance() // '{'
	min, okMin := p.readDecimalInt()
	if !okMin {
		p.pos = save
		return q, false
	}
	q.min, q.max = min, min
	if p.peek() == ',' {
		p.advance()
		if p.peek() == '}' {
			q.max = -1
		} else {
			max, okMax := p.readDecimalInt()
			if !okMax {
				p.pos = save
				return q, false
			}
			q.max = max
		}
	}
	if p.peek() != '}' {
		p.pos = save
		return q, false
	}
	p.advance() // '}'
	if q.max != -1 && q.max < q.min {
		p.fail("numbers out of order in {} quantifier")
	}
	q.greedy = true
	if p.peek() == '?' {
		p.advance()
		q.greedy = false
	}
	return q, true
}

// parseAtom parses a single atom (not an assertion). The caller has already
// excluded ^ $ \b \B (assertions) and ( (groups are handled in parseGroup via
// parseAssertionOrAtom).
func (p *parser) parseAtom() Node {
	c := p.peek()
	switch c {
	case '.':
		p.advance()
		return &AnyChar{}
	case '\\':
		return p.parseAtomEscape()
	case '[':
		return p.parseCharClass()
	case '(':
		n, _ := p.parseGroup()
		return n
	case '*', '+', '?':
		p.fail("nothing to repeat")
	case '{':
		// A '{' that forms a valid quantifier here has nothing to repeat.
		if _, ok := p.parseBraceQuantifier(); ok {
			p.fail("nothing to repeat")
		}
		if p.flags.UnicodeMode() {
			p.fail("lone quantifier bracket")
		}
		p.advance()
		return &Char{R: '{'}
	case '}', ']':
		if p.flags.UnicodeMode() {
			p.fail("lone '" + string(c) + "'")
		}
		p.advance()
		return &Char{R: c}
	default:
		p.advance()
		return &Char{R: c}
	}
	return nil // unreachable
}

// parseGroup parses a construct beginning with '('. It returns the node and
// whether that node is quantifiable (lookbehind is not; lookahead is only in
// Annex B mode).
func (p *parser) parseGroup() (Node, bool) {
	p.advance() // '('
	if p.peek() != '?' {
		// Capturing group.
		p.capIndex++
		idx := p.capIndex
		body := p.parseDisjunction()
		p.expect(')')
		name := ""
		for n, i := range p.names {
			if i == idx {
				name = n
				break
			}
		}
		return &Capture{Index: idx, Name: name, Body: body}, true
	}
	p.advance() // '?'
	switch c := p.peek(); {
	case c == ':':
		p.advance()
		body := p.parseDisjunction()
		p.expect(')')
		return &Group{Body: body}, true
	case c == '=':
		p.advance()
		body := p.parseDisjunction()
		p.expect(')')
		return &Lookaround{Behind: false, Negate: false, Body: body}, !p.flags.UnicodeMode()
	case c == '!':
		p.advance()
		body := p.parseDisjunction()
		p.expect(')')
		return &Lookaround{Behind: false, Negate: true, Body: body}, !p.flags.UnicodeMode()
	case c == '<' && (p.peekAt(1) == '=' || p.peekAt(1) == '!'):
		neg := p.peekAt(1) == '!'
		p.advance() // '<'
		p.advance() // '=' or '!'
		body := p.parseDisjunction()
		p.expect(')')
		return &Lookaround{Behind: true, Negate: neg, Body: body}, false
	case c == '<':
		// Named capturing group (?<name>...).
		p.advance() // '<'
		p.capIndex++
		idx := p.capIndex
		name := p.readGroupName()
		body := p.parseDisjunction()
		p.expect(')')
		return &Capture{Index: idx, Name: name, Body: body}, true
	default:
		// Inline modifier group (?flags:...) / (?flags-flags:...) / (?-flags:...).
		mods := p.parseModifiers()
		p.expect(':')
		body := p.parseDisjunction()
		p.expect(')')
		return &Group{Mods: mods, Body: body}, true
	}
}

// parseModifiers parses the RegularExpressionModifiers before ':' in an inline
// modifier group. Only i, m, s may be added or (after '-') removed; each may
// appear at most once across both sets, and at least one modifier must be
// present.
func (p *parser) parseModifiers() *Modifiers {
	m := &Modifiers{}
	var seen [128]bool
	set := func(add bool) {
		for {
			c := p.peek()
			if c != 'i' && c != 'm' && c != 's' {
				return
			}
			if seen[c] {
				p.fail("duplicate regular expression modifier")
			}
			seen[c] = true
			p.advance()
			switch c {
			case 'i':
				if add {
					m.AddI = true
				} else {
					m.SubI = true
				}
			case 'm':
				if add {
					m.AddM = true
				} else {
					m.SubM = true
				}
			case 's':
				if add {
					m.AddS = true
				} else {
					m.SubS = true
				}
			}
		}
	}
	set(true)
	hasAdd := m.AddI || m.AddM || m.AddS
	if p.peek() == '-' {
		p.advance()
		set(false)
		// §22.2.1: for (?flags-flags:...) it is a Syntax Error only when BOTH the
		// add and remove flag sets are empty. An empty remove set with a non-empty
		// add set (e.g. (?s-:...)) is valid.
		if !hasAdd && !m.SubI && !m.SubM && !m.SubS {
			p.fail("empty regular expression modifiers")
		}
	}
	if p.peek() != ':' {
		p.fail("invalid inline modifier")
	}
	return m
}

// readDecimalInt reads a run of ASCII digits as an int, returning ok=false when
// no digit is present. Overflow saturates rather than wrapping.
func (p *parser) readDecimalInt() (int, bool) {
	start := p.pos
	v := 0
	for p.peek() >= '0' && p.peek() <= '9' {
		if v < 1<<30 {
			v = v*10 + int(p.advance()-'0')
		} else {
			p.advance()
		}
	}
	return v, p.pos > start
}

// isIDStart/isIDContinue approximate the ECMAScript identifier rules used for
// RegExpIdentifierName (group names). '$' and '_' are always permitted; '\u'
// escapes are decoded by readGroupName before validation.
func isIDStart(r rune) bool {
	return r == '$' || r == '_' || unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}
func isIDContinue(r rune) bool {
	return r == '$' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) ||
		unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Nd, r) ||
		unicode.Is(unicode.Pc, r) || r == '‌' || r == '‍'
}

// readGroupName reads a RegExpIdentifierName terminated by '>' (the leading '<'
// is already consumed). It decodes \u escapes and validates the identifier.
func (p *parser) readGroupName() string {
	var name []rune
	first := true
	for {
		if p.eof() {
			p.fail("unterminated group name")
		}
		c := p.advance()
		if c == '>' {
			break
		}
		if c == '\\' {
			if p.peek() != 'u' {
				p.fail("invalid group name escape")
			}
			p.advance() // 'u'
			c = p.readUnicodeEscapeValue()
		}
		if first {
			if !isIDStart(c) {
				p.fail("invalid group name")
			}
			first = false
		} else if !isIDContinue(c) {
			p.fail("invalid group name")
		}
		name = append(name, c)
	}
	if len(name) == 0 {
		p.fail("empty group name")
	}
	return string(name)
}
