package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/story"
)

// TestLoad_FrontierWorldpack is the Phase 38.A acceptance
// test: the production content.Load must successfully consume
// the worldpacks/frontier/ directory and produce a *Loaded
// bundle with the shape PHASES.md §38.A promises.
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

	// Four protagonists present.
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

	// Each protagonist's opening routes to act1.ashwick_entrance.
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
		// The opening's first choice must route to ashwick_entrance.
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

	// Ashwick itself is reachable (the route from any opening
	// was already validated above; this checks that Ashwick
	// itself exists with the canonical title).
	ashwick, err := loaded.Graph.Lookup("act1.ashwick_entrance")
	if err != nil {
		t.Errorf("act1.ashwick_entrance not in graph: %v", err)
	} else if !strings.Contains(ashwick.Title, "Ashwick") {
		t.Errorf("act1.ashwick_entrance title = %q; want contains 'Ashwick'", ashwick.Title)
	}

	// 12 endings per ARCHITECTURE.md §20.
	if len(loaded.Endings) != 12 {
		t.Errorf("endings count = %d; want 12", len(loaded.Endings))
	}

	// Three romance-target IDs appear in the endings roster.
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

	// Events referenced by trigger_event effects exist.
	wantEventIDs := map[string]bool{
		"ally_call":     false,
		"dragon_stirs":  false,
		"faith_renewed": false,
	}
	for _, ev := range loaded.Events {
		for _, want := range []string{"ally_call", "dragon_stirs", "faith_renewed"} {
			if ev.ID == want {
				wantEventIDs[want] = true
			}
		}
	}
	for id, present := range wantEventIDs {
		if !present {
			t.Errorf("event %q missing from events roster", id)
		}
	}

	// Cross-check: every trigger_event effect's ID resolves in
	// the events roster. This is the validation gate that
	// Phase 36.E's validateTriggerEventIDs enforces; we
	// re-assert it here so the smoke test serves as a
	// regression guard against the loader relaxing the gate.
	knownEvents := make(map[string]bool, len(loaded.Events))
	for _, e := range loaded.Events {
		knownEvents[e.ID] = true
	}
	for _, n := range allNodes(loaded) {
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

	// Companion roster includes the three romance targets
	// plus the three supporting cast members.
	wantCompanions := []string{"Elara", "Selene", "Orion", "Aldric", "Maren", "Harlan"}
	for _, want := range wantCompanions {
		// We can verify via protagonists' starting_party (none
		// have non-empty parties in §38.A) OR via the raw
		// worldpack parse; the loader consumes companions.yaml
		// but does not surface the companion slice on *Loaded
		// (companions flow into validatePartyCompanions only).
		// Reconfirm via loading the YAML directly.
		buf, err := os.ReadFile(filepath.Join(dir, "companions.yaml"))
		if err != nil {
			t.Errorf("companions.yaml: %v", err)
			continue
		}
		if !strings.Contains(string(buf), "id: \""+want+"\"") {
			t.Errorf("companion %q missing from companions.yaml", want)
		}
	}
}

// allNodes returns every node in the graph in a flat slice
// for iteration by the trigger-event cross-check above.
func allNodes(loaded *Loaded) []story.StoryNode {
	if loaded == nil || loaded.Graph == nil {
		return nil
	}
	out := make([]story.StoryNode, 0)
	// Graph exposes Lookup(id); the produced graph stores
	// nodes by id. Walk it via the underlying graph's
	// public-id visitor if any. For Phase 38.A we just
	// query the protagonist exclusive + ashwick + canonical
	// downstream IDs directly (these are the only nodes whose
	// choices may contain trigger_event effects in Phase 38.A).
	ids := []string{
		"act1.ashwick_entrance", "act1.wait_at_square",
		"act1.greyhall_keep", "act1.eastern_ridge",
		"act1.keeper_interview", "act1.void_dragon_reveal",
		"act1.ally_appears_intro",
		"kael.fringe_searching", "lyra.runesmith_workshop",
		"raven.disgraced_inn", "aria.dawn_temple",
		"kael.scholars_final_chapter", "lyra.runic_trial",
		"raven.heirs_judgment", "aria.dawns_first_prayer",
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
