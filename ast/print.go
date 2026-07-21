package ast

import (
	"fmt"
	"strings"

	"github.com/aburan28/tlacuilo/token"
)

// String renders the module as formatted TLA+ source.
func (m *Module) String() string {
	var p printer
	p.module(m)
	return p.b.String()
}

// ExprString renders a single expression as TLA+ source.
func ExprString(e Expr) string {
	var p printer
	p.expr(e, 0)
	return p.b.String()
}

const headerWidth = 77

type printer struct {
	b   strings.Builder
	col int // 1-based column of the next rune written
}

func (p *printer) ws(s string) {
	if p.col == 0 {
		p.col = 1
	}
	p.b.WriteString(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		p.col = len([]rune(s[i+1:])) + 1
	} else {
		p.col += len([]rune(s))
	}
}

// nl starts a new line indented so the next rune lands at column col.
func (p *printer) nl(col int) {
	p.b.WriteByte('\n')
	if col < 1 {
		col = 1
	}
	p.b.WriteString(strings.Repeat(" ", col-1))
	p.col = col
}

func (p *printer) module(m *Module) {
	title := " MODULE " + m.Name + " "
	pad := headerWidth - len(title)
	left := pad / 2
	if left < 4 {
		left = 4
	}
	right := pad - left
	if right < 4 {
		right = 4
	}
	p.ws(strings.Repeat("-", left) + title + strings.Repeat("-", right))
	p.nl(1)
	if len(m.Extends) > 0 {
		p.ws("EXTENDS " + strings.Join(m.Extends, ", "))
		p.nl(1)
	}
	for _, u := range m.Units {
		p.nl(1)
		p.unit(u)
		p.nl(1)
	}
	p.nl(1)
	p.ws(strings.Repeat("=", headerWidth))
	p.nl(1)
}

func opDeclString(d OpDecl) string {
	if d.Arity == 0 {
		return d.Name
	}
	return d.Name + "(" + strings.TrimSuffix(strings.Repeat("_, ", d.Arity), ", ") + ")"
}

func opDeclList(ds []OpDecl) string {
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = opDeclString(d)
	}
	return strings.Join(parts, ", ")
}

func (p *printer) unit(u Unit) {
	switch u := u.(type) {
	case *ConstantDecl:
		kw := "CONSTANTS"
		if len(u.Decls) == 1 {
			kw = "CONSTANT"
		}
		p.ws(kw + " " + opDeclList(u.Decls))
	case *VariableDecl:
		kw := "VARIABLES"
		if len(u.Names) == 1 {
			kw = "VARIABLE"
		}
		p.ws(kw + " " + strings.Join(u.Names, ", "))
	case *OperatorDef:
		p.operatorDef(u)
	case *FunctionDef:
		if u.Local {
			p.ws("LOCAL ")
		}
		p.ws(u.Name + "[")
		p.bounds(u.Bounds)
		p.ws("] == ")
		p.expr(u.Body, 0)
	case *Instance:
		p.instance(u)
	case *ModuleDef:
		if u.Local {
			p.ws("LOCAL ")
		}
		p.ws(u.Name)
		if len(u.Params) > 0 {
			p.ws("(" + opDeclList(u.Params) + ")")
		}
		p.ws(" == ")
		p.instance(u.Instance)
	case *Assume:
		kw := u.Keyword
		if kw == "" {
			kw = "ASSUME"
		}
		p.ws(kw + " ")
		if u.Name != "" {
			p.ws(u.Name + " == ")
		}
		p.expr(u.Expr, 0)
	case *Theorem:
		kw := u.Keyword
		if kw == "" {
			kw = "THEOREM"
		}
		p.ws(kw + " ")
		if u.Name != "" {
			p.ws(u.Name + " == ")
		}
		p.expr(u.Expr, 0)
	case *Recursive:
		p.ws("RECURSIVE " + opDeclList(u.Decls))
	case *Separator:
		p.ws(strings.Repeat("-", headerWidth))
	case *NestedModule:
		p.ws(u.Module.String())
	default:
		panic(fmt.Sprintf("ast: unknown unit %T", u))
	}
}

func (p *printer) operatorDef(u *OperatorDef) {
	if u.Local {
		p.ws("LOCAL ")
	}
	switch u.Fixity {
	case Infix:
		p.ws(u.Params[0].Name + " " + u.Name + " " + u.Params[1].Name)
	case Postfix:
		p.ws(u.Params[0].Name + " " + u.Name)
	case PrefixSym:
		p.ws(u.Name + " " + u.Params[0].Name)
	default:
		p.ws(u.Name)
		if len(u.Params) > 0 {
			p.ws("(" + opDeclList(u.Params) + ")")
		}
	}
	p.ws(" == ")
	p.expr(u.Body, 0)
}

func (p *printer) instance(u *Instance) {
	if u.Local {
		p.ws("LOCAL ")
	}
	p.ws("INSTANCE " + u.Module)
	for i, s := range u.With {
		if i == 0 {
			p.ws(" WITH ")
		} else {
			p.ws(", ")
		}
		p.ws(s.Name + " <- ")
		p.expr(s.Expr, 0)
	}
}

func (p *printer) bounds(bs []Bound) {
	for i, b := range bs {
		if i > 0 {
			p.ws(", ")
		}
		if b.Tuple {
			p.ws("<<" + strings.Join(b.Names, ", ") + ">>")
		} else {
			p.ws(strings.Join(b.Names, ", "))
		}
		if b.Set != nil {
			p.ws(" \\in ")
			p.expr(b.Set, 0)
		}
	}
}

// exprInfo returns the effective operator range of e's top construct and
// whether e is "greedy" (extends as far to the right as possible, so it
// must be parenthesized whenever it appears under an operator).
func exprInfo(e Expr) (info token.OpInfo, op string, isOperator, greedy bool) {
	switch e := e.(type) {
	case *Binary:
		return token.InfixOps[e.Op], e.Op, true, false
	case *Unary:
		return token.PrefixOps[e.Op], e.Op, true, false
	case *PostfixExpr:
		return token.PostfixOps[e.Op], e.Op, true, false
	case *Times:
		return token.TimesOp, "\\X", true, false
	case *FnApply:
		return token.OpInfo{Lo: token.PrecApply, Hi: token.PrecApply}, "", true, false
	case *Dot:
		return token.OpInfo{Lo: token.PrecDot, Hi: token.PrecDot}, "", true, false
	case *Junction, *If, *Case, *Let, *Quant, *Choose, *Lambda:
		return token.OpInfo{}, "", false, true
	default:
		return token.OpInfo{}, "", false, false
	}
}

// needParens reports whether child must be parenthesized when it appears
// as an operand whose surrounding context only absorbs operators with
// Lo > min. sameOp is the parent operator when the parent is an
// associative infix operator (chains without parentheses).
func needParens(child Expr, min int, sameOp string) bool {
	info, op, isOperator, greedy := exprInfo(child)
	if greedy {
		return min > 0
	}
	if !isOperator {
		return false
	}
	if sameOp != "" && op == sameOp {
		return false
	}
	return info.Lo <= min
}

// operand prints child, parenthesizing it as required by the context.
func (p *printer) operand(child Expr, min int, sameOp string) {
	if needParens(child, min, sameOp) {
		p.ws("(")
		p.expr(child, 0)
		p.ws(")")
	} else {
		p.expr(child, min)
	}
}

// expr prints e. min is the absorption bound of the surrounding context:
// the context claims all operators with Lo <= min, so any top-level
// operator of e with Lo <= min would be mis-parsed and expr must have
// been wrapped via operand() beforehand. Passing 0 means "unconstrained".
func (p *printer) expr(e Expr, min int) {
	switch e := e.(type) {
	case *Ident:
		p.ws(e.Name)
	case *GeneralIdent:
		for _, arm := range e.Prefix {
			p.ws(arm.Name)
			if len(arm.Args) > 0 {
				p.ws("(")
				p.exprList(arm.Args)
				p.ws(")")
			}
			p.ws("!")
		}
		p.ws(e.Name)
	case *NumberLit:
		p.ws(e.Lit)
	case *StringLit:
		p.ws(quoteTLA(e.Value))
	case *BoolLit:
		if e.Value {
			p.ws("TRUE")
		} else {
			p.ws("FALSE")
		}
	case *OpRef:
		p.ws(e.Op)
	case *Apply:
		p.expr(e.Fun, 0)
		p.ws("(")
		p.exprList(e.Args)
		p.ws(")")
	case *Unary:
		info := token.PrefixOps[e.Op]
		switch e.Op {
		case "~", "[]", "<>", "-":
			p.ws(e.Op)
		default:
			p.ws(e.Op + " ")
		}
		p.operand(e.X, info.Hi, "")
	case *Binary:
		info := token.InfixOps[e.Op]
		p.operand(e.L, info.Hi, chainOp(e.Op, info))
		if e.Op == ".." {
			p.ws("..") // conventionally written tight: 1..N
		} else {
			p.ws(" " + e.Op + " ")
		}
		p.operand(e.R, info.Hi, chainOp(e.Op, info))
	case *PostfixExpr:
		p.postfixOperand(e.X)
		p.ws(e.Op)
	case *Times:
		for i, f := range e.Factors {
			if i > 0 {
				p.ws(" \\X ")
			}
			p.operand(f, token.TimesOp.Hi, "")
		}
	case *Junction:
		start := p.col
		for i, item := range e.Items {
			if i > 0 {
				p.nl(start)
			}
			p.ws(e.Op + " ")
			p.expr(item, 0)
		}
	case *Paren:
		p.ws("(")
		p.expr(e.X, 0)
		p.ws(")")
	case *FnApply:
		p.postfixOperand(e.Fn)
		p.ws("[")
		p.exprList(e.Args)
		p.ws("]")
	case *Dot:
		p.postfixOperand(e.X)
		p.ws("." + e.Field)
	case *Tuple:
		p.ws("<<")
		p.exprList(e.Elems)
		p.ws(">>")
	case *SetEnum:
		p.ws("{")
		p.exprList(e.Elems)
		p.ws("}")
	case *SetFilter:
		p.ws("{")
		p.bounds([]Bound{e.Bound})
		p.ws(" : ")
		p.expr(e.Pred, 0)
		p.ws("}")
	case *SetMap:
		p.ws("{")
		p.expr(e.Body, 0)
		p.ws(" : ")
		p.bounds(e.Bounds)
		p.ws("}")
	case *FuncLit:
		p.ws("[")
		p.bounds(e.Bounds)
		p.ws(" |-> ")
		p.expr(e.Body, 0)
		p.ws("]")
	case *FuncSet:
		p.ws("[")
		p.expr(e.Domain, 0)
		p.ws(" -> ")
		p.expr(e.Range, 0)
		p.ws("]")
	case *RecordLit:
		p.ws("[")
		for i, f := range e.Fields {
			if i > 0 {
				p.ws(", ")
			}
			p.ws(f.Name + " |-> ")
			p.expr(f.Expr, 0)
		}
		p.ws("]")
	case *RecordSet:
		p.ws("[")
		for i, f := range e.Fields {
			if i > 0 {
				p.ws(", ")
			}
			p.ws(f.Name + " : ")
			p.expr(f.Expr, 0)
		}
		p.ws("]")
	case *Except:
		p.ws("[")
		p.expr(e.Fn, 0)
		p.ws(" EXCEPT ")
		for i, s := range e.Specs {
			if i > 0 {
				p.ws(", ")
			}
			p.ws("!")
			for _, step := range s.Path {
				if step.Field != "" {
					p.ws("." + step.Field)
				} else {
					p.ws("[")
					p.exprList(step.Index)
					p.ws("]")
				}
			}
			p.ws(" = ")
			p.expr(s.Value, 0)
		}
		p.ws("]")
	case *At:
		p.ws("@")
	case *If:
		start := p.col
		multi := containsMultiline(e.Cond) || containsMultiline(e.Then) || containsMultiline(e.Else)
		p.ws("IF ")
		p.expr(e.Cond, 0)
		if multi {
			p.nl(start + 2)
			p.ws("THEN ")
		} else {
			p.ws(" THEN ")
		}
		p.expr(e.Then, 0)
		if multi {
			p.nl(start + 2)
			p.ws("ELSE ")
		} else {
			p.ws(" ELSE ")
		}
		p.expr(e.Else, 0)
	case *Case:
		start := p.col
		multi := false
		for _, a := range e.Arms {
			multi = multi || containsMultiline(a.Cond) || containsMultiline(a.Value)
		}
		multi = multi || (e.Other != nil && containsMultiline(e.Other)) || len(e.Arms) > 2
		p.ws("CASE ")
		for i, a := range e.Arms {
			if i > 0 {
				if multi {
					p.nl(start + 2)
				} else {
					p.ws(" ")
				}
				p.ws("[] ")
			}
			p.caseArmCond(a.Cond)
			p.ws(" -> ")
			p.caseArmCond(a.Value)
		}
		if e.Other != nil {
			if multi {
				p.nl(start + 2)
			} else {
				p.ws(" ")
			}
			p.ws("[] OTHER -> ")
			p.expr(e.Other, 0)
		}
	case *Let:
		start := p.col
		p.ws("LET ")
		defCol := p.col
		for i, d := range e.Defs {
			if i > 0 {
				p.nl(defCol)
			}
			p.unit(d)
		}
		p.nl(start)
		p.ws("IN  ")
		p.expr(e.Body, 0)
	case *Quant:
		var kw string
		switch e.Kind {
		case Forall:
			kw = "\\A"
		case Exists:
			kw = "\\E"
		case TemporalForall:
			kw = "\\AA"
		case TemporalExists:
			kw = "\\EE"
		}
		p.ws(kw + " ")
		p.bounds(e.Bounds)
		p.ws(" : ")
		p.expr(e.Body, 0)
	case *Choose:
		p.ws("CHOOSE ")
		if len(e.Tuple) > 0 {
			p.ws("<<" + strings.Join(e.Tuple, ", ") + ">>")
		} else {
			p.ws(e.Var)
		}
		if e.Set != nil {
			p.ws(" \\in ")
			p.expr(e.Set, 0)
		}
		p.ws(" : ")
		p.expr(e.Body, 0)
	case *SquareAct:
		p.ws("[")
		p.expr(e.X, 0)
		p.ws("]_")
		p.subscript(e.Sub)
	case *AngleAct:
		p.ws("<<")
		p.expr(e.X, 0)
		p.ws(">>_")
		p.subscript(e.Sub)
	case *Fairness:
		if e.Strong {
			p.ws("SF_")
		} else {
			p.ws("WF_")
		}
		p.subscript(e.Sub)
		p.ws("(")
		p.expr(e.X, 0)
		p.ws(")")
	case *Lambda:
		p.ws("LAMBDA " + strings.Join(e.Params, ", ") + " : ")
		p.expr(e.Body, 0)
	default:
		panic(fmt.Sprintf("ast: unknown expression %T", e))
	}
}

// chainOp returns op when it is associative (so identical children may
// chain without parentheses), else "".
func chainOp(op string, info token.OpInfo) string {
	if info.Assoc {
		return op
	}
	return ""
}

// postfixOperand prints the operand of a tight postfix construct
// (function application, record selection, prime): only name-like and
// bracketed expressions may appear bare.
func (p *printer) postfixOperand(x Expr) {
	switch x.(type) {
	case *Ident, *GeneralIdent, *Apply, *FnApply, *Dot, *Paren, *Tuple,
		*SetEnum, *RecordLit, *FuncLit, *Except, *StringLit, *NumberLit, *PostfixExpr:
		p.expr(x, 0)
	default:
		p.ws("(")
		p.expr(x, 0)
		p.ws(")")
	}
}

// subscript prints the _v subscript of [A]_v, <<A>>_v, WF_v, SF_v.
func (p *printer) subscript(x Expr) {
	switch x.(type) {
	case *Ident, *GeneralIdent, *Tuple, *Paren, *Dot:
		p.expr(x, 0)
	default:
		p.ws("(")
		p.expr(x, 0)
		p.ws(")")
	}
}

// caseArmCond prints a CASE arm component; greedy constructs would
// swallow the -> or [] that follows, so they are parenthesized.
func (p *printer) caseArmCond(e Expr) {
	if _, _, _, greedy := exprInfo(e); greedy {
		p.ws("(")
		p.expr(e, 0)
		p.ws(")")
		return
	}
	p.expr(e, 0)
}

func (p *printer) exprList(es []Expr) {
	for i, e := range es {
		if i > 0 {
			p.ws(", ")
		}
		p.expr(e, 0)
	}
}

// containsMultiline reports whether printing e produces line breaks.
func containsMultiline(e Expr) bool {
	switch e := e.(type) {
	case *Junction, *Let:
		return true
	case *Binary:
		return containsMultiline(e.L) || containsMultiline(e.R)
	case *Unary:
		return containsMultiline(e.X)
	case *PostfixExpr:
		return containsMultiline(e.X)
	case *Paren:
		return containsMultiline(e.X)
	case *If:
		return containsMultiline(e.Cond) || containsMultiline(e.Then) || containsMultiline(e.Else)
	case *Case:
		if len(e.Arms) > 2 {
			return true
		}
		for _, a := range e.Arms {
			if containsMultiline(a.Cond) || containsMultiline(a.Value) {
				return true
			}
		}
		return e.Other != nil && containsMultiline(e.Other)
	case *Quant:
		return containsMultiline(e.Body)
	case *SquareAct:
		return containsMultiline(e.X)
	case *AngleAct:
		return containsMultiline(e.X)
	}
	return false
}

func quoteTLA(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
