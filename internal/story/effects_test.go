package story

import (
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// TestSetFlag_OnNilMap verifies SetFlag.Apply defensively
// initialises the Flags map if absent.
func TestSetFlag_OnNilMap(t *testing.T) {
	ws := state.WorldState{} // zero - Flags is nil
	eff := SetFlag{Key: "x"}
	if err := eff.Apply(&ws); err != nil {
		t.Fatalf("SetFlag.Apply: got err %v; want nil", err)
	}
	if !ws.Flags["x"] {
		t.Error("after SetFlag: Flags[x] = false; want true")
	}
}

// TestClearFlag_RemovesKey verifies ClearFlag deletes the key
// rather than setting it to false.
func TestClearFlag_RemovesKey(t *testing.T) {
	ws := state.NewWorldState()
	ws.Flags["x"] = true
	eff := ClearFlag{Key: "x"}
	if err := eff.Apply(&ws); err != nil {
		t.Fatalf("ClearFlag.Apply: got err %v; want nil", err)
	}
	if _, present := ws.Flags["x"]; present {
		t.Errorf("after ClearFlag: x still in map; want deleted")
	}
	// Idempotent — clearing an absent key returns nil.
	absentEff := ClearFlag{Key: "absent"}
	if err := absentEff.Apply(&ws); err != nil {
		t.Errorf("ClearFlag on absent key: got err %v; want nil", err)
	}
}

// TestModifyVariable_AbsoluteReplace verifies ModifyVariable
// sets the variable to the absolute Value, regardless of the
// prior value.
func TestModifyVariable_AbsoluteReplace(t *testing.T) {
	ws := state.NewWorldState()
	ws.Variables["courage"] = 30
	eff := ModifyVariable{Key: "courage", Value: 90}
	if err := eff.Apply(&ws); err != nil {
		t.Fatalf("ModifyVariable.Apply: got err %v; want nil", err)
	}
	if ws.Variables["courage"] != 90 {
		t.Errorf("after ModifyVariable(90): courage = %d; want 90 (absolute replace)", ws.Variables["courage"])
	}
	// On nil map.
	ws0 := state.WorldState{}
	freshEff := ModifyVariable{Key: "fresh", Value: 5}
	if err := freshEff.Apply(&ws0); err != nil {
		t.Errorf("ModifyVariable on nil map: got err %v; want nil", err)
	}
	if ws0.Variables["fresh"] != 5 {
		t.Error("after nil-map ModifyVariable: fresh = 5 mismatched")
	}
}

// TestModifyRelationship_ByAxis verifies each axis is updated
// independently of the others.
func TestModifyRelationship_ByAxis(t *testing.T) {
	ws := state.NewWorldState()
	ws.Relationships["elara"] = state.Relationship{Trust: 30, Affection: 20, Respect: 10}

	eff := ModifyRelationship{Character: "elara", Axis: AxisTrust, Value: 80}
	if err := eff.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	rel := ws.Relationships["elara"]
	if rel.Trust != 80 {
		t.Errorf("Trust = %d; want 80", rel.Trust)
	}
	if rel.Affection != 20 {
		t.Errorf("Affection = %d; want 20 (unchanged)", rel.Affection)
	}
	if rel.Respect != 10 {
		t.Errorf("Respect = %d; want 10 (unchanged)", rel.Respect)
	}
}

// TestModifyRelationship_Clamp verifies values outside
// [-100, +100] are clamped per §9 invariant. Covers exact
// boundary points and inset values just outside the boundary.
func TestModifyRelationship_Clamp(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{-200, -100},
		{-101, -100}, // inset below range
		{-100, -100},
		{0, 0},
		{100, 100},
		{101, 100}, // inset above range
		{200, 100},
	}
	for _, c := range cases {
		ws := state.NewWorldState()
		eff := ModifyRelationship{Character: "elara", Axis: AxisTrust, Value: c.in}
		if err := eff.Apply(&ws); err != nil {
			t.Fatal(err)
		}
		if ws.Relationships["elara"].Trust != c.want {
			t.Errorf("ModifyRelationship(value=%d): Trust = %d; want %d (clamped)",
				c.in, ws.Relationships["elara"].Trust, c.want)
		}
	}
}

// TestModifyReputation_ByFaction verifies each faction is
// updated independently.
func TestModifyReputation_ByFaction(t *testing.T) {
	ws := state.NewWorldState()
	kingdomEff := ModifyReputation{Faction: FactionKingdom, Value: 50}
	if err := kingdomEff.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	dragonsEff := ModifyReputation{Faction: FactionDragons, Value: -50}
	if err := dragonsEff.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	if ws.Reputation.Kingdom != 50 {
		t.Errorf("Kingdom = %d; want 50", ws.Reputation.Kingdom)
	}
	if ws.Reputation.Dragons != -50 {
		t.Errorf("Dragons = %d; want -50", ws.Reputation.Dragons)
	}
	if ws.Reputation.Mages != 0 || ws.Reputation.Underworld != 0 {
		t.Errorf("untouched factions changed: Mages = %d, Underworld = %d; want 0,0", ws.Reputation.Mages, ws.Reputation.Underworld)
	}
}

// TestModifyReputation_Clamp at the [-100, +100] boundary
// including inset values just outside.
func TestModifyReputation_Clamp(t *testing.T) {
	cases := []struct {
		faction Faction
		in      int
		want    int
	}{
		{FactionMages, 200, 100},
		{FactionMages, 101, 100}, // inset above
		{FactionUnderworld, -200, -100},
		{FactionUnderworld, -101, -100}, // inset below
		{FactionKingdom, 50, 50},       // in-range (no clamp)
	}
	for _, c := range cases {
		ws := state.NewWorldState()
		eff := ModifyReputation{Faction: c.faction, Value: c.in}
		if err := eff.Apply(&ws); err != nil {
			t.Fatal(err)
		}
		var got int
		switch c.faction {
		case FactionKingdom:
			got = ws.Reputation.Kingdom
		case FactionMages:
			got = ws.Reputation.Mages
		case FactionDragons:
			got = ws.Reputation.Dragons
		case FactionUnderworld:
			got = ws.Reputation.Underworld
		}
		if got != c.want {
			t.Errorf("ModifyReputation(faction=%s, value=%d): got %d; want %d",
				c.faction, c.in, got, c.want)
		}
	}
}

// TestAddItem_Increments verifies AddItem increments each call.
func TestAddItem_Increments(t *testing.T) {
	ws := state.NewWorldState()
	first := AddItem{Key: "sword", Count: 1}
	if err := first.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	second := AddItem{Key: "sword", Count: 2}
	if err := second.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	if ws.Inventory.Items["sword"] != 3 {
		t.Errorf("after 1+2: sword = %d; want 3", ws.Inventory.Items["sword"])
	}
}

// TestAddItem_NegativeError verifies Count < 0 returns an
// error so authoring mistakes are loud, not silent.
func TestAddItem_NegativeError(t *testing.T) {
	ws := state.NewWorldState()
	neg := AddItem{Key: "x", Count: -1}
	if err := neg.Apply(&ws); err == nil {
		t.Error("AddItem(-1): got nil err; want non-nil")
	}
}

// TestRemoveItem_Decrement verifies RemoveItem subtracts
// Count from Inventory.Items[Key] without clamping below.
func TestRemoveItem_Decrement(t *testing.T) {
	ws := state.NewWorldState()
	ws.Inventory.Items["sword"] = 5
	eff := RemoveItem{Key: "sword", Count: 2}
	if err := eff.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	if ws.Inventory.Items["sword"] != 3 {
		t.Errorf("5-2: sword = %d; want 3", ws.Inventory.Items["sword"])
	}
}

// TestRemoveItem_ClampAtZero verifies over-removal deletes
// the key entirely. A player with 1 sword losing 2 ends up
// with no sword entry, never -1 sword.
func TestRemoveItem_ClampAtZero(t *testing.T) {
	ws := state.NewWorldState()
	ws.Inventory.Items["sword"] = 1
	eff := RemoveItem{Key: "sword", Count: 5}
	if err := eff.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	if _, present := ws.Inventory.Items["sword"]; present {
		t.Errorf("after over-removal: sword still present; want deleted")
	}
}

// TestRemoveItem_NegativeError verifies Count < 0 errors.
func TestRemoveItem_NegativeError(t *testing.T) {
	ws := state.NewWorldState()
	ws.Inventory.Items["x"] = 1
	neg := RemoveItem{Key: "x", Count: -1}
	if err := neg.Apply(&ws); err == nil {
		t.Error("RemoveItem(-1): got nil err; want non-nil")
	}
}

// TestTriggerEvent_AppendsToQueue verifies TriggerEvent
// appends to WorldState.TriggeredEvents in declaration order.
// Phase 36.D will read this queue at the engine's "Check Events"
// step.
func TestTriggerEvent_AppendsToQueue(t *testing.T) {
	ws := state.NewWorldState()
	first := TriggerEvent{ID: "elara_confession"}
	if err := first.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	second := TriggerEvent{ID: "dragon_attack"}
	if err := second.Apply(&ws); err != nil {
		t.Fatal(err)
	}
	want := []string{"elara_confession", "dragon_attack"}
	if len(ws.TriggeredEvents) != 2 || ws.TriggeredEvents[0] != want[0] || ws.TriggeredEvents[1] != want[1] {
		t.Errorf("TriggeredEvents = %v; want %v", ws.TriggeredEvents, want)
	}
}

// TestFaction_StringRoundTrip guards the parser<->printer pair
// against drift — new factions must add to both String and
// ParseFaction.
func TestFaction_StringRoundTrip(t *testing.T) {
	for _, s := range []string{"kingdom", "mages", "dragons", "underworld"} {
		f, ok := ParseFaction(s)
		if !ok {
			t.Errorf("ParseFaction(%q): got !ok; want ok", s)
		}
		if f.String() != s {
			t.Errorf("Parse(%q).String(): got %q; want round-trip equals %q", s, f.String(), s)
		}
	}
	if _, ok := ParseFaction("nope"); ok {
		t.Error(`ParseFaction("nope"): got ok; want !ok`)
	}
}
