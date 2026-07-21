//! TLA+ values: booleans, big integers, strings, model values, sets,
//! tuples/sequences, records, explicit functions, and integer intervals
//! — parseable from TLA+ literal syntax (including the syntax TLC uses
//! to print counterexample states), renderable back to TLA+, and
//! convertible to/from the ITF (Informal Trace Format) JSON encoding.

use std::fmt;

use num_bigint::BigInt;
use serde_json::{json, Map, Number};

use crate::ast::Expr;
use crate::parser::parse_expr;

/// A TLA+ value. `Ord` imposes the deterministic total order used for
/// canonical set and function ordering (by kind, then content).
#[derive(Clone, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Value {
    Bool(bool),
    Int(BigInt),
    Str(String),
    /// A TLC model value: an identifier equal only to itself.
    Model(String),
    /// The integer range `lo..hi`.
    Interval(BigInt, BigInt),
    /// Canonical (sorted, deduped) elements.
    Set(Vec<Value>),
    /// Tuples double as TLA+ sequences.
    Tuple(Vec<Value>),
    /// Fields in canonical (name-sorted) order.
    Record(Vec<(String, Value)>),
    /// Explicit function `(k1 :> v1 @@ k2 :> v2)`, entries key-sorted.
    Func(Vec<(Value, Value)>),
}

/// A value-conversion or parse failure.
#[derive(Clone, Debug)]
pub struct ValueError(pub String);

impl fmt::Display for ValueError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for ValueError {}

type Result<T> = std::result::Result<T, ValueError>;

fn err<T>(msg: impl Into<String>) -> Result<T> {
    Err(ValueError(msg.into()))
}

impl Value {
    pub fn int(v: i64) -> Value {
        Value::Int(BigInt::from(v))
    }

    /// A set with canonicalized (sorted, deduplicated) elements.
    pub fn set(mut elems: Vec<Value>) -> Value {
        elems.sort();
        elems.dedup();
        Value::Set(elems)
    }

    /// A record with name-sorted fields.
    pub fn record(mut fields: Vec<(String, Value)>) -> Value {
        fields.sort_by(|a, b| a.0.cmp(&b.0));
        Value::Record(fields)
    }

    /// A function with key-sorted entries.
    pub fn func(mut entries: Vec<(Value, Value)>) -> Value {
        entries.sort_by(|a, b| a.0.cmp(&b.0));
        Value::Func(entries)
    }
}

impl From<bool> for Value {
    fn from(b: bool) -> Value {
        Value::Bool(b)
    }
}

impl From<i64> for Value {
    fn from(v: i64) -> Value {
        Value::int(v)
    }
}

impl From<i32> for Value {
    fn from(v: i32) -> Value {
        Value::int(v as i64)
    }
}

impl From<usize> for Value {
    fn from(v: usize) -> Value {
        Value::Int(BigInt::from(v))
    }
}

impl From<&str> for Value {
    fn from(s: &str) -> Value {
        Value::Str(s.to_string())
    }
}

impl From<String> for Value {
    fn from(s: String) -> Value {
        Value::Str(s)
    }
}

impl<T: Into<Value>> From<Vec<T>> for Value {
    fn from(v: Vec<T>) -> Value {
        Value::Tuple(v.into_iter().map(Into::into).collect())
    }
}

impl fmt::Display for Value {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Value::Bool(true) => f.write_str("TRUE"),
            Value::Bool(false) => f.write_str("FALSE"),
            Value::Int(x) => write!(f, "{x}"),
            Value::Str(s) => {
                f.write_str("\"")?;
                for c in s.chars() {
                    match c {
                        '"' => f.write_str("\\\"")?,
                        '\\' => f.write_str("\\\\")?,
                        '\t' => f.write_str("\\t")?,
                        '\n' => f.write_str("\\n")?,
                        '\x0c' => f.write_str("\\f")?,
                        '\r' => f.write_str("\\r")?,
                        other => write!(f, "{other}")?,
                    }
                }
                f.write_str("\"")
            }
            Value::Model(n) => f.write_str(n),
            Value::Interval(lo, hi) => write!(f, "{lo}..{hi}"),
            Value::Set(es) => {
                f.write_str("{")?;
                for (i, e) in es.iter().enumerate() {
                    if i > 0 {
                        f.write_str(", ")?;
                    }
                    write!(f, "{e}")?;
                }
                f.write_str("}")
            }
            Value::Tuple(es) => {
                f.write_str("<<")?;
                for (i, e) in es.iter().enumerate() {
                    if i > 0 {
                        f.write_str(", ")?;
                    }
                    write!(f, "{e}")?;
                }
                f.write_str(">>")
            }
            Value::Record(fields) => {
                f.write_str("[")?;
                for (i, (n, v)) in fields.iter().enumerate() {
                    if i > 0 {
                        f.write_str(", ")?;
                    }
                    write!(f, "{n} |-> {v}")?;
                }
                f.write_str("]")
            }
            Value::Func(entries) => {
                if entries.is_empty() {
                    return f.write_str("<<>>");
                }
                f.write_str("(")?;
                for (i, (k, v)) in entries.iter().enumerate() {
                    if i > 0 {
                        f.write_str(" @@ ")?;
                    }
                    write!(f, "{k} :> {v}")?;
                }
                f.write_str(")")
            }
        }
    }
}

/// Parses TLA+ literal-value syntax (as printed by TLC in traces).
pub fn parse(src: &str) -> Result<Value> {
    let e = parse_expr(src.trim()).map_err(|e| ValueError(e.to_string()))?;
    from_expr(&e)
}

/// Converts a literal expression into a Value. Identifiers become model
/// values; non-literal expressions are rejected.
pub fn from_expr(e: &Expr) -> Result<Value> {
    match e {
        Expr::Paren(x) => from_expr(x),
        Expr::Bool(b) => Ok(Value::Bool(*b)),
        Expr::Str(s) => Ok(Value::Str(s.clone())),
        Expr::Number(lit) => parse_number(lit),
        Expr::Ident(n) => Ok(Value::Model(n.clone())),
        Expr::Unary { op, x } if op == "-" => match from_expr(x)? {
            Value::Int(i) => Ok(Value::Int(-i)),
            v => err(format!("unary - applied to non-integer value {v}")),
        },
        Expr::SetEnum(es) => Ok(Value::set(
            es.iter().map(from_expr).collect::<Result<Vec<_>>>()?,
        )),
        Expr::Tuple(es) => Ok(Value::Tuple(
            es.iter().map(from_expr).collect::<Result<Vec<_>>>()?,
        )),
        Expr::RecordLit(fields) => Ok(Value::record(
            fields
                .iter()
                .map(|f| Ok((f.name.clone(), from_expr(&f.expr)?)))
                .collect::<Result<Vec<_>>>()?,
        )),
        Expr::Binary { op, l, r } => match op.as_str() {
            ".." => match (from_expr(l)?, from_expr(r)?) {
                (Value::Int(lo), Value::Int(hi)) => Ok(Value::Interval(lo, hi)),
                _ => err("interval bounds must be integers"),
            },
            ":>" => Ok(Value::Func(vec![(from_expr(l)?, from_expr(r)?)])),
            "@@" => match (from_expr(l)?, from_expr(r)?) {
                (Value::Func(mut a), Value::Func(b)) => {
                    a.extend(b);
                    Ok(Value::func(a))
                }
                _ => err("@@ requires function values"),
            },
            _ => err(format!("expression {e} is not a literal value")),
        },
        _ => err(format!("expression {e} is not a literal value")),
    }
}

fn parse_number(lit: &str) -> Result<Value> {
    let (digits, radix) = match lit.as_bytes() {
        [b'\\', b'b' | b'B', ..] => (&lit[2..], 2),
        [b'\\', b'o' | b'O', ..] => (&lit[2..], 8),
        [b'\\', b'h' | b'H', ..] => (&lit[2..], 16),
        _ => (lit, 10),
    };
    if digits.contains('.') {
        return err(format!("real number literals are not supported: {lit}"));
    }
    match BigInt::parse_bytes(digits.as_bytes(), radix) {
        Some(x) => Ok(Value::Int(x)),
        None => err(format!("invalid number literal {lit:?}")),
    }
}

// -------------------------------------------------------------- ITF

/// Largest magnitude ITF encodes as a plain JSON number (2^53 - 1).
const MAX_SAFE_ITF_INT: i64 = (1 << 53) - 1;
/// Bound on Interval-to-#set expansion in [`to_itf`].
const MAX_INTERVAL_EXPANSION: i64 = 1 << 16;

/// Converts a Value to its ITF JSON representation (Apalache ADR-015).
pub fn to_itf(v: &Value) -> Result<serde_json::Value> {
    Ok(match v {
        Value::Bool(b) => json!(b),
        Value::Int(x) => {
            if x >= &BigInt::from(-MAX_SAFE_ITF_INT) && x <= &BigInt::from(MAX_SAFE_ITF_INT) {
                let (_, digits) = x.to_u64_digits();
                let mag = digits.first().copied().unwrap_or(0) as i64;
                let n = if x.sign() == num_bigint::Sign::Minus {
                    -mag
                } else {
                    mag
                };
                json!(n)
            } else {
                json!({"#bigint": x.to_string()})
            }
        }
        Value::Str(s) | Value::Model(s) => json!(s),
        Value::Set(es) => json!({"#set": itf_slice(es)?}),
        Value::Tuple(es) => json!({"#tup": itf_slice(es)?}),
        Value::Record(fields) => {
            let mut m = Map::new();
            for (n, fv) in fields {
                m.insert(n.clone(), to_itf(fv)?);
            }
            serde_json::Value::Object(m)
        }
        Value::Func(entries) => {
            let mut pairs = Vec::new();
            for (k, val) in entries {
                pairs.push(json!([to_itf(k)?, to_itf(val)?]));
            }
            json!({"#map": pairs})
        }
        Value::Interval(lo, hi) => {
            if hi - lo > BigInt::from(MAX_INTERVAL_EXPANSION) {
                return err(format!("interval {v} too large to expand for ITF"));
            }
            let mut elems = Vec::new();
            let mut i = lo.clone();
            while &i <= hi {
                elems.push(to_itf(&Value::Int(i.clone()))?);
                i += 1;
            }
            json!({"#set": elems})
        }
    })
}

fn itf_slice(vs: &[Value]) -> Result<Vec<serde_json::Value>> {
    vs.iter().map(to_itf).collect()
}

/// Converts a decoded ITF JSON tree into a Value. JSON strings decode as
/// `Str`; ITF cannot distinguish strings from model values.
pub fn from_itf(x: &serde_json::Value) -> Result<Value> {
    use serde_json::Value as J;
    match x {
        J::Bool(b) => Ok(Value::Bool(*b)),
        J::String(s) => Ok(Value::Str(s.clone())),
        J::Number(n) => number_to_int(n),
        J::Array(arr) => Ok(Value::Tuple(
            arr.iter().map(from_itf).collect::<Result<Vec<_>>>()?,
        )),
        J::Object(m) => {
            if let Some(b) = m.get("#bigint") {
                let s = b
                    .as_str()
                    .ok_or(ValueError("#bigint must be a string".into()))?;
                return match BigInt::parse_bytes(s.as_bytes(), 10) {
                    Some(x) => Ok(Value::Int(x)),
                    None => err(format!("invalid #bigint {s:?}")),
                };
            }
            if let Some(t) = m.get("#tup") {
                let arr = t
                    .as_array()
                    .ok_or(ValueError("#tup must be an array".into()))?;
                return Ok(Value::Tuple(
                    arr.iter().map(from_itf).collect::<Result<Vec<_>>>()?,
                ));
            }
            if let Some(s) = m.get("#set") {
                let arr = s
                    .as_array()
                    .ok_or(ValueError("#set must be an array".into()))?;
                return Ok(Value::set(
                    arr.iter().map(from_itf).collect::<Result<Vec<_>>>()?,
                ));
            }
            if let Some(mp) = m.get("#map") {
                let arr = mp
                    .as_array()
                    .ok_or(ValueError("#map must be an array of pairs".into()))?;
                let mut entries = Vec::new();
                for p in arr {
                    let pair = p
                        .as_array()
                        .filter(|a| a.len() == 2)
                        .ok_or(ValueError("#map entries must be [key, value] pairs".into()))?;
                    entries.push((from_itf(&pair[0])?, from_itf(&pair[1])?));
                }
                return Ok(Value::func(entries));
            }
            if let Some(u) = m.get("#unserializable") {
                return Ok(Value::Model(u.as_str().unwrap_or("").to_string()));
            }
            Ok(Value::record(
                m.iter()
                    .map(|(k, v)| Ok((k.clone(), from_itf(v)?)))
                    .collect::<Result<Vec<_>>>()?,
            ))
        }
        J::Null => err("cannot decode null from ITF"),
    }
}

fn number_to_int(n: &Number) -> Result<Value> {
    if let Some(i) = n.as_i64() {
        return Ok(Value::int(i));
    }
    if let Some(u) = n.as_u64() {
        return Ok(Value::Int(BigInt::from(u)));
    }
    err(format!("ITF number {n} is not an integer"))
}

/// Renders a Value as ITF JSON text.
pub fn marshal_itf(v: &Value) -> Result<String> {
    Ok(to_itf(v)?.to_string())
}

/// Parses ITF JSON text into a Value.
pub fn unmarshal_itf(data: &str) -> Result<Value> {
    let x: serde_json::Value = serde_json::from_str(data).map_err(|e| ValueError(e.to_string()))?;
    from_itf(&x)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn round_trip(src: &str) -> Value {
        let v = parse(src).unwrap_or_else(|e| panic!("parse {src:?}: {e}"));
        let v2 =
            parse(&v.to_string()).unwrap_or_else(|e| panic!("reparse {} (from {src:?}): {e}", v));
        assert_eq!(v, v2, "round trip changed value");
        v
    }

    #[test]
    fn parse_and_print() {
        for (src, want) in [
            ("TRUE", "TRUE"),
            ("42", "42"),
            ("-17", "-17"),
            ("\"hello\"", "\"hello\""),
            ("mv1", "mv1"),
            ("{3, 1, 2}", "{1, 2, 3}"),
            ("{1, 1, 2}", "{1, 2}"),
            ("<<1, \"a\", TRUE>>", "<<1, \"a\", TRUE>>"),
            ("<<>>", "<<>>"),
            ("[b |-> 2, a |-> 1]", "[a |-> 1, b |-> 2]"),
            ("(2 :> \"b\" @@ 1 :> \"a\")", "(1 :> \"a\" @@ 2 :> \"b\")"),
            ("1..5", "1..5"),
            ("\\b0110", "6"),
            ("\\hFF", "255"),
        ] {
            assert_eq!(round_trip(src).to_string(), want, "for {src:?}");
        }
    }

    #[test]
    fn parses_tlc_state_output() {
        let v = parse("<<[n |-> 0, ok |-> TRUE], [n |-> 1, ok |-> FALSE]>>").unwrap();
        let Value::Tuple(es) = &v else { panic!() };
        assert_eq!(es.len(), 2);
        let Value::Record(fields) = &es[1] else {
            panic!()
        };
        assert_eq!(fields[0], ("n".into(), Value::int(1)));
    }

    #[test]
    fn rejects_non_literals() {
        for src in ["x + 1", "{x \\in S : x}", "f[3]"] {
            assert!(parse(src).is_err(), "{src:?} should fail");
        }
    }

    #[test]
    fn itf_round_trip() {
        for src in [
            "TRUE",
            "42",
            "\"hi\"",
            "{1, 2}",
            "<<1, <<2, 3>>>>",
            "[a |-> 1, b |-> {2}]",
            "(1 :> \"a\" @@ 2 :> \"b\")",
        ] {
            let v = parse(src).unwrap();
            let data = marshal_itf(&v).unwrap();
            let v2 = unmarshal_itf(&data).unwrap();
            assert_eq!(v, v2, "ITF round trip via {data}");
        }
    }

    #[test]
    fn itf_bigint() {
        let huge = BigInt::parse_bytes(b"123456789012345678901234567890", 10).unwrap();
        let v = Value::Int(huge);
        let data = marshal_itf(&v).unwrap();
        assert_eq!(data, "{\"#bigint\":\"123456789012345678901234567890\"}");
        assert_eq!(unmarshal_itf(&data).unwrap(), v);
    }

    #[test]
    fn itf_interval_expands() {
        let v = parse("1..3").unwrap();
        let data = marshal_itf(&v).unwrap();
        let v2 = unmarshal_itf(&data).unwrap();
        assert_eq!(
            v2,
            Value::set(vec![Value::int(1), Value::int(2), Value::int(3)])
        );
    }

    #[test]
    fn from_conversions() {
        assert_eq!(Value::from(3i64).to_string(), "3");
        assert_eq!(Value::from("x").to_string(), "\"x\"");
        assert_eq!(Value::from(vec![1i64, 2]).to_string(), "<<1, 2>>");
    }
}
