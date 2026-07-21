# tlacuilo

*Tlacuilo* (Nahuatl: **scribe**) is a pure-Go library for working with
[TLA+](https://lamport.azurewebsites.net/tla/tla.html): write specifications
programmatically, parse and format TLA+ source, generate TLC configurations,
run the TLC model checker, and consume its results — including typed
counterexample traces with [ITF](https://apalache-mc.org/docs/adr/015adr-trace.html)
JSON interchange.

```
go get github.com/aburan28/tlacuilo
```

No dependencies beyond the Go standard library. Running TLC additionally
requires Java and `tla2tools.jar` — `tlc.EnsureJar(ctx)` finds or
downloads it in one call (see [Running TLC](#running-tlc)).

**[The user guide (docs/GUIDE.md)](docs/GUIDE.md)** walks through every
feature with copy-paste examples, including how to wire spec checking and
trace validation into your own project's `go test` and CI; each package
also ships runnable godoc examples.

## Packages

| Package   | What it does |
|-----------|--------------|
| `tracecheck` | Validate Go implementations against TLA+ specs by trace checking (annotate actions, record under a deterministic harness, let TLC judge) |
| `builder` | Fluent construction of TLA+ specs from Go |
| `parser`  | Full-expression TLA+ parser (column-aware junction lists, the complete operator precedence table) |
| `ast`     | Syntax tree + canonical pretty-printer (`parse ∘ print` is a fixed point) |
| `scanner`, `token` | Column-tracking lexer and the operator tables from *Specifying Systems* |
| `cfg`     | TLC `.cfg` files: generate and parse |
| `tlc`     | Run TLC, stream its `-tool` output, get typed results and traces |
| `trace`   | Counterexample traces; ITF JSON import/export |
| `value`   | TLA+ values (sets, functions, records, big ints, …): parse TLC output, render TLA+, convert to/from ITF |

## Writing a spec from Go

```go
import (
    b "github.com/aburan28/tlacuilo/builder"
)

x := b.ID("x")
m := b.NewModule("Counter").Extends("Naturals")
m.Variables("x")
m.Define("Init", b.Eq(x, b.Num(0)))
m.Define("Next", b.Eq(b.Prime(x), b.Mod(b.Plus(x, b.Num(1)), b.Num(4))))
m.Define("Spec", b.Spec(b.ID("Init"), b.ID("Next"), x, b.WF(x, b.ID("Next"))))
m.Define("TypeOK", b.In(x, b.Range(b.Num(0), b.Num(3))))
fmt.Println(m)
```

produces

```tla
------------------------------ MODULE Counter -------------------------------
EXTENDS Naturals

VARIABLE x

Init == x = 0

Next == x' = (x + 1) % 4

Spec == /\ Init
        /\ [][Next]_x
        /\ WF_x(Next)

TypeOK == x \in 0..3

=============================================================================
```

Everything the builder emits is parseable by `parser` (and by SANY/TLC —
the test suite verifies both).

## Parsing and formatting TLA+

```go
m, err := parser.Parse(src)          // *ast.Module with positions
e, err := parser.ParseExpr("x' = x + 1")
fmt.Println(m)                       // canonical formatting
```

The parser covers the module language and the full constant/action/temporal
expression language: aligned `/\` and `\/` junction lists (the
column-sensitive part of TLA+), the complete operator table with precedence
*ranges* (conflicting unparenthesized mixes are rejected, as SANY does),
`EXCEPT` with `@`, quantifiers, `CHOOSE`, `LAMBDA`, `INSTANCE`/module
references, nested modules, and `WF_v`/`SF_v`/`[A]_v`/`<<A>>_v`.

Not supported (by design, like most tooling outside TLAPS): the TLA+2 proof
language and PlusCal (which lives in comments).

## Running TLC

```go
r, err := tlc.Check(ctx, tlc.Job{
    Module: m.AST(),
    Config: &cfg.Config{
        Specification: "Spec",
        Invariants:    []string{"TypeOK"},
    },
}, tlc.Options{})
if err != nil { ... }              // could not run/parse TLC
fmt.Println(r.Status)              // success | deadlock | safety violation | ...
fmt.Println(r.StatesGenerated, r.DistinctStates, r.Depth)
if r.Trace != nil {                // typed counterexample
    last := r.Trace.States[len(r.Trace.States)-1]
    fmt.Println(last.Vars["x"])    // a value.Value
    itf, _ := r.Trace.MarshalITF() // ITF JSON for other tools
}
```

`tlc.Check` writes the spec and config to a temp dir and runs
`java -cp tla2tools.jar tlc2.TLC -tool`; `tlc.Run` checks an existing
`.tla` file. The jar is located via `$TLA2TOOLS_JAR`, the working
directory, `~/.tlacuilo/`, or the system java directories;
`tlc.EnsureJar` finds it or downloads it from the official GitHub
releases into `~/.tlacuilo/` in one call.

Results are decoded from TLC's machine-readable tool protocol
(`@!@!@STARTMSG …` framing), and exit codes are mapped to statuses
(0 success, 10 assumption violation, 11 deadlock, 12 safety violation,
13 liveness violation). Counterexample states — including liveness lassos
and stuttering — are parsed into `value.Value` trees using the same TLA+
parser used for specs.

## Validating Go implementations (trace checking)

Fully automatic Go→TLA+ extraction hits state explosion fast (goroutine
interleavings, channel semantics). `tracecheck` implements the tractable
middle ground used by industrial trace-validation work: **annotate**
actions in your Go code, **record** only under a deterministic test
harness (never in production), and let TLC decide whether the recorded
behavior is one the spec allows. The annotations are `tla:"var"` struct
tags (the variable mapping) and `Recorder.StepState("Action", state)` calls
(the action mapping → refinement mapping in the generated trace spec).

```go
type controller struct {
    Desired int `tla:"desired"`
    Current int `tla:"current"`
}

rec := tracecheck.NewRecorder()
rec.InitState(c)
c.Desired = 3
rec.StepState("ChangeDesired", c)
for { // the reconcile loop under a deterministic harness
    act, ok := c.reconcile()
    if !ok { break }
    rec.StepState(act, c) // "ScaleUp" / "ScaleDown"
}

report, err := tracecheck.Validate(ctx, tracecheck.Spec{
    Module: "Reconciler",
    Vars:   []string{"desired", "current"},
    Actions: map[string]string{ // recorded action -> spec action
        "ChangeDesired": "ChangeDesired",
        "ScaleUp":       "ScaleUp",
        "ScaleDown":     "ScaleDown",
    },
    Constants: []cfg.Constant{{Name: "MaxReplicas", Value: "5"}},
}, rec.Trace(), reconcilerSpecSource, tlc.Options{})
// report.Conforms, or report.DivergedAt / report.FailedAction
```

The generated trace module embeds the recording as a constant sequence and
requires each step to satisfy the abstract action named by its annotation;
a step the spec cannot take leaves TLC with no successor, so divergence
surfaces as a TLC deadlock that `Validate` turns into "diverges at state
N (action X)". Unmentioned variables carry forward automatically, and
unmapped actions fall back to `[Next]_vars`.

This is exercised end-to-end against three Go tool shapes in
`tracecheck`'s tests, each with a buggy variant that must be caught: a
**Kubernetes-style reconcile controller** (scaling two replicas per step
is caught), a **bounded FIFO queue** (a LIFO pop is caught), and **lock
discipline** (a stolen lock is caught). Try the controller demo:

```
go run ./examples/reconciler        # conforms
go run ./examples/reconciler -bug   # divergence report, exit 1
```

**[examples/k8scontroller](examples/k8scontroller)** is the full
template for controllers: an embedded spec with model-checked safety
*and* liveness proofs, a controller-runtime-shaped reconciler, the
deterministic harness, a caught divergence, and the dedicated
[TLA+ proof CI workflow](.github/workflows/tla-proof.yml) that makes
the proofs a required check (no silent skips).

## Example

```
go run ./examples/diehard
```

builds the classic Die Hard water-jug puzzle with the builder and, when TLC
is available, lets the model checker "solve" it by violating the
`NotSolved` invariant, printing the 7-state solution trace and its ITF JSON.

## Design

See [docs/PLAN.md](docs/PLAN.md) for the research notes, architecture, and
roadmap (semantic checking, Apalache support, and trace validation are
planned follow-ups). The TLC output fixtures in `tlc/testdata` are real
captured `-tool` runs; integration tests execute TLC end-to-end and skip
automatically when Java or the jar is missing.
