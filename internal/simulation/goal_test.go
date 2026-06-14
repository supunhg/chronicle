package simulation

import (
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

func TestGoalEngine_InitSetsDefaultNeeds(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	eng := NewGoalEngine()
	if err := eng.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	person := w.People["p1"]
	if person.Needs == nil {
		t.Fatalf("Needs is nil after Init")
	}
	for k, want := range DefaultNeeds {
		if got := person.Needs[k]; got != want {
			t.Errorf("Needs[%q] = %d, want %d", k, got, want)
		}
	}
}

func TestGoalEngine_TickDecaysNeeds(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	eng := NewGoalEngine()
	sim := tick.NewSimulation(1, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Run 10 ticks
	for i := int64(0); i < 10; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	person := w.People["p1"]
	for k, initial := range DefaultNeeds {
		want := initial - 10
		if want < 0 {
			want = 0
		}
		if got := person.Needs[k]; got != want {
			t.Errorf("after 10 ticks, Needs[%q] = %d, want %d", k, got, want)
		}
	}
}

func TestGoalEngine_NeedsClampAtZero(t *testing.T) {
	w := core.NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&core.Person{ID: "p1", Name: "Alice", BirthTick: 0, Alive: true, Gender: "F"})
	eng := NewGoalEngine()
	sim := tick.NewSimulation(1, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Run more ticks than the initial need value to test clamping.
	for i := int64(0); i < 100; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	for k, got := range w.People["p1"].Needs {
		if got < 0 {
			t.Errorf("Needs[%q] went negative: %d", k, got)
		}
	}
}
