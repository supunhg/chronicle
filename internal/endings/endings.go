package endings

import (
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// Ending is the canonical v2 ending per ARCHITECTURE.md §19.
//
// An Ending evaluates to "valid" if every entry of its
// Conditions (story.Condition, §7) returns true against the
// current WorldState. The configured Endings registry is
// evaluated at finale (§23 step "CheckEvents" — though Endings
// evaluation happens one step later, when Runner.Step lands
// on the finale node via Engine.Endings).
//
// Twelve Endings ship per §20: Hero, Dragon Sovereign, World
// Guardian, Archmage, Shadow Lord, Corruption, Kingdom, Dragon
// Alliance, Elara Romance, Selene Romance, Orion Romance,
// Wanderer.
type Ending struct {
	// ID is the canonical ending identifier (e.g. "hero",
	// "elara_romance"). Unique across the registry.
	ID string

	// Priority is the resolution rank: highest Priority wins.
	// Two Endings with equal Priority are tie-broken by ID
	// lexicographic order (deterministic per §18A).
	Priority int

	// Conditions gate the Ending's validity. If any
	// Condition.Check returns false, this Ending is invalid
	// (skipped during Evaluate).
	Conditions []story.Condition
}

// Evaluate returns the highest-priority valid Ending for ws.
//
// The returned bool is true iff at least one Ending matched.
// If no Ending matches, Evaluate returns the zero Ending and
// false; the engine treats that as "no ending recovered at
// finale" and falls back to a generic miss message (or the
// content-loading test fails — see PHASES.md §38.E
// TestAllEndingsReachable gate).
//
// Determinism: the loop processes Endings in input order.
// Equal-Priority ties are broken by ID lexicographic order
// so two equals-Priority endings resolve deterministically
// across runs and machines (§18A invariant #2).
func Evaluate(ws state.WorldState, list []Ending) (Ending, bool) {
	var best *Ending
	bestID := ""
	for i := range list {
		e := &list[i]
		ok := true
		for _, c := range e.Conditions {
			if !c.Check(ws) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		switch {
		case best == nil:
			best = e
			bestID = e.ID
		case e.Priority > best.Priority:
			best = e
			bestID = e.ID
		case e.Priority == best.Priority && e.ID < bestID:
			// Equal priority — ID order tie-breaks.
			best = e
			bestID = e.ID
		}
	}
	if best == nil {
		return Ending{}, false
	}
	return *best, true
}
