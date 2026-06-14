package simulation

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// TestMemoryEngine_Init is the no-op invariant for Init. Phase 15
// doesn't use Init for caching, but the contract remains.
func TestMemoryEngine_Init(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	// Pre-existing people use BirthTick in the past (negative) so
	// the engine doesn't detect them as "just born" at tick 0.
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
	eng := NewMemoryEngine()
	if err := eng.Init(w); err != nil {
		t.Errorf("Init: %v", err)
	}
}

// TestMemoryEngine_NoEventsNoMemories verifies that ticking on a
// world with no births or deaths creates no memories. This is the
// "no-op" baseline.
func TestMemoryEngine_NoEventsNoMemories(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	// Pre-existing people have BirthTick in the past (negative)
	// so they're not detected as "just born". DeathTick is 0 by
	// default, and w.Tick starts at 0; the recordDeaths check
	// also requires !p.Alive, so living people are not detected
	// as "just died".
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "p1"})
	w.People["p1"].SpouseID = "p2"

	eng := NewMemoryEngine()
	for i := 0; i < 10; i++ {
		if err := eng.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	if len(w.Memories) != 0 {
		t.Errorf("len(Memories) = %d, want 0 (no births or deaths)", len(w.Memories))
	}
}

// TestMemoryEngine_RecordsDeath verifies that when a person dies
// this tick, a memory record is created for their spouse (if
// alive). The memory is a record only — no relationship deltas
// are applied (see MemoryEngine doc comment for rationale).
func TestMemoryEngine_RecordsDeath(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F", SpouseID: "p2"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "p1"})

	eng := NewMemoryEngine()
	// Manually mark p2 as dead this tick.
	w.People["p2"].Alive = false
	w.People["p2"].DeathTick = w.Tick

	if err := eng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(w.Memories) != 1 {
		t.Fatalf("len(Memories) = %d, want 1 (p1's memory of p2's death)", len(w.Memories))
	}
	mem := w.Memories[0]
	if mem.OwnerID != "p1" {
		t.Errorf("OwnerID = %q, want p1", mem.OwnerID)
	}
	if !strings.Contains(mem.Description, "Bob") {
		t.Errorf("Description = %q, want it to contain 'Bob'", mem.Description)
	}
	if mem.Tick != w.Tick {
		t.Errorf("Tick = %d, want %d", mem.Tick, w.Tick)
	}
	if mem.Importance < 0.8 {
		t.Errorf("Importance = %f, want >= 0.8 (death is high-importance)", mem.Importance)
	}
	if mem.EventID == "" {
		t.Error("EventID is empty; expected a deterministic event ID")
	}
	if mem.Tags == nil || len(mem.Tags) == 0 || mem.Tags[0] != "death" {
		t.Errorf("Tags = %v, want at least 'death'", mem.Tags)
	}
}

// TestMemoryEngine_NoMemoryIfSpouseDead verifies that if the
// deceased's spouse is already dead, no memory is created (the
// dead spouse can't form new bonds).
func TestMemoryEngine_NoMemoryIfSpouseDead(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F", SpouseID: "p2"})
	w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: -20 * 365, Alive: false, Gender: "M", SpouseID: "p1", DeathTick: 50})

	eng := NewMemoryEngine()
	w.Tick = 100
	// p1 dies this tick. p2 is the spouse but is already dead.
	w.People["p1"].Alive = false
	w.People["p1"].DeathTick = w.Tick

	if err := eng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(w.Memories) != 0 {
		t.Errorf("len(Memories) = %d, want 0 (spouse is dead)", len(w.Memories))
	}
}

// TestMemoryEngine_NoMemoryIfNoSpouse verifies that a person who
// dies without a spouse generates no death memory.
func TestMemoryEngine_NoMemoryIfNoSpouse(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})

	eng := NewMemoryEngine()
	w.People["p1"].Alive = false
	w.People["p1"].DeathTick = w.Tick

	if err := eng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(w.Memories) != 0 {
		t.Errorf("len(Memories) = %d, want 0 (no spouse)", len(w.Memories))
	}
}

// TestMemoryEngine_RecordsBirth verifies that when a child is born
// this tick (BirthTick == w.Tick), memory records are created for
// the mother AND father (if alive). Each parent's memory has a
// positive TrustDelta that is applied to the parent→child
// relationship.
func TestMemoryEngine_RecordsBirth(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "m1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "f1", Name: "Bob", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "m1"})
	w.People["m1"].SpouseID = "f1"

	popEng := NewPopulationEngine()
	relEng := NewRelationshipEngine()
	memEng := &MemoryEngine{RelationshipEngine: relEng}

	mother := w.People["m1"]
	father := w.People["f1"]
	child := popEng.CreateChild(w, mother, father)

	if err := memEng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// Should have 2 memories: mother's and father's.
	if len(w.Memories) != 2 {
		t.Fatalf("len(Memories) = %d, want 2 (mother's and father's)", len(w.Memories))
	}
	var motherMem, fatherMem *core.Memory
	for i := range w.Memories {
		switch w.Memories[i].OwnerID {
		case "m1":
			motherMem = &w.Memories[i]
		case "f1":
			fatherMem = &w.Memories[i]
		}
	}
	if motherMem == nil {
		t.Fatal("mother's memory not found")
	}
	if fatherMem == nil {
		t.Fatal("father's memory not found")
	}
	if motherMem.TrustDelta != 20.0 {
		t.Errorf("mother TrustDelta = %f, want 20.0", motherMem.TrustDelta)
	}
	if fatherMem.TrustDelta != 15.0 {
		t.Errorf("father TrustDelta = %f, want 15.0", fatherMem.TrustDelta)
	}
	if motherMem.EventID != fatherMem.EventID {
		t.Errorf("mother and father EventIDs differ: %q vs %q (should share the same birth event)",
			motherMem.EventID, fatherMem.EventID)
	}
	if !strings.Contains(motherMem.Description, "gave birth") {
		t.Errorf("mother description = %q, want it to contain 'gave birth'", motherMem.Description)
	}
	if !strings.Contains(fatherMem.Description, "fathered") {
		t.Errorf("father description = %q, want it to contain 'fathered'", fatherMem.Description)
	}
	if motherMem.Importance < 0.6 {
		t.Errorf("mother Importance = %f, want >= 0.6 (birth is medium-importance)", motherMem.Importance)
	}
	_ = child // referenced for clarity
}

// TestMemoryEngine_AppliesDeltasOnBirth verifies that the birth
// memory's TrustDelta is applied to the parent→child relationship
// via RelationshipEngine.ApplyMemoryDeltas. The relationship
// should be created with Trust=20 (mother) or Trust=15 (father).
func TestMemoryEngine_AppliesDeltasOnBirth(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "m1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "f1", Name: "Bob", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "m1"})
	w.People["m1"].SpouseID = "f1"

	popEng := NewPopulationEngine()
	relEng := NewRelationshipEngine()
	memEng := &MemoryEngine{RelationshipEngine: relEng}

	mother := w.People["m1"]
	father := w.People["f1"]
	child := popEng.CreateChild(w, mother, father)

	if err := memEng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Find the mother's relationship with the child.
	var motherToChild, fatherToChild *core.Relationship
	for i := range w.Relationships {
		switch {
		case w.Relationships[i].FromID == "m1" && w.Relationships[i].ToID == child.ID:
			motherToChild = &w.Relationships[i]
		case w.Relationships[i].FromID == "f1" && w.Relationships[i].ToID == child.ID:
			fatherToChild = &w.Relationships[i]
		}
	}
	if motherToChild == nil {
		t.Fatal("mother→child relationship not created")
	}
	if motherToChild.Trust != 20 {
		t.Errorf("mother→child Trust = %f, want 20", motherToChild.Trust)
	}
	if fatherToChild == nil {
		t.Fatal("father→child relationship not created")
	}
	if fatherToChild.Trust != 15 {
		t.Errorf("father→child Trust = %f, want 15", fatherToChild.Trust)
	}
}

// TestMemoryEngine_NoRelationshipCreatedWithNilEngine verifies that
// memories are still recorded even when RelationshipEngine is nil,
// but no parent→child relationships are created (since the
// relationship score requires the engine to apply deltas).
func TestMemoryEngine_NoRelationshipCreatedWithNilEngine(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "m1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "f1", Name: "Bob", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "m1"})
	w.People["m1"].SpouseID = "f1"

	popEng := NewPopulationEngine()
	memEng := NewMemoryEngine() // nil RelationshipEngine

	popEng.CreateChild(w, w.People["m1"], w.People["f1"])

	if err := memEng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// Memories should still be recorded.
	if len(w.Memories) != 2 {
		t.Errorf("len(Memories) = %d, want 2 (memories recorded even without RelationshipEngine)", len(w.Memories))
	}
	// But no parent→child relationship should exist.
	for _, r := range w.Relationships {
		if (r.FromID == "m1" || r.FromID == "f1") && strings.HasPrefix(r.ToID, "c") {
			t.Errorf("parent→child relationship created without RelationshipEngine: %+v", r)
		}
	}
}

// TestMemoryEngine_Deterministic verifies that two runs of the same
// simulation (same seed, same input) produce identical memory
// state. This is the determinism contract.
func TestMemoryEngine_Deterministic(t *testing.T) {
	makeWorld := func() *core.World {
		w := core.NewWorld("det", 42, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
		w.AddPerson(&core.Person{ID: "m1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
		w.AddPerson(&core.Person{ID: "f1", Name: "Bob", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "m1"})
		w.People["m1"].SpouseID = "f1"
		return w
	}
	run := func() []core.Memory {
		w := makeWorld()
		popEng := NewPopulationEngine()
		relEng := NewRelationshipEngine()
		memEng := &MemoryEngine{RelationshipEngine: relEng}
		sim := tick.NewSimulation(42, popEng, relEng, memEng)
		if err := sim.Init(w); err != nil {
			t.Fatalf("Init: %v", err)
		}
		// Create a child to trigger a birth memory.
		popEng.CreateChild(w, w.People["m1"], w.People["f1"])
		for i := 0; i < 5; i++ {
			if err := sim.Tick(w); err != nil {
				t.Fatalf("Tick: %v", err)
			}
		}
		return w.Memories
	}
	a := run()
	b := run()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("non-deterministic memories:\n run 1 = %+v\n run 2 = %+v", a, b)
	}
}

// TestMemoryEngine_BirthAndDeathSameTick verifies that a tick
// containing both a birth and a death creates both kinds of
// memories.
func TestMemoryEngine_BirthAndDeathSameTick(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "m1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
	w.AddPerson(&core.Person{ID: "f1", Name: "Bob", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "m1"})
	w.People["m1"].SpouseID = "f1"
	// Add a third person with a spouse who will die.
	w.AddPerson(&core.Person{ID: "p3", Name: "Carol", BirthTick: -20 * 365, Alive: true, Gender: "F", SpouseID: "p4"})
	w.AddPerson(&core.Person{ID: "p4", Name: "Dave", BirthTick: -20 * 365, Alive: true, Gender: "M", SpouseID: "p3"})

	popEng := NewPopulationEngine()
	relEng := NewRelationshipEngine()
	memEng := &MemoryEngine{RelationshipEngine: relEng}

	// Birth: create a child.
	popEng.CreateChild(w, w.People["m1"], w.People["f1"])
	// Death: mark p4 as dead.
	w.People["p4"].Alive = false
	w.People["p4"].DeathTick = w.Tick

	if err := memEng.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// 2 birth memories (mother, father) + 1 death memory (p3's) = 3.
	if len(w.Memories) != 3 {
		t.Errorf("len(Memories) = %d, want 3 (2 birth + 1 death)", len(w.Memories))
	}
	var birthCount, deathCount int
	for _, mem := range w.Memories {
		if mem.EventID == "" {
			t.Error("memory has empty EventID")
		}
		if len(mem.Tags) > 0 && mem.Tags[0] == "death" {
			deathCount++
			if mem.OwnerID != "p3" {
				t.Errorf("death memory OwnerID = %q, want p3", mem.OwnerID)
			}
		} else {
			birthCount++
		}
	}
	if birthCount != 2 {
		t.Errorf("birth count = %d, want 2", birthCount)
	}
	if deathCount != 1 {
		t.Errorf("death count = %d, want 1", deathCount)
	}
}
