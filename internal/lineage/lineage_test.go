package lineage

import (
	"strings"
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// makeWorld returns a minimal world with the deceased and a small
// set of candidates. Used to test the scoring algorithm with
// controlled inputs.
func makeWorld(t *testing.T, deceasedID string) *core.World {
	t.Helper()
	now := time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC)
	w := core.NewWorld("test", 1, now)
	// Three locations: blackwater (where most candidates live),
	// millbrook (nearby), and the deceased's location.
	w.AddLocation(core.NewLocation("blackwater", "Blackwater", "Free Marches", 100))
	w.AddLocation(core.NewLocation("millbrook", "Millbrook", "Free Marches", 20))
	w.AddLocation(core.NewLocation("ghost_town", "Ghost Town", "Free Marches", 10))
	return w
}

func TestScoreSuccessors_TopCandidateIsSpouse(t *testing.T) {
	w := makeWorld(t, "p1")
	// Deceased: 50-year-old merchant at blackwater.
	deceased := &core.Person{
		ID:         "p1",
		Name:       "Marcus Stone",
		Gender:     "M",
		BirthTick:  -50 * 365,
		Alive:      false,
		DeathTick:  0,
		LocationID: "blackwater",
		Class:      "Middle",
		Occupation: "merchant",
	}
	w.People["p1"] = deceased
	// Spouse: 48-year-old at blackwater, married to p1.
	spouse := &core.Person{
		ID:         "p2",
		Name:       "Elena Stone",
		Gender:     "F",
		BirthTick:  -48 * 365,
		Alive:      true,
		LocationID: "blackwater",
		Occupation: "innkeeper",
		SpouseID:   "p1",
	}
	w.People["p2"] = spouse
	// Daughter: 25-year-old at millbrook, child of p1 and p2.
	daughter := &core.Person{
		ID:         "p3",
		Name:       "Amelia Stone",
		Gender:     "F",
		BirthTick:  -25 * 365,
		Alive:      true,
		LocationID: "millbrook",
		Occupation: "merchant",
		FatherID:   "p1",
		MotherID:   "p2",
	}
	w.People["p3"] = daughter
	// A random merchant: 30 years old, at blackwater, no relation.
	other := &core.Person{
		ID:         "p4",
		Name:       "Bert Black",
		Gender:     "M",
		BirthTick:  -30 * 365,
		Alive:      true,
		LocationID: "blackwater",
		Occupation: "merchant",
	}
	w.People["p4"] = other
	// Realistic: a 20-year marriage has high trust. Without
	// this, the daughter (Family 90 + Age 100 + Proximity 60
	// + Inheritance 100) edges out the spouse (Family 100 +
	// Age 80 + Proximity 100) by ~1.5 points. The trust
	// axis is what makes the spouse the realistic winner.
	w.Relationships = append(w.Relationships, core.Relationship{
		FromID:     "p1",
		ToID:       "p2",
		Trust:      80,
		Respect:    75,
		Loyalty:    85,
		Attraction: 70,
	})

	cands := ScoreSuccessors(w, "p1", 5)
	if len(cands) < 2 {
		t.Fatalf("got %d candidates, want at least 2", len(cands))
	}
	// The spouse should top the list (high trust + family 100).
	if cands[0].Person.ID != "p2" {
		t.Errorf("top candidate = %q, want p2 (spouse)", cands[0].Person.ID)
	}
	// The daughter should be second (firstborn + prime age).
	if cands[1].Person.ID != "p3" {
		t.Errorf("second candidate = %q, want p3 (daughter)", cands[1].Person.ID)
	}
}

func TestScoreSuccessors_FirstbornBonus(t *testing.T) {
	w := makeWorld(t, "p1")
	deceased := &core.Person{
		ID:         "p1",
		Name:       "Marcus",
		Gender:     "M",
		BirthTick:  -50 * 365,
		Alive:      false,
		LocationID: "blackwater",
		Occupation: "merchant",
	}
	w.People["p1"] = deceased
	// Spouse
	w.People["p2"] = &core.Person{
		ID: "p2", Name: "Elena", Gender: "F", BirthTick: -48 * 365,
		Alive: true, LocationID: "blackwater", SpouseID: "p1",
	}
	// Two children with the same family depth (1) and proximity
	// but different IDs. Firstborn bonus should give p3 (the
	// lower ID) the inheritance edge.
	w.People["p3"] = &core.Person{
		ID: "p3", Name: "Amelia", Gender: "F", BirthTick: -25 * 365,
		Alive: true, LocationID: "blackwater", FatherID: "p1", MotherID: "p2",
		Occupation: "merchant",
	}
	w.People["p4"] = &core.Person{
		ID: "p4", Name: "Bert", Gender: "M", BirthTick: -22 * 365,
		Alive: true, LocationID: "blackwater", FatherID: "p1", MotherID: "p2",
		Occupation: "farmer",
	}
	cands := ScoreSuccessors(w, "p1", 5)
	// p3 (Amelia, firstborn by lower ID) should be top.
	if cands[0].Person.ID != "p3" {
		t.Errorf("top = %q, want p3 (firstborn by lower ID)", cands[0].Person.ID)
	}
	// Firstborn bonus should be visible in the breakdown.
	if cands[0].Breakdown.Inheritance != 100 {
		t.Errorf("firstborn Inheritance = %v, want 100", cands[0].Breakdown.Inheritance)
	}
}

func TestScoreSuccessors_AdultsOnly(t *testing.T) {
	w := makeWorld(t, "p1")
	deceased := &core.Person{
		ID:         "p1",
		Name:       "Marcus",
		Gender:     "M",
		BirthTick:  -50 * 365,
		Alive:      false,
		LocationID: "blackwater",
		Occupation: "merchant",
	}
	w.People["p1"] = deceased
	// Child of p1: 5 years old — must NOT appear in candidates.
	w.People["p2"] = &core.Person{
		ID: "p2", Name: "Kid", Gender: "M", BirthTick: -5 * 365,
		Alive: true, LocationID: "blackwater", FatherID: "p1",
	}
	cands := ScoreSuccessors(w, "p1", 5)
	if len(cands) != 0 {
		t.Errorf("got %d candidates (a 5-year-old child), want 0", len(cands))
	}
}

func TestScoreSuccessors_ExcludesDead(t *testing.T) {
	w := makeWorld(t, "p1")
	deceased := &core.Person{
		ID:         "p1",
		Name:       "Marcus",
		Gender:     "M",
		BirthTick:  -50 * 365,
		Alive:      false,
		LocationID: "blackwater",
	}
	w.People["p1"] = deceased
	// A dead spouse — must NOT appear.
	w.People["p2"] = &core.Person{
		ID: "p2", Name: "Ghost", Gender: "F", BirthTick: -48 * 365,
		Alive: false, DeathTick: -10 * 365, LocationID: "blackwater",
		SpouseID: "p1",
	}
	cands := ScoreSuccessors(w, "p1", 5)
	if len(cands) != 0 {
		t.Errorf("got %d candidates, want 0 (only candidate is dead)", len(cands))
	}
}

func TestScoreSuccessors_ExcludesDeceased(t *testing.T) {
	w := makeWorld(t, "p1")
	w.People["p1"] = &core.Person{
		ID: "p1", Name: "Self", Gender: "M", BirthTick: -50 * 365,
		Alive: false, LocationID: "blackwater",
	}
	cands := ScoreSuccessors(w, "p1", 5)
	for _, c := range cands {
		if c.Person.ID == "p1" {
			t.Errorf("deceased %q appears in successor list", "p1")
		}
	}
}

func TestScoreSuccessors_ProximityBoost(t *testing.T) {
	w := makeWorld(t, "p1")
	w.People["p1"] = &core.Person{
		ID: "p1", Name: "Marcus", Gender: "M", BirthTick: -50 * 365,
		Alive: false, LocationID: "blackwater",
	}
	// Same region, different location.
	w.People["p2"] = &core.Person{
		ID: "p2", Name: "Nearby", Gender: "F", BirthTick: -30 * 365,
		Alive: true, LocationID: "millbrook", SpouseID: "p1",
	}
	// Far away (different region would require a different
	// location setup; we'll just use ghost_town which is in
	// the same region too, so the test is "same region vs
	// same location" rather than "different region".)
	cands := ScoreSuccessors(w, "p1", 5)
	if len(cands) != 1 {
		t.Fatalf("got %d cands, want 1", len(cands))
	}
	// Proximity for same region but different location = 60.
	if cands[0].Breakdown.Proximity != 60 {
		t.Errorf("Proximity = %v, want 60 (same region, different location)", cands[0].Breakdown.Proximity)
	}
}

func TestScoreSuccessors_StableTieBreak(t *testing.T) {
	w := makeWorld(t, "p1")
	w.People["p1"] = &core.Person{
		ID: "p1", Name: "Marcus", Gender: "M", BirthTick: -50 * 365,
		Alive: false, LocationID: "blackwater",
	}
	// Two identical spouses? No — monogamy. But two candidates
	// with zero score on all axes: an unrelated adult stranger
	// should fall back to ID order. Use a candidate with no
	// relationship, family, memory, or proximity bonus.
	w.People["p2"] = &core.Person{
		ID: "p2", Name: "Stranger 2", Gender: "F", BirthTick: -30 * 365,
		Alive: true, LocationID: "ghost_town",
	}
	w.People["p3"] = &core.Person{
		ID: "p3", Name: "Stranger 3", Gender: "F", BirthTick: -30 * 365,
		Alive: true, LocationID: "ghost_town",
	}
	cands := ScoreSuccessors(w, "p1", 5)
	if len(cands) != 2 {
		t.Fatalf("got %d cands, want 2", len(cands))
	}
	// Lower ID first.
	if cands[0].Person.ID != "p2" {
		t.Errorf("first = %q, want p2 (lower ID)", cands[0].Person.ID)
	}
}

func TestPickSuccessor_Modes(t *testing.T) {
	// Build a candidate list with a clear heir (spouse, highest
	// family score) and a non-family top scorer.
	cands := []Successor{
		{Person: &core.Person{ID: "p2", Name: "Elena"}, Score: 80, Breakdown: ScoreBreakdown{Family: 100, Age: 80, Proximity: 100, Relationship: 60}},
		{Person: &core.Person{ID: "p3", Name: "Stranger"}, Score: 50, Breakdown: ScoreBreakdown{Age: 80, Proximity: 100}},
	}
	if p := PickSuccessor(cands, ModeHeir); p == nil || p.ID != "p2" {
		t.Errorf("ModeHeir = %v, want p2", p)
	}
	if p := PickSuccessor(cands, ModeCharacter); p == nil || p.ID != "p2" {
		t.Errorf("ModeCharacter = %v, want p2 (top scorer)", p)
	}
	if p := PickSuccessor(cands, ModeFamily); p == nil || p.ID != "p2" {
		t.Errorf("ModeFamily = %v, want p2 (highest family score)", p)
	}
	if p := PickSuccessor(cands, ModeObserver); p != nil {
		t.Errorf("ModeObserver = %v, want nil", p)
	}
	if p := PickSuccessor(cands, ModeEndBloodline); p != nil {
		t.Errorf("ModeEndBloodline = %v, want nil", p)
	}
}

func TestPickSuccessor_FamilyFallsThroughToCharacter(t *testing.T) {
	// No family candidates: ModeFamily should return nil so
	// the caller can fall through to ModeCharacter.
	cands := []Successor{
		{Person: &core.Person{ID: "p3", Name: "Stranger"}, Score: 50, Breakdown: ScoreBreakdown{Age: 80}},
	}
	if p := PickSuccessor(cands, ModeFamily); p != nil {
		t.Errorf("ModeFamily (no family) = %v, want nil", p)
	}
}

func TestComputeLegacy_EmptyRecord(t *testing.T) {
	w := makeWorld(t, "p1")
	w.People["p1"] = &core.Person{
		ID: "p1", Name: "Loner", Gender: "M", BirthTick: -30 * 365,
		Alive: false, DeathTick: 0, LocationID: "blackwater",
	}
	l := ComputeLegacy(w, "p1")
	if l == nil {
		t.Fatal("ComputeLegacy returned nil for known person")
	}
	if l.Name != "Loner" {
		t.Errorf("Name = %q, want 'Loner'", l.Name)
	}
	if l.AgeAtDeath != 30 {
		t.Errorf("AgeAtDeath = %d, want 30", l.AgeAtDeath)
	}
	if l.ChildCount != 0 {
		t.Errorf("ChildCount = %d, want 0", l.ChildCount)
	}
	if l.Reputation != 0 {
		t.Errorf("Reputation = %v, want 0", l.Reputation)
	}
	if l.LegacyScore < 30 {
		t.Errorf("LegacyScore = %v, want >= 30 (age baseline)", l.LegacyScore)
	}
}

func TestComputeLegacy_ChildrenAndReputation(t *testing.T) {
	w := makeWorld(t, "p1")
	w.People["p1"] = &core.Person{
		ID: "p1", Name: "Marcus", Gender: "M", BirthTick: -50 * 365,
		Alive: false, DeathTick: 0, LocationID: "blackwater",
		Occupation: "merchant", SpouseID: "p2",
	}
	w.People["p2"] = &core.Person{
		ID: "p2", Name: "Elena", Gender: "F", BirthTick: -48 * 365,
		Alive: true, LocationID: "blackwater", SpouseID: "p1",
	}
	// 2 children, 1 grandchild
	w.People["p3"] = &core.Person{
		ID: "p3", Name: "Amelia", Gender: "F", BirthTick: -25 * 365,
		Alive: true, FatherID: "p1", MotherID: "p2",
	}
	w.People["p4"] = &core.Person{
		ID: "p4", Name: "Bert", Gender: "M", BirthTick: -22 * 365,
		Alive: true, FatherID: "p1", MotherID: "p2",
	}
	w.People["p5"] = &core.Person{
		ID: "p5", Name: "Cara", Gender: "F", BirthTick: -5 * 365,
		Alive: true, FatherID: "p3",
	}
	// Trust relationship p1→p2
	w.Relationships = append(w.Relationships, core.Relationship{
		FromID: "p1", ToID: "p2", Trust: 90, Respect: 85, Loyalty: 95, Attraction: 80,
	})
	// Significant memory
	w.Memories = append(w.Memories, core.Memory{
		OwnerID: "p1", Description: "Founded Blackwater Trading Guild",
		Importance: 0.9, TrustDelta: 5.0,
	})
	l := ComputeLegacy(w, "p1")
	if l == nil {
		t.Fatal("ComputeLegacy returned nil")
	}
	if l.ChildCount != 2 {
		t.Errorf("ChildCount = %d, want 2", l.ChildCount)
	}
	if l.DescendantCount != 3 {
		t.Errorf("DescendantCount = %d, want 3 (2 children + 1 grandchild)", l.DescendantCount)
	}
	if l.SpouseName != "Elena" {
		t.Errorf("SpouseName = %q, want 'Elena'", l.SpouseName)
	}
	if len(l.Achievements) != 1 {
		t.Errorf("Achievements len = %d, want 1", len(l.Achievements))
	}
	if l.Reputation < 80 || l.Reputation > 95 {
		t.Errorf("Reputation = %v, want 80-95 range", l.Reputation)
	}
	if l.LegacyScore < 200 {
		t.Errorf("LegacyScore = %v, want >= 200 (50 age + 100 children + 25 grandchild + 5 achievement + 87 reputation)", l.LegacyScore)
	}
}

func TestComputeLegacy_UnknownPerson(t *testing.T) {
	w := makeWorld(t, "p1")
	l := ComputeLegacy(w, "does-not-exist")
	if l != nil {
		t.Errorf("ComputeLegacy(unknown) = %+v, want nil", l)
	}
}

func TestRenderDeathMessage_IncludesSuccessor(t *testing.T) {
	w := makeWorld(t, "p1")
	w.Locations["blackwater"].Population = 143
	w.People["p1"] = &core.Person{
		ID: "p1", Name: "Supun Hewagamage", Gender: "M", BirthTick: -84 * 365,
		Alive: false, DeathTick: 0, LocationID: "blackwater",
	}
	successor := &core.Person{
		ID: "p2", Name: "Amelia Hewagamage", Gender: "F", BirthTick: -27 * 365,
		Alive: true, LocationID: "blackwater", Occupation: "Merchant", FatherID: "p1",
	}
	cands := []Successor{
		{Person: successor, Score: 80, Breakdown: ScoreBreakdown{Family: 90, Age: 100}},
	}
	msg := RenderDeathMessage(w, "p1", successor, cands)
	for _, want := range []string{
		"Supun Hewagamage",
		"age 84",
		"Funeral attendees: 143",
		"Amelia Hewagamage",
		"Daughter",
		"Press Enter",
		"successors",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("death message missing %q\n---MSG---\n%s", want, msg)
		}
	}
}

func TestRenderSuccessorsList_Top5(t *testing.T) {
	cands := []Successor{
		{Person: &core.Person{ID: "p2", Name: "Elena", BirthTick: -48 * 365, Occupation: "innkeeper", LocationID: "blackwater"}, Score: 80, Breakdown: ScoreBreakdown{Family: 100}},
		{Person: &core.Person{ID: "p3", Name: "Amelia", BirthTick: -25 * 365, Occupation: "merchant", LocationID: "millbrook"}, Score: 70, Breakdown: ScoreBreakdown{Family: 90}},
	}
	out := RenderSuccessorsList(cands)
	for _, want := range []string{"Elena", "Amelia", "score 80", "score 70", "innkeeper", "merchant"} {
		if !strings.Contains(out, want) {
			t.Errorf("successors list missing %q\n---LIST---\n%s", want, out)
		}
	}
}

func TestRenderLegacyRecord(t *testing.T) {
	l := &Legacy{
		PlayerID:    "p1",
		Name:        "Supun Hewagamage",
		BornDate:    time.Date(1427, 1, 1, 0, 0, 0, 0, time.UTC),
		DiedDate:    time.Date(1511, 1, 1, 0, 0, 0, 0, time.UTC),
		AgeAtDeath:  84,
		SpouseName:  "Elena",
		ChildCount:  4,
		Reputation:  72,
		LegacyScore: 712,
		Achievements: []string{"Founded Blackwater Trading Guild", "Started the Grain Riots"},
	}
	out := RenderLegacyRecord(l)
	for _, want := range []string{
		"SUPUN HEWAGAMAGE",
		"Born: 1427",
		"Died: 1511",
		"Achievements:",
		"Married Elena",
		"4 Children",
		"Respected",
		"Legacy Score: 712",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("legacy record missing %q\n---OUT---\n%s", want, out)
		}
	}
}

func TestReputationLabel(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{95, "Beloved"},
		{75, "Respected"},
		{55, "Known"},
		{40, "Middling"},
		{20, "Disliked"},
		{0, "Unknown"},
	}
	for _, c := range cases {
		if got := reputationLabel(c.score); got != c.want {
			t.Errorf("reputationLabel(%v) = %q, want %q", c.score, got, c.want)
		}
	}
}

func TestContinuationMode_IsValid(t *testing.T) {
	for _, m := range AllModes() {
		if !m.IsValid() {
			t.Errorf("mode %q should be valid", m)
		}
	}
	if ContinuationMode("bogus").IsValid() {
		t.Error("bogus mode should be invalid")
	}
}
