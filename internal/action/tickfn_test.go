package action

import (
	"context"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// makeSim builds a fresh tick.Simulation with the canonical
// engine stack (Phase 31: same as enterREPL/cmd/chronicle) so
// action tests can drive a real per-tick callback. The engines
// are stateless, so the same seed produces identical state
// mutations.
func makeSim(seed int64) *tick.Simulation {
	popEng := simulation.NewPopulationEngine()
	relEng := simulation.NewRelationshipEngine()
	memEng := &simulation.MemoryEngine{RelationshipEngine: relEng}
	sim := tick.NewSimulation(seed,
		popEng,
		relEng,
		simulation.NewGoalEngine(),
		memEng,
	)
	return sim
}

// TestSetTickFn_RoundTrip verifies the SetTickFn setter
// stores and clears the callback.
func TestSetTickFn_RoundTrip(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	if eng.tickFn != nil {
		t.Fatalf("setup: tickFn should be nil, got %T", eng.tickFn)
	}
	calls := 0
	fn := func() error { calls++; return nil }
	eng.SetTickFn(fn)
	eng.advanceTick(1)
	if calls != 1 {
		t.Errorf("tickFn called %d times after advanceTick(1), want 1", calls)
	}
	// Clear the callback and verify clock-only fallback
	// runs no engine calls.
	calls = 0
	eng.SetTickFn(nil)
	eng.advanceTick(1)
	if calls != 0 {
		t.Errorf("tickFn called %d times after SetTickFn(nil) + advanceTick(1), want 0", calls)
	}
}

// TestAdvanceTick_WithTickFn_RunsFullPipeline verifies that
// when a TickFn is set, advanceTick(n) invokes it n times
// (i.e., the world evolves by n full ticks, not just the
// clock). The worldhash after a single advanceTick(1) with
// a real sim.TickFn must differ from the clock-only path
// (which doesn't run engines).
func TestAdvanceTick_WithTickFn_RunsFullPipeline(t *testing.T) {
	w1 := newTestWorld()
	w2 := newTestWorld()
	sim1 := makeSim(w1.Seed)
	// Wire a real per-tick callback into w1's engine.
	eng1 := New(w1, nil)
	eng1.SetTickFn(func() error { return sim1.Tick(w1) })
	// w2 stays clock-only (no TickFn).
	eng2 := New(w2, nil)
	// Same number of advanceTick calls.
	eng1.advanceTick(1)
	eng2.advanceTick(1)
	// Clock advanced identically in both worlds.
	if w1.Tick != w2.Tick {
		t.Errorf("Tick diverged: w1.Tick=%d, w2.Tick=%d", w1.Tick, w2.Tick)
	}
	// The worldhash must differ — w1's engines ran (which
	// can mutate NPCs' needs/relationships), w2's didn't.
	// WorldHash is the canonical determinism contract.
	h1 := core.WorldHash(w1)
	h2 := core.WorldHash(w2)
	if h1 == h2 {
		t.Errorf("WorldHash matches across TickFn/clock-only paths; TickFn did not run engines")
	}
}

// TestAdvanceTick_ZeroOrNegative verifies that advanceTick
// with a non-positive n is a no-op (no TickFn calls, no
// clock change).
func TestAdvanceTick_ZeroOrNegative(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	calls := 0
	eng.SetTickFn(func() error { calls++; return nil })
	startTick := w.Tick
	eng.advanceTick(0)
	eng.advanceTick(-5)
	if calls != 0 {
		t.Errorf("tickFn called %d times for n<=0, want 0", calls)
	}
	if w.Tick != startTick {
		t.Errorf("w.Tick changed: was %d, now %d", startTick, w.Tick)
	}
}

// TestAdvanceTick_ClockOnlyFallback verifies that when no
// TickFn is set, advanceTick(n) advances w.Tick and w.Now
// by n days without running any engines (preserves the
// pre-Phase 31 behavior used by the existing test suite).
func TestAdvanceTick_ClockOnlyFallback(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	startTick := w.Tick
	startNow := w.Now
	eng.advanceTick(3)
	if w.Tick != startTick+3 {
		t.Errorf("w.Tick = %d, want %d", w.Tick, startTick+3)
	}
	want := startNow.AddDate(0, 0, 3)
	if !w.Now.Equal(want) {
		t.Errorf("w.Now = %v, want %v", w.Now, want)
	}
}

// TestActionEngine_Determinism_TravelSeed verifies that the
// same travel action sequence with the same world seed
// produces a byte-identical world state. This is the v1
// determinism contract for player actions (Phase 31 §5.3).
//
// Setup: two worlds with the same seed, same TickFn (real
// sim.Tick). Run the same travel sequence on both. Hash
// both worlds. Hashes must match.
func TestActionEngine_Determinism_TravelSeed(t *testing.T) {
	const seed = int64(42)
	runTravelSequence := func(t *testing.T) *core.World {
		t.Helper()
		w := newTestWorld()
		w.Seed = seed
		// The default world has alice at blackwater. Build
		// a sim with the canonical engine stack.
		sim := makeSim(seed)
		eng := New(w, nil)
		eng.SetTickFn(func() error { return sim.Tick(w) })
		if err := sim.Init(w); err != nil {
			t.Fatalf("sim.Init: %v", err)
		}
		// Travel from blackwater -> ashford. 1 tick.
		if _, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTravel, Target: "ashford"}); err != nil {
			t.Fatalf("travel 1: %v", err)
		}
		// Then sleep 48h. 2 ticks.
		if _, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: 48}}); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		// Then travel back. 1 tick.
		if _, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionTravel, Target: "blackwater"}); err != nil {
			t.Fatalf("travel 2: %v", err)
		}
		return w
	}
	w1 := runTravelSequence(t)
	w2 := runTravelSequence(t)
	h1 := core.WorldHash(w1)
	h2 := core.WorldHash(w2)
	if h1 != h2 {
		t.Errorf("WorldHash diverged across two identical travel sequences:\n  h1=%s\n  h2=%s", h1, h2)
	}
	// Sanity: clock advanced by 1+2+1 = 4 ticks.
	if w1.Tick != newTestWorld().Tick+4 {
		t.Errorf("w1.Tick = %d, want %d", w1.Tick, newTestWorld().Tick+4)
	}
}

// TestActionEngine_Determinism_SleepSeed verifies that
// sleeping with the same hours + same seed produces a
// byte-identical world state.
func TestActionEngine_Determinism_SleepSeed(t *testing.T) {
	const seed = int64(99)
	runSleep := func(t *testing.T) *core.World {
		t.Helper()
		w := newTestWorld()
		w.Seed = seed
		sim := makeSim(seed)
		eng := New(w, nil)
		eng.SetTickFn(func() error { return sim.Tick(w) })
		if err := sim.Init(w); err != nil {
			t.Fatalf("sim.Init: %v", err)
		}
		// Sleep 24h once.
		if _, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: 24}}); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		return w
	}
	w1 := runSleep(t)
	w2 := runSleep(t)
	if core.WorldHash(w1) != core.WorldHash(w2) {
		t.Errorf("WorldHash diverged across two identical sleep actions:\n  h1=%s\n  h2=%s", core.WorldHash(w1), core.WorldHash(w2))
	}
}

// TestActionEngine_SleepAdvancesWorldState verifies that
// sleeping with a TickFn actually mutates world state (not
// just the clock). A sleep that ran no engines would leave
// w.People["baker"].AgeAt(w.Tick) unchanged from
// w.People["baker"].AgeAt(startTick) — except that the
// default PopulationEngine doesn't age by 1 each tick. Use
// a different signal: count the total events in the world.
// After 1 full tick, an event may have been emitted by the
// engine pipeline. Or check that the world's Needs map has
// been touched. The cleanest signal: compare WorldHash
// before and after a single full-pipeline tick.
//
// If the hash doesn't change, the engines didn't run. (It
// COULD be that no engine happened to mutate state in this
// seed, but with 3 NPCs and 1 tick the goal engine almost
// always touches something. The probability of zero mutation
// is low enough to ignore in a smoke test.)
func TestActionEngine_SleepAdvancesWorldState(t *testing.T) {
	w := newTestWorld()
	sim := makeSim(w.Seed)
	eng := New(w, nil)
	eng.SetTickFn(func() error { return sim.Tick(w) })
	if err := sim.Init(w); err != nil {
		t.Fatalf("sim.Init: %v", err)
	}
	before := core.WorldHash(w)
	if _, err := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: 24}}); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	after := core.WorldHash(w)
	if before == after {
		t.Errorf("WorldHash unchanged after 1-tick sleep with TickFn: engines did not run")
	}
	// Clock advanced by 1.
	if w.Tick != newTestWorld().Tick+1 {
		t.Errorf("w.Tick = %d, want %d", w.Tick, newTestWorld().Tick+1)
	}
}

// TestActionEngine_ResultText_ShowsElapsedDays verifies the
// new sleep result text format includes the days elapsed
// (Phase 31 v1). This locks in the player-facing text so
// future regressions in the formatter are caught.
func TestActionEngine_ResultText_ShowsElapsedDays(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: 48}})
	if !res.OK {
		t.Fatalf("sleep: OK = false; Text = %q", res.Text)
	}
	// 48h → 2 days. Format: "You sleep for 48 hours (2 days elapsed, now tick N)."
	wantSubstr := "2 days elapsed"
	if !strings.Contains(res.Text, wantSubstr) {
		t.Errorf("sleep text missing %q: %q", wantSubstr, res.Text)
	}
}

// TestActionEngine_ResultText_SingularDay verifies the
// sleep result text uses "1 day" (singular) when the
// duration is exactly 24 hours.
func TestActionEngine_ResultText_SingularDay(t *testing.T) {
	w := newTestWorld()
	eng := New(w, nil)
	res, _ := eng.Resolve(context.Background(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: 24}})
	if !res.OK {
		t.Fatalf("sleep: OK = false; Text = %q", res.Text)
	}
	// 24h → 1 tick → "1 day elapsed" (singular).
	if !strings.Contains(res.Text, "1 day elapsed") {
		t.Errorf("sleep text missing '1 day elapsed': %q", res.Text)
	}
	if strings.Contains(res.Text, "1 days elapsed") {
		t.Errorf("sleep text has plural '1 days' (should be singular): %q", res.Text)
	}
}
