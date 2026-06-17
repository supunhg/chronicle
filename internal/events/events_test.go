package events

import (
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// helperEvents builds a non-nil WorldState with TriggeredEvents
// initialised — Trigger reads & clears the queue, so each test
// must set the queue explicitly. Reusing the production
// constructor keeps tests aligned with NewWorldState's
// empty-slice contract.
func helperEvents(initial []string) *state.WorldState {
	ws := state.NewWorldState()
	ws.TriggeredEvents = append([]string(nil), initial...)
	return &ws
}

// TestTrigger_HappyPath_FirstMatchInQueueOrderWins verifies
// the §13 deterministic-ordering contract: two queued events
// both pass; whichever was queued first wins, regardless of
// its registry position.
//
// Registry: [beta (NodeID:"BETA"), alpha (NodeID:"ALPHA")]
// Queue:    [alpha, beta]      <- alpha queued first
// Expect:   ALPHA (not BETA).
func TestTrigger_HappyPath_FirstMatchInQueueOrderWins(t *testing.T) {
	ws := helperEvents([]string{"alpha", "beta"})
	registry := []story.Event{
		{ID: "beta", NodeID: "BETA"},
		{ID: "alpha", NodeID: "ALPHA"},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got != "ALPHA" {
		t.Errorf("Trigger = %q, want ALPHA (first in queue)", got)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Errorf("TriggeredEvents after Trigger = %v, want empty", ws.TriggeredEvents)
	}
}

// TestTrigger_SkipsFailingCondition verifies a registered
// event whose Condition evaluates to false is silently
// skipped — no redirect produced, but the queue is still
// cleared.
//
// state.NewWorldState() initialises Flags as an empty map; a
// lookup of a missing key returns false, so the Flag condition
// here does NOT match. The test deliberately does NOT seed
// the flag (the previous version accidentally set it to true,
// making the test's polarity inverted).
func TestTrigger_SkipsFailingCondition(t *testing.T) {
	ws := helperEvents([]string{"never_matches"})
	registry := []story.Event{
		{
			ID:     "never_matches",
			NodeID: "SHOULD_NOT_REDIRECT",
			Conditions: []story.Condition{
				story.Flag{Key: "honored_invitation"},
			},
		},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got != "" {
		t.Errorf("Trigger = %q, want \"\" (failing condition)", got)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Errorf("TriggeredEvents after Trigger = %v, want empty", ws.TriggeredEvents)
	}
}

// TestTrigger_UnknownEventIDIsSilentlySkipped verifies the
// runtime-path-permissive-on-unknown contract: a queue ID
// not in the registry produces no redirect and no error.
// Loader (Phase 36.E) is the front-line gate for content bugs;
// Trigger stays defensive for save/load tampering and external
// stream corruption.
//
// We pair the unknown ID with a known-and-matching event so
// the "skip ahead to the next item" behaviour is observable.
func TestTrigger_UnknownEventIDIsSilentlySkipped(t *testing.T) {
	ws := helperEvents([]string{"not_in_registry", "alpha"})
	registry := []story.Event{
		{ID: "alpha", NodeID: "ALPHA"},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got != "ALPHA" {
		t.Errorf("Trigger = %q, want ALPHA (unknown ID skipped, known match won)", got)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Errorf("TriggeredEvents after Trigger = %v, want empty", ws.TriggeredEvents)
	}
}

// TestTrigger_NilRegistryIsSafe verifies Trigger handles a nil
// registry without panicking. Empty registry produces no
// redirect; queue still cleared.
func TestTrigger_NilRegistryIsSafe(t *testing.T) {
	ws := helperEvents([]string{"alpha", "beta"})
	got, err := Trigger(ws, nil)
	if err != nil {
		t.Fatalf("Trigger(nil registry): %v", err)
	}
	if got != "" {
		t.Errorf("Trigger = %q, want \"\" (nil registry = no match)", got)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Errorf("TriggeredEvents after Trigger = %v, want empty", ws.TriggeredEvents)
	}
}

// TestTrigger_NilWorldStateIsSafe verifies Trigger handles a
// nil WorldState without panicking. Reserved for defensive
// use; production code should never pass nil, but the
// "runtime path stays permissive on bad input" contract
// applies here.
func TestTrigger_NilWorldStateIsSafe(t *testing.T) {
	got, err := Trigger(nil, []story.Event{{ID: "alpha", NodeID: "ALPHA"}})
	if err != nil {
		t.Fatalf("Trigger(nil ws): %v", err)
	}
	if got != "" {
		t.Errorf("Trigger = %q, want \"\" (nil ws is no-op)", got)
	}
}

// TestTrigger_EmptyQueueIsNoop verifies running Trigger
// against an empty queue is idempotent: no error, no
// redirect, queue stays empty.
func TestTrigger_EmptyQueueIsNoop(t *testing.T) {
	ws := helperEvents(nil)
	registry := []story.Event{
		{ID: "alpha", NodeID: "ALPHA"},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger(empty queue): %v", err)
	}
	if got != "" {
		t.Errorf("Trigger = %q, want \"\" (empty queue)", got)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Errorf("TriggeredEvents after Trigger = %v, want empty", ws.TriggeredEvents)
	}
}

// TestTrigger_IdempotentRerunClearsEmptyQueue verifies the
// "stale queued ID never re-fires" contract by calling
// Trigger twice; the second call sees an empty queue and
// produces no redirect.
func TestTrigger_IdempotentRerunClearsEmptyQueue(t *testing.T) {
	ws := helperEvents([]string{"alpha"})
	registry := []story.Event{
		{ID: "alpha", NodeID: "ALPHA"},
	}

	first, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger #1: %v", err)
	}
	if first != "ALPHA" {
		t.Errorf("Trigger #1 = %q, want ALPHA", first)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Fatalf("Trigger #1 left queue non-empty: %v", ws.TriggeredEvents)
	}

	second, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger #2: %v", err)
	}
	if second != "" {
		t.Errorf("Trigger #2 = %q, want \"\" (queue already cleared)", second)
	}
}

// TestTrigger_AllConditionsMustPass verifies that a Condition
// failing on ANY element of the list blocks the event. This
// covers the "two conditions, one passes, one fails" pattern
// — the §13 contract is "all Conditions must be true".
func TestTrigger_AllConditionsMustPass(t *testing.T) {
	ws := helperEvents([]string{"ambush"})
	ws.Flags["first_signal"] = true        // passes
	// ws.Flags does not have "second_signal"   -> fails
	registry := []story.Event{
		{
			ID:     "ambush",
			NodeID: "AMBUSH",
			Conditions: []story.Condition{
				story.Flag{Key: "first_signal"},
				story.Flag{Key: "second_signal"},
			},
		},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got != "" {
		t.Errorf("Trigger = %q, want \"\" (one condition failed)", got)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Errorf("TriggeredEvents after Trigger = %v, want empty", ws.TriggeredEvents)
	}
}

// TestTrigger_QueueOrderWinsOverRegistryPosition is the
// "two matching events in reversed queue order" case. The
// event queued FIRST wins, even if its registry entry comes
// AFTER the second matching event. This nails down the
// "queue order is canonical" rule and protects against a
// future regression that introduces "sort registry by ID".
func TestTrigger_QueueOrderWinsOverRegistryPosition(t *testing.T) {
	ws := helperEvents([]string{"z_event", "a_event"})
	registry := []story.Event{
		{ID: "z_event", NodeID: "Z"},
		{ID: "a_event", NodeID: "A"},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got != "Z" {
		t.Errorf("Trigger = %q, want Z (first in queue order \"z_event\", \"a_event\")", got)
	}
}

// TestTrigger_SidebarEventEmptyNodeID_HasNoRedirect verifies
// a "pure sidebar" event — Conditions match, NodeID is "" —
// clears the queue but returns "". Step interprets "" as
// "no redirect", falling back to Choice.NextNodeID.
//
// Trigger's snapshot-then-clear pattern clears the ENTIRE
// queue up front (not just up to the first match). The
// "first match wins" rule determines only which RedirectNodeID
// is returned; later queued IDs are skipped for redirect but
// already removed from the queue.
func TestTrigger_SidebarEventEmptyNodeID_HasNoRedirect(t *testing.T) {
	ws := helperEvents([]string{"sidebar", "alpha"})
	registry := []story.Event{
		{ID: "sidebar", NodeID: ""}, // pure sidebar, no redirect
		{ID: "alpha", NodeID: "ALPHA"},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got != "" {
		t.Errorf("Trigger = %q, want \"\" (sidebar event has no NodeID)", got)
	}
	if len(ws.TriggeredEvents) != 0 {
		t.Errorf("TriggeredEvents after Trigger = %v, want empty (snapshot-then-clear drained queue)", ws.TriggeredEvents)
	}
}

// TestTrigger_DuplicateRegistryIDs_LastWins is a defensive
// test against content drift: if the registry has two events
// with the same ID, the later entry wins. The loader is
// expected to reject duplicates; the runtime path stays
// permissive. We assert the contract here so a future test
// harness can rely on it.
func TestTrigger_DuplicateRegistryIDs_LastWins(t *testing.T) {
	ws := helperEvents([]string{"shared"})
	registry := []story.Event{
		{ID: "shared", NodeID: "FIRST_DEFINITION"},
		{ID: "shared", NodeID: "LAST_DEFINITION"},
	}
	got, err := Trigger(ws, registry)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got != "LAST_DEFINITION" {
		t.Errorf("Trigger = %q, want LAST_DEFINITION (last-write-wins on duplicate registry IDs)", got)
	}
}
