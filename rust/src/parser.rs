//! Parser for TLA+ modules and expressions: the module language and the
//! full constant/action/temporal expression language (the TLA+2 proof
//! language is not supported). Junction lists are column-sensitive; the
//! parser enforces the alignment rules by filtering the token stream
//! against a stack of active bullet columns.

use crate::ast::*;
use crate::scanner::Scanner;
use crate::token::{
    self, infix_op, postfix_op, prefix_op, Kind, Pos, Token, PREC_APPLY, PREC_DOT, TIMES_OP,
};

/// A parse error at a source position.
#[derive(Clone, Debug)]
pub struct ParseError {
    pub pos: Pos,
    pub msg: String,
}

impl std::fmt::Display for ParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.pos, self.msg)
    }
}

impl std::error::Error for ParseError {}

type Result<T> = std::result::Result<T, ParseError>;

/// Parses a complete TLA+ module.
pub fn parse(src: &str) -> Result<Module> {
    let mut p = Parser::new(src)?;
    let m = p.parse_module()?;
    Ok(m)
}

/// Parses a single TLA+ expression, requiring the whole input.
pub fn parse_expr(src: &str) -> Result<Expr> {
    let mut p = Parser::new(src)?;
    let e = p.parse_expr(0)?;
    if p.cur().kind != Kind::Eof {
        return Err(p.err(format!("unexpected {} after expression", p.cur())));
    }
    Ok(e)
}

struct Parser {
    toks: Vec<Token>,
    i: usize,
    jcol: Vec<u32>, // stack of active junction-list bullet columns
}

impl Parser {
    fn new(src: &str) -> Result<Parser> {
        let mut sc = Scanner::new(src);
        let toks = sc.scan_all();
        if let Some(e) = sc.errors().first() {
            return Err(ParseError {
                pos: e.pos,
                msg: e.msg.clone(),
            });
        }
        Ok(Parser {
            toks,
            i: 0,
            jcol: Vec::new(),
        })
    }

    fn cur(&self) -> &Token {
        self.at(0)
    }

    fn at(&self, n: usize) -> &Token {
        let idx = (self.i + n).min(self.toks.len() - 1);
        &self.toks[idx]
    }

    fn advance(&mut self) -> Token {
        let t = self.toks[self.i].clone();
        if self.i < self.toks.len() - 1 {
            self.i += 1;
        }
        t
    }

    fn err(&self, msg: String) -> ParseError {
        ParseError {
            pos: self.cur().pos,
            msg,
        }
    }

    fn expect(&mut self, k: Kind) -> Result<Token> {
        if self.cur().kind != k {
            return Err(self.err(format!("expected {}, found {}", k.name(), self.cur())));
        }
        Ok(self.advance())
    }

    fn expect_ident(&mut self) -> Result<String> {
        Ok(self.expect(Kind::Ident)?.lit)
    }

    /// Whether the current token is claimed by an enclosing junction
    /// list: any token at or left of the innermost bullet column
    /// terminates the current junct item.
    fn blocked(&self) -> bool {
        match self.jcol.last() {
            None => false,
            Some(&top) => {
                let t = self.cur();
                t.kind == Kind::Eof || t.pos.col <= top
            }
        }
    }

    // ------------------------------------------------------- modules

    fn parse_module(&mut self) -> Result<Module> {
        self.expect(Kind::Dashes)?;
        self.expect(Kind::Module)?;
        let name = self.expect_ident()?;
        self.expect(Kind::Dashes)?;
        let mut m = Module {
            name,
            ..Default::default()
        };
        if self.cur().kind == Kind::Extends {
            self.advance();
            loop {
                m.extends.push(self.expect_ident()?);
                if self.cur().kind != Kind::Comma {
                    break;
                }
                self.advance();
            }
        }
        loop {
            match self.cur().kind {
                Kind::ModEnd => {
                    self.advance();
                    return Ok(m);
                }
                Kind::Eof => {
                    return Err(self.err(format!("missing ==== at end of module {}", m.name)))
                }
                _ => m.units.push(self.parse_unit()?),
            }
        }
    }

    fn parse_unit(&mut self) -> Result<Unit> {
        let t = self.cur().clone();
        match t.kind {
            Kind::Dashes => {
                if self.at(1).kind == Kind::Module {
                    return Ok(Unit::Nested(Box::new(self.parse_module()?)));
                }
                self.advance();
                Ok(Unit::Separator)
            }
            Kind::Constant | Kind::Constants => {
                self.advance();
                Ok(Unit::Constants(self.parse_op_decl_list()?))
            }
            Kind::Variable | Kind::Variables => {
                self.advance();
                let mut names = Vec::new();
                loop {
                    names.push(self.expect_ident()?);
                    if self.cur().kind != Kind::Comma {
                        break;
                    }
                    self.advance();
                }
                Ok(Unit::Variables(names))
            }
            Kind::Recursive => {
                self.advance();
                Ok(Unit::Recursive(self.parse_op_decl_list()?))
            }
            Kind::Assume | Kind::Assumption | Kind::Axiom => {
                self.advance();
                let name = self.optional_def_name()?;
                Ok(Unit::Assume {
                    keyword: t.kind.name().to_string(),
                    name,
                    expr: self.parse_expr(0)?,
                })
            }
            Kind::Theorem | Kind::Lemma | Kind::Proposition | Kind::Corollary => {
                self.advance();
                let name = self.optional_def_name()?;
                Ok(Unit::Theorem {
                    keyword: t.kind.name().to_string(),
                    name,
                    expr: self.parse_expr(0)?,
                })
            }
            Kind::Local => {
                self.advance();
                self.parse_local_unit()
            }
            Kind::Instance => Ok(Unit::Instance(self.parse_instance(false)?)),
            Kind::Ident => self.parse_definition(false),
            Kind::Op if is_definable_prefix_op(&t.lit) => self.parse_prefix_op_def(false),
            _ => Err(self.err(format!("unexpected {t} at module level"))),
        }
    }

    fn optional_def_name(&mut self) -> Result<Option<String>> {
        if self.cur().kind == Kind::Ident && self.at(1).kind == Kind::DefEq {
            let n = self.advance().lit;
            self.advance();
            Ok(Some(n))
        } else {
            Ok(None)
        }
    }

    fn parse_local_unit(&mut self) -> Result<Unit> {
        match self.cur().kind {
            Kind::Instance => Ok(Unit::Instance(self.parse_instance(true)?)),
            Kind::Ident => self.parse_definition(true),
            Kind::Op if is_definable_prefix_op(&self.cur().lit) => self.parse_prefix_op_def(true),
            _ => Err(self.err(format!(
                "expected definition or INSTANCE after LOCAL, found {}",
                self.cur()
            ))),
        }
    }

    fn parse_instance(&mut self, local: bool) -> Result<Instance> {
        self.expect(Kind::Instance)?;
        let module = self.expect_ident()?;
        let mut with = Vec::new();
        if self.cur().kind == Kind::With {
            self.advance();
            loop {
                let name = self.expect_ident()?;
                self.expect(Kind::LArrow)?;
                with.push(Subst {
                    name,
                    expr: self.parse_expr(0)?,
                });
                if self.cur().kind != Kind::Comma {
                    break;
                }
                self.advance();
            }
        }
        Ok(Instance {
            local,
            module,
            with,
        })
    }

    /// Units introduced by an identifier: operator, function, and module
    /// definitions, including infix/postfix symbol forms.
    fn parse_definition(&mut self, local: bool) -> Result<Unit> {
        let name = self.expect_ident()?;
        match self.cur().kind {
            Kind::LBrack => {
                self.advance();
                let bounds = self.parse_bounds(true)?;
                self.expect(Kind::RBrack)?;
                self.expect(Kind::DefEq)?;
                Ok(Unit::FunctionDef {
                    local,
                    name,
                    bounds,
                    body: self.parse_expr(0)?,
                })
            }
            Kind::LParen => {
                self.advance();
                let mut params = Vec::new();
                loop {
                    params.push(self.parse_op_decl()?);
                    if self.cur().kind != Kind::Comma {
                        break;
                    }
                    self.advance();
                }
                self.expect(Kind::RParen)?;
                self.expect(Kind::DefEq)?;
                self.finish_operator_def(local, Fixity::Named, name, params)
            }
            Kind::DefEq => {
                self.advance();
                self.finish_operator_def(local, Fixity::Named, name, Vec::new())
            }
            _ => {
                if let Some(op) = self.definable_op() {
                    if postfix_op(&op).is_some() && self.at(1).kind == Kind::DefEq {
                        self.advance();
                        self.advance();
                        return self.finish_operator_def(
                            local,
                            Fixity::Postfix,
                            op,
                            vec![OpDecl::plain(name)],
                        );
                    }
                    if self.at(1).kind == Kind::Ident && self.at(2).kind == Kind::DefEq {
                        self.advance();
                        let rhs = self.advance().lit;
                        self.advance();
                        return self.finish_operator_def(
                            local,
                            Fixity::Infix,
                            op,
                            vec![OpDecl::plain(name), OpDecl::plain(rhs)],
                        );
                    }
                }
                Err(self.err(format!(
                    "expected ==, (, or [ in definition of {name}, found {}",
                    self.cur()
                )))
            }
        }
    }

    fn definable_op(&self) -> Option<String> {
        match self.cur().kind {
            Kind::Op => Some(self.cur().lit.clone()),
            Kind::And => Some("/\\".into()),
            Kind::Or => Some("\\/".into()),
            _ => None,
        }
    }

    /// `-. a == 0 - a` (Integers.tla) or `~ a == ...`.
    fn parse_prefix_op_def(&mut self, local: bool) -> Result<Unit> {
        let op = self.advance().lit;
        let param = self.expect_ident()?;
        self.expect(Kind::DefEq)?;
        Ok(Unit::OperatorDef {
            local,
            fixity: Fixity::PrefixSym,
            name: op,
            params: vec![OpDecl::plain(param)],
            body: self.parse_expr(0)?,
        })
    }

    fn finish_operator_def(
        &mut self,
        local: bool,
        fixity: Fixity,
        name: String,
        params: Vec<OpDecl>,
    ) -> Result<Unit> {
        if self.cur().kind == Kind::Instance {
            let instance = self.parse_instance(false)?;
            return Ok(Unit::ModuleDef {
                local,
                name,
                params,
                instance,
            });
        }
        Ok(Unit::OperatorDef {
            local,
            fixity,
            name,
            params,
            body: self.parse_expr(0)?,
        })
    }

    fn parse_op_decl_list(&mut self) -> Result<Vec<OpDecl>> {
        let mut ds = Vec::new();
        loop {
            ds.push(self.parse_op_decl()?);
            if self.cur().kind != Kind::Comma {
                return Ok(ds);
            }
            self.advance();
        }
    }

    fn parse_op_decl(&mut self) -> Result<OpDecl> {
        let name = self.expect_ident()?;
        let mut d = OpDecl { name, arity: 0 };
        if self.cur().kind == Kind::LParen && self.at(1).kind == Kind::Underscore {
            self.advance();
            loop {
                self.expect(Kind::Underscore)?;
                d.arity += 1;
                if self.cur().kind != Kind::Comma {
                    break;
                }
                self.advance();
            }
            self.expect(Kind::RParen)?;
        }
        Ok(d)
    }

    // --------------------------------------------------- expressions

    /// Parses an expression, absorbing infix and postfix operators whose
    /// precedence-range low bound is greater than `min`.
    fn parse_expr(&mut self, min: i32) -> Result<Expr> {
        let mut lhs = self.parse_primary()?;
        let mut prev: Option<token::OpInfo> = None;
        let mut prev_op = String::new();
        while !self.blocked() {
            let t = self.cur().clone();
            match t.kind {
                Kind::Dot => {
                    if PREC_DOT <= min {
                        return Ok(lhs);
                    }
                    self.advance();
                    let field = self.expect_ident()?;
                    lhs = Expr::Dot {
                        x: Box::new(lhs),
                        field,
                    };
                    continue;
                }
                Kind::LBrack => {
                    if PREC_APPLY <= min {
                        return Ok(lhs);
                    }
                    self.advance();
                    let mut args = Vec::new();
                    loop {
                        args.push(self.parse_expr(0)?);
                        if self.cur().kind != Kind::Comma {
                            break;
                        }
                        self.advance();
                    }
                    self.expect(Kind::RBrack)?;
                    lhs = Expr::FnApply {
                        f: Box::new(lhs),
                        args,
                    };
                    continue;
                }
                Kind::Prime => {
                    let info = postfix_op("'").unwrap();
                    if info.lo <= min {
                        return Ok(lhs);
                    }
                    self.advance();
                    lhs = Expr::PostfixExpr {
                        op: "'".into(),
                        x: Box::new(lhs),
                    };
                    continue;
                }
                Kind::Times => {
                    if TIMES_OP.lo <= min {
                        return Ok(lhs);
                    }
                    if let Some(p) = prev {
                        if overlaps(p, TIMES_OP) && prev_op != "\\X" {
                            return Err(self.err(
                                "operator \\X conflicts with preceding operator; add parentheses"
                                    .into(),
                            ));
                        }
                    }
                    let mut factors = vec![lhs];
                    while self.cur().kind == Kind::Times && !self.blocked() {
                        self.advance();
                        factors.push(self.parse_expr(TIMES_OP.hi)?);
                    }
                    prev = Some(TIMES_OP);
                    prev_op = "\\X".into();
                    lhs = Expr::Times(factors);
                    continue;
                }
                _ => {}
            }
            let op = match self.infix_at() {
                None => return Ok(lhs),
                Some(InfixAt::Postfix(op)) => {
                    let info = postfix_op(&op).unwrap();
                    if info.lo <= min {
                        return Ok(lhs);
                    }
                    self.advance();
                    lhs = Expr::PostfixExpr {
                        op,
                        x: Box::new(lhs),
                    };
                    continue;
                }
                Some(InfixAt::Infix(op)) => op,
            };
            let info = infix_op(&op).unwrap();
            if info.lo <= min {
                return Ok(lhs);
            }
            if let Some(p) = prev {
                if overlaps(p, info) && !(info.assoc && op == prev_op) {
                    return Err(self.err(format!(
                        "operator {op} conflicts with preceding operator of overlapping precedence; add parentheses"
                    )));
                }
            }
            self.advance();
            let rhs = self.parse_expr(info.hi)?;
            lhs = Expr::Binary {
                op: op.clone(),
                l: Box::new(lhs),
                r: Box::new(rhs),
            };
            prev = Some(info);
            prev_op = op;
        }
        Ok(lhs)
    }

    fn infix_at(&self) -> Option<InfixAt> {
        match self.cur().kind {
            Kind::And => Some(InfixAt::Infix("/\\".into())),
            Kind::Or => Some(InfixAt::Infix("\\/".into())),
            Kind::Op => {
                let lit = &self.cur().lit;
                if postfix_op(lit).is_some() {
                    Some(InfixAt::Postfix(lit.clone()))
                } else if infix_op(lit).is_some() {
                    Some(InfixAt::Infix(lit.clone()))
                } else {
                    None
                }
            }
            _ => None,
        }
    }

    fn parse_primary(&mut self) -> Result<Expr> {
        if self.blocked() {
            return Err(self.err(format!(
                "expression ends here (token {} is outside the junction list alignment)",
                self.cur()
            )));
        }
        let t = self.cur().clone();
        match t.kind {
            Kind::And | Kind::Or => self.parse_junction(),
            Kind::Ident => self.parse_name(),
            Kind::Number => {
                self.advance();
                Ok(Expr::Number(t.lit))
            }
            Kind::Str => {
                self.advance();
                Ok(Expr::Str(t.lit))
            }
            Kind::True => {
                self.advance();
                Ok(Expr::Bool(true))
            }
            Kind::False => {
                self.advance();
                Ok(Expr::Bool(false))
            }
            Kind::Boolean | Kind::StringKw => {
                self.advance();
                Ok(Expr::Ident(t.kind.name().to_string()))
            }
            Kind::At => {
                self.advance();
                Ok(Expr::At)
            }
            Kind::LParen => {
                self.advance();
                let x = self.parse_expr(0)?;
                self.expect(Kind::RParen)?;
                Ok(Expr::Paren(Box::new(x)))
            }
            Kind::LBrace => self.parse_brace(),
            Kind::LBrack => self.parse_bracket(),
            Kind::LTup => self.parse_tuple(),
            Kind::Box | Kind::Diamond => {
                self.advance();
                let op = if t.kind == Kind::Box { "[]" } else { "<>" };
                let info = prefix_op(op).unwrap();
                Ok(Expr::Unary {
                    op: op.into(),
                    x: Box::new(self.parse_expr(info.hi)?),
                })
            }
            Kind::Enabled | Kind::Unchanged | Kind::Subset | Kind::Union | Kind::Domain => {
                self.advance();
                let op = t.kind.name();
                let info = prefix_op(op).unwrap();
                Ok(Expr::Unary {
                    op: op.into(),
                    x: Box::new(self.parse_expr(info.hi)?),
                })
            }
            Kind::Op => {
                if let Some(info) = prefix_op(&t.lit) {
                    self.advance();
                    return Ok(Expr::Unary {
                        op: t.lit,
                        x: Box::new(self.parse_expr(info.hi)?),
                    });
                }
                if infix_op(&t.lit).is_some() {
                    // A bare operator symbol: an operator as an argument.
                    self.advance();
                    return Ok(Expr::OpRef(t.lit));
                }
                Err(self.err(format!("unexpected {t} in expression")))
            }
            Kind::If => {
                self.advance();
                let cond = self.parse_expr(0)?;
                self.expect(Kind::Then)?;
                let then = self.parse_expr(0)?;
                self.expect(Kind::Else)?;
                Ok(Expr::If {
                    cond: Box::new(cond),
                    then: Box::new(then),
                    els: Box::new(self.parse_expr(0)?),
                })
            }
            Kind::Case => self.parse_case(),
            Kind::Let => self.parse_let(),
            Kind::Choose => self.parse_choose(),
            Kind::Lambda => {
                self.advance();
                let mut params = Vec::new();
                loop {
                    params.push(self.expect_ident()?);
                    if self.cur().kind != Kind::Comma {
                        break;
                    }
                    self.advance();
                }
                self.expect(Kind::Colon)?;
                Ok(Expr::Lambda {
                    params,
                    body: Box::new(self.parse_expr(0)?),
                })
            }
            Kind::Forall | Kind::Exists => {
                self.advance();
                let kind = if t.kind == Kind::Forall {
                    QuantKind::Forall
                } else {
                    QuantKind::Exists
                };
                let bounds = self.parse_bounds(false)?;
                self.expect(Kind::Colon)?;
                Ok(Expr::Quant {
                    kind,
                    bounds,
                    body: Box::new(self.parse_expr(0)?),
                })
            }
            Kind::TForall | Kind::TExists => {
                self.advance();
                let kind = if t.kind == Kind::TForall {
                    QuantKind::TemporalForall
                } else {
                    QuantKind::TemporalExists
                };
                let mut names = Vec::new();
                loop {
                    names.push(self.expect_ident()?);
                    if self.cur().kind != Kind::Comma {
                        break;
                    }
                    self.advance();
                }
                self.expect(Kind::Colon)?;
                Ok(Expr::Quant {
                    kind,
                    bounds: vec![Bound {
                        names,
                        tuple: false,
                        set: None,
                    }],
                    body: Box::new(self.parse_expr(0)?),
                })
            }
            Kind::WfSub | Kind::SfSub => {
                self.advance();
                let sub = self.parse_subscript()?;
                self.expect(Kind::LParen)?;
                let x = self.parse_expr(0)?;
                self.expect(Kind::RParen)?;
                Ok(Expr::Fairness {
                    strong: t.kind == Kind::SfSub,
                    sub: Box::new(sub),
                    x: Box::new(x),
                })
            }
            _ => Err(self.err(format!("unexpected {t} in expression"))),
        }
    }

    /// A vertically aligned `/\` or `\/` list; the current token is the
    /// first bullet.
    fn parse_junction(&mut self) -> Result<Expr> {
        let bullet = self.cur().clone();
        let col = bullet.pos.col;
        let op = if bullet.kind == Kind::Or {
            "\\/"
        } else {
            "/\\"
        };
        let mut items = Vec::new();
        self.jcol.push(col);
        let result = loop {
            self.advance(); // bullet
            match self.parse_expr(0) {
                Ok(item) => items.push(item),
                Err(e) => break Err(e),
            }
            let t = self.cur();
            if t.kind != bullet.kind || t.pos.col != col {
                break Ok(());
            }
        };
        self.jcol.pop();
        result?;
        Ok(Expr::Junction {
            op: op.into(),
            items,
        })
    }

    fn parse_name(&mut self) -> Result<Expr> {
        let mut prefix = Vec::new();
        loop {
            let name = self.expect_ident()?;
            let mut args = Vec::new();
            if self.cur().kind == Kind::LParen {
                self.advance();
                loop {
                    args.push(self.parse_expr(0)?);
                    if self.cur().kind != Kind::Comma {
                        break;
                    }
                    self.advance();
                }
                self.expect(Kind::RParen)?;
            }
            if self.cur().kind == Kind::Bang && self.at(1).kind == Kind::Ident {
                self.advance();
                prefix.push(InstArm { name, args });
                continue;
            }
            let fun = if prefix.is_empty() {
                Expr::Ident(name)
            } else {
                Expr::GeneralIdent { prefix, name }
            };
            return Ok(if args.is_empty() {
                fun
            } else {
                Expr::Apply {
                    fun: Box::new(fun),
                    args,
                }
            });
        }
    }

    /// `{...}`: enumeration, filter `{x \in S : P}`, or map `{e : x \in S}`.
    fn parse_brace(&mut self) -> Result<Expr> {
        self.expect(Kind::LBrace)?;
        if self.cur().kind == Kind::RBrace {
            self.advance();
            return Ok(Expr::SetEnum(Vec::new()));
        }
        let first = self.parse_expr(0)?;
        match self.cur().kind {
            Kind::Colon => {
                self.advance();
                if let Some(bound) = filter_bound(&first) {
                    if self.filter_ahead() {
                        let pred = self.parse_expr(0)?;
                        self.expect(Kind::RBrace)?;
                        return Ok(Expr::SetFilter {
                            bound,
                            pred: Box::new(pred),
                        });
                    }
                }
                let bounds = self.parse_bounds(true)?;
                self.expect(Kind::RBrace)?;
                Ok(Expr::SetMap {
                    body: Box::new(first),
                    bounds,
                })
            }
            Kind::Comma => {
                let mut elems = vec![first];
                while self.cur().kind == Kind::Comma {
                    self.advance();
                    elems.push(self.parse_expr(0)?);
                }
                self.expect(Kind::RBrace)?;
                Ok(Expr::SetEnum(elems))
            }
            _ => {
                self.expect(Kind::RBrace)?;
                Ok(Expr::SetEnum(vec![first]))
            }
        }
    }

    /// Disambiguates `{x \in S : P}` from `{e : x \in S, ...}` after the
    /// colon: a set map's colon is followed by a bound list.
    fn filter_ahead(&self) -> bool {
        let mut i = 0;
        if self.at(i).kind != Kind::Ident {
            return true;
        }
        while self.at(i).kind == Kind::Ident {
            i += 1;
            if self.at(i).kind == Kind::Op && self.at(i).lit == "\\in" {
                return false; // looks like a map's bound list
            }
            if self.at(i).kind != Kind::Comma {
                return true;
            }
            i += 1;
        }
        true
    }

    /// Every `[...]` form.
    fn parse_bracket(&mut self) -> Result<Expr> {
        self.expect(Kind::LBrack)?;
        // Record constructor / record set: [name |-> e] / [name : S].
        if self.cur().kind == Kind::Ident {
            match self.at(1).kind {
                Kind::MapsTo => {
                    let mut fields = Vec::new();
                    loop {
                        let name = self.expect_ident()?;
                        self.expect(Kind::MapsTo)?;
                        fields.push(Field {
                            name,
                            expr: self.parse_expr(0)?,
                        });
                        if self.cur().kind != Kind::Comma {
                            break;
                        }
                        self.advance();
                    }
                    self.expect(Kind::RBrack)?;
                    return Ok(Expr::RecordLit(fields));
                }
                Kind::Colon => {
                    let mut fields = Vec::new();
                    loop {
                        let name = self.expect_ident()?;
                        self.expect(Kind::Colon)?;
                        fields.push(Field {
                            name,
                            expr: self.parse_expr(0)?,
                        });
                        if self.cur().kind != Kind::Comma {
                            break;
                        }
                        self.advance();
                    }
                    self.expect(Kind::RBrack)?;
                    return Ok(Expr::RecordSet(fields));
                }
                _ => {}
            }
        }
        // Function constructor [x \in S |-> e]: bounds followed by |->.
        if self.bounds_ahead() {
            let save = self.i;
            let bounds = self.parse_bounds(true)?;
            if self.cur().kind == Kind::MapsTo {
                self.advance();
                let body = self.parse_expr(0)?;
                self.expect(Kind::RBrack)?;
                return Ok(Expr::FuncLit {
                    bounds,
                    body: Box::new(body),
                });
            }
            self.i = save;
        }
        let x = self.parse_expr(0)?;
        match self.cur().kind {
            Kind::Except => {
                self.advance();
                let mut specs = Vec::new();
                loop {
                    self.expect(Kind::Bang)?;
                    let mut path = Vec::new();
                    loop {
                        match self.cur().kind {
                            Kind::Dot => {
                                self.advance();
                                path.push(ExceptPath::Field(self.expect_ident()?));
                            }
                            Kind::LBrack => {
                                self.advance();
                                let mut idx = Vec::new();
                                loop {
                                    idx.push(self.parse_expr(0)?);
                                    if self.cur().kind != Kind::Comma {
                                        break;
                                    }
                                    self.advance();
                                }
                                self.expect(Kind::RBrack)?;
                                path.push(ExceptPath::Index(idx));
                            }
                            _ => break,
                        }
                    }
                    if path.is_empty() {
                        return Err(
                            self.err("expected .field or [index] after ! in EXCEPT".into())
                        );
                    }
                    if !(self.cur().kind == Kind::Op && self.cur().lit == "=") {
                        return Err(self.err(format!(
                            "expected = in EXCEPT clause, found {}",
                            self.cur()
                        )));
                    }
                    self.advance();
                    specs.push(ExceptSpec {
                        path,
                        value: self.parse_expr(0)?,
                    });
                    if self.cur().kind != Kind::Comma {
                        break;
                    }
                    self.advance();
                }
                self.expect(Kind::RBrack)?;
                Ok(Expr::Except {
                    f: Box::new(x),
                    specs,
                })
            }
            Kind::Arrow => {
                self.advance();
                let range = self.parse_expr(0)?;
                self.expect(Kind::RBrack)?;
                Ok(Expr::FuncSet {
                    domain: Box::new(x),
                    range: Box::new(range),
                })
            }
            Kind::RBrackSub => {
                self.advance();
                Ok(Expr::SquareAct {
                    x: Box::new(x),
                    sub: Box::new(self.parse_subscript()?),
                })
            }
            Kind::RBrack => Err(self.err(
                "[expr] must be followed by _subscript, or use |->, ->, :, or EXCEPT inside the brackets"
                    .into(),
            )),
            _ => Err(self.err(format!("unexpected {} in [...] expression", self.cur()))),
        }
    }

    /// Whether the cursor starts a bound list (`x, y \in S` or
    /// `<<x, y>> \in S`).
    fn bounds_ahead(&self) -> bool {
        let mut i = 0;
        if self.at(i).kind == Kind::LTup {
            i += 1;
            while self.at(i).kind == Kind::Ident {
                i += 1;
                if self.at(i).kind == Kind::Comma {
                    i += 1;
                    continue;
                }
                break;
            }
            if self.at(i).kind != Kind::RTup {
                return false;
            }
            i += 1;
            return self.at(i).kind == Kind::Op && self.at(i).lit == "\\in";
        }
        while self.at(i).kind == Kind::Ident {
            i += 1;
            if self.at(i).kind == Kind::Op && self.at(i).lit == "\\in" {
                return true;
            }
            if self.at(i).kind != Kind::Comma {
                return false;
            }
            i += 1;
        }
        false
    }

    fn parse_tuple(&mut self) -> Result<Expr> {
        self.expect(Kind::LTup)?;
        if self.cur().kind == Kind::RTup {
            self.advance();
            return Ok(Expr::Tuple(Vec::new()));
        }
        let mut elems = Vec::new();
        loop {
            elems.push(self.parse_expr(0)?);
            if self.cur().kind != Kind::Comma {
                break;
            }
            self.advance();
        }
        if self.cur().kind == Kind::RTupSub {
            self.advance();
            if elems.len() != 1 {
                return Err(self.err("<<A>>_v takes a single action".into()));
            }
            let x = elems.pop().unwrap();
            return Ok(Expr::AngleAct {
                x: Box::new(x),
                sub: Box::new(self.parse_subscript()?),
            });
        }
        self.expect(Kind::RTup)?;
        Ok(Expr::Tuple(elems))
    }

    fn parse_case(&mut self) -> Result<Expr> {
        self.expect(Kind::Case)?;
        let mut arms = Vec::new();
        let mut other = None;
        loop {
            if self.cur().kind == Kind::Other {
                self.advance();
                self.expect(Kind::Arrow)?;
                other = Some(Box::new(self.parse_expr(0)?));
                break;
            }
            let cond = self.parse_expr(0)?;
            self.expect(Kind::Arrow)?;
            let value = self.parse_expr(0)?;
            arms.push(CaseArm { cond, value });
            if self.cur().kind != Kind::Box || self.blocked() {
                break;
            }
            self.advance();
        }
        Ok(Expr::Case { arms, other })
    }

    fn parse_let(&mut self) -> Result<Expr> {
        self.expect(Kind::Let)?;
        let mut defs = Vec::new();
        while self.cur().kind != Kind::In {
            match self.cur().kind {
                Kind::Ident => defs.push(self.parse_definition(false)?),
                Kind::Recursive => {
                    self.advance();
                    defs.push(Unit::Recursive(self.parse_op_decl_list()?));
                }
                Kind::Op if is_definable_prefix_op(&self.cur().lit) => {
                    defs.push(self.parse_prefix_op_def(false)?)
                }
                _ => {
                    return Err(self.err(format!(
                        "expected definition or IN in LET, found {}",
                        self.cur()
                    )))
                }
            }
        }
        self.expect(Kind::In)?;
        Ok(Expr::Let {
            defs,
            body: Box::new(self.parse_expr(0)?),
        })
    }

    fn parse_choose(&mut self) -> Result<Expr> {
        self.expect(Kind::Choose)?;
        let mut var = String::new();
        let mut tuple = Vec::new();
        if self.cur().kind == Kind::LTup {
            self.advance();
            loop {
                tuple.push(self.expect_ident()?);
                if self.cur().kind != Kind::Comma {
                    break;
                }
                self.advance();
            }
            self.expect(Kind::RTup)?;
        } else {
            var = self.expect_ident()?;
        }
        let mut set = None;
        if self.cur().kind == Kind::Op && self.cur().lit == "\\in" {
            self.advance();
            set = Some(Box::new(self.parse_expr(0)?));
        }
        self.expect(Kind::Colon)?;
        Ok(Expr::Choose {
            var,
            tuple,
            set,
            body: Box::new(self.parse_expr(0)?),
        })
    }

    /// Quantifier/constructor bounds. When `require_set` every group
    /// must have `\in S`.
    fn parse_bounds(&mut self, require_set: bool) -> Result<Vec<Bound>> {
        let mut bounds = Vec::new();
        loop {
            let mut b = Bound {
                names: Vec::new(),
                tuple: false,
                set: None,
            };
            if self.cur().kind == Kind::LTup {
                self.advance();
                b.tuple = true;
                loop {
                    b.names.push(self.expect_ident()?);
                    if self.cur().kind != Kind::Comma {
                        break;
                    }
                    self.advance();
                }
                self.expect(Kind::RTup)?;
            } else {
                // Before \in is seen, ", IDENT" always extends the group.
                loop {
                    b.names.push(self.expect_ident()?);
                    if self.cur().kind != Kind::Comma || self.at(1).kind != Kind::Ident {
                        break;
                    }
                    self.advance();
                }
            }
            if self.cur().kind == Kind::Op && self.cur().lit == "\\in" {
                self.advance();
                b.set = Some(Box::new(self.parse_expr(infix_op("\\in").unwrap().hi)?));
            } else if require_set || b.tuple {
                return Err(self.err(format!("expected \\in in bound, found {}", self.cur())));
            }
            bounds.push(b);
            if self.cur().kind != Kind::Comma {
                return Ok(bounds);
            }
            self.advance();
        }
    }

    /// The `_v` subscript: an identifier (with `.field` selections), a
    /// tuple, or a parenthesized expression — deliberately tight, so
    /// `WF_vars(Next)` reads `vars` as the subscript.
    fn parse_subscript(&mut self) -> Result<Expr> {
        let t = self.cur().clone();
        match t.kind {
            Kind::Ident => {
                self.advance();
                let mut x = Expr::Ident(t.lit);
                while self.cur().kind == Kind::Dot {
                    self.advance();
                    x = Expr::Dot {
                        x: Box::new(x),
                        field: self.expect_ident()?,
                    };
                }
                Ok(x)
            }
            Kind::LTup => self.parse_tuple(),
            Kind::LParen => {
                self.advance();
                let inner = self.parse_expr(0)?;
                self.expect(Kind::RParen)?;
                Ok(Expr::Paren(Box::new(inner)))
            }
            _ => Err(self.err(format!(
                "expected subscript (identifier, tuple, or parenthesized expression), found {t}"
            ))),
        }
    }
}

enum InfixAt {
    Infix(String),
    Postfix(String),
}

fn is_definable_prefix_op(op: &str) -> bool {
    op == "-." || op == "~"
}

fn overlaps(a: token::OpInfo, b: token::OpInfo) -> bool {
    a.lo <= b.hi && b.lo <= a.hi
}

/// Converts an `x \in S` / `<<x, y>> \in S` expression into a
/// set-filter bound.
fn filter_bound(e: &Expr) -> Option<Bound> {
    let Expr::Binary { op, l, r } = e else {
        return None;
    };
    if op != "\\in" {
        return None;
    }
    match l.as_ref() {
        Expr::Ident(n) => Some(Bound {
            names: vec![n.clone()],
            tuple: false,
            set: Some(r.clone()),
        }),
        Expr::Tuple(elems) => {
            let mut names = Vec::new();
            for el in elems {
                match el {
                    Expr::Ident(n) => names.push(n.clone()),
                    _ => return None,
                }
            }
            Some(Bound {
                names,
                tuple: true,
                set: Some(r.clone()),
            })
        }
        _ => None,
    }
}
