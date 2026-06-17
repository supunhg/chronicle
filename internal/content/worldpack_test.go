package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/story"
)

// TestLoad_FrontierWorldpack is the Phase 38.A + §38.B/C acceptance
// test: the production content.Load must successfully consume
// the worldpacks/frontier/ directory and produce a *Loaded
// bundle with the shape PHASES.md promises.
//
// The path is `../../worldpacks/frontier` because this test
// lives in internal/content/. Run from the project root
// (where `go test ./...` does) and the relative path resolves
// to the canonical worldpack root.
//
// Acceptance per PHASES.md §38.A:
//   - Four protagonists present (Kael, Lyra, Raven, Aria).
//   - Each protagonist has at least one ExclusiveNode that
//     resolves in the resulting *story.Graph (so the character-
//     select screen can route there).
//   - The Ashwick entrance is reachable from each protagonist's
//     opening (every protagonist <-> Act 1 opening).
//   - The events.yaml roster includes any IDs referenced by
//     trigger_event effects in nodes.yaml choices.
//   - Endings roster is 12 (matches ARCHITECTURE.md §20).
//
// Acceptance per PHASES.md §38.B/C:
//   - Each protagonist's deep exclusive (ExclusiveNodes[1]) is
//     reachable from act2.act2_entry via BFS across choices'
//     next_node_id (the static topology carries the protagonist
//     detour routes).
//   - act2.act3_handoff is the shared Act 2 terminal and is
//     reachable from act2.act2_entry via BFS.
//   - 8 events parse (3 from §38.A + 5 from §38.B/C).
func TestLoad_FrontierWorldpack(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load %s: %v", dir, err)
	}

	// ----- §38.A: 4 protagonists present + ExclusiveNodes resolve -----

	wantProtagonists := []string{"Kael", "Lyra", "Raven", "Aria"}
	if len(loaded.Protagonists) != len(wantProtagonists) {
		t.Errorf("protagonists count = %d; want %d", len(loaded.Protagonists), len(wantProtagonists))
	}
	gotNames := make(map[string]bool, len(loaded.Protagonists))
	for _, p := range loaded.Protagonists {
		gotNames[p.Name] = true
		if len(p.ExclusiveNodes) == 0 {
			t.Errorf("protagonist %q has empty ExclusiveNodes", p.Name)
		}
		for _, eid := range p.ExclusiveNodes {
			if _, err := loaded.Graph.Lookup(eid); err != nil {
				t.Errorf("protagonist %q exclusive node %q missing in graph: %v", p.Name, eid, err)
			}
		}
	}
	for _, w := range wantProtagonists {
		if !gotNames[w] {
			t.Errorf("protagonist %q missing from loaded.Protagonists (got %v)", w, gotNames)
		}
	}

	// ----- §38.A: each protagonist's opening routes to act1.ashwick_entrance -----

	for _, p := range loaded.Protagonists {
		if len(p.ExclusiveNodes) == 0 {
			continue
		}
		opening := p.ExclusiveNodes[0]
		node, err := loaded.Graph.Lookup(opening)
		if err != nil {
			t.Errorf("protagonist %q opening %q not in graph: %v", p.Name, opening, err)
			continue
		}
		found := false
		for _, c := range node.Choices {
			if c.NextNodeID == "act1.ashwick_entrance" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("protagonist %q opening %q has no choice routing to act1.ashwick_entrance", p.Name, opening)
		}
	}

	// ----- §38.A: Ashwick itself exists with the canonical title -----

	ashwick, err := loaded.Graph.Lookup("act1.ashwick_entrance")
	if err != nil {
		t.Errorf("act1.ashwick_entrance not in graph: %v", err)
	} else if !strings.Contains(ashwick.Title, "Ashwick") {
		t.Errorf("act1.ashwick_entrance title = %q; want contains 'Ashwick'", ashwick.Title)
	}

	// ----- §38.A: 12 endings per ARCHITECTURE.md §20 + romance variants present -----

	if len(loaded.Endings) != 12 {
		t.Errorf("endings count = %d; want 12", len(loaded.Endings))
	}
	wantRomance := map[string]bool{
		"elara_romance": false,
		"selene_romance": false,
		"orion_romance": false,
	}
	for _, e := range loaded.Endings {
		if _, ok := wantRomance[e.ID]; ok {
			wantRomance[e.ID] = true
		}
	}
	for id, present := range wantRomance {
		if !present {
			t.Errorf("ending %q missing from endings roster", id)
		}
	}

	// ----- §38.B/C: BFS reachability from act2.act2_entry to act2.act3_handoff -----

	act2Entry, err := loaded.Graph.Lookup("act2.act2_entry")
	if err != nil {
		t.Fatalf("act2.act2_entry not in graph: %v", err)
	}
	bfsReachable := bfsFrom(loaded.Graph, act2Entry.ID)
	if !bfsReachable["act2.act3_handoff"] {
		t.Errorf("act2.act3_handoff NOT reachable from act2.act2_entry via BFS (static topology)")
	}

	// ----- §38.B/C: BFS reaches each protagonist's deep exclusive -----

	wantDeepExclusives := []string{
		"kael.scholars_final_chapter",
		"lyra.runic_trial",
		"raven.heirs_judgment",
		"aria.dawns_first_prayer",
	}
	for _, want := range wantDeepExclusives {
		if !bfsReachable[want] {
			t.Errorf("deep exclusive %q NOT reachable from act2.act2_entry via BFS", want)
		}
	}

	// ----- §38.B/C: each act2 companion anchor is reachable from act2.act2_entry -----

	wantAnchors := []string{
		"act2.selenes_tower", "act2.orions_camp", "act2.maren_library",
		"act2.aldric_keep", "act2.harlan_roadside",
	}
	for _, want := range wantAnchors {
		if !bfsReachable[want] {
			t.Errorf("act 2 anchor %q NOT reachable from act2.act2_entry via BFS", want)
		}
	}

	// ----- §38.A + §38.B/C: events present + trigger_event cross-reference -----

	wantEventIDs := []string{
		"ally_call", "dragon_stirs", "faith_renewed",
		"relic_rumble", "selene_oath", "orion_betrayal",
		"maren_teaches", "aldric_commission", "harlan_persuades",
	}
	wantEvents := make(map[string]bool, len(wantEventIDs))
	for _, id := range wantEventIDs {
		wantEvents[id] = false
	}
	for _, ev := range loaded.Events {
		if _, ok := wantEvents[ev.ID]; ok {
			wantEvents[ev.ID] = true
		}
	}
	for id, present := range wantEvents {
		if !present {
			t.Errorf("event %q missing from events roster", id)
		}
	}

	// Cross-check: every trigger_event effect's ID resolves in
	// the events roster. Phase 36.E's validateTriggerEventIDs
	// enforces this at load; we re-assert it as a guard.
	knownEvents := make(map[string]bool, len(loaded.Events))
	for _, e := range loaded.Events {
		knownEvents[e.ID] = true
	}
	for _, n := range allKnownNodes(loaded) {
		for _, c := range n.Choices {
			for j, eff := range c.Effects {
				te, ok := eff.(story.TriggerEvent)
				if !ok {
					continue
				}
				if !knownEvents[te.ID] {
					t.Errorf("choice %q -> effect[%d] triggers unknown event %q", c.ID, j, te.ID)
				}
			}
		}
	}

	// ----- §38.A: roster of companions -----

	wantCompanions := []string{"Elara", "Selene", "Orion", "Aldric", "Maren", "Harlan"}
	buf, err := os.ReadFile(filepath.Join(dir, "companions.yaml"))
	if err != nil {
		t.Errorf("companions.yaml: %v", err)
	} else {
		for _, want := range wantCompanions {
			if !strings.Contains(string(buf), "id: \""+want+"\"") {
				t.Errorf("companion %q missing from companions.yaml", want)
			}
		}
	}
}

// bfsFrom walks the graph's static topology (every choice's
// next_node_id, plus the seed node itself) starting from
// startID and returns the set of IDs reachable. Condition gates
// are not applied (that's runtime state); this is the
// structural reachability test that mirrors PHASES.md §38.C's
// "pick protagonist -> reach end of Act 2 succeeds".
func bfsFrom(g *story.Graph, startID string) map[string]bool {
	visited := map[string]bool{}
	queue := []string{startID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		node, err := g.Lookup(cur)
		if err != nil {
			continue
		}
		for _, c := range node.Choices {
			if c.NextNodeID != "" && !visited[c.NextNodeID] {
				queue = append(queue, c.NextNodeID)
			}
		}
	}
	return visited
}

// allKnownNodes returns every authored node in the worldpack
// for iteration by the trigger-event cross-check. Phase 38.B/C
// enumerates ~43 nodes; the loader has no public Graph.Nodes()
// yet, so we list by id.
//
// Adding new nodes to worldpacks/frontier/nodes.yaml
// REQUIRES adding the id here; the cross-event-check will
// otherwise miss trigger_event effects in the new node.
func allKnownNodes(loaded *Loaded) []story.StoryNode {
	if loaded == nil || loaded.Graph == nil {
		return nil
	}
	out := make([]story.StoryNode, 0)
	ids := []string{
		// Phase 38.A nodes
		"act1.ashwick_entrance", "act1.wait_at_square",
		"act1.greyhall_keep", "act1.eastern_ridge",
		"act1.keeper_interview", "act1.void_dragon_reveal",
		"act1.ally_appears_intro",
		"kael.fringe_searching", "lyra.runesmith_workshop",
		"raven.disgraced_inn", "aria.dawn_temple",
		"kael.scholars_final_chapter", "lyra.runic_trial",
		"raven.heirs_judgment", "aria.dawns_first_prayer",
		// Phase 38.B/C supplement nodes
		"act1.dragon_relic_in_vault", "act1.act1_close_ridge",
		"act1.mountain_path_begins", "act1.aldric_arrival",
		// Phase 38.B/C Act 2 scenes
		"act2.act2_entry", "act2.mountain_crossing",
		"act2.selenes_tower", "act2.selenes_initiation",
		"act2.selenes_farewell", "act2.pathway_a",
		"act2.orions_camp", "act2.orions_challenge",
		"act2.orions_farewell", "act2.pathway_b",
		"act2.maren_library", "act2.maren_first_question",
		"act2.maren_farewell", "act2.aldric_keep",
		"act2.aldric_farewell", "act2.harlan_roadside",
		"act2.harlan_warning", "act2.harlan_farewell",
		"act2.act2_closeup", "act2.act3_handoff",
		// Phase 38.B/C protagonist detours
		"act2.kael_letter", "act2.lyra_runic_slate",
		"act2.raven_brother", "act2.aria_pilgrim_book",
	}
	for _, id := range ids {
		n, err := loaded.Graph.Lookup(id)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}
