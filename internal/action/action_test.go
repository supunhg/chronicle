package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
	"github.com/chronicle-dev/chronicle/internal/persistence"
)

// newTestWorld builds a small world with 2 people and 2
// locations for predictable output assertions. The player
// is "alice" by default; tests that want a different player
// (or no player) set w.PlayerID directly. Phase 18: the
// world's item catalog (w.Items) is populated with the
// Phase 17.6 default goods so the action engine's buy/sell
// handlers have a catalog to look up.
func newTestWorld() *core.World {
	w := core.NewWorld("test", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "ashford", Name: "Ashford", PopulationCap: 50})
	w.AddPerson(&core.Person{ID: "alice", Name: "Alice", Alive: true, Gender: "F", BirthTick: -20 * 365, LocationID: "blackwater"})
	w.AddPerson(&core.Person{ID: "bob", Name: "Bob", Alive: true, Gender: "M", BirthTick: -30 * 365, LocationID: "ashford"})
	w.PlayerID = "alice"
	w.Tick = 100
	w.Items = defaultTestCatalog()
	w.RecomputeLocationPopulations()
	return w
}

// defaultTestCatalog returns a minimal Phase 18 item catalog
// for action tests: the same 12 common goods from the
// Phase 17.6 hardcoded priceList, with Phase 18 metadata
// (weight, max_durability) added. The action engine looks
// up the per-item Value from this catalog.
func defaultTestCatalog() map[string]core.Item {
	return map[string]core.Item{
		"bread":  {Name: "bread", Weight: 0.5, Value: 3, MaxDurability: 0.0},
		"ale":    {Name: "ale", Weight: 1.0, Value: 2, MaxDurability: 0.0},
		"meat":   {Name: "meat", Weight: 1.0, Value: 8, MaxDurability: 0.0},
		"apple":  {Name: "apple", Weight: 0.2, Value: 1, MaxDurability: 0.0},
		"cheese": {Name: "cheese", Weight: 0.4, Value: 5, MaxDurability: 0.0},
		"rope":   {Name: "rope", Weight: 1.0, Value: 4, MaxDurability: 0.5},
		"torch":  {Name: "torch", Weight: 0.5, Value: 2, MaxDurability: 0.5},
		"bed":    {Name: "bed", Weight: 30.0, Value: 15, MaxDurability: 1.0},
		"sword":  {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
		"shield": {Name: "shield", Weight: 6.0, Value: 35, MaxDurability: 1.0},
		"potion": {Name: "potion", Weight: 0.3, Value: 20, MaxDurability: 0.0},
		"book":   {Name: "book", Weight: 0.5, Value: 10, MaxDurability: 1.0},
	}
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

// TestResolve_Save verifies that save writes a valid SQLite
// snapshot that can be restored.
func TestResolve_Save(t *testing.T) {
	w := newTestWorld()
	w.Coin = 42
	w.Inventory["bread"] = core.Item{Name: "bread", Count: 3, Weight: 0.5, Value: 3}
	eng := New(w, nil)
	path := filepath.Join(t.TempDir(), "save.db")
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSave, Target: path})
	if !res.OK {
		t.Fatalf("save: OK = false; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "Saved to "+path) {
		t.Errorf("save output missing 'Saved to %s': %q", path, res.Text)
	}
	// Verify the file exists and is non-empty.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Errorf("save produced empty file at %s", path)
	}
	// Verify it can be opened and migrated.
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatalf("reopen %s: %v", path, err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Errorf("reopen migrate: %v", err)
	}
}

// TestResolve_SaveDefaultPath verifies that bare "save"
// defaults to <world-id>.db.
func TestResolve_SaveDefaultPath(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSave})
	if !res.OK {
		t.Fatalf("save default: OK = false; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "test.db") {
		t.Errorf("save default output missing 'test.db': %q", res.Text)
	}
}

// TestResolve_SaveBadPath verifies that save to a bad
// path returns OK=false.
func TestResolve_SaveBadPath(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSave, Target: "/nonexistent/dir/save.db"})
	if res.OK {
		t.Errorf("save bad path: OK = true, want false; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "save:") {
		t.Errorf("save bad path output missing 'save:' error prefix: %q", res.Text)
	}
}

// TestResolve_Buy verifies that buy deducts Coin and adds
// to Inventory.
func TestResolve_Buy(t *testing.T) {
	w := newTestWorld()
	w.Coin = 100
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBuy, Target: "bread", Args: intent.Args{Quantity: 2}})
	if !res.OK {
		t.Fatalf("buy bread: OK = false; Text = %q", res.Text)
	}
	if w.Coin != 94 { // 100 - 2*3 = 94
		t.Errorf("Coin after buy = %d, want 94", w.Coin)
	}
	if w.Inventory["bread"].Count != 2 {
		t.Errorf("Inventory[bread].Count = %d, want 2", w.Inventory["bread"].Count)
	}
	if !strings.Contains(res.Text, "94") {
		t.Errorf("buy output missing new coin balance '94': %q", res.Text)
	}
}

// TestResolve_BuyCopiesMetadata verifies that the buy copies
// the catalog's Weight, Value, and MaxDurability into the
// inventory stack.
func TestResolve_BuyCopiesMetadata(t *testing.T) {
	w := newTestWorld()
	w.Coin = 100
	eng := New(w, nil)
	_, _ = eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBuy, Target: "sword"})
	stack := w.Inventory["sword"]
	if stack.Weight != 4.0 {
		t.Errorf("sword.Weight = %f, want 4.0", stack.Weight)
	}
	if stack.Value != 50 {
		t.Errorf("sword.Value = %d, want 50", stack.Value)
	}
	if stack.MaxDurability != 1.0 {
		t.Errorf("sword.MaxDurability = %f, want 1.0", stack.MaxDurability)
	}
}

// TestResolve_BuyDefaultQuantity verifies that buy with
// no quantity defaults to 1.
func TestResolve_BuyDefaultQuantity(t *testing.T) {
	w := newTestWorld()
	w.Coin = 10
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBuy, Target: "apple"})
	if !res.OK {
		t.Fatalf("buy apple: OK = false; Text = %q", res.Text)
	}
	if w.Coin != 9 { // 10 - 1*1 = 9
		t.Errorf("Coin after buy = %d, want 9", w.Coin)
	}
	if w.Inventory["apple"].Count != 1 {
		t.Errorf("Inventory[apple].Count = %d, want 1", w.Inventory["apple"].Count)
	}
}

// TestResolve_BuyInsufficientFunds verifies that buy is
// rejected when the player can't afford it.
func TestResolve_BuyInsufficientFunds(t *testing.T) {
	w := newTestWorld()
	w.Coin = 1
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBuy, Target: "sword"})
	if res.OK {
		t.Errorf("buy sword with 1 coin: OK = true, want false")
	}
	if !strings.Contains(res.Text, "afford") {
		t.Errorf("buy insufficient output missing 'afford': %q", res.Text)
	}
}

// TestResolve_BuyUnknownItem verifies that buy of an
// unknown item is rejected.
func TestResolve_BuyUnknownItem(t *testing.T) {
	w := newTestWorld()
	w.Coin = 100
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBuy, Target: "unicorn"})
	if res.OK {
		t.Errorf("buy unicorn: OK = true, want false")
	}
	if !strings.Contains(res.Text, "price") {
		t.Errorf("buy unknown output missing 'price': %q", res.Text)
	}
}

// TestResolve_Sell verifies that sell adds Coin and removes
// from Inventory.
func TestResolve_Sell(t *testing.T) {
	w := newTestWorld()
	w.Coin = 0
	w.Inventory["bread"] = core.Item{Name: "bread", Count: 3, Weight: 0.5, Value: 3}
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSell, Target: "bread", Args: intent.Args{Quantity: 2}})
	if !res.OK {
		t.Fatalf("sell bread: OK = false; Text = %q", res.Text)
	}
	if w.Coin != 6 { // 0 + 2*3 = 6
		t.Errorf("Coin after sell = %d, want 6", w.Coin)
	}
	if w.Inventory["bread"].Count != 1 {
		t.Errorf("Inventory[bread].Count = %d, want 1", w.Inventory["bread"].Count)
	}
}

// TestResolve_SellRemovesEmptyEntry verifies that selling
// the last of an item removes the inventory entry.
func TestResolve_SellRemovesEmptyEntry(t *testing.T) {
	w := newTestWorld()
	w.Coin = 0
	w.Inventory["apple"] = core.Item{Name: "apple", Count: 1, Weight: 0.2, Value: 1}
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSell, Target: "apple"})
	if !res.OK {
		t.Fatalf("sell apple: OK = false; Text = %q", res.Text)
	}
	if _, exists := w.Inventory["apple"]; exists {
		t.Errorf("Inventory[apple] still exists after selling last one")
	}
}

// TestResolve_SellInsufficientInventory verifies that sell
// is rejected when the player doesn't have enough.
func TestResolve_SellInsufficientInventory(t *testing.T) {
	w := newTestWorld()
	w.Coin = 0
	w.Inventory["bread"] = core.Item{Name: "bread", Count: 1, Weight: 0.5, Value: 3}
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSell, Target: "bread", Args: intent.Args{Quantity: 5}})
	if res.OK {
		t.Errorf("sell 5 bread with 1: OK = true, want false")
	}
	if !strings.Contains(res.Text, "only have") {
		t.Errorf("sell insufficient output missing 'only have': %q", res.Text)
	}
}

// TestResolve_Inventory_Empty verifies that the inventory
// stub returns a clear "nothing" message when empty.
func TestResolve_Inventory_Empty(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionInventory})
	if !res.OK {
		t.Errorf("inventory: OK = false")
	}
	if !strings.Contains(res.Text, "nothing") {
		t.Errorf("empty inventory output missing 'nothing': %q", res.Text)
	}
	if !strings.Contains(res.Text, "Coin: 0") {
		t.Errorf("empty inventory output missing 'Coin: 0': %q", res.Text)
	}
}

// TestResolve_Inventory_WithItems verifies that the
// inventory shows items sorted by name with counts.
func TestResolve_Inventory_WithItems(t *testing.T) {
	w := newTestWorld()
	w.Coin = 42
	w.Inventory["sword"] = core.Item{Name: "sword", Count: 1, Weight: 4.0, Value: 50, MaxDurability: 1.0}
	w.Inventory["bread"] = core.Item{Name: "bread", Count: 3, Weight: 0.5, Value: 3}
	w.Inventory["apple"] = core.Item{Name: "apple", Count: 5, Weight: 0.2, Value: 1}
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionInventory})
	if !res.OK {
		t.Errorf("inventory: OK = false")
	}
	// Items should be sorted: apple, bread, sword.
	if !strings.Contains(res.Text, "apple x5") {
		t.Errorf("inventory output missing 'apple x5': %q", res.Text)
	}
	if !strings.Contains(res.Text, "bread x3") {
		t.Errorf("inventory output missing 'bread x3': %q", res.Text)
	}
	if !strings.Contains(res.Text, "sword x1") {
		t.Errorf("inventory output missing 'sword x1': %q", res.Text)
	}
	if !strings.Contains(res.Text, "Coin: 42") {
		t.Errorf("inventory output missing 'Coin: 42': %q", res.Text)
	}
}

// TestResolve_Branch_CreatesFile verifies that branch
// saves the current world to branches/<name>.db.
func TestResolve_Branch_CreatesFile(t *testing.T) {
	w := newTestWorld()
	w.Coin = 99
	eng := New(w, nil)
	// Use t.TempDir() as the CWD so branches/ is created
	// in a clean location.
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBranch, Target: "before_war"})
	if !res.OK {
		t.Fatalf("branch before_war: OK = false; Text = %q", res.Text)
	}
	if !strings.Contains(res.Text, "before_war") {
		t.Errorf("branch output missing 'before_war': %q", res.Text)
	}
	// Verify the file exists.
	path := filepath.Join("branches", "before_war.db")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Verify the branches/ directory was created.
	if _, err := os.Stat("branches"); err != nil {
		t.Errorf("branches/ directory not created: %v", err)
	}
}

// TestResolve_Branch_EmptyName verifies that branch with
// no name is rejected.
func TestResolve_Branch_EmptyName(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBranch})
	if res.OK {
		t.Errorf("branch with no name: OK = true, want false")
	}
}

// TestResolve_Branch_InvalidName verifies that branch
// with path-traversal or invalid names is rejected.
func TestResolve_Branch_InvalidName(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	badNames := []string{
		"../etc/passwd",
		"foo/bar",
		"foo\\bar",
		".",
		"..",
		".hidden",
	}
	for _, name := range badNames {
		res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBranch, Target: name})
		if res.OK {
			t.Errorf("branch %q: OK = true, want false (invalid name)", name)
		}
	}
}

// TestResolve_Switch_RestoresWorld verifies that switch
// restores the world state from a branch file.
func TestResolve_Switch_RestoresWorld(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	// Branch the initial state.
	_, _ = eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBranch, Target: "snapshot"})

	// Mutate the world: move alice to ashford, give her 50 coin.
	w.People["alice"].LocationID = "ashford"
	w.Coin = 50

	// Switch back to the branch.
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSwitch, Target: "snapshot"})
	if !res.OK {
		t.Fatalf("switch snapshot: OK = false; Text = %q", res.Text)
	}
	// The world should be restored: alice at blackwater, Coin=0.
	if w.People["alice"].LocationID != "blackwater" {
		t.Errorf("alice at %q after switch, want 'blackwater'", w.People["alice"].LocationID)
	}
	if w.Coin != 0 {
		t.Errorf("Coin = %d after switch, want 0", w.Coin)
	}
}

// TestResolve_Switch_UnknownBranch verifies that switch
// to a non-existent branch is rejected.
func TestResolve_Switch_UnknownBranch(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSwitch, Target: "nonexistent"})
	if res.OK {
		t.Errorf("switch nonexistent: OK = true, want false")
	}
	if !strings.Contains(res.Text, "does not exist") {
		t.Errorf("switch unknown output missing 'does not exist': %q", res.Text)
	}
}

// TestResolve_Switch_EmptyName verifies that switch with
// no name is rejected.
func TestResolve_Switch_EmptyName(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSwitch})
	if res.OK {
		t.Errorf("switch with no name: OK = true, want false")
	}
}

// TestResolve_Switch_InvalidName verifies that switch
// with path-traversal names is rejected.
func TestResolve_Switch_InvalidName(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSwitch, Target: "../etc/passwd"})
	if res.OK {
		t.Errorf("switch ../etc/passwd: OK = true, want false")
	}
}

// TestResolve_BranchSwitchRoundTrip verifies the full
// branch → mutate → switch back → mutation reverted flow.
func TestResolve_BranchSwitchRoundTrip(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	tmpDir := t.TempDir()
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	// 1. Branch the initial state.
	_, _ = eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionBranch, Target: "v1"})

	// 2. Mutate the world heavily.
	w.People["alice"].LocationID = "ashford"
	w.Coin = 100
	w.Inventory["sword"] = core.Item{Name: "sword", Count: 1, Weight: 4.0, Value: 50, MaxDurability: 1.0}
	aliceRel := w.People["alice"]
	_ = aliceRel
	w.Memories = append(w.Memories, core.Memory{
		ID: "test-mem", OwnerID: "alice", Tick: w.Tick, Description: "test",
	})

	// 3. Switch back to v1.
	_, _ = eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSwitch, Target: "v1"})

	// 4. Verify all mutations were reverted.
	if w.People["alice"].LocationID != "blackwater" {
		t.Errorf("alice at %q after round-trip, want 'blackwater'", w.People["alice"].LocationID)
	}
	if w.Coin != 0 {
		t.Errorf("Coin = %d after round-trip, want 0", w.Coin)
	}
	if w.Inventory["sword"].Count != 0 {
		t.Errorf("Inventory[sword].Count = %d after round-trip, want 0", w.Inventory["sword"].Count)
	}
	if len(w.Memories) != 0 {
		t.Errorf("Memories count = %d after round-trip, want 0", len(w.Memories))
	}
}

// TestIsValidBranchName exhaustively checks the branch
// name validator.
func TestIsValidBranchName(t *testing.T) {
	good := []string{"v1", "before_war", "main", "experiment-1", "branch_42"}
	for _, name := range good {
		if !isValidBranchName(name) {
			t.Errorf("isValidBranchName(%q) = false, want true", name)
		}
	}
	bad := []string{"", ".", "..", "../etc/passwd", "foo/bar", "foo\\bar", ".hidden", "foo\x00bar"}
	for _, name := range bad {
		if isValidBranchName(name) {
			t.Errorf("isValidBranchName(%q) = true, want false", name)
		}
	}
}
