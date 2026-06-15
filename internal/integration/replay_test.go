// Phase 25: Deterministic Replay Validation tests.
//
// These tests are the v1 acceptance gate for Chronicle's
// determinism contract. They prove that:
//
//  1. Running the same seed for the same number of ticks
//     produces byte-identical world state (TestDeterministicReplay).
//  2. Different seeds diverge — the RNG is not a no-op
//     (TestDifferentSeedsDiverge).
//
// The tests run the full 100-year frontier simulation through
// the real production tick pipeline (the same engines, in the
// same order, as the 5-Generation Integration Test). They are
// intentionally slow: 200-400 simulated years of work per run.
//
// Skipped under -short so quick-test runs don't pay the runtime
// cost. The v1 acceptance run does:
//
//	go test -count=1 -timeout 30m ./internal/integration/...
package integration

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
	"github.com/chronicle-dev/chronicle/internal/worldpack"
)

// Replay run constants. Match the Phase 24 v1 acceptance gate
// (100 years, 365 ticks/year = 36,500 ticks) so the replay
// hashes are anchored to the same simulation the rest of the
// v1 test suite exercises.
const (
	replaySeed         = int64(42)
	replayDivergedSeed = int64(43)
	replayYears        = 100
	replayTicksPerYear = 365
	replayTotalTicks   = replayYears * replayTicksPerYear
	replayWorldID      = "frontier"
)

// TestDeterministicReplay is the Phase 25 v1 acceptance gate.
// It bootstraps the frontier worldpack twice with the same
// seed (42), runs each for 100 simulated years, and asserts
// the resulting world hashes are identical.
//
// The test is slow: two full 100-year runs through every
// production engine. The cost is the price of determinism
// verification — without it, a single non-determinism bug
// could silently invalidate every save, branch, and replay.
func TestDeterministicReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Deterministic Replay test in -short mode")
	}

	// 1. Run #1: bootstrap + 100 years + hash.
	hash1, metrics1 := runReplay(t, replaySeed)
	t.Logf("replay run #1: hash=%s", hash1)
	t.Logf("METRICS run #1: %s", metrics1)

	// 2. Run #2: fresh world, same seed, same duration, same hash.
	hash2, metrics2 := runReplay(t, replaySeed)
	t.Logf("replay run #2: hash=%s", hash2)
	t.Logf("METRICS run #2: %s", metrics2)

	// 3. Assert byte-identical hashes. This is the core
	//    acceptance criterion. A failure here means the
	//    simulation is non-deterministic — every save, branch,
	//    and replay is unreliable.
	if hash1 != hash2 {
		t.Fatalf(
			"determinism violation: same seed produced different hashes\n"+
				"  hash run #1: %s\n"+
				"  hash run #2: %s\n"+
				"  the simulation is non-deterministic; investigate the engine\n"+
				"  that introduced the divergence (see WorldHash doc for scope)",
			hash1, hash2,
		)
	}

	// 4. Sanity: the world should be a viable multi-generational
	//    society. We don't re-assert all the Phase 24 metrics
	//    (that's TestFiveGenerationSimulation's job) but a
	//    quick viability check guards against a regression that
	//    "passes" because the world collapsed to zero NPCs.
	if metrics1.Population <= 0 {
		t.Errorf("replay run produced empty world (population=%d)", metrics1.Population)
	}
	if metrics1.Marriages <= 0 {
		t.Errorf("replay run produced no marriages (count=%d)", metrics1.Marriages)
	}
}

// TestDifferentSeedsDiverge is the negative control for
// TestDeterministicReplay. It runs the same simulation with
// two different seeds and asserts the resulting hashes
// differ.
//
// If the two hashes were equal, the determinism guarantee
// would be vacuous: the engine would have to be the no-op
// `hash(zero_state)`. A failure here is much less likely
// than a TestDeterministicReplay failure (different seeds
// take different code paths in the RNG), but worth catching.
func TestDifferentSeedsDiverge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Seed Divergence test in -short mode")
	}

	hash42, metrics42 := runReplay(t, replaySeed)
	t.Logf("seed %d: hash=%s", replaySeed, hash42)
	t.Logf("METRICS seed %d: %s", replaySeed, metrics42)

	hash43, metrics43 := runReplay(t, replayDivergedSeed)
	t.Logf("seed %d: hash=%s", replayDivergedSeed, hash43)
	t.Logf("METRICS seed %d: %s", replayDivergedSeed, metrics43)

	if hash42 == hash43 {
		t.Fatalf(
			"seed divergence check failed: seed %d and seed %d produced the same hash\n"+
				"  shared hash: %s\n"+
				"  this means the RNG is not influencing the world state — either the\n"+
				"  engine has lost its way of consuming the seed, or the hash is\n"+
				"  collapsing state that the seed actually does affect",
			replaySeed, replayDivergedSeed, hash42,
		)
	}
}

// runReplay bootstraps a fresh frontier world with the given
// seed, runs it for replayYears years through the production
// tick pipeline, and returns the world hash plus final
// metrics. Used by both TestDeterministicReplay (twice with
// the same seed) and TestDifferentSeedsDiverge (twice with
// different seeds).
//
// The function is the test-equivalent of the production
// replay loop. Any divergence between this loop and the
// production command-line entry point is a bug in the test
// (because then the test would not exercise the real
// determinism contract).
func runReplay(t *testing.T, seed int64) (string, SimulationMetrics) {
	t.Helper()

	// 1. Bootstrap. The frontier worldpack has a fixed
	//    generation spec (150 NPCs, 51/49 F/M, 4 villages,
	//    Blackwater, Dawn Monastery, etc.) so the bootstrap
	//    shape is identical across runs — only the RNG
	//    sequence changes.
	packDir := filepath.Join("..", "..", "worldpacks", "frontier")
	pack, err := worldpack.Load(packDir)
	if err != nil {
		t.Fatalf("Load(frontier): %v", err)
	}
	w := core.NewWorld(replayWorldID, seed, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := worldpack.Bootstrap(pack, w, seed); err != nil {
		t.Fatalf("Bootstrap(seed=%d): %v", seed, err)
	}

	// 2. Wire the 7 production engines in the canonical
	//    order. The order is the determinism contract — see
	//    SIMULATION_TICK_SPEC.md §2. Reordering the engines
	//    is a deterministic change to the simulation, so the
	//    test would fail (correctly) after such a reordering.
	sim := tick.NewSimulation(
		seed,
		simulation.NewPopulationEngine(),
		simulation.NewRelationshipEngine(),
		simulation.NewMarriageEngine(),
		simulation.NewMemoryEngine(),
		simulation.NewGoalEngine(),
		simulation.NewEconomyEngine(),
		simulation.NewEventEngine(),
	)
	if err := sim.Init(w); err != nil {
		t.Fatalf("sim.Init(seed=%d): %v", seed, err)
	}

	// 3. Run. We log progress every 25 years instead of every
	//    10 (the replay test is slower than the 5-generation
	//    test because it runs two full 100-year simulations
	//    serially, so per-year logging would be noisy).
	start := time.Now()
	for year := 1; year <= replayYears; year++ {
		for tick := 0; tick < replayTicksPerYear; tick++ {
			if err := sim.Tick(w); err != nil {
				t.Fatalf("Tick(seed=%d year=%d inner=%d): %v", seed, year, tick, err)
			}
		}
		if year%25 == 0 || year == replayYears {
			pop := len(w.LivingPeople())
			t.Logf("seed=%d year=%3d: tick=%d  pop=%d  mem=%d  rel=%d  evt=%d",
				seed, year, w.Tick, pop,
				len(w.Memories), len(w.Relationships), len(w.Events))
		}
	}
	elapsed := time.Since(start)
	t.Logf("seed=%d simulation complete in %s (%.1fms/tick avg)",
		seed, elapsed, float64(elapsed.Milliseconds())/float64(replayTotalTicks))

	// 4. Hash. The hash is the canonical fingerprint of the
	//    final world state. Two runs with the same seed and
	//    tick count must produce the same hash; the test
	//    asserts that.
	hash := core.WorldHash(w)

	// 5. Metrics. Same struct as TestFiveGenerationSimulation
	//    so the two test outputs are directly comparable.
	metrics := collectMetrics(w, replayTotalTicks)

	// 6. Hash and final metrics. The test caller decides
	//    whether the values match its expectations.
	return hash, metrics
}
