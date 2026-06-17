package story

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// LoadStoryNodes parses yaml.v3-encoded bytes into a slice of
// canonical StoryNode values per the YAML schema documented in
// docs/story-node-yaml.md (PHASES.md §37.A + ARCHITECTURE.md
// §5-§8).
//
// LoadStoryNodes is the canonical entry point for YAML→story.
// Phase 36.E's internal/content/loader.go delegates to it so
// the production content loader and the schema test in
// internal/story/nodes_test.go exercise the same parser.
func LoadStoryNodes(data []byte) ([]StoryNode, error) {
	var doc storyNodeDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("story: LoadStoryNodes: parse: %w", err)
	}
	out := make([]StoryNode, 0, len(doc.Nodes))
	for i, yn := range doc.Nodes {
		node, err := convertYAMLNode(yn)
		if err != nil {
			return nil, fmt.Errorf("story: LoadStoryNodes: node[%d]: %w", i, err)
		}
		out = append(out, node)
	}
	return out, nil
}

// EventsFromYAML parses an events.yaml document per the schema
// documented in docs/choice-yaml.md (PHASES.md §37.B +
// ARCHITECTURE.md §13).
//
// EventsFromYAML is the canonical entry point for YAML→events.
// internal/content/loader.go::readEvents delegates to it.
// Condition parsing delegates to UnmarshalCondition below.
func EventsFromYAML(data []byte) ([]Event, error) {
	var doc storyEventDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("story: EventsFromYAML: parse: %w", err)
	}
	out := make([]Event, 0, len(doc.Events))
	for i, ye := range doc.Events {
		if ye.ID == "" {
			return nil, fmt.Errorf("story: EventsFromYAML: event[%d] has empty id", i)
		}
		ev := Event{
			ID:     ye.ID,
			NodeID: ye.NodeID,
		}
		for j, raw := range ye.Conditions {
			cond, err := UnmarshalCondition(raw)
			if err != nil {
				return nil, fmt.Errorf("story: EventsFromYAML: event[%d] %q condition[%d]: %w", i, ye.ID, j, err)
			}
			ev.Conditions = append(ev.Conditions, cond)
		}
		out = append(out, ev)
	}
	return out, nil
}

// EndingsFromYAML moved to internal/endings/yaml.go per
// PHASES.md §37.B — its return type is []endings.Ending, so
// the parser lives in the package that owns Ending. Putting
// it in story would create the import cycle
// `content → endings → story → endings`. The two packages'
// parsers (LoadStoryNodes + EventsFromYAML in story;
// EndingsFromYAML in endings) all dispatch condition
// parsing through this file's UnmarshalCondition, so the
// single-key-map discipline remains in one canonical home.

// UnmarshalCondition decodes a single-key-map condition from
// YAML into its concrete Condition. The key names the kind
// ("flag", "variable", ...) and the value is the condition's
// payload. A malformed condition with multiple keys surfaces a
// clear error rather than silently dropping the extras.
//
// UnmarshalCondition is exported because event, ending, and
// future condition-bearing file types all embed conditions in
// their own records; centralising the dispatch here means one
// parser for every condition-bearing file.
func UnmarshalCondition(raw map[string]any) (Condition, error) {
	if len(raw) != 1 {
		return nil, fmt.Errorf("condition expected single-key map (got %d keys)", len(raw))
	}
	for kind, v := range raw {
		return decodeConditionKind(kind, v)
	}
	return nil, errors.New("condition: empty map (unreachable)")
}

// UnmarshalEffect decodes a single-key-map effect from YAML
// into its concrete Effect. Same key-discipline convention as
// UnmarshalCondition.
func UnmarshalEffect(raw map[string]any) (Effect, error) {
	if len(raw) != 1 {
		return nil, fmt.Errorf("effect expected single-key map (got %d keys)", len(raw))
	}
	for kind, v := range raw {
		return decodeEffectKind(kind, v)
	}
	return nil, errors.New("effect: empty map (unreachable)")
}

// ----- intermediate YAML types (private to this package) -----

type storyNodeDoc struct {
	Nodes []storyYAMLNode `yaml:"nodes"`
}

type storyYAMLNode struct {
	ID      string            `yaml:"id"`
	Title   string            `yaml:"title"`
	Text    string            `yaml:"text"`
	IsFinal bool              `yaml:"is_final"`
	Choices []storyYAMLChoice `yaml:"choices"`
}

type storyYAMLChoice struct {
	ID         string           `yaml:"id"`
	Text       string           `yaml:"text"`
	Conditions []map[string]any `yaml:"conditions,omitempty"`
	Effects    []map[string]any `yaml:"effects,omitempty"`
	NextNodeID string           `yaml:"next_node_id"`
}

type storyEventDoc struct {
	Events []storyYAMLEvent `yaml:"events"`
}

type storyYAMLEvent struct {
	ID         string           `yaml:"id"`
	NodeID     string           `yaml:"node_id"`
	Conditions []map[string]any `yaml:"conditions,omitempty"`
}

// ----- Conversion helpers (private) -----

func convertYAMLNode(yn storyYAMLNode) (StoryNode, error) {
	if yn.ID == "" {
		return StoryNode{}, errors.New("node has empty id")
	}
	out := StoryNode{
		ID:      yn.ID,
		Title:   yn.Title,
		Text:    yn.Text,
		IsFinal: yn.IsFinal,
	}
	for i, yc := range yn.Choices {
		c, err := convertYAMLChoice(yc)
		if err != nil {
			return StoryNode{}, fmt.Errorf("choice[%d] %q: %w", i, yc.ID, err)
		}
		out.Choices = append(out.Choices, c)
	}
	return out, nil
}

func convertYAMLChoice(yc storyYAMLChoice) (Choice, error) {
	if yc.ID == "" {
		return Choice{}, errors.New("choice has empty id")
	}
	out := Choice{
		ID:         yc.ID,
		Text:       yc.Text,
		NextNodeID: yc.NextNodeID,
	}
	for i, raw := range yc.Conditions {
		cond, err := UnmarshalCondition(raw)
		if err != nil {
			return Choice{}, fmt.Errorf("condition[%d]: %w", i, err)
		}
		out.Conditions = append(out.Conditions, cond)
	}
	for i, raw := range yc.Effects {
		eff, err := UnmarshalEffect(raw)
		if err != nil {
			return Choice{}, fmt.Errorf("effect[%d]: %w", i, err)
		}
		out.Effects = append(out.Effects, eff)
	}
	return out, nil
}

// ----- Polymorphic dispatch (private) -----

func decodeConditionKind(kind string, v any) (Condition, error) {
	switch kind {
	case "flag":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("flag: %w", err)
		}
		return Flag{Key: key}, nil
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
		return VariableGE{Key: key, Value: val}, nil
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
		axis, ok := ParseRelationshipAxis(axisStr)
		if !ok {
			return nil, fmt.Errorf("relationship.axis %q unrecognized (want trust/affection/respect)", axisStr)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("relationship.value: %w", err)
		}
		return RelationshipGE{Character: char, Axis: axis, Value: val}, nil
	case "has_item":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("has_item: %w", err)
		}
		return HasItem{Key: key}, nil
	case "has_ending":
		id, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("has_ending: %w", err)
		}
		return HasEnding{ID: id}, nil
	case "or":
		inner, err := unmarshalConditions(v)
		if err != nil {
			return nil, fmt.Errorf("or: %w", err)
		}
		return Or{Conditions: inner}, nil
	case "and":
		inner, err := unmarshalConditions(v)
		if err != nil {
			return nil, fmt.Errorf("and: %w", err)
		}
		return And{Conditions: inner}, nil
	case "not":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("not: %w", err)
		}
		inner, err := UnmarshalCondition(mm)
		if err != nil {
			return nil, fmt.Errorf("not: %w", err)
		}
		return Not{Inner: inner}, nil
	}
	return nil, fmt.Errorf("unknown condition kind %q", kind)
}

func unmarshalConditions(raw any) ([]Condition, error) {
	list, err := asSlice(raw)
	if err != nil {
		return nil, err
	}
	out := make([]Condition, 0, len(list))
	for i, e := range list {
		mm, err := asMap(e)
		if err != nil {
			return nil, fmt.Errorf("combinator child[%d]: %w", i, err)
		}
		cond, err := UnmarshalCondition(mm)
		if err != nil {
			return nil, fmt.Errorf("combinator child[%d]: %w", i, err)
		}
		out = append(out, cond)
	}
	return out, nil
}

func decodeEffectKind(kind string, v any) (Effect, error) {
	switch kind {
	case "set_flag":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("set_flag: %w", err)
		}
		return SetFlag{Key: key}, nil
	case "clear_flag":
		key, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("clear_flag: %w", err)
		}
		return ClearFlag{Key: key}, nil
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
		return ModifyVariable{Key: key, Value: val}, nil
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
		axis, ok := ParseRelationshipAxis(axisStr)
		if !ok {
			return nil, fmt.Errorf("modify_relationship.axis %q unrecognized (want trust/affection/respect)", axisStr)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("modify_relationship.value: %w", err)
		}
		return ModifyRelationship{Character: char, Axis: axis, Value: val}, nil
	case "modify_reputation":
		mm, err := asMap(v)
		if err != nil {
			return nil, fmt.Errorf("modify_reputation: %w", err)
		}
		factionStr, err := asString(mm["faction"])
		if err != nil {
			return nil, fmt.Errorf("modify_reputation.faction: %w", err)
		}
		faction, ok := ParseFaction(factionStr)
		if !ok {
			return nil, fmt.Errorf("modify_reputation.faction %q unrecognized (want kingdom/mages/dragons/underworld)", factionStr)
		}
		val, err := asInt(mm["value"])
		if err != nil {
			return nil, fmt.Errorf("modify_reputation.value: %w", err)
		}
		return ModifyReputation{Faction: faction, Value: val}, nil
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
		return AddItem{Key: key, Count: count}, nil
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
		return RemoveItem{Key: key, Count: count}, nil
	case "trigger_event":
		id, err := asString(v)
		if err != nil {
			return nil, fmt.Errorf("trigger_event: %w", err)
		}
		return TriggerEvent{ID: id}, nil
	}
	return nil, fmt.Errorf("unknown effect kind %q", kind)
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
