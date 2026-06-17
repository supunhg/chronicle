package endings

import (
	"fmt"

	"github.com/chronicle-dev/chronicle/internal/story"
	"gopkg.in/yaml.v3"
)

// EndingsFromYAML parses an endings.yaml document per the schema
// documented in docs/choice-yaml.md (PHASES.md §37.B +
// ARCHITECTURE.md §19).
//
// EndingsFromYAML is the canonical entry point for
// YAML→endings. internal/content/loader.go::readEndings
// delegates to it. Condition parsing delegates to
// story.UnmarshalCondition so the spec doc, the schema test,
// and the production loader cannot drift apart.
//
// EndingsFromYAML lives in package endings (not story) because
// the returned type is []endings.Ending, an endings-package
// type — placing the parser in its own return-type package
// matches the per-file-type "single canonical parser" rule
// from Phase 37.A without creating an import cycle (story
// does not import endings).
func EndingsFromYAML(data []byte) ([]Ending, error) {
	var doc endingsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("endings: EndingsFromYAML: parse: %w", err)
	}
	out := make([]Ending, 0, len(doc.Endings))
	for i, ye := range doc.Endings {
		if ye.ID == "" {
			return nil, fmt.Errorf("endings: EndingsFromYAML: ending[%d] has empty id", i)
		}
		en := Ending{
			ID:       ye.ID,
			Priority: ye.Priority,
		}
		for j, raw := range ye.Conditions {
			cond, err := story.UnmarshalCondition(raw)
			if err != nil {
				return nil, fmt.Errorf("endings: EndingsFromYAML: ending[%d] %q condition[%d]: %w", i, ye.ID, j, err)
			}
			en.Conditions = append(en.Conditions, cond)
		}
		out = append(out, en)
	}
	return out, nil
}

// ----- intermediate YAML types (private to this package) -----

type endingsDoc struct {
	Endings []yamlEnding `yaml:"endings"`
}

type yamlEnding struct {
	ID         string           `yaml:"id"`
	Priority   int              `yaml:"priority"`
	Conditions []map[string]any `yaml:"conditions,omitempty"`
}
