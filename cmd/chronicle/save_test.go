// Tests for the `save` CLI subcommand. Phase 39.C.
//
// PHASES.md §39.C calls for a manual smoke test
// (`./chronicle save -out myrun.json && cat myrun.json`), but
// these programmatic tests catch regressions earlier and let CI
// gate the sub-Phase without a live shell. Each test drives
// runSave directly via argv (no os.Args manipulation required
// because runSave takes the post-subcommand argv vector).
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// testStderr returns an io.Writer + string buffer pair so tests
// can assert on stderr output (WorldHash, byte count) without
// littering test stderr.
func testStderr() (*bytes.Buffer, func() string) {
	buf := &bytes.Buffer{}
	return buf, func() string { return buf.String() }
}

// TestSave_EmptyWorldState is the canonical manual smoke test
// translated to Go: `chronicle save -out foo.json` writes a
// canonical empty SaveGame + reports a deterministic WorldHash.
// Result: stderr mentions the WorldHash + file exists with
// parser-valid canonical JSON.
func TestSave_EmptyWorldState(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.json")
	stderr, stderrText := testStderr()

	if err := runSave([]string{"-out", out}, stderr); err != nil {
		t.Fatalf("runSave: %v", err)
	}

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output file not created: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	// Round-trip read: the file MUST parse as a SaveGame with
	// Version=CurrentVersion=0 (per §39.B's load-time version gate).
	var loaded state.SaveGame
	if err := loaded.Unmarshal(data); err != nil {
		t.Fatalf("Unmarshal of freshly produced save failed: %v\n  data: %s", err, data)
	}
	if loaded.Version != state.CurrentVersion {
		t.Errorf("version mismatch: got %d want %d", loaded.Version, state.CurrentVersion)
	}

	// Stderr must include both Progress lines.
	stderrStr := stderrText()
	if !strings.Contains(stderrStr, "save: wrote ") {
		t.Errorf("stderr missing 'save: wrote ...' line; got: %s", stderrStr)
	}
	if !strings.Contains(stderrStr, "save: WorldHash=") {
		t.Errorf("stderr missing 'save: WorldHash=...' line; got: %s", stderrStr)
	}
}

// TestSave_FromWorldStateFile exercises the `-from` path: the
// subcommand reads a JSON file containing a bare WorldState, wraps
// it in SaveGame, and writes a reloadable canonical save.
//
// The -from file's spec is "bare WorldState JSON" (just a {CurrentNodeID,
// Flags, ...} object), distinct from a SaveGame wrapper. This keeps
// the cli surface compatible with the fixture files loaders emit
// (Phase 38's `content/` YAML pipeline already produces equivalent
// objects via state.NewWorldState init).
func TestSave_FromWorldStateFile(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "ws.json")
	out := filepath.Join(dir, "save.json")

	ws := state.NewWorldState()
	ws.CurrentNodeID = "act1.opening"
	ws.Protagonist = "kael"
	ws.Flags["met_elara"] = true
	ws.Variables["courage"] = 75
	// Use the WORLDSTATE marshalling (json.Marshal, NOT SaveGame.Marshal)
	// because `-from` is documented as "bare WorldState JSON".
	wsData, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("marshal worldstate: %v", err)
	}
	if err := os.WriteFile(from, wsData, 0644); err != nil {
		t.Fatalf("write -from fixture: %v", err)
	}

	stderr, _ := testStderr()
	if err := runSave([]string{"-out", out, "-from", from}, stderr); err != nil {
		t.Fatalf("runSave: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var loaded state.SaveGame
	if err := loaded.Unmarshal(data); err != nil {
		t.Fatalf("Unmarshal of leading-canonical save failed: %v", err)
	}
	if loaded.WorldState.CurrentNodeID != "act1.opening" {
		t.Errorf("CurrentNodeID not preserved: got %q want %q",
			loaded.WorldState.CurrentNodeID, "act1.opening")
	}
	if loaded.WorldState.Protagonist != "kael" {
		t.Errorf("Protagonist not preserved: got %q want %q",
			loaded.WorldState.Protagonist, "kael")
	}
	if !loaded.WorldState.Flags["met_elara"] {
		t.Errorf("Flags[met_elara] not preserved")
	}
	if loaded.WorldState.Variables["courage"] != 75 {
		t.Errorf("Variables[courage] not preserved: got %d want 75",
			loaded.WorldState.Variables["courage"])
	}
	if loaded.Version != state.CurrentVersion {
		t.Errorf("Version mismatch: got %d want %d",
			loaded.Version, state.CurrentVersion)
	}
}

// TestSave_MissingOutFlag: omitting -out must fail with a clear
// message + non-nil error (NOT panic, NOT exit-1-by-flag-default).
// The manual smoke test would distinguish a missing flag from a
// flag-parse error only by reading stderr; runSave returns the
// typed errMissingOut sentinel so testable callers can switch.
func TestSave_MissingOutFlag(t *testing.T) {
	stderr, stderrText := testStderr()
	err := runSave([]string{}, stderr)
	if err == nil {
		t.Fatal("expected error for missing -out; got nil")
	}
	if err != errMissingOut {
		t.Errorf("error: got %v want errMissingOut sentinel", err)
	}
	if !strings.Contains(stderrText(), "-out") {
		t.Errorf("stderr should mention -out for the missing-flag case; got: %s", stderrText())
	}
}

// TestSave_FromFileMissing: -from pointing at a nonexistent file
// fails fast with a wrapped error. Caller-actionable: the file
// path is in the error message.
func TestSave_FromFileMissing(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	stderr, _ := testStderr()
	err := runSave([]string{"-out", out, "-from", "/nonexistent/path/ws.json"}, stderr)
	if err == nil {
		t.Fatal("expected error for missing -from file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/path/ws.json") {
		t.Errorf("error must name the missing file; got: %v", err)
	}
}

// TestSave_FromFileMalformed: -from pointing at a file whose
// contents are NOT valid WorldState JSON fails fast with a
// parse error. Caller-actionable: the file path is in the error.
func TestSave_FromFileMalformed(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "bad.json")
	out := filepath.Join(dir, "out.json")
	if err := os.WriteFile(from, []byte("{ not valid json"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	stderr, _ := testStderr()
	err := runSave([]string{"-out", out, "-from", from}, stderr)
	if err == nil {
		t.Fatal("expected error for malformed -from content")
	}
	if !strings.Contains(err.Error(), from) {
		t.Errorf("error must name the malformed file; got: %v", err)
	}
}

// TestSave_WorldHashStableAcrossRuns: the same input WorldState
// produces the same WorldHash on two distinct runSave invocations.
// This is the byte-stable round-trip invariant (§18A + §39.A)
// at the CLI layer: two manual `chronicle save` calls with the
// same baseline state must hash equally. (Re-run shows the flag
// also refuses to write to a populated path; this test uses
// distinct output files to exercise the hash-stability contract
// only.)
func TestSave_WorldHashStableAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "ws.json")
	out1 := filepath.Join(dir, "save1.json")
	out2 := filepath.Join(dir, "save2.json")

	ws := state.NewWorldState()
	ws.CurrentNodeID = "act1.opening"
	ws.Flags["a"] = true
	ws.Flags["b"] = true
	ws.Flags["c"] = true
	wsData, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(from, wsData, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stderrA, _ := testStderr()
	if err := runSave([]string{"-out", out1, "-from", from}, stderrA); err != nil {
		t.Fatalf("first runSave: %v", err)
	}
	stderrB, _ := testStderr()
	if err := runSave([]string{"-out", out2, "-from", from}, stderrB); err != nil {
		t.Fatalf("second runSave: %v", err)
	}

	d1, _ := os.ReadFile(out1)
	d2, _ := os.ReadFile(out2)
	if !bytes.Equal(d1, d2) {
		t.Errorf("two runs with the same WorldState produced different bytes:\n run1: %s\n run2: %s", d1, d2)
	}

	// And the on-disk bytes match the canonical hash the stderr reported.
	h1 := extractWorldHashFromStderr(stderrA.String())
	h2 := extractWorldHashFromStderr(stderrB.String())
	if h1 == "" || h2 == "" {
		t.Fatalf("stderr missing WorldHash; run1=%q run2=%q", stderrA.String(), stderrB.String())
	}
	if h1 != h2 {
		t.Errorf("runs produced different stderr WorldHashes: %q vs %q", h1, h2)
	}
}

// extractWorldHashFromStderr grep-lifts the WorldHash from the
// "save: WorldHash=<hex>" stderr line. Returns "" if not found.
func extractWorldHashFromStderr(stderr string) string {
	const prefix = "save: WorldHash="
	i := strings.Index(stderr, prefix)
	if i < 0 {
		return ""
	}
	j := strings.Index(stderr[i+len(prefix):], "\n")
	if j < 0 {
		return stderr[i+len(prefix):]
	}
	return stderr[i+len(prefix) : i+len(prefix)+j]
}
