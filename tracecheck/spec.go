package tracecheck

import (
	"fmt"
	"sort"

	"github.com/aburan28/tlacuilo/ast"
	b "github.com/aburan28/tlacuilo/builder"
	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/parser"
	"github.com/aburan28/tlacuilo/trace"
	"github.com/aburan28/tlacuilo/value"
)

// Spec describes the abstract specification a trace is checked against
// and the refinement mapping from recorded actions onto it.
type Spec struct {
	// Module is the abstract module's name (the trace module extends it).
	Module string
	// Vars are the spec variables bound by the mapping; every recorded
	// state must assign all of them (the recorder's carry-forward
	// guarantees this once Init assigns them).
	Vars []string
	// Actions maps recorded action names to abstract action operators
	// (zero-argument; wrap parameterized actions in an existential
	// operator on the spec side). A recorded step whose action is mapped
	// must satisfy exactly that operator; unmapped actions fall back to
	// [Next]_vars, which also admits stuttering steps.
	Actions map[string]string
	// NextOperator is the abstract next-state operator (default "Next").
	NextOperator string
	// SkipInit drops the requirement that the first recorded state
	// satisfies the abstract Init (for traces starting mid-behavior).
	SkipInit bool
	// InitOperator is the abstract initial predicate (default "Init").
	InitOperator string
	// Constants populate the generated TLC configuration.
	Constants []cfg.Constant
	// Invariants are additionally checked over the trace states.
	Invariants []string
	// Extends lists extra modules the trace module needs (Naturals,
	// Sequences, and Module are always included).
	Extends []string
}

func (s *Spec) next() string {
	if s.NextOperator != "" {
		return s.NextOperator
	}
	return "Next"
}

func (s *Spec) init() string {
	if s.InitOperator != "" {
		return s.InitOperator
	}
	return "Init"
}

// TraceModuleName is the name of the generated module for a spec.
func (s *Spec) TraceModuleName() string { return s.Module + "Trace" }

// reserved names introduced by the generated module.
var generatedNames = []string{"TraceIdx", "Trace", "TraceVars", "TraceMatch",
	"TraceInit", "TraceNext", "TraceSpec", "TraceComplete"}

// GenModule generates the TLA+ trace-validation module for a recorded
// trace: the trace is embedded as a constant sequence of records, and a
// TraceNext action advances an index while requiring each recorded step
// to be a step the abstract spec allows. When the spec cannot take a
// recorded step, the generated system has no successor state, so
// divergence surfaces as a TLC deadlock at the failing index.
func GenModule(s Spec, tr *trace.Trace) (*ast.Module, error) {
	if s.Module == "" {
		return nil, fmt.Errorf("tracecheck: Spec.Module is required")
	}
	if len(s.Vars) == 0 {
		return nil, fmt.Errorf("tracecheck: Spec.Vars is required")
	}
	for _, v := range s.Vars {
		if v == "act" {
			return nil, fmt.Errorf("tracecheck: variable name %q collides with the trace record's action field", v)
		}
		for _, g := range generatedNames {
			if v == g {
				return nil, fmt.Errorf("tracecheck: variable name %q collides with a generated name", v)
			}
		}
	}
	if len(tr.States) == 0 {
		return nil, fmt.Errorf("tracecheck: empty trace (record Init first)")
	}

	// Trace == << [act |-> "...", v1 |-> ..., ...], ... >>
	states := make([]ast.Expr, len(tr.States))
	for i, st := range tr.States {
		fields := []ast.Field{{Name: "act", Expr: b.Str(st.Action)}}
		for _, v := range s.Vars {
			val, ok := st.Vars[v]
			if !ok {
				return nil, fmt.Errorf("tracecheck: state %d does not assign variable %s", i+1, v)
			}
			e, err := valueExpr(val)
			if err != nil {
				return nil, fmt.Errorf("tracecheck: state %d, variable %s: %w", i+1, v, err)
			}
			fields = append(fields, ast.Field{Name: v, Expr: e})
		}
		states[i] = &ast.RecordLit{Fields: fields}
	}

	idx := b.ID("TraceIdx")
	traceID := b.ID("Trace")
	traceLen := b.Apply("Len", traceID)
	specVars := make([]ast.Expr, len(s.Vars))
	for i, v := range s.Vars {
		specVars[i] = b.ID(v)
	}
	specVarsTuple := b.TupleOf(specVars...)
	traceVars := b.TupleOf(append([]ast.Expr{idx}, specVars...)...)

	// Sorted action mapping for deterministic output.
	actionNames := make([]string, 0, len(s.Actions))
	for a := range s.Actions {
		actionNames = append(actionNames, a)
	}
	sort.Strings(actionNames)

	m := b.NewModule(s.TraceModuleName())
	extends := append([]string{"Naturals", "Sequences"}, s.Extends...)
	extends = append(extends, s.Module)
	m.Extends(dedup(extends)...)
	m.Add(&ast.VariableDecl{Names: []string{"TraceIdx"}})
	m.Define("Trace", b.TupleOf(states...))
	m.Define("TraceVars", traceVars)

	fallback := b.BoxAction(b.ID(s.next()), specVarsTuple)
	if len(actionNames) > 0 {
		arms := make([]ast.CaseArm, len(actionNames))
		for i, a := range actionNames {
			arms[i] = ast.CaseArm{
				Cond:  b.Eq(b.Dot(b.FnApp(traceID, b.ID("k")), "act"), b.Str(a)),
				Value: b.ID(s.Actions[a]),
			}
		}
		m.Add(&ast.OperatorDef{Name: "TraceMatch", Params: []ast.OpDecl{{Name: "k"}},
			Body: &ast.Case{Arms: arms, Other: fallback}})
	}

	// TraceInit == /\ TraceIdx = 1 /\ v = Trace[1].v ... [/\ Init]
	initConj := []ast.Expr{b.Eq(idx, b.Num(1))}
	for _, v := range s.Vars {
		initConj = append(initConj, b.Eq(b.ID(v), b.Dot(b.FnApp(traceID, b.Num(1)), v)))
	}
	if !s.SkipInit {
		initConj = append(initConj, b.ID(s.init()))
	}
	m.Define("TraceInit", b.And(initConj...))

	// TraceNext: advance and match, or self-loop at the end (so the only
	// deadlock is a mid-trace divergence).
	k1 := b.Plus(idx, b.Num(1))
	advance := []ast.Expr{
		b.Lt(idx, traceLen),
		b.Eq(b.Prime(idx), k1),
	}
	for _, v := range s.Vars {
		advance = append(advance, b.Eq(b.Prime(b.ID(v)), b.Dot(b.FnApp(traceID, k1), v)))
	}
	if len(actionNames) > 0 {
		advance = append(advance, b.Apply("TraceMatch", k1))
	} else {
		advance = append(advance, fallback)
	}
	done := b.And(
		b.Eq(idx, traceLen),
		b.Unchanged("TraceVars"),
	)
	m.Define("TraceNext", b.Or(b.And(advance...), done))

	m.Define("TraceSpec", b.And(
		b.ID("TraceInit"),
		b.Always(b.BoxAction(b.ID("TraceNext"), b.ID("TraceVars"))),
	))
	m.Define("TraceComplete", b.Eq(idx, traceLen))
	return m.AST(), nil
}

// GenConfig generates the TLC configuration for the trace module.
// Deadlock checking stays enabled: a deadlock is exactly a divergence.
func GenConfig(s Spec) *cfg.Config {
	return &cfg.Config{
		Specification: "TraceSpec",
		Constants:     append([]cfg.Constant(nil), s.Constants...),
		Invariants:    append([]string(nil), s.Invariants...),
	}
}

// valueExpr renders a recorded value as an expression. Values print as
// TLA+ literal syntax, so parsing the rendering is exact.
func valueExpr(v value.Value) (ast.Expr, error) {
	return parser.ParseExpr(v.String())
}

func dedup(ss []string) []string {
	seen := map[string]bool{}
	out := ss[:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
