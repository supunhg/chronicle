package engine

import (
	"fmt"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// Runner orchestrates a single round of the §23 Runtime Flow per
// ARCHITECTURE.md §23 and the spec from PHASES.md §36.A.
//
// Runner is a thin orchestrator. The actual game logic lives in:
//   - story.AvailableChoices (filtering),
//   - story.Effect.Apply (state mutation),
//   - endings.Evaluate (finale resolution),
//   - ui.Renderer.Render / RenderChoices (player display),
//   - ChoiceProvider.Select (player input).
//
// Runner.Step's job is to chain these in the §23 order and to
// surface errors verbatim when any step fails.
//
// NOTE: the Engine field is the package's identity and configuration
// holder (see engine.go). Internally, Runner delegates I/O through
// Engine so that tests can swap a BufferRenderer + scriptedProvider
// into a Runner without touching Runner's code.
type Runner struct {
	Graph  *story.Graph
	Engine *Engine
}

// Step runs one round of the §23 Runtime Flow per PHASES.md §36.A
// acceptance test ("smoke test runs a 3-node story to completion").
//
// Step's algorithm corresponds to §23 verbatim:
//
//	1. Load StoryNode at SaveGame.WorldState.CurrentNodeID.
//	2. Filter node.Choices via story.AvailableChoices (Conditions
//	   evaluated against WorldState).
//	3. Render the node via Engine.Renderer.Render.
//	4. Render the visible choices via Engine.Renderer.RenderChoices.
//	5. Ask Engine.ChoiceProvider.Select for the player's pick.
//	6. Apply each Effect of the chosen Choice in declaration order.
//	   First non-nil error stops Step (no rollback — Phase 36.A).
//	7. Update WorldState.CurrentNodeID to Choice.NextNodeID.
//	8. Increment WorldState.Tick (debug counter).
//	9. If the new node has IsFinal=true, evaluate Engine.Endings
//	   and append the recovered ending ID to EndingsUnlocked.
//	   OnFinale callback is invoked if non-nil.
//
// Step returns the updated SaveGame and the first error
// encountered (or nil on success).
func (r *Runner) Step(s state.SaveGame) (state.SaveGame, error) {
	if r.Graph == nil {
		return s, fmt.Errorf("engine: Runner.Graph is nil")
	}
	if r.Engine == nil {
		return s, fmt.Errorf("engine: Runner.Engine is nil")
	}
	if r.Engine.Renderer == nil {
		return s, fmt.Errorf("engine: Runner.Engine.Renderer is nil")
	}
	if r.Engine.ChoiceProvider == nil {
		return s, fmt.Errorf("engine: Runner.Engine.ChoiceProvider is nil")
	}

	node, err := r.Graph.Lookup(s.WorldState.CurrentNodeID)
	if err != nil {
		return s, fmt.Errorf("engine: load node %q: %w", s.WorldState.CurrentNodeID, err)
	}

	if err := r.Engine.Renderer.Render(node, s.WorldState); err != nil {
		return s, fmt.Errorf("engine: render node %q: %w", node.ID, err)
	}

	available := story.AvailableChoices(node, s.WorldState)
	if len(available) == 0 {
		return s, fmt.Errorf("engine: node %q has no available choices (all gated or empty)", node.ID)
	}

	if err := r.Engine.Renderer.RenderChoices(node, available, s.WorldState); err != nil {
		return s, fmt.Errorf("engine: render choices for %q: %w", node.ID, err)
	}

	chosen, err := r.Engine.ChoiceProvider.Select(node, available, s.WorldState)
	if err != nil {
		return s, fmt.Errorf("engine: select choice at %q: %w", node.ID, err)
	}

	for _, eff := range chosen.Effects {
		if err := eff.Apply(&s.WorldState); err != nil {
			return s, fmt.Errorf("engine: apply effect on choice %q: %w", chosen.ID, err)
		}
	}

	// TODO(phase-36.D): ws.TriggeredEvents (queued by TriggerEvent
	// effects in the loop above) is consumed-and-cleared by
	// internal/events (Phase 36.D). If 36.D's handler is absent
	// or forgets to clear, the queue would re-fire events across
	// Steps; the contract is documented on
	// state.WorldState.TriggeredEvents. 36.D must clear the slice
	// after firing matched events.

	s.WorldState.CurrentNodeID = chosen.NextNodeID
	s.WorldState.Tick++

	// If we just landed on a final node, surface an ending.
	if nextNode, lookupErr := r.Graph.Lookup(s.WorldState.CurrentNodeID); lookupErr == nil && nextNode.IsFinal {
		r.maybeSurfaceEndings(&s)
	}

	return s, nil
}

// maybeSurfaceEndings runs the Endings registry against the
// current WorldState and appends the highest-priority valid
// ending to WorldState.EndingsUnlocked. OnFinale is invoked
// if non-nil.
//
// Finale evaluation is gated on Engine.Endings being non-empty
// AND the next node being IsFinal. When the registry is empty,
// Step still lands on the finale node but no ending is recovered
// (Phase 38.E's TestAllEndingsReachable gate can detect this
// as a regression).
func (r *Runner) maybeSurfaceEndings(s *state.SaveGame) {
	if len(r.Engine.Endings) == 0 {
		return
	}
	if e, ok := endings.Evaluate(s.WorldState, r.Engine.Endings); ok {
		s.WorldState.EndingsUnlocked = append(s.WorldState.EndingsUnlocked, e.ID)
		if r.Engine.OnFinale != nil {
			r.Engine.OnFinale(e, s.WorldState)
		}
	}
}
