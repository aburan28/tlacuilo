package token_test

import (
	"fmt"

	"github.com/aburan28/tlacuilo/token"
)

func ExampleLookup() {
	fmt.Println(token.Lookup("CHOOSE"))
	fmt.Println(token.Lookup("myName"))
	// Output:
	// CHOOSE
	// IDENT
}

func ExampleCanon() {
	// Alternative spellings canonicalize; precedence ranges come from
	// Specifying Systems (higher binds tighter).
	fmt.Println(token.Canon(`\union`))
	fmt.Println(token.InfixOps[`\cup`].Lo, token.InfixOps["=>"].Lo)
	// Output:
	// \cup
	// 8 1
}
