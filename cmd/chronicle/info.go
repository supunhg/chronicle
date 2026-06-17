// `info` CLI subcommand. §39.E follow-up #1.
//
// `./chronicle info <path>` prints a human-readable summary of a
// SaveGame JSON to stdout — the identity + structural counts the
// player cares about: path, Version, WorldHash, Protagonist,
// CurrentNodeID, Tick, flag-count, variable-count, plus the
// relationship/inventory/party counts and a Reputation breakdown.
//
// Reads via os.ReadFile + state.SaveGame.Unmarshal, so §39.A's
// DisallowUnknownFields and §39.B's load-time version gate fire
// here the same way they fire in `resume` — info reads the save
// the same way a runtime loader would. Errors are surfaced on
// stderr; success output goes to stdout so pipes work
// (`chronicle info foo.json | head -3`).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// runInfo implements the `info` subcommand. argv is the
// post-subcommand argument vector (e.g., for
// `./chronicle info foo.json`, argv is []string{"foo.json"}).
// info takes no flags today; flag-args in argv are rejected so
// `--json` etc. don't accidentally route to the wrong reader.
// stdout receives the summary; stderr receives read/parse errors.
func runInfo(argv []string, stdout, stderr io.Writer) error {
	flagArgs, positional := splitFlagsPositional(argv)
	if len(flagArgs) != 0 {
		return fmt.Errorf("info: unrecognized flag %q (no flags today)", flagArgs[0])
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "chronicle info: usage: chronicle info <path-to-savegame.json>")
		return fmt.Errorf("info: expected exactly one <path> argument; got %d", len(positional))
	}
	path := positional[0]
	sg, err := readSaveForInspection(path)
	if err != nil {
		return fmt.Errorf("info: %w", err)
	}

	// Stdout summary. Each line is `chronicle info: <field>=<value>`
	// so a downstream `grep` lifts fields cleanly.
	fmt.Fprintf(stdout, "chronicle info: path=%s\n", path)
	fmt.Fprintf(stdout, "chronicle info: Version=%d\n", sg.Version)
	fmt.Fprintf(stdout, "chronicle info: Tick=%d\n", sg.WorldState.Tick)
	fmt.Fprintf(stdout, "chronicle info: WorldHash=%s\n", state.WorldHash(sg))
	if sg.WorldState.Protagonist != "" {
		fmt.Fprintf(stdout, "chronicle info: Protagonist=%s\n", sg.WorldState.Protagonist)
	}
	if sg.WorldState.CurrentNodeID != "" {
		fmt.Fprintf(stdout, "chronicle info: CurrentNodeID=%s\n", sg.WorldState.CurrentNodeID)
	}
	fmt.Fprintf(stdout, "chronicle info: flags=%d\n", len(sg.WorldState.Flags))
	fmt.Fprintf(stdout, "chronicle info: variables=%d\n", len(sg.WorldState.Variables))
	fmt.Fprintf(stdout, "chronicle info: relationships=%d\n", len(sg.WorldState.Relationships))
	fmt.Fprintf(stdout, "chronicle info: inventory-items=%d\n", len(sg.WorldState.Inventory.Items))
	fmt.Fprintf(stdout, "chronicle info: party=%d\n", len(sg.WorldState.Party))
	fmt.Fprintf(stdout, "chronicle info: endings-unlocked=%d\n", len(sg.WorldState.EndingsUnlocked))
	// Reputation breakdown on one line so the line-format stays
	// compact for `grep` filtering.
	fmt.Fprintf(stdout,
		"chronicle info: reputation=kingdom=%d mages=%d dragons=%d underworld=%d\n",
		sg.WorldState.Reputation.Kingdom,
		sg.WorldState.Reputation.Mages,
		sg.WorldState.Reputation.Dragons,
		sg.WorldState.Reputation.Underworld,
	)
	return nil
}

// readSaveForInspection reads a SaveGame from disk for read-only
// inspection (info, diff). It runs the same load-time checks the
// runtime loader would (DisallowUnknownFields + version gate), so
// an inspector never sees a half-decoded or version-mismatched
// save — exactly what a §39.A/§39.B reader must guarantee.
//
// Shared by runInfo + runDiff. runResume doesn't reuse this
// helper because resume walks the same read path and then emits
// its own stderr diagnostic identity; the asymmetry is in WHO
// reads, not WHAT.
func readSaveForInspection(path string) (state.SaveGame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return state.SaveGame{}, fmt.Errorf("read %q: %w", path, err)
	}
	var sg state.SaveGame
	if err := sg.Unmarshal(data); err != nil {
		return state.SaveGame{}, fmt.Errorf("parse %q as SaveGame: %w", path, err)
	}
	return sg, nil
}
