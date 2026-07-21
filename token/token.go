// Package token defines the lexical tokens of TLA+ together with the
// operator precedence tables from Specifying Systems. Positions carry
// line and column because TLA+ junction lists (aligned /\ and \/ bullets)
// make the grammar column-sensitive.
package token

import "fmt"

// Pos is a position in a source file. Line and Col are 1-based; Col counts
// runes, matching how SANY and tree-sitter-tlaplus measure alignment.
// Offset is the 0-based byte offset into the source.
type Pos struct {
	Line   int
	Col    int
	Offset int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

func (p Pos) IsValid() bool { return p.Line > 0 }

// Kind identifies the class of a token.
type Kind int

const (
	ILLEGAL Kind = iota
	EOF
	COMMENT

	// Literals and names.
	IDENT  // Foo, x_1
	NUMBER // 42, 3.14, \b0101, \o777, \hFF
	STRING // "hello"

	// Structural punctuation.
	LPAREN    // (
	RPAREN    // )
	LBRACK    // [
	RBRACK    // ]
	RBRACKSUB // ]_   (only when _ is adjacent: [A]_v)
	LBRACE    // {
	RBRACE    // }
	LTUP      // <<
	RTUP      // >>
	RTUPSUB   // >>_  (only when _ is adjacent: <<A>>_v)
	COMMA     // ,
	COLON     // :
	DOT       // .
	BANG      // !
	AT        // @
	UNDERSCORE
	DEFEQ  // ==
	MAPSTO // |->
	ARROW  // ->
	LARROW // <-
	DASHES // ---- (4 or more)
	MODEND // ==== (4 or more)

	// Junction bullets and contextual operators.
	AND     // /\
	OR      // \/
	BOX     // []  (temporal always; also the CASE separator)
	DIAMOND // <>
	PRIME   // '
	TIMES   // \X or \times (n-ary Cartesian product)

	// Quantifiers and binders.
	FORALL  // \A
	EXISTS  // \E
	TFORALL // \AA
	TEXISTS // \EE

	// Generic operator token; Lit holds the canonical spelling.
	OP

	// Fairness subscript prefixes.
	WFSUB // WF_
	SFSUB // SF_

	keywordStart
	MODULE
	EXTENDS
	CONSTANT
	CONSTANTS
	VARIABLE
	VARIABLES
	ASSUME
	ASSUMPTION
	AXIOM
	THEOREM
	LEMMA
	PROPOSITION
	COROLLARY
	LOCAL
	INSTANCE
	WITH
	LET
	IN
	IF
	THEN
	ELSE
	CASE
	OTHER
	CHOOSE
	LAMBDA
	RECURSIVE
	TRUE
	FALSE
	BOOLEAN
	STRINGKW // the STRING keyword (set of all strings)
	ENABLED
	UNCHANGED
	SUBSET
	UNION
	DOMAIN
	EXCEPT
	keywordEnd
)

var kindNames = map[Kind]string{
	ILLEGAL: "ILLEGAL", EOF: "EOF", COMMENT: "COMMENT",
	IDENT: "IDENT", NUMBER: "NUMBER", STRING: "STRING",
	LPAREN: "(", RPAREN: ")", LBRACK: "[", RBRACK: "]", RBRACKSUB: "]_",
	LBRACE: "{", RBRACE: "}", LTUP: "<<", RTUP: ">>", RTUPSUB: ">>_",
	COMMA: ",", COLON: ":", DOT: ".", BANG: "!", AT: "@", UNDERSCORE: "_",
	DEFEQ: "==", MAPSTO: "|->", ARROW: "->", LARROW: "<-",
	DASHES: "----", MODEND: "====",
	AND: "/\\", OR: "\\/", BOX: "[]", DIAMOND: "<>", PRIME: "'", TIMES: "\\X",
	FORALL: "\\A", EXISTS: "\\E", TFORALL: "\\AA", TEXISTS: "\\EE",
	OP: "OP", WFSUB: "WF_", SFSUB: "SF_",
	MODULE: "MODULE", EXTENDS: "EXTENDS",
	CONSTANT: "CONSTANT", CONSTANTS: "CONSTANTS",
	VARIABLE: "VARIABLE", VARIABLES: "VARIABLES",
	ASSUME: "ASSUME", ASSUMPTION: "ASSUMPTION", AXIOM: "AXIOM",
	THEOREM: "THEOREM", LEMMA: "LEMMA", PROPOSITION: "PROPOSITION", COROLLARY: "COROLLARY",
	LOCAL: "LOCAL", INSTANCE: "INSTANCE", WITH: "WITH",
	LET: "LET", IN: "IN", IF: "IF", THEN: "THEN", ELSE: "ELSE",
	CASE: "CASE", OTHER: "OTHER", CHOOSE: "CHOOSE", LAMBDA: "LAMBDA",
	RECURSIVE: "RECURSIVE", TRUE: "TRUE", FALSE: "FALSE",
	BOOLEAN: "BOOLEAN", STRINGKW: "STRING",
	ENABLED: "ENABLED", UNCHANGED: "UNCHANGED",
	SUBSET: "SUBSET", UNION: "UNION", DOMAIN: "DOMAIN", EXCEPT: "EXCEPT",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

var keywords = func() map[string]Kind {
	m := make(map[string]Kind)
	for k := keywordStart + 1; k < keywordEnd; k++ {
		m[kindNames[k]] = k
	}
	return m
}()

// Lookup maps an identifier to its keyword kind, or IDENT.
func Lookup(name string) Kind {
	if k, ok := keywords[name]; ok {
		return k
	}
	return IDENT
}

// reserved holds TLA+ reserved words that this library does not give
// structure to (mostly the TLA+2 proof language). They may not be used as
// identifiers.
var reserved = map[string]bool{
	"ACTION": true, "BY": true, "DEF": true, "DEFINE": true, "DEFS": true,
	"HAVE": true, "HIDE": true, "NEW": true, "OBVIOUS": true, "OMITTED": true,
	"ONLY": true, "PICK": true, "PROOF": true, "PROVE": true, "QED": true,
	"STATE": true, "SUFFICES": true, "TAKE": true, "TEMPORAL": true,
	"USE": true, "WITNESS": true,
}

// IsReserved reports whether name is a reserved word with no token kind of
// its own (proof-language words).
func IsReserved(name string) bool { return reserved[name] }

// Token is a single lexical token.
type Token struct {
	Kind Kind
	Lit  string // literal text for IDENT, NUMBER, STRING (decoded), OP (canonical), COMMENT
	Pos  Pos
}

func (t Token) String() string {
	switch t.Kind {
	case IDENT, NUMBER, OP, COMMENT:
		return t.Lit
	case STRING:
		return fmt.Sprintf("%q", t.Lit)
	}
	return t.Kind.String()
}
