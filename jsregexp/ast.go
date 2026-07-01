package jsregexp

// The regex AST. Nodes are constructed by the parser and treated as immutable
// thereafter; the compiler consumes them without mutation. Every node records
// no source position of its own — position-bearing diagnostics are produced at
// parse time — keeping the tree compact and comparable.

// Node is implemented by every regex AST node.
type Node interface{ isNode() }

// Pattern is the root: a Disjunction plus the capture metadata gathered while
// parsing. GroupCount counts capturing groups (1-based indices in Capture
// nodes); GroupNames maps each named group to its capture index.
type Pattern struct {
	Body       Node
	GroupCount int
	GroupNames map[string]int
	Flags      Flags
}

// Disjunction is one or more alternatives separated by '|'. A trailing or
// leading empty alternative (e.g. "a|" or "|a") yields an Empty alternative.
type Disjunction struct {
	Alternatives []Node
}

// Concat is an ordered sequence of terms (an Alternative in the grammar). An
// empty Concat matches the empty string.
type Concat struct {
	Terms []Node
}

// Empty matches the empty string. Used for empty alternatives and empty groups.
type Empty struct{}

// Char matches a single specific code point (already case-folded decisions are
// deferred to the matcher, which consults the i flag).
type Char struct {
	R rune
}

// AnyChar is '.'; whether it matches line terminators depends on the s flag,
// resolved by the matcher.
type AnyChar struct{}

// AssertKind enumerates the zero-width assertions that are not lookarounds.
type AssertKind int

const (
	AssertBOL      AssertKind = iota // ^
	AssertEOL                        // $
	AssertWordB                      // \b
	AssertNotWordB                   // \B
)

// Assertion is a zero-width anchor (^, $, \b, \B).
type Assertion struct {
	Kind AssertKind
}

// Lookaround is (?= ) (?! ) (?<= ) (?<! ). Behind selects lookbehind; Negate
// selects the negative form.
type Lookaround struct {
	Behind bool
	Negate bool
	Body   Node
}

// Capture is a capturing group. Index is its 1-based capture index; Name is the
// group name for (?<name>...) groups, or "" when unnamed.
type Capture struct {
	Index int
	Name  string
	Body  Node
}

// Modifiers carries the add/remove sets for an inline flag modifier group
// (?ims:...) / (?-ims:...). Only i, m, and s may be modified (§22.2.1).
type Modifiers struct {
	AddI, AddM, AddS bool
	SubI, SubM, SubS bool
}

// Group is a non-capturing group (?:...), optionally carrying inline modifiers.
type Group struct {
	Mods *Modifiers // nil for a plain (?:...)
	Body Node
}

// BackRef is a numeric backreference (\1 ... \n). Index is 1-based.
type BackRef struct {
	Index int
}

// NamedBackRef is \k<name>. It is resolved to an index at compile time.
type NamedBackRef struct {
	Name string
}

// Quantifier applies a repetition to Body. Max == -1 means unbounded. Greedy is
// false for the lazy form (a suffix '?').
type Quantifier struct {
	Min, Max int
	Greedy   bool
	Body     Node
}

// CharClass is a bracketed class [...] or [^...]. Negate is true for [^...].
// In v mode the class may use set operations, represented in ClassSet.
type CharClass struct {
	Negate bool
	Set    *ClassSet
}

func (*Pattern) isNode()      {}
func (*Disjunction) isNode()  {}
func (*Concat) isNode()       {}
func (*Empty) isNode()        {}
func (*Char) isNode()         {}
func (*AnyChar) isNode()      {}
func (*Assertion) isNode()    {}
func (*Lookaround) isNode()   {}
func (*Capture) isNode()      {}
func (*Group) isNode()        {}
func (*BackRef) isNode()      {}
func (*NamedBackRef) isNode() {}
func (*Quantifier) isNode()   {}
func (*CharClass) isNode()    {}
