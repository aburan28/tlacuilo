package cfg

import (
	"strings"
	"testing"
)

func TestFormatBasic(t *testing.T) {
	f := false
	c := &Config{
		Specification: "Spec",
		Invariants:    []string{"TypeOK", "Safety"},
		Properties:    []string{"Termination"},
		Constants: []Constant{
			{Name: "N", Value: "3"},
			{Name: "Data", Value: "{d1, d2}"},
			{Name: "Null", Value: "Null"},
			{Name: "Op", Replacement: "MyOp"},
		},
		Symmetry:      "Perms",
		Constraints:   []string{"StateBound"},
		CheckDeadlock: &f,
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	out := c.Format()
	for _, want := range []string{
		"SPECIFICATION Spec",
		"INVARIANT TypeOK",
		"INVARIANT Safety",
		"PROPERTY Termination",
		"N = 3",
		"Data = {d1, d2}",
		"Null = Null",
		"Op <- MyOp",
		"SYMMETRY Perms",
		"CONSTRAINT StateBound",
		"CHECK_DEADLOCK FALSE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Format() missing %q:\n%s", want, out)
		}
	}
}

func TestValidate(t *testing.T) {
	c := &Config{Specification: "Spec", Init: "Init"}
	if err := c.Validate(); err == nil {
		t.Error("SPECIFICATION+INIT should be invalid")
	}
	c = &Config{Init: "Init"}
	if err := c.Validate(); err == nil {
		t.Error("INIT without NEXT should be invalid")
	}
}

func TestParseRoundTrip(t *testing.T) {
	f := false
	c := &Config{
		Init: "Init",
		Next: "Next",
		Constants: []Constant{
			{Name: "N", Value: "3"},
			{Name: "S", Value: `{"a", "b"}`},
			{Name: "M", Value: "m1"},
			{Name: "F", Replacement: "Impl"},
		},
		Invariants:    []string{"Inv"},
		Properties:    []string{"Live"},
		CheckDeadlock: &f,
	}
	out := c.Format()
	c2, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse:\n%s\n%v", out, err)
	}
	if c2.Format() != out {
		t.Errorf("round trip differs:\n%s\n----\n%s", out, c2.Format())
	}
}

func TestParseRealWorld(t *testing.T) {
	src := `
\* comment line
SPECIFICATION Spec
CONSTANTS
    Data = {d1, d2}
    msgQLen = 2
    Null = Null
    Sort <- SortImpl
INVARIANTS Inv TypeInv
PROPERTY Termination
CONSTRAINT SeqConstraint
CHECK_DEADLOCK TRUE
`
	c, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if c.Specification != "Spec" {
		t.Errorf("spec = %q", c.Specification)
	}
	if len(c.Constants) != 4 {
		t.Fatalf("constants = %+v", c.Constants)
	}
	if c.Constants[0].Value != "{d1, d2}" {
		t.Errorf("Data = %q", c.Constants[0].Value)
	}
	if c.Constants[3].Replacement != "SortImpl" {
		t.Errorf("Sort <- %q", c.Constants[3].Replacement)
	}
	if len(c.Invariants) != 2 || c.Invariants[1] != "TypeInv" {
		t.Errorf("invariants = %v", c.Invariants)
	}
	if c.CheckDeadlock == nil || !*c.CheckDeadlock {
		t.Error("CHECK_DEADLOCK TRUE not parsed")
	}
}
