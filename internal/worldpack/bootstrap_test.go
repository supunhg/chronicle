package worldpack

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

const frontierDir = "../../worldpacks/frontier"

func loadFrontierOrSkip(t *testing.T) *Pack {
	t.Helper()
	pack, err := Load(filepath.Clean(frontierDir))
	if err != nil {
		t.Skipf("frontier pack not available: %v", err)
	}
	return pack
}

// TestBootstrap_Frontier loads the frontier pack and creates a fresh
// world. Verifies the structural invariants from the spec.
func TestBootstrap_Frontier(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	w := core.NewWorld("test01", 12345, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := Bootstrap(pack, w, 12345); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// 7 locations created
	if got, want := len(w.Locations), 7; got != want {
		t.Errorf("locations: got %d, want %d", got, want)
	}

	// 4 factions created
	if got, want := len(w.Factions), 4; got != want {
		t.Errorf("factions: got %d, want %d", got, want)
	}

	// 150 people
	if got, want := len(w.People), 150; got != want {
		t.Errorf("people: got %d, want %d", got, want)
	}

	// Blackwater has 85 people: 80 from the "blackwater" bucket plus
	// 5 from the "nobility" bucket (the nobility live in blackwater
	// and are upper class). The 85 exceeds the population_cap of 80,
	// which is the intended trigger for population pressure /
	// migration in Phase 5. Of the 85, exactly 5 are class="upper":
	// the "nobility" bucket is the only source of upper class — the
	// 5% share in social_class_distribution is filtered out to avoid
	// double-counting.
	if got, want := w.Locations["blackwater"].Population, 85; got != want {
		t.Errorf("blackwater.Population: got %d, want %d", got, want)
	}
	upperCount := 0
	for _, p := range w.LivingPeopleAt("blackwater") {
		if p.Class == "upper" {
			upperCount++
		}
	}
	if upperCount != 5 {
		t.Errorf("blackwater upper-class count: got %d, want 5", upperCount)
	}

	// Villages total 50 (12/13/12/13 split, exact distribution may
	// differ because the slots are shuffled; the sum is the invariant).
	villageSum := 0
	for _, id := range []string{"millbrook", "ashford", "thornkeep", "elderfield"} {
		villageSum += w.Locations[id].Population
	}
	if got, want := villageSum, 50; got != want {
		t.Errorf("village sum: got %d, want %d", got, want)
	}

	// Dawn monastery has 5 (the "clergy" bucket), all with occupation
	// "priest" per the slot's OccOverride.
	if got, want := w.Locations["dawn_monastery"].Population, 5; got != want {
		t.Errorf("dawn_monastery.Population: got %d, want %d", got, want)
	}
	priestCount := 0
	for _, p := range w.LivingPeopleAt("dawn_monastery") {
		if p.Occupation == "priest" {
			priestCount++
		}
	}
	if priestCount != 5 {
		t.Errorf("monastery priest count: got %d, want 5", priestCount)
	}

	// Northwatch fort starts empty: entities.yaml says population: 5,
	// but generation.yaml has no fort bucket, so the fort gets its
	// initial inhabitants via migration in Phase 5+.
	if got, want := w.Locations["northwatch_fort"].Population, 0; got != want {
		t.Errorf("northwatch_fort.Population: got %d, want %d", got, want)
	}

	// 10 travelers with empty LocationID
	if got, want := len(w.LivingPeopleAt("")), 10; got != want {
		t.Errorf("travelers: got %d, want %d", got, want)
	}

	// Every person is alive
	for id, p := range w.People {
		if !p.Alive {
			t.Errorf("person %s should be alive at bootstrap", id)
		}
	}

	// Every person has a name, occupation, and class
	for id, p := range w.People {
		if p.Name == "" {
			t.Errorf("person %s missing name", id)
		}
		if p.Occupation == "" {
			t.Errorf("person %s missing occupation", id)
		}
		if p.Class == "" {
			t.Errorf("person %s missing class", id)
		}
	}

	// Faction member occupations look right
	mg, ok := w.Factions["merchant_guild"]
	if !ok {
		t.Fatal("merchant_guild faction missing")
	}
	if len(mg.MemberOccupations) == 0 {
		t.Error("merchant_guild has no member occupations")
	}
}

// TestBootstrap_PopulatesRules verifies that Bootstrap projects
// pack.Rules.Lifecycle and pack.Rules.Family into w.Rules so engines
// can read tunable parameters from the worldpack.
func TestBootstrap_PopulatesRules(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	w := core.NewWorld("rules", 1, time.Unix(0, 0))
	if err := Bootstrap(pack, w, 1); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if w.Rules == nil {
		t.Fatal("w.Rules not set after Bootstrap")
	}
	// Frontier lifecycle (per worldpacks/frontier/rules.yaml)
	if w.Rules.AnnualDeathChance != 0.01 {
		t.Errorf("AnnualDeathChance: got %f, want 0.01", w.Rules.AnnualDeathChance)
	}
	if w.Rules.AdultAge != 16 {
		t.Errorf("AdultAge: got %d, want 16", w.Rules.AdultAge)
	}
	if w.Rules.FertileMinAge != 16 {
		t.Errorf("FertileMinAge: got %d, want 16", w.Rules.FertileMinAge)
	}
	if w.Rules.FertileMaxAge != 50 {
		t.Errorf("FertileMaxAge: got %d, want 50", w.Rules.FertileMaxAge)
	}
	// Frontier family
	if w.Rules.MinBirthIntervalTicks != 365 {
		t.Errorf("MinBirthIntervalTicks: got %d, want 365", w.Rules.MinBirthIntervalTicks)
	}
	if w.Rules.MaxChildren != 6 {
		t.Errorf("MaxChildren: got %d, want 6", w.Rules.MaxChildren)
	}
	// Frontier migration
	if w.Rules.MigrationFraction != 0.5 {
		t.Errorf("MigrationFraction: got %f, want 0.5", w.Rules.MigrationFraction)
	}
	if w.Rules.MinMigrantsPerTick != 1 {
		t.Errorf("MinMigrantsPerTick: got %d, want 1", w.Rules.MinMigrantsPerTick)
	}
}

// TestWorldRulesOrDefault_NilAndNonNil verifies the core fallback
// for worlds that have no worldpack, and the verbatim-return
// behavior when Rules is set (zero values are legitimate).
func TestWorldRulesOrDefault_NilAndNonNil(t *testing.T) {
	// nil Rules -> defaults
	w := core.NewWorld("norules", 1, time.Unix(0, 0))
	rules := w.RulesOrDefault()
	if rules.AdultAge != 16 || rules.FertileMinAge != 16 || rules.FertileMaxAge != 50 {
		t.Errorf("default rules wrong: %+v", rules)
	}
	if rules.AnnualDeathChance != 0.01 {
		t.Errorf("default AnnualDeathChance: got %f, want 0.01", rules.AnnualDeathChance)
	}

	// Non-nil Rules -> returned verbatim (zero values are legitimate)
	w.Rules = &core.WorldRules{AnnualDeathChance: 0.5, FertileMinAge: 18}
	got := w.RulesOrDefault()
	if got.AnnualDeathChance != 0.5 || got.FertileMinAge != 18 {
		t.Errorf("Rules not returned: %+v", got)
	}
}

// TestBootstrap_Deterministic verifies that bootstrapping twice with
// the same seed produces identical worlds.
func TestBootstrap_Deterministic(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	w1 := core.NewWorld("w1", 42, time.Unix(0, 0))
	w2 := core.NewWorld("w2", 42, time.Unix(0, 0))
	if err := Bootstrap(pack, w1, 42); err != nil {
		t.Fatalf("Bootstrap w1: %v", err)
	}
	if err := Bootstrap(pack, w2, 42); err != nil {
		t.Fatalf("Bootstrap w2: %v", err)
	}
	if len(w1.People) != len(w2.People) {
		t.Fatalf("people count: w1=%d, w2=%d", len(w1.People), len(w2.People))
	}
	for id, p1 := range w1.People {
		p2, ok := w2.People[id]
		if !ok {
			t.Errorf("w2 missing person %s", id)
			continue
		}
		if p1.Gender != p2.Gender || p1.BirthTick != p2.BirthTick ||
			p1.LocationID != p2.LocationID || p1.Class != p2.Class ||
			p1.Occupation != p2.Occupation || p1.Name != p2.Name {
			t.Errorf("person %s differs:\n w1=%+v\n w2=%+v", id, p1, p2)
		}
	}
}

// TestBootstrap_DifferentSeeds ensures that different seeds produce
// different worlds (sanity check on the RNG).
func TestBootstrap_DifferentSeeds(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	w1 := core.NewWorld("a", 1, time.Unix(0, 0))
	w2 := core.NewWorld("b", 2, time.Unix(0, 0))
	if err := Bootstrap(pack, w1, 1); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(pack, w2, 2); err != nil {
		t.Fatal(err)
	}
	// Names should not be identical for many people
	diff := 0
	for id, p1 := range w1.People {
		if p2, ok := w2.People[id]; ok && p1.Name != p2.Name {
			diff++
		}
	}
	if diff < 50 {
		t.Errorf("expected at least 50 different names across seeds, got %d", diff)
	}
}

// TestBootstrap_PersonIDFormat verifies that all Person IDs are
// "n" + 4-digit number, matching the deterministic generation scheme.
func TestBootstrap_PersonIDFormat(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	w := core.NewWorld("fmt", 7, time.Unix(0, 0))
	if err := Bootstrap(pack, w, 7); err != nil {
		t.Fatal(err)
	}
	for id := range w.People {
		if len(id) != 5 || id[0] != 'n' {
			t.Errorf("person %s: expected format nNNNN", id)
			break
		}
	}
}
