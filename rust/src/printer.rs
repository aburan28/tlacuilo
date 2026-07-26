//! Canonical pretty-printer: renders syntax trees as parseable TLA+
//! source with aligned junction lists and minimal parenthesization.
//! `parse(print(ast))` is a fixed point.

use crate::ast::*;
use crate::token::{infix_op, postfix_op, prefix_op, OpInfo, PREC_APPLY, PREC_DOT, TIMES_OP};

const HEADER_WIDTH: usize = 77;

/// Renders a module as formatted TLA+ source.
pub fn module_string(m: &Module) -> String {
    let mut p = Printer::new();
    p.module(m);
    p.out
}

/// Renders a single expression.
pub fn expr_string(e: &Expr) -> String {
    let mut p = Printer::new();
    p.expr(e, 0);
    p.out
}

struct Printer {
    out: String,
    col: usize, // 1-based column of the next char written
}

impl Printer {
    fn new() -> Self {
        Printer {
            out: String::new(),
            col: 1,
        }
    }

    fn ws(&mut self, s: &str) {
        self.out.push_str(s);
        match s.rfind('\n') {
            Some(i) => self.col = s[i + 1..].chars().count() + 1,
            None => self.col += s.chars().count(),
        }
    }

    /// Starts a new line indented so the next char lands at column `col`.
    fn nl(&mut self, col: usize) {
        self.out.push('\n');
        let col = col.max(1);
        for _ in 1..col {
            self.out.push(' ');
        }
        self.col = col;
    }

    fn module(&mut self, m: &Module) {
        let title = format!(" MODULE {} ", m.name);
        let pad = HEADER_WIDTH.saturating_sub(title.chars().count());
        let left = (pad / 2).max(4);
        let right = (pad - pad / 2).max(4);
        self.ws(&format!(
            "{}{}{}",
            "-".repeat(left),
            title,
            "-".repeat(right)
        ));
        self.nl(1);
        if !m.extends.is_empty() {
            self.ws(&format!("EXTENDS {}", m.extends.join(", ")));
            self.nl(1);
        }
        for u in &m.units {
            self.nl(1);
            self.unit(u);
            self.nl(1);
        }
        self.nl(1);
        self.ws(&"=".repeat(HEADER_WIDTH));
        self.nl(1);
    }

    fn unit(&mut self, u: &Unit) {
        match u {
            Unit::Constants(ds) => {
                let kw = if ds.len() == 1 {
                    "CONSTANT"
                } else {
                    "CONSTANTS"
                };
                self.ws(&format!("{} {}", kw, op_decl_list(ds)));
            }
            Unit::Variables(ns) => {
                let kw = if ns.len() == 1 {
                    "VARIABLE"
                } else {
                    "VARIABLES"
                };
                self.ws(&format!("{} {}", kw, ns.join(", ")));
            }
            Unit::OperatorDef {
                local,
                fixity,
                name,
                params,
                body,
            } => {
                if *local {
                    self.ws("LOCAL ");
                }
                match fixity {
                    Fixity::Infix => {
                        self.ws(&format!("{} {} {}", params[0].name, name, params[1].name))
                    }
                    Fixity::Postfix => self.ws(&format!("{} {}", params[0].name, name)),
                    Fixity::PrefixSym => self.ws(&format!("{} {}", name, params[0].name)),
                    Fixity::Named => {
                        self.ws(name);
                        if !params.is_empty() {
                            self.ws(&format!("({})", op_decl_list(params)));
                        }
                    }
                }
                self.ws(" == ");
                self.expr(body, 0);
            }
            Unit::FunctionDef {
                local,
                name,
                bounds,
                body,
            } => {
                if *local {
                    self.ws("LOCAL ");
                }
                self.ws(&format!("{name}["));
                self.bounds(bounds);
                self.ws("] == ");
                self.expr(body, 0);
            }
            Unit::Instance(inst) => self.instance(inst),
            Unit::ModuleDef {
                local,
                name,
                params,
                instance,
            } => {
                if *local {
                    self.ws("LOCAL ");
                }
                self.ws(name);
                if !params.is_empty() {
                    self.ws(&format!("({})", op_decl_list(params)));
                }
                self.ws(" == ");
                self.instance(instance);
            }
            Unit::Assume {
                keyword,
                name,
                expr,
            } => {
                self.ws(keyword);
                self.ws(" ");
                if let Some(n) = name {
                    self.ws(&format!("{n} == "));
                }
                self.expr(expr, 0);
            }
            Unit::Theorem {
                keyword,
                name,
                expr,
            } => {
                self.ws(keyword);
                self.ws(" ");
                if let Some(n) = name {
                    self.ws(&format!("{n} == "));
                }
                self.expr(expr, 0);
            }
            Unit::Recursive(ds) => self.ws(&format!("RECURSIVE {}", op_decl_list(ds))),
            Unit::Separator => self.ws(&"-".repeat(HEADER_WIDTH)),
            Unit::Nested(m) => self.ws(&module_string(m)),
        }
    }

    fn instance(&mut self, inst: &Instance) {
        if inst.local {
            self.ws("LOCAL ");
        }
        self.ws(&format!("INSTANCE {}", inst.module));
        for (i, s) in inst.with.iter().enumerate() {
            self.ws(if i == 0 { " WITH " } else { ", " });
            self.ws(&format!("{} <- ", s.name));
            self.expr(&s.expr, 0);
        }
    }

    fn bounds(&mut self, bs: &[Bound]) {
        for (i, b) in bs.iter().enumerate() {
            if i > 0 {
                self.ws(", ");
            }
            if b.tuple {
                self.ws(&format!("<<{}>>", b.names.join(", ")));
            } else {
                self.ws(&b.names.join(", "));
            }
            if let Some(set) = &b.set {
                self.ws(" \\in ");
                self.expr(set, 0);
            }
        }
    }

    /// Prints `child` as an operand of a context that absorbs only
    /// operators with `lo > min`, parenthesizing when required.
    /// `same_op` is set when the parent is an associative infix operator.
    fn operand(&mut self, child: &Expr, min: i32, same_op: Option<&str>) {
        if needs_parens(child, min, same_op) {
            self.ws("(");
            self.expr(child, 0);
            self.ws(")");
        } else {
            self.expr(child, min);
        }
    }

    fn expr(&mut self, e: &Expr, _min: i32) {
        match e {
            Expr::Ident(n) => self.ws(n),
            Expr::GeneralIdent { prefix, name } => {
                for arm in prefix {
                    self.ws(&arm.name);
                    if !arm.args.is_empty() {
                        self.ws("(");
                        self.expr_list(&arm.args);
                        self.ws(")");
                    }
                    self.ws("!");
                }
                self.ws(name);
            }
            Expr::Number(l) => self.ws(l),
            Expr::Str(s) => self.ws(&quote_tla(s)),
            Expr::Bool(b) => self.ws(if *b { "TRUE" } else { "FALSE" }),
            Expr::OpRef(op) => self.ws(op),
            Expr::Apply { fun, args } => {
                self.expr(fun, 0);
                self.ws("(");
                self.expr_list(args);
                self.ws(")");
            }
            Expr::Unary { op, x } => {
                let info = prefix_op(op).expect("known prefix op");
                match op.as_str() {
                    "~" | "[]" | "<>" | "-" => self.ws(op),
                    _ => self.ws(&format!("{op} ")),
                }
                self.operand(x, info.hi, None);
            }
            Expr::Binary { op, l, r } => {
                let info = infix_op(op).expect("known infix op");
                let chain = if info.assoc { Some(op.as_str()) } else { None };
                self.operand(l, info.hi, chain);
                if op == ".." {
                    self.ws(".."); // conventionally written tight: 1..N
                } else {
                    self.ws(&format!(" {op} "));
                }
                self.operand(r, info.hi, chain);
            }
            Expr::PostfixExpr { op, x } => {
                self.postfix_operand(x);
                self.ws(op);
            }
            Expr::Times(factors) => {
                for (i, f) in factors.iter().enumerate() {
                    if i > 0 {
                        self.ws(" \\X ");
                    }
                    self.operand(f, TIMES_OP.hi, None);
                }
            }
            Expr::Junction { op, items } => {
                let start = self.col;
                for (i, item) in items.iter().enumerate() {
                    if i > 0 {
                        self.nl(start);
                    }
                    self.ws(&format!("{op} "));
                    self.expr(item, 0);
                }
            }
            Expr::Paren(x) => {
                self.ws("(");
                self.expr(x, 0);
                self.ws(")");
            }
            Expr::FnApply { f, args } => {
                self.postfix_operand(f);
                self.ws("[");
                self.expr_list(args);
                self.ws("]");
            }
            Expr::Dot { x, field } => {
                self.postfix_operand(x);
                self.ws(&format!(".{field}"));
            }
            Expr::Tuple(es) => {
                self.ws("<<");
                self.expr_list(es);
                self.ws(">>");
            }
            Expr::SetEnum(es) => {
                self.ws("{");
                self.expr_list(es);
                self.ws("}");
            }
            Expr::SetFilter { bound, pred } => {
                self.ws("{");
                self.bounds(std::slice::from_ref(bound));
                self.ws(" : ");
                self.expr(pred, 0);
                self.ws("}");
            }
            Expr::SetMap { body, bounds } => {
                self.ws("{");
                self.expr(body, 0);
                self.ws(" : ");
                self.bounds(bounds);
                self.ws("}");
            }
            Expr::FuncLit { bounds, body } => {
                self.ws("[");
                self.bounds(bounds);
                self.ws(" |-> ");
                self.expr(body, 0);
                self.ws("]");
            }
            Expr::FuncSet { domain, range } => {
                self.ws("[");
                self.expr(domain, 0);
                self.ws(" -> ");
                self.expr(range, 0);
                self.ws("]");
            }
            Expr::RecordLit(fields) => {
                self.ws("[");
                for (i, f) in fields.iter().enumerate() {
                    if i > 0 {
                        self.ws(", ");
                    }
                    self.ws(&format!("{} |-> ", f.name));
                    self.expr(&f.expr, 0);
                }
                self.ws("]");
            }
            Expr::RecordSet(fields) => {
                self.ws("[");
                for (i, f) in fields.iter().enumerate() {
                    if i > 0 {
                        self.ws(", ");
                    }
                    self.ws(&format!("{} : ", f.name));
                    self.expr(&f.expr, 0);
                }
                self.ws("]");
            }
            Expr::Except { f, specs } => {
                self.ws("[");
                self.expr(f, 0);
                self.ws(" EXCEPT ");
                for (i, s) in specs.iter().enumerate() {
                    if i > 0 {
                        self.ws(", ");
                    }
                    self.ws("!");
                    for step in &s.path {
                        match step {
                            ExceptPath::Field(name) => self.ws(&format!(".{name}")),
                            ExceptPath::Index(idx) => {
                                self.ws("[");
                                self.expr_list(idx);
                                self.ws("]");
                            }
                        }
                    }
                    self.ws(" = ");
                    self.expr(&s.value, 0);
                }
                self.ws("]");
            }
            Expr::At => self.ws("@"),
            Expr::If { cond, then, els } => {
                let start = self.col;
                let multi = is_multiline(cond) || is_multiline(then) || is_multiline(els);
                self.ws("IF ");
                self.expr(cond, 0);
                if multi {
                    self.nl(start + 2);
                    self.ws("THEN ");
                } else {
                    self.ws(" THEN ");
                }
                self.expr(then, 0);
                if multi {
                    self.nl(start + 2);
                    self.ws("ELSE ");
                } else {
                    self.ws(" ELSE ");
                }
                self.expr(els, 0);
            }
            Expr::Case { arms, other } => {
                let start = self.col;
                let mut multi = arms.len() > 2
                    || arms
                        .iter()
                        .any(|a| is_multiline(&a.cond) || is_multiline(&a.value));
                if let Some(o) = other {
                    multi = multi || is_multiline(o);
                }
                self.ws("CASE ");
                for (i, a) in arms.iter().enumerate() {
                    if i > 0 {
                        if multi {
                            self.nl(start + 2);
                        } else {
                            self.ws(" ");
                        }
                        self.ws("[] ");
                    }
                    self.case_arm_part(&a.cond);
                    self.ws(" -> ");
                    self.case_arm_part(&a.value);
                }
                if let Some(o) = other {
                    if multi {
                        self.nl(start + 2);
                    } else {
                        self.ws(" ");
                    }
                    self.ws("[] OTHER -> ");
                    self.expr(o, 0);
                }
            }
            Expr::Let { defs, body } => {
                let start = self.col;
                self.ws("LET ");
                let def_col = self.col;
                for (i, d) in defs.iter().enumerate() {
                    if i > 0 {
                        self.nl(def_col);
                    }
                    self.unit(d);
                }
                self.nl(start);
                self.ws("IN  ");
                self.expr(body, 0);
            }
            Expr::Quant { kind, bounds, body } => {
                let kw = match kind {
                    QuantKind::Forall => "\\A",
                    QuantKind::Exists => "\\E",
                    QuantKind::TemporalForall => "\\AA",
                    QuantKind::TemporalExists => "\\EE",
                };
                self.ws(&format!("{kw} "));
                self.bounds(bounds);
                self.ws(" : ");
                self.expr(body, 0);
            }
            Expr::Choose {
                var,
                tuple,
                set,
                body,
            } => {
                self.ws("CHOOSE ");
                if !tuple.is_empty() {
                    self.ws(&format!("<<{}>>", tuple.join(", ")));
                } else {
                    self.ws(var);
                }
                if let Some(s) = set {
                    self.ws(" \\in ");
                    self.expr(s, 0);
                }
                self.ws(" : ");
                self.expr(body, 0);
            }
            Expr::SquareAct { x, sub } => {
                self.ws("[");
                self.expr(x, 0);
                self.ws("]_");
                self.subscript(sub);
            }
            Expr::AngleAct { x, sub } => {
                self.ws("<<");
                self.expr(x, 0);
                self.ws(">>_");
                self.subscript(sub);
            }
            Expr::Fairness { strong, sub, x } => {
                self.ws(if *strong { "SF_" } else { "WF_" });
                self.subscript(sub);
                self.ws("(");
                self.expr(x, 0);
                self.ws(")");
            }
            Expr::Lambda { params, body } => {
                self.ws(&format!("LAMBDA {} : ", params.join(", ")));
                self.expr(body, 0);
            }
        }
    }

    /// Operand of a tight postfix construct (function application, field
    /// selection, prime): only name-like/bracketed expressions stay bare.
    fn postfix_operand(&mut self, x: &Expr) {
        match x {
            Expr::Ident(_)
            | Expr::GeneralIdent { .. }
            | Expr::Apply { .. }
            | Expr::FnApply { .. }
            | Expr::Dot { .. }
            | Expr::Paren(_)
            | Expr::Tuple(_)
            | Expr::SetEnum(_)
            | Expr::RecordLit(_)
            | Expr::FuncLit { .. }
            | Expr::Except { .. }
            | Expr::Str(_)
            | Expr::Number(_)
            | Expr::PostfixExpr { .. } => self.expr(x, 0),
            _ => {
                self.ws("(");
                self.expr(x, 0);
                self.ws(")");
            }
        }
    }

    /// The `_v` subscript of `[A]_v`, `<<A>>_v`, `WF_v`, `SF_v`.
    fn subscript(&mut self, x: &Expr) {
        match x {
            Expr::Ident(_)
            | Expr::GeneralIdent { .. }
            | Expr::Tuple(_)
            | Expr::Paren(_)
            | Expr::Dot { .. } => self.expr(x, 0),
            _ => {
                self.ws("(");
                self.expr(x, 0);
                self.ws(")");
            }
        }
    }

    /// A CASE arm component; greedy constructs would swallow the `->` or
    /// `[]` that follows, so they are parenthesized.
    fn case_arm_part(&mut self, e: &Expr) {
        if expr_shape(e).greedy {
            self.ws("(");
            self.expr(e, 0);
            self.ws(")");
        } else {
            self.expr(e, 0);
        }
    }

    fn expr_list(&mut self, es: &[Expr]) {
        for (i, e) in es.iter().enumerate() {
            if i > 0 {
                self.ws(", ");
            }
            self.expr(e, 0);
        }
    }
}

struct Shape {
    info: Option<OpInfo>,
    op: Option<String>,
    /// Extends as far right as possible; must be parenthesized under any
    /// operator.
    greedy: bool,
}

fn expr_shape(e: &Expr) -> Shape {
    let (info, op, greedy) = match e {
        Expr::Binary { op, .. } => (infix_op(op), Some(op.clone()), false),
        Expr::Unary { op, .. } => (prefix_op(op), Some(op.clone()), false),
        Expr::PostfixExpr { op, .. } => (postfix_op(op), Some(op.clone()), false),
        Expr::Times(_) => (Some(TIMES_OP), Some("\\X".into()), false),
        Expr::FnApply { .. } => (
            Some(OpInfo {
                lo: PREC_APPLY,
                hi: PREC_APPLY,
                assoc: false,
            }),
            None,
            false,
        ),
        Expr::Dot { .. } => (
            Some(OpInfo {
                lo: PREC_DOT,
                hi: PREC_DOT,
                assoc: false,
            }),
            None,
            false,
        ),
        Expr::Junction { .. }
        | Expr::If { .. }
        | Expr::Case { .. }
        | Expr::Let { .. }
        | Expr::Quant { .. }
        | Expr::Choose { .. }
        | Expr::Lambda { .. } => (None, None, true),
        _ => (None, None, false),
    };
    Shape { info, op, greedy }
}

fn needs_parens(child: &Expr, min: i32, same_op: Option<&str>) -> bool {
    let shape = expr_shape(child);
    if shape.greedy {
        return min > 0;
    }
    let info = match shape.info {
        Some(i) => i,
        None => return false, // atom
    };
    if let (Some(same), Some(op)) = (same_op, &shape.op) {
        if same == op {
            return false;
        }
    }
    info.lo <= min
}

fn is_multiline(e: &Expr) -> bool {
    match e {
        Expr::Junction { .. } | Expr::Let { .. } => true,
        Expr::Binary { l, r, .. } => is_multiline(l) || is_multiline(r),
        Expr::Unary { x, .. } | Expr::PostfixExpr { op: _, x } | Expr::Paren(x) => is_multiline(x),
        Expr::If { cond, then, els } => {
            is_multiline(cond) || is_multiline(then) || is_multiline(els)
        }
        Expr::Case { arms, other } => {
            arms.len() > 2
                || arms
                    .iter()
                    .any(|a| is_multiline(&a.cond) || is_multiline(&a.value))
                || other.as_deref().is_some_and(is_multiline)
        }
        Expr::Quant { body, .. } => is_multiline(body),
        Expr::SquareAct { x, .. } | Expr::AngleAct { x, .. } => is_multiline(x),
        _ => false,
    }
}

fn op_decl_string(d: &OpDecl) -> String {
    if d.arity == 0 {
        d.name.clone()
    } else {
        let holes = vec!["_"; d.arity as usize].join(", ");
        format!("{}({})", d.name, holes)
    }
}

fn op_decl_list(ds: &[OpDecl]) -> String {
    ds.iter().map(op_decl_string).collect::<Vec<_>>().join(", ")
}

fn quote_tla(s: &str) -> String {
    let mut b = String::with_capacity(s.len() + 2);
    b.push('"');
    for c in s.chars() {
        match c {
            '"' => b.push_str("\\\""),
            '\\' => b.push_str("\\\\"),
            '\t' => b.push_str("\\t"),
            '\n' => b.push_str("\\n"),
            '\x0c' => b.push_str("\\f"),
            '\r' => b.push_str("\\r"),
            other => b.push(other),
        }
    }
    b.push('"');
    b
}
