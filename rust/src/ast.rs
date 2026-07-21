//! TLA+ syntax trees. The printer in [`crate::printer`] renders them as
//! parseable, canonically formatted source.

/// A TLA+ module.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Module {
    pub name: String,
    pub extends: Vec<String>,
    pub units: Vec<Unit>,
}

impl std::fmt::Display for Module {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&crate::printer::module_string(self))
    }
}

/// An operator name with an arity, as in `CONSTANT F(_, _)` (arity 0
/// declares a plain name).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OpDecl {
    pub name: String,
    pub arity: u32,
}

impl OpDecl {
    pub fn plain(name: impl Into<String>) -> Self {
        OpDecl {
            name: name.into(),
            arity: 0,
        }
    }
}

/// One bound-variable group: `x, y \in S`, `<<x, y>> \in S`, or
/// (unbounded, `set == None`) `x, y`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Bound {
    pub names: Vec<String>,
    pub tuple: bool,
    pub set: Option<Box<Expr>>,
}

/// A record-constructor or record-set field.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Field {
    pub name: String,
    pub expr: Expr,
}

/// How a defined operator's name is written.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Fixity {
    /// `Name(args) == e`, or `Name == e`.
    Named,
    /// `a ++ b == e`.
    Infix,
    /// `a ^+ == e`.
    Postfix,
    /// `-. a == e` (unary minus), `~ a == e`.
    PrefixSym,
}

/// One substitution of an `INSTANCE ... WITH` clause.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Subst {
    pub name: String,
    pub expr: Expr,
}

/// A bare INSTANCE unit.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Instance {
    pub local: bool,
    pub module: String,
    pub with: Vec<Subst>,
}

/// A module-level unit.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Unit {
    Constants(Vec<OpDecl>),
    Variables(Vec<String>),
    /// For `Fixity::Infix`/`Postfix`/`PrefixSym` the name is the operator
    /// symbol and `params` holds the operands.
    OperatorDef {
        local: bool,
        fixity: Fixity,
        name: String,
        params: Vec<OpDecl>,
        body: Expr,
    },
    /// `f[x \in S] == e`.
    FunctionDef {
        local: bool,
        name: String,
        bounds: Vec<Bound>,
        body: Expr,
    },
    Instance(Instance),
    /// `IM(x) == INSTANCE M WITH a <- x`.
    ModuleDef {
        local: bool,
        name: String,
        params: Vec<OpDecl>,
        instance: Instance,
    },
    /// ASSUME/ASSUMPTION/AXIOM (keyword preserved).
    Assume {
        keyword: String,
        name: Option<String>,
        expr: Expr,
    },
    /// THEOREM/LEMMA/PROPOSITION/COROLLARY, without proof.
    Theorem {
        keyword: String,
        name: Option<String>,
        expr: Expr,
    },
    Recursive(Vec<OpDecl>),
    /// A `----` line between units.
    Separator,
    Nested(Box<Module>),
}

/// The quantifier forms.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum QuantKind {
    Forall,
    Exists,
    TemporalForall,
    TemporalExists,
}

/// One step of an EXCEPT path: `.field` or `[e1, ..., en]`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ExceptPath {
    Field(String),
    Index(Vec<Expr>),
}

/// One `!path = value` clause of an EXCEPT expression.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ExceptSpec {
    pub path: Vec<ExceptPath>,
    pub value: Expr,
}

/// One `guard -> value` arm of a CASE expression.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CaseArm {
    pub cond: Expr,
    pub value: Expr,
}

/// One segment of an instance-qualified reference: `Name` or `Name(args)`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InstArm {
    pub name: String,
    pub args: Vec<Expr>,
}

/// A TLA+ expression.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Expr {
    Ident(String),
    /// `A!B!Op` (with optional per-segment arguments).
    GeneralIdent {
        prefix: Vec<InstArm>,
        name: String,
    },
    /// Numeric literal; source form preserved (incl. \b, \o, \h bases).
    Number(String),
    /// String literal (decoded).
    Str(String),
    Bool(bool),
    /// Operator application `Op(e1, ..., en)`.
    Apply {
        fun: Box<Expr>,
        args: Vec<Expr>,
    },
    /// Prefix operator (canonical spelling).
    Unary {
        op: String,
        x: Box<Expr>,
    },
    /// Infix operator (canonical spelling).
    Binary {
        op: String,
        l: Box<Expr>,
        r: Box<Expr>,
    },
    /// Postfix operator (`'`, `^+`, `^*`, `^#`).
    PostfixExpr {
        op: String,
        x: Box<Expr>,
    },
    /// Vertically aligned `/\` or `\/` list.
    Junction {
        op: String,
        items: Vec<Expr>,
    },
    Paren(Box<Expr>),
    /// Function application `f[e1, ..., en]`.
    FnApply {
        f: Box<Expr>,
        args: Vec<Expr>,
    },
    /// Record-field selection `r.f`.
    Dot {
        x: Box<Expr>,
        field: String,
    },
    Tuple(Vec<Expr>),
    /// n-ary Cartesian product `S \X T \X U`.
    Times(Vec<Expr>),
    SetEnum(Vec<Expr>),
    /// `{x \in S : P}`.
    SetFilter {
        bound: Bound,
        pred: Box<Expr>,
    },
    /// `{e : x \in S, y \in T}`.
    SetMap {
        body: Box<Expr>,
        bounds: Vec<Bound>,
    },
    /// `[x \in S |-> e]`.
    FuncLit {
        bounds: Vec<Bound>,
        body: Box<Expr>,
    },
    /// `[S -> T]`.
    FuncSet {
        domain: Box<Expr>,
        range: Box<Expr>,
    },
    RecordLit(Vec<Field>),
    RecordSet(Vec<Field>),
    /// `[f EXCEPT ![i] = e, !.a = g]`.
    Except {
        f: Box<Expr>,
        specs: Vec<ExceptSpec>,
    },
    /// The `@` placeholder inside an EXCEPT value.
    At,
    If {
        cond: Box<Expr>,
        then: Box<Expr>,
        els: Box<Expr>,
    },
    Case {
        arms: Vec<CaseArm>,
        other: Option<Box<Expr>>,
    },
    /// `LET defs IN body`.
    Let {
        defs: Vec<Unit>,
        body: Box<Expr>,
    },
    Quant {
        kind: QuantKind,
        bounds: Vec<Bound>,
        body: Box<Expr>,
    },
    /// `CHOOSE x \in S : P` (`set` optional; `tuple` set for `<<x, y>>`).
    Choose {
        var: String,
        tuple: Vec<String>,
        set: Option<Box<Expr>>,
        body: Box<Expr>,
    },
    /// `[A]_v`.
    SquareAct {
        x: Box<Expr>,
        sub: Box<Expr>,
    },
    /// `<<A>>_v`.
    AngleAct {
        x: Box<Expr>,
        sub: Box<Expr>,
    },
    /// `WF_v(A)` / `SF_v(A)`.
    Fairness {
        strong: bool,
        sub: Box<Expr>,
        x: Box<Expr>,
    },
    /// `LAMBDA x, y : e`.
    Lambda {
        params: Vec<String>,
        body: Box<Expr>,
    },
    /// A bare operator symbol passed as an argument, e.g. `Foo(\cup)`.
    OpRef(String),
}

impl std::fmt::Display for Expr {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&crate::printer::expr_string(self))
    }
}
