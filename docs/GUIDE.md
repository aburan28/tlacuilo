# Using tlacuilo in your Go project

This guide covers every feature of tlacuilo with copy-paste examples,
oriented around embedding it in your own Go library or service — most
commonly as part of your test suite. For the design rationale see
[PLAN.md](PLAN.md); for API reference see the godoc of each package
(every package ships runnable examples).

- [Install and setup](#install-and-setup)
- [Five-minute tour](#five-minute-tour)
- [Writing specs from Go (builder)](#writing-specs-from-go-builder)
- [Parsing and formatting TLA+](#parsing-and-formatting-tla)
- [TLC configurations (cfg)](#tlc-configurations-cfg)
- [Running TLC (tlc)](#running-tlc-tlc)
- [Values, traces, and ITF (value, trace)](#values-traces-and-itf-value-trace)
- [Validating your implementation against a spec (tracecheck)](#validating-your-implementation-against-a-spec-tracecheck)
- [Recipes for your own repo](#recipes-for-your-own-repo)
- [Limitations](#limitations)

## Install and setup

```
go get github.com/aburan28/tlacuilo
```

The library itself is pure Go with no dependencies. Only actually
*running* TLC needs Java (11+) and `tla2tools.jar`. One call handles jar
setup — it looks in `$TLA2TOOLS_JAR`, the working directory,
`~/.tlacuilo/`, and the system java directories, and downloads from the
official TLA+ GitHub releases if missing:

```go
jar, err := tlc.EnsureJar(ctx) // find or download; cached in ~/.tlacuilo
```

Everything that doesn't invoke TLC — building, parsing, formatting,
configs, values, traces, trace-spec generation — works without Java.

## Five-minute tour

Build a spec, model-check it, read the counterexample:

```go
package main

import (
    "context"
    "fmt"

    b "github.com/aburan28/tlacuilo/builder"
    "github.com/aburan28/tlacuilo/cfg"
    "github.com/aburan28/tlacuilo/tlc"
)

func main() {
    x := b.ID("x")
    m := b.NewModule("Counter").Extends("Naturals")
    m.Variables("x")
    m.Define("Init", b.Eq(x, b.Num(0)))
    m.Define("Next", b.Eq(b.Prime(x), b.Plus(x, b.Num(1))))
    m.Define("Spec", b.Spec(b.ID("Init"), b.ID("Next"), x))
    m.Define("Small", b.Lt(x, b.Num(3))) // deliberately violated

    ctx := context.Background()
    if _, err := tlc.EnsureJar(ctx); err != nil {
        panic(err)
    }
    r, err := tlc.Check(ctx, tlc.Job{
        Module: m.AST(),
        Config: &cfg.Config{Specification: "Spec", Invariants: []string{"Small"}},
    }, tlc.Options{DisableDeadlockCheck: true})
    if err != nil {
        panic(err) // could not run TLC at all
    }
    fmt.Println(r.Status) // "safety violation"
    for _, s := range r.Trace.States {
        fmt.Println(s.Index, s.Vars["x"]) // 1 0, 2 1, 3 2, 4 3
    }
}
```

## Writing specs from Go (builder)

`builder.Module` accumulates module units; package-level helpers build
expressions. Everything the builder emits is formatted, parseable TLA+
(round-tripped through the parser and validated against SANY/TLC in the
test suite).

```go
m := b.NewModule("Registry").Extends("Naturals", "Sequences")
m.Constants("Capacity")
m.Variables("entries", "count")
m.Define("Init", b.And(
    b.Eq(b.ID("entries"), b.SetOf()),
    b.Eq(b.ID("count"), b.Num(0)),
))
m.DefineOp("Add", []string{"e"}, b.And(
    b.Lt(b.ID("count"), b.ID("Capacity")),
    b.Eq(b.Prime(b.ID("entries")), b.Union(b.ID("entries"), b.SetOf(b.ID("e")))),
    b.Eq(b.Prime(b.ID("count")), b.Plus(b.ID("count"), b.Num(1))),
))
src := m.String()      // formatted TLA+ source
module := m.AST()      // or the *ast.Module for further manipulation
```

Module-level methods: `Extends`, `Constants`, `Variables`, `Define`,
`DefineOp` (parameters), `DefineFn` (function definitions `f[x \in S] ==`),
`Assume`, `Theorem`, `Separator`, and `Add(ast.Unit)` as the escape hatch
for anything else (LOCAL, INSTANCE, RECURSIVE, nested modules).

Expression helpers by area:

| Area | Helpers |
|------|---------|
| Atoms | `ID`, `Num`, `Str`, `True`, `False`, `Apply(name, args...)` |
| Logic | `And`, `Or` (aligned junction lists), `Not`, `Implies`, `Equiv` |
| Comparison | `Eq`, `Neq`, `Lt`, `Le`, `Gt`, `Ge` |
| Arithmetic | `Plus`, `Minus`, `Mul`, `Div` (`\div`), `Mod`, `Range` (`..`) |
| Sets | `SetOf`, `In`, `NotIn`, `Subseteq`, `Union`, `Intersect`, `SetMinus`, `Powerset`, `BigUnion`, `Domain`, `Filter`, `MapSet`, `Times` |
| Functions/records/tuples | `Bind`, `Fn`, `FnApp`, `FuncSet`, `TupleOf`, `Rec`, `RecSet`, `Field`, `Dot`, `ExceptIdx`, `ExceptField`, `AtOld` |
| Control | `IfThenElse`, `Forall`, `Exists`, `Choose` |
| Actions/temporal | `Prime`, `Unchanged`, `Always`, `Eventually`, `LeadsTo`, `Enabled`, `BoxAction` (`[A]_v`), `AngleAction` (`<<A>>_v`), `WF`, `SF`, `Spec` |

`b.Spec(init, next, vars, fairness...)` builds the standard
`Init /\ [][Next]_vars /\ ...` behavior formula. `And`/`Or` render as
aligned bullet lists — the idiomatic TLA+ style:

```
Init == /\ entries = {}
        /\ count = 0
```

Anything the helpers don't cover can be built from `ast` nodes directly
and mixed freely with builder output (see the `LET` expressions in
`examples/diehard`).

## Parsing and formatting TLA+

```go
m, err := parser.Parse(src)          // *ast.Module
e, err := parser.ParseExpr("[f EXCEPT ![k] = @ + 1]")

formatted := m.String()              // canonical formatting
one := ast.ExprString(e)             // single expression
```

The parser covers the module language and the full
constant/action/temporal expression language with SANY-compatible rules:

- **Junction lists** are column-sensitive, exactly as in SANY: bullets at
  one column form a list; every token of an item lies right of its
  bullet; a dedented token ends the list.
- **Precedence ranges** from *Specifying Systems*: `a \cup b \cap c` or
  `a = b = c` are rejected with "add parentheses", as SANY rejects them.
- Errors carry positions (`line:col`).

Formatting is canonical rather than byte-preserving: `parse → print` is a
fixed point, junctions align, parentheses appear only where precedence
requires. Use it as a `tla-fmt`:

```go
func Format(src string) (string, error) {
    m, err := parser.Parse(src)
    if err != nil {
        return "", err
    }
    return m.String(), nil
}
```

## TLC configurations (cfg)

`cfg.Config` covers the full keyword set — `SPECIFICATION` or
`INIT`/`NEXT`, `INVARIANT`, `PROPERTY`, `CONSTANT` (both `=` value
assignment and `<-` operator replacement), `SYMMETRY`, `VIEW`,
`CONSTRAINT`, `ACTION_CONSTRAINT`, `CHECK_DEADLOCK`, `ALIAS`,
`POSTCONDITION`:

```go
c := &cfg.Config{
    Specification: "Spec",
    Invariants:    []string{"TypeOK", "Safety"},
    Properties:    []string{"Termination"},
    Constants: []cfg.Constant{
        {Name: "N", Value: "3"},
        {Name: "Data", Value: `{"a", "b"}`},
        {Name: "Null", Value: "Null"},        // model value
        {Name: "Sort", Replacement: "FastSort"}, // Sort <- FastSort
    },
}
text := c.Format()            // .cfg file text
back, err := cfg.Parse(text)  // and back
```

`Config.Validate` catches the mutually-exclusive-sections mistakes TLC
would reject.

## Running TLC (tlc)

Two entry points:

- `tlc.Run(ctx, "path/to/Spec.tla", opts)` — check an existing file
  (config defaults to the sibling `.cfg`).
- `tlc.Check(ctx, tlc.Job{...}, opts)` — self-contained: give it a
  `*ast.Module` (or `Source` string), a `*cfg.Config`, and optional
  `AuxModules`; it writes a temp dir, runs, and cleans up.

Options (zero value is sensible):

| Field | Effect |
|-------|--------|
| `JarPath`, `JavaPath`, `JavaOpts` | toolchain overrides (defaults: discovery, `java`, parallel GC) |
| `Workers` | `-workers N`; negative means `auto` (needs a current TLC — 2.15-era jars reject `auto` and the run comes back `Status: failure`) |
| `DisableDeadlockCheck` | passes `-deadlock` (TLC's inverted flag: it *disables* the check) |
| `Simulate`, `SimulateTraces`, `Depth`, `Seed` | simulation mode |
| `MetaDir`, `ConfigPath`, `ExtraArgs` | plumbing |
| `OnMessage` | streaming callback per tool-protocol message (progress) |
| `Stdout` | tee of raw TLC output |

The returned `*Result`:

```go
r.Status          // Success | AssumptionViolation | Deadlock |
                  // SafetyViolation | LivenessViolation | Failure
r.Ok()            // Status == Success
r.Err()           // nil, or a descriptive error for non-success
r.StatesGenerated, r.DistinctStates, r.QueueStates, r.Depth
r.Errors          // []TLCError{Code int, Message string}
r.Trace           // *trace.Trace counterexample (nil when none)
r.Messages        // []Message{Code, Severity, Body}, the full protocol stream
```

**TLC version note**: tlacuilo talks to any TLC, but two conveniences
need a current `tla2tools.jar` (as `EnsureJar` downloads): `-workers
auto` and the `CHECK_DEADLOCK` config keyword are rejected by old jars
(TLC 2.15 and earlier), surfacing as `Status: failure` with the TLC
error in `r.Errors` — not as a Go error. For maximum compatibility
prefer `Options.DisableDeadlockCheck` (the `-deadlock` flag) over
`cfg.Config.CheckDeadlock`, as every example in this guide does.

Statuses come from TLC's exit codes (verified against real TLC: 0, 10,
11, 12, 13, 150+); traces — including liveness lassos (`Trace.Loop`) and
stuttering (`Trace.Stuttering`) — are parsed from the tool protocol's
state messages. The Go-level `error` return is reserved for mechanical
failures (java missing, output unparseable); *verification verdicts are
in the Result*, so a failing model check does not error.

## Values, traces, and ITF (value, trace)

`value.Value` models TLA+ values: `Bool`, `Int` (big), `String`,
`ModelValue`, `Set`, `Tuple` (= sequence), `Record`, `Func`, `Interval`.

```go
v, _ := value.Parse(`<<[n |-> 1], (1 :> "a" @@ 2 :> "b")>>`) // TLC's own syntax
v.String()                      // canonical TLA+ rendering
value.Equal(a, b)               // deep equality
value.Compare(a, b)             // deterministic total order

g, _ := value.FromGo(myStruct)  // Go -> TLA+ via reflection + `tla` tags
```

`value.FromGo` conversion rules: bools, all int kinds and `*big.Int`,
strings, slices/arrays → sequences, maps → functions, structs → records
(exported fields; `tla:"name"` renames, `tla:"-"` skips), nil pointers →
the model value `NULL`. Floats/channels/funcs are rejected — TLC has no
representation for them.

Traces interoperate with the wider ecosystem via ITF (the Informal Trace
Format used by Apalache, Quint, and recent TLC):

```go
data, _ := r.Trace.MarshalITF()   // ITF JSON for trace explorers
tr, _ := trace.UnmarshalITF(data) // and back
value.MarshalITF(v)               // single values too
```

## Validating your implementation against a spec (tracecheck)

This is the feature aimed at Go codebases with a spec: check that your
implementation — a Kubernetes controller's reconcile loop, a queue, a
lock protocol — only does things the spec allows. The approach (the
industrial-trace-validation middle ground that avoids Go→TLA+ extraction
state explosion): annotate actions, record under a deterministic test
harness, generate a trace spec + refinement mapping, let TLC judge.

**1. Annotate your state.** `tla` tags map struct fields to spec
variables:

```go
type controller struct {
    Desired int `tla:"desired"`
    Current int `tla:"current"`
}
```

**2. Record under a deterministic harness.** Actions are `Step` calls;
variables you don't mention carry forward automatically:

```go
rec := tracecheck.NewRecorder()
rec.InitState(c)                       // or rec.Init(map[string]any{...})
c.Desired = 3
rec.StepState("ChangeDesired", c)      // or rec.Step("Name", updates)
for {
    act, ok := c.reconcile()           // returns "ScaleUp" / "ScaleDown"
    if !ok { break }
    rec.StepState(act, c)
}
```

**3. Describe the refinement mapping and validate.**

```go
spec := tracecheck.Spec{
    Module: "Reconciler",                  // abstract module name
    Vars:   []string{"desired", "current"},
    Actions: map[string]string{            // recorded action -> spec action
        "ChangeDesired": "ChangeDesired",
        "ScaleUp":       "ScaleUp",
        "ScaleDown":     "ScaleDown",
    },
    Constants:  []cfg.Constant{{Name: "MaxReplicas", Value: "5"}},
    Invariants: []string{"TypeOK"},        // also checked over the trace
}
report, err := tracecheck.Validate(ctx, spec, rec.Trace(), reconcilerSpecSource, tlc.Options{})
if !report.Conforms {
    t.Fatalf("diverges at state %d (action %q)", report.DivergedAt, report.FailedAction)
}
```

Semantics worth knowing:

- A step whose action is **mapped** must satisfy exactly that spec
  action; **unmapped** actions fall back to `[Next]_vars` (which also
  admits stuttering steps). A mislabeled annotation is therefore itself
  a validation failure — annotations are claims, and TLC checks them.
- Mapped spec actions must be **zero-argument operators**. For
  parameterized actions, point the mapping at the existentially
  quantified wrapper (`Enq == \E v \in Values : ...`), which the trace's
  concrete next state then resolves.
- The first recorded state must satisfy the spec's `Init` unless
  `SkipInit` is set (`InitOperator`/`NextOperator` rename the defaults).
- Divergence is reported with the failing 1-based state index and its
  action annotation, extracted from TLC's own counterexample.
- `GenModule(spec, tr)` gives you the generated TLA+ trace module and
  `GenConfig(spec)` its TLC config, if you want to inspect, save, or
  check them yourself.

`examples/reconciler` is the end-to-end Kubernetes-controller-shaped
demo (`-bug` shows a divergence report); `tracecheck`'s tests validate
three Go tool shapes — reconcile controller, bounded FIFO queue, lock
discipline — each with a buggy variant caught at the exact expected step.

## Recipes for your own repo

**Model checking as a Go test.** Skip cleanly where TLC isn't available,
so `go test ./...` stays green on machines without Java:

```go
func requireTLC(t *testing.T) {
    t.Helper()
    if _, err := exec.LookPath("java"); err != nil {
        t.Skip("java not installed")
    }
    if _, err := tlc.FindJar(); err != nil {
        t.Skip("tla2tools.jar not found; set TLA2TOOLS_JAR")
    }
}

func TestSpecHolds(t *testing.T) {
    requireTLC(t)
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()
    r, err := tlc.Check(ctx, tlc.Job{Source: specSource, Config: config}, tlc.Options{})
    if err != nil {
        t.Fatal(err)
    }
    if err := r.Err(); err != nil {
        t.Fatal(err)
    }
}
```

Prefer auto-download instead of skipping (e.g. on developer machines):

```go
func TestMain(m *testing.M) {
    if jar, err := tlc.EnsureJar(context.Background()); err == nil {
        os.Setenv("TLA2TOOLS_JAR", jar)
    }
    os.Exit(m.Run())
}
```

**Trace validation in CI.** Deterministic harness + tracecheck is just a
regular test; wire the jar in your workflow and the whole suite runs:

```yaml
- uses: actions/setup-java@v4
  with: { distribution: temurin, java-version: "17" }
- run: |
    curl -fsSL -o /tmp/tla2tools.jar \
      https://github.com/tlaplus/tlaplus/releases/latest/download/tla2tools.jar
    echo "TLA2TOOLS_JAR=/tmp/tla2tools.jar" >> "$GITHUB_ENV"
- run: go test ./...
```

**Keep the spec next to the code.** Embed it so the test and the spec
version together:

```go
//go:embed reconciler.tla
var reconcilerSpecSource string
```

**Streaming progress on long checks:**

```go
opts := tlc.Options{OnMessage: func(m tlc.Message) {
    if m.Code == tlc.CodeProgress {
        log.Println(m.Body)
    }
}}
```

## Limitations

- The TLA+2 **proof language** is not parsed (THEOREM bodies carry the
  formula only), and **PlusCal** is not translated — same scope as most
  tooling outside TLAPS.
- Formatting is canonical, not byte-preserving.
- No semantic analysis (name resolution, arity/level checking): SANY
  reports those when TLC runs.
- `tracecheck` records complete states (carry-forward makes per-action
  annotations partial-friendly, but unobserved-variable existentials and
  parameterized action mappings are future work — see PLAN.md §6).
- ITF cannot distinguish model values from strings (a format property,
  not a library one).
