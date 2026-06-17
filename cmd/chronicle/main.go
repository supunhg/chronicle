// Command chronicle is the v2 entry point.
//
// Phase 36 lands the v2 module scaffold (internal/{engine, story,
// state, events, endings, content, ui}). This binary loads the v2
// tree via blank imports so any build breakage in those packages
// surfaces at `go build` time even before subcommands exist.
//
// Actual CLI subcommands will land in subsequent sub-phases:
//
//   - Phase 36.A  internal/engine StoryEngine (runner.go) implements §23.
//   - Phase 36.B  internal/story types (StoryNode, Choice, Condition, Effect).
//   - Phase 36.C  internal/state types (WorldState, SaveGame, JSON I/O).
//   - Phase 36.D  internal/events triggered events + internal/endings eval.
//   - Phase 36.E  internal/content YAML loader (content/ → tree).
//   - Phase 36.F  internal/ui/cli.go choice-menu renderer + scanner.
//   - Phase 39.A-E save / resume / info / diff subcommands.
//   - Phase 40.A-E Definition-of-Done gates (per ARCHITECTURE.md §25).
//
// The v1 simulation stack (sqlite persistence, simulation engines,
// LLM-first narration, intent parser, REPL) is retired per Phase 35
// (commits 166694d, c5fcb10, 9b0a1d3, 0606088, 991941e).
//
// See ARCHITECTURE.md §22 (Engine Architecture), §23 (Runtime Flow),
// chronicle-v2-pivot-spec.md, and PHASES.md for the binding plan.
package main

import (
	"fmt"
	"os"

	// v2 module scaffold — blank imports so build breakage of any v2
	// package surfaces at `go build` time even before subcommands
	// exist. These are side-effect-only until 36.A–36.F land real
	// types and engine wiring. See internal/{content,engine,...}/doc.go
	// in each respective package for the sub-phase that owns each one.
	_ "github.com/chronicle-dev/chronicle/internal/content"
	_ "github.com/chronicle-dev/chronicle/internal/engine"
	_ "github.com/chronicle-dev/chronicle/internal/endings"
	_ "github.com/chronicle-dev/chronicle/internal/events"
	_ "github.com/chronicle-dev/chronicle/internal/state"
	_ "github.com/chronicle-dev/chronicle/internal/story"
	_ "github.com/chronicle-dev/chronicle/internal/ui"
)

const version = "2.0.0-dev"

func main() {
	fmt.Fprintf(os.Stderr, "chronicle v%s\n", version)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Phase 36 v2 module scaffold landed.")
	fmt.Fprintln(os.Stderr, "7 v2 packages compiled cleanly: engine, story, state,")
	fmt.Fprintln(os.Stderr, "events, endings, content, ui.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "v2 CLI subcommands not yet implemented. Roadmap:")
	fmt.Fprintln(os.Stderr, "  Phase 36.A  internal/engine StoryEngine — runner.go (§23).")
	fmt.Fprintln(os.Stderr, "  Phase 36.B–E  story / state / events / endings / content types.")
	fmt.Fprintln(os.Stderr, "  Phase 36.F  internal/ui/cli.go (choice-menu renderer).")
	fmt.Fprintln(os.Stderr, "  Phase 39.A–E  save / resume / info / diff CLI.")
	fmt.Fprintln(os.Stderr, "  Phase 40.A–E  v2 acceptance gates (ARCHITECTURE.md §25).")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "v1 simulation stack retired in Phase 35 (35.A–35.F). See")
	fmt.Fprintln(os.Stderr, "PHASES.md for the rollout order and ARCHITECTURE.md for v2.")
}
