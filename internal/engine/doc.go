// Package engine implements the v2 story engine (orchestrator) per
// ARCHITECTURE.md §22 and §23.
//
// Phase 36.A will land runner.go, which orchestrates the §23 Runtime
// Flow (Load Save → Load Node → Evaluate Conditions → Render Story →
// Render Choices → Player Selection → Apply Effects → Check Events →
// Load Next Node).
//
// The Phase 36 scaffold lands two files:
//
//   - engine/doc.go       this file (package-level doc).
//   - engine/seedhash.go  deterministic RNG helpers (TickRand,
//     EntityRand) ported from v1 internal/tick/rng.go, which was
//     retired in Phase 35.F.
//
// v2 has no runtime RNG in the player path (ARCHITECTURE.md §18A:
// "no math/rand global functions"). TickRand / EntityRand are
// provided for: (a) content authoring tools (which may need to
// express "X% chance" outcomes at design time) and (b) test/fuzz
// infrastructure in this engine package. No production code path on
// the player cycle calls them.
package engine
