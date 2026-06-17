// Command chronicle is the v2 entry point.
//
// As of Phase 35.B (v1 engine retirement), the v1 CLI has been
// retired. The seven subcommands (new / save / resume / info /
// diff / doctor / play) all depended on the v1 engine stack —
// SQLite snapshot persistence via worldpack bootstrap, simulation
// engines (population / relationship / memory / goal), LLM-first
// narration, intent parser, REPL — all gone.
//
// Phase 36 will land the v2 CLI against the new module tree
// (internal/{engine, story, state, events, endings, content, ui})
// per ARCHITECTURE.md §22. The current binary is a stub that
// prints a banner explaining the state of the project.
//
// See ARCHITECTURE.md §23 (Runtime Flow), §22 (Engine
// Architecture), and chronicle-v2-pivot-spec.md for the binding
// retirement plan.
package main

import (
	"fmt"
	"os"
)

const version = "2.0.0-dev"

func main() {
	fmt.Fprintf(os.Stderr, "chronicle v%s\n", version)
	fmt.Fprintln(os.Stderr, "Phase 35.B stub: v1 CLI retired; v2 CLI not yet implemented.")
	fmt.Fprintln(os.Stderr, "See ARCHITECTURE.md §22-§23 and chronicle-v2-pivot-spec.md for the v2 target.")
}
