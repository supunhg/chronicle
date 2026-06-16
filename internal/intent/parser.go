package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/llm"
)

// LLMClient is the subset of *llm.Client that the intent
// parser uses. Defined as an interface in this package so
// tests can inject a mock without depending on the real HTTP
// client. The real *llm.Client satisfies it automatically.
type LLMClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage) (string, error)
}

// Parser turns raw player input into a typed Intent. The
// zero value is not useful; construct via New.
//
// The parser holds a reference to the current World so the
// LLM prompt can include the people and locations the player
// might be referring to. The world pointer is read-only — the
// parser never mutates state.
type Parser struct {
	llm   LLMClient
	world *core.World
}

// New constructs a Parser. The LLM client may be nil if the
// caller is sure no LLM fallback will be needed (e.g., a
// unit test that only exercises the rule parser). When nil
// and the rule parser doesn't match, Parse returns an error
// rather than panicking.
func New(c LLMClient, w *core.World) *Parser {
	return &Parser{llm: c, world: w}
}

// Parse turns raw into an Intent. The flow:
//
//  1. Trim and validate non-empty.
//  2. Try the rule parser (fast, deterministic, free).
//  3. If the rule parser returns ok=false, call the LLM.
//  4. Schema-validate the LLM response.
//  5. Return the Intent with Source="rule" or Source="llm".
//
// Errors are returned for: empty input, LLM call failed,
// malformed JSON, unknown action, empty action. The
// downstream executor only ever sees a validated Intent.
func (p *Parser) Parse(ctx context.Context, raw string) (Intent, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Intent{Raw: raw, Source: "empty"}, fmt.Errorf("intent: empty input")
	}

	// Step 2: rule parser.
	if intent, ok := p.parseRule(raw); ok {
		return intent, nil
	}

	// Step 3-4: LLM fallback.
	return p.parseLLM(ctx, raw)
}

// parseRule attempts to match raw against the 12 known verbs
// (plus aliases). Returns ok=true on success, ok=false if
// the verb is not recognized (caller should try LLM).
//
// The rule parser is intentionally permissive about the
// shape of the rest of the command: it strips common
// prepositions ("to", "at", "with") and accepts a leading
// number as a quantity/duration. Anything more complex is
// the LLM's job.
func (p *Parser) parseRule(raw string) (Intent, bool) {
	tokens := tokenize(raw)
	if len(tokens) == 0 {
		return Intent{}, false
	}
	verb := resolveVerb(tokens[0])
	if verb == ActionUnknown {
		return Intent{}, false
	}
	rest := tokens[1:]

	// Each verb has its own small handler. They all share the
	// pattern: extract target (and any args) from rest, build
	// the Intent, return ok=true.
	switch verb {
	case ActionLook:
		return p.ruleLook(raw, rest), true
	case ActionInventory:
		return p.ruleInventory(raw, rest), true
	case ActionSleep:
		intent, ok := p.ruleSleep(raw, rest)
		return intent, ok
	case ActionTravel:
		intent, ok := p.ruleTravel(raw, rest)
		return intent, ok
	case ActionTalk:
		intent, ok := p.ruleTargeted(raw, rest, ActionTalk, "to", "with")
		return intent, ok
	case ActionInspect:
		intent, ok := p.ruleTargeted(raw, rest, ActionInspect, "at")
		return intent, ok
	case ActionBuy:
		intent, ok := p.ruleTrade(raw, rest, ActionBuy)
		return intent, ok
	case ActionSell:
		intent, ok := p.ruleTrade(raw, rest, ActionSell)
		return intent, ok
	case ActionTime:
		return Intent{Action: verb, Raw: raw, Source: "rule"}, true
	case ActionSave:
		intent, ok := p.ruleSave(raw, rest)
		return intent, ok
	case ActionBranch:
		intent, ok := p.ruleNameArg(raw, rest, ActionBranch)
		return intent, ok
	case ActionSwitch:
		intent, ok := p.ruleNameArg(raw, rest, ActionSwitch)
		return intent, ok
	case ActionListen:
		return p.ruleLook(raw, rest), true // same shape as look: optional target
	case ActionWait:
		intent, ok := p.ruleWait(raw, rest)
		return intent, ok
	case ActionWalk:
		return p.ruleWalk(raw, rest), true
	case ActionSearch:
		return p.ruleLook(raw, rest), true // same shape: optional target
	case ActionPray:
		return Intent{Action: verb, Raw: raw, Source: "rule"}, true
	case ActionStatus:
		return Intent{Action: verb, Raw: raw, Source: "rule"}, true
	}
	return Intent{}, false
}

// ruleLook handles `look` (and aliases). The target is
// optional: `look alice` means "look at alice", bare `look`
// means "look around". If a target is given, strip leading
// "at".
func (p *Parser) ruleLook(raw string, rest []string) Intent {
	if target := stripPreposition(rest, "at"); target != "" {
		return Intent{Action: ActionLook, Target: target, Raw: raw, Source: "rule"}
	}
	return Intent{Action: ActionLook, Raw: raw, Source: "rule"}
}

// ruleInventory handles `inventory` (and aliases). No
// arguments are accepted; the player types `i` to see their
// stuff, not `i sword`.
func (p *Parser) ruleInventory(raw string, rest []string) Intent {
	_ = rest // explicitly unused
	return Intent{Action: ActionInventory, Raw: raw, Source: "rule"}
}

// ruleSleep handles `sleep [hours]`. If the first token
// after the verb parses as an integer, it's the hours;
// otherwise default to 8h. `sleep` with no rest is valid.
func (p *Parser) ruleSleep(raw string, rest []string) (Intent, bool) {
	hours := 8
	if len(rest) > 0 {
		if h, err := strconv.Atoi(rest[0]); err == nil && h > 0 {
			hours = h
		} else {
			// First token wasn't a number — not a valid
			// sleep command. Defer to LLM.
			return Intent{}, false
		}
	}
	return Intent{
		Action: ActionSleep,
		Args:   Args{Hours: hours},
		Raw:    raw,
		Source: "rule",
	}, true
}

// ruleTravel handles `travel <location>`. The target is
// required; strip leading "to" so "travel to blackwater"
// and "travel blackwater" both work.
func (p *Parser) ruleTravel(raw string, rest []string) (Intent, bool) {
	target := stripPreposition(rest, "to")
	if target == "" {
		return Intent{}, false
	}
	return Intent{Action: ActionTravel, Target: target, Raw: raw, Source: "rule"}, true
}

// ruleTargeted handles verbs that take exactly one target
// (talk, inspect). The prepositions list is stripped from
// the front of rest. Returns the intent (with empty target
// when none was given) and ok=true — the action engine
// handles the empty-target case with a friendly usage
// message, avoiding a wasted LLM round trip for inputs
// like bare "talk" or "inspect". The pre-LLM rule path is
// preferred even for empty targets so the player gets a
// deterministic, localizable message instead of an LLM
// "I don't understand" fallback.
func (p *Parser) ruleTargeted(raw string, rest []string, verb Action, prepositions ...string) (Intent, bool) {
	target := stripPreposition(rest, prepositions...)
	return Intent{Action: verb, Target: target, Raw: raw, Source: "rule"}, true
}

// ruleTrade handles `buy` and `sell`. Accepts an optional
// leading quantity: `buy 5 bread` → target=bread,
// args.quantity=5. Bare `buy bread` → quantity=1 (the
// executor interprets 0 as "unspecified, default to 1").
func (p *Parser) ruleTrade(raw string, rest []string, verb Action) (Intent, bool) {
	if len(rest) == 0 {
		return Intent{}, false
	}
	qty := 1
	idx := 0
	if n, err := strconv.Atoi(rest[0]); err == nil && n > 0 {
		qty = n
		idx = 1
	}
	target := strings.Join(rest[idx:], " ")
	if target == "" {
		return Intent{}, false
	}
	return Intent{
		Action: verb,
		Target: target,
		Args:   Args{Quantity: qty},
		Raw:    raw,
		Source: "rule",
	}, true
}

// ruleWalk handles `walk` and `walk <direction>`. Bare `walk`
// enters interactive mode (the REPL asks for direction).
// `walk north` or `walk to the market` passes the target through.
func (p *Parser) ruleWalk(raw string, rest []string) Intent {
	target := stripPreposition(rest, "to", "toward", "towards")
	return Intent{Action: ActionWalk, Target: target, Raw: raw, Source: "rule"}
}

// ruleWait handles `wait [hours]`. Similar to sleep but
// defaults to 1 hour instead of 8. Bare `wait` means "wait
// briefly and observe". A number is interpreted as hours.
// Non-numeric input falls through (returns ok=false) so the
// LLM can interpret freeform commands like "wait for elena".
func (p *Parser) ruleWait(raw string, rest []string) (Intent, bool) {
	hours := 1
	if len(rest) > 0 {
		if h, err := strconv.Atoi(rest[0]); err == nil && h > 0 {
			hours = h
		} else {
			return Intent{}, false
		}
	}
	return Intent{
		Action: ActionWait,
		Args:   Args{Hours: hours},
		Raw:    raw,
		Source: "rule",
	}, true
}

// ruleSave handles `save [path]`. Path is optional; the
// executor picks a default (e.g., <world-id>.db) when
// empty.
func (p *Parser) ruleSave(raw string, rest []string) (Intent, bool) {
	target := strings.Join(rest, " ")
	return Intent{Action: ActionSave, Target: target, Raw: raw, Source: "rule"}, true
}

// ruleNameArg handles verbs that take a single name argument
// (branch, switch). The name is required.
func (p *Parser) ruleNameArg(raw string, rest []string, verb Action) (Intent, bool) {
	target := strings.Join(rest, " ")
	if target == "" {
		return Intent{}, false
	}
	return Intent{Action: verb, Target: target, Raw: raw, Source: "rule"}, true
}

// parseLLM calls the LLM with a system prompt that lists
// the 12 valid actions and the current world context, then
// schema-validates the response. This is the fallback path:
// the rule parser didn't match, so the LLM gets a chance
// to interpret the command.
//
// The LLM is asked to respond with strict JSON:
// {"action": "...", "target": "...", "args": {...}}. Any
// deviation is rejected — validation is the choke point.
func (p *Parser) parseLLM(ctx context.Context, raw string) (Intent, error) {
	if p.llm == nil {
		return Intent{Raw: raw, Source: "llm"}, fmt.Errorf("intent: unknown verb and no LLM configured: %q", firstToken(raw))
	}

	messages := []llm.ChatMessage{
		{Role: "system", Content: p.systemPrompt()},
		{Role: "user", Content: "Player command: " + raw},
	}
	resp, err := p.llm.Chat(ctx, messages)
	if err != nil {
		return Intent{Raw: raw, Source: "llm"}, fmt.Errorf("intent: LLM call: %w", err)
	}

	// Schema-validate the raw response. This is the gate.
	validated, err := validateLLMResponse(resp)
	if err != nil {
		return Intent{Raw: raw, Source: "llm"}, fmt.Errorf("intent: LLM response: %w", err)
	}

	return Intent{
		Action: validated.Action,
		Target: validated.Target,
		Args:   validated.Args,
		Raw:    raw,
		Source: "llm",
	}, nil
}

// systemPrompt builds the LLM system prompt. It includes
// the 12 valid actions and a compact listing of the current
// world's people and locations so the LLM can disambiguate
// "talk to the blacksmith" → target="bob" (if bob is the
// blacksmith). The world context is bounded — if the world
// has more than maxContextEntities, the list is truncated
// with a note so the LLM knows it's working with a subset.
func (p *Parser) systemPrompt() string {
	var b strings.Builder
	b.WriteString("You are the intent parser for a medieval-fantasy simulation game.\n")
	b.WriteString("Given a player command, return ONLY a JSON object with these fields:\n")
	b.WriteString(`  "action": one of [`)
	b.WriteString(strings.Join(canonicalVerbList(), ", "))
	b.WriteString(`]` + "\n")
	b.WriteString(`  "target": the entity or location being acted on (string; omit if not applicable)` + "\n")
	b.WriteString(`  "args": an object with verb-specific fields (omit if not applicable)` + "\n")
	b.WriteString("If you cannot confidently parse the command, return: {\"action\": \"\"}\n")
	b.WriteString("Respond with JSON only. No prose, no markdown fences, no explanation.\n")

	if p.world != nil {
		b.WriteString("\nCurrent world context:\n")
		writeWorldContext(&b, p.world)
	}
	return b.String()
}

// writeWorldContext writes a compact, sorted list of
// people and locations to b. Truncated to maxContextEntities
// per category with a "...(N more)" note so the prompt
// stays bounded for large worlds.
func writeWorldContext(b *strings.Builder, w *core.World) {
	const maxContextEntities = 50

	b.WriteString("People (name | id | location):\n")
	people := make([]*core.Person, 0, len(w.People))
	for _, p := range w.People {
		if p.Alive {
			people = append(people, p)
		}
	}
	sort.Slice(people, func(i, j int) bool { return people[i].ID < people[j].ID })
	if len(people) > maxContextEntities {
		for _, p := range people[:maxContextEntities] {
			fmt.Fprintf(b, "  %s | %s | %s\n", p.Name, p.ID, p.LocationID)
		}
		fmt.Fprintf(b, "  ...(%d more)\n", len(people)-maxContextEntities)
	} else {
		for _, p := range people {
			fmt.Fprintf(b, "  %s | %s | %s\n", p.Name, p.ID, p.LocationID)
		}
	}

	b.WriteString("Locations (name | id):\n")
	locs := make([]*core.Location, 0, len(w.Locations))
	for _, l := range w.Locations {
		locs = append(locs, l)
	}
	sort.Slice(locs, func(i, j int) bool { return locs[i].ID < locs[j].ID })
	if len(locs) > maxContextEntities {
		for _, l := range locs[:maxContextEntities] {
			fmt.Fprintf(b, "  %s | %s\n", l.Name, l.ID)
		}
		fmt.Fprintf(b, "  ...(%d more)\n", len(locs)-maxContextEntities)
	} else {
		for _, l := range locs {
			fmt.Fprintf(b, "  %s | %s\n", l.Name, l.ID)
		}
	}
}

// validatedIntent is the JSON-decoded LLM response after
// schema validation. Kept private because the public Intent
// type adds Raw and Source which the LLM doesn't know
// about.
type validatedIntent struct {
	Action Action `json:"action"`
	Target string `json:"target"`
	Args   Args   `json:"args"`
}

// validateLLMResponse decodes the LLM's response and runs
// the schema validation gate. Rejects:
//
//   - Malformed JSON (decode error).
//   - Empty action (LLM said "I can't parse").
//   - Unknown action (LLM hallucinated a verb not in the spec).
//   - Wrong field types (e.g., target as a number).
//
// The gate is the choke point: a bad LLM response never
// reaches the action executor.
func validateLLMResponse(raw string) (validatedIntent, error) {
	var v validatedIntent
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return v, fmt.Errorf("malformed JSON: %w", err)
	}
	if v.Action == ActionUnknown {
		return v, fmt.Errorf("empty action (LLM could not parse)")
	}
	if !IsKnownAction(v.Action) {
		return v, fmt.Errorf("unknown action %q (not in the 12-verb spec)", v.Action)
	}
	return v, nil
}

// tokenize splits raw on whitespace and lowercases ONLY
// the first token (the verb) for case-insensitive matching.
// The remaining tokens keep their original case so targets
// like file paths and proper nouns are preserved. The LLM
// fallback is unaffected — it receives the original raw
// input in the user message.
//
// Why this matters: lowercasing the whole input would
// mangle file paths in the `save` command (e.g.,
// `/tmp/TestREPL_Save/001/test.db` → `/tmp/testrepl_save/
// 001/test.db`, which doesn't exist on case-sensitive
// filesystems). It would also mangle proper-noun targets
// like "Alice" → "alice", which is fine for case-insensitive
// lookups but loses information the LLM might want.
func tokenize(raw string) []string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return fields
	}
	fields[0] = strings.ToLower(fields[0])
	return fields
}

// stripPreposition removes a leading preposition token from
// rest. If the first token matches one of the prepositions
// (case-insensitive), it's dropped and the remaining tokens
// are joined with spaces. Returns "" if nothing remains.
//
// Case-insensitive because the rule parser preserves the
// original case of the target tokens (only the verb is
// lowercased). "look At Alice" and "look at Alice" should
// both strip the "at".
func stripPreposition(rest []string, prepositions ...string) string {
	if len(rest) == 0 {
		return ""
	}
	first := strings.ToLower(rest[0])
	for _, prep := range prepositions {
		if first == prep {
			return strings.Join(rest[1:], " ")
		}
	}
	return strings.Join(rest, " ")
}

// firstToken returns the first whitespace-delimited token
// of raw, or raw itself if there are no spaces. Used in
// error messages so the user sees what the parser tried to
// match.
func firstToken(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return raw
	}
	return fields[0]
}
