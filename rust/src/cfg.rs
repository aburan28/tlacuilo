//! TLC model-checker configuration (.cfg) files: generate and parse.

use crate::scanner::Scanner;
use crate::token::{Kind, Token};

/// One CONSTANT entry: an assignment `Name = Value` (`value` holds TLA+
/// constant-value text such as `3`, `"s"`, `mv`, or `{m1, m2}`) or an
/// operator replacement `Name <- Replacement`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Constant {
    pub name: String,
    pub value: String,
    pub replacement: String,
}

impl Constant {
    pub fn assign(name: impl Into<String>, value: impl Into<String>) -> Self {
        Constant {
            name: name.into(),
            value: value.into(),
            replacement: String::new(),
        }
    }

    pub fn replace(name: impl Into<String>, replacement: impl Into<String>) -> Self {
        Constant {
            name: name.into(),
            value: String::new(),
            replacement: replacement.into(),
        }
    }
}

/// A TLC run configuration.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Config {
    /// The behavior-spec formula; mutually exclusive with init/next.
    pub specification: String,
    pub init: String,
    pub next: String,
    pub invariants: Vec<String>,
    pub properties: Vec<String>,
    pub constants: Vec<Constant>,
    pub symmetry: String,
    pub view: String,
    pub constraints: Vec<String>,
    pub action_constraints: Vec<String>,
    /// Emits `CHECK_DEADLOCK TRUE/FALSE` when set.
    pub check_deadlock: Option<bool>,
    pub alias: String,
    pub post_condition: String,
}

#[derive(Clone, Debug)]
pub struct CfgError(pub String);

impl std::fmt::Display for CfgError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for CfgError {}

impl Config {
    /// Reports configurations TLC would reject.
    pub fn validate(&self) -> Result<(), CfgError> {
        if !self.specification.is_empty() && (!self.init.is_empty() || !self.next.is_empty()) {
            return Err(CfgError(
                "SPECIFICATION and INIT/NEXT are mutually exclusive".into(),
            ));
        }
        if self.specification.is_empty() && self.init.is_empty() != self.next.is_empty() {
            return Err(CfgError("INIT and NEXT must be given together".into()));
        }
        Ok(())
    }

    /// Renders the configuration as .cfg file text.
    pub fn format(&self) -> String {
        let mut b = String::new();
        let mut sect = |kw: &str, v: &str| {
            if !v.is_empty() {
                b.push_str(&format!("{kw} {v}\n"));
            }
        };
        sect("SPECIFICATION", &self.specification);
        sect("INIT", &self.init);
        sect("NEXT", &self.next);
        if !self.constants.is_empty() {
            b.push_str("CONSTANTS\n");
            for k in &self.constants {
                if !k.replacement.is_empty() {
                    b.push_str(&format!("    {} <- {}\n", k.name, k.replacement));
                } else {
                    b.push_str(&format!("    {} = {}\n", k.name, k.value));
                }
            }
        }
        for inv in &self.invariants {
            b.push_str(&format!("INVARIANT {inv}\n"));
        }
        for p in &self.properties {
            b.push_str(&format!("PROPERTY {p}\n"));
        }
        for c in &self.constraints {
            b.push_str(&format!("CONSTRAINT {c}\n"));
        }
        for c in &self.action_constraints {
            b.push_str(&format!("ACTION_CONSTRAINT {c}\n"));
        }
        if !self.symmetry.is_empty() {
            b.push_str(&format!("SYMMETRY {}\n", self.symmetry));
        }
        if !self.view.is_empty() {
            b.push_str(&format!("VIEW {}\n", self.view));
        }
        if let Some(cd) = self.check_deadlock {
            b.push_str(&format!(
                "CHECK_DEADLOCK {}\n",
                if cd { "TRUE" } else { "FALSE" }
            ));
        }
        if !self.alias.is_empty() {
            b.push_str(&format!("ALIAS {}\n", self.alias));
        }
        if !self.post_condition.is_empty() {
            b.push_str(&format!("POSTCONDITION {}\n", self.post_condition));
        }
        b
    }
}

fn section_keyword(t: &Token) -> Option<&'static str> {
    let name = match t.kind {
        Kind::Constant | Kind::Constants => return Some("CONSTANT"),
        Kind::Ident => t.lit.as_str(),
        _ => return None,
    };
    Some(match name {
        "SPECIFICATION" => "SPECIFICATION",
        "INIT" => "INIT",
        "NEXT" => "NEXT",
        "INVARIANT" | "INVARIANTS" => "INVARIANT",
        "PROPERTY" | "PROPERTIES" => "PROPERTY",
        "SYMMETRY" => "SYMMETRY",
        "VIEW" => "VIEW",
        "CONSTRAINT" | "CONSTRAINTS" => "CONSTRAINT",
        "ACTION_CONSTRAINT" | "ACTION_CONSTRAINTS" => "ACTION_CONSTRAINT",
        "CHECK_DEADLOCK" => "CHECK_DEADLOCK",
        "ALIAS" => "ALIAS",
        "POSTCONDITION" => "POSTCONDITION",
        _ => return None,
    })
}

fn tok_name(t: &Token) -> String {
    if !t.lit.is_empty() {
        t.lit.clone()
    } else {
        t.kind.name().to_string()
    }
}

/// Parses .cfg file text.
pub fn parse(src: &str) -> Result<Config, CfgError> {
    let mut sc = Scanner::new(src);
    let toks = sc.scan_all();
    if let Some(e) = sc.errors().first() {
        return Err(CfgError(e.to_string()));
    }
    let mut c = Config::default();
    let mut section = "";
    let mut i = 0usize;
    let at = |toks: &[Token], n: usize| -> Token { toks[n.min(toks.len() - 1)].clone() };
    while at(&toks, i).kind != Kind::Eof {
        let t = at(&toks, i);
        if let Some(s) = section_keyword(&t) {
            // A section keyword directly after CONSTANT would be a
            // constant named like a keyword, which TLC does not allow —
            // treating every keyword as a section switch is safe.
            section = s;
            i += 1;
            continue;
        }
        match section {
            "SPECIFICATION" => {
                c.specification = tok_name(&t);
                i += 1;
            }
            "INIT" => {
                c.init = tok_name(&t);
                i += 1;
            }
            "NEXT" => {
                c.next = tok_name(&t);
                i += 1;
            }
            "INVARIANT" => {
                c.invariants.push(tok_name(&t));
                i += 1;
            }
            "PROPERTY" => {
                c.properties.push(tok_name(&t));
                i += 1;
            }
            "SYMMETRY" => {
                c.symmetry = tok_name(&t);
                i += 1;
            }
            "VIEW" => {
                c.view = tok_name(&t);
                i += 1;
            }
            "CONSTRAINT" => {
                c.constraints.push(tok_name(&t));
                i += 1;
            }
            "ACTION_CONSTRAINT" => {
                c.action_constraints.push(tok_name(&t));
                i += 1;
            }
            "ALIAS" => {
                c.alias = tok_name(&t);
                i += 1;
            }
            "POSTCONDITION" => {
                c.post_condition = tok_name(&t);
                i += 1;
            }
            "CHECK_DEADLOCK" => {
                c.check_deadlock = Some(t.kind == Kind::True);
                i += 1;
            }
            "CONSTANT" => {
                if t.kind != Kind::Ident {
                    return Err(CfgError(format!(
                        "{}: expected constant name, found {t}",
                        t.pos
                    )));
                }
                let name = t.lit.clone();
                i += 1;
                let nxt = at(&toks, i);
                match nxt.kind {
                    Kind::LArrow => {
                        i += 1;
                        let r = at(&toks, i);
                        if r.kind != Kind::Ident {
                            return Err(CfgError(format!(
                                "{}: expected operator name after <-",
                                r.pos
                            )));
                        }
                        i += 1;
                        c.constants.push(Constant::replace(name, r.lit));
                    }
                    Kind::Op if nxt.lit == "=" => {
                        i += 1;
                        let val = scan_constant_value(src, &toks, &mut i)?;
                        c.constants.push(Constant::assign(name, val));
                    }
                    _ => {
                        return Err(CfgError(format!(
                            "{}: expected = or <- after {name}, found {nxt}",
                            nxt.pos
                        )))
                    }
                }
            }
            _ => {
                return Err(CfgError(format!(
                    "{}: unexpected {t} outside any section",
                    t.pos
                )))
            }
        }
    }
    Ok(c)
}

/// Consumes the balanced token run forming a constant value and returns
/// the corresponding source text.
fn scan_constant_value(src: &str, toks: &[Token], i: &mut usize) -> Result<String, CfgError> {
    let at = |n: usize| -> &Token { &toks[n.min(toks.len() - 1)] };
    let start = at(*i).pos.offset;
    let mut end = start;
    let mut depth = 0i32;
    let mut first = true;
    loop {
        let t = at(*i).clone();
        if t.kind == Kind::Eof {
            break;
        }
        if depth == 0 && !first {
            if section_keyword(&t).is_some() && t.kind != Kind::Ident {
                break;
            }
            if t.kind == Kind::Ident && section_keyword(&t).is_some() {
                break;
            }
            if t.kind == Kind::Ident {
                let nt = at(*i + 1);
                if nt.kind == Kind::LArrow || (nt.kind == Kind::Op && nt.lit == "=") {
                    break;
                }
            }
        }
        match t.kind {
            Kind::LBrace | Kind::LBrack | Kind::LParen | Kind::LTup => depth += 1,
            Kind::RBrace | Kind::RBrack | Kind::RParen | Kind::RTup => depth -= 1,
            _ => {}
        }
        end = t.pos.offset + token_byte_len(src, &t);
        *i += 1;
        first = false;
        if depth == 0 && closes_value(&t) && value_done(at(*i)) {
            break;
        }
    }
    if end <= start {
        return Err(CfgError("empty constant value".into()));
    }
    Ok(src[start..end].trim().to_string())
}

fn closes_value(t: &Token) -> bool {
    matches!(
        t.kind,
        Kind::Ident
            | Kind::Number
            | Kind::Str
            | Kind::True
            | Kind::False
            | Kind::RBrace
            | Kind::RBrack
            | Kind::RParen
            | Kind::RTup
    )
}

fn value_done(next: &Token) -> bool {
    !matches!(next.kind, Kind::Op | Kind::Comma | Kind::Dot)
}

fn token_byte_len(src: &str, t: &Token) -> usize {
    match t.kind {
        Kind::Str => {
            // The lit is decoded; measure from source: find closing quote.
            let bytes = src.as_bytes();
            let mut j = t.pos.offset + 1;
            while j < bytes.len() {
                match bytes[j] {
                    b'\\' => j += 2,
                    b'"' => return j - t.pos.offset + 1,
                    _ => j += 1,
                }
            }
            bytes.len() - t.pos.offset
        }
        Kind::Ident | Kind::Number | Kind::Op => t.lit.len(),
        _ => t.kind.name().len(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn format_and_round_trip() {
        let c = Config {
            init: "Init".into(),
            next: "Next".into(),
            constants: vec![
                Constant::assign("N", "3"),
                Constant::assign("S", "{\"a\", \"b\"}"),
                Constant::assign("M", "m1"),
                Constant::replace("F", "Impl"),
            ],
            invariants: vec!["Inv".into()],
            properties: vec!["Live".into()],
            check_deadlock: Some(false),
            ..Default::default()
        };
        c.validate().unwrap();
        let out = c.format();
        let c2 = parse(&out).unwrap_or_else(|e| panic!("parse:\n{out}\n{e}"));
        assert_eq!(c2.format(), out, "round trip differs");
    }

    #[test]
    fn parse_real_world() {
        let src = "\\* comment line\nSPECIFICATION Spec\nCONSTANTS\n    Data = {d1, d2}\n    msgQLen = 2\n    Null = Null\n    Sort <- SortImpl\nINVARIANTS Inv TypeInv\nPROPERTY Termination\nCHECK_DEADLOCK TRUE\n";
        let c = parse(src).unwrap();
        assert_eq!(c.specification, "Spec");
        assert_eq!(c.constants.len(), 4);
        assert_eq!(c.constants[0].value, "{d1, d2}");
        assert_eq!(c.constants[3].replacement, "SortImpl");
        assert_eq!(c.invariants, vec!["Inv".to_string(), "TypeInv".into()]);
        assert_eq!(c.check_deadlock, Some(true));
    }

    #[test]
    fn validate_rejects() {
        let c = Config {
            specification: "Spec".into(),
            init: "Init".into(),
            ..Default::default()
        };
        assert!(c.validate().is_err());
        let c = Config {
            init: "Init".into(),
            ..Default::default()
        };
        assert!(c.validate().is_err());
    }
}
