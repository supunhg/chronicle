package core

// GoalID is the canonical identifier for a long-term goal an NPC
// may hold. Phase 21 v1 ships 5 goals: BecomeWealthy, GetMarried,
// RaiseFamily, GainInfluence, JoinGuild. GoalIDs are part of the
// public API: they appear in worldpack YAML, persisted worlds,
// and event logs.
type GoalID string

// The 5 v1 long-term goals. Per chronicle-spec.md §5.3 these are
// the only long-horizon drivers the Goal Engine considers for
// Phase 21; more can be added without breaking existing worlds
// (the engine falls back to "no GoalAlignment bonus" for
// unknown IDs).
const (
	GoalBecomeWealthy GoalID = "become_wealthy"
	GoalGetMarried    GoalID = "get_married"
	GoalRaiseFamily   GoalID = "raise_family"
	GoalGainInfluence GoalID = "gain_influence"
	GoalJoinGuild     GoalID = "join_guild"
)

// Goal is a long-term goal an NPC is pursuing. Phase 21 v1
// fields: ID, Priority, Progress. Priority is 0..1 (higher =
// more important). Progress is 0..1 (0 = not started, 1 =
// completed). The engine reads Priority when scoring
// GoalAlignment, and writes Progress back as actions advance
// the goal.
type Goal struct {
	// ID identifies the goal. See GoalID constants.
	ID GoalID

	// Priority is 0..1. A goal with Priority=0 is dormant
	// (the engine skips it during scoring). Goals are
	// typically seeded at 0.5 and decay over time if not
	// advanced; future phases can boost priority from
	// events (e.g., seeing a wealthy neighbor increases
	// BecomeWealthy priority).
	Priority float64

	// Progress is 0..1. 0 = not started, 1 = completed.
	// A completed goal may be retired by the engine
	// (removed from Person.Goals) or kept as a "trophy"
	// record depending on the goal type.
	Progress float64
}

// NeedID is the canonical identifier for a short-term need.
// Phase 21 reuses the Phase 2 v1 set (hunger, wealth,
// companionship, safety) and adds rest. Using a typed
// constant avoids string typos across the engine.
type NeedID string

// The v1 needs. Same set as the Phase 2 DefaultNeeds plus
// rest. Defined here so action scoring and engine decay use
// the same identifiers.
const (
	NeedHunger        NeedID = "hunger"
	NeedWealth        NeedID = "wealth"
	NeedCompanionship NeedID = "companionship"
	NeedSafety        NeedID = "safety"
	NeedRest          NeedID = "rest"
)

// DefaultNeedDecay is the per-tick decay applied to every
// need, clamped at 0. Phase 21 v1: uniform decay (no
// per-need multiplier) — the engine can grow per-need
// decay in Phase 22+ as the economy engine starts
// providing real consumption signals.
const DefaultNeedDecay = 1

// DefaultNeedInitial is the starting value assigned to every
// need in Init. Phase 21 v1: 50 (the Phase 2 default).
const DefaultNeedInitial = 50
