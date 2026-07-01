package jsregexp

// Flags is the parsed form of a RegExp flags string. The field order matches
// the canonical ordering ECMAScript uses when reflecting `.flags`
// (d, g, i, m, s, u, v, y).
type Flags struct {
	HasIndices  bool // d — provide match indices
	Global      bool // g — global (advance lastIndex)
	IgnoreCase  bool // i — case-insensitive
	Multiline   bool // m — ^ and $ match line boundaries
	DotAll      bool // s — . matches line terminators
	Unicode     bool // u — Unicode (code-point) mode
	UnicodeSets bool // v — Unicode Sets mode
	Sticky      bool // y — anchored at lastIndex
}

// UnicodeMode reports whether either Unicode mode (u or v) is active. Most
// grammar strictness is shared between the two modes; the v-only set-notation
// rules are gated on UnicodeSets specifically.
func (f Flags) UnicodeMode() bool { return f.Unicode || f.UnicodeSets }

// String renders the flags in canonical order, matching RegExp.prototype.flags.
func (f Flags) String() string {
	var b []byte
	if f.HasIndices {
		b = append(b, 'd')
	}
	if f.Global {
		b = append(b, 'g')
	}
	if f.IgnoreCase {
		b = append(b, 'i')
	}
	if f.Multiline {
		b = append(b, 'm')
	}
	if f.DotAll {
		b = append(b, 's')
	}
	if f.Unicode {
		b = append(b, 'u')
	}
	if f.UnicodeSets {
		b = append(b, 'v')
	}
	if f.Sticky {
		b = append(b, 'y')
	}
	return string(b)
}

// ParseFlags parses a RegExp flags string. Per §22.2.3.1 each flag character
// must be one of dgimsuvy, must not repeat, and u and v are mutually exclusive.
func ParseFlags(s string) (Flags, error) {
	var f Flags
	var seen [128]bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 128 || !seen0(c, &seen) {
			return Flags{}, &SyntaxError{Msg: "invalid or duplicate regular expression flag", Pos: -1}
		}
		switch c {
		case 'd':
			f.HasIndices = true
		case 'g':
			f.Global = true
		case 'i':
			f.IgnoreCase = true
		case 'm':
			f.Multiline = true
		case 's':
			f.DotAll = true
		case 'u':
			f.Unicode = true
		case 'v':
			f.UnicodeSets = true
		case 'y':
			f.Sticky = true
		default:
			return Flags{}, &SyntaxError{Msg: "invalid regular expression flag", Pos: -1}
		}
	}
	if f.Unicode && f.UnicodeSets {
		return Flags{}, &SyntaxError{Msg: "the u and v regular expression flags are mutually exclusive", Pos: -1}
	}
	return f, nil
}

// seen0 marks a flag byte as seen, returning false if it is not a recognized
// flag character or has already been seen (a duplicate).
func seen0(c byte, seen *[128]bool) bool {
	switch c {
	case 'd', 'g', 'i', 'm', 's', 'u', 'v', 'y':
		if seen[c] {
			return false
		}
		seen[c] = true
		return true
	}
	return false
}
