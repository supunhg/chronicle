// Tests for the `resume` CLI subcommand. Phase 39.D, extended
// in Phase 39.E with the `--json` flag tests.
//
// PHASES.md §39.D calls for a manual smoke test
// (`./chronicle save -out foo.json && ./chronicle resume foo.json`),
// but these programmatic tests catch regressions earlier and let CI
// gate the sub-Phase without a live shell. Three pillars:
//
//  1. resume's own surface tests (file-permission, arg-count, JSON
//     type, unknown-field rejection, flag-parse errors).
//  2. The cross-subcommand round-trip: a save → resume pair produces
//     the same WorldHash. This is the §18A byte-stable property as
//     exercised at the CLI boundary.
//  3. Phase 39.E's `--json` flag tests: stdout receives the canonical
//     SaveGame JSON (parseable by jq), stderr still carries the
//     diagnostic lines, and the JSON is byte-equal to `save`'s
//     Marshal output for the same SaveGame.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// testResumeOutputs returns (stdout, stderr, stdoutText, stderrText)
// so tests can assert on either stream without polluting test
// stdout/stderr locally. Pass either to runResume as needed.
func testResumeOutputs() (*bytes.Buffer, *bytes.Buffer, func() string, func() string) {
	out, err := &bytes.Buffer{}, &bytes.Buffer{}
	return out, err,
		func() string { return out.String() },
		func() string { return err.String() }
}

// discardStderr is the io.Writer used in tests that don't care
// about stderr (e.g., JSON-flag tests that pipe stdout).
var discardStderr io.Writer = io.Discard

// TestResume_ValidSave: write a canonical SaveGame to disk via
// state.SaveGame.Marshal, run runResume against it, confirm the
// post-load stderr names the expected fields. This is the canonical
// happy-path. Coverage of the spec's "demo-drops the player at
// WorldState.CurrentNodeID" requirement.
func TestResume_ValidSave(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "save.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Protagonist = "kael"
	sg.WorldState.CurrentNodeID = "act1.opening"
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal fixture: %v", err)
	}
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, stderr, _, stderrText := testResumeOutputs()
	if err := runResume([]string{out}, io.Discard, stderr); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	text := stderrText()
	// The 4-line identity block (Version, Protagonist, CurrentNodeID,
	// WorldHash=) follows the variable-content "loaded" line and is
	// what we want to fingerprint in adjacency.
	idBlock := strings.Join([]string{
		fmt.Sprintf("resume: Version=%d", state.CurrentVersion),
		"resume: Protagonist=kael",
		"resume: CurrentNodeID=act1.opening",
		"resume: WorldHash=",
	}, "\n")
	if !strings.Contains(text, idBlock) {
		t.Errorf("stderr missing joined identity block; got: %s", text)
	}
	if !strings.Contains(text, "resume: loaded ") {
		t.Errorf("stderr missing the 'loaded' lead line; got: %s", text)
	}
}

// TestResume_EmptyWorldState: a SaveGame with Version set but a
// fully default WorldState (state.NewWorldState: no Protagonist, no
// CurrentNodeID, no flags, no history) round-trips cleanly. The
// conditional Protagonist/CurrentNodeID stderr lines are correctly
// suppressed when those fields are empty.
func TestResume_EmptyWorldState(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal fixture: %v", err)
	}
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, stderr, _, stderrText := testResumeOutputs()
	if err := runResume([]string{out}, io.Discard, stderr); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	text := stderrText()
	for _, must := range []string{"resume: Version=", "resume: WorldHash="} {
		if !strings.Contains(text, must) {
			t.Errorf("stderr missing %q; got: %s", must, text)
		}
	}
	for _, mustNot := range []string{"resume: Protagonist=", "resume: CurrentNodeID="} {
		if strings.Contains(text, mustNot) {
			t.Errorf("empty WorldState must NOT print %q; got: %s", mustNot, text)
		}
	}
}

// TestResume_MissingPath: zero positional args returns a usage
// error naming "exactly one <path> argument". User-actionable:
// usage line was already printed to stderr.
func TestResume_MissingPath(t *testing.T) {
	_, stderr, _, stderrText := testResumeOutputs()
	err := runResume([]string{}, io.Discard, stderr)
	if err == nil {
		t.Fatal("expected error for missing <path>; got nil")
	}
	if !strings.Contains(err.Error(), "expected exactly one") {
		t.Errorf("error must state arg-count expectation; got: %v", err)
	}
	if !strings.Contains(stderrText(), "usage: chronicle resume") {
		t.Errorf("stderr should print usage; got: %s", stderrText())
	}
}

// TestResume_TooManyArgs: more than one positional arg fails
// cleanly, rejecting extra tokens so a user typo (e.g., two paths)
// doesn't silently resume one + ignore the other.
func TestResume_TooManyArgs(t *testing.T) {
	_, stderr, _, _ := testResumeOutputs()
	err := runResume([]string{"a.json", "b.json"}, io.Discard, stderr)
	if err == nil {
		t.Fatal("expected error for extra positional arg; got nil")
	}
	if !strings.Contains(err.Error(), "got 2") {
		t.Errorf("error must report actual arg count; got: %v", err)
	}
}

// TestResume_FileMissing: a path that doesn't exist returns a
// wrapped error mentioning the path. Lets the user diagnose
// typos + missing-file scenarios without consulting docs.
func TestResume_FileMissing(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.json")
	missing := filepath.Join(dir, "this-does-not-exist.json")
	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	data, _ := sg.Marshal()
	if err := os.WriteFile(good, data, 0600); err != nil {
		t.Fatalf("write defence fixture: %v", err)
	}

	_, stderr, _, _ := testResumeOutputs()
	err := runResume([]string{missing}, io.Discard, stderr)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must name the missing file; got: %v", err)
	}
}

// TestResume_OldVersionRefused: writes a SaveGame.Version=-1 to
// disk, expects the §39.B version gate to refuse. The error
// message is matched to ensure the sided remediation ("older
// / new Chronicle build") survives future refactoring.
func TestResume_OldVersionRefused(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "old.json")
	sg := state.SaveGame{Version: -1, WorldState: state.NewWorldState()}
	data, _ := sg.Marshal()
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, stderr, _, _ := testResumeOutputs()
	err := runResume([]string{out}, io.Discard, stderr)
	if err == nil {
		t.Fatal("expected error for old-version save")
	}
	if !strings.Contains(err.Error(), "older Chronicle build") {
		t.Errorf("error must mention 'older Chronicle build'; got: %v", err)
	}
}

// TestResume_NewVersionRefused: §39.B new-version gate. Matches
// the "newer" / "update Chronicle" sided remediation message.
func TestResume_NewVersionRefused(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "newer.json")
	sg := state.SaveGame{Version: state.CurrentVersion + 1, WorldState: state.NewWorldState()}
	data, _ := sg.Marshal()
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, stderr, _, _ := testResumeOutputs()
	err := runResume([]string{out}, io.Discard, stderr)
	if err == nil {
		t.Fatal("expected error for future-version save")
	}
	if !strings.Contains(err.Error(), "newer Chronicle build") {
		t.Errorf("error must mention 'newer Chronicle build'; got: %v", err)
	}
}

// TestResume_TamperedJSON: malformed JSON content is rejected at
// Unmarshal (no panic). The bytes are still on disk; the user
// sees a parse error pointing at the file path.
func TestResume_TamperedJSON(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "tampered.json")
	if err := os.WriteFile(out, []byte("{ broken json }"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, stderr, _, _ := testResumeOutputs()
	err := runResume([]string{out}, io.Discard, stderr)
	if err == nil {
		t.Fatal("expected error for tampered save")
	}
	if !strings.Contains(err.Error(), out) {
		t.Errorf("error must name the tampered file; got: %v", err)
	}
}

// TestResume_UnknownFieldRefused: §39.A DisallowUnknownFields
// catches a structurally tampered save where a hacker injects
// a "HackerField" alongside version 0. The error message must
// mention either the field name or "unknown".
func TestResume_UnknownFieldRefused(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "hacked.json")
	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	data, _ := sg.Marshal()
	tampered := bytes.Replace(data, []byte(`"Version":0`), []byte(`"Version":0,"HackerField":"injected"`), 1)
	if err := os.WriteFile(out, tampered, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, stderr, _, _ := testResumeOutputs()
	err := runResume([]string{out}, io.Discard, stderr)
	if err == nil {
		t.Fatal("expected error for unknown-field save")
	}
	if !strings.Contains(err.Error(), "HackerField") && !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error must mention the unknown field; got: %v", err)
	}
}

// TestResume_HashReflectsContent: two distinct but
// structurally identical SaveGames (differing only in a flag
// value) produce distinct WorldHashes.
func TestResume_HashReflectsContent(t *testing.T) {
	dir := t.TempDir()

	makeFile := func(flagVal bool) string {
		sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
		sg.WorldState.CurrentNodeID = "act1.opening"
		sg.WorldState.Flags["named"] = flagVal
		data, _ := sg.Marshal()
		out := filepath.Join(dir, fileNameFor(flagVal))
		if err := os.WriteFile(out, data, 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		return out
	}
	a := makeFile(true)
	b := makeFile(false)

	savedA := worldHashFromResume(t, a)
	savedB := worldHashFromResume(t, b)
	if savedA == savedB {
		t.Fatalf("two saves with different flag values produced equal hashes: %q vs %q", savedA, savedB)
	}
}

// fileNameFor returns a test-specific output filename so the
// t.TempDir()-managed dir knows which file corresponds to which
// flag-value pair in HashReflectsContent's makeFile.
func fileNameFor(flagVal bool) string {
	if flagVal {
		return "with-flag.json"
	}
	return "without-flag.json"
}

// worldHashFromResume is a small helper that runs the resume
// subcommand against a path and lift-out the WorldHash from the
// stderr text (using extractWorldHashFromStderr from save_test.go).
func worldHashFromResume(t *testing.T, path string) string {
	t.Helper()
	_, stderr, _, stderrText := testResumeOutputs()
	if err := runResume([]string{path}, io.Discard, stderr); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	h := extractWorldHashFromStderr(stderrText())
	if h == "" {
		t.Fatalf("resume stderr missing WorldHash for %s; got: %s", path, stderrText())
	}
	return h
}

// --- Phase 39.E: --json flag tests ---------------------------

// TestResume_JSONFlag_EmitsCanonicalSave: --json writes the
// loaded SaveGame as canonical JSON to stdout. Output bytes
// parse back into a SaveGame whose WorldHash equals the stderr
// WorldHash line. This proves the §18A round-trip invariant at
// the CLI boundary + that the JSON output is the same canonical
// form Marshal produces.
func TestResume_JSONFlag_EmitsCanonicalSave(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "save.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Protagonist = "kael"
	sg.WorldState.CurrentNodeID = "act1.opening"
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal fixture: %v", err)
	}
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout, stderr, stdoutText, stderrText := testResumeOutputs()
	if err := runResume([]string{"--json", out}, stdout, stderr); err != nil {
		t.Fatalf("runResume --json: %v", err)
	}

	// stdout parses as a SaveGame.
	var got state.SaveGame
	if err := json.Unmarshal([]byte(stdoutText()), &got); err != nil {
		t.Fatalf("stdout is not valid SaveGame JSON; got: %s; parse err: %v", stdoutText(), err)
	}
	if got.Version != sg.Version {
		t.Errorf("Version: got %d want %d", got.Version, sg.Version)
	}
	if got.WorldState.Protagonist != sg.WorldState.Protagonist {
		t.Errorf("Protagonist: got %q want %q", got.WorldState.Protagonist, sg.WorldState.Protagonist)
	}
	if got.WorldState.CurrentNodeID != sg.WorldState.CurrentNodeID {
		t.Errorf("CurrentNodeID: got %q want %q", got.WorldState.CurrentNodeID, sg.WorldState.CurrentNodeID)
	}

	// stderr still carries the diagnostic identity.
	if !strings.Contains(stderrText(), "resume: WorldHash=") {
		t.Errorf("stderr missing WorldHash diagnostic in --json mode; got: %s", stderrText())
	}
}

// TestResume_JSONFlag_EquivalenceWithSave: the stdout JSON
// bytes are byte-equal to what `save --out X` writes for the
// same SaveGame. This proves the --json output is the canonical
// form — if a future author reformats Marshal without realizing
// the CLI depends on byte-stable equivalence, this test breaks.
func TestResume_JSONFlag_EquivalenceWithSave(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "roundtrip.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Protagonist = "lyra"
	sg.WorldState.CurrentNodeID = "act2.ashwick_arrival"
	canonical, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal canonical: %v", err)
	}
	if err := os.WriteFile(saveFile, canonical, 0600); err != nil {
		t.Fatalf("write save file: %v", err)
	}

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runResume([]string{"--json", saveFile}, stdout, stderr); err != nil {
		t.Fatalf("runResume --json: %v", err)
	}

	if stdoutText() != string(canonical) {
		t.Errorf("resume --json stdout != canonical Marshal bytes\n"+
			"  resume stdout:  %s\n"+
			"  save canonical: %s",
			stdoutText(), string(canonical))
	}
}

// TestResume_JSONFlag_FlagOrder: --json may appear before OR
// after the positional <path>. stdlib flag.Parse accepts both
// orderings and we want to verify ours does too — users with
// tab-completion muscle memory may type either.
func TestResume_JSONFlag_FlagOrder(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "save.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Protagonist = "raven"
	data, _ := sg.Marshal()
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// --json before path.
	stdout1, stderr1, stdoutText1, _ := testResumeOutputs()
	if err := runResume([]string{"--json", out}, stdout1, stderr1); err != nil {
		t.Fatalf("runResume --json before path: %v", err)
	}
	if stdoutText1() == "" {
		t.Errorf("--json before path: stdout empty")
	}

	// --json after path.
	stdout2, stderr2, stdoutText2, _ := testResumeOutputs()
	if err := runResume([]string{out, "--json"}, stdout2, stderr2); err != nil {
		t.Fatalf("runResume --json after path: %v", err)
	}
	if stdoutText2() == "" {
		t.Errorf("--json after path: stdout empty")
	}

	// Both orderings must produce the same stdout bytes
	// (deterministic, byte-stable).
	if stdoutText1() != stdoutText2() {
		t.Errorf("flag-position changed output\n"+
			"  --json before path: %s\n"+
			"  --json after path:  %s",
			stdoutText1(), stdoutText2())
	}
}

// TestResume_JSONFlag_PipeCompatible: stdout is parseable by
// encoding/json with no preamble / trailing crud — the canonical
// pipe-friendliness check. jq-style consumers can pipe resume's
// stdout directly without preprocessing.
func TestResume_JSONFlag_PipeCompatible(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "save.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Protagonist = "aria"
	sg.WorldState.CurrentNodeID = "act1.opening"
	data, _ := sg.Marshal()
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runResume([]string{"--json", out}, stdout, stderr); err != nil {
		t.Fatalf("runResume --json: %v", err)
	}

	// Round-trip through json.Decoder (tolerant of trailing
	// whitespace / multiple docs) — strict via Unmarshal may
	// double-fail if a future change adds a trailing newline.
	dec := json.NewDecoder(strings.NewReader(stdoutText()))
	var got state.SaveGame
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("stdout is not pipe-friendly JSON; got: %s; err: %v", stdoutText(), err)
	}
	if dec.More() {
		t.Errorf("stdout contains multiple JSON documents; jq would error")
	}
}
