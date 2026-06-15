// Package integration contains cross-engine integration tests
// that exercise the full simulation pipeline (Population,
// Relationship, Memory, Goal, Economy, Event, Marriage engines
// wired through the real tick.Simulation orchestration).
//
// Phase 24 v1 ships the 5-Generation Integration Test, the
// v1 acceptance gate. The test bootstraps the frontier
// worldpack, runs 100 simulated years (36,500 ticks) through
// the production tick loop, and asserts that the world
// produces a viable multi-generational society.
//
// The test is NOT a unit test. It is slow (minutes), uses the
// real frontier worldpack, and exercises every engine in the
// real production order. It is gated behind `testing.Short()`
// so quick-test runs skip it; the v1 acceptance run does
// `go test -count=1 ./internal/integration/...`.
package integration

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
	"github.com/chronicle-dev/chronicle/internal/worldpack"
)

// SimulationMetrics is the per-run summary collected at the
// end of TestFiveGenerationSimulation. Printed to stdout and
// (for some fields) asserted against minimums. See the spec
// for the rationale (metrics for observation, assertions for
// regression catches).
type SimulationMetrics struct {
	// Population is the number of living people at the end of
	// the run.
	Population int

	// Births is the number of people born during the run
	// (BirthTick > 0). Does not count the initial bootstrap
	// population.
	Births int

	// Deaths is the number of people who died during the run
	// (DeathTick > 0).
	Deaths int

	// Marriages is the number of distinct married couples at
	// the end of the run (both spouses alive).
	Marriages int

	// LivingFamilies is the number of distinct family units
	// (a parent with at least one child, alive or dead).
	// V1: approximate, counted as the number of distinct
	// parent IDs referenced by any child.
	LivingFamilies int

	// MaxFamilyDepth is the longest ancestry chain from an
	// initial-bootstrap person to a current descendant. 0
	// means no one has had children. 5+ means 5 generations
	// have lived.
	MaxFamilyDepth int

	// RelationshipCount is the final size of w.Relationships.
	RelationshipCount int

	// MemoryCount is the final size of w.Memories.
	MemoryCount int

	// EventCount is the final size of w.Events.
	EventCount int

	// FinalTick is w.Tick at the end of the run.
	FinalTick int64
}

// String returns a human-readable one-line-per-field summary
// suitable for t.Logf.
func (m SimulationMetrics) String() string {
	return fmt.Sprintf(
		"Population=%d  Births=%d  Deaths=%d  Marriages=%d  "+
			"LivingFamilies=%d  MaxFamilyDepth=%d  "+
			"Relationships=%d  Memories=%d  Events=%d  "+
			"FinalTick=%d",
		m.Population, m.Births, m.Deaths, m.Marriages,
		m.LivingFamilies, m.MaxFamilyDepth,
		m.RelationshipCount, m.MemoryCount, m.EventCount,
		m.FinalTick,
	)
}

// TestFiveGenerationSimulation is the Phase 24 v1 acceptance
// gate. It bootstraps the frontier worldpack, runs 100
// simulated years (36,500 ticks) through the real production
// tick pipeline, and asserts that the world remains viable.
//
// The test is intentionally slow: minutes, not seconds. The
// production tick loop is O(N^2) per tick in the worst case
// (RelationshipEngine's co-location bond formation) and the
// 36,500-tick run with 150-200 NPCs is the only way to
// discover latent bugs (event spam, infinite memory growth,
// family-tree blow-ups, economic collapse).
//
// Skipped under -short so quick-test runs don't pay the
// runtime cost. To run the full acceptance test, set a long
// timeout (the 100-year run can take several minutes):
//
//	go test -count=1 -timeout 30m ./internal/integration/...
func TestFiveGenerationSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 5-Generation Integration Test in -short mode")
	}

	const (
		seed         = int64(42)
		yearsToRun   = 100
		ticksPerYear = 365
		totalTicks   = yearsToRun * ticksPerYear
	)

	// 1. Bootstrap the frontier worldpack.
	packDir := filepath.Join("..", "..", "worldpacks", "frontier")
	_, pack, err := worldpack.Load(packDir)
	if err != nil {
		t.Fatalf("Load(frontier): %v", err)
	}
	w := core.NewWorld("frontier", seed, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := worldpack.Bootstrap(pack, w, seed); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	initialPop := len(w.People)
	t.Logf("bootstrap complete: %d people, %d locations, %d factions",
		initialPop, len(w.Locations), len(w.Factions))

	// 2. Wire the 7 production engines in the canonical order.
	//    Order: Population -> Relationship -> Marriage ->
	//    Memory -> Goal -> Economy -> Event. Marriage runs
	//    immediately after Relationship so the trust score is
	//    current when matches are evaluated, and immediately
	//    before Memory so marriage-related memories (future
	//    phase) can be written in the same tick. Economy and
	//    Event run last (the Economy feedback loop runs after
	//    the action-selection loop, so Goal must come before
	//    Economy; Event observes the post-Economy state).
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
		t.Fatalf("Init: %v", err)
	}

	// 3. Run the simulation. Report progress every 10 years
	//    so a hung test can be diagnosed.
	start := time.Now()
	for year := 1; year <= yearsToRun; year++ {
		for tick := 0; tick < ticksPerYear; tick++ {
			if err := sim.Tick(w); err != nil {
				t.Fatalf("Tick at year=%d inner=%d: %v", year, tick, err)
			}
		}
		if year%10 == 0 || year == yearsToRun {
			pop := len(w.LivingPeople())
			t.Logf("year %3d: tick=%d  pop=%d  births=%d  deaths=%d  marriages=%d  mem=%d",
				year, w.Tick, pop,
				countBirths(w), countDeaths(w),
				simulation.MarriageCount(w), len(w.Memories))
		}
	}
	elapsed := time.Since(start)
	t.Logf("simulation complete in %s (%.1fms/tick avg)",
		elapsed, float64(elapsed.Milliseconds())/float64(totalTicks))

	// 4. Collect metrics.
	metrics := collectMetrics(w, totalTicks)
	t.Logf("METRICS: %s", metrics)

	// 5. Assertions. Minimal: the world produced a viable
	//    multi-generational society. The upper-bound on
	//    population guards against the v1 MarriageEngine's
	//    "any opposite-gender pair marries" rule producing a
	//    population explosion — if 100 years yields 10,000+
	//    people, the test fails loudly so the explosion is
	//    caught early.
	const maxPopulation = 1000
	if metrics.Population <= 0 {
		t.Errorf("population = %d, want > 0 (population extinction)", metrics.Population)
	}
	if metrics.Population > maxPopulation {
		t.Errorf("population = %d, want <= %d (population explosion)", metrics.Population, maxPopulation)
	}
	if metrics.Births <= 0 {
		t.Errorf("births = %d, want > 0 (no births in 100 years)", metrics.Births)
	}
	if metrics.Marriages <= 0 {
		t.Errorf("marriages = %d, want > 0 (no marriages in 100 years)", metrics.Marriages)
	}
	if metrics.MaxFamilyDepth < 5 {
		t.Errorf("max family depth = %d, want >= 5 (5 generations)", metrics.MaxFamilyDepth)
	}
	if metrics.RelationshipCount <= 0 {
		t.Errorf("relationship count = %d, want > 0", metrics.RelationshipCount)
	}
	if metrics.MemoryCount <= 0 {
		t.Errorf("memory count = %d, want > 0", metrics.MemoryCount)
	}
}

// collectMetrics gathers the final SimulationMetrics from w.
// Pure read; no mutation.
func collectMetrics(w *core.World, finalTick int64) SimulationMetrics {
	return SimulationMetrics{
		Population:       len(w.LivingPeople()),
		Births:           countBirths(w),
		Deaths:           countDeaths(w),
		Marriages:        simulation.MarriageCount(w),
		LivingFamilies:   countLivingFamilies(w),
		MaxFamilyDepth:   maxFamilyDepth(w),
		RelationshipCount: len(w.Relationships),
		MemoryCount:      len(w.Memories),
		EventCount:       len(w.Events),
		FinalTick:        finalTick,
	}
}

// countBirths returns the number of people whose BirthTick is
// at or after the simulation's start (tick 0). The bootstrap
// generates people with negative BirthTick (born before t=0),
// so BirthTick >= 0 means born during the simulation.
func countBirths(w *core.World) int {
	n := 0
	for _, p := range w.People {
		if p.BirthTick >= 0 {
			n++
		}
	}
	return n
}

// countDeaths returns the number of people whose DeathTick is
// set (> 0). Counts everyone who died during the run,
// including those whose bodies have since been forgotten by
// other systems.
func countDeaths(w *core.World) int {
	n := 0
	for _, p := range w.People {
		if p.DeathTick > 0 {
			n++
		}
	}
	return n
}

// countLivingFamilies returns the number of distinct family
// units — parents (FatherID or MotherID) referenced by at
// least one child. V1 approximation: a "family" is any parent
// who has at least one child, regardless of whether the
// parents themselves are alive. This counts "founded" families
// rather than "currently living together" families, which is
// the more useful metric for a multi-generational test.
func countLivingFamilies(w *core.World) int {
	parents := make(map[string]bool)
	for _, p := range w.People {
		if p.FatherID != "" {
			parents[p.FatherID] = true
		}
		if p.MotherID != "" {
			parents[p.MotherID] = true
		}
	}
	return len(parents)
}

// maxFamilyDepth returns the longest ancestry chain from an
// initial-bootstrap person (no parents) to a current living
// descendant. Depth 0 means no one has had children; depth 5
// means 5 generations have lived.
//
// V1 algorithm: BFS from each initial person (FatherID==""
// and MotherID=="") downward, computing the maximum
// generation number. The result is the deepest chain in the
// current population. O(N + E) where E is the number of
// parent-child edges (N-1 for a tree, N*2 worst case).
func maxFamilyDepth(w *core.World) int {
	// Build a child index: parentID -> [childID].
	children := make(map[string][]string)
	for _, p := range w.People {
		if p.FatherID != "" {
			children[p.FatherID] = append(children[p.FatherID], p.ID)
		}
		if p.MotherID != "" {
			children[p.MotherID] = append(children[p.MotherID], p.ID)
		}
	}
	// Find the roots: people with no parents in the world.
	roots := make([]string, 0)
	for _, p := range w.People {
		if p.FatherID == "" && p.MotherID == "" {
			roots = append(roots, p.ID)
		}
	}
	// BFS/DFS from each root, tracking max depth.
	maxDepth := 0
	for _, rootID := range roots {
		d := depthFrom(rootID, children)
		if d > maxDepth {
			maxDepth = d
		}
	}
	return maxDepth
}

// depthFrom returns the maximum chain length starting at rootID
// and going down through children. Chain length counts the
// number of NODES (root = depth 1, child = depth 2, etc.) so a
// 5-generation chain (root, child, grandchild, great-grandchild,
// great-great-grandchild) returns 5, matching the Phase 24
// integration test's "max family depth >= 5" assertion.
func depthFrom(rootID string, children map[string][]string) int {
	// Iterative DFS to avoid stack-blowup on deep trees.
	// (Stack-based, but the stack is heap-allocated; Go's
	// goroutine stack is small.)
	stack := []struct {
		id    string
		depth int
	}{{id: rootID, depth: 1}}
	max := 0
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top.depth > max {
			max = top.depth
		}
		for _, childID := range children[top.id] {
			stack = append(stack, struct {
				id    string
				depth int
			}{id: childID, depth: top.depth + 1})
		}
	}
	return max
}
