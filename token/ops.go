package token

// Operator precedence, from the table in Specifying Systems (and the
// summary in "The Operators of TLA+"). Precedences are ranges Lo–Hi;
// operators whose ranges overlap may not be mixed without parentheses
// unless they are the same associative operator. Higher numbers bind
// tighter.

// OpInfo describes one operator's parsing properties.
type OpInfo struct {
	Lo, Hi int
	Assoc  bool // left-associative: the operator may chain with itself
}

// Precedence levels for the built-in postfix-like constructs.
const (
	PrecApply = 16 // f[x] function application
	PrecDot   = 17 // r.field record selection
)

// InfixOps maps canonical operator spellings to precedence info.
var InfixOps = map[string]OpInfo{
	"=>":   {1, 1, false},
	"-+->": {2, 2, false},
	"<=>":  {2, 2, false},
	"~>":   {2, 2, false},
	"/\\":  {3, 3, true},
	"\\/":  {3, 3, true},

	"/=": {5, 5, false}, "=": {5, 5, false},
	"<": {5, 5, false}, ">": {5, 5, false}, "<=": {5, 5, false}, ">=": {5, 5, false},
	"\\in": {5, 5, false}, "\\notin": {5, 5, false},
	"\\subset": {5, 5, false}, "\\subseteq": {5, 5, false},
	"\\supset": {5, 5, false}, "\\supseteq": {5, 5, false},
	"\\sqsubset": {5, 5, false}, "\\sqsubseteq": {5, 5, false},
	"\\sqsupset": {5, 5, false}, "\\sqsupseteq": {5, 5, false},
	"\\prec": {5, 5, false}, "\\preceq": {5, 5, false},
	"\\succ": {5, 5, false}, "\\succeq": {5, 5, false},
	"\\ll": {5, 5, false}, "\\gg": {5, 5, false},
	"\\sim": {5, 5, false}, "\\simeq": {5, 5, false},
	"\\approx": {5, 5, false}, "\\cong": {5, 5, false},
	"\\asymp": {5, 5, false}, "\\doteq": {5, 5, false}, "\\propto": {5, 5, false},
	":=": {5, 5, false}, "::=": {5, 5, false},
	"-|": {5, 5, false}, "|-": {5, 5, false}, "=|": {5, 5, false}, "|=": {5, 5, false},

	"\\cdot": {5, 14, true}, // action composition

	"@@": {6, 6, true},
	":>": {7, 7, false},
	"<:": {7, 7, false},

	"\\":    {8, 8, false}, // set difference
	"\\cap": {8, 8, true}, "\\cup": {8, 8, true},

	"..": {9, 9, false}, "...": {9, 9, false},
	"!!": {9, 13, false}, "##": {9, 13, true},
	"$": {9, 13, true}, "$$": {9, 13, true}, "??": {9, 13, true},
	"\\sqcap": {9, 13, true}, "\\sqcup": {9, 13, true},
	"\\uplus": {9, 13, true},

	"\\oplus": {10, 10, true},
	"+":       {10, 10, true},
	"++":      {10, 10, true},
	"%":       {10, 11, false},
	"%%":      {10, 11, true},
	"|":       {10, 11, true},
	"||":      {10, 11, true},

	"\\ominus": {11, 11, true},
	"-":        {11, 11, true},
	"--":       {11, 11, true},

	"&": {13, 13, true}, "&&": {13, 13, true},
	"\\odot": {13, 13, true}, "\\oslash": {13, 13, false},
	"\\otimes":  {13, 13, true},
	"*":         {13, 13, true},
	"**":        {13, 13, true},
	"/":         {13, 13, false},
	"//":        {13, 13, false},
	"\\bigcirc": {13, 13, true}, "\\bullet": {13, 13, true},
	"\\div": {13, 13, false}, "\\o": {13, 13, true}, "\\star": {13, 13, true},
	"\\wr": {9, 14, false},

	"^": {14, 14, false}, "^^": {14, 14, false},
}

// TimesOp is the precedence of \X / \times, which is n-ary rather than
// binary and therefore handled specially by parsers and printers.
var TimesOp = OpInfo{10, 13, true}

// PrefixOps maps canonical prefix operator spellings to precedence info.
// The Hi bound determines how much of what follows the operator absorbs:
// its operand extends over infix operators with Lo > Hi.
var PrefixOps = map[string]OpInfo{
	"~":         {4, 4, false},
	"ENABLED":   {4, 15, false},
	"UNCHANGED": {4, 15, false},
	"[]":        {4, 15, false},
	"<>":        {4, 15, false},
	"SUBSET":    {8, 8, false},
	"UNION":     {8, 8, false},
	"DOMAIN":    {9, 9, false},
	"-":         {12, 12, false},
}

// PostfixOps maps canonical postfix operator spellings to precedence info.
var PostfixOps = map[string]OpInfo{
	"'":  {15, 15, false},
	"^+": {15, 15, false},
	"^*": {15, 15, false},
	"^#": {15, 15, false},
}

// Aliases maps alternative spellings to the canonical spelling used in
// Lit fields and the tables above.
var Aliases = map[string]string{
	"\\land":      "/\\",
	"\\lor":       "\\/",
	"\\lnot":      "~",
	"\\neg":       "~",
	"\\equiv":     "<=>",
	"#":           "/=",
	"=<":          "<=",
	"\\leq":       "<=",
	"\\geq":       ">=",
	"\\intersect": "\\cap",
	"\\union":     "\\cup",
	"\\circ":      "\\o",
	"\\times":     "\\X",
	"(+)":         "\\oplus",
	"(-)":         "\\ominus",
	"(.)":         "\\odot",
	"(/)":         "\\oslash",
	"(\\X)":       "\\otimes",
}

// Canon returns the canonical spelling of an operator.
func Canon(op string) string {
	if c, ok := Aliases[op]; ok {
		return c
	}
	return op
}

// backslashWords is the set of alphabetic operators written with a leading
// backslash, in canonical spelling (aliases resolved by the scanner).
var backslashWords = map[string]bool{}

func init() {
	for op := range InfixOps {
		if len(op) > 1 && op[0] == '\\' && isAlpha(op[1]) {
			backslashWords[op[1:]] = true
		}
	}
	// Alias sources spelled \word.
	for op := range Aliases {
		if len(op) > 1 && op[0] == '\\' && isAlpha(op[1]) {
			backslashWords[op[1:]] = true
		}
	}
	// \X handled as TIMES but listed for scanner lookup.
	backslashWords["X"] = true
}

func isAlpha(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// IsBackslashWord reports whether \word is a known operator spelling.
func IsBackslashWord(word string) bool { return backslashWords[word] }
