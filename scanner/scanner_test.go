package scanner

import (
	"testing"

	"github.com/aburan28/tlacuilo/token"
)

type tok struct {
	kind token.Kind
	lit  string
}

func scanKinds(t *testing.T, src string) []token.Token {
	t.Helper()
	s := New(src)
	toks := s.ScanAll()
	for _, e := range s.Errors() {
		t.Errorf("scan error: %v", e)
	}
	return toks[:len(toks)-1] // drop EOF
}

func expect(t *testing.T, src string, want []tok) {
	t.Helper()
	got := scanKinds(t, src)
	if len(got) != len(want) {
		t.Fatalf("%q: got %d tokens %v, want %d", src, len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].Kind != w.kind {
			t.Errorf("%q token %d: got kind %v (%q), want %v", src, i, got[i].Kind, got[i].Lit, w.kind)
		}
		if w.lit != "" && got[i].Lit != w.lit {
			t.Errorf("%q token %d: got lit %q, want %q", src, i, got[i].Lit, w.lit)
		}
	}
}

func TestBasicTokens(t *testing.T) {
	expect(t, `---- MODULE Foo ----`, []tok{
		{token.DASHES, ""}, {token.MODULE, ""}, {token.IDENT, "Foo"}, {token.DASHES, ""},
	})
	expect(t, `x' = x + 1`, []tok{
		{token.IDENT, "x"}, {token.PRIME, ""}, {token.OP, "="},
		{token.IDENT, "x"}, {token.OP, "+"}, {token.NUMBER, "1"},
	})
	expect(t, `Init == /\ x = 0`, []tok{
		{token.IDENT, "Init"}, {token.DEFEQ, ""}, {token.AND, ""},
		{token.IDENT, "x"}, {token.OP, "="}, {token.NUMBER, "0"},
	})
	expect(t, `====`, []tok{{token.MODEND, ""}})
}

func TestOperators(t *testing.T) {
	expect(t, `a => b <=> c ~> d -+-> e`, []tok{
		{token.IDENT, "a"}, {token.OP, "=>"}, {token.IDENT, "b"},
		{token.OP, "<=>"}, {token.IDENT, "c"}, {token.OP, "~>"},
		{token.IDENT, "d"}, {token.OP, "-+->"}, {token.IDENT, "e"},
	})
	expect(t, `S \cup T \cap U \ V`, []tok{
		{token.IDENT, "S"}, {token.OP, "\\cup"}, {token.IDENT, "T"},
		{token.OP, "\\cap"}, {token.IDENT, "U"}, {token.OP, "\\"}, {token.IDENT, "V"},
	})
	// Aliases canonicalize.
	expect(t, `a \land b \lor c # d =< e \union f`, []tok{
		{token.IDENT, "a"}, {token.AND, ""}, {token.IDENT, "b"},
		{token.OR, ""}, {token.IDENT, "c"}, {token.OP, "/="},
		{token.IDENT, "d"}, {token.OP, "<="}, {token.IDENT, "e"},
		{token.OP, "\\cup"}, {token.IDENT, "f"},
	})
	expect(t, `x \in S \notin T \subseteq U`, []tok{
		{token.IDENT, "x"}, {token.OP, "\\in"}, {token.IDENT, "S"},
		{token.OP, "\\notin"}, {token.IDENT, "T"},
		{token.OP, "\\subseteq"}, {token.IDENT, "U"},
	})
	expect(t, `f @@ g :> h <: i`, []tok{
		{token.IDENT, "f"}, {token.OP, "@@"}, {token.IDENT, "g"},
		{token.OP, ":>"}, {token.IDENT, "h"}, {token.OP, "<:"}, {token.IDENT, "i"},
	})
	expect(t, `(+) (-) (.) (/) (\X)`, []tok{
		{token.OP, "\\oplus"}, {token.OP, "\\ominus"}, {token.OP, "\\odot"},
		{token.OP, "\\oslash"}, {token.OP, "\\otimes"},
	})
	expect(t, `a^+ b^* c^# d^e`, []tok{
		{token.IDENT, "a"}, {token.OP, "^+"}, {token.IDENT, "b"}, {token.OP, "^*"},
		{token.IDENT, "c"}, {token.OP, "^#"}, {token.IDENT, "d"}, {token.OP, "^"}, {token.IDENT, "e"},
	})
	expect(t, `1..5 s \o t`, []tok{
		{token.NUMBER, "1"}, {token.OP, ".."}, {token.NUMBER, "5"},
		{token.IDENT, "s"}, {token.OP, "\\o"}, {token.IDENT, "t"},
	})
}

func TestConstructTokens(t *testing.T) {
	expect(t, `<<1, 2>>`, []tok{
		{token.LTUP, ""}, {token.NUMBER, "1"}, {token.COMMA, ""},
		{token.NUMBER, "2"}, {token.RTUP, ""},
	})
	expect(t, `[][Next]_vars`, []tok{
		{token.BOX, ""}, {token.LBRACK, ""}, {token.IDENT, "Next"},
		{token.RBRACKSUB, ""}, {token.IDENT, "vars"},
	})
	expect(t, `<><<A>>_vars`, []tok{
		{token.DIAMOND, ""}, {token.LTUP, ""}, {token.IDENT, "A"},
		{token.RTUPSUB, ""}, {token.IDENT, "vars"},
	})
	expect(t, `WF_vars(Next) /\ SF_x(A)`, []tok{
		{token.WFSUB, ""}, {token.IDENT, "vars"}, {token.LPAREN, ""},
		{token.IDENT, "Next"}, {token.RPAREN, ""}, {token.AND, ""},
		{token.SFSUB, ""}, {token.IDENT, "x"}, {token.LPAREN, ""},
		{token.IDENT, "A"}, {token.RPAREN, ""},
	})
	expect(t, `[a |-> 1, b |-> 2].a`, []tok{
		{token.LBRACK, ""}, {token.IDENT, "a"}, {token.MAPSTO, ""}, {token.NUMBER, "1"},
		{token.COMMA, ""}, {token.IDENT, "b"}, {token.MAPSTO, ""}, {token.NUMBER, "2"},
		{token.RBRACK, ""}, {token.DOT, ""}, {token.IDENT, "a"},
	})
	expect(t, `\A x \in S : \E y : TRUE`, []tok{
		{token.FORALL, ""}, {token.IDENT, "x"}, {token.OP, "\\in"}, {token.IDENT, "S"},
		{token.COLON, ""}, {token.EXISTS, ""}, {token.IDENT, "y"},
		{token.COLON, ""}, {token.TRUE, ""},
	})
	expect(t, `S \X T \times U`, []tok{
		{token.IDENT, "S"}, {token.TIMES, ""}, {token.IDENT, "T"},
		{token.TIMES, ""}, {token.IDENT, "U"},
	})
	expect(t, `F(_, _)`, []tok{
		{token.IDENT, "F"}, {token.LPAREN, ""}, {token.UNDERSCORE, ""},
		{token.COMMA, ""}, {token.UNDERSCORE, ""}, {token.RPAREN, ""},
	})
	expect(t, `[f EXCEPT ![1] = @ + 1]`, []tok{
		{token.LBRACK, ""}, {token.IDENT, "f"}, {token.EXCEPT, ""}, {token.BANG, ""},
		{token.LBRACK, ""}, {token.NUMBER, "1"}, {token.RBRACK, ""}, {token.OP, "="},
		{token.AT, ""}, {token.OP, "+"}, {token.NUMBER, "1"},
		{token.RBRACK, ""},
	})
}

func TestNumbersAndStrings(t *testing.T) {
	expect(t, `42 3.14 \b0101 \o777 \hFF \HAB`, []tok{
		{token.NUMBER, "42"}, {token.NUMBER, "3.14"}, {token.NUMBER, "\\b0101"},
		{token.NUMBER, "\\o777"}, {token.NUMBER, "\\hFF"}, {token.NUMBER, "\\HAB"},
	})
	expect(t, `"hello" "a\"b" "t\tn\n"`, []tok{
		{token.STRING, "hello"}, {token.STRING, `a"b`}, {token.STRING, "t\tn\n"},
	})
	// \o followed by non-octal is the composition operator.
	expect(t, `s \o t8`, []tok{
		{token.IDENT, "s"}, {token.OP, "\\o"}, {token.IDENT, "t8"},
	})
}

func TestComments(t *testing.T) {
	expect(t, "x \\* line comment\ny", []tok{
		{token.IDENT, "x"}, {token.IDENT, "y"},
	})
	expect(t, "a (* block (* nested *) still *) b", []tok{
		{token.IDENT, "a"}, {token.IDENT, "b"},
	})
}

func TestPositions(t *testing.T) {
	s := New("ab cd\n  ef")
	t1 := s.Scan()
	t2 := s.Scan()
	t3 := s.Scan()
	at := func(tk token.Token, line, col, off int) bool {
		return tk.Pos.Line == line && tk.Pos.Col == col && tk.Pos.Offset == off
	}
	if !at(t1, 1, 1, 0) {
		t.Errorf("t1 pos = %+v", t1.Pos)
	}
	if !at(t2, 1, 4, 3) {
		t.Errorf("t2 pos = %+v", t2.Pos)
	}
	if !at(t3, 2, 3, 8) {
		t.Errorf("t3 pos = %+v", t3.Pos)
	}
}

func TestReservedWordRejected(t *testing.T) {
	s := New("PROOF")
	tk := s.Scan()
	if tk.Kind != token.ILLEGAL {
		t.Errorf("PROOF should be rejected, got %v", tk.Kind)
	}
	if len(s.Errors()) == 0 {
		t.Error("expected scan error for reserved word")
	}
}
