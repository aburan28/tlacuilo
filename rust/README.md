# tlacuilo (Rust)

The Rust port of the [tlacuilo](../README.md) TLA+ toolkit. Same design,
same semantics, verified against the same fixtures and the same live TLC
runs as the Go library:

| Module | What it does |
|--------|--------------|
| `scanner` / `token` / `parser` / `ast` / `printer` | Column-aware TLA+ parser (junction lists, the full operator precedence table) and canonical pretty-printer (`parse ∘ print` is a fixed point) |
| `value` | TLA+ values (big ints, sets, tuples, records, functions) — parse TLC output, render TLA+, ITF JSON interchange |
| `cfg` | TLC `.cfg` files: generate and parse |
| `tlc` | Run TLC, parse its `-tool` protocol into typed results and traces |
| `trace` | Traces with ITF import/export |
| `tracecheck` | Validate implementations against specs by trace checking |

Dependencies: `num-bigint` and `serde_json` only. Running TLC requires
Java and `tla2tools.jar` (`$TLA2TOOLS_JAR`, working directory, or
`~/.tlacuilo/`).

## Quick start

```rust
use tlacuilo::{cfg::Config, parser, tlc};

// Parse and reformat a spec.
let m = parser::parse(&src)?;
println!("{m}");

// Model-check it.
let r = tlc::check(&tlc::Job {
    source: src,
    config: Config {
        specification: "Spec".into(),
        invariants: vec!["TypeOK".into()],
        ..Default::default()
    },
    ..Default::default()
}, &tlc::Options::default())?;
println!("{}", r.status());          // success | safety violation | ...
if let Some(t) = &r.trace {          // typed counterexample
    println!("{t}");
    println!("{}", t.marshal_itf()?);
}
```

## Trace validation

```rust
use tlacuilo::tracecheck::{Recorder, Spec, validate};

let mut rec = Recorder::new();
rec.init([("desired", 1i64), ("current", 0i64)]);
rec.step("ScaleUp", [("current", 1i64)]);

let report = validate(&Spec {
    module: "Reconciler".into(),
    vars: vec!["desired".into(), "current".into()],
    actions: vec![("ScaleUp".into(), "ScaleUp".into())],
    ..Default::default()
}, rec.trace(), spec_source, &tlc::Options::default())?;
// report.conforms, or report.diverged_at / report.failed_action
```

`rust/tests/integration.rs` runs the same Kubernetes-controller scenario
as the Go tests — the safety and convergence proofs over the shared
`examples/k8scontroller/ReplicaController.tla`, the refinement check,
and the batch-create bug caught at the exact same step.

## Differences from the Go library

- No builder module: construct `ast::Module`/`ast::Expr` values directly
  (see `tracecheck::gen_module` for a worked example) — Rust's enum
  literals make a separate fluent layer less necessary.
- No jar downloader: provide `tla2tools.jar` via `$TLA2TOOLS_JAR` (see
  the [Rust CI workflow](../.github/workflows/rust.yml) for the fetch
  step).
- AST nodes don't carry source positions (parse errors still do).
- `Recorder` state maps use `Into<Value>` conversions instead of Go's
  reflection-based `FromGo`.
