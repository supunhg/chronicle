package content

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/story"

	"gopkg.in/yaml.v3"
)

// Loaded bundles every artifact parsed from a content
// directory. Construction is fail-fast: any missing file or
// broken cross-reference returns an error before Loaded is
// allocated.
type Loaded struct {
	// Graph is the v2 StoryGraph built from nodes.yaml. Every
	// node's NextNodeID resolves to a Node.ID in this graph
	// (validated at load time).
	Graph *story.Graph

	// Events is the registry Phase 36.D's internal/events.Trigger
	// consults when TriggerEvent effects fire in Step.
	Events []story.Event

	// Endings is the registry Phase 36.D's runner.maybeSurfaceEndings
	// consults when Step lands on a final node.
	Endings []endings.Ending

	// Protagonists is the §15 list of CharacterProfiles the
	// character-select screen iterates over. Phase 38.C will
	// wire Lifecycle.NewSave from a Protagonist's
	// starting_* fields.
	Protagonists []Protagonist
}

// Protagonist is the loader-side representation of a
// CharacterProfile (ARCHITECTURE.md §15). It maps 1-to-1 to
// the YAML form in protagonists.yaml.
type Protagonist struct {
	Name              string
	StartingFlags     []string
	StartingVariables map[string]int
	StartingInventory []string
	ExclusiveNodes    []string
	StartingParty     []string
}

// Companion is the loader-side representation of an entry in
// companions.yaml. Phase 36.E uses Companion.ID for
// cross-reference validation only; richer companion data
// (backstory nodes, romance thresholds) lands in Phase 38.
type Companion struct {
	ID          string
	Description string
}

// Load reads YAML content from `dir` and returns a fully
// validated *Loaded bundle.
//
// See package doc for the canonical file layout. Load is
// fail-fast; the returned error names the file whose content
// failed or the cross-reference that broke.
//
// Empty/nil Loaded fields are valid: an authored world with
// no events or no endings will produce zero-length slices
// rather than nil (so callers can `len(loaded.Events)` without
// defence).
func Load(dir string) (*Loaded, error) {
	if dir == "" {
		return nil, errors.New("content: Load: empty content directory path")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("content: Load: stat directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("content: Load: %q is not a directory", dir)
	}

	nodes, err := readNodes(filepath.Join(dir, "nodes.yaml"))
	if err != nil {
		return nil, err
	}
	events, err := readEvents(filepath.Join(dir, "events.yaml"))
	if err != nil {
		return nil, err
	}
	endingsList, err := readEndings(filepath.Join(dir, "endings.yaml"))
	if err != nil {
		return nil, err
	}
	protagonists, err := readProtagonists(filepath.Join(dir, "protagonists.yaml"))
	if err != nil {
		return nil, err
	}

	// Optional file: companions.yaml. If missing, Load still
	// succeeds — protagonists with empty starting_party are
	// valid; only references to absent companions error.
	var companions map[string]Companion
	companionsPath := filepath.Join(dir, "companions.yaml")
	if fileExists(companionsPath) {
		companions, err = readCompanions(companionsPath)
		if err != nil {
			return nil, err
		}
	}

	// Acceptance-phase validation: cross-references.
	if err := validateNodeReferences(nodes); err != nil {
		return nil, err
	}
	if err := validateTriggerEventIDs(nodes, events); err != nil {
		return nil, err
	}
	if err := validatePartyCompanions(protagonists, companions); err != nil {
		return nil, err
	}

	// Build the *story.Graph. Nodes with duplicate IDs or
	// empty IDs would already have been caught by validation,
	// but Add returns its own descriptive error — surface it
	// without re-prefixing twice.
	g := story.NewGraph()
	for _, n := range nodes {
		if err := g.Add(n); err != nil {
			return nil, fmt.Errorf("content: Load: graph.Add(%q): %w", n.ID, err)
		}
	}

	loaded := &Loaded{
		Graph:        g,
		Events:       events,
		Endings:      endingsList,
		Protagonists: protagonists,
	}
	// Normalise nil to zero-length so len() works as expected.
	if loaded.Events == nil {
		loaded.Events = []story.Event{}
	}
	if loaded.Endings == nil {
		loaded.Endings = []endings.Ending{}
	}
	if loaded.Protagonists == nil {
		loaded.Protagonists = []Protagonist{}
	}
	return loaded, nil
}

// fileExists returns true iff path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ----- nodes.yaml -----

type nodeDoc struct {
	Nodes []yamlNode `yaml:"nodes"`
}

type yamlNode struct {
	ID      string       `yaml:"id"`
	Title   string       `yaml:"title"`
	Text    string       `yaml:"text"`
	IsFinal bool         `yaml:"is_final"`
	Choices []yamlChoice `yaml:"choices"`
}

type yamlChoice struct {
	ID         string           `yaml:"id"`
	Text       string           `yaml:"text"`
	Conditions []map[string]any `yaml:"conditions,omitempty"`
	Effects    []map[string]any `yaml:"effects,omitempty"`
	NextNodeID string           `yaml:"next_node_id"`
}

func readNodes(path string) ([]story.StoryNode, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("content: readNodes: %s: %w", path, err)
	}
	var doc nodeDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("content: readNodes: parse %s: %w", path, err)
	}
	out := make([]story.StoryNode, 0, len(doc.Nodes))
	for i, yn := range doc.Nodes {
		node, err := convertNode(yn)
		if err != nil {
			return nil, fmt.Errorf("content: readNodes: %s node[%d]: %w", path, i, err)
		}
		out = append(out, node)
	}
	return out, nil
}

func convertNode(yn yamlNode) (story.StoryNode, error) {
	if yn.ID == "" {
		return story.StoryNode{}, errors.New("node has empty id")
	}
	out := story.StoryNode{
		ID:      yn.ID,
		Title:   yn.Title,
		Text:    yn.Text,
		IsFinal: yn.IsFinal,
	}
	for i, yc := range yn.Choices {
		c, err := convertChoice(yc)
		if err != nil {
			return story.StoryNode{}, fmt.Errorf("choice[%d] %q: %w", i, yc.ID, err)
		}
		out.Choices = append(out.Choices, c)
	}
	return out, nil
}

func convertChoice(yc yamlChoice) (story.Choice, error) {
	if yc.ID == "" {
		return story.Choice{}, errors.New("choice has empty id")
	}
	out := story.Choice{
		ID:         yc.ID,
		Text:       yc.Text,
		NextNodeID: yc.NextNodeID,
	}
	for i, raw := range yc.Conditions {
		cond, err := unmarshalCondition(raw)
		if err != nil {
			return story.Choice{}, fmt.Errorf("condition[%d]: %w", i, err)
		}
		out.Conditions = append(out.Conditions, cond)
	}
	for i, raw := range yc.Effects {
		eff, err := unmarshalEffect(raw)
		if err != nil {
			return story.Choice{}, fmt.Errorf("effect[%d]: %w", i, err)
		}
		out.Effects = append(out.Effects, eff)
	}
	return out, nil
}

// unmarshalCondition decodes a single-key-map condition from
// YAML into its concrete story.Condition. The key names the
// kind ("flag", "variable", ...) and the value is the
// condition's payload. Single-key maps surface a typo loud:
// a malformed condition with multiple keys is an error, not
// a silent drop of the extras.
func unmarshalCondition(raw map[string]any) (story.Condition, error) {
	if len(raw) != 1 {
		return nil, fmt.Errorf("condition expected single-key map (got %d keys)", len(raw))
	}
	for kind, v := range raw {
		return decodeConditionKind(kind, v)
	}
	return nil, errors.New("condition: empty map (unreachable)")
}

func decodeConditionKind(kind string, v any) (story.Condition, error) {
	switch kind {
	case "flag":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("flag: %w", err)
		}
		return story.Flag{Key: key}, nil
	case "variable":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("variable: %w", err)
		}
		key, err := asString(mm["key"])
		if err != nil {
			return nil, fmt.Errorf("variable.key: %w", err)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("variable.value: %w", err)
		}
		return story.VariableGE{Key: key, Value: val}, nil
	case "relationship":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("relationship: %w", err)
		}
		char, err := asString(mm["character"])
		if err != nil {
			return nil, fmt.Errorf("relationship.character: %w", err)
		}
		axisStr, err := asString(mm["axis"])
		if err != nil {
			return nil, fmt.Errorf("relationship.axis: %w", err)
		}
		axis, ok := story.ParseRelationshipAxis(axisStr)
		if !ok {
			return nil, fmt.Errorf("relationship.axis %q unrecognized (want trust/affection/respect)", axisStr)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("relationship.value: %w", err)
		}
		return story.RelationshipGE{Character: char, Axis: axis, Value: val}, nil
	case "has_item":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("has_item: %w", err)
		}
		return story.HasItem{Key: key}, nil
	case "has_ending":
		id, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("has_ending: %w", err)
		}
		return story.HasEnding{ID: id}, nil
	case "or":
		inner, err := unmarshalConditions(v)
		if err != nil {
			return nil, fmt.Errorf("or: %w", err)
		}
		return story.Or{Conditions: inner}, nil
	case "and":
		inner, err := unmarshalConditions(v)
		if err != nil {
			return nil, fmt.Errorf("and: %w", err)
		}
		return story.And{Conditions: inner}, nil
	case "not":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("not: %w", err)
		}
		inner, err := unmarshalCondition(mm)
		if err != nil {
			return nil, fmt.Errorf("not: %w", err)
		}
		return story.Not{Inner: inner}, nil
	}
	return nil, fmt.Errorf("unknown condition kind %q", kind)
}

// unmarshalConditions decodes a list of single-key-map
// conditions. Used by the Or/And combinators.
func unmarshalConditions(raw any) ([]story.Condition, error) {
	list, err := asSlice(raw)
	if err != nil {
		return nil, err
	}
	out := make([]story.Condition, 0, len(list))
	for i, e := range list {
		mm, err := asMap(e)
		if err != nil {
			return nil, fmt.Errorf("combinator child[%d]: %w", i, err)
		}
		cond, err := unmarshalCondition(mm)
		if err != nil {
			return nil, fmt.Errorf("combinator child[%d]: %w", i, err)
		}
		out = append(out, cond)
	}
	return out, nil
}

// unmarshalEffect decodes a single-key-map effect from YAML
// into its concrete story.Effect. Same key-disciplined
// convention as unmarshalCondition.
func unmarshalEffect(raw map[string]any) (story.Effect, error) {
	if len(raw) != 1 {
		return nil, fmt.Errorf("effect expected single-key map (got %d keys)", len(raw))
	}
	for kind, v := range raw {
		return decodeEffectKind(kind, v)
	}
	return nil, errors.New("effect: empty map (unreachable)")
}

func decodeEffectKind(kind string, v any) (story.Effect, error) {
	switch kind {
	case "set_flag":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("set_flag: %w", err)
		}
		return story.SetFlag{Key: key}, nil
	case "clear_flag":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("clear_flag: %w", err)
		}
		return story.ClearFlag{Key: key}, nil
	case "modify_variable":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("modify_variable: %w", err)
		}
		key, err := asString(mm["key"])
		if err != nil {
			return nil, fmt.Errorf("modify_variable.key: %w", err)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("modify_variable.value: %w", err)
		}
		return story.ModifyVariable{Key: key, Value: val}, nil
	case "modify_relationship":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("modify_relationship: %w", err)
		}
		char, err := asString(mm["character"])
		if err != nil {
			return nil, fmt.Errorf("modify_relationship.character: %w", err)
		}
		axisStr, err := asString(mm["axis"])
		if err != nil {
			return nil, fmt.Errorf("modify_relationship.axis: %w", err)
		}
		axis, ok := story.ParseRelationshipAxis(axisStr)
		if !ok {
			return nil, fmt.Errorf("modify_relationship.axis %q unrecognized (want trust/affection/respect)", axisStr)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("modify_relationship.value: %w", err)
		}
		return story.ModifyRelationship{Character: char, Axis: axis, Value: val}, nil
	case "modify_reputation":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("modify_reputation: %w", err)
		}
		factionStr, err := asString(mm["faction"])
		if err != nil {
			return nil, fmt.Errorf("modify_reputation.faction: %w", err)
		}
		faction, ok := story.ParseFaction(factionStr)
		if !ok {
			return nil, fmt.Errorf("modify_reputation.faction %q unrecognized (want kingdom/mages/dragons/underworld)", factionStr)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("modify_reputation.value: %w", err)
		}
		return story.ModifyReputation{Faction: faction, Value: val}, nil
	case "add_item":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("add_item: %w", err)
		}
		key, err := asString(mm["key"])
		if err != nil {
			return nil, fmt.Errorf("add_item.key: %w", err)
		}
		count, err := asInt(mm["count"])
		if err != nil {
			return nil, fmt.Errorf("add_item.count: %w", err)
		}
		return story.AddItem{Key: key, Count: count}, nil
	case "remove_item":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("remove_item: %w", err)
		}
		key, err := asString(mm["key"])
		if err != nil {
			return nil, fmt.Errorf("remove_item.key: %w", err)
		}
		count, err := asInt(mm["count"])
		if err != nil {
			return nil, fmt.Errorf("remove_item.count: %w", err)
		}
		return story.RemoveItem{Key: key, Count: count}, nil
	case "trigger_event":
		id, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("trigger_event: %w", err)
		}
		return story.TriggerEvent{ID: id}, nil
	}
	return nil, fmt.Errorf("unknown effect kind %q", kind)
}

// ----- events.yaml -----

type eventDoc struct {
	Events []yamlEvent `yaml:"events"`
}

type yamlEvent struct {
	ID         string           `yaml:"id"`
	NodeID     string           `yaml:"node_id"`
	Conditions []map[string]any `yaml:"conditions,omitempty"`
}

func readEvents(path string) ([]story.Event, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("content: readEvents: %s: %w", path, err)
	}
	var doc eventDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("content: readEvents: parse %s: %w", path, err)
	}
	out := make([]story.Event, 0, len(doc.Events))
	for i, ye := range doc.Events {
		ev := story.Event{
			ID:     ye.ID,
			NodeID: ye.NodeID,
		}
		if ye.ID == "" {
			return nil, fmt.Errorf("content: readEvents: %s event[%d] has empty id", path, i)
		}
		for j, raw := range ye.Conditions {
			cond, err := unmarshalCondition(raw)
			if err != nil {
				return nil, fmt.Errorf("content: readEvents: %s event[%d] %q condition[%d]: %w", path, i, ye.ID, j, err)
			}
			ev.Conditions = append(ev.Conditions, cond)
		}
		out = append(out, ev)
	}
	return out, nil
}

// ----- endings.yaml -----

type endingDoc struct {
	Endings []yamlEnding `yaml:"endings"`
}

type yamlEnding struct {
	ID         string           `yaml:"id"`
	Priority   int              `yaml:"priority"`
	Conditions []map[string]any `yaml:"conditions,omitempty"`
}

func readEndings(path string) ([]endings.Ending, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("content: readEndings: %s: %w", path, err)
	}
	var doc endingDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("content: readEndings: parse %s: %w", path, err)
	}
	out := make([]endings.Ending, 0, len(doc.Endings))
	for i, ye := range doc.Endings {
		en := endings.Ending{
			ID:       ye.ID,
			Priority: ye.Priority,
		}
		if ye.ID == "" {
			return nil, fmt.Errorf("content: readEndings: %s ending[%d] has empty id", path, i)
		}
		for j, raw := range ye.Conditions {
			cond, err := unmarshalCondition(raw)
			if err != nil {
				return nil, fmt.Errorf("content: readEndings: %s ending[%d] %q condition[%d]: %w", path, i, ye.ID, j, err)
			}
			en.Conditions = append(en.Conditions, cond)
		}
		out = append(out, en)
	}
	return out, nil
}

// ----- protagonists.yaml -----

type protagonistsDoc struct {
	Protagonists []yamlProtagonist `yaml:"protagonists"`
}

type yamlProtagonist struct {
	Name              string            `yaml:"name"`
	StartingFlags     []string          `yaml:"starting_flags"`
	StartingVariables map[string]int    `yaml:"starting_variables"`
	StartingInventory []string          `yaml:"starting_inventory"`
	ExclusiveNodes    []string          `yaml:"exclusive_nodes"`
	StartingParty     []string          `yaml:"starting_party"`
}

func readProtagonists(path string) ([]Protagonist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("content: readProtagonists: %s: %w", path, err)
	}
	var doc protagonistsDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("content: readProtagonists: parse %s: %w", path, err)
	}
	seen := make(map[string]bool, len(doc.Protagonists))
	out := make([]Protagonist, 0, len(doc.Protagonists))
	for i, yp := range doc.Protagonists {
		if yp.Name == "" {
			return nil, fmt.Errorf("content: readProtagonists: %s protagonist[%d] has empty name", path, i)
		}
		if seen[yp.Name] {
			return nil, fmt.Errorf("content: readProtagonists: %s protagonist %q duplicated", path, yp.Name)
		}
		seen[yp.Name] = true
		out = append(out, Protagonist{
			Name:              yp.Name,
			StartingFlags:     yp.StartingFlags,
			StartingVariables: yp.StartingVariables,
			StartingInventory: yp.StartingInventory,
			ExclusiveNodes:    yp.ExclusiveNodes,
			StartingParty:     yp.StartingParty,
		})
	}
	return out, nil
}

// ----- companions.yaml (optional) -----

type companionsDoc struct {
	Companions []yamlCompanion `yaml:"companions"`
}

type yamlCompanion struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

func readCompanions(path string) (map[string]Companion, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("content: readCompanions: %s: %w", path, err)
	}
	var doc companionsDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("content: readCompanions: parse %s: %w", path, err)
	}
	out := make(map[string]Companion, len(doc.Companions))
	for i, yc := range doc.Companions {
		if yc.ID == "" {
			return nil, fmt.Errorf("content: readCompanions: %s companion[%d] has empty id", path, i)
		}
		if _, ok := out[yc.ID]; ok {
			return nil, fmt.Errorf("content: readCompanions: %s companion %q duplicated", path, yc.ID)
		}
		out[yc.ID] = Companion{
			ID:          yc.ID,
			Description: yc.Description,
		}
	}
	return out, nil
}

// ----- Validation -----

// validateNodeReferences ensures every Choice.NextNodeID
// resolves to a Node.ID. This is the "no broken node
// references" gate from ARCHITECTURE.md §24.
func validateNodeReferences(nodes []story.StoryNode) error {
	known := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		known[n.ID] = true
	}
	for _, n := range nodes {
		for _, c := range n.Choices {
			if c.NextNodeID == "" {
				return fmt.Errorf("validateNodeReferences: node %q choice %q has empty next_node_id", n.ID, c.ID)
			}
			if !known[c.NextNodeID] {
				return fmt.Errorf("validateNodeReferences: node %q choice %q references missing node %q", n.ID, c.ID, c.NextNodeID)
			}
		}
	}
	return nil
}

// validateTriggerEventIDs ensures every TriggerEvent effect's
// ID is present in the events registry. This is the "no
// dangling event references" gate from PHASES.md §36.E.
func validateTriggerEventIDs(nodes []story.StoryNode, events []story.Event) error {
	knownEvents := make(map[string]bool, len(events))
	for _, e := range events {
		if e.ID == "" {
			return errors.New("validateTriggerEventIDs: event with empty id")
		}
		knownEvents[e.ID] = true
	}
	for _, n := range nodes {
		for _, c := range n.Choices {
			for j, eff := range c.Effects {
				te, ok := eff.(story.TriggerEvent)
				if !ok {
					continue
				}
				if !knownEvents[te.ID] {
					return fmt.Errorf("validateTriggerEventIDs: node %q choice %q effect[%d] triggers unknown event %q", n.ID, c.ID, j, te.ID)
				}
			}
		}
	}
	return nil
}

// validatePartyCompanions ensures every Protagonist's
// starting_party is consistent with the supplied
// companions.yaml. A protagonist with empty starting_party
// is valid without companions.yaml (the player's party starts
// empty).
func validatePartyCompanions(protagonists []Protagonist, companions map[string]Companion) error {
	for _, p := range protagonists {
		if len(p.StartingParty) == 0 {
			continue
		}
		if len(companions) == 0 {
			return fmt.Errorf("validatePartyCompanions: protagonist %q references starting_party (%v) but companions.yaml is absent", p.Name, p.StartingParty)
		}
		for _, cid := range p.StartingParty {
			if _, ok := companions[cid]; !ok {
				return fmt.Errorf("validatePartyCompanions: protagonist %q starting_party references missing companion %q", p.Name, cid)
			}
		}
	}
	return nil
}

// ----- Helpers for YAML value coercion -----

func asString(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("expected string, got %T", v)
	}
	if s == "" {
		return "", errors.New("empty string")
	}
	return s, nil
}

func asInt(v any) (int, error) {
	switch t := v.(type) {
	case int:
		return t, nil
	case int64:
		return int(t), nil
	case float64:
		return int(t), nil
	case uint64:
		return int(t), nil
	}
	return 0, fmt.Errorf("expected int, got %T", v)
}

func asMap(v any) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected map, got %T", v)
	}
	return m, nil
}

func asSlice(v any) ([]any, error) {
	s, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected slice, got %T", v)
	}
	return s, nil
}
