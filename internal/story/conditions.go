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

// VariableGE is a Condition that returns true when
// WorldState.Variables[Key] is greater than or equal to Value.
// Phase 37.B YAML tag: VariableGE.
//
// VariableGE is the canonical "stat threshold" condition. It
// is used for DragonAffinity ≥ 30, Corruption ≥ 50, Faith ≥ 75,
// etc. Authors compose VariableGE with other Conditions via the
// Or/And combinators to express richer thresholds like
// "≥ 50 AND HasItem(\"key\")".
type VariableGE struct {
	Key   string
	Value int
}

// Check returns ws.Variables[Key] >= Value. A zero value (or
// missing key) reads as 0 (Go's zero value for int), so an
// author who writes `VariableGE{Key: "courage", Value: 1}`
// triggers once the variable is set to anything ≥ 1.
func (c VariableGE) Check(ws state.WorldState) bool {
	return ws.Variables[c.Key] >= c.Value
}

// RelationshipAxis identifies which sub-axis of a Relationship
// is referenced by RelationshipGE / ModifyRelationship. The
// three axes are §9 §10's Trust (≥ 50 unlocks backstory, ≤ -50
// unlocks betrayal), Affection (≥ 75 unlocks romance), and
// Respect (used by faction-specific approval events).
//
// Phase 37.B YAML tag: axis is encoded as the lowercase string
// "trust" / "affection" / "respect" — encoding via enum keeps
// the ABI stable across YAML schema versions.
type RelationshipAxis int

const (
	// AxisTrust is the trust axis of a Relationship.
	AxisTrust RelationshipAxis = iota
	// AxisAffection is the affection axis of a Relationship.
	AxisAffection
	// AxisRespect is the respect axis of a Relationship.
	AxisRespect
)

// String returns the lowercase YAML representation of the axis.
// Used by the content loader (Phase 36.E) for encoding the
// canonical form into content/companions/*.yaml.
func (a RelationshipAxis) String() string {
	switch a {
	case AxisTrust:
		return "trust"
	case AxisAffection:
		return "affection"
	case AxisRespect:
		return "respect"
	default:
		return "unknown"
	}
}

// ParseRelationshipAxis decodes a YAML axis string. Returns
// (AxisTrust, false) when the string is unrecognized — the
// loader surfaces a clear error in this case rather than mapping
// an unknown axis to a silent default.
func ParseRelationshipAxis(s string) (RelationshipAxis, bool) {
	switch s {
	case "trust":
		return AxisTrust, true
	case "affection":
		return AxisAffection, true
	case "respect":
		return AxisRespect, true
	}
	return AxisTrust, false
}

// RelationshipGE is a Condition that returns true when
// `WorldState.Relationships[Character].<Axis>` is greater than
// or equal to Value. Phase 37.B YAML tag: RelationshipGE.
//
// Sample author usage: RelationshipGE{Character: "elara",
// Axis: AxisTrust, Value: 50} reads as "Elara's trust ≥ 50"
// and gates the Elara backstory scene per §10.
type RelationshipGE struct {
	Character string
	Axis      RelationshipAxis
	Value     int
}

// Check extracts the named axis from Relationships[Character]
// and tests against Value. A missing entry returns the zero
// Relationship (all axes at 0), so a fresh save cannot
// accidentally satisfy a high-threshold RelationshipGE.
func (c RelationshipGE) Check(ws state.WorldState) bool {
	rel, ok := ws.Relationships[c.Character]
	if !ok {
		return false
	}
	var got int
	switch c.Axis {
	case AxisTrust:
		got = rel.Trust
	case AxisAffection:
		got = rel.Affection
	case AxisRespect:
		got = rel.Respect
	}
	return got >= c.Value
}

// HasItem is a Condition that returns true when
// WorldState.Inventory.Items[Key] has Count > 0.
// Phase 37.B YAML tag: HasItem.
//
// A stack at exactly 0 (or the key absent) does NOT satisfy
// HasItem. Authors use HasItem as a "do I physically have this
// in my pack?" gate.
type HasItem struct {
	Key string
}

// Check returns ws.Inventory.Items[Key] > 0.
func (c HasItem) Check(ws state.WorldState) bool {
	return ws.Inventory.Items[c.Key] > 0
}

// HasEnding is a Condition that returns true when ID is
// present in WorldState.EndingsUnlocked. Phase 37.B YAML tag:
// HasEnding.
//
// HasEnding is unusual among Conditions because it gates on
// history (has the player reached a previous ending in this
// playthrough?). It is preserved per §20's "some endings
// reference prior endings" specification.
type HasEnding struct {
	ID string
}

// Check returns true iff ws.EndingsUnlocked contains ID.
func (c HasEnding) Check(ws state.WorldState) bool {
	for _, e := range ws.EndingsUnlocked {
		if e == c.ID {
			return true
		}
	}
	return false
}

// Or is a combinator Condition that returns true when ANY of
// its inner Conditions return true. Phase 37.B YAML tag: Or.
//
// Or is the canonical "either/or" condition. Sample usage:
// `Or{Conditions: []Condition{VariableGE{Key:"trust_kael",
// Value:50}, HasItem{Key:"royal_seal"}}}` reads "either trust
// Kael ≥ 50 OR have the royal seal".
type Or struct {
	Conditions []Condition
}

// Check returns true iff at least one inner Condition returns
// true. An empty Conditions list returns false (no inner = no
// path to be true).
func (c Or) Check(ws state.WorldState) bool {
	for _, inner := range c.Conditions {
		if inner.Check(ws) {
			return true
		}
	}
	return false
}

// And is a combinator Condition that returns true when ALL of
// its inner Conditions return true. Phase 37.B YAML tag: And.
//
// And is mostly redundant with AvailableChoices' implicit AND
// across Choice.Conditions, but is useful when an author nests
// an AND inside an OR ("(A AND B) OR C").
type And struct {
	Conditions []Condition
}

// Check returns true iff every inner Condition returns true.
// An empty Conditions list returns true (vacuous truth).
func (c And) Check(ws state.WorldState) bool {
	for _, inner := range c.Conditions {
		if !inner.Check(ws) {
			return false
		}
	}
	return true
}

// Not is a combinator Condition that inverts Inner.
// Phase 37.B YAML tag: Not.
//
// Not is the canonical negation. Sample usage:
// `Not{Inner: Flag{Key:"rested"}}` reads "the player has not
// rested yet".
type Not struct {
	Inner Condition
}

// Check returns the negation of Inner.Check.
func (c Not) Check(ws state.WorldState) bool {
	return !c.Inner.Check(ws)
}
