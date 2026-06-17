// Package content loads authored v2 YAML content per
// ARCHITECTURE.md §21 and PHASES.md §37 (Content YAML Schema).
//
// Phase 36.E will land loader.go which reads:
//
//   - content/protagonists/*.yaml → []CharacterProfile (§15).
//   - content/companions/*.yaml   → map[string]Relationship[] (§9).
//   - content/acts/act*.yaml      → []StoryNode (§5) merged into a
//                                   single StoryGraph keyed by ID.
//   - content/endings.yaml        → []Ending (§19).
//
// loader.go is fail-fast on any reference error: broken NodeIDs,
// missing companion YAMLs, undeclared flags/variables, duplicate
// IDs. Per §18A invariant #3, content is content-addressed (hashed)
// at load; hash mismatches are a hard error.
//
// Phase 37 (the YAML schema spec) lands BEFORE Phase 36.E: loader.go
// cannot be implemented until §37.A–§37.E resolve the on-disk schema.
// The Phase 36 scaffold lands only this doc.go file so the package
// exists in the build graph from day one.
package content
