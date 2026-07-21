package value

import (
	"math/big"
	"testing"
)

func mustParse(t *testing.T, src string) Value {
	t.Helper()
	v, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return v
}

// roundTrip checks Parse(v.String()) == v.
func roundTrip(t *testing.T, src string) Value {
	t.Helper()
	v := mustParse(t, src)
	v2, err := Parse(v.String())
	if err != nil {
		t.Fatalf("reparse %q (from %q): %v", v.String(), src, err)
	}
	if !Equal(v, v2) {
		t.Errorf("round trip changed value: %s -> %s", v, v2)
	}
	return v
}

func TestParseAndPrint(t *testing.T) {
	cases := map[string]string{
		"TRUE":                       "TRUE",
		"42":                         "42",
		"-17":                        "-17",
		`"hello"`:                    `"hello"`,
		"mv1":                        "mv1",
		"{3, 1, 2}":                  "{1, 2, 3}", // canonical order
		"{1, 1, 2}":                  "{1, 2}",    // deduped
		"<<1, \"a\", TRUE>>":         `<<1, "a", TRUE>>`,
		"<<>>":                       "<<>>",
		"[b |-> 2, a |-> 1]":         "[a |-> 1, b |-> 2]",
		"(2 :> \"b\" @@ 1 :> \"a\")": `(1 :> "a" @@ 2 :> "b")`,
		"1..5":                       "1..5",
		"{<<1, 2>>, <<1, 1>>}":       "{<<1, 1>>, <<1, 2>>}",
		"\\b0110":                    "6",
		"\\hFF":                      "255",
	}
	for src, want := range cases {
		v := roundTrip(t, src)
		if got := v.String(); got != want {
			t.Errorf("Parse(%q).String() = %q, want %q", src, got, want)
		}
	}
}

func TestParseTLCStateValue(t *testing.T) {
	// Exactly as TLC prints a state variable in a counterexample.
	v := mustParse(t, `<<[n |-> 0, ok |-> TRUE], [n |-> 1, ok |-> FALSE]>>`)
	tup, ok := v.(Tuple)
	if !ok || len(tup.Elems) != 2 {
		t.Fatalf("got %#v", v)
	}
	rec := tup.Elems[1].(Record)
	if !Equal(rec.Fields[0].V, NewInt(1)) {
		t.Errorf("n field = %s", rec.Fields[0].V)
	}
}

func TestParseRejectsNonLiterals(t *testing.T) {
	for _, src := range []string{"x + 1", "{x \\in S : x}", "f[3]"} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) should fail", src)
		}
	}
}

func TestCompareTotalOrder(t *testing.T) {
	vals := []Value{
		Bool(false), Bool(true), NewInt(-1), NewInt(3), String("a"),
		ModelValue("m"), Interval{big.NewInt(1), big.NewInt(2)},
		NewSet(NewInt(1)), Tuple{[]Value{NewInt(1)}},
		NewRecord(RecField{"a", NewInt(1)}),
		NewFunc(FuncEntry{NewInt(1), NewInt(2)}),
	}
	for i, a := range vals {
		if Compare(a, a) != 0 {
			t.Errorf("Compare(%s, %s) != 0", a, a)
		}
		for _, b := range vals[i+1:] {
			if Compare(a, b) >= 0 || Compare(b, a) <= 0 {
				t.Errorf("ordering inconsistent between %s and %s", a, b)
			}
		}
	}
}

func TestITFRoundTrip(t *testing.T) {
	for _, src := range []string{
		"TRUE", "42", `"hi"`, "{1, 2}", "<<1, <<2, 3>>>>",
		"[a |-> 1, b |-> {2}]", `(1 :> "a" @@ 2 :> "b")`,
	} {
		v := mustParse(t, src)
		data, err := MarshalITF(v)
		if err != nil {
			t.Fatalf("MarshalITF(%s): %v", v, err)
		}
		v2, err := UnmarshalITF(data)
		if err != nil {
			t.Fatalf("UnmarshalITF(%s): %v", data, err)
		}
		if !Equal(v, v2) {
			t.Errorf("ITF round trip: %s -> %s -> %s", v, data, v2)
		}
	}
}

func TestITFBigInt(t *testing.T) {
	huge, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	v := Int{huge}
	data, err := MarshalITF(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"#bigint":"123456789012345678901234567890"}` {
		t.Errorf("big int encoding = %s", data)
	}
	v2, err := UnmarshalITF(data)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(v, v2) {
		t.Errorf("big int round trip failed: %s", v2)
	}
}

func TestITFIntervalExpands(t *testing.T) {
	v := mustParse(t, "1..3")
	data, err := MarshalITF(v)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := UnmarshalITF(data)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(v2, NewSet(NewInt(1), NewInt(2), NewInt(3))) {
		t.Errorf("interval ITF = %s -> %s", data, v2)
	}
}

func TestModelValueRoundTripsAsString(t *testing.T) {
	// ITF cannot distinguish model values from strings; decoding yields
	// String. Document the behavior.
	data, err := MarshalITF(ModelValue("m1"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := UnmarshalITF(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(String); !ok {
		t.Errorf("model value decoded as %T", v)
	}
}
