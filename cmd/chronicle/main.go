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
//                      CurrentNodeID. §39.D. With `--json`,
//                      emits the canonical SaveGame to stdout
//                      for shell piping. §39.E.
//   - `info`         — print a single-save human-readable
//                      summary to stdout. §39.E follow-up #1.
//   - `diff`         — compare two saves; print field-level
//                      discrepancies. Exit 1 if any differences
//                      found (standard `diff` semantics);
//                      exit 2 if either file fails to load.
//                      §39.E follow-up #1.
//   - default        — print roadmap + exit 0.
//
// The v1 simulation stack (sqlite persistence, simulation
// engines, LLM-first narration, intent parser, REPL) is retired
// per Phase 35.
package main

import (
	"errors"
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
	case "resume":
		if err := runResume(args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "info":
		if err := runInfo(args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "diff":
		if err := runDiff(args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			// diff follows standard `diff` exit semantics: 1 if
			// differences found (errDiffFound sentinel), 2 if a
			// load/parse error tripped before comparison could
			// complete. Wrapped load errors come back as their
			// own non-nil type and fall through to 2.
			if errors.Is(err, errDiffFound) {
				os.Exit(1)
			}
			os.Exit(2)
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

func printRoadmap(w io.Writer) {		fmt.Fprintf(w, "chronicle v%s\n", version)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Available v2 CLI subcommands:")
	fmt.Fprintln(w, "  save       write a SaveGame JSON to disk (-out, -from, §39.C)")
	fmt.Fprintln(w, "  resume     rehydrate a SaveGame from disk; --json (§39.E)")
	fmt.Fprintln(w, "             emits canonical SaveGame to stdout")
	fmt.Fprintln(w, "  info       print a single-save summary to stdout")
	fmt.Fprintln(w, "             (§39.E follow-up #1)")
	fmt.Fprintln(w, "  diff       compare two saves; exit 1 if differences")
	fmt.Fprintln(w, "             (§39.E follow-up #1)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Use `chronicle help` for the full command list. Coming in")
	fmt.Fprintln(w, "later phases: engine runner (§40), persistence drop")
	fmt.Fprintln(w, "(§39.F).")
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
	fmt.Fprintln(w, "  save        write a SaveGame JSON to disk (-out, -from)")
	fmt.Fprintln(w, "  resume      rehydrate a SaveGame from disk; --json emits the")
	fmt.Fprintln(w, "              canonical SaveGame to stdout for piping")
	fmt.Fprintln(w, "  info        print a single-save summary to stdout")
	fmt.Fprintln(w, "  diff        compare two saves; exit 1 if differences")
	fmt.Fprintln(w, "  version     print the Chronicle version")
	fmt.Fprintln(w, "  help        print this message")
}
