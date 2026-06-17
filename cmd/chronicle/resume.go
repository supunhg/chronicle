// `resume` CLI subcommand. Phase 39.D of the v2 rollout.
//
// `./chronicle resume <path>` is the round-trip partner of Phase 39.C's
// `save` subcommand: it reads a SaveGame JSON from disk via
// state.SaveGame.Unmarshal's canonical decoder — which DOES run §39.B's
// load-time version gate (current-version match required) and §39.A's
// DisallowUnknownFields tamper-resistance — and reports the rehydrated
// world's identity on stderr for diff-comparable inspection.
//
// The "demo-drops the player at WorldState.CurrentNodeID" promise in
// the §39.D row is implemented at the CLI surface today as a
// stderr-reprinted CurrentNodeID. The actual gameplay binding —
// wiring CurrentNodeID into the engine's StoryNode lookup and stepping
// forward — lands in §40.B's PlaythroughPath tests when the
// internal/engine Runner goes live. Until then, `resume` is a
// faithful state-reader: it produces the same WorldHash the matching
// `save` invocation emitted (the §18A byte-stable round-trip
// invariant), and prints Protagonist / CurrentNodeID / WorldHash to
// stderr.
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
//
// resume intentionally takes exactly one positional argument (the
// path). Mirroring save's flag set makes little sense: there is no
// output file (the file is already on disk), and no "from" input
// (the world state IS the input).
func runResume(argv []string, stderr io.Writer) error {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "chronicle resume: <path> is required.")
		fmt.Fprintln(stderr, "usage: chronicle resume <path-to-savegame.json>")
		return errMissingPath
	}
	if len(argv) > 1 {
		fmt.Fprintf(stderr, "chronicle resume: unexpected extra args: %v\n", argv[1:])
		fmt.Fprintln(stderr, "usage: chronicle resume <path-to-savegame.json>")
		return errTooManyArgs
	}
	path := argv[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("resume: read %q: %w", path, err)
	}

	// Decode via the canonical SaveGame.Unmarshal. Three checks happen
	// here in sequence:
	//   - DisallowUnknownFields (§39.A tamper-resistance, "section 1"):
	//     any unknown top-level or nested field fails the decode.
	//   - json type validation: a non-numeric Version string fails as
	//     BadType.
	//   - Version gate (§39.B invariant #3): mismatch with
	//     state.CurrentVersion fails the load with sided remediation
	//     message ("older Chronicle build...no backward migration" or
	//     "newer Chronicle build...update Chronicle to load it").
	//
	// §18A's byte-stable round-trip invariant ensures the WorldHash
	// printed here matches what the matching `save` invocation emitted
	// (assuming no in-flight tampering).
	var sg state.SaveGame
	if err := sg.Unmarshal(data); err != nil {
		return fmt.Errorf("resume: parse %q as SaveGame: %w", path, err)
	}

	// Post-load identity: bytes count, Protagonist, CurrentNodeID,
	// stable WorldHash. These are stderr-only; stdout stays empty so
	// the binary composes cleanly in pipes (Phase 39.E will surface
	// JSON to stdout via a separate flag).
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

// errMissingPath and errTooManyArgs are typed sentinels for the
// arg-count gates. They mirror §39.C's errMissingOut so callers can
// distinguish "user forgot the path" from "user typo'd a flag" (the
// latter doesn't apply here because resume takes no flags, but we
// keep the pattern uniform across both subcommands).
var (
	errMissingPath  = fmt.Errorf("resume: <path> argument is required")
	errTooManyArgs  = fmt.Errorf("resume: accepts exactly one <path> argument")
)
