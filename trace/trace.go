// Package trace represents TLC/Apalache execution traces
// (counterexamples) and converts them to and from the ITF JSON format
// (Apalache ADR-015).
package trace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/aburan28/tlacuilo/value"
)

// State is one state of a trace: an assignment of values to variables.
type State struct {
	// Index is the 1-based state number as TLC prints it.
	Index int
	// Action describes the transition that produced this state, e.g.
	// "Initial predicate" or "Next line 6, col 9 to line 7, col 56 of
	// module Counter".
	Action string
	Vars   map[string]value.Value
}

// Trace is a finite behavior, optionally with lasso or stuttering
// information for liveness counterexamples.
type Trace struct {
	// Vars lists variable names in display order.
	Vars   []string
	States []State
	// Loop is the 0-based index of the state the behavior returns to
	// after the last state (a liveness lasso), or -1.
	Loop int
	// Stuttering is set when the behavior ends in infinite stuttering.
	Stuttering bool
}

// New returns an empty trace.
func New() *Trace { return &Trace{Loop: -1} }

// AddState appends a state, extending Vars with newly seen variables.
func (t *Trace) AddState(s State) {
	seen := make(map[string]bool, len(t.Vars))
	for _, v := range t.Vars {
		seen[v] = true
	}
	var added []string
	for name := range s.Vars {
		if !seen[name] {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	t.Vars = append(t.Vars, added...)
	t.States = append(t.States, s)
}

// String renders the trace in TLC's textual style.
func (t *Trace) String() string {
	var b bytes.Buffer
	for i, s := range t.States {
		fmt.Fprintf(&b, "State %d: <%s>\n", i+1, s.Action)
		for _, name := range t.Vars {
			if v, ok := s.Vars[name]; ok {
				fmt.Fprintf(&b, "/\\ %s = %s\n", name, v)
			}
		}
	}
	if t.Loop >= 0 && t.Loop < len(t.States) {
		fmt.Fprintf(&b, "Back to state %d\n", t.Loop+1)
	}
	if t.Stuttering {
		b.WriteString("Stuttering\n")
	}
	return b.String()
}

// itfDocument mirrors the ITF trace JSON layout.
type itfDocument struct {
	Meta   map[string]any   `json:"#meta,omitempty"`
	Params []string         `json:"params,omitempty"`
	Vars   []string         `json:"vars"`
	States []map[string]any `json:"states"`
	Loop   *int             `json:"loop,omitempty"`
}

// MarshalITF renders the trace as ITF JSON.
func (t *Trace) MarshalITF() ([]byte, error) {
	doc := itfDocument{
		Meta: map[string]any{
			"format":             "ITF",
			"format-description": "https://apalache-mc.org/docs/adr/015adr-trace.html",
			"description":        "Trace produced by tlacuilo",
		},
		Vars: t.Vars,
	}
	if doc.Vars == nil {
		doc.Vars = []string{}
	}
	for _, s := range t.States {
		st := make(map[string]any, len(s.Vars)+1)
		meta := map[string]any{"index": s.Index}
		if s.Action != "" {
			meta["action"] = s.Action
		}
		st["#meta"] = meta
		for name, v := range s.Vars {
			enc, err := value.ToITF(v)
			if err != nil {
				return nil, fmt.Errorf("state %d, variable %s: %w", s.Index, name, err)
			}
			st[name] = enc
		}
		doc.States = append(doc.States, st)
	}
	if doc.States == nil {
		doc.States = []map[string]any{}
	}
	if t.Loop >= 0 {
		l := t.Loop
		doc.Loop = &l
	}
	return json.MarshalIndent(doc, "", "  ")
}

// UnmarshalITF parses an ITF JSON trace.
func UnmarshalITF(data []byte) (*Trace, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc itfDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	t := New()
	t.Vars = doc.Vars
	for i, st := range doc.States {
		s := State{Index: i + 1, Vars: map[string]value.Value{}}
		if m, ok := st["#meta"].(map[string]any); ok {
			if idx, ok := m["index"].(json.Number); ok {
				if n, err := idx.Int64(); err == nil {
					s.Index = int(n)
				}
			}
			if a, ok := m["action"].(string); ok {
				s.Action = a
			}
		}
		for name, raw := range st {
			if name == "#meta" {
				continue
			}
			v, err := value.FromITF(raw)
			if err != nil {
				return nil, fmt.Errorf("state %d, variable %s: %w", i+1, name, err)
			}
			s.Vars[name] = v
		}
		t.States = append(t.States, s)
	}
	if doc.Loop != nil {
		t.Loop = *doc.Loop
	}
	return t, nil
}
