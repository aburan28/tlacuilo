// Package cfg generates and parses TLC model-checker configuration
// (.cfg) files.
package cfg

import (
	"fmt"
	"strings"

	"github.com/aburan28/tlacuilo/scanner"
	"github.com/aburan28/tlacuilo/token"
)

// Constant is one CONSTANT entry: either an assignment Name = Value
// (Value holds TLA+ constant-value text such as `3`, `"s"`, `mv`, or
// `{m1, m2}`) or an operator replacement Name <- Replacement.
type Constant struct {
	Name        string
	Value       string
	Replacement string
}

// Config describes a TLC run configuration.
type Config struct {
	// Specification names the behavior spec formula. Mutually exclusive
	// with Init/Next.
	Specification string
	Init          string
	Next          string

	Invariants []string
	Properties []string
	Constants  []Constant

	Symmetry          string
	View              string
	Constraints       []string
	ActionConstraints []string

	// CheckDeadlock emits CHECK_DEADLOCK TRUE/FALSE when non-nil.
	CheckDeadlock *bool

	Alias         string
	PostCondition string
}

// Validate reports configurations TLC would reject.
func (c *Config) Validate() error {
	if c.Specification != "" && (c.Init != "" || c.Next != "") {
		return fmt.Errorf("cfg: SPECIFICATION and INIT/NEXT are mutually exclusive")
	}
	if c.Specification == "" && (c.Init == "") != (c.Next == "") {
		return fmt.Errorf("cfg: INIT and NEXT must be given together")
	}
	return nil
}

// Format renders the configuration as .cfg file text.
func (c *Config) Format() string {
	var b strings.Builder
	section := func(kw string, vals ...string) {
		if len(vals) == 1 {
			fmt.Fprintf(&b, "%s %s\n", kw, vals[0])
			return
		}
		b.WriteString(kw + "\n")
		for _, v := range vals {
			b.WriteString("    " + v + "\n")
		}
	}
	if c.Specification != "" {
		section("SPECIFICATION", c.Specification)
	}
	if c.Init != "" {
		section("INIT", c.Init)
	}
	if c.Next != "" {
		section("NEXT", c.Next)
	}
	if len(c.Constants) > 0 {
		b.WriteString("CONSTANTS\n")
		for _, k := range c.Constants {
			if k.Replacement != "" {
				fmt.Fprintf(&b, "    %s <- %s\n", k.Name, k.Replacement)
			} else {
				fmt.Fprintf(&b, "    %s = %s\n", k.Name, k.Value)
			}
		}
	}
	for _, inv := range c.Invariants {
		section("INVARIANT", inv)
	}
	for _, p := range c.Properties {
		section("PROPERTY", p)
	}
	for _, con := range c.Constraints {
		section("CONSTRAINT", con)
	}
	for _, con := range c.ActionConstraints {
		section("ACTION_CONSTRAINT", con)
	}
	if c.Symmetry != "" {
		section("SYMMETRY", c.Symmetry)
	}
	if c.View != "" {
		section("VIEW", c.View)
	}
	if c.CheckDeadlock != nil {
		v := "FALSE"
		if *c.CheckDeadlock {
			v = "TRUE"
		}
		section("CHECK_DEADLOCK", v)
	}
	if c.Alias != "" {
		section("ALIAS", c.Alias)
	}
	if c.PostCondition != "" {
		section("POSTCONDITION", c.PostCondition)
	}
	return b.String()
}

// section keywords understood by Parse. Values are canonical names.
var sectionKeywords = map[string]string{
	"SPECIFICATION":      "SPECIFICATION",
	"INIT":               "INIT",
	"NEXT":               "NEXT",
	"INVARIANT":          "INVARIANT",
	"INVARIANTS":         "INVARIANT",
	"PROPERTY":           "PROPERTY",
	"PROPERTIES":         "PROPERTY",
	"CONSTANT":           "CONSTANT",
	"CONSTANTS":          "CONSTANT",
	"SYMMETRY":           "SYMMETRY",
	"VIEW":               "VIEW",
	"CONSTRAINT":         "CONSTRAINT",
	"CONSTRAINTS":        "CONSTRAINT",
	"ACTION_CONSTRAINT":  "ACTION_CONSTRAINT",
	"ACTION_CONSTRAINTS": "ACTION_CONSTRAINT",
	"CHECK_DEADLOCK":     "CHECK_DEADLOCK",
	"ALIAS":              "ALIAS",
	"POSTCONDITION":      "POSTCONDITION",
}

// Parse reads .cfg file text.
func Parse(src string) (*Config, error) {
	sc := scanner.New(src)
	toks := sc.ScanAll()
	if errs := sc.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("cfg: %v", errs[0])
	}
	c := &Config{}
	i := 0
	cur := func() token.Token { return toks[i] }
	next := func() token.Token { t := toks[i]; i++; return t }
	name := func(t token.Token) string {
		if t.Lit != "" {
			return t.Lit
		}
		return t.Kind.String()
	}
	isSection := func(t token.Token) (string, bool) {
		if t.Kind != token.IDENT && t.Kind != token.CONSTANT && t.Kind != token.CONSTANTS {
			return "", false
		}
		s, ok := sectionKeywords[name(t)]
		return s, ok
	}
	section := ""
	for cur().Kind != token.EOF {
		if s, ok := isSection(cur()); ok {
			// CONSTANT names can shadow keywords only via position; a
			// section keyword directly after CONSTANT would be a
			// constant named like a keyword, which TLC does not allow —
			// so treating every keyword as a section switch is safe.
			section = s
			next()
			continue
		}
		t := cur()
		switch section {
		case "SPECIFICATION":
			c.Specification = name(next())
		case "INIT":
			c.Init = name(next())
		case "NEXT":
			c.Next = name(next())
		case "INVARIANT":
			c.Invariants = append(c.Invariants, name(next()))
		case "PROPERTY":
			c.Properties = append(c.Properties, name(next()))
		case "SYMMETRY":
			c.Symmetry = name(next())
		case "VIEW":
			c.View = name(next())
		case "CONSTRAINT":
			c.Constraints = append(c.Constraints, name(next()))
		case "ACTION_CONSTRAINT":
			c.ActionConstraints = append(c.ActionConstraints, name(next()))
		case "ALIAS":
			c.Alias = name(next())
		case "POSTCONDITION":
			c.PostCondition = name(next())
		case "CHECK_DEADLOCK":
			v := next().Kind == token.TRUE
			c.CheckDeadlock = &v
		case "CONSTANT":
			if t.Kind != token.IDENT {
				return nil, fmt.Errorf("cfg: %s: expected constant name, found %s", t.Pos, t)
			}
			cname := name(next())
			switch cur().Kind {
			case token.LARROW:
				next()
				r := cur()
				if r.Kind != token.IDENT {
					return nil, fmt.Errorf("cfg: %s: expected operator name after <-", r.Pos)
				}
				next()
				c.Constants = append(c.Constants, Constant{Name: cname, Replacement: name(r)})
			case token.OP:
				if cur().Lit != "=" {
					return nil, fmt.Errorf("cfg: %s: expected = or <- after %s", cur().Pos, cname)
				}
				next()
				val, err := scanConstantValue(src, toks, &i)
				if err != nil {
					return nil, err
				}
				c.Constants = append(c.Constants, Constant{Name: cname, Value: val})
			default:
				return nil, fmt.Errorf("cfg: %s: expected = or <- after %s, found %s", cur().Pos, cname, cur())
			}
		default:
			return nil, fmt.Errorf("cfg: %s: unexpected %s outside any section", t.Pos, t)
		}
	}
	return c, nil
}

// scanConstantValue consumes the balanced token run forming a constant
// value and returns the corresponding source text. The value ends when,
// at bracket depth zero, the next token is a section keyword or an
// identifier followed by = or <-.
func scanConstantValue(src string, toks []token.Token, i *int) (string, error) {
	start := toks[*i].Pos.Offset
	end := start
	depth := 0
	first := true
	for {
		t := toks[*i]
		if t.Kind == token.EOF {
			break
		}
		if depth == 0 && !first {
			if _, ok := sectionKeywords[t.Lit]; ok && t.Kind == token.IDENT {
				break
			}
			if t.Kind == token.CONSTANT || t.Kind == token.CONSTANTS {
				break
			}
			if t.Kind == token.IDENT && *i+1 < len(toks) {
				nt := toks[*i+1]
				if nt.Kind == token.LARROW || (nt.Kind == token.OP && nt.Lit == "=") {
					break
				}
			}
		}
		switch t.Kind {
		case token.LBRACE, token.LBRACK, token.LPAREN, token.LTUP:
			depth++
		case token.RBRACE, token.RBRACK, token.RPAREN, token.RTUP:
			depth--
		}
		end = t.Pos.Offset + tokenByteLen(src, t)
		*i++
		first = false
		if depth == 0 && closesValue(t) && valueDone(toks, *i) {
			break
		}
	}
	if end <= start {
		return "", fmt.Errorf("cfg: empty constant value at %s", toks[*i].Pos)
	}
	return strings.TrimSpace(src[start:end]), nil
}

// closesValue reports whether t can end a constant value.
func closesValue(t token.Token) bool {
	switch t.Kind {
	case token.IDENT, token.NUMBER, token.STRING, token.TRUE, token.FALSE,
		token.RBRACE, token.RBRACK, token.RPAREN, token.RTUP:
		return true
	}
	return false
}

// valueDone reports whether the token at index i starts new content
// rather than continuing the current value (e.g. an infix .. or comma).
func valueDone(toks []token.Token, i int) bool {
	if i >= len(toks) {
		return true
	}
	switch toks[i].Kind {
	case token.OP, token.COMMA, token.DOT:
		return false
	}
	return true
}

func tokenByteLen(src string, t token.Token) int {
	switch t.Kind {
	case token.STRING:
		// The literal is decoded; measure from source: find closing quote.
		j := t.Pos.Offset + 1
		for j < len(src) {
			if src[j] == '\\' {
				j += 2
				continue
			}
			if src[j] == '"' {
				return j - t.Pos.Offset + 1
			}
			j++
		}
		return len(src) - t.Pos.Offset
	case token.IDENT, token.NUMBER, token.OP, token.COMMENT:
		return len(t.Lit)
	default:
		return len(t.Kind.String())
	}
}
