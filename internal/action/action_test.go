package action

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
)

// newTestWorld builds a small world with 2 people and 2
// locations for predictable output assertions. The player
// is "alice" by default; tests that want a different player
// (or no player) set w.PlayerID directly.
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

// TestResolve_Time verifies that the time action returns
// the current tick and date, with no world changes.
func TestResolve_Time(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTime})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.OK {
		t.Errorf("time: OK = false, want true; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "tick 100") {
		t.Errorf("time output missing 'tick 100': %q", res.Text)
	}
	if w.Tick != 100 {
		t.Errorf("time advanced tick: w.Tick = %d, want 100", w.Tick)
	}
}

// TestResolve_Inventory verifies that the inventory stub
// returns a clear "nothing" message.
func TestResolve_Inventory(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionInventory})
	if !res.OK {
		t.Errorf("inventory: OK = false; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "nothing") {
		t.Errorf("inventory output missing 'nothing': %q", res.Text)
	}
}

// TestResolve_LookPerson verifies that "look <name>" finds
// the person by name (case-insensitive) and shows their
// details.
func TestResolve_LookPerson(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionLook, Target: "alice"})
	if !res.OK {
		t.Errorf("look alice: OK = false; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "Alice") {
		t.Errorf("look alice output missing 'Alice': %q", res.Text)
	}
}

// TestResolve_LookUnknown verifies that looking for an
// unknown target returns OK=false with a clear message.
func TestResolve_LookUnknown(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionLook, Target: "nobody"})
	if res.OK {
		t.Errorf("look nobody: OK = true, want false")
	}
	if !strings.Contains(res.Text, "I don't see") {
		t.Errorf("look unknown output missing 'I don't see': %q", res.Text)
	}
}

// TestResolve_Inspect verifies that "inspect <name>" shows
// the person's details.
func TestResolve_Inspect(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionInspect, Target: "alice"})
	if !res.OK {
		t.Errorf("inspect alice: OK = false; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "Alice") {
		t.Errorf("inspect alice output missing 'Alice': %q", res.Text)
	}
}

// TestResolve_Talk_CreatesMemory verifies that talking to
// someone creates a memory record.
func TestResolve_Talk_CreatesMemory(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	memCount := len(w.Memories)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTalk, Target: "alice"})
	if !res.OK {
		t.Errorf("talk alice: OK = false; Text = %q", res.Text)
	}
	if len(w.Memories) != memCount+1 {
		t.Errorf("memories after talk = %d, want %d", len(w.Memories), memCount+1)
	}
	// Verify the memory has the expected fields.
	mem := w.Memories[len(w.Memories)-1]
	if mem.OwnerID != "alice" {
		t.Errorf("memory OwnerID = %q, want 'alice'", mem.OwnerID)
	}
	if !strings.Contains(mem.Description, "Alice") {
		t.Errorf("memory description missing 'Alice': %q", mem.Description)
	}
}

// TestResolve_Talk_TrustDelta verifies that talking to
// someone applies a TrustDelta to the relationship.
func TestResolve_Talk_TrustDelta(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTalk, Target: "bob"})
	if !res.OK {
		t.Fatalf("talk bob: OK = false; Text = %q", res.Text)
	}
	// Find the alice→bob relationship (should be created
	// with Trust = 50 + TrustDelta = 52).
	found := false
	for _, rel := range w.Relationships {
		if rel.FromID == "alice" && rel.ToID == "bob" {
			found = true
			if rel.Trust != 52 {
				t.Errorf("alice→bob Trust = %f, want 52 (50 + 2.0 TrustDelta)", rel.Trust)
			}
		}
	}
	if !found {
		t.Errorf("alice→bob relationship not created")
	}
}

// TestResolve_Talk_UnknownTarget verifies that talking to
// a non-existent person returns OK=false.
func TestResolve_Talk_UnknownTarget(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTalk, Target: "nobody"})
	if res.OK {
		t.Errorf("talk nobody: OK = true, want false")
	}
}

// TestResolve_Talk_DeadPerson verifies that talking to a
// dead person is rejected.
func TestResolve_Talk_DeadPerson(t *testing.T) {
	w := newTestWorld()
	w.People["bob"].Alive = false
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTalk, Target: "bob"})
	if res.OK {
		t.Errorf("talk bob (dead): OK = true, want false")
	}
	if !strings.Contains(res.Text, "dead") {
		t.Errorf("talk dead output missing 'dead': %q", res.Text)
	}
}

// TestResolve_Travel_MovesPlayer verifies that travel moves
// the player to the destination and advances time by 1.
func TestResolve_Travel_MovesPlayer(t *testing.T) {
	w := newTestWorld()
	player := w.People["alice"]
	if player.LocationID != "blackwater" {
		t.Fatalf("setup: alice at %q, want 'blackwater'", player.LocationID)
	}
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTravel, Target: "ashford"})
	if !res.OK {
		t.Fatalf("travel ashford: OK = false; Text = %q", res.Text)
	}
	if player.LocationID != "ashford" {
		t.Errorf("alice at %q after travel, want 'ashford'", player.LocationID)
	}
	if res.TicksAdvanced != 1 {
		t.Errorf("TicksAdvanced = %d, want 1", res.TicksAdvanced)
	}
	if w.Tick != 101 {
		t.Errorf("w.Tick = %d after travel, want 101", w.Tick)
	}
}

// TestResolve_Travel_NoPlayer verifies that travel without
// a player is rejected.
func TestResolve_Travel_NoPlayer(t *testing.T) {
	w := newTestWorld()
	w.PlayerID = ""
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTravel, Target: "ashford"})
	if res.OK {
		t.Errorf("travel without player: OK = true, want false")
	}
	if !strings.Contains(res.Text, "player") {
		t.Errorf("travel no-player output missing 'player': %q", res.Text)
	}
}

// TestResolve_Travel_UnknownLocation verifies that travel
// to an unknown location is rejected.
func TestResolve_Travel_UnknownLocation(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTravel, Target: "nowhere"})
	if res.OK {
		t.Errorf("travel nowhere: OK = true, want false")
	}
}

// TestResolve_Travel_AlreadyThere verifies that traveling
// to the player's current location is rejected.
func TestResolve_Travel_AlreadyThere(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTravel, Target: "blackwater"})
	if res.OK {
		t.Errorf("travel to current location: OK = true, want false")
	}
	if !strings.Contains(res.Text, "already") {
		t.Errorf("travel already-there output missing 'already': %q", res.Text)
	}
}

// TestResolve_Travel_CreatesMemory verifies that travel
// creates a memory record.
func TestResolve_Travel_CreatesMemory(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	memCount := len(w.Memories)
	_, _ = eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTravel, Target: "ashford"})
	if len(w.Memories) != memCount+1 {
		t.Errorf("memories after travel = %d, want %d", len(w.Memories), memCount+1)
	}
	mem := w.Memories[len(w.Memories)-1]
	if mem.OwnerID != "alice" {
		t.Errorf("travel memory OwnerID = %q, want 'alice'", mem.OwnerID)
	}
	if !strings.Contains(mem.Description, "Ashford") {
		t.Errorf("travel memory description missing 'Ashford': %q", mem.Description)
	}
}

// TestResolve_Sleep_AdvancesTime verifies that sleep advances
// the tick by the expected number of ticks.
func TestResolve_Sleep_AdvancesTime(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: 48}})
	if !res.OK {
		t.Errorf("sleep 48: OK = false; Text = %q", res.Text)
	}
	if res.TicksAdvanced != 2 {
		t.Errorf("TicksAdvanced = %d, want 2 (48h = 2 days)", res.TicksAdvanced)
	}
	if w.Tick != 102 {
		t.Errorf("w.Tick = %d after sleep, want 102", w.Tick)
	}
}

// TestResolve_Sleep_DefaultHours verifies that sleep with
// no hours defaults to 8.
func TestResolve_Sleep_DefaultHours(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep})
	if !res.OK {
		t.Errorf("sleep: OK = false; Text = %q", res.Text)
	}
	// 8h → 8/24 = 0 ticks → rounded up to 1.
	if res.TicksAdvanced != 1 {
		t.Errorf("TicksAdvanced = %d, want 1 (8h rounds up to 1 tick)", res.TicksAdvanced)
	}
}

// TestResolve_Sleep_ClampedToWeek verifies that sleep is
// clamped to a week.
func TestResolve_Sleep_ClampedToWeek(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: 99999}})
	if !res.OK {
		t.Errorf("sleep 99999: OK = false; Text = %q", res.Text)
	}
	// 99999h clamped to 7*24 = 168h → 7 ticks.
	if res.TicksAdvanced != 7 {
		t.Errorf("TicksAdvanced = %d, want 7 (168h clamped)", res.TicksAdvanced)
	}
}

// TestResolve_UnknownAction verifies that an unknown action
// returns an error.
func TestResolve_UnknownAction(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	_, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.Action("frobnicate")})
	if err == nil {
		t.Errorf("Resolve with unknown action: err = nil, want error")
	}
}

// TestResolve_NilWorld verifies that a nil world returns
// an error.
func TestResolve_NilWorld(t *testing.T) {
	eng := New(nil, nil)
	_, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTime})
	if err == nil {
		t.Errorf("Resolve with nil world: err = nil, want error")
	}
}

// TestApplyTalkDelta_ExistingRelationship verifies that
// applying a talk delta to an existing relationship adds
// the trust delta to the existing trust value.
func TestApplyTalkDelta_ExistingRelationship(t *testing.T) {
	w := newTestWorld()
	// Pre-create a relationship alice→bob with Trust=50.
	w.Relationships = append(w.Relationships, core.Relationship{
		FromID: "alice",
		ToID:   "bob",
		Trust:  50,
	})
	mem := core.Memory{OwnerID: "alice", TrustDelta: 5.0}
	applyTalkDelta(w, mem, "bob")
	for _, rel := range w.Relationships {
		if rel.FromID == "alice" && rel.ToID == "bob" {
			if rel.Trust != 55 {
				t.Errorf("alice→bob Trust = %f, want 55 (50 + 5.0)", rel.Trust)
			}
			return
		}
	}
	t.Errorf("alice→bob relationship not found")
}
