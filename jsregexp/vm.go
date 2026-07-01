package jsregexp

import (
	"context"
	"errors"
	"unicode/utf16"
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
	err     error // set once on cancellation/budget; short-circuits all matchers
}

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
	names     map[string]int
	flags     Flags
	unicode   bool
}

// ToUnits encodes a Go string to the UTF-16 code-unit slice the matcher operates
// on. Match offsets are indices into this slice, matching ECMAScript's string
// indexing (lastIndex, match indices, etc.).
func ToUnits(s string) []uint16 { return utf16.Encode([]rune(s)) }

// FromUnits decodes a UTF-16 code-unit slice back to a Go string.
func FromUnits(u []uint16) string { return string(utf16.Decode(u)) }

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
	for at := start; at <= len(input); at++ {
		m := &machine{
			input:   input,
			caps:    make([]int, 2*(p.numGroups+1)),
			unicode: p.unicode,
			ctx:     ctx,
			limit:   re.stepLimit,
		}
		for i := range m.caps {
			m.caps[i] = -1
		}
		m.caps[0] = at
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
