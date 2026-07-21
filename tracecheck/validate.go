package tracecheck

import (
	"context"
	"fmt"

	"github.com/aburan28/tlacuilo/parser"
	"github.com/aburan28/tlacuilo/tlc"
	"github.com/aburan28/tlacuilo/trace"
	"github.com/aburan28/tlacuilo/value"
)

// Report is the outcome of validating a recorded trace against a spec.
type Report struct {
	// Conforms is true when every recorded step is a behavior the
	// abstract spec allows.
	Conforms bool
	// DivergedAt is the 1-based index of the first recorded state the
	// spec could not reach (0 when Conforms or undetermined).
	DivergedAt int
	// FailedAction is the recorded action annotation of the diverging
	// step.
	FailedAction string
	// Result is the underlying TLC result.
	Result *tlc.Result
}

func (r *Report) String() string {
	if r.Conforms {
		return "trace conforms to the specification"
	}
	if r.DivergedAt > 0 {
		return fmt.Sprintf("trace diverges from the specification at state %d (action %q)",
			r.DivergedAt, r.FailedAction)
	}
	return fmt.Sprintf("trace validation failed: %s", r.Result.Status)
}

// Validate generates the trace module for tr, model-checks it with TLC
// against the abstract spec source, and interprets the outcome.
//
// The returned error is non-nil only for mechanical failures (generation,
// running TLC); a diverging trace is reported via Report.Conforms.
func Validate(ctx context.Context, s Spec, tr *trace.Trace, specSource string, opts tlc.Options) (*Report, error) {
	abstract, err := parser.Parse(specSource)
	if err != nil {
		return nil, fmt.Errorf("tracecheck: abstract spec does not parse: %w", err)
	}
	if abstract.Name != s.Module {
		return nil, fmt.Errorf("tracecheck: spec source is module %q, but Spec.Module is %q",
			abstract.Name, s.Module)
	}
	m, err := GenModule(s, tr)
	if err != nil {
		return nil, err
	}
	// Divergence is detected AS a TLC deadlock (the generated trace spec
	// has no successor exactly when the spec cannot take a recorded
	// step), so deadlock checking must stay on no matter what the caller
	// put in opts.
	opts.DisableDeadlockCheck = false
	res, err := tlc.Check(ctx, tlc.Job{
		Module:     m,
		Config:     GenConfig(s),
		AuxModules: map[string]string{s.Module: specSource},
	}, opts)
	if err != nil {
		return nil, err
	}
	report := &Report{Result: res}
	switch res.Status {
	case tlc.StatusSuccess:
		report.Conforms = true
	case tlc.StatusDeadlock:
		// TLC's deadlock trace ends in the last state the spec could
		// reach; its TraceIdx names the last matched recorded state, so
		// the diverging one is the next.
		if res.Trace != nil && len(res.Trace.States) > 0 {
			last := res.Trace.States[len(res.Trace.States)-1]
			if iv, ok := last.Vars["TraceIdx"].(value.Int); ok && iv.X.IsInt64() {
				k := int(iv.X.Int64())
				if k >= 0 && k < len(tr.States) {
					report.DivergedAt = k + 1
					report.FailedAction = tr.States[k].Action
				}
			}
		}
	}
	return report, nil
}
