package jsregexp

// This file implements escape-sequence parsing shared between atom context and
// character-class context: \n \t \xHH \uHHHH \u{...} \cX, legacy octal, the
// character-class escapes (\d \w \s), property escapes (\p{...}), numeric and
// named backreferences, and identity escapes. Flag-dependent rules (Annex B vs
// Unicode mode) are applied per §22.2.1 and Annex B.1.2.

// parseAtomEscape parses '\...' in atom position, with p.pos at the backslash.
func (p *parser) parseAtomEscape() Node {
	p.advance() // '\'
	if p.eof() {
		p.fail("\\ at end of pattern")
	}
	c := p.peek()
	switch {
	case c == 'd' || c == 'D' || c == 'w' || c == 'W' || c == 's' || c == 'S':
		p.advance()
		return &CharClass{Set: &ClassSet{Items: []ClassItem{ClassEscape{Kind: classEscKind(c)}}}}

	case c == 'p' || c == 'P':
		if p.flags.UnicodeMode() {
			prop := p.parsePropertyEscape(c == 'P')
			return &CharClass{Set: &ClassSet{Items: []ClassItem{prop}}}
		}
		p.advance() // Annex B: \p / \P are identity escapes.
		return &Char{R: c}

	case c == 'k':
		if p.flags.UnicodeMode() || len(p.names) > 0 {
			p.advance()
			if p.peek() != '<' {
				p.fail("invalid named backreference")
			}
			p.advance()
			name := p.readGroupName()
			if _, ok := p.names[name]; !ok {
				p.fail("reference to undefined named group")
			}
			return &NamedBackRef{Name: name}
		}
		p.advance() // Annex B, no named groups: identity escape 'k'.
		return &Char{R: 'k'}

	case c >= '1' && c <= '9':
		save := p.pos
		n, _ := p.readDecimalInt()
		if n <= p.numGroups {
			return &BackRef{Index: n}
		}
		if p.flags.UnicodeMode() {
			p.fail("invalid backreference")
		}
		p.pos = save // Annex B: reparse as a legacy octal / identity escape.
		return &Char{R: p.parseLegacyOctalOrIdentity()}

	default:
		return &Char{R: p.parseCharacterEscape()}
	}
}

// parseCharacterEscape parses a CharacterEscape with p.pos at the escape's first
// character (the backslash already consumed), returning the code point it
// denotes.
func (p *parser) parseCharacterEscape() rune {
	c := p.advance()
	switch c {
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'v':
		return '\v'
	case '0':
		if p.peek() >= '0' && p.peek() <= '9' {
			if p.flags.UnicodeMode() {
				p.fail("invalid decimal escape")
			}
			p.pos-- // put '0' back for the legacy-octal reader
			return p.parseLegacyOctalOrIdentity()
		}
		return 0
	case 'x':
		if !isHexDigit(p.peek()) || !isHexDigit(p.peekAt(1)) {
			if p.flags.UnicodeMode() {
				p.fail("invalid hexadecimal escape")
			}
			return 'x' // Annex B identity escape.
		}
		return rune(hexVal(p.advance())*16 + hexVal(p.advance()))
	case 'u':
		return p.readUnicodeEscapeValue()
	case 'c':
		if isASCIILetter(p.peek()) {
			return p.advance() % 32
		}
		if p.flags.UnicodeMode() {
			p.fail("invalid control escape")
		}
		// Annex B: a '\c' not followed by a letter is a literal backslash.
		return '\\'
	default:
		if p.flags.UnicodeMode() && !isSyntaxChar(c) && c != '/' {
			p.fail("invalid identity escape")
		}
		return c
	}
}

// readUnicodeEscapeValue parses the tail of a \u escape (the 'u' consumed),
// returning the code point. In Unicode mode it accepts \u{...} and combines a
// surrogate pair; otherwise it requires exactly four hex digits and falls back
// to the identity escape 'u' in Annex B.
func (p *parser) readUnicodeEscapeValue() rune {
	if p.flags.UnicodeMode() && p.peek() == '{' {
		p.advance() // '{'
		start := p.pos
		v := 0
		for isHexDigit(p.peek()) {
			v = v*16 + hexVal(p.advance())
			if v > 0x10FFFF {
				p.fail("Unicode code point out of range")
			}
		}
		if p.pos == start || p.peek() != '}' {
			p.fail("invalid Unicode escape")
		}
		p.advance() // '}'
		return rune(v)
	}
	if !p.has4Hex() {
		if p.flags.UnicodeMode() {
			p.fail("invalid Unicode escape")
		}
		return 'u'
	}
	hi := p.read4Hex()
	if p.flags.UnicodeMode() && hi >= 0xD800 && hi <= 0xDBFF && p.peek() == '\\' && p.peekAt(1) == 'u' {
		save := p.pos
		p.advance() // '\'
		p.advance() // 'u'
		if p.peek() != '{' && p.has4Hex() {
			lo := p.read4Hex()
			if lo >= 0xDC00 && lo <= 0xDFFF {
				return (hi-0xD800)<<10 + (lo - 0xDC00) + 0x10000
			}
		}
		p.pos = save
	}
	return hi
}

func (p *parser) has4Hex() bool {
	return isHexDigit(p.peek()) && isHexDigit(p.peekAt(1)) &&
		isHexDigit(p.peekAt(2)) && isHexDigit(p.peekAt(3))
}

func (p *parser) read4Hex() rune {
	v := 0
	for i := 0; i < 4; i++ {
		v = v*16 + hexVal(p.advance())
	}
	return rune(v)
}

// parseLegacyOctalOrIdentity reads an Annex B legacy octal escape (up to three
// octal digits, value <= 255) with p.pos at the first digit, or treats a
// leading 8/9 as an identity escape.
func (p *parser) parseLegacyOctalOrIdentity() rune {
	c := p.peek()
	if c == '8' || c == '9' {
		p.advance()
		return c
	}
	v := 0
	for count := 0; count < 3 && p.peek() >= '0' && p.peek() <= '7'; count++ {
		nv := v*8 + int(p.peek()-'0')
		if nv > 255 {
			break
		}
		v = nv
		p.advance()
	}
	return rune(v)
}

// parsePropertyEscape parses \p{Name} or \p{Name=Value} with p.pos at the
// 'p'/'P'. Name/value validation is deferred to the unicode layer.
func (p *parser) parsePropertyEscape(negate bool) ClassProperty {
	p.advance() // 'p' or 'P'
	if p.peek() != '{' {
		p.fail("invalid property escape")
	}
	p.advance() // '{'
	var name, value []rune
	for !p.eof() && p.peek() != '}' && p.peek() != '=' {
		name = append(name, p.advance())
	}
	hasValue := false
	if p.peek() == '=' {
		hasValue = true
		p.advance()
		for !p.eof() && p.peek() != '}' {
			value = append(value, p.advance())
		}
	}
	if p.peek() != '}' {
		p.fail("unterminated property escape")
	}
	p.advance() // '}'
	if len(name) == 0 || (hasValue && len(value) == 0) {
		p.fail("invalid property name")
	}
	return ClassProperty{Name: string(name), Value: string(value), Negate: negate}
}
