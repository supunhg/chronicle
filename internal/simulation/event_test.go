package simulation

import (
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// makeEventWorld builds a minimal world with the standard
// faction set (town_council, faith_of_dawn) and 2
// locations. The worldpack item catalog is pre-populated
// for the FamineRule tests.
func makeEventWorld(seed int64) *core.World {
	w := core.NewWorld("test", seed, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "village", Name: "Village", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "town", Name: "Town", PopulationCap: 100})
	w.Items = map[string]core.Item{
		"food":  {Name: "food", Value: 5},
		"wood":  {Name: "wood", Value: 3},
		"iron":  {Name: "iron", Value: 10},
		"cloth": {Name: "cloth", Value: 8},
	}
	// Factions for the Political/Religious rules.
	w.Factions["town_council"] = &core.Faction{
		ID:                "town_council",
		Name:              "Town Council",
		MemberOccupations: []string{"mayor", "clerk"},
	}
	w.Factions["faith_of_dawn"] = &core.Faction{
		ID:                "faith_of_dawn",
		Name:              "Faith of Dawn",
		MemberOccupations: []string{"priest", "teacher"},
	}
	return w
}

// addOccupiedPerson adds a living adult with the given
// occupation and location to the world. Used by the rule
// tests to seed faction members.
func addOccupiedPerson(w *core.World, id, occupation, locationID string) *core.Person {
	p := &core.Person{
		ID:         id,
		Name:       id,
		Gender:     "F",
		BirthTick:  -20 * 365,
		Alive:      true,
		LocationID: locationID,
		Occupation: occupation,
		Needs:      map[string]int{string(core.NeedHunger): 50, string(core.NeedWealth): 50},
		Goals:      []core.Goal{},
	}
	w.AddPerson(p)
	return p
}

// countEventsByType returns the number of events in w.Events
// matching the given type.
func countEventsByType(w *core.World, t core.EventType) int {
	n := 0
	for _, e := range w.Events {
		if e.Type == t {
			n++
		}
	}
	return n
}

// TestEventEngine_FamineRuleEmitsOnShortage verifies that
// FamineRule emits one FamineRisk event per location in
// shortage. Two locations, one in shortage -> one event.
func TestEventEngine_FamineRuleEmitsOnShortage(t *testing.T) {
	w := makeEventWorld(1)
	w.Locations["village"].Settlement.Food = 5 // below threshold
	w.Locations["town"].Settlement.Food = 50   // above
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventFamineRisk); got != 1 {
		t.Errorf("FamineRisk count = %d, want 1 (one location in shortage)", got)
	}
	// Verify the event has the right Location and Payload.
	var ev *core.Event
	for i := range w.Events {
		if w.Events[i].Type == core.EventFamineRisk {
			ev = &w.Events[i]
			break
		}
	}
	if ev == nil {
		t.Fatal("no FamineRisk event found")
	}
	if ev.Location != "village" {
		t.Errorf("FamineRisk Location = %q, want %q", ev.Location, "village")
	}
	if got := ev.Payload["current_food"]; got != 5.0 {
		t.Errorf("FamineRisk Payload[current_food] = %v, want 5.0", got)
	}
}

// TestEventEngine_FamineRuleNoEmitAboveThreshold verifies
// that no FamineRisk is emitted when every location is
// well-stocked.
func TestEventEngine_FamineRuleNoEmitAboveThreshold(t *testing.T) {
	w := makeEventWorld(1)
	w.Locations["village"].Settlement.Food = 50
	w.Locations["town"].Settlement.Food = 50
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventFamineRisk); got != 0 {
		t.Errorf("FamineRisk count = %d, want 0 (no shortage)", got)
	}
}

// TestEventEngine_CrimeRuleEmitsOnHighHungerAndLowWealth
// verifies that TheftWave fires when world-wide average
// hunger need > 70 AND average wealth need > 70. V1
// measures needs as "deficit" (higher = more urgent).
func TestEventEngine_CrimeRuleEmitsOnHighHungerAndLowWealth(t *testing.T) {
	w := makeEventWorld(1)
	// 3 people with high hunger and high wealth desire.
	for i := 0; i < 3; i++ {
		p := addOccupiedPerson(w, "n"+string(rune('1'+i)), "farmer", "village")
		p.Needs[string(core.NeedHunger)] = 90
		p.Needs[string(core.NeedWealth)] = 90
	}
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventTheftWave); got != 1 {
		t.Errorf("TheftWave count = %d, want 1 (high hunger + high wealth desire)", got)
	}
}

// TestEventEngine_CrimeRuleNoEmitOnHappy verifies that
// TheftWave does NOT fire when the world is well-fed and
// prosperous (both needs at 50, the default).
func TestEventEngine_CrimeRuleNoEmitOnHappy(t *testing.T) {
	w := makeEventWorld(1)
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "n"+string(rune('1'+i)), "farmer", "village")
	}
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventTheftWave); got != 0 {
		t.Errorf("TheftWave count = %d, want 0 (default needs are not extreme)", got)
	}
}

// TestEventEngine_PoliticalRuleEmitsWithCouncilMembers
// verifies that CouncilScandal fires when town_council has
// at least 5 members (mayors + clerks).
func TestEventEngine_PoliticalRuleEmitsWithCouncilMembers(t *testing.T) {
	w := makeEventWorld(1)
	// 3 mayors + 3 clerks = 6 members, above threshold.
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "mayor"+string(rune('0'+i)), "mayor", "village")
	}
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "clerk"+string(rune('0'+i)), "clerk", "village")
	}
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventCouncilScandal); got != 1 {
		t.Errorf("CouncilScandal count = %d, want 1 (6 council members)", got)
	}
}

// TestEventEngine_PoliticalRuleNoEmitFewMembers verifies
// that CouncilScandal does NOT fire when the council has
// fewer than 5 members.
func TestEventEngine_PoliticalRuleNoEmitFewMembers(t *testing.T) {
	w := makeEventWorld(1)
	// 2 mayors, no clerks = 2 members, below threshold.
	for i := 0; i < 2; i++ {
		addOccupiedPerson(w, "mayor"+string(rune('0'+i)), "mayor", "village")
	}
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventCouncilScandal); got != 0 {
		t.Errorf("CouncilScandal count = %d, want 0 (only 2 members)", got)
	}
}

// TestEventEngine_ReligiousRuleEmitsWithFaithMembers
// verifies that RevivalMovement fires when faith_of_dawn
// has at least 5 members (priests + teachers).
func TestEventEngine_ReligiousRuleEmitsWithFaithMembers(t *testing.T) {
	w := makeEventWorld(1)
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "priest"+string(rune('0'+i)), "priest", "town")
	}
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "teacher"+string(rune('0'+i)), "teacher", "town")
	}
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventRevivalMovement); got != 1 {
		t.Errorf("RevivalMovement count = %d, want 1 (6 faith members)", got)
	}
}

// TestEventEngine_ReligiousRuleNoEmitFewMembers verifies
// that RevivalMovement does NOT fire when the faith faction
// has fewer than 5 members.
func TestEventEngine_ReligiousRuleNoEmitFewMembers(t *testing.T) {
	w := makeEventWorld(1)
	for i := 0; i < 2; i++ {
		addOccupiedPerson(w, "priest"+string(rune('0'+i)), "priest", "town")
	}
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventRevivalMovement); got != 0 {
		t.Errorf("RevivalMovement count = %d, want 0 (only 2 faith members)", got)
	}
}

// TestEventEngine_CooldownDeduplicates verifies that the
// per-(rule, location) cooldown prevents the same event
// from being emitted on consecutive ticks (even if the
// condition persists).
func TestEventEngine_CooldownDeduplicates(t *testing.T) {
	w := makeEventWorld(1)
	w.Locations["village"].Settlement.Food = 5  // persistent shortage
	w.Locations["town"].Settlement.Food = 100    // not in shortage
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Tick 1: emit (cooldown never fired before).
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	count1 := countEventsByType(w, core.EventFamineRisk)
	// Tick 2: still in shortage but cooldown blocks.
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	count2 := countEventsByType(w, core.EventFamineRisk)
	if count1 != 1 || count2 != 1 {
		t.Errorf("FamineRisk count: tick1=%d, tick2=%d; want 1, 1 (cooldown dedup)", count1, count2)
	}
}

// TestEventEngine_CooldownReleases verifies that the
// cooldown eventually releases, so a persistent condition
// re-emits after EventCooldownTicks (30).
func TestEventEngine_CooldownReleases(t *testing.T) {
	w := makeEventWorld(1)
	w.Locations["village"].Settlement.Food = 5
	w.Locations["town"].Settlement.Food = 100
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Tick 1: first emission.
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	// Fast-forward past the cooldown.
	w.Tick += EventCooldownTicks
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick after cooldown: %v", err)
	}
	if got := countEventsByType(w, core.EventFamineRisk); got != 2 {
		t.Errorf("FamineRisk count after cooldown = %d, want 2 (re-emit after %d ticks)", got, EventCooldownTicks)
	}
}

// TestEventEngine_NoMutation verifies that the engine does
// NOT mutate world state in response to events firing. We
// snapshot the world before Tick and compare key fields
// after.
func TestEventEngine_NoMutation(t *testing.T) {
	w := makeEventWorld(1)
	w.Locations["village"].Settlement.Food = 5
	addOccupiedPerson(w, "n1", "mayor", "village")
	addOccupiedPerson(w, "n2", "priest", "village")
	// Snapshot.
	popBefore := w.Locations["village"].Population
	stockBefore := w.Locations["village"].Settlement
	priceBefore := w.Locations["village"].Prices
	coinsBefore := w.Coin
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// The event engine does not own Population/Settlement/Prices/Coin.
	// Population is 0 because the economy/recompute engine hasn't run;
	// the event engine must not have changed it from 0.
	if w.Locations["village"].Population != popBefore {
		t.Errorf("Population mutated by EventEngine: before=%d after=%d", popBefore, w.Locations["village"].Population)
	}
	// Stock is unchanged by the event engine.
	if w.Locations["village"].Settlement != stockBefore {
		t.Errorf("Settlement mutated by EventEngine: before=%+v after=%+v", stockBefore, w.Locations["village"].Settlement)
	}
	// Prices unchanged.
	if w.Locations["village"].Prices != priceBefore {
		t.Errorf("Prices mutated by EventEngine: before=%+v after=%+v", priceBefore, w.Locations["village"].Prices)
	}
	// Coin unchanged.
	if w.Coin != coinsBefore {
		t.Errorf("Coin mutated by EventEngine: before=%d after=%d", coinsBefore, w.Coin)
	}
}

// TestEventEngine_Deterministic verifies that two worlds
// with the same seed, same state, and same number of ticks
// produce identical event logs.
func TestEventEngine_Deterministic(t *testing.T) {
	mk := func() *core.World {
		w := makeEventWorld(42)
		w.Locations["village"].Settlement.Food = 5
		addOccupiedPerson(w, "n1", "mayor", "village")
		return w
	}
	w1, w2 := mk(), mk()
	eng1, eng2 := NewEventEngine(), NewEventEngine()
	sim1 := NewEventTickSim(w1.Seed, eng1)
	sim2 := NewEventTickSim(w2.Seed, eng2)
	for _, w := range []*core.World{w1, w2} {
		if err := sim1.Init(w); err != nil {
			t.Fatalf("Init: %v", err)
		}
	}
	// Run 3 ticks.
	for i := 0; i < 3; i++ {
		if err := sim1.Tick(w1); err != nil {
			t.Fatalf("Tick1: %v", err)
		}
		if err := sim2.Tick(w2); err != nil {
			t.Fatalf("Tick2: %v", err)
		}
	}
	if len(w1.Events) != len(w2.Events) {
		t.Fatalf("event log length differs: w1=%d w2=%d", len(w1.Events), len(w2.Events))
	}
	for i := range w1.Events {
		if w1.Events[i].Type != w2.Events[i].Type {
			t.Errorf("event[%d] Type differs: %q vs %q", i, w1.Events[i].Type, w2.Events[i].Type)
		}
		if w1.Events[i].Location != w2.Events[i].Location {
			t.Errorf("event[%d] Location differs: %q vs %q", i, w1.Events[i].Location, w2.Events[i].Location)
		}
		if w1.Events[i].Tick != w2.Events[i].Tick {
			t.Errorf("event[%d] Tick differs: %d vs %d", i, w1.Events[i].Tick, w2.Events[i].Tick)
		}
	}
}

// TestEventEngine_AllFourRulesCoexist verifies that all
// four rules can fire in the same tick when their
// conditions are simultaneously met (no cross-rule
// interference).
func TestEventEngine_AllFourRulesCoexist(t *testing.T) {
	w := makeEventWorld(1)
	// Famine: village food shortage; town well-stocked so
	// only one FamineRisk fires.
	w.Locations["village"].Settlement.Food = 5
	w.Locations["town"].Settlement.Food = 100
	// Political: 3 mayors + 3 clerks at village.
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "mayor"+string(rune('0'+i)), "mayor", "village")
	}
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "clerk"+string(rune('0'+i)), "clerk", "village")
	}
	// Religious: 3 priests + 3 teachers at town.
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "priest"+string(rune('0'+i)), "priest", "town")
	}
	for i := 0; i < 3; i++ {
		addOccupiedPerson(w, "teacher"+string(rune('0'+i)), "teacher", "town")
	}
	// Crime: set EVERYONE to high hunger + high wealth
	// desire so the world-wide average clears the 70
	// threshold. (Political/Religious rules don't care about
	// needs; only Crime does.)
	for _, p := range w.LivingPeople() {
		p.Needs[string(core.NeedHunger)] = 90
		p.Needs[string(core.NeedWealth)] = 90
	}
	eng := NewEventEngine()
	sim := NewEventTickSim(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := countEventsByType(w, core.EventFamineRisk); got != 1 {
		t.Errorf("FamineRisk count = %d, want 1", got)
	}
	if got := countEventsByType(w, core.EventTheftWave); got != 1 {
		t.Errorf("TheftWave count = %d, want 1", got)
	}
	if got := countEventsByType(w, core.EventCouncilScandal); got != 1 {
		t.Errorf("CouncilScandal count = %d, want 1", got)
	}
	if got := countEventsByType(w, core.EventRevivalMovement); got != 1 {
		t.Errorf("RevivalMovement count = %d, want 1", got)
	}
}

// NewEventTickSim is a tiny shim that wraps a tick.Engine
// so the test can call Init/Tick without depending on the
// full tick.Simulation orchestration. It increments w.Tick
// on each Tick call (matching the real tick.Simulation
// behavior).
type eventTickSim struct {
	seed int64
	eng  interface {
		Init(*core.World) error
		Tick(*core.World) error
	}
}

func NewEventTickSim(seed int64, eng interface {
	Init(*core.World) error
	Tick(*core.World) error
}) *eventTickSim {
	return &eventTickSim{seed: seed, eng: eng}
}

func (s *eventTickSim) Init(w *core.World) error {
	w.Seed = s.seed
	return s.eng.Init(w)
}

func (s *eventTickSim) Tick(w *core.World) error {
	w.Tick++
	return s.eng.Tick(w)
}
