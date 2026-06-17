// `resume` CLI subcommand. Phase 39.D of the v2 rollout,
// extended in Phase 39.E with a `--json` flag for stdout
// pipeability.
//
// `./chronicle resume <path>` is the round-trip partner of
// Phase 39.C's `save` subcommand: it reads a SaveGame JSON from
// disk via state.SaveGame.Unmarshal's canonical decoder — which
// DOES run §39.B's load-time version gate and §39.A's
// DisallowUnknownFields tamper-resistance — and reports the
// rehydrated world's identity on stderr for diff-comparable
// inspection.
//
// The "demo-drops the player at WorldState.CurrentNodeID" promise
// in the §39.D row is implemented at the CLI surface today as a
// stderr-reprinted CurrentNodeID. The actual gameplay binding
// lands in §40.B's PlaythroughPath tests when the
// internal/engine Runner goes live. Until then, `resume` is a
// faithful state-reader.
//
// Phase 39.E added the `--json` flag: when set, the loaded
// SaveGame is emitted to stdout as canonical JSON (reuses
// state.SaveGame.Marshal's §39.A sorted-keys encoding), so the
// binary composes cleanly in shell pipelines:
//
//	chronicle resume foo.json --json | jq .WorldHash
//
// The diagnostic lines (loaded-n-bytes, Version, Protagonist,
// CurrentNodeID, WorldHash) stay on stderr in --json mode; they
// don't pollute stdout. A user wanting only the JSON pipes
// stdout and ignores stderr.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// splitFlagsPositional partitions argv into flag-args (anything
// starting with "-") and positional args. We do this BEFORE
// passing to flag.FlagSet.Parse because stdlib Parse stops at the
// first non-flag arg, which precludes the user-friendly ordering
// `chronicle resume <path> --json`. Unknown flags are still routed
// to flag-args so fs.Parse produces the standard error message.
func splitFlagsPositional(argv []string) (flags, positional []string) {
	for _, a := range argv {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}
	return flags, positional
}

// runResume implements the `resume` subcommand. argv is the
// post-subcommand argument vector (e.g., for
// `./chronicle resume --json foo.json`, argv is
// []string{"--json", "foo.json"}). stdout receives the optional
// --json payload. stderr receives Progress + post-load identity.
func runResume(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false,
		"emit the loaded SaveGame as canonical JSON to stdout "+
			"(e.g., `chronicle resume foo.json --json | jq .WorldHash`)")

	// Split argv into flag-args + positional BEFORE handing off to
	// fs.Parse. stdlib flag.Parse stops at the first non-flag arg,
	// so `chronicle resume foo.json --json` would otherwise treat
	// `--json` as positional (failure). Splitting first lets flags
	// appear anywhere in argv, which matches the user's mental
	// model and the worked example in the --json help string.
	flagArgs, positional := splitFlagsPositional(argv)
	if err := fs.Parse(flagArgs); err != nil {
		return fmt.Errorf("resume: parse flags: %w", err)
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "chronicle resume: usage: chronicle resume [--json] <path-to-savegame.json>")
		return fmt.Errorf("resume: expected exactly one <path> argument; got %d", len(positional))
	}
	path := positional[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("resume: read %q: %w", path, err)
	}

	// Decode via the canonical SaveGame.Unmarshal, which in
	// sequence runs: §39.A DisallowUnknownFields, json type
	// validation, and the §39.B version gate. §18A round-trip
	// invariance ensures the printed WorldHash matches the
	// matching `save` invocation's hash (assuming no in-flight
	// tampering).
	var sg state.SaveGame
	if err := sg.Unmarshal(data); err != nil {
		return fmt.Errorf("resume: parse %q as SaveGame: %w", path, err)
	}

	// Optional stdout JSON output (--json). Emitted BEFORE the
	// stderr lines so a downstream jq sees a complete document
	// even if the program exits after stderr.
	if jsonOut {
		encoded, err := sg.Marshal()
		if err != nil {
			return fmt.Errorf("resume: re-encode loaded SaveGame for --json: %w", err)
		}
		if _, err := stdout.Write(encoded); err != nil {
			return fmt.Errorf("resume: write --json to stdout: %w", err)
		}
	}

	// Post-load identity: bytes count, Version, Protagonist
	// (when non-empty), CurrentNodeID (when non-empty), stable
	// WorldHash. stderr-only.
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
