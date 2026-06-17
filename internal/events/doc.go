// Package events triggers authored events when their conditions match.
//
// Phase 36.D will land events.go containing:
//
//   - Event struct (ID, Conditions, NodeID) per §13.
//   - Trigger(world *state.WorldState, currentNode story.StoryNode,
//              nodeEvents []Event) story.StoryNode — evaluates node
//              events first, then world-level events, returning the
//              StoryNode to redirect to (or currentNode unchanged if
//              no event triggered).
//
// Events fire automatically when the engine reaches the §23
// "Check Events" step (Phase 36.A's runner.go). The first event
// whose Conditions all evaluate to true wins; ties are broken by
// deterministic ID order.
//
// Event triggering is deterministic: an Event with passing
// Conditions always fires in the same order across runs. Multiple
// events that fire on the same tick are processed in ID order;
// only the first redirect wins.
package events
