package worldpack

import (
	"path/filepath"
	"testing"
)

// TestLoad_Frontier loads the real frontier pack and checks that
// every file is parsed and every list has the expected length.
func TestLoad_Frontier(t *testing.T) {
	// worldpacks/frontier is at <repo>/worldpacks/frontier. Tests run
	// from <repo>/internal/worldpack, so go up two levels.
	dir := filepath.Join("..", "..", "worldpacks", "frontier")

	_, pack, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(frontier): %v", err)
	}

	// 7 locations: Blackwater + 4 villages + monastery + fort
	if got, want := len(pack.Locations), 7; got != want {
		t.Errorf("Locations: got %d, want %d", got, want)
	}

	// 4 factions
	if got, want := len(pack.Factions), 4; got != want {
		t.Errorf("Factions: got %d, want %d", got, want)
	}
	wantFactions := map[string]bool{
		"merchant_guild": false, "town_council": false,
		"faith_of_dawn": false, "frontier_league": false,
	}
	for _, f := range pack.Factions {
		if _, ok := wantFactions[f.ID]; ok {
			wantFactions[f.ID] = true
		}
	}
	for id, found := range wantFactions {
		if !found {
			t.Errorf("missing faction %q", id)
		}
	}

	// 18 occupations (Phase 22: added woodcutter, miner, weaver
	// to the 15 from the v1 pack).
	if got, want := len(pack.Occupations), 18; got != want {
		t.Errorf("Occupations: got %d, want %d", got, want)
	}

	// Action rules
	if got := len(pack.ActionRules); got < 10 {
		t.Errorf("ActionRules: got %d, want at least 10", got)
	}

	// Generation
	if got, want := pack.Generation.Population.Total, 150; got != want {
		t.Errorf("Generation.Population.Total: got %d, want %d", got, want)
	}

	// Blackwater is the town with cap 80
	var blackwater *LocationSpec
	for i := range pack.Locations {
		if pack.Locations[i].ID == "blackwater" {
			blackwater = &pack.Locations[i]
			break
		}
	}
	if blackwater == nil {
		t.Fatal("blackwater location not found")
	}
	if blackwater.PopulationCap != 80 {
		t.Errorf("blackwater.PopulationCap: got %d, want 80", blackwater.PopulationCap)
	}
}

// TestLoad_MissingDir returns an error when the directory does not exist.
func TestLoad_MissingDir(t *testing.T) {
	_, _, err := Load("does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}
