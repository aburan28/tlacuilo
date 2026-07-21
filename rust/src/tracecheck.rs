//! Validates implementations against TLA+ specifications by trace
//! checking: the implementation, driven by a deterministic test harness,
//! records its state at annotated action points; tracecheck generates a
//! TLA+ trace specification embedding the recorded behavior together
//! with a refinement mapping onto the abstract spec's actions, and asks
//! TLC whether the behavior is one the spec allows.
//!
//! Divergence is detected as a TLC deadlock (the generated trace spec
//! has no successor exactly when the spec cannot take a recorded step),
//! which [`validate`] translates into a [`Report`] naming the failing
//! step and action.

use std::collections::BTreeMap;

use crate::ast::{CaseArm, Expr, Field, Module, OpDecl, Unit};
use crate::cfg::{Config, Constant};
use crate::parser;
use crate::tlc::{self, Options, RunResult, Status};
use crate::trace::{State, Trace};
use crate::value::Value;

/// The action name recorded for the initial state.
pub const INIT_ACTION: &str = "Init";

/// A tracecheck failure (generation or TLC execution; a diverging trace
/// is reported via [`Report`], not an error).
#[derive(Debug)]
pub struct TraceCheckError(pub String);

impl std::fmt::Display for TraceCheckError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for TraceCheckError {}

type Result<T> = std::result::Result<T, TraceCheckError>;

fn err<T>(msg: impl Into<String>) -> Result<T> {
    Err(TraceCheckError(msg.into()))
}

/// Collects an implementation trace. `step` merges updates over the
/// previous state, so variables not mentioned are recorded unchanged.
#[derive(Default)]
pub struct Recorder {
    trace: Trace,
    cur: Option<BTreeMap<String, Value>>,
}

impl Recorder {
    pub fn new() -> Recorder {
        Recorder::default()
    }

    /// Records the initial state.
    pub fn init<I, K, V>(&mut self, state: I)
    where
        I: IntoIterator<Item = (K, V)>,
        K: Into<String>,
        V: Into<Value>,
    {
        self.trace = Trace::new();
        self.cur = Some(BTreeMap::new());
        self.record(INIT_ACTION, state);
    }

    /// Records one action; panics if called before [`Recorder::init`].
    pub fn step<I, K, V>(&mut self, action: &str, updates: I)
    where
        I: IntoIterator<Item = (K, V)>,
        K: Into<String>,
        V: Into<Value>,
    {
        assert!(
            self.cur.is_some(),
            "tracecheck: step({action:?}) before init"
        );
        self.record(action, updates);
    }

    fn record<I, K, V>(&mut self, action: &str, updates: I)
    where
        I: IntoIterator<Item = (K, V)>,
        K: Into<String>,
        V: Into<Value>,
    {
        let cur = self.cur.as_mut().expect("init first");
        for (k, v) in updates {
            cur.insert(k.into(), v.into());
        }
        let snapshot = cur.clone();
        self.trace.add_state(State {
            index: self.trace.states.len() + 1,
            action: action.to_string(),
            vars: snapshot,
        });
    }

    pub fn trace(&self) -> &Trace {
        &self.trace
    }

    pub fn len(&self) -> usize {
        self.trace.states.len()
    }

    pub fn is_empty(&self) -> bool {
        self.trace.states.is_empty()
    }
}

/// The abstract specification a trace is checked against and the
/// refinement mapping from recorded actions onto it.
#[derive(Clone, Debug, Default)]
pub struct Spec {
    /// The abstract module's name (the trace module extends it).
    pub module: String,
    /// The spec variables bound by the mapping; every recorded state
    /// must assign all of them (carry-forward guarantees this once
    /// `init` assigns them).
    pub vars: Vec<String>,
    /// Recorded action name -> abstract action operator (zero-argument;
    /// wrap parameterized actions in an existential on the spec side).
    /// Mapped steps must satisfy exactly that operator; unmapped ones
    /// fall back to `[Next]_vars`, which also admits stuttering.
    pub actions: Vec<(String, String)>,
    /// The abstract next-state operator (default "Next").
    pub next_operator: String,
    /// Drops the requirement that the first recorded state satisfies
    /// the abstract Init.
    pub skip_init: bool,
    /// The abstract initial predicate (default "Init").
    pub init_operator: String,
    pub constants: Vec<Constant>,
    /// Additionally checked over the trace states.
    pub invariants: Vec<String>,
    /// Extra modules the trace module needs (Naturals, Sequences, and
    /// `module` are always included).
    pub extends: Vec<String>,
}

impl Spec {
    fn next(&self) -> &str {
        if self.next_operator.is_empty() {
            "Next"
        } else {
            &self.next_operator
        }
    }

    fn init(&self) -> &str {
        if self.init_operator.is_empty() {
            "Init"
        } else {
            &self.init_operator
        }
    }

    pub fn trace_module_name(&self) -> String {
        format!("{}Trace", self.module)
    }
}

const GENERATED_NAMES: [&str; 8] = [
    "TraceIdx",
    "Trace",
    "TraceVars",
    "TraceMatch",
    "TraceInit",
    "TraceNext",
    "TraceSpec",
    "TraceComplete",
];

fn ident(n: &str) -> Expr {
    Expr::Ident(n.to_string())
}

fn num(n: usize) -> Expr {
    Expr::Number(n.to_string())
}

fn eq(l: Expr, r: Expr) -> Expr {
    Expr::Binary {
        op: "=".into(),
        l: Box::new(l),
        r: Box::new(r),
    }
}

fn conj(items: Vec<Expr>) -> Expr {
    Expr::Junction {
        op: "/\\".into(),
        items,
    }
}

fn trace_at(k: Expr, var: &str) -> Expr {
    Expr::Dot {
        x: Box::new(Expr::FnApply {
            f: Box::new(ident("Trace")),
            args: vec![k],
        }),
        field: var.to_string(),
    }
}

fn idx_plus_one() -> Expr {
    Expr::Binary {
        op: "+".into(),
        l: Box::new(ident("TraceIdx")),
        r: Box::new(num(1)),
    }
}

fn trace_len() -> Expr {
    Expr::Apply {
        fun: Box::new(ident("Len")),
        args: vec![ident("Trace")],
    }
}

/// Generates the TLA+ trace-validation module for a recorded trace.
pub fn gen_module(s: &Spec, tr: &Trace) -> Result<Module> {
    if s.module.is_empty() {
        return err("Spec.module is required");
    }
    if s.vars.is_empty() {
        return err("Spec.vars is required");
    }
    for v in &s.vars {
        if v == "act" {
            return err(format!(
                "variable name {v:?} collides with the trace record's action field"
            ));
        }
        if GENERATED_NAMES.contains(&v.as_str()) {
            return err(format!(
                "variable name {v:?} collides with a generated name"
            ));
        }
    }
    if tr.states.is_empty() {
        return err("empty trace (record init first)");
    }

    // Trace == << [act |-> "...", v1 |-> ..., ...], ... >>
    let mut states = Vec::new();
    for (i, st) in tr.states.iter().enumerate() {
        let mut fields = vec![Field {
            name: "act".into(),
            expr: Expr::Str(st.action.clone()),
        }];
        for v in &s.vars {
            let val = st.vars.get(v).ok_or_else(|| {
                TraceCheckError(format!("state {} does not assign variable {v}", i + 1))
            })?;
            let e = parser::parse_expr(&val.to_string())
                .map_err(|e| TraceCheckError(format!("state {}, variable {v}: {e}", i + 1)))?;
            fields.push(Field {
                name: v.clone(),
                expr: e,
            });
        }
        states.push(Expr::RecordLit(fields));
    }

    let spec_vars_tuple = Expr::Tuple(s.vars.iter().map(|v| ident(v)).collect());
    let mut trace_vars = vec![ident("TraceIdx")];
    trace_vars.extend(s.vars.iter().map(|v| ident(v)));

    let mut actions = s.actions.clone();
    actions.sort();

    let mut m = Module {
        name: s.trace_module_name(),
        ..Default::default()
    };
    for e in ["Naturals", "Sequences"]
        .iter()
        .map(|x| x.to_string())
        .chain(s.extends.iter().cloned())
        .chain(std::iter::once(s.module.clone()))
    {
        if !m.extends.contains(&e) {
            m.extends.push(e);
        }
    }
    m.units.push(Unit::Variables(vec!["TraceIdx".into()]));
    m.units.push(def("Trace", Expr::Tuple(states)));
    m.units.push(def("TraceVars", Expr::Tuple(trace_vars)));

    let fallback = Expr::SquareAct {
        x: Box::new(ident(s.next())),
        sub: Box::new(spec_vars_tuple),
    };
    if !actions.is_empty() {
        let arms = actions
            .iter()
            .map(|(rec, op)| CaseArm {
                cond: eq(trace_at(ident("k"), "act"), Expr::Str(rec.clone())),
                value: ident(op),
            })
            .collect();
        m.units.push(Unit::OperatorDef {
            local: false,
            fixity: crate::ast::Fixity::Named,
            name: "TraceMatch".into(),
            params: vec![OpDecl::plain("k")],
            body: Expr::Case {
                arms,
                other: Some(Box::new(fallback.clone())),
            },
        });
    }

    // TraceInit == /\ TraceIdx = 1 /\ v = Trace[1].v ... [/\ Init]
    let mut init_conj = vec![eq(ident("TraceIdx"), num(1))];
    for v in &s.vars {
        init_conj.push(eq(ident(v), trace_at(num(1), v)));
    }
    if !s.skip_init {
        init_conj.push(ident(s.init()));
    }
    m.units.push(def("TraceInit", conj(init_conj)));

    // Advance and match, or self-loop at the end (so the only deadlock
    // is a mid-trace divergence).
    let mut advance = vec![
        Expr::Binary {
            op: "<".into(),
            l: Box::new(ident("TraceIdx")),
            r: Box::new(trace_len()),
        },
        eq(
            Expr::PostfixExpr {
                op: "'".into(),
                x: Box::new(ident("TraceIdx")),
            },
            idx_plus_one(),
        ),
    ];
    for v in &s.vars {
        advance.push(eq(
            Expr::PostfixExpr {
                op: "'".into(),
                x: Box::new(ident(v)),
            },
            trace_at(idx_plus_one(), v),
        ));
    }
    if actions.is_empty() {
        advance.push(fallback);
    } else {
        advance.push(Expr::Apply {
            fun: Box::new(ident("TraceMatch")),
            args: vec![idx_plus_one()],
        });
    }
    let done = conj(vec![
        eq(ident("TraceIdx"), trace_len()),
        Expr::Unary {
            op: "UNCHANGED".into(),
            x: Box::new(ident("TraceVars")),
        },
    ]);
    m.units.push(def(
        "TraceNext",
        Expr::Junction {
            op: "\\/".into(),
            items: vec![conj(advance), done],
        },
    ));

    m.units.push(def(
        "TraceSpec",
        conj(vec![
            ident("TraceInit"),
            Expr::Unary {
                op: "[]".into(),
                x: Box::new(Expr::SquareAct {
                    x: Box::new(ident("TraceNext")),
                    sub: Box::new(ident("TraceVars")),
                }),
            },
        ]),
    ));
    m.units
        .push(def("TraceComplete", eq(ident("TraceIdx"), trace_len())));
    Ok(m)
}

fn def(name: &str, body: Expr) -> Unit {
    Unit::OperatorDef {
        local: false,
        fixity: crate::ast::Fixity::Named,
        name: name.into(),
        params: Vec::new(),
        body,
    }
}

/// The TLC configuration for the trace module. Deadlock checking stays
/// enabled: a deadlock is exactly a divergence.
pub fn gen_config(s: &Spec) -> Config {
    Config {
        specification: "TraceSpec".into(),
        constants: s.constants.clone(),
        invariants: s.invariants.clone(),
        ..Default::default()
    }
}

/// The outcome of validating a recorded trace against a spec.
#[derive(Debug)]
pub struct Report {
    /// Whether every recorded step is a behavior the spec allows.
    pub conforms: bool,
    /// 1-based index of the first recorded state the spec could not
    /// reach (None when conforming or undetermined).
    pub diverged_at: Option<usize>,
    /// The recorded action annotation of the diverging step.
    pub failed_action: String,
    pub result: RunResult,
}

impl std::fmt::Display for Report {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if self.conforms {
            return f.write_str("trace conforms to the specification");
        }
        match self.diverged_at {
            Some(k) => write!(
                f,
                "trace diverges from the specification at state {k} (action {:?})",
                self.failed_action
            ),
            None => write!(f, "trace validation failed: {}", self.result.status()),
        }
    }
}

/// Generates the trace module for `tr`, model-checks it with TLC against
/// the abstract spec source, and interprets the outcome.
pub fn validate(s: &Spec, tr: &Trace, spec_source: &str, opts: &Options) -> Result<Report> {
    let abstract_mod = parser::parse(spec_source)
        .map_err(|e| TraceCheckError(format!("abstract spec does not parse: {e}")))?;
    if abstract_mod.name != s.module {
        return err(format!(
            "spec source is module {:?}, but Spec.module is {:?}",
            abstract_mod.name, s.module
        ));
    }
    let m = gen_module(s, tr)?;
    // Divergence is detected AS a TLC deadlock, so deadlock checking must
    // stay on no matter what the caller put in opts.
    let mut opts = opts.clone();
    opts.disable_deadlock_check = false;
    let res = tlc::check(
        &tlc::Job {
            module: Some(m),
            config: gen_config(s),
            aux_modules: vec![(s.module.clone(), spec_source.to_string())],
            ..Default::default()
        },
        &opts,
    )
    .map_err(|e| TraceCheckError(e.to_string()))?;

    let mut report = Report {
        conforms: false,
        diverged_at: None,
        failed_action: String::new(),
        result: res,
    };
    match report.result.status() {
        Status::Success => report.conforms = true,
        Status::Deadlock => {
            // TLC's deadlock trace ends in the last state the spec could
            // reach; its TraceIdx names the last matched recorded state,
            // so the diverging one is the next.
            if let Some(t) = &report.result.trace {
                if let Some(last) = t.states.last() {
                    if let Some(Value::Int(iv)) = last.vars.get("TraceIdx") {
                        if let Ok(k) = usize::try_from(iv.clone()) {
                            if k < tr.states.len() {
                                report.diverged_at = Some(k + 1);
                                report.failed_action = tr.states[k].action.clone();
                            }
                        }
                    }
                }
            }
        }
        _ => {}
    }
    Ok(report)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::printer::module_string;

    #[test]
    fn recorder_carry_forward() {
        let mut r = Recorder::new();
        r.init([("x", Value::int(0)), ("y", Value::from(vec![1i64]))]);
        r.step("Bump", [("x", Value::int(1))]);
        let t = r.trace();
        assert_eq!(t.states.len(), 2);
        assert_eq!(t.states[1].vars["y"], Value::from(vec![1i64]));
        assert_eq!(t.states[0].action, INIT_ACTION);
        assert_eq!(t.states[1].action, "Bump");
    }

    #[test]
    fn gen_module_parses_and_shape() {
        let mut r = Recorder::new();
        r.init([("x", 0i64)]);
        r.step("Inc", [("x", 1i64)]);
        r.step("Inc", [("x", 2i64)]);
        let s = Spec {
            module: "Counter".into(),
            vars: vec!["x".into()],
            actions: vec![("Inc".into(), "Inc".into())],
            ..Default::default()
        };
        let m = gen_module(&s, r.trace()).unwrap();
        let src = module_string(&m);
        parser::parse(&src)
            .unwrap_or_else(|e| panic!("generated module does not parse: {e}\n{src}"));
        for want in [
            "EXTENDS Naturals, Sequences, Counter",
            "VARIABLE TraceIdx",
            "[act |-> \"Init\", x |-> 0]",
            "[act |-> \"Inc\", x |-> 2]",
            "CASE Trace[k].act = \"Inc\" -> Inc [] OTHER -> [Next]_<<x>>",
            "TraceInit == /\\ TraceIdx = 1",
            "/\\ UNCHANGED TraceVars",
        ] {
            assert!(src.contains(want), "missing {want:?} in:\n{src}");
        }
        assert!(gen_config(&s).format().contains("SPECIFICATION TraceSpec"));
    }

    #[test]
    fn gen_module_rejects_collisions() {
        let mut r = Recorder::new();
        r.init([("act", 1i64)]);
        let s = Spec {
            module: "M".into(),
            vars: vec!["act".into()],
            ..Default::default()
        };
        assert!(gen_module(&s, r.trace()).is_err());
    }
}
