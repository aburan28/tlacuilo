// Command reconciler demonstrates trace validation of a
// Kubernetes-style reconcile controller against a TLA+ spec.
//
// The controller is driven by a deterministic harness and records its
// state at each annotated action. tracecheck generates a TLA+ trace
// spec embedding the recorded behavior with a refinement mapping onto
// the abstract Reconciler spec, then asks TLC whether the behavior is
// allowed. Run with -bug to see a divergence report: the buggy
// controller reconciles two replicas per step, which the spec forbids.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/tlc"
	"github.com/aburan28/tlacuilo/tracecheck"
)

const reconcilerSpec = `---- MODULE Reconciler ----
EXTENDS Naturals

CONSTANT MaxReplicas

VARIABLES desired, current

vars == <<desired, current>>

TypeOK == /\ desired \in 0..MaxReplicas
          /\ current \in 0..MaxReplicas

Init == /\ desired = 0
        /\ current = 0

ChangeDesired == /\ desired' \in 0..MaxReplicas
                 /\ desired' /= desired
                 /\ UNCHANGED current

ScaleUp == /\ current < desired
           /\ current' = current + 1
           /\ UNCHANGED desired

ScaleDown == /\ current > desired
             /\ current' = current - 1
             /\ UNCHANGED desired

Next == ChangeDesired \/ ScaleUp \/ ScaleDown

Spec == Init /\ [][Next]_vars
====
`

// controller is the implementation under test; `tla` tags are the state
// mapping onto the spec's variables.
type controller struct {
	Desired int `tla:"desired"`
	Current int `tla:"current"`
	stepBy  int
}

func (c *controller) reconcile() (string, bool) {
	gap := c.Desired - c.Current
	switch {
	case gap > 0:
		c.Current += min(c.stepBy, gap)
		return "ScaleUp", true
	case gap < 0:
		c.Current -= min(c.stepBy, -gap)
		return "ScaleDown", true
	}
	return "", false
}

func main() {
	bug := flag.Bool("bug", false, "run the buggy controller (reconciles 2 replicas per step)")
	flag.Parse()

	c := &controller{stepBy: 1}
	if *bug {
		c.stepBy = 2
	}

	// Deterministic harness: change desired state, reconcile to
	// convergence, change it again mid-flight.
	rec := tracecheck.NewRecorder()
	check(rec.InitState(c))
	for _, desired := range []int{3, 1} {
		c.Desired = desired
		check(rec.StepState("ChangeDesired", c))
		for {
			act, ok := c.reconcile()
			if !ok {
				break
			}
			check(rec.StepState(act, c))
		}
	}

	spec := tracecheck.Spec{
		Module: "Reconciler",
		Vars:   []string{"desired", "current"},
		Actions: map[string]string{
			"ChangeDesired": "ChangeDesired",
			"ScaleUp":       "ScaleUp",
			"ScaleDown":     "ScaleDown",
		},
		Constants:  []cfg.Constant{{Name: "MaxReplicas", Value: "5"}},
		Invariants: []string{"TypeOK"},
	}

	m, err := tracecheck.GenModule(spec, rec.Trace())
	check(err)
	fmt.Println("Generated trace specification:")
	fmt.Println(m)

	if _, err := tlc.FindJar(); err != nil {
		fmt.Fprintln(os.Stderr, "skipping validation:", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := tracecheck.Validate(ctx, spec, rec.Trace(), reconcilerSpec, tlc.Options{})
	check(err)
	fmt.Println(report)
	if !report.Conforms {
		os.Exit(1)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
