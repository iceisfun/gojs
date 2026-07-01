// Package ast defines the abstract syntax tree (AST) node types produced by
// the parser and consumed by the interpreter.
//
// The tree is organized into two interface hierarchies — [Expr] for
// expressions and [Stmt] for statements — both extending the common [Node]
// interface. Every node carries the source position of its first token via
// Pos, and the position just past its last token via End, enabling precise
// error reporting.
//
// The shape follows the ESTree specification loosely, adapted to idiomatic Go:
// concrete struct types with exported fields, small marker methods to seal the
// interfaces, and no visitor indirection (the interpreter type-switches).
//
// ECMA-262 Reference: §13–§16 (Expressions, Statements, Functions, Programs).
package ast

import "github.com/iceisfun/gojs/token"

// Node is the interface implemented by every AST node.
type Node interface {
	// Pos returns the position of the node's first token.
	Pos() token.Pos
	// End returns the position immediately after the node's last token.
	End() token.Pos
}

// Expr is the interface implemented by all expression nodes.
type Expr interface {
	Node
	exprNode()
}

// Stmt is the interface implemented by all statement nodes.
type Stmt interface {
	Node
	stmtNode()
}

// Program is the root node of a parsed source file (a Script goal symbol).
// It holds the top-level statement list plus source metadata.
type Program struct {
	Source string // source name (filename or "<eval>")
	Body   []Stmt // top-level statements
	// Strict reports whether the program body began with a "use strict"
	// directive prologue.
	Strict bool
	// EndPos is the position of the trailing EOF token.
	EndPos token.Pos
}

// Pos returns the start of the program (position of the first statement, or the
// EOF position for an empty program).
func (p *Program) Pos() token.Pos {
	if len(p.Body) > 0 {
		return p.Body[0].Pos()
	}
	return p.EndPos
}

// End returns the end of the program.
func (p *Program) End() token.Pos { return p.EndPos }
