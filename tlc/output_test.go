package tlc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aburan28/tlacuilo/value"
)

// The testdata fixtures are real `tlc2.TLC -tool` output captured from
// TLC2 Version 2.15 running the specs described in each test.

func loadFixture(t *testing.T, name string, exitCode int) *Result {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	msgs, err := ParseToolOutput(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("no messages parsed")
	}
	return NewResult(msgs, exitCode)
}

func TestSuccessRun(t *testing.T) {
	r := loadFixture(t, "success.out", 0)
	if !r.Ok() || r.Err() != nil {
		t.Fatalf("status = %v, errors = %v", r.Status, r.Errors)
	}
	if r.StatesGenerated != 6 || r.DistinctStates != 5 || r.QueueStates != 0 {
		t.Errorf("stats = %d/%d/%d", r.StatesGenerated, r.DistinctStates, r.QueueStates)
	}
	if r.Depth != 5 {
		t.Errorf("depth = %d", r.Depth)
	}
	if r.Trace != nil {
		t.Error("success run should have no trace")
	}
	if r.Version == "" {
		t.Error("version not captured")
	}
}

func TestInvariantViolation(t *testing.T) {
	r := loadFixture(t, "violation.out", 12)
	if r.Status != StatusSafetyViolation {
		t.Fatalf("status = %v", r.Status)
	}
	if len(r.Errors) == 0 || r.Errors[0].Code != CodeInvariantViolated {
		t.Fatalf("errors = %v", r.Errors)
	}
	tr := r.Trace
	if tr == nil {
		t.Fatal("no trace")
	}
	if len(tr.States) != 4 {
		t.Fatalf("trace has %d states, want 4", len(tr.States))
	}
	if tr.States[0].Action != "Initial predicate" {
		t.Errorf("state 1 action = %q", tr.States[0].Action)
	}
	if got := tr.States[3].Vars["x"]; !value.Equal(got, value.NewInt(3)) {
		t.Errorf("final x = %v", got)
	}
	// hist is a sequence of records exactly as TLC printed it.
	hist := tr.States[3].Vars["hist"].(value.Tuple)
	if len(hist.Elems) != 3 {
		t.Fatalf("hist = %v", hist)
	}
	rec := hist.Elems[2].(value.Record)
	okField := rec.Fields[1]
	if okField.Name != "ok" || !value.Equal(okField.V, value.Bool(false)) {
		t.Errorf("last record = %v", rec)
	}
	// The trace exports to ITF and back.
	data, err := tr.MarshalITF()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty ITF")
	}
}

func TestDeadlock(t *testing.T) {
	r := loadFixture(t, "deadlock.out", 11)
	if r.Status != StatusDeadlock {
		t.Fatalf("status = %v", r.Status)
	}
	if r.Trace == nil || len(r.Trace.States) != 3 {
		t.Fatalf("trace = %+v", r.Trace)
	}
	if got := r.Trace.States[2].Vars["x"]; !value.Equal(got, value.NewInt(2)) {
		t.Errorf("deadlocked x = %v", got)
	}
}

func TestLivenessStuttering(t *testing.T) {
	r := loadFixture(t, "liveness.out", 13)
	if r.Status != StatusLivenessViolation {
		t.Fatalf("status = %v", r.Status)
	}
	tr := r.Trace
	if tr == nil || len(tr.States) != 4 {
		t.Fatalf("trace = %+v", tr)
	}
	if !tr.Stuttering {
		t.Error("stuttering not detected")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == CodeTemporalViolated {
			found = true
		}
	}
	if !found {
		t.Errorf("temporal violation error not captured: %v", r.Errors)
	}
}

func TestStatusMapping(t *testing.T) {
	cases := map[int]Status{
		0: StatusSuccess, 10: StatusAssumptionViolation, 11: StatusDeadlock,
		12: StatusSafetyViolation, 13: StatusLivenessViolation,
		150: StatusFailure, 151: StatusFailure, 255: StatusFailure,
	}
	for code, want := range cases {
		if got := StatusFromExitCode(code); got != want {
			t.Errorf("exit %d -> %v, want %v", code, got, want)
		}
	}
}

func TestParseTraceStateSingleVar(t *testing.T) {
	s, err := parseTraceState("3: <Next line 5, col 9 to line 5, col 27 of module Dead>\nx = 2\n")
	if err != nil {
		t.Fatal(err)
	}
	if s.Index != 3 || !value.Equal(s.Vars["x"], value.NewInt(2)) {
		t.Errorf("state = %+v", s)
	}
}
