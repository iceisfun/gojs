package interp

import "strings"

// strRope is an immutable "cons-string": a lazily-flattened concatenation tree
// produced by the + operator. Repeatedly building a string with `s += chunk`
// would otherwise copy the whole accumulator on every step (O(n²) total); a rope
// makes each concatenation an O(1) tree node and flattens to a String once, when
// the actual characters are needed (ToStringV, comparison, indexing, a native
// call, ...). Because it is never mutated it is safe to alias and share.
//
// A rope only ever holds String or *strRope children, so its value is always a
// string primitive; Typeof reports "string" and every string-consuming abstract
// operation flattens it (see flattenRope and the ToPrimitive/ToStringV paths).
type strRope struct {
	left, right Value // each a String or *strRope
	length      int   // byte length of the flattened string
}

func (*strRope) Typeof() string { return "string" }

// stringByteLen returns the flattened byte length of a String or *strRope.
func stringByteLen(v Value) int {
	switch x := v.(type) {
	case String:
		return len(x)
	case *strRope:
		return x.length
	}
	return 0
}

// isStringish reports whether v is already a string primitive (a String or a
// rope) and therefore needs no ToPrimitive/ToString coercion.
func isStringish(v Value) bool {
	switch v.(type) {
	case String, *strRope:
		return true
	}
	return false
}

// concatStrings builds the concatenation of two string primitives as a rope node
// in O(1), collapsing an empty operand so a rope never wraps "".
func concatStrings(l, r Value) Value {
	ln, rn := stringByteLen(l), stringByteLen(r)
	if ln == 0 {
		return r
	}
	if rn == 0 {
		return l
	}
	return &strRope{left: l, right: r, length: ln + rn}
}

// flattenRope resolves a rope to its String; any other value passes through
// unchanged. Use it at the boundary of code that needs a concrete String.
func flattenRope(v Value) Value {
	if r, ok := v.(*strRope); ok {
		return String(r.build())
	}
	return v
}

// build flattens the rope to a Go string. It walks the tree with an explicit
// stack (not recursion) so a left-leaning `((a+b)+c)+…` chain of any depth — the
// exact shape `s += chunk` produces — cannot overflow the Go stack; for that
// shape the working stack also stays shallow.
func (r *strRope) build() string {
	var b strings.Builder
	b.Grow(r.length)
	stack := []Value{r}
	for len(stack) > 0 {
		n := len(stack) - 1
		v := stack[n]
		stack = stack[:n]
		switch x := v.(type) {
		case String:
			b.WriteString(string(x))
		case *strRope:
			stack = append(stack, x.right, x.left) // left popped (written) first
		}
	}
	return b.String()
}
