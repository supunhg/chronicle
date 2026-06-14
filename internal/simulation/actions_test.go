package simulation

import (
	"testing"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// makeWorld constructs a minimal world with one adult NPC at
// "village" for action-scoring tests. The NPC starts with the
// v1 needs initialized and empty goals. Callers can mutate
// Needs, Traits, and Goals to test specific scoring paths.
func makeWorld(seed int64, mutate func(*core.Person)) *core.World {
	w := core.NewWorld("test", seed, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "village", Name: "Village", PopulationCap: 100})
	w.AddLocation(&core.Location{ID: "city", Name: "City", PopulationCap: 100})
	p := &core.Person{
		ID:         "n0001",
		Name:       "Alice",
		Gender:     "F",
		BirthTick:  -20 * 365, // 20 years old
		Alive:      true,
		LocationID: "village",
		Needs:      make(map[string]int),
		Goals:      []core.Goal{},
	}
	for _, need := range []core.NeedID{
		core.NeedHunger, core.NeedWealth, core.NeedCompanionship,
		core.NeedSafety, core.NeedRest,
	} {
		p.Needs[string(need)] = 50
	}
	if mutate != nil {
		mutate(p)
	}
	w.AddPerson(p)
	return w
}

// TestSelectAction_HungryPrefersWork verifies that an NPC
// with very high hunger (0..100, lower = hungrier; here we
// test the high-hunger-need path via setting hunger=80 to
// mean "I am hungry — I want food") prefers WorkAction over
// SocializeAction.
//
// Per the scoring formula: WorkAction includes +hunger*0.4
// and SocializeAction doesn't read hunger. So a hungrier NPC
// should consistently pick WorkAction.
func TestSelectAction_HungryPrefersWork(t *testing.T) {
	w := makeWorld(42, func(p *core.Person) {
		p.Needs[string(core.NeedHunger)] = 90 // very hungry
		p.Needs[string(core.NeedWealth)] = 50
	})
	eng := NewGoalEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// Verify a work memory was created.
	found := false
	for _, m := range w.Memories {
		if m.OwnerID == "n0001" && containsTag(m.Tags, "work") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected hungry NPC to take WorkAction; got memories: %v", summarizeMemories(w.Memories))
	}
}

// TestSelectAction_LonelyPrefersSocialize verifies that an
// NPC with low companionship (high loneliness) prefers
// SocializeAction over WorkAction.
func TestSelectAction_LonelyPrefersSocialize(t *testing.T) {
	w := makeWorld(7, func(p *core.Person) {
		p.Needs[string(core.NeedCompanionship)] = 5 // very lonely
		p.Needs[string(core.NeedHunger)] = 50
		p.Needs[string(core.NeedWealth)] = 50
	})
	// Add a co-located NPC to socialize with.
	w.AddPerson(&core.Person{
		ID: "n0002", Name: "Bob", Gender: "M", BirthTick: -20 * 365,
		Alive: true, LocationID: "village",
		Needs: make(map[string]int),
	})
	eng := NewGoalEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	found := false
	for _, m := range w.Memories {
		if m.OwnerID == "n0001" && containsTag(m.Tags, "socialize") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected lonely NPC to take SocializeAction; got: %v", summarizeMemories(w.Memories))
	}
}

// TestSelectAction_AmbitiousBiasesTowardInfluence verifies
// that an NPC with the GainInfluence goal and a high
// ambitious trait scores TravelAction higher than RestAction
// when other needs are neutral.
//
// TravelAction: base 20 + adventurous*0.4 + ambitious*0.2
//   - hunger*0.2 + GainInfluence*10
//
// With ambitious=80, GainInfluence=0.6: 20 + 16 + 0 - 10
// + 6 = 32, plus small noise. RestAction: 20 + 0 + 0 + 0
// = 20. Travel wins.
func TestSelectAction_AmbitiousBiasesTowardInfluence(t *testing.T) {
	w := makeWorld(7, func(p *core.Person) {
		p.Traits = map[string]int{"ambitious": 80, "adventurous": 50}
		p.Goals = []core.Goal{
			{ID: core.GoalGainInfluence, Priority: 0.6, Progress: 0.0},
		}
		// Zero out hunger and wealth so WorkAction's
		// hunger*0.4 and wealth*0.5 terms don't dominate.
		// The NPC has plenty of food and coin; what it
		// wants is influence.
		p.Needs[string(core.NeedHunger)] = 0
		p.Needs[string(core.NeedWealth)] = 0
		p.Needs[string(core.NeedCompanionship)] = 50
		p.Needs[string(core.NeedSafety)] = 50
		p.Needs[string(core.NeedRest)] = 50
	})
	eng := NewGoalEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	found := false
	for _, m := range w.Memories {
		if m.OwnerID == "n0001" && containsTag(m.Tags, "travel") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ambitious NPC to take TravelAction; got: %v", summarizeMemories(w.Memories))
	}
}

// TestSelectAction_DeterministicSameSeed verifies that two
// worlds with the same seed, same needs, same traits, and
// same goals produce the same selected action for a given
// tick. Determinism is the core contract of the engine.
func TestSelectAction_DeterministicSameSeed(t *testing.T) {
	mk := func() *core.World {
		return makeWorld(1234, func(p *core.Person) {
			p.Traits = map[string]int{"ambitious": 60, "kind": 40, "lazy": 20}
			p.Goals = []core.Goal{
				{ID: core.GoalBecomeWealthy, Priority: 0.5},
			}
		})
	}
	w1 := mk()
	w2 := mk()
	eng1 := NewGoalEngine()
	eng2 := NewGoalEngine()
	sim1 := tick.NewSimulation(w1.Seed, eng1)
	sim2 := tick.NewSimulation(w2.Seed, eng2)
	if err := sim1.Init(w1); err != nil {
		t.Fatalf("Init1: %v", err)
	}
	if err := sim2.Init(w2); err != nil {
		t.Fatalf("Init2: %v", err)
	}
	if err := sim1.Tick(w1); err != nil {
		t.Fatalf("Tick1: %v", err)
	}
	if err := sim2.Tick(w2); err != nil {
		t.Fatalf("Tick2: %v", err)
	}
	// Compare the last memory (the action's memory) by tag
	// set — the action kind is encoded in the tags.
	if len(w1.Memories) == 0 || len(w2.Memories) == 0 {
		t.Fatalf("no memories in one of the worlds: w1=%d w2=%d", len(w1.Memories), len(w2.Memories))
	}
	m1 := w1.Memories[len(w1.Memories)-1]
	m2 := w2.Memories[len(w2.Memories)-1]
	if !sameTagSet(m1.Tags, m2.Tags) {
		t.Errorf("non-deterministic action selection: w1 last tags=%v w2 last tags=%v", m1.Tags, m2.Tags)
	}
}

// TestWorkAction_ReducesHunger verifies that executing
// WorkAction reduces the hunger need.
func TestWorkAction_ReducesHunger(t *testing.T) {
	w := makeWorld(1, nil)
	p := w.People["n0001"]
	p.Needs[string(core.NeedHunger)] = 80
	a := &WorkAction{}
	_ = a.Execute(p, w)
	if p.Needs[string(core.NeedHunger)] >= 80 {
		t.Errorf("hunger did not decrease after WorkAction.Execute; got %d", p.Needs[string(core.NeedHunger)])
	}
}

// TestSocializeAction_CreatesRelationship verifies that
// SocializeAction between two co-located NPCs creates a
// new relationship with a positive trust delta.
func TestSocializeAction_CreatesRelationship(t *testing.T) {
	w := makeWorld(1, nil)
	w.AddPerson(&core.Person{
		ID: "n0002", Name: "Bob", Gender: "M", BirthTick: -20 * 365,
		Alive: true, LocationID: "village",
		Needs: make(map[string]int),
	})
	a := &SocializeAction{}
	_ = a.Execute(w.People["n0001"], w)
	found := false
	for _, r := range w.Relationships {
		if r.FromID == "n0001" && r.ToID == "n0002" {
			found = true
			if r.Trust < 50 {
				t.Errorf("relationship trust = %f, want >= 50 (initial 50 + 2.0 delta)", r.Trust)
			}
		}
	}
	if !found {
		t.Errorf("SocializeAction did not create a relationship")
	}
}

// TestGoalEngine_DecaysAllNeeds verifies that Tick decays
// every need by DefaultNeedDecay. Uses a child NPC (BirthTick
// = 0, so AgeAt = 0) so action selection is skipped and only
// the decay path is exercised.
func TestGoalEngine_DecaysAllNeeds(t *testing.T) {
	w := core.NewWorld("test", 1, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))
	w.AddLocation(&core.Location{ID: "village", Name: "Village", PopulationCap: 100})
	// Child NPC: BirthTick=0 means AgeAt(any tick) <= 0,
	// so IsAdult=false and the action-selection loop is
	// skipped. Only the decay path runs.
	w.AddPerson(&core.Person{
		ID: "n0001", Name: "Child", Gender: "F",
		BirthTick: 0, Alive: true, LocationID: "village",
		Needs: make(map[string]int),
	})
	for _, need := range []core.NeedID{
		core.NeedHunger, core.NeedWealth, core.NeedCompanionship,
		core.NeedSafety, core.NeedRest,
	} {
		w.People["n0001"].Needs[string(need)] = core.DefaultNeedInitial
	}
	eng := NewGoalEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Capture initial values.
	initial := make(map[string]int)
	for k, v := range w.People["n0001"].Needs {
		initial[k] = v
	}
	// Run 3 ticks.
	for i := 0; i < 3; i++ {
		if err := sim.Tick(w); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	for need, startVal := range initial {
		expected := startVal - 3*core.DefaultNeedDecay
		if expected < 0 {
			expected = 0
		}
		if got := w.People["n0001"].Needs[need]; got != expected {
			t.Errorf("after 3 ticks, Needs[%q] = %d, want %d", need, got, expected)
		}
	}
}

// TestGoalEngine_InitAssignsDefaultGoals verifies that
// Init gives every adult the v1 default goals.
func TestGoalEngine_InitAssignsDefaultGoals(t *testing.T) {
	w := makeWorld(1, nil)
	// Add an adult to ensure the goal-defaulting path runs.
	w.AddPerson(&core.Person{
		ID: "n0002", Name: "Bob", Gender: "M", BirthTick: -20 * 365,
		Alive: true, LocationID: "village",
		Needs: make(map[string]int),
	})
	eng := NewGoalEngine()
	if err := eng.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, p := range w.LivingPeople() {
		if !p.IsAdult(w.Tick) {
			continue
		}
		if len(p.Goals) != 2 {
			t.Errorf("%s has %d goals, want 2 (BecomeWealthy + RaiseFamily)", p.ID, len(p.Goals))
		}
		hasWealthy := false
		hasFamily := false
		for _, g := range p.Goals {
			if g.ID == core.GoalBecomeWealthy {
				hasWealthy = true
			}
			if g.ID == core.GoalRaiseFamily {
				hasFamily = true
			}
		}
		if !hasWealthy || !hasFamily {
			t.Errorf("%s missing default goals: hasWealthy=%v hasFamily=%v", p.ID, hasWealthy, hasFamily)
		}
	}
}

// TestGoalEngine_AdvancesGoalProgress verifies that taking
// a work-related action bumps the BecomeWealthy goal's
// progress.
func TestGoalEngine_AdvancesGoalProgress(t *testing.T) {
	w := makeWorld(42, func(p *core.Person) {
		p.Goals = []core.Goal{
			{ID: core.GoalBecomeWealthy, Priority: 0.8, Progress: 0.0},
		}
		// Make work attractive.
		p.Needs[string(core.NeedHunger)] = 80
	})
	eng := NewGoalEngine()
	sim := tick.NewSimulation(w.Seed, eng)
	if err := sim.Init(w); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := sim.Tick(w); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	p := w.People["n0001"]
	if p.Goals[0].Progress <= 0 {
		t.Errorf("GoalBecomeWealthy.Progress = %f, want > 0 after a work tick", p.Goals[0].Progress)
	}
}

// TestMemoryBonus_PositiveSocialMemoryBoostsSocializeScore
// verifies the spec's "memory modifiers affect action
// scoring" requirement: a recent positive socialize memory
// in the owner's memory log boosts SocializeAction.Score.
func TestMemoryBonus_PositiveSocialMemoryBoostsSocializeScore(t *testing.T) {
	w := makeWorld(1, nil)
	p := w.People["n0001"]
	// No memories: baseline score.
	scoreWithout := (&SocializeAction{}).Score(p, w)
	// Pre-seed a positive socialize memory.
	w.Memories = append(w.Memories, core.Memory{
		ID:          "mem-prev-socialize",
		OwnerID:     p.ID,
		EventID:     "socialize-prev",
		Tick:        0,
		Importance:  0.6,
		TrustDelta:  5.0,
		Description: "had a great chat",
		Tags:        []string{"socialize", "chat"},
	})
	scoreWith := (&SocializeAction{}).Score(p, w)
	if scoreWith <= scoreWithout {
		t.Errorf("SocializeAction.Score with positive memory = %f, want > %f (without memory)", scoreWith, scoreWithout)
	}
}

// TestMemoryBonus_NegativeMemoryDoesNotBoost verifies that
// a memory with negative TrustDelta does not inflate the
// action's score (no free boost from bad experiences).
func TestMemoryBonus_NegativeMemoryDoesNotBoost(t *testing.T) {
	w := makeWorld(1, nil)
	p := w.People["n0001"]
	w.Memories = append(w.Memories, core.Memory{
		ID:          "mem-prev-socialize-bad",
		OwnerID:     p.ID,
		EventID:     "socialize-prev",
		Tick:        0,
		Importance:  0.2,
		TrustDelta:  -10.0,
		Description: "argument",
		Tags:        []string{"socialize"},
	})
	scoreWith := (&SocializeAction{}).Score(p, w)
	w.Memories = nil
	scoreWithout := (&SocializeAction{}).Score(p, w)
	if scoreWith > scoreWithout {
		t.Errorf("SocializeAction.Score with negative memory = %f, want <= %f (no boost from bad memories)", scoreWith, scoreWithout)
	}
}

// TestAction_AllSixKinds verifies that AllActions() returns
// exactly 6 candidate actions covering the v1 spec.
func TestAction_AllSixKinds(t *testing.T) {
	actions := AllActions()
	if len(actions) != 6 {
		t.Fatalf("len(AllActions()) = %d, want 6", len(actions))
	}
	want := map[ActionKind]bool{
		ActionKindWork: true, ActionKindSocialize: true,
		ActionKindRest: true, ActionKindTravel: true,
		ActionKindTrade: true, ActionKindCourt: true,
	}
	for _, a := range actions {
		if !want[a.Kind()] {
			t.Errorf("unexpected action kind: %s", a.Kind())
		}
		delete(want, a.Kind())
	}
	if len(want) > 0 {
		t.Errorf("missing action kinds: %v", want)
	}
}

// ============================================================================
// Helpers
// ============================================================================

// sameTagSet returns true if two tag slices contain the
// same elements (order-independent).
func sameTagSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, t := range a {
		m[t]++
	}
	for _, t := range b {
		m[t]--
		if m[t] < 0 {
			return false
		}
	}
	return true
}

// summarizeMemories returns a short string for the test
// failure message.
func summarizeMemories(mems []core.Memory) string {
	if len(mems) == 0 {
		return "<no memories>"
	}
	m := mems[len(mems)-1]
	return m.Description + " tags=" + joinTags(m.Tags)
}

func joinTags(tags []string) string {
	out := ""
	for i, t := range tags {
		if i > 0 {
			out += ","
		}
		out += t
	}
	return out
}
