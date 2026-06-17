package story

import "github.com/chronicle-dev/chronicle/internal/state"

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
// + SetFlag/ClearFlag concrete impls; the remaining impls land
// in Phase 36.B.
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
