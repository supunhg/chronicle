// Romance-route priority-wiring test (Phase 38.F).
//
// §38.E landed TestAllEndingsReachable, which proves that for
// each (protagonist × ending) combination, the runtime walker
// can drive the authored YAML to act3.the_end with the target
// ending's gate satisfied. §38.E is REACHABILITY.
//
// §38.F is PRIORITY-WIN. For each romance target (Elara /
// Selene / Orion), the spine that fires BOTH the romance-
// conditioning scene AND a non-conflicting realm-claim
// ending's flag must return the ROMANCE ending (priority
// 9-11) from endings.Evaluate, NOT the realm claim
// (priority 1-8). That's §20's priority ordering actually
// resolving at the finale, as opposed to be an inert spec
// property nobody has hooked up to the runtime.
//
// Asserted alongside the priority-win are two supporting
// properties to make the priority comparison genuine (rather
// than a vacuous "romance beats wanderer"):
//   P1. Evaluate returns the romance ending ID.
//   P2. Every realm-claim Ending condition resolves true
//       against the post-walk WorldState (the spine DID fire
//       the realm flag, so priorities are being compared on
//       equal footing).
//   P3. The romance's Priority numerically exceeds the realm
//       claim's Priority per §20 (romance 9-11 vs realm 1-8).
//
// Together with §38.E, this file delivers §40.C's combined
// reachability + priority-dominance coverage from the test
// side.
//
// Terminology note: §38.E proves the romance ending is
// REACHABLE. §38.F proves the romance ending WINS over the
// realm claim the same spine triggers. Per §20, priority
// 9-11 (romance) always outranks 1-8 (realm claim); §38.F
// enforces that order is honored when both gates fire on
// one walk.

package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/state"
)

// priorityOf returns the Priority of the ending with the
// given ID in list, or -1 if not found. Used to format
// failure messages with the actual numeric priority of the
// compared endings.
func priorityOf(list []endings.Ending, id string) int {
	for _, e := range list {
		if e.ID == id {
			return e.Priority
		}
	}
	return -1
}

// romanceRoute is a single §38.F row: a target romance ending
// paired with the canonical spine + tail that fires it on
// the same walk as a non-conflicting realm-claim ending.
type romanceRoute struct {
	name       string   // ending ID for t.Run
	spine      []string // canonical pre-Act-3 path
	tail       []string // hub_<companion> → terminal_choice → claim_<realm> → realm_terminal
	realmClaim string   // §20 ending ID the tail's third+fourth choice fires (along with the romance)
	// realmChoices is the ordered list of choice IDs in the
	// realm-claim portion of the tail. Lifted out so the
	// assertions can list them by name when reporting failures.
	realmChoices []string
}

// TestRomanceRoutesWired walks each of the three romance
// routes through the runtime walker, then asserts P1+P2+P3
// on the post-walk WorldState. Each row corresponds to one
// of the three romance premiums §20 names (Elara, Selene,
// Orion) paired with the realm-claim ending reachable from
// the same spine so priority-win has meaning.
//
// Spine definitions are duplicated from §38.E's
// TestAllEndingsReachable. The duplication is intentional:
// §38.F asserts a different property (positive priority-win,
// not negative ID-match), and a self-contained test row set
// keeps failure messages scoped to one phase.
func TestRomanceRoutesWired(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// spineA — Ashwick relic quest, accept_elara,
	//          let_selene_read, decline_aldrics_commission,
	//          give_harlan_the_letter, end-of-Act-2 → act3 hub.
	spineA := []string{
		"continue_to_ashwick",         // 1
		"enter_keep",                  // 2: act1.greyhall_keep
		"speak_to_keeper",             // 3: act1.keeper_interview (ally_call sidebar; no redirect)
		"offer_help",                  // 4: act1.dragon_relic_in_vault
		"claim_relic",                 // 5: act1.ally_appears_intro (relic_claimed)
		"accept_elara",                // 6: act1.act1_close_ridge (recruited_elara)
		"head_down_to_mountains",      // 7: act1.mountain_path_begins
		"continue_to_act2",            // 8: act2.act2_entry
		"cross_into_mountains",        // 9: act2.mountain_crossing
		"approach_selenes_tower",      // 10: act2.selenes_tower
		"approach_selene",             // 11: act2.selenes_initiation
		"let_selene_read",             // 12: act2.selenes_farewell (selene_read_the_relic)
		"study_with_maren",            // 13: act2.maren_first_question → act2.maren_farewell
		"head_for_aldrics_keep",       // 14: act2.aldric_keep
		"decline_aldrics_commission",  // 15: act2.aldric_farewell
		"head_for_harlans_roadside",   // 16: act2.harlan_roadside
		"give_harlan_the_letter",      // 17: act2.harlan_warning (harlan_persuades no-redirect)
		"follow_harlans_warning",      // 18: act2.harlan_farewell
		"head_for_act2_closeup",       // 19: act2.act2_closeup
		"continue_to_act3",            // 20: act2.act3_handoff
		"enter_the_keep",              // 21: act3.keep_interior
	}

	// spineC — same prefix as A through approach_selene, then
	// refuses Selene's reading, takes Orion's broth, declines
	// Aldric, ends at act3.keep_interior. Sets orion_oath_taken;
	// leaves selene_read_the_relic un-set.
	spineC := []string{
		"continue_to_ashwick",          // 1
		"enter_keep",                   // 2
		"speak_to_keeper",              // 3
		"offer_help",                   // 4
		"claim_relic",                  // 5
		"accept_elara",                 // 6
		"head_down_to_mountains",       // 7
		"continue_to_act2",             // 8
		"cross_into_mountains",         // 9
		"approach_selenes_tower",       // 10
		"approach_selene",              // 11
		"refuse_selene",                // 12: spine C diverges (selene trust -15)
		"head_for_orions_camp",         // 13: act2.pathway_a
		"enter_orions_camp",            // 14: act2.orions_camp
		"accept_orions_challenge",      // 15: act2.orions_challenge (orion_betrayal event)
		"take_orions_broth",            // 16: act2.orions_farewell (orion_oath_taken)
		"head_for_maren_library",       // 17: act2.pathway_b
		"approach_maren_library",       // 18: act2.maren_library
		"ask_first_question",           // 19: act2.maren_first_question (maren_teaches no-op, selene_read not set)
		"study_with_maren",             // 20: act2.maren_farewell
		"head_for_aldrics_keep",        // 21: act2.aldric_keep
		"decline_aldrics_commission",   // 22: act2.aldric_farewell
		"head_for_harlans_roadside",    // 23: act2.harlan_roadside
		"give_harlan_the_letter",       // 24: act2.harlan_warning
		"follow_harlans_warning",       // 25: act2.harlan_farewell
		"head_for_act2_closeup",        // 26: act2.act2_closeup
		"continue_to_act3",             // 27: act2.act3_handoff
		"enter_the_keep",               // 28: act3.keep_interior
	}

	// The three romance routes. Each rows's tail fires the
	// romance-conditioning scene FIRST (sets the relationship
	// axis to gate threshold) and a non-conflicting claim_*
	// route SECOND (sets the realm-claim flag). The romance
	// (pri 9-11) is expected to win over the realm (pri 1-8).
	routes := []romanceRoute{
		{
			name:  "elara_romance",
			spine: spineA,
			tail: []string{
				"stand_watch_with_elara", // hub choice, gated [recruited_elara]
				"vigil_until_dawn",       // sets Elara affection=75 (absolute-set)
				"claim_unbound_magic",    // hub choice, unconditional
				"speak_the_language",     // sets archmage_unbound=true
			},
			realmClaim:   "archmage",
			realmChoices: []string{"claim_unbound_magic", "speak_the_language"},
		},
		{
			name:  "selene_romance",
			spine: spineA,
			tail: []string{
				"let_selene_finish",      // hub choice, gated [selene_read_the_relic]
				"thank_selene",           // sets Selene trust=50 (absolute-set, gate threshold)
				"claim_both_worlds",      // hub choice, unconditional
				"step_between",           // sets both_worlds_saved=true
			},
			realmClaim:   "world_guardian",
			realmChoices: []string{"claim_both_worlds", "step_between"},
		},
		{
			name:  "orion_romance",
			spine: spineC,
			tail: []string{
				"sit_with_orion",         // hub choice, gated [orion_oath_taken]
				"take_the_ring",          // sets Orion affection=75 (absolute-set)
				"claim_hero_path",        // hub choice, unconditional
				"walk_with_them",         // sets mid_completed + saved_companions
			},
			realmClaim:   "hero",
			realmChoices: []string{"claim_hero_path", "walk_with_them"},
		},
	}

	for _, p := range loaded.Protagonists {
		for _, r := range routes {
			t.Run(p.Name+"/"+r.name, func(t *testing.T) {
				ws := state.NewWorldState()
				if err := initProtagonistState(&ws, p); err != nil {
					t.Fatalf("initProtagonistState: %v", err)
				}

				path := append([]string(nil), r.spine...)
				path = append(path, r.tail...)

				trace, walkErrs := runtimeWalk(&ws, loaded.Graph, loaded.Events, path)
				if len(walkErrs) > 0 {
					for _, e := range walkErrs {
						t.Errorf("runtimeWalk[%s/%s]: %v", p.Name, r.name, e)
					}
					t.Logf("trace so far: %s", formatTrace(trace))
					return
				}

				// Property 1: Evaluate returns the romance ending.
				got, ok := endings.Evaluate(ws, loaded.Endings)
				if !ok {
					t.Fatalf("Evaluate returned no ending for %s/%s at end-state", p.Name, r.name)
				}
				if got.ID != r.name {
					t.Errorf("Evaluate = %q (priority %d); want %q (priority %d); the romance gate should win over %q (priority %d), which the spine also fires via [%s]. Per §20, romance priorities 9-11 outrank realm-claim priorities 1-8; either the Evaluate priority comparison regressed, or the romance gate failed to fire (%s/%s).",
						got.ID, got.Priority,
						r.name, priorityOf(loaded.Endings, r.name),
						r.realmClaim, priorityOf(loaded.Endings, r.realmClaim),
						strings.Join(r.realmChoices, ", "),
						p.Name, r.name)
					// Don't return — let P2/P3 report independently
					// so the diagnostic tells the maintainer whether
					// both gates fired or only one.
				}

				// Property 2: the realm-claim ending's conditions
				// resolve AGAINST the post-walk WorldState. This
				// proves the spine actually fired the realm flag,
				// making the priority-win above a real comparison
				// and not a vacuous "romance beats wanderer".
				var realmEnd endings.Ending
				var realmFound bool
				for _, e := range loaded.Endings {
					if e.ID == r.realmClaim {
						realmEnd = e
						realmFound = true
						break
					}
				}
				if !realmFound {
					t.Fatalf("realm claim %q not found in loaded endings (registry corruption)", r.realmClaim)
				}
				for i, cond := range realmEnd.Conditions {
					if !cond.Check(ws) {
						t.Errorf("realm-claim %q condition[%d] did not resolve against post-walk WorldState; the spine's tail [%s] did not fire the §20 realm gate, so the priority-win above is vacuous (romance beats wanderer, not the realm). Trace: %s",
							r.realmClaim, i,
							strings.Join(r.realmChoices, ", "),
							formatTrace(trace))
					}
				}

				// Property 3: romance priority numerically exceeds
				// realm priority per §20 ordering. This should hold
				// even when both gates fail (it's a YAML invariant,
				// not a runtime invariant), so it's an unconditional
				// check on the loaded registry.
				romancePri := priorityOf(loaded.Endings, r.name)
				realmPri := priorityOf(loaded.Endings, r.realmClaim)
				if romancePri <= realmPri {
					t.Errorf("romance %q priority %d should numerically exceed realm %q priority %d per §20 (romance 9-11, realm 1-8). If this fires, ending priorities in worldpacks/frontier/endings.yaml are out of canonical §20 order — review the priority column.",
						r.name, romancePri, r.realmClaim, realmPri)
				}
			})
		}
	}
}
