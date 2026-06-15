package worldpack

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// TestIntegration_LoadBootstrapRun loads the frontier pack, bootstraps
// a fresh world, wires up the simulation engines, and runs 100 ticks.
// Asserts no panics and that the world state remains internally
// consistent (alive/dead counts add up, no duplicate IDs).
func TestIntegration_LoadBootstrapRun(t *testing.T) {
	_, pack, err := Load(filepath.Clean(frontierDir))
	if err != nil {
		t.Skipf("frontier pack not available: %v", err)
	}

	w := core.NewWorld("integ", 12345, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := Bootstrap(pack, w, 12345); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	sim := tick.NewSimulation(12345,
		simulation.NewPopulationEngine(),
		simulation.NewRelationshipEngine(),
		simulation.NewGoalEngine(),
	)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}

	const targetTicks = 100
	for i := 0; i < targetTicks; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick %d: %v", i, err)
		}
		if w.Tick != int64(i+1) {
			t.Errorf("after %d ticks, w.Tick=%d", i+1, w.Tick)
		}
	}

	// No duplicate IDs (the AddPerson panic would have already
	// caught this, but be explicit).
	seen := make(map[string]bool, len(w.People))
	for id := range w.People {
		if seen[id] {
			t.Errorf("duplicate person ID: %s", id)
		}
		seen[id] = true
	}

	// Population in a sane range: should not have crashed, should not
	// be wildly larger or smaller than 150. Births and deaths can move
	// the number, but a 100-tick run is short (about 4 months in
	// 1-day ticks — well under a year), so population should be near
	// 150 (the engines run 1 tick = 1 day; 100 ticks = 100 days).
	alive := 0
	for _, p := range w.People {
		if p.Alive {
			alive++
		}
	}
	if alive < 100 || alive > 200 {
		t.Errorf("population after 100 ticks out of range: %d", alive)
	}

	// Locations still have correct population sums (recompute to be sure)
	w.RecomputeLocationPopulations()
	sum := 0
	ids := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		sum += w.Locations[id].Population
	}
	if got := sum + len(w.LivingPeopleAt("")); got != alive {
		t.Errorf("location pop sum %d + travelers %d != alive count %d", sum, len(w.LivingPeopleAt("")), got)
	}
}
