package jsregexp

import "unicode/utf16"

// The compiler lowers a parsed *Pattern into a tree of matcher closures (the
// prog). It follows the continuation-passing Matcher model of §22.2.2: each node
// becomes a matcher that consumes input and calls its continuation, and choice
// points (disjunction, quantifier, lookaround) drive backtracking by trying
// alternatives in the spec-mandated order. Character classes are resolved to
// runeSets here so matching is a range test rather than a tree walk.

type compiler struct {
	flags Flags
	names map[string]int
	ic    bool // ignoreCase
	ml    bool // multiline
	da    bool // dotAll
	u     bool // unicode mode (u or v)
	back  bool // compile for right-to-left matching (inside a lookbehind body)
}

// compile builds re.prog from the parsed pattern. It is idempotent.
func (re *Regexp) compile() error {
	if re.prog != nil {
		return nil
	}
	c := &compiler{
		flags: re.flags,
		names: re.pattern.GroupNames,
		ic:    re.flags.IgnoreCase,
		ml:    re.flags.Multiline,
		da:    re.flags.DotAll,
		u:     re.flags.UnicodeMode(),
	}
	entry, err := c.node(re.pattern.Body)
	if err != nil {
		return err
	}
	re.prog = &prog{
		entry:     entry,
		numGroups: re.pattern.GroupCount,
		names:     re.pattern.GroupNames,
		flags:     re.flags,
		unicode:   re.flags.UnicodeMode(),
	}
	return nil
}

// node compiles a single AST node to a matcher.
func (c *compiler) node(n Node) (matcher, error) {
	switch t := n.(type) {
	case *Empty:
		return func(m *machine, sp int, k cont) bool { return k(sp) }, nil

	case *Char:
		return c.charMatcher(t.R), nil

	case *AnyChar:
		return consumerMatcher(c.anyConsumer()), nil

	case *Assertion:
		return c.assertionMatcher(t.Kind), nil

	case *CharClass:
		return c.classMatcher(t)

	case *Concat:
		return c.concat(t.Terms)

	case *Disjunction:
		return c.disjunction(t.Alternatives)

	case *Capture:
		return c.capture(t)

	case *Group:
		// Inline modifiers change ignoreCase/multiline/dotAll for the body only.
		if t.Mods != nil {
			return c.withModifiers(t.Mods, t.Body)
		}
		return c.node(t.Body)

	case *Quantifier:
		return c.quantifier(t)

	case *BackRef:
		return c.backref(t.Index), nil

	case *NamedBackRef:
		idx, ok := c.names[t.Name]
		if !ok {
			return nil, &SyntaxError{Msg: "reference to undefined named group " + t.Name, Pos: -1}
		}
		return c.backref(idx), nil

	case *Lookaround:
		return c.lookaround(t)

	default:
		return nil, &SyntaxError{Msg: "unsupported regex construct", Pos: -1}
	}
}

// unitConsumer matches a single fixed-width character at sp and returns the
// position after it. It performs no backtracking and takes no continuation, so a
// quantifier over a consumer can iterate instead of recurse — the key to not
// overflowing the Go stack on long inputs.
type unitConsumer func(m *machine, sp int) (int, bool)

// consumerMatcher lifts a unitConsumer into a matcher.
func consumerMatcher(cons unitConsumer) matcher {
	return func(m *machine, sp int, k cont) bool {
		if m.err != nil || !m.step() {
			return false
		}
		if np, ok := cons(m, sp); ok {
			return k(np)
		}
		return false
	}
}

// readCP reads one code point in the compiler's match direction: forward returns
// the character at sp and the position after it; backward (inside a lookbehind)
// returns the character ending at sp and the position before it. ok is false at
// the boundary in the direction of travel.
func (c *compiler) readCP(m *machine, sp int) (r rune, next int, ok bool) {
	if c.back {
		if sp <= 0 {
			return 0, 0, false
		}
		r, w := m.codePointBefore(sp)
		return r, sp - w, true
	}
	if sp >= len(m.input) {
		return 0, 0, false
	}
	r, w := m.codePointAt(sp)
	return r, sp + w, true
}

func (c *compiler) charConsumer(r rune) unitConsumer {
	// A non-Unicode astral literal is itself a surrogate pair — match both units.
	if !c.u && r > 0xFFFF {
		hi, lo := utf16.EncodeRune(r)
		uh, ul := uint16(hi), uint16(lo)
		if c.back {
			return func(m *machine, sp int) (int, bool) {
				if sp-2 >= 0 && m.input[sp-2] == uh && m.input[sp-1] == ul {
					return sp - 2, true
				}
				return 0, false
			}
		}
		return func(m *machine, sp int) (int, bool) {
			if sp+1 < len(m.input) && m.input[sp] == uh && m.input[sp+1] == ul {
				return sp + 2, true
			}
			return 0, false
		}
	}
	ic, u := c.ic, c.u
	cr := canonicalize(r, u)
	return func(m *machine, sp int) (int, bool) {
		ch, next, ok := c.readCP(m, sp)
		if !ok {
			return 0, false
		}
		if ch == r || (ic && canonicalize(ch, u) == cr) {
			return next, true
		}
		return 0, false
	}
}

func (c *compiler) anyConsumer() unitConsumer {
	da := c.da
	return func(m *machine, sp int) (int, bool) {
		r, next, ok := c.readCP(m, sp)
		if !ok {
			return 0, false
		}
		if da || !isLineTerminator(r) {
			return next, true
		}
		return 0, false
	}
}

func (c *compiler) classConsumer(set *runeSet, negate bool) unitConsumer {
	ic, u := c.ic, c.u
	return func(m *machine, sp int) (int, bool) {
		r, next, ok := c.readCP(m, sp)
		if !ok {
			return 0, false
		}
		if classContainsFold(set, r, ic, u) != negate {
			return next, true
		}
		return 0, false
	}
}

func (c *compiler) charMatcher(r rune) matcher { return consumerMatcher(c.charConsumer(r)) }

// simpleConsumer returns a unitConsumer for nodes that match exactly one
// fixed-width character with no captures or internal backtracking, so a
// quantifier over them can run iteratively. ok is false for anything else.
func (c *compiler) simpleConsumer(n Node) (unitConsumer, bool) {
	switch t := n.(type) {
	case *Char:
		return c.charConsumer(t.R), true
	case *AnyChar:
		return c.anyConsumer(), true
	case *CharClass:
		cs, err := c.compileClassSet(t.Set)
		if err != nil || len(cs.strings) > 0 {
			return nil, false // string-bearing (or erroring) classes take the normal path
		}
		return c.classConsumer(cs.set, t.Negate), true
	}
	return nil, false
}

func (c *compiler) assertionMatcher(kind AssertKind) matcher {
	ml := c.ml
	// Under /iu, ſ (U+017F) and K (U+212A) fold to word characters (§22.2.2.7.3).
	fold := c.ic && c.u
	isWord := func(units []uint16, i int) bool {
		if i < 0 || i >= len(units) {
			return false
		}
		return wordCharFold(rune(units[i]), fold)
	}
	return func(m *machine, sp int, k cont) bool {
		if m.err != nil || !m.step() {
			return false
		}
		in := m.input
		switch kind {
		case AssertBOL:
			if sp == 0 || (ml && isLineTerminator(rune(in[sp-1]))) {
				return k(sp)
			}
		case AssertEOL:
			if sp == len(in) || (ml && isLineTerminator(rune(in[sp]))) {
				return k(sp)
			}
		case AssertWordB, AssertNotWordB:
			boundary := isWord(in, sp-1) != isWord(in, sp)
			if kind == AssertNotWordB {
				boundary = !boundary
			}
			if boundary {
				return k(sp)
			}
		}
		return false
	}
}

func (c *compiler) classMatcher(cc *CharClass) (matcher, error) {
	cs, err := c.compileClassSet(cc.Set)
	if err != nil {
		return nil, err
	}
	if len(cs.strings) > 0 {
		if cc.Negate {
			return nil, &SyntaxError{Msg: "negated character class may contain strings", Pos: -1}
		}
		return c.classStringMatcher(cs), nil
	}
	return consumerMatcher(c.classConsumer(cs.set, cc.Negate)), nil
}

// compileClassSet resolves a ClassSet into a runeSet, applying v-mode set
// operations. Multi-code-point class strings are not yet representable in a
// runeSet and are rejected here (handled in the unicode phase).
func (c *compiler) compileClassSet(cs *ClassSet) (charSetV, error) {
	operand := func(item ClassItem) (charSetV, error) {
		var b setBuilder
		switch it := item.(type) {
		case ClassRange:
			b.addRange(it.Lo, it.Hi)
		case ClassEscape:
			b.addClassEscape(it.Kind, c.ic && c.u)
		case ClassProperty:
			s, err := resolveProperty(it.Name, it.Value)
			if err != nil {
				// May be a property of strings (v mode only).
				if pos, ok := propertyOfStrings(it.Name); ok && it.Value == "" {
					if it.Negate {
						return charSetV{}, &SyntaxError{Msg: "negated property of strings", Pos: -1}
					}
					return pos, nil
				}
				return charSetV{}, err
			}
			if it.Negate {
				var nb setBuilder
				nb.addComplement(s.ranges)
				return charSetV{set: nb.build()}, nil
			}
			return charSetV{set: s}, nil
		case ClassString:
			switch len(it.Runes) {
			case 1:
				b.addRune(it.Runes[0])
			default: // 0 (empty) or >=2 (multi-code-point)
				return charSetV{set: &runeSet{}, strings: [][]rune{append([]rune(nil), it.Runes...)}}, nil
			}
		case ClassStringDisjunction:
			var b2 setBuilder
			var strs [][]rune
			for _, alt := range it.Alts {
				if len(alt) == 1 {
					b2.addRune(alt[0])
				} else {
					strs = append(strs, append([]rune(nil), alt...))
				}
			}
			return charSetV{set: b2.build(), strings: strs}, nil
		case NestedClass:
			inner, err := c.compileClassSet(it.Set)
			if err != nil {
				return charSetV{}, err
			}
			if it.Negate {
				if len(inner.strings) > 0 {
					return charSetV{}, &SyntaxError{Msg: "negated character class may contain strings", Pos: -1}
				}
				var nb setBuilder
				nb.addComplement(inner.set.ranges)
				return charSetV{set: nb.build()}, nil
			}
			return inner, nil
		}
		return charSetV{set: b.build()}, nil
	}

	ops := make([]charSetV, 0, len(cs.Items))
	for _, item := range cs.Items {
		s, err := operand(item)
		if err != nil {
			return charSetV{}, err
		}
		if s.set == nil {
			s.set = &runeSet{}
		}
		ops = append(ops, s)
	}
	if len(ops) == 0 {
		return charSetV{set: &runeSet{}}, nil
	}

	switch cs.Op {
	case SetIntersection:
		acc := ops[0]
		for _, s := range ops[1:] {
			set := intersect(acc.set, s.set)
			var strs [][]rune
			for _, str := range acc.strings {
				if stringInList(s.strings, str) {
					strs = append(strs, str)
				}
			}
			acc = charSetV{set: set, strings: strs}
		}
		return acc, nil
	case SetSubtraction:
		acc := ops[0]
		for _, s := range ops[1:] {
			set := subtract(acc.set, s.set)
			var strs [][]rune
			for _, str := range acc.strings {
				if !stringInList(s.strings, str) {
					strs = append(strs, str)
				}
			}
			acc = charSetV{set: set, strings: strs}
		}
		return acc, nil
	default: // union
		var b setBuilder
		var strs [][]rune
		for _, s := range ops {
			b.ranges = append(b.ranges, s.set.ranges...)
			strs = append(strs, s.strings...)
		}
		return charSetV{set: b.build(), strings: dedupStrings(strs)}, nil
	}
}

func (c *compiler) concat(terms []Node) (matcher, error) {
	ms := make([]matcher, len(terms))
	for i, t := range terms {
		m, err := c.node(t)
		if err != nil {
			return nil, err
		}
		ms[i] = m
	}
	// Right-to-left matching consumes the rightmost term first, so apply the
	// (already backward-compiled) terms in reverse source order.
	if c.back {
		for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
			ms[i], ms[j] = ms[j], ms[i]
		}
	}
	return func(m *machine, sp int, k cont) bool {
		var run func(i, sp int) bool
		run = func(i, sp int) bool {
			if m.err != nil {
				return false
			}
			if i == len(ms) {
				return k(sp)
			}
			if !m.enter() {
				return false
			}
			defer m.leave()
			return ms[i](m, sp, func(sp2 int) bool { return run(i+1, sp2) })
		}
		return run(0, sp)
	}, nil
}

func (c *compiler) disjunction(alts []Node) (matcher, error) {
	ms := make([]matcher, len(alts))
	for i, a := range alts {
		m, err := c.node(a)
		if err != nil {
			return nil, err
		}
		ms[i] = m
	}
	return func(m *machine, sp int, k cont) bool {
		for _, alt := range ms {
			if m.err != nil {
				return false
			}
			if alt(m, sp, k) {
				return true
			}
		}
		return false
	}, nil
}

func (c *compiler) capture(cap *Capture) (matcher, error) {
	body, err := c.node(cap.Body)
	if err != nil {
		return nil, err
	}
	i2, i2p1 := 2*cap.Index, 2*cap.Index+1
	back := c.back
	return func(m *machine, sp int, k cont) bool {
		oldS, oldE := m.caps[i2], m.caps[i2p1]
		ok := body(m, sp, func(end int) bool {
			ps, pe := m.caps[i2], m.caps[i2p1]
			// Store the group as [start,end) with start<end. Forward matching
			// enters at the start (sp) and exits at the end; backward matching
			// enters at the end (sp) and exits at the start.
			if back {
				m.caps[i2], m.caps[i2p1] = end, sp
			} else {
				m.caps[i2], m.caps[i2p1] = sp, end
			}
			if k(end) {
				return true
			}
			m.caps[i2], m.caps[i2p1] = ps, pe
			return false
		})
		if !ok {
			m.caps[i2], m.caps[i2p1] = oldS, oldE
		}
		return ok
	}, nil
}

func (c *compiler) backref(index int) matcher {
	i2, i2p1 := 2*index, 2*index+1
	ic := c.ic
	u := c.u
	back := c.back
	return func(m *machine, sp int, k cont) bool {
		if m.err != nil || !m.step() {
			return false
		}
		s, e := m.caps[i2], m.caps[i2p1]
		if s < 0 || e < 0 {
			return k(sp) // an unmatched group backreference matches the empty string
		}
		if back {
			// Match the referenced text [s,e) so that it ends at sp, consuming
			// leftward. Case-folding compares whole code points from both ends.
			if !ic {
				n := e - s
				if sp-n < 0 {
					return false
				}
				for x := 0; x < n; x++ {
					if m.input[sp-n+x] != m.input[s+x] {
						return false
					}
				}
				return k(sp - n)
			}
			i, j := sp, e
			for j > s {
				if i <= 0 {
					return false
				}
				ri, wi := m.codePointBefore(i)
				rj, wj := m.codePointBefore(j)
				if canonicalize(ri, u) != canonicalize(rj, u) {
					return false
				}
				i -= wi
				j -= wj
			}
			return k(i)
		}
		if !ic {
			n := e - s
			if sp+n > len(m.input) {
				return false
			}
			for x := 0; x < n; x++ {
				if m.input[sp+x] != m.input[s+x] {
					return false
				}
			}
			return k(sp + n)
		}
		// Case-insensitive: compare code point by code point so folding applies to
		// whole characters (astral included) rather than surrogate halves.
		i, j := sp, s
		for j < e {
			if i >= len(m.input) {
				return false
			}
			ri, wi := m.codePointAt(i)
			rj, wj := m.codePointAt(j)
			if canonicalize(ri, u) != canonicalize(rj, u) {
				return false
			}
			i += wi
			j += wj
		}
		return k(i)
	}
}

// quantifier implements RepeatMatcher (§22.2.2.5), including the per-iteration
// reset of captures declared inside the body and the empty-iteration guard that
// prevents infinite loops on nullable bodies.
func (c *compiler) quantifier(q *Quantifier) (matcher, error) {
	// Fast path: a quantifier over a single fixed-width character with no
	// captures runs iteratively, so long inputs (a*, \w+, [^"]* ...) never grow
	// the Go stack.
	if cons, ok := c.simpleConsumer(q.Body); ok {
		return c.simpleQuantifier(cons, q.Min, q.Max, q.Greedy), nil
	}

	body, err := c.node(q.Body)
	if err != nil {
		return nil, err
	}
	lo, hi := captureRange(q.Body)
	greedy := q.Greedy

	return func(m *machine, sp int, k cont) bool {
		var repeat func(min, max, x int) bool
		repeat = func(min, max, x int) bool {
			if m.err != nil || !m.step() || !m.enter() {
				return false
			}
			defer m.leave()
			if max == 0 {
				return k(x)
			}
			d := func(y int) bool {
				if min == 0 && y == x {
					return false // empty iteration: force the loop to stop
				}
				nmin := min
				if nmin > 0 {
					nmin--
				}
				nmax := max
				if nmax > 0 {
					nmax--
				}
				return repeat(nmin, nmax, y)
			}
			saved := cloneRange(m, lo, hi)
			resetRange(m, lo, hi)
			if min != 0 {
				if body(m, x, d) {
					return true
				}
				setRange(m, lo, hi, saved)
				return false
			}
			if greedy {
				if body(m, x, d) {
					return true
				}
				setRange(m, lo, hi, saved)
				return k(x)
			}
			// lazy: try the continuation (with original captures) before the body.
			setRange(m, lo, hi, saved)
			if k(x) {
				return true
			}
			resetRange(m, lo, hi)
			if body(m, x, d) {
				return true
			}
			setRange(m, lo, hi, saved)
			return false
		}
		return repeat(q.Min, q.Max, sp)
	}, nil
}

// simpleQuantifier matches a quantifier whose body consumes exactly one
// fixed-width character (no captures) by greedily collecting the positions after
// each repetition, then trying continuations from the longest (greedy) or
// shortest (lazy) — all iteratively, with no recursion over the repetition count.
func (c *compiler) simpleQuantifier(cons unitConsumer, min, max int, greedy bool) matcher {
	return func(m *machine, sp int, k cont) bool {
		if m.err != nil {
			return false
		}
		positions := []int{sp}
		cur := sp
		for max < 0 || len(positions)-1 < max {
			if !m.step() {
				return false
			}
			np, ok := cons(m, cur)
			if !ok || np == cur {
				break
			}
			cur = np
			positions = append(positions, cur)
		}
		count := len(positions) - 1
		if count < min {
			return false
		}
		if greedy {
			for n := count; n >= min; n-- {
				if k(positions[n]) {
					return true
				}
				if m.err != nil {
					return false
				}
			}
		} else {
			for n := min; n <= count; n++ {
				if k(positions[n]) {
					return true
				}
				if m.err != nil {
					return false
				}
			}
		}
		return false
	}
}

func (c *compiler) lookaround(l *Lookaround) (matcher, error) {
	// A lookahead body always matches forward and a lookbehind body always
	// matches backward (right-to-left), independent of the enclosing direction —
	// so compile the body with its own direction rather than inheriting c.back.
	sub := *c
	sub.back = l.Behind
	body, err := sub.node(l.Body)
	if err != nil {
		return nil, err
	}

	// Both directions are zero-width from the outside: the body is anchored at sp
	// (a lookahead consumes rightward to some end, a lookbehind consumes leftward
	// to some start) and the continuation resumes at sp regardless.
	//
	// A lookaround is atomic with respect to the surrounding match (§22.2.2.4/.5):
	// the body is run with a continuation that always succeeds, so it stops at its
	// first complete match and fixes its captures; the outer continuation k then
	// runs SEPARATELY. If k fails there is no backtracking back into the body to
	// try a different internal match — hence "do not backtrack into a lookbehind".
	// (Internal backtracking WITHIN the body still happens while it searches for
	// that first complete match.)
	if !l.Negate {
		return func(m *machine, sp int, k cont) bool {
			if m.err != nil {
				return false
			}
			saved := cloneAll(m)
			if !body(m, sp, func(int) bool { return true }) {
				setAll(m, saved)
				return false
			}
			// Positive form: the body's captures are visible to k and the result.
			if k(sp) {
				return true
			}
			setAll(m, saved) // atomic: discard the body's captures on outer failure
			return false
		}, nil
	}
	return func(m *machine, sp int, k cont) bool {
		if m.err != nil {
			return false
		}
		saved := cloneAll(m)
		found := body(m, sp, func(int) bool { return true })
		setAll(m, saved)
		if found {
			return false
		}
		return k(sp)
	}, nil
}

// withModifiers compiles body under a temporarily adjusted flag set for inline
// (?ims-ims:...) groups.
func (c *compiler) withModifiers(mods *Modifiers, body Node) (matcher, error) {
	sub := *c
	if mods.AddI {
		sub.ic = true
	}
	if mods.SubI {
		sub.ic = false
	}
	if mods.AddM {
		sub.ml = true
	}
	if mods.SubM {
		sub.ml = false
	}
	if mods.AddS {
		sub.da = true
	}
	if mods.SubS {
		sub.da = false
	}
	return sub.node(body)
}

// --- capture-range helpers ---

// captureRange returns the inclusive [min,max] capture indices declared inside
// n, or lo>hi when n contains no captures.
func captureRange(n Node) (int, int) {
	lo, hi := 1<<30, 0
	var walk func(Node)
	walk = func(x Node) {
		switch t := x.(type) {
		case *Capture:
			if t.Index < lo {
				lo = t.Index
			}
			if t.Index > hi {
				hi = t.Index
			}
			walk(t.Body)
		case *Group:
			walk(t.Body)
		case *Quantifier:
			walk(t.Body)
		case *Lookaround:
			walk(t.Body)
		case *Concat:
			for _, term := range t.Terms {
				walk(term)
			}
		case *Disjunction:
			for _, alt := range t.Alternatives {
				walk(alt)
			}
		}
	}
	walk(n)
	return lo, hi
}

func cloneRange(m *machine, lo, hi int) []int {
	if lo > hi {
		return nil
	}
	out := make([]int, 2*(hi-lo+1))
	copy(out, m.caps[2*lo:2*hi+2])
	return out
}

func setRange(m *machine, lo, hi int, saved []int) {
	if lo > hi || saved == nil {
		return
	}
	copy(m.caps[2*lo:2*hi+2], saved)
}

func resetRange(m *machine, lo, hi int) {
	if lo > hi {
		return
	}
	for i := 2 * lo; i <= 2*hi+1; i++ {
		m.caps[i] = -1
	}
}

func cloneAll(m *machine) []int {
	out := make([]int, len(m.caps))
	copy(out, m.caps)
	return out
}

func setAll(m *machine, saved []int) { copy(m.caps, saved) }
