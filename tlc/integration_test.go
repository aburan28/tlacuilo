package tlc

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/value"
)

// requireTLC skips unless java and tla2tools.jar are available.
func requireTLC(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java not installed")
	}
	jar, err := FindJar()
	if err != nil {
		t.Skip("tla2tools.jar not found (set TLA2TOOLS_JAR to enable integration tests)")
	}
	return jar
}

const counterSpec = `---- MODULE TlacuiloCounter ----
EXTENDS Naturals
VARIABLE x
Init == x = 0
Next == x' = (x + 1) % 4
Spec == Init /\ [][Next]_x /\ WF_x(Next)
TypeOK == x \in 0..3
Reaches == <>(x = 3)
====
`

func TestIntegrationSuccess(t *testing.T) {
	jar := requireTLC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, err := Check(ctx, Job{
		Source: counterSpec,
		Config: &cfg.Config{
			Specification: "Spec",
			Invariants:    []string{"TypeOK"},
			Properties:    []string{"Reaches"},
		},
	}, Options{JarPath: jar, DisableDeadlockCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Ok() {
		t.Fatalf("status = %v, errors = %v", r.Status, r.Errors)
	}
	if r.DistinctStates != 4 {
		t.Errorf("distinct states = %d, want 4", r.DistinctStates)
	}
}

func TestIntegrationInvariantViolation(t *testing.T) {
	jar := requireTLC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	bad := &cfg.Config{
		Specification: "Spec",
		Invariants:    []string{"Bad"},
	}
	src := counterSpec[:len(counterSpec)-5] + "Bad == x < 3\n====\n"
	r, err := Check(ctx, Job{Source: src, Config: bad},
		Options{JarPath: jar, DisableDeadlockCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusSafetyViolation {
		t.Fatalf("status = %v, errors = %v", r.Status, r.Errors)
	}
	tr := r.Trace
	if tr == nil || len(tr.States) != 4 {
		t.Fatalf("trace = %+v", tr)
	}
	last := tr.States[len(tr.States)-1]
	if !value.Equal(last.Vars["x"], value.NewInt(3)) {
		t.Errorf("violating x = %v", last.Vars["x"])
	}
}
