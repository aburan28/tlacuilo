// Package k8scontroller is a worked example of validating a Kubernetes
// controller against a TLA+ specification with tlacuilo's tracecheck.
//
// The reconciler below is shaped like a controller-runtime reconciler
// (Reconcile(ctx, Request) (Result, error) against a client), without
// importing Kubernetes dependencies: Request, Result, and Cluster stand
// in for reconcile.Request, reconcile.Result, and client.Client. In a
// real controller you would keep the same three ingredients:
//
//  1. the abstract spec (ReplicaController.tla, embedded below),
//  2. a projection from cluster state to the spec's variables
//     (TLAState),
//  3. action annotations at the points where the controller mutates the
//     cluster (the strings returned by Reconcile, recorded by the test
//     harness).
//
// The tests in controller_test.go drive the reconciler under a
// deterministic harness (the fake cluster plays the role of
// envtest/fake client), record the trace, and let TLC decide whether
// the behavior refines the spec. They also model-check the spec itself
// exhaustively — see the TLA+ proof workflow in
// .github/workflows/tla-proof.yml.
package k8scontroller

import (
	"context"
	_ "embed"
	"sort"

	"github.com/aburan28/tlacuilo/value"
)

// SpecSource is the abstract TLA+ specification, embedded so the code
// and its spec version together.
//
//go:embed ReplicaController.tla
var SpecSource string

// Request mirrors controller-runtime's reconcile.Request.
type Request struct {
	Name string
}

// Result mirrors controller-runtime's reconcile.Result.
type Result struct {
	Requeue bool
}

// Cluster is the slice of the API the controller uses; in a real
// controller this is the controller-runtime client (or a fake client in
// tests).
type Cluster interface {
	Desired() int
	Pods() []string // sorted
	CreatePod(name string)
	DeletePod(name string)
}

// FakeCluster is the deterministic in-memory test double.
type FakeCluster struct {
	desired int
	pods    map[string]bool
}

func NewFakeCluster(desired int) *FakeCluster {
	return &FakeCluster{desired: desired, pods: map[string]bool{}}
}

func (c *FakeCluster) Desired() int { return c.desired }

func (c *FakeCluster) Pods() []string {
	out := make([]string, 0, len(c.pods))
	for p := range c.pods {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (c *FakeCluster) CreatePod(name string) { c.pods[name] = true }
func (c *FakeCluster) DeletePod(name string) { delete(c.pods, name) }

// SetDesired and CrashPod are the environment's moves, driven by the
// test harness (a user editing the object's spec; a node losing a pod).
func (c *FakeCluster) SetDesired(d int) { c.desired = d }
func (c *FakeCluster) CrashPod(name string) {
	delete(c.pods, name)
}

// ReplicaReconciler converges the running pod set toward the desired
// count, one pod per reconcile invocation — requeueing until level, the
// way real controllers make incremental progress.
type ReplicaReconciler struct {
	Cluster Cluster
	PodPool []string // names the controller may create, e.g. p1..p3

	// BatchCreate is a deliberate bug switch used by the tests: create
	// two pods per reconcile "as an optimization". The spec's CreatePod
	// action adds exactly one pod, so trace validation must catch it.
	BatchCreate bool
}

// Reconcile performs one reconcile step. It returns the spec action it
// performed ("CreatePod", "DeletePod", or "" for a level no-op) — the
// annotation the harness records. In a real controller the annotation
// call sits next to the client.Create/Delete call.
func (r *ReplicaReconciler) Reconcile(_ context.Context, _ Request) (Result, string) {
	pods := r.Cluster.Pods()
	desired := r.Cluster.Desired()
	switch {
	case len(pods) < desired:
		created := 0
		want := 1
		if r.BatchCreate && desired-len(pods) >= 2 {
			want = 2
		}
		for _, name := range r.PodPool {
			if created == want {
				break
			}
			if !contains(pods, name) {
				r.Cluster.CreatePod(name)
				created++
			}
		}
		return Result{Requeue: len(pods)+created != desired}, "CreatePod"
	case len(pods) > desired:
		r.Cluster.DeletePod(pods[0])
		return Result{Requeue: len(pods)-1 != desired}, "DeletePod"
	}
	return Result{}, ""
}

// TLAState projects cluster state onto the spec's variables. This is
// the variable mapping: desired stays an integer; the pod list becomes
// the SET the spec talks about (order is a Go artifact, not part of the
// abstraction).
func TLAState(c Cluster) map[string]any {
	pods := c.Pods()
	elems := make([]value.Value, len(pods))
	for i, p := range pods {
		elems[i] = value.String(p)
	}
	return map[string]any{
		"desired": c.Desired(),
		"pods":    value.NewSet(elems...),
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
