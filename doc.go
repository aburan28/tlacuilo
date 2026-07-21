// Package tlacuilo is a pure-Go toolkit for TLA+.
//
// Tlacuilo (Nahuatl: "scribe") lets Go programs write, parse, format,
// and model-check TLA+ specifications:
//
//   - builder: fluent construction of specs from Go code
//   - parser / scanner / token / ast: a column-aware TLA+ parser (junction
//     lists, the full operator table) and a canonical pretty-printer
//   - cfg: TLC configuration files, generated and parsed
//   - tlc: run the TLC model checker and consume its tool-mode output as
//     typed results
//   - trace / value: counterexample traces and TLA+ values, with ITF
//     (Informal Trace Format) JSON interchange
//
// See docs/PLAN.md in the repository for the design document.
package tlacuilo
