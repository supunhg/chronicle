package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// SaveGame is the canonical v2 save file shape per ARCHITECTURE.md §18.
//
// One file, JSON-serialized. Version is 0 for v2.0 (Phase 39.B
// adds the version-mismatch refuse-to-load gate). Marshal + Unmarshal
// use the canonical sorted-key + DisallowUnknownFields form per
// §18A invariants 1 (byte-stable round-trip) and tamper-resistance.
type SaveGame struct {
	Version    int
	WorldState WorldState
}

// CurrentVersion is the canonical save version shipped by this
// build of Chronicle. It gates load-time migration per
// ARCHITECTURE.md §18A invariant #3 ("versioning and migration
// are explicit; no silent migration"). Unmarshal refuses to
// load a SaveGame whose Version differs from CurrentVersion —
// older saves are rejected because there's no forward migration,
// newer saves are rejected because the player should update
// Chronicle to load them.
//
// Bumping CurrentVersion is the explicit migration checkpoint:
// old-version saves will refuse to load, the migration code
// (when authored in a future phase) should run on the rejected
// SaveGame before the load retry.
const CurrentVersion = 0

// Marshal encodes a SaveGame using the canonical (§18A invariant #1)
// sorted-key JSON form. The output is byte-stable:
//
//	SaveGame -> bytes -> SaveGame -> bytes
//
// yields identical bytes because (a) the intermediate encoding/json
// pass is deterministic over a fixed struct, and (b) the
// encodeCanonical re-emitting pass sorts map keys lexicographically.
//
// Marshal preserves integer/float distinction via json.Number
// tokens read back from the intermediate decoding pass. This avoids
// the float64 coercion path in encoding/json that loses precision
// for large integers (rare in CYOA but the discipline matters).
func (sg SaveGame) Marshal() ([]byte, error) {
	intermediate, err := json.Marshal(sg)
	if err != nil {
		return nil, fmt.Errorf("state: marshal intermediate: %w", err)
	}
	var root any
	dec := json.NewDecoder(bytes.NewReader(intermediate))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("state: decode intermediate: %w", err)
	}
	var buf bytes.Buffer
	if err := encodeCanonical(&buf, root); err != nil {
		return nil, fmt.Errorf("state: emit canonical: %w", err)
	}
	return buf.Bytes(), nil
}

// Unmarshal decodes a SaveGame from canonical JSON. Per §18A
// tamper-resistance, DisallowUnknownFields rejects any save with
// fields outside the SaveGame struct — a structural guarantee
// rather than a content check.
//
// DisallowUnknownFields fires when:
//   - an unknown top-level field (e.g., "HackerField") appears
//   - an unknown nested field appears (e.g., Flags has a value
//     where a string is expected).
//
// It does NOT catch a field whose VALUE has been tampered. To
// catch content tampering, callers compare the post-load WorldHash
// against a separately-stored pre-save hash (Phase 40.E's
// TestSaveLoadResilience gate).
//
// Per §18A invariant #3, Unmarshal refuses to load a save whose
// Version differs from CurrentVersion. Specifically:
//   - sg.Version < CurrentVersion -> "save is from an older build";
//     no backward migration is supported.
//   - sg.Version > CurrentVersion -> "save is from a newer build";
//     update Chronicle to load it.
// Sided errors (vs. a generic "version mismatch") so the player
// can act on them — either roll back to the older build or
// upgrade.
func (sg *SaveGame) Unmarshal(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(sg); err != nil {
		return fmt.Errorf("state: unmarshal: %w", err)
	}
	if sg.Version < CurrentVersion {
		return fmt.Errorf("state: unmarshal: this save is from an older Chronicle build (save version=%d, current=%d); no backward migration is supported", sg.Version, CurrentVersion)
	}
	if sg.Version > CurrentVersion {
		return fmt.Errorf("state: unmarshal: this save is from a newer Chronicle build (save version=%d, current=%d); update Chronicle to load it", sg.Version, CurrentVersion)
	}
	return nil
}

// WorldHash returns the SHA-256 hex digest of sg's canonical
// JSON encoding. Per ARCHITECTURE.md §18A's WorldHash reference:
//
//	The v2 save's WorldHash(w) is a SHA256 of the canonicalized
//	SaveGame JSON (sorted keys, normalized numbers). It is used
//	for save-load regression tests, NOT for cross-simulation
//	reproducibility (v2 has no simulation in the player path).
//
// TestSaveLoadRoundTrip asserts pre-save WorldHash == post-load
// WorldHash. Returns empty string only if Marshal fails, which
// should not happen for a well-formed SaveGame.
func WorldHash(sg SaveGame) string {
	b, err := sg.Marshal()
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// encodeCanonical re-emits the JSON-tokenised v (parsed from
// encoding/json's intermediate pass) with sorted keys and
// json.Number-preserving numeric tokens.
//
// It walks the tree depth-first; for each object, keys are
// collected and lexicographically sorted before re-emission.
// Map iteration order in Go is non-deterministic; this normalisation
// is what makes the WorldHash byte-stable across runs and machines.
//
// encodeCanonical is intentionally minimal: it only handles the
// JSON value types that can appear in a SaveGame
// (nil, bool, json.Number, string, []any, map[string]any). If a
// future field adds an unsupported type, encodeCanonical surfaces
// an explicit error so the migration path is loud, not silent.
func encodeCanonical(w io.Writer, v any) error {
	switch t := v.(type) {
	case nil:
		_, err := w.Write([]byte("null"))
		return err
	case bool:
		if t {
			_, err := w.Write([]byte("true"))
			return err
		}
		_, err := w.Write([]byte("false"))
		return err
	case json.Number:
		// json.Number preserves the original numeric token from the
		// JSON stream (e.g., "90" vs "90.0"). Emitting as-is keeps
		// integer-vs-float distinction intact across round-trips.
		_, err := w.Write([]byte(t.String()))
		return err
	case string:
		// json.Marshal escapes the string per RFC 8259 (quotes,
		// backslashes, control characters). Bytes are returned
		// unchanged because the canonical output is byte-stable for
		// the same input string.
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	case []any:
		if _, err := w.Write([]byte("[")); err != nil {
			return err
		}
		for i, e := range t {
			if i > 0 {
				if _, err := w.Write([]byte(",")); err != nil {
					return err
				}
			}
			if err := encodeCanonical(w, e); err != nil {
				return err
			}
		}
		_, err := w.Write([]byte("]"))
		return err
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if _, err := w.Write([]byte("{")); err != nil {
			return err
		}
		for i, k := range keys {
			if i > 0 {
				if _, err := w.Write([]byte(",")); err != nil {
					return err
				}
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			if _, err := w.Write(kb); err != nil {
				return err
			}
			if _, err := w.Write([]byte(":")); err != nil {
				return err
			}
			if err := encodeCanonical(w, t[k]); err != nil {
				return err
			}
		}
		_, err := w.Write([]byte("}"))
		return err
	}
	return fmt.Errorf("state: encodeCanonical: unsupported type %T", v)
}
