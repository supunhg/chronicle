package simulation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

func newPopWorld(t *testing.T, seed int64) *core.World {
	t.Helper()
	start := time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC)
	return core.NewWorld("test", seed, start)
}

func TestPopulationEngine_AgesViaBirthTick(t *testing.T) {
	w := newPopWorld(t, 42)
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	sim := tick.NewSimulation(42, NewPopulationEngine())
	for i := int64(0); i < 365; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	if got := w.People["p1"].AgeAt(w.Tick); got != 1 {
		t.Errorf("after 1 year, AgeAt = %d, want 1", got)
	}
}

func TestPopulationEngine_DeterministicDeaths(t *testing.T) {
	const population = 50
	const years = 10

	run := func() (alive int, finalTick int64) {
		w := newPopWorld(t, 999)
		for i := 0; i < population; i++ {
			id := fmt.Sprintf("p%03d", i)
			w.AddPerson(&core.Person{
				ID:        id,
				Name:      id,
				Gender:    "F",
				BirthTick: -int64((20 + i%50) * 365),
				Alive:     true,
			})
		}
		eng := &PopulationEngine{AnnualDeathChance: 0.10}
		sim := tick.NewSimulation(999, eng)
		for i := int64(0); i < int64(years*365); i++ {
			if err := sim.Tick(w); err != nil {
				t.Fatalf("Tick: %v", err)
			}
		}
		for _, p := range w.People {
			if p.Alive {
				alive++
			}
		}
		return alive, w.Tick
	}

	a1, t1 := run()
	a2, t2 := run()
	if t1 != t2 {
		t.Fatalf("tick diverged: %d vs %d", t1, t2)
	}
	if a1 != a2 {
		t.Errorf("alive count diverged: %d vs %d (determinism broken)", a1, a2)
	}
}

func TestPopulationEngine_CreateChildSetsFamilyLinks(t *testing.T) {
	// Deterministic test of createChild: bypasses the conception roll.
	w := newPopWorld(t, 7)
	w.AddPerson(&core.Person{ID: "m1", Name: "Mira", Gender: "F", BirthTick: -20 * 365, Alive: true, SpouseID: "f1"})
	w.AddPerson(&core.Person{ID: "f1", Name: "Finn", Gender: "M", BirthTick: -22 * 365, Alive: true, SpouseID: "m1"})
	eng := NewPopulationEngine()
	w.Tick = 100

	mother := w.People["m1"]
	father := w.People["f1"]
	child := eng.CreateChild(w, mother, father)
	if child == nil {
		t.Fatal("CreateChild returned nil")
	}
	if child.MotherID != "m1" {
		t.Errorf("MotherID = %q, want m1", child.MotherID)
	}
	if child.FatherID != "f1" {
		t.Errorf("FatherID = %q, want f1", child.FatherID)
	}
	if child.BirthTick != 100 {
		t.Errorf("BirthTick = %d, want 100", child.BirthTick)
	}
	if !child.Alive {
		t.Errorf("child should be alive")
	}
	if !strings.HasPrefix(child.ID, "c100-") {
		t.Errorf("ID = %q, want prefix c100-", child.ID)
	}
	if child.Gender != "M" && child.Gender != "F" {
		t.Errorf("Gender = %q, want M or F", child.Gender)
	}
	if child.LocationID != "" {
		// The mother has no LocationID set in this test; child should match.
		t.Errorf("LocationID = %q, want empty (mother's location)", child.LocationID)
	}
}

func TestPopulationEngine_MaxChildrenEnforced(t *testing.T) {
	w := newPopWorld(t, 21)
	w.AddPerson(&core.Person{ID: "m1", Name: "Mira", Gender: "F", BirthTick: -20 * 365, Alive: true, SpouseID: "f1"})
	w.AddPerson(&core.Person{ID: "f1", Name: "Finn", Gender: "M", BirthTick: -22 * 365, Alive: true, SpouseID: "m1"})
	eng := NewPopulationEngine()
	mother := w.People["m1"]
	father := w.People["f1"]

	// Manually create MaxChildren children with proper spacing.
	for i := 0; i < MaxChildren; i++ {
		w.Tick = int64(i+1) * MinBirthIntervalTicks
		eng.CreateChild(w, mother, father)
	}

	// Verify count.
	count := 0
	for _, p := range w.People {
		if p.MotherID == "m1" {
			count++
		}
	}
	if count != MaxChildren {
		t.Fatalf("after creating %d children, count = %d, want %d", MaxChildren, count, MaxChildren)
	}

	// Now run sim ticks; the births() function should refuse to create a 7th.
	sim := tick.NewSimulation(21, eng)
	startCount := len(w.People)
	for i := int64(0); i < 5*365; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	endCount := len(w.People)
	if endCount > startCount {
		t.Errorf("with MaxChildren reached, %d new people were created in 5 years; want 0", endCount-startCount)
	}
}

func TestPopulationEngine_BirthsRespectMinInterval(t *testing.T) {
	// The interval check happens BEFORE the conception roll, so this
	// test is deterministic: even with luck on the RNG, the mother
	// is skipped because her last birth was too recent.
	w := newPopWorld(t, 13)
	w.AddPerson(&core.Person{ID: "m1", Name: "Mira", Gender: "F", BirthTick: -20 * 365, Alive: true, SpouseID: "f1"})
	w.AddPerson(&core.Person{ID: "f1", Name: "Finn", Gender: "M", BirthTick: -22 * 365, Alive: true, SpouseID: "m1"})
	eng := &PopulationEngine{AnnualDeathChance: 0.0}
	sim := tick.NewSimulation(13, eng)
	mother := w.People["m1"]
	father := w.People["f1"]

	// Manually create one child at tick 100.
	w.Tick = 100
	eng.CreateChild(w, mother, father)

	// Run ticks within MinBirthIntervalTicks. Mortality is zero, so the
	// only new entries would be births — which the interval should block.
	startCount := len(w.People)
	for i := int64(0); i < MinBirthIntervalTicks-1; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	endCount := len(w.People)
	if endCount > startCount {
		t.Errorf("%d new births within MinBirthIntervalTicks; want 0 (interval check failed)", endCount-startCount)
	}
}

func TestPopulationEngine_MalesCannotBeMothers(t *testing.T) {
	// The births() loop only considers females. Even if a male has a
	// spouse, no child will have him as MotherID.
	w := newPopWorld(t, 33)
	w.AddPerson(&core.Person{ID: "m2", Name: "Mark", Gender: "M", BirthTick: -20 * 365, Alive: true, SpouseID: "f2"})
	w.AddPerson(&core.Person{ID: "f2", Name: "Fay", Gender: "F", BirthTick: -22 * 365, Alive: true, SpouseID: "m2"})

	eng := &PopulationEngine{AnnualDeathChance: 0.0}
	sim := tick.NewSimulation(33, eng)
	for i := int64(0); i < 10*365; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}

	maleMothers := 0
	for _, p := range w.People {
		if p.MotherID == "m2" {
			maleMothers++
		}
	}
	if maleMothers > 0 {
		t.Errorf("male m2 was mother of %d children; should be impossible", maleMothers)
	}
}

func TestPopulationEngine_MigrationWhenOvercrowded(t *testing.T) {
	w := newPopWorld(t, 99)
	loc := w.AddLocation(core.NewLocation("village", "Test Village", "Marches", 2))
	w.AddLocation(core.NewLocation("town", "Test Town", "Marches", 100))

	// 5 people at village (cap 2 → over by 3).
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("p%02d", i)
		w.AddPerson(&core.Person{
			ID:         id,
			Name:       id,
			Gender:     "M",
			BirthTick:  -20 * 365,
			Alive:      true,
			LocationID: "village",
		})
	}
	w.RecomputeLocationPopulations()

	eng := &PopulationEngine{AnnualDeathChance: 0.0}
	sim := tick.NewSimulation(99, eng)
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// At least 1 person should have migrated to "town".
	movedToTown := 0
	for _, p := range w.People {
		if p.Alive && p.LocationID == "town" {
			movedToTown++
		}
	}
	if movedToTown == 0 {
		t.Errorf("expected at least 1 migration to town, got 0; pressure=%d", loc.Pressure)
	}
	if loc.Pressure == 0 {
		t.Errorf("expected non-zero pressure on over-cap location, got 0")
	}
}

func TestPopulationEngine_MigrationNoOpWhenUnderCap(t *testing.T) {
	w := newPopWorld(t, 77)
	w.AddLocation(core.NewLocation("village", "Test Village", "Marches", 100))
	w.AddLocation(core.NewLocation("town", "Test Town", "Marches", 100))
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("p%02d", i)
		w.AddPerson(&core.Person{
			ID: id, Name: id, Gender: "M", BirthTick: -20 * 365,
			Alive: true, LocationID: "village",
		})
	}
	w.RecomputeLocationPopulations()
	eng := NewPopulationEngine()
	sim := tick.NewSimulation(77, eng)
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	for _, p := range w.People {
		if p.LocationID == "town" {
			t.Errorf("person %s unexpectedly migrated when village was under cap", p.ID)
		}
	}
}

func TestPopulationEngine_AgingOutMarksAdult(t *testing.T) {
	w := newPopWorld(t, 55)
	w.AddPerson(&core.Person{ID: "kid", Name: "Kid", Gender: "F", BirthTick: -15 * 365, Alive: true})
	w.AddPerson(&core.Person{ID: "grown", Name: "Grown", Gender: "F", BirthTick: -20 * 365, Alive: true})
	eng := NewPopulationEngine()
	sim := tick.NewSimulation(55, eng)
	for i := int64(0); i < 2*365; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	if !w.People["kid"].IsAdult(w.Tick) {
		t.Errorf("kid should be adult at age %d, IsAdult=false", w.People["kid"].AgeAt(w.Tick))
	}
	if !w.People["grown"].IsAdult(w.Tick) {
		t.Errorf("grown should be adult, IsAdult=false")
	}
}

func TestPopulationEngine_DeathSetsDeathTick(t *testing.T) {
	w := newPopWorld(t, 1234)
	// 100% death chance per tick.
	eng := &PopulationEngine{AnnualDeathChance: 365.0}
	sim := tick.NewSimulation(1234, eng)
	w.AddPerson(&core.Person{ID: "doomed", Name: "Doomed", Gender: "M", BirthTick: -20 * 365, Alive: true})
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if w.People["doomed"].Alive {
		t.Errorf("person should be dead with 100%% death chance")
	}
	if w.People["doomed"].DeathTick != w.Tick {
		t.Errorf("DeathTick = %d, want %d", w.People["doomed"].DeathTick, w.Tick)
	}
}

// TestPopulationEngine_RespectsWorldpackRules verifies that when
// w.Rules is set (e.g. by worldpack.Bootstrap), the engine uses
// the pack's AnnualDeathChance instead of the engine's own field.
func TestPopulationEngine_RespectsWorldpackRules(t *testing.T) {
	// Build a world with a high annual death chance (50% per year).
	// Run 1 year; expect roughly half the population to die.
	w := newPopWorld(t, 101)
	w.Rules = &core.WorldRules{AnnualDeathChance: 0.50, FertileMinAge: 16, FertileMaxAge: 50, MinBirthIntervalTicks: 365, MaxChildren: 6}
	const n = 1000
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("p%04d", i)
		w.AddPerson(&core.Person{ID: id, Name: id, Gender: "M", BirthTick: -20 * 365, Alive: true})
	}
	// Engine field is high (100%) but pack rules should win (50%).
	eng := &PopulationEngine{AnnualDeathChance: 1.0}
	sim := tick.NewSimulation(101, eng)
	for i := int64(0); i < 365; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	alive := 0
	for _, p := range w.People {
		if p.Alive {
			alive++
		}
	}
	// With 50% annual death, expected survivors ~ 1000 * 0.5 = 500.
	// Allow a wide margin to avoid flakiness.
	if alive < 350 || alive > 650 {
		t.Errorf("with pack AnnualDeathChance=0.50, expected ~500 alive, got %d (pack rules not honored?)", alive)
	}
	// Sanity: if engine field had been used (1.0 = 365/365 per year),
	// everyone would be dead. If pack rules had been ignored, alive
	// would be 0. So alive > 100 is a positive signal.
	if alive < 100 {
		t.Errorf("alive count too low: %d — engine may be ignoring pack rules", alive)
	}
}

// TestPopulationEngine_FallsBackWhenNoRules verifies the backwards-
// compat path: if w.Rules is nil, the engine uses its own
// AnnualDeathChance field, exactly as it did before Phase 5.
func TestPopulationEngine_FallsBackWhenNoRules(t *testing.T) {
	w := newPopWorld(t, 202)
	if w.Rules != nil {
		t.Fatal("test prerequisite: w.Rules should be nil")
	}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("p%03d", i)
		w.AddPerson(&core.Person{ID: id, Name: id, Gender: "F", BirthTick: -20 * 365, Alive: true})
	}
	// 0% death via the engine field; pack is absent.
	eng := &PopulationEngine{AnnualDeathChance: 0.0}
	sim := tick.NewSimulation(202, eng)
	for i := int64(0); i < 365; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	for _, p := range w.People {
		if !p.Alive {
			t.Errorf("person %s died with AnnualDeathChance=0; fallback broken", p.ID)
		}
	}
}

// TestPopulationEngine_FertileAgeFromRules verifies that the engine
// respects the pack's fertile age range, not the hardcoded 16-50
// default. A 17-year-old is fertile under the default (16) but not
// under a pack with FertileMinAge=18. Run a single tick so the
// mother doesn't age into fertility.
func TestPopulationEngine_FertileAgeFromRules(t *testing.T) {
	w := newPopWorld(t, 303)
	w.Rules = &core.WorldRules{
		AnnualDeathChance:     0,
		FertileMinAge:         18,
		FertileMaxAge:         50,
		MinBirthIntervalTicks: 1, // any gap is enough
		MaxChildren:           6,
	}
	if w.Rules.FertileMinAge != 18 {
		t.Fatalf("test prerequisite: FertileMinAge should be 18, got %d", w.Rules.FertileMinAge)
	}
	w.AddPerson(&core.Person{ID: "m17", Name: "Seventeen", Gender: "F", BirthTick: -17 * 365, Alive: true, SpouseID: "f17"})
	w.AddPerson(&core.Person{ID: "f17", Name: "Husband", Gender: "M", BirthTick: -19 * 365, Alive: true, SpouseID: "m17"})
	eng := NewPopulationEngine()
	sim := tick.NewSimulation(303, eng)
	startCount := len(w.People)
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	endCount := len(w.People)
	if endCount > startCount {
		t.Errorf("17-year-old mother had %d children; with FertileMinAge=18, expected 0", endCount-startCount)
	}
	// Sanity: the mother is still 17 after one tick.
	if got := w.People["m17"].AgeAt(w.Tick); got != 17 {
		t.Errorf("mother's age after 1 tick = %d, want 17", got)
	}
}

// TestPopulationEngine_MigrationFractionFromRules verifies that the
// engine uses the pack's MigrationFraction instead of the hardcoded
// 0.5 default. With MigrationFraction=1.0, all excess people should
// migrate in a single tick.
func TestPopulationEngine_MigrationFractionFromRules(t *testing.T) {
	// Set up: village cap 2, town cap 100. 5 people at village.
	// Excess = 3.
	w := newPopWorld(t, 404)
	w.Rules = &core.WorldRules{
		AnnualDeathChance:  0,
		MigrationFraction:  1.0, // 100% of excess
		MinMigrantsPerTick: 1,
	}
	w.AddLocation(core.NewLocation("village", "V", "Marches", 2))
	w.AddLocation(core.NewLocation("town", "T", "Marches", 100))
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("p%02d", i)
		w.AddPerson(&core.Person{
			ID: id, Name: id, Gender: "M", BirthTick: -20 * 365,
			Alive: true, LocationID: "village",
		})
	}
	w.RecomputeLocationPopulations()

	eng := NewPopulationEngine()
	sim := tick.NewSimulation(404, eng)
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// With MigrationFraction=1.0, all 3 excess should move.
	movedToTown := 0
	stillInVillage := 0
	for _, p := range w.People {
		if p.Alive {
			switch p.LocationID {
			case "town":
				movedToTown++
			case "village":
				stillInVillage++
			}
		}
	}
	if movedToTown != 3 {
		t.Errorf("with MigrationFraction=1.0, expected 3 to move to town, got %d (still in village: %d)", movedToTown, stillInVillage)
	}
	if stillInVillage != 2 {
		t.Errorf("village should hold cap (2), got %d", stillInVillage)
	}
}

// TestPopulationEngine_MinMigrantsFromRules verifies that the
// MinMigrantsPerTick field is honored. With a fraction that
// produces 0, minMigrants should force at least that many migrations.
func TestPopulationEngine_MinMigrantsFromRules(t *testing.T) {
	w := newPopWorld(t, 505)
	w.Rules = &core.WorldRules{
		AnnualDeathChance:  0,
		MigrationFraction:  0.01, // very small fraction
		MinMigrantsPerTick: 2,    // but force 2 to move
	}
	w.AddLocation(core.NewLocation("village", "V", "Marches", 1)) // cap 1
	w.AddLocation(core.NewLocation("town", "T", "Marches", 100))
	for i := 0; i < 3; i++ { // 3 people at cap-1 village: excess = 2
		id := fmt.Sprintf("p%02d", i)
		w.AddPerson(&core.Person{
			ID: id, Name: id, Gender: "M", BirthTick: -20 * 365,
			Alive: true, LocationID: "village",
		})
	}
	w.RecomputeLocationPopulations()

	eng := NewPopulationEngine()
	sim := tick.NewSimulation(505, eng)
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	movedToTown := 0
	for _, p := range w.People {
		if p.Alive && p.LocationID == "town" {
			movedToTown++
		}
	}
	if movedToTown != 2 {
		t.Errorf("with MinMigrantsPerTick=2, expected 2 to move, got %d", movedToTown)
	}
}
