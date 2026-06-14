package core

import (
	"testing"
	"time"
)

// TestLivingPeople_StableOrder verifies that LivingPeople returns people
// in a deterministic order (sorted by ID) regardless of insertion order.
// This is the foundation of the per-tick determinism contract — if the
// order were unstable, the PopulationEngine would produce different
// death outcomes for the same seed.
func TestLivingPeople_StableOrder(t *testing.T) {
	w := NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	// Insert in non-sorted order.
	for _, id := range []string{"c", "a", "b", "z", "m"} {
		w.AddPerson(&Person{ID: id, Name: id, Alive: true})
	}
	got := w.LivingPeople()
	want := []string{"a", "b", "c", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("len(LivingPeople) = %d, want %d", len(got), len(want))
	}
	for i, p := range got {
		if p.ID != want[i] {
			t.Errorf("LivingPeople[%d].ID = %q, want %q", i, p.ID, want[i])
		}
	}
}

// TestLivingPeople_ExcludesDead verifies that dead people are not
// returned by LivingPeople.
func TestLivingPeople_ExcludesDead(t *testing.T) {
	w := NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&Person{ID: "a", Name: "a", Alive: true})
	w.AddPerson(&Person{ID: "b", Name: "b", Alive: false})
	w.AddPerson(&Person{ID: "c", Name: "c", Alive: true})
	got := w.LivingPeople()
	if len(got) != 2 {
		t.Fatalf("len(LivingPeople) = %d, want 2 (dead person should be excluded)", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("LivingPeople = [%s, %s], want [a, c]", got[0].ID, got[1].ID)
	}
}

// TestLivingPeopleAt verifies the per-location filter and stable order.
func TestLivingPeopleAt(t *testing.T) {
	w := NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddPerson(&Person{ID: "a", Name: "a", Alive: true, LocationID: "village"})
	w.AddPerson(&Person{ID: "b", Name: "b", Alive: true, LocationID: "town"})
	w.AddPerson(&Person{ID: "c", Name: "c", Alive: true, LocationID: "village"})
	w.AddPerson(&Person{ID: "d", Name: "d", Alive: false, LocationID: "village"})

	got := w.LivingPeopleAt("village")
	if len(got) != 2 {
		t.Fatalf("len(LivingPeopleAt(village)) = %d, want 2", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("LivingPeopleAt(village) = [%s, %s], want [a, c]", got[0].ID, got[1].ID)
	}
}

// TestRecomputeLocationPopulations verifies that the population counts
// on each location match the actual number of living people there.
func TestRecomputeLocationPopulations(t *testing.T) {
	w := NewWorld("t", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(NewLocation("village", "V", "Marches", 100))
	w.AddLocation(NewLocation("town", "T", "Marches", 100))
	for _, id := range []string{"a", "b", "c"} {
		w.AddPerson(&Person{ID: id, Name: id, Alive: true, LocationID: "village"})
	}
	w.AddPerson(&Person{ID: "d", Name: "d", Alive: true, LocationID: "town"})
	w.AddPerson(&Person{ID: "e", Name: "e", Alive: false, LocationID: "village"})
	w.RecomputeLocationPopulations()
	if w.Locations["village"].Population != 3 {
		t.Errorf("village population = %d, want 3 (3 alive - 1 dead)", w.Locations["village"].Population)
	}
	if w.Locations["town"].Population != 1 {
		t.Errorf("town population = %d, want 1", w.Locations["town"].Population)
	}
}
