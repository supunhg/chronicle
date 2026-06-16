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
	return "You are the narrator of an immersive medieval fantasy text adventure set in The Free Marches, a frontier region. " +
		"The player IS their character — they live, breathe, and experience this world firsthand. " +
		"Given an event, produce a vivid, sensory description (2-4 sentences). " +
		"Use second person (\"You see...\", \"You hear...\"). Describe sights, sounds, smells, textures, and the feel of the moment. " +
		"Stay in character. Never break the fourth wall. Do not mention game mechanics, ticks, or numbers. " +
		"The world should feel alive, dangerous, beautiful, and real."
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

// --- Immersive text adventure additions ---

// DescribeScene generates an atmospheric, first-person description
// of the player's current location. This is the core of the
// immersive experience — it builds rich context from the world
// state (location details, NPCs present, their activities,
// relationships with the player, time of day, recent events) and
// asks the LLM to produce evocative prose. Falls back to a rich
// template when the LLM is unavailable.
func (n *Narrator) DescribeScene(ctx context.Context, w *core.World) string {
	if w == nil || w.PlayerID == "" {
		return "You look around, unsure of where you are."
	}
	player, ok := w.People[w.PlayerID]
	if !ok || !player.Alive {
		return "You try to look around, but something feels wrong."
	}
	loc, lok := w.Locations[player.LocationID]
	if !lok {
		return "You look around, but can't make out your surroundings."
	}
	people := w.LivingPeopleAt(player.LocationID)

	// Try LLM for rich atmospheric description
	if n.llm != nil {
		text, err := n.callSceneLLM(ctx, w, player, loc, people)
		if err == nil && strings.TrimSpace(text) != "" {
			return text
		}
	}

	// Rich template fallback
	return n.sceneTemplate(w, player, loc, people)
}

// callSceneLLM builds a rich scene description prompt and calls
// the LLM. The prompt includes the location, all NPCs with their
// activities and relationships to the player, time context, and
// recent world events.
func (n *Narrator) callSceneLLM(ctx context.Context, w *core.World, player *core.Person, loc *core.Location, people []*core.Person) (string, error) {
	sysPrompt := "You are the narrator of an immersive medieval text adventure set in The Free Marches, a frontier region. " +
		"The player is exploring the world in first person. " +
		"Write in second person (\"You see...\", \"You notice...\"). " +
		"Be atmospheric and evocative — describe sights, sounds, smells, and the feel of the place. " +
		"Include the people nearby with brief descriptions of what they're doing and how they relate to the player. " +
		"Keep it to one vivid paragraph (3-6 sentences). " +
		"Do not include meta-commentary, game instructions, or break the fourth wall."

	var userPrompt strings.Builder
	fmt.Fprintf(&userPrompt, "The player (%s, a %s, age %d) is at %s.\n\n", player.Name, player.Occupation, player.AgeAt(w.Tick), loc.Name)
	fmt.Fprintf(&userPrompt, "Location details:\n")
	fmt.Fprintf(&userPrompt, "  Kind: %s\n", loc.Region)
	fmt.Fprintf(&userPrompt, "  Population: %d / %d\n", loc.Population, loc.PopulationCap)
	if loc.IsOvercrowded() {
		userPrompt.WriteString("  The settlement feels crowded.\n")
	}

	// Time context
	season := SeasonFromTick(w.Tick)
	timeDesc := TimeOfDayFromTick(w.Tick)
	fmt.Fprintf(&userPrompt, "\nTime: %s, %s\n", timeDesc, season)

	// NPCs present
	if len(people) > 0 {
		fmt.Fprintf(&userPrompt, "\nPeople nearby:\n")
		for _, p := range people {
			if p.ID == player.ID {
				continue
			}
			activity := NPCActivity(p, w.Tick)
			rel := "stranger"
			for _, r := range w.Relationships {
				if r.FromID == player.ID && r.ToID == p.ID {
					rel = RelationshipSummary(r)
					break
				}
			}
			fmt.Fprintf(&userPrompt, "  - %s, %s (age %d, %s) — %s. Your relationship: %s.\n",
				p.Name, p.Occupation, p.AgeAt(w.Tick), p.Gender, activity, rel)
		}
	}

	// Recent events at this location
	var recentEvents []string
	for i := len(w.Events) - 1; i >= 0 && len(recentEvents) < 3; i-- {
		ev := w.Events[i]
		if ev.Location == loc.ID {
			recentEvents = append(recentEvents, fmt.Sprintf("[%s]", ev.Type))
		}
	}
	if len(recentEvents) > 0 {
		fmt.Fprintf(&userPrompt, "\nRecent events here:\n")
		for _, ev := range recentEvents {
			fmt.Fprintf(&userPrompt, "  - %s\n", ev)
		}
	}

	messages := []llm.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userPrompt.String()},
	}
	return n.llm.Chat(ctx, messages)
}

// sceneTemplate is the rich template fallback for scene descriptions
// when the LLM is unavailable. It produces a much richer output than
// the old tplLook.
func (n *Narrator) sceneTemplate(w *core.World, player *core.Person, loc *core.Location, people []*core.Person) string {
	var b strings.Builder

	// Location header with atmosphere
	season := SeasonFromTick(w.Tick)
	timeDesc := TimeOfDayFromTick(w.Tick)
	fmt.Fprintf(&b, "\n  %s — %s, %s\n\n", loc.Name, timeDesc, season)

	// Location description based on kind
	switch loc.ID {
	case "blackwater":
		b.WriteString("The bustling town of Blackwater stretches before you. Cobblestone streets wind between ")
		b.WriteString("timber-framed buildings, and the sounds of commerce fill the air. The market square ")
		b.WriteString("bustles with activity, while smoke rises lazily from the smithy's chimney.\n")
	default:
		fmt.Fprintf(&b, "You stand in %s, a %s in the Free Marches. ", loc.Name, loc.Region)
		if loc.Population > loc.PopulationCap/2 {
			b.WriteString("The settlement is lively with activity.\n")
		} else {
			b.WriteString("It is quiet here.\n")
		}
	}

	// People nearby
	var others []*core.Person
	for _, p := range people {
		if p.ID != player.ID {
			others = append(others, p)
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "\n  People nearby (%d):\n", len(others))
		for _, p := range others {
			activity := NPCActivity(p, w.Tick)
			rel := ""
			for _, r := range w.Relationships {
				if r.FromID == player.ID && r.ToID == p.ID {
					if r.Trust > 60 {
						rel = " [you trust them]"
					} else if r.Trust < 30 {
						rel = " [you distrust them]"
					}
					break
				}
			}
			fmt.Fprintf(&b, "    %s (%s, %d) — %s%s\n", p.Name, p.Occupation, p.AgeAt(w.Tick), activity, rel)
		}
	} else {
		b.WriteString("\n  You are alone here.\n")
	}

	// Earnings potential hint
	if player.Occupation != "" {
		fmt.Fprintf(&b, "\n  As a %s, you could find work here.\n", player.Occupation)
	}

	return b.String()
}

// DescribePerson generates an atmospheric description of a specific
// NPC the player is looking at. Includes their appearance, apparent
// mood, activity, and relationship context.
func (n *Narrator) DescribePerson(ctx context.Context, w *core.World, target *core.Person) string {
	if target == nil {
		return "You don't see anyone by that name."
	}

	// Try LLM
	if n.llm != nil {
		var b strings.Builder
		player := w.People[w.PlayerID]
		rel := "a stranger"
		if player != nil {
			for _, r := range w.Relationships {
				if r.FromID == player.ID && r.ToID == target.ID {
					rel = RelationshipSummary(r)
					break
				}
			}
		}
		sysPrompt := "You are the narrator of an immersive medieval text adventure. Describe a person the player is observing. " +
			"Write in second person. Be vivid but brief (2-4 sentences). Describe their appearance, apparent mood, and what they're doing. " +
			"Include their relationship to the player if relevant. Do not break the fourth wall."
		fmt.Fprintf(&b, "The player is looking at: %s\n", target.Name)
		fmt.Fprintf(&b, "Age: %d, Gender: %s, Occupation: %s, Class: %s\n", target.AgeAt(w.Tick), target.Gender, target.Occupation, target.Class)
		fmt.Fprintf(&b, "Activity: %s\n", NPCActivity(target, w.Tick))
		fmt.Fprintf(&b, "Relationship to player: %s\n", rel)
		if len(target.Needs) > 0 {
			fmt.Fprintf(&b, "Needs: %v\n", target.Needs)
		}

		messages := []llm.ChatMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: b.String()},
		}
		text, err := n.llm.Chat(ctx, messages)
		if err == nil && strings.TrimSpace(text) != "" {
			return text
		}
	}

	// Template fallback
	return PersonTemplate(w, target)
}

// DescribeWalk generates an atmospheric walk narrative with terrain,
// encounters, weather, and seasonal details. This is the immersive
// version of travel narration — it describes what the player sees,
// hears, and experiences along the way. distMod controls the length
// and detail: 0 = short stroll, 1 = good walk, 2 = long trek.
func (n *Narrator) DescribeWalk(ctx context.Context, w *core.World, from, to *core.Location, distMod int) string {
	if w == nil || from == nil || to == nil {
		return "You walk for a while."
	}
	player, _ := w.People[w.PlayerID]
	if player == nil {
		return fmt.Sprintf("You walk from %s to %s through the frontier landscape.", from.Name, to.Name)
	}

	// Try LLM for rich walk description
	if n.llm != nil {
		text, err := n.callWalkLLM(ctx, w, player, from, to, distMod)
		if err == nil && strings.TrimSpace(text) != "" {
			return text
		}
	}

	// Rich template fallback
	return n.walkTemplate(w, player, from, to, distMod)
}

// callWalkLLM builds a walk-specific prompt with terrain, encounters,
// weather, and seasonal context, then calls the LLM.
func (n *Narrator) callWalkLLM(ctx context.Context, w *core.World, player *core.Person, from, to *core.Location, distMod int) (string, error) {
	distanceDesc := "a good walk"
	switch distMod {
	case 0:
		distanceDesc = "a short stroll"
	case 2:
		distanceDesc = "a long trek"
	}

	sysPrompt := "You are the narrator of an immersive medieval text adventure set in The Free Marches, a frontier region. " +
		"The player is walking between settlements. Write in second person (\"You set out...\", \"The path winds...\"). " +
		"Describe the journey vividly — terrain changes, weather, sounds, smells, wildlife encounters, other travelers on the road. " +
		"Include at least one small encounter or observation (a bird of prey overhead, a ruined waymarker, a distant campfire, a merchant's cart). " +
		"Match the tone to the distance: a stroll is brief and gentle, a walk is steady and observant, a trek is arduous and eventful. " +
		"Keep it to one vivid paragraph (3-6 sentences). Do not break the fourth wall or include game mechanics."

	var userPrompt strings.Builder
	fmt.Fprintf(&userPrompt, "The player (%s) is walking from %s to %s — %s.\n\n", player.Name, from.Name, to.Name, distanceDesc)
	fmt.Fprintf(&userPrompt, "Departure: %s (%s, population %d)\n", from.Name, from.Region, from.Population)
	fmt.Fprintf(&userPrompt, "Destination: %s (%s, population %d)\n\n", to.Name, to.Region, to.Population)

	// Season and time context
	season := SeasonFromTick(w.Tick)
	timeDesc := TimeOfDayFromTick(w.Tick)
	fmt.Fprintf(&userPrompt, "Season: %s\n", season)
	fmt.Fprintf(&userPrompt, "Time of day: %s\n\n", timeDesc)

	// Weather hints based on season
	switch season {
	case "Spring":
		userPrompt.WriteString("Weather: mild, occasional rain showers, new growth along the path\n")
	case "Summer":
		userPrompt.WriteString("Weather: warm, dusty roads, long daylight hours\n")
	case "Autumn":
		userPrompt.WriteString("Weather: cool, falling leaves, misty mornings\n")
	case "Winter":
		userPrompt.WriteString("Weather: cold, bare trees, possible frost or snow\n")
	}

	// Other travelers on the road (people from the world who are moving)
	var travelers []string
	for _, p := range w.People {
		if p.ID == player.ID || !p.Alive {
			continue
		}
		if p.LocationID == to.ID || p.LocationID == from.ID {
			travelers = append(travelers, fmt.Sprintf("%s (%s)", p.Name, p.Occupation))
		}
	}
	if len(travelers) > 0 && len(travelers) <= 5 {
		fmt.Fprintf(&userPrompt, "\nPeople who live near the route: %s\n", strings.Join(travelers, ", "))
	}

	messages := []llm.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userPrompt.String()},
	}
	return n.llm.Chat(ctx, messages)
}

// walkTemplate is the rich template fallback for walk descriptions.
// It varies based on season, distance, and departure/destination.
func (n *Narrator) walkTemplate(w *core.World, player *core.Person, from, to *core.Location, distMod int) string {
	var b strings.Builder
	season := SeasonFromTick(w.Tick)
	timeDesc := TimeOfDayFromTick(w.Tick)

	// Opening — varies by distance
	switch distMod {
	case 0:
		fmt.Fprintf(&b, "You step out from %s for a short stroll in the %s air. The path is well-trodden, and %s lies just ahead. ", from.Name, strings.ToLower(season), to.Name)
	case 2:
		fmt.Fprintf(&b, "You shoulder your pack and set out from %s on the long road to %s. It is %s, %s — the journey will take the better part of a day or more. ", from.Name, to.Name, timeDesc, strings.ToLower(season))
	default:
		fmt.Fprintf(&b, "You set out from %s along the path toward %s. The %s air greets you as you walk. ", from.Name, to.Name, strings.ToLower(season))
	}

	// Terrain — based on season and location type
	terrains := map[string][]string{
		"Spring": {
			"wildflowers dot the roadside, and birdsong fills the air",
			"the path is soft with recent rain, and new leaves unfurl overhead",
			"a gentle breeze carries the scent of blossoms from nearby meadows",
		},
		"Summer": {
			"the sun beats down on the dusty road, and cicadas hum in the grass",
			"heat shimmers rise from the path, and you wipe sweat from your brow",
			"the road stretches ahead through golden fields of wheat",
		},
		"Autumn": {
			"crunching through fallen leaves as mist clings to the hollows",
			"the trees blaze with red and gold, and the air smells of woodsmoke",
			"a chill wind tugs at your cloak as you walk through drifts of amber leaves",
		},
		"Winter": {
			"your breath mists in the cold air, and frost crunches underfoot",
			"bare branches claw at a grey sky, and the road is hard with ice",
			"you pull your cloak tight against the biting wind as snowflakes begin to fall",
		},
	}
	hash := int(w.Tick) + len(player.ID)*7
	if options, ok := terrains[season]; ok {
		b.WriteString(options[hash%len(options)])
		b.WriteString(". ")
	}

	// Encounter — a small observation on the road
	encounters := []string{
		"A hawk circles overhead, riding the thermals.",
		"You pass a weathered waymarker, its carvings worn smooth by years of rain.",
		"A distant plume of smoke rises from somewhere beyond the tree line.",
		"You hear the creak of a cart wheel — a merchant heading the other way nods as he passes.",
		"A fox darts across the path ahead, disappearing into the undergrowth.",
		"The ruins of an old stone wall line the road for a stretch, half-consumed by ivy.",
		"You stop to drink from a clear stream that crosses the path, its water icy cold.",
		"The distant sound of a bell carries on the wind — perhaps from a monastery or chapel.",
		"A crow watches you from a fence post, head tilted with curiosity.",
		"You find a cluster of wild mushrooms growing at the base of an old oak.",
	}
	b.WriteString(encounters[(hash+3)%len(encounters)])
	b.WriteString(" ")

	// Closing — arrival at destination
	if distMod >= 2 {
		fmt.Fprintf(&b, "By the time %s comes into view, your legs ache but the sight lifts your spirits.", to.Name)
	} else if distMod == 0 {
		fmt.Fprintf(&b, "%s is close enough that you arrive without breaking a sweat.", to.Name)
	} else {
		fmt.Fprintf(&b, "Before long, the rooftops of %s appear through the %s haze.", to.Name, strings.ToLower(season))
	}

	return b.String()
}

// DescribeBuilding generates an atmospheric description of walking to
// a building within a settlement. LLM-first with rich context about
// the building type, settlement, and NPCs who might be there.
func (n *Narrator) DescribeBuilding(ctx context.Context, w *core.World, building string, loc *core.Location) string {
	if w == nil || loc == nil || building == "" {
		return "You walk to a nearby building."
	}
	player, _ := w.People[w.PlayerID]

	// Try LLM for rich building description
	if n.llm != nil {
		sysPrompt := "You are the narrator of an immersive medieval text adventure set in The Free Marches. " +
			"The player is walking to a building within a settlement. Write in second person. " +
			"Describe what they see as they approach and enter — sights, sounds, smells, the people inside. " +
			"Keep it to one vivid paragraph (2-4 sentences). Do not break the fourth wall."

		var userPrompt strings.Builder
		fmt.Fprintf(&userPrompt, "The player walks to the %s in %s.\n", building, loc.Name)
		fmt.Fprintf(&userPrompt, "Settlement: %s (%s, population %d)\n", loc.Name, loc.Region, loc.Population)

		season := SeasonFromTick(w.Tick)
		timeDesc := TimeOfDayFromTick(w.Tick)
		fmt.Fprintf(&userPrompt, "Time: %s, %s\n", timeDesc, season)

		// Who's at this building? People with matching occupations
		var occupants []string
		for _, p := range w.LivingPeopleAt(loc.ID) {
			if player != nil && p.ID == player.ID {
				continue
			}
			activity := NPCActivity(p, w.Tick)
			occupants = append(occupants, fmt.Sprintf("%s (%s, %s)", p.Name, p.Occupation, activity))
		}
		if len(occupants) > 3 {
			occupants = occupants[:3]
		}
		if len(occupants) > 0 {
			fmt.Fprintf(&userPrompt, "People nearby: %s\n", strings.Join(occupants, ", "))
		}

		messages := []llm.ChatMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt.String()},
		}
		text, err := n.llm.Chat(ctx, messages)
		if err == nil && strings.TrimSpace(text) != "" {
			return text
		}
	}

	// Rich template fallback based on building type
	return n.buildingTemplate(w, building, loc)
}

// buildingTemplate provides rich template descriptions for common
// building types. Each building has a unique atmospheric description.
func (n *Narrator) buildingTemplate(w *core.World, building string, loc *core.Location) string {
	season := SeasonFromTick(w.Tick)
	lower := strings.ToLower(building)

	// Count people at this location
	popDesc := "quiet"
	pop := len(w.LivingPeopleAt(loc.ID))
	if pop > 10 {
		popDesc = "bustling with activity"
	} else if pop > 3 {
		popDesc = "moderately busy"
	}

	switch {
	case strings.Contains(lower, "inn"):
		return fmt.Sprintf("You push open the heavy door of the %s and step inside. The common room is %s — " +
			"warm firelight dances across worn wooden tables, and the smell of roasting meat mingles with ale and pipe smoke. " +
			"A hearth crackles in the corner, warding off the %s chill.", building, popDesc, strings.ToLower(season))
	case strings.Contains(lower, "market"):
		return fmt.Sprintf("You make your way to the %s. Stalls and awnings fill the square, merchants calling out their wares — " +
			"fresh produce, bolts of cloth, iron tools. The air is thick with the sounds of haggling and the clink of coin. " +
			"It is %s and %s here.", building, TimeOfDayFromTick(w.Tick), popDesc)
	case strings.Contains(lower, "smithy") || strings.Contains(lower, "forge") || strings.Contains(lower, "armory"):
		return fmt.Sprintf("You approach the %s. Heat radiates from the open forge, and the rhythmic ring of hammer on anvil fills the air. " +
			"Sparks fly as metal is shaped, and the sharp smell of hot iron catches in your throat. The smith nods briefly without looking up.", building)
	case strings.Contains(lower, "bakery"):
		return fmt.Sprintf("You walk to the %s. The warm, yeasty smell of fresh bread wafts through the doorway. " +
			"Loaves cool on racks inside, golden-crusted and still steaming. A flour-dusted figure works the ovens in the back.", building)
	case strings.Contains(lower, "temple") || strings.Contains(lower, "shrine") || strings.Contains(lower, "chapel") || strings.Contains(lower, "monastery"):
		return fmt.Sprintf("You enter the %s. Candles flicker in alcoves, casting dancing shadows across stone walls. " +
			"The air is heavy with incense, and a hushed reverence fills the space. Offerings of flowers and coins line the altar.", building)
	case strings.Contains(lower, "hall") || strings.Contains(lower, "tavern") || strings.Contains(lower, "lodge"):
		return fmt.Sprintf("You walk to the %s. The building stands at the heart of %s — broad timber doors, a flagstone floor, " +
			"and the faded banners of old allegiances hanging from the rafters. %s.", building, loc.Name, capitalizeFirst(popDesc))
	case strings.Contains(lower, "stable"):
		return fmt.Sprintf("You head to the %s. The earthy smell of hay and horse greets you before you reach the door. " +
			"A few animals stamp and snort in their stalls, and a stablehand leans against a post, idly tossing feed.", building)
	case strings.Contains(lower, "guild"):
		return fmt.Sprintf("You approach the %s. The building is well-maintained, with a carved wooden sign above the door. " +
			"Inside, ledgers and notices line the walls, and the %s hum of trade talk fills the room.", building, popDesc)
	default:
		return fmt.Sprintf("You walk to the %s in %s. The %s air of %s greets you as you arrive.",
			building, loc.Name, strings.ToLower(season), TimeOfDayFromTick(w.Tick))
	}
}

// capitalizeFirst capitalizes the first letter of a string.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// DescribeJourney generates an atmospheric travel narrative.
func (n *Narrator) DescribeJourney(ctx context.Context, w *core.World, from, to *core.Location) string {
	if n.llm != nil {
		sysPrompt := "You are the narrator of an immersive medieval text adventure. Describe a journey between two locations in The Free Marches. " +
			"Write in second person. Be atmospheric (2-4 sentences). Describe the road, landscape, and feeling of travel. Do not break the fourth wall."
		var userPrompt strings.Builder
		fmt.Fprintf(&userPrompt, "Traveling from %s to %s.\n", from.Name, to.Name)
		fmt.Fprintf(&userPrompt, "Region: %s\n", from.Region)

		messages := []llm.ChatMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt.String()},
		}
		text, err := n.llm.Chat(ctx, messages)
		if err == nil && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return fmt.Sprintf("You set out from %s and make your way toward %s. The road stretches ahead through the frontier landscape.", from.Name, to.Name)
}

// NPCActivity returns a short description of what an NPC is doing,
// based on their occupation, needs, and the time context. This makes
// the world feel alive when the player looks around.
func NPCActivity(p *core.Person, tick int64) string {
	if !p.Alive {
		return "deceased"
	}
	hash := int(tick) + len(p.ID)*7

	if hunger, ok := p.Needs["hunger"]; ok && hunger < 20 {
		return "looking for something to eat"
	}
	if safety, ok := p.Needs["safety"]; ok && safety < 20 {
		return "glancing around nervously"
	}

	switch p.Occupation {
	case "farmer":
		activities := []string{"tending the fields", "checking on the crops", "mending a fence", "wiping sweat from their brow", "carrying a bundle of hay"}
		return activities[hash%len(activities)]
	case "merchant", "trader":
		activities := []string{"haggling with a customer", "arranging goods on a stall", "counting coins", "examining a ledger", "calling out prices"}
		return activities[hash%len(activities)]
	case "blacksmith":
		activities := []string{"hammering at the forge", "quenching a blade in water", "stoking the coals", "inspecting a piece of work", "wiping soot from their face"}
		return activities[hash%len(activities)]
	case "innkeeper":
		activities := []string{"wiping down the bar", "pouring a drink", "chatting with a patron", "carrying a tray of food", "sweeping the floor"}
		return activities[hash%len(activities)]
	case "guard":
		activities := []string{"standing watch", "patrolling the area", "leaning on their spear", "surveying the crowd", "sharpening their blade"}
		return activities[hash%len(activities)]
	case "priest":
		activities := []string{"muttering a prayer", "reading from a worn book", "offering blessings", "tending to the shrine", "speaking softly to a follower"}
		return activities[hash%len(activities)]
	case "baker":
		activities := []string{"pulling bread from the oven", "kneading dough", "dusting flour from their apron", "arranging loaves on a rack"}
		return activities[hash%len(activities)]
	case "carpenter":
		activities := []string{"sanding a piece of wood", "measuring planks", "hammering nails", "applying varnish", "inspecting a joint"}
		return activities[hash%len(activities)]
	case "hunter":
		activities := []string{"cleaning a catch", "stringing a bow", "studying tracks in the dirt", "skinning a rabbit", "adjusting their pack"}
		return activities[hash%len(activities)]
	case "teacher":
		activities := []string{"lecturing to a small group", "writing on a slate", "reading aloud", "correcting a student", "organizing papers"}
		return activities[hash%len(activities)]
	case "mayor":
		activities := []string{"discussing town matters", "reviewing documents", "meeting with officials", "addressing a small crowd", "signing a decree"}
		return activities[hash%len(activities)]
	case "miner":
		activities := []string{"resting with a pickaxe", "wiping dust from their eyes", "examining a rock sample", "hefting a sack of ore"}
		return activities[hash%len(activities)]
	case "weaver":
		activities := []string{"threading a loom", "examining cloth", "cutting fabric", "sorting dyed thread"}
		return activities[hash%len(activities)]
	case "laborer":
		activities := []string{"hauling supplies", "resting against a wall", "wiping their brow", "carrying tools", "stretching their back"}
		return activities[hash%len(activities)]
	case "clerk":
		activities := []string{"scribbling in a ledger", "sorting papers", "filing documents", "dipping a quill in ink"}
		return activities[hash%len(activities)]
	case "miller":
		activities := []string{"checking the millstone", "sacking flour", "examining grain", "oiling the gears"}
		return activities[hash%len(activities)]
	case "woodcutter":
		activities := []string{"splitting logs", "stacking firewood", "sharpening an axe", "wiping sawdust from their eyes"}
		return activities[hash%len(activities)]
	}

	generic := []string{"going about their day", "resting quietly", "looking around", "talking softly to someone", "sitting in thought"}
	return generic[hash%len(generic)]
}

// RelationshipSummary returns a brief description of the relationship
// between two people, based on the 5-axis relationship data.
func RelationshipSummary(r core.Relationship) string {
	if r.Trust > 70 && r.Respect > 60 {
		return "trusted friend"
	}
	if r.Trust > 60 && r.Attraction > 60 {
		return "someone you're fond of"
	}
	if r.Trust > 50 {
		return "acquaintance"
	}
	if r.Fear > 60 {
		return "someone you fear"
	}
	if r.Trust < 30 && r.Respect < 30 {
		return "someone you distrust"
	}
	if r.Loyalty > 60 {
		return "loyal ally"
	}
	return "acquaintance"
}

// PersonTemplate produces a rich template description of a person
// for the LLM fallback.
func PersonTemplate(w *core.World, p *core.Person) string {
	var b strings.Builder
	age := p.AgeAt(w.Tick)
	status := "alive"
	if !p.Alive {
		status = "deceased"
	}
	loc := "unknown"
	if l, ok := w.Locations[p.LocationID]; ok {
		loc = l.Name
	}
	activity := NPCActivity(p, w.Tick)

	fmt.Fprintf(&b, "\n  %s (%s)\n\n", p.Name, status)
	fmt.Fprintf(&b, "  %s, age %d", p.Gender, age)
	if p.Occupation != "" {
		fmt.Fprintf(&b, ", %s", p.Occupation)
	}
	fmt.Fprintf(&b, ", %s class\n", p.Class)
	fmt.Fprintf(&b, "  Location: %s\n", loc)
	fmt.Fprintf(&b, "  Currently: %s\n", activity)
	if p.SpouseID != "" {
		if spouse, ok := w.People[p.SpouseID]; ok {
			fmt.Fprintf(&b, "  Spouse: %s\n", spouse.Name)
		}
	}
	if p.FatherID != "" {
		if father, ok := w.People[p.FatherID]; ok {
			fmt.Fprintf(&b, "  Father: %s\n", father.Name)
		}
	}
	if p.MotherID != "" {
		if mother, ok := w.People[p.MotherID]; ok {
			fmt.Fprintf(&b, "  Mother: %s\n", mother.Name)
		}
	}
	return b.String()
}

// SeasonFromTick returns a season name based on the tick.
func SeasonFromTick(tick int64) string {
	day := tick % 365
	switch {
	case day < 90:
		return "Spring"
	case day < 180:
		return "Summer"
	case day < 270:
		return "Autumn"
	default:
		return "Winter"
	}
}

// TimeOfDayFromTick returns a time-of-day description. Since the
// simulation ticks in days, we use the tick value to deterministically
// assign a time of day for atmospheric variety.
func TimeOfDayFromTick(tick int64) string {
	phases := []string{"early morning", "morning", "midday", "afternoon", "evening", "night"}
	return phases[int(tick)%len(phases)]
}
