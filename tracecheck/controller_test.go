package tracecheck

import (
	"testing"

	"github.com/aburan28/tlacuilo/cfg"
)

// Tool type 1: a Kubernetes-style reconcile controller. The abstract
// spec models a replica controller: the desired count changes out from
// under the controller, which converges current toward desired one
// replica per reconcile step.
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

// replicaController is the implementation under test: the kind of
// reconcile loop a Kubernetes controller runs, driven here by a
// deterministic harness.
type replicaController struct {
	Desired int `tla:"desired"`
	Current int `tla:"current"`
	// stepBy is the reconcile increment; the correct controller moves
	// one replica per step.
	stepBy int
}

// Reconcile performs one reconcile step and reports the action taken.
func (c *replicaController) Reconcile() (string, bool) {
	switch {
	case c.Current < c.Desired:
		c.Current += min(c.stepBy, c.Desired-c.Current)
		return "ScaleUp", true
	case c.Current > c.Desired:
		c.Current -= min(c.stepBy, c.Current-c.Desired)
		return "ScaleDown", true
	}
	return "", false
}

func reconcilerTraceSpec() Spec {
	return Spec{
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
}

// driveController runs the deterministic harness: set a desired count,
// let the controller converge, change it again mid-flight.
func driveController(t *testing.T, c *replicaController) *Recorder {
	t.Helper()
	rec := NewRecorder()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(rec.InitState(c))
	setDesired := func(d int) {
		c.Desired = d
		must(rec.StepState("ChangeDesired", c))
	}
	reconcileAll := func() {
		for {
			act, ok := c.Reconcile()
			if !ok {
				return
			}
			must(rec.StepState(act, c))
		}
	}
	setDesired(3)
	reconcileAll()
	setDesired(1) // scale down mid-run
	reconcileAll()
	return rec
}

func TestControllerConforms(t *testing.T) {
	c := &replicaController{stepBy: 1}
	rec := driveController(t, c)
	rep := validateT(t, reconcilerTraceSpec(), rec, reconcilerSpec)
	if !rep.Conforms {
		t.Fatalf("correct controller rejected: %s (TLC: %v)", rep, rep.Result.Errors)
	}
}

func TestBuggyControllerDiverges(t *testing.T) {
	// The bug: reconciles two replicas at a time, violating the spec's
	// one-step ScaleUp/ScaleDown actions.
	c := &replicaController{stepBy: 2}
	rec := NewRecorder()
	if err := rec.InitState(c); err != nil {
		t.Fatal(err)
	}
	c.Desired = 3
	if err := rec.StepState("ChangeDesired", c); err != nil {
		t.Fatal(err)
	}
	for {
		act, ok := c.Reconcile()
		if !ok {
			break
		}
		if err := rec.StepState(act, c); err != nil {
			t.Fatal(err)
		}
	}
	rep := validateT(t, reconcilerTraceSpec(), rec, reconcilerSpec)
	if rep.Conforms {
		t.Fatal("buggy controller accepted")
	}
	// States: 1 Init(0,0), 2 ChangeDesired(3,0), 3 ScaleUp(3,2) <- diverges.
	if rep.DivergedAt != 3 || rep.FailedAction != "ScaleUp" {
		t.Errorf("divergence at state %d action %q, want state 3 action ScaleUp",
			rep.DivergedAt, rep.FailedAction)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
