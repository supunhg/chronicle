package events

import (
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// Trigger walks WorldState.TriggeredEvents in queue order and
// returns the NodeID of the first event whose Conditions all
// match. The queue is cleared on exit (whether a redirect was
// found, no match was found, or an unknown ID was silently
// skipped).
//
// Parameters
//   - ws: the live game state. Trigger reads ws.TriggeredEvents
//     and resets it to a zero-length slice on return. Other
//     state is read-only here; Condition implementations handle
//     their own reads.
//   - registry: the set of authored event definitions the
//     runner knows about. May be nil (yields no redirect; queue
//     still cleared). A future phase may pass a merged registry
//     that combines content/events.yaml with StoryNode.Events
//     from the current node.
//
// Returns
//   - redirectNodeID: the first matching event's NodeID, or ""
//     when no event matched. An event with empty NodeID is a
//     "pure sidebar" — it still clears the queue but produces
//     no redirect, since StoryNode.NodeID is empty.
//   - err: reserved for future internal errors; currently always
//     nil on the success path.
//
// Unknown event IDs in the queue are silently skipped — the
// content loader (Phase 36.E) is the front-line defense against
// authored-event typos (fail-fast on broken references); the
// runtime path stays permissive so a malformed save or external
// event stream does not block gameplay. Producers are expected
// to author against the loader's content/events.yaml; a stale
// queue ID is treated as a no-op rather than a hard failure.
//
// Duplicate IDs in `registry` resolve last-write-wins — the
// loader is the canonical duplicate-rejection gate; the runtime
// path stays defensive against content drift.
//
// Per the §13 deterministic-ordering contract, two events whose
// Conditions both pass are resolved by queue insertion order
// (i.e., the order their TriggerEvent effects were applied on
// the player's chosen Choice on this Step). The registry's own
// iteration order never affects the result.
func Trigger(ws *state.WorldState, registry []story.Event) (redirectNodeID string, err error) {
	if ws == nil {
		return "", nil
	}

	// Build the ID→Event lookup once. Empty/nil registry is a
	// valid input — every queue lookup misses, the queue is
	// still cleared, and Trigger returns ("", nil).
	byID := make(map[string]story.Event, len(registry))
	for _, e := range registry {
		// Last-write-wins on duplicate IDs. The loader is
		// expected to reject duplicates, but the runtime path
		// stays defensive against content drift.
		byID[e.ID] = e
	}

	// Snapshot the queue, then clear it BEFORE iterating. This
	// guarantees the queue is empty on return even if a future
	// Condition implementation re-entrantly triggers events
	// (defensive, though no current Condition does so).
	queued := ws.TriggeredEvents
	ws.TriggeredEvents = ws.TriggeredEvents[:0]

	for _, id := range queued {
		e, ok := byID[id]
		if !ok {
			continue
		}
		if !allConditionsMatch(e.Conditions, ws) {
			continue
		}
		// First matching event wins. Its NodeID may be ""
		// for a sidebar event — caller sees empty redirect
		// and keeps the chosen Choice.NextNodeID.
		return e.NodeID, nil
	}
	return "", nil
}

// allConditionsMatch returns true iff every Condition in cs
// evaluates to true against ws. An empty condition list
// vacuously matches (the event always fires when reached — the
// loader is responsible for ensuring events with no Conditions
// are intentional, not author typos).
//
// Conditions take state.WorldState by value (read-only); Trigger
// holds *ws so we dereference here. The ws==nil guard is in
// Trigger itself, so this helper is only called when ws is
// non-nil.
func allConditionsMatch(cs []story.Condition, ws *state.WorldState) bool {
	for _, c := range cs {
		if !c.Check(*ws) {
			return false
		}
	}
	return true
}
