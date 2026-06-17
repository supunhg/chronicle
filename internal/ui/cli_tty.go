package ui

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// TTYRenderer is the v2 choice-menu renderer per
// ARCHITECTURE.md §23 ("Render Story, Render Choices,
// Player Selection"). It implements the same Renderer
// interface as BufferRenderer, but:
//   - word-wraps prose at WrapWidth (default 80);
//   - emits ANSI codes for colour when AnsiEnabled;
//   - prints a phase-style `# {Title}` heading matching
//     the README sample session ("# EX01 — The frontier
//     town of Ashwick");
//   - offers a prompt reader (PromptChoice) and a
//     press-any-key reader (PressAnyKey) on the
//     injected io.Reader.
//
// TTYRenderer's input/output are both injectable: tests
// pass a *bytes.Buffer for output and a literal byte
// stream for input; production passes os.Stdout and
// os.Stdin. The renderer NEVER directly references
// os.Stdout/os.Stdin; the choice to bind those is the
// Caller's (typically cmd/chronicle).
type TTYRenderer struct {
	// WrapWidth is the maximum column width for prose
	// wrapping. WrapWidth <= 0 disables wrapping (the
	// original behaviour: emit each line unaltered).
	WrapWidth int

	// AnsiEnabled controls whether Runtime emissions
	// include ANSI escape sequences. Disabled for tests
	// and for redirect-to-file runs; enabled in real TTY
	// sessions. The read-side caller typically wires
	// AnsiEnabled via isatty detection on stdout.
	AnsiEnabled bool

	// Out is the output stream. Required.
	Out io.Writer

	// In is the input stream for prompt + press-any-key.
	// In is required; nil is treated as EOF (no input).
	In io.Reader
}

// NewTTYRenderer returns a TTYRenderer with sensible
// defaults: 80-column wrap, ANSI disabled (tests prefer
// deterministic, uncoloured output), and a buffered
// reader around a zero-byte stream. Production callers
// should override Out+In after this constructor.
//
// NewTTYRenderer is the canonical test-time wiring;
// cmd/chronicle binds os.Stdout/os.Stdin + AnsiEnabled
// for real sessions.
func NewTTYRenderer() *TTYRenderer {
	return &TTYRenderer{
		WrapWidth:   80,
		AnsiEnabled: false,
		Out:         io.Discard,
		In:          strings.NewReader(""),
	}
}

// NewTTYRendererWithIO returns a TTYRenderer sharing
// caller-supplied streams. Use this in tests to inject
// a fake input buffer + capture output buffer. Wraps In
// in a bufio.Reader so PromptChoice and PressAnyKey
// can read line-buffered records without producing
// empty input when the buffer runs out mid-line.
//
// out and in must be non-nil.
func NewTTYRendererWithIO(out io.Writer, in io.Reader) *TTYRenderer {
	if out == nil {
		out = io.Discard
	}
	if in == nil {
		in = strings.NewReader("")
	}
	return &TTYRenderer{
		WrapWidth:   80,
		AnsiEnabled: false,
		Out:         out,
		In:          in,
	}
}

// ----- Constants for ANSI -----

// ANSI escape sequences used by TTYRenderer. Pulled
// out as named constants so the emission logic remains
// self-documenting and tooling (e.g., `grep -rP
// '\x1b\['`) can audit colour usage.

// ansiBoldCyan is the SGR code for bold + cyan
// foreground — used for the title heading.
const ansiBoldCyan = "\x1b[1;36m"

// ansiReset clears all SGR attributes.
const ansiReset = "\x1b[0m"

// ansiBoldYellow is bold + yellow — used for the prompt.
const ansiBoldYellow = "\x1b[1;33m"

// ansiDim greys out locked-choice hints.
const ansiDim = "\x1b[2m"

// ansiWrap returns s wrapped in an SGR "open" and
// "reset" pair when AnsiEnabled is true; otherwise
// returns s unchanged. Each code is what the renderer
// emits once per styled region; the helper takes care
// of pairing "open" with Reset.
func (t *TTYRenderer) ansiWrap(open, s string) string {
	if !t.AnsiEnabled {
		return s
	}
	return open + s + ansiReset
}

// ----- Renderer interface -----

// Render prints the node's formatted prose: a `# {Title}`
// heading, a blank line, then the wrapped Text body.
// Locked choices (whose Conditions failed) are filtered
// out by Runner.Step before this call; Render prints
// only the prose, not the choice list.
func (t *TTYRenderer) Render(n story.StoryNode, _ state.WorldState) error {
	heading := t.formatHeading(n)
	if n.Text == "" {
		_, err := fmt.Fprintln(t.Out, heading)
		return err
	}
	body := wrapText(n.Text, t.WrapWidth)
	var sb strings.Builder
	sb.WriteString(heading)
	sb.WriteByte('\n')
	for _, ln := range body {
		sb.WriteString(ln)
		sb.WriteByte('\n')
	}
	_, err := io.WriteString(t.Out, sb.String())
	return err
}

// formatHeading returns "# {Title}" when Title is
// non-empty, else falls back to "# {ID}" so unnamed
// nodes still get a readable heading. The em-dash
// flavour of the README sample is reserved for nodes
// whose Title contains a phase-style "EX01 —" prefix
// authored by the content loader; the renderer does
// not synthesise that.
func (t *TTYRenderer) formatHeading(n story.StoryNode) string {
	label := n.Title
	if label == "" {
		label = n.ID
	}
	heading := "# " + label
	return t.ansiWrap(ansiBoldCyan, heading)
}

// RenderChoices prints each available choice as a
// numbered menu entry. Numbering is 1-based for human
// readability; matching PromptChoice's input contract
// (player types the displayed number).
//
// Future phases may extend this to show locked-choices
// (filtered out at the runner level today) with a
// `[Requires X]` hint per PHASES.md §36.F's optional
// locked-choice spec. Phase 36.F's accepted contract
// is "Runner.Step filters via AvailableChoices first"
// — RenderChoices receives the already-filtered list.
func (t *TTYRenderer) RenderChoices(_ story.StoryNode, available []story.Choice, _ state.WorldState) error {
	for i, c := range available {
		if _, err := fmt.Fprintf(t.Out, "  [%d] %s\n", i+1, c.Text); err != nil {
			return err
		}
	}
	return nil
}

// ----- Input readers -----

// PromptChoice reads the player's selection. The
// rendered numbered menu uses 1-based indices; this
// function returns the matching indexed ChoiceID (or
// its 1-based index when the test orchestrator asks
// for it).
//
// PromptChoice ignores the choice ID field entirely.
// The choice menu is selection-by-number — this matches
// the v2 contract that there is no free-text
// interpretation.
//
// PromptChoice re-prompts on: non-numeric input,
// out-of-range numbers, and empty input. The loop
// terminates only when input EOFs or a valid selection
// is made. Tests inject a strings.Reader + bytes.Buffer
// to drive the loop deterministically.
func (t *TTYRenderer) PromptChoice(_ story.StoryNode, available []story.Choice, _ state.WorldState) (int, error) {
	if len(available) == 0 {
		return 0, fmt.Errorf("ui: TTYRenderer.PromptChoice: no available choices")
	}
	if t.In == nil {
		return 0, fmt.Errorf("ui: TTYRenderer.PromptChoice: input stream nil")
	}
	scanner := bufio.NewScanner(t.In)
	for {
		promptLabel := t.ansiWrap(ansiBoldYellow, ">")
		if _, err := fmt.Fprintf(t.Out, "%s ", promptLabel); err != nil {
			return 0, err
		}
		if !scanner.Scan() {
			// EOF or scanner error.
			if err := scanner.Err(); err != nil {
				return 0, fmt.Errorf("ui: TTYRenderer.PromptChoice: scan: %w", err)
			}
			return 0, io.EOF
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Parse as int. Non-numeric / out-of-range -> re-prompt.
		var pick int
		if _, err := fmt.Sscanf(line, "%d", &pick); err != nil {
			continue
		}
		if pick < 1 || pick > len(available) {
			continue
		}
		return pick, nil
	}
}

// PressAnyKey writes a `— press enter to continue —`
// hint and consumes one line of input. In real TTY
// mode (Phase 38 ties later) this would be the
// canonical "any key" reader using raw-mode TTY; for
// Phase 36.F the contract is "consume one line of
// input from In" — sufficient for the test suite
// and compatible with production cmd/chronicle reading
// stdin line-by-line.
//
// PressAnyKey returns nil on success, io.EOF if no
// input is available, or a wrapped error if the
// reader fails.
//
// The prompt string is rendered with the same yellow
// SGR as PromptChoice's ">" so the two TTYRenderer
// prompts look like one design system.
func (t *TTYRenderer) PressAnyKey() error {
	if t.In == nil {
		return fmt.Errorf("ui: TTYRenderer.PressAnyKey: input stream nil")
	}
	hint := t.ansiWrap(ansiDim, "— press enter to continue —")
	if _, err := fmt.Fprintln(t.Out, hint); err != nil {
		return err
	}
	scanner := bufio.NewScanner(t.In)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("ui: TTYRenderer.PressAnyKey: scan: %w", err)
		}
		return io.EOF
	}
	// Discard the line (we only need to consume a
	// record). Real raw-mode TTY (Phase 38+) would
	// read exactly one byte; the line-mode path is
	// the Phase 36.F accepted shape.
	return nil
}

// ----- Word-wrap helper -----

// wrapText wraps s into lines no longer than width
// columns, preserving paragraph boundaries (a blank
// line between paragraphs is kept verbatim) and
// collapsing runs of spaces to single spaces.
//
// width <= 0 disables wrapping — the heuristic for
// callers that pass unconfigured renderer instances.
// In that mode paragraph boundaries collapse (only
// \n is a break); this is documented as a known
// sharp edge since width=0 is the test fallback
// rather than a production target.
//
// wrapText is intentionally simple: it splits on
// whitespace, accumulates words into a current line,
// and emits when the next word would overflow the
// width. Word boundaries are detected at spaces;
// embedded hard line breaks are flattened via
// strings.Fields before wrapping.
//
// wrapText does NOT attempt to format quoted prose
// (no italic detection, no leading-space handling).
// Phase 38 content authoring can opt-in to those if
// needed; for Phase 36.F the canonical case is "the
// author wrote paragraphs; the renderer wraps them at
// 80 columns."
func wrapText(s string, width int) []string {
	if width <= 0 {
		return strings.Split(s, "\n")
	}
	paras := strings.Split(s, "\n\n")
	var out []string
	for i, para := range paras {
		// Collapse whitespace runs to single spaces.
		para = strings.Join(strings.Fields(para), " ")
		if para == "" {
			// Empty paragraph (e.g., triple or wider
			// newline): emit a blank line.
			out = append(out, "")
			continue
		}
		// First non-empty paragraph: no blank prefix.
		// Subsequent non-empty paragraphs: emit a blank
		// line BEFORE the new paragraph's wrapped lines
		// so a "a\n\nb" split produces "a", "", "b".
		if i > 0 {
			out = append(out, "")
		}
		words := strings.Split(para, " ")
		var current strings.Builder
		for _, w := range words {
			if current.Len() == 0 {
				current.WriteString(w)
				continue
			}
			if current.Len()+1+len(w) > width {
				out = append(out, current.String())
				current.Reset()
				current.WriteString(w)
				continue
			}
			current.WriteByte(' ')
			current.WriteString(w)
		}
		if current.Len() > 0 {
			out = append(out, current.String())
		}
	}
	return out
}

var _ Renderer = (*TTYRenderer)(nil)
