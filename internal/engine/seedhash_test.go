package engine

import "testing"

// Verifies ARCHITECTURE.md §18A determinism invariant #2 plus the
// entityRand determinism rule applied to TickRand: two callers with
// the same (worldSeed, tick) MUST get identical RNG streams.
//
// This test is the canonical regression check for the determinism
// contract. If you break it, you have introduced non-determinism
// into what should be a pure function of (worldSeed, tick).
func TestTickRand_SameArgsProduceSameValue(t *testing.T) {
	r1 := TickRand(42, 100)
	r2 := TickRand(42, 100)
	if v1, v2 := r1.Int63(), r2.Int63(); v1 != v2 {
		t.Errorf("TickRand(42,100) diverged: %d vs %d", v1, v2)
	}
}

// Verifies the EntityRand determinism rule (same-args-same-stream),
// which is the canonical regression check for the per-entity RNG.
func TestEntityRand_SameArgsProduceSameValue(t *testing.T) {
	r1 := EntityRand(42, 100, "alice")
	r2 := EntityRand(42, 100, "alice")
	if v1, v2 := r1.Int63(), r2.Int63(); v1 != v2 {
		t.Errorf("EntityRand(42,100,alice) diverged: %d vs %d", v1, v2)
	}
}

// Verifies the second invariant of EntityRand: two different entities
// at the same (worldSeed, tick) MUST get different RNG streams.
// In practice they could collide (SHA-256 collisions), but the
// probability is 1/2^63 and a collision in test would indicate a
// real-world collision, not a bug we can fix.
func TestEntityRand_DifferentEntitiesDiffer(t *testing.T) {
	r1 := EntityRand(42, 100, "alice")
	r2 := EntityRand(42, 100, "bob")
	if v1, v2 := r1.Int63(), r2.Int63(); v1 == v2 {
		t.Errorf("EntityRand(42,100,alice)==EntityRand(42,100,bob)=%d (collision or bug)", v1)
	}
}

// Verifies that TickRand changes its stream when tick increments —
// iterates 10 ticks at the same worldSeed and asserts each produces
// a different first-Int63() value. Collision rate for two random
// int64s is 1/2^63, so all 10 distinct is a deterministic contract,
// not a probabilistic one. Any collision is a real bug.
func TestTickRand_DifferentTicksDiffer(t *testing.T) {
	values := make(map[int64]struct{})
	for tick := int64(0); tick < 10; tick++ {
		r := TickRand(42, tick)
		values[r.Int63()] = struct{}{}
	}
	if len(values) != 10 {
		t.Errorf("TickRand over 10 ticks produced %d distinct Int63() values; expected 10 (collision rate 1/2^63 per pair)", len(values))
	}
}
