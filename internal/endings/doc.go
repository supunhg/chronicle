// Package endings evaluates endings at finale per ARCHITECTURE.md §19.
//
// Phase 36.D will land endings.go containing:
//
//   - Ending struct (ID, Priority, Conditions) per §19.
//   - Evaluate(ws state.WorldState, endings []Ending) Ending —
//     "highest valid" priority wins, with deterministic ID order
//     breaking ties.
//
// Twelve endings ship per §20: Hero, Dragon Sovereign, World
// Guardian, Archmage, Shadow Lord, Corruption, Kingdom, Dragon
// Alliance, Elara Romance, Selene Romance, Orion Romance, Wanderer.
//
// Each ending's Conditions determine when it is "valid". Evaluate
// returns the highest-priority ending that is currently valid; if
// none are valid, the engine surfaces a fallback message and
// (per Phase 38.E's TestAllEndingsReachable gate) this branch
// must never be reached in production playthroughs.
package endings
