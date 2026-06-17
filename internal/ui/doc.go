// Package ui renders the v2 choice menu per ARCHITECTURE.md §23
// (Render Story, Render Choices, Player Selection) and §1's
// "The player never types commands" principle.
//
// Phase 36.F lands two files:
//
//   - cli.go       BufferRenderer — writes Render/RenderChoices
//                  output to an injected io.Writer (used by engine
//                  tests). BufferRenderer does NOT implement the
//                  "select a choice" step; that is
//                  engine.Engine.ChoiceProvider's job
//                  (internal/engine/engine.go).
//
//   - cli_tty.go   TTYRenderer — the production choice-menu
//                  renderer. Word-wraps prose at WrapWidth
//                  (default 80), emits ANSI codes when
//                  AnsiEnabled, prints a phase-style `# {Title}`
//                  heading matching the README sample session
//                  ("# EX01 — The frontier town of Ashwick"),
//                  offers a numeric `>` prompt (PromptChoice),
//                  and a press-any-key reader (PressAnyKey)
//                  between nodes. TTYRenderer's input/output
//                  are both injectable via NewTTYRendererWithIO;
//                  tests bind a *bytes.Buffer for stdout
//                  capture and a strings.Reader for stdin.
//
// Locked choices are filtered out by Runner.Step via
// story.AvailableChoices before RenderChoices runs (per §7:
// "Hidden choices are not displayed"). RenderChoices
// receives only the available (Conditions-pass) list.
//
// There is NO free-text interpretation. Any input that
// isn't a numeric choice is re-prompted by PromptChoice.
// This honours §1: "The player never types commands.
// The player selects from available choices."
package ui
