package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/persistence"
)

// frontierPackDir returns the absolute path to the frontier worldpack
// directory regardless of which directory `go test` happens to be
// invoked from. Tests in this package depend on the real frontier
// pack, so we resolve the path relative to this test file rather
// than the process's working directory.
func frontierPackDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile is cmd/chronicle/main_test.go; project root is 3 levels up.
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "worldpacks", "frontier")
}

// quietStderr redirects os.Stderr to /dev/null for the duration of t,
// suppressing the resume/save functions' chatter. Returns a cleanup func.
func quietStderr(t *testing.T) func() {
	t.Helper()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = devNull
	return func() {
		os.Stderr = oldStderr
		_ = devNull.Close()
	}
}

// captureStderrToFile redirects os.Stderr to a temp file for the
// duration of the test and returns the file's path plus a restore
// function. The test can then read the file and assert on the
// captured output. Used by info tests that need to verify the
// printed metadata content (not just the error return).
func captureStderrToFile(t *testing.T) (path string, restore func()) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "stderr.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create %s: %v", path, err)
	}
	oldStderr := os.Stderr
	os.Stderr = f
	return path, func() {
		_ = f.Close()
		os.Stderr = oldStderr
	}
}

// snapshotWorld opens a fresh DB at dbPath, migrates, snapshots w,
// and closes. Used by diff tests that need two snapshots in a temp
// dir.
func snapshotWorld(t *testing.T, w *core.World, dbPath string) {
	t.Helper()
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open %s: %v", dbPath, err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestResume_ZeroTicksPreservesState verifies that resuming with
// numTicks=0 loads the snapshot faithfully and does not mutate it.
func TestResume_ZeroTicksPreservesState(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "snap.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	original := core.NewWorld("test-resume", 42, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	original.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	original.AddPerson(&core.Person{ID: "p2", Name: "Bob", Gender: "M", BirthTick: -25 * 365, Alive: true})
	original.Rules = &core.WorldRules{
		AnnualDeathChance: 0.0, // no deaths during resume
		MigrationFraction: 0.5,
		FertileMinAge:     16,
		FertileMaxAge:     50,
	}
	original.Tick = 100
	original.Now = time.Date(1400, 1, 1, 0, 0, 0, 100, time.UTC)

	if err := db.Snapshot(original); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Resume with 0 ticks: state should be identical to the snapshot.
	w, err := runResume(dbPath, 0)
	if err != nil {
		t.Fatalf("runResume(0): %v", err)
	}
	if w.ID != "test-resume" {
		t.Errorf("ID = %q, want test-resume", w.ID)
	}
	if w.Seed != 42 {
		t.Errorf("Seed = %d, want 42", w.Seed)
	}
	if w.Tick != 100 {
		t.Errorf("Tick = %d, want 100 (no ticks should be run)", w.Tick)
	}
	if len(w.People) != 2 {
		t.Errorf("len(People) = %d, want 2", len(w.People))
	}
	if w.Rules == nil {
		t.Fatal("Rules is nil; expected populated")
	}
	if w.Rules.MigrationFraction != 0.5 {
		t.Errorf("MigrationFraction = %f, want 0.5", w.Rules.MigrationFraction)
	}
}

// TestResume_RunsTicks verifies that resuming with N>0 ticks
// advances the world (Tick counter increases by exactly N).
func TestResume_RunsTicks(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "snap.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	original := core.NewWorld("test-ticks", 7, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	original.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	original.Rules = &core.WorldRules{
		AnnualDeathChance:     0.0, // deterministic: no deaths
		FertileMinAge:         16,
		FertileMaxAge:         50,
		MinBirthIntervalTicks: 365,
		MaxChildren:           6,
		MigrationFraction:     0.5,
		MinMigrantsPerTick:    1,
	}
	original.Tick = 50

	if err := db.Snapshot(original); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Run 10 more ticks.
	w, err := runResume(dbPath, 10)
	if err != nil {
		t.Fatalf("runResume(10): %v", err)
	}
	if w.Tick != 60 {
		t.Errorf("Tick = %d, want 60 (50 + 10)", w.Tick)
	}
	if len(w.People) != 1 {
		t.Errorf("len(People) = %d, want 1 (no births, no deaths)", len(w.People))
	}
}

// TestResume_EmptySnapshotWarns verifies that resuming a snapshot
// with no people succeeds (with a warning) rather than erroring. An
// empty world is a valid state (e.g., all NPCs died mid-game).
func TestResume_EmptySnapshotWarns(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Snapshot a world with zero people.
	empty := core.NewWorld("empty", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := db.Snapshot(empty); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	w, err := runResume(dbPath, 5)
	if err != nil {
		t.Fatalf("runResume on empty snapshot: %v", err)
	}
	if len(w.People) != 0 {
		t.Errorf("len(People) = %d, want 0", len(w.People))
	}
	if w.Tick != 5 {
		t.Errorf("Tick = %d, want 5", w.Tick)
	}
}

// TestResume_BadPathErrors verifies that resuming a non-existent
// file returns a clear error.
func TestResume_BadPathErrors(t *testing.T) {
	defer quietStderr(t)()

	_, err := runResume("/nonexistent/path/snap.db", 10)
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

// TestResume_EngineUsesRestoredRules is the CLI-level integration
// test that proves the full resume pipeline honors restored
// WorldRules. Snapshots a world with AnnualDeathChance=0.50 and
// 1000 people, resumes with 0 engine-field mortality, runs 1 year,
// and asserts ~500 alive. If runResume silently dropped w.Rules,
// all 1000 would survive.
func TestResume_EngineUsesRestoredRules(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "rules.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	original := core.NewWorld("rules-rt", 7, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	// 1000 people with stable IDs.
	for i := 0; i < 1000; i++ {
		id := "p" + strconv.Itoa(i)
		original.AddPerson(&core.Person{ID: id, Name: id, Gender: "M", BirthTick: -20 * 365, Alive: true})
	}
	original.Rules = &core.WorldRules{
		AnnualDeathChance:     0.50,
		FertileMinAge:         16,
		FertileMaxAge:         50,
		MinBirthIntervalTicks: 365,
		MaxChildren:           6,
		MigrationFraction:     0.5,
		MinMigrantsPerTick:    1,
	}
	if err := db.Snapshot(original); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	w, err := runResume(dbPath, 365)
	if err != nil {
		t.Fatalf("runResume(365): %v", err)
	}
	if w.Rules == nil {
		t.Fatal("w.Rules is nil after resume")
	}
	if w.Rules.AnnualDeathChance != 0.50 {
		t.Fatalf("AnnualDeathChance = %f, want 0.50", w.Rules.AnnualDeathChance)
	}
	alive := 0
	for _, p := range w.People {
		if p.Alive {
			alive++
		}
	}
	// With restored AnnualDeathChance=0.50, expected ~500 alive.
	// If w.Rules were silently dropped, alive would be ~990.
	if alive < 350 || alive > 650 {
		t.Errorf("alive = %d; expected ~500 (proves restored AnnualDeathChance=0.50 is honored at the CLI level); if ~990, w.Rules was dropped", alive)
	}
}

// TestSave_DefaultPath verifies that calling runSave with an empty
// outPath writes to <world-id>.db. The world-id is derived from the
// seed by runPlay as fmt.Sprintf("%08x", uint32(seed)). For seed=12345
// the world-id is 00003039, so the default path is 00003039.db. We
// chdir into a temp dir so the default-path DB lands there (not in
// the project root, which would litter the working tree).
func TestSave_DefaultPath(t *testing.T) {
	defer quietStderr(t)()

	packDir := frontierPackDir(t)
	if _, err := os.Stat(packDir); err != nil {
		t.Skipf("frontier worldpack not available at %s: %v", packDir, err)
	}

	tmpDir := t.TempDir()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	// t.Cleanup is panic-safe; defer would not run if runSave panics.
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })

	// Empty outPath -> runSave should default to w.ID + ".db".
	w, resolved, err := runSave(packDir, 5, 12345, "")
	if err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if w == nil {
		t.Fatal("runSave returned nil world")
	}
	want := w.ID + ".db"
	if resolved != want {
		t.Errorf("resolved outPath = %q, want %q", resolved, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected default snapshot at %s, got: %v", want, err)
	}
	if w.Tick != 5 {
		t.Errorf("Tick = %d, want 5", w.Tick)
	}
}

// TestSave_CustomPath verifies that an explicit -out path is honored
// exactly (no <world-id>.db substitution). Useful for grouping many
// runs into different filenames.
func TestSave_CustomPath(t *testing.T) {
	defer quietStderr(t)()

	packDir := frontierPackDir(t)
	if _, err := os.Stat(packDir); err != nil {
		t.Skipf("frontier worldpack not available at %s: %v", packDir, err)
	}

	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "my-run-2026.db")

	w, resolved, err := runSave(packDir, 3, 99, customPath)
	if err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if resolved != customPath {
		t.Errorf("resolved outPath = %q, want %q (caller-supplied)", resolved, customPath)
	}
	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("expected snapshot at %s, got: %v", customPath, err)
	}
	if w.Tick != 3 {
		t.Errorf("Tick = %d, want 3", w.Tick)
	}
}

// TestSave_RoundTripWithResume is the end-to-end save+resume test:
// save a world with the frontier pack (which sets a non-default
// AnnualDeathChance via worldpack.Bootstrap), then resume the same
// DB and assert Tick advances. Proves the full play->save->resume
// cycle works at the CLI level.
func TestSave_RoundTripWithResume(t *testing.T) {
	defer quietStderr(t)()

	packDir := frontierPackDir(t)
	if _, err := os.Stat(packDir); err != nil {
		t.Skipf("frontier worldpack not available at %s: %v", packDir, err)
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "roundtrip.db")

	// Step 1: play 10 ticks, then save to dbPath.
	saved, resolved, err := runSave(packDir, 10, 7, dbPath)
	if err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if resolved != dbPath {
		t.Errorf("resolved outPath = %q, want %q", resolved, dbPath)
	}
	if saved.Tick != 10 {
		t.Fatalf("saved Tick = %d, want 10", saved.Tick)
	}

	// Step 2: resume and run 5 more ticks. Final Tick should be 15.
	resumed, err := runResume(dbPath, 5)
	if err != nil {
		t.Fatalf("runResume: %v", err)
	}
	if resumed.ID != saved.ID {
		t.Errorf("resumed.ID = %q, want %q", resumed.ID, saved.ID)
	}
	if resumed.Seed != saved.Seed {
		t.Errorf("resumed.Seed = %d, want %d", resumed.Seed, saved.Seed)
	}
	if resumed.Tick != 15 {
		t.Errorf("resumed.Tick = %d, want 15 (10 saved + 5 resumed)", resumed.Tick)
	}
}

// TestSave_PackRulesPersisted verifies that the worldpack's Rules
// (set by worldpack.Bootstrap during runPlay) survive Snapshot and
// are visible to a fresh Restore. Proves the save subcommand
// preserves the pack-driven rules for downstream consumers.
func TestSave_PackRulesPersisted(t *testing.T) {
	defer quietStderr(t)()

	packDir := frontierPackDir(t)
	if _, err := os.Stat(packDir); err != nil {
		t.Skipf("frontier worldpack not available at %s: %v", packDir, err)
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "rules-save.db")

	// Step 1: save with the frontier pack.
	saved, resolved, err := runSave(packDir, 5, 42, dbPath)
	if err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if resolved != dbPath {
		t.Errorf("resolved outPath = %q, want %q", resolved, dbPath)
	}
	if saved.Rules == nil {
		t.Fatal("saved.Rules is nil; bootstrap should have populated it")
	}
	wantAnnualDeath := saved.Rules.AnnualDeathChance

	// Step 2: open the DB directly and read world_rules.
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Rules == nil {
		t.Fatal("loaded.Rules is nil after Restore from save")
	}
	if loaded.Rules.AnnualDeathChance != wantAnnualDeath {
		t.Errorf("AnnualDeathChance: got %f, want %f (from worldpack)", loaded.Rules.AnnualDeathChance, wantAnnualDeath)
	}
	if loaded.Rules.MigrationFraction != saved.Rules.MigrationFraction {
		t.Errorf("MigrationFraction: got %f, want %f", loaded.Rules.MigrationFraction, saved.Rules.MigrationFraction)
	}
}

// TestSave_AutoResumeTriggersOnExtinction verifies that when the
// post-save world has no alive people AND SaveOptions.AutoResume is
// true, the injected ResumeFn is called with the snapshot path and
// the configured tick count. Uses a synthetic world (no pack needed)
// so the test is fully deterministic.
func TestSave_AutoResumeTriggersOnExtinction(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "extinct.db")

	// Synthetic world in game-over state: 2 people, both dead.
	w := core.NewWorld("extinct", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "d1", Name: "Dead1", Gender: "F", BirthTick: -20 * 365, Alive: false, DeathTick: 50})
	w.AddPerson(&core.Person{ID: "d2", Name: "Dead2", Gender: "M", BirthTick: -25 * 365, Alive: false, DeathTick: 60})
	w.Rules = &core.WorldRules{AnnualDeathChance: 0.01, MigrationFraction: 0.5}

	// Mock ResumeFn records the call instead of actually resuming.
	var calledWithPath string
	var calledWithTicks int
	mockResume := func(dbPath string, numTicks int) (*core.World, error) {
		calledWithPath = dbPath
		calledWithTicks = numTicks
		// Return a non-nil world so the caller can chain.
		return core.NewWorld("resumed", 1, time.Now()), nil
	}

	opts := SaveOptions{
		AutoResume:      true,
		AutoResumeTicks: 42,
		ResumeFn:        mockResume,
	}
	got, resolved, autoResumed, err := saveAndMaybeResume(w, dbPath, opts)
	if err != nil {
		t.Fatalf("saveAndMaybeResume: %v", err)
	}
	if !autoResumed {
		t.Fatal("autoResumed = false; expected true (world is in game-over state)")
	}
	if calledWithPath != dbPath {
		t.Errorf("ResumeFn called with path %q, want %q", calledWithPath, dbPath)
	}
	if calledWithTicks != 42 {
		t.Errorf("ResumeFn called with ticks %d, want 42", calledWithTicks)
	}
	if resolved != dbPath {
		t.Errorf("resolved = %q, want %q", resolved, dbPath)
	}
	if got == nil {
		t.Error("got world is nil; expected the resumed world from the mock")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected snapshot at %s, got: %v", dbPath, err)
	}
}

// TestSave_AutoResumeSkippedWhenAlive verifies that when the
// post-save world has at least one alive person, the ResumeFn is
// NOT called even if AutoResume is true. The save proceeds normally
// and autoResumed is false.
func TestSave_AutoResumeSkippedWhenAlive(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "alive.db")

	w := core.NewWorld("alive", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "a1", Name: "Alive1", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w.AddPerson(&core.Person{ID: "d1", Name: "Dead1", Gender: "M", BirthTick: -25 * 365, Alive: false, DeathTick: 60})
	w.Rules = &core.WorldRules{AnnualDeathChance: 0.01, MigrationFraction: 0.5}

	called := false
	mockResume := func(dbPath string, numTicks int) (*core.World, error) {
		called = true
		return nil, nil
	}

	opts := SaveOptions{
		AutoResume:      true,
		AutoResumeTicks: 100,
		ResumeFn:        mockResume,
	}
	got, resolved, autoResumed, err := saveAndMaybeResume(w, dbPath, opts)
	if err != nil {
		t.Fatalf("saveAndMaybeResume: %v", err)
	}
	if autoResumed {
		t.Error("autoResumed = true; expected false (world is not in game-over state)")
	}
	if called {
		t.Error("ResumeFn was called; expected it to be skipped (world has alive people)")
	}
	if resolved != dbPath {
		t.Errorf("resolved = %q, want %q", resolved, dbPath)
	}
	// got should be the input world (no auto-resume fired), not a
	// freshly-constructed resumed world. Use pointer equality to
	// verify: saveAndMaybeResume returns w as-is on the no-resume path.
	if got != w {
		t.Error("got world is not the same pointer as the input; expected saveAndMaybeResume to return w as-is (no auto-resume)")
	}
}

// TestSave_AutoResumeOffByDefault verifies that even with a
// game-over world, the ResumeFn is NOT called when AutoResume is
// false (the default). This guards against accidentally firing
// auto-resume for users who don't set the flag.
func TestSave_AutoResumeOffByDefault(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "off.db")

	w := core.NewWorld("off", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "d1", Name: "Dead1", Gender: "F", BirthTick: -20 * 365, Alive: false, DeathTick: 50})
	w.Rules = &core.WorldRules{AnnualDeathChance: 0.01, MigrationFraction: 0.5}

	called := false
	mockResume := func(dbPath string, numTicks int) (*core.World, error) {
		called = true
		return nil, nil
	}

	// AutoResume omitted (false). ResumeFn is set so we can detect
	// the regression of firing on false.
	opts := SaveOptions{
		AutoResume:      false,
		AutoResumeTicks: 100,
		ResumeFn:        mockResume,
	}
	_, _, autoResumed, err := saveAndMaybeResume(w, dbPath, opts)
	if err != nil {
		t.Fatalf("saveAndMaybeResume: %v", err)
	}
	if autoResumed {
		t.Error("autoResumed = true; expected false (AutoResume is off)")
	}
	if called {
		t.Error("ResumeFn was called; expected it to be skipped (AutoResume is off)")
	}
}

// TestSave_AutoResumeRequiresResumeFn verifies that enabling
// AutoResume without setting ResumeFn returns an error rather than
// silently doing nothing or panicking. Defensive: catches the
// "I forgot to wire the fn" mistake at test time.
func TestSave_AutoResumeRequiresResumeFn(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nofn.db")

	w := core.NewWorld("nofn", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "d1", Name: "Dead1", Gender: "F", BirthTick: -20 * 365, Alive: false})

	opts := SaveOptions{
		AutoResume:      true,
		AutoResumeTicks: 100,
		ResumeFn:        nil, // missing!
	}
	_, _, _, err := saveAndMaybeResume(w, dbPath, opts)
	if err == nil {
		t.Fatal("expected error when AutoResume=true and ResumeFn=nil, got nil")
	}
}

// TestSaveCmd_AutoResumeFlagParses is a CLI-level test that proves
// the -auto-resume flag is wired correctly through runSaveCmd. It
// uses the frontier pack (which won't go extinct in 5 ticks, so
// auto-resume won't fire) and asserts that the command runs to
// completion with the flag set. This catches flag-parsing and
// dispatch regressions at the CLI boundary that the unit tests on
// saveAndMaybeResume can't see.
func TestSaveCmd_AutoResumeFlagParses(t *testing.T) {
	defer quietStderr(t)()

	packDir := frontierPackDir(t)
	if _, err := os.Stat(packDir); err != nil {
		t.Skipf("frontier worldpack not available at %s: %v", packDir, err)
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli-auto.db")

	// Build the argv that the CLI would see. The "save" subcommand
	// itself is consumed by main()'s dispatch, so runSaveCmd sees
	// the remaining args.
	args := []string{
		"-pack", packDir,
		"-ticks", "3",
		"-seed", "12345",
		"-out", dbPath,
		"-auto-resume",
		"-auto-resume-ticks", "2",
	}
	if err := runSaveCmd(args); err != nil {
		t.Fatalf("runSaveCmd: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected snapshot at %s, got: %v", dbPath, err)
	}
}

// TestInfo_PrintsMetadata verifies that runInfo returns nil for a
// valid snapshot AND that the captured stderr contains the expected
// metadata substrings (ID, seed, tick, rules fields, people counts).
// This catches formatting regressions (typos, missing fields, wrong
// alignment) that a bare err-check would miss.
func TestInfo_PrintsMetadata(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "info.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	w := core.NewWorld("info-test", 42, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", Gender: "M", BirthTick: -25 * 365, Alive: false, DeathTick: 50})
	w.Rules = &core.WorldRules{
		AnnualDeathChance:     0.05,
		FertileMinAge:         18,
		FertileMaxAge:         45,
		MinBirthIntervalTicks: 730,
		MaxChildren:           4,
		MigrationFraction:     0.5,
		MinMigrantsPerTick:    1,
	}
	w.Tick = 200

	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := runInfo(dbPath); err != nil {
		t.Fatalf("runInfo: %v", err)
	}

	// Assert the captured output contains the expected metadata.
	data, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", stderrPath, err)
	}
	output := string(data)
	wants := []string{
		"--- Info for " + dbPath + " ---",
		"World ID:  info-test",
		"Seed:      42",
		"Tick:      200",
		"AnnualDeathChance:      0.050000",
		"FertileMinAge:          18",
		"MigrationFraction:      0.500000",
		"People:     1 alive / 2 total",
		"Locations:  0",
		"Relationships:  0",
		"Memories:       0",
	}
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Errorf("captured stderr missing %q\n--- full output ---\n%s", want, output)
		}
	}
}

// TestInfo_BadPathErrors verifies that runInfo returns a clear
// error for a non-existent path.
func TestInfo_BadPathErrors(t *testing.T) {
	defer quietStderr(t)()

	err := runInfo("/nonexistent/path/info.db")
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

// TestInfo_DoesNotAdvanceTick is the read-only invariant test for
// runInfo. We snapshot a world at Tick=200, call runInfo, then
// re-Restore the DB and verify Tick is still 200. The test proves
// the DB file is unchanged after runInfo — which, given that runInfo
// has no db.Snapshot/db.Restore calls in its body, is equivalent to
// "no ticks were run" for the purpose of user-visible state. (A
// stronger test would instrument sim.Tick via a counter or mock
// sim, but that's out of scope for this phase.)
func TestInfo_DoesNotAdvanceTick(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "no-tick.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	w := core.NewWorld("no-tick", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w.Tick = 200

	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Call runInfo. It should restore + print + close, but NOT
	// mutate the DB (info is read-only).
	if err := runInfo(dbPath); err != nil {
		t.Fatalf("runInfo: %v", err)
	}

	// Re-open the DB and verify Tick is still 200.
	db2, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db2.Close()

	loaded := core.NewWorld("", 0, time.Time{})
	if err := db2.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Tick != 200 {
		t.Errorf("Tick = %d, want 200 (runInfo must not advance ticks)", loaded.Tick)
	}
	if len(loaded.People) != 1 {
		t.Errorf("len(People) = %d, want 1 (runInfo must not mutate people)", len(loaded.People))
	}
}

// TestInfo_NoRules verifies that runInfo handles a snapshot with
// w.Rules == nil (a legacy or rules-less world) without erroring.
// The "World Rules: (none, ...)" branch in runInfo must not panic
// on a nil pointer.
func TestInfo_NoRules(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "no-rules.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	w := core.NewWorld("no-rules", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if w.Rules != nil {
		t.Fatal("test prerequisite: w.Rules should be nil")
	}

	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := runInfo(dbPath); err != nil {
		t.Fatalf("runInfo on rules-less world: %v", err)
	}
}

// TestInfoCmd_FlagParses is the CLI-level test that proves the
// `info` subcommand is dispatched correctly by main() and that
// runInfoCmd parses the positional <db-path> arg.
func TestInfoCmd_FlagParses(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli-info.db")

	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	w := core.NewWorld("cli-info", 7, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := runInfoCmd([]string{dbPath}); err != nil {
		t.Fatalf("runInfoCmd: %v", err)
	}
}

// TestInfoCmd_MissingPathErrors verifies that runInfoCmd returns
// a clear usage error when no <db-path> is provided.
func TestInfoCmd_MissingPathErrors(t *testing.T) {
	defer quietStderr(t)()

	err := runInfoCmd([]string{})
	if err == nil {
		t.Fatal("expected error for missing db-path, got nil")
	}
}

// TestDiff_IdenticalSnapshots verifies that diffing a snapshot
// against itself reports no differences across metadata, rules, and
// people. This is the "no-op" baseline.
func TestDiff_IdenticalSnapshots(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "same.db")

	w := core.NewWorld("same", 42, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w.Rules = &core.WorldRules{AnnualDeathChance: 0.01, MigrationFraction: 0.5, FertileMinAge: 16, FertileMaxAge: 50}
	w.Tick = 100
	snapshotWorld(t, w, dbPath)

	if err := runDiff(dbPath, dbPath); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	// No differences should be reported.
	if !strings.Contains(output, "World ID:  \"same\" (same)") {
		t.Errorf("expected World ID 'same' to be reported as same, got: %s", output)
	}
	if !strings.Contains(output, "Seed:      42 (same)") {
		t.Errorf("expected Seed 42 to be reported as same, got: %s", output)
	}
	if !strings.Contains(output, "Tick:      100 (same)") {
		t.Errorf("expected Tick 100 to be reported as same, got: %s", output)
	}
	if !strings.Contains(output, "(all 8 rules fields are equal)") {
		t.Errorf("expected all rules fields to be equal, got: %s", output)
	}
	if !strings.Contains(output, "(no people differences)") {
		t.Errorf("expected no people differences, got: %s", output)
	}
	if !strings.Contains(output, "Counts:  1 alive / 1 total  ->  1 alive / 1 total") {
		t.Errorf("expected counts to be equal, got: %s", output)
	}
}

// TestDiff_DifferentTicks verifies that the same world at different
// tick values is reported as a tick delta (the core "branching
// timelines" use case).
func TestDiff_DifferentTicks(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "early.db")
	dbPath2 := filepath.Join(tmpDir, "late.db")

	w1 := core.NewWorld("branch", 42, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w1.Rules = &core.WorldRules{AnnualDeathChance: 0.01, MigrationFraction: 0.5}
	w1.Tick = 100
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("branch", 42, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w2.Rules = &core.WorldRules{AnnualDeathChance: 0.01, MigrationFraction: 0.5}
	w2.Tick = 150
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	if !strings.Contains(output, "Tick:      100 -> 150 (db2 is +50)") {
		t.Errorf("expected tick delta 100->150 (+50), got: %s", output)
	}
	if !strings.Contains(output, "(no people differences)") {
		t.Errorf("expected no people differences (same person, just different tick), got: %s", output)
	}
}

// TestDiff_PeopleAdded verifies that people present in db2 but not
// db1 are listed under "Added".
func TestDiff_PeopleAdded(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "before.db")
	dbPath2 := filepath.Join(tmpDir, "after.db")

	w1 := core.NewWorld("add", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("add", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w2.AddPerson(&core.Person{ID: "p2", Name: "Bob", Gender: "M", BirthTick: -25 * 365, Alive: true})
	w2.AddPerson(&core.Person{ID: "p3", Name: "Carol", Gender: "F", BirthTick: -22 * 365, Alive: true})
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	if !strings.Contains(output, "Added (2):    p2, p3") {
		t.Errorf("expected 'Added (2): p2, p3', got: %s", output)
	}
	if !strings.Contains(output, "Counts:  1 alive / 1 total  ->  3 alive / 3 total") {
		t.Errorf("expected counts 1/1 -> 3/3, got: %s", output)
	}
}

// TestDiff_PeopleRemoved verifies that people present in db1 but not
// db2 are listed under "Removed".
func TestDiff_PeopleRemoved(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "before.db")
	dbPath2 := filepath.Join(tmpDir, "after.db")

	w1 := core.NewWorld("remove", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w1.AddPerson(&core.Person{ID: "p2", Name: "Bob", Gender: "M", BirthTick: -25 * 365, Alive: false, DeathTick: 50})
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("remove", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	if !strings.Contains(output, "Removed (1):  p2") {
		t.Errorf("expected 'Removed (1): p2', got: %s", output)
	}
}

// TestDiff_PeopleChanged verifies that people with the same ID but
// different fields are listed under "Changed" with the field-level
// deltas.
func TestDiff_PeopleChanged(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "before.db")
	dbPath2 := filepath.Join(tmpDir, "after.db")

	w1 := core.NewWorld("change", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{
		ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true,
		LocationID: "village", Class: "commoner", Occupation: "farmer",
	})
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("change", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{
		ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: false, DeathTick: 100,
		LocationID: "town", Class: "noble", Occupation: "governor",
	})
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	if !strings.Contains(output, "Changed (1):") {
		t.Errorf("expected 'Changed (1):', got: %s", output)
	}
	// Spot-check a few of the 7 field deltas.
	for _, want := range []string{
		"Alive: true -> false",
		`LocationID: "village" -> "town"`,
		`Class: "commoner" -> "noble"`,
		`Occupation: "farmer" -> "governor"`,
		"DeathTick: 0 -> 100",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected change line %q, got: %s", want, output)
		}
	}
}

// TestDiff_BothRulesNil verifies that when neither world has
// WorldRules, diffRules prints "(both worlds have no rules)".
func TestDiff_BothRulesNil(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "norules-a.db")
	dbPath2 := filepath.Join(tmpDir, "norules-b.db")

	w1 := core.NewWorld("norules", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("norules", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w2.Tick = 10 // tick differs, rules don't
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	if !strings.Contains(output, "(both worlds have no rules)") {
		t.Errorf("expected '(both worlds have no rules)' in output, got: %s", output)
	}
}

// TestDiff_OneRulesNil verifies that when only one world has
// WorldRules, diffRules prints the "(db1 has no rules; db2 has
// rules)" branch and the rules-block dump for the non-nil side.
func TestDiff_OneRulesNil(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "with-rules.db")
	dbPath2 := filepath.Join(tmpDir, "no-rules.db")

	w1 := core.NewWorld("mixed", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w1.Rules = &core.WorldRules{AnnualDeathChance: 0.01, MigrationFraction: 0.5, FertileMinAge: 16}
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("mixed", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	if w2.Rules != nil {
		t.Fatal("test prerequisite: w2.Rules should be nil")
	}
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	if !strings.Contains(output, "(db2 has no rules; db1 has rules)") {
		t.Errorf("expected '(db2 has no rules; db1 has rules)' in output, got: %s", output)
	}
	if !strings.Contains(output, "  db1 rules:") {
		t.Errorf("expected '  db1 rules:' header in output, got: %s", output)
	}
	if !strings.Contains(output, "    AnnualDeathChance:      0.01") {
		t.Errorf("expected db1's AnnualDeathChance in the rules block, got: %s", output)
	}
}

// TestDiff_SummaryLine verifies that runDiff prints a top-level
// summary line with the total difference count.
func TestDiff_SummaryLine(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "sum-a.db")
	dbPath2 := filepath.Join(tmpDir, "sum-b.db")

	w1 := core.NewWorld("sum", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w1.Tick = 50
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("sum", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w2.Tick = 100
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	if !strings.Contains(output, "--- 1 differences: 1 metadata, 0 rules, 0 added, 0 removed, 0 changed, 0 relationships, 0 memories ---") {
		t.Errorf("expected 1-difference summary line, got: %s", output)
	}
}

// TestDiff_DifferentRules verifies that WorldRules field changes are
// reported under "World Rules".
func TestDiff_DifferentRules(t *testing.T) {
	stderrPath, restoreStderr := captureStderrToFile(t)
	defer restoreStderr()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "rules-a.db")
	dbPath2 := filepath.Join(tmpDir, "rules-b.db")

	w1 := core.NewWorld("rules", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.Rules = &core.WorldRules{
		AnnualDeathChance:     0.01,
		FertileMinAge:         16,
		MigrationFraction:     0.5,
		MinMigrantsPerTick:    1,
		MinBirthIntervalTicks: 365,
		MaxChildren:           6,
	}
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("rules", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.Rules = &core.WorldRules{
		AnnualDeathChance:     0.05,
		FertileMinAge:         18,
		MigrationFraction:     0.3,
		MinMigrantsPerTick:    2,
		MinBirthIntervalTicks: 730,
		MaxChildren:           4,
	}
	snapshotWorld(t, w2, dbPath2)

	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	output := readCapturedStderr(t, stderrPath)
	// The diffRules function uses %-26s padding for field names, so
	// we don't assert on exact spacing — just on the field name and
	// the (CHANGED) marker being present.
	for _, want := range []string{
		"AnnualDeathChance:",
		"0.01 -> 0.05 (CHANGED)",
		"FertileMinAge:",
		"16 -> 18 (CHANGED)",
		"MigrationFraction:",
		"0.5 -> 0.3 (CHANGED)",
		"MinMigrantsPerTick:",
		"1 -> 2 (CHANGED)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected rules change %q, got: %s", want, output)
		}
	}
}

// TestDiff_BadPathErrors verifies that runDiff returns a clear
// error if either input path is non-existent.
func TestDiff_BadPathErrors(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	goodPath := filepath.Join(tmpDir, "good.db")
	w := core.NewWorld("good", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	snapshotWorld(t, w, goodPath)

	if err := runDiff(goodPath, "/nonexistent/foo.db"); err == nil {
		t.Fatal("expected error for non-existent db2, got nil")
	}
	if err := runDiff("/nonexistent/foo.db", goodPath); err == nil {
		t.Fatal("expected error for non-existent db1, got nil")
	}
}

// TestDiffCmd_MissingArgErrors verifies that runDiffCmd returns a
// usage error when fewer than 2 args are provided.
func TestDiffCmd_MissingArgErrors(t *testing.T) {
	defer quietStderr(t)()

	if err := runDiffCmd([]string{}); err == nil {
		t.Fatal("expected error for 0 args, got nil")
	}
	if err := runDiffCmd([]string{"only-one.db"}); err == nil {
		t.Fatal("expected error for 1 arg, got nil")
	}
}

// TestDiffCmd_FlagParses is the CLI-level test that proves the
// `diff` subcommand is dispatched correctly and that runDiffCmd
// parses the two positional args.
func TestDiffCmd_FlagParses(t *testing.T) {
	defer quietStderr(t)()

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "cli-a.db")
	dbPath2 := filepath.Join(tmpDir, "cli-b.db")

	w1 := core.NewWorld("cli-diff", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w1.Tick = 50
	snapshotWorld(t, w1, dbPath1)

	w2 := core.NewWorld("cli-diff", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20 * 365, Alive: true})
	w2.Tick = 100
	snapshotWorld(t, w2, dbPath2)

	if err := runDiffCmd([]string{dbPath1, dbPath2}); err != nil {
		t.Fatalf("runDiffCmd: %v", err)
	}
}

// readCapturedStderr reads the captured stderr file and returns its
// contents as a string. Used by diff/info tests to assert on the
// printed output.
func readCapturedStderr(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(data)
}

// TestEndToEnd_SaveResumeDiffRelationshipsAndMemories is the
// end-to-end integration test that proves Phase 14 (RelationshipEngine)
// + Phase 15 (MemoryEngine) + Phase 12/13 (save/resume/diff CLI) all
// work together. The test:
//  1. Saves a world after 100 ticks (creates many birth/death memories
//     and co-located relationships via the MemoryEngine and
//     RelationshipEngine wired into the CLI).
//  2. Resumes and runs 100 more ticks (more events, more memories,
//     more relationships, and relationship scores shift due to
//     decay + memory-driven deltas).
//  3. Snapshots the post-resume world to a second DB.
//  4. Diffs the two snapshots.
//  5. Asserts: tick advanced 100→200, memory count changed, relationship
//     count changed, and at least some relationship scores actually
//     shifted between snapshots (proving co-location + decay +
//     memory-driven deltas all work together).
func TestEndToEnd_SaveResumeDiffRelationshipsAndMemories(t *testing.T) {
	packDir := frontierPackDir(t)
	if _, err := os.Stat(packDir); err != nil {
		t.Skipf("frontier worldpack not available at %s: %v", packDir, err)
	}

	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "first.db")
	dbPath2 := filepath.Join(tmpDir, "second.db")

	// Suppress save/resume output (it's noisy and not what we're
	// asserting on). We'll capture stderr separately for the diff.
	// The defer guarantees restoration on panic; the explicit
	// restoreStderr() call later swaps stderr to a capture file
	// for the diff assertion.
	restoreStderr := quietStderr(t)
	defer restoreStderr()

	// Step 1: play 100 ticks, save to dbPath1.
	w1, resolved1, err := runSave(packDir, 100, 7, dbPath1)
	if err != nil {
		restoreStderr()
		t.Fatalf("first runSave: %v", err)
	}
	if resolved1 != dbPath1 {
		restoreStderr()
		t.Errorf("first resolved = %q, want %q", resolved1, dbPath1)
	}
	memCount1 := len(w1.Memories)
	relCount1 := len(w1.Relationships)
	if memCount1 == 0 {
		restoreStderr()
		t.Error("first save has 0 memories; expected many (frontier pack has births/deaths in 100 ticks)")
	}
	if relCount1 == 0 {
		restoreStderr()
		t.Error("first save has 0 relationships; expected many (co-located pairs)")
	}

	// Record the first relationship scores for later comparison.
	// Key → [trust, respect, fear, attraction, loyalty].
	firstRelScores := make(map[string][5]float64)
	for _, r := range w1.Relationships {
		firstRelScores[r.Key()] = [5]float64{r.Trust, r.Respect, r.Fear, r.Attraction, r.Loyalty}
	}

	// Step 2: resume and run 100 more ticks.
	w2, err := runResume(dbPath1, 100)
	if err != nil {
		restoreStderr()
		t.Fatalf("runResume: %v", err)
	}
	if w2.Tick != 200 {
		restoreStderr()
		t.Errorf("Tick after resume = %d, want 200 (100 + 100)", w2.Tick)
	}
	memCount2 := len(w2.Memories)
	relCount2 := len(w2.Relationships)
	if memCount2 <= memCount1 {
		restoreStderr()
		t.Errorf("memories did not increase across the second 100 ticks: db1=%d, db2=%d", memCount1, memCount2)
	}
	if relCount2 < relCount1 {
		restoreStderr()
		t.Errorf("relationships decreased across the second 100 ticks: db1=%d, db2=%d", relCount1, relCount2)
	}

	// Step 3: snapshot the post-resume world to dbPath2. We use
	// the persistence layer directly because runSave would create
	// a fresh world via runPlay, not snapshot the resumed one.
	db2, err := persistence.Open(dbPath2)
	if err != nil {
		restoreStderr()
		t.Fatalf("Open db2: %v", err)
	}
	if err := db2.Migrate(); err != nil {
		restoreStderr()
		_ = db2.Close()
		t.Fatalf("Migrate db2: %v", err)
	}
	if err := db2.Snapshot(w2); err != nil {
		restoreStderr()
		_ = db2.Close()
		t.Fatalf("Snapshot db2: %v", err)
	}
	if err := db2.Close(); err != nil {
		restoreStderr()
		t.Fatalf("Close db2: %v", err)
	}

	// Restore stderr so we can capture diff output cleanly.
	restoreStderr()

	// Step 4: diff the two snapshots (capture stderr).
	stderrPath, captureFn := captureStderrToFile(t)
	defer captureFn()
	if err := runDiff(dbPath1, dbPath2); err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	output := readCapturedStderr(t, stderrPath)

	// Step 5: assert the diff output shows what we expect.
	if !strings.Contains(output, "Tick:      100 -> 200") {
		t.Errorf("expected tick delta 100->200 in diff output, got:\n%s", output)
	}
	if !strings.Contains(output, "Memories:") {
		t.Errorf("expected 'Memories:' section in diff output, got:\n%s", output)
	}
	if !strings.Contains(output, "Relationships:") {
		t.Errorf("expected 'Relationships:' section in diff output, got:\n%s", output)
	}
	if !strings.Contains(output, "differences:") {
		t.Errorf("expected summary line with 'differences:' in diff output, got:\n%s", output)
	}

	// Step 6: verify the relationship scores actually shifted.
	// Count how many relationships that existed in BOTH w1 and w2
	// have different axis values. With DecayRate=0.01/tick over
	// 100 more ticks, every relationship's axes should shift by
	// ~1.0, and new parent→child relationships should have
	// +20/+15 Trust from the MemoryEngine.
	var shiftedCount int
	var comparedCount int
	for _, r := range w2.Relationships {
		key := r.Key()
		if first, ok := firstRelScores[key]; ok {
			comparedCount++
			if first[0] != r.Trust || first[1] != r.Respect ||
				first[2] != r.Fear || first[3] != r.Attraction ||
				first[4] != r.Loyalty {
				shiftedCount++
			}
		}
	}
	if comparedCount == 0 {
		t.Error("no relationships were present in both snapshots; the test setup is broken")
	}
	if shiftedCount == 0 {
		t.Errorf("no relationship scores shifted across %d compared relationships; expected decay + memory deltas to change at least some", comparedCount)
	}
	t.Logf("end-to-end summary: memories %d→%d, relationships %d→%d, %d/%d relationship scores shifted",
		memCount1, memCount2, relCount1, relCount2, shiftedCount, comparedCount)
}

