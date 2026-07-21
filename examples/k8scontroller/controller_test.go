package k8scontroller

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/tlc"
	"github.com/aburan28/tlacuilo/tracecheck"
)

// requireTLC skips when TLC isn't available — unless TLACUILO_REQUIRE_TLC
// is set (as the TLA+ proof CI job sets it), in which case a missing
// toolchain is a hard failure rather than a silent skip.
func requireTLC(t *testing.T) tlc.Options {
	t.Helper()
	required := os.Getenv("TLACUILO_REQUIRE_TLC") != ""
	if _, err := exec.LookPath("java"); err != nil {
		if required {
			t.Fatal("TLACUILO_REQUIRE_TLC is set but java is not installed")
		}
		t.Skip("java not installed")
	}
	jar, err := tlc.FindJar()
	if err != nil {
		if required {
			t.Fatal("TLACUILO_REQUIRE_TLC is set but tla2tools.jar was not found (set TLA2TOOLS_JAR)")
		}
		t.Skip("tla2tools.jar not found (set TLA2TOOLS_JAR)")
	}
	return tlc.Options{JarPath: jar}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

var specConstants = []cfg.Constant{
	{Name: "Pods", Value: `{"p1", "p2", "p3"}`},
	{Name: "MaxReplicas", Value: "3"},
}

// TestSpecSafety is the design proof: TLC exhaustively explores every
// interleaving of spec changes, crashes, and reconcile steps, checking
// the invariants. This is where concurrency bugs in the DESIGN surface.
func TestSpecSafety(t *testing.T) {
	opts := requireTLC(t)
	r, err := tlc.Check(testCtx(t), tlc.Job{
		Source: SpecSource,
		Config: &cfg.Config{
			Specification: "Spec",
			Constants:     specConstants,
			Invariants:    []string{"TypeOK", "NeverOverProvision"},
		},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("safety proof failed: %v\ntrace:\n%v", err, r.Trace)
	}
	t.Logf("safety: %d states, %d distinct", r.StatesGenerated, r.DistinctStates)
}

// TestSpecConvergence is the liveness proof: from ANY type-correct
// state, once the environment goes quiet, fair reconciliation converges
// the pod count to the desired count and keeps it there. Converged
// states have no quiescent successor, so deadlock checking is disabled
// for this run only.
func TestSpecConvergence(t *testing.T) {
	opts := requireTLC(t)
	opts.DisableDeadlockCheck = true
	r, err := tlc.Check(testCtx(t), tlc.Job{
		Source: SpecSource,
		Config: &cfg.Config{
			Specification: "QuiescentSpec",
			Constants:     specConstants,
			Properties:    []string{"Convergence"},
		},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("convergence proof failed: %v\ntrace:\n%v", err, r.Trace)
	}
}

func replicaTraceSpec() tracecheck.Spec {
	return tracecheck.Spec{
		Module: "ReplicaController",
		Vars:   []string{"desired", "pods"},
		Actions: map[string]string{
			"ChangeDesired": "ChangeDesired",
			"PodCrash":      "PodCrash",
			"CreatePod":     "CreatePod",
			"DeletePod":     "DeletePod",
		},
		Constants:  specConstants,
		Invariants: []string{"TypeOK", "NeverOverProvision"},
	}
}

// driveScenario runs the deterministic harness: spec changes and crashes
// interleaved with reconcile-to-level loops, recording each action.
func driveScenario(t *testing.T, r *ReplicaReconciler, cl *FakeCluster) *tracecheck.Recorder {
	t.Helper()
	rec := tracecheck.NewRecorder()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	record := func(action string) { must(rec.Step(action, TLAState(cl))) }
	reconcileToLevel := func() {
		for {
			res, action := r.Reconcile(context.Background(), Request{Name: "web"})
			if action != "" {
				record(action) // annotate exactly what the controller did
			}
			if !res.Requeue {
				return
			}
		}
	}

	must(rec.Init(TLAState(cl))) // matches the spec's Init: desired=1, no pods

	reconcileToLevel() // create the first pod

	cl.SetDesired(3) // user scales up
	record("ChangeDesired")
	reconcileToLevel()

	cl.CrashPod(cl.Pods()[0]) // a node eats a pod
	record("PodCrash")
	reconcileToLevel() // controller heals

	cl.SetDesired(0) // user scales to zero
	record("ChangeDesired")
	reconcileToLevel()

	return rec
}

// TestControllerRefinesSpec is the implementation check: the recorded
// behavior of the real reconcile loop must be a behavior the spec
// allows.
func TestControllerRefinesSpec(t *testing.T) {
	opts := requireTLC(t)
	cl := NewFakeCluster(1)
	r := &ReplicaReconciler{Cluster: cl, PodPool: []string{"p1", "p2", "p3"}}
	rec := driveScenario(t, r, cl)

	report, err := tracecheck.Validate(testCtx(t), replicaTraceSpec(), rec.Trace(), SpecSource, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Conforms {
		t.Fatalf("controller diverges from spec: %s\nTLC: %v", report, report.Result.Errors)
	}
	t.Logf("validated %d recorded states against the spec", rec.Len())
}

// TestBatchCreateBugCaught shows what a divergence looks like: the
// "optimized" controller creates two pods per reconcile, which the
// spec's one-pod CreatePod action forbids. Validation must name the
// exact step.
func TestBatchCreateBugCaught(t *testing.T) {
	opts := requireTLC(t)
	cl := NewFakeCluster(1)
	r := &ReplicaReconciler{Cluster: cl, PodPool: []string{"p1", "p2", "p3"}, BatchCreate: true}
	rec := driveScenario(t, r, cl)

	report, err := tracecheck.Validate(testCtx(t), replicaTraceSpec(), rec.Trace(), SpecSource, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Conforms {
		t.Fatal("batch-create bug was not caught")
	}
	// States: 1 Init(d=1,{}), 2 CreatePod {p1}, 3 ChangeDesired d=3,
	// 4 CreatePod {p1,p2,p3} <- two pods at once diverges here.
	if report.DivergedAt != 4 || report.FailedAction != "CreatePod" {
		t.Errorf("divergence at state %d action %q, want state 4 action CreatePod",
			report.DivergedAt, report.FailedAction)
	}
	t.Logf("caught: %s", report)
}
