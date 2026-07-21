package scanner_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/scanner"
	"github.com/aburan28/tlacuilo/token"
)

func ExampleScanner() {
	s := scanner.New(`x' = x \cup {1}`)
	for {
		tok := s.Scan()
		if tok.Kind == token.EOF {
			break
		}
		fmt.Printf("%v %q\n", tok.Kind, tok.Lit)
	}
	// Output:
	// IDENT "x"
	// ' ""
	// OP "="
	// IDENT "x"
	// OP "\\cup"
	// { ""
	// NUMBER "1"
	// } ""
}
