// Package events implements the v2 event trigger handler per
// ARCHITECTURE.md §13.
//
// Events are queued by the TriggerEvent Effect
// (internal/story/effects.go), which appends the event ID to
// WorldState.TriggeredEvents. The Trigger function walks the
// queue, finds the first queued event whose Conditions all
// evaluate to true, and returns that event's NodeID as a
// redirect candidate — Runner.Step interprets a non-empty
// redirect by overriding the chosen Choice.NextNodeID.
//
// Determinism contract:
//   - Queue order = the order TriggerEvent effects were applied
//     (Step applies Choice.Effects in declaration order, so
//     each TriggerEvent append lands in deterministic position).
//   - First matching event in queue order wins; later entries
//     are not considered once a redirect is produced.
//   - The queue is always cleared after the walk, even when no
//     event matched — so a stale queued ID never re-fires across
//     subsequent Steps.
//
// Phase 36.D lands this event-fire consumer. Future phases:
//   - Phase 38 adds authored node-level events (StoryNode.Events),
//     which is the same Event struct referenced through the
//     registry parameter below. The signature is registry-driven
//     (not node-driven) so the same Trigger handles both
//     world-level (TriggeredEvents queue) and node-level
//     (StoryNode.Events) sources transparently — the runner
//     merges both into a single registry.
//   - Phase 36.E wires the registry from content/events.yaml.
package events
