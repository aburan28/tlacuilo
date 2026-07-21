// Command diehard builds the classic Die Hard water-jug puzzle as a TLA+
// spec with the tlacuilo builder, prints it, and — when java and
// tla2tools.jar are available — asks TLC to solve the puzzle by violating
// the "not solved yet" invariant, then prints the solution trace and its
// ITF JSON form.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aburan28/tlacuilo/ast"
	b "github.com/aburan28/tlacuilo/builder"
	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/tlc"
)

func dieHard() *b.Module {
	big, small := b.ID("big"), b.ID("small")
	bigP, smallP := b.Prime(big), b.Prime(small)
	min := func(x, y ast.Expr) ast.Expr { return b.IfThenElse(b.Lt(x, y), x, y) }

	m := b.NewModule("DieHard").Extends("Naturals")
	m.Variables("big", "small")
	m.Define("TypeOK", b.And(
		b.In(big, b.Range(b.Num(0), b.Num(5))),
		b.In(small, b.Range(b.Num(0), b.Num(3))),
	))
	m.Define("Init", b.And(b.Eq(big, b.Num(0)), b.Eq(small, b.Num(0))))
	m.Define("FillBig", b.And(b.Eq(bigP, b.Num(5)), b.Eq(smallP, small)))
	m.Define("FillSmall", b.And(b.Eq(smallP, b.Num(3)), b.Eq(bigP, big)))
	m.Define("EmptyBig", b.And(b.Eq(bigP, b.Num(0)), b.Eq(smallP, small)))
	m.Define("EmptySmall", b.And(b.Eq(smallP, b.Num(0)), b.Eq(bigP, big)))
	poured := b.ID("poured")
	m.Define("BigToSmall", &ast.Let{
		Defs: []ast.Unit{&ast.OperatorDef{Name: "poured", Body: min(big, b.Minus(b.Num(3), small))}},
		Body: b.And(b.Eq(bigP, b.Minus(big, poured)), b.Eq(smallP, b.Plus(small, poured))),
	})
	m.Define("SmallToBig", &ast.Let{
		Defs: []ast.Unit{&ast.OperatorDef{Name: "poured", Body: min(small, b.Minus(b.Num(5), big))}},
		Body: b.And(b.Eq(smallP, b.Minus(small, poured)), b.Eq(bigP, b.Plus(big, poured))),
	})
	m.Define("Next", b.Or(
		b.ID("FillBig"), b.ID("FillSmall"), b.ID("EmptyBig"),
		b.ID("EmptySmall"), b.ID("BigToSmall"), b.ID("SmallToBig"),
	))
	m.Define("vars", b.TupleOf(big, small))
	m.Define("Spec", b.Spec(b.ID("Init"), b.ID("Next"), b.ID("vars")))
	m.Define("NotSolved", b.Neq(big, b.Num(4)))
	return m
}

func main() {
	m := dieHard()
	fmt.Println(m)

	if _, err := tlc.FindJar(); err != nil {
		fmt.Fprintln(os.Stderr, "skipping model check:", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, err := tlc.Check(ctx, tlc.Job{
		Module: m.AST(),
		Config: &cfg.Config{
			Specification: "Spec",
			Invariants:    []string{"TypeOK", "NotSolved"},
		},
	}, tlc.Options{DisableDeadlockCheck: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tlc:", err)
		os.Exit(1)
	}
	fmt.Printf("TLC finished: %s (%d states, %d distinct)\n\n",
		r.Status, r.StatesGenerated, r.DistinctStates)
	if r.Trace != nil {
		fmt.Println("Solution (the trace violating NotSolved):")
		fmt.Println(r.Trace)
		itf, err := r.Trace.MarshalITF()
		if err == nil {
			fmt.Println("As ITF JSON:")
			fmt.Println(string(itf))
		}
	}
}
