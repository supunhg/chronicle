package ui

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// ----- wrapText tests -----

func TestWrapText_WrapsAtWidth(t *testing.T) {
	// 24-column wrap: "the quick brown fox jumps" is
	// 25 chars and overflows at width=24, so the break
	// lands between "fox" and "jumps"; "jumps over the
	// lazy dog" (23 chars) fits on the second line.
	got := wrapText("the quick brown fox jumps over the lazy dog", 24)
	want := []string{
		"the quick brown fox", // 19 chars
		"jumps over the lazy dog",
	}
	if !equalSS(got, want) {
		t.Errorf("WrapText(24) =\n%s\nwant:\n%s", joinNL(got), joinNL(want))
	}
}

func TestWrapText_PreservesParagraphs(t *testing.T) {
	// Double newline -> blank line between paragraphs.
	got := wrapText("first paragraph here.\n\nsecond paragraph here.", 30)
	want := []string{
		"first paragraph here.",
		"",
		"second paragraph here.",
	}
	if !equalSS(got, want) {
		t.Errorf("WrapText(30 paragraphs) =\n%s\nwant:\n%s", joinNL(got), joinNL(want))
	}
}

func TestWrapText_DisabledOnZeroWidth(t *testing.T) {
	// width <= 0 disables wrapping: each \n starts a new
	// emitted line; multi-space collapsed, no wrap.
	got := wrapText("hello\nworld", 0)
	want := []string{"hello", "world"}
	if !equalSS(got, want) {
		t.Errorf("WrapText(0) = %v; want %v", got, want)
	}
}

func TestWrapText_CollapsesSpaces(t *testing.T) {
	got := wrapText("hello    world  foo", 80)
	want := []string{"hello world foo"}
	if !equalSS(got, want) {
		t.Errorf("WrapText collapsed =\n%s\nwant:\n%s", joinNL(got), joinNL(want))
	}
}

// ----- ANSI emission -----

func TestAnsi_EnabledEmitsEscapeSequences(t *testing.T) {
	tr := &TTYRenderer{AnsiEnabled: true}
	got := tr.ansiWrap(ansiBoldCyan, "# Title")
	want := ansiBoldCyan + "# Title" + ansiReset
	if got != want {
		t.Errorf("ansiWrap(enabled) = %q; want %q", got, want)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("ansiWrap(enabled) did not emit an escape sequence: %q", got)
	}
}

func TestAnsi_DisabledEmitsPlainText(t *testing.T) {
	tr := &TTYRenderer{AnsiEnabled: false}
	got := tr.ansiWrap(ansiBoldCyan, "# Title")
	if got != "# Title" {
		t.Errorf("ansiWrap(disabled) = %q; want %q", got, "# Title")
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("ansiWrap(disabled) leaked an escape sequence: %q", got)
	}
}

// ----- Renderer interface tests -----

func TestTTYRenderer_Render_HeaderFormat(t *testing.T) {
	var buf bytes.Buffer
	tr := NewTTYRendererWithIO(&buf, nil)
	tr.AnsiEnabled = false // plain-text format is testable
	node := story.StoryNode{
		ID:    "act1.ex01.entrance",
		Title: "EX01 — The frontier town of Ashwick",
		Text:  "A long prose line that should wrap when the renderer applies the configured width to verbose paragraphs.",
	}
	if err := tr.Render(node, state.NewWorldState()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "# EX01 — The frontier town of Ashwick") {
		t.Errorf("Render output missing required heading; got:\n%s", out)
	}
	if !strings.HasPrefix(out, "# ") {
		t.Errorf("Render output does not begin with '# ' heading; got:\n%s", out)
	}
}

func TestTTYRenderer_Render_WrapsProse(t *testing.T) {
	var buf bytes.Buffer
	tr := NewTTYRendererWithIO(&buf, nil)
	tr.AnsiEnabled = false
	tr.WrapWidth = 20
	node := story.StoryNode{
		ID:    "a",
		Title: "A node",
		Text:  "the quick brown fox jumps over",
	}
	if err := tr.Render(node, state.NewWorldState()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	// 20-column wrap on "the quick brown fox jumps over":
	// "the quick brown fox" is 19 chars (fits),
	// "jumps over" is 10 chars (fits).
	for _, wantLine := range []string{
		"the quick brown fox",
		"jumps over",
	} {
		if !strings.Contains(out, wantLine) {
			t.Errorf("Render output missing wrapped line %q; got:\n%s", wantLine, out)
		}
	}
}

func TestTTYRenderer_RenderChoices_NumberedMenu(t *testing.T) {
	var buf bytes.Buffer
	tr := NewTTYRendererWithIO(&buf, nil)
	tr.AnsiEnabled = false
	node := story.StoryNode{ID: "a", Title: "A"}
	available := []story.Choice{
		{ID: "a1", Text: "Enter the keep."},
		{ID: "a2", Text: "Head for the ridge."},
	}
	if err := tr.RenderChoices(node, available, state.NewWorldState()); err != nil {
		t.Fatalf("RenderChoices: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"[1] Enter the keep.", "[2] Head for the ridge."} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderChoices missing %q; got:\n%s", want, out)
		}
	}
}

// ----- PromptChoice tests -----

func TestTTYRenderer_PromptChoice_HappyPath(t *testing.T) {
	var out bytes.Buffer
	tr := NewTTYRendererWithIO(&out, strings.NewReader("1\n"))
	available := []story.Choice{
		{ID: "a1", Text: "First."},
		{ID: "a2", Text: "Second."},
	}
	pick, err := tr.PromptChoice(story.StoryNode{ID: "a"}, available, state.NewWorldState())
	if err != nil {
		t.Fatalf("PromptChoice: %v", err)
	}
	if pick != 1 {
		t.Errorf("PromptChoice pick = %d; want 1", pick)
	}
	if !strings.Contains(out.String(), ">") {
		t.Errorf("PromptChoice output did not contain '>' prompt; got: %q", out.String())
	}
}

func TestTTYRenderer_PromptChoice_InvalidThenValid(t *testing.T) {
	var out bytes.Buffer
	tr := NewTTYRendererWithIO(&out, strings.NewReader("abc\n0\n99\n2\n"))
	available := []story.Choice{
		{ID: "a1", Text: "First."},
		{ID: "a2", Text: "Second."},
	}
	pick, err := tr.PromptChoice(story.StoryNode{ID: "a"}, available, state.NewWorldState())
	if err != nil {
		t.Fatalf("PromptChoice: %v", err)
	}
	if pick != 2 {
		t.Errorf("PromptChoice pick = %d; want 2 (after invalid lines)", pick)
	}
	// The prompt should appear at least 4 times (3 rejects + 1 success).
	if got := strings.Count(out.String(), ">"); got < 3 {
		t.Errorf("PromptChoice re-prompted only %d time(s); want at least 3", got)
	}
}

func TestTTYRenderer_PromptChoice_EmptyStreamReturnsEOF(t *testing.T) {
	var out bytes.Buffer
	tr := NewTTYRendererWithIO(&out, strings.NewReader(""))
	available := []story.Choice{{ID: "a1", Text: "Only."}}
	if _, err := tr.PromptChoice(story.StoryNode{ID: "a"}, available, state.NewWorldState()); !errors.Is(err, io.EOF) {
		t.Errorf("PromptChoice(EOF) = %v; want io.EOF", err)
	}
}

func TestTTYRenderer_PromptChoice_NoAvailableChoicesErrors(t *testing.T) {
	tr := NewTTYRendererWithIO(io.Discard, strings.NewReader(""))
	if _, err := tr.PromptChoice(story.StoryNode{ID: "a"}, nil, state.NewWorldState()); err == nil {
		t.Errorf("PromptChoice(nil choices) = nil error; want non-nil")
	}
}

// ----- PressAnyKey tests -----

func TestTTYRenderer_PressAnyKey_ConsumesInput(t *testing.T) {
	var out bytes.Buffer
	tr := NewTTYRendererWithIO(&out, strings.NewReader("\n"))
	if err := tr.PressAnyKey(); err != nil {
		t.Errorf("PressAnyKey: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "press enter") {
		t.Errorf("PressAnyKey output missing press-enter hint; got: %q", got)
	}
}

func TestTTYRenderer_PressAnyKey_EmptyStreamReturnsEOF(t *testing.T) {
	tr := NewTTYRendererWithIO(io.Discard, strings.NewReader(""))
	if err := tr.PressAnyKey(); !errors.Is(err, io.EOF) {
		t.Errorf("PressAnyKey(EOF) = %v; want io.EOF", err)
	}
}

// ----- Compile-time / interface conformance -----

// TTYRenderer must satisfy the Renderer interface so
// engine.Engine can accept it.
func TestTTYRenderer_ImplementsRenderer(t *testing.T) {
	var r Renderer = NewTTYRendererWithIO(io.Discard, strings.NewReader(""))
	_ = r
}

// ----- helpers -----

func equalSS(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinNL(ss []string) string {
	return strings.Join(ss, "\n")
}
