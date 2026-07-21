# tlacuilo — a TLA+ library for Go: design plan

*Tlacuilo* (Nahuatl: "scribe") is a pure-Go library for working with TLA+:
writing specifications programmatically, parsing and formatting TLA+ source,
generating and parsing TLC model-checker configuration, running TLC, and
consuming its results — including machine-readable counterexample traces.

## 1. Research summary

Findings that shaped this design:

- **There is no existing Go library for TLA+.** The canonical parser is SANY
  (Java, part of [tlaplus/tlaplus](https://github.com/tlaplus/tlaplus));
  [tree-sitter-tlaplus](https://github.com/tlaplus-community/tree-sitter-tlaplus)
  re-implements the grammar for editors; [PGo](https://github.com/DistCompiler/pgo)
  compiles PlusCal *to* Go but is written in Scala. A native Go library fills a
  real gap for Go teams that model-check their systems (a common industry
  practice: MongoDB, AWS, CCF, etcd-adjacent work all pair TLA+ specs with
  implementations).
- **TLA+ is not context-free.** Vertically-aligned conjunction/disjunction
  ("junction") lists use column alignment for grouping: juncts bulleted with
  `/\` or `\/` at the same column belong to one list, and every token of an
  item must lie strictly to the right of its bullet. A parser therefore needs
  column-aware token handling (tree-sitter uses an external scanner with a
  stack of junction columns; we do the equivalent inside a recursive-descent
  parser).
- **Operator precedence is specified as ranges** (e.g. `=>` is 1–1, `/\` 3–3,
  `=` 5–5, `+` 10–10, `*` 13–13, prime `'` 15–15), from *Specifying Systems*.
  Operators with overlapping ranges may not be mixed without parentheses;
  associative operators (`/\`, `\/`, `+`, `\cup`, …) may chain.
- **TLC has a stable machine interface.** With `-tool`, every message is
  framed as `@!@!@STARTMSG <code>:<severity> @!@!@ … @!@!@ENDMSG <code> @!@!@`
  (severity: 0 none, 1 error, 2 TLC-bug, 3 warning, 4 state). Counterexample
  states arrive as code-2217 blocks: a `N: <ActionName …>` header followed by
  the state as a TLA+ conjunction of `var = value` lines; lasso/stuttering
  info arrives as code 2218. We verified this empirically against TLC 2.15
  and captured fixtures for success, invariant violation, deadlock, and
  liveness (lasso) runs.
- **TLC exit codes** (verified empirically): 0 success, 10 assumption
  violation, 11 deadlock, 12 safety/invariant violation, 13 liveness
  violation, 150+ tool/parse failures.
- **TLC config files** are a small keyword language: `SPECIFICATION` or
  `INIT`/`NEXT`, `INVARIANT(S)`, `PROPERTY`/`PROPERTIES`,
  `CONSTANT(S)` (with `Name = Value` assignments or `Name <- Operator`
  replacements, model values, sets of model values), `SYMMETRY`, `VIEW`,
  `CONSTRAINT(S)`, `ACTION_CONSTRAINT(S)`, `CHECK_DEADLOCK`, `ALIAS`,
  `POSTCONDITION`, with TLA+-style comments.
- **The Informal Trace Format (ITF)** is the emerging standard for
  machine-readable traces (Apalache ADR-015; produced by Apalache, Quint, and
  recent TLC via `-dumpTrace json`): a JSON object with `#meta`, `params`,
  `vars`, `states`, `loop`, and typed value encodings (`{"#bigint": "…"}`,
  `{"#set": […]}`, `{"#tup": […]}`, `{"#map": [[k,v],…]}`,
  `{"#unserializable": "…"}`). Supporting ITF makes traces interoperable with
  the wider ecosystem (itf-rs, itf-py, trace explorers).

## 2. Goals and non-goals

### Goals (v1)

1. **Write TLA+ from Go** — a typed AST plus a fluent builder API that renders
   correctly formatted, parseable TLA+ (the library's namesake feature).
2. **Parse TLA+ in Go** — lexer and parser for the module language and full
   expression language, with positions, producing the same AST.
3. **Format TLA+** — a canonical pretty-printer such that
   `parse(print(ast)) ≡ ast` (structural round-trip).
4. **Drive TLC** — generate/parse `.cfg` files, locate/run `tla2tools.jar`,
   stream and parse `-tool` output into typed results, map exit codes.
5. **Consume counterexamples** — typed TLA+ values (sets, functions, records,
   sequences, big integers, model values), traces parsed from TLC output, and
   ITF import/export.

### Non-goals (v1)

- The TLA+2 **proof language** (`THEOREM … PROOF`, `ASSUME/PROVE` steps) —
  parsed specs may declare theorems, but proof bodies are out of scope (as in
  most tooling outside TLAPS).
- **PlusCal** (it lives inside comments; a future translator could reuse the
  AST).
- **Semantic analysis** (level checking, arity/def resolution) and
  **evaluation** beyond literal values — TLC remains the checker; we are the
  interface to it.
- Bundling Java or the jar — we discover or download `tla2tools.jar` and give
  clear errors otherwise.

## 3. Architecture

Package layout mirrors the Go standard library's `go/*` family:

```
github.com/aburan28/tlacuilo
├── token/      token kinds, positions, precedence table, operator metadata
├── scanner/    column-aware lexer (comments, strings, numbers, \-operators,
│               ---- and ==== module lines, WF_/SF_, ]_ and >>_)
├── ast/        AST nodes for modules, units, expressions + pretty-printer
├── parser/     recursive-descent + precedence-climbing parser,
│               junction lists via a column stack
├── value/      TLA+ value model (Bool, Int/big, String, ModelValue, Set,
│               Tuple/Seq, Record, Function, Interval) + parse/format + ITF
├── cfg/        TLC configuration: struct ⇄ .cfg text (generate and parse)
├── trace/      State/Trace types, ITF (JSON) encode/decode
├── tlc/        runner: jar discovery/download, option building, streaming
│               -tool output parser, exit-status mapping, typed Result
└── builder/    fluent API for constructing modules/specs programmatically
```

Dependency flow is strictly downward: `builder → ast → token`,
`parser → scanner → token`, `tlc → cfg, trace, value, parser`. No third-party
dependencies (standard library only).

### 3.1 token / scanner

- Tokens carry `Pos{Line, Col, Offset}` — columns are load-bearing (junction
  lists).
- Tricky lexemes handled in the scanner:
  - `----+` (module header/separator) vs `-`; `====+` (module end) vs `=`.
  - Backslash operators (`\in`, `\cup`, `\o`, …) vs backslash number bases
    (`\b0101`, `\o777`, `\hFF`) vs `\` (set difference).
  - Multi-char operators with longest match: `<=>`, `=>`, `<=`, `>=`, `<>`,
    `<<`, `>>`, `|->`, `->`, `<-`, `==`, `..`, `…`, `@@`, `:>`, `<:`, `[]`,
    `(+)`, `^+`, etc.
  - `WF_` / `SF_` split from following subscript; `]_` and `>>_` fused tokens
    for `[A]_v` / `<<A>>_v` (only when `_` is immediately adjacent).
  - Nested block comments `(* (* *) *)` and line comments `\*`.
- The precedence/associativity table from *Specifying Systems* lives in
  `token` as data, shared by parser (parsing decisions) and printer
  (minimal parenthesization).

### 3.2 ast + printer

Nodes preserve enough structure to round-trip meaning, not bytes: junction
lists are a first-class node (`JunctionList{Op, Items}`) distinct from binary
`/\`, so the formatter can re-emit aligned bullets. The printer produces a
canonical style: 4-column module indent, aligned junctions, multiline
`LET … IN`, `IF/THEN/ELSE`, and `CASE` when wide, and inserts parentheses only
where precedence requires.

### 3.3 parser

- Recursive descent for module structure; precedence climbing for
  expressions using the range table (single effective precedence = range low;
  chaining of non-associative operators is rejected, matching SANY).
- **Junction lists:** the parser keeps a stack of active bullet columns. Every
  token fetch is filtered: a token starting at column ≤ the innermost bullet
  column acts as a virtual terminator for the current item; a bullet of the
  same kind at exactly the bullet column starts the next item. This is the
  same discipline as tree-sitter's external scanner, expressed in the reader.
- `\X` parses as n-ary Cartesian product (not left-nested binary).
- Supported: all declarations/definitions (constants with arity, variables,
  operator/function/module definitions, `LOCAL`, `RECURSIVE`, `INSTANCE …
  WITH`, `ASSUME`, `THEOREM`), nested modules, and the full constant/action/
  temporal expression language (quantifiers incl. `\EE`/`\AA`, `CHOOSE`,
  `LAMBDA`, `EXCEPT` with `@`, `[A]_v`, `<<A>>_v`, `WF_v/SF_v`, `[]`, `<>`,
  `~>`, `-+->`, `\cdot`, module refs `M!Op`).

### 3.4 value

A closed interface `value.Value` with kinds Bool, Int (`math/big`), String,
ModelValue, Set, Tuple (= sequence), Record, Function, and Interval (`a..b`).
Values support equality, stable ordering (for canonical set printing),
TLA+-syntax rendering, and conversion from parsed expressions — which is what
lets us parse TLC's textual counterexample states (`/\ x = <<[n |-> 0]>>`,
functions printed as `(1 :> "a" @@ 2 :> "b")`) with the same parser used for
specs. ITF encode/decode lives here too.

### 3.5 cfg

`cfg.Config` covers the full keyword set (§1). `Config.Format()` emits a
`.cfg`; `cfg.Parse` reads one back; round-trip tested. Constant assignments
distinguish `=` value assignment (including model values and sets of model
values) from `<-` operator replacement.

### 3.6 tlc

- `tlc.FindJar()` checks `$TLA2TOOLS_JAR`, the working directory, and
  well-known locations; `tlc.DownloadJar(ctx, dest)` fetches from GitHub
  releases for CI setups.
- `tlc.Run(ctx, opts)` builds the `java -cp tla2tools.jar tlc2.TLC -tool …`
  invocation (workers, config, deadlock checking, simulation mode, depth,
  seed, metadir, extra args), streams stdout through the tool-mode parser,
  and returns a typed `Result`: status (mapped from exit code + messages),
  states generated/distinct/queue, depth, duration, errors with codes, and a
  `*trace.Trace` counterexample when one was produced (safety traces, and
  liveness lassos with the loop index).
- The output parser is fixture-tested against real captured TLC output and
  degrades gracefully on unknown message codes.

### 3.7 trace + ITF

`trace.Trace{States []State, Loop *int, Kind}` with
`State{Index, Action, Vars map[string]value.Value}`. `trace.ToITF/FromITF`
(de)serializes the Apalache ITF JSON format so traces interoperate with the
broader ecosystem.

### 3.8 builder

A fluent, misuse-resistant layer over `ast`:

```go
m := builder.NewModule("Counter").Extends("Naturals")
x := builder.ID("x")
m.Variables("x")
m.Define("Init", builder.Eq(x, builder.Int(0)))
m.Define("Next", builder.Eq(builder.Prime(x), builder.Add(x, builder.Int(1))))
m.Define("Spec", builder.And(
    builder.ID("Init"),
    builder.Always(builder.Sub(builder.ID("Next"), x)), // [][Next]_x
))
src := m.Module().String()
```

plus expression helpers for the whole surface (sets, functions, records,
`EXCEPT`, quantifiers, fairness, …). The builder output is always formatted
and parseable — tests round-trip every built spec through the parser and,
when a jar is available, through TLC itself.

## 4. Testing strategy

- **Scanner:** table-driven token/position tests, tricky-lexeme corpus.
- **Parser:** golden tests over a corpus of representative specs (incl. the
  classic DieHard/simple protocol shapes); property: `parse ∘ print ∘ parse`
  is a fixed point; error-position tests for misaligned junction lists.
- **value/cfg/trace:** round-trip tests (text ⇄ types ⇄ ITF JSON).
- **tlc:** parser unit tests run against checked-in fixtures captured from a
  real TLC (success, invariant violation, deadlock, liveness lasso);
  integration tests execute TLC end-to-end and are skipped automatically when
  `java`/jar are unavailable.
- **CI:** GitHub Actions — gofmt, `go vet`, `go test ./...` (with jar
  download for integration coverage when the network allows).

## 5. Milestones

1. `token` + `scanner` (with tests) — the column-aware foundation.
2. `ast` + printer.
3. `parser` (junction lists, precedence ranges, full expression language).
4. `value` (+ TLC textual value parsing, ITF).
5. `cfg` generate/parse.
6. `trace` + `tlc` runner with fixture-tested tool-mode parser.
7. `builder` fluent API.
8. Examples, CI, docs.

## 6. Future directions (post-v1)

- Semantic checks (symbol resolution, arity, level checking) for
  fail-fast-before-Java feedback.
- Apalache runner (same cfg/trace machinery, JSON interface).
- Trace *validation* helpers: emit TLA+ trace specs from Go execution logs to
  check implementations against specs (the MongoDB/CCF technique).
- PlusCal parsing/translation; byte-fidelity formatting mode.
