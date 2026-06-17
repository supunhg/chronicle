package story

import (
	"reflect"
	"strings"
	"testing"
)

// TestCompanionsFromYAML_Canonical exercises the standard
// single-companion case with id + description. The companion
// shape is intentionally simple (no conditions, no effects)
// — companions are a passive roster, gated by protagonist
// starting_party references at Phase 36.E's validation gate.
func TestCompanionsFromYAML_Canonical(t *testing.T) {
	in := `companions:
  - id: "Elara"
    description: "A ranger from the eastern wood."
`
	got, err := CompanionsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("CompanionsFromYAML: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("companions count = %d; want 1", len(got))
	}
	if got[0].ID != "Elara" {
		t.Errorf("companion[0].ID = %q; want Elara", got[0].ID)
	}
	if !strings.Contains(got[0].Description, "ranger") {
		t.Errorf("companion[0].Description = %q; want contains 'ranger'", got[0].Description)
	}
}

// TestCompanionsFromYAML_MultipleCompanionsPreservedOrder
// verifies that input order is preserved (future phases may
// rely on this for deterministic companion-select ordering).
func TestCompanionsFromYAML_MultipleCompanionsPreservedOrder(t *testing.T) {
	in := `companions:
  - id: "Elara"
    description: "Ranger."
  - id: "Selene"
    description: "Mage."
  - id: "Orion"
    description: "Warrior."
`
	got, err := CompanionsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("CompanionsFromYAML: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("companions count = %d; want 3", len(got))
	}
	wantIDs := []string{"Elara", "Selene", "Orion"}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("companion[%d].ID = %q; want %q (input order not preserved)", i, got[i].ID, want)
		}
	}
}

// TestCompanionsFromYAML_DuplicateIDRejected verifies the
// uniqueness gate. Two companions with the same id is a Load
// error — content authors cannot accidentally double-register
// a roster entry.
func TestCompanionsFromYAML_DuplicateIDRejected(t *testing.T) {
	in := `companions:
  - id: "Elara"
    description: "First."
  - id: "Elara"
    description: "Second."
`
	_, err := CompanionsFromYAML([]byte(in))
	if err == nil {
		t.Fatal("CompanionsFromYAML with duplicate id did not error")
	}
	if !strings.Contains(err.Error(), "duplicated") || !strings.Contains(err.Error(), "Elara") {
		t.Errorf("CompanionsFromYAML duplicate-id error = %q; want 'duplicated' + id name", err.Error())
	}
}

// TestCompanionsFromYAML_EmptyIDRejected verifies the
// fail-fast contract: a companion with id: "" errors before
// any artifact is allocated.
func TestCompanionsFromYAML_EmptyIDRejected(t *testing.T) {
	in := `companions:
  - id: ""
    description: "Anonymous."
`
	_, err := CompanionsFromYAML([]byte(in))
	if err == nil {
		t.Fatal("CompanionsFromYAML with empty id did not error")
	}
	if !strings.Contains(err.Error(), "empty id") {
		t.Errorf("CompanionsFromYAML empty-id error = %q; want 'empty id'", err.Error())
	}
}

// TestCompanionsFromYAML_EmptyListAllowed verifies the
// boundary case: an authored world with no companions is
// valid (and was the canonical state before Phase 38.A's
// first companion roster ships).
func TestCompanionsFromYAML_EmptyListAllowed(t *testing.T) {
	got, err := CompanionsFromYAML([]byte("companions: []\n"))
	if err != nil {
		t.Fatalf("CompanionsFromYAML empty list: %v", err)
	}
	if got == nil {
		t.Fatal("CompanionsFromYAML empty list returned nil; want non-nil zero-length slice")
	}
	if len(got) != 0 {
		t.Errorf("CompanionsFromYAML empty list count = %d; want 0", len(got))
	}
}

// TestCompanionsFromYAML_ParseErrorBubbles verifies that
// malformed YAML surfaces a wrapped error naming the
// canonical parser, not a raw yaml.v3 error.
func TestCompanionsFromYAML_ParseErrorBubbles(t *testing.T) {
	_, err := CompanionsFromYAML([]byte(`companions: [{ broken`))
	if err == nil {
		t.Fatal("CompanionsFromYAML bad YAML did not error")
	}
	if !strings.Contains(err.Error(), "CompanionsFromYAML") {
		t.Errorf("CompanionsFromYAML parse error = %q; want prefix 'CompanionsFromYAML'", err.Error())
	}
}

// TestCompanionsFromYAML_UnknownFieldTolerated locks the
// forward-compatibility guarantee: Phase 38 may add a
// `backstory_node` field to companions, and existing YAML
// files with that field MUST still parse. yaml.v3 silently
// drops unknown fields unless a stricter mode is set, so
// this test is more a regression guard than a correctness
// check — but it's worth pinning so an unannounced change
// to the parser (e.g., introducing Strict mode) is caught.
//
// Assertions target only the stable ID + Description fields
// (Phase 37.C's surface), NOT the full Companion struct via
// reflect.DeepEqual: when Phase 38's depth pass adds new
// Companion fields, this test stays green as long as ID +
// Description still land correctly.
func TestCompanionsFromYAML_UnknownFieldTolerated(t *testing.T) {
	in := `companions:
  - id: "Elara"
    description: "Ranger."
    backstory_node: "kael.companion_elara_origin"
    future_field: 42
`
	got, err := CompanionsFromYAML([]byte(in))
	if err != nil {
		t.Fatalf("CompanionsFromYAML unknown-field: %v (parser must tolerate unknown keys)", err)
	}
	if len(got) != 1 {
		t.Fatalf("companions count = %d; want 1", len(got))
	}
	if reflect.TypeOf(got[0]) != reflect.TypeOf(Companion{}) {
		t.Errorf("companion[0] type = %s; want %s", reflect.TypeOf(got[0]), reflect.TypeOf(Companion{}))
	}
	if got[0].ID != "Elara" {
		t.Errorf("companion[0].ID = %q; want Elara", got[0].ID)
	}
	if got[0].Description != "Ranger." {
		t.Errorf("companion[0].Description = %q; want %q (unknown fields leaked into Description)", got[0].Description, "Ranger.")
	}
}
