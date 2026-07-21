package trace_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/trace"
	"github.com/aburan28/tlacuilo/value"
)

func ExampleTrace_MarshalITF() {
	tr := trace.New()
	tr.AddState(trace.State{Index: 1, Action: "Init",
		Vars: map[string]value.Value{"x": value.NewInt(0)}})
	tr.AddState(trace.State{Index: 2, Action: "Inc",
		Vars: map[string]value.Value{"x": value.NewInt(1)}})
	data, err := tr.MarshalITF()
	if err != nil {
		panic(err)
	}
	back, err := trace.UnmarshalITF(data)
	if err != nil {
		panic(err)
	}
	fmt.Println(back.Vars, len(back.States), back.States[1].Vars["x"])
	// Output:
	// [x] 2 1
}
