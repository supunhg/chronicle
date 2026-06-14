package simulation

import (
	"fmt"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// MemoryEngine implements chronicle-spec.md §5.6 (Memory Engine —
// Causal Anchoring).
//
// Phase 15 behavior:
//
//  1. Detects deaths (PopulationEngine.mortality set DeathTick =
//     w.Tick) and creates a memory record for the deceased's
//     spouse (if alive). The death memory is a record only — it
//     does NOT call RelationshipEngine.ApplyMemoryDeltas, because
//     the relationship with a dead person is semantically frozen
//     and creating a new Trust=0 relationship would be misleading.
//     Phase 16+ can add non-zero deltas (e.g., LoyaltyDelta for
//     grief).
//
//  2. Detects births (PopulationEngine.births created a child with
//     BirthTick == w.Tick) and creates memory records for the
//     mother AND father (if alive). Each parent's memory has
//     TrustDelta=+20 (mother) or +15 (father) and IS applied to
//     the parent→child relationship via
//     RelationshipEngine.ApplyMemoryDeltas. This is the O(1)
//     application path per the spec's §5.2.
//
//  3. Recency decay is a Phase 16+ concern; for now, Recency stays
//     at 1.0 (just happened) on every new memory.
//
// The engine is deterministic: same (worldSeed, tick, input state)
// produces the same output. Memory IDs and EventIDs are derived
// from (tick, personID) so two runs with the same input produce
// identical records.
type MemoryEngine struct {
	// RelationshipEngine is the engine that maintains the
	// relationship score. The MemoryEngine calls
	// ApplyMemoryDeltas on it to bake memory deltas into the
	// relationship cache. If nil, memories are still recorded
	// but relationship scores don't update.
	RelationshipEngine *RelationshipEngine
}

// NewMemoryEngine returns a MemoryEngine with no
// RelationshipEngine wired. The CLI must set the field before
// calling Tick (see runPlay/runResume in cmd/chronicle/main.go).
func NewMemoryEngine() *MemoryEngine {
	return &MemoryEngine{}
}

// Init is a no-op for Phase 15. Phase 16+ may snapshot the alive
// people set here to enable causal-chain detection (e.g., "this
// death was caused by the famine event 50 ticks ago").
func (m *MemoryEngine) Init(w *core.World) error { return nil }

// Tick advances the memory state by one tick. Order of
// operations:
//  1. recordDeaths: detect new deaths, create spouse memory
//  2. recordBirths: detect new births, create parent memories
//  3. decayMemories: placeholder for Phase 16+ recency decay
func (m *MemoryEngine) Tick(w *core.World) error {
	m.recordDeaths(w)
	m.recordBirths(w)
	m.decayMemories(w)
	return nil
}

// recordDeaths creates a memory record for the spouse of each
// person who died this tick (DeathTick == w.Tick AND !Alive). The
// memory targets the deceased; the owner is the spouse. No
// relationship deltas are applied (see MemoryEngine doc comment
// for rationale).
func (m *MemoryEngine) recordDeaths(w *core.World) {
	for _, p := range w.People {
		// Both conditions must hold: DeathTick was set this tick
		// (PopulationEngine.mortality sets it) AND the person is
		// now marked as not alive. The Alive check guards against
		// stale snapshots where DeathTick happens to equal w.Tick
		// but the person died in a previous load.
		if p.DeathTick != w.Tick || p.Alive {
			continue
		}
		if p.SpouseID == "" {
			continue
		}
		spouse, ok := w.People[p.SpouseID]
		if !ok || !spouse.Alive {
			continue
		}
		mem := core.Memory{
			ID:             fmt.Sprintf("mem-death-%d-%s", w.Tick, p.ID),
			OwnerID:        spouse.ID,
			EventID:        fmt.Sprintf("death-%d-%s", w.Tick, p.ID),
			Tick:           w.Tick,
			Importance:     0.9,
			Recency:        1.0,
			EmotionalScore: 0.8,
			Description:    fmt.Sprintf("%s died", p.Name),
			Tags:           []string{"death"},
		}
		w.Memories = append(w.Memories, mem)
		// Death memories don't update the relationship score.
	}
}

// recordBirths creates a memory record for the mother and father
// of each person born this tick (BirthTick == w.Tick AND Alive).
// Each parent's memory targets the child and is applied to the
// parent→child relationship via ApplyMemoryDeltas.
//
// Mother gets TrustDelta=+20 (slightly stronger bond — she
// experienced the birth). Father gets TrustDelta=+15 (slightly
// weaker — he may not have been present). These are Phase 15
// v1 values; worldpacks can override.
func (m *MemoryEngine) recordBirths(w *core.World) {
	for _, p := range w.People {
		if p.BirthTick != w.Tick || !p.Alive {
			continue
		}
		// Mother's memory.
		if p.MotherID != "" {
			mother, ok := w.People[p.MotherID]
			if !ok || !mother.Alive {
				// Don't create a memory for a dead mother — she
				// can't form new bonds. Phase 16+ may create a
				// posthumous memory with different semantics.
			} else {
				mem := core.Memory{
					ID:             fmt.Sprintf("mem-birth-%d-%s-mother", w.Tick, p.ID),
					OwnerID:        mother.ID,
					EventID:        fmt.Sprintf("birth-%d-%s", w.Tick, p.ID),
					Tick:           w.Tick,
					Importance:     0.7,
					Recency:        1.0,
					EmotionalScore: 0.6,
					TrustDelta:     20.0,
					Description:    fmt.Sprintf("gave birth to %s", p.Name),
					Tags:           []string{"birth", "family"},
				}
				w.Memories = append(w.Memories, mem)
				m.applyDeltas(w, mem, p.ID)
			}
		}
		// Father's memory.
		if p.FatherID != "" {
			father, ok := w.People[p.FatherID]
			if !ok || !father.Alive {
				// Don't create a memory for a dead father.
			} else {
				mem := core.Memory{
					ID:             fmt.Sprintf("mem-birth-%d-%s-father", w.Tick, p.ID),
					OwnerID:        father.ID,
					EventID:        fmt.Sprintf("birth-%d-%s", w.Tick, p.ID),
					Tick:           w.Tick,
					Importance:     0.7,
					Recency:        1.0,
					EmotionalScore: 0.6,
					TrustDelta:     15.0,
					Description:    fmt.Sprintf("fathered %s", p.Name),
					Tags:           []string{"birth", "family"},
				}
				w.Memories = append(w.Memories, mem)
				m.applyDeltas(w, mem, p.ID)
			}
		}
	}
}

// decayMemories is a placeholder for Phase 16+ recency decay.
// For now, Recency stays at 1.0 on every memory (just happened).
func (m *MemoryEngine) decayMemories(w *core.World) {}

// applyDeltas calls RelationshipEngine.ApplyMemoryDeltas if the
// field is set. No-op if RelationshipEngine is nil (the engine
// still records memories, but relationship scores don't update).
func (m *MemoryEngine) applyDeltas(w *core.World, mem core.Memory, targetID string) {
	if m.RelationshipEngine == nil {
		return
	}
	m.RelationshipEngine.ApplyMemoryDeltas(w, mem, targetID)
}
