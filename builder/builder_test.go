package builder_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aburan28/tlacuilo/ast"
	"github.com/aburan28/tlacuilo/builder"
	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/parser"
	"github.com/aburan28/tlacuilo/tlc"
	"github.com/aburan28/tlacuilo/value"
)

// dieHard builds the classic Die Hard water-jug puzzle as a spec: a
// 5-gallon and a 3-gallon jug; the invariant big /= 4 is violated
// exactly when the puzzle is solved.
func dieHard() *builder.Module {
	big, small := builder.ID("big"), builder.ID("small")
	bigP, smallP := builder.Prime(big), builder.Prime(small)
	min := func(a, b ast.Expr) ast.Expr {
		return builder.IfThenElse(builder.Lt(a, b), a, b)
	}

	m := builder.NewModule("DieHard").Extends("Naturals")
	m.Variables("big", "small")
	m.Define("TypeOK", builder.And(
		builder.In(big, builder.Range(builder.Num(0), builder.Num(5))),
		builder.In(small, builder.Range(builder.Num(0), builder.Num(3))),
	))
	m.Define("Init", builder.And(
		builder.Eq(big, builder.Num(0)),
		builder.Eq(small, builder.Num(0)),
	))
	m.Define("FillBig", builder.And(
		builder.Eq(bigP, builder.Num(5)),
		builder.Eq(smallP, small),
	))
	m.Define("FillSmall", builder.And(
		builder.Eq(smallP, builder.Num(3)),
		builder.Eq(bigP, big),
	))
	m.Define("EmptyBig", builder.And(
		builder.Eq(bigP, builder.Num(0)),
		builder.Eq(smallP, small),
	))
	m.Define("EmptySmall", builder.And(
		builder.Eq(smallP, builder.Num(0)),
		builder.Eq(bigP, big),
	))
	poured := builder.ID("poured")
	m.Define("BigToSmall", &ast.Let{
		Defs: []ast.Unit{&ast.OperatorDef{Name: "poured",
			Body: min(big, builder.Minus(builder.Num(3), small))}},
		Body: builder.And(
			builder.Eq(bigP, builder.Minus(big, poured)),
			builder.Eq(smallP, builder.Plus(small, poured)),
		),
	})
	m.Define("SmallToBig", &ast.Let{
		Defs: []ast.Unit{&ast.OperatorDef{Name: "poured",
			Body: min(small, builder.Minus(builder.Num(5), big))}},
		Body: builder.And(
			builder.Eq(smallP, builder.Minus(small, poured)),
			builder.Eq(bigP, builder.Plus(big, poured)),
		),
	})
	m.Define("Next", builder.Or(
		builder.ID("FillBig"), builder.ID("FillSmall"),
		builder.ID("EmptyBig"), builder.ID("EmptySmall"),
		builder.ID("BigToSmall"), builder.ID("SmallToBig"),
	))
	m.Define("vars", builder.TupleOf(big, small))
	m.Define("Spec", builder.Spec(builder.ID("Init"), builder.ID("Next"), builder.ID("vars")))
	m.Define("NotSolved", builder.Neq(big, builder.Num(4)))
	return m
}

func TestBuiltSpecParses(t *testing.T) {
	src := dieHard().String()
	m, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("built spec does not parse: %v\n%s", err, src)
	}
	if m.Name != "DieHard" {
		t.Errorf("name = %q", m.Name)
	}
	// The canonical form is stable.
	if again := m.String(); again != src {
		t.Errorf("built spec is not canonical:\n%s\n----\n%s", src, again)
	}
	for _, want := range []string{
		"Init == /\\ big = 0",
		"[][Next]_vars",
		"LET poured == IF big < 3 - small THEN big ELSE 3 - small",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

func TestBuilderConstructs(t *testing.T) {
	cases := []struct {
		e    ast.Expr
		want string
	}{
		{builder.Num(-3), "-3"},
		{builder.Unchanged("x", "y"), "UNCHANGED <<x, y>>"},
		{builder.Unchanged("x"), "UNCHANGED x"},
		{builder.WF(builder.ID("vars"), builder.ID("Next")), "WF_vars(Next)"},
		{builder.Forall(builder.Bind(builder.ID("S"), "x"), builder.Gt(builder.ID("x"), builder.Num(0))),
			"\\A x \\in S : x > 0"},
		{builder.Exists(builder.Bind(builder.ID("S"), "x", "y"), builder.Neq(builder.ID("x"), builder.ID("y"))),
			"\\E x, y \\in S : x /= y"},
		{builder.Fn(builder.Bind(builder.ID("S"), "x"), builder.Mul(builder.ID("x"), builder.ID("x"))),
			"[x \\in S |-> x * x]"},
		{builder.ExceptIdx(builder.ID("f"), builder.Num(1), builder.Plus(builder.AtOld(), builder.Num(1))),
			"[f EXCEPT ![1] = @ + 1]"},
		{builder.ExceptField(builder.ID("r"), "count", builder.Num(0)),
			"[r EXCEPT !.count = 0]"},
		{builder.Rec(builder.Field("a", builder.Num(1))), "[a |-> 1]"},
		{builder.LeadsTo(builder.ID("P"), builder.ID("Q")), "P ~> Q"},
		{builder.Eventually(builder.Eq(builder.ID("x"), builder.Num(3))), "<>(x = 3)"},
		{builder.Choose("x", builder.ID("S"), builder.True()), "CHOOSE x \\in S : TRUE"},
	}
	for _, c := range cases {
		got := ast.ExprString(c.e)
		if got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
		if _, err := parser.ParseExpr(got); err != nil {
			t.Errorf("built expr %q does not reparse: %v", got, err)
		}
	}
}

func TestIntegrationDieHardSolved(t *testing.T) {
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java not installed")
	}
	jar, err := tlc.FindJar()
	if err != nil {
		t.Skip("tla2tools.jar not found (set TLA2TOOLS_JAR to enable integration tests)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, err := tlc.Check(ctx, tlc.Job{
		Module: dieHard().AST(),
		Config: &cfg.Config{
			Specification: "Spec",
			Invariants:    []string{"TypeOK", "NotSolved"},
		},
	}, tlc.Options{JarPath: jar, DisableDeadlockCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != tlc.StatusSafetyViolation {
		t.Fatalf("expected the puzzle to be solvable, got %v (errors %v)", r.Status, r.Errors)
	}
	tr := r.Trace
	if tr == nil {
		t.Fatal("no solution trace")
	}
	last := tr.States[len(tr.States)-1]
	if !value.Equal(last.Vars["big"], value.NewInt(4)) {
		t.Errorf("final big = %v, want 4", last.Vars["big"])
	}
	// The classic solution takes 7 steps (6 pours after Init).
	if len(tr.States) != 7 {
		t.Errorf("solution length = %d states, expected 7", len(tr.States))
	}
}
