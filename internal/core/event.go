package core

// EventType is the canonical identifier of a v1 state-driven
// event. Phase 23 ships exactly 4 event types — see the spec
// for the rationale ("extremely small Event Engine"). New
// event types can be added in future phases without breaking
// existing worlds (consumers match by the EventType string).
type EventType string

// The 4 v1 state-driven events. Each rule in the EventEngine
// (see internal/simulation/event.go) emits one of these.
//
//	FamineRisk        - Settlement food stock below threshold
//	TheftWave         - Aggregate hunger high AND wealth low
//	CouncilScandal    - town_council faction influence high
//	RevivalMovement   - faith_of_dawn faction influence high
//
// Adding more is a v2 concern: more types = more complexity =
// harder to balance. The v1 set covers the four most
// narrative-rich signals without overcomplicating the engine.
const (
	EventFamineRisk      EventType = "famine_risk"
	EventTheftWave       EventType = "theft_wave"
	EventCouncilScandal  EventType = "council_scandal"
	EventRevivalMovement EventType = "revival_movement"
)

// Event is a state-driven event emitted by the EventEngine
// when a rule fires. Events are immutable once appended to
// w.Events. Engines (Phase 24+) consume them on later ticks
// to react (e.g., a MemoryEngine could create a
// "theft_wave_observed" memory for each resident).
//
// Phase 23 design rule: events are EMIT, never APPLIED. The
// engine does not mutate world state in response to an event
// firing. This preserves determinism and makes replay
// debugging tractable: the event log is the audit trail.
//
// `Location` is the per-event location scope. Empty string
// means "global" (e.g., a TheftWave that affects the whole
// world, not a single settlement).
//
// `Payload` is a free-form bag for rule-specific data (e.g.,
// the current food stock for a FamineRisk event, or the
// average hunger/wealth for a TheftWave event). Consumers
// should treat the keys as rule-specific and not assume a
// stable schema across rules.
type Event struct {
	// ID is a deterministic ID combining the rule, location,
	// and tick. Used for dedup and for the event log audit
	// trail.
	ID string

	// Type identifies the rule that emitted the event. See
	// the EventType constants.
	Type EventType

	// Tick is the simulation tick at which the event was
	// emitted. Stamped by EventEngine.Tick from w.Tick.
	Tick int64

	// Location is the per-event location scope, or "" for
	// global. Rules that observe per-settlement state
	// (FamineRisk) set this to the settlement ID; rules that
	// observe world-wide state (TheftWave) leave it empty.
	Location string

	// Payload is rule-specific data. See the per-rule
	// documentation in internal/simulation/event.go for the
	// expected keys.
	Payload map[string]any
}
