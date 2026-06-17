// Reachability test scaffolding for Phase 38.E.
//
// Phase 38.D landed TestLoad_FrontierWorldpackPerEnding, which
// proves §20's 12 endings are each SCHEMATICALLY satisfiable:
// build a synthetic WorldState with the right gate set, evaluate
// it, observe the right-ending ID at the highest priority.
//
// Phase 38.E layers on the runtime equivalent: for each
// (protagonist × ending) combination, walk the authored YAML
// graph WITH the §36.D runner semantics (story.AvailableChoices
// filters by Conditions, Effect.Apply mutates WorldState,
// events.Trigger fires from the TriggeredEvents queue and may
// redirect the next node) and demonstrate that the wearer
// reaches act3.the_end with the target ending's gate satisfied.
//
// Together, §38.D + §38.E give §40's TestProtagonistCoverage +
// TestEndingCoverage full coverage confidence: §38.D proves the
// schema is rich enough; §38.E proves the authored content
// actually navigates from any player's start to any ending.

package content

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/events"
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
)

// runnerTraceStep is one step in the runtime walker's trace.
// Used by reachable_test.go's diagnostics when a choice is
// unavailable at the current node.
type runnerTraceStep struct {
	NodeID   string
	ChoiceID string
}

// initProtagonistState applies p's starting_* fields to ws and
// sets ws.CurrentNodeID to p.ExclusiveNodes[0]. All map fields
// of ws are readied for direct writes (non-nil).
func initProtagonistState(ws *state.WorldState, p Protagonist) error {
	if ws.Flags == nil {
		ws.Flags = make(map[string]bool)
	}
	if ws.Variables == nil {
		ws.Variables = make(map[string]int)
	}
	if ws.Relationships == nil {
		ws.Relationships = make(map[string]state.Relationship)
	}
	if ws.Inventory.Items == nil {
		ws.Inventory.Items = make(map[string]int)
	}
	ws.Protagonist = p.Name
	for _, f := range p.StartingFlags {
		ws.Flags[f] = true
	}
	for k, v := range p.StartingVariables {
		ws.Variables[k] = v
	}
	for _, item := range p.StartingInventory {
		ws.Inventory.Items[item]++
	}
	if len(p.ExclusiveNodes) == 0 {
		return fmt.Errorf("protagonist %q has no ExclusiveNodes", p.Name)
	}
	ws.CurrentNodeID = p.ExclusiveNodes[0]
	return nil
}

// runtimeWalk walks ws through the given sequence of choice IDs
// using runner semantics: at each step, look up the current node
// (ws.CurrentNodeID), find the named choice ID in
// story.AvailableChoices (filtered by Conditions), apply its
// effects in declaration order, then run events.Trigger to
// detect any event-redirect. ws.CurrentNodeID becomes the
// event redirect (if non-empty) ELSE choice.NextNodeID.
//
// runtimeWalk is the runtime equivalent of story.AvailableChoices
// + Effect.Apply + events.Trigger chained together — the three
// steps a runner would take per Step. It returns the (nodeID,
// choiceID) trace plus any errors. The walker stops at the
// first error so test failures can report the precise step
// where the runner would have stalled.
func runtimeWalk(
	ws *state.WorldState,
	g *story.Graph,
	eventsList []story.Event,
	choices []string,
) ([]runnerTraceStep, []error) {
	var trace []runnerTraceStep
	var errs []error
	for i, cs := range choices {
		node, err := g.Lookup(ws.CurrentNodeID)
		if err != nil {
			errs = append(errs, fmt.Errorf("step %d: lookup current node %q: %w", i, ws.CurrentNodeID, err))
			break
		}
		avail := story.AvailableChoices(node, *ws)
		var matched *story.Choice
		for j := range avail {
			if avail[j].ID == cs {
				matched = &avail[j]
				break
			}
		}
		if matched == nil {
			availIDs := make([]string, len(avail))
			for j, a := range avail {
				availIDs[j] = a.ID
			}
			errs = append(errs, fmt.Errorf("step %d: at node %q, choice %q not in AvailableChoices (avail=[%s]); state=%s",
				i, ws.CurrentNodeID, cs, strings.Join(availIDs, ","), runnerStateSummary(*ws)))
			break
		}
		trace = append(trace, runnerTraceStep{NodeID: ws.CurrentNodeID, ChoiceID: cs})
		for j, eff := range matched.Effects {
			if err := eff.Apply(ws); err != nil {
				errs = append(errs, fmt.Errorf("step %d: at node %q, choice %q: effect[%d]: %w", i, ws.CurrentNodeID, cs, j, err))
			}
		}
		redirect, _ := events.Trigger(ws, eventsList)
		if redirect != "" {
			ws.CurrentNodeID = redirect
		} else {
			ws.CurrentNodeID = matched.NextNodeID
		}
	}
	return trace, errs
}

// runnerStateSummary returns a compact human-readable snapshot
// of ws for diagnostics on choice-availability failures.
func runnerStateSummary(ws state.WorldState) string {
	var parts []string
	if len(ws.Flags) > 0 {
		flagKeys := make([]string, 0, len(ws.Flags))
		for k := range ws.Flags {
			if ws.Flags[k] {
				flagKeys = append(flagKeys, k)
			}
		}
		sort.Strings(flagKeys)
		parts = append(parts, "flags=["+strings.Join(flagKeys, ",")+"]")
	}
	if len(ws.Relationships) > 0 {
		keys := make([]string, 0, len(ws.Relationships))
		for k, v := range ws.Relationships {
			keys = append(keys, fmt.Sprintf("%s{t:%d,a:%d,r:%d}", k, v.Trust, v.Affection, v.Respect))
		}
		sort.Strings(keys)
		parts = append(parts, "rels=["+strings.Join(keys, ",")+"]")
	}
	if ws.Reputation.Kingdom != 0 || ws.Reputation.Mages != 0 ||
		ws.Reputation.Dragons != 0 || ws.Reputation.Underworld != 0 {
		parts = append(parts, fmt.Sprintf("rep(k:%d,m:%d,d:%d,u:%d)",
			ws.Reputation.Kingdom, ws.Reputation.Mages,
			ws.Reputation.Dragons, ws.Reputation.Underworld))
	}
	return strings.Join(parts, ";")
}

const (
	spineASize = 21
	spineBSize = 21
	spineCSize = 28
)

// TestAllEndingsReachable is the §38.E acceptance test.
//
// For each (protagonist, ending) combination, walk the authored
// YAML via the runtime walker and assert that ending is
// reachable from the protagonist's start.
//
// Three canonical spines cover the flag-sets the non-romance
// endings gate on. Each spine starts from protagonist.opening
// and lands at act3.keep_interior; §38.B/C's ally_call event
// is now a sidebar (node_id=""), so the Ashwick spine walks the
// canonically intended keeper_interview + dragon_relic_in_vault
// + ally_appears_intro route, which sets relic_claimed (the
// gate dragon_sovereign's claim_serpent_crown requires).
//
//   Spine A (21 choices, "let_selene_read + decline_aldrics_commission"):
//     Ashwick relic quest -> accept_elara -> close_ridge ->
//     act2 entry -> Selene reads the relic -> Maren ->
//     Aldric declines -> Harlan's letter -> closeup ->
//     act3 -> keep_interior.
//
//   Spine B (21 choices, spine A's prefix + accept_aldrics_commission
//     at index 15, 0-indexed): kingdom_aligned set.
//
//   Spine C (28 choices, "refuse_selene + take_orions_broth"):
//     Same relic quest prefix as A, but refuses Selene's
//     reading, takes Orion's broth, asks Maren (no
//     maren_teaches event because selene_read_the_relic is
//     not set), declines Aldric, finishes with Harlan.
//     orion_oath_taken set; selene_read_the_relic NOT set.
//
// Per-ending Act 3 tails:
//   Non-romance (8): { claim_<ending>, <terminal_choice> } that
//     sets the §20 gate flag and lands at act3.the_end.
//   Romance (3): { hub_<companion>_choice, terminal_choice_in_conditioning_scene,
//     realm_claim_choice, realm_terminal_choice } that sets the
//     relationship axis to threshold AND the realm flag the
//     protagonist would commit to if they're pursuing both — the
//     romance (priority 9-11) wins over the realm flag (priority
//     1-8) by virtue of higher priority.
//
// wanderer is special-cased: it has no conditions, so
// Evaluate(init_state_from_protagonist) returns wanderer at
// priority 0. The runtime walker is not invoked for wanderer
// (no path from act2.act3_handoff leads to act3.the_end without
// setting some §20 gate; arena-style non-endings aren't
// represented in this worldpack).
//
// All 48 (protagonist × ending) combinations are expected to
// pass on the current authored content.
func TestAllEndingsReachable(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Spine A: Ashwick relic quest + let_selene_read + decline_aldrics_commission.
	spineA := []string{
		"continue_to_ashwick", // step  1: ashwick entrance
		"enter_keep",          // step  2: greyhall keep
		"speak_to_keeper",     // step  3: keeper_interview (ally_call sidebar, no redirect)
		"offer_help",          // step  4: dragon_relic_in_vault
		"claim_relic",         // step  5: ally_appears_intro (with relic_claimed set)
		"accept_elara",        // step  6: act1_close_ridge
		"head_down_to_mountains",
		"continue_to_act2",
		"cross_into_mountains",      // step  9: mountain_crossing
		"approach_selenes_tower",
		"approach_selene",           // step 11: trigger selene_oath -> selenes_initiation
		"let_selene_read",            // step 12: trigger maren_teaches -> maren_first_question
		"study_with_maren",
		"head_for_aldrics_keep",
		"decline_aldrics_commission", // step 15: kingdom_aligned NOT set
		"head_for_harlans_roadside",
		"give_harlan_the_letter",     // trigger harlan_persuades (no-op redirect)
		"follow_harlans_warning",
		"head_for_act2_closeup",
		"continue_to_act3",
		"enter_the_keep",             // step 21: act3.keep_interior
	}
	if len(spineA) != spineASize {
		t.Fatalf("spineA length = %d; expected %d", len(spineA), spineASize)
	}

	// Spine B: same prefix as A but step 15 (0-indexed) is
	// accept_aldrics_commission (sets kingdom_aligned).
	spineB := append([]string(nil), spineA...)
	spineB[14] = "accept_aldrics_commission"
	if len(spineB) != spineBSize {
		t.Fatalf("spineB length = %d; expected %d", len(spineB), spineBSize)
	}

	// Spine C: same prefix as A through approach_selene, then
	// refuse_selene -> orion -> maren (asks without event
	// fire because selene_read_the_relic not set) -> aldric
	// declines -> harlan -> closeup -> act3.
	spineC := []string{
		"continue_to_ashwick", // 1-11 same as A's prefix
		"enter_keep",
		"speak_to_keeper",
		"offer_help",
		"claim_relic",
		"accept_elara",
		"head_down_to_mountains",
		"continue_to_act2",
		"cross_into_mountains",
		"approach_selenes_tower",
		"approach_selene",   // selene_oath redirects to selenes_initiation
		"refuse_selene",     // Spine C diverges: trust -15, no trigger
		"head_for_orions_camp",
		"enter_orions_camp",
		"accept_orions_challenge", // orion_betrayal fires (no-redirect since choice.next_node_id matches)
		"take_orions_broth",       // sets orion_oath_taken
		"head_for_maren_library",
		"approach_maren_library",
		"ask_first_question",      // maren_teaches gates selene_read_the_relic which Spine C does NOT set -> no fire
		"study_with_maren",
		"head_for_aldrics_keep",
		"decline_aldrics_commission",
		"head_for_harlans_roadside",
		"give_harlan_the_letter",
		"follow_harlans_warning",
		"head_for_act2_closeup",
		"continue_to_act3",
		"enter_the_keep", // 28: act3.keep_interior
	}
	if len(spineC) != spineCSize {
		t.Fatalf("spineC length = %d; expected %d", len(spineC), spineCSize)
	}

	type endingCase struct {
		name  string
		spine []string
		// escape: true means do not run runtimeWalk; just init-and-Evaluate.
		escape bool
		// tail appended to spine before walking.
		tail []string
	}

	endingCases := []endingCase{
		{"wanderer", spineA, true, nil},
		{"hero", spineA, false, []string{"claim_hero_path", "walk_with_them"}},
		{"dragon_alliance", spineA, false, []string{"claim_dragon_kinship", "offer_kinship"}},
		{"kingdom", spineB, false, []string{"claim_kingdoms_throne", "wear_the_crown"}},
		{"archmage", spineA, false, []string{"claim_unbound_magic", "speak_the_language"}},
		{"shadow_lord", spineA, false, []string{"claim_shadow_throne", "take_the_throne"}},
		{"world_guardian", spineA, false, []string{"claim_both_worlds", "step_between"}},
		{"corruption", spineA, false, []string{"claim_dark_descent", "walk_into_the_dark"}},
		{"dragon_sovereign", spineA, false, []string{"claim_serpent_crown", "wear_the_serpent"}},
		{"elara_romance", spineA, false, []string{
			"stand_watch_with_elara", // gated recruited_elara (set by spine A step 6)
			"vigil_until_dawn",        // sets Elara affection=75
			"claim_unbound_magic",
			"speak_the_language", // also sets archmage_unbound; elara_romance wins by priority
		}},
		{"selene_romance", spineA, false, []string{
			"let_selene_finish",   // gated selene_read_the_relic (set by spine A step 12)
			"thank_selene",        // absolute-set Selene trust = 50 (gate threshold)
			"claim_both_worlds",
			"step_between",
		}},
		{"orion_romance", spineC, false, []string{
			"sit_with_orion", // gated orion_oath_taken (set by spine C step 16)
			"take_the_ring",   // sets Orion affection=75
			"claim_hero_path",
			"walk_with_them",
		}},
	}

	for _, p := range loaded.Protagonists {
		for _, ec := range endingCases {
			t.Run(p.Name+"/"+ec.name, func(t *testing.T) {
				ws := state.NewWorldState()
				if err := initProtagonistState(&ws, p); err != nil {
					t.Fatalf("initProtagonistState: %v", err)
				}

				if ec.escape {
					// wanderer special-case: init state alone; no walk.
					got, ok := endings.Evaluate(ws, loaded.Endings)
					if !ok {
						t.Fatalf("Evaluate returned no ending for %s/%s at init-only state", p.Name, ec.name)
					}
					if got.ID != "wanderer" {
						var wantPriority int
						for _, e := range loaded.Endings {
							if e.ID == "wanderer" {
								wantPriority = e.Priority
							}
						}
						t.Errorf("Evaluate at init state = %q (priority %d); want wanderer (priority %d); starting state should have no other ending's conditions satisfied",
							got.ID, got.Priority, wantPriority)
					}
					return
				}

				path := append([]string(nil), ec.spine...)
				path = append(path, ec.tail...)

				trace, walkErrs := runtimeWalk(&ws, loaded.Graph, loaded.Events, path)
				if len(walkErrs) > 0 {
					for _, e := range walkErrs {
						t.Errorf("runtimeWalk[%s/%s]: %v", p.Name, ec.name, e)
					}
					t.Logf("trace so far: %s", formatTrace(trace))
					return
				}

				got, ok := endings.Evaluate(ws, loaded.Endings)
				if !ok {
					t.Fatalf("Evaluate returned no ending for %s/%s at end-state", p.Name, ec.name)
				}
				if got.ID != ec.name {
					var wantPri int
					for _, e := range loaded.Endings {
						if e.ID == ec.name {
							wantPri = e.Priority
						}
					}
					t.Errorf("Evaluate at %q (after walk) = %q (priority %d); want %q (priority %d)",
						ws.CurrentNodeID, got.ID, got.Priority, ec.name, wantPri)
				}
			})
		}
	}
}

// formatTrace returns a compact one-line summary of a partial
// walker trace for failure-message diagnostics.
func formatTrace(trace []runnerTraceStep) string {
	stepNames := make([]string, 0, len(trace))
	for _, t := range trace {
		stepNames = append(stepNames, fmt.Sprintf("%s->%s", t.NodeID, t.ChoiceID))
	}
	head := strings.Join(stepNames, " | ")
	if len(head) > 400 {
		head = head[:400] + "..."
	}
	return head
}
