//! Execution traces (counterexamples) and the ITF (Informal Trace
//! Format) JSON interchange, per Apalache ADR-015.

use std::collections::BTreeMap;
use std::fmt;

use serde_json::{json, Map};

use crate::value::{self, Value, ValueError};

/// One state of a trace: an assignment of values to variables.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct State {
    /// 1-based state number as TLC prints it.
    pub index: usize,
    /// The transition that produced this state, e.g. "Initial predicate".
    pub action: String,
    pub vars: BTreeMap<String, Value>,
}

/// A finite behavior, optionally with lasso or stuttering information
/// for liveness counterexamples.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Trace {
    /// Variable names in display order.
    pub vars: Vec<String>,
    pub states: Vec<State>,
    /// 0-based index of the state the behavior returns to after the
    /// last state (a liveness lasso).
    pub loop_to: Option<usize>,
    /// Set when the behavior ends in infinite stuttering.
    pub stuttering: bool,
}

impl Trace {
    pub fn new() -> Trace {
        Trace::default()
    }

    /// Appends a state, extending `vars` with newly seen variables.
    pub fn add_state(&mut self, s: State) {
        for name in s.vars.keys() {
            if !self.vars.iter().any(|v| v == name) {
                self.vars.push(name.clone());
            }
        }
        self.states.push(s);
    }

    /// Renders the trace in TLC's textual style.
    pub fn render(&self) -> String {
        let mut b = String::new();
        for (i, s) in self.states.iter().enumerate() {
            b.push_str(&format!("State {}: <{}>\n", i + 1, s.action));
            for name in &self.vars {
                if let Some(v) = s.vars.get(name) {
                    b.push_str(&format!("/\\ {name} = {v}\n"));
                }
            }
        }
        if let Some(l) = self.loop_to {
            if l < self.states.len() {
                b.push_str(&format!("Back to state {}\n", l + 1));
            }
        }
        if self.stuttering {
            b.push_str("Stuttering\n");
        }
        b
    }

    /// Renders the trace as ITF JSON.
    pub fn marshal_itf(&self) -> Result<String, ValueError> {
        let mut states = Vec::new();
        for s in &self.states {
            let mut m = Map::new();
            let mut meta = Map::new();
            meta.insert("index".into(), json!(s.index));
            if !s.action.is_empty() {
                meta.insert("action".into(), json!(s.action));
            }
            m.insert("#meta".into(), serde_json::Value::Object(meta));
            for (name, v) in &s.vars {
                m.insert(name.clone(), value::to_itf(v)?);
            }
            states.push(serde_json::Value::Object(m));
        }
        let mut doc = Map::new();
        doc.insert(
            "#meta".into(),
            json!({
                "format": "ITF",
                "format-description": "https://apalache-mc.org/docs/adr/015adr-trace.html",
                "description": "Trace produced by tlacuilo (rust)",
            }),
        );
        doc.insert("vars".into(), json!(self.vars));
        doc.insert("states".into(), serde_json::Value::Array(states));
        if let Some(l) = self.loop_to {
            doc.insert("loop".into(), json!(l));
        }
        serde_json::to_string_pretty(&serde_json::Value::Object(doc))
            .map_err(|e| ValueError(e.to_string()))
    }
}

impl fmt::Display for Trace {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.render())
    }
}

/// Parses an ITF JSON trace.
pub fn unmarshal_itf(data: &str) -> Result<Trace, ValueError> {
    let doc: serde_json::Value =
        serde_json::from_str(data).map_err(|e| ValueError(e.to_string()))?;
    let mut t = Trace::new();
    if let Some(vars) = doc.get("vars").and_then(|v| v.as_array()) {
        t.vars = vars
            .iter()
            .filter_map(|v| v.as_str().map(String::from))
            .collect();
    }
    for (i, st) in doc
        .get("states")
        .and_then(|s| s.as_array())
        .unwrap_or(&Vec::new())
        .iter()
        .enumerate()
    {
        let mut s = State {
            index: i + 1,
            ..Default::default()
        };
        let obj = st
            .as_object()
            .ok_or(ValueError("ITF state must be an object".into()))?;
        if let Some(meta) = obj.get("#meta").and_then(|m| m.as_object()) {
            if let Some(idx) = meta.get("index").and_then(|x| x.as_u64()) {
                s.index = idx as usize;
            }
            if let Some(a) = meta.get("action").and_then(|x| x.as_str()) {
                s.action = a.to_string();
            }
        }
        for (name, raw) in obj {
            if name == "#meta" {
                continue;
            }
            s.vars.insert(name.clone(), value::from_itf(raw)?);
        }
        t.states.push(s);
    }
    t.loop_to = doc.get("loop").and_then(|l| l.as_u64()).map(|l| l as usize);
    Ok(t)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> Trace {
        let mut t = Trace::new();
        let mut v1 = BTreeMap::new();
        v1.insert("x".to_string(), Value::int(0));
        v1.insert("q".to_string(), Value::Tuple(vec![]));
        t.add_state(State {
            index: 1,
            action: "Init".into(),
            vars: v1,
        });
        let mut v2 = BTreeMap::new();
        v2.insert("x".to_string(), Value::int(1));
        v2.insert("q".to_string(), Value::Tuple(vec![Value::from("a")]));
        t.add_state(State {
            index: 2,
            action: "Next".into(),
            vars: v2,
        });
        t.loop_to = Some(0);
        t
    }

    #[test]
    fn itf_round_trip() {
        let t = sample();
        let data = t.marshal_itf().unwrap();
        assert!(data.contains("\"format\": \"ITF\""));
        let t2 = unmarshal_itf(&data).unwrap();
        assert_eq!(t2.states.len(), 2);
        assert_eq!(t2.loop_to, Some(0));
        assert_eq!(t2.states[1].vars["x"], Value::int(1));
        assert_eq!(t2.states[0].action, "Init");
        assert_eq!(t2.vars.len(), 2);
    }

    #[test]
    fn render_style() {
        let out = sample().render();
        assert!(out.contains("State 1"));
        assert!(out.contains("/\\ x = 0"));
        assert!(out.contains("Back to state 1"));
    }
}
