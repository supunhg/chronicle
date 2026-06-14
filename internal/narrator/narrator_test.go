package narrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/llm"
)

// mockLLM is a hand-rolled fake of LLMClient. It returns
// a canned response (or error) from Chat, with a record
// of how many times it was called and what messages it
// received. Used to assert the prompt was built correctly.
type mockLLM struct {
	response string
	err      error
	calls    int
	lastMsgs []llm.ChatMessage
}

func (m *mockLLM) Chat(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	m.calls++
	m.lastMsgs = messages
	return m.response, m.err
}

// newTestWorld builds a small world with 2 people and 2
// locations for predictable output assertions.
func newTestWorld() *core.World {
	w := core.NewWorld("test", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "ashford", Name: "Ashford", PopulationCap: 50})
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", Alive: true, Gender: "F", BirthTick: -20 * 365, LocationID: "blackwater"})
	w.AddPerson(&core.Person{ID: "bob", Name: "Bob", Alive: true, Gender: "M", BirthTick: -30 * 365, LocationID: "ashford"})
	w.Tick = 100
	w.Now = w.Now.AddDate(0, 0, 100)
	return w
}

// TestNarrate_LookTemplate verifies that look events
// always use the template (never the LLM).
func TestNarrate_LookTemplate(t *testing.T) {
	mock := &mockLLM{response: "should not be called"}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventLook, Person: w.People["alice"]})
	if !strings.Contains(got, "Alice") {
		t.Errorf("look output missing 'Alice': %q", got)
	}
	if mock.calls != 0 {
		t.Errorf("LLM calls = %d, want 0 (look is always template)", mock.calls)
	}
}

// TestNarrate_TimeTemplate verifies that time events
// use the template and include the current tick.
func TestNarrate_TimeTemplate(t *testing.T) {
	mock := &mockLLM{}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventTime})
	if !strings.Contains(got, "tick 100") {
		t.Errorf("time output missing 'tick 100': %q", got)
	}
	if mock.calls != 0 {
		t.Errorf("LLM calls = %d, want 0 (time is always template)", mock.calls)
	}
}

// TestNarrate_InventoryTemplate verifies that inventory
// uses the template stub.
func TestNarrate_InventoryTemplate(t *testing.T) {
	mock := &mockLLM{}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventInventory})
	if !strings.Contains(got, "nothing") {
		t.Errorf("inventory output missing 'nothing': %q", got)
	}
	if mock.calls != 0 {
		t.Errorf("LLM calls = %d, want 0 (inventory is always template)", mock.calls)
	}
}

// TestNarrate_TalkTemplate verifies that talk events
// use the template (LLM is rate-limited or unavailable).
func TestNarrate_TalkTemplate(t *testing.T) {
	mock := &mockLLM{err: errors.New("LLM unavailable")}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if !strings.Contains(got, "Alice") {
		t.Errorf("talk output missing 'Alice': %q", got)
	}
	if mock.calls != 1 {
		// LLM was called but failed; template is the fallback.
		t.Errorf("LLM calls = %d, want 1 (talk is significant; LLM was attempted then failed)", mock.calls)
	}
}

// TestNarrate_TravelTemplate verifies that travel events
// use the template.
func TestNarrate_TravelTemplate(t *testing.T) {
	mock := &mockLLM{}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventTravel, Location: w.Locations["blackwater"]})
	if !strings.Contains(got, "Blackwater") {
		t.Errorf("travel output missing 'Blackwater': %q", got)
	}
	if mock.calls != 0 {
		t.Errorf("LLM calls = %d, want 0 (travel is template in Phase 17.4)", mock.calls)
	}
}

// TestNarrate_DeathTemplate verifies that death events
// use the template.
func TestNarrate_DeathTemplate(t *testing.T) {
	mock := &mockLLM{}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventDeath, Person: w.People["bob"]})
	if !strings.Contains(got, "Bob") {
		t.Errorf("death output missing 'Bob': %q", got)
	}
	if !strings.Contains(got, "died") {
		t.Errorf("death output missing 'died': %q", got)
	}
}

// TestNarrate_BirthTemplate verifies that birth events
// use the template.
func TestNarrate_BirthTemplate(t *testing.T) {
	mock := &mockLLM{}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventBirth, Person: w.People["alice"]})
	if !strings.Contains(got, "Alice") {
		t.Errorf("birth output missing 'Alice': %q", got)
	}
	if !strings.Contains(got, "born") {
		t.Errorf("birth output missing 'born': %q", got)
	}
}

// TestNarrate_FirstMeetTemplate verifies that first
// meeting events use the template.
func TestNarrate_FirstMeetTemplate(t *testing.T) {
	mock := &mockLLM{}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventFirstMeet, Person: w.People["alice"]})
	if !strings.Contains(got, "Alice") {
		t.Errorf("first_meet output missing 'Alice': %q", got)
	}
}

// TestNarrate_LLMCall_Significant verifies that
// significant events call the LLM (when not rate-limited)
// and return the LLM's response.
func TestNarrate_LLMCall_Significant(t *testing.T) {
	mock := &mockLLM{response: "Alice greets you warmly, her eyes bright with curiosity."}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if got != mock.response {
		t.Errorf("talk output = %q, want %q (LLM response)", got, mock.response)
	}
	if mock.calls != 1 {
		t.Errorf("LLM calls = %d, want 1", mock.calls)
	}
	// The LLM's system prompt should describe the narrator's role.
	if len(mock.lastMsgs) < 1 {
		t.Fatal("LLM received no messages")
	}
	if !strings.Contains(mock.lastMsgs[0].Content, "narrator") {
		t.Errorf("system prompt missing narrator role: %q", mock.lastMsgs[0].Content)
	}
	// The user prompt should describe the event.
	if len(mock.lastMsgs) < 2 {
		t.Fatal("LLM received only system message, no user message")
	}
	if !strings.Contains(mock.lastMsgs[1].Content, "Alice") {
		t.Errorf("user prompt missing person 'Alice': %q", mock.lastMsgs[1].Content)
	}
	if !strings.Contains(mock.lastMsgs[1].Content, "talk") {
		t.Errorf("user prompt missing event type 'talk': %q", mock.lastMsgs[1].Content)
	}
}

// TestNarrate_RateLimit verifies that the second
// significant call within the rate-limit window uses
// the template, not the LLM.
func TestNarrate_RateLimit(t *testing.T) {
	mock := &mockLLM{response: "generated text"}
	n := New(mock, 4) // 4 ticks between calls
	w := newTestWorld()

	// First call at tick 100 → LLM.
	_ = n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if mock.calls != 1 {
		t.Fatalf("first call: LLM calls = %d, want 1", mock.calls)
	}

	// Second call at tick 102 (within 4 ticks) → template.
	_ = n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["bob"]})
	if mock.calls != 1 {
		t.Errorf("second call (within rate limit): LLM calls = %d, want still 1 (rate-limited)", mock.calls)
	}

	// Third call at tick 105 (5 ticks after first) → LLM.
	w.Tick = 105
	_ = n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if mock.calls != 2 {
		t.Errorf("third call (after rate limit): LLM calls = %d, want 2", mock.calls)
	}
}

// TestNarrate_CacheHit verifies that the same (event,
// world state) returns the cached narration without
// calling the LLM again.
func TestNarrate_CacheHit(t *testing.T) {
	mock := &mockLLM{response: "generated text"}
	n := New(mock, 4)
	w := newTestWorld()

	// First call → LLM.
	got1 := n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if got1 != mock.response {
		t.Errorf("first call = %q, want %q", got1, mock.response)
	}

	// Second call (same event, same world) → cache hit.
	// Note: even though we're within the rate limit, the
	// cache check happens BEFORE the rate limit check, so
	// the cached text is returned.
	got2 := n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if got2 != mock.response {
		t.Errorf("second call = %q, want %q (cached)", got2, mock.response)
	}
	if mock.calls != 1 {
		t.Errorf("LLM calls = %d, want 1 (cache hit on second call)", mock.calls)
	}
	if stats := n.Stats(); stats.CacheSize != 1 {
		t.Errorf("cache size = %d, want 1", stats.CacheSize)
	}
}

// TestNarrate_CacheMissOnWorldChange verifies that a
// world change (person added) invalidates the cache.
func TestNarrate_CacheMissOnWorldChange(t *testing.T) {
	mock := &mockLLM{response: "generated text"}
	n := New(mock, 4)
	w := newTestWorld()

	// First call → LLM.
	_ = n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	// Advance tick past the rate limit.
	w.Tick = 200

	// Add a new person to change the world hash.
	w.AddPerson(&core.Person{ID: "carol", Name: "Carol", Alive: true, Gender: "F", BirthTick: -10 * 365, LocationID: "blackwater"})

	// Second call → cache miss (world changed) → LLM.
	_ = n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if mock.calls != 2 {
		t.Errorf("LLM calls = %d, want 2 (cache miss on world change)", mock.calls)
	}
}

// TestNarrate_LLMErrorFallback verifies that an LLM
// error falls back to the template silently.
func TestNarrate_LLMErrorFallback(t *testing.T) {
	mock := &mockLLM{err: errors.New("connection refused")}
	n := New(mock, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if !strings.Contains(got, "Alice") {
		t.Errorf("LLM error fallback missing 'Alice': %q", got)
	}
	if mock.calls != 1 {
		t.Errorf("LLM calls = %d, want 1 (called then failed)", mock.calls)
	}
	// LastCallTick should NOT be updated on failure.
	if stats := n.Stats(); stats.LastCallTick != 0 {
		t.Errorf("LastCallTick = %d, want 0 (not updated on failure)", stats.LastCallTick)
	}
}

// TestNarrate_NilLLMClient verifies that a nil LLM
// client produces template output for significant
// events (no panic, no LLM call).
func TestNarrate_NilLLMClient(t *testing.T) {
	n := New(nil, 4)
	w := newTestWorld()

	got := n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	if !strings.Contains(got, "Alice") {
		t.Errorf("nil LLM fallback missing 'Alice': %q", got)
	}
}

// TestNarrate_NilWorld verifies that a nil world
// produces empty output without panicking.
func TestNarrate_NilWorld(t *testing.T) {
	n := New(nil, 4)
	got := n.Narrate(context.Background(), nil, Event{Type: EventLook})
	if got != "" {
		t.Errorf("nil world output = %q, want empty", got)
	}
}

// TestNarrate_Stats verifies the Stats helper.
func TestNarrate_Stats(t *testing.T) {
	mock := &mockLLM{response: "x"}
	n := New(mock, 4)
	w := newTestWorld()

	_ = n.Narrate(context.Background(), w, Event{Type: EventTalk, Person: w.People["alice"]})
	stats := n.Stats()
	if stats.CacheSize != 1 {
		t.Errorf("CacheSize = %d, want 1", stats.CacheSize)
	}
	if stats.LastCallTick != w.Tick {
		t.Errorf("LastCallTick = %d, want %d", stats.LastCallTick, w.Tick)
	}
}

// TestIsSignificant exhaustively checks the significant/
// routine classification.
func TestIsSignificant(t *testing.T) {
	significant := []EventType{EventTalk, EventDeath, EventBirth, EventFirstMeet}
	for _, et := range significant {
		if !isSignificant(et) {
			t.Errorf("isSignificant(%q) = false, want true", et)
		}
	}
	routine := []EventType{EventLook, EventInventory, EventTime, EventTravel}
	for _, et := range routine {
		if isSignificant(et) {
			t.Errorf("isSignificant(%q) = true, want false", et)
		}
	}
}
