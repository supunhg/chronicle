package state

// WorldState is the canonical v2 game state per ARCHITECTURE.md §4.
//
// WorldState is the entire game state. Every field is part of the
// canonical save format (§18 SaveGame). v2 has no runtime-only
// state — every WorldState field persists across save/load
// round-trips without loss (§18A invariant #1).
type WorldState struct {
	// Tick is the game-step counter. Bumped once per successful
	// Step iteration. v2 does not use Tick as a clock; it is a
	// debug counter for save-load regression tests.
	Tick int

	// Protagonist is the chosen CharacterProfile.Name from
	// content/protagonists/*.yaml (Kael, Lyra, Raven, Aria).
	// Set at game start; never changes.
	Protagonist string

	// CurrentNodeID is the StoryNode.ID the player is currently
	// at. Step reads this to look up the current node in the
	// StoryGraph (engine/runner.go).
	CurrentNodeID string

	// Flags is the major-decisions map. Examples: "JoinedDragons",
	// "SavedKing", "RomancedElara". Flags are permanent.
	Flags map[string]bool

	// Variables is the continuous-state map. Examples:
	// "DragonAffinity", "Corruption", "Courage". Each range is
	// normally 0-100.
	Variables map[string]int

	// Relationships is the per-companion trust/affection/respect
	// map keyed by companion ID. See Relationship (§9).
	Relationships map[string]Relationship

	// Reputation is the per-faction standing. See
	// ReputationState (§11).
	Reputation ReputationState

	// Inventory is the player's carried items. See Inventory (§12).
	Inventory Inventory

	// Party is the list of companion IDs currently travelling
	// with the protagonist.
	Party []string

	// EndingsUnlocked is the list of Ending.ID values the player
	// has reached so far. A playthrough normally appends one
	// entry at the finale (Step appends on transition into a
	// final node when an ending evaluates).
	EndingsUnlocked []string

	// TriggeredEvents is the per-Step queue of event IDs queued
	// by TriggerEvent effects. Step applies choice Effects in
	// order; events triggered by those effects accumulate here.
	// Phase 36.D's event handler reads this queue, fires matching
	// events, and clears it before the next Step.
	//
	// Phase 36.B declares this field for TriggerEvent's effect
	// queue management; Phase 36.D adds the handler that
	// consumes the queue.
	TriggeredEvents []string
}

// Relationship is the per-companion trust/affection/respect
// triple per §9. Range: -100 to +100 per axis.
//
// Three separate axes (per README "Three romance targets" —
// each romance unlocks at a different threshold combination),
// not a single aggregate score.
type Relationship struct {
	Trust     int
	Affection int
	Respect   int
}

// ReputationState is the per-faction reputation per §11.
// Range: -100 to +100 per faction.
//
// Four factions are canonical (Kingdom, Mages, Dragons,
// Underworld). Adding a fifth requires a content-authoring
// change (content/companions/*.yaml + content/endings.yaml)
// and the loader emits a fresh Reputation struct keyed to
// the new name.
type ReputationState struct {
	Kingdom    int
	Mages      int
	Dragons    int
	Underworld int
}

// Inventory is the per-item count map per §12. Items are story
// tools — no durability or encumbrance in v2.
//
// The map is keyed by item canonical name; the value is the
// stack count. Negative counts are forbidden by the action
// engine (AddItem < 0 is rejected; RemoveItem > count clamps
// to count in Phase 36.B's inventory manager).
type Inventory struct {
	Items map[string]int
}

// NewWorldState returns a WorldState with every collection
// initialised to its empty form. The caller fills in
// CurrentNodeID (from the protagonist's opening node ID)
// and Protagonist (from the chosen CharacterProfile name).
//
// NewWorldState is the canonical constructor; tests and
// production code SHOULD use it rather than relying on zero
// values for maps (zero-value maps are unsafe for writes —
// see "Effective Go" §Maps).
func NewWorldState() WorldState {
	return WorldState{
		Flags:           make(map[string]bool),
		Variables:       make(map[string]int),
		Relationships:   make(map[string]Relationship),
		Inventory:       Inventory{Items: make(map[string]int)},
		Party:           []string{},
		EndingsUnlocked: []string{},
		TriggeredEvents: []string{},
	}
}
