// Tests for the `diff` CLI subcommand. §39.E follow-up #1.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// makeSaveFile is a shared helper that builds a SaveGame, makes
// `dir/fileName` and saves it there; returns the path. Used by
// every diff test to keep fixture-construction noise out of
// the assertion bodies.
func makeSaveFile(t *testing.T, dir, fileName string, sg state.SaveGame) string {
	t.Helper()
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal fixture: %v", err)
	}
	out := filepath.Join(dir, fileName)
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	return out
}

// TestDiff_Identical: two SaveGames with identical content
// produce an `identical` line on stdout and a nil error (exit 0).
func TestDiff_Identical(t *testing.T) {
	dir := t.TempDir()

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Protagonist = "kael"
	sg.WorldState.CurrentNodeID = "act1.opening"
	sg.WorldState.Flags["joined_dragons"] = true
	sg.WorldState.Variables["courage"] = 75

	a := makeSaveFile(t, dir, "a.json", sg)
	b := makeSaveFile(t, dir, "b.json", sg)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runDiff([]string{a, b}, stdout, stderr); err != nil {
		t.Fatalf("runDiff identical: %v", err)
	}
	if !strings.Contains(stdoutText(), "chronicle diff: identical") {
		t.Errorf("stdout missing 'identical' line; got: %s", stdoutText())
	}
}

// TestDiff_ProtagonistDiffers: Protagonist meta differs; stdout
// surfaces `chronicle diff: Protagonist: "x" -> "y"` and the
// returned error is errDiffFound (signalling exit 1).
func TestDiff_ProtagonistDiffers(t *testing.T) {
	dir := t.TempDir()

	aSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	aSG.WorldState.Protagonist = "kael"
	bSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	bSG.WorldState.Protagonist = "lyra"

	a := makeSaveFile(t, dir, "a.json", aSG)
	b := makeSaveFile(t, dir, "b.json", bSG)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	err := runDiff([]string{a, b}, stdout, stderr)
	if err != errDiffFound {
		t.Errorf("expected errDiffFound; got: %v", err)
	}
	want := `chronicle diff: Protagonist: "kael" -> "lyra"`
	if !strings.Contains(stdoutText(), want) {
		t.Errorf("stdout missing %q; got: %s", want, stdoutText())
	}
}

// TestDiff_NodeDiffers: CurrentNodeID differs; stdout surfaces
// the field-level line.
func TestDiff_NodeDiffers(t *testing.T) {
	dir := t.TempDir()

	aSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	aSG.WorldState.CurrentNodeID = "act1.opening"
	bSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	bSG.WorldState.CurrentNodeID = "act2.spire_arrival"

	a := makeSaveFile(t, dir, "a.json", aSG)
	b := makeSaveFile(t, dir, "b.json", bSG)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	err := runDiff([]string{a, b}, stdout, stderr)
	if err != errDiffFound {
		t.Errorf("expected errDiffFound; got: %v", err)
	}
	if !strings.Contains(stdoutText(), `chronicle diff: CurrentNodeID: "act1.opening" -> "act2.spire_arrival"`) {
		t.Errorf("stdout missing CurrentNodeID diff line; got: %s", stdoutText())
	}
}

// TestDiff_FlagValueDiffers: same flag key, different values;
// stdout surfaces the per-key diff line.
func TestDiff_FlagValueDiffers(t *testing.T) {
	dir := t.TempDir()

	aSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	aSG.WorldState.Flags["joined_dragons"] = true
	bSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	bSG.WorldState.Flags["joined_dragons"] = false

	a := makeSaveFile(t, dir, "a.json", aSG)
	b := makeSaveFile(t, dir, "b.json", bSG)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	err := runDiff([]string{a, b}, stdout, stderr)
	if err != errDiffFound {
		t.Errorf("expected errDiffFound; got: %v", err)
	}
	if !strings.Contains(stdoutText(), "chronicle diff: flags[joined_dragons]: true -> false") {
		t.Errorf("stdout missing flag diff line; got: %s", stdoutText())
	}
}

// TestDiff_FlagInsertedDeletion: a flag that's true on the left
// but absent on the right surfaces "true -> false" (zero value
// of bool is false). Likewise the other direction.
func TestDiff_FlagInsertedDeletion(t *testing.T) {
	dir := t.TempDir()

	aSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	aSG.WorldState.Flags["romanced_elara"] = true
	// bSG intentionally has no romanced_elara flag.

	a := makeSaveFile(t, dir, "a.json", aSG)
	b := makeSaveFile(t, dir, "b.json", state.SaveGame{
		Version:    state.CurrentVersion,
		WorldState: state.NewWorldState(),
	})

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runDiff([]string{a, b}, stdout, stderr); err != errDiffFound {
		t.Fatalf("expected errDiffFound; got: %v", err)
	}
	if !strings.Contains(stdoutText(), "chronicle diff: flags[romanced_elara]: true -> false") {
		t.Errorf("stdout missing insertion/deletion diff; got: %s", stdoutText())
	}
}

// TestDiff_VariableDiffers: variable value differs.
func TestDiff_VariableDiffers(t *testing.T) {
	dir := t.TempDir()

	aSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	aSG.WorldState.Variables["courage"] = 75
	bSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	bSG.WorldState.Variables["courage"] = 30

	a := makeSaveFile(t, dir, "a.json", aSG)
	b := makeSaveFile(t, dir, "b.json", bSG)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runDiff([]string{a, b}, stdout, stderr); err != errDiffFound {
		t.Fatalf("expected errDiffFound; got: %v", err)
	}
	if !strings.Contains(stdoutText(), "chronicle diff: variables[courage]: 75 -> 30") {
		t.Errorf("stdout missing variable diff; got: %s", stdoutText())
	}
}

// TestDiff_RelationshipAxisDiffers: per-companion axis differences
// surface on a single dedicated line per differing companion.
func TestDiff_RelationshipAxisDiffers(t *testing.T) {
	dir := t.TempDir()

	aSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	aSG.WorldState.Relationships["elara"] = state.Relationship{Trust: 80, Affection: 90, Respect: 70}
	bSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	bSG.WorldState.Relationships["elara"] = state.Relationship{Trust: 80, Affection: 60, Respect: 70}

	a := makeSaveFile(t, dir, "a.json", aSG)
	b := makeSaveFile(t, dir, "b.json", bSG)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runDiff([]string{a, b}, stdout, stderr); err != errDiffFound {
		t.Fatalf("expected errDiffFound; got: %v", err)
	}
	want := "chronicle diff: relationships[elara]:"
	if !strings.Contains(stdoutText(), want) {
		t.Errorf("stdout missing %q; got: %s", want, stdoutText())
	}
	// Only the Affection axis is different; the line should
	// encode that (Trust=80 matches, Affection 90->60).
	if !strings.Contains(stdoutText(), "Affection=90") {
		t.Errorf("stdout should print left-axis values; got: %s", stdoutText())
	}
}

// TestDiff_MultipleDifferences: many fields differ; every
// differing field is surfaced on its own line (no aggregation,
// no brevity for the sake of formatting).
func TestDiff_MultipleDifferences(t *testing.T) {
	dir := t.TempDir()

	aSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	aSG.WorldState.Protagonist = "kael"
	aSG.WorldState.CurrentNodeID = "act1.opening"
	aSG.WorldState.Flags["romanced_elara"] = true
	aSG.WorldState.Variables["courage"] = 75

	bSG := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	bSG.WorldState.Protagonist = "raven"
	bSG.WorldState.CurrentNodeID = "act1.forge_visit"
	bSG.WorldState.Flags["romanced_elara"] = false
	bSG.WorldState.Variables["courage"] = 50

	a := makeSaveFile(t, dir, "a.json", aSG)
	b := makeSaveFile(t, dir, "b.json", bSG)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runDiff([]string{a, b}, stdout, stderr); err != errDiffFound {
		t.Fatalf("expected errDiffFound; got: %v", err)
	}
	for _, want := range []string{
		`chronicle diff: Protagonist: "kael" -> "raven"`,
		`chronicle diff: CurrentNodeID: "act1.opening" -> "act1.forge_visit"`,
		"chronicle diff: flags[romanced_elara]: true -> false",
		"chronicle diff: variables[courage]: 75 -> 50",
	} {
		if !strings.Contains(stdoutText(), want) {
			t.Errorf("stdout missing %q; got: %s", want, stdoutText())
		}
	}
	// identical line must NOT appear alongside diff lines.
	if strings.Contains(stdoutText(), "chronicle diff: identical") {
		t.Errorf("stdout must not contain 'identical' when differences were found; got: %s", stdoutText())
	}
}

// TestDiff_SameFileBothArgs: passing the same path twice
// produces identical content (Marshal+Unmarshal+Marshal is
// byte-stable per §18A invariant #1), so diff exits 0 with
// the 'identical' line.
func TestDiff_SameFileBothArgs(t *testing.T) {
	dir := t.TempDir()

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Flags["joined_dragons"] = true
	out := makeSaveFile(t, dir, "save.json", sg)

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runDiff([]string{out, out}, stdout, stderr); err != nil {
		t.Fatalf("runDiff same-file-both-args: %v", err)
	}
	if !strings.Contains(stdoutText(), "chronicle diff: identical") {
		t.Errorf("stdout missing 'identical' line; got: %s", stdoutText())
	}
}

// TestDiff_MissingArg: zero positional args fails fast.
func TestDiff_MissingArg(t *testing.T) {
	stdout, stderr, _, stderrText := testResumeOutputs()
	err := runDiff([]string{}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "expected exactly two") {
		t.Errorf("error must state arg-count expectation; got: %v", err)
	}
	if !strings.Contains(stderrText(), "usage: chronicle diff") {
		t.Errorf("stderr should print usage; got: %s", stderrText())
	}
}

// TestDiff_OnlyOneArg: one positional arg fails fast with the
// same arg-count error.
func TestDiff_OnlyOneArg(t *testing.T) {
	dir := t.TempDir()
	out := makeSaveFile(t, dir, "save.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	stdout, stderr, _, _ := testResumeOutputs()
	err := runDiff([]string{out}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for one arg; got nil")
	}
	if !strings.Contains(err.Error(), "got 1") {
		t.Errorf("error must report actual arg count; got: %v", err)
	}
}

// TestDiff_ThreeArgs: three positional args fails fast; not
// silently truncated to two.
func TestDiff_ThreeArgs(t *testing.T) {
	dir := t.TempDir()
	sgA := makeSaveFile(t, dir, "a.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	sgB := makeSaveFile(t, dir, "b.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	sgC := makeSaveFile(t, dir, "c.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	stdout, stderr, _, _ := testResumeOutputs()
	err := runDiff([]string{sgA, sgB, sgC}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for three args; got nil")
	}
	if !strings.Contains(err.Error(), "got 3") {
		t.Errorf("error must report actual arg count; got: %v", err)
	}
}

// TestDiff_LoadErrorOnEitherSide: if either path fails to load
// (missing, tampered, version-mismatched) the load error wins
// over any diff/identical decision; diff never produces a
// half-comparison. A missing right-side file must surface the
// read error named at the right path.
func TestDiff_LoadErrorOnEitherSide(t *testing.T) {
	dir := t.TempDir()
	sg := makeSaveFile(t, dir, "real.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	missing := filepath.Join(dir, "this-does-not-exist.json")

	stdout, stderr, _, _ := testResumeOutputs()
	err := runDiff([]string{sg, missing}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must name the missing right-side file; got: %v", err)
	}
	if err == errDiffFound {
		t.Errorf("must NOT be errDiffFound (load failure, not a content diff); got: %v", err)
	}
}

// TestDiff_RejectsFlags: passing --json etc. is rejected
// upfront. diff has no flag surface today; the diagnostic
// names the offending flag explicitly.
func TestDiff_RejectsFlags(t *testing.T) {
	dir := t.TempDir()
	a := makeSaveFile(t, dir, "a.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	b := makeSaveFile(t, dir, "b.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	stdout, stderr, _, _ := testResumeOutputs()
	err := runDiff([]string{"--json", a, b}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for flag-arg; got nil")
	}
	if !strings.Contains(err.Error(), "unrecognized flag") {
		t.Errorf("error must call out unrecognized-flag policy; got: %v", err)
	}
	if !strings.Contains(err.Error(), "--json") {
		t.Errorf("error must name the offending flag; got: %v", err)
	}
}

// TestDiff_OldVersionRefusedOnEitherSide: §39.B version gate
// fires inside readSaveForInspection; an old-version file on
// either side short-circuits the diff with no half-comparison.
func TestDiff_OldVersionRefusedOnEitherSide(t *testing.T) {
	dir := t.TempDir()
	good := makeSaveFile(t, dir, "good.json",
		state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()})
	old := makeSaveFile(t, dir, "old.json",
		state.SaveGame{Version: -1, WorldState: state.NewWorldState()})

	stdout, stderr, _, _ := testResumeOutputs()
	err := runDiff([]string{good, old}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "older Chronicle build") {
		t.Errorf("error must call out the older-build remediation; got: %v", err)
	}
}

// silence unused-import linter for bytes if every test ever
// stops using it; today TestDiff_*.OutputText via the shared
// testResumeOutputs helper returns *bytes.Buffer, but we never
// import bytes directly here.
