// Package tick provides the orchestration layer for the simulation.
//
// This file implements the deterministic RNG contract from
// SIMULATION_TICK_SPEC.md §3. There MUST be no other source of randomness
// in the simulation.
package tick

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
)

// TickRand returns a deterministic *rand.Rand seeded by
// (worldSeed + tick). Two callers passing the same (worldSeed, tick)
// get identical streams.
//
// Use this for tick-level randomness that is not tied to a specific
// entity (e.g. global event probability rolls).
func TickRand(worldSeed, tick int64) *rand.Rand {
	return rand.New(rand.NewSource(worldSeed + tick))
}

// EntityRand returns a deterministic *rand.Rand seeded by
// (worldSeed, tick, entityID). Different entities get different streams
// for the same tick; the same entity always gets the same stream.
//
// Use this for any per-entity randomness (e.g. an NPC's daily death roll).
func EntityRand(worldSeed, tick int64, entityID string) *rand.Rand {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", worldSeed, tick, entityID)))
	seed := int64(binary.BigEndian.Uint64(h[:8]))
	return rand.New(rand.NewSource(seed))
}
