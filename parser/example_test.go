package parser_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/ast"
	"github.com/aburan28/tlacuilo/parser"
)

func ExampleParse() {
	src := `---- MODULE Hour ----
EXTENDS Naturals
VARIABLE h
Init == h = 0
Next == h' = (h % 12) + 1
====
`
	m, err := parser.Parse(src)
	if err != nil {
		panic(err)
	}
	fmt.Println(m.Name, m.Extends)
	// Output:
	// Hour [Naturals]
}

func ExampleParseExpr() {
	e, err := parser.ParseExpr(`[f EXCEPT ![k] = @ + 1]`)
	if err != nil {
		panic(err)
	}
	fmt.Println(ast.ExprString(e))

	// Precedence conflicts are rejected, as SANY rejects them.
	_, err = parser.ParseExpr(`a \cup b \cap c`)
	fmt.Println(err)
	// Output:
	// [f EXCEPT ![k] = @ + 1]
	// 1:10: operator \cap conflicts with preceding operator of overlapping precedence; add parentheses
}
