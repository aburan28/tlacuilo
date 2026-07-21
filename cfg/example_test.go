package cfg_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/cfg"
)

func ExampleConfig_Format() {
	c := &cfg.Config{
		Specification: "Spec",
		Invariants:    []string{"TypeOK"},
		Constants: []cfg.Constant{
			{Name: "N", Value: "3"},
			{Name: "Sort", Replacement: "FastSort"},
		},
	}
	fmt.Print(c.Format())
	// Output:
	// SPECIFICATION Spec
	// CONSTANTS
	//     N = 3
	//     Sort <- FastSort
	// INVARIANT TypeOK
}
