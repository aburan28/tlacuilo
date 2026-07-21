// Package value models TLA+ values: booleans, (big) integers, strings,
// model values, sets, tuples/sequences, records, explicit functions, and
// integer intervals.
//
// Values can be parsed from TLA+ literal syntax — including the syntax
// TLC uses to print counterexample states, such as
// (1 :> "a" @@ 2 :> "b") — rendered back to TLA+ source, and converted to
// and from the ITF (Informal Trace Format) JSON encoding used by
// Apalache, Quint, and TLC.
package value

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/aburan28/tlacuilo/ast"
	"github.com/aburan28/tlacuilo/parser"
)

// Value is a TLA+ value.
type Value interface {
	fmt.Stringer
	kind() int
}

// Bool is TRUE or FALSE.
type Bool bool

// Int is an integer of arbitrary size.
type Int struct{ X *big.Int }

// String is a TLA+ string.
type String string

// ModelValue is a TLC model value: an identifier equal only to itself.
type ModelValue string

// Set is a finite set. Elements are kept in canonical (sorted, deduped)
// order.
type Set struct{ Elems []Value }

// Tuple is a tuple; TLA+ sequences are tuples.
type Tuple struct{ Elems []Value }

// RecField is one field of a Record.
type RecField struct {
	Name string
	V    Value
}

// Record is a record with fields in canonical (name-sorted) order.
type Record struct{ Fields []RecField }

// FuncEntry is one maplet of a Func.
type FuncEntry struct {
	K, V Value
}

// Func is an explicit function (d1 :> v1 @@ d2 :> v2), entries in
// canonical key order.
type Func struct{ Entries []FuncEntry }

// Interval is the integer range Lo..Hi.
type Interval struct{ Lo, Hi *big.Int }

func (Bool) kind() int       { return 0 }
func (Int) kind() int        { return 1 }
func (String) kind() int     { return 2 }
func (ModelValue) kind() int { return 3 }
func (Interval) kind() int   { return 4 }
func (Set) kind() int        { return 5 }
func (Tuple) kind() int      { return 6 }
func (Record) kind() int     { return 7 }
func (Func) kind() int       { return 8 }

// NewInt returns an Int for a small integer.
func NewInt(v int64) Int { return Int{big.NewInt(v)} }

// NewSet returns a Set with canonicalized (sorted, deduplicated) elements.
func NewSet(elems ...Value) Set {
	s := append([]Value(nil), elems...)
	sort.SliceStable(s, func(i, j int) bool { return Compare(s[i], s[j]) < 0 })
	out := s[:0]
	for i, v := range s {
		if i == 0 || Compare(s[i-1], v) != 0 {
			out = append(out, v)
		}
	}
	return Set{Elems: out}
}

// NewRecord returns a Record with name-sorted fields.
func NewRecord(fields ...RecField) Record {
	fs := append([]RecField(nil), fields...)
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Name < fs[j].Name })
	return Record{Fields: fs}
}

// NewFunc returns a Func with key-sorted entries.
func NewFunc(entries ...FuncEntry) Func {
	es := append([]FuncEntry(nil), entries...)
	sort.SliceStable(es, func(i, j int) bool { return Compare(es[i].K, es[j].K) < 0 })
	return Func{Entries: es}
}

// Compare imposes a deterministic total order on values: by kind, then by
// content. It is used for canonical set and function ordering.
func Compare(a, b Value) int {
	if ka, kb := a.kind(), b.kind(); ka != kb {
		return ka - kb
	}
	switch a := a.(type) {
	case Bool:
		bb := b.(Bool)
		switch {
		case a == bb:
			return 0
		case !bool(a):
			return -1
		default:
			return 1
		}
	case Int:
		return a.X.Cmp(b.(Int).X)
	case String:
		return strings.Compare(string(a), string(b.(String)))
	case ModelValue:
		return strings.Compare(string(a), string(b.(ModelValue)))
	case Interval:
		bb := b.(Interval)
		if c := a.Lo.Cmp(bb.Lo); c != 0 {
			return c
		}
		return a.Hi.Cmp(bb.Hi)
	case Set:
		return compareSlices(a.Elems, b.(Set).Elems)
	case Tuple:
		return compareSlices(a.Elems, b.(Tuple).Elems)
	case Record:
		bb := b.(Record)
		for i := 0; i < len(a.Fields) && i < len(bb.Fields); i++ {
			if c := strings.Compare(a.Fields[i].Name, bb.Fields[i].Name); c != 0 {
				return c
			}
			if c := Compare(a.Fields[i].V, bb.Fields[i].V); c != 0 {
				return c
			}
		}
		return len(a.Fields) - len(bb.Fields)
	case Func:
		bb := b.(Func)
		for i := 0; i < len(a.Entries) && i < len(bb.Entries); i++ {
			if c := Compare(a.Entries[i].K, bb.Entries[i].K); c != 0 {
				return c
			}
			if c := Compare(a.Entries[i].V, bb.Entries[i].V); c != 0 {
				return c
			}
		}
		return len(a.Entries) - len(bb.Entries)
	}
	return 0
}

func compareSlices(a, b []Value) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

// Equal reports deep equality of two values.
func Equal(a, b Value) bool { return Compare(a, b) == 0 }

func (v Bool) String() string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}

func (v Int) String() string { return v.X.String() }

func (v String) String() string {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\t", `\t`, "\n", `\n`, "\f", `\f`, "\r", `\r`)
	return `"` + q.Replace(string(v)) + `"`
}

func (v ModelValue) String() string { return string(v) }

func (v Set) String() string {
	parts := make([]string, len(v.Elems))
	for i, e := range v.Elems {
		parts[i] = e.String()
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (v Tuple) String() string {
	parts := make([]string, len(v.Elems))
	for i, e := range v.Elems {
		parts[i] = e.String()
	}
	return "<<" + strings.Join(parts, ", ") + ">>"
}

func (v Record) String() string {
	if len(v.Fields) == 0 {
		return "[]" // not valid TLA+ input, but unambiguous output
	}
	parts := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		parts[i] = f.Name + " |-> " + f.V.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (v Func) String() string {
	if len(v.Entries) == 0 {
		return "<<>>"
	}
	parts := make([]string, len(v.Entries))
	for i, e := range v.Entries {
		parts[i] = e.K.String() + " :> " + e.V.String()
	}
	return "(" + strings.Join(parts, " @@ ") + ")"
}

func (v Interval) String() string { return v.Lo.String() + ".." + v.Hi.String() }

// Parse parses TLA+ literal-value syntax (as printed by TLC in traces).
func Parse(src string) (Value, error) {
	e, err := parser.ParseValueExpr(src)
	if err != nil {
		return nil, err
	}
	return FromExpr(e)
}

// FromExpr converts a literal expression into a Value. Identifiers become
// model values. Non-literal expressions yield an error.
func FromExpr(e ast.Expr) (Value, error) {
	switch e := e.(type) {
	case *ast.Paren:
		return FromExpr(e.X)
	case *ast.BoolLit:
		return Bool(e.Value), nil
	case *ast.StringLit:
		return String(e.Value), nil
	case *ast.NumberLit:
		return parseNumber(e.Lit)
	case *ast.Ident:
		return ModelValue(e.Name), nil
	case *ast.Unary:
		if e.Op == "-" {
			v, err := FromExpr(e.X)
			if err != nil {
				return nil, err
			}
			i, ok := v.(Int)
			if !ok {
				return nil, fmt.Errorf("unary - applied to non-integer value %s", v)
			}
			return Int{new(big.Int).Neg(i.X)}, nil
		}
	case *ast.SetEnum:
		elems := make([]Value, len(e.Elems))
		for i, el := range e.Elems {
			v, err := FromExpr(el)
			if err != nil {
				return nil, err
			}
			elems[i] = v
		}
		return NewSet(elems...), nil
	case *ast.Tuple:
		elems := make([]Value, len(e.Elems))
		for i, el := range e.Elems {
			v, err := FromExpr(el)
			if err != nil {
				return nil, err
			}
			elems[i] = v
		}
		return Tuple{Elems: elems}, nil
	case *ast.RecordLit:
		fields := make([]RecField, len(e.Fields))
		for i, f := range e.Fields {
			v, err := FromExpr(f.Expr)
			if err != nil {
				return nil, err
			}
			fields[i] = RecField{Name: f.Name, V: v}
		}
		return NewRecord(fields...), nil
	case *ast.Binary:
		switch e.Op {
		case "..":
			lo, err := FromExpr(e.L)
			if err != nil {
				return nil, err
			}
			hi, err := FromExpr(e.R)
			if err != nil {
				return nil, err
			}
			li, ok1 := lo.(Int)
			hi2, ok2 := hi.(Int)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("interval bounds must be integers: %s..%s", lo, hi)
			}
			return Interval{Lo: li.X, Hi: hi2.X}, nil
		case ":>":
			k, err := FromExpr(e.L)
			if err != nil {
				return nil, err
			}
			v, err := FromExpr(e.R)
			if err != nil {
				return nil, err
			}
			return Func{Entries: []FuncEntry{{K: k, V: v}}}, nil
		case "@@":
			l, err := FromExpr(e.L)
			if err != nil {
				return nil, err
			}
			r, err := FromExpr(e.R)
			if err != nil {
				return nil, err
			}
			lf, ok1 := l.(Func)
			rf, ok2 := r.(Func)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("@@ requires function values")
			}
			return NewFunc(append(lf.Entries, rf.Entries...)...), nil
		}
	}
	return nil, fmt.Errorf("expression %s is not a literal value", ast.ExprString(e))
}

func parseNumber(lit string) (Value, error) {
	base := 10
	digits := lit
	if strings.HasPrefix(lit, "\\") && len(lit) > 2 {
		switch lit[1] {
		case 'b', 'B':
			base, digits = 2, lit[2:]
		case 'o', 'O':
			base, digits = 8, lit[2:]
		case 'h', 'H':
			base, digits = 16, lit[2:]
		}
	}
	if strings.Contains(digits, ".") {
		return nil, fmt.Errorf("real number literals are not supported as values: %s", lit)
	}
	x, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return nil, fmt.Errorf("invalid number literal %q", lit)
	}
	return Int{x}, nil
}
