package simulation

import "github.com/chronicle-dev/chronicle/internal/core"

// Action kinds per chronicle-spec.md §5.3.
const (
	ActionWork  = "work"
	ActionSleep = "sleep"
	ActionEat   = "eat"
	ActionRest  = "rest"
	ActionTalk  = "talk"
	ActionIdle  = "idle"
)

// HardActions is the Phase 2 v1 set of engine-defined verbs available
// to any NPC that meets preconditions. Phase 3 (world pack) will add
// contextual and opportunistic actions.
var HardActions = []string{
	ActionWork, ActionSleep, ActionEat, ActionRest, ActionTalk, ActionIdle,
}

// DefaultNeeds is the initial need set assigned by Init to every
// living person whose Needs map is empty. Values are 0-100.
var DefaultNeeds = map[string]int{
	"hunger":        50,
	"wealth":        50,
	"companionship": 50,
	"safety":        50,
}

// GoalEngine is the Phase 2 stub for the GoalEngine per
// chronicle-spec.md §5.3.
//
// In Phase 2, this engine:
//   - Initializes Needs for every living person in Init
//   - Decays all needs by 1 per tick
//
// Phase 3+ will:
//   - Generate candidate actions (3-layer system: Hard, Contextual,
//     Opportunistic)
//   - Score each action with the utility formula
//   - Apply the chosen action to entity state
type GoalEngine struct{}

// NewGoalEngine returns a GoalEngine with default settings.
func NewGoalEngine() *GoalEngine {
	return &GoalEngine{}
}

// Init sets Needs to DefaultNeeds for every living person whose Needs
// map is empty. Existing needs are preserved.
func (g *GoalEngine) Init(w *core.World) error {
	for _, p := range w.LivingPeople() {
		if p.Needs == nil {
			p.Needs = make(map[string]int, len(DefaultNeeds))
			for k, v := range DefaultNeeds {
				p.Needs[k] = v
			}
		}
	}
	return nil
}

// Tick decays all needs by 1 per tick, clamping at 0. Phase 2 does
// not yet pick or apply actions.
func (g *GoalEngine) Tick(w *core.World) error {
	for _, p := range w.LivingPeople() {
		for k, v := range p.Needs {
			if v > 0 {
				p.Needs[k] = v - 1
			}
		}
	}
	return nil
}
