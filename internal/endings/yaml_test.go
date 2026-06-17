package endings

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/story"
)

// TestEndingsFromYAML_Canonical exercises the standard "highest
// priority valid wins" archetype plus a §19 fallback (no
// conditions = always valid).
func TestEndingsFromYAML_Canonical(t *testing.T) {
	in := `endings:
  - id: "hero"
    priority: 1
    conditions:
      - flag: "mid_completed"
  - id: "fallback"
    priority: 0
`
	got, err := EndingsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("EndingsFromYAML: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("endings count = %d; want 2", len(got))
	}
	if got[0].ID != "hero" || got[0].Priority != 1 || len(got[0].Conditions) != 1 {
		t.Errorf("ending[0] = %+v; want id=hero priority=1 1 condition", got[0])
	}
	if cond, ok := got[0].Conditions[0].(story.Flag); !ok || cond.Key != "mid_completed" {
		t.Errorf("ending[0] condition = %+v; want story.Flag{mid_completed}", got[0].Conditions[0])
	}
	if got[1].ID != "fallback" || got[1].Priority != 0 || len(got[1].Conditions) != 0 {
		t.Errorf("ending[1] = %+v; want id=fallback priority=0 no conditions", got[1])
	}
}

// TestEndingsFromYAML_FullSchema_OneConditionEveryKind verifies
// every condition kind can appear in an ending and lands as the
// correct concrete Go type. Drift guard: adding a new Condition
// concrete to story/conditions.go MUST also extend
// story.UnmarshalCondition (parser) AND this assertion list.
func TestEndingsFromYAML_FullSchema_OneConditionEveryKind(t *testing.T) {
	in := `endings:
  - id: "rich_ending"
    priority: 50
    conditions:
      - flag: "F"
      - variable: { key: "V", value: 1 }
      - relationship: { character: "Elara", axis: affection, value: 75 }
      - has_item: "Royal Seal"
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
	got, err := EndingsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("EndingsFromYAML: %v", err)
	}
	wantConds := []story.Condition{
		story.Flag{Key: "F"},
		story.VariableGE{Key: "V", Value: 1},
		story.RelationshipGE{Character: "Elara", Axis: story.AxisAffection, Value: 75},
		story.HasItem{Key: "Royal Seal"},
		story.HasEnding{ID: "hero"},
		story.Or{Conditions: []story.Condition{story.Flag{Key: "A"}, story.Flag{Key: "B"}}},
		story.And{Conditions: []story.Condition{story.Flag{Key: "C"}, story.Flag{Key: "D"}}},
		story.Not{Inner: story.Flag{Key: "negated"}},
	}
	if len(got[0].Conditions) != len(wantConds) {
		t.Fatalf("ending conditions count = %d; want %d", len(got[0].Conditions), len(wantConds))
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

// TestEndingsFromYAML_MultiKeyRejected verifies the single-key
// discipline also applies to ending conditions (no double-condition
// per element).
func TestEndingsFromYAML_MultiKeyRejected(t *testing.T) {
	in := `endings:
  - id: "x"
    priority: 1
    conditions:
      - flag: "A"
        has_item: "Bad"
`
	_, err := EndingsFromYAML([]byte(in))
	if err == nil {
		t.Fatal("EndingsFromYAML with multi-key condition did not error")
	}
	if !strings.Contains(err.Error(), "single-key") {
		t.Errorf("EndingsFromYAML multi-key error = %q; want 'single-key'", err.Error())
	}
}

// TestEndingsFromYAML_EmptyList + TestEndingsFromYAML_ParseError
// boundary cases: no endings is a valid authored world; bad YAML
// surfaces a wrapped error.
func TestEndingsFromYAML_EmptyList(t *testing.T) {
	got, err := EndingsFromYAML([]byte("endings: []\n"))
	if err != nil {
		t.Fatalf("EndingsFromYAML empty list: %v", err)
	}
	if got == nil {
		t.Fatal("EndingsFromYAML empty list returned nil; want non-nil zero-length slice")
	}
	if len(got) != 0 {
		t.Errorf("EndingsFromYAML empty list count = %d; want 0", len(got))
	}
}

func TestEndingsFromYAML_ParseError(t *testing.T) {
	_, err := EndingsFromYAML([]byte(`endings: [{ broken`))
	if err == nil {
		t.Fatal("EndingsFromYAML bad YAML did not error")
	}
	if !strings.Contains(err.Error(), "EndingsFromYAML") {
		t.Errorf("EndingsFromYAML parse error = %q; want prefix 'EndingsFromYAML'", err.Error())
	}
}

// TestEndingsFromYAML_PriorityBoundary checks the priority
// ordering is preserved (parser doesn't reorder) and that
// negative priorities are accepted — useful for an "anti-ending"
// gate that competes with §19's always-valid fallback via
// priority inversion (i.e., a negative-priority ending takes
// effect only when no positive ending matched).
func TestEndingsFromYAML_PriorityBoundary(t *testing.T) {
	in := `endings:
  - id: "low"
    priority: -10
  - id: "high"
    priority: 100
`
	got, err := EndingsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("EndingsFromYAML: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("endings count = %d; want 2", len(got))
	}
	if got[0].Priority != -10 || got[1].Priority != 100 {
		t.Errorf("priorities = [%d %d]; want [-10 100]", got[0].Priority, got[1].Priority)
	}
}
