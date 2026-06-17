// `resume` CLI subcommand. Phase 39.D of the v2 rollout.
//
// `./chronicle resume <path>` is the round-trip partner of Phase 39.C's
// `save` subcommand: it reads a SaveGame JSON from disk via
// state.SaveGame.Unmarshal's canonical decoder — which DOES run §39.B's
// load-time version gate and §39.A's DisallowUnknownFields
// tamper-resistance — and reports the rehydrated world's identity on
// stderr for diff-comparable inspection.
//
// The "demo-drops the player at WorldState.CurrentNodeID" promise in
// the §39.D row is implemented at the CLI surface today as a
// stderr-reprinted CurrentNodeID. The actual gameplay binding lands in
// §40.B's PlaythroughPath tests when the internal/engine Runner goes
// live. Until then, `resume` is a faithful state-reader.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// runResume implements the `resume` subcommand. argv is the
// post-subcommand argument vector (e.g., for
// `./chronicle resume foo.json`, argv is []string{"foo.json"}).
// stderr receives Progress + post-load identity.
func runResume(argv []string, stderr io.Writer) error {
	if len(argv) != 1 {
		fmt.Fprintln(stderr, "chronicle resume: usage: chronicle resume <path-to-savegame.json>")
		return fmt.Errorf("resume: expected exactly one <path> argument; got %d", len(argv))
	}
	path := argv[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("resume: read %q: %w", path, err)
	}

	// Decode via the canonical SaveGame.Unmarshal, which in sequence
	// runs: §39.A DisallowUnknownFields, json type validation, and the
	// §39.B version gate. §18A round-trip invariance ensures the
	// printed WorldHash matches the matching `save` invocation's
	// hash (assuming no in-flight tampering).
	var sg state.SaveGame
	if err := sg.Unmarshal(data); err != nil {
		return fmt.Errorf("resume: parse %q as SaveGame: %w", path, err)
	}

	// Post-load identity: bytes count, Version, Protagonist (when
	// non-empty), CurrentNodeID (when non-empty), stable WorldHash.
	// stderr-only; stdout stays empty so the binary composes cleanly
	// in pipes (Phase 39.E will surface JSON to stdout via a flag).
	fmt.Fprintf(stderr, "resume: loaded %d bytes from %s\n", len(data), path)
	fmt.Fprintf(stderr, "resume: Version=%d\n", sg.Version)
	if sg.WorldState.Protagonist != "" {
		fmt.Fprintf(stderr, "resume: Protagonist=%s\n", sg.WorldState.Protagonist)
	}
	if sg.WorldState.CurrentNodeID != "" {
		fmt.Fprintf(stderr, "resume: CurrentNodeID=%s\n", sg.WorldState.CurrentNodeID)
	}
	fmt.Fprintf(stderr, "resume: WorldHash=%s\n", state.WorldHash(sg))
	return nil
}
