package builder_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/ast"
	b "github.com/aburan28/tlacuilo/builder"
)

func ExampleNewModule() {
	x := b.ID("x")
	m := b.NewModule("Counter").Extends("Naturals")
	m.Variables("x")
	m.Define("Init", b.Eq(x, b.Num(0)))
	m.Define("Next", b.Eq(b.Prime(x), b.Mod(b.Plus(x, b.Num(1)), b.Num(4))))
	m.Define("Spec", b.Spec(b.ID("Init"), b.ID("Next"), x, b.WF(x, b.ID("Next"))))
	m.Define("TypeOK", b.In(x, b.Range(b.Num(0), b.Num(3))))
	fmt.Print(m)
	// Output:
	// ------------------------------ MODULE Counter -------------------------------
	// EXTENDS Naturals
	//
	// VARIABLE x
	//
	// Init == x = 0
	//
	// Next == x' = (x + 1) % 4
	//
	// Spec == /\ Init
	//         /\ [][Next]_x
	//         /\ WF_x(Next)
	//
	// TypeOK == x \in 0..3
	//
	// =============================================================================
}

func ExampleSpec() {
	vars := b.ID("vars")
	fmt.Println(ast.ExprString(b.Spec(b.ID("Init"), b.ID("Next"), vars, b.WF(vars, b.ID("Next")))))
	// Output:
	// /\ Init
	// /\ [][Next]_vars
	// /\ WF_vars(Next)
}
