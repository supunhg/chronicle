// Phase 22: Economy Engine V1.
//
// Implements the closed production/consumption loop and dynamic
// pricing called for in the Phase 22 spec:
//
//	NPC production
//	      ↓
//	Settlement stock
//	      ↓
//	Population consumption
//	      ↓
//	Price recalculation
//
// V1 scope: 4 produced resources (food, wood, iron, cloth),
// 4 production occupations (farmer, woodcutter, miner, weaver),
// uniform per-person food consumption, and a simple target-ratio
// price function. Coin is NOT produced — it circulates through
// buy/sell and lives on Person / World.Coin.
//
// Determinism: all per-person production is driven by
// occupation (a static field on Person) and consumes no RNG.
// Consumption is uniform (no RNG). Price recalc is pure math.
// The daily loop is therefore fully deterministic for a given
// (worldSeed, tick, locationID, personID) triple.
package simulation

import (
	"fmt"
	"sort"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// EconomyEngine runs the per-tick production, consumption, and
// price loop. See the package doc above for the V1 scope.
type EconomyEngine struct{}

// NewEconomyEngine returns an EconomyEngine with default settings.
func NewEconomyEngine() *EconomyEngine { return &EconomyEngine{} }

// Phase 22 v1 economy constants. Tunable in v2 via a worldpack
// EconomySpec; for v1 they are hardcoded so the loop is
// self-contained and the test is straightforward.
const (
	// EconomyDefaultTargetStock is the per-resource target stock
	// used by the price function. Prices rise when stock falls
	// below this and fall when stock rises above it. V1: 100,
	// matching the worldpack's default starting food stock
	// (50) * 2 — a single bad harvest will start raising
	// prices but the worst-case clamp (10x) gives plenty of
	// headroom before the player is priced out.
	EconomyDefaultTargetStock = 100.0

	// EconomyDefaultConsumePerTick is the per-person food
	// consumption rate (food units per tick). V1: 0.1, so 100
	// people eat 10 food/tick = 3650 food/year. A settlement
	// starting with 50 food runs out in 5 days without
	// production, which is the design intent: food
	// scarcity drives the narrative.
	EconomyDefaultConsumePerTick = 0.1

	// EconomyPriceClampMin and EconomyPriceClampMax bound the
	// price function. Without clamping, a stock of 0.01 would
	// make food cost 10000x base, and a stock of 1000 would
	// make it free. V1: [0.1x, 10x] of the base price.
	EconomyPriceClampMin = 0.1
	EconomyPriceClampMax = 10.0

	// EconomyProductionPerTick is the per-adult producer output
	// for a single resource. V1: 1.0 (the spec's "produce
	// one unit per day" default). A miner producing 0.5
	// iron/tick would balance the default, but the v1
	// specification is uniform 1.0/unit; the miner's
	// "harder" nature is encoded in the
	// `production_loops.miner.output.iron = 0.5` field of
	// the worldpack and can be hooked up in v2.
	EconomyProductionPerTick = 1.0

	// EconomyShortageThreshold is the per-location food stock
	// below which the location is considered "in shortage".
	// Phase 22.5: when Settlement.Food < threshold, every
	// resident's NeedHunger is bumped by EconomyShortagePenalty
	// each tick and a famine_risk memory is emitted on the
	// first shortage tick.
	EconomyShortageThreshold = 20.0

	// EconomyShortagePenalty is the per-tick hunger bump
	// applied to every person at a location in food shortage.
	// Phase 22.5 v1: 5 (a single tick of shortage raises
	// every resident's hunger by 5). With the default
	// NeedHunger starting at 50, a 10-tick unbroken shortage
	// pins hunger at 100, which dominates WorkAction scoring.
	EconomyShortagePenalty = 5
)

// Init is a no-op for the v1 EconomyEngine — the bootstrap
// pre-seeds each location's Settlement and Prices from the
// worldpack. Init exists to satisfy the tick.Engine interface.
//
// A future phase may use Init to compute derived stats (e.g.,
// the per-location producer count) and stash them in a
// side-table; for now, the engine recomputes what it needs
// each Tick.
func (e *EconomyEngine) Init(w *core.World) error { return nil }

// Tick runs the daily economy loop: production, consumption,
// price recalc. Order is fixed:
//
//  1. Production: each living adult producer deposits 1 unit
//     of their occupation's resource into their location's
//     Settlement.
//  2. Consumption: each living person at a location consumes
//     0.1 food (uniform). Food stock clamps at 0 (a shortage
//     does not crash; it triggers a price spike and, in
//     future phases, a hunger-need bump).
//  3. Price recalc: per-location Prices[resource] = base *
//     (target / max(0.01, stock)) clamped to [0.1x, 10x].
//
// Children do not produce. Dead people are skipped. People
// with an empty LocationID are skipped (no consumption at
// "the road").
func (e *EconomyEngine) Tick(w *core.World) error {
	e.runProduction(w)
	e.runConsumption(w)
	e.runPriceRecalc(w)
	return nil
}

// runProduction walks every living adult and deposits
// EconomyProductionPerTick units of their occupation's
// resource into their location's Settlement. Non-producers
// (and people with an unknown occupation) produce nothing.
// People with no LocationID are skipped.
func (e *EconomyEngine) runProduction(w *core.World) {
	for _, p := range w.LivingPeople() {
		if !p.IsAdult(w.Tick) {
			continue
		}
		if p.LocationID == "" {
			continue
		}
		resource := producedBy(p.Occupation)
		if resource == "" {
			continue
		}
		loc, ok := w.Locations[p.LocationID]
		if !ok {
			continue
		}
		addToSettlement(&loc.Settlement, resource, EconomyProductionPerTick)
	}
}

// runConsumption has every living person at a location eat
// EconomyDefaultConsumePerTick food. If the location is out
// of food, the stock clamps at 0 and the price recalc will
// produce a 10x spike.
//
// Phase 22.5 (Economic Feedback Loop): if a location's food
// stock falls below EconomyShortageThreshold, every person
// at that location has their NeedHunger bumped by
// EconomyShortagePenalty (clamped at 100). The first tick of
// a shortage emits a famine_risk memory for every affected
// resident; subsequent ticks of the same shortage do not
// emit again (transition detection via LastShortageTick).
//
// This is the spec's "Shortages -> New NPC Goals" arrow:
// hungry NPCs see a higher WorkAction.Score (which has
// +hunger*0.4) and start preferring food production, which
// stabilizes the settlement.
func (e *EconomyEngine) runConsumption(w *core.World) {
	for _, p := range w.LivingPeople() {
		if p.LocationID == "" {
			continue
		}
		loc, ok := w.Locations[p.LocationID]
		if !ok {
			continue
		}
		consumed := EconomyDefaultConsumePerTick
		if loc.Settlement.Food < consumed {
			consumed = loc.Settlement.Food
		}
		loc.Settlement.Food -= consumed

		// Phase 22.5: shortage detection. Bump hunger and
		// (on the transition tick) emit a memory.
		if loc.Settlement.Food < EconomyShortageThreshold {
			if p.Needs == nil {
				p.Needs = make(map[string]int)
			}
			hunger := p.Needs[string(core.NeedHunger)] + EconomyShortagePenalty
			if hunger > 100 {
				hunger = 100
			}
			p.Needs[string(core.NeedHunger)] = hunger
		}
	}

	// Phase 22.5: emit a famine_risk memory on the first tick
	// of each shortage. Transition detection: a memory is
	// emitted only if the previous tick was NOT a shortage.
	// LastShortageTick=0 is the "never in shortage" sentinel
	// (set at bootstrap); a non-zero value means we have
	// recorded a shortage in some previous tick.
	//
	// Phase 26 Part E: iterate locations in sorted ID order so
	// the order of memory appends (and LastShortageTick
	// mutations) is deterministic. Go map iteration is
	// randomized.
	locIDs := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		locIDs = append(locIDs, id)
	}
	sort.Strings(locIDs)
	for _, locID := range locIDs {
		loc := w.Locations[locID]
		if loc.Settlement.Food >= EconomyShortageThreshold {
			continue
		}
		// Emit iff we have a non-zero prior shortage record
		// AND the prior record is for the immediately
		// preceding tick. The non-zero check handles the
		// bootstrap case (LastShortageTick=0 means "never in
		// shortage" — the first shortage tick IS a
		// transition). The >= w.Tick-1 check handles the
		// in-shortage case (no transition between consecutive
		// shortage ticks).
		if loc.LastShortageTick != 0 && loc.LastShortageTick >= w.Tick-1 {
			continue
		}
		loc.LastShortageTick = w.Tick
		for _, p := range w.LivingPeopleAt(loc.ID) {
			w.Memories = append(w.Memories, core.Memory{
				ID:          fmt.Sprintf("mem-famine-%d-%s", w.Tick, p.ID),
				OwnerID:     p.ID,
				EventID:     fmt.Sprintf("famine-%d-%s", w.Tick, loc.ID),
				Tick:        w.Tick,
				Importance:  0.5,
				Recency:     1.0,
				Description: fmt.Sprintf("Food shortages spread through %s.", loc.Name),
				Tags:        []string{"famine_risk"},
			})
		}
	}
}

// runPriceRecalc computes the new Prices for each location's
// 4 resources from the current Settlement stock and the
// worldpack's item catalog base Value. The formula is:
//
//	price = round(base * target / max(0.01, stock))
//	clamped to [base * 0.1, base * 10]
//
// A missing item-catalog entry falls back to a base price of
// 1 coin (so the world is still playable, just with placeholder
// prices).
func (e *EconomyEngine) runPriceRecalc(w *core.World) {
	// Phase 26 Part E: iterate locations in sorted ID order.
	// The Prices writes are commutative (each location's
	// prices only depend on its own Settlement), but a
	// deterministic iteration order makes engine behavior
	// provably order-insensitive and matches the pattern used
	// by runConsumption.
	locIDs := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		locIDs = append(locIDs, id)
	}
	sort.Strings(locIDs)
	for _, locID := range locIDs {
		loc := w.Locations[locID]
		loc.Prices.Food = recomputePrice(w, core.ResourceFood, loc.Settlement.Food)
		loc.Prices.Wood = recomputePrice(w, core.ResourceWood, loc.Settlement.Wood)
		loc.Prices.Iron = recomputePrice(w, core.ResourceIron, loc.Settlement.Iron)
		loc.Prices.Cloth = recomputePrice(w, core.ResourceCloth, loc.Settlement.Cloth)
	}
}

// recomputePrice returns the clamped dynamic price for one
// resource at one location. basePrice is the item-catalog
// value (1 if the catalog has no entry). The formula is
// `base * target / max(0.01, stock)`, clamped to
// [base*EconomyPriceClampMin, base*EconomyPriceClampMax],
// rounded to the nearest integer coin.
func recomputePrice(w *core.World, resource core.ResourceID, stock float64) int {
	base := 1
	if w.Items != nil {
		if item, ok := w.Items[string(resource)]; ok && item.Value > 0 {
			base = item.Value
		}
	}
	stockSafe := stock
	if stockSafe < 0.01 {
		stockSafe = 0.01
	}
	raw := float64(base) * EconomyDefaultTargetStock / stockSafe
	if raw < float64(base)*EconomyPriceClampMin {
		raw = float64(base) * EconomyPriceClampMin
	}
	if raw > float64(base)*EconomyPriceClampMax {
		raw = float64(base) * EconomyPriceClampMax
	}
	// Round to nearest int (the player's coin is integer; a
	// fractional price would be un-buyable in the action
	// engine's int-arithmetic coin ledger).
	if raw < 0 {
		raw = 0
	}
	return int(raw + 0.5)
}

// producedBy returns the resource produced by the given
// occupation, or "" for non-producers. V1: exactly 4
// producers (farmer, woodcutter, miner, weaver) per the
// Phase 22 spec. Bakers, hunters, and carpenters are
// non-producers in v1: bakers consume food (like everyone
// else), hunters do not produce in v1 (could be added in
// v2), and carpenters do not cut wood (they shape it from
// existing stock — that path can be added when the
// per-occupation production scaling lands).
func producedBy(occupation string) core.ResourceID {
	switch occupation {
	case "farmer":
		return core.ResourceFood
	case "woodcutter":
		return core.ResourceWood
	case "miner":
		return core.ResourceIron
	case "weaver":
		return core.ResourceCloth
	}
	return ""
}

// addToSettlement adds delta to the matching Settlement field
// for the given resource. delta can be negative (for future
// consumption paths that need to refund) but the engine
// doesn't use that in v1.
func addToSettlement(s *core.SettlementInventory, resource core.ResourceID, delta float64) {
	switch resource {
	case core.ResourceFood:
		s.Food += delta
	case core.ResourceWood:
		s.Wood += delta
	case core.ResourceIron:
		s.Iron += delta
	case core.ResourceCloth:
		s.Cloth += delta
	}
}
