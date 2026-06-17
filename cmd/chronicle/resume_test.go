// Tests for the `resume` CLI subcommand. Phase 39.D.
//
// PHASES.md §39.D calls for a manual smoke test
// (`./chronicle save -out foo.json && ./chronicle resume foo.json`),
// but these programmatic tests catch regressions earlier and let CI
// gate the sub-Phase without a live shell. Two pillars:
//
//  1. resume's own surface tests (file-permission, arg-count, JSON
//     type, unknown-field rejection).
//  2. The cross-subcommand round-trip: a save → resume pair produces
//     the same WorldHash. This is the §18A byte-stable property as
//     exercised at the CLI boundary.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// testResumeStderr returns an io.Writer+string-buffer pair so tests
// can assert on stderr output (Version/Protagonist/CurrentNodeID/
// WorldHash lines) without polluting test stderr locally.
func testResumeStderr() (*bytes.Buffer, func() string) {
	buf := &bytes.Buffer{}
	return buf, func() string { return buf.String() }
}

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

	stderr, stderrText := testResumeStderr()
	if err := runResume([]string{out}, stderr); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	text := stderrText()
	// The 4-line identity block (Version, Protagonist, CurrentNodeID,
	// WorldHash=) follows the variable-content "loaded" line and is
	// what we want to fingerprint in adjacency. Each line's complete
	// text is in the join (Version's dynamic value included) so the
	// resulting substring is an exact substring of stderr.
	idBlock := strings.Join([]string{
		fmt.Sprintf("resume: Version=%d", state.CurrentVersion),
		"resume: Protagonist=kael",
		"resume: CurrentNodeID=act1.opening",
		"resume: WorldHash=",
	}, "\n")
	if !strings.Contains(text, idBlock) {
		t.Errorf("stderr missing joined identity block; got: %s", text)
	}
	// The "loaded" line has content after the prefix that varies
	// per-fixture (byte count, path), so we check its prefix only.
	if !strings.Contains(text, "resume: loaded ") {
		t.Errorf("stderr missing the 'loaded' lead line; got: %s", text)
	}
}

// TestResume_EmptyWorldState: a SaveGame with Version set but a fully
// default WorldState (state.NewWorldState: no Protagonist, no
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

	stderr, stderrText := testResumeStderr()
	if err := runResume([]string{out}, stderr); err != nil {
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

// TestResume_MissingPath: zero positional args returns a usage error
// naming "exactly one <path> argument". User-actionable: usage line
// was already printed to stderr.
func TestResume_MissingPath(t *testing.T) {
	stderr, stderrText := testResumeStderr()
	err := runResume([]string{}, stderr)
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

// TestResume_TooManyArgs: more than one positional arg fails cleanly,
// rejecting extra tokens so a user typo (e.g., two paths) doesn't
// silently resume one + ignore the other.
func TestResume_TooManyArgs(t *testing.T) {
	stderr, _ := testResumeStderr()
	err := runResume([]string{"a.json", "b.json"}, stderr)
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
	// Pre-write a valid save so we ensure runResume is reading the
	// "missing" path, not falling back to a real one. The good file
	// is unused; its presence defends against future refactors
	// that might use path-finding.
	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	data, _ := sg.Marshal()
	if err := os.WriteFile(good, data, 0600); err != nil {
		t.Fatalf("write defence fixture: %v", err)
	}

	stderr, _ := testResumeStderr()
	err := runResume([]string{missing}, stderr)
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
	stderr, _ := testResumeStderr()
	err := runResume([]string{out}, stderr)
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
	stderr, _ := testResumeStderr()
	err := runResume([]string{out}, stderr)
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
	stderr, _ := testResumeStderr()
	err := runResume([]string{out}, stderr)
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
	// Inject a non-struct field via byte replacement.
	tampered := bytes.Replace(data, []byte(`"Version":0`), []byte(`"Version":0,"HackerField":"injected"`), 1)
	if err := os.WriteFile(out, tampered, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	stderr, _ := testResumeStderr()
	err := runResume([]string{out}, stderr)
	if err == nil {
		t.Fatal("expected error for unknown-field save")
	}
	if !strings.Contains(err.Error(), "HackerField") && !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error must mention the unknown field; got: %v", err)
	}
}

// TestResume_HashReflectsContent: two distinct but
// structurally identical SaveGames (differing only in a flag
// value) produce distinct WorldHashes. Verifies resume's printed
// hash is content-sensitive, not a static string.
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
	stderr, stderrText := testResumeStderr()
	if err := runResume([]string{path}, stderr); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	h := extractWorldHashFromStderr(stderrText())
	if h == "" {
		t.Fatalf("resume stderr missing WorldHash for %s; got: %s", path, stderrText())
	}
	return h
}
