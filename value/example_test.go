package value_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/value"
)

func ExampleParse() {
	// The syntax TLC uses when printing counterexample states.
	v, err := value.Parse(`<<[n |-> 1], (1 :> "a" @@ 2 :> "b")>>`)
	if err != nil {
		panic(err)
	}
	fmt.Println(v)
	// Output:
	// <<[n |-> 1], (1 :> "a" @@ 2 :> "b")>>
}

func ExampleFromGo() {
	type Pod struct {
		Name  string `tla:"name"`
		Ready bool   `tla:"ready"`
	}
	v, err := value.FromGo(map[string][]Pod{
		"web": {{Name: "web-0", Ready: true}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(v)
	// Output:
	// ("web" :> <<[name |-> "web-0", ready |-> TRUE]>>)
}

func ExampleMarshalITF() {
	v, _ := value.Parse(`{1, <<2, 3>>}`)
	data, err := value.MarshalITF(v)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
	// Output:
	// {"#set":[1,{"#tup":[2,3]}]}
}
