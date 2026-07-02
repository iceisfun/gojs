package ast

import "github.com/iceisfun/gojs/token"

// This file defines statement node types and the shared function/class
// definition structures. Every statement type implements [Stmt] via the
// stmtNode marker methods at the bottom of the file.

// ---------------------------------------------------------------------------
// Shared definitions
// ---------------------------------------------------------------------------

// FuncDef holds the shared shape of a function: its (optional) name, parameter
// list, and body. It is embedded by [FuncDecl], [FuncExpr], and object/class
// methods so the interpreter can treat all callable definitions uniformly.
type FuncDef struct {
	Name      *Ident // nil for anonymous function expressions
	Params    []Expr // Ident, patterns, AssignPattern, or *RestElement
	Body      *BlockStmt
	Async     bool
	Generator bool
	// Strict reports whether this function's body runs in strict mode, either
	// because it carries its own "use strict" directive prologue or because it
	// is lexically nested in strict code (a strict script, module, or function).
	Strict bool
}

// ClassDef holds the shared shape of a class declaration or expression.
type ClassDef struct {
	Name       *Ident // nil for anonymous class expressions
	SuperClass Expr   // nil when the class has no `extends` clause
	Members    []*ClassMember
	Lbrace     token.Pos
	Rbrace     token.Pos
}

// ClassMember is a single element of a class body: a method, accessor, or
// field.
type ClassMember struct {
	KeyPos   token.Pos
	Key      Expr         // Ident, StringLit, NumberLit, PrivateIdent, or computed
	Value    Expr         // *FuncExpr for methods, initializer Expr for fields
	Kind     PropertyKind // PropInit (field/method), PropGet, or PropSet
	Static   bool
	Computed bool
	// Field reports whether this member is a class field (as opposed to a
	// method or accessor).
	Field bool
}

func (m *ClassMember) Pos() token.Pos { return m.KeyPos }
func (m *ClassMember) End() token.Pos {
	if m.Value != nil {
		return m.Value.End()
	}
	return m.Key.End()
}

// ---------------------------------------------------------------------------
// Simple statements
// ---------------------------------------------------------------------------

// BlockStmt is a brace-delimited statement list { ... }.
type BlockStmt struct {
	Lbrace token.Pos
	Rbrace token.Pos
	Body   []Stmt
}

func (s *BlockStmt) Pos() token.Pos { return s.Lbrace }
func (s *BlockStmt) End() token.Pos { return endOf(s.Rbrace, 1) }

// EmptyStmt is a lone semicolon.
type EmptyStmt struct {
	Semicolon token.Pos
}

func (s *EmptyStmt) Pos() token.Pos { return s.Semicolon }
func (s *EmptyStmt) End() token.Pos { return endOf(s.Semicolon, 1) }

// ExprStmt is an expression used in statement position.
type ExprStmt struct {
	X Expr
	// Directive holds the string value when this statement is a directive
	// prologue entry such as "use strict"; otherwise it is empty.
	Directive string
}

func (s *ExprStmt) Pos() token.Pos { return s.X.Pos() }
func (s *ExprStmt) End() token.Pos { return s.X.End() }

// VarDecl is a variable declaration: var/let/const with one or more
// declarators.
type VarDecl struct {
	Keyword token.Pos
	Kind    token.Type // token.VAR, token.LET, or token.CONST
	Decls   []*VarDeclarator
	EndPos  token.Pos
}

func (s *VarDecl) Pos() token.Pos { return s.Keyword }
func (s *VarDecl) End() token.Pos { return s.EndPos }

// VarDeclarator is a single name = init binding within a [VarDecl].
type VarDeclarator struct {
	Target Expr // Ident or a destructuring pattern (ArrayLit/ObjectLit)
	Init   Expr // may be nil
}

func (d *VarDeclarator) Pos() token.Pos { return d.Target.Pos() }
func (d *VarDeclarator) End() token.Pos {
	if d.Init != nil {
		return d.Init.End()
	}
	return d.Target.End()
}

// FuncDecl is a function declaration statement.
type FuncDecl struct {
	Keyword token.Pos
	Def     *FuncDef
}

func (s *FuncDecl) Pos() token.Pos { return s.Keyword }
func (s *FuncDecl) End() token.Pos { return s.Def.Body.End() }

// ClassDecl is a class declaration statement.
type ClassDecl struct {
	Keyword token.Pos
	Def     *ClassDef
}

func (s *ClassDecl) Pos() token.Pos { return s.Keyword }
func (s *ClassDecl) End() token.Pos { return endOf(s.Def.Rbrace, 1) }

// ---------------------------------------------------------------------------
// Control-flow statements
// ---------------------------------------------------------------------------

// IfStmt is an if/else statement. Alternate is nil when there is no else.
type IfStmt struct {
	Keyword    token.Pos
	Test       Expr
	Consequent Stmt
	Alternate  Stmt
}

func (s *IfStmt) Pos() token.Pos { return s.Keyword }
func (s *IfStmt) End() token.Pos {
	if s.Alternate != nil {
		return s.Alternate.End()
	}
	return s.Consequent.End()
}

// ForStmt is a C-style for loop: for (init; test; update) body. Any of Init,
// Test, Update may be nil. Init is either a [*VarDecl] or an [Expr].
type ForStmt struct {
	Keyword token.Pos
	Init    Node // *VarDecl, Expr, or nil
	Test    Expr
	Update  Expr
	Body    Stmt
}

func (s *ForStmt) Pos() token.Pos { return s.Keyword }
func (s *ForStmt) End() token.Pos { return s.Body.End() }

// ForInStmt is a for-in or for-of loop. Of distinguishes the two. Left is a
// [*VarDecl] or an assignment target [Expr].
type ForInStmt struct {
	Keyword token.Pos
	Left    Node // *VarDecl or Expr
	Right   Expr
	Body    Stmt
	Of      bool // true for for-of, false for for-in
	Await   bool // true for `for await (... of ...)`
}

func (s *ForInStmt) Pos() token.Pos { return s.Keyword }
func (s *ForInStmt) End() token.Pos { return s.Body.End() }

// WhileStmt is a while loop.
type WhileStmt struct {
	Keyword token.Pos
	Test    Expr
	Body    Stmt
}

func (s *WhileStmt) Pos() token.Pos { return s.Keyword }
func (s *WhileStmt) End() token.Pos { return s.Body.End() }

// WithStmt is a `with (Object) Body` statement (legacy, sloppy-mode only).
type WithStmt struct {
	Keyword token.Pos
	Object  Expr
	Body    Stmt
}

func (s *WithStmt) Pos() token.Pos { return s.Keyword }
func (s *WithStmt) End() token.Pos { return s.Body.End() }

// DoWhileStmt is a do/while loop.
type DoWhileStmt struct {
	Keyword token.Pos
	Body    Stmt
	Test    Expr
	EndPos  token.Pos
}

func (s *DoWhileStmt) Pos() token.Pos { return s.Keyword }
func (s *DoWhileStmt) End() token.Pos { return s.EndPos }

// ReturnStmt is a return statement. Argument is nil for a bare return.
type ReturnStmt struct {
	Keyword  token.Pos
	Argument Expr
	EndPos   token.Pos
}

func (s *ReturnStmt) Pos() token.Pos { return s.Keyword }
func (s *ReturnStmt) End() token.Pos { return s.EndPos }

// BreakStmt is a break statement, optionally with a label.
type BreakStmt struct {
	Keyword token.Pos
	Label   *Ident // nil for an unlabeled break
	EndPos  token.Pos
}

func (s *BreakStmt) Pos() token.Pos { return s.Keyword }
func (s *BreakStmt) End() token.Pos { return s.EndPos }

// ContinueStmt is a continue statement, optionally with a label.
type ContinueStmt struct {
	Keyword token.Pos
	Label   *Ident // nil for an unlabeled continue
	EndPos  token.Pos
}

func (s *ContinueStmt) Pos() token.Pos { return s.Keyword }
func (s *ContinueStmt) End() token.Pos { return s.EndPos }

// ThrowStmt is a throw statement.
type ThrowStmt struct {
	Keyword  token.Pos
	Argument Expr
	EndPos   token.Pos
}

func (s *ThrowStmt) Pos() token.Pos { return s.Keyword }
func (s *ThrowStmt) End() token.Pos { return s.EndPos }

// TryStmt is a try/catch/finally statement. Handler and Finalizer may be nil,
// but at least one is always present.
type TryStmt struct {
	Keyword   token.Pos
	Block     *BlockStmt
	Handler   *CatchClause // nil when there is no catch
	Finalizer *BlockStmt   // nil when there is no finally
}

func (s *TryStmt) Pos() token.Pos { return s.Keyword }
func (s *TryStmt) End() token.Pos {
	if s.Finalizer != nil {
		return s.Finalizer.End()
	}
	return s.Handler.Body.End()
}

// CatchClause is the catch (param) { ... } part of a try statement. Param is
// nil for an optional-catch-binding form (catch { ... }).
type CatchClause struct {
	Keyword token.Pos
	Param   Expr // Ident or destructuring pattern, may be nil
	Body    *BlockStmt
}

// SwitchStmt is a switch statement.
type SwitchStmt struct {
	Keyword      token.Pos
	Discriminant Expr
	Cases        []*SwitchCase
	Rbrace       token.Pos
}

func (s *SwitchStmt) Pos() token.Pos { return s.Keyword }
func (s *SwitchStmt) End() token.Pos { return endOf(s.Rbrace, 1) }

// SwitchCase is one case/default clause. Test is nil for the default clause.
type SwitchCase struct {
	CasePos token.Pos
	Test    Expr // nil for `default:`
	Body    []Stmt
}

// LabeledStmt is a labeled statement label: stmt.
type LabeledStmt struct {
	Label *Ident
	Body  Stmt
}

func (s *LabeledStmt) Pos() token.Pos { return s.Label.Pos() }
func (s *LabeledStmt) End() token.Pos { return s.Body.End() }

// DebuggerStmt is a debugger statement.
type DebuggerStmt struct {
	Keyword token.Pos
	EndPos  token.Pos
}

func (s *DebuggerStmt) Pos() token.Pos { return s.Keyword }
func (s *DebuggerStmt) End() token.Pos { return s.EndPos }

// ---------------------------------------------------------------------------
// stmtNode markers
// ---------------------------------------------------------------------------

func (*BlockStmt) stmtNode()    {}
func (*EmptyStmt) stmtNode()    {}
func (*ExprStmt) stmtNode()     {}
func (*VarDecl) stmtNode()      {}
func (*FuncDecl) stmtNode()     {}
func (*ClassDecl) stmtNode()    {}
func (*IfStmt) stmtNode()       {}
func (*ForStmt) stmtNode()      {}
func (*ForInStmt) stmtNode()    {}
func (*WhileStmt) stmtNode()    {}
func (*WithStmt) stmtNode()     {}
func (*DoWhileStmt) stmtNode()  {}
func (*ReturnStmt) stmtNode()   {}
func (*BreakStmt) stmtNode()    {}
func (*ContinueStmt) stmtNode() {}
func (*ThrowStmt) stmtNode()    {}
func (*TryStmt) stmtNode()      {}
func (*SwitchStmt) stmtNode()   {}
func (*LabeledStmt) stmtNode()  {}
func (*DebuggerStmt) stmtNode() {}
