// Command chronicle is the v2 entry point.
//
// Phase 36 lands the v2 module scaffold (internal/{engine, story,
// state, events, endings, content, ui}). This binary loads the v2
// tree via blank imports so any build breakage in those packages
// surfaces at `go build` time even before subcommands exist.
//
// Phase 39 lands the persistence CLI. Subcommands dispatch on
// os.Args[1] (no cobra/urfave dependency — stdlib flag is
// sufficient for the current surface):
//
//   - `save`         — write a SaveGame JSON to disk. §39.C.
//   - `resume`       — rehydrate a SaveGame, drop the player at
//                      CurrentNodeID. (lands in §39.D.)
//   - `info / diff`  — read-only inspection of saves. (lands in
//                      §39.E.)
//   - default        — print roadmap + exit 0.
//
// The v1 simulation stack (sqlite persistence, simulation
// engines, LLM-first narration, intent parser, REPL) is retired
// per Phase 35.
package main

import (
	"fmt"
	"io"
	"os"

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
	args := os.Args[1:]
	if len(args) == 0 {
		printRoadmap(os.Stderr)
		return
	}
	switch args[0] {
	case "save":
		if err := runSave(args[1:], os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Fprintf(os.Stdout, "chronicle v%s\n", version)
	case "help", "--help", "-h":
		printHelp(os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "chronicle: unknown subcommand %q\n\n", args[0])
		printHelp(os.Stderr)
		os.Exit(1)
	}
}

func printRoadmap(w io.Writer) {
	fmt.Fprintf(w, "chronicle v%s\n", version)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Available v2 CLI subcommands:")
	fmt.Fprintln(w, "  save       write a SaveGame JSON to disk (-out, -from, §39.C)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Use `chronicle help` for the full command list. Coming in")
	fmt.Fprintln(w, "later phases: resume (§39.D), info/diff (§39.E).")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "v2 module scaffold verified at build time via blank imports")
	fmt.Fprintln(w, "of internal/{engine, story, state, events, endings, content,")
	fmt.Fprintln(w, "ui}. See PHASES.md for the rollout order.")
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, "chronicle v%s\n\n", version)
	fmt.Fprintln(w, "Usage: chronicle <subcommand> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  save        write a SaveGame JSON to disk")
	fmt.Fprintln(w, "  version     print the Chronicle version")
	fmt.Fprintln(w, "  help        print this message")
}
