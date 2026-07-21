// Package tracecheck validates Go implementations against TLA+
// specifications by trace checking: the implementation, driven by a
// deterministic test harness, records its state at annotated action
// points; tracecheck generates a TLA+ trace specification embedding the
// recorded behavior together with a refinement mapping onto the abstract
// spec's actions, and asks TLC whether the behavior is one the spec
// allows.
//
// Fully automatic Go-to-TLA+ extraction runs into state explosion
// (goroutine interleavings, channel semantics); the tractable middle
// ground implemented here is the one used by industrial trace-validation
// work (MongoDB, etcd-style): annotate the actions, record only under a
// deterministic test harness rather than in production, and let the
// model checker decide conformance. PGo compiles specs to Go; this
// package is the inverse direction — checking Go against specs.
//
// The generated trace spec advances an index over the recorded states
// and requires each step to satisfy the abstract action named by its
// annotation (or the spec's Next as a fallback). A step the spec cannot
// take leaves TLC with no successor state, so divergence surfaces as a
// TLC deadlock, which Validate translates into a Report naming the
// failing step and action.
package tracecheck

import (
	"fmt"
	"sync"

	"github.com/aburan28/tlacuilo/trace"
	"github.com/aburan28/tlacuilo/value"
)

// InitAction is the action name recorded for the initial state.
const InitAction = "Init"

// Recorder collects an implementation trace. It is safe for concurrent
// use, but meaningful traces come from deterministic harnesses — record
// from one goroutine at a time.
type Recorder struct {
	mu  sync.Mutex
	tr  *trace.Trace
	cur map[string]value.Value
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{tr: trace.New()} }

// Init records the initial state. Values are converted with
// value.FromGo (value.Value passes through).
func (r *Recorder) Init(state map[string]any) error {
	return r.record(InitAction, state, true)
}

// Step records one action: updates are merged over the previous state,
// so variables not mentioned are recorded unchanged (the deterministic
// harness only annotates what each action touches).
func (r *Recorder) Step(action string, updates map[string]any) error {
	return r.record(action, updates, false)
}

// InitState and StepState record a whole state snapshot taken from a Go
// struct via value.FromGo: exported fields become variables, honoring
// `tla:"name"` tags.
func (r *Recorder) InitState(state any) error {
	m, err := Snapshot(state)
	if err != nil {
		return err
	}
	return r.record(InitAction, anyMap(m), true)
}

func (r *Recorder) StepState(action string, state any) error {
	m, err := Snapshot(state)
	if err != nil {
		return err
	}
	return r.record(action, anyMap(m), false)
}

func anyMap(m map[string]value.Value) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (r *Recorder) record(action string, updates map[string]any, init bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if init {
		r.tr = trace.New()
		r.cur = map[string]value.Value{}
	} else if r.cur == nil {
		return fmt.Errorf("tracecheck: Step(%q) before Init", action)
	}
	next := make(map[string]value.Value, len(r.cur)+len(updates))
	for k, v := range r.cur {
		next[k] = v
	}
	for k, raw := range updates {
		v, err := value.FromGo(raw)
		if err != nil {
			return fmt.Errorf("tracecheck: variable %s: %w", k, err)
		}
		next[k] = v
	}
	r.cur = next
	snapshot := make(map[string]value.Value, len(next))
	for k, v := range next {
		snapshot[k] = v
	}
	r.tr.AddState(trace.State{
		Index:  len(r.tr.States) + 1,
		Action: action,
		Vars:   snapshot,
	})
	return nil
}

// Trace returns the recorded trace.
func (r *Recorder) Trace() *trace.Trace {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tr
}

// Len returns the number of recorded states.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tr.States)
}

// Snapshot converts a Go struct into a variable assignment using
// value.FromGo field conversion and `tla` tags.
func Snapshot(state any) (map[string]value.Value, error) {
	v, err := value.FromGo(state)
	if err != nil {
		return nil, err
	}
	rec, ok := v.(value.Record)
	if !ok {
		return nil, fmt.Errorf("tracecheck: snapshot needs a struct, got %T", state)
	}
	out := make(map[string]value.Value, len(rec.Fields))
	for _, f := range rec.Fields {
		out[f.Name] = f.V
	}
	return out, nil
}
