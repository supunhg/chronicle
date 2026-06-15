package simulation

import (
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// makeEvents builds n events with deterministic IDs ("e0", "e1",
// ...) and ascending Tick values. Used by the retention tests to
// build a w.Events slice without going through the EventEngine.
// Insertion order matches Tick order, which is the production
// invariant the FIFO trim relies on.
func makeEvents(n int) []core.Event {
	out := make([]core.Event, n)
	for i := 0; i < n; i++ {
		out[i] = core.Event{
			ID:   "e" + itoaSmall(i),
			Type: core.EventFamineRisk,
			Tick: int64(i),
		}
	}
	return out
}

// itoaSmall is a tiny int-to-string helper for the test fixtures.
// Avoids importing strconv for one call site.
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// makeMemories builds n memories with ascending Tick values.
func makeMemories(n int) []core.Memory {
	out := make([]core.Memory, n)
	for i := 0; i < n; i++ {
		out[i] = core.Memory{
			ID:        "m" + itoaSmall(i),
			OwnerID:   "alice",
			Tick:      int64(i),
			Importance: 0.5,
		}
	}
	return out
}

// newTestWorld returns a fresh empty world for the retention tests.
func newTestWorld() *core.World {
	return core.NewWorld("retention-test", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
}

// TestTrimEvents_UnderCap verifies that a log under the cap is
// untouched. The live set keeps all entries, the archive stays
// empty, and the function reports 0 archived.
func TestTrimEvents_UnderCap(t *testing.T) {
	w := newTestWorld()
	w.Events = makeEvents(50) // well under MaxLiveEvents
	n := TrimEvents(w)
	if n != 0 {
		t.Errorf("TrimEvents returned %d, want 0 (under cap)", n)
	}
	if len(w.Events) != 50 {
		t.Errorf("len(Events) = %d, want 50 (unchanged)", len(w.Events))
	}
	if len(w.ArchivedEvents) != 0 {
		t.Errorf("len(ArchivedEvents) = %d, want 0 (no archiving when under cap)", len(w.ArchivedEvents))
	}
}

// TestTrimEvents_AtCap verifies that a log exactly at the cap
// is untouched (the cap is inclusive).
func TestTrimEvents_AtCap(t *testing.T) {
	w := newTestWorld()
	w.Events = makeEvents(MaxLiveEvents) // exactly at the cap
	n := TrimEvents(w)
	if n != 0 {
		t.Errorf("TrimEvents returned %d, want 0 (at cap is inclusive)", n)
	}
	if len(w.Events) != MaxLiveEvents {
		t.Errorf("len(Events) = %d, want %d (unchanged at cap)", len(w.Events), MaxLiveEvents)
	}
}

// TestTrimEvents_OverCap verifies that the oldest excess is moved
// to the archive and the live set is reduced to MaxLiveEvents.
// FIFO semantics: makeEvents inserts in Tick-ascending order, so
// the first overflow entries (lowest Ticks) are archived and the
// last MaxLiveEvents (highest Ticks) are kept live.
func TestTrimEvents_OverCap(t *testing.T) {
	w := newTestWorld()
	const overflow = 50
	w.Events = makeEvents(MaxLiveEvents + overflow)
	n := TrimEvents(w)
	if n != overflow {
		t.Errorf("TrimEvents returned %d, want %d (excess)", n, overflow)
	}
	if len(w.Events) != MaxLiveEvents {
		t.Errorf("len(Events) = %d, want %d (capped)", len(w.Events), MaxLiveEvents)
	}
	if len(w.ArchivedEvents) != overflow {
		t.Errorf("len(ArchivedEvents) = %d, want %d", len(w.ArchivedEvents), overflow)
	}
	// The oldest entries (Tick 0..overflow-1) should be in the
	// archive; the newest (Tick overflow..MaxLiveEvents+overflow-1)
	// should be in the live set.
	for i := 0; i < overflow; i++ {
		if w.ArchivedEvents[i].Tick != int64(i) {
			t.Errorf("ArchivedEvents[%d].Tick = %d, want %d", i, w.ArchivedEvents[i].Tick, i)
		}
	}
	// Live set is the suffix (oldest-first), so the first live
	// entry has Tick == overflow (the cutoff).
	if w.Events[0].Tick != int64(overflow) {
		t.Errorf("Events[0].Tick = %d, want %d (oldest live is at the cutoff)", w.Events[0].Tick, overflow)
	}
}

// TestTrimEvents_AppendsNewToArchive verifies that the archive
// accumulates across trims (does not get replaced) and that the
// second trim only archives the NEW excess, not the original
// archive contents. This exercises the production scenario where
// each tick appends a few new events and the trim runs every
// tick — the archive must grow monotonically.
func TestTrimEvents_AppendsNewToArchive(t *testing.T) {
	w := newTestWorld()
	// First trim: archive 10 events.
	w.Events = makeEvents(MaxLiveEvents + 10)
	if n := TrimEvents(w); n != 10 {
		t.Fatalf("first TrimEvents = %d, want 10", n)
	}
	if len(w.ArchivedEvents) != 10 {
		t.Fatalf("after first trim: len(ArchivedEvents) = %d, want 10", len(w.ArchivedEvents))
	}
	// Second tick: append 5 more events with monotonically
	// higher Ticks (MaxLiveEvents+10 .. MaxLiveEvents+14). The
	// live set grows to MaxLiveEvents+5; the trim drops the
	// oldest 5, so the archive grows by exactly 5 (from 10 to
	// 15), not by 10+5.
	newEvents := make([]core.Event, 5)
	for i := range newEvents {
		newEvents[i] = core.Event{
			ID:   "enew" + itoaSmall(i),
			Type: core.EventFamineRisk,
			Tick: int64(MaxLiveEvents + 10 + i),
		}
	}
	w.Events = append(w.Events, newEvents...)
	before := len(w.ArchivedEvents)
	if n := TrimEvents(w); n != 5 {
		t.Errorf("second TrimEvents = %d, want 5 (new excess only)", n)
	}
	if len(w.ArchivedEvents) != before+5 {
		t.Errorf("after second trim: len(ArchivedEvents) = %d, want %d (archive accumulates)",
			len(w.ArchivedEvents), before+5)
	}
}

// TestTrimEvents_DisabledWhenMaxZero verifies that a cap of 0 or
// negative disables retention. This is the v1-pre-Phase-26
// behavior: events are never trimmed.
func TestTrimEvents_DisabledWhenMaxZero(t *testing.T) {
	w := newTestWorld()
	w.Events = makeEvents(MaxLiveEvents * 2) // way over the production cap
	n := trimEventsTo(w, 0)                  // explicit zero
	if n != 0 {
		t.Errorf("trimEventsTo(_, 0) returned %d, want 0 (cap disabled)", n)
	}
	if len(w.Events) != MaxLiveEvents*2 {
		t.Errorf("len(Events) = %d, want %d (cap disabled, no trim)",
			len(w.Events), MaxLiveEvents*2)
	}
	if len(w.ArchivedEvents) != 0 {
		t.Errorf("len(ArchivedEvents) = %d, want 0 (cap disabled, no archiving)", len(w.ArchivedEvents))
	}
}

// TestTrimEvents_ArchiveIsDecoupledFromLive verifies that the
// archive is not affected by subsequent appends to w.Events.
// The FIFO trim copies the dropped prefix into a fresh slice,
// so even if the live slice's backing array is re-used and
// overwritten, the archive must be untouched.
func TestTrimEvents_ArchiveIsDecoupledFromLive(t *testing.T) {
	w := newTestWorld()
	w.Events = makeEvents(MaxLiveEvents + 10)
	TrimEvents(w)
	// Capture the archived event IDs.
	archivedIDs := make([]string, len(w.ArchivedEvents))
	for i, e := range w.ArchivedEvents {
		archivedIDs[i] = e.ID
	}
	// Force the live slice to grow past its current capacity by
	// appending many events. This may re-use the backing array
	// and overwrite the dropped prefix in place.
	for i := 0; i < MaxLiveEvents*2; i++ {
		w.Events = append(w.Events, core.Event{
			ID:   "fill" + itoaSmall(i),
			Type: core.EventFamineRisk,
			Tick: int64(MaxLiveEvents + 100 + i),
		})
	}
	// The archive's IDs must be unchanged.
	if len(w.ArchivedEvents) != len(archivedIDs) {
		t.Fatalf("len(ArchivedEvents) changed: got %d, want %d",
			len(w.ArchivedEvents), len(archivedIDs))
	}
	for i, id := range archivedIDs {
		if w.ArchivedEvents[i].ID != id {
			t.Errorf("ArchivedEvents[%d].ID = %q, want %q (archive corrupted by live-slice append)",
				i, w.ArchivedEvents[i].ID, id)
		}
	}
}

// TestTrimMemories_UnderCap is the memory-side equivalent of
// TestTrimEvents_UnderCap.
func TestTrimMemories_UnderCap(t *testing.T) {
	w := newTestWorld()
	w.Memories = makeMemories(50)
	n := TrimMemories(w)
	if n != 0 {
		t.Errorf("TrimMemories returned %d, want 0", n)
	}
	if len(w.Memories) != 50 {
		t.Errorf("len(Memories) = %d, want 50 (unchanged)", len(w.Memories))
	}
	if len(w.ArchivedMemories) != 0 {
		t.Errorf("len(ArchivedMemories) = %d, want 0", len(w.ArchivedMemories))
	}
}

// TestTrimMemories_OverCap verifies that the oldest excess
// memories are archived and the live set is bounded.
func TestTrimMemories_OverCap(t *testing.T) {
	w := newTestWorld()
	const overflow = 100
	w.Memories = makeMemories(MaxLiveMemories + overflow)
	n := TrimMemories(w)
	if n != overflow {
		t.Errorf("TrimMemories returned %d, want %d", n, overflow)
	}
	if len(w.Memories) != MaxLiveMemories {
		t.Errorf("len(Memories) = %d, want %d (capped)", len(w.Memories), MaxLiveMemories)
	}
	if len(w.ArchivedMemories) != overflow {
		t.Errorf("len(ArchivedMemories) = %d, want %d", len(w.ArchivedMemories), overflow)
	}
	// Cutoff check: oldest live has Tick == overflow.
	if w.Memories[0].Tick != int64(overflow) {
		t.Errorf("Memories[0].Tick = %d, want %d (cutoff)", w.Memories[0].Tick, overflow)
	}
}

// TestTrimMemories_AppendsNewToArchive is the memory-side
// equivalent of TestTrimEvents_AppendsNewToArchive.
func TestTrimMemories_AppendsNewToArchive(t *testing.T) {
	w := newTestWorld()
	w.Memories = makeMemories(MaxLiveMemories + 50)
	if n := TrimMemories(w); n != 50 {
		t.Fatalf("first TrimMemories = %d, want 50", n)
	}
	before := len(w.ArchivedMemories)
	// Second tick: append 25 NEW memories with the next 25
	// ticks in monotonic order. The trim drops the oldest 25.
	newMems := make([]core.Memory, 25)
	for i := range newMems {
		newMems[i] = core.Memory{
			ID:        "mnew" + itoaSmall(i),
			OwnerID:   "alice",
			Tick:      int64(MaxLiveMemories + 50 + i),
			Importance: 0.5,
		}
	}
	w.Memories = append(w.Memories, newMems...)
	if n := TrimMemories(w); n != 25 {
		t.Errorf("second TrimMemories = %d, want 25", n)
	}
	if len(w.ArchivedMemories) != before+25 {
		t.Errorf("after second trim: len(ArchivedMemories) = %d, want %d (append)",
			len(w.ArchivedMemories), before+25)
	}
}

// TestTrimMemories_DisabledWhenMaxZero verifies cap-disabled
// behavior for memories.
func TestTrimMemories_DisabledWhenMaxZero(t *testing.T) {
	w := newTestWorld()
	w.Memories = makeMemories(MaxLiveMemories * 2)
	n := trimMemoriesTo(w, 0)
	if n != 0 {
		t.Errorf("trimMemoriesTo(_, 0) returned %d, want 0 (cap disabled)", n)
	}
	if len(w.Memories) != MaxLiveMemories*2 {
		t.Errorf("len(Memories) = %d, want %d (cap disabled, no trim)",
			len(w.Memories), MaxLiveMemories*2)
	}
}
