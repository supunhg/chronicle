package simulation

import (
	"sort"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// RelationshipEngine implements chronicle-spec.md §5.2.
//
// Phase 14 behavior:
//
//  1. New relationships are formed when two living NPCs are at the
//     same location and no (from, to) relationship already exists.
//     New relationships start at NewAcquaintanceTrust on the Trust
//     axis and 0 on the other 4 axes. Relationships are directed
//     (per spec): an A→B relationship is created, not a symmetric
//     pair. Pairs are iterated in (A.ID, B.ID) sorted order so
//     the output is deterministic.
//
//  2. All existing relationship axes decay toward 50 (neutral) by
//     DecayRate per tick. Axes are clamped to [0, 100]. An axis
//     already at 50 is unchanged. This implements the spec's
//     "decay old relationships slightly".
//
//  3. ApplyMemoryDeltas is a public O(1) method that the Phase 15+
//     MemoryEngine calls at memory creation time to bake a
//     memory's TrustDelta and RelationshipDelta into the
//     corresponding relationship score. Tick does NOT re-apply
//     deltas every tick; the relationship is the cached aggregate
//     (see spec §5.6 "The historical event log that produced the
//     current values lives in the memories table").
//
// The engine is deterministic: same (worldSeed, tick, input state)
// produces the same output. No global RNG is used.
type RelationshipEngine struct {
	// NewAcquaintanceTrust is the starting Trust value for newly
	// created relationships. Default 50 (neutral). Set to 0 to
	// start new relationships with no trust. The other 4 axes
	// always start at 0.
	NewAcquaintanceTrust float64

	// DecayRate is the per-tick amount each relationship axis
	// moves toward 50 (neutral). Default 0.01 (~3.65 per sim-year).
	// Set to 0 to disable decay.
	DecayRate float64
}

// NewRelationshipEngine returns a RelationshipEngine with default
// settings (NewAcquaintanceTrust=50, DecayRate=0.01).
func NewRelationshipEngine() *RelationshipEngine {
	return &RelationshipEngine{
		NewAcquaintanceTrust: 50,
		DecayRate:            0.01,
	}
}

// Init is a no-op for Phase 14. The relationship index used by
// Tick is rebuilt on every Tick (the cost is O(R) which is
// negligible compared to the O(N²) pair scan). Future phases may
// cache the index here.
func (r *RelationshipEngine) Init(w *core.World) error { return nil }

// Tick advances the relationship state by one tick. Order of
// operations (deterministic):
//  1. formRelationships: create new (from, to) pairs for co-located NPCs
//  2. decayRelationships: move all axes toward 50
func (r *RelationshipEngine) Tick(w *core.World) error {
	r.formRelationships(w)
	r.decayRelationships(w)
	return nil
}

// formRelationships creates a new relationship for each pair of
// living people at the same location where no (from, to)
// relationship already exists. New relationships start at
// NewAcquaintanceTrust on the Trust axis and 0 on the other 4
// axes. Pairs are processed in (A.ID, B.ID) sorted order for
// determinism.
//
// Complexity: O(N²) per tick in the worst case (all NPCs in one
// location), but in practice each location has a small population
// (villages ~20, towns ~80), so the work is bounded. Once all
// pairs have been processed, subsequent ticks are O(R) for the
// decay step.
func (r *RelationshipEngine) formRelationships(w *core.World) {
	// Build a set of existing (from, to) keys for O(1) lookup.
	existing := make(map[string]bool, len(w.Relationships))
	for _, rel := range w.Relationships {
		existing[rel.Key()] = true
	}

	// Group living people by location.
	byLocation := make(map[string][]*core.Person)
	for _, p := range w.LivingPeople() {
		byLocation[p.LocationID] = append(byLocation[p.LocationID], p)
	}

	// For each location, iterate over pairs in sorted ID order.
	// Phase 26 Part E: iterate locations in sorted ID order so the
	// append order to w.Relationships is deterministic. Go map
	// iteration is randomized, and the per-location inner sort
	// only covers pairs within a single location — it does not
	// order locations relative to each other.
	locIDs := make([]string, 0, len(byLocation))
	for id := range byLocation {
		locIDs = append(locIDs, id)
	}
	sort.Strings(locIDs)
	for _, locID := range locIDs {
		people := byLocation[locID]
		sort.Slice(people, func(i, j int) bool { return people[i].ID < people[j].ID })
		for i := 0; i < len(people); i++ {
			for j := i + 1; j < len(people); j++ {
				a, b := people[i], people[j]
				keyA := a.ID + "|" + b.ID
				keyB := b.ID + "|" + a.ID
				// If either direction already has a relationship,
				// skip. This prevents creating a duplicate when
				// the spec's symmetric-relationship rule is later
				// relaxed.
				if existing[keyA] || existing[keyB] {
					continue
				}
				w.Relationships = append(w.Relationships, core.Relationship{
					FromID:     a.ID,
					ToID:       b.ID,
					Trust:      r.NewAcquaintanceTrust,
					Respect:    0,
					Fear:       0,
					Attraction: 0,
					Loyalty:    0,
				})
				existing[keyA] = true
			}
		}
	}
}

// decayRelationships moves each relationship axis toward 50
// (neutral) by DecayRate per tick. Axes are clamped to [0, 100].
// An axis already at 50 is unchanged. A zero DecayRate disables
// decay entirely.
func (r *RelationshipEngine) decayRelationships(w *core.World) {
	if r.DecayRate <= 0 {
		return
	}
	for i := range w.Relationships {
		rel := &w.Relationships[i]
		rel.Trust = decayAxis(rel.Trust, r.DecayRate)
		rel.Respect = decayAxis(rel.Respect, r.DecayRate)
		rel.Fear = decayAxis(rel.Fear, r.DecayRate)
		rel.Attraction = decayAxis(rel.Attraction, r.DecayRate)
		rel.Loyalty = decayAxis(rel.Loyalty, r.DecayRate)
	}
}

// decayAxis returns value moved toward 50 (neutral) by rate. If
// the move would cross 50, the result is clamped at 50 (the
// axis stops at neutral, not at 0 or 100). An axis already at 50
// is unchanged. No [0, 100] clamping is needed because the
// result is always in [min(value, 50), max(value, 50)], which is
// within [0, 100] for any value in [0, 100].
func decayAxis(value, rate float64) float64 {
	const neutral = 50.0
	switch {
	case value > neutral:
		v := value - rate
		if v <= neutral {
			return neutral
		}
		return v
	case value < neutral:
		v := value + rate
		if v >= neutral {
			return neutral
		}
		return v
	default:
		return neutral
	}
}

// ApplyMemoryDeltas applies a Memory's TrustDelta and
// RelationshipDelta to the owner's relationship with targetID. If
// the relationship doesn't exist, it is created with the deltas
// baked in. This is the O(1) application path per the spec's
// §5.2 ("updates O(1) per event instead of O(history)").
//
// TrustDelta is applied to the Trust axis. RelationshipDelta is
// spread evenly across all 5 axes (each gets RelationshipDelta/5).
// After both deltas are applied, each axis is clamped to [0, 100].
//
// No-op (returns silently) when targetID is empty or equal to
// m.OwnerID (a person cannot have a relationship with themselves).
// targetID is NOT validated against w.People; the Phase 15+
// MemoryEngine is responsible for passing a valid, alive target.
// Passing a non-existent target will create an orphaned
// relationship row that no Snapshot will be able to render in a
// meaningful way — this is a bug in the caller, not the engine.
//
// This method is public so the Phase 15+ MemoryEngine can call it
// at memory creation time. The RelationshipEngine.Tick method does
// NOT call this; deltas are applied once, at creation time.
func (r *RelationshipEngine) ApplyMemoryDeltas(w *core.World, m core.Memory, targetID string) {
	if targetID == "" || targetID == m.OwnerID {
		return
	}
	key := m.OwnerID + "|" + targetID
	rel := findOrCreateRelationship(&w.Relationships, m.OwnerID, targetID, key)

	// Apply TrustDelta to Trust axis, then add the spread (1/5 of
	// RelationshipDelta) to all 5 axes including Trust. This matches
	// the spec: "TrustDelta is specific to the Trust axis.
	// RelationshipDelta is spread across all 5 axes."
	spread := m.RelationshipDelta / 5
	rel.Trust = clampAxis(rel.Trust + m.TrustDelta + spread)
	rel.Respect = clampAxis(rel.Respect + spread)
	rel.Fear = clampAxis(rel.Fear + spread)
	rel.Attraction = clampAxis(rel.Attraction + spread)
	rel.Loyalty = clampAxis(rel.Loyalty + spread)
}

// findOrCreateRelationship returns a pointer to the relationship
// with the given key in rels, creating it (with zero axes) if it
// doesn't exist. The returned pointer aliases the slice element,
// so mutations are visible to the caller.
func findOrCreateRelationship(rels *[]core.Relationship, fromID, toID, key string) *core.Relationship {
	for i := range *rels {
		if (*rels)[i].Key() == key {
			return &(*rels)[i]
		}
	}
	*rels = append(*rels, core.Relationship{FromID: fromID, ToID: toID})
	return &(*rels)[len(*rels)-1]
}

// clampAxis returns v clamped to [0, 100].
func clampAxis(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
