package ast_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/ast"
)

func ExampleExprString() {
	// AST nodes can be built directly; the printer inserts parentheses
	// only where precedence requires them.
	e := &ast.Binary{
		Op: "*",
		L:  &ast.Paren{X: &ast.Binary{Op: "+", L: &ast.Ident{Name: "a"}, R: &ast.Ident{Name: "b"}}},
		R:  &ast.NumberLit{Lit: "2"},
	}
	fmt.Println(ast.ExprString(e))

	j := &ast.Junction{Op: "/\\", Items: []ast.Expr{
		&ast.Ident{Name: "Init"},
		&ast.SquareAct{X: &ast.Ident{Name: "Next"}, Sub: &ast.Ident{Name: "vars"}},
	}}
	fmt.Println(ast.ExprString(j))
	// Output:
	// (a + b) * 2
	// /\ Init
	// /\ [Next]_vars
}
