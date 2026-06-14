package intent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/llm"
)

// mockLLM is a hand-rolled fake of LLMClient. It returns a
// canned response (or error) from Chat, with a record of how
// many times it was called and what messages it received.
// The record is useful for tests that want to assert the
// prompt was built correctly.
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

// newTestWorld builds a small world with a handful of people
// and locations for prompt-content assertions. The IDs are
// lowercase and the names are simple so the assertions stay
// readable.
func newTestWorld() *core.World {
	w := core.NewWorld("test", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater"})
	w.AddLocation(&core.Location{ID: "ashford", Name: "Ashford"})
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", Alive: true, LocationID: "blackwater"})
	w.AddPerson(&core.Person{ID: "bob", Name: "Bob", Alive: true, LocationID: "blackwater"})
	return w
}

// TestParser_Rule_Look covers the look verb: bare, with a
// target, with "at", and with aliases.
func TestParser_Rule_Look(t *testing.T) {
	p := New(nil, newTestWorld())

	cases := []struct {
		in     string
		action Action
		target string
	}{
		{"look", ActionLook, ""},
		{"l", ActionLook, ""},
		{"watch", ActionLook, ""},
		{"look alice", ActionLook, "alice"},
		{"look at alice", ActionLook, "alice"},
		{"LOOK", ActionLook, ""},
		// "Look At Alice" — verb is lowercased for matching,
		// the "At" preposition is stripped (case-insensitive),
		// and the target "Alice" keeps its original case.
		{"Look At Alice", ActionLook, "Alice"},
	}
	for _, c := range cases {
		got, err := p.Parse(context.Background(), c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got.Action != c.action {
			t.Errorf("Parse(%q).Action = %q, want %q", c.in, got.Action, c.action)
		}
		if got.Target != c.target {
			t.Errorf("Parse(%q).Target = %q, want %q", c.in, got.Target, c.target)
		}
		if got.Source != "rule" {
			t.Errorf("Parse(%q).Source = %q, want %q", c.in, got.Source, "rule")
		}
		if got.Raw != c.in {
			t.Errorf("Parse(%q).Raw = %q, want %q", c.in, got.Raw, c.in)
		}
	}
}

// TestParser_Rule_Inventory covers inventory and its aliases
// (i, inv). No arguments are accepted.
func TestParser_Rule_Inventory(t *testing.T) {
	p := New(nil, newTestWorld())
	for _, in := range []string{"inventory", "inv", "i", "INVENTORY"} {
		got, err := p.Parse(context.Background(), in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got.Action != ActionInventory {
			t.Errorf("Parse(%q).Action = %q, want %q", in, got.Action, ActionInventory)
		}
		if got.Source != "rule" {
			t.Errorf("Parse(%q).Source = %q, want %q", in, got.Source, "rule")
		}
	}
}

// TestParser_Rule_Sleep covers sleep with and without a
// duration. An invalid first token (not a number) should
// fall through to the LLM (or error if no LLM is configured).
func TestParser_Rule_Sleep(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "sleep")
	if err != nil {
		t.Fatalf("Parse(\"sleep\"): %v", err)
	}
	if got.Action != ActionSleep {
		t.Errorf("Action = %q, want %q", got.Action, ActionSleep)
	}
	if got.Args.Hours != 8 {
		t.Errorf("Args.Hours = %d, want 8 (default)", got.Args.Hours)
	}

	got, err = p.Parse(context.Background(), "sleep 12")
	if err != nil {
		t.Fatalf("Parse(\"sleep 12\"): %v", err)
	}
	if got.Args.Hours != 12 {
		t.Errorf("Args.Hours = %d, want 12", got.Args.Hours)
	}

	// "sleep forever" — first token isn't a number. With no
	// LLM configured, the parser should error.
	_, err = p.Parse(context.Background(), "sleep forever")
	if err == nil {
		t.Error("Parse(\"sleep forever\") with nil LLM: got nil error, want non-nil")
	}
}

// TestParser_Rule_Travel covers travel with target, with
// "to", and missing target. Missing target falls through to
// the LLM.
func TestParser_Rule_Travel(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "travel blackwater")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionTravel {
		t.Errorf("Action = %q, want %q", got.Action, ActionTravel)
	}
	if got.Target != "blackwater" {
		t.Errorf("Target = %q, want %q", got.Target, "blackwater")
	}

	got, err = p.Parse(context.Background(), "go to ashford")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Target != "ashford" {
		t.Errorf("Target = %q, want %q", got.Target, "ashford")
	}

	// Missing target: should fall through to LLM (and error
	// because LLM is nil).
	_, err = p.Parse(context.Background(), "travel")
	if err == nil {
		t.Error("Parse(\"travel\") with no target and nil LLM: got nil error, want non-nil")
	}
}

// TestParser_Rule_Talk covers talk with target, with "to",
// and with "with".
func TestParser_Rule_Talk(t *testing.T) {
	p := New(nil, newTestWorld())

	cases := []struct {
		in     string
		target string
	}{
		{"talk alice", "alice"},
		{"talk to alice", "alice"},
		{"talk with bob", "bob"},
		{"chat with carol", "carol"},
		{"ask dave", "dave"},
	}
	for _, c := range cases {
		got, err := p.Parse(context.Background(), c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got.Action != ActionTalk {
			t.Errorf("Parse(%q).Action = %q, want %q", c.in, got.Action, ActionTalk)
		}
		if got.Target != c.target {
			t.Errorf("Parse(%q).Target = %q, want %q", c.in, got.Target, c.target)
		}
	}
}

// TestParser_Rule_Inspect covers inspect and its aliases
// (x, examine, check).
func TestParser_Rule_Inspect(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "inspect alice")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionInspect {
		t.Errorf("Action = %q, want %q", got.Action, ActionInspect)
	}
	if got.Target != "alice" {
		t.Errorf("Target = %q, want %q", got.Target, "alice")
	}

	// Alias: examine. Same target.
	got, err = p.Parse(context.Background(), "examine bob")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionInspect {
		t.Errorf("Action = %q, want %q", got.Action, ActionInspect)
	}
	if got.Target != "bob" {
		t.Errorf("Target = %q, want %q", got.Target, "bob")
	}
}

// TestParser_Rule_Buy covers buy with and without a
// leading quantity, and sell (which uses the same parser).
func TestParser_Rule_Buy(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "buy bread")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionBuy {
		t.Errorf("Action = %q, want %q", got.Action, ActionBuy)
	}
	if got.Target != "bread" {
		t.Errorf("Target = %q, want %q", got.Target, "bread")
	}
	if got.Args.Quantity != 1 {
		t.Errorf("Args.Quantity = %d, want 1 (default)", got.Args.Quantity)
	}

	got, err = p.Parse(context.Background(), "buy 5 bread")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Args.Quantity != 5 {
		t.Errorf("Args.Quantity = %d, want 5", got.Args.Quantity)
	}
	if got.Target != "bread" {
		t.Errorf("Target = %q, want %q", got.Target, "bread")
	}
}

// TestParser_Rule_Sell mirrors TestParser_Rule_Buy but for
// sell. Same parser underneath, different verb.
func TestParser_Rule_Sell(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "sell sword")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionSell {
		t.Errorf("Action = %q, want %q", got.Action, ActionSell)
	}
	if got.Target != "sword" {
		t.Errorf("Target = %q, want %q", got.Target, "sword")
	}
	if got.Args.Quantity != 1 {
		t.Errorf("Args.Quantity = %d, want 1", got.Args.Quantity)
	}
}

// TestParser_Rule_Time covers the time verb (no arguments).
func TestParser_Rule_Time(t *testing.T) {
	p := New(nil, newTestWorld())

	for _, in := range []string{"time", "date", "now", "when", "TIME"} {
		got, err := p.Parse(context.Background(), in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got.Action != ActionTime {
			t.Errorf("Parse(%q).Action = %q, want %q", in, got.Action, ActionTime)
		}
	}
}

// TestParser_Rule_Save covers save with and without a path.
func TestParser_Rule_Save(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "save")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionSave {
		t.Errorf("Action = %q, want %q", got.Action, ActionSave)
	}
	if got.Target != "" {
		t.Errorf("Target = %q, want empty", got.Target)
	}

	got, err = p.Parse(context.Background(), "save myrun.db")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Target != "myrun.db" {
		t.Errorf("Target = %q, want %q", got.Target, "myrun.db")
	}
}

// TestParser_Rule_Branch covers branch with a name (required).
func TestParser_Rule_Branch(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "branch before_war")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionBranch {
		t.Errorf("Action = %q, want %q", got.Action, ActionBranch)
	}
	if got.Target != "before_war" {
		t.Errorf("Target = %q, want %q", got.Target, "before_war")
	}

	// Missing name: falls through to LLM.
	_, err = p.Parse(context.Background(), "branch")
	if err == nil {
		t.Error("Parse(\"branch\") with no name and nil LLM: got nil error, want non-nil")
	}
}

// TestParser_Rule_Switch covers switch with a name (required).
func TestParser_Rule_Switch(t *testing.T) {
	p := New(nil, newTestWorld())

	got, err := p.Parse(context.Background(), "switch before_war")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionSwitch {
		t.Errorf("Action = %q, want %q", got.Action, ActionSwitch)
	}
	if got.Target != "before_war" {
		t.Errorf("Target = %q, want %q", got.Target, "before_war")
	}
}

// TestParser_EmptyInput verifies that whitespace-only and
// empty input both return an error.
func TestParser_EmptyInput(t *testing.T) {
	p := New(nil, newTestWorld())
	for _, in := range []string{"", "   ", "\t\n"} {
		_, err := p.Parse(context.Background(), in)
		if err == nil {
			t.Errorf("Parse(%q): got nil error, want non-nil", in)
		}
	}
}

// TestParser_UnknownVerbNoLLM verifies that an unknown verb
// returns an error when no LLM is configured.
func TestParser_UnknownVerbNoLLM(t *testing.T) {
	p := New(nil, newTestWorld())
	_, err := p.Parse(context.Background(), "frobnicate the gizmo")
	if err == nil {
		t.Fatal("Parse with unknown verb and nil LLM: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("error %q should mention the unknown verb", err.Error())
	}
}

// TestParser_LLMFallback_Success verifies the happy path:
// unknown verb → LLM called → valid JSON returned → Intent
// built with Source="llm".
func TestParser_LLMFallback_Success(t *testing.T) {
	mock := &mockLLM{
		response: `{"action": "talk", "target": "elena", "args": {}}`,
	}
	p := New(mock, newTestWorld())

	// "converse" is not in the alias table, so the rule
	// parser falls through to the LLM.
	got, err := p.Parse(context.Background(), "converse with the blacksmith's daughter")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Action != ActionTalk {
		t.Errorf("Action = %q, want %q", got.Action, ActionTalk)
	}
	if got.Target != "elena" {
		t.Errorf("Target = %q, want %q", got.Target, "elena")
	}
	if got.Source != "llm" {
		t.Errorf("Source = %q, want %q", got.Source, "llm")
	}
	if mock.calls != 1 {
		t.Errorf("LLM calls = %d, want 1", mock.calls)
	}
	// The system prompt should mention the valid actions.
	if len(mock.lastMsgs) < 1 {
		t.Fatal("LLM received no messages")
	}
	if !strings.Contains(mock.lastMsgs[0].Content, "talk") {
		t.Errorf("system prompt missing action list: %q", mock.lastMsgs[0].Content)
	}
}

// TestParser_LLMFallback_InvalidJSON verifies that
// malformed JSON is rejected by the schema gate.
func TestParser_LLMFallback_InvalidJSON(t *testing.T) {
	mock := &mockLLM{response: "this is not json at all"}
	p := New(mock, newTestWorld())

	_, err := p.Parse(context.Background(), "garble")
	if err == nil {
		t.Fatal("Parse with invalid LLM JSON: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Errorf("error %q should mention JSON", err.Error())
	}
}

// TestParser_LLMFallback_UnknownAction verifies that an
// action not in the 12-verb spec is rejected.
func TestParser_LLMFallback_UnknownAction(t *testing.T) {
	mock := &mockLLM{response: `{"action": "fly", "target": ""}`}
	p := New(mock, newTestWorld())

	_, err := p.Parse(context.Background(), "soar above the clouds")
	if err == nil {
		t.Fatal("Parse with unknown action from LLM: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "fly") {
		t.Errorf("error %q should mention the rejected action", err.Error())
	}
}

// TestParser_LLMFallback_EmptyAction verifies that the
// LLM's "I can't parse" signal (empty action) is rejected.
func TestParser_LLMFallback_EmptyAction(t *testing.T) {
	mock := &mockLLM{response: `{"action": "", "target": ""}`}
	p := New(mock, newTestWorld())

	// "seek" is not in the alias table, so the rule parser
	// falls through to the LLM.
	_, err := p.Parse(context.Background(), "seek wisdom from the oracle")
	if err == nil {
		t.Fatal("Parse with empty action from LLM: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "empty action") {
		t.Errorf("error %q should mention empty action", err.Error())
	}
}

// TestParser_LLMFallback_TransportError verifies that a
// failed LLM call propagates as an error.
func TestParser_LLMFallback_TransportError(t *testing.T) {
	mock := &mockLLM{err: errors.New("connection refused")}
	p := New(mock, newTestWorld())

	_, err := p.Parse(context.Background(), "do something weird")
	if err == nil {
		t.Fatal("Parse with LLM transport error: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "LLM call") {
		t.Errorf("error %q should mention the LLM call", err.Error())
	}
}

// TestParser_LLMFallback_ContextIncluded verifies that the
// world context (people, locations) is included in the
// system prompt. This is what lets the LLM disambiguate
// "talk to the blacksmith" → target="bob".
func TestParser_LLMFallback_ContextIncluded(t *testing.T) {
	mock := &mockLLM{response: `{"action": "look", "target": "alice"}`}
	p := New(mock, newTestWorld())

	_, err := p.Parse(context.Background(), "who's around?")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(mock.lastMsgs) < 1 {
		t.Fatal("LLM received no messages")
	}
	sys := mock.lastMsgs[0].Content
	if !strings.Contains(sys, "Alice") {
		t.Errorf("system prompt missing person 'Alice': %q", sys)
	}
	if !strings.Contains(sys, "Blackwater") {
		t.Errorf("system prompt missing location 'Blackwater': %q", sys)
	}
}

// TestValidateLLMResponse exercises the schema validation
// gate directly. This is the choke point — every LLM
// response flows through it.
func TestValidateLLMResponse(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
		wantAct Action
	}{
		{"valid talk", `{"action": "talk", "target": "alice"}`, false, ActionTalk},
		{"valid with args", `{"action": "sleep", "args": {"hours": 12}}`, false, ActionSleep},
		{"empty action", `{"action": ""}`, true, ActionUnknown},
		{"unknown action", `{"action": "fly"}`, true, ActionUnknown},
		{"malformed json", `not json`, true, ActionUnknown},
		{"missing action field", `{"target": "alice"}`, true, ActionUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := validateLLMResponse(c.input)
			if c.wantErr {
				if err == nil {
					t.Errorf("validateLLMResponse(%q): got nil error, want non-nil", c.input)
				}
				return
			}
			if err != nil {
				t.Errorf("validateLLMResponse(%q): %v", c.input, err)
				return
			}
			if got.Action != c.wantAct {
				t.Errorf("validateLLMResponse(%q).Action = %q, want %q",
					c.input, got.Action, c.wantAct)
			}
		})
	}
}

// TestIsKnownAction exhaustively verifies that IsKnownAction
// accepts the 12 spec verbs and rejects everything else.
func TestIsKnownAction(t *testing.T) {
	for _, a := range AllActions() {
		if !IsKnownAction(a) {
			t.Errorf("IsKnownAction(%q) = false, want true", a)
		}
	}
	for _, bad := range []Action{"", "fly", "Look", "LOOK", "talk ", "talkto"} {
		if IsKnownAction(bad) {
			t.Errorf("IsKnownAction(%q) = true, want false", bad)
		}
	}
}

// TestParser_AliasCoverage verifies that the alias table
// entries from types.go actually route to their canonical
// Action. This catches drift if someone adds an alias to
// the table but forgets to wire it into parseRule.
func TestParser_AliasCoverage(t *testing.T) {
	p := New(nil, newTestWorld())
	cases := []struct {
		in     string
		action Action
		target string
	}{
		// save alias
		{"snapshot myrun.db", ActionSave, "myrun.db"},
		// branch alias
		{"fork before_war", ActionBranch, "before_war"},
		// switch aliases
		{"checkout before_war", ActionSwitch, "before_war"},
		{"goto before_war", ActionSwitch, "before_war"},
	}
	for _, c := range cases {
		got, err := p.Parse(context.Background(), c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got.Action != c.action {
			t.Errorf("Parse(%q).Action = %q, want %q", c.in, got.Action, c.action)
		}
		if got.Target != c.target {
			t.Errorf("Parse(%q).Target = %q, want %q", c.in, got.Target, c.target)
		}
	}
}
