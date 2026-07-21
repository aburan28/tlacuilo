package tracecheck

import (
	"testing"

	"github.com/aburan28/tlacuilo/cfg"
)

// Tool type 3: lock discipline around a shared resource. The spec says
// the lock has one owner at a time; the implementation records who it
// believes holds the lock at each acquire/release.
const mutexSpec = `---- MODULE Mutex ----
CONSTANT Procs

VARIABLE owner

Init == owner = "none"

Acquire == /\ owner = "none"
           /\ owner' \in Procs

Release == /\ owner /= "none"
           /\ owner' = "none"

Next == Acquire \/ Release

Spec == Init /\ [][Next]_owner
====
`

// guard tracks lock ownership; checked=false models code that bypasses
// the "is it free?" check (a stolen lock).
type guard struct {
	owner   string
	checked bool
}

func (g *guard) Acquire(p string) bool {
	if g.checked && g.owner != "" {
		return false
	}
	g.owner = p
	return true
}

func (g *guard) Release() { g.owner = "" }

func (g *guard) recorded() map[string]any {
	o := g.owner
	if o == "" {
		o = "none"
	}
	return map[string]any{"owner": o}
}

func mutexTraceSpec() Spec {
	return Spec{
		Module:  "Mutex",
		Vars:    []string{"owner"},
		Actions: map[string]string{"Acquire": "Acquire", "Release": "Release"},
		Constants: []cfg.Constant{
			{Name: "Procs", Value: `{"p1", "p2"}`},
		},
	}
}

func TestLockDisciplineConforms(t *testing.T) {
	g := &guard{checked: true}
	rec := NewRecorder()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(rec.Init(g.recorded()))
	g.Acquire("p1")
	must(rec.Step("Acquire", g.recorded()))
	g.Release()
	must(rec.Step("Release", g.recorded()))
	g.Acquire("p2")
	must(rec.Step("Acquire", g.recorded()))
	g.Release()
	must(rec.Step("Release", g.recorded()))

	rep := validateT(t, mutexTraceSpec(), rec, mutexSpec)
	if !rep.Conforms {
		t.Fatalf("correct locking rejected: %s (TLC: %v)", rep, rep.Result.Errors)
	}
}

func TestStolenLockDiverges(t *testing.T) {
	g := &guard{checked: false}
	rec := NewRecorder()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(rec.Init(g.recorded()))
	g.Acquire("p1")
	must(rec.Step("Acquire", g.recorded()))
	// p2 steals the lock while p1 holds it.
	g.Acquire("p2")
	must(rec.Step("Acquire", g.recorded()))

	rep := validateT(t, mutexTraceSpec(), rec, mutexSpec)
	if rep.Conforms {
		t.Fatal("stolen lock accepted")
	}
	// States: 1 Init none, 2 Acquire p1, 3 Acquire p2 <- diverges.
	if rep.DivergedAt != 3 || rep.FailedAction != "Acquire" {
		t.Errorf("divergence at state %d action %q, want state 3 action Acquire",
			rep.DivergedAt, rep.FailedAction)
	}
}
