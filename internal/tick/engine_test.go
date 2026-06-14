package tick

import (
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

func TestSimulation_AdvancesTick(t *testing.T) {
	start := time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC)
	w := core.NewWorld("test", 42, start)
	sim := NewSimulation(42)

	if w.Tick != 0 {
		t.Fatalf("initial tick = %d, want 0", w.Tick)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if w.Tick != 1 {
		t.Fatalf("after 1 tick, tick = %d, want 1", w.Tick)
	}
	if !w.Now.Equal(start.AddDate(0, 0, 1)) {
		t.Errorf("after 1 tick, clock = %v, want %v", w.Now, start.AddDate(0, 0, 1))
	}
}

func TestSimulation_Deterministic(t *testing.T) {
	// The determinism contract: two runs with the same seed and the
	// same action sequence must produce identical state.
	const N = 1000

	run := func() *core.World {
		w := core.NewWorld("test", 1234, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
		w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: -20 * 365, Alive: true, Gender: "F"})
		w.AddPerson(&core.Person{ID: "p2", Name: "Bob", BirthTick: -30 * 365, Alive: true, Gender: "M"})
		sim := NewSimulation(1234)
		for i := int64(0); i < N; i++ {
			if err := sim.Tick(w); err != nil {
				t.Fatalf("Tick() error = %v", err)
			}
		}
		return w
	}

	w1 := run()
	w2 := run()

	if w1.Tick != w2.Tick {
		t.Fatalf("tick diverged: %d vs %d", w1.Tick, w2.Tick)
	}
	if !w1.Now.Equal(w2.Now) {
		t.Fatalf("clock diverged: %v vs %v", w1.Now, w2.Now)
	}
}

func TestEntityRand_SameArgsProduceSameValue(t *testing.T) {
	r1 := EntityRand(42, 100, "alice")
	r2 := EntityRand(42, 100, "alice")
	if v1, v2 := r1.Int63(), r2.Int63(); v1 != v2 {
		t.Errorf("EntityRand(42,100,alice) diverged: %d vs %d", v1, v2)
	}
}

func TestTickRand_SameArgsProduceSameValue(t *testing.T) {
	r1 := TickRand(42, 100)
	r2 := TickRand(42, 100)
	if v1, v2 := r1.Int63(), r2.Int63(); v1 != v2 {
		t.Errorf("TickRand(42,100) diverged: %d vs %d", v1, v2)
	}
}

// initCounter is a test helper engine that counts how many times Init
// and Tick are called. It implements the tick.Engine interface.
type initCounter struct {
	initCount int
	tickCount int
}

func (e *initCounter) Init(w *core.World) error { e.initCount++; return nil }
func (e *initCounter) Tick(w *core.World) error { e.tickCount++; return nil }

func TestSimulation_Init(t *testing.T) {
	w := core.NewWorld("test", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	eng := &initCounter{}
	sim := NewSimulation(1, eng)

	// Before Init, counts are zero.
	if eng.initCount != 0 {
		t.Errorf("initCount before Init = %d, want 0", eng.initCount)
	}
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if eng.initCount != 1 {
		t.Errorf("initCount after Init = %d, want 1", eng.initCount)
	}

	// Tick should not call Init again.
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if eng.initCount != 1 {
		t.Errorf("initCount after Tick = %d, want 1 (Init must not be called from Tick)", eng.initCount)
	}
	if eng.tickCount != 1 {
		t.Errorf("tickCount after Tick = %d, want 1", eng.tickCount)
	}
}
