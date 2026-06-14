// Package simulation contains the Tier 2 engines of the simulation.
//
// Phase 2 implements the complete PopulationEngine per
// chronicle-spec.md §5.1: aging, mortality with DeathTick, aging-out,
// births with family trees, and migration with population pressure.
//
// Phase 14 implements the RelationshipEngine per chronicle-spec.md
// §5.2: creates new relationships when NPCs are co-located, decays
// existing axes toward neutral, and exposes ApplyMemoryDeltas for
// the MemoryEngine to bake memory-driven deltas into relationship
// scores.
//
// Phase 15 implements the MemoryEngine per chronicle-spec.md §5.6:
// detects births and deaths and writes causally-anchored Memory
// records to w.Memories. The engine calls
// RelationshipEngine.ApplyMemoryDeltas at memory creation time so
// the relationship score is the O(1) cached aggregate.
//
// Phase 21 implements the Goal Engine V1 per the Phase 21 spec:
// traits → needs → goals → candidate actions → utility scoring
// → action selection → execution → memory/relationship updates.
// See the Phase 21 spec at the top of actions.go for the
// candidate-action set and the utility formula.
package simulation

import (
	"github.com/chronicle-dev/chronicle/internal/core"
)

// GoalEngine is the Phase 21 v1 implementation of the Goal Engine
// per chronicle-spec.md §5.3.
//
// The engine runs a per-NPC decision loop once per tick:
//
//  1. Decay needs by DefaultNeedDecay (clamped at 0).
//  2. For each NPC, generate the 6 candidate actions and score
//     them via the utility formula (see actions.go).
//  3. Pick the highest-scoring action and execute it.
//  4. Append the resulting memories to w.Memories.
//
// Action scoring and noise are deterministic per
// (worldSeed, tick, personID) — see tick.EntityRand.
//
// Init seeds DefaultNeeds for every person whose Needs map is
// empty, and assigns default goals (GoalBecomeWealthy and
// GoalRaiseFamily at priority 0.5) for adults whose Goals slice
// is nil/empty. The default goals match the v1 set in the spec.
type GoalEngine struct{}

// NewGoalEngine returns a GoalEngine with default settings.
func NewGoalEngine() *GoalEngine {
	return &GoalEngine{}
}

// DefaultNeeds is the Phase 2 v1 default need set. Phase 21 keeps
// the same string keys (hunger, wealth, companionship, safety)
// and adds "rest". The map is exported so pre-existing tests
// and external code can iterate the default need set without
// reaching into the engine's internal Init logic.
var DefaultNeeds = map[string]int{
	"hunger":        core.DefaultNeedInitial,
	"wealth":        core.DefaultNeedInitial,
	"companionship": core.DefaultNeedInitial,
	"safety":        core.DefaultNeedInitial,
	"rest":          core.DefaultNeedInitial,
}

// Init sets Needs to the v1 default for every living person
// whose Needs map is empty, and assigns default goals for
// adults whose Goals slice is nil. Existing needs and goals
// are preserved.
//
// Default goals (Phase 21 v1): every adult (>=16 years) gets
// GoalBecomeWealthy and GoalRaiseFamily at priority 0.5.
// These are the two most universally applicable v1 goals;
// the engine can specialize per-occupation defaults in a
// future phase.
func (g *GoalEngine) Init(w *core.World) error {
	for _, p := range w.LivingPeople() {
		if p.Needs == nil {
			p.Needs = make(map[string]int, len(DefaultNeeds))
			for k, v := range DefaultNeeds {
				p.Needs[k] = v
			}
		}
		if p.Goals == nil {
			p.Goals = []core.Goal{}
		}
		if !p.IsAdult(w.Tick) {
			continue
		}
		if len(p.Goals) == 0 {
			p.Goals = []core.Goal{
				{ID: core.GoalBecomeWealthy, Priority: 0.5},
				{ID: core.GoalRaiseFamily, Priority: 0.5},
			}
		}
	}
	return nil
}

// Tick advances the goal state by one tick. The per-NPC
// decision loop:
//
//  1. Decay all needs by DefaultNeedDecay (clamped at 0).
//  2. For each adult living person, generate the 6 candidate
//     actions and pick the highest-scoring one.
//  3. Execute the chosen action and append its memories.
//
// Children (IsAdult=false) skip the action loop but still
// get their needs decayed. Dead people (Alive=false) are
// skipped entirely.
func (g *GoalEngine) Tick(w *core.World) error {
	for _, p := range w.LivingPeople() {
		// 1. Decay needs.
		for need := range p.Needs {
			v := p.Needs[need] - core.DefaultNeedDecay
			if v < 0 {
				v = 0
			}
			p.Needs[need] = v
		}
		// 2. Skip action selection for children.
		if !p.IsAdult(w.Tick) {
			continue
		}
		// 3. Generate, score, select.
		chosen := selectAction(p, w)
		if chosen == nil {
			continue
		}
		// 4. Execute and append memories.
		mems := chosen.Execute(p, w)
		w.Memories = append(w.Memories, mems...)
	}
	return nil
}

// selectAction scores every candidate action for p and
// returns the highest-scoring one. Returns nil only if the
// action list is empty (which is not possible in Phase 21 —
// the v1 list always has 6 entries).
func selectAction(p *core.Person, w *core.World) Action {
	actions := AllActions()
	if len(actions) == 0 {
		return nil
	}
	var best Action
	var bestScore float64 = -1
	for _, a := range actions {
		s := a.Score(p, w)
		if s > bestScore {
			bestScore = s
			best = a
		}
	}
	return best
}
