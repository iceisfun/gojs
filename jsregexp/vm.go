package jsregexp

import (
	"context"
	"errors"
	"unicode/utf8"
)

// ErrBudget is returned when a match exceeds the configured step budget. This is
// the ReDoS guard: because ECMAScript matching is backtracking, a pathological
// pattern/input can require exponential steps, so the host bounds the work and
// receives this error instead of hanging.
var ErrBudget = errors.New("jsregexp: step budget exceeded")

// DefaultStepBudget bounds the number of matcher steps for a single match
// attempt when the caller does not specify one. It is large enough for ordinary
// patterns yet small enough to fail fast on catastrophic backtracking.
const DefaultStepBudget = 10_000_000

// maxRecursionDepth bounds the live recursion depth of the backtracking matcher
// so a long input or huge quantifier over a complex body cannot overflow the Go
// stack; exceeding it fails the match with ErrBudget rather than crashing the
// host. Simple quantifiers use an iterative path and do not count against it.
const maxRecursionDepth = 60_000

// A matcher consumes input starting at sp and, on a successful partial match,
// invokes the continuation k with the new position; it returns whether the whole
// tail (this matcher plus k) succeeded. Matchers mutate m.caps in place and are
// responsible for restoring them when a branch fails. This mirrors the
// continuation-passing Matcher model of §22.2.2.
type matcher func(m *machine, sp int, k cont) bool

type cont func(sp int) bool

// machine holds the mutable state of one match attempt. input is a slice of
// UTF-16 code units — the unit ECMAScript strings and RegExp indices are defined
// over. In Unicode mode the matcher decodes surrogate pairs to code points via
// codePointAt; otherwise each unit is one character.
type machine struct {
	input   []uint16
	caps    []int // 2*(numGroups+1); index 2i/2i+1 are group i's [start,end); -1 = unset
	unicode bool
	ctx     context.Context
	steps   int
	limit   int
	depth   int
	err     error // set once on cancellation/budget; short-circuits all matchers
}

// enter accounts one level of matcher recursion, returning false (with m.err set)
// when the stack-depth guard trips. Every recursive driver pairs it with leave.
func (m *machine) enter() bool {
	if m.depth >= maxRecursionDepth {
		m.err = ErrBudget
		return false
	}
	m.depth++
	return true
}

func (m *machine) leave() { m.depth-- }

// codePointAt returns the character at code-unit index sp and its width in code
// units (1, or 2 for a surrogate pair). In non-Unicode mode every unit — lone
// surrogates included — is a one-unit character. Callers must ensure sp is in
// range.
func (m *machine) codePointAt(sp int) (rune, int) {
	c := rune(m.input[sp])
	if m.unicode && c >= 0xD800 && c <= 0xDBFF && sp+1 < len(m.input) {
		if lo := rune(m.input[sp+1]); lo >= 0xDC00 && lo <= 0xDFFF {
			return (c-0xD800)<<10 + (lo - 0xDC00) + 0x10000, 2
		}
	}
	return c, 1
}

// codePointBefore returns the character ending immediately before code-unit
// index sp and its width in code units (1, or 2 for a surrogate pair). It is the
// leftward companion of codePointAt, used when a lookbehind body is matched
// right-to-left. In non-Unicode mode every unit is a one-unit character.
func (m *machine) codePointBefore(sp int) (rune, int) {
	c := rune(m.input[sp-1])
	if m.unicode && c >= 0xDC00 && c <= 0xDFFF && sp-2 >= 0 {
		if hi := rune(m.input[sp-2]); hi >= 0xD800 && hi <= 0xDBFF {
			return (hi-0xD800)<<10 + (c - 0xDC00) + 0x10000, 2
		}
	}
	return c, 1
}

// step accounts one unit of work and enforces the budget and context. It returns
// false (with m.err set) once the attempt must be abandoned; every matcher checks
// m.err on entry so the failure unwinds the whole recursion promptly.
func (m *machine) step() bool {
	m.steps++
	if m.limit > 0 && m.steps > m.limit {
		m.err = ErrBudget
		return false
	}
	if m.ctx != nil && m.steps&0x3fff == 0 {
		select {
		case <-m.ctx.Done():
			m.err = m.ctx.Err()
			return false
		default:
		}
	}
	return true
}

// prog is a compiled pattern: an entry matcher plus the metadata needed to run
// and report a match.
type prog struct {
	entry     matcher
	numGroups int
	names     map[string][]int
	flags     Flags
	unicode   bool
}

// ToUnits encodes a Go string to the UTF-16 code-unit slice the matcher operates
// on. Match offsets are indices into this slice, matching ECMAScript's string
// indexing (lastIndex, match indices, etc.). The host stores strings as WTF-8,
// so a lone surrogate — encoded as 0xED 0xA0-0xBF 0x80-0xBF — is recovered as a
// single surrogate code unit rather than being folded to U+FFFD.
func ToUnits(s string) []uint16 {
	units := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
			units = append(units, uint16(rune(s[i]&0x0F)<<12|rune(s[i+1]&0x3F)<<6|rune(s[i+2]&0x3F)))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			r -= 0x10000
			units = append(units, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			units = append(units, uint16(r))
		}
		i += size
	}
	return units
}

// FromUnits decodes a UTF-16 code-unit slice back to a (WTF-8) Go string. A
// high/low surrogate pair combines into its astral scalar value; any other
// surrogate is preserved as a lone surrogate in WTF-8 (so `/./g` extracting one
// half of a pair yields the exact lone-surrogate code unit, not U+FFFD).
func FromUnits(u []uint16) string {
	buf := make([]byte, 0, len(u))
	for k := 0; k < len(u); {
		cu := u[k]
		if cu >= 0xD800 && cu <= 0xDBFF && k+1 < len(u) && u[k+1] >= 0xDC00 && u[k+1] <= 0xDFFF {
			cp := 0x10000 + (rune(cu)-0xD800)<<10 + (rune(u[k+1]) - 0xDC00)
			buf = utf8.AppendRune(buf, cp)
			k += 2
			continue
		}
		if cu >= 0xD800 && cu <= 0xDFFF {
			buf = append(buf, 0xE0|byte(cu>>12), 0x80|byte((cu>>6)&0x3F), 0x80|byte(cu&0x3F))
		} else {
			buf = utf8.AppendRune(buf, rune(cu))
		}
		k++
	}
	return string(buf)
}

// Match reports whether re matches anywhere in input, using the default budget.
func (re *Regexp) Match(ctx context.Context, input string) (bool, error) {
	loc, err := re.FindStringSubmatchIndex(ctx, input, 0)
	return loc != nil, err
}

// FindStringSubmatchIndex is FindSubmatchIndex over a Go string; the returned
// offsets are UTF-16 code-unit indices.
func (re *Regexp) FindStringSubmatchIndex(ctx context.Context, s string, start int) ([]int, error) {
	return re.FindSubmatchIndex(ctx, ToUnits(s), start)
}

// FindSubmatchIndex attempts a match at or after start (a UTF-16 code-unit
// index). On success it returns a slice of 2*(NumSubexp+1) code-unit offsets:
// pair 0 is the whole match, pair i is capture group i, with -1/-1 for groups
// that did not participate. It returns (nil, nil) for no match, or a non-nil
// error if the step budget or context was exceeded. The search honors the sticky
// (y) flag by anchoring at start.
func (re *Regexp) FindSubmatchIndex(ctx context.Context, input []uint16, start int) ([]int, error) {
	if re.prog == nil {
		if err := re.compile(); err != nil {
			return nil, err
		}
	}
	p := re.prog
	// One machine spans the whole search so the step budget and context deadline
	// bound the entire find, not each starting position independently. Otherwise
	// an unanchored no-match over a long input is O(n^2) work that resets steps
	// (and the every-0x3fff ctx poll) at every position, ignoring both the ReDoS
	// budget and a cancelled context.
	m := &machine{
		input:   input,
		caps:    make([]int, 2*(p.numGroups+1)),
		unicode: p.unicode,
		ctx:     ctx,
		limit:   re.stepLimit,
	}
	for at := start; at <= len(input); at = m.nextStart(at) {
		// Charge one step per starting position so advancing the anchor is itself
		// bounded and periodically polls the context.
		if !m.step() {
			return nil, m.err
		}
		for i := range m.caps {
			m.caps[i] = -1
		}
		m.caps[0] = at
		m.depth = 0
		matched := p.entry(m, at, func(end int) bool {
			m.caps[1] = end
			return true
		})
		if m.err != nil {
			return nil, m.err
		}
		if matched {
			return m.caps, nil
		}
		if p.flags.Sticky {
			break
		}
	}
	return nil, nil
}

// nextStart advances the unanchored search anchor by one position. In Unicode
// mode the anchor moves by a whole code point — two units across a surrogate
// pair — so a match can never begin between the halves of an astral character
// (§22.2.7.2 RegExpBuiltinExec uses AdvanceStringIndex for each retry). In
// non-Unicode mode every code unit is its own position.
func (m *machine) nextStart(at int) int {
	if m.unicode && at < len(m.input) &&
		m.input[at] >= 0xD800 && m.input[at] <= 0xDBFF &&
		at+1 < len(m.input) && m.input[at+1] >= 0xDC00 && m.input[at+1] <= 0xDFFF {
		return at + 2
	}
	return at + 1
}
