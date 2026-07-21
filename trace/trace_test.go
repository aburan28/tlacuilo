package trace

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aburan28/tlacuilo/value"
)

func sample() *Trace {
	t := New()
	t.AddState(State{Index: 1, Action: "Initial predicate", Vars: map[string]value.Value{
		"x": value.NewInt(0),
		"q": value.Tuple{},
	}})
	t.AddState(State{Index: 2, Action: "Next", Vars: map[string]value.Value{
		"x": value.NewInt(1),
		"q": value.Tuple{Elems: []value.Value{value.String("a")}},
	}})
	t.Loop = 0
	return t
}

func TestITFRoundTrip(t *testing.T) {
	tr := sample()
	data, err := tr.MarshalITF()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if meta, ok := doc["#meta"].(map[string]any); !ok || meta["format"] != "ITF" {
		t.Errorf("#meta = %v", doc["#meta"])
	}
	tr2, err := UnmarshalITF(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr2.States) != 2 || tr2.Loop != 0 {
		t.Fatalf("decoded = %+v", tr2)
	}
	if !value.Equal(tr2.States[1].Vars["x"], value.NewInt(1)) {
		t.Errorf("x = %v", tr2.States[1].Vars["x"])
	}
	if tr2.States[0].Action != "Initial predicate" {
		t.Errorf("action = %q", tr2.States[0].Action)
	}
	if len(tr2.Vars) != 2 {
		t.Errorf("vars = %v", tr2.Vars)
	}
}

func TestString(t *testing.T) {
	out := sample().String()
	for _, want := range []string{"State 1", "/\\ x = 0", "Back to state 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("String() missing %q:\n%s", want, out)
		}
	}
}
