package story

// StoryNode is the canonical v2 authored-content node per
// ARCHITECTURE.md §5.
//
// A StoryNode represents one "screen" of the game: a title, a
// piece of authored prose (Text), and a list of Choices the
// player can pick from. Nodes with IsFinal=true represent
// terminal nodes where endings are evaluated (§19); their
// Choices list is expected to be empty.
//
// StoryNode is pure data: the engine applies no procedural
// substitution to Text. The prose on screen is exactly what the
// author wrote in content/acts/act*.yaml (no LLM, no template
// substitution, no procedural change per §1 + §2 non-goals).
type StoryNode struct {
	// ID is the unique identifier for this node
	// (e.g. "act1.dragon_temple_entrance").
	ID string

	// Title is the short heading shown above Text
	// (optional display; empty is OK).
	Title string

	// Text is the authored prose, printed verbatim by
	// Renderer.Render. No LLM, no procedural change.
	Text string

	// Choices is the list of player-selectable options at
	// this node. Runner.Step filters to Available(node, ws)
	// before displaying; see conditions.go.
	Choices []Choice

	// IsFinal marks this as a terminal node. Final nodes
	// have no Choices; Step reaches them via Choice.NextNodeID
	// and ends there (ending evaluated by Step on landing).
	IsFinal bool

	// Events is the list of authored Events at this node.
	// Phase 36.A declares the Events field but Step does not
	// process it yet (event triggering is Phase 36.D's job
	// — see internal/events/doc.go).
	Events []Event
}

// Choice is the canonical v2 choice per ARCHITECTURE.md §6,
// declared in this file alongside StoryNode so callers can
// reference both via internal/story.
type Choice struct {
	// ID is the unique identifier for this choice
	// (e.g. "act1.dragon_temple.inside").
	ID string

	// Text is the prose the player sees when this choice is
	// displayed ("Enter the temple.", "Return to town.", ...).
	Text string

	// Conditions gates choice availability. If any Condition
	// returns false, the choice is filtered out by
	// AvailableChoices. Empty Conditions means always available.
	Conditions []Condition

	// Effects is the list of mutations applied to WorldState
	// in declaration order when this choice is selected.
	Effects []Effect

	// NextNodeID is the StoryNode.ID the engine loads after
	// applying this choice's Effects. Loader (Phase 36.E) is
	// fail-fast on broken NextNodeID references.
	NextNodeID string
}

// Event is the canonical v2 authored event per §13. Phase 36.A
// declares the type so StoryNode.Events compiles; the trigger
// logic (Conditions evaluation, NodeID redirect, deterministic
// ordering) lands in Phase 36.D's internal/events/events.go.
//
// At Phase 36.D, internal/events will define its Trigger function
// taking ([]story.Event, state.WorldState) → (string, bool) so
// Runner.Step can call it as a single line. Until then, Engine
// users leave Events empty.
type Event struct {
	// ID is the canonical event identifier.
	ID string

	// NodeID is the redirect target ID when this event triggers.
	// When empty, an event has no redirect effect (it is a
	// pure sidebar — Phase 36.D's design refinement).
	NodeID string

	// Conditions, if non-empty, gate event triggering. If any
	// Condition returns false, the event does not fire. If empty,
	// the event always fires (when its parent node is loaded).
	//
	// Defined as the local Condition interface (story.Condition
	// in this package) to keep StoryNode.Events loadable without
	// an internal/events<->internal/story import cycle.
	Conditions []Condition
}
