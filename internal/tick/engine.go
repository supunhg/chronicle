package tick

import (
	"github.com/chronicle-dev/chronicle/internal/core"
)

// Engine is the interface every simulation engine implements.
//
// An engine is a pure function over the world state. It reads from the
// world, mutates it, and returns. It MUST NOT call the LLM, MUST NOT do
// I/O outside of the world, and MUST be deterministic given the same
// (worldSeed, tick, input state).
//
// Engines are run by a Simulation in a fixed order. The order is the
// determinism contract — see SIMULATION_TICK_SPEC.md §2.
type Engine interface {
	// Init is called once when a Simulation is constructed or when a
	// world is loaded from persistence. Engines use Init to rebuild
	// caches or apply state derived from a freshly-loaded world.
	// It is called BEFORE the first Tick. The default behavior for
	// stateless engines is a no-op.
	Init(w *core.World) error

	// Tick advances the world by one tick.
	Tick(w *core.World) error
}

// Simulation runs a fixed list of engines in order, once per tick.
//
// The engine order is the determinism contract (see
// SIMULATION_TICK_SPEC.md §2). Reordering engines changes the result.
type Simulation struct {
	// engines is the ordered list of engines to run.
	engines []Engine

	// worldSeed is captured at construction time so all RNG is
	// deterministic per-world.
	worldSeed int64
}

// NewSimulation returns a Simulation that runs the given engines in
// the given order.
//
// NewSimulation does NOT call Init on the engines. The caller is
// responsible for calling sim.Init(w) before the first Tick, typically
// at world creation or load.
func NewSimulation(worldSeed int64, engines ...Engine) *Simulation {
	return &Simulation{
		engines:   engines,
		worldSeed: worldSeed,
	}
}

// Init calls Init on every engine in order. It MUST be called before
// the first Tick. Returns the first error from any engine.
func (s *Simulation) Init(w *core.World) error {
	for _, e := range s.engines {
		if err := e.Init(w); err != nil {
			return err
		}
	}
	return nil
}

// Tick advances the world by one tick, running all engines in order.
// The world's Tick counter is incremented and the simulated clock
// advances by one day before any engine runs.
//
// Known limitation: Tick is not transactional. If an engine returns an
// error, earlier engines in the same tick have already mutated state.
// Phase 0 engines cannot fail, so this is latent. Phase 2 engines with
// multi-step writes will need either per-entity commit-at-end-of-tick
// or engine-internal rollback. See SIMULATION_TICK_SPEC.md §2.2.
func (s *Simulation) Tick(w *core.World) error {
	w.Tick++
	w.Now = w.Now.AddDate(0, 0, 1)
	for _, e := range s.engines {
		if err := e.Tick(w); err != nil {
			return err
		}
	}
	return nil
}

// WorldSeed returns the seed used by this simulation.
func (s *Simulation) WorldSeed() int64 {
	return s.worldSeed
}
