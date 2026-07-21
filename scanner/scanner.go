// Package scanner implements a tokenizer for TLA+ source text.
//
// The scanner tracks line and column for every token because TLA+
// junction lists (vertically aligned /\ and \/ bullets) are
// column-sensitive; the parser relies on accurate positions.
package scanner

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aburan28/tlacuilo/token"
)

// Error describes a scanning error at a position.
type Error struct {
	Pos token.Pos
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

// Scanner tokenizes TLA+ source text.
type Scanner struct {
	src  string
	off  int // byte offset of next rune
	line int
	col  int // column of next rune, 1-based, in runes

	pending *token.Token // pushed-back token (used for WF_/SF_ splitting)
	errs    []*Error
}

// New returns a Scanner over src.
func New(src string) *Scanner {
	return &Scanner{src: src, line: 1, col: 1}
}

// Errors returns the scan errors encountered so far.
func (s *Scanner) Errors() []*Error { return s.errs }

func (s *Scanner) errorf(pos token.Pos, format string, args ...any) {
	s.errs = append(s.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

func (s *Scanner) peek() rune {
	if s.off >= len(s.src) {
		return -1
	}
	r, _ := utf8.DecodeRuneInString(s.src[s.off:])
	return r
}

func (s *Scanner) peekAt(n int) rune {
	off := s.off
	for i := 0; i <= n; i++ {
		if off >= len(s.src) {
			return -1
		}
		r, w := utf8.DecodeRuneInString(s.src[off:])
		if i == n {
			return r
		}
		off += w
	}
	return -1
}

func (s *Scanner) next() rune {
	if s.off >= len(s.src) {
		return -1
	}
	r, w := utf8.DecodeRuneInString(s.src[s.off:])
	s.off += w
	if r == '\n' {
		s.line++
		s.col = 1
	} else {
		s.col++
	}
	return r
}

func (s *Scanner) pos() token.Pos { return token.Pos{Line: s.line, Col: s.col, Offset: s.off} }

// accept consumes the next rune if it equals r.
func (s *Scanner) accept(r rune) bool {
	if s.peek() == r {
		s.next()
		return true
	}
	return false
}

// acceptSeq consumes seq if the input starts with it.
func (s *Scanner) acceptSeq(seq string) bool {
	if strings.HasPrefix(s.src[s.off:], seq) {
		for range seq {
			s.next()
		}
		return true
	}
	return false
}

func isLetter(r rune) bool { return unicode.IsLetter(r) }
func isDigit(r rune) bool  { return r >= '0' && r <= '9' }
func isIdentRune(r rune) bool {
	return isLetter(r) || isDigit(r) || r == '_'
}

// Scan returns the next token, skipping whitespace and comments.
func (s *Scanner) Scan() token.Token {
	for {
		t := s.scan()
		if t.Kind != token.COMMENT {
			return t
		}
	}
}

// ScanAll returns all remaining non-comment tokens including the final EOF.
func (s *Scanner) ScanAll() []token.Token {
	var toks []token.Token
	for {
		t := s.Scan()
		toks = append(toks, t)
		if t.Kind == token.EOF {
			return toks
		}
	}
}

func (s *Scanner) scan() token.Token {
	if s.pending != nil {
		t := *s.pending
		s.pending = nil
		return t
	}
	// Skip whitespace.
	for {
		r := s.peek()
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '\f' {
			s.next()
			continue
		}
		break
	}
	pos := s.pos()
	r := s.peek()
	switch {
	case r == -1:
		return token.Token{Kind: token.EOF, Pos: pos}
	case isLetter(r) || r == '_':
		return s.scanIdent(pos)
	case isDigit(r):
		return s.scanNumber(pos)
	case r == '"':
		return s.scanString(pos)
	}
	s.next()
	op := func(lit string) token.Token {
		return token.Token{Kind: token.OP, Lit: token.Canon(lit), Pos: pos}
	}
	simple := func(k token.Kind) token.Token { return token.Token{Kind: k, Pos: pos} }
	switch r {
	case '(':
		if s.acceptSeq("*") {
			return s.scanBlockComment(pos)
		}
		for _, b := range []string{"+)", "-)", ".)", "/)", "\\X)"} {
			if s.acceptSeq(b) {
				return op("(" + b)
			}
		}
		return simple(token.LPAREN)
	case ')':
		return simple(token.RPAREN)
	case '[':
		if s.accept(']') {
			return simple(token.BOX)
		}
		return simple(token.LBRACK)
	case ']':
		if s.accept('_') {
			return simple(token.RBRACKSUB)
		}
		return simple(token.RBRACK)
	case '{':
		return simple(token.LBRACE)
	case '}':
		return simple(token.RBRACE)
	case ',':
		return simple(token.COMMA)
	case '_':
		return simple(token.UNDERSCORE)
	case '\'':
		return simple(token.PRIME)
	case '@':
		if s.accept('@') {
			return op("@@")
		}
		return simple(token.AT)
	case '!':
		if s.accept('!') {
			return op("!!")
		}
		return simple(token.BANG)
	case '#':
		if s.accept('#') {
			return op("##")
		}
		return op("#")
	case '&':
		if s.accept('&') {
			return op("&&")
		}
		return op("&")
	case '$':
		if s.accept('$') {
			return op("$$")
		}
		return op("$")
	case '%':
		if s.accept('%') {
			return op("%%")
		}
		return op("%")
	case '*':
		if s.accept('*') {
			return op("**")
		}
		return op("*")
	case '?':
		if s.accept('?') {
			return op("??")
		}
		s.errorf(pos, "unexpected character %q", r)
		return token.Token{Kind: token.ILLEGAL, Lit: string(r), Pos: pos}
	case '^':
		switch {
		case s.accept('^'):
			return op("^^")
		case s.accept('+'):
			return op("^+")
		case s.accept('*'):
			return op("^*")
		case s.accept('#'):
			return op("^#")
		}
		return op("^")
	case '+':
		if s.accept('+') {
			return op("++")
		}
		return op("+")
	case '-':
		if s.acceptSeq("+->") {
			return op("-+->")
		}
		if s.accept('|') {
			return op("-|")
		}
		if s.peek() == '-' {
			n := 1
			for s.accept('-') {
				n++
			}
			if n >= 4 {
				return token.Token{Kind: token.DASHES, Pos: pos}
			}
			if n == 2 {
				return op("--")
			}
			s.errorf(pos, "run of %d dashes is not a TLA+ token", n)
			return token.Token{Kind: token.ILLEGAL, Lit: strings.Repeat("-", n), Pos: pos}
		}
		if s.accept('>') {
			return simple(token.ARROW)
		}
		if s.accept('.') {
			return op("-.") // prefix-minus marker in operator definitions
		}
		return op("-")
	case '=':
		if s.peek() == '=' {
			n := 1
			for s.accept('=') {
				n++
			}
			if n >= 4 {
				return token.Token{Kind: token.MODEND, Pos: pos}
			}
			if n == 2 {
				return simple(token.DEFEQ)
			}
			s.errorf(pos, "run of %d equals signs is not a TLA+ token", n)
			return token.Token{Kind: token.ILLEGAL, Lit: strings.Repeat("=", n), Pos: pos}
		}
		if s.accept('>') {
			return op("=>")
		}
		if s.accept('<') {
			return op("=<")
		}
		if s.accept('|') {
			return op("=|")
		}
		return op("=")
	case '<':
		if s.acceptSeq("=>") {
			return op("<=>")
		}
		if s.accept('<') {
			return simple(token.LTUP)
		}
		if s.accept('-') {
			return simple(token.LARROW)
		}
		if s.accept(':') {
			return op("<:")
		}
		if s.accept('=') {
			return op("<=")
		}
		if s.accept('>') {
			return simple(token.DIAMOND)
		}
		return op("<")
	case '>':
		if s.accept('>') {
			if s.accept('_') {
				return simple(token.RTUPSUB)
			}
			return simple(token.RTUP)
		}
		if s.accept('=') {
			return op(">=")
		}
		return op(">")
	case '.':
		if s.accept('.') {
			if s.accept('.') {
				return op("...")
			}
			return op("..")
		}
		return simple(token.DOT)
	case ':':
		if s.acceptSeq(":=") {
			return op("::=")
		}
		if s.accept('>') {
			return op(":>")
		}
		if s.accept('=') {
			return op(":=")
		}
		return simple(token.COLON)
	case '|':
		if s.acceptSeq("->") {
			return simple(token.MAPSTO)
		}
		if s.accept('|') {
			return op("||")
		}
		if s.accept('-') {
			return op("|-")
		}
		if s.accept('=') {
			return op("|=")
		}
		return op("|")
	case '/':
		if s.accept('\\') {
			return token.Token{Kind: token.AND, Pos: pos}
		}
		if s.accept('=') {
			return op("/=")
		}
		if s.accept('/') {
			return op("//")
		}
		return op("/")
	case '~':
		if s.accept('>') {
			return op("~>")
		}
		return op("~")
	case '\\':
		return s.scanBackslash(pos)
	}
	s.errorf(pos, "unexpected character %q", r)
	return token.Token{Kind: token.ILLEGAL, Lit: string(r), Pos: pos}
}

func (s *Scanner) scanIdent(pos token.Pos) token.Token {
	start := s.off
	hasLetter := false
	for {
		r := s.peek()
		if !isIdentRune(r) {
			break
		}
		if isLetter(r) {
			hasLetter = true
		}
		s.next()
	}
	lit := s.src[start:s.off]
	if !hasLetter {
		if lit == "_" {
			return token.Token{Kind: token.UNDERSCORE, Pos: pos}
		}
		s.errorf(pos, "identifier %q must contain a letter", lit)
		return token.Token{Kind: token.ILLEGAL, Lit: lit, Pos: pos}
	}
	// WF_x / SF_x: split the fairness prefix from the subscript.
	for _, pre := range []string{"WF_", "SF_"} {
		if strings.HasPrefix(lit, pre) && len(lit) > len(pre) {
			rest := lit[len(pre):]
			restPos := token.Pos{Line: pos.Line, Col: pos.Col + len(pre), Offset: pos.Offset + len(pre)}
			s.pending = &token.Token{Kind: token.IDENT, Lit: rest, Pos: restPos}
			kind := token.WFSUB
			if pre == "SF_" {
				kind = token.SFSUB
			}
			return token.Token{Kind: kind, Pos: pos}
		}
	}
	if lit == "WF_" || lit == "SF_" {
		if lit == "WF_" {
			return token.Token{Kind: token.WFSUB, Pos: pos}
		}
		return token.Token{Kind: token.SFSUB, Pos: pos}
	}
	if k := token.Lookup(lit); k != token.IDENT {
		return token.Token{Kind: k, Lit: lit, Pos: pos}
	}
	if token.IsReserved(lit) {
		s.errorf(pos, "reserved word %q cannot be used here (TLA+ proof language is not supported)", lit)
		return token.Token{Kind: token.ILLEGAL, Lit: lit, Pos: pos}
	}
	return token.Token{Kind: token.IDENT, Lit: lit, Pos: pos}
}

func (s *Scanner) scanNumber(pos token.Pos) token.Token {
	start := s.off
	for isDigit(s.peek()) {
		s.next()
	}
	// Fraction, but not the '..' range operator.
	if s.peek() == '.' && isDigit(s.peekAt(1)) {
		s.next()
		for isDigit(s.peek()) {
			s.next()
		}
	}
	return token.Token{Kind: token.NUMBER, Lit: s.src[start:s.off], Pos: pos}
}

func (s *Scanner) scanString(pos token.Pos) token.Token {
	s.next() // opening quote
	var b strings.Builder
	for {
		r := s.next()
		switch r {
		case -1, '\n':
			s.errorf(pos, "unterminated string")
			return token.Token{Kind: token.ILLEGAL, Lit: b.String(), Pos: pos}
		case '"':
			return token.Token{Kind: token.STRING, Lit: b.String(), Pos: pos}
		case '\\':
			e := s.next()
			switch e {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'f':
				b.WriteByte('\f')
			case 'r':
				b.WriteByte('\r')
			default:
				s.errorf(pos, "invalid string escape \\%c", e)
			}
		default:
			b.WriteRune(r)
		}
	}
}

func (s *Scanner) scanBlockComment(pos token.Pos) token.Token {
	// "(*" already consumed; block comments nest.
	depth := 1
	start := s.off
	for depth > 0 {
		r := s.next()
		switch r {
		case -1:
			s.errorf(pos, "unterminated block comment")
			return token.Token{Kind: token.COMMENT, Lit: s.src[start:s.off], Pos: pos}
		case '(':
			if s.peek() == '*' {
				s.next()
				depth++
			}
		case '*':
			if s.peek() == ')' {
				s.next()
				depth--
			}
		}
	}
	end := s.off - 2
	if end < start {
		end = start
	}
	return token.Token{Kind: token.COMMENT, Lit: s.src[start:end], Pos: pos}
}

// scanBackslash handles every token that begins with '\': the \/ bullet,
// set difference, \-word operators, quantifiers, and \b \o \h numbers.
func (s *Scanner) scanBackslash(pos token.Pos) token.Token {
	if s.accept('/') {
		return token.Token{Kind: token.OR, Pos: pos}
	}
	if s.accept('*') {
		// Line comment to end of line.
		start := s.off
		for s.peek() != '\n' && s.peek() != -1 {
			s.next()
		}
		return token.Token{Kind: token.COMMENT, Lit: s.src[start:s.off], Pos: pos}
	}
	r := s.peek()
	if !isLetter(r) {
		return token.Token{Kind: token.OP, Lit: "\\", Pos: pos} // set difference
	}
	// Number bases: \b0101, \B0101, \o777, \O777, \hFF, \HFF.
	if r == 'b' || r == 'B' || r == 'o' || r == 'O' || r == 'h' || r == 'H' {
		if lit, ok := s.tryBaseNumber(r); ok {
			return token.Token{Kind: token.NUMBER, Lit: lit, Pos: pos}
		}
	}
	start := s.off
	for isLetter(s.peek()) {
		s.next()
	}
	word := s.src[start:s.off]
	switch word {
	case "A":
		return token.Token{Kind: token.FORALL, Pos: pos}
	case "E":
		return token.Token{Kind: token.EXISTS, Pos: pos}
	case "AA":
		return token.Token{Kind: token.TFORALL, Pos: pos}
	case "EE":
		return token.Token{Kind: token.TEXISTS, Pos: pos}
	case "X", "times":
		return token.Token{Kind: token.TIMES, Pos: pos}
	case "land":
		return token.Token{Kind: token.AND, Pos: pos}
	case "lor":
		return token.Token{Kind: token.OR, Pos: pos}
	}
	if token.IsBackslashWord(word) {
		return token.Token{Kind: token.OP, Lit: token.Canon("\\" + word), Pos: pos}
	}
	s.errorf(pos, "unknown operator \\%s", word)
	return token.Token{Kind: token.ILLEGAL, Lit: "\\" + word, Pos: pos}
}

func (s *Scanner) tryBaseNumber(base rune) (string, bool) {
	valid := func(r rune) bool {
		switch base {
		case 'b', 'B':
			return r == '0' || r == '1'
		case 'o', 'O':
			return r >= '0' && r <= '7'
		default: // h, H
			return isDigit(r) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
		}
	}
	if !valid(s.peekAt(1)) {
		return "", false
	}
	s.next() // base letter
	start := s.off
	for valid(s.peek()) {
		s.next()
	}
	return "\\" + string(base) + s.src[start:s.off], true
}
