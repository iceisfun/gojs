package ast

import "github.com/iceisfun/gojs/token"

// This file defines the expression node types. Every type here implements the
// [Expr] interface via the exprNode marker method at the bottom of the file.

// ---------------------------------------------------------------------------
// Literals and primary expressions
// ---------------------------------------------------------------------------

// Ident is an identifier reference such as a variable name.
type Ident struct {
	NamePos token.Pos
	Name    string
	// Parenthesized marks an identifier that appeared as the immediate content of
	// a ParenthesizedExpression, e.g. `(fn)`. Such an operand is a
	// CoverParenthesizedExpression, whose IsIdentifierRef is false (§13.2.5.2), so
	// it does not trigger NamedEvaluation: `(fn) = function(){}` leaves the
	// function anonymous even though a bare `fn = function(){}` would name it.
	Parenthesized bool
}

func (e *Ident) Pos() token.Pos { return e.NamePos }
func (e *Ident) End() token.Pos { return endOf(e.NamePos, len(e.Name)) }

// PrivateIdent is a private class member name such as #count. It is only valid
// inside a class body (as a member key or in a `#x in obj` check).
type PrivateIdent struct {
	NamePos token.Pos
	Name    string // includes the leading '#'
}

func (e *PrivateIdent) Pos() token.Pos { return e.NamePos }
func (e *PrivateIdent) End() token.Pos { return endOf(e.NamePos, len(e.Name)) }

// NumberLit is a numeric literal. Value holds the parsed IEEE-754 value; Raw
// preserves the original spelling (e.g. "0xff", "1_000").
type NumberLit struct {
	ValuePos token.Pos
	Value    float64
	Raw      string
}

func (e *NumberLit) Pos() token.Pos { return e.ValuePos }
func (e *NumberLit) End() token.Pos { return endOf(e.ValuePos, len(e.Raw)) }

// BigIntLit is a BigInt literal such as 123n. Digits holds the decimal digit
// string without the trailing 'n'.
type BigIntLit struct {
	ValuePos token.Pos
	Raw      string // full spelling including trailing 'n'
	Digits   string // normalized decimal digits
}

func (e *BigIntLit) Pos() token.Pos { return e.ValuePos }
func (e *BigIntLit) End() token.Pos { return endOf(e.ValuePos, len(e.Raw)) }

// StringLit is a string literal. Value holds the decoded contents; Raw includes
// the surrounding quotes.
type StringLit struct {
	ValuePos token.Pos
	Value    string
	Raw      string
}

func (e *StringLit) Pos() token.Pos { return e.ValuePos }
func (e *StringLit) End() token.Pos { return endOf(e.ValuePos, len(e.Raw)) }

// BoolLit is a boolean literal (true or false).
type BoolLit struct {
	ValuePos token.Pos
	Value    bool
}

func (e *BoolLit) Pos() token.Pos { return e.ValuePos }
func (e *BoolLit) End() token.Pos {
	if e.Value {
		return endOf(e.ValuePos, 4)
	}
	return endOf(e.ValuePos, 5)
}

// NullLit is the null literal.
type NullLit struct {
	ValuePos token.Pos
}

func (e *NullLit) Pos() token.Pos { return e.ValuePos }
func (e *NullLit) End() token.Pos { return endOf(e.ValuePos, 4) }

// ThisExpr is the `this` keyword.
type ThisExpr struct {
	Keyword token.Pos
}

func (e *ThisExpr) Pos() token.Pos { return e.Keyword }
func (e *ThisExpr) End() token.Pos { return endOf(e.Keyword, 4) }

// SuperExpr is the `super` keyword (valid only as a member/call base inside
// class methods).
type SuperExpr struct {
	Keyword token.Pos
}

func (e *SuperExpr) Pos() token.Pos { return e.Keyword }
func (e *SuperExpr) End() token.Pos { return endOf(e.Keyword, 5) }

// TemplateLit is a template literal `a${x}b`. Quasis holds the literal string
// segments; Exprs holds the interpolated expressions. len(Quasis) is always
// len(Exprs)+1.
type TemplateLit struct {
	Start  token.Pos
	EndPos token.Pos
	Quasis []TemplateElement
	Exprs  []Expr
}

// TemplateElement is one cooked/raw string segment of a template literal.
type TemplateElement struct {
	Pos    token.Pos
	Cooked string // interpreted value (with escapes processed)
	Raw    string // exact source text
	// CookedInvalid reports that this segment contained an escape with no cooked
	// value (a legacy octal, \8/\9, or malformed hex/unicode escape). It is an
	// early SyntaxError in an untagged template; a tagged template yields an
	// undefined cooked value for the segment (ECMA-262 §12.9.6).
	CookedInvalid bool
}

func (e *TemplateLit) Pos() token.Pos { return e.Start }
func (e *TemplateLit) End() token.Pos { return e.EndPos }

// TaggedTemplateExpr is a tagged template such as tag`a${x}b`.
type TaggedTemplateExpr struct {
	Tag   Expr
	Quasi *TemplateLit
}

func (e *TaggedTemplateExpr) Pos() token.Pos { return e.Tag.Pos() }
func (e *TaggedTemplateExpr) End() token.Pos { return e.Quasi.End() }

// RegexLit is a regular-expression literal /pattern/flags.
type RegexLit struct {
	ValuePos token.Pos
	Pattern  string
	Flags    string
	Raw      string
}

func (e *RegexLit) Pos() token.Pos { return e.ValuePos }
func (e *RegexLit) End() token.Pos { return endOf(e.ValuePos, len(e.Raw)) }

// ---------------------------------------------------------------------------
// Composite literals
// ---------------------------------------------------------------------------

// ArrayLit is an array literal such as [1, 2, ...rest]. A nil element denotes
// an elision (a hole), e.g. [1, , 3].
type ArrayLit struct {
	Lbracket token.Pos
	Rbracket token.Pos
	Elements []Expr // element may be nil (hole) or *SpreadElement
	// TrailingComma is set when the literal ends with a comma (e.g. `[a,]`). It
	// is benign for an array expression but, when the literal is refined into an
	// ArrayAssignmentPattern, an elision/comma may not follow a rest element.
	TrailingComma bool
}

func (e *ArrayLit) Pos() token.Pos { return e.Lbracket }
func (e *ArrayLit) End() token.Pos { return endOf(e.Rbracket, 1) }

// ObjectLit is an object literal such as { a: 1, b, ...rest, [k]: v }.
type ObjectLit struct {
	Lbrace     token.Pos
	Rbrace     token.Pos
	Properties []*Property
}

func (e *ObjectLit) Pos() token.Pos { return e.Lbrace }
func (e *ObjectLit) End() token.Pos { return endOf(e.Rbrace, 1) }

// PropertyKind classifies an object-literal or class member.
type PropertyKind int

const (
	// PropInit is a normal data property (key: value or shorthand).
	PropInit PropertyKind = iota
	// PropGet is a getter accessor (get key() {...}).
	PropGet
	// PropSet is a setter accessor (set key(v) {...}).
	PropSet
	// PropSpread is a spread element (...expr).
	PropSpread
)

// Property is a single member of an object literal.
type Property struct {
	KeyPos token.Pos
	Key    Expr // Ident, StringLit, NumberLit, or computed Expr
	Value  Expr // value expression (nil for spread's separate handling)
	Kind   PropertyKind
	// Computed reports whether the key was written as [expr].
	Computed bool
	// Shorthand reports whether this was written as { x } rather than { x: x }.
	Shorthand bool
	// Method reports whether the value is a concise method definition.
	Method bool
}

func (p *Property) Pos() token.Pos { return p.KeyPos }
func (p *Property) End() token.Pos {
	if p.Value != nil {
		return p.Value.End()
	}
	return p.Key.End()
}

// SpreadElement is a spread/rest element ...expr used in array/call/object
// contexts.
type SpreadElement struct {
	Ellipsis token.Pos
	Argument Expr
}

func (e *SpreadElement) Pos() token.Pos { return e.Ellipsis }
func (e *SpreadElement) End() token.Pos { return e.Argument.End() }

// ---------------------------------------------------------------------------
// Operator expressions
// ---------------------------------------------------------------------------

// UnaryExpr is a prefix unary operation such as -x, !x, typeof x, void x,
// delete x.
type UnaryExpr struct {
	OpPos   token.Pos
	Op      token.Type
	Operand Expr
}

func (e *UnaryExpr) Pos() token.Pos { return e.OpPos }
func (e *UnaryExpr) End() token.Pos { return e.Operand.End() }

// UpdateExpr is an increment/decrement (++x, x--). Prefix distinguishes ++x
// from x++.
type UpdateExpr struct {
	OpPos   token.Pos
	Op      token.Type // token.INC or token.DEC
	Operand Expr
	Prefix  bool
}

func (e *UpdateExpr) Pos() token.Pos {
	if e.Prefix {
		return e.OpPos
	}
	return e.Operand.Pos()
}
func (e *UpdateExpr) End() token.Pos {
	if e.Prefix {
		return e.Operand.End()
	}
	return endOf(e.OpPos, 2)
}

// BinaryExpr is a binary operation such as a + b, a < b, a instanceof b. It
// does NOT include the short-circuiting logical operators (see [LogicalExpr]).
type BinaryExpr struct {
	Left  Expr
	OpPos token.Pos
	Op    token.Type
	Right Expr
}

func (e *BinaryExpr) Pos() token.Pos { return e.Left.Pos() }
func (e *BinaryExpr) End() token.Pos { return e.Right.End() }

// LogicalExpr is a short-circuiting logical operation: &&, ||, or ?? (nullish
// coalescing). It is distinct from [BinaryExpr] because evaluation order and
// short-circuit semantics differ.
type LogicalExpr struct {
	Left  Expr
	OpPos token.Pos
	Op    token.Type
	Right Expr
}

func (e *LogicalExpr) Pos() token.Pos { return e.Left.Pos() }
func (e *LogicalExpr) End() token.Pos { return e.Right.End() }

// AssignExpr is an assignment such as x = y or x += y. Op is token.ASSIGN for a
// plain assignment, or a compound-assignment token otherwise.
type AssignExpr struct {
	Target Expr // assignment target (Ident, MemberExpr, or destructuring pattern)
	OpPos  token.Pos
	Op     token.Type
	Value  Expr
}

func (e *AssignExpr) Pos() token.Pos { return e.Target.Pos() }
func (e *AssignExpr) End() token.Pos { return e.Value.End() }

// ConditionalExpr is the ternary operator test ? cons : alt.
type ConditionalExpr struct {
	Test       Expr
	Consequent Expr
	Alternate  Expr
}

func (e *ConditionalExpr) Pos() token.Pos { return e.Test.Pos() }
func (e *ConditionalExpr) End() token.Pos { return e.Alternate.End() }

// SequenceExpr is a comma expression (a, b, c) evaluating to its last operand.
type SequenceExpr struct {
	Exprs []Expr
}

func (e *SequenceExpr) Pos() token.Pos { return e.Exprs[0].Pos() }
func (e *SequenceExpr) End() token.Pos { return e.Exprs[len(e.Exprs)-1].End() }

// ---------------------------------------------------------------------------
// Access and call expressions
// ---------------------------------------------------------------------------

// MemberExpr is a property access a.b or a[b]. When Optional is true the access
// is part of an optional chain (a?.b).
type MemberExpr struct {
	Object   Expr
	Property Expr // Ident (dot) / PrivateIdent, or arbitrary Expr (computed)
	EndPos   token.Pos
	Computed bool
	Optional bool
}

func (e *MemberExpr) Pos() token.Pos { return e.Object.Pos() }
func (e *MemberExpr) End() token.Pos { return e.EndPos }

// CallExpr is a function call callee(args). When Optional is true it is an
// optional call (fn?.()).
type CallExpr struct {
	Callee    Expr
	Arguments []Expr // may contain *SpreadElement
	Rparen    token.Pos
	Optional  bool
}

func (e *CallExpr) Pos() token.Pos { return e.Callee.Pos() }
func (e *CallExpr) End() token.Pos { return endOf(e.Rparen, 1) }

// NewExpr is a constructor invocation new Callee(args).
type NewExpr struct {
	Keyword   token.Pos
	Callee    Expr
	Arguments []Expr
	EndPos    token.Pos
}

func (e *NewExpr) Pos() token.Pos { return e.Keyword }
func (e *NewExpr) End() token.Pos { return e.EndPos }

// ImportCall is a dynamic import() expression (ECMA-262 ES2020):
// import(Specifier) or import(Specifier, Options). It evaluates to a Promise
// for the imported module's namespace object. It is distinct from an import
// declaration and from the import.meta meta-property.
type ImportCall struct {
	Keyword   token.Pos
	Specifier Expr
	Options   Expr // the optional second argument; nil when absent
	Rparen    token.Pos
}

func (e *ImportCall) Pos() token.Pos { return e.Keyword }
func (e *ImportCall) End() token.Pos { return endOf(e.Rparen, 1) }

// ---------------------------------------------------------------------------
// Function expressions
// ---------------------------------------------------------------------------

// FuncExpr is a function expression: function (params) { body } or a named
// function expression. It shares its shape with function declarations via the
// embedded [FuncDef].
type FuncExpr struct {
	Keyword token.Pos
	Def     *FuncDef
}

func (e *FuncExpr) Pos() token.Pos { return e.Keyword }
func (e *FuncExpr) End() token.Pos { return e.Def.Body.End() }

// ArrowFunc is an arrow function (params) => body. Body is either a
// [*BlockStmt] or an expression (when Expression is true).
type ArrowFunc struct {
	Start      token.Pos
	Params     []Expr // Ident, patterns, or *RestElement
	Body       Node   // *BlockStmt or Expr
	Async      bool
	Expression bool // true when Body is an expression (concise body)
	Strict     bool // strict-mode code (own directive or lexically nested in strict code)
	// Source is the exact source text of the arrow function, returned by
	// Function.prototype.toString ([[SourceText]], §20.2.3.5).
	Source string
}

func (e *ArrowFunc) Pos() token.Pos { return e.Start }
func (e *ArrowFunc) End() token.Pos { return e.Body.End() }

// ClassExpr is a class expression (see [ClassDef]).
type ClassExpr struct {
	Keyword token.Pos
	Def     *ClassDef
}

func (e *ClassExpr) Pos() token.Pos { return e.Keyword }
func (e *ClassExpr) End() token.Pos { return e.Def.Rbrace }

// YieldExpr is a yield expression inside a generator. Delegate marks yield*.
type YieldExpr struct {
	Keyword  token.Pos
	Argument Expr // may be nil
	Delegate bool
}

func (e *YieldExpr) Pos() token.Pos { return e.Keyword }
func (e *YieldExpr) End() token.Pos {
	if e.Argument != nil {
		return e.Argument.End()
	}
	return endOf(e.Keyword, 5)
}

// AwaitExpr is an await expression inside an async function.
type AwaitExpr struct {
	Keyword  token.Pos
	Argument Expr
}

func (e *AwaitExpr) Pos() token.Pos { return e.Keyword }
func (e *AwaitExpr) End() token.Pos { return e.Argument.End() }

// ---------------------------------------------------------------------------
// Destructuring patterns
// ---------------------------------------------------------------------------

// RestElement is a rest binding ...target used in parameter lists and
// destructuring patterns.
type RestElement struct {
	Ellipsis token.Pos
	Target   Expr
}

func (e *RestElement) Pos() token.Pos { return e.Ellipsis }
func (e *RestElement) End() token.Pos { return e.Target.End() }

// AssignPattern is a binding with a default value, target = default, used in
// parameter lists and destructuring patterns.
type AssignPattern struct {
	Target  Expr
	Default Expr
}

func (e *AssignPattern) Pos() token.Pos { return e.Target.Pos() }
func (e *AssignPattern) End() token.Pos { return e.Default.End() }

// ---------------------------------------------------------------------------
// exprNode markers
// ---------------------------------------------------------------------------

func (*ImportCall) exprNode()         {}
func (*Ident) exprNode()              {}
func (*PrivateIdent) exprNode()       {}
func (*NumberLit) exprNode()          {}
func (*BigIntLit) exprNode()          {}
func (*StringLit) exprNode()          {}
func (*BoolLit) exprNode()            {}
func (*NullLit) exprNode()            {}
func (*ThisExpr) exprNode()           {}
func (*SuperExpr) exprNode()          {}
func (*TemplateLit) exprNode()        {}
func (*TaggedTemplateExpr) exprNode() {}
func (*RegexLit) exprNode()           {}
func (*ArrayLit) exprNode()           {}
func (*ObjectLit) exprNode()          {}
func (*SpreadElement) exprNode()      {}
func (*UnaryExpr) exprNode()          {}
func (*UpdateExpr) exprNode()         {}
func (*BinaryExpr) exprNode()         {}
func (*LogicalExpr) exprNode()        {}
func (*AssignExpr) exprNode()         {}
func (*ConditionalExpr) exprNode()    {}
func (*SequenceExpr) exprNode()       {}
func (*MemberExpr) exprNode()         {}
func (*CallExpr) exprNode()           {}
func (*NewExpr) exprNode()            {}
func (*FuncExpr) exprNode()           {}
func (*ArrowFunc) exprNode()          {}
func (*ClassExpr) exprNode()          {}
func (*YieldExpr) exprNode()          {}
func (*AwaitExpr) exprNode()          {}
func (*RestElement) exprNode()        {}
func (*AssignPattern) exprNode()      {}

// endOf returns a position offset by n columns/bytes from start. It is a coarse
// approximation used by leaf nodes whose text does not span lines.
func endOf(start token.Pos, n int) token.Pos {
	end := start
	end.Offset += n
	end.Column += n
	return end
}
