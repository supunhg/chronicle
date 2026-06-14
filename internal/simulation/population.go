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
package simulation

import (
	"fmt"
	"math/rand"
	"sort"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// Phase 2 v1 demographic constants. The engine reads its tunable
// parameters from w.RulesOrDefault() in production. The constants
// below are kept only for two reasons:
//
//   - MinBirthIntervalTicks and MaxChildren are used as fallbacks
//     inside births() when w.Rules has zero values, and are referenced
//     directly by tests.
//   - AnnualConceptionChance is not (yet) part of the worldpack and
//     remains a hardcoded engine constant.
const (
	// MinBirthIntervalTicks is the minimum number of ticks between
	// successive births to the same mother (~12 sim-months = 1 sim-year).
	MinBirthIntervalTicks int64 = 365

	// MaxChildren is the lifetime cap on children per mother.
	MaxChildren = 6

	// AnnualConceptionChance is the per-year probability that a fertile
	// couple conceives on any given year (~18% per year).
	AnnualConceptionChance = 0.18
)

// PopulationEngine implements births, deaths, aging, aging-out, and
// migration. Tick order per SIMULATION_TICK_SPEC.md §2: Population
// runs after the Economy and before the Relationship/Goal engines.
type PopulationEngine struct {
	// AnnualDeathChance is the per-year death probability. Default 0.01.
	AnnualDeathChance float64
}

// NewPopulationEngine returns a PopulationEngine with default parameters.
// The AnnualDeathChance field is only consulted when w.Rules is nil
// (see mortality); the built-in default of 0.01 matches
// core.WorldRules.RulesOrDefault().
func NewPopulationEngine() *PopulationEngine {
	return &PopulationEngine{AnnualDeathChance: 0.01}
}

// Init is a no-op for the Phase 2 PopulationEngine. It exists to
// satisfy the tick.Engine interface.
func (p *PopulationEngine) Init(w *core.World) error { return nil }

// Tick runs the population engine for one tick.
//
// Order of operations (deterministic):
//  1. Mortality — per-person death roll, sets DeathTick
//  2. Births — fertile couples conceive
//  3. Migration — over-cap locations push excess to under-cap locations
//  4. Recompute location populations
//
// Tunable parameters (death chance, fertile ages, min birth interval,
// max children) come from w.Rules (set by worldpack.Bootstrap). If
// w.Rules is nil, falls back to the engine's built-in defaults via
// w.RulesOrDefault. The engine's own AnnualDeathChance field is
// honored only when w.Rules is nil, for backwards-compat with tests
// that don't set up a worldpack.
func (p *PopulationEngine) Tick(w *core.World) error {
	rules := w.RulesOrDefault()
	p.mortality(w, rules)
	p.births(w, rules)
	p.migration(w)
	w.RecomputeLocationPopulations()
	return nil
}

// mortality applies the per-day death roll. Sets DeathTick on death.
//
// Precedence: w.Rules.AnnualDeathChance > p.AnnualDeathChance.
// A zero chance disables mortality for this tick.
func (p *PopulationEngine) mortality(w *core.World, rules core.WorldRules) {
	var chance float64
	if w.Rules != nil {
		chance = rules.AnnualDeathChance
	} else {
		chance = p.AnnualDeathChance
	}
	if chance <= 0 {
		return
	}
	dailyChance := chance / 365.0
	for _, person := range w.LivingPeople() {
		r := tick.EntityRand(w.Seed, w.Tick, person.ID+":death")
		if r.Float64() < dailyChance {
			person.Alive = false
			person.DeathTick = w.Tick
		}
	}
}

// births iterates fertile women, checks birth constraints, and
// creates new children. Births are deterministic given the world
// seed and the current tick.
//
// Fertile ages, max children, and min birth interval come from
// w.Rules. Zero values in Rules are treated as "use the built-in
// Phase 2 v1 default" so partial rules packs still work.
func (p *PopulationEngine) births(w *core.World, rules core.WorldRules) {
	minAge := rules.FertileMinAge
	if minAge == 0 {
		minAge = 16
	}
	maxAge := rules.FertileMaxAge
	if maxAge == 0 {
		maxAge = 50
	}
	minInterval := rules.MinBirthIntervalTicks
	if minInterval == 0 {
		minInterval = MinBirthIntervalTicks
	}
	maxKids := rules.MaxChildren
	if maxKids == 0 {
		maxKids = MaxChildren
	}

	for _, mother := range w.LivingPeople() {
		if mother.Gender != "F" {
			continue
		}
		age := mother.AgeAt(w.Tick)
		if age < minAge || age > maxAge {
			continue
		}
		if mother.SpouseID == "" {
			continue
		}
		father, ok := w.People[mother.SpouseID]
		if !ok || !father.Alive {
			continue
		}
		// Enforce max children.
		if countChildren(w, mother.ID) >= maxKids {
			continue
		}
		// Enforce min birth interval.
		if last := lastBirthTick(w, mother.ID); w.Tick-last < minInterval {
			continue
		}
		// Roll for conception (per-day chance derived from annual).
		dailyChance := AnnualConceptionChance / 365.0
		r := tick.EntityRand(w.Seed, w.Tick, mother.ID+":conceive")
		if r.Float64() >= dailyChance {
			continue
		}
		p.createChild(w, mother, father, r)
	}
}

// CreateChild creates a new child of the given mother and father, using
// the world's deterministic RNG. The child is added to the world and
// returned. Used by births() during a Tick; also exposed for tests that
// need to bypass the conception roll.
//
// Callers (production) are responsible for ensuring preconditions:
//   - mother and father are alive
//   - mother is fertile
//   - mother is married to father
//   - MinBirthIntervalTicks has passed since mother's last birth
//   - mother has fewer than MaxChildren total
func (p *PopulationEngine) CreateChild(w *core.World, mother, father *core.Person) *core.Person {
	r := tick.EntityRand(w.Seed, w.Tick, mother.ID+":createChild")
	return p.createChild(w, mother, father, r)
}

// createChild is the internal implementation. The child ID is
// deterministic from (worldTick, motherID).
func (p *PopulationEngine) createChild(w *core.World, mother, father *core.Person, r *rand.Rand) *core.Person {
	gender := "M"
	if r.Float64() < 0.5 {
		gender = "F"
	}
	childID := fmt.Sprintf("c%d-%s", w.Tick, mother.ID)
	child := &core.Person{
		ID:         childID,
		Name:       pickName(r, gender),
		Gender:     gender,
		BirthTick:  w.Tick,
		Alive:      true,
		LocationID: mother.LocationID,
		FatherID:   father.ID,
		MotherID:   mother.ID,
	}
	w.AddPerson(child)
	return child
}

// migration moves people out of over-cap locations to under-cap
// locations. Pressure is recomputed per tick. Callers must have
// already called RecomputeLocationPopulations (Tick does this once
// at the end, so the order is: mortality, births, migration, recompute).
//
// Tunable parameters (MigrationFraction, MinMigrantsPerTick) come from
// w.RulesOrDefault(), populated by worldpack.Bootstrap from
// pack.Rules.Migration. The defaults built into RulesOrDefault are
// 0.5 and 1 respectively, matching the Phase 2 v1 hardcoded values.
func (p *PopulationEngine) migration(w *core.World) {
	rules := w.RulesOrDefault()
	fraction := rules.MigrationFraction
	minMigrants := rules.MinMigrantsPerTick
	for _, loc := range w.Locations {
		if !loc.IsOvercrowded() {
			loc.Pressure = 0
			continue
		}
		excess := loc.Population - loc.PopulationCap
		// Pressure: ratio of excess to cap, capped at 100.
		pressure := (excess * 100) / loc.PopulationCap
		if pressure > 100 {
			pressure = 100
		}
		loc.Pressure = pressure

		// Find a destination: first under-cap location in ID order.
		var destID string
		ids := sortedLocationIDs(w)
		for _, id := range ids {
			if id == loc.ID {
				continue
			}
			if w.Locations[id].Population < w.Locations[id].PopulationCap {
				destID = id
				break
			}
		}
		if destID == "" {
			continue
		}
		// Number of migrants this tick: fraction of the excess,
		// rounded up, at least minMigrants.
		nMove := (excess*int(fraction*100) + 99) / 100
		if nMove < minMigrants {
			nMove = minMigrants
		}
		if nMove > excess {
			nMove = excess
		}
		// Pick the first N people at this location in ID order.
		candidates := w.LivingPeopleAt(loc.ID)
		if nMove > len(candidates) {
			nMove = len(candidates)
		}
		for i := 0; i < nMove; i++ {
			candidates[i].LocationID = destID
		}
	}
}

// countChildren returns the number of children whose MotherID or
// FatherID matches the given parent ID. Counts all children (alive
// or dead) to enforce the lifetime MaxChildren cap.
func countChildren(w *core.World, parentID string) int {
	n := 0
	for _, p := range w.People {
		if p.MotherID == parentID || p.FatherID == parentID {
			n++
		}
	}
	return n
}

// lastBirthTick returns the most recent BirthTick among children of
// the given mother. Returns 0 if she has no children yet.
func lastBirthTick(w *core.World, motherID string) int64 {
	var last int64
	for _, p := range w.People {
		if p.MotherID == motherID && p.BirthTick > last {
			last = p.BirthTick
		}
	}
	return last
}

// sortedLocationIDs returns the location IDs in deterministic order.
func sortedLocationIDs(w *core.World) []string {
	ids := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// pickName returns a deterministic first name for the given gender.
func pickName(r *rand.Rand, gender string) string {
	if gender == "F" {
		return femaleNames[r.Intn(len(femaleNames))]
	}
	return maleNames[r.Intn(len(maleNames))]
}

// Name pools for Phase 2 v1. The world pack will replace these in
// Phase 3 with era-appropriate names.
var (
	maleNames = []string{
		"Adam", "Bert", "Carl", "David", "Eric", "Frank", "George", "Henry",
		"Ian", "Jacob", "Karl", "Liam", "Mark", "Ned", "Owen", "Paul",
	}
	femaleNames = []string{
		"Alice", "Beth", "Carol", "Diana", "Eve", "Fiona", "Grace", "Helen",
		"Iris", "Jane", "Kate", "Lily", "Mary", "Nora", "Olive", "Rose",
	}
)
