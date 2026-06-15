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
	_, pack, err := Load(filepath.Clean(frontierDir))
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

// TestBootstrap_AutoPlayerID verifies that Bootstrap
// auto-designates a PlayerID so the action engine's
// travel/buy/sell handlers work out of the box. Without
// this, `chronicle new` would produce a world where
// travel/buy/sell fail with "You need a player character".
// The player must be a non-merchant alive person at
// blackwater (the main town with a merchant), with a
// deterministic choice (sorted ID).
func TestBootstrap_AutoPlayerID(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	w := core.NewWorld("autoplayer", 42, time.Unix(0, 0))
	if err := Bootstrap(pack, w, 42); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if w.PlayerID == "" {
		t.Fatal("Bootstrap did not auto-set PlayerID")
	}
	player, ok := w.People[w.PlayerID]
	if !ok {
		t.Fatalf("PlayerID %q not in w.People", w.PlayerID)
	}
	if !player.Alive {
		t.Errorf("auto-designated player %q is not alive", w.PlayerID)
	}
	if player.IsMerchant {
		t.Errorf("auto-designated player %q is a merchant (should be a regular person)", w.PlayerID)
	}
	if player.LocationID != "blackwater" {
		t.Errorf("auto-designated player %q is at %q, want 'blackwater'", w.PlayerID, player.LocationID)
	}
}

// TestBootstrap_PreservesExplicitPlayerID verifies that if
// the world already has a PlayerID set before Bootstrap
// (e.g., a test or a future worldpack config), Bootstrap
// does not overwrite it. The auto-designation is a default,
// not a mandate.
func TestBootstrap_PreservesExplicitPlayerID(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	w := core.NewWorld("explicit", 42, time.Unix(0, 0))
	w.PlayerID = "n0001" // explicit; should be preserved
	if err := Bootstrap(pack, w, 42); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if w.PlayerID != "n0001" {
		t.Errorf("Bootstrap overwrote explicit PlayerID: got %q, want n0001", w.PlayerID)
	}
}

// TestBootstrap_RegionDefaultPlayerLocation verifies that
// the region.default_player_location field is loaded from
// the worldpack and respected by Bootstrap. The frontier
// pack declares "blackwater", so the auto-designated player
// should be at blackwater. A custom pack that declares
// "dawn_monastery" should place the auto-designated player
// at dawn_monastery instead. This makes the
// implicit-player choice worldpack-driven instead of
// hardcoded in bootstrap.go.
func TestBootstrap_RegionDefaultPlayerLocation(t *testing.T) {
	pack := loadFrontierOrSkip(t)
	if pack.Region.DefaultPlayerLocation != "blackwater" {
		t.Fatalf("frontier pack DefaultPlayerLocation: got %q, want 'blackwater' (regression: YAML key rename?)", pack.Region.DefaultPlayerLocation)
	}

	// Build a minimal in-memory pack with dawn_monastery as the
	// default player location, populated with 5 people (the
	// clergy bucket's 5 priests). The auto-designated player
	// should be a non-merchant alive priest at dawn_monastery.
	customPack := &Pack{
		Region: Region{
			Name:                   "Test Region",
			DefaultPlayerLocation:  "dawn_monastery",
		},
		Locations: []LocationSpec{
			{ID: "dawn_monastery", Name: "Dawn Monastery", Kind: "monastery", PopulationCap: 10},
		},
		Occupations: []OccupationSpec{
			{ID: "priest", Name: "Priest", SocialClass: "lower", NeedsWeights: map[string]float64{"devout": 2.0}},
		},
		Generation: GenerationSpec{
			Population: PopulationSpec{Total: 5},
			GenderRatio: GenderRatio{Female: 0.5},
			AgeDistribution: []AgeBracket{{Range: [2]int{20, 40}, Share: 1.0}},
			LocationDistribution: []LocationBucket{
				{Location: "clergy", Count: 5}, // the "clergy" bucket maps to dawn_monastery
			},
			SocialClassDistribution: []ClassBucket{{Class: "lower", Share: 1.0}},
			Names: NamePools{
				Male:     []string{"Alaric"},
				Female:   []string{"Mira"},
				Surnames: []string{"of the Dawn"},
			},
		},
		Rules: RulesSpec{
			Lifecycle: LifecycleSpec{AdultAge: 16, FertileMinAge: 16, FertileMaxAge: 50, AnnualDeathChance: 0.01},
			Family:    FamilySpec{MinBirthIntervalTicks: 365, MaxChildren: 6},
		},
	}
	w := core.NewWorld("customregion", 42, time.Unix(0, 0))
	if err := Bootstrap(customPack, w, 42); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if w.PlayerID == "" {
		t.Fatal("Bootstrap did not auto-set PlayerID for custom pack")
	}
	player, ok := w.People[w.PlayerID]
	if !ok {
		t.Fatalf("PlayerID %q not in w.People", w.PlayerID)
	}
	if !player.Alive {
		t.Errorf("auto-designated player %q is not alive", w.PlayerID)
	}
	if player.IsMerchant {
		t.Errorf("auto-designated player %q is a merchant", w.PlayerID)
	}
	if player.LocationID != "dawn_monastery" {
		t.Errorf("auto-designated player %q is at %q, want 'dawn_monastery' (region.default_player_location not respected)", w.PlayerID, player.LocationID)
	}
}

// TestBootstrap_AutoPlayerID_FallsBackWhenNoOneAtLocation
// verifies the fallback chain: if region.default_player_location
// is set but no qualifying (alive, non-merchant) person happens
// to be at that location, Bootstrap falls back to the first
// alive person by sorted ID. This keeps the auto-designation
// robust against worldpack authors declaring a "main town" that
// happens to be empty in some run.
func TestBootstrap_AutoPlayerID_FallsBackWhenNoOneAtLocation(t *testing.T) {
	customPack := &Pack{
		Region: Region{
			Name:                  "Empty Main Town",
			DefaultPlayerLocation: "ghost_town", // nobody lives here
		},
		Locations: []LocationSpec{
			{ID: "ghost_town", Name: "Ghost Town", Kind: "town", PopulationCap: 10},
			{ID: "real_town", Name: "Real Town", Kind: "town", PopulationCap: 80},
		},
		Occupations: []OccupationSpec{
			{ID: "farmer", Name: "Farmer", SocialClass: "lower", NeedsWeights: map[string]float64{"hunger": 1.0}},
		},
		Generation: GenerationSpec{
			Population: PopulationSpec{Total: 5},
			GenderRatio: GenderRatio{Female: 0.5},
			AgeDistribution: []AgeBracket{{Range: [2]int{20, 40}, Share: 1.0}},
			LocationDistribution: []LocationBucket{
				{Location: "real_town", Count: 5}, // all 5 people at real_town
			},
			SocialClassDistribution: []ClassBucket{{Class: "lower", Share: 1.0}},
			Names: NamePools{
				Male:     []string{"Bren"},
				Female:   []string{"Sera"},
				Surnames: []string{"of the Vale"},
			},
		},
		Rules: RulesSpec{
			Lifecycle: LifecycleSpec{AdultAge: 16, FertileMinAge: 16, FertileMaxAge: 50, AnnualDeathChance: 0.01},
			Family:    FamilySpec{MinBirthIntervalTicks: 365, MaxChildren: 6},
		},
	}
	w := core.NewWorld("fallback", 42, time.Unix(0, 0))
	if err := Bootstrap(customPack, w, 42); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if w.PlayerID == "" {
		// We have 5 alive people (the fallback should fire) — failing
		// here means the fallback path is broken.
		t.Fatal("Bootstrap did not auto-set PlayerID; expected fallback to first alive person")
	}
	player, ok := w.People[w.PlayerID]
	if !ok {
		t.Fatalf("PlayerID %q not in w.People", w.PlayerID)
	}
	if !player.Alive {
		t.Errorf("auto-designated player %q is not alive", w.PlayerID)
	}
	if player.LocationID == "ghost_town" {
		t.Errorf("auto-designated player %q is at empty ghost_town; fallback should have picked real_town", w.PlayerID)
	}
	// With 5 people sorted by ID, the first is n0001. The sorted-ID
	// tie-break is the documented deterministic fallback.
	if w.PlayerID != "n0001" {
		t.Errorf("fallback PlayerID: got %q, want n0001 (first alive person by sorted ID)", w.PlayerID)
	}
}
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
