// Tests for the `info` CLI subcommand. §39.E follow-up #1.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// TestInfo_ValidSave: build a SaveGame with non-default Protagonist /
// CurrentNodeID / Flags / Variables / Relationships / Reputation /
// Inventory / Party / EndingsUnlocked; run runInfo; assert stdout
// contains every expected `chronicle info:` line with correct values.
func TestInfo_ValidSave(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "save.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Protagonist = "kael"
	sg.WorldState.CurrentNodeID = "act1.opening"
	sg.WorldState.Tick = 42
	sg.WorldState.Flags["romanced_elara"] = true
	sg.WorldState.Flags["joined_dragons"] = false
	sg.WorldState.Variables["courage"] = 75
	sg.WorldState.Relationships["elara"] = state.Relationship{Trust: 80, Affection: 90, Respect: 70}
	sg.WorldState.Inventory.Items["dragon_relic"] = 1
	sg.WorldState.Party = []string{"elara", "orion"}
	sg.WorldState.EndingsUnlocked = []string{"ending.peaceful_companion"}
	sg.WorldState.Reputation.Kingdom = 10
	sg.WorldState.Reputation.Mages = -5
	sg.WorldState.Reputation.Dragons = 50
	sg.WorldState.Reputation.Underworld = 0
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runInfo([]string{out}, stdout, stderr); err != nil {
		t.Fatalf("runInfo: %v", err)
	}
	want := []string{
		"chronicle info: path=" + out,
		"chronicle info: Version=0",
		"chronicle info: Tick=42",
		"chronicle info: WorldHash=",
		"chronicle info: Protagonist=kael",
		"chronicle info: CurrentNodeID=act1.opening",
		"chronicle info: flags=2",
		"chronicle info: variables=1",
		"chronicle info: relationships=1",
		"chronicle info: inventory-items=1",
		"chronicle info: party=2",
		"chronicle info: endings-unlocked=1",
		"chronicle info: reputation=kingdom=10 mages=-5 dragons=50 underworld=0",
	}
	text := stdoutText()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("stdout missing %q; got: %s", w, text)
		}
	}
}

// TestInfo_EmptyWorldState: state.NewWorldState (no flags, no
// variables, no Protagonist, no CurrentNodeID) round-trips through
// info; conditional lines (Protagonist, CurrentNodeID) are
// suppressed; counts are zero.
func TestInfo_EmptyWorldState(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	data, _ := sg.Marshal()
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runInfo([]string{out}, stdout, stderr); err != nil {
		t.Fatalf("runInfo: %v", err)
	}
	text := stdoutText()
	for _, must := range []string{
		"chronicle info: flags=0",
		"chronicle info: variables=0",
		"chronicle info: relationships=0",
		"chronicle info: inventory-items=0",
		"chronicle info: party=0",
		"chronicle info: endings-unlocked=0",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("stdout missing %q; got: %s", must, text)
		}
	}
	for _, mustNot := range []string{"Protagonist=", "CurrentNodeID="} {
		if strings.Contains(text, mustNot) {
			t.Errorf("empty WorldState must NOT print %q; got: %s", mustNot, text)
		}
	}
}

// TestInfo_MissingPath: zero positional args returns a usage error.
// stderr carries the usage line.
func TestInfo_MissingPath(t *testing.T) {
	stdout, stderr, _, stderrText := testResumeOutputs()
	err := runInfo([]string{}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for missing arg; got nil")
	}
	if !strings.Contains(err.Error(), "expected exactly one") {
		t.Errorf("error must state arg-count expectation; got: %v", err)
	}
	if !strings.Contains(stderrText(), "usage: chronicle info") {
		t.Errorf("stderr should print usage; got: %s", stderrText())
	}
}

// TestInfo_TooManyArgs: more than one positional arg is rejected.
func TestInfo_TooManyArgs(t *testing.T) {
	stdout, stderr, _, _ := testResumeOutputs()
	err := runInfo([]string{"a.json", "b.json"}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "got 2") {
		t.Errorf("error must report actual arg count; got: %v", err)
	}
}

// TestInfo_FileMissing: a path that doesn't exist returns a
// wrapped error mentioning the path.
func TestInfo_FileMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "this-does-not-exist.json")
	stdout, stderr, _, _ := testResumeOutputs()
	err := runInfo([]string{missing}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must name the missing file; got: %v", err)
	}
}

// TestInfo_OldVersionRefused: §39.B version gate fires at the
// load layer (readSaveForInspection → sg.Unmarshal) so info
// respects it identically to resume.
func TestInfo_OldVersionRefused(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "old.json")
	sg := state.SaveGame{Version: -1, WorldState: state.NewWorldState()}
	data, _ := sg.Marshal()
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	stdout, stderr, _, _ := testResumeOutputs()
	err := runInfo([]string{out}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for old-version save")
	}
	if !strings.Contains(err.Error(), "older Chronicle build") {
		t.Errorf("error must mention older-build remediation; got: %v", err)
	}
}

// TestInfo_RejectsFlags: passing a flag (e.g., --json) is rejected
// upfront with a clear error. info takes no flags today; users
// who accidentally wire --json should get a friendly diagnostic
// that calls out the specific unrecognized flag.
func TestInfo_RejectsFlags(t *testing.T) {
	stdout, stderr, _, _ := testResumeOutputs()
	err := runInfo([]string{"--json", "/tmp/foo.json"}, stdout, stderr)
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

// TestInfo_CountsMatch: counts in info output exactly track map
// sizes — a regression guard against wrong-counts bugs that would
// mislead players about their save state.
func TestInfo_CountsMatch(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "save.json")

	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: state.NewWorldState()}
	sg.WorldState.Flags["a"] = true
	sg.WorldState.Flags["b"] = true
	sg.WorldState.Flags["c"] = true // 3 flags
	sg.WorldState.Variables["x"] = 1
	sg.WorldState.Variables["y"] = 2 // 2 variables
	data, _ := sg.Marshal()
	if err := os.WriteFile(out, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, stderr, stdoutText, _ := testResumeOutputs()
	if err := runInfo([]string{out}, stdout, stderr); err != nil {
		t.Fatalf("runInfo: %v", err)
	}
	want := []string{
		"chronicle info: flags=3",
		"chronicle info: variables=2",
	}
	for _, w := range want {
		if !strings.Contains(stdoutText(), w) {
			t.Errorf("stdout missing %q; got: %s", w, stdoutText())
		}
	}
}
