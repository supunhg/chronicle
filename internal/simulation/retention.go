// Phase 26 Part B: Retention Policies.
//
// Chronicle's event and memory logs grow unbounded over time. The
// 5-Generation Integration Test (Phase 24) runs 36,500 ticks; a
// 10-seed stress test (Phase 26 Part C) will run 200 years per
// seed. Without a cap, the live event log bloats the WorldHash
// (which sorts + hashes every event) and the memory log bloats
// the memory engine's per-tick decay and lookback passes.
//
// This file provides bounded growth: each log keeps at most
// MaxLiveEvents / MaxLiveMemories of its newest entries. The
// oldest excess is moved to World.ArchivedEvents /
// World.ArchivedMemories, which the live engines never read. The
// archive is unbounded in v1 (a future phase may cap it).
//
// The trim runs at the end of EventEngine.Tick and
// MemoryEngine.Tick, AFTER the engine has finished appending to
// the log for this tick. This means the engine's "this tick"
// events/memories always survive the trim (they are the newest).
//
// # Implementation note: FIFO, not sort
//
// A previous version of this file sorted w.Events / w.Memories
// by (Tick, ID) before trimming. That was O(n log n) per tick
// once the cap was hit, which over a 36,500-tick run added
// enough wall time to blow past the v1 acceptance suite's
// 30-minute timeout (and tripped the 5-minute per-test timeout
// in CI).
//
// The sort was unnecessary. Every engine that appends to
// w.Events or w.Memories does so in monotonically increasing
// tick order, and within a single tick the engine iterates
// either a sorted slice (FamineRule) or a deterministic Go-map
// traversal that was hardened in Phase 26 Part E. So the
// underlying slice is already in (Tick, within-tick) order,
// and the oldest excess is simply the first len-maxLive
// entries.
//
// The current implementation is O(excess) for the copy +
// O(1) for the slice reslice: ~10x faster on a 100K-cap
// workload and orders of magnitude faster on the 200-year
// stress runs.
//
// # Why FIFO is correct for the archive
//
// The archive is consumed by tools that read it in insertion
// order (Phase 27+). Insertion order with FIFO trim is
// chronological, so the archive reads oldest-first naturally.
// The core.WorldHash function sorts the live set before
// hashing (Phase 25), so the trim's slice order does not
// affect the hash.
//
// # Contract for engine authors
//
// Every engine that appends to w.Events or w.Memories MUST
// append in monotonically increasing tick order. The FIFO
// trim relies on this: the oldest excess is simply the
// first len-maxLive entries. An engine that needs to emit
// an out-of-order event (e.g. a delayed retrospective event
// with Tick < w.Tick) must sort w.Events by Tick before
// calling TrimEvents, or the archival will be wrong. This
// is not a v1 concern (no engine emits out of order), but
// v2 engines should treat it as load-bearing.

package simulation

import (
	"github.com/chronicle-dev/chronicle/internal/core"
)

// MaxLiveEvents is the upper bound on len(w.Events). When the
// live event log grows past this, the oldest excess is moved
// to w.ArchivedEvents. The cap is generous (10,000) — a 100-year
// frontier run produces ~5,000-10,000 events, so the cap is
// rarely hit in the v1 acceptance suite. The Phase 26 Part C
// 10-seed × 200-year stress test will exercise the cap more
// aggressively.
//
// Setting MaxLiveEvents to 0 or a negative value disables
// retention: w.Events is never trimmed and w.ArchivedEvents
// stays empty. This is the v1-pre-Phase-26 behavior; new tests
// can opt in by leaving the constant at 10000.
const MaxLiveEvents = 10000

// MaxLiveMemories is the upper bound on len(w.Memories). Same
// pattern as MaxLiveEvents. A 100-year run produces ~50,000-
// 100,000 memories (one per birth + one per death + several
// per work/socialize/court action), so the cap is right at the
// edge of the 100-year horizon. The Phase 26 Part C 200-year
// stress test will hit the cap routinely.
//
// Setting MaxLiveMemories to 0 or a negative value disables
// retention.
const MaxLiveMemories = 100000

// TrimEvents trims w.Events to at most MaxLiveEvents entries
// by dropping the oldest excess (FIFO). Returns the number
// of events archived (0 when under the cap).
//
// The dropped events are copied into a fresh slice and
// appended to w.ArchivedEvents. The copy is necessary because
// w.Events' backing array may be re-used on subsequent appends
// (a Go slice re-slice keeps the original backing array, and
// a later append can write into slots that the archived slice
// is still pointing at).
func TrimEvents(w *core.World) int {
	return trimEventsTo(w, MaxLiveEvents)
}

// TrimMemories trims w.Memories to at most MaxLiveMemories
// entries by dropping the oldest excess (FIFO). Returns the
// number archived.
func TrimMemories(w *core.World) int {
	return trimMemoriesTo(w, MaxLiveMemories)
}

// trimEventsTo is the testable form of TrimEvents: it takes an
// explicit cap so a test can exercise the under-cap, at-cap, and
// over-cap cases without touching the production constant.
//
// Contract: w.Events MUST be in monotonic tick order at the time
// of the call (oldest at index 0, newest at the end). See the
// file-level comment for the rationale and the v2 caveat.
func trimEventsTo(w *core.World, maxLive int) int {
	if maxLive <= 0 || len(w.Events) <= maxLive {
		return 0
	}
	excess := len(w.Events) - maxLive
	// Copy the dropped prefix into a fresh slice so the archive
	// is decoupled from the live slice's backing array. The live
	// slice's underlying array will be re-used / extended by
	// later appends; the archive must not be affected.
	archived := make([]core.Event, excess)
	copy(archived, w.Events[:excess])
	w.ArchivedEvents = append(w.ArchivedEvents, archived...)
	// Reslice the live set in place. The backing array is shared
	// with the original slice, but the dropped prefix is no
	// longer reachable through w.Events, so future appends (which
	// may write into those slots if the cap is large enough)
	// cannot affect the archive slice above.
	w.Events = w.Events[excess:]
	return excess
}

// trimMemoriesTo is the testable form of TrimMemories. Same
// FIFO pattern as trimEventsTo.
//
// Contract: w.Memories MUST be in monotonic tick order at the
// time of the call (oldest at index 0, newest at the end). See
// the file-level comment for the rationale and the v2 caveat.
func trimMemoriesTo(w *core.World, maxLive int) int {
	if maxLive <= 0 || len(w.Memories) <= maxLive {
		return 0
	}
	excess := len(w.Memories) - maxLive
	archived := make([]core.Memory, excess)
	copy(archived, w.Memories[:excess])
	w.ArchivedMemories = append(w.ArchivedMemories, archived...)
	w.Memories = w.Memories[excess:]
	return excess
}
