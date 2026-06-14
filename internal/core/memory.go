package core

// Memory is a causally anchored record of a past event that an NPC
// remembers, per chronicle-spec.md §5.6.
//
// Causal anchoring means: every Memory points to the event that
// caused it (EventID) and, for cascading events, the prior cause
// (CauseEventID). This lets NPCs reason about blame, gratitude, and
// pattern recognition across generations.
//
// The TrustDelta and RelationshipDelta fields bake the effect of
// this memory into the relationship score at write time, so future
// relationship queries are O(1) instead of O(history). See
// chronicle-spec.md §5.2 for the "memory-driven deltas" pattern.
type Memory struct {
	// ID is a unique identifier for this memory record.
	ID string

	// OwnerID is the Person who remembers this event.
	OwnerID string

	// EventID is the event that occurred (what happened).
	// May be empty for memories that aren't tied to a specific event
	// (e.g. ambient observations).
	EventID string

	// CauseEventID links to the prior cause in a causal chain.
	// Empty for root events. Chains longer than 10 generations are
	// summarized in the Memory Engine (see spec §15.6).
	CauseEventID string

	// Tick is the sim tick when the event happened.
	Tick int64

	// Importance is 0..1, derived from event severity. Memories
	// with Importance > 0.7 persist indefinitely; others decay
	// (see spec §5.6 "Decay").
	Importance float64

	// Recency decays over time (1.0 = just happened, 0.0 = ancient).
	// Recency is recomputed by the Memory Engine on read; the
	// stored value is a snapshot at write time.
	Recency float64

	// EmotionalScore is 0..1; higher = more emotionally charged.
	// Used by the Goal Engine when scoring memory-influenced
	// actions.
	EmotionalScore float64

	// TrustDelta is the change in trust this event caused in the
	// target (or owner, if no specific target). Baked into the
	// relationship score on write.
	TrustDelta float64

	// RelationshipDelta is a general relationship-axis delta
	// (spread across the 5 axes). For axis-specific effects, use
	// TrustDelta (the trust axis) plus a future per-axis delta
	// column.
	RelationshipDelta float64

	// Description is a human-readable summary of the event.
	// May be empty (e.g. for ambient observations).
	Description string

	// Tags are short strings for filtering/grouping (e.g. "fire",
	// "wedding", "theft"). Used by the deterministic memory
	// summarizer in the Memory Engine.
	Tags []string
}
