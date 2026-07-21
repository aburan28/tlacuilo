// Package builder provides a fluent API for constructing TLA+
// specifications programmatically. It is a thin, misuse-resistant layer
// over package ast: everything it produces renders as formatted,
// parseable TLA+ source.
//
//	m := builder.NewModule("Counter").Extends("Naturals")
//	m.Variables("x")
//	m.Define("Init", builder.Eq(builder.ID("x"), builder.Num(0)))
//	m.Define("Next", builder.Eq(builder.Prime(builder.ID("x")),
//	    builder.Plus(builder.ID("x"), builder.Num(1))))
//	m.Define("Spec", builder.And(
//	    builder.ID("Init"),
//	    builder.Always(builder.BoxAction(builder.ID("Next"), builder.ID("x"))),
//	))
//	src := m.String()
package builder

import (
	"fmt"

	"github.com/aburan28/tlacuilo/ast"
)

// Module accumulates module units.
type Module struct {
	m *ast.Module
}

// NewModule starts a module.
func NewModule(name string) *Module {
	return &Module{m: &ast.Module{Name: name}}
}

// Extends adds EXTENDS entries.
func (b *Module) Extends(names ...string) *Module {
	b.m.Extends = append(b.m.Extends, names...)
	return b
}

// Constants declares constants.
func (b *Module) Constants(names ...string) *Module {
	ds := make([]ast.OpDecl, len(names))
	for i, n := range names {
		ds[i] = ast.OpDecl{Name: n}
	}
	b.m.Units = append(b.m.Units, &ast.ConstantDecl{Decls: ds})
	return b
}

// Variables declares variables.
func (b *Module) Variables(names ...string) *Module {
	b.m.Units = append(b.m.Units, &ast.VariableDecl{Names: append([]string(nil), names...)})
	return b
}

// Define adds a zero-parameter operator definition.
func (b *Module) Define(name string, body ast.Expr) *Module {
	b.m.Units = append(b.m.Units, &ast.OperatorDef{Name: name, Body: body})
	return b
}

// DefineOp adds an operator definition with named parameters.
func (b *Module) DefineOp(name string, params []string, body ast.Expr) *Module {
	ds := make([]ast.OpDecl, len(params))
	for i, p := range params {
		ds[i] = ast.OpDecl{Name: p}
	}
	b.m.Units = append(b.m.Units, &ast.OperatorDef{Name: name, Params: ds, Body: body})
	return b
}

// DefineFn adds a function definition f[x \in S] == body.
func (b *Module) DefineFn(name string, bound ast.Bound, body ast.Expr) *Module {
	b.m.Units = append(b.m.Units, &ast.FunctionDef{Name: name,
		Bounds: []ast.Bound{bound}, Body: body})
	return b
}

// Assume adds an ASSUME unit.
func (b *Module) Assume(e ast.Expr) *Module {
	b.m.Units = append(b.m.Units, &ast.Assume{Keyword: "ASSUME", Expr: e})
	return b
}

// Theorem adds a THEOREM unit.
func (b *Module) Theorem(e ast.Expr) *Module {
	b.m.Units = append(b.m.Units, &ast.Theorem{Keyword: "THEOREM", Expr: e})
	return b
}

// Separator adds a ---- divider line.
func (b *Module) Separator() *Module {
	b.m.Units = append(b.m.Units, &ast.Separator{})
	return b
}

// Add appends an arbitrary ast unit, as an escape hatch.
func (b *Module) Add(u ast.Unit) *Module {
	b.m.Units = append(b.m.Units, u)
	return b
}

// AST returns the built module.
func (b *Module) AST() *ast.Module { return b.m }

// String renders the module as TLA+ source.
func (b *Module) String() string { return b.m.String() }

// ------------------------------------------------------ atoms and names

// ID references a name.
func ID(name string) ast.Expr { return &ast.Ident{Name: name} }

// Num is an integer literal.
func Num(v int64) ast.Expr {
	if v < 0 {
		return &ast.Unary{Op: "-", X: &ast.NumberLit{Lit: fmt.Sprintf("%d", -v)}}
	}
	return &ast.NumberLit{Lit: fmt.Sprintf("%d", v)}
}

// Str is a string literal.
func Str(s string) ast.Expr { return &ast.StringLit{Value: s} }

// True and False are the boolean literals.
func True() ast.Expr  { return &ast.BoolLit{Value: true} }
func False() ast.Expr { return &ast.BoolLit{Value: false} }

// Apply applies a named operator: Apply("Append", s, x) is Append(s, x).
func Apply(name string, args ...ast.Expr) ast.Expr {
	return &ast.Apply{Fun: &ast.Ident{Name: name}, Args: args}
}

// ---------------------------------------------------------------- logic

func binary(op string, l, r ast.Expr) ast.Expr { return &ast.Binary{Op: op, L: l, R: r} }

// And builds an aligned conjunction list (a single expression is
// returned unchanged).
func And(es ...ast.Expr) ast.Expr {
	if len(es) == 1 {
		return es[0]
	}
	return &ast.Junction{Op: "/\\", Items: es}
}

// Or builds an aligned disjunction list.
func Or(es ...ast.Expr) ast.Expr {
	if len(es) == 1 {
		return es[0]
	}
	return &ast.Junction{Op: "\\/", Items: es}
}

// Not is ~e.
func Not(e ast.Expr) ast.Expr { return &ast.Unary{Op: "~", X: e} }

// Implies is l => r.
func Implies(l, r ast.Expr) ast.Expr { return binary("=>", l, r) }

// Equiv is l <=> r.
func Equiv(l, r ast.Expr) ast.Expr { return binary("<=>", l, r) }

// ----------------------------------------------------------- comparison

func Eq(l, r ast.Expr) ast.Expr  { return binary("=", l, r) }
func Neq(l, r ast.Expr) ast.Expr { return binary("/=", l, r) }
func Lt(l, r ast.Expr) ast.Expr  { return binary("<", l, r) }
func Le(l, r ast.Expr) ast.Expr  { return binary("<=", l, r) }
func Gt(l, r ast.Expr) ast.Expr  { return binary(">", l, r) }
func Ge(l, r ast.Expr) ast.Expr  { return binary(">=", l, r) }

// ----------------------------------------------------------- arithmetic

func Plus(l, r ast.Expr) ast.Expr  { return binary("+", l, r) }
func Minus(l, r ast.Expr) ast.Expr { return binary("-", l, r) }
func Mul(l, r ast.Expr) ast.Expr   { return binary("*", l, r) }
func Div(l, r ast.Expr) ast.Expr   { return binary("\\div", l, r) }
func Mod(l, r ast.Expr) ast.Expr   { return binary("%", l, r) }

// Range is the interval lo..hi.
func Range(lo, hi ast.Expr) ast.Expr { return binary("..", lo, hi) }

// ----------------------------------------------------------------- sets

// SetOf enumerates a set: {e1, ..., en}.
func SetOf(es ...ast.Expr) ast.Expr { return &ast.SetEnum{Elems: es} }

// In is e \in s; NotIn is e \notin s.
func In(e, s ast.Expr) ast.Expr    { return binary("\\in", e, s) }
func NotIn(e, s ast.Expr) ast.Expr { return binary("\\notin", e, s) }

// Subseteq is l \subseteq r.
func Subseteq(l, r ast.Expr) ast.Expr { return binary("\\subseteq", l, r) }

func Union(l, r ast.Expr) ast.Expr     { return binary("\\cup", l, r) }
func Intersect(l, r ast.Expr) ast.Expr { return binary("\\cap", l, r) }
func SetMinus(l, r ast.Expr) ast.Expr  { return binary("\\", l, r) }

// Powerset is SUBSET s; BigUnion is UNION s; Domain is DOMAIN f.
func Powerset(s ast.Expr) ast.Expr { return &ast.Unary{Op: "SUBSET", X: s} }
func BigUnion(s ast.Expr) ast.Expr { return &ast.Unary{Op: "UNION", X: s} }
func Domain(f ast.Expr) ast.Expr   { return &ast.Unary{Op: "DOMAIN", X: f} }

// Filter is {names \in set : pred}.
func Filter(b ast.Bound, pred ast.Expr) ast.Expr {
	return &ast.SetFilter{Bound: b, Pred: pred}
}

// MapSet is {body : names \in set}.
func MapSet(body ast.Expr, bounds ...ast.Bound) ast.Expr {
	return &ast.SetMap{Body: body, Bounds: bounds}
}

// Times is the Cartesian product S \X T \X ...
func Times(factors ...ast.Expr) ast.Expr { return &ast.Times{Factors: factors} }

// ------------------------------------- functions, records, and tuples

// Bind builds the bound "names \in set" used by quantifiers, Filter, Fn,
// and Choose.
func Bind(set ast.Expr, names ...string) ast.Bound {
	return ast.Bound{Names: names, Set: set}
}

// TupleOf is <<e1, ..., en>>.
func TupleOf(es ...ast.Expr) ast.Expr { return &ast.Tuple{Elems: es} }

// Fn is the function constructor [names \in set |-> body].
func Fn(b ast.Bound, body ast.Expr) ast.Expr {
	return &ast.FuncLit{Bounds: []ast.Bound{b}, Body: body}
}

// FnApp applies a function: f[x].
func FnApp(f ast.Expr, args ...ast.Expr) ast.Expr {
	return &ast.FnApply{Fn: f, Args: args}
}

// FuncSet is [domain -> range].
func FuncSet(domain, rng ast.Expr) ast.Expr {
	return &ast.FuncSet{Domain: domain, Range: rng}
}

// Field pairs a record field name with its value.
func Field(name string, v ast.Expr) ast.Field { return ast.Field{Name: name, Expr: v} }

// Rec is the record constructor [f1 |-> v1, ...].
func Rec(fields ...ast.Field) ast.Expr { return &ast.RecordLit{Fields: fields} }

// RecSet is the record-set constructor [f1 : S1, ...].
func RecSet(fields ...ast.Field) ast.Expr { return &ast.RecordSet{Fields: fields} }

// Dot selects a record field: r.name.
func Dot(r ast.Expr, name string) ast.Expr { return &ast.Dot{X: r, Field: name} }

// ExceptIdx builds [f EXCEPT ![idx] = v].
func ExceptIdx(f, idx, v ast.Expr) ast.Expr {
	return &ast.Except{Fn: f, Specs: []ast.ExceptSpec{
		{Path: []ast.ExceptPath{{Index: []ast.Expr{idx}}}, Value: v},
	}}
}

// ExceptField builds [r EXCEPT !.name = v].
func ExceptField(r ast.Expr, name string, v ast.Expr) ast.Expr {
	return &ast.Except{Fn: r, Specs: []ast.ExceptSpec{
		{Path: []ast.ExceptPath{{Field: name}}, Value: v},
	}}
}

// AtOld is the @ placeholder (the previous value) in EXCEPT clauses.
func AtOld() ast.Expr { return &ast.At{} }

// ------------------------------------------------------- control forms

// IfThenElse is IF c THEN t ELSE e.
func IfThenElse(c, t, e ast.Expr) ast.Expr {
	return &ast.If{Cond: c, Then: t, Else: e}
}

// Forall is \A names \in set : body.
func Forall(b ast.Bound, body ast.Expr) ast.Expr {
	return &ast.Quant{Kind: ast.Forall, Bounds: []ast.Bound{b}, Body: body}
}

// Exists is \E names \in set : body.
func Exists(b ast.Bound, body ast.Expr) ast.Expr {
	return &ast.Quant{Kind: ast.Exists, Bounds: []ast.Bound{b}, Body: body}
}

// Choose is CHOOSE name \in set : body (set may be nil).
func Choose(name string, set, body ast.Expr) ast.Expr {
	return &ast.Choose{Var: name, Set: set, Body: body}
}

// ------------------------------------------------- actions and temporal

// Prime is e'.
func Prime(e ast.Expr) ast.Expr { return &ast.PostfixExpr{Op: "'", X: e} }

// Unchanged is UNCHANGED v, or UNCHANGED <<v1, v2, ...>> for several
// variables.
func Unchanged(vars ...string) ast.Expr {
	if len(vars) == 1 {
		return &ast.Unary{Op: "UNCHANGED", X: ID(vars[0])}
	}
	es := make([]ast.Expr, len(vars))
	for i, v := range vars {
		es[i] = ID(v)
	}
	return &ast.Unary{Op: "UNCHANGED", X: &ast.Tuple{Elems: es}}
}

// Always is []e; Eventually is <>e; LeadsTo is l ~> r.
func Always(e ast.Expr) ast.Expr     { return &ast.Unary{Op: "[]", X: e} }
func Eventually(e ast.Expr) ast.Expr { return &ast.Unary{Op: "<>", X: e} }
func LeadsTo(l, r ast.Expr) ast.Expr { return binary("~>", l, r) }

// Enabled is ENABLED a.
func Enabled(a ast.Expr) ast.Expr { return &ast.Unary{Op: "ENABLED", X: a} }

// BoxAction is [A]_sub; AngleAction is <<A>>_sub.
func BoxAction(a, sub ast.Expr) ast.Expr   { return &ast.SquareAct{X: a, Sub: sub} }
func AngleAction(a, sub ast.Expr) ast.Expr { return &ast.AngleAct{X: a, Sub: sub} }

// WF is WF_sub(A); SF is SF_sub(A).
func WF(sub, a ast.Expr) ast.Expr { return &ast.Fairness{Sub: sub, X: a} }
func SF(sub, a ast.Expr) ast.Expr { return &ast.Fairness{Strong: true, Sub: sub, X: a} }

// Spec builds the standard behavior formula
// init /\ [][next]_sub [/\ fairness...].
func Spec(init, next, sub ast.Expr, fairness ...ast.Expr) ast.Expr {
	conjuncts := []ast.Expr{init, Always(BoxAction(next, sub))}
	conjuncts = append(conjuncts, fairness...)
	return And(conjuncts...)
}
