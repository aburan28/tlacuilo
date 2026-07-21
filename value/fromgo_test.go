package value

import (
	"math/big"
	"testing"
)

func TestFromGoScalars(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{true, "TRUE"},
		{42, "42"},
		{int8(-3), "-3"},
		{uint64(1 << 40), "1099511627776"},
		{"hi", `"hi"`},
		{nil, "NULL"},
		{big.NewInt(7), "7"},
		{[]int{1, 2, 3}, "<<1, 2, 3>>"},
		{[]int(nil), "<<>>"},
		{[2]string{"a", "b"}, `<<"a", "b">>`},
		{map[string]int{"b": 2, "a": 1}, `("a" :> 1 @@ "b" :> 2)`},
		{NewInt(9), "9"},
	}
	for _, c := range cases {
		v, err := FromGo(c.in)
		if err != nil {
			t.Errorf("FromGo(%v): %v", c.in, err)
			continue
		}
		if v.String() != c.want {
			t.Errorf("FromGo(%v) = %s, want %s", c.in, v, c.want)
		}
	}
}

func TestFromGoStruct(t *testing.T) {
	type Pod struct {
		Name    string `tla:"name"`
		Ready   bool
		secret  int    //nolint:unused // must be skipped
		Ignored string `tla:"-"`
	}
	type State struct {
		Desired int    `tla:"desired"`
		Pods    []Pod  `tla:"pods"`
		Owner   *State `tla:"owner"`
	}
	v, err := FromGo(State{Desired: 2, Pods: []Pod{{Name: "p1", Ready: true, Ignored: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `[desired |-> 2, owner |-> NULL, pods |-> <<[Ready |-> TRUE, name |-> "p1"]>>]`
	if v.String() != want {
		t.Errorf("got  %s\nwant %s", v, want)
	}
}

func TestFromGoRejectsUnrepresentable(t *testing.T) {
	for _, in := range []any{3.14, make(chan int), func() {}} {
		if _, err := FromGo(in); err == nil {
			t.Errorf("FromGo(%T) should fail", in)
		}
	}
}
