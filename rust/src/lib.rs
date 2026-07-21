//! # tlacuilo
//!
//! A pure-Rust toolkit for TLA+ (*tlacuilo* is Nahuatl for "scribe"),
//! the Rust port of the Go library in this repository:
//!
//! - [`scanner`] / [`token`] / [`parser`] / [`ast`] / [`printer`] — a
//!   column-aware TLA+ parser (junction lists, the full operator
//!   precedence table from *Specifying Systems*) and a canonical
//!   pretty-printer (`parse ∘ print` is a fixed point)
//! - [`value`] — TLA+ values (big ints, sets, tuples, records,
//!   functions) with TLC-output parsing and ITF JSON interchange
//! - [`cfg`] — TLC configuration files, generated and parsed
//! - [`tlc`] — run the TLC model checker and consume its tool-mode
//!   output as typed results with counterexample traces
//! - [`trace`] — traces with ITF (Informal Trace Format) import/export
//! - [`tracecheck`] — validate implementations against specs by trace
//!   checking: annotate actions, record under a deterministic harness,
//!   let TLC judge conformance

pub mod ast;
pub mod cfg;
pub mod parser;
pub mod printer;
pub mod scanner;
pub mod tlc;
pub mod token;
pub mod trace;
pub mod tracecheck;
pub mod value;

#[cfg(test)]
mod parser_tests {
    use crate::ast::{Expr, Unit};
    use crate::parser::{parse, parse_expr};
    use crate::printer::{expr_string, module_string};

    /// parse → print → parse → print is a fixed point; returns the
    /// canonical form.
    fn round_trip(src: &str) -> String {
        let e = parse_expr(src).unwrap_or_else(|err| panic!("parse {src:?}: {err}"));
        let out1 = expr_string(&e);
        let e2 = parse_expr(&out1)
            .unwrap_or_else(|err| panic!("reparse {out1:?} (from {src:?}): {err}"));
        let out2 = expr_string(&e2);
        assert_eq!(out1, out2, "print not stable for {src:?}");
        out1
    }

    fn expect_print(src: &str, want: &str) {
        assert_eq!(round_trip(src), want, "canonical form of {src:?}");
    }

    #[test]
    fn precedence() {
        expect_print("1 + 2 * 3", "1 + 2 * 3");
        expect_print("(1 + 2) * 3", "(1 + 2) * 3");
        expect_print("a + b - c", "a + b - c"); // '-' binds tighter: a + (b - c)
        expect_print("x = 1 /\\ y = 2", "x = 1 /\\ y = 2");
        expect_print("~x \\in S", "~x \\in S");
        expect_print("-a * b", "-a * b");
        expect_print("(a + b)'", "(a + b)'");
        expect_print("f[x][y]", "f[x][y]");
        expect_print("S \\cup T \\cup U", "S \\cup T \\cup U");
        expect_print("1..N", "1..N");
        expect_print("S \\X T \\X U", "S \\X T \\X U");
        expect_print("DOMAIN f \\cup S", "DOMAIN f \\cup S");
    }

    #[test]
    fn conflicts_rejected() {
        for src in [
            "a = b = c",
            "a \\cup b \\cap c",
            "a < b <= c",
            "a /\\ b \\/ c",
        ] {
            assert!(parse_expr(src).is_err(), "{src:?} should be rejected");
        }
    }

    #[test]
    fn constructs() {
        expect_print("{1, 2, 3}", "{1, 2, 3}");
        expect_print("{x \\in S : x > 0}", "{x \\in S : x > 0}");
        expect_print("{x * x : x \\in S}", "{x * x : x \\in S}");
        expect_print("[x \\in S |-> x + 1]", "[x \\in S |-> x + 1]");
        expect_print("[S -> T]", "[S -> T]");
        expect_print("[a |-> 1, b |-> \"x\"]", "[a |-> 1, b |-> \"x\"]");
        expect_print("[a : Nat, b : STRING]", "[a : Nat, b : STRING]");
        expect_print(
            "[f EXCEPT ![i].a = @ + 1, !.b = 0]",
            "[f EXCEPT ![i].a = @ + 1, !.b = 0]",
        );
        expect_print("[<<1, 2>> EXCEPT ![1] = 0]", "[<<1, 2>> EXCEPT ![1] = 0]");
        expect_print(
            "IF x > 0 THEN \"pos\" ELSE \"neg\"",
            "IF x > 0 THEN \"pos\" ELSE \"neg\"",
        );
        expect_print(
            "CASE a -> 1 [] b -> 2 [] OTHER -> 3",
            "CASE a -> 1 [] b -> 2 [] OTHER -> 3",
        );
        expect_print(
            "LET sq(a) == a * a IN sq(3)",
            "LET sq(a) == a * a\nIN  sq(3)",
        );
        expect_print("\\A x \\in S : x > 0", "\\A x \\in S : x > 0");
        expect_print("\\E x, y \\in S : x /= y", "\\E x, y \\in S : x /= y");
        expect_print("CHOOSE x \\in S : x > 0", "CHOOSE x \\in S : x > 0");
        expect_print("M!Op(x)", "M!Op(x)");
        expect_print("Foo(LAMBDA a, b : a + b, S)", "Foo(LAMBDA a, b : a + b, S)");
        expect_print("UNCHANGED <<x, y>>", "UNCHANGED <<x, y>>");
    }

    #[test]
    fn actions_and_temporal() {
        expect_print("x' = x + 1", "x' = x + 1");
        expect_print("[][Next]_vars", "[][Next]_vars");
        expect_print("<><<Next>>_vars", "<><<Next>>_vars");
        expect_print("[Next]_<<x, y>>", "[Next]_<<x, y>>");
        expect_print("WF_vars(Next)", "WF_vars(Next)");
        expect_print("SF_<<x, y>>(A \\/ B)", "SF_<<x, y>>(A \\/ B)");
        expect_print("[]P => <>Q", "[]P => <>Q");
        expect_print(
            "Init /\\ [][Next]_vars /\\ WF_vars(Next)",
            "Init /\\ [][Next]_vars /\\ WF_vars(Next)",
        );
    }

    #[test]
    fn junction_lists() {
        let e = parse_expr("/\\ x = 1\n/\\ y = 2\n/\\ z = 3").unwrap();
        let Expr::Junction { items, .. } = &e else {
            panic!("want junction, got {e:?}");
        };
        assert_eq!(items.len(), 3);
        round_trip("/\\ x = 1\n/\\ y = 2\n/\\ z = 3");

        // Nested junctions grouped by column.
        let e = parse_expr("/\\ \\/ a\n   \\/ b\n/\\ c").unwrap();
        let Expr::Junction { items, .. } = &e else {
            panic!()
        };
        assert_eq!(items.len(), 2);
        assert!(
            matches!(&items[0], Expr::Junction { op, items } if op == "\\/" && items.len() == 2)
        );

        // Multi-line item: deeper-indented continuation stays inside.
        let e = parse_expr("/\\ x =\n     1\n/\\ y = 2").unwrap();
        let Expr::Junction { items, .. } = &e else {
            panic!()
        };
        assert_eq!(items.len(), 2);

        // A dedented operator ends the list.
        let e = parse_expr("/\\ a\n/\\ b\n=> c").unwrap();
        assert!(matches!(&e, Expr::Binary { op, .. } if op == "=>"));
    }

    #[test]
    fn module_round_trip() {
        let src = "---- MODULE Counter ----\nEXTENDS Naturals\nVARIABLE x\nInit == x = 0\nNext == /\\ x < 3\n        /\\ x' = x + 1\nSpec == Init /\\ [][Next]_x /\\ WF_x(Next)\n====\n";
        let m = parse(src).unwrap();
        assert_eq!(m.name, "Counter");
        assert_eq!(m.units.len(), 4);
        let out1 = module_string(&m);
        let m2 = parse(&out1).unwrap_or_else(|e| panic!("reparse failed: {e}\n{out1}"));
        assert_eq!(out1, module_string(&m2), "module print not stable");
    }

    #[test]
    fn prefix_operator_definitions() {
        let src = "---- MODULE Ints ----\nLOCAL INSTANCE Naturals\n-. a == 0 - a\n~ b == b = 0\nAbs(n) == LET -. m == 0 - m IN IF n < 0 THEN -n ELSE n\n====\n";
        let m = parse(src).unwrap();
        assert!(matches!(
            &m.units[1],
            Unit::OperatorDef {
                fixity: crate::ast::Fixity::PrefixSym,
                name,
                ..
            } if name == "-."
        ));
        let out = module_string(&m);
        let m2 = parse(&out).unwrap_or_else(|e| panic!("reparse: {e}\n{out}"));
        assert_eq!(out, module_string(&m2));
    }

    #[test]
    fn shared_controller_spec_round_trips() {
        // The spec shared with the Go tests and the TLA+ proof CI.
        let src = include_str!("../../examples/k8scontroller/ReplicaController.tla");
        let m = parse(src).unwrap_or_else(|e| panic!("ReplicaController.tla: {e}"));
        assert_eq!(m.name, "ReplicaController");
        let out = module_string(&m);
        let m2 = parse(&out).unwrap_or_else(|e| panic!("reparse: {e}\n{out}"));
        assert_eq!(out, module_string(&m2));
    }

    #[test]
    fn errors_have_positions() {
        let err = parse_expr("1 +").unwrap_err();
        assert!(err.to_string().contains(':'), "{err}");
    }
}
