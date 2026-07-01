package ast

import (
	"strings"
	"testing"

	"github.com/iceisfun/gojs/token"
)

func TestDump(t *testing.T) {
	prog := &Program{
		Source: "t",
		Body: []Stmt{
			&ExprStmt{X: &BinaryExpr{
				Left:  &NumberLit{Value: 1, Raw: "1"},
				Op:    token.PLUS,
				Right: &NumberLit{Value: 2, Raw: "2"},
			}},
		},
	}
	out := Dump(prog)
	for _, want := range []string{"Program", "ExprStmt", "BinaryExpr", "Op: +", "NumberLit"} {
		if !strings.Contains(out, want) {
			t.Errorf("dump missing %q\n%s", want, out)
		}
	}
}
