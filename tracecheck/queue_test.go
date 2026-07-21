package tracecheck

import (
	"testing"

	"github.com/aburan28/tlacuilo/cfg"
)

// Tool type 2: a bounded FIFO queue — the shape of a Go channel or work
// queue. The trace is recorded under a deterministic harness; checking
// arbitrary goroutine interleavings against the spec is exactly the
// state explosion this package's design avoids.
const queueSpec = `---- MODULE BoundedQueue ----
EXTENDS Naturals, Sequences

CONSTANTS Capacity, Values

VARIABLE q

Init == q = <<>>

Enq == \E v \in Values : /\ Len(q) < Capacity
                         /\ q' = Append(q, v)

Deq == /\ q /= <<>>
       /\ q' = Tail(q)

Next == Enq \/ Deq

Spec == Init /\ [][Next]_q
====
`

type fifo struct {
	buf  []int
	cap  int
	lifo bool // the bug: pop from the wrong end
}

func (f *fifo) Enq(v int) bool {
	if len(f.buf) >= f.cap {
		return false
	}
	f.buf = append(f.buf, v)
	return true
}

func (f *fifo) Deq() (int, bool) {
	if len(f.buf) == 0 {
		return 0, false
	}
	if f.lifo {
		v := f.buf[len(f.buf)-1]
		f.buf = f.buf[:len(f.buf)-1]
		return v, true
	}
	v := f.buf[0]
	f.buf = f.buf[1:]
	return v, true
}

func queueTraceSpec() Spec {
	return Spec{
		Module:  "BoundedQueue",
		Vars:    []string{"q"},
		Actions: map[string]string{"Enq": "Enq", "Deq": "Deq"},
		Constants: []cfg.Constant{
			{Name: "Capacity", Value: "3"},
			{Name: "Values", Value: "{1, 2, 3}"},
		},
	}
}

func driveQueue(t *testing.T, f *fifo) *Recorder {
	t.Helper()
	rec := NewRecorder()
	record := func(action string) {
		if err := rec.Step(action, map[string]any{"q": f.buf}); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.Init(map[string]any{"q": f.buf}); err != nil {
		t.Fatal(err)
	}
	f.Enq(1)
	record("Enq")
	f.Enq(2)
	record("Enq")
	f.Deq()
	record("Deq")
	f.Enq(3)
	record("Enq")
	f.Deq()
	record("Deq")
	f.Deq()
	record("Deq")
	return rec
}

func TestQueueConforms(t *testing.T) {
	f := &fifo{cap: 3}
	rec := driveQueue(t, f)
	rep := validateT(t, queueTraceSpec(), rec, queueSpec)
	if !rep.Conforms {
		t.Fatalf("correct queue rejected: %s (TLC: %v)", rep, rep.Result.Errors)
	}
}

func TestBuggyLIFOQueueDiverges(t *testing.T) {
	f := &fifo{cap: 3, lifo: true}
	rec := driveQueue(t, f)
	rep := validateT(t, queueTraceSpec(), rec, queueSpec)
	if rep.Conforms {
		t.Fatal("LIFO queue accepted as FIFO")
	}
	// States: 1 Init <<>>, 2 Enq <<1>>, 3 Enq <<1,2>>, 4 Deq -> LIFO
	// leaves <<1>> but the spec's Tail gives <<2>>.
	if rep.DivergedAt != 4 || rep.FailedAction != "Deq" {
		t.Errorf("divergence at state %d action %q, want state 4 action Deq",
			rep.DivergedAt, rep.FailedAction)
	}
}
