// Package intent parses natural-language player commands into a
// typed Intent that the rest of the engine can act on.
//
// Design (per chronicle-spec.md §4.1):
//
//  1. The rule parser handles 12 hard-coded verbs (look,
//     inventory, sleep, travel, talk, inspect, buy, sell, time,
//     save, branch, switch) plus a small set of aliases. These
//     are the commands the player can type without an LLM round
//     trip — fast, deterministic, free.
//  2. If the rule parser doesn't match, the LLM is called with
//     a system prompt that lists the 12 valid actions and the
//     current world context (people, locations). The LLM must
//     respond with a strict JSON object: {action, target, args}.
//  3. The schema validation gate rejects malformed JSON,
//     unknown actions, empty actions, and type-mismatched
//     fields. Validation is the choke point — a bad LLM
//     response never reaches the action executor.
//
// The parser does NOT execute the intent. Execution lives in
// the action layer (Phase 17.3+). The parser's job is to turn
// "talk to elena about the harvest" into a typed record; the
// executor's job is to decide whether the talk is allowed,
// who elena is, and what the harvest refers to.
package intent

import (
	"fmt"
	"sort"
	"strings"
)

// Action is the verb of a parsed player command. Go has no
// native enums, so we use string constants for readability in
// logs, LLM prompts, and JSON wire format. The string values
// are part of the public API — they appear in the LLM's
// response and in persisted intents.
type Action string

// The 12 hard-coded verbs from the spec, plus ActionUnknown
// for the "I can't parse this" fallback. The empty string
// value for ActionUnknown is the schema validator's signal
// that the LLM gave up.
const (
	ActionLook      Action = "look"
	ActionInventory Action = "inventory"
	ActionSleep     Action = "sleep"
	ActionTravel    Action = "travel"
	ActionTalk      Action = "talk"
	ActionInspect   Action = "inspect"
	ActionBuy       Action = "buy"
	ActionSell      Action = "sell"
	ActionTime      Action = "time"
	ActionSave      Action = "save"
	ActionBranch    Action = "branch"
	ActionSwitch    Action = "switch"
	ActionUnknown   Action = ""
)

// AllActions returns the 12 known actions in the spec's
// canonical order. Used by the LLM prompt to enumerate valid
// verbs. The order matches the spec §4.1 list.
func AllActions() []Action {
	return []Action{
		ActionLook, ActionInventory, ActionSleep, ActionTravel,
		ActionTalk, ActionInspect, ActionBuy, ActionSell,
		ActionTime, ActionSave, ActionBranch, ActionSwitch,
	}
}

// IsKnownAction reports whether a is one of the 12 spec verbs.
// ActionUnknown and any other string return false.
func IsKnownAction(a Action) bool {
	for _, known := range AllActions() {
		if a == known {
			return true
		}
	}
	return false
}

// Args holds the optional, verb-specific arguments of an
// Intent. Most intents have no args; the ones that do are
// sleep (hours) and buy/sell (quantity). Using a typed struct
// (rather than map[string]any) gives the schema validator
// something to check and the action executor something to
// read without type assertions.
type Args struct {
	// Quantity is set by buy/sell. 0 means "unspecified" (the
	// executor will pick a sensible default like 1).
	Quantity int `json:"quantity,omitempty"`

	// Hours is set by sleep. 0 means "default" (8h).
	Hours int `json:"hours,omitempty"`
}

// Intent is the typed result of parsing a player command.
// It is the contract between the parser and the action
// executor: every field the executor needs is here, and
// every field has been schema-validated.
//
// Raw and Source are for logging, debugging, and the REPL
// (Phase 17.3). Raw is the trimmed input; Source is "rule"
// for the deterministic parser and "llm" for the fallback.
type Intent struct {
	// Action is the verb. Always one of AllActions() after
	// validation; the schema validator rejects ActionUnknown.
	Action Action `json:"action"`

	// Target is the entity being acted on. Its type depends
	// on Action: a person ID for talk/inspect, a location ID
	// for travel, an item name for buy/sell, a snapshot path
	// for save, a timeline name for branch/switch. Empty
	// when the verb doesn't take a target (look, inventory,
	// time, sleep with no duration).
	Target string `json:"target,omitempty"`

	// Args holds verb-specific extras. See Args above.
	Args Args `json:"args"`

	// Raw is the original trimmed input. Kept for logs and
	// for the REPL's "you said: ..." echo.
	Raw string `json:"raw"`

	// Source is "rule" if the rule parser matched, "llm" if
	// the LLM fallback produced the intent, or "empty" if
	// the input was blank (the Intent in that case is
	// discarded along with the error, but the field is set
	// for symmetry). Useful for metrics (e.g., "what
	// fraction of commands need an LLM round trip?") and
	// for debugging.
	Source string `json:"source"`
}

// String renders the Intent in a human-friendly form for the
// REPL and for log lines. Example: `Intent{action=talk,
// target=alice, source=rule, raw="talk to alice"}`.
func (i Intent) String() string {
	if i.Target == "" {
		return fmt.Sprintf("Intent{action=%s, source=%s, raw=%q}",
			i.Action, i.Source, i.Raw)
	}
	return fmt.Sprintf("Intent{action=%s, target=%q, source=%s, raw=%q}",
		i.Action, i.Target, i.Source, i.Raw)
}

// alias maps short verb forms to their canonical Action.
// The keys are lowercase; the rule parser lowercases the
// input before lookup. Aliases are a usability feature: a
// player who types `l` or `i` gets the same behavior as
// `look` or `inventory` without burning an LLM round trip.
var alias = map[string]Action{
	// look
	"l":     ActionLook,
	"watch": ActionLook,
	// inventory
	"i":   ActionInventory,
	"inv": ActionInventory,
	// travel
	"go":   ActionTravel,
	"walk": ActionTravel,
	"move": ActionTravel,
	// talk
	"say":    ActionTalk,
	"chat":   ActionTalk,
	"speak":  ActionTalk,
	"ask":    ActionTalk,
	// inspect
	"x":       ActionInspect,
	"examine": ActionInspect,
	"check":   ActionInspect,
	"lookat":  ActionInspect,
	// time
	"date": ActionTime,
	"now":  ActionTime,
	"when": ActionTime,
	// save
	"snapshot": ActionSave,
	// branch
	"fork":   ActionBranch,
	// switch
	"checkout": ActionSwitch,
	"goto":     ActionSwitch,
}

// resolveVerb returns the canonical Action for the first
// token, or ActionUnknown if the token is not a known verb
// or alias. Lookup is case-insensitive.
func resolveVerb(token string) Action {
	t := strings.ToLower(token)
	if a, ok := alias[t]; ok {
		return a
	}
	// Direct hit on a canonical action name (also lowercase).
	if a := Action(t); IsKnownAction(a) {
		return a
	}
	return ActionUnknown
}

// canonicalVerbList returns the sorted list of canonical
// action names. Used in the LLM prompt to enumerate valid
// verbs.
func canonicalVerbList() []string {
	out := make([]string, 0, len(AllActions()))
	for _, a := range AllActions() {
		out = append(out, string(a))
	}
	sort.Strings(out)
	return out
}
