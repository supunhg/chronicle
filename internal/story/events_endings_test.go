package story

import (
	"reflect"
	"strings"
	"testing"
)

// ----- EventsFromYAML tests -----

// TestEventsFromYAML_Canonical exercises a single self-contained
// events.yaml with two events: one with a Flag condition, one
// without (always-fires sidebar).
func TestEventsFromYAML_Canonical(t *testing.T) {
	in := `events:
  - id: "ally_call"
    node_id: "act1.ally_appears"
    conditions:
      - flag: "began_journey"
  - id: "dragon_stirs"
    node_id: "act2.dragon_reveal"
`
	got, err := EventsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("EventsFromYAML: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("events count = %d; want 2", len(got))
	}
	e1 := got[0]
	if e1.ID != "ally_call" || e1.NodeID != "act1.ally_appears" {
		t.Errorf("event[0] = %+v; want id=ally_call node_id=act1.ally_appears", e1)
	}
	if len(e1.Conditions) != 1 {
		t.Errorf("event[0] conditions count = %d; want 1", len(e1.Conditions))
	}
	if cond, ok := e1.Conditions[0].(Flag); !ok || cond.Key != "began_journey" {
		t.Errorf("event[0] condition[0] = %+v; want Flag{began_journey}", e1.Conditions[0])
	}
	e2 := got[1]
	if e2.ID != "dragon_stirs" || e2.NodeID != "act2.dragon_reveal" {
		t.Errorf("event[1] = %+v; want id=dragon_stirs node_id=act2.dragon_reveal", e2)
	}
	if len(e2.Conditions) != 0 {
		t.Errorf("event[1] conditions count = %d; want 0 (always-fires sidebar)", len(e2.Conditions))
	}
}

// TestEventsFromYAML_FullSchema_OneConditionEveryKind verifies
// every condition kind can appear in an event's conditions list
// and lands as the correct concrete Go type. Drift guard: adding
// a new Condition concrete to story/conditions.go MUST also
// extend story.UnmarshalCondition + this assertion list.
func TestEventsFromYAML_FullSchema_OneConditionEveryKind(t *testing.T) {
	in := `events:
  - id: "rich_event"
    node_id: "rich_target"
    conditions:
      - flag: "F"
      - variable: { key: "V", value: 1 }
      - relationship: { character: "Elara", axis: trust, value: 5 }
      - has_item: "Key"
      - has_ending: "hero"
      - or:
          - flag: "A"
          - flag: "B"
      - and:
          - flag: "C"
          - flag: "D"
      - not:
          flag: "negated"
`
	got, err := EventsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("EventsFromYAML: %v", err)
	}
	wantConds := []Condition{
		Flag{Key: "F"},
		VariableGE{Key: "V", Value: 1},
		RelationshipGE{Character: "Elara", Axis: AxisTrust, Value: 5},
		HasItem{Key: "Key"},
		HasEnding{ID: "hero"},
		Or{Conditions: []Condition{Flag{Key: "A"}, Flag{Key: "B"}}},
		And{Conditions: []Condition{Flag{Key: "C"}, Flag{Key: "D"}}},
		Not{Inner: Flag{Key: "negated"}},
	}
	if len(got[0].Conditions) != len(wantConds) {
		t.Fatalf("event conditions count = %d; want %d", len(got[0].Conditions), len(wantConds))
	}
	for i, want := range wantConds {
		gotType := reflect.TypeOf(got[0].Conditions[i])
		wantType := reflect.TypeOf(want)
		if gotType != wantType {
			t.Errorf("condition[%d] type = %s; want %s", i, gotType, wantType)
		}
		if !reflect.DeepEqual(got[0].Conditions[i], want) {
			t.Errorf("condition[%d] = %+v; want %+v", i, got[0].Conditions[i], want)
		}
	}
}

// TestEventsFromYAML_UnknownKindRejected verifies authoring typos
// surface clear errors naming the bad kind.
func TestEventsFromYAML_UnknownKindRejected(t *testing.T) {
	in := `events:
  - id: "e"
    node_id: "n"
    conditions:
      - magic_flag: "X"
`
	_, err := EventsFromYAML([]byte(in))
	if err == nil {
		t.Fatal("EventsFromYAML with unknown condition kind did not error")
	}
	if !strings.Contains(err.Error(), "unknown") || !strings.Contains(err.Error(), "magic_flag") {
		t.Errorf("EventsFromYAML unknown-kind error = %q; want 'unknown' + kind name", err.Error())
	}
}

// TestEventsFromYAML_MultiKeyRejected verifies the single-key-map
// discipline also applies to event conditions.
func TestEventsFromYAML_MultiKeyRejected(t *testing.T) {
	in := `events:
  - id: "e"
    node_id: "n"
    conditions:
      - flag: "A"
        variable: { key: "B", value: 1 }
`
	_, err := EventsFromYAML([]byte(in))
	if err == nil {
		t.Fatal("EventsFromYAML with multi-key condition did not error")
	}
	if !strings.Contains(err.Error(), "single-key") {
		t.Errorf("EventsFromYAML multi-key error = %q; want 'single-key'", err.Error())
	}
}

// TestEventsFromYAML_EmptyList + TestEventsFromYAML_ParseError
// verify the boundary cases: empty list is allowed (no events
// is a valid authored world); bad YAML surfaces a wrapped error.
func TestEventsFromYAML_EmptyList(t *testing.T) {
	got, err := EventsFromYAML([]byte("events: []\n"))
	if err != nil {
		t.Fatalf("EventsFromYAML empty list: %v", err)
	}
	if got == nil {
		t.Fatal("EventsFromYAML empty list returned nil; want non-nil zero-length slice")
	}
	if len(got) != 0 {
		t.Errorf("EventsFromYAML empty list count = %d; want 0", len(got))
	}
}

func TestEventsFromYAML_ParseError(t *testing.T) {
	_, err := EventsFromYAML([]byte(`events: [{ broken`))
	if err == nil {
		t.Fatal("EventsFromYAML bad YAML did not error")
	}
	if !strings.Contains(err.Error(), "EventsFromYAML") {
		t.Errorf("EventsFromYAML parse error = %q; want prefix 'EventsFromYAML'", err.Error())
	}
}
