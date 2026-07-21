# Validating a Kubernetes controller against a TLA+ spec

This directory is a complete, runnable template for putting a controller
under formal verification with tlacuilo — both halves of it:

1. **The design proof.** `ReplicaController.tla` models the protocol: an
   environment that changes the desired replica count and crashes pods,
   and a controller that creates/deletes one pod per reconcile step. TLC
   exhaustively checks *every* interleaving for the invariants (`TypeOK`,
   `NeverOverProvision`) and, in a quiescent model starting from *any*
   type-correct state, the liveness property `Convergence` — once the
   environment goes quiet, fair reconciliation reaches and keeps
   `Cardinality(pods) = desired`.
2. **The refinement check.** `controller.go` is a controller-runtime-shaped
   reconciler (`Reconcile(ctx, Request) (Result, action)` against a
   `Cluster` client). The tests drive it with a deterministic harness —
   scale up, crash a pod, heal, scale to zero — record the trace, and let
   TLC verify the recorded behavior is one the spec allows.
   `TestBatchCreateBugCaught` shows the failure mode: a "batch create"
   optimization diverges from the spec's one-pod `CreatePod` at exactly
   the reported step.

Run it (the tests skip without Java + `tla2tools.jar`; `tlc.EnsureJar`
or `TLA2TOOLS_JAR` provides the jar):

```
go test -v ./examples/k8scontroller/
```

## The pattern, ported to a real controller

The three ingredients transfer directly to a controller-runtime project:

- **Spec next to code**: keep the `.tla` file in the controller package,
  `//go:embed` it (`SpecSource` here), so code and spec version together.
- **Projection**: `TLAState` maps client state onto the spec's variables.
  Note the deliberate translation: the pod *list* Go sees becomes the
  *set* the spec reasons about (`value.NewSet`), because ordering is a Go
  artifact, not part of the abstraction. With a real client this reads
  from the fake client / envtest instead of `FakeCluster`.
- **Action annotations**: the reconciler returns the spec action it
  performed; the harness records it (`rec.Step(action, TLAState(cl))`).
  In a real controller, put the annotation at each `client.Create/Delete`
  call site. Level no-op reconciles are simply not recorded.
- **Deterministic harness**: the test — not a manager's watch loop —
  decides when the environment moves (`SetDesired`, `CrashPod`) and when
  the controller reconciles. That is what keeps trace validation immune
  to the interleaving explosion; the *spec* check is where all
  interleavings are explored.

## CI: making the proof a required check

`.github/workflows/tla-proof.yml` runs both halves on every PR as a
dedicated check. Two details worth copying:

- The jar fetch is **pinned with a latest fallback and hard-fails** —
  unlike an optional integration job, a proof that can't run must fail,
  not skip.
- `TLACUILO_REQUIRE_TLC=1` flips the tests' skip-when-missing behavior
  into failures, so a broken toolchain can never produce a silently
  green proof.

```yaml
- uses: actions/setup-java@v4
  with: { distribution: temurin, java-version: "17" }
- name: Fetch tla2tools.jar
  run: |
    curl -fsSL --retry 3 -o /tmp/tla2tools.jar \
      https://github.com/tlaplus/tlaplus/releases/download/v1.7.1/tla2tools.jar
    echo "TLA2TOOLS_JAR=/tmp/tla2tools.jar" >> "$GITHUB_ENV"
- name: TLA+ proof
  env: { TLACUILO_REQUIRE_TLC: "1" }
  run: go test -v -count=1 ./internal/controller/  # wherever your spec tests live
```

## Scope honestly stated

Trace validation certifies the schedules the harness drives — the
exhaustive interleaving coverage lives in the spec-level check, which is
why both run in CI. A finite trace witnesses safety only; liveness
("eventually converges") is checked on the spec (`Convergence`) and can
additionally be asserted concretely in the harness (the reconcile loop
terminating *is* convergence for that schedule).
