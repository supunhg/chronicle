package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
)

// TickRand returns a deterministic *rand.Rand seeded by
// (worldSeed + tick). Two callers passing the same (worldSeed, tick)
// MUST get identical streams.
//
// Use this for tick-level randomness that is not tied to a specific
// entity (e.g. global event probability rolls in content authoring
// tools, or for test/fuzz infrastructure in this engine package).
//
// v2's player-path production code does NOT call TickRand — see
// ARCHITECTURE.md §18A. It is exported here so that any future v2
// code path needing deterministic per-tick RNG has a single
// canonical seeding rule to follow.
//
// Ported from v1 internal/tick/rng.go (deleted in Phase 35.F).
func TickRand(worldSeed, tick int64) *rand.Rand {
	return rand.New(rand.NewSource(worldSeed + tick))
}

// EntityRand returns a deterministic *rand.Rand seeded by
// (worldSeed, tick, entityID). Different entities get different
// streams for the same tick; the same entity always gets the same
// stream across runs.
//
// Use this for any per-entity randomness (e.g. an NPC's daily
// decision roll in authoring tools or test infrastructure).
//
// v2's player-path production code does NOT call EntityRand. It is
// exported for the same reasons as TickRand above.
//
// Ported from v1 internal/tick/rng.go (deleted in Phase 35.F) with
// identical semantics: SHA-256 of "worldSeed:tick:entityID", first
// 8 bytes read as big-endian uint64, interpreted as int64.
func EntityRand(worldSeed, tick int64, entityID string) *rand.Rand {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", worldSeed, tick, entityID)))
	seed := int64(binary.BigEndian.Uint64(h[:8]))
	return rand.New(rand.NewSource(seed))
}
