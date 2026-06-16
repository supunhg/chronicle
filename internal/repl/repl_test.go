package repl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/action"
	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
	"github.com/chronicle-dev/chronicle/internal/llm"
	"github.com/chronicle-dev/chronicle/internal/narrator"
)

// mockLLM is a hand-rolled fake of the narrator's
// LLMClient interface. Returns a canned response (or
// error) from Chat, with a record of how many times it
// was called. Used to assert the Narrator is wired into
// execTalk/execTravel and that the LLM is called.
type mockLLM struct {
	response string
	err      error
	calls    int
}

func (m *mockLLM) Chat(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	m.calls++
	return m.response, m.err
}

// mockTick records how many times it was called and
// increments the world's tick counter (mirroring the
// real *tick.Simulation behavior). Returns the
// configured error if any.
type mockTick struct {
	calls   int
	wantErr error
}

func (m *mockTick) tick(w *core.World) func() error {
	return func() error {
		m.calls++
		if m.wantErr != nil {
			return m.wantErr
		}
		w.Tick++
		return nil
	}
}

// newTestWorld builds a small world with 2 people and 2
// locations for predictable output assertions.
func newTestWorld() *core.World {
	w := core.NewWorld("test", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "ashford", Name: "Ashford", PopulationCap: 50})
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", Alive: true, Gender: "F", BirthTick: -20 * 365, LocationID: "blackwater"})
	w.AddPerson(&core.Person{ID: "bob", Name: "Bob", Alive: true, Gender: "M", BirthTick: -30 * 365, LocationID: "ashford"})
	w.PlayerID = "alice"
	w.Tick = 100
	w.RecomputeLocationPopulations()
	return w
}

// runREPL is a test helper that constructs a REPL with
// the given input and options, runs it, and returns the
// captured output. It uses a no-op tick fn by default;
// tests that need tick tracking pass their own.
func runREPL(t *testing.T, w *core.World, input string, opts Options) string {
	t.Helper()
	if opts.In == nil {
		opts.In = strings.NewReader(input)
	}
	var out bytes.Buffer
	if opts.Out == nil {
		opts.Out = &out
	}
	parser := intent.New(nil, w)
	r := New(w, parser, opts)
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String()
}

// TestREPL_Quit verifies that "quit" exits the REPL.
func TestREPL_Quit(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "quit\n", Options{})
	if !strings.Contains(out, "Goodbye") {
		t.Errorf("output missing 'Goodbye': %q", out)
	}
}

// TestREPL_Exit verifies that "exit" is an alias for
// "quit".
func TestREPL_Exit(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "exit\n", Options{})
	if !strings.Contains(out, "Goodbye") {
		t.Errorf("output missing 'Goodbye': %q", out)
	}
}

// TestREPL_EOF verifies that EOF (no commands) exits
// the REPL cleanly.
func TestREPL_EOF(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "", Options{})
	if !strings.Contains(out, "> ") {
		t.Errorf("output missing prompt: %q", out)
	}
	// No "Goodbye" because EOF is silent.
	if strings.Contains(out, "Goodbye") {
		t.Errorf("EOF should not print 'Goodbye': %q", out)
	}
}

// TestREPL_EmptyLine verifies that blank lines are
// no-ops (the prompt reappears).
func TestREPL_EmptyLine(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "\n\n\nquit\n", Options{})
	// Three blank lines + the quit prompt = 4 prompts.
	prompts := strings.Count(out, "> ")
	if prompts < 4 {
		t.Errorf("expected at least 4 prompts, got %d in: %q", prompts, out)
	}
}

// TestREPL_Time verifies that "time" prints the current
// tick and date.
func TestREPL_Time(t *testing.T) {
	w := newTestWorld()
	w.Tick = 42
	out := runREPL(t, w, "time\nquit\n", Options{})
	if !strings.Contains(out, "Tick 42") {
		t.Errorf("output missing 'Tick 42': %q", out)
	}
	if !strings.Contains(out, "1400-01-01") {
		t.Errorf("output missing date: %q", out)
	}
}

// TestREPL_Help verifies that the `help` meta-command prints
// the canonical command reference covering the 12 spec verbs,
// the meta-commands, and the pacing knobs. The output is
// deterministic (no world state), so the test asserts on a
// handful of substring invariants.
func TestREPL_Help(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "help\nquit\n", Options{})
	for _, want := range []string{
		"look",
		"talk",
		"travel",
		"sleep",
		"inventory",
		"save",
		"branch",
		"switch",
		"advance day",
		"auto-tick",
		"quit",
		"people",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
}

// TestREPL_HelpQuestionAlias verifies that `?` is accepted as a
// short alias for `help` (the two should produce identical output).
func TestREPL_HelpQuestionAlias(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "?\nquit\n", Options{})
	if !strings.Contains(out, "look") {
		t.Errorf("'?' alias output missing 'look':\n%s", out)
	}
	if !strings.Contains(out, "quit") {
		t.Errorf("'?' alias output missing 'quit':\n%s", out)
	}
}

// TestREPL_People verifies that "people" lists all alive
// people with their age and location.
func TestREPL_People(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "people\nquit\n", Options{})
	if !strings.Contains(out, "Alice") {
		t.Errorf("output missing 'Alice': %q", out)
	}
	if !strings.Contains(out, "Bob") {
		t.Errorf("output missing 'Bob': %q", out)
	}
	if !strings.Contains(out, "Blackwater") {
		t.Errorf("output missing 'Blackwater': %q", out)
	}
	if !strings.Contains(out, "Ashford") {
		t.Errorf("output missing 'Ashford': %q", out)
	}
}

// TestREPL_LookNoTarget verifies that bare "look" shows
// the first location (sorted by ID: "ashford") and its
// people.
func TestREPL_LookNoTarget(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "look\nquit\n", Options{})
	if !strings.Contains(out, "Ashford") {
		t.Errorf("output missing 'Ashford' (first location): %q", out)
	}
	if !strings.Contains(out, "Bob") {
		t.Errorf("output missing 'Bob' (in Ashford): %q", out)
	}
}

// TestREPL_LookPerson verifies that "look <name>" shows
// the person's details.
func TestREPL_LookPerson(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "look alice\nquit\n", Options{})
	if !strings.Contains(out, "Alice") {
		t.Errorf("output missing 'Alice': %q", out)
	}
	if !strings.Contains(out, "Gender: F") {
		t.Errorf("output missing 'Gender: F': %q", out)
	}
	if !strings.Contains(out, "Location: Blackwater") {
		t.Errorf("output missing 'Location: Blackwater': %q", out)
	}
}

// TestREPL_LookPersonByID verifies that "look <id>"
// works the same as "look <name>".
func TestREPL_LookPersonByID(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "look bob\nquit\n", Options{})
	if !strings.Contains(out, "Bob") {
		t.Errorf("output missing 'Bob': %q", out)
	}
	if !strings.Contains(out, "Gender: M") {
		t.Errorf("output missing 'Gender: M': %q", out)
	}
}

// TestREPL_LookLocation verifies that "look <location>"
// shows the location and its people.
func TestREPL_LookLocation(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "look blackwater\nquit\n", Options{})
	if !strings.Contains(out, "Blackwater") {
		t.Errorf("output missing 'Blackwater': %q", out)
	}
	if !strings.Contains(out, "Alice") {
		t.Errorf("output missing 'Alice' (in Blackwater): %q", out)
	}
}

// TestREPL_LookUnknown verifies that an unknown target
// prints a clear "I don't see" message.
func TestREPL_LookUnknown(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "look nobody\nquit\n", Options{})
	if !strings.Contains(out, "I don't see") {
		t.Errorf("output missing 'I don't see': %q", out)
	}
}

// TestREPL_Inspect verifies that "inspect <name>" shows
// the person's full details.
func TestREPL_Inspect(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "inspect alice\nquit\n", Options{})
	if !strings.Contains(out, "Alice") {
		t.Errorf("output missing 'Alice': %q", out)
	}
	if !strings.Contains(out, "Gender: F") {
		t.Errorf("output missing 'Gender: F': %q", out)
	}
	if !strings.Contains(out, "Location: Blackwater") {
		t.Errorf("output missing 'Location: Blackwater': %q", out)
	}
}

// TestREPL_AdvanceDay verifies that "advance day" calls
// TickFn exactly once.
func TestREPL_AdvanceDay(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	out := runREPL(t, w, "advance day\nquit\n", Options{TickFn: mt.tick(w)})
	if mt.calls != 1 {
		t.Errorf("TickFn calls = %d, want 1", mt.calls)
	}
	if !strings.Contains(out, "Advanced 1 day") {
		t.Errorf("output missing 'Advanced 1 day': %q", out)
	}
	if !strings.Contains(out, "tick 1") {
		t.Errorf("output missing 'tick 1': %q", out)
	}
}

// TestREPL_AdvanceWeek verifies that "advance week"
// calls TickFn 7 times.
func TestREPL_AdvanceWeek(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	runREPL(t, w, "advance week\nquit\n", Options{TickFn: mt.tick(w)})
	if mt.calls != 7 {
		t.Errorf("TickFn calls = %d, want 7", mt.calls)
	}
}

// TestREPL_AdvanceMonth verifies that "advance month"
// calls TickFn 30 times.
func TestREPL_AdvanceMonth(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	runREPL(t, w, "advance month\nquit\n", Options{TickFn: mt.tick(w)})
	if mt.calls != 30 {
		t.Errorf("TickFn calls = %d, want 30", mt.calls)
	}
}

// TestREPL_AdvanceInvalid verifies that an invalid unit
// returns an error.
func TestREPL_AdvanceInvalid(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	out := runREPL(t, w, "advance year\nquit\n", Options{TickFn: mt.tick(w)})
	if mt.calls != 0 {
		t.Errorf("TickFn calls = %d, want 0 (invalid unit)", mt.calls)
	}
	if !strings.Contains(out, "advance:") {
		t.Errorf("output missing 'advance:' error prefix: %q", out)
	}
}

// TestREPL_AutoTickOn verifies that "auto-tick on"
// causes one TickFn call before each subsequent prompt.
func TestREPL_AutoTickOn(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	// 4 lines: "auto-tick on" (no tick before this prompt,
	// auto-tick starts OFF), then "time", "time", "quit" —
	// each preceded by one auto-tick. Total: 3 ticks.
	out := runREPL(t, w, "auto-tick on\ntime\ntime\nquit\n", Options{TickFn: mt.tick(w)})
	if mt.calls != 3 {
		t.Errorf("TickFn calls = %d, want 3 (auto-tick before each of 3 prompts)", mt.calls)
	}
	if !strings.Contains(out, "Auto-tick enabled") {
		t.Errorf("output missing 'Auto-tick enabled': %q", out)
	}
}

// TestREPL_AutoTickOff verifies that "auto-tick off"
// disables auto-ticking. The on→off toggle: the "off"
// command itself is preceded by one auto-tick (because
// auto-tick is still on when it's read), then off takes
// effect for the remaining prompts. Total: 1 tick.
func TestREPL_AutoTickOff(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	runREPL(t, w, "auto-tick on\nauto-tick off\ntime\ntime\nquit\n", Options{TickFn: mt.tick(w)})
	if mt.calls != 1 {
		t.Errorf("TickFn calls = %d, want 1 (auto-tick fires once before 'auto-tick off' is read)", mt.calls)
	}
}

// TestREPL_AutoTickInvalid verifies that an invalid arg
// returns an error and does not change the state.
func TestREPL_AutoTickInvalid(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	out := runREPL(t, w, "auto-tick maybe\nquit\n", Options{TickFn: mt.tick(w)})
	if !strings.Contains(out, "auto-tick:") {
		t.Errorf("output missing 'auto-tick:' error prefix: %q", out)
	}
	if mt.calls != 0 {
		t.Errorf("TickFn calls = %d, want 0 (invalid arg)", mt.calls)
	}
}

// TestREPL_Inventory verifies that "inventory" prints
// the stub message.
func TestREPL_Inventory(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "inventory\nquit\n", Options{})
	if !strings.Contains(out, "Inventory") {
		t.Errorf("output missing 'Inventory': %q", out)
	}
	if !strings.Contains(out, "not yet implemented") {
		t.Errorf("output missing 'not yet implemented': %q", out)
	}
}

// TestREPL_Talk verifies that "talk <name>" prints the
// stub message and handles unknown targets.
func TestREPL_Talk(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "talk alice\nquit\n", Options{})
	if !strings.Contains(out, "You talk to Alice") {
		t.Errorf("output missing 'You talk to Alice': %q", out)
	}

	out = runREPL(t, w, "talk nobody\nquit\n", Options{})
	if !strings.Contains(out, "I don't see") {
		t.Errorf("output missing 'I don't see' for unknown talk target: %q", out)
	}
}

// TestREPL_Travel verifies that "travel <location>"
// prints the stub message and handles unknown targets.
func TestREPL_Travel(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "travel blackwater\nquit\n", Options{})
	if !strings.Contains(out, "You travel to Blackwater") {
		t.Errorf("output missing 'You travel to Blackwater': %q", out)
	}

	out = runREPL(t, w, "travel nowhere\nquit\n", Options{})
	if !strings.Contains(out, "I don't know the location") {
		t.Errorf("output missing 'I don't know the location': %q", out)
	}
}

// TestREPL_TalkWithNarrator verifies that when a Narrator
// is configured, "talk <name>" delegates to it and prints
// the rendered text instead of the stub.
func TestREPL_TalkWithNarrator(t *testing.T) {
	w := newTestWorld()
	mock := &mockLLM{response: "Alice greets you warmly."}
	nar := narrator.New(mock, 4)
	act := action.New(w, nar)
	out := runREPL(t, w, "talk alice\nquit\n", Options{Narrator: nar, Action: act})
	if !strings.Contains(out, "Alice greets you warmly") {
		t.Errorf("output missing narrator-rendered text: %q", out)
	}
	if mock.calls != 1 {
		t.Errorf("LLM calls = %d, want 1", mock.calls)
	}
}

// TestREPL_TravelWithNarrator verifies that when a Narrator
// is configured, "travel <location>" delegates to it and
// prints the template-rendered text (travel is routine,
// so the LLM is NOT called).
func TestREPL_TravelWithNarrator(t *testing.T) {
	w := newTestWorld()
	mock := &mockLLM{response: "The road to Ashford stretches before you."}
	nar := narrator.New(mock, 4)
	act := action.New(w, nar)
	out := runREPL(t, w, "travel ashford\nquit\n", Options{Narrator: nar, Action: act})
	if !strings.Contains(out, "You travel to Ashford") {
		t.Errorf("output missing narrator template text: %q", out)
	}
	// Travel is routine — LLM should NOT be called.
	if mock.calls != 0 {
		t.Errorf("LLM calls = %d, want 0 (travel is routine)", mock.calls)
	}
}

// TestREPL_TalkWithNarrator_Fallback verifies that when the
// Narrator's LLM errors out, the REPL still prints something
// (the template fallback).
func TestREPL_TalkWithNarrator_Fallback(t *testing.T) {
	w := newTestWorld()
	mock := &mockLLM{err: errors.New("llm down")}
	nar := narrator.New(mock, 4)
	act := action.New(w, nar)
	out := runREPL(t, w, "talk alice\nquit\n", Options{Narrator: nar, Action: act})
	// Template fallback: "You talk to Alice."
	if !strings.Contains(out, "You talk to Alice") {
		t.Errorf("output missing template fallback text: %q", out)
	}
}

// TestREPL_Sleep verifies that "sleep" calls TickFn at
// least once (rounding up partial days).
func TestREPL_Sleep(t *testing.T) {
	w := newTestWorld()
	mt := &mockTick{}
	out := runREPL(t, w, "sleep\nquit\n", Options{TickFn: mt.tick(w)})
	// 8 hours → 8/24 = 0 ticks → rounded up to 1.
	if mt.calls != 1 {
		t.Errorf("TickFn calls = %d, want 1 (8h rounds up to 1 tick)", mt.calls)
	}
	if !strings.Contains(out, "You sleep for 8 hours") {
		t.Errorf("output missing 'You sleep for 8 hours': %q", out)
	}
}

// TestREPL_Save verifies that "save <path>" writes a
// SQLite file at the given path.
func TestREPL_Save(t *testing.T) {
	w := newTestWorld()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	out := runREPL(t, w, "save "+path+"\nquit\n", Options{})
	if !strings.Contains(out, "Saved to "+path) {
		t.Errorf("output missing 'Saved to %s': %q", path, out)
	}
	// Verify the file exists and is non-empty.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Errorf("save produced empty file at %s", path)
	}
}

// TestREPL_SaveDefaultPath verifies that bare "save"
// uses <world-id>.db as the default path.
func TestREPL_SaveDefaultPath(t *testing.T) {
	w := newTestWorld() // ID = "test"
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	out := runREPL(t, w, "save\nquit\n", Options{})
	if !strings.Contains(out, "Saved to test.db") {
		t.Errorf("output missing 'Saved to test.db': %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "test.db")); err != nil {
		t.Errorf("default-path save did not create test.db: %v", err)
	}
}

// TestREPL_IntentParserWiring verifies that typed
// commands flow through the intent parser. The verb
// "look" is matched by the rule parser; the resulting
// Intent is dispatched to execLook.
func TestREPL_IntentParserWiring(t *testing.T) {
	w := newTestWorld()
	// "hail" is not an alias → rule parser fails → LLM
	// fallback is called (but LLM is nil in the test
	// parser, so it returns an error). The REPL should
	// surface that error.
	out := runREPL(t, w, "hail alice\nquit\n", Options{})
	if !strings.Contains(out, "error:") {
		t.Errorf("output missing 'error:' for unknown verb: %q", out)
	}
}

// TestREPL_AliasRouting verifies that aliases route to
// the right action (e.g., "i" → inventory, "time" → time).
func TestREPL_AliasRouting(t *testing.T) {
	w := newTestWorld()
	out := runREPL(t, w, "i\nquit\n", Options{})
	if !strings.Contains(out, "Inventory") {
		t.Errorf("'i' alias should route to inventory: %q", out)
	}

	out = runREPL(t, w, "date\nquit\n", Options{})
	if !strings.Contains(out, "Tick") {
		t.Errorf("'date' alias should route to time: %q", out)
	}
}

// TestREPL_ContextCancel verifies that cancelling the
// context causes Run to return ctx.Err().
func TestREPL_ContextCancel(t *testing.T) {
	w := newTestWorld()
	parser := intent.New(nil, w)
	r := New(w, parser, Options{In: strings.NewReader("time\ntime\ntime\n"), Out: &bytes.Buffer{}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run

	err := r.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run with cancelled context: error = %v, want context.Canceled", err)
	}
}

// TestREPL_GameOver verifies that the REPL exits with a
// clear message when the world has no alive people.
//
// Phase 30 update: when the player is dead and there are no
// living candidates, the lineage flow handles the exit with
// "The chronicle ends here." (set r.bloodlineEnded = true and
// return). The old isGameOver path's "Everyone has died."
// message is now the fallback for the no-PlayerID case
// (everyone died but no lineage flow fired). The world built
// by newTestWorld has alice as the PlayerID, so the lineage
// path fires first.
func TestREPL_GameOver(t *testing.T) {
	w := newTestWorld()
	// Mark everyone dead.
	for _, p := range w.People {
		p.Alive = false
	}
	out := runREPL(t, w, "time\n", Options{})
	// The lineage path produces "The chronicle ends here."
	if !strings.Contains(out, "The chronicle ends here") {
		t.Errorf("output missing 'The chronicle ends here': %q", out)
	}
}

// TestREPL_GameOverNoPlayer verifies the fallback path:
// when the world has no alive people AND no PlayerID is
// set, the isGameOver branch fires with "Everyone has died."
// (the lineage flow only runs when there's a dead player).
func TestREPL_GameOverNoPlayer(t *testing.T) {
	w := newTestWorld()
	w.PlayerID = "" // no player → lineage flow is a no-op
	for _, p := range w.People {
		p.Alive = false
	}
	out := runREPL(t, w, "time\n", Options{})
	if !strings.Contains(out, "Everyone has died") {
		t.Errorf("output missing 'Everyone has died' (no-player game-over fallback): %q", out)
	}
}

// TestREPL_NoGameOverForEmptyWorld verifies that a
// freshly-created world (no people) is NOT treated as
// game-over — it's a valid pre-game state.
func TestREPL_NoGameOverForEmptyWorld(t *testing.T) {
	w := core.NewWorld("empty", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	out := runREPL(t, w, "quit\n", Options{})
	if strings.Contains(out, "Everyone has died") {
		t.Errorf("empty world should not trigger game-over: %q", out)
	}
}

// TestREPL_TickFnError verifies that a TickFn error
// propagates from auto-tick and aborts the loop.
func TestREPL_TickFnError(t *testing.T) {
	w := newTestWorld()
	tickErr := errors.New("engine exploded")
	parser := intent.New(nil, w)
	r := New(w, parser, Options{
		In:       strings.NewReader("time\ntime\n"),
		Out:      &bytes.Buffer{},
		TickFn:   func() error { return tickErr },
		AutoTick: true,
	})
	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "engine exploded") {
		t.Errorf("Run with failing TickFn: error = %v, want it to contain 'engine exploded'", err)
	}
}
