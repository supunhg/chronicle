package engine

import (
	"fmt"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
	"github.com/chronicle-dev/chronicle/internal/ui"
)

// Engine is the engine package's identity type. It owns the
// pluggable I/O sub-systems a Runner needs to play one round
// of the §23 Runtime Flow:
//
//   - Renderer (ui.Renderer) — how prose + choices reach the player.
//   - ChoiceProvider (ChoiceProvider) — how the player's input is read.
//   - Endings (registry of all valid Endings) — evaluated at finale.
//
// Engine does NOT hold any game state (the SaveGame does, see
// Runner.Step's parameter). Engine is purely configuration +
// collaboration context.
type Engine struct {
	// Renderer is required. Runner.Step calls Renderer.Render
	// before the player's prompt and Renderer.RenderChoices at
	// the prompt.
	Renderer ui.Renderer

	// ChoiceProvider is required. Runner.Step delegates the
	// "which choice did the player pick?" question to it.
	ChoiceProvider ChoiceProvider

	// Endings is the registry of valid endings. Step evaluates
	// this list when the chosen Choice.NextNodeID lands on a
	// StoryNode with IsFinal=true. Empty list disables ending
	// recovery (Step still lands on the finale node but does
	// not surface an ending).
	Endings []endings.Ending

	// OnFinale is an optional side-effect invoked when Step
	// reaches a final node AND finds a valid ending.
	// Production wiring uses this to display the recovered
	// ending to the player (Phase 36.F ui/cli.go will integrate
	// it). Tests may leave it nil.
	OnFinale func(e endings.Ending, ws state.WorldState)
}

// ChoiceProvider abstracts "how does the runner read the player's
// choice from input?". Real-world implementations read from
// stdin (TTY — Phase 36.F); test implementations return a
// pre-supplied sequence via NewScripted.
//
// The interface intentionally takes story.StoryNode + available
// + ws so the provider can implement context-aware prompts
// (e.g., "would you like to proceed? (yes/no)").
type ChoiceProvider interface {
	// Select returns the player's choice from the available
	// list. Implementation note: implementations should
	// validate that the chosen Choice is in `available` and
	// return an error if not — Step trusts the return value
	// to be one of `available`, so a misbehaving provider
	// could cause Condition-filtered choices to be selected.
	Select(node story.StoryNode, available []story.Choice, ws state.WorldState) (story.Choice, error)
}

// scriptedProvider is the canonical test-time ChoiceProvider.
// Its responses list is consumed in order: the Nth call returns
// responses[N]. When responses is exhausted, scriptedProvider
// returns ErrNoScriptedChoice.
type scriptedProvider struct {
	responses []story.Choice
	i         int
}

// ErrNoScriptedChoice is returned by scriptedProvider when its
// responses list is exhausted. Tests that exhaust the list
// fail with this error rather than silently looping.
var ErrNoScriptedChoice = fmt.Errorf("engine: scripted ChoiceProvider has no more scripted responses")

// Select returns the next scripted ID mapped to the corresponding
// available Choice from the node. The scripted response's
// NextNodeID/Effects are ignored — Step reads them from the
// authoritative Choice in `available` (the one bound to the
// node's content, not the test's scripted "selector").
//
// If the scripted ID is not in the available list, scriptedProvider
// returns an error so tests catch misrouted scripted-input setups.
// If the responses list is exhausted, scriptedProvider returns
// ErrNoScriptedChoice.
func (s *scriptedProvider) Select(_ story.StoryNode, available []story.Choice, _ state.WorldState) (story.Choice, error) {
	if s.i >= len(s.responses) {
		return story.Choice{}, ErrNoScriptedChoice
	}
	scriptedID := s.responses[s.i].ID
	s.i++

	for _, a := range available {
		if a.ID == scriptedID {
			return a, nil
		}
	}
	return story.Choice{}, fmt.Errorf("engine: scripted ChoiceProvider: scripted choice %q not in available list [%d choices]", scriptedID, len(available))
}

// NewScripted returns a ChoiceProvider that supplies the
// supplied choices in declaration order. Used by tests; production
// code wires a stdin-reading provider (Phase 36.F).
//
// The returned ChoiceProvider fails fast when responses is
// exhausted — tests will see ErrNoScriptedChoice rather than
// silently looping. This is the canonical test-time wiring.
func NewScripted(responses []story.Choice) ChoiceProvider {
	return &scriptedProvider{responses: responses}
}
