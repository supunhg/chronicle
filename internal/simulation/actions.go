package simulation

import (
	"fmt"
	"math"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/tick"
)

// ActionKind is the canonical name of a candidate action. Used
// in memory records, logs, and test assertions. Per the Phase 21
// spec, we ship exactly 6 action kinds; more can be added in
// future phases by implementing the Action interface.
type ActionKind string

// The 6 v1 action kinds. See the spec in the project root.
const (
	ActionKindWork      ActionKind = "work"
	ActionKindSocialize ActionKind = "socialize"
	ActionKindRest      ActionKind = "rest"
	ActionKindTravel    ActionKind = "travel"
	ActionKindTrade     ActionKind = "trade"
	ActionKindCourt     ActionKind = "court"
)

// AllActionKinds is the deterministic iteration order used by
// the engine when generating candidates. Keep this list in sync
// with the constants above.
var AllActionKinds = []ActionKind{
	ActionKindWork, ActionKindSocialize, ActionKindRest,
	ActionKindTravel, ActionKindTrade, ActionKindCourt,
}

// Action is the interface every candidate action implements.
// The GoalEngine generates one of each per NPC per tick,
// scores them, picks the highest, and executes it.
//
// Score returns the utility (0..100 range, but the engine
// doesn't enforce a hard cap) of this action for the given
// person in the current world state. Higher = more attractive.
//
// Execute applies the action's effects to the world and
// returns the list of Memory records to append. The engine
// is responsible for appending; the action returns the list
// so unit tests can inspect what would happen without
// mutating the world.
type Action interface {
	// Kind returns the canonical name (e.g. ActionKindWork).
	Kind() ActionKind

	// Score returns the utility of this action for p in the
	// current world. Must be deterministic given the
	// (worldSeed, tick, personID) — use tick.EntityRand.
	Score(p *core.Person, w *core.World) float64

	// Execute applies the action to the world and returns the
	// resulting memories. May return an empty slice. The
	// engine appends to w.Memories.
	Execute(p *core.Person, w *core.World) []core.Memory
}

// AllActions returns the canonical 6 candidate actions in
// the same order as AllActionKinds. Phase 21 v1 ships a fixed
// set; future phases can add more by extending this slice.
func AllActions() []Action {
	return []Action{
		&WorkAction{},
		&SocializeAction{},
		&RestAction{},
		&TravelAction{},
		&TradeAction{},
		&CourtAction{},
	}
}

// needValue safely reads a need, returning 0 if the need or
// the map is nil. Used by the action scorers so a partially-
// initialized NPC doesn't crash scoring.
func needValue(p *core.Person, need core.NeedID) int {
	if p.Needs == nil {
		return 0
	}
	return p.Needs[string(need)]
}

// traitValue safely reads a trait, returning 0 if the trait
// or the map is nil.
func traitValue(p *core.Person, trait string) int {
	if p.Traits == nil {
		return 0
	}
	return p.Traits[trait]
}

// goalPriority returns the highest Priority among p's goals
// with the given ID, or 0 if absent.
func goalPriority(p *core.Person, id core.GoalID) float64 {
	if p.Goals == nil {
		return 0
	}
	for _, g := range p.Goals {
		if g.ID == id {
			return g.Priority
		}
	}
	return 0
}

// relationshipBonus returns a small positive bonus when the
// owner has an existing positive relationship with a
// co-located potential interaction target. Phase 21 v1 uses
// it for CourtAction (high-trust partners are more likely to
// reciprocate). Future phases can extend to SocializeAction.
// A nil w.Relationships is safe.
func relationshipBonus(p *core.Person, w *core.World, targetID string) float64 {
	if targetID == "" || w.Relationships == nil {
		return 0
	}
	for _, r := range w.Relationships {
		if r.FromID == p.ID && r.ToID == targetID {
			// Trust > 60 → reciprocal interest. Returns 0..15.
			if r.Trust > 60 {
				return (r.Trust - 60) * 0.5
			}
			return 0
		}
	}
	return 0
}

// bumpGoalProgress increases the Progress of the named goal
// by delta, clamped to [0, 1]. A goal with Priority=0 stays
// dormant. If the goal is absent, this is a no-op.
func bumpGoalProgress(p *core.Person, id core.GoalID, delta float64) {
	if p.Goals == nil {
		return
	}
	for i := range p.Goals {
		if p.Goals[i].ID == id {
			p.Goals[i].Progress = clamp01(p.Goals[i].Progress + delta)
			return
		}
	}
}

// clamp01 returns v clamped to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// baseScore is the floor added to every action's score so
// even unappealing actions have a non-zero utility (the
// engine still picks the highest, but no candidate is
// rejected outright unless its score is dominated).
const baseScore = 20.0

// noise returns a deterministic per-(tick, person) noise in
// [-NoiseAmplitude, +NoiseAmplitude] so action selection
// is not 100% deterministic when two actions have equal
// utility. The noise is small relative to score deltas, so
// the dominant action usually still wins; this just breaks
// ties in a reproducible way.
func noise(w *core.World, personID string, suffix string) float64 {
	const NoiseAmplitude = 3.0
	r := tick.EntityRand(w.Seed, w.Tick, personID+":"+suffix)
	return (r.Float64()*2 - 1) * NoiseAmplitude
}

// memoryBonus scans the owner's recent memories (last
// MemoryLookback entries) for a memory tagged with the given
// action kind and a positive TrustDelta or Importance. If
// found, returns a small bonus (0..15) proportional to the
// memory's positive signal. Used by SocializeAction and
// CourtAction so past pleasant experiences reinforce current
// behavior. A nil w.Memories is safe.
func memoryBonus(p *core.Person, w *core.World, kindTag string) float64 {
	const MemoryLookback = 10
	const MaxBonus = 15.0
	n := len(w.Memories)
	if n == 0 {
		return 0
	}
	// Walk the most recent MemoryLookback memories owned by p.
	lo := 0
	if n > MemoryLookback {
		lo = n - MemoryLookback
	}
	bonus := 0.0
	for _, m := range w.Memories[lo:] {
		if m.OwnerID != p.ID {
			continue
		}
		if !containsTag(m.Tags, kindTag) {
			continue
		}
		// Positive signal: trust delta (0..5) or importance
		// (0..1, scaled). We cap the per-memory contribution
		// to keep totals sane.
		signal := m.TrustDelta*0.5 + m.Importance*5
		if signal > 0 {
			bonus += signal
		}
	}
	if bonus > MaxBonus {
		bonus = MaxBonus
	}
	return bonus
}

// containsTag returns true if tags contains target. Duplicated
// from actions_test.go to keep this file's helpers local
// (avoids an _test.go import).
func containsTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

// reduceNeed lowers a need by delta, clamped at 0. No-op if
// the need map is nil.
func reduceNeed(p *core.Person, need core.NeedID, delta int) {
	if p.Needs == nil {
		return
	}
	key := string(need)
	v := p.Needs[key] - delta
	if v < 0 {
		v = 0
	}
	p.Needs[key] = v
}

// bumpRelationship applies a trust delta to the
// (from→to) relationship in w.Relationships, creating a new
// one if absent. The result is a new relationship with Trust
// clamped to [0, 100]. This mirrors the O(1) "memory-driven
// deltas" pattern in RelationshipEngine.ApplyMemoryDeltas
// (the action layer is intentionally a local reimplementation
// to keep the dependency direction simulation → core only).
func bumpRelationship(w *core.World, fromID, toID string, trustDelta float64) {
	if fromID == "" || toID == "" || fromID == toID {
		return
	}
	for i := range w.Relationships {
		r := &w.Relationships[i]
		if r.FromID == fromID && r.ToID == toID {
			r.Trust = clamp100(r.Trust + trustDelta)
			return
		}
	}
	w.Relationships = append(w.Relationships, core.Relationship{
		FromID: fromID,
		ToID:   toID,
		Trust:  clamp100(50 + trustDelta),
	})
}

// clamp100 returns v clamped to [0, 100].
func clamp100(v float64) float64 {
	return math.Max(0, math.Min(100, v))
}

// ============================================================================
// WorkAction
// ============================================================================

// WorkAction addresses hunger and wealth. The NPC produces
// food (reducing hunger need) and earns a small amount of
// coin (advancing BecomeWealthy goal). Advances the worker's
// own goals; in Phase 22+ this is the entry point for the
// production-loops economy engine.
type WorkAction struct{}

// Kind returns ActionKindWork.
func (a *WorkAction) Kind() ActionKind { return ActionKindWork }

// Score for WorkAction:
//
//	20 (base) + hunger*0.4 + wealth*0.5 + ambitious*0.2
//	+ GoalPriority(BecomeWealthy)*30
//	+ small noise
//
// Hungry NPCs prefer work (it produces food). Wealthy NPCs
// also prefer work (coin income). Ambitious NPCs get a small
// trait bonus. NPCs with the BecomeWealthy goal get a
// stronger boost.
func (a *WorkAction) Score(p *core.Person, w *core.World) float64 {
	hunger := needValue(p, core.NeedHunger)
	wealth := needValue(p, core.NeedWealth)
	ambitious := traitValue(p, "ambitious")
	wealthyGoal := goalPriority(p, core.GoalBecomeWealthy)
	guildGoal := goalPriority(p, core.GoalJoinGuild)
	// Guild membership ≈ skilled labor; working aligns with it.
	score := baseScore +
		float64(hunger)*0.4 +
		float64(wealth)*0.5 +
		float64(ambitious)*0.2 +
		wealthyGoal*30 +
		guildGoal*10
	return score + noise(w, p.ID, "work")
}

// Execute produces a small coin gain, reduces hunger, and
// (Phase 22) marks progress on the JoinGuild goal. Returns
// a work memory. Production itself is done by the
// EconomyEngine.Tick loop, not by this Execute — Execute is
// the player-facing action handler; the engine does the
// bulk daily production for every adult producer regardless
// of which action they pick.
func (a *WorkAction) Execute(p *core.Person, w *core.World) []core.Memory {
	reduceNeed(p, core.NeedHunger, 5)
	bumpGoalProgress(p, core.GoalBecomeWealthy, 0.01)
	// Phase 22: JoinGuild alignment. Working for a living is
	// the entry path into skilled-labor guilds, so each
	// work tick advances this goal a little.
	bumpGoalProgress(p, core.GoalJoinGuild, 0.005)
	return []core.Memory{
		{
			ID:             fmt.Sprintf("mem-work-%d-%s", w.Tick, p.ID),
			OwnerID:        p.ID,
			EventID:        fmt.Sprintf("work-%d-%s", w.Tick, p.ID),
			Tick:           w.Tick,
			Importance:     0.2,
			Recency:        1.0,
			EmotionalScore: 0.0,
			Description:    fmt.Sprintf("%s worked", p.Name),
			Tags:           []string{"work"},
		},
	}
}

// ============================================================================
// SocializeAction
// ============================================================================

// SocializeAction addresses companionship. The NPC spends
// time with co-located people, building bonds (creates
// small +trust memories) and reducing loneliness. The action
// is more attractive when companionship is low and when the
// NPC has a kind trait.
type SocializeAction struct{}

// Kind returns ActionKindSocialize.
func (a *SocializeAction) Kind() ActionKind { return ActionKindSocialize }

// Score for SocializeAction:
//
//	20 (base) + companionship*0.5 + kind*0.2
//	+ GoalPriority(RaiseFamily)*25
//	+ small noise
//
// Convention: all needs are "deficit" (higher = more urgent),
// so a high companionship NEED means "I want company" — i.e.
// I am lonely. Lonely NPCs strongly prefer socializing. Kind
// NPCs get a bonus. NPCs pursuing RaiseFamily get a
// goal-alignment boost.
func (a *SocializeAction) Score(p *core.Person, w *core.World) float64 {
	companionship := needValue(p, core.NeedCompanionship)
	kind := traitValue(p, "kind")
	goal := goalPriority(p, core.GoalRaiseFamily)
	score := baseScore +
		float64(companionship)*0.5 +
		float64(kind)*0.2 +
		goal*25 +
		memoryBonus(p, w, "socialize")
	return score + noise(w, p.ID, "socialize")
}

// Execute picks a random co-located person (deterministic
// per-tick) and creates a chat memory with a small trust
// delta. If no one is at the same location, the action is
// a no-op (just a "talked to no one" memory, no trust bump).
func (a *SocializeAction) Execute(p *core.Person, w *core.World) []core.Memory {
	targets := w.LivingPeopleAt(p.LocationID)
	var others []string
	for _, t := range targets {
		if t.ID != p.ID {
			others = append(others, t.ID)
		}
	}
	if len(others) == 0 {
		return []core.Memory{
			{
				ID:             fmt.Sprintf("mem-socialize-%d-%s-alone", w.Tick, p.ID),
				OwnerID:        p.ID,
				EventID:        fmt.Sprintf("socialize-%d-%s", w.Tick, p.ID),
				Tick:           w.Tick,
				Importance:     0.1,
				Recency:        1.0,
				EmotionalScore: 0.0,
				Description:    fmt.Sprintf("%s socialized alone", p.Name),
				Tags:           []string{"socialize"},
			},
		}
	}
	r := tick.EntityRand(w.Seed, w.Tick, p.ID+":socialize-target")
	targetID := others[r.Intn(len(others))]
	target := w.People[targetID]
	bumpRelationship(w, p.ID, targetID, 2.0)
	reduceNeed(p, core.NeedCompanionship, 8)
	bumpGoalProgress(p, core.GoalRaiseFamily, 0.02)
	actor := core.Memory{
		ID:             fmt.Sprintf("mem-socialize-%d-%s-%s", w.Tick, p.ID, targetID),
		OwnerID:        p.ID,
		EventID:        fmt.Sprintf("socialize-%d-%s-%s", w.Tick, p.ID, targetID),
		Tick:           w.Tick,
		Importance:     0.2,
		Recency:        1.0,
		EmotionalScore: 0.2,
		TrustDelta:     2.0,
		Description:    fmt.Sprintf("%s chatted with %s", p.Name, target.Name),
		Tags:           []string{"socialize", "chat"},
	}
	targetMem := core.Memory{
		ID:             fmt.Sprintf("mem-socialize-%d-%s-%s-rev", w.Tick, p.ID, targetID),
		OwnerID:        targetID,
		EventID:        fmt.Sprintf("socialize-%d-%s-%s", w.Tick, p.ID, targetID),
		Tick:           w.Tick,
		Importance:     0.2,
		Recency:        1.0,
		EmotionalScore: 0.2,
		TrustDelta:     2.0,
		Description:    fmt.Sprintf("%s chatted with %s", target.Name, p.Name),
		Tags:           []string{"socialize", "chat"},
	}
	return []core.Memory{actor, targetMem}
}

// ============================================================================
// RestAction
// ============================================================================

// RestAction addresses rest and safety. The NPC stays home,
// recharging. The action is more attractive when rest is
// low and the NPC is lazy (high lazy trait). Resting gives a
// small safety boost too.
type RestAction struct{}

// Kind returns ActionKindRest.
func (a *RestAction) Kind() ActionKind { return ActionKindRest }

// Score for RestAction:
//
//	20 (base) + rest*0.5 + lazy*0.3 + safety*0.2
//	+ small noise
//
// Convention: all needs are "deficit" (higher = more urgent),
// so a high rest NEED means "I need to rest" — i.e. I am
// tired. Tired NPCs strongly prefer rest. Lazy NPCs get a
// bonus. NPCs that feel unsafe also prefer to stay put.
func (a *RestAction) Score(p *core.Person, w *core.World) float64 {
	rest := needValue(p, core.NeedRest)
	lazy := traitValue(p, "lazy")
	safety := needValue(p, core.NeedSafety)
	score := baseScore +
		float64(rest)*0.5 +
		float64(lazy)*0.3 +
		float64(safety)*0.2
	return score + noise(w, p.ID, "rest")
}

// Execute restores rest and a small safety boost. Creates a
// quiet memory with low importance.
func (a *RestAction) Execute(p *core.Person, w *core.World) []core.Memory {
	reduceNeed(p, core.NeedRest, 30)
	reduceNeed(p, core.NeedSafety, 5)
	return []core.Memory{
		{
			ID:             fmt.Sprintf("mem-rest-%d-%s", w.Tick, p.ID),
			OwnerID:        p.ID,
			EventID:        fmt.Sprintf("rest-%d-%s", w.Tick, p.ID),
			Tick:           w.Tick,
			Importance:     0.05,
			Recency:        1.0,
			EmotionalScore: 0.0,
			Description:    fmt.Sprintf("%s rested", p.Name),
			Tags:           []string{"rest"},
		},
	}
}

// ============================================================================
// TravelAction
// ============================================================================

// TravelAction moves the NPC to a different (random) location.
// The action is more attractive for adventurous/ambitious
// NPCs and less attractive when hunger is high (travel is
// risky). In Phase 23 the Event Engine will start gating
// travel on safety; for Phase 21 the action is "wander to a
// random new location" with no risk roll.
type TravelAction struct{}

// Kind returns ActionKindTravel.
func (a *TravelAction) Kind() ActionKind { return ActionKindTravel }

// Score for TravelAction:
//
//	20 (base) + adventurous*0.4 + ambitious*0.2
//	- hunger*0.2 (risk)
//	+ GoalPriority(GainInfluence)*10
//	+ small noise
//
// Adventurous and ambitious NPCs prefer travel. Hungry NPCs
// avoid it (they'd rather work for food). Influence-pursuing
// NPCs get a small boost (you can't gain influence without
// going places).
func (a *TravelAction) Score(p *core.Person, w *core.World) float64 {
	adventurous := traitValue(p, "adventurous")
	ambitious := traitValue(p, "ambitious")
	hunger := needValue(p, core.NeedHunger)
	goal := goalPriority(p, core.GoalGainInfluence)
	// GainInfluence is the strongest single signal here:
	// travel is the only action that advances influence
	// (you can't gain influence without going places), so
	// an influence-pursuing NPC strongly prefers it.
	score := baseScore +
		float64(adventurous)*0.4 +
		float64(ambitious)*0.2 -
		float64(hunger)*0.2 +
		goal*40
	return score + noise(w, p.ID, "travel")
}

// Execute picks a random location (sorted IDs for
// determinism) different from the current one, moves the
// NPC there, and creates a travel memory. If only one
// location exists, the action is a no-op (memory only).
func (a *TravelAction) Execute(p *core.Person, w *core.World) []core.Memory {
	var otherIDs []string
	for id := range w.Locations {
		if id != p.LocationID {
			otherIDs = append(otherIDs, id)
		}
	}
	if len(otherIDs) == 0 {
		return []core.Memory{
			{
				ID:             fmt.Sprintf("mem-travel-%d-%s-nowhere", w.Tick, p.ID),
				OwnerID:        p.ID,
				EventID:        fmt.Sprintf("travel-%d-%s", w.Tick, p.ID),
				Tick:           w.Tick,
				Importance:     0.1,
				Recency:        1.0,
				Description:    fmt.Sprintf("%s considered travel but had nowhere to go", p.Name),
				Tags:           []string{"travel"},
			},
		}
	}
	r := tick.EntityRand(w.Seed, w.Tick, p.ID+":travel-dest")
	p.LocationID = otherIDs[r.Intn(len(otherIDs))]
	bumpGoalProgress(p, core.GoalGainInfluence, 0.005)
	return []core.Memory{
		{
			ID:             fmt.Sprintf("mem-travel-%d-%s", w.Tick, p.ID),
			OwnerID:        p.ID,
			EventID:        fmt.Sprintf("travel-%d-%s", w.Tick, p.ID),
			Tick:           w.Tick,
			Importance:     0.3,
			Recency:        1.0,
			EmotionalScore: 0.1,
			Description:    fmt.Sprintf("%s traveled to %s", p.Name, p.LocationID),
			Tags:           []string{"travel"},
		},
	}
}

// ============================================================================
// TradeAction
// ============================================================================

// TradeAction addresses wealth directly. The NPC sells one
// of their inventory items for coin. The action is more
// attractive when wealth is low and the NPC has a greedy
// trait. In Phase 22 this hands off to the production-loops
// economy engine; for Phase 21 it's a stub that just ticks
// the wealth goal.
type TradeAction struct{}

// Kind returns ActionKindTrade.
func (a *TradeAction) Kind() ActionKind { return ActionKindTrade }

// Score for TradeAction:
//
//	20 (base) + wealth*0.6 + greedy*0.4
//	+ GoalPriority(BecomeWealthy)*35
//	+ small noise
//
// Convention: all needs are "deficit" (higher = more urgent),
// so a high wealth NEED means "I want money" — i.e. I am
// poor. Poor NPCs prefer trade. Greedy NPCs get a strong
// bonus. BecomeWealthy is the most-aligned goal.
func (a *TradeAction) Score(p *core.Person, w *core.World) float64 {
	wealth := needValue(p, core.NeedWealth)
	greedy := traitValue(p, "greedy")
	goal := goalPriority(p, core.GoalBecomeWealthy)
	score := baseScore +
		float64(wealth)*0.6 +
		float64(greedy)*0.4 +
		goal*35
	return score + noise(w, p.ID, "trade")
}

// Execute advances the wealth goal and creates a trade
// memory. Phase 22 will plug in real inventory selling via
// the economy engine.
func (a *TradeAction) Execute(p *core.Person, w *core.World) []core.Memory {
	reduceNeed(p, core.NeedWealth, 8)
	bumpGoalProgress(p, core.GoalBecomeWealthy, 0.02)
	return []core.Memory{
		{
			ID:             fmt.Sprintf("mem-trade-%d-%s", w.Tick, p.ID),
			OwnerID:        p.ID,
			EventID:        fmt.Sprintf("trade-%d-%s", w.Tick, p.ID),
			Tick:           w.Tick,
			Importance:     0.2,
			Recency:        1.0,
			EmotionalScore: 0.0,
			Description:    fmt.Sprintf("%s traded", p.Name),
			Tags:           []string{"trade"},
		},
	}
}

// ============================================================================
// CourtAction
// ============================================================================

// CourtAction addresses the GetMarried and RaiseFamily
// goals. The NPC spends time with a potential partner
// (another unmarried co-located adult of the opposite
// gender), creating a chat memory with a stronger trust
// delta than SocializeAction. The action is more attractive
// when the NPC has a romantic trait or the GetMarried goal.
type CourtAction struct{}

// Kind returns ActionKindCourt.
func (a *CourtAction) Kind() ActionKind { return ActionKindCourt }

// Score for CourtAction:
//
//	20 (base) + romantic*0.5 + companionship*0.2
//	+ GoalPriority(GetMarried)*40 + GoalPriority(RaiseFamily)*10
//	+ small noise
//
// Convention: all needs are "deficit" (higher = more urgent),
// so a high companionship NEED means "I want a partner" — i.e.
// I am lonely. Romantic NPCs strongly prefer courting. Lonely
// NPCs get a smaller boost. The GetMarried goal alignment is
// the strongest single signal.
func (a *CourtAction) Score(p *core.Person, w *core.World) float64 {
	romantic := traitValue(p, "romantic")
	companionship := needValue(p, core.NeedCompanionship)
	marryGoal := goalPriority(p, core.GoalGetMarried)
	familyGoal := goalPriority(p, core.GoalRaiseFamily)
	// RelationshipModifier: if there's a high-trust potential
	// partner at the same location, courting them is more
	// likely to succeed — boost the score proportionally.
	relBonus := relationshipBonus(p, w, pickCourtTargetID(p, w))
	score := baseScore +
		float64(romantic)*0.5 +
		float64(companionship)*0.2 +
		marryGoal*40 +
		familyGoal*10 +
		relBonus +
		memoryBonus(p, w, "court")
	return score + noise(w, p.ID, "court")
}

// Execute finds a co-located unmarried adult of the opposite
// gender and creates a courtship memory. If no suitable
// target exists, the action is a no-op memory.
func (a *CourtAction) Execute(p *core.Person, w *core.World) []core.Memory {
	target := pickCourtTarget(p, w)
	if target == nil {
		return []core.Memory{
			{
				ID:             fmt.Sprintf("mem-court-%d-%s-none", w.Tick, p.ID),
				OwnerID:        p.ID,
				EventID:        fmt.Sprintf("court-%d-%s", w.Tick, p.ID),
				Tick:           w.Tick,
				Importance:     0.1,
				Recency:        1.0,
				Description:    fmt.Sprintf("%s sought a partner but found none nearby", p.Name),
				Tags:           []string{"court"},
			},
		}
	}
	bumpRelationship(w, p.ID, target.ID, 5.0)
	bumpGoalProgress(p, core.GoalGetMarried, 0.03)
	bumpGoalProgress(p, core.GoalRaiseFamily, 0.01)
	actor := core.Memory{
		ID:             fmt.Sprintf("mem-court-%d-%s-%s", w.Tick, p.ID, target.ID),
		OwnerID:        p.ID,
		EventID:        fmt.Sprintf("court-%d-%s-%s", w.Tick, p.ID, target.ID),
		Tick:           w.Tick,
		Importance:     0.4,
		Recency:        1.0,
		EmotionalScore: 0.5,
		TrustDelta:     5.0,
		Description:    fmt.Sprintf("%s courted %s", p.Name, target.Name),
		Tags:           []string{"court", "romance"},
	}
	targetMem := core.Memory{
		ID:             fmt.Sprintf("mem-court-%d-%s-%s-rev", w.Tick, p.ID, target.ID),
		OwnerID:        target.ID,
		EventID:        fmt.Sprintf("court-%d-%s-%s", w.Tick, p.ID, target.ID),
		Tick:           w.Tick,
		Importance:     0.4,
		Recency:        1.0,
		EmotionalScore: 0.5,
		TrustDelta:     5.0,
		Description:    fmt.Sprintf("%s was courted by %s", target.Name, p.Name),
		Tags:           []string{"court", "romance"},
	}
	return []core.Memory{actor, targetMem}
}

// pickCourtTarget returns the first unmarried co-located
// adult of the opposite gender (sorted by ID for
// determinism), or nil if no such person exists.
func pickCourtTarget(p *core.Person, w *core.World) *core.Person {
	id := pickCourtTargetID(p, w)
	if id == "" {
		return nil
	}
	return w.People[id]
}

// pickCourtTargetID returns the ID of a co-located unmarried
// adult of the opposite gender, chosen deterministically per
// (worldSeed, tick, personID) via tick.EntityRand. Returns ""
// if no such person exists. Used by CourtAction.Score via
// relationshipBonus to read the existing-trust delta without
// taking a *core.Person pointer.
//
// Phase 22 fix: the previous "first by sorted ID" rule made
// every courter in a location with multiple eligible partners
// compete for the same target. Determinism is preserved by
// seeding the pick with (seed, tick, personID).
func pickCourtTargetID(p *core.Person, w *core.World) string {
	wantGender := "M"
	if p.Gender == "M" {
		wantGender = "F"
	}
	var candidates []string
	for _, other := range w.LivingPeopleAt(p.LocationID) {
		if other.ID == p.ID {
			continue
		}
		if !other.IsAdult(w.Tick) {
			continue
		}
		if other.Gender != wantGender {
			continue
		}
		if other.SpouseID != "" {
			continue
		}
		candidates = append(candidates, other.ID)
	}
	if len(candidates) == 0 {
		return ""
	}
	r := tick.EntityRand(w.Seed, w.Tick, p.ID+":court-target")
	return candidates[r.Intn(len(candidates))]
}
