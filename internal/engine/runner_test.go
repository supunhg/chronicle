package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
	"github.com/chronicle-dev/chronicle/internal/ui"
)

// TestRunner_3NodeWalkthrough is the canonical Phase 36.A
// acceptance test from PHASES.md:
//
//	"internal/engine compiles; smoke test runs a 3-node story to
//	 completion."
//
// Graph shape:
//
//	start (a) --[Choice a1]--> mid (b) --[Choice b1]--> finale (c).
//
// Steps:
//
//	Step 1: a → b.  Verifies CurrentNodeID, Effect `began_journey` set.
//	Step 2: b → c.  Verifies CurrentNodeID, Effect `mid_completed` set,
//	         Engine.Endings evaluated, OnFinale fired with `hero`
//	         ending, EndingsUnlocked contains "hero".
func TestRunner_3NodeWalkthrough(t *testing.T) {
	g := story.NewGraph()
	if err := g.Add(story.StoryNode{
		ID: "a", Title: "Opening", Text: "Opening prose.",
		Choices: []story.Choice{
			{
				ID: "a1", Text: "Go inside.", NextNodeID: "b",
				Effects: []story.Effect{
					story.SetFlag{Key: "began_journey"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Add a: %v", err)
	}
	if err := g.Add(story.StoryNode{
		ID: "b", Title: "Mid", Text: "Middle prose.",
		Choices: []story.Choice{
			{
				ID: "b1", Text: "Continue.", NextNodeID: "c",
				Effects: []story.Effect{
					story.SetFlag{Key: "mid_completed"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Add b: %v", err)
	}
	if err := g.Add(story.StoryNode{
		ID: "c", Title: "Finale", Text: "Final prose.",
		IsFinal: true,
	}); err != nil {
		t.Fatalf("Add c: %v", err)
	}

	var finaleSeen endings.Ending
	var finaleNotified bool
	runner := &Runner{
		Graph: g,
		Engine: &Engine{
			Renderer: ui.NewBufferRenderer(&bytes.Buffer{}),
			ChoiceProvider: NewScripted([]story.Choice{
				{ID: "a1", NextNodeID: "b"},
				{ID: "b1", NextNodeID: "c"},
			}),
			Endings: []endings.Ending{
				{
					ID: "hero",
					// Priority 1 > Priority 0 fallback (below) so a
					// regression that drops the Flag condition only
					// fails the next test, not this one.
					Priority: 1,
					Conditions: []story.Condition{
						story.Flag{Key: "mid_completed"},
					},
				},
				// Priority-0 fallback exercises the §19 "default"
				// pattern (no Conditions ⇒ always-valid). This is
				// not asserted in TestRunner_3NodeWalkthrough but is
				// explicitly tested in TestRunner_DefaultEndingFallback.
				{ID: "fallback", Priority: 0},
			},
			OnFinale: func(e endings.Ending, _ state.WorldState) {
				finaleSeen = e
				finaleNotified = true
			},
		},
	}

	ws := state.NewWorldState()
	ws.CurrentNodeID = "a"
	s := state.SaveGame{Version: 0, WorldState: ws}

	// Step 1: a → b.
	s1, err := runner.Step(s)
	if err != nil {
		t.Fatalf("Step 1 (a→b): %v", err)
	}
	if s1.WorldState.CurrentNodeID != "b" {
		t.Fatalf("after Step 1: CurrentNodeID = %q, want b", s1.WorldState.CurrentNodeID)
	}
	if !s1.WorldState.Flags["began_journey"] {
		t.Errorf("after Step 1: flag began_journey = false, want true")
	}
	if finaleNotified {
		t.Errorf("after Step 1: OnFinale fired (non-final node); not wanted")
	}

	// Step 2: b → c (finale).
	s2, err := runner.Step(s1)
	if err != nil {
		t.Fatalf("Step 2 (b→c): %v", err)
	}
	if s2.WorldState.CurrentNodeID != "c" {
		t.Fatalf("after Step 2: CurrentNodeID = %q, want c", s2.WorldState.CurrentNodeID)
	}
	if !s2.WorldState.Flags["mid_completed"] {
		t.Errorf("after Step 2: flag mid_completed = false, want true")
	}
	if !finaleNotified {
		t.Errorf("after Step 2: OnFinale did not fire (finale reached); wanted hero ending")
	}
	if finaleSeen.ID != "hero" {
		t.Errorf("after Step 2: OnFinale saw %q, want hero", finaleSeen.ID)
	}
	if len(s2.WorldState.EndingsUnlocked) != 1 || s2.WorldState.EndingsUnlocked[0] != "hero" {
		t.Errorf("after Step 2: EndingsUnlocked = %v, want [hero]", s2.WorldState.EndingsUnlocked)
	}

	// Step 3 at finale: we expect an error (no available choices).
	if _, err := runner.Step(s2); err == nil {
		t.Fatalf("Step 3 at finale: expected error (no available choices), got nil")
	} else if !strings.Contains(err.Error(), "no available choices") {
		t.Errorf("Step 3 at finale: err = %q, want message containing 'no available choices'", err.Error())
	}
}

// TestRunner_AvailableChoiceFiltering verifies that a Choice
// gated behind a failing Condition is filtered out by
// story.AvailableChoices — the player sees only choices whose
// Conditions are valid against the current WorldState.
//
// This is the gate logic protected by Phase 36.A's Story
// interface; Phase 36.B will add more concrete Condition
// implementations (VariableGE, RelationshipGE, ...) but
// AvailableChoices itself depends only on the interface.
func TestRunner_AvailableChoiceFiltering(t *testing.T) {
	g := story.NewGraph()
	if err := g.Add(story.StoryNode{
		ID: "gate", Title: "Gate", Text: "Gate text.",
		Choices: []story.Choice{
			{
				ID: "open", Text: "Open the gate.",
				NextNodeID: "after",
			},
			{
				ID:        "locked",
				Text:      "Try the locked door.",
				NextNodeID: "should_not_appear",
				Conditions: []story.Condition{
					story.Flag{Key: "key_in_inventory"},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(story.StoryNode{ID: "after", Text: "After."}); err != nil {
		t.Fatal(err)
	}

	var recorded [][]string // each Step's available Choice.IDs
	recorder := recordProvider(func(avail []story.Choice) story.Choice {
		ids := make([]string, len(avail))
		for i, c := range avail {
			ids[i] = c.ID
		}
		recorded = append(recorded, ids)
		return avail[0]
	})

	runner := &Runner{
		Graph: g,
		Engine: &Engine{
			Renderer:       ui.NewBufferRenderer(&bytes.Buffer{}),
			ChoiceProvider: recorder,
		},
	}

	ws := state.NewWorldState()
	ws.CurrentNodeID = "gate"
	s := state.SaveGame{Version: 0, WorldState: ws}

	s1, err := runner.Step(s)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if len(recorded) != 1 || len(recorded[0]) != 1 || recorded[0][0] != "open" {
		t.Errorf("recorded = %v, want [[open]] (locked choice should be filtered)", recorded)
	}
	if s1.WorldState.CurrentNodeID != "after" {
		t.Errorf("Step: CurrentNodeID = %q, want after", s1.WorldState.CurrentNodeID)
	}
}

// recordProvider is the test-time ChoiceProvider that records
// the Choice.IDs slice offered per call. The closure-based design
// keeps the helper local to runner_test.go without leaking
// internal engine.go API surface.
type recordProvider func(avail []story.Choice) story.Choice

func (r recordProvider) Select(_ story.StoryNode, available []story.Choice, _ state.WorldState) (story.Choice, error) {
	return r(available), nil
}

// TestRunner_EventRedirectOverridesNextNodeID verifies the
// Phase 36.D wiring: a Choice whose Effects queue a
// TriggerEvent takes the player to the EVENT's NodeID, not
// the Choice's NextNodeID. The queue is cleared after the
// Step; the TriggerEvent effect that queued the redirect
// itself is observable via WorldState.TriggeredEvents being
// empty in the post-Step state.
//
// Graph shape:
//
//	a (1 choice) --[Choice a1 (TriggerEvent "ally_call")]--> b
//	                  but Event "ally_call" redirects to "c"
//
// Step a → c (NOT b). Tick reflects the Step. TriggeredEvents
// is empty post-Step.
func TestRunner_EventRedirectOverridesNextNodeID(t *testing.T) {
	g := story.NewGraph()
	if err := g.Add(story.StoryNode{
		ID: "a", Text: "Hero needs an ally.",
		Choices: []story.Choice{
			{
				ID: "a1", Text: "Call for an ally.",
				NextNodeID: "b", // would normally land on b
				Effects: []story.Effect{
					story.TriggerEvent{ID: "ally_call"},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(story.StoryNode{ID: "b", Text: "Should NOT land here."}); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(story.StoryNode{ID: "c", Text: "Ally arrives."}); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{
		Graph: g,
		Engine: &Engine{
			Renderer: ui.NewBufferRenderer(&bytes.Buffer{}),
			ChoiceProvider: NewScripted([]story.Choice{
				{ID: "a1"}, // NextNodeID ignored by scriptedProvider
			}),
			Events: []story.Event{
				// "ally_call" redirects to c, overriding a1->b.
				{ID: "ally_call", NodeID: "c"},
			},
		},
	}
	ws := state.NewWorldState()
	ws.CurrentNodeID = "a"
	s := state.SaveGame{Version: 0, WorldState: ws}

	s2, err := runner.Step(s)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if s2.WorldState.CurrentNodeID != "c" {
		t.Fatalf("after Step: CurrentNodeID = %q, want c (event redirect)", s2.WorldState.CurrentNodeID)
	}
	if len(s2.WorldState.TriggeredEvents) != 0 {
		t.Errorf("after Step: TriggeredEvents = %v, want empty (queue cleared)", s2.WorldState.TriggeredEvents)
	}
	if s2.WorldState.Tick != 1 {
		t.Errorf("after Step: Tick = %d, want 1", s2.WorldState.Tick)
	}
}

// TestRunner_NoEventMatch_FallsBackToNextNodeID verifies the
// "TriggerEvent with no matching registry entry is silently
// skipped, queue still cleared, Choice.NextNodeID is used"
// path. The queue contains an ID the registry doesn't know
// about; Trigger returns ("", nil); Runner uses
// chosen.NextNodeID as normal.
func TestRunner_NoEventMatch_FallsBackToNextNodeID(t *testing.T) {
	g := story.NewGraph()
	if err := g.Add(story.StoryNode{
		ID: "a", Text: "Quiet node.",
		Choices: []story.Choice{
			{
				ID: "a1", Text: "Continue.",
				NextNodeID: "b",
				Effects: []story.Effect{
					story.TriggerEvent{ID: "no_such_event"},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(story.StoryNode{ID: "b", Text: "Continue."}); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{
		Graph: g,
		Engine: &Engine{
			Renderer:       ui.NewBufferRenderer(&bytes.Buffer{}),
			ChoiceProvider: NewScripted([]story.Choice{{ID: "a1"}}),
			// Empty registry: unknown event ID is silently
			// skipped by Phase 36.D's events.Trigger.
			Events: nil,
		},
	}
	ws := state.NewWorldState()
	ws.CurrentNodeID = "a"
	s := state.SaveGame{Version: 0, WorldState: ws}

	s2, err := runner.Step(s)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if s2.WorldState.CurrentNodeID != "b" {
		t.Fatalf("after Step: CurrentNodeID = %q, want b (no event match, Choice.NextNodeID used)", s2.WorldState.CurrentNodeID)
	}
	if len(s2.WorldState.TriggeredEvents) != 0 {
		t.Errorf("after Step: TriggeredEvents = %v, want empty (queue cleared)", s2.WorldState.TriggeredEvents)
	}
}

// TestRunner_DefaultEndingFallback verifies the §19 "no
// Conditions" implicit-default pattern: an Ending with empty
// Conditions is always valid and wins when no higher-priority
// matching Ending is present.
//
// Graph shape: start (a) --[Choice a1]--> finale (b).
// Step lands on b; the ending registry has a Priority-1 hero
// whose Condition is unsatisfiable + a Priority-0 fallback
// with no Conditions — fallback wins by §19's "always valid"
// rule.
func TestRunner_DefaultEndingFallback(t *testing.T) {
	g := story.NewGraph()
	if err := g.Add(story.StoryNode{
		ID: "a", Text: "Start.",
		Choices: []story.Choice{
			{ID: "a1", Text: "End.", NextNodeID: "b"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(story.StoryNode{ID: "b", Text: "Final.", IsFinal: true}); err != nil {
		t.Fatal(err)
	}

	var seen endings.Ending
	var seenNotified bool
	runner := &Runner{
		Graph: g,
		Engine: &Engine{
			Renderer:       ui.NewBufferRenderer(&bytes.Buffer{}),
			ChoiceProvider: NewScripted([]story.Choice{{ID: "a1"}}),
			Endings: []endings.Ending{
				{
					ID: "hero", Priority: 1,
					Conditions: []story.Condition{
						story.Flag{Key: "never_set_in_this_test"},
					},
				},
				{ID: "fallback", Priority: 0}, // no Conditions = always valid
			},
			OnFinale: func(e endings.Ending, _ state.WorldState) {
				seen = e
				seenNotified = true
			},
		},
	}
	ws := state.NewWorldState()
	ws.CurrentNodeID = "a"
	s := state.SaveGame{Version: 0, WorldState: ws}

	s2, err := runner.Step(s)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if s2.WorldState.CurrentNodeID != "b" {
		t.Fatalf("after Step: CurrentNodeID = %q, want b (finale)", s2.WorldState.CurrentNodeID)
	}
	if !seenNotified {
		t.Fatalf("OnFinale did not fire on landing at finale")
	}
	if seen.ID != "fallback" {
		t.Errorf("OnFinale saw %q, want fallback", seen.ID)
	}
	if len(s2.WorldState.EndingsUnlocked) != 1 || s2.WorldState.EndingsUnlocked[0] != "fallback" {
		t.Errorf("EndingsUnlocked = %v, want [fallback]", s2.WorldState.EndingsUnlocked)
	}
}
