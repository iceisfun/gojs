package jsregexp

// ClassSet models the contents of a character class [...]. In the default and
// u modes a class is a flat union of items. In v mode ([...] with the v flag) a
// class may instead be an intersection or difference of nested operands and may
// contain multi-code-point strings; those richer forms are represented with the
// same structure (Op plus Items, where an Item can itself be a nested class).
type ClassSet struct {
	Op    SetOp
	Items []ClassItem
}

// SetOp selects how a ClassSet's items combine. Union is the default (and the
// only possibility outside v mode).
type SetOp int

const (
	SetUnion        SetOp = iota // A B C ...
	SetIntersection              // A&&B&&...   (v mode)
	SetSubtraction               // A--B--...   (v mode)
)

// ClassItem is one member/operand of a ClassSet.
type ClassItem interface{ isClassItem() }

// ClassRange is a single code point (Lo == Hi) or an inclusive range Lo-Hi.
type ClassRange struct {
	Lo, Hi rune
}

// ClassEscKind enumerates the character-class escapes usable inside a class.
type ClassEscKind int

const (
	EscDigit    ClassEscKind = iota // \d
	EscNotDigit                     // \D
	EscWord                         // \w
	EscNotWord                      // \W
	EscSpace                        // \s
	EscNotSpace                     // \S
)

// ClassEscape is \d \D \w \W \s \S appearing inside a class.
type ClassEscape struct {
	Kind ClassEscKind
}

// ClassProperty is a Unicode property escape \p{...} / \P{...}. Value is empty
// for the shorthand \p{Name} form; Negate is true for \P. Resolution to a code
// point set is deferred to the unicode layer.
type ClassProperty struct {
	Name   string
	Value  string
	Negate bool
}

// ClassString is a multi-code-point class string, produced by \q{...} members
// in v mode (and by folding in v-mode string properties). A single-element or
// empty string is legal.
type ClassString struct {
	Runes []rune
}

// NestedClass is a bracketed class nested inside another (v mode only), e.g.
// [\w&&[^_]].
type NestedClass struct {
	Negate bool
	Set    *ClassSet
}

func (ClassRange) isClassItem()    {}
func (ClassEscape) isClassItem()   {}
func (ClassProperty) isClassItem() {}
func (ClassString) isClassItem()   {}
func (NestedClass) isClassItem()   {}
