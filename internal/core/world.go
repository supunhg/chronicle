// Package core defines the minimal domain types for Chronicle's simulation.
package core

import (
	"sort"
	"time"
)

// World is the top-level container for all simulation state.
type World struct {
	// ID is the unique identifier for this world (short hex, 8 chars).
	ID string

	// Tick is the current simulation tick. Starts at 0 and increments by
	// one each time the orchestration layer runs a full tick.
	Tick int64

	// Seed is the deterministic seed for all RNG in this world.
	Seed int64

	// Now is the simulated clock time corresponding to the current tick.
	Now time.Time

	// People is the registry of all Person records, keyed by ID.
	People map[string]*Person

	// Locations is the registry of all Location records, keyed by ID.
	// Phase 2: required for migration and birth logic.
	Locations map[string]*Location

	// Factions is the registry of all Faction records, keyed by ID.
	// Phase 4: populated by worldpack.Bootstrap. Membership is derived.
	Factions map[string]*Faction

	// Relationships is the registry of all Relationship records.
	// Phase 2: stub-only, populated by RelationshipEngine in later phases.
	Relationships []Relationship

	// Memories is the registry of all Memory records, owned by
	// the PopulationEngine / EventEngine (see chronicle-spec.md §5.6).
	// Phase 13: persisted by Snapshot/Restore. A memory's
	// TrustDelta and RelationshipDelta are baked into the
	// corresponding relationship score on write, so the
	// relationships slice is the cached aggregate of the
	// memories slice.
	Memories []Memory

	// Rules holds the tunable world rules, populated by
	// worldpack.Bootstrap from pack.Rules. If nil, engines fall back
	// to their built-in defaults (see World.RulesOrDefault).
	//
	// Defined as a core type (rather than a *worldpack.Pack) to avoid
	// a circular import: worldpack already imports core, so core
	// cannot import worldpack.
	Rules *WorldRules

	// PlayerID is the ID of the person the player controls.
	// Empty means "no specific player" — the simulation runs in
	// world-level mode (the player has no body, so player-scoped
	// actions like travel are unavailable). Phase 17.5: the
	// action engine reads this to scope travel and talk. Future
	// phases may populate it from the CLI (e.g., a `--player`
	// flag on `chronicle play`).
	PlayerID string

	// Items is the world's item catalog: metadata for all known
	// item types, keyed by canonical lowercase name. Phase 18
	// replaces the Phase 17.6 hardcoded priceList in the action
	// engine. Populated by worldpack.Bootstrap from the
	// worldpack's EconomySpec.Items (with sensible defaults
	// when a resource has no explicit spec). The action engine's
	// buy/sell handlers look up the per-item Value from this
	// catalog. A nil or empty catalog means no items can be
	// bought or sold (which is the correct Phase 17.6 fallback
	// for worlds without a worldpack).
	Items map[string]Item

	// Inventory is the player's carried items, keyed by
	// canonical lowercase item name. Phase 18 promoted this
	// from map[string]int to map[string]Item: each stack now
	// carries its own Count, Weight, Value, and MaxDurability.
	// The metadata is copied from the catalog (Items) at
	// acquisition time so a switch back to a pre-buy world still
	// preserves the snapshot of the items' properties at the
	// time they were acquired.
	Inventory map[string]Item

	// Coin is the player's money. Phase 17.6: a simple integer
	// counter. Buy reduces Coin; sell increases it. Phase 18+
	// may add inflation, debt, and per-faction currencies.
	Coin int
}

// Item represents a single item type in the world. The same
// struct is used for both the world's item catalog
// (World.Items) and for stacks in inventories (World.Inventory).
// Count is only meaningful for inventory entries; catalog
// entries have Count=0.
//
// Phase 18 promoted Inventory from map[string]int to
// map[string]Item so each stack carries its own weight, value,
// and durability metadata. The worldpack's EconomySpec.Items
// is the source of truth for the per-item properties; the
// action engine copies them into Inventory stacks at
// acquisition time.
type Item struct {
	// Name is the canonical lowercase item name (e.g. "bread").
	// This is also the map key in World.Items and World.Inventory.
	Name string

	// Count is the number of this item in the stack. Only
	// meaningful for inventory entries; catalog entries
	// (World.Items) have Count=0.
	Count int

	// Weight is the mass of a single unit, in kg. Used for
	// future encumbrance rules (Phase 19+).
	Weight float64

	// Value is the coin price per unit, set from the worldpack's
	// economy at catalog-load time. Buy deducts Value*Count from
	// Coin; sell adds Value*Count to Coin.
	Value int

	// MaxDurability is the starting durability of a fresh item
	// (1.0 = perfect, 0.5 = worn). 0 means the item has no
	// durability (perishable consumables like bread). Phase 18
	// does not yet track per-instance durability; this is the
	// upper bound for the future per-instance field.
	MaxDurability float64
}

// WorldRules holds the tunable world rules. Fields are sourced from a
// worldpack's Rules.Lifecycle and Rules.Family blocks.
//
// Zero values in a populated WorldRules are legitimate (e.g.,
// AnnualDeathChance = 0 for a world without mortality). Engines that
// consult World.RulesOrDefault should treat zero values as "use this
// value", not "use the default". The default is only used when
// World.Rules itself is nil.
type WorldRules struct {
	// Lifecycle (from pack.Rules.Lifecycle)
	AdultAge          int
	FertileMinAge     int
	FertileMaxAge     int
	AnnualDeathChance float64

	// Family (from pack.Rules.Family)
	MinBirthIntervalTicks int64
	MaxChildren           int

	// Migration (from pack.Rules.Migration)
	//
	// MigrationFraction is the fraction of the over-cap excess that
	// attempts to migrate each tick. Set to 0 to disable migration
	// entirely. The built-in default (via RulesOrDefault when
	// w.Rules is nil) is 0.5.
	MigrationFraction float64

	// MinMigrantsPerTick is the floor on the number of migrants that
	// move from an over-cap location each tick. The built-in default
	// is 1.
	MinMigrantsPerTick int
}

// NewWorld returns a World with the given ID, seed, and starting time.
// Rules is left nil; callers (typically worldpack.Bootstrap) set it
// after loading a worldpack.
func NewWorld(id string, seed int64, start time.Time) *World {
	return &World{
		ID:            id,
		Seed:          seed,
		Now:           start,
		People:        make(map[string]*Person),
		Locations:     make(map[string]*Location),
		Factions:      make(map[string]*Faction),
		Relationships: []Relationship{},
		Memories:      []Memory{},
		Inventory:     make(map[string]Item),
	}
}

// RulesOrDefault returns w.Rules if set, otherwise a default value
// matching the Phase 2 v1 simulation constants. Engines should call
// this at the start of Tick (or per-decision) so a world with no
// pack falls back to sane defaults.
func (w *World) RulesOrDefault() WorldRules {
	if w.Rules != nil {
		return *w.Rules
	}
	return WorldRules{
		AdultAge:              16,
		FertileMinAge:         16,
		FertileMaxAge:         50,
		AnnualDeathChance:     0.01,
		MinBirthIntervalTicks: 365,
		MaxChildren:           6,
		MigrationFraction:     0.5,
		MinMigrantsPerTick:    1,
	}
}

// AddPerson registers a person. Panics on duplicate ID (programming error).
func (w *World) AddPerson(p *Person) *Person {
	if _, exists := w.People[p.ID]; exists {
		panic("core: duplicate person ID " + p.ID)
	}
	w.People[p.ID] = p
	return p
}

// AddLocation registers a location. Panics on duplicate ID.
func (w *World) AddLocation(l *Location) *Location {
	if _, exists := w.Locations[l.ID]; exists {
		panic("core: duplicate location ID " + l.ID)
	}
	w.Locations[l.ID] = l
	return l
}

// LivingPeople returns all currently-alive people in deterministic order
// (sorted by ID). The deterministic order is required for reproducibility.
func (w *World) LivingPeople() []*Person {
	out := make([]*Person, 0, len(w.People))
	for _, p := range w.People {
		if p.Alive {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LivingPeopleAt returns the alive people at a given location, in
// deterministic order (sorted by ID). People with an empty
// LocationID are returned only when the empty string is passed.
func (w *World) LivingPeopleAt(locationID string) []*Person {
	out := make([]*Person, 0, len(w.People))
	for _, p := range w.People {
		if p.Alive && p.LocationID == locationID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RecomputeLocationPopulations walks all living people and updates
// each location's Population count. Call this before reading
// Location.Population in tests or snapshots.
func (w *World) RecomputeLocationPopulations() {
	// Reset
	ids := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w.Locations[id].Population = 0
	}
	// Tally
	for _, p := range w.LivingPeople() {
		if p.LocationID != "" {
			if loc, ok := w.Locations[p.LocationID]; ok {
				loc.Population++
			}
		}
	}
}
