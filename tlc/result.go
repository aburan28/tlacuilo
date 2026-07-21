package tlc

import (
	"fmt"
	"time"

	"github.com/aburan28/tlacuilo/trace"
)

// Status classifies the outcome of a TLC run, derived from its exit
// status (verified against TLC's tlc2.output.EC.ExitStatus values).
type Status int

const (
	StatusUnknown Status = iota
	StatusSuccess
	StatusAssumptionViolation // exit 10
	StatusDeadlock            // exit 11
	StatusSafetyViolation     // exit 12: invariant or action property
	StatusLivenessViolation   // exit 13: temporal property
	StatusFailure             // parse/config/system errors
)

func (s Status) String() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusAssumptionViolation:
		return "assumption violation"
	case StatusDeadlock:
		return "deadlock"
	case StatusSafetyViolation:
		return "safety violation"
	case StatusLivenessViolation:
		return "liveness violation"
	case StatusFailure:
		return "failure"
	}
	return "unknown"
}

// StatusFromExitCode maps a TLC exit code to a Status.
func StatusFromExitCode(code int) Status {
	switch code {
	case 0:
		return StatusSuccess
	case 10:
		return StatusAssumptionViolation
	case 11:
		return StatusDeadlock
	case 12:
		return StatusSafetyViolation
	case 13:
		return StatusLivenessViolation
	}
	if code >= 150 {
		return StatusFailure
	}
	return StatusUnknown
}

// Result is the outcome of a TLC run.
type Result struct {
	Status   Status
	ExitCode int
	Duration time.Duration

	Version         string
	StatesGenerated int64
	DistinctStates  int64
	QueueStates     int64
	Depth           int

	// Errors are TLC's error-severity messages; Warnings its warnings
	// plus any output this library could not interpret.
	Errors   []TLCError
	Warnings []string

	// Trace is the counterexample behavior, when TLC printed one.
	Trace *trace.Trace

	// Messages is the full framed message stream.
	Messages []Message
}

// Ok reports whether the model check passed.
func (r *Result) Ok() bool { return r.Status == StatusSuccess }

// Err returns nil for successful runs and a descriptive error otherwise.
func (r *Result) Err() error {
	if r.Ok() {
		return nil
	}
	if len(r.Errors) > 0 {
		return fmt.Errorf("tlc: %s: %s", r.Status, r.Errors[0].Message)
	}
	return fmt.Errorf("tlc: %s (exit code %d)", r.Status, r.ExitCode)
}

// NewResult interprets a message stream and exit code as a Result.
func NewResult(msgs []Message, exitCode int) *Result {
	r := &Result{
		Status:   StatusFromExitCode(exitCode),
		ExitCode: exitCode,
		Messages: msgs,
	}
	r.interpret(msgs)
	if r.Status == StatusUnknown && len(r.Errors) > 0 {
		r.Status = StatusFailure
	}
	return r
}
