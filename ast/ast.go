// Package ast declares the types used to represent TLA+ syntax trees,
// together with a pretty-printer that renders them as parseable,
// canonically formatted TLA+ source.
package ast

import "github.com/aburan28/tlacuilo/token"

// Node is the interface implemented by all AST nodes.
type Node interface {
	Pos() token.Pos
}

// Expr is the interface implemented by all expression nodes.
type Expr interface {
	Node
	exprNode()
}

// Unit is a module-level unit: a declaration, definition, assumption,
// theorem, instance, separator line, or nested module.
type Unit interface {
	Node
	unitNode()
}

// Module is a TLA+ module.
type Module struct {
	StartPos token.Pos
	Name     string
	Extends  []string
	Units    []Unit
}

func (m *Module) Pos() token.Pos { return m.StartPos }

// OpDecl declares an operator name with an arity, as in CONSTANT F(_, _).
// Arity 0 declares a plain name.
type OpDecl struct {
	Name  string
	Arity int
}

// Bound is one bound-variable group of a quantifier, set construct, or
// function constructor: "x, y \in S", "<<x, y>> \in S", or (unbounded,
// Set == nil) "x, y".
type Bound struct {
	Names []string
	Tuple bool // names were written as a tuple <<x, y>>
	Set   Expr // nil for unbounded quantification
}

// Field is a record-constructor or record-set field.
type Field struct {
	Name string
	Expr Expr
}

// ---------------------------------------------------------------- units

// ConstantDecl is a CONSTANT/CONSTANTS declaration.
type ConstantDecl struct {
	StartPos token.Pos
	Decls    []OpDecl
}

// VariableDecl is a VARIABLE/VARIABLES declaration.
type VariableDecl struct {
	StartPos token.Pos
	Names    []string
}

// Fixity describes how a defined operator's name is written.
type Fixity int

const (
	Prefix    Fixity = iota // Name(args) == e, or Name == e
	Infix                   // a ++ b == e
	Postfix                 // a ^+ == e
	PrefixSym               // -. a == e (unary minus), ~ a == e
)

// OperatorDef is an operator definition. For Fixity == Infix,
// Postfix, or PrefixSym the Name is the operator symbol and Params
// holds the operands.
type OperatorDef struct {
	StartPos token.Pos
	Local    bool
	Fixity   Fixity
	Name     string
	Params   []OpDecl
	Body     Expr
}

// FunctionDef is a function definition: f[x \in S] == e.
type FunctionDef struct {
	StartPos token.Pos
	Local    bool
	Name     string
	Bounds   []Bound
	Body     Expr
}

// Subst is one substitution in an INSTANCE ... WITH clause.
type Subst struct {
	Name string
	Expr Expr
}

// Instance is a bare INSTANCE unit.
type Instance struct {
	StartPos token.Pos
	Local    bool
	Module   string
	With     []Subst
}

// ModuleDef is a named instance: IM(x) == INSTANCE M WITH a <- x.
type ModuleDef struct {
	StartPos token.Pos
	Local    bool
	Name     string
	Params   []OpDecl
	Instance *Instance
}

// Assume is an ASSUME/ASSUMPTION/AXIOM unit.
type Assume struct {
	StartPos token.Pos
	Keyword  string // ASSUME, ASSUMPTION, or AXIOM
	Name     string // optional
	Expr     Expr
}

// Theorem is a THEOREM/LEMMA/PROPOSITION/COROLLARY unit (without proof).
type Theorem struct {
	StartPos token.Pos
	Keyword  string
	Name     string // optional
	Expr     Expr
}

// Recursive is a RECURSIVE declaration.
type Recursive struct {
	StartPos token.Pos
	Decls    []OpDecl
}

// Separator is a ---- line between units.
type Separator struct {
	StartPos token.Pos
}

// NestedModule is a module nested inside another module.
type NestedModule struct {
	Module *Module
}

func (u *ConstantDecl) Pos() token.Pos { return u.StartPos }
func (u *VariableDecl) Pos() token.Pos { return u.StartPos }
func (u *OperatorDef) Pos() token.Pos  { return u.StartPos }
func (u *FunctionDef) Pos() token.Pos  { return u.StartPos }
func (u *Instance) Pos() token.Pos     { return u.StartPos }
func (u *ModuleDef) Pos() token.Pos    { return u.StartPos }
func (u *Assume) Pos() token.Pos       { return u.StartPos }
func (u *Theorem) Pos() token.Pos      { return u.StartPos }
func (u *Recursive) Pos() token.Pos    { return u.StartPos }
func (u *Separator) Pos() token.Pos    { return u.StartPos }
func (u *NestedModule) Pos() token.Pos { return u.Module.Pos() }

func (*ConstantDecl) unitNode() {}
func (*VariableDecl) unitNode() {}
func (*OperatorDef) unitNode()  {}
func (*FunctionDef) unitNode()  {}
func (*Instance) unitNode()     {}
func (*ModuleDef) unitNode()    {}
func (*Assume) unitNode()       {}
func (*Theorem) unitNode()      {}
func (*Recursive) unitNode()    {}
func (*Separator) unitNode()    {}
func (*NestedModule) unitNode() {}

// ---------------------------------------------------------- expressions

// Ident is a reference to a name.
type Ident struct {
	StartPos token.Pos
	Name     string
}

// InstArm is one segment of an instance-qualified reference: Name or
// Name(args).
type InstArm struct {
	Name string
	Args []Expr
}

// GeneralIdent is an instance-qualified reference such as A!B!Op.
type GeneralIdent struct {
	StartPos token.Pos
	Prefix   []InstArm
	Name     string
}

// NumberLit is a numeric literal; Lit preserves the source form
// (including \b, \o, \h bases).
type NumberLit struct {
	StartPos token.Pos
	Lit      string
}

// StringLit is a string literal; Value is the decoded text.
type StringLit struct {
	StartPos token.Pos
	Value    string
}

// BoolLit is TRUE or FALSE.
type BoolLit struct {
	StartPos token.Pos
	Value    bool
}

// Apply is operator application: Op(e1, ..., en). Fun is an Ident or
// GeneralIdent.
type Apply struct {
	Fun  Expr
	Args []Expr
}

// Unary is a prefix-operator application; Op is the canonical spelling
// (~, ENABLED, UNCHANGED, [], <>, SUBSET, UNION, DOMAIN, -).
type Unary struct {
	StartPos token.Pos
	Op       string
	X        Expr
}

// Binary is an infix-operator application; Op is the canonical spelling.
type Binary struct {
	Op   string
	L, R Expr
}

// PostfixExpr is a postfix-operator application (', ^+, ^*, ^#).
type PostfixExpr struct {
	Op string
	X  Expr
}

// Junction is a vertically aligned conjunction or disjunction list.
// Op is "/\" or "\/". It renders as aligned bullets, one item per line.
type Junction struct {
	StartPos token.Pos
	Op       string
	Items    []Expr
}

// Paren is an explicitly parenthesized expression.
type Paren struct {
	StartPos token.Pos
	X        Expr
}

// FnApply is function application: f[e1, ..., en].
type FnApply struct {
	Fn   Expr
	Args []Expr
}

// Dot is record-field selection: r.f.
type Dot struct {
	X     Expr
	Field string
}

// Tuple is <<e1, ..., en>>.
type Tuple struct {
	StartPos token.Pos
	Elems    []Expr
}

// Times is the n-ary Cartesian product S \X T \X U.
type Times struct {
	Factors []Expr
}

// SetEnum is {e1, ..., en}.
type SetEnum struct {
	StartPos token.Pos
	Elems    []Expr
}

// SetFilter is {x \in S : P}.
type SetFilter struct {
	StartPos token.Pos
	Bound    Bound
	Pred     Expr
}

// SetMap is {e : x \in S, y \in T}.
type SetMap struct {
	StartPos token.Pos
	Body     Expr
	Bounds   []Bound
}

// FuncLit is [x \in S |-> e].
type FuncLit struct {
	StartPos token.Pos
	Bounds   []Bound
	Body     Expr
}

// FuncSet is [S -> T].
type FuncSet struct {
	StartPos token.Pos
	Domain   Expr
	Range    Expr
}

// RecordLit is [a |-> e, b |-> f].
type RecordLit struct {
	StartPos token.Pos
	Fields   []Field
}

// RecordSet is [a : S, b : T].
type RecordSet struct {
	StartPos token.Pos
	Fields   []Field
}

// ExceptPath is one step of an EXCEPT path: .field or [e1, ..., en].
type ExceptPath struct {
	Field string // set for .field steps
	Index []Expr // set for [e] steps
}

// ExceptSpec is one !path = value clause of an EXCEPT expression.
type ExceptSpec struct {
	Path  []ExceptPath
	Value Expr
}

// Except is [f EXCEPT ![i] = e, !.a = g].
type Except struct {
	StartPos token.Pos
	Fn       Expr
	Specs    []ExceptSpec
}

// At is the @ placeholder inside an EXCEPT value.
type At struct {
	StartPos token.Pos
}

// If is IF c THEN t ELSE e.
type If struct {
	StartPos token.Pos
	Cond     Expr
	Then     Expr
	Else     Expr
}

// CaseArm is one guard -> value arm of a CASE expression.
type CaseArm struct {
	Cond  Expr
	Value Expr
}

// Case is CASE g1 -> e1 [] g2 -> e2 [] OTHER -> e.
type Case struct {
	StartPos token.Pos
	Arms     []CaseArm
	Other    Expr // nil when there is no OTHER arm
}

// Let is LET defs IN body; Defs holds *OperatorDef, *FunctionDef,
// *Recursive, and *ModuleDef units.
type Let struct {
	StartPos token.Pos
	Defs     []Unit
	Body     Expr
}

// QuantKind distinguishes the quantifier forms.
type QuantKind int

const (
	Forall         QuantKind = iota // \A
	Exists                          // \E
	TemporalForall                  // \AA
	TemporalExists                  // \EE
)

// Quant is a quantified formula.
type Quant struct {
	StartPos token.Pos
	Kind     QuantKind
	Bounds   []Bound
	Body     Expr
}

// Choose is CHOOSE x \in S : P (Set may be nil).
type Choose struct {
	StartPos token.Pos
	Var      string
	Tuple    []string // set when the bound is a tuple <<x, y>>
	Set      Expr
	Body     Expr
}

// SquareAct is [A]_v.
type SquareAct struct {
	StartPos token.Pos
	X        Expr
	Sub      Expr
}

// AngleAct is <<A>>_v.
type AngleAct struct {
	StartPos token.Pos
	X        Expr
	Sub      Expr
}

// Fairness is WF_v(A) or SF_v(A).
type Fairness struct {
	StartPos token.Pos
	Strong   bool
	Sub      Expr
	X        Expr
}

// Lambda is LAMBDA x, y : e (legal only as an operator argument).
type Lambda struct {
	StartPos token.Pos
	Params   []string
	Body     Expr
}

// OpRef names an operator passed as an argument where the bare name is
// itself an operator symbol, e.g. Foo(\cup).
type OpRef struct {
	StartPos token.Pos
	Op       string
}

func (e *Ident) Pos() token.Pos        { return e.StartPos }
func (e *GeneralIdent) Pos() token.Pos { return e.StartPos }
func (e *NumberLit) Pos() token.Pos    { return e.StartPos }
func (e *StringLit) Pos() token.Pos    { return e.StartPos }
func (e *BoolLit) Pos() token.Pos      { return e.StartPos }
func (e *Apply) Pos() token.Pos        { return e.Fun.Pos() }
func (e *Unary) Pos() token.Pos        { return e.StartPos }
func (e *Binary) Pos() token.Pos       { return e.L.Pos() }
func (e *PostfixExpr) Pos() token.Pos  { return e.X.Pos() }
func (e *Junction) Pos() token.Pos     { return e.StartPos }
func (e *Paren) Pos() token.Pos        { return e.StartPos }
func (e *FnApply) Pos() token.Pos      { return e.Fn.Pos() }
func (e *Dot) Pos() token.Pos          { return e.X.Pos() }
func (e *Tuple) Pos() token.Pos        { return e.StartPos }
func (e *Times) Pos() token.Pos        { return e.Factors[0].Pos() }
func (e *SetEnum) Pos() token.Pos      { return e.StartPos }
func (e *SetFilter) Pos() token.Pos    { return e.StartPos }
func (e *SetMap) Pos() token.Pos       { return e.StartPos }
func (e *FuncLit) Pos() token.Pos      { return e.StartPos }
func (e *FuncSet) Pos() token.Pos      { return e.StartPos }
func (e *RecordLit) Pos() token.Pos    { return e.StartPos }
func (e *RecordSet) Pos() token.Pos    { return e.StartPos }
func (e *Except) Pos() token.Pos       { return e.StartPos }
func (e *At) Pos() token.Pos           { return e.StartPos }
func (e *If) Pos() token.Pos           { return e.StartPos }
func (e *Case) Pos() token.Pos         { return e.StartPos }
func (e *Let) Pos() token.Pos          { return e.StartPos }
func (e *Quant) Pos() token.Pos        { return e.StartPos }
func (e *Choose) Pos() token.Pos       { return e.StartPos }
func (e *SquareAct) Pos() token.Pos    { return e.StartPos }
func (e *AngleAct) Pos() token.Pos     { return e.StartPos }
func (e *Fairness) Pos() token.Pos     { return e.StartPos }
func (e *Lambda) Pos() token.Pos       { return e.StartPos }
func (e *OpRef) Pos() token.Pos        { return e.StartPos }

func (*Ident) exprNode()        {}
func (*GeneralIdent) exprNode() {}
func (*NumberLit) exprNode()    {}
func (*StringLit) exprNode()    {}
func (*BoolLit) exprNode()      {}
func (*Apply) exprNode()        {}
func (*Unary) exprNode()        {}
func (*Binary) exprNode()       {}
func (*PostfixExpr) exprNode()  {}
func (*Junction) exprNode()     {}
func (*Paren) exprNode()        {}
func (*FnApply) exprNode()      {}
func (*Dot) exprNode()          {}
func (*Tuple) exprNode()        {}
func (*Times) exprNode()        {}
func (*SetEnum) exprNode()      {}
func (*SetFilter) exprNode()    {}
func (*SetMap) exprNode()       {}
func (*FuncLit) exprNode()      {}
func (*FuncSet) exprNode()      {}
func (*RecordLit) exprNode()    {}
func (*RecordSet) exprNode()    {}
func (*Except) exprNode()       {}
func (*At) exprNode()           {}
func (*If) exprNode()           {}
func (*Case) exprNode()         {}
func (*Let) exprNode()          {}
func (*Quant) exprNode()        {}
func (*Choose) exprNode()       {}
func (*SquareAct) exprNode()    {}
func (*AngleAct) exprNode()     {}
func (*Fairness) exprNode()     {}
func (*Lambda) exprNode()       {}
func (*OpRef) exprNode()        {}
