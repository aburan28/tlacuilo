package tracecheck_test

import (
	"context"
	"fmt"

	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/tlc"
	"github.com/aburan28/tlacuilo/tracecheck"
)

const exampleReconcilerSpec = `---- MODULE Reconciler ----
EXTENDS Naturals
CONSTANT MaxReplicas
VARIABLES desired, current
Init == desired = 0 /\ current = 0
ChangeDesired == /\ desired' \in 0..MaxReplicas
                 /\ desired' /= desired
                 /\ UNCHANGED current
ScaleUp == current < desired /\ current' = current + 1 /\ UNCHANGED desired
ScaleDown == current > desired /\ current' = current - 1 /\ UNCHANGED desired
Next == ChangeDesired \/ ScaleUp \/ ScaleDown
====
`

// ExampleGenModule shows the TLA+ trace-validation module generated for
// a small recorded run.
func ExampleGenModule() {
	rec := tracecheck.NewRecorder()
	_ = rec.Init(map[string]any{"x": 0})
	_ = rec.Step("Inc", map[string]any{"x": 1})

	m, err := tracecheck.GenModule(tracecheck.Spec{
		Module:  "Counter",
		Vars:    []string{"x"},
		Actions: map[string]string{"Inc": "Inc"},
	}, rec.Trace())
	if err != nil {
		panic(err)
	}
	fmt.Print(m)
	// Output:
	// ---------------------------- MODULE CounterTrace ----------------------------
	// EXTENDS Naturals, Sequences, Counter
	//
	// VARIABLE TraceIdx
	//
	// Trace == <<[act |-> "Init", x |-> 0], [act |-> "Inc", x |-> 1]>>
	//
	// TraceVars == <<TraceIdx, x>>
	//
	// TraceMatch(k) == CASE Trace[k].act = "Inc" -> Inc [] OTHER -> [Next]_<<x>>
	//
	// TraceInit == /\ TraceIdx = 1
	//              /\ x = Trace[1].x
	//              /\ Init
	//
	// TraceNext == \/ /\ TraceIdx < Len(Trace)
	//                 /\ TraceIdx' = TraceIdx + 1
	//                 /\ x' = Trace[TraceIdx + 1].x
	//                 /\ TraceMatch(TraceIdx + 1)
	//              \/ /\ TraceIdx = Len(Trace)
	//                 /\ UNCHANGED TraceVars
	//
	// TraceSpec == /\ TraceInit
	//              /\ [][TraceNext]_TraceVars
	//
	// TraceComplete == TraceIdx = Len(Trace)
	//
	// =============================================================================
}

// ExampleValidate checks a recorded implementation run against a spec.
// It needs Java and tla2tools.jar, so it is compiled but not run.
func ExampleValidate() {
	type state struct {
		Desired int `tla:"desired"`
		Current int `tla:"current"`
	}
	s := state{}
	rec := tracecheck.NewRecorder()
	_ = rec.InitState(s)
	s.Desired = 1
	_ = rec.StepState("ChangeDesired", s)
	s.Current = 1
	_ = rec.StepState("ScaleUp", s)

	report, err := tracecheck.Validate(context.Background(), tracecheck.Spec{
		Module: "Reconciler",
		Vars:   []string{"desired", "current"},
		Actions: map[string]string{
			"ChangeDesired": "ChangeDesired",
			"ScaleUp":       "ScaleUp",
			"ScaleDown":     "ScaleDown",
		},
		Constants: []cfg.Constant{{Name: "MaxReplicas", Value: "5"}},
	}, rec.Trace(), exampleReconcilerSpec, tlc.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Println(report.Conforms)
}
