// `save` CLI subcommand. Phase 39.C of the v2 rollout.
//
// `./chronicle save -out myrun.json` writes a SaveGame (per
// ARCHITECTURE.md §18 + §18A invariants) to disk via the
// canonical sorted-key + json.Number-preserving Marshal from
// internal/state. The "current world state" is sourced from
// either:
//
//   - `-from <path>` — a JSON file containing a bare WorldState
//     object (NOT wrapped in SaveGame). This is the round-trip
//     bridge to `-resume <path>` and to fixture-loading for
//     test playthroughs.
//
//   - no `-from` flag — a fresh NewWorldState (every map/slice
//     initialised to its empty form; CurrentNodeID un-set).
//
// Output path is required via `-out`. Save is fail-fast: missing
// `-out`, missing/unreadable `-from`, malformed -from, or any
// Marshal/WriteFile error returns non-zero.
//
// On success, two lines are written to stderr:
//   save: wrote N bytes to <path>
//   save: WorldHash=<64-hex-digits>
// The WorldHash is the SHA-256 of the canonical-encoded SaveGame (§39.A);
// round-tripping through `-resume <path>` (§39.D) and re-hashing
// must produce the same value.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// runSave implements the `save` subcommand. argv is the post-subcommand
// argument vector (e.g., for `./chronicle save -out foo.json`, argv
// is []string{"-out", "foo.json"}). stderr receives Progress + WorldHash.
//
// runSave separates flag parsing (returns flag.ContinueOnError) from
// the actual save flow so the unit tests in save_test.go can drive
// argv directly without going through os.Args.
func runSave(argv []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("save", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("out", "", "output save file path (required)")
	fromPath := fs.String("from", "", "optional WorldState JSON source file (default: empty NewWorldState)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *outPath == "" {
		fmt.Fprintln(stderr, "chronicle save: -out <path> is required.")
		fmt.Fprintln(stderr, "usage: chronicle save -out <path> [-from <ws.json>]")
		return errMissingOut
	}

	ws, err := loadWorldStateForSave(*fromPath, stderr)
	if err != nil {
		return err
	}

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: ws}
	data, err := sg.Marshal()
	if err != nil {
		return fmt.Errorf("save: marshal: %w", err)
	}
	if err := os.WriteFile(*outPath, data, 0600); err != nil {
		return fmt.Errorf("save: write %q: %w", *outPath, err)
	}

	fmt.Fprintf(stderr, "save: wrote %d bytes to %s\n", len(data), *outPath)
	fmt.Fprintf(stderr, "save: WorldHash=%s\n", state.WorldHash(sg))
	return nil
}

// errMissingOut is the typed sentinel returned when -out is absent.
// Distinct from a flag-parse error so callers can distinguish
// "user typo'd a flag" from "user forgot -out".
var errMissingOut = fmt.Errorf("save: -out flag is required")

// loadWorldStateForSave resolves the WorldState backing the save.
// A blank -from path uses NewWorldState; a populated path reads a
// JSON file containing a bare WorldState JSON object -- deliberately
// NOT a SaveGame wrapper, because `resume` (§39.D) is the subcommand
// that handles SaveGame-with-version files. `save -from` is for
// bare-WorldState fixtures emitted by Phase-38 YAML loaders and
// future engine runners. Map fields are ensured non-nil to satisfy
// the invariant that "writes to nil maps panic" never happens
// downstream (NewWorldState's empty init is reused for the -from
// path so the load-time field population is consistent).
func loadWorldStateForSave(fromPath string, stderr io.Writer) (state.WorldState, error) {
	if fromPath == "" {
		return state.NewWorldState(), nil
	}
	data, err := os.ReadFile(fromPath)
	if err != nil {
		return state.WorldState{}, fmt.Errorf("save: read -from %q: %w", fromPath, err)
	}
	var ws state.WorldState
	if err := json.Unmarshal(data, &ws); err != nil {
		return state.WorldState{}, fmt.Errorf("save: parse -from %q as WorldState JSON: %w", fromPath, err)
	}
	if ws.Flags == nil {
		ws.Flags = make(map[string]bool)
	}
	if ws.Variables == nil {
		ws.Variables = make(map[string]int)
	}
	if ws.Relationships == nil {
		ws.Relationships = make(map[string]state.Relationship)
	}
	if ws.Inventory.Items == nil {
		ws.Inventory.Items = make(map[string]int)
	}
	fmt.Fprintf(stderr, "save: loaded WorldState from %s\n", fromPath)
	return ws, nil
}
