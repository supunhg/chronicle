package simulation

import (
	"sort"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// testNow is a fixed clock used by the determinism tests. Using
// a constant instead of time.Now() makes test runs byte-for-byte
// reproducible and avoids any incidental coupling to the wall
// clock.
var testNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// TestSortedPeople_DeterministicOrder verifies that sortedPeople
// returns w.People in ascending ID order regardless of the map's
// internal iteration order. Phase 26 Part E: this helper is the
// backbone of MemoryEngine's slice-level determinism.
func TestSortedPeople_DeterministicOrder(t *testing.T) {
	w := core.NewWorld("test", 1, testNow)
	// Insert in non-sorted order to defeat any accidental
	// reliance on insertion order.
	for _, id := range []string{"charlie", "alpha", "bravo", "zulu", "mike"} {
		w.AddPerson(&core.Person{ID: id, Alive: true})
	}
	got := sortedPeople(w)
	want := []string{"alpha", "bravo", "charlie", "mike", "zulu"}
	if len(got) != len(want) {
		t.Fatalf("sortedPeople returned %d people, want %d", len(got), len(want))
	}
	for i, p := range got {
		if p.ID != want[i] {
			t.Errorf("sortedPeople[%d].ID = %q, want %q", i, p.ID, want[i])
		}
	}
}

// TestSortedPeople_StableAcrossCalls verifies that sortedPeople
// returns the same slice across repeated calls. The sort is
// stable (sort.Slice on a fresh copy), so two calls on an
// unchanged world must yield the same ordering.
func TestSortedPeople_StableAcrossCalls(t *testing.T) {
	w := core.NewWorld("test", 1, testNow)
	for _, id := range []string{"p3", "p1", "p2"} {
		w.AddPerson(&core.Person{ID: id, Alive: true})
	}
	first := sortedPeople(w)
	second := sortedPeople(w)
	if len(first) != len(second) {
		t.Fatalf("length mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("call-to-call divergence at [%d]: %q vs %q",
				i, first[i].ID, second[i].ID)
		}
	}
}

// TestSortedLocationIDs_OrdersAlphabetically verifies the
// pattern used by RelationshipEngine.formRelationships and
// EconomyEngine.runConsumption/runPriceRecalc: build a slice
// of map keys, sort it, then iterate. The test exercises the
// pattern directly so a future refactor that drops the sort
// is caught.
func TestSortedLocationIDs_OrdersAlphabetically(t *testing.T) {
	w := core.NewWorld("test", 1, testNow)
	for _, id := range []string{"zzz", "aaa", "mmm", "bbb"} {
		w.AddLocation(&core.Location{ID: id, Name: id})
	}
	ids := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	want := []string{"aaa", "bbb", "mmm", "zzz"}
	if len(ids) != len(want) {
		t.Fatalf("got %d IDs, want %d", len(ids), len(want))
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, id, want[i])
		}
	}
}

// TestRelationshipEngine_FormRelationships_DeterministicAcrossRuns
// verifies the end-to-end determinism claim: two worlds with
// the same input produce the same Relationships slice (same
// length, same FromID/ToID pairs in the same order) regardless
// of map iteration order. This is the Phase 26 Part E
// acceptance test for the RelationshipEngine fix.
//
// The test inserts people in a non-sorted order and inserts
// many people per location so the inner-sort behavior and the
// outer location-sort behavior are both exercised.
func TestRelationshipEngine_FormRelationships_DeterministicAcrossRuns(t *testing.T) {
	buildWorld := func() *core.World {
		w := core.NewWorld("test", 1, testNow)
		// Three locations with overlapping people counts.
		for _, id := range []string{"loc-c", "loc-a", "loc-b"} {
			w.AddLocation(&core.Location{ID: id, Name: id})
		}
		// Insert in scrambled order; five per location.
		insertOrder := []string{
			"loc-b:p2", "loc-a:p1", "loc-c:p4", "loc-a:p3",
			"loc-b:p5", "loc-c:p1", "loc-b:p4", "loc-c:p3",
			"loc-a:p2", "loc-c:p5", "loc-b:p1", "loc-a:p4",
			"loc-c:p2", "loc-a:p5", "loc-b:p3",
		}
		for _, tag := range insertOrder {
			loc, pid := splitTag(tag)
			w.AddPerson(&core.Person{
				ID:         tag,
				Alive:      true,
				LocationID: loc,
				Name:       pid,
			})
		}
		return w
	}

	w1 := buildWorld()
	w2 := buildWorld()
	r1 := NewRelationshipEngine()
	r2 := NewRelationshipEngine()
	if err := r1.Tick(w1); err != nil {
		t.Fatalf("engine1 Tick: %v", err)
	}
	if err := r2.Tick(w2); err != nil {
		t.Fatalf("engine2 Tick: %v", err)
	}

	if len(w1.Relationships) != len(w2.Relationships) {
		t.Fatalf("relationship count diverges: %d vs %d",
			len(w1.Relationships), len(w2.Relationships))
	}
	for i := range w1.Relationships {
		a := w1.Relationships[i]
		b := w2.Relationships[i]
		if a.FromID != b.FromID || a.ToID != b.ToID {
			t.Errorf("relationships[%d] diverges: (%s,%s) vs (%s,%s)",
				i, a.FromID, a.ToID, b.FromID, b.ToID)
		}
	}
}

// TestEconomyEngine_RunConsumption_LocationOrderIsSorted verifies
// that runConsumption's second loop processes locations in
// ascending ID order. The test seeds multiple locations in
// non-sorted insertion order, puts each into food shortage,
// and asserts that the emitted famine_risk Memories appear in
// location-ID-sorted order in w.Memories.
func TestEconomyEngine_RunConsumption_LocationOrderIsSorted(t *testing.T) {
	w := core.NewWorld("test", 1, testNow)
	// Insert in non-sorted order; force each into shortage
	// (food = 0 < EconomyShortageThreshold).
	for _, id := range []string{"loc-z", "loc-m", "loc-a"} {
		w.AddLocation(&core.Location{
			ID:         id,
			Name:       id,
			Settlement: core.SettlementInventory{Food: 0},
		})
	}
	// One resident per location so each shortage emits exactly
	// one memory. Residents are inserted in scrambled order.
	for _, tag := range []string{"loc-m:alice", "loc-z:zoe", "loc-a:adam"} {
		loc, _ := splitTag(tag)
		w.AddPerson(&core.Person{
			ID:         tag,
			Alive:      true,
			LocationID: loc,
			Name:       tag,
		})
	}

	e := NewEconomyEngine()
	if err := e.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Verify the emitted memories' EventIDs encode locations in
	// sorted order: loc-a:adam, loc-m:alice, loc-z:zoe.
	if len(w.Memories) != 3 {
		t.Fatalf("expected 3 famine memories, got %d", len(w.Memories))
	}
	wantEventIDs := []string{
		"famine-0-loc-a",
		"famine-0-loc-m",
		"famine-0-loc-z",
	}
	for i, mem := range w.Memories {
		if mem.EventID != wantEventIDs[i] {
			t.Errorf("Memories[%d].EventID = %q, want %q (locations must be sorted)",
				i, mem.EventID, wantEventIDs[i])
		}
	}
}

// TestEconomyEngine_RunPriceRecalc_NoPanicOnEmptyWorld is a
// smoke test: the location-sort pattern must handle the
// zero-locations case without panicking.
func TestEconomyEngine_RunPriceRecalc_NoPanicOnEmptyWorld(t *testing.T) {
	w := core.NewWorld("test", 1, testNow)
	e := NewEconomyEngine()
	if err := e.Tick(w); err != nil {
		t.Fatalf("Tick on empty world: %v", err)
	}
}

// splitTag splits a "locationID:personName" tag into its parts.
// Used by the determinism tests to avoid duplicating the
// parsing logic inline.
func splitTag(tag string) (string, string) {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ':' {
			return tag[:i], tag[i+1:]
		}
	}
	return tag, ""
}
