package story

import (
	"reflect"
	"strings"
	"testing"
)

// canonicalExampleYAML is the in-source copy of the README's
// Ashwick-entrance sample session. It matches docs/story-node-yaml.md
// section "Canonical example" byte-for-byte (modulo trailing
// whitespace) and is the acceptance fixture for PHASES.md §37.A.
//
// canonicalExampleYAML is read by TestLoadStoryNodes_CanonicalExample.
// If you change one, change both.
const canonicalExampleYAML = `nodes:
  - id: "act1.ashwick_entrance"
    title: "EX01 — The frontier town of Ashwick"
    text: |
      You stand at the threshold of Greyhall Keep. The wind
      carries the smell of woodsmoke and something older —
      incense, or the memory of incense, from the ruins on
      the eastern ridge.
    is_final: false
    choices:
      - id: "enter_keep"
        text: "Enter the keep."
        next_node_id: "act1.greyhall_keep"
        effects:
          - set_flag: "entered_keep"
      - id: "head_east"
        text: "Head for the eastern ridge."
        next_node_id: "act1.eastern_ridge"
        conditions:
          - variable: { key: "Courage", value: 30 }
      - id: "wait_watch"
        text: "Wait in the square and watch."
        next_node_id: "act1.wait_at_square"

  - id: "act1.greyhall_keep"
    title: "Inside Greyhall Keep"
    text: "The hall is quieter than it should be."
    choices:
      - id: "speak_to_keeper"
        text: "Speak to the keeper."
        next_node_id: "act1.keeper_interview"

  - id: "act1.eastern_ridge"
    title: "The Eastern Ridge"
    text: "Old ruins stretch along the ridge."
    choices:
      - id: "investigate_ruins"
        text: "Investigate the ruins."
        next_node_id: "act1.void_dragon_reveal"
        effects:
          - modify_variable: { key: "Courage", value: 60 }
          - trigger_event: "dragon_stirs"
`

// TestLoadStoryNodes_MinimalCanonical exercises the smallest
// valid nodes.yaml: one node, one choice, no conditions, no
// effects. The canonical smoke test for PHASES.md §37.A.
//
// Confirms:
//   - top-level `nodes:` list unwraps to a non-empty slice.
//   - StoryNode fields id/title/text are populated.
//   - Choice fields id/text/next_node_id are populated, with
//     zero-length Conditions / Effects slices.
func TestLoadStoryNodes_MinimalCanonical(t *testing.T) {
	yaml := `nodes:
  - id: "smoke"
    title: "Smoke"
    text: "Hello."
    choices:
      - id: "go"
        text: "Go."
        next_node_id: "smoke_next"
`
	got, err := LoadStoryNodes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadStoryNodes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded nodes count = %d; want 1", len(got))
	}
	n := got[0]
	if n.ID != "smoke" {
		t.Errorf("ID = %q; want smoke", n.ID)
	}
	if n.Title != "Smoke" {
		t.Errorf("Title = %q; want Smoke", n.Title)
	}
	if n.Text != "Hello." {
		t.Errorf("Text = %q; want Hello.", n.Text)
	}
	if n.IsFinal {
		t.Errorf("IsFinal = true; want false (default)")
	}
	if len(n.Choices) != 1 {
		t.Fatalf("Choices count = %d; want 1", len(n.Choices))
	}
	c := n.Choices[0]
	if c.ID != "go" || c.Text != "Go." || c.NextNodeID != "smoke_next" {
		t.Errorf("Choice = %+v; want id=go text=Go. next_node_id=smoke_next", c)
	}
	if len(c.Conditions) != 0 {
		t.Errorf("Conditions = %v; want []", c.Conditions)
	}
	if len(c.Effects) != 0 {
		t.Errorf("Effects = %v; want []", c.Effects)
	}
}

// TestLoadStoryNodes_FinalNode verifies the schema flags
// is_final: true correctly and allows empty choices.
func TestLoadStoryNodes_FinalNode(t *testing.T) {
	yaml := `nodes:
  - id: "end"
    title: "The End"
    text: "Farewell."
    is_final: true
`
	got, err := LoadStoryNodes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadStoryNodes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded nodes count = %d; want 1", len(got))
	}
	n := got[0]
	if !n.IsFinal {
		t.Errorf("IsFinal = false; want true")
	}
}

// TestLoadStoryNodes_FullSchema_OneChoiceEveryKind exercises
// every documented condition kind AND every documented effect
// kind in a single choice. Asserts the concrete Go types are
// produced. This is the schema-drift guard: any new Condition
// or Effect kind added to conditions.go / effects.go must also
// be added here, AND to internal/story/yaml.go's dispatch, AND
// to docs/story-node-yaml.md.
func TestLoadStoryNodes_FullSchema_OneChoiceEveryKind(t *testing.T) {
	yaml := `nodes:
  - id: "rich"
    title: "All The Kinds"
    text: "One choice for everything."
    choices:
      - id: "do_it"
        text: "Do everything."
        next_node_id: "rich_next"
        conditions:
          - flag: "F"
          - variable: { key: "V", value: 10 }
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
        effects:
          - set_flag: "f1"
          - clear_flag: "f1"
          - modify_variable: { key: "Courage", value: 5 }
          - modify_relationship: { character: "Elara", axis: trust, value: 5 }
          - modify_reputation: { faction: kingdom, value: 5 }
          - add_item: { key: "Coin", count: 1 }
          - remove_item: { key: "Coin", count: 1 }
          - trigger_event: "e1"
`
	got, err := LoadStoryNodes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadStoryNodes: %v", err)
	}
	c := got[0].Choices[0]

	wantCondTypes := []Condition{
		Flag{Key: "F"},
		VariableGE{Key: "V", Value: 10},
		RelationshipGE{Character: "Elara", Axis: AxisTrust, Value: 5},
		HasItem{Key: "Key"},
		HasEnding{ID: "hero"},
		Or{Conditions: []Condition{Flag{Key: "A"}, Flag{Key: "B"}}},
		And{Conditions: []Condition{Flag{Key: "C"}, Flag{Key: "D"}}},
		Not{Inner: Flag{Key: "negated"}},
	}
	if len(c.Conditions) != len(wantCondTypes) {
		t.Fatalf("conditions count = %d; want %d", len(c.Conditions), len(wantCondTypes))
	}
	for i, want := range wantCondTypes {
		assertSameConcreteType(t, "condition", i, c.Conditions[i], want)
	}

	wantEffConcrete := []string{
		"SetFlag", "ClearFlag", "ModifyVariable", "ModifyRelationship",
		"ModifyReputation", "AddItem", "RemoveItem", "TriggerEvent",
	}
	if len(c.Effects) != len(wantEffConcrete) {
		t.Fatalf("effects count = %d; want %d", len(c.Effects), len(wantEffConcrete))
	}
	for i, wantName := range wantEffConcrete {
		if got := concreteTypeName(c.Effects[i]); got != wantName {
			t.Errorf("effect[%d] concrete type = %s; want %s", i, got, wantName)
		}
	}
}

// TestLoadStoryNodes_CanonicalExample is the PHASES.md §37.A
// acceptance test: parse the README sample session's fixture
// and assert the structure came through intact. The fixture is
// canonicalExampleYAML at the top of this file.
func TestLoadStoryNodes_CanonicalExample(t *testing.T) {
	got, err := LoadStoryNodes([]byte(canonicalExampleYAML))
	if err != nil {
		t.Fatalf("LoadStoryNodes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("nodes count = %d; want 3 (per README sample session)", len(got))
	}

	ashwick := got[0]
	if ashwick.ID != "act1.ashwick_entrance" {
		t.Errorf("ashwick ID = %q; want act1.ashwick_entrance", ashwick.ID)
	}
	if !strings.Contains(ashwick.Title, "EX01") || !strings.Contains(ashwick.Title, "Ashwick") {
		t.Errorf("ashwick title missing 'EX01' or 'Ashwick'; got %q", ashwick.Title)
	}
	if len(ashwick.Choices) != 3 {
		t.Errorf("ashwick choices count = %d; want 3", len(ashwick.Choices))
	}
	// First choice: enter_keep with no conditions.
	if c := ashwick.Choices[0]; c.ID != "enter_keep" || len(c.Conditions) != 0 || len(c.Effects) != 1 {
		t.Errorf("enter_keep choice unexpected: id=%q conditions=%v effects=%v", c.ID, c.Conditions, c.Effects)
	}
	if _, ok := ashwick.Choices[0].Effects[0].(SetFlag); !ok {
		t.Errorf("enter_keep effect[0] type = %T; want SetFlag", ashwick.Choices[0].Effects[0])
	}
	// Second choice: head_east gated on a VariableGE(Courage, 30).
	if c := ashwick.Choices[1]; c.ID != "head_east" || len(c.Conditions) != 1 {
		t.Errorf("head_east choice unexpected: id=%q conditions=%v", c.ID, c.Conditions)
	}
	if cond, ok := ashwick.Choices[1].Conditions[0].(VariableGE); !ok || cond.Key != "Courage" || cond.Value != 30 {
		t.Errorf("head_east condition = %+v; want VariableGE{Courage,30}", ashwick.Choices[1].Conditions[0])
	}

	// Third node's investigate_ruins: must emit TriggerEvent with the
	// documented event ID.
	ridge := got[2]
	ir := ridge.Choices[0]
	if ir.ID != "investigate_ruins" || len(ir.Effects) != 2 {
		t.Errorf("investigate_ruins shape: id=%q effects=%v", ir.ID, ir.Effects)
	}
	if te, ok := ir.Effects[1].(TriggerEvent); !ok || te.ID != "dragon_stirs" {
		t.Errorf("investigate_ruins effect[1] = %+v; want TriggerEvent{dragon_stirs}", ir.Effects[1])
	}
}

// TestLoadStoryNodes_UnknownKind verifies authoring typos
// surface clear errors naming the bad kind.
func TestLoadStoryNodes_UnknownKind(t *testing.T) {
	for _, badKind := range []string{"magic_flag", "ultra_effect"} {
		yaml := `nodes:
  - id: "bad"
    title: "Bad"
    text: "Typo."
    choices:
      - id: "go"
        text: "Go."
        next_node_id: "bad_next"
        conditions:
          - ` + badKind + `: "X"
`
		_, err := LoadStoryNodes([]byte(yaml))
		if err == nil {
			t.Errorf("LoadStoryNodes with %q did not error; want unknown-kind error", badKind)
			continue
		}
		if !strings.Contains(err.Error(), "unknown") || !strings.Contains(err.Error(), badKind) {
			t.Errorf("LoadStoryNodes %q error = %q; want 'unknown' and the bad kind name", badKind, err.Error())
		}
	}
}

// TestLoadStoryNodes_MultiKeyRejected verifies the
// single-key-map discipline: conditions / effects with
// multiple keys surface a clear error.
func TestLoadStoryNodes_MultiKeyRejected(t *testing.T) {
	yaml := `nodes:
  - id: "bad"
    title: "Bad"
    text: "Two keys."
    choices:
      - id: "go"
        text: "Go."
        next_node_id: "bad_next"
        conditions:
          - flag: "A"
            variable: { key: "B", value: 1 }
`
	_, err := LoadStoryNodes([]byte(yaml))
	if err == nil {
		t.Fatal("LoadStoryNodes with multi-key condition did not error")
	}
	if !strings.Contains(err.Error(), "single-key") {
		t.Errorf("LoadStoryNodes multi-key error = %q; want 'single-key'", err.Error())
	}
}

// TestLoadStoryNodes_EmptyNodesList returns a zero-length
// slice (not nil) for an empty `nodes` list. This keeps
// production callers' len() checks valid.
func TestLoadStoryNodes_EmptyNodesList(t *testing.T) {
	got, err := LoadStoryNodes([]byte("nodes: []\n"))
	if err != nil {
		t.Fatalf("LoadStoryNodes: %v", err)
	}
	if got == nil {
		t.Fatalf("LoadStoryNodes empty input returned nil; want non-nil zero-length slice")
	}
	if len(got) != 0 {
		t.Errorf("LoadStoryNodes empty nodes count = %d; want 0", len(got))
	}
}

// TestLoadStoryNodes_ParseError surfaces yaml.v3 parse
// failures with the wrapping prefix `LoadStoryNodes: ...`.
func TestLoadStoryNodes_ParseError(t *testing.T) {
	// Unbalanced braces will trip yaml.Unmarshal.
	_, err := LoadStoryNodes([]byte(`nodes: [{ broken`))
	if err == nil {
		t.Fatal("LoadStoryNodes bad YAML did not error; want parse error")
	}
	if !strings.Contains(err.Error(), "LoadStoryNodes") {
		t.Errorf("LoadStoryNodes parse error = %q; want prefix 'LoadStoryNodes'", err.Error())
	}
}

// ----- helpers -----

// assertSameConcreteType asserts v and want are the SAME Go
// concrete type AND have field-for-field value equality (via
// reflect.DeepEqual on the value types this test exercises —
// all Conditions and Effects in the canonical schema are pure
// value types with comparable fields).
func assertSameConcreteType(t *testing.T, kind string, idx int, got, want any) {
	t.Helper()
	if concreteTypeName(got) != concreteTypeName(want) {
		t.Errorf("%s[%d] type = %s; want %s", kind, idx, concreteTypeName(got), concreteTypeName(want))
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s[%d] = %+v; want %+v", kind, idx, got, want)
	}
}

// concreteTypeName returns the concrete type name of v
// (e.g. "Flag" for a story.Flag value). Pointer and NamedType
// unwrapping is not needed for v2 Condition/Effect concretes —
// they are all value types.
func concreteTypeName(v any) string {
	if v == nil {
		return "<nil>"
	}
	name := reflect.TypeOf(v).Name()
	if name == "" {
		return "unknown"
	}
	return name
}
