package parser

import (
	"strings"
	"testing"

	"github.com/aburan28/tlacuilo/ast"
)

// roundTripExpr checks parse → print → parse → print is a fixed point.
func roundTripExpr(t *testing.T, src string) string {
	t.Helper()
	e, err := ParseExpr(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	out1 := ast.ExprString(e)
	e2, err := ParseExpr(out1)
	if err != nil {
		t.Fatalf("reparse %q (printed from %q): %v", out1, src, err)
	}
	out2 := ast.ExprString(e2)
	if out1 != out2 {
		t.Fatalf("print not stable for %q:\n first: %s\nsecond: %s", src, out1, out2)
	}
	return out1
}

// expectPrint parses src and requires the canonical form to equal want.
func expectPrint(t *testing.T, src, want string) {
	t.Helper()
	got := roundTripExpr(t, src)
	if got != want {
		t.Errorf("parse %q:\n got: %s\nwant: %s", src, got, want)
	}
}

func TestExprPrecedence(t *testing.T) {
	expectPrint(t, "1 + 2 * 3", "1 + 2 * 3")
	expectPrint(t, "(1 + 2) * 3", "(1 + 2) * 3")
	expectPrint(t, "a + b - c", "a + b - c") // '-' binds tighter: a + (b - c)
	expectPrint(t, "a - b - c", "a - b - c") // left-assoc chain
	expectPrint(t, "x = 1 /\\ y = 2", "x = 1 /\\ y = 2")
	expectPrint(t, "(a /\\ b) \\/ c", "(a /\\ b) \\/ c")
	expectPrint(t, "~x \\in S", "~x \\in S")       // ~ (4) absorbs \in (5)
	expectPrint(t, "a => b <=> c", "a => b <=> c") // <=> (2) binds tighter than => (1)
	expectPrint(t, "-a * b", "-a * b")             // unary minus (12) absorbs * (13)
	expectPrint(t, "-(a + b)", "-(a + b)")
	expectPrint(t, "x'", "x'")
	expectPrint(t, "(a + b)'", "(a + b)'")
	expectPrint(t, "f[x]'", "f[x]'")
	expectPrint(t, "a.b.c", "a.b.c")
	expectPrint(t, "f[x][y]", "f[x][y]")
	expectPrint(t, "S \\cup T \\cup U", "S \\cup T \\cup U")
	expectPrint(t, "S \\cup (T \\cap U)", "S \\cup (T \\cap U)")
	expectPrint(t, "1..N", "1..N")
	expectPrint(t, "s \\o t", "s \\o t")
	expectPrint(t, "S \\X T \\X U", "S \\X T \\X U")
	expectPrint(t, "(S \\X T) \\X U", "(S \\X T) \\X U")
	expectPrint(t, "DOMAIN f \\cup S", "DOMAIN f \\cup S") // DOMAIN (9) > \cup (8)
	expectPrint(t, "SUBSET (S \\cup T)", "SUBSET (S \\cup T)")
}

func TestExprConflictsRejected(t *testing.T) {
	for _, src := range []string{
		"a = b = c",
		"a \\cup b \\cap c",
		"a < b <= c",
		"x \\in S \\in T",
		"a /\\ b \\/ c", // /\ and \/ share level 3 and may not mix inline
	} {
		if _, err := ParseExpr(src); err == nil {
			t.Errorf("expected precedence-conflict error for %q", src)
		}
	}
}

func TestExprConstructs(t *testing.T) {
	expectPrint(t, "{1, 2, 3}", "{1, 2, 3}")
	expectPrint(t, "{}", "{}")
	expectPrint(t, "{x \\in S : x > 0}", "{x \\in S : x > 0}")
	expectPrint(t, "{x * x : x \\in S}", "{x * x : x \\in S}")
	expectPrint(t, "{<<a, b>> \\in S \\X T : a < b}", "{<<a, b>> \\in S \\X T : a < b}")
	expectPrint(t, "<<1, \"two\", TRUE>>", "<<1, \"two\", TRUE>>")
	expectPrint(t, "<<>>", "<<>>")
	expectPrint(t, "[x \\in S |-> x + 1]", "[x \\in S |-> x + 1]")
	expectPrint(t, "[x \\in S, y \\in T |-> x + y]", "[x \\in S, y \\in T |-> x + y]")
	expectPrint(t, "[<<x, y>> \\in S \\X T |-> x]", "[<<x, y>> \\in S \\X T |-> x]")
	expectPrint(t, "[S -> T]", "[S -> T]")
	expectPrint(t, "[a |-> 1, b |-> \"x\"]", "[a |-> 1, b |-> \"x\"]")
	expectPrint(t, "[a : Nat, b : STRING]", "[a : Nat, b : STRING]")
	expectPrint(t, "r.field", "r.field")
	expectPrint(t, "[f EXCEPT ![1] = 2]", "[f EXCEPT ![1] = 2]")
	expectPrint(t, "[f EXCEPT ![i].a = @ + 1, !.b = 0]", "[f EXCEPT ![i].a = @ + 1, !.b = 0]")
	expectPrint(t, "[<<1, 2>> EXCEPT ![1] = 0]", "[<<1, 2>> EXCEPT ![1] = 0]")
	expectPrint(t, "IF x > 0 THEN \"pos\" ELSE \"neg\"", "IF x > 0 THEN \"pos\" ELSE \"neg\"")
	expectPrint(t, "CASE a -> 1 [] b -> 2 [] OTHER -> 3", "CASE a -> 1 [] b -> 2 [] OTHER -> 3")
	expectPrint(t, "LET sq(a) == a * a IN sq(3)", "LET sq(a) == a * a\nIN  sq(3)")
	expectPrint(t, "\\A x \\in S : x > 0", "\\A x \\in S : x > 0")
	expectPrint(t, "\\E x, y \\in S : x /= y", "\\E x, y \\in S : x /= y")
	expectPrint(t, "\\A x \\in S, y \\in T : P(x, y)", "\\A x \\in S, y \\in T : P(x, y)")
	expectPrint(t, "\\A x : TRUE", "\\A x : TRUE")
	expectPrint(t, "\\EE x : F(x)", "\\EE x : F(x)")
	expectPrint(t, "CHOOSE x \\in S : x > 0", "CHOOSE x \\in S : x > 0")
	expectPrint(t, "CHOOSE x : x \\notin S", "CHOOSE x : x \\notin S")
	expectPrint(t, "Cardinality({1, 2})", "Cardinality({1, 2})")
	expectPrint(t, "M!Op(x)", "M!Op(x)")
	expectPrint(t, "A!B!C", "A!B!C")
	expectPrint(t, "Foo(LAMBDA a, b : a + b, S)", "Foo(LAMBDA a, b : a + b, S)")
	expectPrint(t, "UNCHANGED <<x, y>>", "UNCHANGED <<x, y>>")
	expectPrint(t, "\\b0101 + \\hFF", "\\b0101 + \\hFF")
}

func TestActionAndTemporal(t *testing.T) {
	expectPrint(t, "x' = x + 1", "x' = x + 1")
	expectPrint(t, "[][Next]_vars", "[][Next]_vars")
	expectPrint(t, "<><<Next>>_vars", "<><<Next>>_vars")
	expectPrint(t, "[Next]_<<x, y>>", "[Next]_<<x, y>>")
	expectPrint(t, "WF_vars(Next)", "WF_vars(Next)")
	expectPrint(t, "SF_<<x, y>>(A \\/ B)", "SF_<<x, y>>(A \\/ B)")
	expectPrint(t, "[]P => <>Q", "[]P => <>Q")
	expectPrint(t, "P ~> Q", "P ~> Q")
	expectPrint(t, "ENABLED Next", "ENABLED Next")
	expectPrint(t, "Init /\\ [][Next]_vars /\\ WF_vars(Next)",
		"Init /\\ [][Next]_vars /\\ WF_vars(Next)")
}

func TestJunctionLists(t *testing.T) {
	src := "/\\ x = 1\n/\\ y = 2\n/\\ z = 3"
	e, err := ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	j, ok := e.(*ast.Junction)
	if !ok {
		t.Fatalf("got %T, want Junction", e)
	}
	if len(j.Items) != 3 {
		t.Fatalf("got %d items, want 3", len(j.Items))
	}
	roundTripExpr(t, src)

	// Nested junctions grouped by column.
	src = "/\\ \\/ a\n   \\/ b\n/\\ c"
	e, err = ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	j = e.(*ast.Junction)
	if len(j.Items) != 2 {
		t.Fatalf("outer: got %d items, want 2", len(j.Items))
	}
	inner, ok := j.Items[0].(*ast.Junction)
	if !ok || len(inner.Items) != 2 || inner.Op != "\\/" {
		t.Fatalf("inner junction wrong: %#v", j.Items[0])
	}
	roundTripExpr(t, src)

	// Multi-line item: the expression continues while indented deeper.
	src = "/\\ x =\n     1\n/\\ y = 2"
	e, err = ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.(*ast.Junction).Items) != 2 {
		t.Fatal("continuation line should stay in the first item")
	}

	// A dedented operator ends the junction list.
	src = "/\\ a\n/\\ b\n=> c"
	e, err = ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := e.(*ast.Binary)
	if !ok || b.Op != "=>" {
		t.Fatalf("dedented => should apply to the whole list, got %#v", e)
	}
}

func TestModuleParse(t *testing.T) {
	src := `---- MODULE Counter ----
EXTENDS Naturals, Sequences

CONSTANT N
VARIABLES x, hist

ASSUME NAssump == N \in Nat

Init == /\ x = 0
        /\ hist = <<>>

Next == /\ x < N
        /\ x' = x + 1
        /\ hist' = Append(hist, x)

vars == <<x, hist>>

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

TypeOK == /\ x \in 0..N
          /\ hist \in Seq(Nat)

THEOREM Safety == Spec => []TypeOK
====
`
	m, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "Counter" {
		t.Errorf("name = %q", m.Name)
	}
	if len(m.Extends) != 2 {
		t.Errorf("extends = %v", m.Extends)
	}
	if len(m.Units) != 9 {
		t.Errorf("got %d units, want 9", len(m.Units))
	}
	// Round-trip.
	out1 := m.String()
	m2, err := Parse(out1)
	if err != nil {
		t.Fatalf("reparse failed: %v\n%s", err, out1)
	}
	out2 := m2.String()
	if out1 != out2 {
		t.Errorf("module print not stable:\n%s\n----\n%s", out1, out2)
	}
}

func TestModuleUnits(t *testing.T) {
	src := `---- MODULE Kitchen ----
CONSTANTS Foo, Bar(_, _)
VARIABLE s

LOCAL INSTANCE Naturals

M == INSTANCE Other WITH a <- s, b <- 1 + 2

max(a, b) == IF a > b THEN a ELSE b

a ++ b == <<a, b>>

f[i \in 1..10] == i * i

RECURSIVE fact(_)
fact(n) == IF n = 0 THEN 1 ELSE n * fact(n - 1)

----

LOCAL half(n) == n \div 2

ASSUME Foo \in {1, 2}
THEOREM TRUE
====
`
	m, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	out1 := m.String()
	m2, err := Parse(out1)
	if err != nil {
		t.Fatalf("reparse failed: %v\n%s", err, out1)
	}
	if out2 := m2.String(); out1 != out2 {
		t.Errorf("not stable:\n%s\n----\n%s", out1, out2)
	}
	var kinds []string
	for _, u := range m.Units {
		kinds = append(kinds, strings.TrimPrefix(strings.TrimPrefix(
			strings.Split(strings.TrimPrefix(
				fmtType(u), "*"), ".")[1], "ast."), "*"))
	}
	want := []string{"ConstantDecl", "VariableDecl", "Instance", "ModuleDef",
		"OperatorDef", "OperatorDef", "FunctionDef", "Recursive", "OperatorDef",
		"Separator", "OperatorDef", "Assume", "Theorem"}
	if len(kinds) != len(want) {
		t.Fatalf("units = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("unit %d = %s, want %s", i, kinds[i], want[i])
		}
	}
}

func fmtType(v any) string {
	switch v.(type) {
	case *ast.ConstantDecl:
		return "*ast.ConstantDecl"
	case *ast.VariableDecl:
		return "*ast.VariableDecl"
	case *ast.OperatorDef:
		return "*ast.OperatorDef"
	case *ast.FunctionDef:
		return "*ast.FunctionDef"
	case *ast.Instance:
		return "*ast.Instance"
	case *ast.ModuleDef:
		return "*ast.ModuleDef"
	case *ast.Assume:
		return "*ast.Assume"
	case *ast.Theorem:
		return "*ast.Theorem"
	case *ast.Recursive:
		return "*ast.Recursive"
	case *ast.Separator:
		return "*ast.Separator"
	case *ast.NestedModule:
		return "*ast.NestedModule"
	}
	return "?"
}

func TestNestedModule(t *testing.T) {
	src := `---- MODULE Outer ----
VARIABLE x
---- MODULE Inner ----
Zero == 0
====
Init == x = 0
====
`
	m, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range m.Units {
		if n, ok := u.(*ast.NestedModule); ok {
			found = true
			if n.Module.Name != "Inner" {
				t.Errorf("inner name = %q", n.Module.Name)
			}
		}
	}
	if !found {
		t.Error("nested module not found")
	}
}

func TestDedentedBulletEndsJunction(t *testing.T) {
	// An operator right of the bullet column stays inside the current
	// item: item 2 becomes (y = 2) => z.
	e, err := ParseExpr("/\\ x = 1\n/\\ y = 2\n  => z")
	if err != nil {
		t.Fatal(err)
	}
	j2, ok := e.(*ast.Junction)
	if !ok || len(j2.Items) != 2 {
		t.Fatalf("expected two-item junction, got %#v", e)
	}
	if b, ok := j2.Items[1].(*ast.Binary); !ok || b.Op != "=>" {
		t.Fatalf("indented => should continue item 2, got %#v", j2.Items[1])
	}

	src := "/\\ x = 1\n/\\ y = 2"
	j, err := ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.(*ast.Junction).Items) != 2 {
		t.Fatal("aligned bullets should form one two-item list")
	}
}

func TestParseErrorsHavePositions(t *testing.T) {
	_, err := ParseExpr("1 +")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), ":") {
		t.Errorf("error should carry position: %v", err)
	}
}
