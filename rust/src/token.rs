//! Lexical tokens of TLA+ and the operator precedence tables from
//! *Specifying Systems*. Positions carry line and column because TLA+
//! junction lists (aligned `/\` and `\/` bullets) make the grammar
//! column-sensitive.

use std::fmt;

/// A position in source text. `line` and `col` are 1-based (`col` counts
/// chars, matching SANY's alignment rules); `offset` is the byte offset.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Pos {
    pub line: u32,
    pub col: u32,
    pub offset: usize,
}

impl fmt::Display for Pos {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}", self.line, self.col)
    }
}

/// The class of a token.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Eof,
    Ident,
    Number,
    Str,

    LParen,
    RParen,
    LBrack,
    RBrack,
    RBrackSub, // ]_ (only when _ is adjacent: [A]_v)
    LBrace,
    RBrace,
    LTup,    // <<
    RTup,    // >>
    RTupSub, // >>_ (only when _ is adjacent: <<A>>_v)
    Comma,
    Colon,
    Dot,
    Bang,
    At,
    Underscore,
    DefEq,  // ==
    MapsTo, // |->
    Arrow,  // ->
    LArrow, // <-
    Dashes, // ---- (4 or more)
    ModEnd, // ==== (4 or more)

    And,     // /\
    Or,      // \/
    Box,     // [] (temporal always; also the CASE separator)
    Diamond, // <>
    Prime,   // '
    Times,   // \X or \times (n-ary Cartesian product)

    Forall,  // \A
    Exists,  // \E
    TForall, // \AA
    TExists, // \EE

    /// Generic operator; the token's `lit` holds the canonical spelling.
    Op,

    WfSub, // WF_
    SfSub, // SF_

    // Keywords.
    Module,
    Extends,
    Constant,
    Constants,
    Variable,
    Variables,
    Assume,
    Assumption,
    Axiom,
    Theorem,
    Lemma,
    Proposition,
    Corollary,
    Local,
    Instance,
    With,
    Let,
    In,
    If,
    Then,
    Else,
    Case,
    Other,
    Choose,
    Lambda,
    Recursive,
    True,
    False,
    Boolean,
    StringKw, // the STRING keyword (set of all strings)
    Enabled,
    Unchanged,
    Subset,
    Union,
    Domain,
    Except,
}

impl Kind {
    /// The canonical display name (keyword text or symbol).
    pub fn name(self) -> &'static str {
        use Kind::*;
        match self {
            Eof => "EOF",
            Ident => "IDENT",
            Number => "NUMBER",
            Str => "STRING-LIT",
            LParen => "(",
            RParen => ")",
            LBrack => "[",
            RBrack => "]",
            RBrackSub => "]_",
            LBrace => "{",
            RBrace => "}",
            LTup => "<<",
            RTup => ">>",
            RTupSub => ">>_",
            Comma => ",",
            Colon => ":",
            Dot => ".",
            Bang => "!",
            At => "@",
            Underscore => "_",
            DefEq => "==",
            MapsTo => "|->",
            Arrow => "->",
            LArrow => "<-",
            Dashes => "----",
            ModEnd => "====",
            And => "/\\",
            Or => "\\/",
            Box => "[]",
            Diamond => "<>",
            Prime => "'",
            Times => "\\X",
            Forall => "\\A",
            Exists => "\\E",
            TForall => "\\AA",
            TExists => "\\EE",
            Op => "OP",
            WfSub => "WF_",
            SfSub => "SF_",
            Module => "MODULE",
            Extends => "EXTENDS",
            Constant => "CONSTANT",
            Constants => "CONSTANTS",
            Variable => "VARIABLE",
            Variables => "VARIABLES",
            Assume => "ASSUME",
            Assumption => "ASSUMPTION",
            Axiom => "AXIOM",
            Theorem => "THEOREM",
            Lemma => "LEMMA",
            Proposition => "PROPOSITION",
            Corollary => "COROLLARY",
            Local => "LOCAL",
            Instance => "INSTANCE",
            With => "WITH",
            Let => "LET",
            In => "IN",
            If => "IF",
            Then => "THEN",
            Else => "ELSE",
            Case => "CASE",
            Other => "OTHER",
            Choose => "CHOOSE",
            Lambda => "LAMBDA",
            Recursive => "RECURSIVE",
            True => "TRUE",
            False => "FALSE",
            Boolean => "BOOLEAN",
            StringKw => "STRING",
            Enabled => "ENABLED",
            Unchanged => "UNCHANGED",
            Subset => "SUBSET",
            Union => "UNION",
            Domain => "DOMAIN",
            Except => "EXCEPT",
        }
    }
}

/// Maps an identifier to its keyword kind.
pub fn keyword(name: &str) -> Option<Kind> {
    use Kind::*;
    Some(match name {
        "MODULE" => Module,
        "EXTENDS" => Extends,
        "CONSTANT" => Constant,
        "CONSTANTS" => Constants,
        "VARIABLE" => Variable,
        "VARIABLES" => Variables,
        "ASSUME" => Assume,
        "ASSUMPTION" => Assumption,
        "AXIOM" => Axiom,
        "THEOREM" => Theorem,
        "LEMMA" => Lemma,
        "PROPOSITION" => Proposition,
        "COROLLARY" => Corollary,
        "LOCAL" => Local,
        "INSTANCE" => Instance,
        "WITH" => With,
        "LET" => Let,
        "IN" => In,
        "IF" => If,
        "THEN" => Then,
        "ELSE" => Else,
        "CASE" => Case,
        "OTHER" => Other,
        "CHOOSE" => Choose,
        "LAMBDA" => Lambda,
        "RECURSIVE" => Recursive,
        "TRUE" => True,
        "FALSE" => False,
        "BOOLEAN" => Boolean,
        "STRING" => StringKw,
        "ENABLED" => Enabled,
        "UNCHANGED" => Unchanged,
        "SUBSET" => Subset,
        "UNION" => Union,
        "DOMAIN" => Domain,
        "EXCEPT" => Except,
        _ => return None,
    })
}

/// Reserved words with no structure in this library (the TLA+2 proof
/// language); they may not be used as identifiers.
pub fn is_reserved(name: &str) -> bool {
    matches!(
        name,
        "ACTION"
            | "BY"
            | "DEF"
            | "DEFINE"
            | "DEFS"
            | "HAVE"
            | "HIDE"
            | "NEW"
            | "OBVIOUS"
            | "OMITTED"
            | "ONLY"
            | "PICK"
            | "PROOF"
            | "PROVE"
            | "QED"
            | "STATE"
            | "SUFFICES"
            | "TAKE"
            | "TEMPORAL"
            | "USE"
            | "WITNESS"
    )
}

/// A single lexical token.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Token {
    pub kind: Kind,
    /// Literal text for Ident/Number/Str (decoded)/Op (canonical).
    pub lit: String,
    pub pos: Pos,
}

impl fmt::Display for Token {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.kind {
            Kind::Ident | Kind::Number | Kind::Op => write!(f, "{}", self.lit),
            Kind::Str => write!(f, "{:?}", self.lit),
            _ => write!(f, "{}", self.kind.name()),
        }
    }
}

/// One operator's parsing properties: a precedence range and whether the
/// operator is left-associative (may chain with itself). Higher numbers
/// bind tighter; overlapping ranges may not mix without parentheses.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct OpInfo {
    pub lo: i32,
    pub hi: i32,
    pub assoc: bool,
}

const fn op(lo: i32, hi: i32, assoc: bool) -> OpInfo {
    OpInfo { lo, hi, assoc }
}

/// Precedence of `f[x]` function application.
pub const PREC_APPLY: i32 = 16;
/// Precedence of `r.field` selection.
pub const PREC_DOT: i32 = 17;
/// Precedence of the n-ary Cartesian product `\X`.
pub const TIMES_OP: OpInfo = op(10, 13, true);

/// Infix operator table (canonical spellings).
pub fn infix_op(s: &str) -> Option<OpInfo> {
    Some(match s {
        "=>" => op(1, 1, false),
        "-+->" | "<=>" | "~>" => op(2, 2, false),
        "/\\" | "\\/" => op(3, 3, true),
        "/=" | "=" | "<" | ">" | "<=" | ">=" | "\\in" | "\\notin" | "\\subset" | "\\subseteq"
        | "\\supset" | "\\supseteq" | "\\sqsubset" | "\\sqsubseteq" | "\\sqsupset"
        | "\\sqsupseteq" | "\\prec" | "\\preceq" | "\\succ" | "\\succeq" | "\\ll" | "\\gg"
        | "\\sim" | "\\simeq" | "\\approx" | "\\cong" | "\\asymp" | "\\doteq" | "\\propto"
        | ":=" | "::=" | "-|" | "|-" | "=|" | "|=" => op(5, 5, false),
        "\\cdot" => op(5, 14, true),
        "@@" => op(6, 6, true),
        ":>" | "<:" => op(7, 7, false),
        "\\" => op(8, 8, false),
        "\\cap" | "\\cup" => op(8, 8, true),
        ".." | "..." => op(9, 9, false),
        "!!" => op(9, 13, false),
        "##" | "$" | "$$" | "??" | "\\sqcap" | "\\sqcup" | "\\uplus" => op(9, 13, true),
        "\\oplus" | "+" | "++" => op(10, 10, true),
        "%" => op(10, 11, false),
        "%%" | "|" | "||" => op(10, 11, true),
        "\\ominus" | "-" | "--" => op(11, 11, true),
        "&" | "&&" | "\\odot" | "\\otimes" | "*" | "**" | "\\bigcirc" | "\\bullet" | "\\o"
        | "\\star" => op(13, 13, true),
        "\\oslash" | "/" | "//" | "\\div" => op(13, 13, false),
        "\\wr" => op(9, 14, false),
        "^" | "^^" => op(14, 14, false),
        _ => return None,
    })
}

/// Prefix operator table. The `hi` bound determines how much the operand
/// absorbs: it extends over infix operators with `lo > hi`.
pub fn prefix_op(s: &str) -> Option<OpInfo> {
    Some(match s {
        "~" => op(4, 4, false),
        "ENABLED" | "UNCHANGED" | "[]" | "<>" => op(4, 15, false),
        "SUBSET" | "UNION" => op(8, 8, false),
        "DOMAIN" => op(9, 9, false),
        "-" => op(12, 12, false),
        _ => return None,
    })
}

/// Postfix operator table.
pub fn postfix_op(s: &str) -> Option<OpInfo> {
    Some(match s {
        "'" | "^+" | "^*" | "^#" => op(15, 15, false),
        _ => return None,
    })
}

/// Resolves alternative spellings to the canonical spelling used in
/// token `lit`s and the tables above.
pub fn canon(s: &str) -> &str {
    match s {
        "\\land" => "/\\",
        "\\lor" => "\\/",
        "\\lnot" | "\\neg" => "~",
        "\\equiv" => "<=>",
        "#" => "/=",
        "=<" | "\\leq" => "<=",
        "\\geq" => ">=",
        "\\intersect" => "\\cap",
        "\\union" => "\\cup",
        "\\circ" => "\\o",
        "\\times" => "\\X",
        "(+)" => "\\oplus",
        "(-)" => "\\ominus",
        "(.)" => "\\odot",
        "(/)" => "\\oslash",
        "(\\X)" => "\\otimes",
        other => other,
    }
}

/// Whether `\word` is a known alphabetic operator spelling.
pub fn is_backslash_word(word: &str) -> bool {
    matches!(
        word,
        "in" | "notin"
            | "subset"
            | "subseteq"
            | "supset"
            | "supseteq"
            | "sqsubset"
            | "sqsubseteq"
            | "sqsupset"
            | "sqsupseteq"
            | "prec"
            | "preceq"
            | "succ"
            | "succeq"
            | "ll"
            | "gg"
            | "sim"
            | "simeq"
            | "approx"
            | "cong"
            | "asymp"
            | "doteq"
            | "propto"
            | "cdot"
            | "cap"
            | "cup"
            | "sqcap"
            | "sqcup"
            | "uplus"
            | "oplus"
            | "ominus"
            | "odot"
            | "otimes"
            | "oslash"
            | "bigcirc"
            | "bullet"
            | "o"
            | "star"
            | "div"
            | "wr"
            | "land"
            | "lor"
            | "lnot"
            | "neg"
            | "equiv"
            | "leq"
            | "geq"
            | "intersect"
            | "union"
            | "circ"
            | "times"
            | "X"
    )
}
