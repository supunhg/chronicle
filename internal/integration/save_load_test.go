// Phase 26 Part A: Save/Load Round Trip.
//
// TestSaveLoadRoundTrip is the v1 acceptance gate for Chronicle's
// persistence contract. It proves that Snapshot followed by Restore
// yields a world whose core.WorldHash is identical to the pre-save
// hash. A divergence here means the persistence layer is dropping
// state (migrations silently truncated, fields not written, etc.)
// and the determinism guarantees from Phase 25 are invalid.
//
// # Procedure
//
//  1. Bootstrap the frontier worldpack with seed 42.
//  2. Run 100 simulated years through the real production tick
//     pipeline (same 7 engines, same order as the Phase 24 and
//     Phase 25 tests).
//  3. Compute hash_before := core.WorldHash(world).
//  4. Open a fresh SQLite DB, Migrate to schema v4, Snapshot the
//     world, Close.
//  5. Open a fresh core.World, Restore from the DB, Close.
//  6. Compute hash_after := core.WorldHash(restored).
//  7. Assert hash_before == hash_after.
//
// The test is intentionally slow: a 100-year frontier run plus a
// full save/load round-trip. Skipped under -short.
package integration

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/persistence"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
	"github.com/chronicle-dev/chronicle/internal/worldpack"
)

// Save/load round-trip test constants. Match the Phase 24 and
// Phase 25 acceptance gates (100 years, 365 ticks/year = 36,500
// ticks) so all three test suites exercise the same horizon.
const (
	saveLoadSeed         = int64(42)
	saveLoadYears        = 100
	saveLoadTicksPerYear = 365
	saveLoadTotalTicks   = saveLoadYears * saveLoadTicksPerYear
	saveLoadWorldID      = "frontier"
)

// TestSaveLoadRoundTrip is the Phase 26 Part A acceptance gate.
// It bootstraps the frontier worldpack, runs 100 simulated years
// through the production tick pipeline, snapshots the result to
// SQLite, restores from SQLite into a fresh world, and asserts the
// pre-save and post-load world hashes are identical.
//
// The test is slow: a 100-year frontier run plus a full save/load
// round-trip (which involves JSON-encoding every person, faction,
// location, event, and item, plus a SQLite write/read pass). The
// total cost is comparable to a single TestDeterministicReplay run.
//
// Skipped under -short so quick-test runs don't pay the runtime
// cost. The v1 acceptance run does:
//
//	go test -count=1 -timeout 60m ./internal/integration/...
func TestSaveLoadRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Save/Load Round Trip test in -short mode")
	}

	// 1. Bootstrap.
	packDir := filepath.Join("..", "..", "worldpacks", "frontier")
	_, pack, err := worldpack.Load(packDir)
	if err != nil {
		t.Fatalf("Load(frontier): %v", err)
	}
	start := time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC)
	w := core.NewWorld(saveLoadWorldID, saveLoadSeed, start)
	if err := worldpack.Bootstrap(pack, w, saveLoadSeed); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Logf("bootstrap complete: %d people, %d locations, %d factions, %d items",
		len(w.People), len(w.Locations), len(w.Factions), len(w.Items))

	// 2. Wire the 7 production engines and run 100 years. The
	//    order is the determinism contract (Phase 24 v1): Pop →
	//    Rel → Mar → Mem → Goal → Eco → Evt.
	sim := tick.NewSimulation(
		saveLoadSeed,
		simulation.NewPopulationEngine(),
		simulation.NewRelationshipEngine(),
		simulation.NewMarriageEngine(),
		simulation.NewMemoryEngine(),
		simulation.NewGoalEngine(),
		simulation.NewEconomyEngine(),
		simulation.NewEventEngine(),
	)
	if err := sim.Init(w); err != nil {
		t.Fatalf("sim.Init: %v", err)
	}
	startTime := time.Now()
	for year := 1; year <= saveLoadYears; year++ {
		for i := 0; i < saveLoadTicksPerYear; i++ {
			if err := sim.Tick(w); err != nil {
				t.Fatalf("Tick at year=%d inner=%d: %v", year, i, err)
			}
		}
		if year%25 == 0 || year == saveLoadYears {
			t.Logf("seed=%d year=%3d: tick=%d  pop=%d  mem=%d  rel=%d  evt=%d",
				saveLoadSeed, year, w.Tick,
				len(w.LivingPeople()),
				len(w.Memories), len(w.Relationships), len(w.Events))
		}
	}
	t.Logf("simulation complete in %s (%.1fms/tick avg)",
		time.Since(startTime),
		float64(time.Since(startTime).Milliseconds())/float64(saveLoadTotalTicks))

	// 3. Hash before save.
	hashBefore := core.WorldHash(w)
	t.Logf("hash_before save: %s", hashBefore)

	// 4. Snapshot to a fresh DB.
	dbPath := filepath.Join(t.TempDir(), "save-load-rt.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Snapshot(w); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Logf("snapshot written to %s", dbPath)

	// 5. Restore into a fresh world.
	db2, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) for restore: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	loaded := core.NewWorld("", 0, time.Time{})
	if err := db2.Restore(loaded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := db2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 6. Hash after load.
	hashAfter := core.WorldHash(loaded)
	t.Logf("hash_after  load: %s", hashAfter)

	// 7. Assert identical hashes. The core assertion of the
	//    Phase 26 Part A acceptance gate. A failure here means
	//    the persistence layer is dropping state and the
	//    determinism guarantees from Phase 25 are not actually
	//    load-bearing.
	if hashBefore != hashAfter {
		t.Fatalf(
			"save/load round-trip diverged: persistence is dropping state\n"+
				"  hash_before: %s\n"+
				"  hash_after:  %s\n"+
				"  investigate: Snapshot/Restore coverage in internal/persistence/snapshot.go",
			hashBefore, hashAfter,
		)
	}

	// 8. Sanity checks: the loaded world is a viable
	//    multi-generational society (matches the Phase 24
	//    acceptance bounds). A world that hash-matches but
	//    has 0 people would be a bug in the hash, not the
	//    persistence layer, but a quick viability check guards
	//    against that.
	metrics := collectMetrics(loaded, saveLoadTotalTicks)
	t.Logf("METRICS after load: %s", metrics)
	if metrics.Population <= 0 {
		t.Errorf("loaded world has no living people (population=%d)", metrics.Population)
	}
	if metrics.Marriages <= 0 {
		t.Errorf("loaded world has no marriages (count=%d)", metrics.Marriages)
	}
}
