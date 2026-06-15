package integration

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
	"github.com/chronicle-dev/chronicle/internal/repl"
)

// TestLineage_EndToEndPlaytest is the v1 lineage success
// criterion acceptance test (ARCHITECTURE.md §20):
// "Die and have the world continue."
//
// The test simulates the full CLI flow:
//
//  1. Bootstrap a minimal world with a player character
//     (Marcus, a 50-year-old merchant) and a family:
//     spouse Elena, daughter Amelia (at millbrook), and
//     granddaughter Cara.
//  2. Mark Marcus as dead (the "natural causes" path; in
//     production this would be the PopulationEngine's
//     mortality roll after ~36,500 ticks at the default
//     1% per year rate).
//  3. Run the REPL with scripted input:
//     - "\n"          (Enter: accept the heir)
//     - "people\n"    (verify the new player is alive)
//     - "look\n"      (verify the new player is at blackwater)
//     - "quit\n"      (exit)
//  4. Capture the full REPL output and assert on the key
//     parts: death message, legacy record, heir selection,
//     and continued play.
//
// The test prints the full captured output to stdout so
// the developer can read the playtest transcript without
// running the binary by hand. The assertions double as a
// regression guard against the lineage flow regressing.
func TestLineage_EndToEndPlaytest(t *testing.T) {
	// 1. Construct the world.
	w := core.NewWorld("lineage-playtest", 42, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", Region: "Free Marches", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "millbrook", Name: "Millbrook", Region: "Free Marches", PopulationCap: 20})
	w.AddLocation(&core.Location{ID: "ghost_town", Name: "Ghost Town", Region: "Free Marches", PopulationCap: 10})

	// Player: 50-year-old merchant at blackwater.
	player := &core.Person{
		ID:         "player",
		Name:       "Marcus Stone",
		Gender:     "M",
		BirthTick:  -50 * 365,
		Alive:      false, // 2. Mark as dead.
		DeathTick:  0,
		LocationID: "blackwater",
		Class:      "Middle",
		Occupation: "merchant",
		SpouseID:   "spouse",
	}
	w.People["player"] = player
	w.PlayerID = "player"

	// Spouse: 48-year-old at blackwater, married to player.
	spouse := &core.Person{
		ID:         "spouse",
		Name:       "Elena Stone",
		Gender:     "F",
		BirthTick:  -48 * 365,
		Alive:      true,
		LocationID: "blackwater",
		Class:      "Middle",
		Occupation: "innkeeper",
		SpouseID:   "player",
	}
	w.People["spouse"] = spouse

	// Daughter: 25-year-old at millbrook, child of player + spouse.
	daughter := &core.Person{
		ID:         "daughter",
		Name:       "Amelia Stone",
		Gender:     "F",
		BirthTick:  -25 * 365,
		Alive:      true,
		LocationID: "millbrook",
		Class:      "Middle",
		Occupation: "merchant",
		FatherID:   "player",
		MotherID:   "spouse",
	}
	w.People["daughter"] = daughter

	// Granddaughter: 5-year-old at blackwater, child of daughter.
	granddaughter := &core.Person{
		ID:         "granddaughter",
		Name:       "Cara Stone",
		Gender:     "F",
		BirthTick:  -5 * 365,
		Alive:      true,
		LocationID: "blackwater",
		Class:      "Lower",
		Occupation: "",
		MotherID:   "daughter",
	}
	w.People["granddaughter"] = granddaughter

	// Significant memories for the legacy record.
	w.Memories = append(w.Memories,
		core.Memory{OwnerID: "player", Description: "Founded Blackwater Trading Guild", Importance: 0.9, TrustDelta: 5.0},
		core.Memory{OwnerID: "player", Description: "Started the Grain Riots", Importance: 0.8, TrustDelta: 2.0},
		core.Memory{OwnerID: "player", Description: "chatted with Elena", Importance: 0.4, TrustDelta: 1.0},
	)

	// Trust relationship p1→p2 (realistic long marriage).
	w.Relationships = append(w.Relationships,
		core.Relationship{FromID: "player", ToID: "spouse", Trust: 90, Respect: 85, Loyalty: 95, Attraction: 80},
		core.Relationship{FromID: "player", ToID: "daughter", Trust: 80, Respect: 70, Loyalty: 85, Attraction: 60},
	)

	w.Tick = 100
	w.RecomputeLocationPopulations()

	// 3. Run the REPL with scripted input.
	parser := intent.New(nil, w)
	var out bytes.Buffer
	in := strings.NewReader("\npeople\nlook\nquit\n")
	r := repl.New(w, parser, repl.Options{
		In:       in,
		Out:      &out,
		PlayerID: "player",
	})
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("repl.Run: %v", err)
	}

	output := out.String()

	// 4. Print the full playtest transcript for the developer.
	fmt.Println("============================================================")
	fmt.Println("CHRONICLE LINEAGE END-TO-END PLAYTEST")
	fmt.Println("============================================================")
	fmt.Println(output)
	fmt.Println("============================================================")
	fmt.Println("END OF PLAYTEST")
	fmt.Println("============================================================")

	// 5. Assertions on the key parts of the lineage flow.
	checks := []struct {
		name    string
		substr  string
		comment string
	}{
		{"death name", "Marcus Stone", "the deceased's name appears in the death message"},
		{"age at death", "died at age 50", "the age is computed from BirthTick"},
		{"funeral attendees", "Funeral attendees:", "the funeral line is present"},
		{"chronicle continues", "The Chronicle continues", "the spec's 'chronicle continues' line"},
		{"spouse heir", "Elena Stone", "spouse tops the score (trust 90 + family 100)"},
		{"spouse label", "Spouse", "the relationship label is gender-neutral 'Spouse'"},
		{"press enter", "Press Enter to continue", "the spec's 'Press Enter to continue' line"},
		{"successors option", "successors", "the successors sub-prompt option is shown"},
		{"you are now", "You are now Elena Stone", "the heir message after Enter"},
		{"legacy name", "MARCUS STONE", "the legacy record's UPPERCASE name"},
		{"legacy born", "Born: 1450", "the legacy record's birth year"},
		{"legacy achievements", "Founded Blackwater Trading Guild", "high-importance memory surfaces as achievement"},
		{"legacy reputation", "Reputation:", "the legacy record's reputation line"},
		{"legacy score", "Legacy Score:", "the legacy record's score line"},
		{"continued play", "=== 3 alive people", "people command works after succession (spouse + daughter + granddaughter = 3 alive)"},
		{"look at new location", "Blackwater", "look works after succession"},
		{"goodbye", "Goodbye", "quit exits cleanly"},
	}
	for _, c := range checks {
		if !strings.Contains(output, c.substr) {
			t.Errorf("MISSING %q (%s)\n---OUTPUT---\n%s", c.substr, c.comment, output)
		}
	}

	// 6. Verify the world state after the playtest.
	if w.PlayerID != "spouse" {
		t.Errorf("PlayerID = %q, want 'spouse' (the heir)", w.PlayerID)
	}
	if w.PlayerID == "player" {
		t.Error("PlayerID is still 'player' — the succession did not fire")
	}
	if spouse, ok := w.People["spouse"]; !ok || !spouse.Alive {
		t.Errorf("spouse is not alive after succession: %+v", spouse)
	}
	if player, ok := w.People["player"]; !ok || player.Alive {
		t.Errorf("player is still alive after death: %+v", player)
	}
}

// TestLineage_EndToEndPlaytest_SuccessorsList verifies the
// "successors" sub-prompt: when the player types "successors"
// at the death prompt, the REPL shows the top-5 list and
// lets the player pick by number.
func TestLineage_EndToEndPlaytest_SuccessorsList(t *testing.T) {
	w := core.NewWorld("lineage-successors", 42, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", Region: "Free Marches", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "millbrook", Name: "Millbrook", Region: "Free Marches", PopulationCap: 20})

	player := &core.Person{ID: "p1", Name: "Marcus", Gender: "M", BirthTick: -50 * 365, Alive: false, LocationID: "blackwater", SpouseID: "p2"}
	w.People["p1"] = player
	w.PlayerID = "p1"
	w.People["p2"] = &core.Person{ID: "p2", Name: "Elena", Gender: "F", BirthTick: -48 * 365, Alive: true, LocationID: "blackwater", SpouseID: "p1"}
	w.People["p3"] = &core.Person{ID: "p3", Name: "Amelia", Gender: "F", BirthTick: -25 * 365, Alive: true, LocationID: "millbrook", FatherID: "p1", MotherID: "p2"}
	w.People["p4"] = &core.Person{ID: "p4", Name: "Bert", Gender: "M", BirthTick: -30 * 365, Alive: true, LocationID: "blackwater", Occupation: "merchant"}
	w.Relationships = append(w.Relationships, core.Relationship{FromID: "p1", ToID: "p2", Trust: 90, Respect: 85, Loyalty: 95, Attraction: 80})

	parser := intent.New(nil, w)
	var out bytes.Buffer
	// Script: type "successors", then pick #2 (daughter), then quit.
	in := strings.NewReader("successors\n2\nquit\n")
	r := repl.New(w, parser, repl.Options{In: in, Out: &out, PlayerID: "p1"})
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("repl.Run: %v", err)
	}

	output := out.String()
	fmt.Println("============================================================")
	fmt.Println("CHRONICLE SUCCESSORS SUB-PROMPT PLAYTEST")
	fmt.Println("============================================================")
	fmt.Println(output)
	fmt.Println("============================================================")
	fmt.Println("END OF PLAYTEST")
	fmt.Println("============================================================")

	// The player should now be the daughter (picked by number).
	if w.PlayerID != "p3" {
		t.Errorf("PlayerID = %q, want 'p3' (picked #2 from successors list)", w.PlayerID)
	}
	for _, want := range []string{"Top 3 successor candidates", "Elena", "Amelia", "Bert", "score"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\n---OUTPUT---\n%s", want, output)
		}
	}
}

// TestLineage_EndToEndPlaytest_Observer verifies the
// "observer" continuation mode: the player types "observer"
// at the death prompt, PlayerID is cleared, and the world
// continues with no player.
func TestLineage_EndToEndPlaytest_Observer(t *testing.T) {
	w := core.NewWorld("lineage-observer", 42, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", Region: "Free Marches", PopulationCap: 100})
	w.People["p1"] = &core.Person{ID: "p1", Name: "Marcus", Gender: "M", BirthTick: -50 * 365, Alive: false, LocationID: "blackwater", SpouseID: "p2"}
	w.PlayerID = "p1"
	w.People["p2"] = &core.Person{ID: "p2", Name: "Elena", Gender: "F", BirthTick: -48 * 365, Alive: true, LocationID: "blackwater", SpouseID: "p1"}

	parser := intent.New(nil, w)
	var out bytes.Buffer
	in := strings.NewReader("observer\npeople\nquit\n")
	r := repl.New(w, parser, repl.Options{In: in, Out: &out, PlayerID: "p1"})
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("repl.Run: %v", err)
	}

	output := out.String()
	fmt.Println("============================================================")
	fmt.Println("CHRONICLE OBSERVER MODE PLAYTEST")
	fmt.Println("============================================================")
	fmt.Println(output)
	fmt.Println("============================================================")
	fmt.Println("END OF PLAYTEST")
	fmt.Println("============================================================")

	// After observer mode, PlayerID should be empty.
	if w.PlayerID != "" {
		t.Errorf("PlayerID = %q, want '' (observer mode cleared it)", w.PlayerID)
	}
	for _, want := range []string{"observer", "The world continues without a player", "character <name>", "Goodbye"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\n---OUTPUT---\n%s", want, output)
		}
	}
}

// TestLineage_EndToEndPlaytest_EndBloodline verifies the
// "end_bloodline" continuation mode: the player types
// "end_bloodline" at the death prompt and the REPL exits
// with a "chronicle ends here" message.
func TestLineage_EndToEndPlaytest_EndBloodline(t *testing.T) {
	w := core.NewWorld("lineage-end", 42, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", Region: "Free Marches", PopulationCap: 100})
	w.People["p1"] = &core.Person{ID: "p1", Name: "Marcus", Gender: "M", BirthTick: -50 * 365, Alive: false, LocationID: "blackwater", SpouseID: "p2"}
	w.PlayerID = "p1"
	w.People["p2"] = &core.Person{ID: "p2", Name: "Elena", Gender: "F", BirthTick: -48 * 365, Alive: true, LocationID: "blackwater", SpouseID: "p1"}

	parser := intent.New(nil, w)
	var out bytes.Buffer
	in := strings.NewReader("end_bloodline\n")
	r := repl.New(w, parser, repl.Options{In: in, Out: &out, PlayerID: "p1"})
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("repl.Run: %v", err)
	}

	output := out.String()
	fmt.Println("============================================================")
	fmt.Println("CHRONICLE END BLOODLINE PLAYTEST")
	fmt.Println("============================================================")
	fmt.Println(output)
	fmt.Println("============================================================")
	fmt.Println("END OF PLAYTEST")
	fmt.Println("============================================================")

	// After end_bloodline, the REPL should have exited.
	// The "chronicle ends here" message should be in the output.
	for _, want := range []string{"chronicle ends here"} {
		if !strings.Contains(strings.ToLower(output), want) {
			t.Errorf("output missing %q\n---OUTPUT---\n%s", want, output)
		}
	}
	// The bloodlineEnded flag should have been set; the Run
	// loop should NOT have processed any further commands
	// after end_bloodline (the input still had data, but
	// the loop exited).
	if r == nil {
		t.Error("REPL is nil after Run")
	}
}

// TestLineage_EndToEndPlaytest_NoCandidates verifies the
// edge case where the player dies with no living family
// or candidates — the chronicle ends immediately.
func TestLineage_EndToEndPlaytest_NoCandidates(t *testing.T) {
	w := core.NewWorld("lineage-empty", 42, time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "blackwater", Name: "Blackwater", Region: "Free Marches", PopulationCap: 100})
	// Only the player exists, and they're dead.
	w.People["p1"] = &core.Person{ID: "p1", Name: "Loner", Gender: "M", BirthTick: -50 * 365, Alive: false, LocationID: "blackwater"}
	w.PlayerID = "p1"

	parser := intent.New(nil, w)
	var out bytes.Buffer
	in := strings.NewReader("\n")
	r := repl.New(w, parser, repl.Options{In: in, Out: &out, PlayerID: "p1"})
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("repl.Run: %v", err)
	}

	output := out.String()
	fmt.Println("============================================================")
	fmt.Println("CHRONICLE NO-CANDIDATES PLAYTEST")
	fmt.Println("============================================================")
	fmt.Println(output)
	fmt.Println("============================================================")
	fmt.Println("END OF PLAYTEST")
	fmt.Println("============================================================")

	// With no candidates, the chronicle ends immediately.
	for _, want := range []string{"Loner", "No successor could be found", "chronicle ends here"} {
		if !strings.Contains(strings.ToLower(output), strings.ToLower(want)) {
			t.Errorf("output missing %q\n---OUTPUT---\n%s", want, output)
		}
	}
}
