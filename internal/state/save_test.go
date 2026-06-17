package state

import (
	"bytes"
	"strings"
	"testing"
)

// TestSaveLoadRoundTrip asserts the §18A byte-stable round-trip
// invariant: pre-save WorldHash equals post-load WorldHash after
// Marshal → Unmarshal. The test also asserts structural fidelity on
// key fields so a hash-only check can't accidentally pass when
// fields are silently dropped.
func TestSaveLoadRoundTrip(t *testing.T) {
	ws := NewWorldState()
	ws.CurrentNodeID = "act1.opening"
	ws.Protagonist = "kael"
	ws.Flags["met_elara"] = true
	ws.Flags["visited_temple"] = true
	ws.Variables["courage"] = 90
	ws.Variables["corruption"] = 5
	ws.Relationships["elara"] = Relationship{Trust: 75, Affection: 30, Respect: 50}
	ws.Relationships["orion"] = Relationship{Trust: -10, Affection: 60, Respect: 0}
	ws.Reputation = ReputationState{Kingdom: 60, Mages: -25, Dragons: 0, Underworld: 10}
	ws.Inventory.Items["dragon_key"] = 1
	ws.Inventory.Items["healing_potion"] = 3
	ws.Party = []string{"elara", "orion"}
	ws.EndingsUnlocked = []string{}
	ws.TriggeredEvents = []string{"event_a", "event_b"}

	sg := SaveGame{Version: 0, WorldState: ws}
	pre := WorldHash(sg)

	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var loaded SaveGame
	if err := loaded.Unmarshal(data); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	post := WorldHash(loaded)
	if pre != post {
		t.Errorf("WorldHash mismatch\n  pre:  %s\n  post: %s\n  data: %s", pre, post, data)
	}

	if loaded.WorldState.CurrentNodeID != ws.CurrentNodeID {
		t.Errorf("CurrentNodeID round-trip: got %q want %q",
			loaded.WorldState.CurrentNodeID, ws.CurrentNodeID)
	}
	if loaded.WorldState.Variables["courage"] != ws.Variables["courage"] {
		t.Errorf("courage round-trip: got %d want %d",
			loaded.WorldState.Variables["courage"], ws.Variables["courage"])
	}
	if loaded.WorldState.Relationships["elara"] != ws.Relationships["elara"] {
		t.Errorf("elara relationship round-trip: got %+v want %+v",
			loaded.WorldState.Relationships["elara"], ws.Relationships["elara"])
	}
	if loaded.WorldState.Reputation != ws.Reputation {
		t.Errorf("reputation round-trip: got %+v want %+v",
			loaded.WorldState.Reputation, ws.Reputation)
	}
	if loaded.WorldState.Inventory.Items["dragon_key"] != 1 {
		t.Errorf("dragon_key round-trip: got %d want 1",
			loaded.WorldState.Inventory.Items["dragon_key"])
	}
}

// TestSaveLoadRoundTrip_Empty covers the zero-state case: a fresh
// NewWorldState with only CurrentNodeID set. Empty maps and slices
// must round-trip cleanly (encoding/json drops them).
func TestSaveLoadRoundTrip_Empty(t *testing.T) {
	sg := SaveGame{Version: 0, WorldState: NewWorldState()}
	sg.WorldState.CurrentNodeID = "intro"

	pre := WorldHash(sg)
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var loaded SaveGame
	if err := loaded.Unmarshal(data); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if pre != WorldHash(loaded) {
		t.Errorf("WorldHash mismatch (empty): pre=%s post=%s", pre, WorldHash(loaded))
	}
}

// TestCanonicalMarshal_KeyOrder verifies map keys are emitted
// in lexicographic order. Three keys inserted in non-alphabetical
// order (zebra, alpha, middle) must surface in the canonical
// output as alpha, middle, zebra.
func TestCanonicalMarshal_KeyOrder(t *testing.T) {
	sg := SaveGame{0, NewWorldState()}
	sg.WorldState.Flags["zebra"] = true
	sg.WorldState.Flags["alpha"] = true
	sg.WorldState.Flags["middle"] = true

	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	aIdx := bytes.Index(data, []byte(`"alpha":true`))
	mIdx := bytes.Index(data, []byte(`"middle":true`))
	zIdx := bytes.Index(data, []byte(`"zebra":true`))
	if aIdx == -1 || mIdx == -1 || zIdx == -1 {
		t.Fatalf("expected all three keys present in: %s", data)
	}
	if !(aIdx < mIdx && mIdx < zIdx) {
		t.Errorf("keys not in sorted order: a=%d m=%d z=%d in: %s", aIdx, mIdx, zIdx, data)
	}
}

// TestCanonicalMarshal_NumberPreservation verifies integer
// Variables round-trip as integers (not floats). Go's
// encoding/json otherwise coerces numbers to float64 on the
// decoder side; the canonical pass tokens via json.Number
// preserves the integer representation.
func TestCanonicalMarshal_NumberPreservation(t *testing.T) {
	sg := SaveGame{0, NewWorldState()}
	sg.WorldState.Variables["courage"] = 90

	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"courage":90`) {
		t.Errorf("expected \"courage\":90 (integer) in: %s", data)
	}
	if strings.Contains(string(data), `"courage":90.0`) {
		t.Errorf("courage must not be float-formatted: %s", data)
	}
}

// TestTamperedSave_UnknownField: injecting an unknown field into
// the canonical JSON must produce a clear error on Unmarshal.
// §18A tamper-resistance via DisallowUnknownFields.
func TestTamperedSave_UnknownField(t *testing.T) {
	sg := SaveGame{0, NewWorldState()}
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	tampered := strings.Replace(string(data),
		`"Version":0`,
		`"Version":0,"HackerField":"injected"`,
		1)

	var loaded SaveGame
	err = loaded.Unmarshal([]byte(tampered))
	if err == nil {
		t.Errorf("tampered save (unknown field): got nil err; want non-nil")
		return
	}
	// The Go std-library error names the offending field. We
	// require the message mention either the field name or
	// "unknown" so future readers recognise the failure.
	if !strings.Contains(err.Error(), "HackerField") &&
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("error message should mention unknown field; got: %v", err)
	}
}

// TestTamperedSave_BadType: replacing Version with a non-numeric
// value must reject the load. Even though a content-level change
// (e.g. flagging corruption), this verifies the JSON type-system
// gate catches typed-tampering.
func TestTamperedSave_BadType(t *testing.T) {
	sg := SaveGame{0, NewWorldState()}
	data, err := sg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	tampered := strings.Replace(string(data),
		`"Version":0`,
		`"Version":"not_a_number"`,
		1)

	var loaded SaveGame
	err = loaded.Unmarshal([]byte(tampered))
	if err == nil {
		t.Errorf("tampered save (bad type): got nil err; want non-nil")
	}
}

// TestTamperedSave_BadJSON: malformed JSON must reject the load
// with a clear error from encoding/json.
func TestTamperedSave_BadJSON(t *testing.T) {
	var loaded SaveGame
	err := loaded.Unmarshal([]byte("{ broken json "))
	if err == nil {
		t.Errorf("broken JSON: got nil err; want non-nil")
	}
}

// TestWorldHash_Deterministic verifies hashing the same SaveGame
// twice yields the same WorldHash (idempotent).
func TestWorldHash_Deterministic(t *testing.T) {
	sg := SaveGame{0, NewWorldState()}
	sg.WorldState.Flags["a"] = true
	sg.WorldState.Variables["x"] = 100
	sg.WorldState.Relationships["elara"] = Relationship{Trust: 50}

	h1 := WorldHash(sg)
	h2 := WorldHash(sg)
	if h1 != h2 {
		t.Errorf("WorldHash non-deterministic across same input: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("WorldHash length = %d; want 64 (SHA-256 hex)", len(h1))
	}
}

// TestWorldHash_DistinctForDifferentSaves verifies two structurally-
// different SaveGames produce different WorldHashes.
func TestWorldHash_DistinctForDifferentSaves(t *testing.T) {
	sg1 := SaveGame{0, NewWorldState()}
	sg1.WorldState.Flags["a"] = true
	sg2 := SaveGame{0, NewWorldState()}
	sg2.WorldState.Flags["a"] = true
	sg2.WorldState.Flags["b"] = true // different by exactly one flag

	if WorldHash(sg1) == WorldHash(sg2) {
		t.Errorf("different saves must produce different WorldHashes")
	}
}
