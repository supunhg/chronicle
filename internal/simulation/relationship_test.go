package simulation

import (
	"reflect"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// TestRelationshipEngine_Init is the no-op invariant for Init.
// Phase 14 doesn't use Init for caching, but the contract remains.
func TestRelationshipEngine_Init(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	eng := NewRelationshipEngine()
	if err := eng.Init(w); err != nil {
		t.Errorf("Init: %v", err)
	}
}

// TestRelationshipEngine_NoCoLocationPreservesRelationships verifies
// that when no two NPCs share a location, no new relationships are
// created and existing relationships are unchanged. This is the
// "no-op" baseline for the Phase 14 engine (the old TestRelationshipEngine_NoOpTick).
func TestRelationshipEngine_NoCoLocationPreservesRelationships(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "town"})
	w.Relationships = []core.Relationship{
		{FromID: "p1", ToID: "p2", Trust: 50, Respect: 60},
	}
	eng := NewRelationshipEngine()
	// Zero decay so the existing relationship is exactly preserved.
	eng.DecayRate = 0
	sim := tick.NewSimulation(1, eng)
	for i := int64(0); i < 10; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	if len(w.Relationships) != 1 {
		t.Errorf("len(Relationships) = %d, want 1 (no co-location, no new pairs)", len(w.Relationships))
	}
	if w.Relationships[0].Trust != 50 || w.Relationships[0].Respect != 60 {
		t.Errorf("existing relationship mutated: got %+v, want Trust=50 Respect=60", w.Relationships[0])
	}
}

// TestRelationshipEngine_CreatesRelationshipsOnCoLocation verifies
// that when two NPCs share a location, a new directed A→B
// relationship is created with the engine's NewAcquaintanceTrust
// on the Trust axis and 0 on the other 4 axes.
func TestRelationshipEngine_CreatesRelationshipsOnCoLocation(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "village"})

	eng := NewRelationshipEngine()
	eng.DecayRate = 0 // no decay for this test
	if err := eng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(w.Relationships) != 1 {
		t.Fatalf("len(Relationships) = %d, want 1 (p1→p2)", len(w.Relationships))
	}
	r := w.Relationships[0]
	if r.FromID != "p1" || r.ToID != "p2" {
		t.Errorf("relationship = %+v, want {p1 -> p2}", r)
	}
	if r.Trust != eng.NewAcquaintanceTrust {
		t.Errorf("Trust = %f, want %f", r.Trust, eng.NewAcquaintanceTrust)
	}
	if r.Respect != 0 || r.Fear != 0 || r.Attraction != 0 || r.Loyalty != 0 {
		t.Errorf("new relationship has non-zero axes: %+v", r)
	}
}

// TestRelationshipEngine_NoDuplicateRelationships verifies that
// ticking multiple times does not create duplicate (from, to)
// pairs. The existing-key check in formRelationships guards
// against this.
func TestRelationshipEngine_NoDuplicateRelationships(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "village"})
	w.AddPerson(&core.Person{ID: "p3", Name: "Carol", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village"})

	eng := NewRelationshipEngine()
	eng.DecayRate = 0
	sim := tick.NewSimulation(1, eng)
	for i := 0; i < 50; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	// 3 people at the same location: 3 choose 2 = 3 directed pairs
	// (p1→p2, p1→p3, p2→p3) — all in sorted ID order, A.ID < B.ID.
	// No reverse-direction pairs.
	want := 3
	if len(w.Relationships) != want {
		t.Errorf("len(Relationships) = %d, want %d (no duplicates after 50 ticks)", len(w.Relationships), want)
	}
	// Verify no duplicate (from, to) keys.
	seen := make(map[string]bool, len(w.Relationships))
	for _, r := range w.Relationships {
		key := r.Key()
		if seen[key] {
			t.Errorf("duplicate relationship key %q", key)
		}
		seen[key] = true
	}
}

// TestRelationshipEngine_DecayTowardNeutral verifies that all 5
// relationship axes decay toward 50 (neutral) by DecayRate per
// tick. An axis above 50 decreases; an axis below 50 increases;
// an axis at 50 is unchanged.
func TestRelationshipEngine_DecayTowardNeutral(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village_a"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "village_b"})
	w.Relationships = []core.Relationship{
		{FromID: "p1", ToID: "p2", Trust: 80, Respect: 20, Fear: 50, Attraction: 90, Loyalty: 10},
	}
	eng := NewRelationshipEngine()
	eng.DecayRate = 1.0 // 1.0 per tick so the math is easy to verify

	// Run 10 ticks: Trust should decay from 80 toward 50 by 10.
	if err := eng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	r := w.Relationships[0]
	if r.Trust != 79 {
		t.Errorf("Trust after 1 tick: got %f, want 79 (80 - 1.0)", r.Trust)
	}
	if r.Respect != 21 {
		t.Errorf("Respect after 1 tick: got %f, want 21 (20 + 1.0)", r.Respect)
	}
	if r.Fear != 50 {
		t.Errorf("Fear at neutral: got %f, want 50 (unchanged)", r.Fear)
	}
	if r.Attraction != 89 {
		t.Errorf("Attraction after 1 tick: got %f, want 89 (90 - 1.0)", r.Attraction)
	}
	if r.Loyalty != 11 {
		t.Errorf("Loyalty after 1 tick: got %f, want 11 (10 + 1.0)", r.Loyalty)
	}
}

// TestRelationshipEngine_DecayStopsAtNeutral verifies that decay
// stops at 50 (neutral) and does not oscillate or overshoot. A
// value below 50 decays upward and clamps at 50; a value above 50
// decays downward and clamps at 50. An axis at 50 is unchanged.
func TestRelationshipEngine_DecayStopsAtNeutral(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village_a"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "village_b"})
	w.Relationships = []core.Relationship{
		// Trust at 5 (below neutral) decays upward and clamps at 50.
		// Respect at 95 (above neutral) decays downward and clamps at 50.
		{FromID: "p1", ToID: "p2", Trust: 5, Respect: 95},
	}
	eng := NewRelationshipEngine()
	eng.DecayRate = 10.0 // aggressive decay
	for i := 0; i < 10; i++ {
		if err := eng.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	r := w.Relationships[0]
	// After enough ticks, both axes should be at 50 (neutral).
	// With rate=10.0 and starting at 5, Trust reaches 50 in 5 ticks:
	// 5→15→25→35→45→50. Same for Respect starting at 95: 95→85→...→55→50.
	if r.Trust != 50 {
		t.Errorf("Trust after 10 ticks of 10.0-rate decay from 5: got %f, want 50 (clamped at neutral)", r.Trust)
	}
	if r.Respect != 50 {
		t.Errorf("Respect after 10 ticks of 10.0-rate decay from 95: got %f, want 50 (clamped at neutral)", r.Respect)
	}
	// And the axes must stay at 50 — no overshoot or oscillation.
	for i := 0; i < 100; i++ {
		if err := eng.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	if w.Relationships[0].Trust != 50 || w.Relationships[0].Respect != 50 {
		t.Errorf("axes drifted from neutral: got Trust=%f Respect=%f, want both 50",
			w.Relationships[0].Trust, w.Relationships[0].Respect)
	}
}

// TestRelationshipEngine_ZeroDecayIsNoop verifies that
// DecayRate=0 disables the decay step entirely (matches the
// existing-world case where the user wants relationships to be
// preserved exactly).
func TestRelationshipEngine_ZeroDecayIsNoop(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village_a"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "village_b"})
	w.Relationships = []core.Relationship{
		{FromID: "p1", ToID: "p2", Trust: 80, Respect: 20},
	}
	eng := NewRelationshipEngine()
	eng.DecayRate = 0
	for i := 0; i < 100; i++ {
		if err := eng.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	r := w.Relationships[0]
	if r.Trust != 80 || r.Respect != 20 {
		t.Errorf("zero-decay tick mutated relationship: got %+v, want Trust=80 Respect=20", r)
	}
}

// TestRelationshipEngine_ApplyMemoryDeltasCreatesRelationship
// verifies that calling ApplyMemoryDeltas for a non-existent
// (owner, target) pair creates a new relationship with the deltas
// baked in. The relationship starts at 0 on all axes; the deltas
// are applied to the starting 0.
func TestRelationshipEngine_ApplyMemoryDeltasCreatesRelationship(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "bob", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M"})

	eng := NewRelationshipEngine()
	mem := core.Memory{
		ID:         "m1",
		OwnerID:    "alice",
		EventID:    "e1",
		Tick:       100,
		TrustDelta: 10,
	}
	eng.ApplyMemoryDeltas(w, mem, "bob")

	if len(w.Relationships) != 1 {
		t.Fatalf("len(Relationships) = %d, want 1 (alice→bob created)", len(w.Relationships))
	}
	r := w.Relationships[0]
	if r.FromID != "alice" || r.ToID != "bob" {
		t.Errorf("relationship = %+v, want {alice -> bob}", r)
	}
	// TrustDelta=10 is applied to Trust axis. RelationshipDelta=0
	// so spread=0. Trust should be exactly 10.
	if r.Trust != 10 {
		t.Errorf("Trust = %f, want 10 (TrustDelta only)", r.Trust)
	}
	if r.Respect != 0 || r.Fear != 0 || r.Attraction != 0 || r.Loyalty != 0 {
		t.Errorf("non-Trust axes: %+v, want all 0 (RelationshipDelta=0)", r)
	}
}

// TestRelationshipEngine_ApplyMemoryDeltasUpdatesExistingRelationship
// verifies that calling ApplyMemoryDeltas for an existing
// relationship updates the axes (adds the deltas to the current
// values, clamped to [0, 100]).
func TestRelationshipEngine_ApplyMemoryDeltasUpdatesExistingRelationship(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "bob", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M"})
	w.Relationships = []core.Relationship{
		{FromID: "alice", ToID: "bob", Trust: 50, Respect: 50, Fear: 50, Attraction: 50, Loyalty: 50},
	}

	eng := NewRelationshipEngine()
	// TrustDelta=-20, RelationshipDelta=10 (spread=2 per axis).
	mem := core.Memory{
		ID:                "m1",
		OwnerID:           "alice",
		EventID:           "e1",
		Tick:              100,
		TrustDelta:        -20,
		RelationshipDelta: 10,
	}
	eng.ApplyMemoryDeltas(w, mem, "bob")

	if len(w.Relationships) != 1 {
		t.Fatalf("len(Relationships) = %d, want 1 (no duplicate created)", len(w.Relationships))
	}
	r := w.Relationships[0]
	// Trust: 50 + (-20) + 2 (spread) = 32.
	if r.Trust != 32 {
		t.Errorf("Trust = %f, want 32 (50 - 20 + 2)", r.Trust)
	}
	// Other axes: 50 + 2 = 52.
	if r.Respect != 52 || r.Fear != 52 || r.Attraction != 52 || r.Loyalty != 52 {
		t.Errorf("other axes: %+v, want all 52 (50 + 2 spread)", r)
	}
}

// TestRelationshipEngine_ApplyMemoryDeltasClamping verifies that
// the deltas are clamped to [0, 100] after application. A large
// negative TrustDelta on an already-low Trust should floor at 0.
func TestRelationshipEngine_ApplyMemoryDeltasClamping(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "bob", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M"})
	w.Relationships = []core.Relationship{
		{FromID: "alice", ToID: "bob", Trust: 5},
	}
	eng := NewRelationshipEngine()
	mem := core.Memory{ID: "m1", OwnerID: "alice", TrustDelta: -50}
	eng.ApplyMemoryDeltas(w, mem, "bob")
	r := w.Relationships[0]
	if r.Trust < 0 {
		t.Errorf("Trust went negative: %f (clamping failed)", r.Trust)
	}
	if r.Trust != 0 {
		t.Errorf("Trust = %f, want 0 (5 - 50 clamped)", r.Trust)
	}
}

// TestRelationshipEngine_ApplyMemoryDeltasSelfIsNoop verifies that
// calling ApplyMemoryDeltas with targetID == OwnerID is a no-op
// (a person cannot have a relationship with themselves).
func TestRelationshipEngine_ApplyMemoryDeltasSelfIsNoop(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	eng := NewRelationshipEngine()
	mem := core.Memory{ID: "m1", OwnerID: "alice", TrustDelta: 10}
	eng.ApplyMemoryDeltas(w, mem, "alice")
	if len(w.Relationships) != 0 {
		t.Errorf("self-relationship created: %+v", w.Relationships)
	}
	// Empty targetID is also a no-op.
	eng.ApplyMemoryDeltas(w, mem, "")
	if len(w.Relationships) != 0 {
		t.Errorf("empty-target relationship created: %+v", w.Relationships)
	}
}

// TestRelationshipEngine_Deterministic verifies that two runs of
// the same simulation (same seed, same input) produce identical
// relationship state. This is the determinism contract from
// SIMULATION_TICK_SPEC.md §3.
func TestRelationshipEngine_Deterministic(t *testing.T) {
	makeWorld := func() *core.World {
		w := core.NewWorld("det", 42, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
		w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village"})
		w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "village"})
		w.AddPerson(&core.Person{ID: "p3", Name: "Carol", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village"})
		return w
	}
	run := func() []core.Relationship {
		w := makeWorld()
		eng := NewRelationshipEngine()
		sim := tick.NewSimulation(42, eng)
		if err := sim.Init(w); err != nil {
			t.Fatalf("Init: %v", err)
		}
		for i := 0; i < 20; i++ {
			if err := sim.Tick(w); err != nil {
				t.Fatalf("Tick: %v", err)
			}
		}
		return w.Relationships
	}
	a := run()
	b := run()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("non-deterministic relationship state:\n run 1 = %+v\n run 2 = %+v", a, b)
	}
}

// TestRelationshipEngine_PersistsAcrossSimulationTicks verifies
// that relationships created in early ticks are still present in
// later ticks (and are subject to the same decay). This is the
// "state accumulates over time" invariant.
func TestRelationshipEngine_PersistsAcrossSimulationTicks(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F", LocationID: "village"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: 0, Alive: true, Gender: "M", LocationID: "village"})

	eng := NewRelationshipEngine()
	eng.DecayRate = 0
	sim := tick.NewSimulation(1, eng)
	for i := 0; i < 50; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	if len(w.Relationships) != 1 {
		t.Fatalf("len(Relationships) = %d, want 1 (the p1→p2 pair from tick 1)", len(w.Relationships))
	}
	r := w.Relationships[0]
	if r.FromID != "p1" || r.ToID != "p2" {
		t.Errorf("relationship = %+v, want {p1 -> p2}", r)
	}
	if r.Trust != 50 {
		t.Errorf("Trust = %f, want 50 (zero decay, unchanged)", r.Trust)
	}
}
