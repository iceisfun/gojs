package jsregexp

// classAtom is one parsed element inside a character class. A plain code point
// (IsChar) can serve as a range endpoint; a set-valued item (\d, \p{...}) cannot.
type classAtom struct {
	R      rune
	Item   ClassItem
	IsChar bool
}

func (a classAtom) toItem() ClassItem {
	if a.IsChar {
		return ClassRange{Lo: a.R, Hi: a.R}
	}
	return a.Item
}

// parseCharClass parses a character class beginning at '['. In v mode it defers
// to the ClassSetExpression grammar; otherwise it parses the classic
// ClassRanges grammar.
func (p *parser) parseCharClass() Node {
	p.advance() // '['
	negate := false
	if p.peek() == '^' {
		negate = true
		p.advance()
	}
	if p.flags.UnicodeSets {
		set := p.parseClassSetV()
		p.expect(']')
		return &CharClass{Negate: negate, Set: set}
	}

	var items []ClassItem
	for {
		if p.eof() {
			p.fail("unterminated character class")
		}
		if p.peek() == ']' {
			break
		}
		lo := p.parseClassAtom()

		// A '-' that is not the closing ']' and not the end introduces a range.
		if p.peek() == '-' && p.peekAt(1) != ']' && p.peekAt(1) != -1 {
			if !lo.IsChar {
				// e.g. [\d-a]: a class escape cannot start a range.
				if p.flags.UnicodeMode() {
					p.fail("invalid character class range")
				}
				items = append(items, lo.toItem())
				continue // let '-' be parsed as a literal next iteration
			}
			p.advance() // '-'
			hi := p.parseClassAtom()
			if !hi.IsChar {
				if p.flags.UnicodeMode() {
					p.fail("invalid character class range")
				}
				items = append(items, ClassRange{Lo: lo.R, Hi: lo.R}, ClassRange{Lo: '-', Hi: '-'}, hi.toItem())
				continue
			}
			if lo.R > hi.R {
				p.fail("range out of order in character class")
			}
			items = append(items, ClassRange{Lo: lo.R, Hi: hi.R})
			continue
		}
		items = append(items, lo.toItem())
	}
	p.advance() // ']'
	return &CharClass{Negate: negate, Set: &ClassSet{Items: items}}
}

// parseClassAtom parses a single class member (a literal code point or an escape).
func (p *parser) parseClassAtom() classAtom {
	if p.peek() == '\\' {
		return p.parseClassEscape()
	}
	return classAtom{R: p.advance(), IsChar: true}
}

// parseClassEscape parses '\...' within a character class (p.pos at the backslash).
func (p *parser) parseClassEscape() classAtom {
	p.advance() // '\'
	if p.eof() {
		p.fail("\\ at end of pattern")
	}
	c := p.peek()
	switch {
	case c == 'd' || c == 'D' || c == 'w' || c == 'W' || c == 's' || c == 'S':
		p.advance()
		return classAtom{Item: ClassEscape{Kind: classEscKind(c)}}
	case c == 'b':
		p.advance()
		return classAtom{R: '\b', IsChar: true} // backspace inside a class
	case c == '-':
		p.advance()
		return classAtom{R: '-', IsChar: true}
	case (c == 'p' || c == 'P') && p.flags.UnicodeMode():
		return classAtom{Item: p.parsePropertyEscape(c == 'P')}
	default:
		return classAtom{R: p.parseCharacterEscape(), IsChar: true}
	}
}

// parseClassSetV parses a v-mode ClassSetExpression (after '[' and optional
// '^'). It parses the first member, then commits to a union, intersection (&&),
// or difference (--) based on the following operator, rejecting mixed operators.
// Full string-property semantics are refined later, but ranges, nested classes,
// set operations, and \q strings are handled here.
func (p *parser) parseClassSetV() *ClassSet {
	first := p.parseClassSetMember()
	switch {
	case p.peek() == '&' && p.peekAt(1) == '&':
		set := &ClassSet{Op: SetIntersection, Items: []ClassItem{first}}
		for p.peek() == '&' && p.peekAt(1) == '&' {
			p.advance()
			p.advance()
			if p.peek() == '&' {
				p.fail("invalid set operation")
			}
			set.Items = append(set.Items, p.parseClassSetOperand())
		}
		p.requireClassClose()
		return set
	case p.peek() == '-' && p.peekAt(1) == '-':
		set := &ClassSet{Op: SetSubtraction, Items: []ClassItem{first}}
		for p.peek() == '-' && p.peekAt(1) == '-' {
			p.advance()
			p.advance()
			set.Items = append(set.Items, p.parseClassSetOperand())
		}
		p.requireClassClose()
		return set
	default:
		set := &ClassSet{Op: SetUnion, Items: []ClassItem{first}}
		for !p.eof() && p.peek() != ']' {
			if (p.peek() == '&' && p.peekAt(1) == '&') || (p.peek() == '-' && p.peekAt(1) == '-') {
				p.fail("mixed set operations")
			}
			set.Items = append(set.Items, p.parseClassSetMember())
		}
		p.requireClassClose()
		return set
	}
}

func (p *parser) requireClassClose() {
	if p.peek() != ']' {
		p.fail("unterminated character class")
	}
}

// parseClassSetMember parses one union member: a nested class, a \q string, or a
// single atom that may form a range (a-z). Used for both the first member and
// every subsequent union member so ranges parse consistently.
func (p *parser) parseClassSetMember() ClassItem {
	if p.peek() == '[' {
		return p.parseNestedClassV()
	}
	if p.peek() == '\\' && p.peekAt(1) == 'q' {
		return p.parseClassStringDisjunction()
	}
	lo := p.parseClassAtom()
	if lo.IsChar && p.peek() == '-' && p.peekAt(1) != '-' && p.peekAt(1) != ']' && p.peekAt(1) != -1 {
		p.advance() // '-'
		hi := p.parseClassAtom()
		if !hi.IsChar {
			p.fail("invalid character class range")
		}
		if lo.R > hi.R {
			p.fail("range out of order in character class")
		}
		return ClassRange{Lo: lo.R, Hi: hi.R}
	}
	return lo.toItem()
}

// parseClassSetOperand parses a single operand of a v-mode intersection or
// difference (a nested class, a class-string disjunction, or a single atom).
// Operands are not range-capable — ranges must be written as nested classes.
func (p *parser) parseClassSetOperand() ClassItem {
	switch {
	case p.peek() == '[':
		return p.parseNestedClassV()
	case p.peek() == '\\' && p.peekAt(1) == 'q':
		return p.parseClassStringDisjunction()
	default:
		return p.parseClassAtom().toItem()
	}
}

func (p *parser) parseNestedClassV() ClassItem {
	p.advance() // '['
	neg := false
	if p.peek() == '^' {
		neg = true
		p.advance()
	}
	inner := p.parseClassSetV()
	p.expect(']')
	return NestedClass{Negate: neg, Set: inner}
}

// parseClassStringDisjunction parses \q{s1|s2|...} (v mode). Each alternative is
// a (possibly empty, possibly multi-code-point) class string.
func (p *parser) parseClassStringDisjunction() ClassItem {
	p.advance() // '\'
	p.advance() // 'q'
	if p.peek() != '{' {
		p.fail("invalid \\q")
	}
	p.advance() // '{'
	// Collect each '|'-separated alternative as its own class string.
	var alts [][]rune
	var runes []rune
	for !p.eof() && p.peek() != '}' {
		if p.peek() == '|' {
			p.advance()
			alts = append(alts, runes)
			runes = nil
			continue
		}
		if p.peek() == '\\' {
			p.advance()
			runes = append(runes, p.parseCharacterEscape())
			continue
		}
		runes = append(runes, p.advance())
	}
	if p.peek() != '}' {
		p.fail("unterminated \\q")
	}
	p.advance() // '}'
	alts = append(alts, runes)
	return ClassStringDisjunction{Alts: alts}
}
