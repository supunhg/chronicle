package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/story"
)

// writeContentDir writes each `key: content` pair as `key`
// under dir. Used to assemble minimal content trees for tests.
func writeContentDir(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

// happyPathContent is the canonical 2-node + 1-event + 1-ending
// + 1-protagonist content bundle used by TestLoad_HappyPath
// and the integration tests.
const happyPathNodes = `nodes:
  - id: "a"
    title: "A"
    text: "First node."
    choices:
      - id: "a1"
        text: "Go to B."
        next_node_id: "b"
  - id: "b"
    title: "B"
    text: "Second node."
    choices:
      - id: "b1"
        text: "Continue."
        next_node_id: "c"
  - id: "c"
    title: "C"
    text: "Finale."
    is_final: true
`

const happyPathEvents = `events:
  - id: "ally_call"
    node_id: "a"
`

const happyPathEndings = `endings:
  - id: "hero"
    priority: 1
  - id: "fallback"
    priority: 0
`

const happyPathProtagonists = `protagonists:
  - name: "Kael"
    starting_flags:
      - "Kael_Protagonist"
    starting_variables:
      Courage: 50
      DragonAffinity: 0
    starting_inventory:
      - "Iron Sword"
    exclusive_nodes:
      - "kael.special"
    starting_party: []
`

const happyPathCompanions = `companions:
  - id: "Elara"
    description: "Ranger companion."
`

// TestLoad_HappyPath validates a full canonical content
// directory round-trips to a Loaded bundle with graph add,
// events list, endings list, and protagonists list all
// populated.
func TestLoad_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml":        happyPathNodes,
		"events.yaml":       happyPathEvents,
		"endings.yaml":      happyPathEndings,
		"protagonists.yaml": happyPathProtagonists,
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Graph == nil {
		t.Fatal("Loaded.Graph is nil")
	}
	if got, err := loaded.Graph.Lookup("a"); err != nil || got.Text != "First node." {
		t.Errorf("Lookup(a): text=%q err=%v; want First node. nil", got.Text, err)
	}
	if got, err := loaded.Graph.Lookup("c"); err != nil || !got.IsFinal {
		t.Errorf("Lookup(c): is_final=false err=%v; want true nil", err)
	}
	if len(loaded.Events) != 1 || loaded.Events[0].ID != "ally_call" {
		t.Errorf("Events = %v; want [ally_call]", loaded.Events)
	}
	if len(loaded.Endings) != 2 {
		t.Errorf("Endings count = %d; want 2", len(loaded.Endings))
	}
	if len(loaded.Protagonists) != 1 || loaded.Protagonists[0].Name != "Kael" {
		t.Errorf("Protagonists = %v; want one with name=Kael", loaded.Protagonists)
	}
}

// TestLoad_BrokenNextNodeID fails the validation gate when a
// Choice.next_node_id points to a node that doesn't exist.
func TestLoad_BrokenNextNodeID(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml": `nodes:
  - id: "a"
    text: "First."
    choices:
      - id: "a1"
        text: "Bad link."
        next_node_id: "nonexistent"
`,
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for broken NextNodeID, got nil")
	}
	if !strings.Contains(err.Error(), "missing node") || !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("Load err = %q; want message containing 'missing node' and 'nonexistent'", err.Error())
	}
}

// TestLoad_UnknownEventID fails when a TriggerEvent effect's
// ID isn't registered in events.yaml.
func TestLoad_UnknownEventID(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml": `nodes:
  - id: "a"
    text: "A."
    choices:
      - id: "a1"
        text: "Trigger something."
        next_node_id: "b"
        effects:
          - trigger_event: "no_such_event"
  - id: "b"
    text: "B."
`,
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for unknown event ID, got nil")
	}
	if !strings.Contains(err.Error(), "unknown event") || !strings.Contains(err.Error(), "no_such_event") {
		t.Errorf("Load err = %q; want message containing 'unknown event' and 'no_such_event'", err.Error())
	}
}

// TestLoad_MissingNodesFile fails when the required
// nodes.yaml doesn't exist.
func TestLoad_MissingNodesFile(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for missing nodes.yaml, got nil")
	}
	if !strings.Contains(err.Error(), "nodes.yaml") {
		t.Errorf("Load err = %q; want message naming nodes.yaml", err.Error())
	}
}

// TestLoad_MissingCompanionWhenProtagonistHasParty fails
// when a protagonist's starting_party references a companion
// not in companions.yaml.
func TestLoad_MissingCompanionWhenProtagonistHasParty(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml":        "nodes: []",
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": `protagonists:
  - name: "Kael"
    starting_party:
      - "MissingFriend"
`,
		"companions.yaml": `companions:
  - id: "Elara"
    description: "Ranger."
`,
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for missing companion, got nil")
	}
	if !strings.Contains(err.Error(), "MissingFriend") {
		t.Errorf("Load err = %q; want message naming 'MissingFriend'", err.Error())
	}
}

// TestLoad_EmptyStartingParty_NoCompanionsFile verifies an
// empty starting_party works WITHOUT companions.yaml.
func TestLoad_EmptyStartingParty_NoCompanionsFile(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml":        "nodes: []",
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": `protagonists:
  - name: "Kael"
    starting_party: []
`,
	})

	_, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v (empty starting_party should not require companions.yaml)", err)
	}
}

// TestLoad_NonEmptyParty_NoCompanionsFile verifies a
// protagonist with non-empty starting_party, when
// companions.yaml is absent, fails the validation gate with
// a clear message naming the protagonist.
func TestLoad_NonEmptyParty_NoCompanionsFile(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml":        "nodes: []",
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": `protagonists:
  - name: "Kael"
    starting_party:
      - "Elara"
`,
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error (companions.yaml absent but party non-empty), got nil")
	}
	if !strings.Contains(err.Error(), "Kael") || !strings.Contains(err.Error(), "companions.yaml") {
		t.Errorf("Load err = %q; want message naming protagonist and companions.yaml", err.Error())
	}
}

// TestLoad_MultipleKeyConditionRejected verifies that a
// condition with multiple keys surfaces a clear error rather
// than silently dropping the extras.
func TestLoad_MultipleKeyConditionRejected(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml": `nodes:
  - id: "a"
    text: "A."
    choices:
      - id: "a1"
        text: "Bad."
        next_node_id: "b"
        conditions:
          - flag: "x"
            variable: { key: "y", value: 1 }
  - id: "b"
    text: "B."
`,
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for multi-key condition, got nil")
	}
	if !strings.Contains(err.Error(), "single-key") {
		t.Errorf("Load err = %q; want message mentioning single-key", err.Error())
	}
}

// TestLoad_UnknownEffectKindRejected verifies authoring typos
// in effects are loud: an unknown kind surfaces a clear
// error before any artifact is allocated.
func TestLoad_UnknownEffectKindRejected(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml": `nodes:
  - id: "a"
    text: "A."
    choices:
      - id: "a1"
        text: "Bad."
        next_node_id: "b"
        effects:
          - super_special_teleport: "nowhere"
  - id: "b"
    text: "B."
`,
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for unknown effect kind, got nil")
	}
	if !strings.Contains(err.Error(), "super_special_teleport") {
		t.Errorf("Load err = %q; want message naming the bad effect kind", err.Error())
	}
}

// TestLoad_UnknownRelationshipAxisRejected verifies that a
// relationship condition/effect with an unrecognized axis
// surfaces a clear error before allocation.
func TestLoad_UnknownRelationshipAxisRejected(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml": `nodes:
  - id: "a"
    text: "A."
    choices:
      - id: "a1"
        text: "Bad."
        next_node_id: "b"
        conditions:
          - relationship: { character: "Elara", axis: "trustt", value: 50 }
  - id: "b"
    text: "B."
`,
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for unknown axis, got nil")
	}
	if !strings.Contains(err.Error(), "trustt") || !strings.Contains(err.Error(), "axis") {
		t.Errorf("Load err = %q; want message naming bad axis and 'axis'", err.Error())
	}
}

// TestLoad_UnknownFactionRejected verifies that a
// modify_reputation effect with an unrecognized faction
// surfaces a clear error before allocation.
func TestLoad_UnknownFactionRejected(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml": `nodes:
  - id: "a"
    text: "A."
    choices:
      - id: "a1"
        text: "Bad."
        next_node_id: "b"
        effects:
          - modify_reputation: { faction: "undead", value: 5 }
  - id: "b"
    text: "B."
`,
		"events.yaml":       "events: []",
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for unknown faction, got nil")
	}
	if !strings.Contains(err.Error(), "undead") {
		t.Errorf("Load err = %q; want message naming 'undead'", err.Error())
	}
}

// TestLoad_RichEffectsAndConditionsParsed verifies that the
// full Condition/Effect polymorphism is wired end-to-end:
// VariableGE, RelationshipGE, HasItem, HasEnding, Or/And/Not,
// ModifyVariable, ModifyRelationship, ModifyReputation,
// AddItem, RemoveItem, TriggerEvent. Each compound type
// surfaces in the final Loaded bundle for downstream use,
// with concrete type assertions against the real story types.
func TestLoad_RichEffectsAndConditionsParsed(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml": `nodes:
  - id: "a"
    text: "A."
    choices:
      - id: "a1"
        text: "Composite."
        next_node_id: "b"
        conditions:
          - variable: { key: "Courage", value: 30 }
          - relationship: { character: "Elara", axis: trust, value: 50 }
          - has_item: "DragonKey"
          - or:
              - has_ending: "hero"
              - flag: "began_journey"
        effects:
          - set_flag: "a1_was_picked"
          - clear_flag: "stale_flag"
          - modify_variable: { key: "Courage", value: 90 }
          - modify_relationship: { character: "Elara", axis: affection, value: 10 }
          - modify_reputation: { faction: kingdom, value: 5 }
          - add_item: { key: "Royal Seal", count: 1 }
          - remove_item: { key: "Torch", count: 1 }
          - trigger_event: "ally_call"
  - id: "b"
    text: "B."
`,
		"events.yaml": `events:
  - id: "ally_call"
    node_id: "b"
`,
		"endings.yaml":      "endings: []",
		"protagonists.yaml": "protagonists: []",
	})
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	node, err := loaded.Graph.Lookup("a")
	if err != nil {
		t.Fatalf("Lookup(a): %v", err)
	}
	if len(node.Choices) != 1 {
		t.Fatalf("node a choices = %d; want 1", len(node.Choices))
	}
	c := node.Choices[0]
	if len(c.Conditions) != 4 {
		// variable + relationship + has_item + or (which counts as 1)
		t.Errorf("conditions count = %d; want 4", len(c.Conditions))
	}
	if len(c.Effects) != 8 {
		t.Errorf("effects count = %d; want 8", len(c.Effects))
	}
	// Spot-check the concrete types landed.
	if _, ok := c.Conditions[0].(story.VariableGE); !ok {
		t.Errorf("conditions[0] type = %T; want story.VariableGE", c.Conditions[0])
	}
	if _, ok := c.Conditions[1].(story.RelationshipGE); !ok {
		t.Errorf("conditions[1] type = %T; want story.RelationshipGE", c.Conditions[1])
	}
	if _, ok := c.Conditions[2].(story.HasItem); !ok {
		t.Errorf("conditions[2] type = %T; want story.HasItem", c.Conditions[2])
	}
	if _, ok := c.Conditions[3].(story.Or); !ok {
		t.Errorf("conditions[3] type = %T; want story.Or", c.Conditions[3])
	}
	if _, ok := c.Effects[0].(story.SetFlag); !ok {
		t.Errorf("effects[0] type = %T; want story.SetFlag", c.Effects[0])
	}
	if _, ok := c.Effects[7].(story.TriggerEvent); !ok {
		t.Errorf("effects[7] type = %T; want story.TriggerEvent", c.Effects[7])
	}
}

// TestLoad_FullEndToEndWithCompanionsAndProtagonists verifies
// that starting_party + companions.yaml round-trip produce a
// Loaded whose protagonist and graph are internally consistent.
func TestLoad_FullEndToEndWithCompanionsAndProtagonists(t *testing.T) {
	dir := t.TempDir()
	writeContentDir(t, dir, map[string]string{
		"nodes.yaml":        happyPathNodes,
		"events.yaml":       happyPathEvents,
		"endings.yaml":      happyPathEndings,
		"protagonists.yaml": `protagonists:
  - name: "Kael"
    starting_party:
      - "Elara"
`,
		"companions.yaml": happyPathCompanions,
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Protagonists) != 1 {
		t.Fatalf("protagonists count = %d; want 1", len(loaded.Protagonists))
	}
	p := loaded.Protagonists[0]
	if p.Name != "Kael" {
		t.Errorf("protagonist name = %q; want Kael", p.Name)
	}
	if len(p.StartingParty) != 1 || p.StartingParty[0] != "Elara" {
		t.Errorf("starting_party = %v; want [Elara]", p.StartingParty)
	}
}
