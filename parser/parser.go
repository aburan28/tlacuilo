// Package parser implements a parser for TLA+ modules and expressions.
//
// The parser covers the module language and the full constant, action, and
// temporal expression language. The TLA+2 proof language is not supported:
// THEOREM and ASSUME units carry their formula only.
//
// TLA+ junction lists are column-sensitive; the parser enforces the
// alignment rules by filtering the token stream against a stack of active
// bullet columns, the same discipline SANY and tree-sitter-tlaplus apply.
package parser

import (
	"fmt"
	"strings"

	"github.com/aburan28/tlacuilo/ast"
	"github.com/aburan28/tlacuilo/scanner"
	"github.com/aburan28/tlacuilo/token"
)

// Error is a parse error at a source position.
type Error struct {
	Pos token.Pos
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

// Parse parses a complete TLA+ module.
func Parse(src string) (*ast.Module, error) {
	p, err := newParser(src)
	if err != nil {
		return nil, err
	}
	m := p.parseModule()
	if p.err != nil {
		return nil, p.err
	}
	return m, nil
}

// ParseExpr parses a single TLA+ expression.
func ParseExpr(src string) (ast.Expr, error) {
	p, err := newParser(src)
	if err != nil {
		return nil, err
	}
	e := p.parseExpr(0)
	if p.err == nil && p.cur().Kind != token.EOF {
		p.errorf(p.cur().Pos, "unexpected %s after expression", p.cur())
	}
	if p.err != nil {
		return nil, p.err
	}
	return e, nil
}

type parser struct {
	toks []token.Token
	i    int
	jcol []int // stack of active junction-list bullet columns
	err  *Error
}

func newParser(src string) (*parser, error) {
	sc := scanner.New(src)
	toks := sc.ScanAll()
	if errs := sc.Errors(); len(errs) > 0 {
		return nil, &Error{Pos: errs[0].Pos, Msg: errs[0].Msg}
	}
	return &parser{toks: toks}, nil
}

func (p *parser) cur() token.Token { return p.at(0) }

func (p *parser) at(n int) token.Token {
	if p.i+n >= len(p.toks) {
		return p.toks[len(p.toks)-1] // EOF
	}
	return p.toks[p.i+n]
}

func (p *parser) next() token.Token {
	t := p.cur()
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) errorf(pos token.Pos, format string, args ...any) {
	if p.err == nil {
		p.err = &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
	}
	// Jump to EOF so parsing unwinds quickly.
	p.i = len(p.toks) - 1
}

func (p *parser) expect(k token.Kind) token.Token {
	if p.cur().Kind != k {
		p.errorf(p.cur().Pos, "expected %s, found %s", k, p.cur())
		return p.cur()
	}
	return p.next()
}

// blocked reports whether the current token is claimed by an enclosing
// junction list: any token starting at or left of the innermost bullet
// column terminates the current junct item.
func (p *parser) blocked() bool {
	if len(p.jcol) == 0 {
		return false
	}
	t := p.cur()
	return t.Kind != token.EOF && t.Pos.Col <= p.jcol[len(p.jcol)-1] || t.Kind == token.EOF
}

// ------------------------------------------------------------- modules

func (p *parser) parseModule() *ast.Module {
	start := p.expect(token.DASHES).Pos
	p.expect(token.MODULE)
	name := p.expect(token.IDENT)
	p.expect(token.DASHES)
	m := &ast.Module{StartPos: start, Name: name.Lit}
	if p.cur().Kind == token.EXTENDS {
		p.next()
		for {
			m.Extends = append(m.Extends, p.expect(token.IDENT).Lit)
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
	}
	for p.err == nil {
		switch p.cur().Kind {
		case token.MODEND:
			p.next()
			return m
		case token.EOF:
			p.errorf(p.cur().Pos, "missing ==== at end of module %s", m.Name)
			return m
		}
		u := p.parseUnit()
		if u != nil {
			m.Units = append(m.Units, u)
		}
	}
	return m
}

func (p *parser) parseUnit() ast.Unit {
	t := p.cur()
	switch t.Kind {
	case token.DASHES:
		if p.at(1).Kind == token.MODULE {
			return &ast.NestedModule{Module: p.parseModule()}
		}
		p.next()
		return &ast.Separator{StartPos: t.Pos}
	case token.CONSTANT, token.CONSTANTS:
		p.next()
		return &ast.ConstantDecl{StartPos: t.Pos, Decls: p.parseOpDeclList()}
	case token.VARIABLE, token.VARIABLES:
		p.next()
		d := &ast.VariableDecl{StartPos: t.Pos}
		for {
			d.Names = append(d.Names, p.expect(token.IDENT).Lit)
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
		return d
	case token.RECURSIVE:
		p.next()
		return &ast.Recursive{StartPos: t.Pos, Decls: p.parseOpDeclList()}
	case token.ASSUME, token.ASSUMPTION, token.AXIOM:
		p.next()
		u := &ast.Assume{StartPos: t.Pos, Keyword: t.Kind.String()}
		if p.cur().Kind == token.IDENT && p.at(1).Kind == token.DEFEQ {
			u.Name = p.next().Lit
			p.next()
		}
		u.Expr = p.parseExpr(0)
		return u
	case token.THEOREM, token.LEMMA, token.PROPOSITION, token.COROLLARY:
		p.next()
		u := &ast.Theorem{StartPos: t.Pos, Keyword: t.Kind.String()}
		if p.cur().Kind == token.IDENT && p.at(1).Kind == token.DEFEQ {
			u.Name = p.next().Lit
			p.next()
		}
		u.Expr = p.parseExpr(0)
		return u
	case token.LOCAL:
		p.next()
		return p.parseLocalUnit(t.Pos)
	case token.INSTANCE:
		return p.parseInstance(false)
	case token.IDENT:
		return p.parseDefinition(false, t.Pos)
	case token.OP:
		if isDefinablePrefixOp(t.Lit) {
			return p.parsePrefixOpDef(false, t.Pos)
		}
	}
	p.errorf(t.Pos, "unexpected %s at module level", t)
	return nil
}

// isDefinablePrefixOp reports whether op may head a prefix-operator
// definition: unary minus is written -. in its definition, and ~ (with
// its \lnot and \neg aliases, canonicalized by the scanner) may be
// redefined.
func isDefinablePrefixOp(op string) bool { return op == "-." || op == "~" }

// parsePrefixOpDef parses a prefix-operator definition such as
// "-. a == 0 - a" (Integers.tla) or "~ a == ...".
func (p *parser) parsePrefixOpDef(local bool, start token.Pos) ast.Unit {
	op := p.next().Lit
	param := p.expect(token.IDENT).Lit
	p.expect(token.DEFEQ)
	return &ast.OperatorDef{StartPos: start, Local: local, Fixity: ast.PrefixSym,
		Name: op, Params: []ast.OpDecl{{Name: param}}, Body: p.parseExpr(0)}
}

func (p *parser) parseLocalUnit(pos token.Pos) ast.Unit {
	switch p.cur().Kind {
	case token.INSTANCE:
		u := p.parseInstance(true)
		return u
	case token.IDENT:
		return p.parseDefinition(true, pos)
	case token.OP:
		if isDefinablePrefixOp(p.cur().Lit) {
			return p.parsePrefixOpDef(true, pos)
		}
	}
	p.errorf(p.cur().Pos, "expected definition or INSTANCE after LOCAL, found %s", p.cur())
	return nil
}

func (p *parser) parseInstance(local bool) *ast.Instance {
	start := p.expect(token.INSTANCE).Pos
	u := &ast.Instance{StartPos: start, Local: local, Module: p.expect(token.IDENT).Lit}
	if p.cur().Kind == token.WITH {
		p.next()
		for {
			name := p.expect(token.IDENT).Lit
			p.expect(token.LARROW)
			u.With = append(u.With, ast.Subst{Name: name, Expr: p.parseExpr(0)})
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
	}
	return u
}

// parseDefinition parses units introduced by an identifier: operator,
// function, and module definitions, including infix/postfix symbol forms.
func (p *parser) parseDefinition(local bool, start token.Pos) ast.Unit {
	name := p.expect(token.IDENT)
	switch p.cur().Kind {
	case token.LBRACK:
		// f[x \in S] == e
		p.next()
		bounds := p.parseBounds(true)
		p.expect(token.RBRACK)
		p.expect(token.DEFEQ)
		return &ast.FunctionDef{StartPos: start, Local: local, Name: name.Lit,
			Bounds: bounds, Body: p.parseExpr(0)}
	case token.LPAREN:
		p.next()
		var params []ast.OpDecl
		for {
			params = append(params, p.parseOpDecl())
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
		p.expect(token.RPAREN)
		p.expect(token.DEFEQ)
		return p.finishOperatorDef(start, local, ast.Prefix, name.Lit, params)
	case token.DEFEQ:
		p.next()
		return p.finishOperatorDef(start, local, ast.Prefix, name.Lit, nil)
	}
	// Infix definition: a ++ b == e; postfix definition: a ^+ == e.
	if op, ok := p.definableOp(); ok {
		if _, isPost := token.PostfixOps[op]; isPost && p.at(1).Kind == token.DEFEQ {
			p.next()
			p.next()
			return p.finishOperatorDef(start, local, ast.Postfix, op,
				[]ast.OpDecl{{Name: name.Lit}})
		}
		if p.at(1).Kind == token.IDENT && p.at(2).Kind == token.DEFEQ {
			p.next()
			rhs := p.next().Lit
			p.next()
			return p.finishOperatorDef(start, local, ast.Infix, op,
				[]ast.OpDecl{{Name: name.Lit}, {Name: rhs}})
		}
	}
	p.errorf(p.cur().Pos, "expected ==, (, or [ in definition of %s, found %s", name.Lit, p.cur())
	return nil
}

// definableOp returns the operator symbol at the current token when it
// could be a user-defined operator name.
func (p *parser) definableOp() (string, bool) {
	switch t := p.cur(); t.Kind {
	case token.OP:
		return t.Lit, true
	case token.AND:
		return "/\\", true
	case token.OR:
		return "\\/", true
	}
	return "", false
}

func (p *parser) finishOperatorDef(start token.Pos, local bool, fix ast.Fixity, name string, params []ast.OpDecl) ast.Unit {
	if p.cur().Kind == token.INSTANCE {
		inst := p.parseInstance(false)
		return &ast.ModuleDef{StartPos: start, Local: local, Name: name, Params: params, Instance: inst}
	}
	return &ast.OperatorDef{StartPos: start, Local: local, Fixity: fix,
		Name: name, Params: params, Body: p.parseExpr(0)}
}

func (p *parser) parseOpDeclList() []ast.OpDecl {
	var ds []ast.OpDecl
	for {
		ds = append(ds, p.parseOpDecl())
		if p.cur().Kind != token.COMMA {
			return ds
		}
		p.next()
	}
}

func (p *parser) parseOpDecl() ast.OpDecl {
	name := p.expect(token.IDENT)
	d := ast.OpDecl{Name: name.Lit}
	if p.cur().Kind == token.LPAREN && p.at(1).Kind == token.UNDERSCORE {
		p.next()
		for {
			p.expect(token.UNDERSCORE)
			d.Arity++
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
		p.expect(token.RPAREN)
	}
	return d
}

// --------------------------------------------------------- expressions

// parseExpr parses an expression, absorbing infix and postfix operators
// whose precedence-range low bound is greater than min.
func (p *parser) parseExpr(min int) ast.Expr {
	lhs := p.parsePrimary()
	var prev *token.OpInfo
	prevOp := ""
	for p.err == nil && !p.blocked() {
		t := p.cur()
		// Tight postfix constructs: field selection and function application.
		switch t.Kind {
		case token.DOT:
			if token.PrecDot <= min {
				return lhs
			}
			p.next()
			lhs = &ast.Dot{X: lhs, Field: p.expect(token.IDENT).Lit}
			continue
		case token.LBRACK:
			if token.PrecApply <= min {
				return lhs
			}
			p.next()
			var args []ast.Expr
			for {
				args = append(args, p.parseExpr(0))
				if p.cur().Kind != token.COMMA {
					break
				}
				p.next()
			}
			p.expect(token.RBRACK)
			lhs = &ast.FnApply{Fn: lhs, Args: args}
			continue
		case token.PRIME:
			info := token.PostfixOps["'"]
			if info.Lo <= min {
				return lhs
			}
			p.next()
			lhs = &ast.PostfixExpr{Op: "'", X: lhs}
			continue
		case token.TIMES:
			if token.TimesOp.Lo <= min {
				return lhs
			}
			if prev != nil && overlaps(*prev, token.TimesOp) && prevOp != "\\X" {
				p.errorf(t.Pos, "operator \\X conflicts with preceding operator; add parentheses")
				return lhs
			}
			factors := []ast.Expr{lhs}
			for p.cur().Kind == token.TIMES && !p.blocked() {
				p.next()
				factors = append(factors, p.parseExpr(token.TimesOp.Hi))
			}
			cp := token.TimesOp
			prev = &cp
			prevOp = "\\X"
			lhs = &ast.Times{Factors: factors}
			continue
		}
		op, ok := p.infixAt()
		if !ok {
			return lhs
		}
		if op == "" { // postfix ^+ ^* ^#
			info := token.PostfixOps[p.cur().Lit]
			if info.Lo <= min {
				return lhs
			}
			lhs = &ast.PostfixExpr{Op: p.next().Lit, X: lhs}
			continue
		}
		info := token.InfixOps[op]
		if info.Lo <= min {
			return lhs
		}
		if prev != nil && overlaps(*prev, info) && !(info.Assoc && op == prevOp) {
			p.errorf(t.Pos, "operator %s conflicts with preceding operator of overlapping precedence; add parentheses", op)
			return lhs
		}
		p.next()
		rhs := p.parseExpr(info.Hi)
		lhs = &ast.Binary{Op: op, L: lhs, R: rhs}
		cp := info
		prev = &cp
		prevOp = op
	}
	return lhs
}

func overlaps(a, b token.OpInfo) bool { return a.Lo <= b.Hi && b.Lo <= a.Hi }

// infixAt reports the canonical infix operator at the cursor. It returns
// ("", true) for postfix OP tokens (^+ ^* ^#).
func (p *parser) infixAt() (string, bool) {
	switch t := p.cur(); t.Kind {
	case token.AND:
		return "/\\", true
	case token.OR:
		return "\\/", true
	case token.OP:
		if _, ok := token.PostfixOps[t.Lit]; ok {
			return "", true
		}
		if _, ok := token.InfixOps[t.Lit]; ok {
			return t.Lit, true
		}
	}
	return "", false
}

func (p *parser) parsePrimary() ast.Expr {
	if p.blocked() {
		p.errorf(p.cur().Pos, "expression ends here (token %s is outside the junction list alignment)", p.cur())
		return &ast.Ident{StartPos: p.cur().Pos, Name: "?"}
	}
	t := p.cur()
	switch t.Kind {
	case token.AND, token.OR:
		return p.parseJunction()
	case token.IDENT:
		return p.parseName()
	case token.NUMBER:
		p.next()
		return &ast.NumberLit{StartPos: t.Pos, Lit: t.Lit}
	case token.STRING:
		p.next()
		return &ast.StringLit{StartPos: t.Pos, Value: t.Lit}
	case token.TRUE:
		p.next()
		return &ast.BoolLit{StartPos: t.Pos, Value: true}
	case token.FALSE:
		p.next()
		return &ast.BoolLit{StartPos: t.Pos, Value: false}
	case token.BOOLEAN, token.STRINGKW:
		p.next()
		return &ast.Ident{StartPos: t.Pos, Name: t.Kind.String()}
	case token.AT:
		p.next()
		return &ast.At{StartPos: t.Pos}
	case token.LPAREN:
		p.next()
		x := p.parseExpr(0)
		p.expect(token.RPAREN)
		return &ast.Paren{StartPos: t.Pos, X: x}
	case token.LBRACE:
		return p.parseBrace()
	case token.LBRACK:
		return p.parseBracket()
	case token.LTUP:
		return p.parseTuple()
	case token.BOX, token.DIAMOND:
		p.next()
		op := "[]"
		if t.Kind == token.DIAMOND {
			op = "<>"
		}
		return &ast.Unary{StartPos: t.Pos, Op: op, X: p.parseExpr(token.PrefixOps[op].Hi)}
	case token.ENABLED, token.UNCHANGED, token.SUBSET, token.UNION, token.DOMAIN:
		p.next()
		op := t.Kind.String()
		return &ast.Unary{StartPos: t.Pos, Op: op, X: p.parseExpr(token.PrefixOps[op].Hi)}
	case token.OP:
		if info, ok := token.PrefixOps[t.Lit]; ok {
			p.next()
			return &ast.Unary{StartPos: t.Pos, Op: t.Lit, X: p.parseExpr(info.Hi)}
		}
		if _, ok := token.InfixOps[t.Lit]; ok {
			// A bare operator symbol: an operator passed as an argument.
			p.next()
			return &ast.OpRef{StartPos: t.Pos, Op: t.Lit}
		}
	case token.IF:
		p.next()
		cond := p.parseExpr(0)
		p.expect(token.THEN)
		then := p.parseExpr(0)
		p.expect(token.ELSE)
		return &ast.If{StartPos: t.Pos, Cond: cond, Then: then, Else: p.parseExpr(0)}
	case token.CASE:
		return p.parseCase()
	case token.LET:
		return p.parseLet()
	case token.CHOOSE:
		return p.parseChoose()
	case token.LAMBDA:
		p.next()
		var params []string
		for {
			params = append(params, p.expect(token.IDENT).Lit)
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
		p.expect(token.COLON)
		return &ast.Lambda{StartPos: t.Pos, Params: params, Body: p.parseExpr(0)}
	case token.FORALL, token.EXISTS:
		p.next()
		kind := ast.Forall
		if t.Kind == token.EXISTS {
			kind = ast.Exists
		}
		bounds := p.parseBounds(false)
		p.expect(token.COLON)
		return &ast.Quant{StartPos: t.Pos, Kind: kind, Bounds: bounds, Body: p.parseExpr(0)}
	case token.TFORALL, token.TEXISTS:
		p.next()
		kind := ast.TemporalForall
		if t.Kind == token.TEXISTS {
			kind = ast.TemporalExists
		}
		var names []string
		for {
			names = append(names, p.expect(token.IDENT).Lit)
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
		p.expect(token.COLON)
		return &ast.Quant{StartPos: t.Pos, Kind: kind,
			Bounds: []ast.Bound{{Names: names}}, Body: p.parseExpr(0)}
	case token.WFSUB, token.SFSUB:
		p.next()
		sub := p.parseSubscript()
		p.expect(token.LPAREN)
		x := p.parseExpr(0)
		p.expect(token.RPAREN)
		return &ast.Fairness{StartPos: t.Pos, Strong: t.Kind == token.SFSUB, Sub: sub, X: x}
	}
	p.errorf(t.Pos, "unexpected %s in expression", t)
	return &ast.Ident{StartPos: t.Pos, Name: "?"}
}

// parseJunction parses a vertically aligned /\ or \/ list. The current
// token is the first bullet.
func (p *parser) parseJunction() ast.Expr {
	bullet := p.cur()
	col := bullet.Pos.Col
	op := "/\\"
	if bullet.Kind == token.OR {
		op = "\\/"
	}
	j := &ast.Junction{StartPos: bullet.Pos, Op: op}
	p.jcol = append(p.jcol, col)
	for p.err == nil {
		p.next() // bullet
		j.Items = append(j.Items, p.parseExpr(0))
		t := p.cur()
		if t.Kind != bullet.Kind || t.Pos.Col != col {
			break
		}
	}
	p.jcol = p.jcol[:len(p.jcol)-1]
	return j
}

func (p *parser) parseName() ast.Expr {
	start := p.cur().Pos
	var prefix []ast.InstArm
	for {
		name := p.expect(token.IDENT).Lit
		var args []ast.Expr
		if p.cur().Kind == token.LPAREN {
			p.next()
			for {
				args = append(args, p.parseExpr(0))
				if p.cur().Kind != token.COMMA {
					break
				}
				p.next()
			}
			p.expect(token.RPAREN)
		}
		if p.cur().Kind == token.BANG && p.at(1).Kind == token.IDENT {
			p.next()
			prefix = append(prefix, ast.InstArm{Name: name, Args: args})
			continue
		}
		var fun ast.Expr
		if len(prefix) > 0 {
			fun = &ast.GeneralIdent{StartPos: start, Prefix: prefix, Name: name}
		} else {
			fun = &ast.Ident{StartPos: start, Name: name}
		}
		if len(args) > 0 {
			return &ast.Apply{Fun: fun, Args: args}
		}
		return fun
	}
}

// parseBrace parses {…}: enumeration, filter {x \in S : P}, or map
// {e : x \in S}.
func (p *parser) parseBrace() ast.Expr {
	start := p.expect(token.LBRACE).Pos
	if p.cur().Kind == token.RBRACE {
		p.next()
		return &ast.SetEnum{StartPos: start}
	}
	first := p.parseExpr(0)
	switch p.cur().Kind {
	case token.COLON:
		p.next()
		if b, ok := filterBound(first); ok && p.filterAhead() {
			pred := p.parseExpr(0)
			p.expect(token.RBRACE)
			return &ast.SetFilter{StartPos: start, Bound: b, Pred: pred}
		}
		bounds := p.parseBounds(true)
		p.expect(token.RBRACE)
		return &ast.SetMap{StartPos: start, Body: first, Bounds: bounds}
	case token.COMMA:
		elems := []ast.Expr{first}
		for p.cur().Kind == token.COMMA {
			p.next()
			elems = append(elems, p.parseExpr(0))
		}
		p.expect(token.RBRACE)
		return &ast.SetEnum{StartPos: start, Elems: elems}
	default:
		p.expect(token.RBRACE)
		return &ast.SetEnum{StartPos: start, Elems: []ast.Expr{first}}
	}
}

// filterBound converts an "x \in S" or "<<x, y>> \in S" expression into a
// set-filter bound.
func filterBound(e ast.Expr) (ast.Bound, bool) {
	b, ok := e.(*ast.Binary)
	if !ok || b.Op != "\\in" {
		return ast.Bound{}, false
	}
	switch l := b.L.(type) {
	case *ast.Ident:
		return ast.Bound{Names: []string{l.Name}, Set: b.R}, true
	case *ast.Tuple:
		var names []string
		for _, el := range l.Elems {
			id, ok := el.(*ast.Ident)
			if !ok {
				return ast.Bound{}, false
			}
			names = append(names, id.Name)
		}
		return ast.Bound{Names: names, Tuple: true, Set: b.R}, true
	}
	return ast.Bound{}, false
}

// filterAhead disambiguates {x \in S : P} from {e : x \in S, ...} after
// the colon: a set map's colon is followed by a bound list.
func (p *parser) filterAhead() bool {
	// A bound list begins IDENT (, IDENT)* \in — anything else means the
	// colon introduced a filter predicate.
	i := 0
	if p.at(i).Kind != token.IDENT {
		return true
	}
	for p.at(i).Kind == token.IDENT {
		i++
		if p.at(i).Kind == token.OP && p.at(i).Lit == "\\in" {
			return false // looks like a map's bound list
		}
		if p.at(i).Kind != token.COMMA {
			return true
		}
		i++
	}
	return true
}

// parseBracket parses every [...] form.
func (p *parser) parseBracket() ast.Expr {
	start := p.expect(token.LBRACK).Pos
	// Record constructor / record set: [name |-> e] / [name : S].
	if p.cur().Kind == token.IDENT {
		switch p.at(1).Kind {
		case token.MAPSTO:
			rec := &ast.RecordLit{StartPos: start}
			for {
				f := p.expect(token.IDENT).Lit
				p.expect(token.MAPSTO)
				rec.Fields = append(rec.Fields, ast.Field{Name: f, Expr: p.parseExpr(0)})
				if p.cur().Kind != token.COMMA {
					break
				}
				p.next()
			}
			p.expect(token.RBRACK)
			return rec
		case token.COLON:
			rs := &ast.RecordSet{StartPos: start}
			for {
				f := p.expect(token.IDENT).Lit
				p.expect(token.COLON)
				rs.Fields = append(rs.Fields, ast.Field{Name: f, Expr: p.parseExpr(0)})
				if p.cur().Kind != token.COMMA {
					break
				}
				p.next()
			}
			p.expect(token.RBRACK)
			return rs
		}
	}
	// Function constructor [x \in S |-> e]: bounds followed by |->.
	if save := p.i; p.boundsAhead() {
		bounds := p.parseBounds(true)
		if p.cur().Kind == token.MAPSTO {
			p.next()
			body := p.parseExpr(0)
			p.expect(token.RBRACK)
			return &ast.FuncLit{StartPos: start, Bounds: bounds, Body: body}
		}
		p.i = save
	}
	x := p.parseExpr(0)
	switch p.cur().Kind {
	case token.EXCEPT:
		p.next()
		ex := &ast.Except{StartPos: start, Fn: x}
		for {
			p.expect(token.BANG)
			var spec ast.ExceptSpec
			for {
				if p.cur().Kind == token.DOT {
					p.next()
					spec.Path = append(spec.Path, ast.ExceptPath{Field: p.expect(token.IDENT).Lit})
					continue
				}
				if p.cur().Kind == token.LBRACK {
					p.next()
					var idx []ast.Expr
					for {
						idx = append(idx, p.parseExpr(0))
						if p.cur().Kind != token.COMMA {
							break
						}
						p.next()
					}
					p.expect(token.RBRACK)
					spec.Path = append(spec.Path, ast.ExceptPath{Index: idx})
					continue
				}
				break
			}
			if len(spec.Path) == 0 {
				p.errorf(p.cur().Pos, "expected .field or [index] after ! in EXCEPT")
				return ex
			}
			if p.cur().Kind == token.OP && p.cur().Lit == "=" {
				p.next()
			} else {
				p.errorf(p.cur().Pos, "expected = in EXCEPT clause, found %s", p.cur())
				return ex
			}
			spec.Value = p.parseExpr(0)
			ex.Specs = append(ex.Specs, spec)
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
		p.expect(token.RBRACK)
		return ex
	case token.ARROW:
		p.next()
		rng := p.parseExpr(0)
		p.expect(token.RBRACK)
		return &ast.FuncSet{StartPos: start, Domain: x, Range: rng}
	case token.RBRACKSUB:
		p.next()
		return &ast.SquareAct{StartPos: start, X: x, Sub: p.parseSubscript()}
	case token.RBRACK:
		p.errorf(p.cur().Pos, "[expr] must be followed by _subscript, or use |->, ->, :, or EXCEPT inside the brackets")
		return x
	}
	p.errorf(p.cur().Pos, "unexpected %s in [...] expression", p.cur())
	return x
}

// boundsAhead reports whether the cursor starts a bound list
// (x, y \in S or <<x, y>> \in S).
func (p *parser) boundsAhead() bool {
	i := 0
	if p.at(i).Kind == token.LTUP {
		i++
		for p.at(i).Kind == token.IDENT {
			i++
			if p.at(i).Kind == token.COMMA {
				i++
				continue
			}
			break
		}
		if p.at(i).Kind != token.RTUP {
			return false
		}
		i++
		return p.at(i).Kind == token.OP && p.at(i).Lit == "\\in"
	}
	for p.at(i).Kind == token.IDENT {
		i++
		if p.at(i).Kind == token.OP && p.at(i).Lit == "\\in" {
			return true
		}
		if p.at(i).Kind != token.COMMA {
			return false
		}
		i++
	}
	return false
}

func (p *parser) parseTuple() ast.Expr {
	start := p.expect(token.LTUP).Pos
	if p.cur().Kind == token.RTUP {
		p.next()
		return &ast.Tuple{StartPos: start}
	}
	var elems []ast.Expr
	for {
		elems = append(elems, p.parseExpr(0))
		if p.cur().Kind != token.COMMA {
			break
		}
		p.next()
	}
	switch p.cur().Kind {
	case token.RTUPSUB:
		p.next()
		if len(elems) != 1 {
			p.errorf(p.cur().Pos, "<<A>>_v takes a single action")
		}
		return &ast.AngleAct{StartPos: start, X: elems[0], Sub: p.parseSubscript()}
	default:
		p.expect(token.RTUP)
		return &ast.Tuple{StartPos: start, Elems: elems}
	}
}

func (p *parser) parseCase() ast.Expr {
	start := p.expect(token.CASE).Pos
	c := &ast.Case{StartPos: start}
	for p.err == nil {
		if p.cur().Kind == token.OTHER {
			p.next()
			p.expect(token.ARROW)
			c.Other = p.parseExpr(0)
			break
		}
		cond := p.parseExpr(0)
		p.expect(token.ARROW)
		val := p.parseExpr(0)
		c.Arms = append(c.Arms, ast.CaseArm{Cond: cond, Value: val})
		if p.cur().Kind != token.BOX || p.blocked() {
			break
		}
		p.next()
	}
	return c
}

func (p *parser) parseLet() ast.Expr {
	start := p.expect(token.LET).Pos
	l := &ast.Let{StartPos: start}
	for p.err == nil && p.cur().Kind != token.IN {
		var d ast.Unit
		switch {
		case p.cur().Kind == token.IDENT:
			d = p.parseDefinition(false, p.cur().Pos)
		case p.cur().Kind == token.RECURSIVE:
			pos := p.next().Pos
			d = &ast.Recursive{StartPos: pos, Decls: p.parseOpDeclList()}
		case p.cur().Kind == token.OP && isDefinablePrefixOp(p.cur().Lit):
			d = p.parsePrefixOpDef(false, p.cur().Pos)
		default:
			p.errorf(p.cur().Pos, "expected definition or IN in LET, found %s", p.cur())
			return l
		}
		if d != nil {
			l.Defs = append(l.Defs, d)
		}
	}
	p.expect(token.IN)
	l.Body = p.parseExpr(0)
	return l
}

func (p *parser) parseChoose() ast.Expr {
	start := p.expect(token.CHOOSE).Pos
	c := &ast.Choose{StartPos: start}
	if p.cur().Kind == token.LTUP {
		p.next()
		for {
			c.Tuple = append(c.Tuple, p.expect(token.IDENT).Lit)
			if p.cur().Kind != token.COMMA {
				break
			}
			p.next()
		}
		p.expect(token.RTUP)
	} else {
		c.Var = p.expect(token.IDENT).Lit
	}
	if p.cur().Kind == token.OP && p.cur().Lit == "\\in" {
		p.next()
		c.Set = p.parseExpr(0)
	}
	p.expect(token.COLON)
	c.Body = p.parseExpr(0)
	return c
}

// parseBounds parses quantifier/constructor bounds. If requireSet is true
// every group must have "\in S".
func (p *parser) parseBounds(requireSet bool) []ast.Bound {
	var bounds []ast.Bound
	for {
		var b ast.Bound
		if p.cur().Kind == token.LTUP {
			p.next()
			b.Tuple = true
			for {
				b.Names = append(b.Names, p.expect(token.IDENT).Lit)
				if p.cur().Kind != token.COMMA {
					break
				}
				p.next()
			}
			p.expect(token.RTUP)
		} else {
			// Before \in is seen, ", IDENT" always extends the current
			// group: "x, y \in S" is one group, and in "x \in S, y \in T"
			// the second group starts after this group's \in.
			for {
				b.Names = append(b.Names, p.expect(token.IDENT).Lit)
				if p.cur().Kind != token.COMMA || p.at(1).Kind != token.IDENT {
					break
				}
				p.next()
			}
		}
		if p.cur().Kind == token.OP && p.cur().Lit == "\\in" {
			p.next()
			b.Set = p.parseExpr(token.InfixOps["\\in"].Hi)
		} else if requireSet || b.Tuple {
			p.errorf(p.cur().Pos, "expected \\in in bound, found %s", p.cur())
			return bounds
		}
		bounds = append(bounds, b)
		if p.cur().Kind != token.COMMA {
			return bounds
		}
		p.next()
	}
}

// parseSubscript parses the _v subscript of [A]_v, <<A>>_v, WF_v, SF_v.
// Subscripts are deliberately tight: an identifier (with .field
// selections), a tuple, or a parenthesized expression. In particular a (
// after an identifier is NOT treated as operator application, so that
// WF_vars(Next) reads vars as the subscript and (Next) as the action.
func (p *parser) parseSubscript() ast.Expr {
	t := p.cur()
	var x ast.Expr
	switch t.Kind {
	case token.IDENT:
		p.next()
		x = &ast.Ident{StartPos: t.Pos, Name: t.Lit}
		for p.cur().Kind == token.DOT {
			p.next()
			x = &ast.Dot{X: x, Field: p.expect(token.IDENT).Lit}
		}
	case token.LTUP:
		x = p.parseTuple()
	case token.LPAREN:
		p.next()
		inner := p.parseExpr(0)
		p.expect(token.RPAREN)
		x = &ast.Paren{StartPos: t.Pos, X: inner}
	default:
		p.errorf(t.Pos, "expected subscript (identifier, tuple, or parenthesized expression), found %s", t)
		x = &ast.Ident{StartPos: t.Pos, Name: "?"}
	}
	return x
}

// ParseValueExpr parses src as an expression and requires the entire
// input to be consumed; it is used for parsing TLC value output.
func ParseValueExpr(src string) (ast.Expr, error) {
	return ParseExpr(strings.TrimSpace(src))
}
