// Package state manages the v2 save-game state per
// ARCHITECTURE.md §4 and §18.
//
// Phase 36.C will land two files:
//
//   - world.go  WorldState struct (§4) — Tick, Protagonist,
//                CurrentNodeID, Flags, Variables, Relationships,
//                Reputation, Inventory, Party, EndingsUnlocked.
//                All fields are part of the canonical save format.
//   - save.go   SaveGame struct (§18) — Version, WorldState;
//                canonical JSON marshal/unmarshal that produces
//                byte-stable round-trips per §18A invariants.
//
// v2 has no runtime-only state — every WorldState field is
// serialized to disk on save, and a load reproduces exact equality
// under struct-equality after canonical JSON normalization (sorted
// keys, normalized numbers per §18A invariant #1).
//
// WorldHash(w) is the SHA-256 over canonicalized SaveGame JSON.
// It is exposed for save-load regression tests (Phase 39.A,
// TestSaveLoadRoundTrip) — NOT for cross-session reproducibility
// (v2 has no simulation in the player path, so there is no
// "same simulation, different machine" cross-check).
package state
