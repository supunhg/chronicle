// Phase 23: Event Engine V1.
//
// Implements the state-driven event system per the Phase 23
// spec. Exactly 4 rules, no world mutation, no scripted
// stories — only consequences of observed state.
//
// Architecture:
//
//	type EventRule interface {
//	    Evaluate(*core.World) []core.Event
//	}
//
// EventEngine.Tick iterates the 4 v1 rules, calls Evaluate
// on each, applies a per-(rule, location) cooldown, and
// appends surviving events to w.Events. Consumers (future
// phases) read w.Events and react on later ticks.
//
// The "no mutation" rule is critical: events are EMIT, never
// APPLIED. This preserves determinism (the event log is the
// audit trail) and makes replay debugging tractable.
package simulation

import (
	"sort"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// sortStrings is a tiny indirection around sort.Strings so
// the FamineRule can iterate locations in deterministic
// order without an explicit sort.Strings call.
func sortStrings(s []string) { sort.Strings(s) }

// EventCooldownTicks is the minimum number of ticks between
// consecutive emissions of the same (Type, Location) event.
// V1 default: 30 ticks (~1 month at 365 ticks/year). This
// prevents event spam when a condition persists (e.g., a
// settlement with 0 food for 100 ticks should not emit 100
// FamineRisk events). Per-rule overrides are possible in v2
// by adding a per-rule cooldown table; for v1, 30 is a
// reasonable v1 default that matches the spec's "don't
// spam" guidance.
const EventCooldownTicks int64 = 30

// EventRule is the interface every state-driven event rule
// implements. Evaluate reads world state and returns a list
// of events to emit. Rules must NOT mutate world state; if
// they need to, the mutation belongs in a separate engine
// that consumes the emitted events on a later tick.
type EventRule interface {
	// Name returns a short, unique name for the rule (used
	// as the cooldown key prefix and in test assertions).
	Name() string

	// Evaluate reads the world and returns events to emit.
	// May return an empty slice (the most common case when
	// the rule's preconditions are not met).
	Evaluate(w *core.World) []core.Event
}

// EventEngine runs the 4 v1 event rules once per tick and
// dedupes via a per-(rule, location) cooldown map.
type EventEngine struct {
	// lastFired maps "ruleName:location" to the tick at
	// which the rule last emitted for that location. The map
	// is rebuilt from scratch on each Tick (cheap: 4 keys
	// max in v1) so the engine has no persistent state
	// beyond its cooldown table.
	lastFired map[string]int64
}

// NewEventEngine returns an EventEngine with default
// settings and the 4 v1 rules registered.
func NewEventEngine() *EventEngine {
	return &EventEngine{
		lastFired: make(map[string]int64),
	}
}

// AllEventRules returns the canonical 4 v1 event rules in a
// stable order. Phase 24+ can extend this slice to add more
// rules; the engine doesn't care about the order (Evaluate is
// pure), but a stable order makes test assertions easier.
func AllEventRules() []EventRule {
	return []EventRule{
		&FamineRule{},
		&CrimeRule{},
		&PoliticalRule{},
		&ReligiousRule{},
	}
}

// Init is a no-op for the v1 EventEngine. The cooldown map
// is built in the constructor.
func (e *EventEngine) Init(w *core.World) error { return nil }

// Tick runs the 4 v1 rules, applies the per-(rule, location)
// cooldown, and appends surviving events to w.Events. Does
// NOT mutate world state. The cooldown prevents event spam
// when a condition persists (a settlement with 0 food emits
// a FamineRisk at most once every EventCooldownTicks).
//
// Determinism: the rule order is fixed, the cooldown key
// is deterministic, and the event ID is derived from
// (rule, location, tick). Two worlds with the same seed
// and tick produce the same event log.
func (e *EventEngine) Tick(w *core.World) error {
	for _, rule := range AllEventRules() {
		events := rule.Evaluate(w)
		for _, ev := range events {
			key := rule.Name() + ":" + ev.Location
			if last, ok := e.lastFired[key]; ok {
				if w.Tick-last < EventCooldownTicks {
					continue
				}
			}
			e.lastFired[key] = w.Tick
			ev.ID = makeEventID(rule.Name(), ev.Location, w.Tick)
			ev.Tick = w.Tick
			w.Events = append(w.Events, ev)
		}
	}
	// Phase 26 Part B: enforce the live-event cap so the event
	// log stays bounded across long runs. The trim runs AFTER
	// the engine has finished appending for this tick, so this
	// tick's events always survive (they are the newest).
	// No-op when the log is under MaxLiveEvents.
	TrimEvents(w)
	return nil
}

// makeEventID returns a deterministic event ID for the
// given (rule, location, tick). The format is
// "evt-<rule>-<location>-<tick>". Used for the event log
// audit trail and for test assertions.
func makeEventID(rule, location string, tick int64) string {
	if location == "" {
		location = "global"
	}
	return "evt-" + rule + "-" + location + "-" + itoa(tick)
}

// itoa is a tiny int64-to-string helper to avoid importing
// strconv (the v1 EventEngine is deliberately dependency-
// free above core). For negative ticks the result still
// round-trips through makeEventID (deterministic) but is
// unlikely in practice (world.Tick starts at 0).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ============================================================================
// FamineRule
// ============================================================================

// FamineRule emits a FamineRisk event for every location
// whose Settlement.Food is below the EconomyEngine's
// shortage threshold. The threshold is shared with the
// per-tick shortage penalty so the two systems stay in sync
// (a location in "famine risk" is also the one whose
// residents are getting their NeedHunger bumped).
type FamineRule struct{}

// Name returns "famine_risk".
func (r *FamineRule) Name() string { return "famine_risk" }

// Evaluate returns one FamineRisk event per location in
// shortage. Empty if no location is in shortage.
//
// Iteration order is sorted by location ID so the event log
// is deterministic across runs with the same world state
// (map iteration in Go is intentionally randomized, which
// would make the order of w.Events depend on the runtime).
func (r *FamineRule) Evaluate(w *core.World) []core.Event {
	ids := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		ids = append(ids, id)
	}
	sortStrings(ids)
	var out []core.Event
	for _, id := range ids {
		loc := w.Locations[id]
		if loc.Settlement.Food >= EconomyShortageThreshold {
			continue
		}
		out = append(out, core.Event{
			Type:     core.EventFamineRisk,
			Location: id,
			Payload: map[string]any{
				"current_food": loc.Settlement.Food,
				"threshold":    EconomyShortageThreshold,
			},
		})
	}
	return out
}

// ============================================================================
// CrimeRule (TheftWave)
// ============================================================================

// CrimeRule emits a TheftWave event when the aggregate
// hunger across all living people is high AND the aggregate
// wealth is low. Both conditions must hold (the spec's
// "High hunger + Low wealth -> TheftWave").
//
// V1 thresholds: hunger > 70, wealth < 30. These are
// hand-tuned starting points; v2 can read them from the
// worldpack's economy spec. A v1 "theft wave" is a world-
// level event (Location="") because crime in this model is
// not location-scoped.
type CrimeRule struct{}

// Name returns "theft_wave".
func (r *CrimeRule) Name() string { return "theft_wave" }

// Evaluate returns at most one TheftWave event (or none).
// V1: the rule fires when the world-wide average hunger
// need exceeds 70 AND the average wealth need exceeds 70
// (i.e., people want both food AND money). Both are
// measured as the average over all living people with
// non-nil Needs maps. Children and dead people are
// excluded (LivingPeople does the filtering).
//
// The v1 model treats "hunger need" and "wealth need" as
// deficit measures: high values = "I want food / I want
// money". The thresholds (70, 70) are calibrated to the
// default starting value of 50: an NPC at 70 is "noticeably
// unhappy", at 100 is "desperate".
func (r *CrimeRule) Evaluate(w *core.World) []core.Event {
	people := w.LivingPeople()
	if len(people) == 0 {
		return nil
	}
	var sumHunger, sumWealth, n int
	for _, p := range people {
		if p.Needs == nil {
			continue
		}
		sumHunger += p.Needs[string(core.NeedHunger)]
		sumWealth += p.Needs[string(core.NeedWealth)]
		n++
	}
	if n == 0 {
		return nil
	}
	avgHunger := float64(sumHunger) / float64(n)
	avgWealth := float64(sumWealth) / float64(n)
	if avgHunger <= 70 || avgWealth <= 70 {
		return nil
	}
	return []core.Event{{
		Type:     core.EventTheftWave,
		Location: "",
		Payload: map[string]any{
			"avg_hunger": avgHunger,
			"avg_wealth": avgWealth,
		},
	}}
}

// ============================================================================
// PoliticalRule (CouncilScandal)
// ============================================================================

// PoliticalRule emits a CouncilScandal event when the
// town_council faction exists and has at least 5 members.
// V1 model: a faction's "corruption" is approximated by its
// member count (more members = more political actors = more
// opportunities for scandal). A v2 rule can read a real
// corruption metric from a worldpack field.
type PoliticalRule struct{}

// Name returns "council_scandal".
func (r *PoliticalRule) Name() string { return "council_scandal" }

// Evaluate returns at most one CouncilScandal event. V1:
// the rule fires when there is a "town_council" faction in
// w.Factions AND at least 5 living people have an
// occupation that is in the faction's member-occupations
// list. Location is "" (the scandal is world-wide, not
// location-scoped).
func (r *PoliticalRule) Evaluate(w *core.World) []core.Event {
	council, ok := w.Factions["town_council"]
	if !ok {
		return nil
	}
	// Count living members: a person is a "member" of the
	// council if their occupation is in the faction's
	// MemberOccupations list (set by worldpack bootstrap).
	memberSet := make(map[string]bool, len(council.MemberOccupations))
	for _, occ := range council.MemberOccupations {
		memberSet[occ] = true
	}
	members := 0
	for _, p := range w.LivingPeople() {
		if memberSet[p.Occupation] {
			members++
		}
	}
	if members < 5 {
		return nil
	}
	return []core.Event{{
		Type:     core.EventCouncilScandal,
		Location: "",
		Payload: map[string]any{
			"member_count": members,
		},
	}}
}

// ============================================================================
// ReligiousRule (RevivalMovement)
// ============================================================================

// ReligiousRule emits a RevivalMovement event when the
// faith_of_dawn faction exists and has at least 5 members.
// Symmetric with PoliticalRule: a faction with 5+ members
// is "active enough" to drive a movement. V2 can introduce
// a "faith intensity" per-person metric.
type ReligiousRule struct{}

// Name returns "revival_movement".
func (r *ReligiousRule) Name() string { return "revival_movement" }

// Evaluate returns at most one RevivalMovement event. V1:
// the rule fires when there is a "faith_of_dawn" faction
// AND at least 5 living people have an occupation in the
// faction's MemberOccupations (priest, teacher, etc.).
func (r *ReligiousRule) Evaluate(w *core.World) []core.Event {
	faith, ok := w.Factions["faith_of_dawn"]
	if !ok {
		return nil
	}
	memberSet := make(map[string]bool, len(faith.MemberOccupations))
	for _, occ := range faith.MemberOccupations {
		memberSet[occ] = true
	}
	members := 0
	for _, p := range w.LivingPeople() {
		if memberSet[p.Occupation] {
			members++
		}
	}
	if members < 5 {
		return nil
	}
	return []core.Event{{
		Type:     core.EventRevivalMovement,
		Location: "",
		Payload: map[string]any{
			"member_count": members,
		},
	}}
}
