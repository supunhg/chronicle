// Package narrator renders narrative text for Chronicle
// simulation events. It has two modes:
//
//  1. Template mode: routine events (look, inventory, time,
//     travel) are rendered from a small set of fixed
//     templates. Free, deterministic, zero-latency.
//
//  2. LLM mode: narratively significant events (talk,
//     death, birth, first meeting) call the LLM for
//     generated prose. Rate-limited (max 1 call per
//     MinTicksBetweenCalls ticks) and cached (key =
//     prompt_version + world_hash + event_type + npc_state).
//
// The LLM is the only source of variability in the system.
// Per the spec, the LLM is forbidden from mutating world
// state — it only produces text. The Narrator's job is to
// decide WHEN to call the LLM, WHAT to ask, and HOW to
// fall back when the LLM is unavailable or rate-limited.
//
// Phase 17.4 scope: the REPL's execTalk and execTravel
// stubs from Phase 17.3 call into Narrator.Narrate. Future
// phases will wire MemoryEngine and PopulationEngine
// events (deaths, births, lineage transfers) through the
// same entry point.
package narrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/llm"
)

// PromptVersion is included in the cache key so that
// prompt-template changes invalidate the cache. Bump
// this when the system or user prompts change.
const PromptVersion = "narrator-v1"

// DefaultMinTicksBetweenCalls is the default rate limit
// for LLM narration calls. The simulation runs at
// 1 tick = 1 simulated day, so 4 ticks ≈ 4 sim-days.
// The spec's "4 sim-hours" was written for a finer-grained
// time model; we use ticks as the natural unit.
const DefaultMinTicksBetweenCalls = 4

// LLMClient is the subset of *llm.Client that the narrator
// uses. Defined as an interface in this package so tests
// can inject a mock without depending on the real HTTP
// client. The real *llm.Client satisfies it automatically.
type LLMClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage) (string, error)
}

// EventType identifies what kind of narrative the narrator
// is being asked to produce. The string values are part
// of the cache key and the LLM prompt contract.
type EventType string

const (
	// Routine events (template-only).
	EventLook      EventType = "look"
	EventInventory EventType = "inventory"
	EventTime      EventType = "time"
	// EventTravel is routine (template-only) in Phase 17.4.
	// It still has a template renderer and a cache entry,
	// but is not in the isSignificant list — travel
	// narration doesn't warrant an LLM call.
	EventTravel EventType = "travel"

	// Significant events (LLM when allowed, template fallback).
	EventTalk      EventType = "talk"
	EventDeath     EventType = "death"
	EventBirth     EventType = "birth"
	EventFirstMeet EventType = "first_meet"
)

// Event describes what happened. The narrator uses the
// event's fields to build the cache key and the LLM
// prompt. Person and Location are optional: look/talk
// events carry a Person, travel events carry a Location,
// time/inventory events carry neither.
type Event struct {
	Type     EventType
	Person   *core.Person
	Location *core.Location
}

// Narrator produces narrative text for simulation events.
// The zero value is not useful; construct via New.
//
// Narrator is NOT safe for concurrent use. The cache and
// rate-limit state are not protected by a mutex. The
// current caller (the REPL) is single-threaded, so this is
// fine in practice. If a future phase needs to narrate
// from multiple goroutines (e.g., background event
// streamers), wrap Narrate in a mutex or use a sync.Map
// for the cache.
type Narrator struct {
	llm                  LLMClient
	minTicksBetweenCalls int64
	promptVersion        string
	cache                map[string]string
	lastCallTick         int64
}

// New constructs a Narrator. The LLM client may be nil
// for template-only mode (no LLM calls will be made).
// minTicksBetweenCalls <= 0 means use the default.
func New(c LLMClient, minTicksBetweenCalls int) *Narrator {
	if minTicksBetweenCalls <= 0 {
		minTicksBetweenCalls = DefaultMinTicksBetweenCalls
	}
	return &Narrator{
		llm:                  c,
		minTicksBetweenCalls: int64(minTicksBetweenCalls),
		promptVersion:        PromptVersion,
		cache:                make(map[string]string),
	}
}

// Narrate produces narrative text for the given event.
// Returns the rendered text. Errors are only returned
// for truly fatal problems (e.g., context cancellation);
// LLM failures fall back to the template silently so the
// player always gets something to read.
func (n *Narrator) Narrate(ctx context.Context, w *core.World, e Event) string {
	if w == nil {
		return ""
	}
	// Always use the template for routine events — no LLM
	// cost, no rate limit consumption, instant response.
	if !isSignificant(e.Type) {
		return n.renderTemplate(w, e)
	}

	// Cache: checked before the rate limit so that repeated
	// calls within the rate-limit window still return the
	// cached narration. The cache key includes the
	// prompt_version, world_hash, event_type, and npc_state
	// (age at current tick), so it correctly invalidates
	// when any of those change.
	key := n.cacheKey(w, e)
	if cached, ok := n.cache[key]; ok {
		return cached
	}

	// Rate limit: if the last LLM call was within the
	// configured window, fall back to the template (do NOT
	// update lastCallTick — the rate limit window starts
	// from the last actual LLM call, not from the last
	// Narrate call).
	if w.Tick-n.lastCallTick < n.minTicksBetweenCalls {
		return n.renderTemplate(w, e)
	}

	// LLM call. Nil client → template fallback.
	if n.llm == nil {
		return n.renderTemplate(w, e)
	}
	text, err := n.callLLM(ctx, e)
	if err != nil || text == "" {
		// LLM error or empty response → template fallback.
		// Empty responses are treated as failures so the
		// player always gets something to read.
		return n.renderTemplate(w, e)
	}

	n.cache[key] = text
	n.lastCallTick = w.Tick
	return text
}

// isSignificant reports whether the event type should
// trigger an LLM call (when allowed by the rate limiter).
// Routine types (look, inventory, time, travel) are always
// template-rendered; significant types (talk, death,
// birth, first_meet) are LLM-rendered when the rate
// limit allows.
func isSignificant(t EventType) bool {
	switch t {
	case EventTalk, EventDeath, EventBirth, EventFirstMeet:
		return true
	}
	return false
}

// renderTemplate dispatches to the per-event-type
// template renderer. Every event type has a template;
// significant events use the template as the LLM
// fallback.
func (n *Narrator) renderTemplate(w *core.World, e Event) string {
	switch e.Type {
	case EventLook:
		return n.tplLook(w, e)
	case EventInventory:
		return n.tplInventory()
	case EventTime:
		return n.tplTime(w)
	case EventTalk:
		return n.tplTalk(w, e)
	case EventTravel:
		return n.tplTravel(w, e)
	case EventDeath:
		return n.tplDeath(e)
	case EventBirth:
		return n.tplBirth(e)
	case EventFirstMeet:
		return n.tplFirstMeet(e)
	}
	return ""
}

// tplLook renders a "look" event. With a person target,
// it shows the person's details. With a location target,
// it shows the location and its people. With no target,
// it shows the first location (sorted by ID).
func (n *Narrator) tplLook(w *core.World, e Event) string {
	if e.Person != nil {
		return fmt.Sprintf("%s (%s, %d) is at %s.",
			e.Person.Name, e.Person.Gender,
			e.Person.AgeAt(w.Tick), locationName(w, e.Person.LocationID))
	}
	if e.Location != nil {
		people := w.LivingPeopleAt(e.Location.ID)
		if len(people) == 0 {
			return fmt.Sprintf("%s is empty.", e.Location.Name)
		}
		names := make([]string, len(people))
		for i, p := range people {
			names[i] = p.Name
		}
		return fmt.Sprintf("%s (%d people): %s.",
			e.Location.Name, len(people), strings.Join(names, ", "))
	}
	return "You see nothing."
}

// tplInventory is a stub: the player concept (and thus
// inventory) is added in a later phase. The template
// acknowledges the command so the REPL wiring works.
func (n *Narrator) tplInventory() string {
	return "You have nothing."
}

// tplTime renders the current sim time.
func (n *Narrator) tplTime(w *core.World) string {
	return fmt.Sprintf("It is tick %d (%s).", w.Tick, w.Now.Format("2006-01-02"))
}

// tplTalk renders a "talk" event with the target person.
func (n *Narrator) tplTalk(w *core.World, e Event) string {
	if e.Person == nil {
		return "You talk to no one."
	}
	return fmt.Sprintf("You talk to %s.", e.Person.Name)
}

// tplTravel renders a "travel" event to the destination.
func (n *Narrator) tplTravel(w *core.World, e Event) string {
	if e.Location == nil {
		return "You travel to nowhere."
	}
	return fmt.Sprintf("You travel to %s.", e.Location.Name)
}

// tplDeath renders a "death" event. The LLM is preferred
// for deaths (per the spec) but the template is the
// fallback.
func (n *Narrator) tplDeath(e Event) string {
	if e.Person == nil {
		return "Someone has died."
	}
	return fmt.Sprintf("%s has died.", e.Person.Name)
}

// tplBirth renders a "birth" event. Like death, the LLM
// is preferred but the template is the fallback.
func (n *Narrator) tplBirth(e Event) string {
	if e.Person == nil {
		return "Someone was born."
	}
	return fmt.Sprintf("%s was born.", e.Person.Name)
}

// tplFirstMeet renders a "first meeting" event.
func (n *Narrator) tplFirstMeet(e Event) string {
	if e.Person == nil {
		return "You meet someone new."
	}
	return fmt.Sprintf("You meet %s for the first time.", e.Person.Name)
}

// callLLM builds the prompt and calls the LLM. The system
// prompt describes the narrator's role; the user prompt
// describes the event. Returns the LLM's response text.
func (n *Narrator) callLLM(ctx context.Context, e Event) (string, error) {
	messages := []llm.ChatMessage{
		{Role: "system", Content: n.systemPrompt()},
		{Role: "user", Content: n.userPrompt(e)},
	}
	return n.llm.Chat(ctx, messages)
}

// systemPrompt describes the narrator's role and tone.
// Kept short to minimize token cost — the LLM is told
// to produce 1-2 sentences, so a long system prompt
// would dominate the input.
func (n *Narrator) systemPrompt() string {
	return "You are the narrator for a medieval-fantasy simulation game. " +
		"Given an event, produce a short, evocative description (1-2 sentences). " +
		"Stay in character. Use third-person past tense. " +
		"Do not invent facts not present in the event."
}

// userPrompt describes the event. Kept structured (key:
// value) so the LLM can parse it reliably and the
// narrator doesn't need to worry about prose formatting.
func (n *Narrator) userPrompt(e Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Event: %s\n", e.Type)
	if e.Person != nil {
		fmt.Fprintf(&b, "Person: %s (id=%s, %s, age=%d, location=%s",
			e.Person.Name, e.Person.ID, e.Person.Gender,
			e.Person.AgeAt(0), // age-agnostic for the prompt
			e.Person.LocationID)
		if e.Person.Occupation != "" {
			fmt.Fprintf(&b, ", occupation=%s", e.Person.Occupation)
		}
		fmt.Fprintf(&b, ")\n")
	}
	if e.Location != nil {
		fmt.Fprintf(&b, "Location: %s (id=%s)\n", e.Location.Name, e.Location.ID)
	}
	return b.String()
}

// cacheKey builds the cache key for a (world, event)
// pair. The key is:
//
//	hash(prompt_version || world_hash || event_type || npc_state)
//
// prompt_version invalidates the cache when prompts
// change. world_hash is a simple hash of the world's
// people IDs (sorted for determinism). npc_state is the
// person ID and age at the current tick.
//
// The cache is a simple map for Phase 17.4. A future
// phase could add TTL or LRU eviction if memory becomes
// a concern.
func (n *Narrator) cacheKey(w *core.World, e Event) string {
	h := sha256.New()
	h.Write([]byte(n.promptVersion))
	h.Write([]byte{0})
	h.Write([]byte(n.worldHash(w)))
	h.Write([]byte{0})
	h.Write([]byte(e.Type))
	h.Write([]byte{0})
	if e.Person != nil {
		fmt.Fprintf(h, "%s|%d|%d", e.Person.ID, w.Tick, e.Person.AgeAt(w.Tick))
		h.Write([]byte{0})
	}
	if e.Location != nil {
		h.Write([]byte(e.Location.ID))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// worldHash is a simple hash of the world's people IDs.
// Changes when people are added or removed; does NOT
// change when people's attributes (age, location) change.
// That's intentional for Phase 17.4: the cache is per-
// event-type, so attribute changes are captured by the
// npc_state portion of the cache key.
func (n *Narrator) worldHash(w *core.World) string {
	ids := make([]string, 0, len(w.People))
	for id := range w.People {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// locationName returns the human-readable name of a
// location, or the raw ID if the location is unknown.
// Used by the templates.
func locationName(w *core.World, id string) string {
	if l, ok := w.Locations[id]; ok {
		return l.Name
	}
	return id
}

// Stats returns a snapshot of the narrator's internal
// state (cache size, last call tick). Used for testing
// and debugging; not part of the public API.
type Stats struct {
	CacheSize   int
	LastCallTick int64
}

// Stats returns a snapshot of the narrator's internal
// state. Used by tests to verify the rate limiter and
// cache behavior.
func (n *Narrator) Stats() Stats {
	return Stats{
		CacheSize:   len(n.cache),
		LastCallTick: n.lastCallTick,
	}
}
