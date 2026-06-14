package simulation

import (
	"math"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// makeEconomyWorld builds a minimal world with two locations
// and one adult farmer at the village. The worldpack item
// catalog is pre-populated with sensible base prices for the
// 4 v1 resources. The starting settlement is zero so each
// test can set its own initial stock.
func makeEconomyWorld(seed int64) *core.World {
	w := core.NewWorld("test", seed, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "village", Name: "Village", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "town", Name: "Town", PopulationCap: 100})
	// Item catalog with explicit base prices for the 4 v1
	// resources. DefaultItemSpec gives these zero values, so
	// we set them explicitly to make price-recalc arithmetic
	// easy to verify.
	w.Items = map[string]core.Item{
		"food":  {Name: "food", Value: 5},
		"wood":  {Name: "wood", Value: 3},
		"iron":  {Name: "iron", Value: 10},
		"cloth": {Name: "cloth", Value: 8},
	}
	return w
}

// addFarmer appends a living adult farmer at the given
// location ID. Used by the production test to seed one
// producer.
func addFarmer(w *core.World, id, name, locationID string) *core.Person {
	p := &core.Person{
		ID:         id,
		Name:       name,
		Gender:     "F",
		BirthTick:  -20 * 365, // 20 years old
		Alive:      true,
		LocationID: locationID,
		Occupation: "farmer",
		Needs:      map[string]int{"hunger": 50, "wealth": 50, "rest": 50, "companionship": 50, "safety": 50},
		Goals:      []core.Goal{},
	}
	w.AddPerson(p)
	return p
}

// TestEconomyEngine_ProductionIncreasesStock verifies that a
// single farmer running one Tick increases the village's
// Food stock by EconomyProductionPerTick (1.0).
func TestEconomyEngine_ProductionIncreasesStock(t *testing.T) {
	w := makeEconomyWorld(1)
	addFarmer(w, "n0001", "Alice", "village")
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before := w.Locations["village"].Settlement.Food
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	after := w.Locations["village"].Settlement.Food
	// One farmer produces 1.0 food and consumes 0.1 food
	// (everyone eats), so net change is +0.9.
	got := after - before
	if math.Abs(got-0.9) > 1e-9 {
		t.Errorf("food stock change = %f, want 0.9 (1.0 produced - 0.1 consumed)", got)
	}
}

// TestEconomyEngine_ConsumptionDecreasesFood verifies that
// consumption is applied to every living person (even
// non-producers) and that food stock drops accordingly.
func TestEconomyEngine_ConsumptionDecreasesFood(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 50
	// One non-producer (a clerk) at the village — clerk is
	// not in the producedBy table, so net food change
	// should be -0.1 (consumption only).
	w.AddPerson(&core.Person{
		ID: "n0001", Name: "Clerk", Gender: "F",
		BirthTick: -20 * 365, Alive: true,
		LocationID: "village", Occupation: "clerk",
		Needs: map[string]int{"hunger": 50},
	})
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before := w.Locations["village"].Settlement.Food
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	after := w.Locations["village"].Settlement.Food
	got := before - after
	if math.Abs(got-0.1) > 1e-9 {
		t.Errorf("food stock decrease = %f, want 0.1 (one person's consumption)", got)
	}
}

// TestEconomyEngine_ShortageClampsAtZero verifies that
// consumption never goes negative: if the location runs out
// of food mid-tick, the per-person consumption for the
// remaining people is reduced to the remaining stock, and
// the final stock is exactly 0.
func TestEconomyEngine_ShortageClampsAtZero(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 0.05 // less than one person's 0.1
	w.AddPerson(&core.Person{
		ID: "n0001", Name: "Alice", Gender: "F",
		BirthTick: -20 * 365, Alive: true,
		LocationID: "village", Occupation: "clerk",
		Needs: map[string]int{"hunger": 50},
	})
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := w.Locations["village"].Settlement.Food; got != 0 {
		t.Errorf("food stock after shortage tick = %f, want 0 (clamped)", got)
	}
}

// TestEconomyEngine_PriceRisesOnShortage verifies that when
// stock falls below the target (EconomyDefaultTargetStock =
// 100), the price formula produces a multiplier > 1x. With
// base=5 and stock=50, raw price = 5 * 100 / 50 = 10.
func TestEconomyEngine_PriceRisesOnShortage(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 50
	w.Locations["village"].Settlement.Wood = 200 // surplus (above target)
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := w.Locations["village"].Prices.Food; got != 10 {
		t.Errorf("food price with stock=50, base=5, target=100 = %d, want 10 (2x shortage)", got)
	}
	// Wood with base=3, stock=200, target=100: raw = 3 *
	// 100 / 200 = 1.5, rounded to 2 (no clamp because > 0.1
	// and < 10x).
	if got := w.Locations["village"].Prices.Wood; got != 2 {
		t.Errorf("wood price with stock=200, base=3, target=100 = %d, want 2 (0.5x rounded up)", got)
	}
}

// TestEconomyEngine_PriceClampsAt10x verifies the upper
// clamp: with stock=0.01, raw price = 5 * 100 / 0.01 =
// 50000, but the clamp caps it at 10x = 50.
func TestEconomyEngine_PriceClampsAt10x(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 0.01
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := w.Locations["village"].Prices.Food; got != 50 {
		t.Errorf("food price with stock=0.01 = %d, want 50 (10x clamp on base=5)", got)
	}
}

// TestEconomyEngine_PriceClampsAtPoint1x verifies the lower
// clamp: with stock=10000, raw price = 5 * 100 / 10000 =
// 0.05, but the clamp caps it at 0.1x of base = 0.5, which
// rounds to 1.
func TestEconomyEngine_PriceClampsAtPoint1x(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 10000
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	got := w.Locations["village"].Prices.Food
	if got != 1 {
		t.Errorf("food price with stock=10000 = %d, want 1 (0.1x clamp of base 5 rounds up)", got)
	}
}

// TestEconomyEngine_ChildrenDoNotProduce verifies that
// children (IsAdult=false) do not produce resources, even if
// they have a producer occupation.
func TestEconomyEngine_ChildrenDoNotProduce(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 10
	// A 10-year-old "farmer" — IsAdult returns false, so no
	// production. Net food change is -0.1 (consumption).
	w.AddPerson(&core.Person{
		ID: "n0001", Name: "Kid", Gender: "F",
		BirthTick: -10 * 365, // 10 years old
		Alive: true, LocationID: "village",
		Occupation: "farmer",
		Needs:      map[string]int{"hunger": 50},
	})
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before := w.Locations["village"].Settlement.Food
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	after := w.Locations["village"].Settlement.Food
	got := before - after
	if math.Abs(got-0.1) > 1e-9 {
		t.Errorf("food stock change with child farmer = %f, want 0.1 (consumption only, no production)", got)
	}
}

// TestEconomyEngine_DeterministicDailyLoop verifies that two
// worlds with the same seed, same starting stock, and same
// producer layout produce identical stock and prices after
// 10 ticks. Determinism is the core contract.
func TestEconomyEngine_DeterministicDailyLoop(t *testing.T) {
	mk := func() *core.World {
		w := makeEconomyWorld(42)
		w.Locations["village"].Settlement = core.SettlementInventory{Food: 50, Wood: 30, Iron: 10, Cloth: 10}
		w.Locations["town"].Settlement = core.SettlementInventory{Food: 50, Wood: 30, Iron: 10, Cloth: 10}
		addFarmer(w, "n0001", "F1", "village")
		addFarmer(w, "n0002", "F2", "town")
		return w
	}
	w1, w2 := mk(), mk()
	eng1, eng2 := NewEconomyEngine(), NewEconomyEngine()
	sim1, sim2 := tick.NewSimulation(w1.Seed, eng1), tick.NewSimulation(w2.Seed, eng2)
	if err := sim1.Init(w1); err != nil {
		t.Fatalf("Init1: %v", err)
	}
	if err := sim2.Init(w2); err != nil {
		t.Fatalf("Init2: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := sim1.Tick(w1); err != nil {
			t.Fatalf("Tick1: %v", err)
		}
		if err := sim2.Tick(w2); err != nil {
			t.Fatalf("Tick2: %v", err)
		}
	}
	v1, v2 := w1.Locations["village"], w2.Locations["village"]
	if v1.Settlement != v2.Settlement {
		t.Errorf("village settlement differs: %+v vs %+v", v1.Settlement, v2.Settlement)
	}
	if v1.Prices != v2.Prices {
		t.Errorf("village prices differ: %+v vs %+v", v1.Prices, v2.Prices)
	}
	t1, t2 := w1.Locations["town"], w2.Locations["town"]
	if t1.Settlement != t2.Settlement {
		t.Errorf("town settlement differs: %+v vs %+v", t1.Settlement, t2.Settlement)
	}
	if t1.Prices != t2.Prices {
		t.Errorf("town prices differ: %+v vs %+v", t1.Prices, t2.Prices)
	}
}

// TestEconomyEngine_MultipleProducersAccumulate verifies
// that 3 farmers at the same location deposit 3 units of
// food per tick (minus 3 * 0.1 consumption = net +2.7).
func TestEconomyEngine_MultipleProducersAccumulate(t *testing.T) {
	w := makeEconomyWorld(1)
	addFarmer(w, "n0001", "F1", "village")
	addFarmer(w, "n0002", "F2", "village")
	addFarmer(w, "n0003", "F3", "village")
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before := w.Locations["village"].Settlement.Food
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	after := w.Locations["village"].Settlement.Food
	// 3 producers +1.0 each = +3.0, 3 consumers -0.1 each =
	// -0.3, net = +2.7.
	got := after - before
	if math.Abs(got-2.7) > 1e-9 {
		t.Errorf("food stock change with 3 farmers = %f, want 2.7 (3 produced - 0.3 consumed)", got)
	}
}

// TestEconomyEngine_ShortageBumpsHunger verifies that when
// Settlement.Food falls below EconomyShortageThreshold (20),
// every resident's NeedHunger is bumped by
// EconomyShortagePenalty (5). The clamp at 100 protects
// against unbounded growth.
func TestEconomyEngine_ShortageBumpsHunger(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 10 // below threshold
	p := &core.Person{
		ID: "n0001", Name: "Alice", Gender: "F",
		BirthTick: -20 * 365, Alive: true,
		LocationID: "village", Occupation: "clerk",
		Needs:      map[string]int{string(core.NeedHunger): 30},
	}
	w.AddPerson(p)
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	got := p.Needs[string(core.NeedHunger)]
	if got != 35 {
		t.Errorf("hunger after 1 shortage tick = %d, want 35 (30 + 5)", got)
	}
}

// TestEconomyEngine_ShortageHungerClampsAt100 verifies
// that the shortage hunger bump is clamped at 100.
func TestEconomyEngine_ShortageHungerClampsAt100(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 5
	p := &core.Person{
		ID: "n0001", Name: "Alice", Gender: "F",
		BirthTick: -20 * 365, Alive: true,
		LocationID: "village", Occupation: "clerk",
		Needs:      map[string]int{string(core.NeedHunger): 98},
	}
	w.AddPerson(p)
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	got := p.Needs[string(core.NeedHunger)]
	if got != 100 {
		t.Errorf("hunger after shortage tick starting at 98 = %d, want 100 (clamped)", got)
	}
}

// TestEconomyEngine_FamineMemoryEmittedOnce verifies that a
// famine_risk memory is emitted on the FIRST tick of a
// shortage, not on subsequent ticks of the same shortage.
// Transition detection uses Location.LastShortageTick.
func TestEconomyEngine_FamineMemoryEmittedOnce(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 5
	w.AddPerson(&core.Person{
		ID: "n0001", Name: "Alice", Gender: "F",
		BirthTick: -20 * 365, Alive: true,
		LocationID: "village", Occupation: "clerk",
		Needs:      map[string]int{string(core.NeedHunger): 50},
	})
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Tick 1: first shortage tick — should emit 1 memory.
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	count1 := 0
	for _, m := range w.Memories {
		if containsTag(m.Tags, "famine_risk") {
			count1++
		}
	}
	if count1 != 1 {
		t.Errorf("tick 1 famine_risk memory count = %d, want 1 (transition into shortage)", count1)
	}
	// Tick 2: still in shortage — no new memory.
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	count2 := 0
	for _, m := range w.Memories {
		if containsTag(m.Tags, "famine_risk") {
			count2++
		}
	}
	if count2 != 1 {
		t.Errorf("tick 2 famine_risk memory count = %d, want 1 (no new memory during same shortage)", count2)
	}
	// Recover food; tick 3: no new memory.
	w.Locations["village"].Settlement.Food = 100
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick 3: %v", err)
	}
	count3 := 0
	for _, m := range w.Memories {
		if containsTag(m.Tags, "famine_risk") {
			count3++
		}
	}
	if count3 != 1 {
		t.Errorf("tick 3 famine_risk memory count = %d, want 1 (recovered, no new memory)", count3)
	}
	// Drop food again; tick 4: NEW memory (recovery -> re-shortage transition).
	w.Locations["village"].Settlement.Food = 5
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick 4: %v", err)
	}
	count4 := 0
	for _, m := range w.Memories {
		if containsTag(m.Tags, "famine_risk") {
			count4++
		}
	}
	if count4 != 2 {
		t.Errorf("tick 4 famine_risk memory count = %d, want 2 (re-shortage transition emits a new memory)", count4)
	}
}

// TestEconomyEngine_NoShortageNoMemory verifies that a
// well-stocked location never emits famine_risk memories.
func TestEconomyEngine_NoShortageNoMemory(t *testing.T) {
	w := makeEconomyWorld(1)
	w.Locations["village"].Settlement.Food = 50 // above threshold
	w.AddPerson(&core.Person{
		ID: "n0001", Name: "Alice", Gender: "F",
		BirthTick: -20 * 365, Alive: true,
		LocationID: "village", Occupation: "clerk",
		Needs:      map[string]int{string(core.NeedHunger): 50},
	})
	eng := NewEconomyEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick %d: %v", i, err)
		}
	}
	for _, m := range w.Memories {
		if containsTag(m.Tags, "famine_risk") {
			t.Errorf("unexpected famine_risk memory at tick %d: %+v", m.Tick, m)
		}
	}
}

// TestProducedByMapping verifies the occupation -> resource
// mapping for the v1 producer set. This is the canonical
// reference for what each occupation produces. Phase 22.5
// removed baker/hunter/carpenter from the producer set to
// match the spec's exact 4 producers.
func TestProducedByMapping(t *testing.T) {
	cases := []struct {
		occ  string
		want core.ResourceID
	}{
		{"farmer", core.ResourceFood},
		{"woodcutter", core.ResourceWood},
		{"miner", core.ResourceIron},
		{"weaver", core.ResourceCloth},
		// Phase 22.5: dropped from producers (per spec).
		{"baker", ""},
		{"hunter", ""},
		{"carpenter", ""},
		// Other non-producers.
		{"merchant", ""},
		{"blacksmith", ""},
		{"guard", ""},
		{"priest", ""},
		{"mayor", ""},
	}
	for _, c := range cases {
		if got := producedBy(c.occ); got != c.want {
			t.Errorf("producedBy(%q) = %q, want %q", c.occ, got, c.want)
		}
	}
}
