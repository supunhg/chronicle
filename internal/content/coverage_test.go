// Phase 40.A acceptance test gates.
//
// PHASES.md §40.D nails the four gates:
//   – TestProtagonistCoverage: each of 4 playable protagonists
//     (Kael, Lyra, Raven, Aria) can reach AT LEAST ONE §20
//     romance ending (priority 9-11) AND AT LEAST ONE §20
//     non-romance ending (priority 0-8). §38.E + §38.F already
//     prove the full 4×12 matrix is reachable; this gate re-
//     asserts the per-protag minimum so the §40 Definition of
//     Done reads as a single, scannable condition.
//   – TestEndingCoverage: each of the 12 §20 endings is
//     reachable from at least one protag's start via runtimeWalk.
//     Uses the same spine definitions as §38.E; one protag per
//     ending suffices because §38.E already proved all 4 protags.
//   – TestConditionCoverage: every concrete Condition type in
//     internal/story/conditions.go (Flag, VariableGE,
//     RelationshipGE, HasItem, HasEnding, Or, And, Not) is
//     exercised by ≥ 1 authored choice across the loaded
//     worldpack. Catches dead-on-arrival schema extensions.
//   – TestEffectCoverage: every concrete Effect type in
//     internal/story/effects.go (SetFlag, ClearFlag,
//     ModifyVariable, ModifyRelationship, ModifyReputation,
//     AddItem, RemoveItem, TriggerEvent) is exercised by ≥ 1
//     authored choice across the loaded worldpack. Same
//     rationale — live schema coverage, not dead imports.
//
// Spine definitions are duplicated from reachable_test.go
// (spineA/B/C) and romance_routes_test.go (romance tails).
// The duplication is intentional: this file is the §40
// Definition-of-Done gate and must be self-contained so a
// future refactor of §38 tests does not silently retire the
// §40 contract.
//
// The 4 tests share the load-the-frontier-worldpack setup,
// but each is independent — a failure in one does not skip
// the others, so the test report surfaces all four gate
// pass/fail states at once.

package content

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// spineAPrefix is the canonical Ashwick-relic-quest spine
// (21 choices). Every Choice ID here is reachable from any
// ExclusiveNodes[0] of the four protagonists, so this spine
// is universal — see reachable_test.go's TestAllEndingsReachable.
func spineAPrefix() []string {
	return []string{
		"continue_to_ashwick",         // 1: ashwick entrance
		"enter_keep",                  // 2: greyhall keep
		"speak_to_keeper",             // 3: keeper_interview
		"offer_help",                  // 4: dragon_relic_in_vault
		"claim_relic",                 // 5: ally_appears_intro
		"accept_elara",                // 6: act1_close_ridge
		"head_down_to_mountains",      // 7
		"continue_to_act2",            // 8: act2 entry
		"cross_into_mountains",        // 9
		"approach_selenes_tower",      // 10
		"approach_selene",             // 11: selene_oath -> selenes_initiation
		"let_selene_read",             // 12: maren_teaches -> maren_first_question
		"study_with_maren",            // 13
		"head_for_aldrics_keep",       // 14
		"decline_aldrics_commission",  // 15: kingdom_aligned NOT set
		"head_for_harlans_roadside",   // 16
		"give_harlan_the_letter",      // 17: harlan_persuades (no-redirect)
		"follow_harlans_warning",      // 18
		"head_for_act2_closeup",       // 19
		"continue_to_act3",            // 20: act3_handoff
		"enter_the_keep",              // 21: act3.keep_interior
	}
}

// spineBPrefix is spineAPrefix with choice 15 exchanged for
// `accept_aldrics_commission` (sets kingdom_aligned).
func spineBPrefix() []string {
	s := spineAPrefix()
	s[14] = "accept_aldrics_commission"
	return s
}

// spineCPrefix is the Orion-path variant (28 choices). Same
// Act 1 prefix as A through approach_selene, then refuses
// Selene's reading, takes Orion's broth, declines Aldric,
// ends at act3.keep_interior. Sets orion_oath_taken; leaves
// selene_read_the_relic unset.
func spineCPrefix() []string {
	return []string{
		"continue_to_ashwick",
		"enter_keep",
		"speak_to_keeper",
		"offer_help",
		"claim_relic",
		"accept_elara",
		"head_down_to_mountains",
		"continue_to_act2",
		"cross_into_mountains",
		"approach_selenes_tower",
		"approach_selene",
		"refuse_selene", // Spine C diverges here.
		"head_for_orions_camp",
		"enter_orions_camp",
		"accept_orions_challenge",
		"take_orions_broth",
		"head_for_maren_library",
		"approach_maren_library",
		"ask_first_question", // maren_teaches does NOT fire (selene_read not set)
		"study_with_maren",
		"head_for_aldrics_keep",
		"decline_aldrics_commission",
		"head_for_harlans_roadside",
		"give_harlan_the_letter",
		"follow_harlans_warning",
		"head_for_act2_closeup",
		"continue_to_act3",
		"enter_the_keep",
	}
}

// TestProtagonistCoverage is the §40.D gate that each
// protagonist can reach ≥ 1 romance + ≥ 1 non-romance
// ending.
//
// Per-protag: walk the elara_romance tail (priority 9, sets
// recruited_elara + Elara affection = 75) to demonstrate a
// romance outcome, then walk the hero tail (priority 1, sets
// mid_completed + saved_companions) to demonstrate a non-
// romance outcome. Each outcome is asserted against the §20
// priority band (9-11 = romance, 0-8 = non-romance).
func TestProtagonistCoverage(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Protagonists) == 0 {
		t.Fatal("loaded.Protagonists is empty; frontier worldpack must declare ≥ 1 protagonist per PHASES.md §38.A")
	}
	if len(loaded.Endings) == 0 {
		t.Fatal("loaded.Endings is empty; frontier worldpack must declare ≥ 1 ending per PHASES.md §38.A (12 per §20)")
	}

	const (
		romanceEnd = "elara_romance"
		nonEnd     = "hero"
	)
	romanceTail := []string{
		"stand_watch_with_elara",
		"vigil_until_dawn",
		"claim_unbound_magic",
		"speak_the_language",
	}
	nonTail := []string{
		"claim_hero_path",
		"walk_with_them",
	}

	for _, p := range loaded.Protagonists {
		t.Run(p.Name+"/romance", func(t *testing.T) {
			ws := state.NewWorldState()
			if err := initProtagonistState(&ws, p); err != nil {
				t.Fatalf("init: %v", err)
			}
			path := append([]string(nil), spineAPrefix()...)
			path = append(path, romanceTail...)
			trace, walkErrs := runtimeWalk(&ws, loaded.Graph, loaded.Events, path)
			if len(walkErrs) > 0 {
				for _, e := range walkErrs {
					t.Errorf("runtimeWalk: %v", e)
				}
				t.Logf("trace: %s", formatTrace(trace))
				return
			}
			got, ok := endings.Evaluate(ws, loaded.Endings)
			if !ok {
				t.Fatalf("Evaluate returned no ending for %s/%s at post-walk state", p.Name, romanceEnd)
			}
			if got.ID != romanceEnd {
				t.Errorf("Evaluate = %q (priority %d); want %q. Per §20, romance priorities live at 9-11; if a priority mismatch is visible, the elara_romance ending is wired wrong in endings.yaml.", got.ID, got.Priority, romanceEnd)
				return
			}
			if got.Priority < 9 || got.Priority > 11 {
				t.Errorf("Evaluate returned %q with priority %d; per §20, romance targets live at priority 9-11. Authored-priority column may have shifted.", got.ID, got.Priority)
			}
		})

		t.Run(p.Name+"/non_romance", func(t *testing.T) {
			ws := state.NewWorldState()
			if err := initProtagonistState(&ws, p); err != nil {
				t.Fatalf("init: %v", err)
			}
			path := append([]string(nil), spineAPrefix()...)
			path = append(path, nonTail...)
			trace, walkErrs := runtimeWalk(&ws, loaded.Graph, loaded.Events, path)
			if len(walkErrs) > 0 {
				for _, e := range walkErrs {
					t.Errorf("runtimeWalk: %v", e)
				}
				t.Logf("trace: %s", formatTrace(trace))
				return
			}
			got, ok := endings.Evaluate(ws, loaded.Endings)
			if !ok {
				t.Fatalf("Evaluate returned no ending for %s/%s at post-walk state", p.Name, nonEnd)
			}
			if got.ID != nonEnd {
				t.Errorf("Evaluate = %q (priority %d); want %q. hero gates on mid_completed + saved_companions; the spine above sets both via walk_with_them.", got.ID, got.Priority, nonEnd)
				return
			}
			if got.Priority > 8 {
				t.Errorf("Evaluate returned %q with priority %d; per §20, non-romance endings live at priority 0-8 (wanderer is 0; realm-claim variants are 1-7; dragon_sovereign is 8). Authored-priority column may have shifted.", got.ID, got.Priority)
			}
		})
	}
}

// TestEndingCoverage is the §40.D gate that every one of the
// 12 §20 endings is reachable from at least one protag's
// start via runtimeWalk. §38.E's TestAllEndingsReachable
// already proves the full 4×12 matrix; this gate re-asserts
// the simpler per-ending existence so the §40 DoD has a
// single named check.
//
// wanderer is special-cased: it has no conditions, so
// Evaluate(init_state_from_protagonist) returns wanderer at
// priority 0 — no runtime walk needed.
func TestEndingCoverage(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Protagonists) == 0 {
		t.Fatal("loaded.Protagonists is empty; frontier worldpack must declare ≥ 1 protagonist per PHASES.md §38.A")
	}
	if len(loaded.Endings) == 0 {
		t.Fatal("loaded.Endings is empty; frontier worldpack must declare ≥ 1 ending per PHASES.md §38.A (12 per §20)")
	}

	type endingCase struct {
		name   string
		spine  []string
		tail   []string
		escape bool // wanderer-style: init-only Evaluate, no walk.
	}
	cases := []endingCase{
		{"wanderer", spineAPrefix(), nil, true},
		{"hero", spineAPrefix(), []string{"claim_hero_path", "walk_with_them"}, false},
		{"dragon_alliance", spineAPrefix(), []string{"claim_dragon_kinship", "offer_kinship"}, false},
		{"kingdom", spineBPrefix(), []string{"claim_kingdoms_throne", "wear_the_crown"}, false},
		{"archmage", spineAPrefix(), []string{"claim_unbound_magic", "speak_the_language"}, false},
		{"shadow_lord", spineAPrefix(), []string{"claim_shadow_throne", "take_the_throne"}, false},
		{"world_guardian", spineAPrefix(), []string{"claim_both_worlds", "step_between"}, false},
		{"corruption", spineAPrefix(), []string{"claim_dark_descent", "walk_into_the_dark"}, false},
		{"dragon_sovereign", spineAPrefix(), []string{"claim_serpent_crown", "wear_the_serpent"}, false},
		{"elara_romance", spineAPrefix(), []string{"stand_watch_with_elara", "vigil_until_dawn", "claim_unbound_magic", "speak_the_language"}, false},
		{"selene_romance", spineAPrefix(), []string{"let_selene_finish", "thank_selene", "claim_both_worlds", "step_between"}, false},
		{"orion_romance", spineCPrefix(), []string{"sit_with_orion", "take_the_ring", "claim_hero_path", "walk_with_them"}, false},
	}

	// Pick any protagonist (Kael — first in loaded.Protagonists
	// order). §38.E has already proven all 4 protags reach all
	// 12 endings; this gate only needs ONE per-ending reachability
	// witness.
	if len(loaded.Protagonists) == 0 {
		t.Fatal("loaded.Endings []Protagonists empty; cannot pick a protagonist")
	}
	protag := loaded.Protagonists[0]

	for _, ec := range cases {
		t.Run(ec.name, func(t *testing.T) {
			ws := state.NewWorldState()
			if err := initProtagonistState(&ws, protag); err != nil {
				t.Fatalf("init: %v", err)
			}
			if ec.escape {
				got, ok := endings.Evaluate(ws, loaded.Endings)
				if !ok {
					t.Fatalf("Evaluate returned no ending at init-only state")
				}
				if got.ID != "wanderer" {
					t.Errorf("init-only Evaluate = %q (priority %d); want wanderer (priority 0). At init state, only wanderer's empty-conditions path can fire.", got.ID, got.Priority)
				}
				return
			}
			path := append([]string(nil), ec.spine...)
			path = append(path, ec.tail...)
			trace, walkErrs := runtimeWalk(&ws, loaded.Graph, loaded.Events, path)
			if len(walkErrs) > 0 {
				for _, e := range walkErrs {
					t.Errorf("runtimeWalk: %v", e)
				}
				t.Logf("trace: %s", formatTrace(trace))
				return
			}
			got, ok := endings.Evaluate(ws, loaded.Endings)
			if !ok {
				t.Fatalf("Evaluate returned no ending for %s at post-walk state", ec.name)
			}
			if got.ID != ec.name {
				t.Errorf("Evaluate = %q (priority %d); want %q. If this fires, %q's gate conditions in endings.yaml are not satisfied by the spine above; consult the Phase 38.F spine tables for the canonical choice sequence.", got.ID, got.Priority, ec.name, ec.name)
			}
		})
	}
}

// TestConditionCoverage asserts every concrete type that
// implements story.Condition is exercised by ≥ 1 authored
// choice across the loaded worldpack (nodes + endings).
//
// Required types come from internal/story/conditions.go: Flag,
// VariableGE, RelationshipGE, HasItem, HasEnding, Or, And, Not.
// The list is enumerated here (not auto-discovered via
// reflection) because Go reflection would also catch test-
// only Condition fakes and Author-side draft conditions
// that haven't shipped into canonical schema — the explicit
// list keeps the test contract in lockstep with the canonical
// Phase 37.B schema.
func TestConditionCoverage(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Graph.IDs()) == 0 {
		t.Fatal("loaded.Graph.IDs() is empty; frontier worldpack must declare ≥ 1 node per PHASES.md §37.A")
	}
	if len(loaded.Endings) == 0 {
		t.Fatal("loaded.Endings is empty; frontier worldpack must declare ≥ 1 ending per PHASES.md §38.A (12 per §20)")
	}

	seen := map[string]int{} // type name → count of uses
	for _, id := range loaded.Graph.IDs() {
		n, err := loaded.Graph.Lookup(id)
		if err != nil {
			t.Fatalf("Lookup %s: %v", id, err)
		}
		for _, c := range n.Choices {
			for _, cond := range c.Conditions {
				seen[conditionType(cond)]++
			}
		}
	}
	// Endings also carry conditions (the §20 gate list).
	for _, e := range loaded.Endings {
		for _, cond := range e.Conditions {
			seen[conditionType(cond)]++
		}
	}

	required := []string{
		"Flag", "VariableGE", "RelationshipGE", "HasItem",
		"HasEnding", "Or", "And", "Not",
	}
	missing := missingTypes(required, seen)
	if len(missing) > 0 {
		t.Errorf("condition type(s) used in schema but not exercised by any authored choice in worldpacks/frontier/: %s. Either (a) the YAML author needs to add a choice that uses the type, or (b) the type is documented but not implemented and should be removed from internal/story/conditions.go and PHASES.md §37.B.",
			strings.Join(missing, ", "))
	}
	t.Logf("condition-type usage in worldpack: %s", formatUsage(seen))
}

// TestEffectCoverage asserts every concrete type that
// implements story.Effect is exercised by ≥ 1 authored choice
// across the loaded worldpack.
//
// Required types come from internal/story/effects.go:
// SetFlag, ClearFlag, ModifyVariable, ModifyRelationship,
// ModifyReputation, AddItem, RemoveItem, TriggerEvent.
// Same explicit-list rationale as TestConditionCoverage.
func TestEffectCoverage(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Graph.IDs()) == 0 {
		t.Fatal("loaded.Graph.IDs() is empty; frontier worldpack must declare ≥ 1 node per PHASES.md §37.A")
	}
	if len(loaded.Endings) == 0 {
		t.Fatal("loaded.Endings is empty; frontier worldpack must declare ≥ 1 ending per PHASES.md §38.A (12 per §20)")
	}

	seen := map[string]int{}
	for _, id := range loaded.Graph.IDs() {
		n, err := loaded.Graph.Lookup(id)
		if err != nil {
			t.Fatalf("Lookup %s: %v", id, err)
		}
		for _, c := range n.Choices {
			for _, eff := range c.Effects {
				seen[effectType(eff)]++
			}
		}
	}
	// Endings are condition-only — they don't carry effects.
	// (Effects ARE distinct from conditions: effects mutate
	// state, conditions gate on it.)

	required := []string{
		"SetFlag", "ClearFlag", "ModifyVariable", "ModifyRelationship",
		"ModifyReputation", "AddItem", "RemoveItem", "TriggerEvent",
	}
	missing := missingTypes(required, seen)
	if len(missing) > 0 {
		t.Errorf("effect type(s) used in schema but not exercised by any authored choice in worldpacks/frontier/: %s. Either (a) the YAML author needs to add a choice that uses the type, or (b) the type is documented but not implemented and should be removed from internal/story/effects.go and PHASES.md §37.B.",
			strings.Join(missing, ", "))
	}
	t.Logf("effect-type usage in worldpack: %s", formatUsage(seen))
}

// conditionType returns the name of the concrete Condition
// type underlying c. Returns "unknown(<T>)" for unregistered
// types so the test failure message surfaces the actual
// underlying type rather than crashing on a missing case.
func conditionType(c story.Condition) string {
	switch c.(type) {
	case story.Flag:
		return "Flag"
	case story.VariableGE:
		return "VariableGE"
	case story.RelationshipGE:
		return "RelationshipGE"
	case story.HasItem:
		return "HasItem"
	case story.HasEnding:
		return "HasEnding"
	case story.Or:
		return "Or"
	case story.And:
		return "And"
	case story.Not:
		return "Not"
	}
	return fmt.Sprintf("unknown(%T)", c)
}

// effectType returns the name of the concrete Effect type
// underlying e. Returns "unknown(<T>)" for unregistered
// types so the test failure message surfaces the actual
// underlying type rather than crashing on a missing case.
func effectType(e story.Effect) string {
	switch e.(type) {
	case story.SetFlag:
		return "SetFlag"
	case story.ClearFlag:
		return "ClearFlag"
	case story.ModifyVariable:
		return "ModifyVariable"
	case story.ModifyRelationship:
		return "ModifyRelationship"
	case story.ModifyReputation:
		return "ModifyReputation"
	case story.AddItem:
		return "AddItem"
	case story.RemoveItem:
		return "RemoveItem"
	case story.TriggerEvent:
		return "TriggerEvent"
	}
	return fmt.Sprintf("unknown(%T)", e)
}

// missingTypes returns the subset of required that has zero
// uses in the seen map. Stable order (alphabetic on input).
func missingTypes(required []string, seen map[string]int) []string {
	reqSorted := append([]string(nil), required...)
	sort.Strings(reqSorted)
	var missing []string
	for _, name := range reqSorted {
		if seen[name] == 0 {
			missing = append(missing, name)
		}
	}
	return missing
}

// formatUsage renders the seen map as "Name=n Name=n ..." with
// names sorted alphabetically, for t.Logf diagnostics.
func formatUsage(seen map[string]int) string {
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s=%d", k, seen[k]))
	}
	return strings.Join(parts, " ")
}
