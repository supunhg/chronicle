// Package repl is a v2 placeholder.
//
// As of Phase 35.B (v1 engine retirement), the v1 REPL has been
// retired. Parsing (internal/intent) and narration (internal/narrator)
// are both deleted, and so are the v1 REPL verbs (look, walk, talk,
// search, pray, etc.) — v2 has a pure choice menu per D6.
//
// Phase 36 will replace this shell with internal/ui/cli.go: a
// choice-menu renderer that reads numeric choices and never
// interprets free text. This stub remains so the v1-derived
// package tree still compiles after the v1 engine retirement.
//
// See ARCHITECTURE.md §23 (Runtime Flow) and D6 in
// chronicle-v2-pivot-spec.md.
package repl
