package story

import (
	"testing"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// TestFlag_AbsentAndPresent verifies Flag.Check returns the
// boolean truth of the WorldState.Flags entry. Absent / unset
// keys read as false (Go's zero value for bool).
func TestFlag_AbsentAndPresent(t *testing.T) {
	ws := state.NewWorldState()
	absent := Flag{Key: "absent"}
	if absent.Check(ws) {
		t.Error("Flag{Key:absent}.Check on empty world returned true; want false")
	}
	ws.Flags["present"] = true
	present := Flag{Key: "present"}
	if !present.Check(ws) {
		t.Error("Flag{Key:present}.Check on set flag returned false; want true")
	}
}

// TestVariableGE_Threshold verifies the "≥" semantics:
// equal-to-threshold passes, below threshold fails, above passes.
func TestVariableGE_Threshold(t *testing.T) {
	ws := state.NewWorldState()
	cond := VariableGE{Key: "courage", Value: 50}

	ws.Variables["courage"] = 49
	if cond.Check(ws) {
		t.Errorf("VariableGE(49) returned true; want false")
	}
	ws.Variables["courage"] = 50
	if !cond.Check(ws) {
		t.Errorf("VariableGE(50) returned false; want true (boundary)")
	}
	ws.Variables["courage"] = 100
	if !cond.Check(ws) {
		t.Errorf("VariableGE(100) returned false; want true")
	}
	absentCond := VariableGE{Key: "absent_var", Value: 1}
	if absentCond.Check(state.NewWorldState()) {
		t.Errorf("VariableGE on absent key returned true; want false (zero value)")
	}
}

// TestRelationshipGE_ByAxis verifies each axis
// (Trust/Affection/Respect) is evaluated independently. A high
// Trust value with low Affection must NOT satisfy an
// Affection ≥ 50 condition.
func TestRelationshipGE_ByAxis(t *testing.T) {
	ws := state.NewWorldState()
	ws.Relationships["elara"] = state.Relationship{Trust: 90, Affection: 10, Respect: 30}

	tests := []struct {
		axis RelationshipAxis
		want bool
	}{
		{AxisTrust, true},      // trust 90 ≥ 50
		{AxisAffection, false}, // affection 10 < 50
		{AxisRespect, false},   // respect 30 < 50
	}
	for _, tc := range tests {
		cond := RelationshipGE{Character: "elara", Axis: tc.axis, Value: 50}
		got := cond.Check(ws)
		if got != tc.want {
			t.Errorf("RelationshipGE(axis=%s, value=50): got %v want %v", tc.axis, got, tc.want)
		}
	}
	absentCond := RelationshipGE{Character: "absent", Axis: AxisTrust, Value: 1}
	if absentCond.Check(ws) {
		t.Error("RelationshipGE on absent companion returned true; want false")
	}
}

// TestHasItem_ZeroIsAbsent verifies Count > 0 is required.
// A stack at exactly 0 does NOT satisfy HasItem.
func TestHasItem_ZeroIsAbsent(t *testing.T) {
	ws := state.NewWorldState()
	emptyCond := HasItem{Key: "sword"}
	if emptyCond.Check(ws) {
		t.Error("HasItem on empty inventory returned true; want false")
	}
	ws.Inventory.Items["sword"] = 0
	zeroCond := HasItem{Key: "sword"}
	if zeroCond.Check(ws) {
		t.Error("HasItem on zero-count stack returned true; want false")
	}
	ws.Inventory.Items["sword"] = 1
	oneCond := HasItem{Key: "sword"}
	if !oneCond.Check(ws) {
		t.Error("HasItem on count-1 stack returned false; want true")
	}
}

// TestHasEnding_InListNotInList verifies the membership test.
func TestHasEnding_InListNotInList(t *testing.T) {
	ws := state.NewWorldState()
	ws.EndingsUnlocked = []string{"hero", "wanderer"}
	inList := HasEnding{ID: "hero"}
	if !inList.Check(ws) {
		t.Error("HasEnding(hero): got false; want true (in list)")
	}
	notInList := HasEnding{ID: "elara_romance"}
	if notInList.Check(ws) {
		t.Error("HasEnding(elara_romance): got true; want false (not in list)")
	}
	absent := HasEnding{ID: "absent"}
	if absent.Check(state.NewWorldState()) {
		t.Error("HasEnding on empty EndingsUnlocked: got true; want false")
	}
}

// TestOr_AnyOrNone verifies OR truth table:
//
//	empty           -> false (no path to true)
//	[a passes]      -> true
//	[a fails]       -> false
//	[a+b one passes] -> true
//	[a+b both fail] -> false
func TestOr_AnyOrNone(t *testing.T) {
	ws := state.NewWorldState()
	ws.Flags["b"] = true

	emptyOr := Or{}
	if emptyOr.Check(ws) {
		t.Error("Or{}: got true; want false")
	}
	orB := Or{Conditions: []Condition{Flag{Key: "b"}}}
	if !orB.Check(ws) {
		t.Error("Or{[b]}: got false; want true")
	}
	orA := Or{Conditions: []Condition{Flag{Key: "a"}}}
	if orA.Check(ws) {
		t.Error("Or{[a]}: got true; want false")
	}
	orAB := Or{Conditions: []Condition{Flag{Key: "a"}, Flag{Key: "b"}}}
	if !orAB.Check(ws) {
		t.Error("Or{[a,b]}: got false; want true (b passes)")
	}
	orAC := Or{Conditions: []Condition{Flag{Key: "a"}, Flag{Key: "c"}}}
	if orAC.Check(ws) {
		t.Error("Or{[a,c]}: got true; want false")
	}
}

// TestAnd_AllOrNone verifies AND truth table:
//
//	empty           -> true  (vacuous truth)
//	[a passes]      -> ws[a]
//	[a+b both pass] -> true
//	[a+b one fails] -> false
func TestAnd_AllOrNone(t *testing.T) {
	ws := state.NewWorldState()
	ws.Flags["b"] = true

	emptyAnd := And{}
	if !emptyAnd.Check(ws) {
		t.Error("And{}: got false; want true (vacuous)")
	}
	andB := And{Conditions: []Condition{Flag{Key: "b"}}}
	if !andB.Check(ws) {
		t.Error("And{[b]}: got false; want true")
	}
	andBB := And{Conditions: []Condition{Flag{Key: "b"}, Flag{Key: "b"}}}
	if !andBB.Check(ws) {
		t.Error("And{[b,b]}: got false; want true")
	}
	andBA := And{Conditions: []Condition{Flag{Key: "b"}, Flag{Key: "a"}}}
	if andBA.Check(ws) {
		t.Error("And{[b,a]}: got true; want false (a fails)")
	}
}

// TestNot_Inverts returns the negation of Inner.
func TestNot_Inverts(t *testing.T) {
	ws := state.NewWorldState()
	ws.Flags["b"] = true
	notB := Not{Inner: Flag{Key: "b"}}
	if notB.Check(ws) {
		t.Error("Not(Flag{b}): got true; want false (b is set)")
	}
	notA := Not{Inner: Flag{Key: "a"}}
	if !notA.Check(ws) {
		t.Error("Not(Flag{a}): got false; want true (a is not set)")
	}
}

// TestComposed_OrOfAnd_Nested verifies the canonical
// "(A AND B) OR (C AND D)" pattern via combinator nesting.
func TestComposed_OrOfAnd_Nested(t *testing.T) {
	ws := state.NewWorldState()
	ws.Flags["a"] = true
	ws.Flags["c"] = true

	cond := Or{Conditions: []Condition{
		And{Conditions: []Condition{Flag{Key: "a"}, Flag{Key: "b"}}},
		And{Conditions: []Condition{Flag{Key: "c"}, Flag{Key: "d"}}},
	}}
	// (a AND b): a=T, b=F -> false. (c AND d): c=T, d=F -> false.
	// OR: false OR false = false.
	if cond.Check(ws) {
		t.Error("OR-OF-AND with [a+b] failing AND [c+d] failing: got true; want false")
	}

	ws.Flags["b"] = true
	// Now [a AND b] is true. Whole expression is true.
	if !cond.Check(ws) {
		t.Error("After setting b: got false; want true (a+b satisfies OR)")
	}
}

// TestRelationshipAxis_StringRoundTrip guards the
// parser<->printer pair against drift. New axes that don't add
// to both String and ParseRelationshipAxis must add to both —
// or Phase 36.E content loader will refuse them at load time.
func TestRelationshipAxis_StringRoundTrip(t *testing.T) {
	for _, s := range []string{"trust", "affection", "respect"} {
		axis, ok := ParseRelationshipAxis(s)
		if !ok {
			t.Errorf("ParseRelationshipAxis(%q): got !ok; want ok", s)
		}
		if axis.String() != s {
			t.Errorf("Parse(%q).String(): got %q; want round-trip equals %q", s, axis.String(), s)
		}
	}
	if _, ok := ParseRelationshipAxis("nope"); ok {
		t.Error(`ParseRelationshipAxis("nope"): got ok; want !ok`)
	}
}
