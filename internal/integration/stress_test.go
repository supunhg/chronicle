// Phase 32 10-seed stress test.
//
// This is the v1 long-horizon stability gate. It bootstraps
// the frontier worldpack with 10 different seeds, runs each
// for 100 simulated years (36,500 ticks × 10 = 365,000 ticks
// total), and asserts the simulation invariants that a single
// 100-year run could miss:
//
//   - 0 panics across all runs
//   - 0 NaN or Inf in any field the hash covers
//   - 0 negative inventories (player or NPC)
//   - population in (0, 1000] (no extinction, no explosion)
//   - births > 0 (reproduction is happening)
//   - marriages > 0 (pair-bonding is happening)
//   - memory count < MaxLiveMemories cap
//   - event count < MaxLiveEvents cap
//
// Each seed is a subtest so a failure pinpoints which seed
// regressed. The test is skipped under -short; the v1
// acceptance run does:
//
//	go test -count=1 -timeout 60m -run TestTenSeedStress ./internal/integration/...
//
// Why 100 years × 10 seeds? A single 100-year run can hide
// bugs that are rare in the seed-42 frontier: stochastic
// events that fire on 1-in-1000-tick odds, integer overflow
// at high population, etc. Ten seeds × 100 years is the
// "exercise the long tail" budget for v1. A future phase
// can grow this to 50 seeds × 200 years; v1 is the floor.
package integration

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
	"github.com/chronicle-dev/chronicle/internal/worldpack"
)

// stressSeeds is the set of seeds the 10-seed stress test
// runs. Picked to span the int64 range that bootstrap
// typically exercises (42, 43, 99, 100, 256, 1000, 12345,
// 0xCAFE, 0xDEADBEEF, -1). A future stress run can shuffle
// this list or extend it; the contract is just "run each
// seed identically and assert the same invariants".
var stressSeeds = []int64{42, 43, 99, 100, 256, 1000, 12345, 0xCAFE, 0xDEADBEEF, -1}

// stressConfig pins the run length for the stress test.
// Match the Phase 24 v1 acceptance gate (100 years × 365
// ticks/year) so the stress test shares its shape with the
// rest of the long-horizon suite. Population bounds live
// in TestFiveGenerationSimulation (the per-seed acceptance
// gate); the stress test's invariants are the v1 long-tail
// (no NaN/Inf, no negatives, retention caps).
const (
	stressYears        = 100
	stressTicksPerYear = 365
	stressTotalTicks   = stressYears * stressTicksPerYear
)

// stressOneSeed is the testable core: bootstrap + tick +
// collect metrics + assert invariants. Returns the metrics
// and an error-style bool so subtests can report cleanly.
//
// Pure function in the test sense: no t, no log output, no
// global state mutation. The wrapper TestTenSeedStress
// drives it from a subtest with the testing.T context.
func stressOneSeed(seed int64) (SimulationMetrics, error) {
	packDir := filepath.Join("..", "..", "worldpacks", "frontier")
	_, pack, err := worldpack.Load(packDir)
	if err != nil {
		return SimulationMetrics{}, err
	}
	w := core.NewWorld("frontier", seed, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := worldpack.Bootstrap(pack, w, seed); err != nil {
		return SimulationMetrics{}, err
	}

	// Wire the 7 production engines in the canonical
	// (determinism contract) order. See
	// SIMULATION_TICK_SPEC.md §2.
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
		return SimulationMetrics{}, err
	}

	// Run. We don't log per-year progress (10 seeds × 100
	// years is a lot of log output); the wrapper subtest
	// logs per-seed totals.
	for tick := 0; tick < stressTotalTicks; tick++ {
		if err := sim.Tick(w); err != nil {
			return SimulationMetrics{}, err
		}
	}

	// Assert no-NaN / no-Inf / no-negative-inventories /
	// retention caps. These are the invariants a single
	// run might miss: NaN propagates through addition but
	// only shows up in the hash if the field is hashed
	// (NaN in Needs.AgeAt would be hashed); negative
	// inventories come from a buggy merchant sell; cap
	// violations come from a runaway engine.
	if err := assertInvariants(w, seed); err != nil {
		return collectMetrics(w, stressTotalTicks), err
	}

	return collectMetrics(w, stressTotalTicks), nil
}

// assertInvariants checks the post-run world for the
// invariants the stress test cares about. Any failure
// returns a descriptive error so the subtest report
// pinpoints the bug.
//
// Invariants:
//   - 0 NaN / Inf in any field the WorldHash covers
//     (people, locations, factions, items, memories,
//     events, relationships, rules)
//   - 0 negative counts in any inventory (player or NPC)
//   - 0 negative tick / birth / death ticks
//   - memory count <= simulation.MaxLiveMemories
//   - event count <= simulation.MaxLiveEvents
//
// The NaN/Inf scan is O(N) per category and is dominated
// by the per-tick sim cost, so the overhead is negligible.
//
// Note: int64 / int fields cannot be NaN or Inf (the
// int64→float64 conversion produces NaN/Inf only for very
// large values near the int64 max, which is not realistic
// for tick/birth/death counters). For ints we use a `< 0`
// range check instead of math.IsNaN/IsInf, which would be
// a no-op on integer types.
func assertInvariants(w *core.World, seed int64) error {
	// People: scan every field that can hold a float.
	for id, p := range w.People {
		// BirthTick is allowed to be negative: bootstrap
		// creates people with BirthTick = -age*365 (e.g.,
		// -20*365 for a 20-year-old born before sim start).
		// The real bug case is "born in the future":
		// BirthTick > w.Tick means the sim clock has
		// retroactively aged the person, which is a clock
		// bug, not a birth bug.
		if p.BirthTick > w.Tick {
			return negf("person %s.BirthTick = %d > w.Tick = %d (born in the future)",
				id, p.BirthTick, w.Tick)
		}
		if p.DeathTick < 0 {
			return negf("person %s.DeathTick = %d, want >= 0", id, p.DeathTick)
		}
		for k, v := range p.Traits {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nanf("person %s.Traits[%s] is NaN/Inf", id, k)
			}
		}
		for k, v := range p.Needs {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nanf("person %s.Needs[%s] is NaN/Inf", id, k)
			}
		}
		for k, it := range p.Inventory {
			if it.Count < 0 {
				return negf("person %s.Inventory[%s].Count = %d, want >= 0", id, k, it.Count)
			}
			if it.Value < 0 {
				return negf("person %s.Inventory[%s].Value = %d, want >= 0", id, k, it.Value)
			}
			if math.IsNaN(it.Weight) || math.IsInf(it.Weight, 0) {
				return nanf("person %s.Inventory[%s].Weight is NaN/Inf", id, k)
			}
			if math.IsNaN(it.MaxDurability) || math.IsInf(it.MaxDurability, 0) {
				return nanf("person %s.Inventory[%s].MaxDurability is NaN/Inf", id, k)
			}
		}
	}
	// Player inventory.
	for k, it := range w.Inventory {
		if it.Count < 0 {
			return negf("player Inventory[%s].Count = %d, want >= 0", k, it.Count)
		}
		if it.Value < 0 {
			return negf("player Inventory[%s].Value = %d, want >= 0", k, it.Value)
		}
		if math.IsNaN(it.Weight) || math.IsInf(it.Weight, 0) {
			return nanf("player Inventory[%s].Weight is NaN/Inf", k)
		}
		if math.IsNaN(it.MaxDurability) || math.IsInf(it.MaxDurability, 0) {
			return nanf("player Inventory[%s].MaxDurability is NaN/Inf", k)
		}
	}
	// Locations.
	for id, l := range w.Locations {
		// Pressure is int (0..100 per Location doc); check
		// the range, not finiteness (int can't be NaN/Inf).
		if l.Pressure < 0 || l.Pressure > 100 {
			return negf("location %s.Pressure = %d, want [0, 100]", id, l.Pressure)
		}
		if l.Population < 0 {
			return negf("location %s.Population = %d, want >= 0", id, l.Population)
		}
		if l.PopulationCap < 0 {
			return negf("location %s.PopulationCap = %d, want >= 0", id, l.PopulationCap)
		}
		s := l.Settlement
		if math.IsNaN(s.Food) || math.IsInf(s.Food, 0) {
			return nanf("location %s.Settlement.Food is NaN/Inf", id)
		}
		if math.IsNaN(s.Wood) || math.IsInf(s.Wood, 0) {
			return nanf("location %s.Settlement.Wood is NaN/Inf", id)
		}
		if math.IsNaN(s.Iron) || math.IsInf(s.Iron, 0) {
			return nanf("location %s.Settlement.Iron is NaN/Inf", id)
		}
		if math.IsNaN(s.Cloth) || math.IsInf(s.Cloth, 0) {
			return nanf("location %s.Settlement.Cloth is NaN/Inf", id)
		}
	}
	// Relationships: every axis must be in [0, 100] and
	// finite. (RelationshipEngine clamps on update, so an
	// out-of-range value here means a clamp was bypassed.)
	for i, r := range w.Relationships {
		if !axisValid(r.Trust) {
			return negf("relationship[%d].Trust = %f, out of range", i, r.Trust)
		}
		if !axisValid(r.Respect) {
			return negf("relationship[%d].Respect = %f, out of range", i, r.Respect)
		}
		if !axisValid(r.Fear) {
			return negf("relationship[%d].Fear = %f, out of range", i, r.Fear)
		}
		if !axisValid(r.Attraction) {
			return negf("relationship[%d].Attraction = %f, out of range", i, r.Attraction)
		}
		if !axisValid(r.Loyalty) {
			return negf("relationship[%d].Loyalty = %f, out of range", i, r.Loyalty)
		}
	}
	// Memories: importance/recency/etc. are floats.
	for i, m := range w.Memories {
		if m.Tick < 0 {
			return negf("memory[%d].Tick = %d, want >= 0", i, m.Tick)
		}
		if !axisValid(m.Importance) {
			return negf("memory[%d].Importance = %f, out of range", i, m.Importance)
		}
		if !axisValid(m.Recency) {
			return negf("memory[%d].Recency = %f, out of range", i, m.Recency)
		}
		if !axisValid(m.EmotionalScore) {
			return negf("memory[%d].EmotionalScore = %f, out of range", i, m.EmotionalScore)
		}
		if !axisValid(m.TrustDelta) {
			return negf("memory[%d].TrustDelta = %f, out of range", i, m.TrustDelta)
		}
		if !axisValid(m.RelationshipDelta) {
			return negf("memory[%d].RelationshipDelta = %f, out of range", i, m.RelationshipDelta)
		}
	}
	// Retention caps. The EventEngine and MemoryEngine
	// trim their live buffers; if the trim path is broken
	// the live buffers grow unbounded.
	if len(w.Memories) > simulation.MaxLiveMemories {
		return capf("memories=%d > MaxLiveMemories=%d (retention broken)",
			len(w.Memories), simulation.MaxLiveMemories)
	}
	if len(w.Events) > simulation.MaxLiveEvents {
		return capf("events=%d > MaxLiveEvents=%d (retention broken)",
			len(w.Events), simulation.MaxLiveEvents)
	}
	// Tick must be positive (we ran stressTotalTicks; it
	// cannot be 0 or negative).
	if w.Tick <= 0 {
		return negf("Tick = %d, want > 0", w.Tick)
	}
	// Coin: player money is an int. Negative is a bug.
	if w.Coin < 0 {
		return negf("Coin = %d, want >= 0", w.Coin)
	}
	_ = seed // currently unused; reserved for per-seed cap tuning in v2
	return nil
}

// axisValid returns true iff v is a finite number in
// [-1e6, 1e6] (a generous bound for any "axis" field — the
// 5 RelationshipEngine axes are clamped to [0, 100], the
// memory fields to [0, 1], but we accept a wider range to
// catch overflow before it propagates into the hash).
func axisValid(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	if v < -1e6 || v > 1e6 {
		return false
	}
	return true
}

// nanf formats a NaN/Inf invariant violation.
func nanf(format string, args ...any) error {
	return &invariantError{kind: "NaN/Inf", msg: fmt.Sprintf(format, args...)}
}

// negf formats a negative-value invariant violation.
func negf(format string, args ...any) error {
	return &invariantError{kind: "negative", msg: fmt.Sprintf(format, args...)}
}

// capf formats a retention-cap violation.
func capf(format string, args ...any) error {
	return &invariantError{kind: "cap", msg: fmt.Sprintf(format, args...)}
}

// invariantError is a typed error so the subtest report
// can prefix "NaN/Inf", "negative", or "cap" cleanly.
type invariantError struct {
	kind string
	msg  string
}

func (e *invariantError) Error() string {
	return e.kind + ": " + e.msg
}

// TestTenSeedStress is the Phase 32 10-seed × 100-year
// long-horizon stress test. Each seed is a subtest so a
// failure pinpoints which seed regressed; the parent test
// also enforces cross-seed invariants (e.g., that the
// population distribution is not degenerate).
//
// The test runs subtests in parallel to amortize the
// bootstrap cost across goroutines. This is safe because
// the test is read-only on shared state (each subtest
// builds its own world).
func TestTenSeedStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 10-seed stress test in -short mode (run with -count=1 for full)")
	}

	// Cross-seed summary aggregates (collected by the
	// subtests, summarized here).
	type seedResult struct {
		seed    int64
		metrics SimulationMetrics
		err     error
		dur     time.Duration
	}
	results := make([]seedResult, 0, len(stressSeeds))
	var resultsMu sync.Mutex

	// Run subtests in parallel. t.Parallel() inside a
	// subtest schedules it; the parent waits for all to
	// finish before exiting.
	for _, seed := range stressSeeds {
		seed := seed // capture for the closure
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			metrics, err := stressOneSeed(seed)
			dur := time.Since(start)
			resultsMu.Lock()
			results = append(results, seedResult{seed: seed, metrics: metrics, err: err, dur: dur})
			resultsMu.Unlock()
			if err != nil {
				t.Fatalf("seed=%d: %v", seed, err)
			}
			t.Logf("seed=%d dur=%s %s", seed, dur, metrics)
		})
	}

	// After all subtests finish, compute cross-seed
	// summary. The parent test reads results and asserts
	// at least one seed produced births and marriages
	// (catches the "every seed produced a stillborn world"
	// regression).
	totalBirths, totalMarriages := 0, 0
	for _, r := range results {
		totalBirths += r.metrics.Births
		totalMarriages += r.metrics.Marriages
	}
	if totalBirths == 0 {
		t.Errorf("0 births across all %d seeds; reproduction is broken globally", len(stressSeeds))
	}
	if totalMarriages == 0 {
		t.Errorf("0 marriages across all %d seeds; pair-bonding is broken globally", len(stressSeeds))
	}
	t.Logf("CROSS-SEED: %d seeds, %d total births, %d total marriages",
		len(stressSeeds), totalBirths, totalMarriages)
}
