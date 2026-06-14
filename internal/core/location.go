package core

// ResourceID is the canonical name of a v1 economy resource. Phase 22
// ships exactly 4 produced resources (food, wood, iron, cloth) plus
// coin (which is not produced — it circulates through buy/sell).
type ResourceID string

// The 4 v1 produced resources. Each one has a matching
// settlement stock field and a price field. Add more here when
// extending the production chain; the rest of the engine
// (EconomyEngine, worldpack) reads through these constants.
const (
	ResourceFood  ResourceID = "food"
	ResourceWood  ResourceID = "wood"
	ResourceIron  ResourceID = "iron"
	ResourceCloth ResourceID = "cloth"
)

// SettlementInventory is the per-location stock of the 4 v1
// produced resources. Coin is intentionally NOT tracked here —
// coin lives on Person records (and on World.Coin for the player)
// and is not a settlement stock. Food/wood/iron/cloth are produced
// and consumed at the settlement level by the EconomyEngine.
//
// Phase 22 v1: all resources are stored as float64 so production
// and consumption can use fractional rates (e.g., a miner produces
// 0.5 iron per tick, everyone consumes 0.1 food per tick). The
// per-tick float arithmetic stays in a stable range as long as
// the production:consumption ratio is near 1.0.
type SettlementInventory struct {
	Food  float64
	Wood  float64
	Iron  float64
	Cloth float64
}

// Prices is the per-location dynamic price of each v1 resource,
// in coin. Prices are recomputed by the EconomyEngine each tick
// from the base price (from the worldpack's item catalog) and the
// current settlement stock:
//
//	price = base * (target_stock / max(0.01, actual_stock))
//	clamped to [0.1x, 10x] of base.
//
// A shortage (stock < target) raises the price; a surplus lowers
// it. Future phases can hook events (famine, good harvest) to
// modify the target stock instead of the formula.
type Prices struct {
	Food  int
	Wood  int
	Iron  int
	Cloth int
}

// Location is a settlement in the simulation.
//
// Phase 2 v1 fields per chronicle-spec.md §5.1:
//   - PopulationCap is a soft cap; population pressure rises when exceeded
//   - Pressure is a 0-100 scalar that modifies migration probability
//
// Phase 22 v1 fields:
//   - Settlement is the per-location stock of the 4 produced
//     resources. Mutated in place by the EconomyEngine each tick.
//   - Prices is the dynamic per-resource price, recomputed each
//     tick from base price and stock.
type Location struct {
	ID            string
	Name          string
	Region        string
	Population    int
	PopulationCap int
	Pressure      int // 0..100, derived from Population vs PopulationCap

	// Settlement is the location's stock of the 4 v1 produced
	// resources. Coin is tracked separately (on Person and on
	// World.Coin). Zero-value is a valid initial state; the
	// worldpack's bootstrap can pre-seed it from defaults.
	Settlement SettlementInventory

	// Prices is the current per-unit coin price of each v1
	// resource at this location, recomputed by the EconomyEngine
	// each tick. Zero values are a valid initial state; the
	// engine seeds them from the worldpack catalog at Init.
	Prices Prices
}

// NewLocation returns a Location with the given ID, name, region, and
// population cap. Population starts at 0 and Pressure at 0; both are
// recomputed by the PopulationEngine. Settlement starts at zero — the
// worldpack's Bootstrap pre-seeds it from defaults.
func NewLocation(id, name, region string, cap int) *Location {
	return &Location{
		ID:            id,
		Name:          name,
		Region:        region,
		Population:    0,
		PopulationCap: cap,
		Pressure:      0,
	}
}

// IsOvercrowded returns true if Population exceeds PopulationCap.
func (l *Location) IsOvercrowded() bool {
	return l.Population > l.PopulationCap
}
