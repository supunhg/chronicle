package core

// Person is an individual in the simulation.
//
// Phase 2 fields per chronicle-spec.md §5.1–5.3:
//   - BirthTick / DeathTick for lifecycle
//   - Gender for PopulationEngine births
//   - LocationID for migration
//   - Class, Occupation for social modeling
//   - FatherID, MotherID, SpouseID for family trees
//   - Traits, Needs, Goals for the GoalEngine (stubs in Phase 2)
//
// Phase 19 added:
//   - Inventory: each NPC's carried items, keyed by canonical
//     lowercase item name. For non-merchants this is what they
//     happen to be carrying. For merchants it is their stock
//     (Buy decrements, Sell increments).
//   - IsMerchant: true if this NPC is a merchant. Buy/sell
//     handlers in the action engine look for a merchant at the
//     player's location; IsMerchant is the marker. Set by
//     worldpack.Bootstrap based on the occupation's
//     `is_merchant` flag.
//
// Age is derived: use Person.AgeAt(currentTick).
type Person struct {
	// Identity
	ID     string
	Name   string
	Gender string // "M" or "F"

	// Lifecycle
	BirthTick int64
	DeathTick int64 // 0 if alive
	Alive     bool

	// Social
	LocationID string
	Class      string // Lower, Middle, Upper
	Occupation string

	// Merchant (Phase 19). When IsMerchant is true, Inventory
	// serves as the merchant's stock: the action engine's
	// resolveBuy decrements the merchant's Count, and resolveSell
	// increments it. Non-merchants have IsMerchant=false and an
	// Inventory that is just whatever they're carrying (currently
	// a stub — no NPC self-consumes in Phase 19).
	IsMerchant bool

	// Family
	FatherID string
	MotherID string
	SpouseID string

	// Personality (GoalEngine input)
	Traits map[string]int // e.g. {"ambitious": 90, "kind": 40}

	// Dynamic needs (GoalEngine input)
	Needs map[string]int // e.g. {"hunger": 50, "wealth": 30}

	// Long-term goals (GoalEngine input)
	Goals []string // e.g. ["become_noble", "find_spouse"]

	// Inventory is the person's carried items, keyed by
	// canonical lowercase item name. Phase 19: every person
	// has their own inventory (not just the player). The
	// value is a full core.Item (Name, Count, Weight, Value,
	// MaxDurability) — the same struct used for the world's
	// item catalog and the player's inventory. The metadata
	// is copied from the catalog at acquisition time so a
	// switch back to a pre-buy world still preserves the
	// snapshot of the items' properties at the time they
	// were acquired. For merchants, this map doubles as
	// stock-on-hand: the action engine's resolveBuy/resolveSell
	// increment and decrement the Count directly.
	Inventory map[string]Item
}

// AgeAt returns the person's age in years at the given tick.
// Returns 0 if tick is before BirthTick.
func (p *Person) AgeAt(tick int64) int {
	if tick < p.BirthTick {
		return 0
	}
	return int((tick - p.BirthTick) / 365)
}

// IsAdult returns true if the person is at least 16 years old at tick.
func (p *Person) IsAdult(tick int64) bool {
	return p.AgeAt(tick) >= 16
}

// IsFertile returns true if the person is between 16 and 50 years old
// at tick. Phase 2 v1 gender rules: only females carry children.
func (p *Person) IsFertile(tick int64) bool {
	age := p.AgeAt(tick)
	return p.Gender == "F" && age >= 16 && age <= 50
}
