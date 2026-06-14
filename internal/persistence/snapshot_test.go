package persistence

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// newTestDB opens a fresh database in a temp directory, runs migrations,
// and registers a cleanup to close it.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chronicle-test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestOpenAndMigrate(t *testing.T) {
	db := newTestDB(t)
	v, err := db.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != CurrentVersion {
		t.Errorf("Version = %d, want %d", v, CurrentVersion)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db := newTestDB(t)
	if err := db.Migrate(); err != nil {
		t.Errorf("second Migrate: %v", err)
	}
	v, err := db.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != CurrentVersion {
		t.Errorf("Version after second Migrate = %d, want %d", v, CurrentVersion)
	}
}

func TestOpenCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if db.Path() != path {
		t.Errorf("Path() = %q, want %q", db.Path(), path)
	}
}

func TestSnapshot_RestoreRoundTrip(t *testing.T) {
	db := newTestDB(t)
	start := time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC)
	w := core.NewWorld("round-trip", 42, start)
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", Gender: "F", BirthTick: -20*365, Alive: true, LocationID: "village", FatherID: "pf1", MotherID: "pm1"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", Gender: "M", BirthTick: -30*365, Alive: false, LocationID: "town", DeathTick: 100})
	w.Tick = 100
	w.Now = start.AddDate(0, 0, 100)

	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if loaded.ID != w.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, w.ID)
	}
	if loaded.Seed != w.Seed {
		t.Errorf("Seed = %d, want %d", loaded.Seed, w.Seed)
	}
	if loaded.Tick != w.Tick {
		t.Errorf("Tick = %d, want %d", loaded.Tick, w.Tick)
	}
	if !loaded.Now.Equal(w.Now) {
		t.Errorf("Now = %v, want %v", loaded.Now, w.Now)
	}
	if len(loaded.People) != len(w.People) {
		t.Fatalf("len(People) = %d, want %d", len(loaded.People), len(w.People))
	}
	for id, want := range w.People {
		got, ok := loaded.People[id]
		if !ok {
			t.Errorf("person %s missing from loaded world", id)
			continue
		}
		if got.ID != want.ID {
			t.Errorf("person %s: ID = %q, want %q", id, got.ID, want.ID)
		}
		if got.Name != want.Name {
			t.Errorf("person %s: Name = %q, want %q", id, got.Name, want.Name)
		}
		if got.Gender != want.Gender {
			t.Errorf("person %s: Gender = %q, want %q", id, got.Gender, want.Gender)
		}
		if got.BirthTick != want.BirthTick {
			t.Errorf("person %s: BirthTick = %d, want %d", id, got.BirthTick, want.BirthTick)
		}
		if got.Alive != want.Alive {
			t.Errorf("person %s: Alive = %v, want %v", id, got.Alive, want.Alive)
		}
		if got.LocationID != want.LocationID {
			t.Errorf("person %s: LocationID = %q, want %q", id, got.LocationID, want.LocationID)
		}
		if got.DeathTick != want.DeathTick {
			t.Errorf("person %s: DeathTick = %d, want %d", id, got.DeathTick, want.DeathTick)
		}
		if got.FatherID != want.FatherID {
			t.Errorf("person %s: FatherID = %q, want %q", id, got.FatherID, want.FatherID)
		}
		if got.MotherID != want.MotherID {
			t.Errorf("person %s: MotherID = %q, want %q", id, got.MotherID, want.MotherID)
		}
	}
}

func TestSnapshot_OverwritesPrevious(t *testing.T) {
	db := newTestDB(t)

	w1 := core.NewWorld("world-one", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.AddPerson(&core.Person{ID: "alice", Name: "Alice", Gender: "F", BirthTick: -20*365, Alive: true})
	if err := db.Snapshot(w1); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}

	w2 := core.NewWorld("world-two", 2, time.Date(1500, 6, 15, 0, 0, 0, 0, time.UTC))
	w2.AddPerson(&core.Person{ID: "bob", Name: "Bob", Gender: "M", BirthTick: -40*365, Alive: true})
	if err := db.Snapshot(w2); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}

	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.ID != "world-two" {
		t.Errorf("ID = %q, want world-two", loaded.ID)
	}
	if loaded.Seed != 2 {
		t.Errorf("Seed = %d, want 2", loaded.Seed)
	}
	if _, exists := loaded.People["alice"]; exists {
		t.Errorf("alice from w1 should be gone after overwrite")
	}
	bob, ok := loaded.People["bob"]
	if !ok {
		t.Fatalf("bob missing from loaded world")
	}
	if bob.Name != "Bob" {
		t.Errorf("bob = %+v, want Name=Bob", *bob)
	}
}

func TestRestore_EmptyDB(t *testing.T) {
	db := newTestDB(t)
	loaded := core.NewWorld("default", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore on empty DB: %v", err)
	}
	if loaded.ID != "default" {
		t.Errorf("ID = %q, want default", loaded.ID)
	}
	if loaded.People == nil {
		t.Errorf("People should be non-nil (empty map) after Restore")
	}
	if len(loaded.People) != 0 {
		t.Errorf("len(People) = %d, want 0", len(loaded.People))
	}
}

func TestSnapshot_EmptyWorld(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("empty-world", 7, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.ID != "empty-world" {
		t.Errorf("ID = %q, want empty-world", loaded.ID)
	}
	if loaded.Seed != 7 {
		t.Errorf("Seed = %d, want 7", loaded.Seed)
	}
	if len(loaded.People) != 0 {
		t.Errorf("len(People) = %d, want 0", len(loaded.People))
	}
}

// TestSnapshot_RulesRoundTrip verifies that a world with non-nil
// WorldRules round-trips through Snapshot/Restore: all 8 fields
// (4 Lifecycle, 2 Family, 2 Migration) come back identical.
func TestSnapshot_RulesRoundTrip(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("rules-rt", 99, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.Rules = &core.WorldRules{
		AdultAge:              18,
		FertileMinAge:         17,
		FertileMaxAge:         45,
		AnnualDeathChance:     0.025,
		MinBirthIntervalTicks: 730,
		MaxChildren:           4,
		MigrationFraction:     0.75,
		MinMigrantsPerTick:    3,
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Rules == nil {
		t.Fatal("loaded.Rules is nil; expected populated")
	}
	got, want := *loaded.Rules, *w.Rules
	if got != want {
		t.Errorf("rules mismatch:\n got  %+v\n want %+v", got, want)
	}
}

// TestSnapshot_NoRules verifies that a world with w.Rules == nil
// round-trips correctly: the loaded world also has w.Rules == nil.
// This is the "no worldpack" path — a legacy world or a test world
// without rules.
func TestSnapshot_NoRules(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("no-rules", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if w.Rules != nil {
		t.Fatal("test prerequisite: w.Rules should be nil")
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Rules != nil {
		t.Errorf("loaded.Rules = %+v, want nil", loaded.Rules)
	}
}

// TestSnapshot_RulesOverwrite verifies full-replace semantics: a
// second Snapshot with different Rules replaces the first. This
// matches the existing pattern for world_meta and people.
func TestSnapshot_RulesOverwrite(t *testing.T) {
	db := newTestDB(t)

	w1 := core.NewWorld("first", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.Rules = &core.WorldRules{AnnualDeathChance: 0.10, MigrationFraction: 0.9}
	if err := db.Snapshot(w1); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}

	w2 := core.NewWorld("second", 2, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.Rules = &core.WorldRules{AnnualDeathChance: 0.02, MigrationFraction: 0.4}
	if err := db.Snapshot(w2); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}

	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.ID != "second" {
		t.Errorf("ID = %q, want second", loaded.ID)
	}
	if loaded.Rules == nil {
		t.Fatal("loaded.Rules is nil")
	}
	if loaded.Rules.AnnualDeathChance != 0.02 {
		t.Errorf("AnnualDeathChance: got %f, want 0.02", loaded.Rules.AnnualDeathChance)
	}
	if loaded.Rules.MigrationFraction != 0.4 {
		t.Errorf("MigrationFraction: got %f, want 0.4", loaded.Rules.MigrationFraction)
	}
}

// TestSnapshot_RulesWithZeroValues verifies that zero-valued rules
// (e.g., AnnualDeathChance=0 to disable mortality) are preserved
// through the round-trip. A naive "" -> default-substitution on
// parse would silently turn 0 into a non-zero value.
func TestSnapshot_RulesWithZeroValues(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("zeros", 5, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.Rules = &core.WorldRules{
		AnnualDeathChance:  0,    // explicit: no mortality
		MigrationFraction:  0,    // explicit: no migration
		MinMigrantsPerTick: 0,    // explicit: floor is 0 (engine will use 1 as min)
		FertileMinAge:      0,    // explicit: zero is preserved
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Rules == nil {
		t.Fatal("loaded.Rules is nil")
	}
	if loaded.Rules.AnnualDeathChance != 0 {
		t.Errorf("AnnualDeathChance: got %f, want 0", loaded.Rules.AnnualDeathChance)
	}
	if loaded.Rules.MigrationFraction != 0 {
		t.Errorf("MigrationFraction: got %f, want 0", loaded.Rules.MigrationFraction)
	}
	if loaded.Rules.MinMigrantsPerTick != 0 {
		t.Errorf("MinMigrantsPerTick: got %d, want 0", loaded.Rules.MinMigrantsPerTick)
	}
	if loaded.Rules.FertileMinAge != 0 {
		t.Errorf("FertileMinAge: got %d, want 0", loaded.Rules.FertileMinAge)
	}
}

// TestSnapshot_EngineUsesRestoredRules is the end-to-end test that
// proves the persistence layer + engine integration: a world with
// a non-default AnnualDeathChance is snapshotted, restored into a
// fresh world, and run through the PopulationEngine. The mortality
// outcome (deaths over 1 year) should match what the rules predict,
// proving the engine reads w.Rules from the restored world — not
// just the engine's own field.
func TestSnapshot_EngineUsesRestoredRules(t *testing.T) {
	db := newTestDB(t)

	// Build a world with 1000 people, AnnualDeathChance=0.50, no
	// mortality from the engine field. After a year, ~500 should
	// be alive if the engine honors the restored Rules.
	original := core.NewWorld("engine-rt", 7, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("p%04d", i)
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

	// Restore into a fresh world. Crucially, the new world has
	// w.Rules == nil BEFORE Restore; if Restore repopulates it,
	// the engine will use the pack-driven 0.50.
	loaded := core.NewWorld("", 0, time.Time{})
	if loaded.Rules != nil {
		t.Fatal("test prerequisite: loaded.Rules should be nil before Restore")
	}
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Rules == nil {
		t.Fatal("loaded.Rules is nil after Restore; engine would use defaults")
	}
	if loaded.Rules.AnnualDeathChance != 0.50 {
		t.Fatalf("AnnualDeathChance = %f, want 0.50", loaded.Rules.AnnualDeathChance)
	}

	// Run 1 year of ticks. With AnnualDeathChance=0.50, expected
	// survivors ~ 500. If w.Rules is ignored and the engine field
	// (0.01) drives mortality instead, expected survivors ~ 990
	// (well above the test's [350, 650] range).
	eng := simulation.NewPopulationEngine()
	// The engine's own field is the built-in default (0.01). If
	// w.Rules is honored, the pack's 0.50 drives mortality.
	if eng.AnnualDeathChance == 0.0 {
		t.Fatalf("engine field is 0.0; if w.Rules is ignored, all 1000 would survive — test would be inconclusive")
	}
	if eng.AnnualDeathChance == loaded.Rules.AnnualDeathChance {
		t.Fatalf("engine field matches pack value; test cannot distinguish")
	}
	sim := tick.NewSimulation(7, eng)
	for i := int64(0); i < 365; i++ {
		if err := sim.Tick(loaded); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	alive := 0
	for _, p := range loaded.People {
		if p.Alive {
			alive++
		}
	}
	// With AnnualDeathChance=0.50, expected ~500 alive. Allow a wide
	// margin to avoid flakiness. If w.Rules was not honored (e.g.,
	// Restore silently dropped it), every person would survive
	// (engine field is 0.0), so alive would be 1000. A value in
	// the 350-650 range proves the restored rules drove mortality.
	if alive < 350 || alive > 650 {
		t.Errorf("alive count = %d; expected ~500 (proves restored AnnualDeathChance=0.50 is honored); if 1000, engine is ignoring w.Rules", alive)
	}
}

// TestSnapshot_RelationshipsRoundTrip verifies that a world with
// non-empty w.Relationships round-trips through Snapshot/Restore:
// all 5 axes (trust, respect, fear, attraction, loyalty) plus the
// (from_id, to_id) key come back identical.
func TestSnapshot_RelationshipsRoundTrip(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("rel-rt", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.Relationships = []core.Relationship{
		{FromID: "alice", ToID: "bob", Trust: 50, Respect: 60, Fear: 10, Attraction: 0, Loyalty: 70},
		{FromID: "alice", ToID: "carol", Trust: 80, Respect: 75, Fear: 5, Attraction: 20, Loyalty: 90},
		{FromID: "bob", ToID: "carol", Trust: 30, Respect: 25, Fear: 15, Attraction: 5, Loyalty: 40},
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(loaded.Relationships) != len(w.Relationships) {
		t.Fatalf("len(Relationships) = %d, want %d", len(loaded.Relationships), len(w.Relationships))
	}
	// Index by key for order-independent comparison.
	got := make(map[string]core.Relationship, len(loaded.Relationships))
	for _, r := range loaded.Relationships {
		got[r.Key()] = r
	}
	for _, want := range w.Relationships {
		g, ok := got[want.Key()]
		if !ok {
			t.Errorf("relationship %s missing from loaded world", want.Key())
			continue
		}
		if g.FromID != want.FromID {
			t.Errorf("%s: FromID = %q, want %q", want.Key(), g.FromID, want.FromID)
		}
		if g.ToID != want.ToID {
			t.Errorf("%s: ToID = %q, want %q", want.Key(), g.ToID, want.ToID)
		}
		if g.Trust != want.Trust {
			t.Errorf("%s: Trust = %f, want %f", want.Key(), g.Trust, want.Trust)
		}
		if g.Respect != want.Respect {
			t.Errorf("%s: Respect = %f, want %f", want.Key(), g.Respect, want.Respect)
		}
		if g.Fear != want.Fear {
			t.Errorf("%s: Fear = %f, want %f", want.Key(), g.Fear, want.Fear)
		}
		if g.Attraction != want.Attraction {
			t.Errorf("%s: Attraction = %f, want %f", want.Key(), g.Attraction, want.Attraction)
		}
		if g.Loyalty != want.Loyalty {
			t.Errorf("%s: Loyalty = %f, want %f", want.Key(), g.Loyalty, want.Loyalty)
		}
	}
}

// TestSnapshot_MemoriesRoundTrip verifies that a world with
// non-empty w.Memories round-trips through Snapshot/Restore: all
// 12 fields (ID, OwnerID, EventID, CauseEventID, Tick, Importance,
// Recency, EmotionalScore, TrustDelta, RelationshipDelta,
// Description, Tags) come back identical. The Tags slice is JSON-
// decoded; we compare the slice contents (not the exact encoding).
func TestSnapshot_MemoriesRoundTrip(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("mem-rt", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.Memories = []core.Memory{
		{
			ID:                "m1",
			OwnerID:           "alice",
			EventID:           "e_fire_001",
			CauseEventID:      "",
			Tick:              100,
			Importance:        0.85,
			Recency:           1.0,
			EmotionalScore:    0.6,
			TrustDelta:        -15.0,
			RelationshipDelta: -10.0,
			Description:       "House burned down",
			Tags:              []string{"fire", "loss"},
		},
		{
			ID:                "m2",
			OwnerID:           "bob",
			EventID:           "e_wedding_002",
			CauseEventID:      "e_proposal_001",
			Tick:              250,
			Importance:        0.95,
			Recency:           1.0,
			EmotionalScore:    0.9,
			TrustDelta:        20.0,
			RelationshipDelta: 25.0,
			Description:       "Married carol",
			Tags:              []string{"wedding", "joy"},
		},
		{
			// Edge case: nil/empty optional fields.
			ID:          "m3",
			OwnerID:     "carol",
			EventID:     "",
			Tick:        300,
			Importance:  0.5,
			Recency:     0.8,
			Description: "Quiet day",
		},
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(loaded.Memories) != len(w.Memories) {
		t.Fatalf("len(Memories) = %d, want %d", len(loaded.Memories), len(w.Memories))
	}
	// Index by ID for order-independent comparison.
	got := make(map[string]core.Memory, len(loaded.Memories))
	for _, m := range loaded.Memories {
		got[m.ID] = m
	}
	for _, want := range w.Memories {
		g, ok := got[want.ID]
		if !ok {
			t.Errorf("memory %s missing from loaded world", want.ID)
			continue
		}
		if g.OwnerID != want.OwnerID {
			t.Errorf("%s: OwnerID = %q, want %q", want.ID, g.OwnerID, want.OwnerID)
		}
		if g.EventID != want.EventID {
			t.Errorf("%s: EventID = %q, want %q", want.ID, g.EventID, want.EventID)
		}
		if g.CauseEventID != want.CauseEventID {
			t.Errorf("%s: CauseEventID = %q, want %q", want.ID, g.CauseEventID, want.CauseEventID)
		}
		if g.Tick != want.Tick {
			t.Errorf("%s: Tick = %d, want %d", want.ID, g.Tick, want.Tick)
		}
		if g.Importance != want.Importance {
			t.Errorf("%s: Importance = %f, want %f", want.ID, g.Importance, want.Importance)
		}
		if g.Recency != want.Recency {
			t.Errorf("%s: Recency = %f, want %f", want.ID, g.Recency, want.Recency)
		}
		if g.EmotionalScore != want.EmotionalScore {
			t.Errorf("%s: EmotionalScore = %f, want %f", want.ID, g.EmotionalScore, want.EmotionalScore)
		}
		if g.TrustDelta != want.TrustDelta {
			t.Errorf("%s: TrustDelta = %f, want %f", want.ID, g.TrustDelta, want.TrustDelta)
		}
		if g.RelationshipDelta != want.RelationshipDelta {
			t.Errorf("%s: RelationshipDelta = %f, want %f", want.ID, g.RelationshipDelta, want.RelationshipDelta)
		}
		if g.Description != want.Description {
			t.Errorf("%s: Description = %q, want %q", want.ID, g.Description, want.Description)
		}
		// Tags: compare as sets (order-independent) since the spec
		// treats them as a filter/grouping mechanism.
		if !sameStringSet(g.Tags, want.Tags) {
			t.Errorf("%s: Tags = %v, want %v", want.ID, g.Tags, want.Tags)
		}
	}
}

// sameStringSet reports whether two []string contain the same
// elements regardless of order. Empty/nil are considered equal.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}

// TestSnapshot_RelationshipsOverwrite verifies full-replace
// semantics for the relationships table: a second Snapshot with
// different relationships replaces the first. Matches the existing
// pattern for world_meta, people, and world_rules.
func TestSnapshot_RelationshipsOverwrite(t *testing.T) {
	db := newTestDB(t)

	w1 := core.NewWorld("first", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.Relationships = []core.Relationship{
		{FromID: "alice", ToID: "bob", Trust: 50},
		{FromID: "alice", ToID: "carol", Trust: 80},
	}
	if err := db.Snapshot(w1); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}

	w2 := core.NewWorld("second", 2, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.Relationships = []core.Relationship{
		{FromID: "dave", ToID: "eve", Trust: 30},
	}
	if err := db.Snapshot(w2); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}

	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.ID != "second" {
		t.Errorf("ID = %q, want second", loaded.ID)
	}
	if len(loaded.Relationships) != 1 {
		t.Fatalf("len(Relationships) = %d, want 1", len(loaded.Relationships))
	}
	r := loaded.Relationships[0]
	if r.FromID != "dave" || r.ToID != "eve" || r.Trust != 30 {
		t.Errorf("relationship = %+v, want {dave eve 30}", r)
	}
}

// TestSnapshot_MemoriesOverwrite verifies full-replace semantics
// for the memories table: a second Snapshot with different memories
// replaces the first.
func TestSnapshot_MemoriesOverwrite(t *testing.T) {
	db := newTestDB(t)

	w1 := core.NewWorld("first", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w1.Memories = []core.Memory{
		{ID: "m1", OwnerID: "alice", Tick: 100, Importance: 0.5},
		{ID: "m2", OwnerID: "bob", Tick: 200, Importance: 0.7},
	}
	if err := db.Snapshot(w1); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}

	w2 := core.NewWorld("second", 2, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w2.Memories = []core.Memory{
		{ID: "m3", OwnerID: "carol", Tick: 300, Importance: 0.9},
	}
	if err := db.Snapshot(w2); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}

	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.ID != "second" {
		t.Errorf("ID = %q, want second", loaded.ID)
	}
	if len(loaded.Memories) != 1 {
		t.Fatalf("len(Memories) = %d, want 1", len(loaded.Memories))
	}
	m := loaded.Memories[0]
	if m.ID != "m3" || m.OwnerID != "carol" || m.Tick != 300 {
		t.Errorf("memory = %+v, want {m3 carol 300}", m)
	}
}

// TestSnapshot_NoRelationships verifies that a world with an empty
// Relationships slice round-trips correctly: the loaded world also
// has an empty (non-nil) slice. This is the common case for a
// freshly bootstrapped world where the RelationshipEngine hasn't
// created any bonds yet.
func TestSnapshot_NoRelationships(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("no-rel", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(w.Relationships) != 0 {
		t.Fatal("test prerequisite: Relationships should be empty")
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Relationships == nil {
		t.Errorf("Relationships is nil; expected non-nil empty slice")
	}
	if len(loaded.Relationships) != 0 {
		t.Errorf("len(Relationships) = %d, want 0", len(loaded.Relationships))
	}
}

// TestSnapshot_NoMemories verifies that a world with an empty
// Memories slice round-trips correctly: the loaded world also has
// an empty (non-nil) slice. The JSON-decoding path in readMemories
// must not leave the slice as nil.
func TestSnapshot_NoMemories(t *testing.T) {
	db := newTestDB(t)
	w := core.NewWorld("no-mem", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(w.Memories) != 0 {
		t.Fatal("test prerequisite: Memories should be empty")
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if loaded.Memories == nil {
		t.Errorf("Memories is nil; expected non-nil empty slice")
	}
	if len(loaded.Memories) != 0 {
		t.Errorf("len(Memories) = %d, want 0", len(loaded.Memories))
	}
}

// TestSnapshot_ResumeEngineReadsMemories is the end-to-end test
// that proves memories survive a full save/resume cycle: a world
// with non-empty Memories is snapshotted, restored into a fresh
// world, and the loaded world is re-snapshotted into a second DB.
// The second DB's contents (when restored into a third world) must
// match the first — proving the full save → restore → re-save →
// restore round-trip preserves the causal memory graph.
func TestSnapshot_ResumeEngineReadsMemories(t *testing.T) {
	db1 := newTestDB(t)
	w := core.NewWorld("mem-rt2", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.Memories = []core.Memory{
		{ID: "m1", OwnerID: "alice", EventID: "e1", Tick: 100, Importance: 0.8, Description: "first memory"},
		{ID: "m2", OwnerID: "bob", EventID: "e2", CauseEventID: "e1", Tick: 200, Importance: 0.6, Description: "caused by e1"},
	}
	w.Relationships = []core.Relationship{
		{FromID: "alice", ToID: "bob", Trust: 60, Loyalty: 40},
	}
	if err := db1.Snapshot(w); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}

	// Restore into a fresh world.
	mid := core.NewWorld("", 0, time.Time{})
	if err := db1.Restore(mid); err != nil {
		t.Fatalf("first Restore: %v", err)
	}
	if len(mid.Memories) != 2 || len(mid.Relationships) != 1 {
		t.Fatalf("mid: got %d memories, %d relationships; want 2, 1",
			len(mid.Memories), len(mid.Relationships))
	}

	// Re-snapshot into a second DB and restore into a third world.
	// Use a different *DB to force a fresh file (and prove the
	// second Snapshot writes a complete set of rows, not just a
	// delta).
	db2Path := filepath.Join(t.TempDir(), "second.db")
	db2, err := Open(db2Path)
	if err != nil {
		t.Fatalf("Open db2: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	if err := db2.Migrate(); err != nil {
		t.Fatalf("db2 Migrate: %v", err)
	}
	if err := db2.Snapshot(mid); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	final := core.NewWorld("", 0, time.Time{})
	if err := db2.Restore(final); err != nil {
		t.Fatalf("second Restore: %v", err)
	}
	if len(final.Memories) != 2 {
		t.Errorf("final: len(Memories) = %d, want 2", len(final.Memories))
	}
	if len(final.Relationships) != 1 {
		t.Errorf("final: len(Relationships) = %d, want 1", len(final.Relationships))
	}
	// Spot-check the causal chain survived.
	var m1, m2 core.Memory
	for _, m := range final.Memories {
		if m.ID == "m1" {
			m1 = m
		}
		if m.ID == "m2" {
			m2 = m
		}
	}
	if m1.CauseEventID != "" {
		t.Errorf("m1.CauseEventID = %q, want empty (root event)", m1.CauseEventID)
	}
	if m2.CauseEventID != "e1" {
		t.Errorf("m2.CauseEventID = %q, want e1", m2.CauseEventID)
	}
}
