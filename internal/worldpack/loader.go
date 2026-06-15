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
//
// The first return value is the resolved absolute path that was
// actually loaded (post-walk-up). Callers that need to read sibling
// files in the same worldpack (e.g., llm.yaml) should use this path
// rather than the raw input dir, which may have been CWD-relative
// and unreachable from the caller's working directory.
//
// The dir argument is resolved via resolvePackDir, which tries:
//  1. The path as given (CWD-relative or absolute).
//  2. Relative to the running binary's directory (handles the case
//     where the user invokes chronicle from any working directory).
//  3. A walk-up from CWD looking for a worldpacks/frontier sibling
//     (handles the case where the user is in a subdirectory of the
//     project root).
//
// Without this fallback, `chronicle new` only works when invoked
// from the project root — a friction point for v1 playability.
func Load(dir string) (string, *Pack, error) {
	resolved, err := resolvePackDir(dir)
	if err != nil {
		return "", nil, err
	}
	dir = resolved

	var ef entitiesFile
	if err := readYAML(filepath.Join(dir, "entities.yaml"), &ef); err != nil {
		return "", nil, fmt.Errorf("worldpack: load entities.yaml: %w", err)
	}

	var ff factionsFile
	if err := readYAML(filepath.Join(dir, "factions.yaml"), &ff); err != nil {
		return "", nil, fmt.Errorf("worldpack: load factions.yaml: %w", err)
	}

	var af actionsFile
	if err := readYAML(filepath.Join(dir, "actions.yaml"), &af); err != nil {
		return "", nil, fmt.Errorf("worldpack: load actions.yaml: %w", err)
	}

	var of occupationsFile
	if err := readYAML(filepath.Join(dir, "occupations.yaml"), &of); err != nil {
		return "", nil, fmt.Errorf("worldpack: load occupations.yaml: %w", err)
	}

	var rules RulesSpec
	if err := readYAML(filepath.Join(dir, "rules.yaml"), &rules); err != nil {
		return "", nil, fmt.Errorf("worldpack: load rules.yaml: %w", err)
	}

	var generation GenerationSpec
	if err := readYAML(filepath.Join(dir, "generation.yaml"), &generation); err != nil {
		return "", nil, fmt.Errorf("worldpack: load generation.yaml: %w", err)
	}

	return dir, &Pack{
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

// ResolveDir returns the absolute path to the worldpack directory
// for the given input. Exported wrapper around resolvePackDir so
// callers that need to read sibling files (e.g., llm.yaml) without
// loading the whole Pack can still get the walk-up-resolved path.
func ResolveDir(dir string) (string, error) {
	return resolvePackDir(dir)
}

// resolvePackDir tries the given path first, then falls back to
// binary-relative and walk-up resolutions. This allows `chronicle
// new` to work from any directory, not just the project root.
//
// Resolution order:
//  1. The path as given (CWD-relative or absolute). If entities.yaml
//     exists in that directory, use it.
//  2. Relative to the running binary's directory (../worldpacks/<name>
//     and ../../worldpacks/<name>). Handles the common case where
//     the binary lives in the project root and the user invokes it
//     from a different CWD.
//  3. Walk up from CWD looking for a worldpacks/<basename> sibling.
//     Handles the case where the user is inside a subdirectory of
//     the project (e.g., ./cmd/chronicle during development).
//
// Returns the resolved absolute path, or an error if none of the
// candidates exist. The error message lists what was tried so the
// user can diagnose a missing pack quickly.
func resolvePackDir(dir string) (string, error) {
	basename := filepath.Base(dir)

	// 1. Try the path as given.
	if abs, err := filepath.Abs(dir); err == nil {
		if isPackDir(abs) {
			return abs, nil
		}
	}

	// 2. Walk up from the binary's directory.
	if exe, err := os.Executable(); err == nil {
		for d := filepath.Dir(exe); d != "/" && d != "." && d != filepath.Dir(d); d = filepath.Dir(d) {
			candidate := filepath.Join(d, "worldpacks", basename)
			if isPackDir(candidate) {
				return candidate, nil
			}
		}
	}

	// 3. Walk up from CWD.
	if cwd, err := os.Getwd(); err == nil {
		for d := cwd; d != "/" && d != "." && d != filepath.Dir(d); d = filepath.Dir(d) {
			candidate := filepath.Join(d, "worldpacks", basename)
			if isPackDir(candidate) {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("worldpack directory %q not found (tried: CWD-relative, walk-up from binary dir, walk-up from CWD)", dir)
}

// isPackDir returns true if path is a directory containing entities.yaml
// (the canonical worldpack marker file). Used by resolvePackDir to
// test candidate paths without triggering a full Load.
func isPackDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(path, "entities.yaml"))
	return err == nil
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
