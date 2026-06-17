package story

import "github.com/chronicle-dev/chronicle/internal/state"

// Condition is the canonical v2 condition interface per
// ARCHITECTURE.md §7.
//
// A Condition decides whether a Choice is available at the
// current WorldState. Each concrete Condition implementation
// inspects its own slice of state (Flags, Variables, Reputation,
// Party, ...) and returns true/false.
//
// Phase 37's YAML schema (§37.B) maps each YAML condition tag
// 1-to-1 to a concrete Condition implementation: Flag,
// VariableGE, RelationshipGE, HasItem, HasEnding, etc. Phase
// 36.A lands the Condition interface + the Flag concrete
// impl so the smoke test exercises both the interface and
// the filtering logic. The remaining concrete impls land
// in Phase 36.B.
type Condition interface {
	// Check returns true when the WorldState satisfies the
	// condition. Check MUST be a pure function of ws (no
	// side effects, no I/O) so the engine can call it many
	// times during a single Step without state drift.
	Check(ws state.WorldState) bool
}

// AvailableChoices returns the Choice slice filtered to those
// whose Conditions all return true (logical AND).
//
// AvailableChoices preserves the declared order of n.Choices
// so the player sees choices in the order the author wrote
// them (an implicit authoring convention; the YAML schema
// sorts by declaration order too).
//
// Choices with no Conditions are always available.
//
// AvailableChoices is the only place where Conditions are
// evaluated at runtime. Engine wiring (Runner.Step, see
// internal/engine/runner.go) calls AvailableChoices once
// per Step and passes the filtered slice to the player's
// prompt.
func AvailableChoices(n StoryNode, ws state.WorldState) []Choice {
	out := make([]Choice, 0, len(n.Choices))
	for _, c := range n.Choices {
		ok := true
		for _, cond := range c.Conditions {
			if !cond.Check(ws) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// Flag is a Condition that returns true when WorldState.Flags[Key]
// is set. Phase 37.B YAML tag: Flag.
//
// Flag is a value type (no pointer receiver) so a Choice in
// StoryNode can be expressed as a struct literal in test code:
//
//	Conditions: []story.Condition{ story.Flag{Key: "began_journey"} }
type Flag struct {
	Key string
}

// Check returns ws.Flags[Key] (false if the map is nil or the
// key is absent — Go's zero value for bool is false).
func (f Flag) Check(ws state.WorldState) bool {
	return ws.Flags[f.Key]
}
