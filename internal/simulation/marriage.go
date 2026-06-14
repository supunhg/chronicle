// MarriageEngine matches unmarried opposite-gender adults at the
// same location and sets their SpouseID. Phase 24 v1: required by
// the 5-Generation Integration Test (the test asserts marriages >
// 0) and by the existing PopulationEngine.births (which requires
// mother.SpouseID to be set).
//
// Determinism: marriage matching is deterministic per (worldSeed,
// tick, personID) via sorted IDs. The person with the lower ID
// acts as the "matcher" — when P (unmarried) finds Q (unmarried,
// opposite gender, same location, Q.ID > P.ID), the engine sets
// both P.SpouseID and Q.SpouseID. This prevents double-matching
// (A marries B AND B marries A) and keeps the engine free of RNG.
//
// V1 model: any co-located unmarried opposite-gender adult pair
// becomes a marriage. There is no trust-threshold check — the
// CourtAction (Phase 21) builds trust over time, and a future
// phase can add a "mutual courtship required" rule. For v1 the
// goal is to produce marriages, not to gate them on social
// preconditions.
package simulation

import (
	"github.com/chronicle-dev/chronicle/internal/core"
)

// MarriageEngine is the Phase 24 v1 marriage matcher. Runs once
// per tick. Stateless (no Init).
type MarriageEngine struct{}

// NewMarriageEngine returns a MarriageEngine with default settings.
func NewMarriageEngine() *MarriageEngine { return &MarriageEngine{} }

// Init is a no-op for the v1 MarriageEngine.
func (m *MarriageEngine) Init(w *core.World) error { return nil }

// Tick matches unmarried opposite-gender adults at the same
// location. Runs in O(N + sum-locationSize^2) in the worst case;
// in practice the v1 populations are small enough that this is
// trivial.
//
// Order: iterate living people in deterministic ID order. For
// each unmarried adult P, look at the co-located living people
// (also in ID order) and find the first eligible match Q with
// Q.ID > P.ID. Set both SpouseID. The Q.ID > P.ID rule ensures
// each match is processed exactly once.
func (m *MarriageEngine) Tick(w *core.World) error {
	for _, p := range w.LivingPeople() {
		if !p.IsAdult(w.Tick) {
			continue
		}
		if p.SpouseID != "" {
			continue
		}
		wantGender := "M"
		if p.Gender == "M" {
			wantGender = "F"
		}
		// Find the first eligible partner at the same
		// location with ID > p.ID. This is deterministic.
		var partnerID string
		for _, other := range w.LivingPeopleAt(p.LocationID) {
			if other.ID == p.ID {
				continue
			}
			if !other.IsAdult(w.Tick) {
				continue
			}
			if other.Gender != wantGender {
				continue
			}
			if other.SpouseID != "" {
				continue
			}
			if other.ID <= p.ID {
				// We've already passed this person in
				// the outer loop; if they were eligible
				// they would have been married to
				// someone with an even lower ID.
				continue
			}
			partnerID = other.ID
			break
		}
		if partnerID == "" {
			continue
		}
		p.SpouseID = partnerID
		w.People[partnerID].SpouseID = p.ID
	}
	return nil
}

// MarriageCount returns the number of distinct married couples
// currently alive in the world. A marriage is counted only when
// both spouses are alive. Used by the Phase 24 integration test
// metrics.
func MarriageCount(w *core.World) int {
	seen := make(map[string]bool)
	n := 0
	for _, p := range w.LivingPeople() {
		if p.SpouseID == "" || seen[p.ID] {
			continue
		}
		spouse, ok := w.People[p.SpouseID]
		if !ok || !spouse.Alive {
			continue
		}
		seen[p.ID] = true
		seen[spouse.ID] = true
		n++
	}
	return n
}
