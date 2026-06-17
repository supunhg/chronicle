package state

import "encoding/json"

// SaveGame is the canonical v2 save file shape per §18.
//
// One file, JSON-serialized. Version is 0 for v2.0 (Phase 39.B
// adds the version-mismatch refuse-to-load gate — old-version
// saves fail with a clear error message rather than silent
// migration).
//
// SaveGame intentionally uses a value receiver for Marshal
// (the JSON encoder doesn't care) so the loop-level pointer
// indirection stays explicit. Decode into a pointer per Go
// json conventions; Unmarshal needs a pointer receiver.
type SaveGame struct {
	Version    int
	WorldState WorldState
}

// Marshal encodes a SaveGame as JSON. Phase 39.A will swap this
// for a canonical sorted-key encoder that produces byte-stable
// round-trips per §18A invariant #1; Phase 36.A uses encoding/json
// directly so the smoke test can exercise round-trip semantics
// without bringing in the canonical encoder.
//
// Phase 36.A: Marshal returns encoding/json bytes. The Player
// CLI call is `json.Marshal(sg)`. A facade method here keeps the
// internal API clean so 39.A's swap is one line later.
func (sg SaveGame) Marshal() ([]byte, error) {
	return json.Marshal(sg)
}

// Unmarshal decodes a SaveGame from JSON. It is the inverse of
// Marshal (encoding/json symmetry). Phase 39.B adds a version
// mismatch gate; Phase 36.A accepts any well-formed JSON that
// decodes into SaveGame's exported fields.
func (sg *SaveGame) Unmarshal(data []byte) error {
	return json.Unmarshal(data, sg)
}
