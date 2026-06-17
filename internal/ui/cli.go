package ui

import (
	"fmt"
	"io"

	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// Renderer is the canonical v2 UI rendering interface.
//
// Runner.Step calls Render before prompting the player so they
// read the node's prose, then reads the choices, then types a
// selection. Real-world implementations wire Render to a TTY
// terminal (Phase 36.F cli.go) and PromptChoice to stdin.
// Test implementations use BufferRenderer (this file) for
// prose-and-choices output and engine.NewScripted() for the
// selection step (see internal/engine/engine.go).
type Renderer interface {
	// Render prints the StoryNode's prose (Text) to the player's
	// view. The Title, when non-empty, heads the rendering.
	// Conditions are NOT shown here — choice conditions render
	// inline with the choice list (per §7 "Locked choices may
	// optionally display: [Requires Dragon Affinity 30]").
	// Locked choices are filtered out by Runner.Step before
	// RenderChoices runs.
	Render(node story.StoryNode, ws state.WorldState) error

	// RenderChoices prints the available choices as a numbered
	// menu — `[1] Text`, `[2] Text`, etc. — using the player's
	// view. Hidden choices (those whose Conditions failed) are
	// NOT passed to RenderChoices — Runner.Step filters via
	// story.AvailableChoices first.
	RenderChoices(node story.StoryNode, available []story.Choice, ws state.WorldState) error
}

// BufferRenderer writes Render/RenderChoices outputs to an
// in-memory io.Writer. Used by tests to capture stdout-style
// rendering without a real TTY.
//
// BufferRenderer does NOT implement the "select a choice"
// step — that is engine.Runner.Engine.ChoiceProvider's job
// (see internal/engine/engine.go's ChoiceProvider interface
// and NewScripted factory).
type BufferRenderer struct {
	W io.Writer
}

// NewBufferRenderer returns a BufferRenderer that writes to w.
// w is typically a *bytes.Buffer in tests and a *os.File in
// integration scenarios (the CLI wires a real TTY later).
func NewBufferRenderer(w io.Writer) *BufferRenderer {
	return &BufferRenderer{W: w}
}

// Render writes the StoryNode's title (when non-empty) and
// the Text to w. Node ID is included bracketed for test
// assertions (so the renderer output is easy to grep in
// integration tests).
func (b *BufferRenderer) Render(n story.StoryNode, _ state.WorldState) error {
	_, err := fmt.Fprintf(b.W, "[%s] %s\n%s\n", n.ID, n.Title, n.Text)
	return err
}

// RenderChoices writes each available choice as a numbered
// menu entry. Numbering is 1-based for human readability;
// NextNodeID is shown in parens for test assertions.
func (b *BufferRenderer) RenderChoices(_ story.StoryNode, available []story.Choice, _ state.WorldState) error {
	for i, c := range available {
		if _, err := fmt.Fprintf(b.W, "  [%d] %s (→ %s)\n", i+1, c.Text, c.NextNodeID); err != nil {
			return err
		}
	}
	return nil
}
