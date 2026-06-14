package worldpack

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// entitiesFile is the on-disk shape of entities.yaml. It has multiple
// top-level keys, so it needs its own struct distinct from Pack.
type entitiesFile struct {
	Region      Region         `yaml:"region"`
	Locations   []LocationSpec `yaml:"locations"`
	TradeRoutes []TradeRoute   `yaml:"trade_routes"`
	Geography   Geography      `yaml:"geography"`
}

// factionsFile is the on-disk shape of factions.yaml. The top-level
// "factions" key wraps the list.
type factionsFile struct {
	Factions []FactionSpec `yaml:"factions"`
}

// actionsFile is the on-disk shape of actions.yaml. The "contextual"
// key holds the list of action rules.
type actionsFile struct {
	Contextual []ActionRuleSpec `yaml:"contextual"`
}

// occupationsFile is the on-disk shape of occupations.yaml. The
// top-level "occupations" key wraps the list.
type occupationsFile struct {
	Occupations []OccupationSpec `yaml:"occupations"`
}

// Load reads all six YAML files from the given directory and returns
// a Pack. The directory must contain: entities.yaml, factions.yaml,
// actions.yaml, occupations.yaml, rules.yaml, generation.yaml.
//
// Load returns an error on the first file that is missing or fails to
// parse; no partial Pack is returned.
func Load(dir string) (*Pack, error) {
	var ef entitiesFile
	if err := readYAML(filepath.Join(dir, "entities.yaml"), &ef); err != nil {
		return nil, fmt.Errorf("worldpack: load entities.yaml: %w", err)
	}

	var ff factionsFile
	if err := readYAML(filepath.Join(dir, "factions.yaml"), &ff); err != nil {
		return nil, fmt.Errorf("worldpack: load factions.yaml: %w", err)
	}

	var af actionsFile
	if err := readYAML(filepath.Join(dir, "actions.yaml"), &af); err != nil {
		return nil, fmt.Errorf("worldpack: load actions.yaml: %w", err)
	}

	var of occupationsFile
	if err := readYAML(filepath.Join(dir, "occupations.yaml"), &of); err != nil {
		return nil, fmt.Errorf("worldpack: load occupations.yaml: %w", err)
	}

	var rules RulesSpec
	if err := readYAML(filepath.Join(dir, "rules.yaml"), &rules); err != nil {
		return nil, fmt.Errorf("worldpack: load rules.yaml: %w", err)
	}

	var generation GenerationSpec
	if err := readYAML(filepath.Join(dir, "generation.yaml"), &generation); err != nil {
		return nil, fmt.Errorf("worldpack: load generation.yaml: %w", err)
	}

	return &Pack{
		Region:      ef.Region,
		Locations:   ef.Locations,
		TradeRoutes: ef.TradeRoutes,
		Geography:   ef.Geography,
		Factions:    ff.Factions,
		Occupations: of.Occupations,
		ActionRules: af.Contextual,
		Rules:       rules,
		Generation:  generation,
	}, nil
}

// readYAML reads a file at path and unmarshals it into dst.
func readYAML(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
