package tracecheck

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aburan28/tlacuilo/parser"
	"github.com/aburan28/tlacuilo/tlc"
	"github.com/aburan28/tlacuilo/value"
)

func TestRecorderCarryForward(t *testing.T) {
	r := NewRecorder()
	if err := r.Step("X", nil); err == nil {
		t.Fatal("Step before Init should fail")
	}
	if err := r.Init(map[string]any{"x": 0, "y": []int{1}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Step("Bump", map[string]any{"x": 1}); err != nil {
		t.Fatal(err)
	}
	tr := r.Trace()
	if len(tr.States) != 2 {
		t.Fatalf("states = %d", len(tr.States))
	}
	// y carried forward into state 2.
	if !value.Equal(tr.States[1].Vars["y"], value.Tuple{Elems: []value.Value{value.NewInt(1)}}) {
		t.Errorf("y not carried forward: %v", tr.States[1].Vars["y"])
	}
	if tr.States[0].Action != InitAction || tr.States[1].Action != "Bump" {
		t.Errorf("actions = %q, %q", tr.States[0].Action, tr.States[1].Action)
	}
}

func TestGenModuleParsesAndShape(t *testing.T) {
	r := NewRecorder()
	_ = r.Init(map[string]any{"x": 0})
	_ = r.Step("Inc", map[string]any{"x": 1})
	_ = r.Step("Inc", map[string]any{"x": 2})

	s := Spec{
		Module:  "Counter",
		Vars:    []string{"x"},
		Actions: map[string]string{"Inc": "Inc"},
	}
	m, err := GenModule(s, r.Trace())
	if err != nil {
		t.Fatal(err)
	}
	src := m.String()
	if _, err := parser.Parse(src); err != nil {
		t.Fatalf("generated module does not parse: %v\n%s", err, src)
	}
	for _, want := range []string{
		"EXTENDS Naturals, Sequences, Counter",
		"VARIABLE TraceIdx",
		`[act |-> "Init", x |-> 0]`,
		`[act |-> "Inc", x |-> 2]`,
		`CASE Trace[k].act = "Inc" -> Inc`,
		"[] OTHER -> [Next]_<<x>>",
		"TraceInit == /\\ TraceIdx = 1",
		"/\\ TraceIdx = Len(Trace)",
		"/\\ UNCHANGED TraceVars",
		"TraceSpec == /\\ TraceInit",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated module missing %q:\n%s", want, src)
		}
	}
	cfgText := GenConfig(s).Format()
	if !strings.Contains(cfgText, "SPECIFICATION TraceSpec") {
		t.Errorf("config = %s", cfgText)
	}
}

func TestGenModuleRejectsCollisions(t *testing.T) {
	r := NewRecorder()
	_ = r.Init(map[string]any{"act": 1})
	if _, err := GenModule(Spec{Module: "M", Vars: []string{"act"}}, r.Trace()); err == nil {
		t.Error("variable named act should be rejected")
	}
	r2 := NewRecorder()
	_ = r2.Init(map[string]any{"TraceIdx": 1})
	if _, err := GenModule(Spec{Module: "M", Vars: []string{"TraceIdx"}}, r2.Trace()); err == nil {
		t.Error("variable named TraceIdx should be rejected")
	}
}

// requireTLC skips unless java and tla2tools.jar are available; shared by
// the per-tool-type validation tests.
func requireTLC(t *testing.T) tlc.Options {
	t.Helper()
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java not installed")
	}
	jar, err := tlc.FindJar()
	if err != nil {
		t.Skip("tla2tools.jar not found (set TLA2TOOLS_JAR to enable integration tests)")
	}
	return tlc.Options{JarPath: jar}
}

func validateT(t *testing.T, s Spec, r *Recorder, specSrc string) *Report {
	t.Helper()
	opts := requireTLC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rep, err := Validate(ctx, s, r.Trace(), specSrc, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}
