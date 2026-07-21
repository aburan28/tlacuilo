//! Tokenizer for TLA+ source text. Tracks line/column for every token
//! (junction lists are column-sensitive) and byte offsets (used by the
//! cfg parser to slice source text).

use crate::token::{canon, is_backslash_word, is_reserved, keyword, Kind, Pos, Token};

/// A scanning error at a position.
#[derive(Clone, Debug)]
pub struct ScanError {
    pub pos: Pos,
    pub msg: String,
}

impl std::fmt::Display for ScanError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.pos, self.msg)
    }
}

impl std::error::Error for ScanError {}

pub struct Scanner {
    chars: Vec<char>,
    i: usize,
    line: u32,
    col: u32,
    offset: usize,
    pending: Option<Token>,
    errors: Vec<ScanError>,
}

impl Scanner {
    pub fn new(src: &str) -> Self {
        Scanner {
            chars: src.chars().collect(),
            i: 0,
            line: 1,
            col: 1,
            offset: 0,
            pending: None,
            errors: Vec::new(),
        }
    }

    pub fn errors(&self) -> &[ScanError] {
        &self.errors
    }

    fn errorf(&mut self, pos: Pos, msg: String) {
        self.errors.push(ScanError { pos, msg });
    }

    fn peek(&self) -> Option<char> {
        self.chars.get(self.i).copied()
    }

    fn peek_at(&self, n: usize) -> Option<char> {
        self.chars.get(self.i + n).copied()
    }

    fn next_ch(&mut self) -> Option<char> {
        let c = self.chars.get(self.i).copied()?;
        self.i += 1;
        self.offset += c.len_utf8();
        if c == '\n' {
            self.line += 1;
            self.col = 1;
        } else {
            self.col += 1;
        }
        Some(c)
    }

    fn pos(&self) -> Pos {
        Pos {
            line: self.line,
            col: self.col,
            offset: self.offset,
        }
    }

    fn accept(&mut self, c: char) -> bool {
        if self.peek() == Some(c) {
            self.next_ch();
            true
        } else {
            false
        }
    }

    fn accept_seq(&mut self, seq: &str) -> bool {
        for (n, c) in seq.chars().enumerate() {
            if self.peek_at(n) != Some(c) {
                return false;
            }
        }
        for _ in seq.chars() {
            self.next_ch();
        }
        true
    }

    /// The next non-comment token.
    pub fn scan(&mut self) -> Token {
        loop {
            if let Some(t) = self.scan_raw() {
                return t;
            }
        }
    }

    /// All remaining non-comment tokens including the final EOF.
    pub fn scan_all(&mut self) -> Vec<Token> {
        let mut out = Vec::new();
        loop {
            let t = self.scan();
            let done = t.kind == Kind::Eof;
            out.push(t);
            if done {
                return out;
            }
        }
    }

    fn tok(&self, kind: Kind, pos: Pos) -> Token {
        Token {
            kind,
            lit: String::new(),
            pos,
        }
    }

    fn op_tok(&self, lit: &str, pos: Pos) -> Token {
        Token {
            kind: Kind::Op,
            lit: canon(lit).to_string(),
            pos,
        }
    }

    /// Scans one token; None means a comment was consumed.
    fn scan_raw(&mut self) -> Option<Token> {
        if let Some(t) = self.pending.take() {
            return Some(t);
        }
        while matches!(self.peek(), Some(' ' | '\t' | '\r' | '\n' | '\x0c')) {
            self.next_ch();
        }
        let pos = self.pos();
        let c = match self.peek() {
            None => return Some(self.tok(Kind::Eof, pos)),
            Some(c) => c,
        };
        if c.is_alphabetic() || c == '_' {
            return Some(self.scan_ident(pos));
        }
        if c.is_ascii_digit() {
            return Some(self.scan_number(pos));
        }
        if c == '"' {
            return Some(self.scan_string(pos));
        }
        self.next_ch();
        Some(match c {
            '(' => {
                if self.accept_seq("*") {
                    self.skip_block_comment(pos);
                    return None;
                }
                for b in ["+)", "-)", ".)", "/)", "\\X)"] {
                    if self.accept_seq(b) {
                        return Some(self.op_tok(&format!("({b}"), pos));
                    }
                }
                self.tok(Kind::LParen, pos)
            }
            ')' => self.tok(Kind::RParen, pos),
            '[' => {
                if self.accept(']') {
                    self.tok(Kind::Box, pos)
                } else {
                    self.tok(Kind::LBrack, pos)
                }
            }
            ']' => {
                if self.accept('_') {
                    self.tok(Kind::RBrackSub, pos)
                } else {
                    self.tok(Kind::RBrack, pos)
                }
            }
            '{' => self.tok(Kind::LBrace, pos),
            '}' => self.tok(Kind::RBrace, pos),
            ',' => self.tok(Kind::Comma, pos),
            '\'' => self.tok(Kind::Prime, pos),
            '@' => {
                if self.accept('@') {
                    self.op_tok("@@", pos)
                } else {
                    self.tok(Kind::At, pos)
                }
            }
            '!' => {
                if self.accept('!') {
                    self.op_tok("!!", pos)
                } else {
                    self.tok(Kind::Bang, pos)
                }
            }
            '#' => {
                if self.accept('#') {
                    self.op_tok("##", pos)
                } else {
                    self.op_tok("#", pos)
                }
            }
            '&' => {
                if self.accept('&') {
                    self.op_tok("&&", pos)
                } else {
                    self.op_tok("&", pos)
                }
            }
            '$' => {
                if self.accept('$') {
                    self.op_tok("$$", pos)
                } else {
                    self.op_tok("$", pos)
                }
            }
            '%' => {
                if self.accept('%') {
                    self.op_tok("%%", pos)
                } else {
                    self.op_tok("%", pos)
                }
            }
            '*' => {
                if self.accept('*') {
                    self.op_tok("**", pos)
                } else {
                    self.op_tok("*", pos)
                }
            }
            '?' => {
                if self.accept('?') {
                    self.op_tok("??", pos)
                } else {
                    self.errorf(pos, format!("unexpected character {c:?}"));
                    self.tok(Kind::Eof, pos)
                }
            }
            '^' => {
                if self.accept('^') {
                    self.op_tok("^^", pos)
                } else if self.accept('+') {
                    self.op_tok("^+", pos)
                } else if self.accept('*') {
                    self.op_tok("^*", pos)
                } else if self.accept('#') {
                    self.op_tok("^#", pos)
                } else {
                    self.op_tok("^", pos)
                }
            }
            '+' => {
                if self.accept('+') {
                    self.op_tok("++", pos)
                } else {
                    self.op_tok("+", pos)
                }
            }
            '-' => {
                if self.accept_seq("+->") {
                    self.op_tok("-+->", pos)
                } else if self.accept('|') {
                    self.op_tok("-|", pos)
                } else if self.peek() == Some('-') {
                    let mut n = 1;
                    while self.accept('-') {
                        n += 1;
                    }
                    if n >= 4 {
                        self.tok(Kind::Dashes, pos)
                    } else if n == 2 {
                        self.op_tok("--", pos)
                    } else {
                        self.errorf(pos, format!("run of {n} dashes is not a TLA+ token"));
                        self.tok(Kind::Eof, pos)
                    }
                } else if self.accept('>') {
                    self.tok(Kind::Arrow, pos)
                } else if self.accept('.') {
                    self.op_tok("-.", pos) // prefix-minus marker in operator definitions
                } else {
                    self.op_tok("-", pos)
                }
            }
            '=' => {
                if self.peek() == Some('=') {
                    let mut n = 1;
                    while self.accept('=') {
                        n += 1;
                    }
                    if n >= 4 {
                        self.tok(Kind::ModEnd, pos)
                    } else if n == 2 {
                        self.tok(Kind::DefEq, pos)
                    } else {
                        self.errorf(pos, format!("run of {n} equals signs is not a TLA+ token"));
                        self.tok(Kind::Eof, pos)
                    }
                } else if self.accept('>') {
                    self.op_tok("=>", pos)
                } else if self.accept('<') {
                    self.op_tok("=<", pos)
                } else if self.accept('|') {
                    self.op_tok("=|", pos)
                } else {
                    self.op_tok("=", pos)
                }
            }
            '<' => {
                if self.accept_seq("=>") {
                    self.op_tok("<=>", pos)
                } else if self.accept('<') {
                    self.tok(Kind::LTup, pos)
                } else if self.accept('-') {
                    self.tok(Kind::LArrow, pos)
                } else if self.accept(':') {
                    self.op_tok("<:", pos)
                } else if self.accept('=') {
                    self.op_tok("<=", pos)
                } else if self.accept('>') {
                    self.tok(Kind::Diamond, pos)
                } else {
                    self.op_tok("<", pos)
                }
            }
            '>' => {
                if self.accept('>') {
                    if self.accept('_') {
                        self.tok(Kind::RTupSub, pos)
                    } else {
                        self.tok(Kind::RTup, pos)
                    }
                } else if self.accept('=') {
                    self.op_tok(">=", pos)
                } else {
                    self.op_tok(">", pos)
                }
            }
            '.' => {
                if self.accept('.') {
                    if self.accept('.') {
                        self.op_tok("...", pos)
                    } else {
                        self.op_tok("..", pos)
                    }
                } else {
                    self.tok(Kind::Dot, pos)
                }
            }
            ':' => {
                if self.accept_seq(":=") {
                    self.op_tok("::=", pos)
                } else if self.accept('>') {
                    self.op_tok(":>", pos)
                } else if self.accept('=') {
                    self.op_tok(":=", pos)
                } else {
                    self.tok(Kind::Colon, pos)
                }
            }
            '|' => {
                if self.accept_seq("->") {
                    self.tok(Kind::MapsTo, pos)
                } else if self.accept('|') {
                    self.op_tok("||", pos)
                } else if self.accept('-') {
                    self.op_tok("|-", pos)
                } else if self.accept('=') {
                    self.op_tok("|=", pos)
                } else {
                    self.op_tok("|", pos)
                }
            }
            '/' => {
                if self.accept('\\') {
                    self.tok(Kind::And, pos)
                } else if self.accept('=') {
                    self.op_tok("/=", pos)
                } else if self.accept('/') {
                    self.op_tok("//", pos)
                } else {
                    self.op_tok("/", pos)
                }
            }
            '~' => {
                if self.accept('>') {
                    self.op_tok("~>", pos)
                } else {
                    self.op_tok("~", pos)
                }
            }
            '\\' => return self.scan_backslash(pos),
            other => {
                self.errorf(pos, format!("unexpected character {other:?}"));
                self.tok(Kind::Eof, pos)
            }
        })
    }

    fn scan_ident(&mut self, pos: Pos) -> Token {
        let start = self.i;
        let mut has_letter = false;
        while let Some(c) = self.peek() {
            if !(c.is_alphanumeric() || c == '_') {
                break;
            }
            if c.is_alphabetic() {
                has_letter = true;
            }
            self.next_ch();
        }
        let lit: String = self.chars[start..self.i].iter().collect();
        if !has_letter {
            if lit == "_" {
                return self.tok(Kind::Underscore, pos);
            }
            self.errorf(pos, format!("identifier {lit:?} must contain a letter"));
            return self.tok(Kind::Eof, pos);
        }
        for (pre, kind) in [("WF_", Kind::WfSub), ("SF_", Kind::SfSub)] {
            if let Some(rest) = lit.strip_prefix(pre) {
                if !rest.is_empty() {
                    self.pending = Some(Token {
                        kind: Kind::Ident,
                        lit: rest.to_string(),
                        pos: Pos {
                            line: pos.line,
                            col: pos.col + pre.len() as u32,
                            offset: pos.offset + pre.len(),
                        },
                    });
                }
                if lit.starts_with(pre) {
                    return self.tok(kind, pos);
                }
            }
        }
        if let Some(k) = keyword(&lit) {
            return Token { kind: k, lit, pos };
        }
        if is_reserved(&lit) {
            self.errorf(
                pos,
                format!("reserved word {lit:?} cannot be used here (TLA+ proof language is not supported)"),
            );
            return self.tok(Kind::Eof, pos);
        }
        Token {
            kind: Kind::Ident,
            lit,
            pos,
        }
    }

    fn scan_number(&mut self, pos: Pos) -> Token {
        let start = self.i;
        while matches!(self.peek(), Some(c) if c.is_ascii_digit()) {
            self.next_ch();
        }
        if self.peek() == Some('.') && matches!(self.peek_at(1), Some(c) if c.is_ascii_digit()) {
            self.next_ch();
            while matches!(self.peek(), Some(c) if c.is_ascii_digit()) {
                self.next_ch();
            }
        }
        Token {
            kind: Kind::Number,
            lit: self.chars[start..self.i].iter().collect(),
            pos,
        }
    }

    fn scan_string(&mut self, pos: Pos) -> Token {
        self.next_ch(); // opening quote
        let mut s = String::new();
        loop {
            match self.next_ch() {
                None | Some('\n') => {
                    self.errorf(pos, "unterminated string".into());
                    return Token {
                        kind: Kind::Str,
                        lit: s,
                        pos,
                    };
                }
                Some('"') => {
                    return Token {
                        kind: Kind::Str,
                        lit: s,
                        pos,
                    }
                }
                Some('\\') => match self.next_ch() {
                    Some('"') => s.push('"'),
                    Some('\\') => s.push('\\'),
                    Some('t') => s.push('\t'),
                    Some('n') => s.push('\n'),
                    Some('f') => s.push('\x0c'),
                    Some('r') => s.push('\r'),
                    other => self.errorf(pos, format!("invalid string escape \\{other:?}")),
                },
                Some(c) => s.push(c),
            }
        }
    }

    fn skip_block_comment(&mut self, pos: Pos) {
        let mut depth = 1;
        while depth > 0 {
            match self.next_ch() {
                None => {
                    self.errorf(pos, "unterminated block comment".into());
                    return;
                }
                Some('(') => {
                    if self.peek() == Some('*') {
                        self.next_ch();
                        depth += 1;
                    }
                }
                Some('*') => {
                    if self.peek() == Some(')') {
                        self.next_ch();
                        depth -= 1;
                    }
                }
                _ => {}
            }
        }
    }

    fn scan_backslash(&mut self, pos: Pos) -> Option<Token> {
        if self.accept('/') {
            return Some(self.tok(Kind::Or, pos));
        }
        if self.accept('*') {
            while !matches!(self.peek(), None | Some('\n')) {
                self.next_ch();
            }
            return None; // line comment
        }
        let c = match self.peek() {
            Some(c) if c.is_alphabetic() => c,
            _ => return Some(self.op_tok("\\", pos)), // set difference
        };
        if matches!(c, 'b' | 'B' | 'o' | 'O' | 'h' | 'H') {
            if let Some(lit) = self.try_base_number(c) {
                return Some(Token {
                    kind: Kind::Number,
                    lit,
                    pos,
                });
            }
        }
        let start = self.i;
        while matches!(self.peek(), Some(c) if c.is_alphabetic()) {
            self.next_ch();
        }
        let word: String = self.chars[start..self.i].iter().collect();
        Some(match word.as_str() {
            "A" => self.tok(Kind::Forall, pos),
            "E" => self.tok(Kind::Exists, pos),
            "AA" => self.tok(Kind::TForall, pos),
            "EE" => self.tok(Kind::TExists, pos),
            "X" | "times" => self.tok(Kind::Times, pos),
            "land" => self.tok(Kind::And, pos),
            "lor" => self.tok(Kind::Or, pos),
            w if is_backslash_word(w) => self.op_tok(&format!("\\{w}"), pos),
            w => {
                self.errorf(pos, format!("unknown operator \\{w}"));
                self.tok(Kind::Eof, pos)
            }
        })
    }

    fn try_base_number(&mut self, base: char) -> Option<String> {
        let valid = |c: char| match base {
            'b' | 'B' => c == '0' || c == '1',
            'o' | 'O' => ('0'..='7').contains(&c),
            _ => c.is_ascii_hexdigit(),
        };
        if !matches!(self.peek_at(1), Some(c) if valid(c)) {
            return None;
        }
        self.next_ch(); // base letter
        let start = self.i;
        while matches!(self.peek(), Some(c) if valid(c)) {
            self.next_ch();
        }
        let digits: String = self.chars[start..self.i].iter().collect();
        Some(format!("\\{base}{digits}"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn kinds(src: &str) -> Vec<(Kind, String)> {
        let mut s = Scanner::new(src);
        let toks = s.scan_all();
        assert!(s.errors().is_empty(), "scan errors: {:?}", s.errors());
        toks.into_iter()
            .take_while(|t| t.kind != Kind::Eof)
            .map(|t| (t.kind, t.lit))
            .collect()
    }

    #[test]
    fn basic_tokens() {
        let ts = kinds("---- MODULE Foo ----");
        assert_eq!(ts[0].0, Kind::Dashes);
        assert_eq!(ts[1].0, Kind::Module);
        assert_eq!(ts[2], (Kind::Ident, "Foo".into()));
        assert_eq!(ts[3].0, Kind::Dashes);

        let ts = kinds("x' = x + 1");
        assert_eq!(
            ts.iter().map(|t| t.0).collect::<Vec<_>>(),
            vec![
                Kind::Ident,
                Kind::Prime,
                Kind::Op,
                Kind::Ident,
                Kind::Op,
                Kind::Number
            ]
        );
    }

    #[test]
    fn aliases_canonicalize() {
        let ts = kinds(r"a \land b # c =< d \union e");
        assert_eq!(ts[1].0, Kind::And);
        assert_eq!(ts[3], (Kind::Op, "/=".into()));
        assert_eq!(ts[5], (Kind::Op, "<=".into()));
        assert_eq!(ts[7], (Kind::Op, "\\cup".into()));
    }

    #[test]
    fn subscripts_and_fairness() {
        let ts = kinds("[][Next]_vars /\\ WF_v(N) <><<A>>_x");
        assert_eq!(ts[0].0, Kind::Box);
        assert_eq!(ts[3].0, Kind::RBrackSub);
        assert_eq!(ts[6].0, Kind::WfSub);
        assert_eq!(ts[7], (Kind::Ident, "v".into()));
        assert_eq!(ts[11].0, Kind::Diamond);
        assert_eq!(ts[14].0, Kind::RTupSub);
    }

    #[test]
    fn numbers_strings_comments() {
        let ts = kinds("42 3.14 \\b0101 \\hFF \"a\\\"b\" (* c (* d *) *) x \\* tail\ny");
        assert_eq!(ts[0], (Kind::Number, "42".into()));
        assert_eq!(ts[1], (Kind::Number, "3.14".into()));
        assert_eq!(ts[2], (Kind::Number, "\\b0101".into()));
        assert_eq!(ts[3], (Kind::Number, "\\hFF".into()));
        assert_eq!(ts[4], (Kind::Str, "a\"b".into()));
        assert_eq!(ts[5], (Kind::Ident, "x".into()));
        assert_eq!(ts[6], (Kind::Ident, "y".into()));
    }

    #[test]
    fn positions() {
        let mut s = Scanner::new("ab cd\n  ef");
        let t1 = s.scan();
        let t2 = s.scan();
        let t3 = s.scan();
        assert_eq!((t1.pos.line, t1.pos.col, t1.pos.offset), (1, 1, 0));
        assert_eq!((t2.pos.line, t2.pos.col, t2.pos.offset), (1, 4, 3));
        assert_eq!((t3.pos.line, t3.pos.col, t3.pos.offset), (2, 3, 8));
    }

    #[test]
    fn reserved_rejected() {
        let mut s = Scanner::new("PROOF");
        s.scan();
        assert!(!s.errors().is_empty());
    }
}
