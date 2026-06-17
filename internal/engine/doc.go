// Package engine implements the v2 story engine (orchestrator) per
// ARCHITECTURE.md §22 and §23.
//
// Phase 36.A landed runner.go, which orchestrates the §23 Runtime
// Flow. The Runner type is the engine's primary entry point;
// Engine holds the pluggable I/O sub-systems (Renderer, ChoiceProvider,
// Endings registry, OnFinale callback).
//
// Phase 36.A also lands minimally-permissioned stub types across
// the v2 dependency set so Runner.Step compiles end-to-end:
//
//   - internal/story/    StoryGraph + StoryNode + Choice + Event
//                        (§5, §6, §13) + Condition interface +
//                        Flag concrete (§7) + Effect interface +
//                        SetFlag/ClearFlag concretes (§8) +
//                        AvailableChoices helper.
//   - internal/state/    WorldState (§4) + Relationship (§9) +
//                        ReputationState (§11) + Inventory (§12) +
//                        SaveGame + JSON marshal (§18).
//   - internal/endings/  Ending + Evaluate (highest-valid-wins,
//                        tie-broken by ID per §18A).
//   - internal/ui/       Renderer interface + BufferRenderer
//                        (Phase 36.F cli.go will land a TTY reader).
//
// The full §23 Runtime Flow implemented in runner.go:
//
//	Load Node → AvailableChoices (Conditions) → Render Choice
//	  → Renderer.RenderChoices → ChoiceProvider.Select →
//	  Apply Effects on WorldState → Set CurrentNodeID + Tick++ →
//	  if next node is IsFinal: Evaluate Endings → OnFinale.
//
// Phase 36.B-F will fill in concrete Condition types
// (VariableGE, RelationshipGE, HasItem, HasEnding, ...) and more
// Effect types (ModifyVariable, ModifyRelationship, AddItem, ...),
// the YAML loader for content/acts/*.yaml, and a TTY-reading
// Renderer / ChoiceProvider for the actual player CLI.
//
// The Phase 36 scaffold also landed engine/seedhash.go, which ports
// the deterministic TickRand / EntityRand helpers from v1
// internal/tick/rng.go (deleted in Phase 35.F). v2 has no runtime
// RNG in the player path (§18A); seedhash.go is for content
// authoring tools and test infrastructure.
package engine
