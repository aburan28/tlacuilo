//! Live TLC integration: the Rust port drives the same spec and
//! scenarios as the Go tests in examples/k8scontroller. Tests skip when
//! java or tla2tools.jar is unavailable — unless TLACUILO_REQUIRE_TLC is
//! set (as the CI proof job sets it), which turns skips into failures.

use tlacuilo::cfg::{Config, Constant};
use tlacuilo::tlc::{self, Options, Status};
use tlacuilo::tracecheck::{self, Recorder, Spec};
use tlacuilo::value::Value;

/// The spec shared with the Go tests and the TLA+ proof CI workflow.
const RECONCILER_SPEC: &str = include_str!("../../examples/k8scontroller/ReplicaController.tla");

fn tlc_options() -> Option<Options> {
    let required = std::env::var("TLACUILO_REQUIRE_TLC").is_ok();
    let have_java = std::process::Command::new("java")
        .arg("-version")
        .output()
        .is_ok();
    if !have_java {
        assert!(
            !required,
            "TLACUILO_REQUIRE_TLC is set but java is not installed"
        );
        eprintln!("skipping: java not installed");
        return None;
    }
    match tlc::find_jar() {
        Some(jar) => Some(Options {
            jar_path: Some(jar),
            ..Default::default()
        }),
        None => {
            assert!(
                !required,
                "TLACUILO_REQUIRE_TLC is set but tla2tools.jar was not found (set TLA2TOOLS_JAR)"
            );
            eprintln!("skipping: tla2tools.jar not found (set TLA2TOOLS_JAR)");
            None
        }
    }
}

fn spec_constants() -> Vec<Constant> {
    vec![
        Constant::assign("Pods", "{\"p1\", \"p2\", \"p3\"}"),
        Constant::assign("MaxReplicas", "3"),
    ]
}

#[test]
fn spec_safety_proof() {
    let Some(opts) = tlc_options() else { return };
    let r = tlc::check(
        &tlc::Job {
            source: RECONCILER_SPEC.into(),
            config: Config {
                specification: "Spec".into(),
                constants: spec_constants(),
                invariants: vec!["TypeOK".into(), "NeverOverProvision".into()],
                ..Default::default()
            },
            ..Default::default()
        },
        &opts,
    )
    .unwrap();
    assert!(r.ok(), "safety proof failed: {:?}", r.err());
    assert_eq!(r.distinct_states, 32);
}

#[test]
fn spec_convergence_proof() {
    let Some(mut opts) = tlc_options() else {
        return;
    };
    // Converged quiescent states have no successor; disable deadlock
    // checking for the liveness run only.
    opts.disable_deadlock_check = true;
    let r = tlc::check(
        &tlc::Job {
            source: RECONCILER_SPEC.into(),
            config: Config {
                specification: "QuiescentSpec".into(),
                constants: spec_constants(),
                properties: vec!["Convergence".into()],
                ..Default::default()
            },
            ..Default::default()
        },
        &opts,
    )
    .unwrap();
    assert!(r.ok(), "convergence proof failed: {:?}", r.err());
}

/// A minimal replica controller: the implementation under test.
struct Controller {
    desired: i64,
    pods: Vec<String>,
    pool: Vec<String>,
    /// The bug switch: create two pods per reconcile.
    batch_create: bool,
}

impl Controller {
    fn new() -> Controller {
        Controller {
            desired: 1,
            pods: Vec::new(),
            pool: vec!["p1".into(), "p2".into(), "p3".into()],
            batch_create: false,
        }
    }

    /// One reconcile step; returns the spec action taken and whether to
    /// requeue.
    fn reconcile(&mut self) -> (Option<&'static str>, bool) {
        let n = self.pods.len() as i64;
        if n < self.desired {
            let want = if self.batch_create && self.desired - n >= 2 {
                2
            } else {
                1
            };
            let mut created = 0;
            let pool = self.pool.clone();
            for name in pool {
                if created == want {
                    break;
                }
                if !self.pods.contains(&name) {
                    self.pods.push(name);
                    self.pods.sort();
                    created += 1;
                }
            }
            (Some("CreatePod"), n + created != self.desired)
        } else if n > self.desired {
            self.pods.remove(0);
            (Some("DeletePod"), n - 1 != self.desired)
        } else {
            (None, false)
        }
    }

    fn tla_state(&self) -> Vec<(String, Value)> {
        let pods = Value::set(self.pods.iter().map(|p| Value::from(p.as_str())).collect());
        vec![
            ("desired".to_string(), Value::int(self.desired)),
            ("pods".to_string(), pods),
        ]
    }
}

fn trace_spec() -> Spec {
    Spec {
        module: "ReplicaController".into(),
        vars: vec!["desired".into(), "pods".into()],
        actions: vec![
            ("ChangeDesired".into(), "ChangeDesired".into()),
            ("PodCrash".into(), "PodCrash".into()),
            ("CreatePod".into(), "CreatePod".into()),
            ("DeletePod".into(), "DeletePod".into()),
        ],
        constants: spec_constants(),
        invariants: vec!["TypeOK".into(), "NeverOverProvision".into()],
        ..Default::default()
    }
}

/// The deterministic harness: spec changes and crashes interleaved with
/// reconcile-to-level loops.
fn drive(c: &mut Controller) -> Recorder {
    let mut rec = Recorder::new();
    rec.init(c.tla_state());
    let reconcile_to_level = |c: &mut Controller, rec: &mut Recorder| loop {
        let (action, requeue) = c.reconcile();
        if let Some(a) = action {
            rec.step(a, c.tla_state());
        }
        if !requeue {
            break;
        }
    };
    reconcile_to_level(c, &mut rec);
    c.desired = 3;
    rec.step("ChangeDesired", c.tla_state());
    reconcile_to_level(c, &mut rec);
    let victim = c.pods[0].clone();
    c.pods.retain(|p| p != &victim);
    rec.step("PodCrash", c.tla_state());
    reconcile_to_level(c, &mut rec);
    c.desired = 0;
    rec.step("ChangeDesired", c.tla_state());
    reconcile_to_level(c, &mut rec);
    rec
}

#[test]
fn controller_refines_spec() {
    let Some(opts) = tlc_options() else { return };
    let mut c = Controller::new();
    let rec = drive(&mut c);
    let report = tracecheck::validate(&trace_spec(), rec.trace(), RECONCILER_SPEC, &opts).unwrap();
    assert!(
        report.conforms,
        "controller diverges: {report} (TLC: {:?})",
        report.result.errors
    );
}

#[test]
fn batch_create_bug_caught() {
    let Some(opts) = tlc_options() else { return };
    let mut c = Controller::new();
    c.batch_create = true;
    let rec = drive(&mut c);
    let report = tracecheck::validate(&trace_spec(), rec.trace(), RECONCILER_SPEC, &opts).unwrap();
    assert!(!report.conforms, "batch-create bug was not caught");
    // States: 1 Init(d=1,{}), 2 CreatePod {p1}, 3 ChangeDesired d=3,
    // 4 CreatePod {p1,p2,p3} <- two pods at once diverges here.
    assert_eq!(report.diverged_at, Some(4), "{report}");
    assert_eq!(report.failed_action, "CreatePod");
}

#[test]
fn invariant_violation_end_to_end() {
    let Some(opts) = tlc_options() else { return };
    let r = tlc::check(
        &tlc::Job {
            source: "---- MODULE RustCounter ----\nEXTENDS Naturals\nVARIABLE x\nInit == x = 0\nNext == x' = x + 1\nSmall == x < 3\n====\n"
                .into(),
            config: Config {
                init: "Init".into(),
                next: "Next".into(),
                invariants: vec!["Small".into()],
                ..Default::default()
            },
            ..Default::default()
        },
        &Options {
            disable_deadlock_check: true,
            ..opts
        },
    )
    .unwrap();
    assert_eq!(r.status(), Status::SafetyViolation);
    let t = r.trace.as_ref().expect("trace");
    assert_eq!(t.states.len(), 4);
    assert_eq!(t.states[3].vars["x"], Value::int(3));
}
