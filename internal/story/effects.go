package story

import (
	"fmt"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// Effect is the canonical v2 effect interface per ARCHITECTURE.md §8.
//
// An Effect mutates the WorldState. Each concrete Effect inspects
// its own slice of state (Flags, Variables, Reputation, ...) and
// applies a single mutation.
//
// Step applies each Effect on the chosen Choice one by one in
// declaration order. The first non-nil error stops Step and is
// returned to the caller. Phase 36.A does NOT roll back earlier
// Effects on later-effect failure; Phase 36.B+ may add a
// transactional snapshot/restore layer for multi-step choices.
//
// Phase 37's YAML schema (§37.B) maps each YAML effect tag 1-to-1
// to a concrete Effect implementation: SetFlag, ClearFlag,
// ModifyVariable, ModifyRelationship, ModifyReputation, AddItem,
// RemoveItem, TriggerEvent. Phase 36.A lands the Effect interface
// + SetFlag/ClearFlag concrete impls; Phase 36.B lands the rest.
type Effect interface {
	// Apply mutates *ws and returns nil on success.
	// Apply MUST NOT acquire locks or perform I/O; the engine
	// calls Apply inside critical sections and a slow Apply
	// would block the whole game.
	Apply(ws *state.WorldState) error
}

// SetFlag is an Effect that sets WorldState.Flags[Key] to true.
// Phase 37.B YAML tag: SetFlag.
//
// SetFlag initialises the Flags map if nil so it is safe to
// call against a zero WorldState (defensive; Step's SaveGame
// normally has Flags non-nil via state.NewWorldState()).
type SetFlag struct {
	Key string
}

// Apply sets ws.Flags[Key] = true.
func (e SetFlag) Apply(ws *state.WorldState) error {
	if ws.Flags == nil {
		ws.Flags = make(map[string]bool)
	}
	ws.Flags[e.Key] = true
	return nil
}

// ClearFlag is an Effect that removes WorldState.Flags[Key].
// Phase 37.B YAML tag: ClearFlag.
//
// ClearFlag deletes the key entirely (rather than setting it
// to false) so the Flags map stays minimal and downstream
// readers don't have to distinguish explicit-false from missing.
// Per Go's map semantics, reading a missing key returns the
// zero value (false), which is what SetFlag's "set true" pair
// wants.
type ClearFlag struct {
	Key string
}

// Apply deletes ws.Flags[Key].
func (e ClearFlag) Apply(ws *state.WorldState) error {
	if ws.Flags != nil {
		delete(ws.Flags, e.Key)
	}
	return nil
}

// ModifyVariable is an Effect that sets WorldState.Variables[Key]
// to Value (absolute, not delta). Phase 37.B YAML tag:
// ModifyVariable.
//
// Authors use ModifyVariable to land the player at a specific
// stat value ("Courage = 90" after a brave act) rather than
// chasing them through delta arithmetic. To achieve deltas, the
// author reads the current value from save state and writes
// back Variables[Key] = current+delta — but Phase 36.B's API is
// absolute so authoring is unambiguous.
//
// ModifyVariable does NOT clamp; variables have no canonical
// range. Authors who want a "0-100" cap on a stat implement it
// via two effects: read current, write clamped = current+delta.
type ModifyVariable struct {
	Key   string
	Value int
}

// Apply sets ws.Variables[Key] = Value.
func (e ModifyVariable) Apply(ws *state.WorldState) error {
	if ws.Variables == nil {
		ws.Variables = make(map[string]int)
	}
	ws.Variables[e.Key] = e.Value
	return nil
}

// ModifyRelationship is an Effect that sets
// WorldState.Relationships[Character].<Axis> to Value.
// Phase 37.B YAML tag: ModifyRelationship.
//
// Value is clamped to [-100, 100] per §9's axis range. Authors
// can rely on ModifyRelationship never producing out-of-range
// scores, even when authoring with generous numbers.
type ModifyRelationship struct {
	Character string
	Axis      RelationshipAxis
	Value     int
}

// Apply sets the named axis on Relationships[Character] to
// clamped Value. Relationships[Character] is initialised if
// absent.
func (e ModifyRelationship) Apply(ws *state.WorldState) error {
	if ws.Relationships == nil {
		ws.Relationships = make(map[string]state.Relationship)
	}
	rel := ws.Relationships[e.Character]
	val := clampAxisValue(e.Value)
	switch e.Axis {
	case AxisTrust:
		rel.Trust = val
	case AxisAffection:
		rel.Affection = val
	case AxisRespect:
		rel.Respect = val
	}
	ws.Relationships[e.Character] = rel
	return nil
}

// clampAxisValue restricts v to [-100, 100] per §9/§11's
// documented axis range. Shared between ModifyRelationship and
// ModifyReputation to preserve the range contract.
func clampAxisValue(v int) int {
	if v < -100 {
		return -100
	}
	if v > 100 {
		return 100
	}
	return v
}

// Faction identifies which faction's reputation a ModifyReputation
// effect adjusts. The four factions are §11: Kingdom, Mages,
// Dragons, Underworld.
type Faction int

const (
	// FactionKingdom is the standing-crown faction.
	FactionKingdom Faction = iota
	// FactionMages is the Arcanum/magic-guild faction.
	FactionMages
	// FactionDragons is the old-powers faction.
	FactionDragons
	// FactionUnderworld is the syndicate faction.
	FactionUnderworld
)

// String returns the lowercase YAML representation of the
// faction. Used by the content loader (Phase 36.E) for the
// canonical form in content/companions/*.yaml.
func (f Faction) String() string {
	switch f {
	case FactionKingdom:
		return "kingdom"
	case FactionMages:
		return "mages"
	case FactionDragons:
		return "dragons"
	case FactionUnderworld:
		return "underworld"
	default:
		return "unknown"
	}
}

// ParseFaction decodes a YAML faction string. Returns
// (FactionKingdom, false) when the string is unrecognized —
// the loader surfaces a clear error rather than silently mapping
// an unknown faction to a default.
func ParseFaction(s string) (Faction, bool) {
	switch s {
	case "kingdom":
		return FactionKingdom, true
	case "mages":
		return FactionMages, true
	case "dragons":
		return FactionDragons, true
	case "underworld":
		return FactionUnderworld, true
	}
	return FactionKingdom, false
}

// ModifyReputation is an Effect that sets ReputationState's
// named faction to Value (absolute, clamped). Phase 37.B YAML
// tag: ModifyReputation.
//
// ModifyReputation shares clampAxisValue behaviour with
// ModifyRelationship to preserve the §11 -100..+100 contract.
type ModifyReputation struct {
	Faction Faction
	Value   int
}

// Apply sets ws.Reputation.<Faction> to clamped Value.
func (e ModifyReputation) Apply(ws *state.WorldState) error {
	val := clampAxisValue(e.Value)
	switch e.Faction {
	case FactionKingdom:
		ws.Reputation.Kingdom = val
	case FactionMages:
		ws.Reputation.Mages = val
	case FactionDragons:
		ws.Reputation.Dragons = val
	case FactionUnderworld:
		ws.Reputation.Underworld = val
	}
	return nil
}

// AddItem is an Effect that increments
// WorldState.Inventory.Items[Key] by Count. Phase 37.B YAML
// tag: AddItem.
//
// Count must be non-negative; otherwise Apply returns an error
// so authoring mistakes are loud, not silent.
type AddItem struct {
	Key   string
	Count int
}

// Apply adds Count to Inventory.Items[Key].
func (e AddItem) Apply(ws *state.WorldState) error {
	if e.Count < 0 {
		return fmt.Errorf("story: AddItem(%q, %d): count must be non-negative", e.Key, e.Count)
	}
	if ws.Inventory.Items == nil {
		ws.Inventory.Items = make(map[string]int)
	}
	ws.Inventory.Items[e.Key] += e.Count
	return nil
}

// RemoveItem is an Effect that decrements
// WorldState.Inventory.Items[Key] by Count, clamping at 0
// (deleting the key entirely when count drops to 0).
// Phase 37.B YAML tag: RemoveItem.
//
// Count must be non-negative. Apply also allows over-removal
// (when Count > Items[Key], the key is deleted rather than
// going negative — so a player with 1 sword who loses 2 ends
// up with no sword entry, never -1).
type RemoveItem struct {
	Key   string
	Count int
}

// Apply decrements Inventory.Items[Key] by Count; clamps at 0.
func (e RemoveItem) Apply(ws *state.WorldState) error {
	if e.Count < 0 {
		return fmt.Errorf("story: RemoveItem(%q, %d): count must be non-negative", e.Key, e.Count)
	}
	if ws.Inventory.Items == nil {
		return nil // nothing to remove
	}
	have := ws.Inventory.Items[e.Key]
	if e.Count >= have {
		delete(ws.Inventory.Items, e.Key)
		return nil
	}
	ws.Inventory.Items[e.Key] = have - e.Count
	return nil
}

// TriggerEvent is an Effect that queues the named event ID
// for the engine's event trigger handler (Phase 36.D).
// Phase 37.B YAML tag: TriggerEvent.
//
// Phase 36.B appends the ID to WorldState.TriggeredEvents;
// Phase 36.D's internal/events reads and clears that queue at
// the engine's §23 "Check Events" step.
//
// Phase 36.B does not yet run the trigger handler; tests of
// TriggerEvent verify queue-mutation only. Phase 36.D adds
// the actual event-firing semantics.
type TriggerEvent struct {
	ID string
}

// Apply appends ID to ws.TriggeredEvents.
func (e TriggerEvent) Apply(ws *state.WorldState) error {
	ws.TriggeredEvents = append(ws.TriggeredEvents, e.ID)
	return nil
}
