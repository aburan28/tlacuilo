package tlc_test

import (
	"context"
	"fmt"

	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/tlc"
)

// ExampleCheck model-checks an in-memory spec end to end. It needs Java
// and tla2tools.jar, so it is compiled but not run by go test.
func ExampleCheck() {
	ctx := context.Background()
	if _, err := tlc.EnsureJar(ctx); err != nil {
		panic(err)
	}
	r, err := tlc.Check(ctx, tlc.Job{
		Source: `---- MODULE Clock ----
EXTENDS Naturals
VARIABLE h
Init == h = 0
Next == h' = (h + 1) % 12
TypeOK == h \in 0..11
====
`,
		Config: &cfg.Config{Init: "Init", Next: "Next", Invariants: []string{"TypeOK"}},
	}, tlc.Options{DisableDeadlockCheck: true})
	if err != nil {
		panic(err) // mechanical failure; verdicts are in r
	}
	fmt.Println(r.Status, r.DistinctStates)
	if r.Trace != nil {
		fmt.Println(r.Trace) // counterexample, when there is one
	}
}
