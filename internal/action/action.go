// Package action resolves parsed Intents into world mutations
// and narrative text. It is the bridge between the REPL
// (Phase 17.3), the intent parser (Phase 17.2), and the
// narrator (Phase 17.4).
//
// Design:
//
//  1. The action engine is a pure function over (world, intent,
//     narrator). It mutates the world in place (matching the
//     existing tick.Simulation pattern) and returns a Result
//     containing the rendered text and a record of what changed.
//
//  2. Each action has a handler method (resolveTalk,
//     resolveTravel, etc.) that knows the action's world-mutation
//     semantics. Handlers for "look" and "inspect" are read-only;
//     handlers for "talk", "travel", "buy", "sell", and "save"
//     mutate state.
//
//  3. The engine delegates text rendering to the Narrator
//     (Phase 17.4). The engine's job is to decide WHEN to call
//     the Narrator, WHAT event to pass, and HOW to mutate the
//     world around that narration.
//
//  4. Branch/switch are stubs that return a clear "not yet
//     implemented" Result. These will be wired in Phase 19
//     (timelines).
//
// Phase 17.6 scope:
//
//   - save: writes the world to a SQLite snapshot via the
//     persistence layer; default path is <world-id>.db
//   - buy: increases the player's inventory, decreases Coin
//   - sell: decreases the player's inventory, increases Coin
//   - inventory: now reads the player's actual inventory
//     (was a "You have nothing." stub)
//
// Economy v1 (Phase 17.6): a fixed price list for common
// goods. Phase 18+ will read prices from the worldpack's
// EconomySpec. The buy/sell handlers are intentionally
// simple — they validate the transaction, mutate Coin and
// Inventory, and return a result. No price discovery, no
// merchant NPCs, no negotiation.
package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
	"github.com/chronicle-dev/chronicle/internal/narrator"
	"github.com/chronicle-dev/chronicle/internal/persistence"
)

// Result is the outcome of resolving an Intent. The REPL
// prints Text to the player; TicksAdvanced tells the REPL
// how many simulation ticks the action consumed (so the
// REPL can keep its display in sync with the world's
// Tick counter).
type Result struct {
	// Text is the narrative to show the player. Already
	// rendered (template or LLM); the REPL just prints it.
	Text string

	// TicksAdvanced is the number of simulation ticks the
	// action consumed. Travel advances 1 (a day on the
	// road); sleep advances hours/24; talk advances 0 (a
	// brief exchange). The REPL uses this for display
	// ("You travel to X (tick N+1)") and for time-pressure
	// tracking.
	TicksAdvanced int64

	// OK reports whether the action succeeded. False means
	// the action was rejected (unknown target, no player,
	// etc.) and Text contains the reason. The REPL
	// surfaces this as an error to the player.
	OK bool
}

// Engine resolves Intents into Results. It holds a reference
// to the world (mutated in place) and an optional Narrator.
// The zero value is not useful; construct via New.
//
// Engine is NOT safe for concurrent use. The current caller
// (the REPL) is single-threaded, matching the Narrator's
// concurrency contract.
//
// Phase 31: tickFn is the per-tick callback (typically
// tick.Simulation.Tick on the same world). When set, every
// call to advanceTick(n) invokes it n times — running the
// full engine pipeline (population/relationship/goal/memory
// + economy + event) per tick, so player actions like
// travel and sleep trigger the same world evolution as a
// tick from runPlay. When nil, advanceTick falls back to a
// clock-only update (Tick and Now advanced, no engine
// side-effects) — useful for tests and for code paths that
// want time advancement without engine side-effects.
type Engine struct {
	world    *core.World
	narrator *narrator.Narrator
	tickFn   func() error
}

// New constructs an Action Engine. The world is required and
// is mutated in place as actions resolve. The narrator is
// optional; when nil, the engine uses short template strings
// for all narration (no LLM calls, no cache). To enable
// full-pipeline time advancement (Phase 31), call SetTickFn
// with the same function the REPL uses for auto-tick.
func New(w *core.World, n *narrator.Narrator) *Engine {
	return &Engine{world: w, narrator: n}
}

// SetTickFn wires the per-tick callback. Pass the same
// `func() error` that the REPL hands to its auto-tick loop
// (typically `func() error { return sim.Tick(w) }`). After
// this call, every advanceTick(n) inside the engine runs
// the full tick pipeline n times, mirroring the spec's
// "for each tick in the elapsed time, run full tick
// pipeline" (SIMULATION_TICK_SPEC.md §5.3). Calling with nil
// reverts to clock-only advancement.
func (e *Engine) SetTickFn(fn func() error) {
	e.tickFn = fn
}

// Resolve dispatches an Intent to its per-action handler.
// Returns a Result describing what happened. Errors are
// reserved for truly fatal problems (nil world, unknown
// action); action-level rejections (target not found) are
// returned as Result{OK: false, Text: "..."} so the REPL
// can surface them as user-facing messages.
func (e *Engine) Resolve(ctx context.Context, in intent.Intent) (Result, error) {
	if e.world == nil {
		return Result{}, fmt.Errorf("action: world is nil")
	}
	switch in.Action {
	case intent.ActionLook:
		return e.resolveLook(in.Target), nil
	case intent.ActionInspect:
		return e.resolveInspect(in.Target), nil
	case intent.ActionTime:
		return e.resolveTime(), nil
	case intent.ActionInventory:
		return e.resolveInventory(), nil
	case intent.ActionTalk:
		return e.resolveTalk(ctx, in.Target), nil
	case intent.ActionTravel:
		return e.resolveTravel(ctx, in.Target), nil
	case intent.ActionSleep:
		return e.resolveSleep(in.Args.Hours), nil
	case intent.ActionSave:
		return e.resolveSave(in.Target), nil
	case intent.ActionBuy:
		return e.resolveBuy(in.Target, in.Args.Quantity), nil
	case intent.ActionSell:
		return e.resolveSell(in.Target, in.Args.Quantity), nil
	case intent.ActionBranch:
		return e.resolveBranch(in.Target), nil
	case intent.ActionSwitch:
		return e.resolveSwitch(in.Target), nil
	case intent.ActionListen:
		return e.resolveListen(), nil
	case intent.ActionWait:
		return e.resolveWait(in.Args.Hours), nil
	case intent.ActionWalk:
		distMod := in.Args.Hours // repurposed: 0=stroll, 1=walk, 2=trek
		if distMod <= 0 {
			distMod = 1
		}
		return e.resolveWalk(in.Target, distMod), nil
	case intent.ActionSearch:
		return e.resolveSearch(in.Target), nil
	case intent.ActionPray:
		return e.resolvePray(), nil
	case intent.ActionStatus:
		return e.resolveStatus(), nil
	default:
		return Result{}, fmt.Errorf("action: unknown action %q", in.Action)
	}
}

// resolveLook handles "look" — read-only, no world changes.
// With a target, it shows a person or location. Without a
// target, it generates an immersive scene description via
// the narrator's DescribeScene (LLM-first, template fallback).
func (e *Engine) resolveLook(target string) Result {
	if target == "" {
		// Immersive scene description
		if e.narrator != nil {
			text := e.narrator.DescribeScene(context.Background(), e.world)
			return Result{OK: true, Text: text}
		}
		return e.lookLocation("")
	}
	if p := e.findPerson(target); p != nil {
		// Immersive person description
		if e.narrator != nil {
			text := e.narrator.DescribePerson(context.Background(), e.world, p)
			return Result{OK: true, Text: text}
		}
		return e.lookPerson(p)
	}
	if l := e.findLocation(target); l != nil {
		return e.lookLocation(l.ID)
	}
	return Result{OK: false, Text: fmt.Sprintf("I don't see %q here.", target)}
}

// resolveInspect handles "inspect <name>" — read-only, shows
// a person's full details with immersive description.
func (e *Engine) resolveInspect(target string) Result {
	if strings.TrimSpace(target) == "" {
		return Result{OK: false, Text: "Inspect whom? (Usage: inspect <name>)"}
	}
	p := e.findPerson(target)
	if p == nil {
		return Result{OK: false, Text: fmt.Sprintf("I don't see %q here.", target)}
	}
	if e.narrator != nil {
		text := e.narrator.DescribePerson(context.Background(), e.world, p)
		return Result{OK: true, Text: text}
	}
	return e.lookPerson(p)
}

// resolveTime handles "time" — read-only, shows the current
// tick and simulated date.
func (e *Engine) resolveTime() Result {
	return Result{
		OK:   true,
		Text: fmt.Sprintf("It is tick %d (%s).", e.world.Tick, e.world.Now.Format("2006-01-02")),
	}
}

// resolveInventory shows the player's current inventory
// and coin balance. Phase 18: reads from the world's
// map[string]Item Inventory. Each stack shows name, count,
// weight, value, and durability. Sorted by name for
// deterministic output.
func (e *Engine) resolveInventory() Result {
	if len(e.world.Inventory) == 0 {
		return Result{OK: true, Text: fmt.Sprintf("You have nothing. (Coin: %d)", e.world.Coin)}
	}
	// Sort items by name for deterministic output.
	names := make([]string, 0, len(e.world.Inventory))
	for name := range e.world.Inventory {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "You are carrying (Coin: %d):\n", e.world.Coin)
	for _, name := range names {
		it := e.world.Inventory[name]
		fmt.Fprintf(&b, "  %s x%d (%.1fkg, %d coin, dur %.0f%%)\n",
			it.Name, it.Count, it.Weight, it.Value, it.MaxDurability*100)
	}
	return Result{OK: true, Text: b.String()}
}

// resolveTalk handles "talk <name>" — creates a memory
// record for the conversation and applies a small TrustDelta
// to the relationship. Narration is delegated to the Narrator.
// No time advancement (a brief exchange).
//
// The target must be a living person at the same location
// as the player (or anywhere, if no player is set — the
// REPL can be run in world-level mode).
func (e *Engine) resolveTalk(ctx context.Context, target string) Result {
	if strings.TrimSpace(target) == "" {
		return Result{OK: false, Text: "Talk to whom? (Usage: talk <name>)"}
	}
	p := e.findPerson(target)
	if p == nil {
		return Result{OK: false, Text: fmt.Sprintf("I don't see %q here.", target)}
	}
	if !p.Alive {
		return Result{OK: false, Text: fmt.Sprintf("%s is dead; you cannot talk to them.", p.Name)}
	}

	// Record the conversation as a memory. This is a
	// "chat" memory: importance 0.3, trust +2 (a small
	// positive nudge from a successful conversation).
	mem := core.Memory{
		ID:             fmt.Sprintf("mem-talk-%d-%s", e.world.Tick, p.ID),
		OwnerID:        playerID(e.world),
		EventID:        fmt.Sprintf("talk-%d-%s", e.world.Tick, p.ID),
		Tick:           e.world.Tick,
		Importance:     0.3,
		Recency:        1.0,
		EmotionalScore: 0.2,
		TrustDelta:     2.0,
		Description:    fmt.Sprintf("chatted with %s", p.Name),
		Tags:           []string{"talk"},
	}
	e.world.Memories = append(e.world.Memories, mem)

	// Apply the TrustDelta to the player→target
	// relationship. Uses the same O(1) path as
	// MemoryEngine.recordBirths. If no player is set,
	// the OwnerID is "" and the delta is a no-op.
	applyTalkDelta(e.world, mem, p.ID)

	// Narration: delegate to the Narrator if present.
	text := e.renderTalk(ctx, p)
	return Result{OK: true, Text: text, TicksAdvanced: 0}
}

// resolveTravel handles "travel <location>" — moves the
// player to the destination and advances time by 1 tick
// (a day on the road). Uses immersive journey narration
// when the narrator is available.
func (e *Engine) resolveTravel(ctx context.Context, target string) Result {
	l := e.findLocation(target)
	if l == nil {
		return Result{OK: false, Text: fmt.Sprintf("I don't know the location %q.", target)}
	}
	player := e.player()
	if player == nil {
		return Result{OK: false, Text: "You need a player character to travel. (Set Options.PlayerID.)"}
	}
	if player.LocationID == l.ID {
		return Result{OK: false, Text: fmt.Sprintf("You are already at %s.", l.Name)}
	}

	oldLocID := player.LocationID

	// Move the player and advance time by 1 tick.
	player.LocationID = l.ID
	e.advanceTick(1)

	// Record the travel as a memory.
	mem := core.Memory{
		ID:             fmt.Sprintf("mem-travel-%d-%s", e.world.Tick, player.ID),
		OwnerID:        player.ID,
		EventID:        fmt.Sprintf("travel-%d-%s", e.world.Tick, l.ID),
		Tick:           e.world.Tick,
		Importance:     0.1,
		Recency:        1.0,
		EmotionalScore: 0.0,
		Description:    fmt.Sprintf("traveled from %s to %s", locationNameOrID(e.world, oldLocID), l.Name),
		Tags:           []string{"travel"},
	}
	e.world.Memories = append(e.world.Memories, mem)

	// Narration: delegate to the Narrator if present.
	text := e.renderTravel(ctx, l)
	return Result{OK: true, Text: text + fmt.Sprintf(" (1 day elapsed, now tick %d).", e.world.Tick), TicksAdvanced: 1}
}

// resolveSleep handles "sleep" — advances time by
// hours/24 ticks (rounded up to at least 1). Hours is
// clamped to a week (7*24) to prevent a typo or malicious
// input from spinning the loop. No narration needed (the
// REPL already prints "You sleep for N hours" from its
// execSleep handler).
func (e *Engine) resolveSleep(hours int) Result {
	if hours <= 0 {
		hours = 8
	}
	if hours > 7*24 {
		hours = 7 * 24
	}
	ticks := int64(hours / 24)
	if ticks < 1 {
		ticks = 1
	}
	e.advanceTick(ticks)
	// Phase 31: show days elapsed, not raw tick numbers, so
	// the player sees the simulated-time cost of sleeping
	// (a full day for 8h, 7 days for a week, etc.).
	dayWord := "days"
	if ticks == 1 {
		dayWord = "day"
	}
	return Result{
		OK:            true,
		Text:          fmt.Sprintf("You sleep for %d hours (%d %s elapsed, now tick %d).", hours, ticks, dayWord, e.world.Tick),
		TicksAdvanced: ticks,
	}
}

// resolveWait handles "wait" — the player pauses briefly, observing
// the world around them. Does not advance simulation ticks (the
// simulation operates on 1-day ticks; waiting an hour is sub-tick).
// The narrator provides an atmospheric description of the scene.
func (e *Engine) resolveWait(hours int) Result {
	if hours <= 0 {
		hours = 1
	}
	if hours > 24 {
		hours = 24
	}
	dayWord := "hours"
	if hours == 1 {
		dayWord = "hour"
	}
	if e.narrator != nil {
		text := e.narrator.DescribeScene(context.Background(), e.world)
		return Result{OK: true, Text: fmt.Sprintf("You wait for %d %s...\n\n%s", hours, dayWord, text)}
	}
	return Result{OK: true, Text: fmt.Sprintf("You wait for %d %s. Time passes.", hours, dayWord)}
}

// resolveWalk handles "walk <destination>" — resolves the destination
// and describes the journey. Bare "walk" (empty target) is handled by
// the REPL's execWalkInteractive before reaching the engine.
// distMod modifies the tick advancement: "short stroll" = 0, "good walk" = 1, "long trek" = 2.
func (e *Engine) resolveWalk(target string, distMod int) Result {
	if strings.TrimSpace(target) == "" {
		return Result{OK: false, Text: "Walk where?"}
	}
	player := e.player()
	if player == nil {
		return Result{OK: false, Text: "You need a player character to walk. (Set Options.PlayerID.)"}
	}
	// Try to find destination by location name
	destination := e.findLocation(target)
	if destination == nil {
		// Check if the target is a building within the current settlement
		currentLoc, lok := e.world.Locations[player.LocationID]
		if lok {
			if building := e.findBuilding(currentLoc, target); building != "" {
				text := e.describeWalkingToBuilding(building, currentLoc)
				return Result{OK: true, Text: text, TicksAdvanced: 0}
			}
		}
		// No location or building found — describe walking in that direction
		text := e.describeWalkingNowhere(target)
		return Result{OK: true, Text: text, TicksAdvanced: 0}
	}
	if destination.ID == player.LocationID {
		return Result{OK: false, Text: fmt.Sprintf("You are already at %s.", destination.Name)}
	}
	// Found a destination — travel there, adjusting ticks for distance
	oldLocID := player.LocationID
	player.LocationID = destination.ID
	e.advanceTick(int64(distMod))

	// Record the walk as a memory
	mem := core.Memory{
		ID:             fmt.Sprintf("mem-walk-%d-%s", e.world.Tick, player.ID),
		OwnerID:        player.ID,
		EventID:        fmt.Sprintf("walk-%d-%s", e.world.Tick, destination.ID),
		Tick:           e.world.Tick,
		Importance:     0.1,
		Recency:        1.0,
		EmotionalScore: 0.0,
		Description:    fmt.Sprintf("walked from %s to %s", locationNameOrID(e.world, oldLocID), destination.Name),
		Tags:           []string{"walk"},
	}
	e.world.Memories = append(e.world.Memories, mem)

	// Narration: use walk-specific narrator when available
	var text string
	if e.narrator != nil {
		oldLoc, lok := e.world.Locations[oldLocID]
		if lok {
			text = e.narrator.DescribeWalk(context.Background(), e.world, oldLoc, destination, distMod)
		} else {
			text = e.renderTravel(context.Background(), destination)
		}
	} else {
		text = e.renderTravel(context.Background(), destination)
	}
	if distMod > 0 {
		days := "day"
		if distMod > 1 {
			days = "days"
		}
		text += fmt.Sprintf("\n\n(%d %s elapsed, now tick %d).", distMod, days, e.world.Tick)
	}
	return Result{OK: true, Text: text, TicksAdvanced: int64(distMod)}
}

// describeWalkingNowhere generates an atmospheric description when the
// player walks in a direction with no specific destination.
func (e *Engine) describeWalkingNowhere(direction string) string {
	player := e.player()
	if player == nil {
		return fmt.Sprintf("You walk %s, but there's nothing notable in that direction.", direction)
	}
	loc, ok := e.world.Locations[player.LocationID]
	if !ok {
		return fmt.Sprintf("You walk %s. The path is unfamiliar.", direction)
	}
	// Try narrator for atmospheric nowhere-walk
	if e.narrator != nil {
		season := narrator.SeasonFromTick(e.world.Tick)
		timeDesc := narrator.TimeOfDayFromTick(e.world.Tick)
		return fmt.Sprintf("You wander %s from %s in the %s %s air. The countryside stretches before you — meadows, scattered trees, and the distant hum of frontier life. After a pleasant stroll, you find yourself back where you started, refreshed by the outing.",
			direction, loc.Name, strings.ToLower(timeDesc), strings.ToLower(season))
	}
	return fmt.Sprintf("You walk %s from %s. The countryside stretches before you. After a pleasant stroll, you find yourself back where you started.",
		direction, loc.Name)
}

// ConnectedLocations returns a sorted list of destination names the
// player can walk to. Includes buildings within the current settlement
// and other settlements. Returns raw canonical names — the REPL is
// responsible for display formatting.
func (e *Engine) ConnectedLocations() []string {
	p := e.player()
	if p == nil {
		return nil
	}
	var names []string
	// Buildings within the current settlement
	if loc, ok := e.world.Locations[p.LocationID]; ok {
		for _, b := range loc.Buildings {
			names = append(names, b)
		}
	}
	// Other settlements
	for _, l := range e.world.Locations {
		if l.ID != p.LocationID {
			names = append(names, l.Name)
		}
	}
	sort.Strings(names)
	return names
}

// CurrentBuildings returns the building names at the player's current
// location. Used by the REPL to distinguish buildings from settlements
// in the walk prompt.
func (e *Engine) CurrentBuildings() []string {
	p := e.player()
	if p == nil {
		return nil
	}
	if loc, ok := e.world.Locations[p.LocationID]; ok {
		return loc.Buildings
	}
	return nil
}

// findBuilding matches a building name within a location. Returns
// the canonical building name if found, empty string otherwise.
// Case-insensitive exact match first, then word-boundary match
// ("inn" matches "The Prancing Inn" but not "Inner Sanctum").
func (e *Engine) findBuilding(loc *core.Location, target string) string {
	lower := strings.ToLower(strings.TrimSpace(target))
	if lower == "" {
		return ""
	}
	// Exact match first
	for _, b := range loc.Buildings {
		if strings.ToLower(b) == lower {
			return b
		}
	}
	// Word-boundary match: target appears as a whole word in the building name
	for _, b := range loc.Buildings {
		words := strings.Fields(strings.ToLower(b))
		for _, w := range words {
			if w == lower {
				return b
			}
		}
	}
	return ""
}

// describeWalkingToBuilding produces an atmospheric description of
// walking to a building within the current settlement. Delegates to
// the narrator when available.
func (e *Engine) describeWalkingToBuilding(building string, loc *core.Location) string {
	if e.narrator != nil {
		return e.narrator.DescribeBuilding(context.Background(), e.world, building, loc)
	}
	return fmt.Sprintf("You walk to the %s in %s.", building, loc.Name)
}

// resolveSearch handles "search" — the player searches their
// surroundings for something of interest. Finds items, clues, or
// describes what's hidden in the current location or building.
func (e *Engine) resolveSearch(target string) Result {
	player := e.player()
	if player == nil {
		return Result{OK: true, Text: "You look around carefully, but there's nothing remarkable to find."}
	}
	loc, ok := e.world.Locations[player.LocationID]
	if !ok {
		return Result{OK: true, Text: "You search, but find nothing of interest."}
	}
	// Atmospheric search results — what you find depends on where you are
	season := narrator.SeasonFromTick(e.world.Tick)
	timeDesc := narrator.TimeOfDayFromTick(e.world.Tick)

	if target != "" {
		// Searching for something specific
		if e.narrator != nil {
			text := e.narrator.DescribeScene(context.Background(), e.world)
			return Result{OK: true, Text: fmt.Sprintf("You search for %s in %s...\n\n%s", target, loc.Name, text)}
		}
		return Result{OK: true, Text: fmt.Sprintf("You search for %s in %s, but find nothing specific. The %s air is quiet around you.", target, loc.Name, strings.ToLower(season))}
	}

	// General searching — describe what catches your eye
	searchResults := []string{
		fmt.Sprintf("You search %s carefully in the %s light. A worn leather pouch catches your eye in the gutter — but it's empty. Still, someone was here recently.", loc.Name, strings.ToLower(timeDesc)),
		fmt.Sprintf("You rummage through the area. Under a loose stone, you find a faded scrap of paper with barely legible writing. The ink has run in the %s rain.", strings.ToLower(season)),
		fmt.Sprintf("You search the surroundings. The %s air carries the scent of woodsmoke and old timber. You notice scratch marks on a doorframe — claw marks, perhaps.", strings.ToLower(season)),
		fmt.Sprintf("You scour %s from end to end. Nothing of value presents itself, but you notice details you missed before — a hidden alcove, a cracked window, the way shadows pool in corners.", loc.Name),
		fmt.Sprintf("You search the area in the %s. Near the base of a wall, you find a small cache of candle stubs and a bent copper coin. Someone else was hiding things here.", strings.ToLower(timeDesc)),
	}
	hash := int(e.world.Tick) + len(player.ID)*13
	return Result{OK: true, Text: searchResults[hash%len(searchResults)]}
}

// resolvePray handles "pray" — the player takes a moment to pray
// or meditate. Restores a sense of calm and can provide narrative
// reflection on recent events. No world mutation.
func (e *Engine) resolvePray() Result {
	player := e.player()
	if player == nil {
		return Result{OK: true, Text: "You close your eyes and find a moment of stillness."}
	}
	loc, ok := e.world.Locations[player.LocationID]
	if !ok {
		return Result{OK: true, Text: "You bow your head in prayer. A quiet peace settles over you."}
	}
	// Check if there's a temple or shrine nearby
	hasTemple := false
	for _, b := range loc.Buildings {
		lower := strings.ToLower(b)
		if strings.Contains(lower, "temple") || strings.Contains(lower, "shrine") || strings.Contains(lower, "church") || strings.Contains(lower, "chapel") || strings.Contains(lower, "monastery") {
			hasTemple = true
			break
		}
	}
	if hasTemple {
		return Result{OK: true, Text: fmt.Sprintf("You find the %s and kneel before the altar. Candles flicker in the dim light, and the scent of incense fills the air. In the quiet, you reflect on your journey so far. A deep peace settles in your heart.", loc.Name)}
	}
	if e.narrator != nil {
		season := narrator.SeasonFromTick(e.world.Tick)
		return Result{OK: true, Text: fmt.Sprintf("You step away from the bustle of %s and find a quiet spot. Bowing your head, you offer a prayer to whatever gods watch over the Free Marches. The %s air is still, and for a moment, the world feels at peace.", loc.Name, strings.ToLower(season))}
	}
	return Result{OK: true, Text: fmt.Sprintf("You bow your head in prayer in %s. The world is quiet for a moment, and you feel a sense of calm.", loc.Name)}
}

// resolveStatus handles "status" — an immersive narrative moment of
// introspection. Delegates to the narrator for LLM-first prose that
// reads like a character's inner monologue, with a rich template
// fallback that paints a picture rather than dumping data.
func (e *Engine) resolveStatus() Result {
	player := e.player()
	if player == nil {
		return Result{OK: true, Text: "You pause, but cannot recall who you are. The frontier has a way of erasing identities."}
	}

	if e.narrator != nil {
		text := e.narrator.DescribeStatus(context.Background(), e.world)
		return Result{OK: true, Text: text}
	}

	// Minimal fallback when narrator is nil
	loc := "the open frontier"
	if l, ok := e.world.Locations[player.LocationID]; ok {
		loc = l.Name
	}
	season := narrator.SeasonFromTick(e.world.Tick)
	timeDesc := narrator.TimeOfDayFromTick(e.world.Tick)
	return Result{OK: true, Text: fmt.Sprintf("You pause in %s. It is %s, %s. You are %s, a %d-year-old %s.",
		loc, timeDesc, season, player.Name, player.AgeAt(e.world.Tick), player.Occupation)}
}

// resolveListen handles "listen" — the player pauses to listen to
// their surroundings. Surfaces ambient sounds, overheard conversations,
// and environmental details. No world mutation, no time advancement.
func (e *Engine) resolveListen() Result {
	if e.narrator != nil {
		text := e.narrator.DescribeScene(context.Background(), e.world)
		prefix := "You pause and listen carefully...\n\n"
		return Result{OK: true, Text: prefix + text}
	}
	player := e.player()
	if player == nil {
		return Result{OK: true, Text: "You listen. The world hums around you."}
	}
	loc, ok := e.world.Locations[player.LocationID]
	if !ok {
		return Result{OK: true, Text: "You listen, but hear nothing remarkable."}
	}
	people := e.world.LivingPeopleAt(player.LocationID)
	count := 0
	for _, p := range people {
		if p.ID != player.ID {
			count++
		}
	}
	if count > 0 {
		return Result{OK: true, Text: fmt.Sprintf("You listen carefully. The sounds of %d people fill %s — voices, footsteps, the clatter of daily life.", count, loc.Name)}
	}
	return Result{OK: true, Text: fmt.Sprintf("You listen. %s is quiet. A gentle breeze and distant birdsong are all you hear.", loc.Name)}
}

// resolveSave handles "save [path]" — writes the current
// world to a SQLite snapshot via the persistence layer.
// If path is empty, defaults to <world-id>.db. The
// snapshot is a full world dump (people, locations,
// relationships, memories, rules) that can be restored
// with `chronicle resume <path>`.
//
// Save does not advance time (it's a disk operation, not
// a simulation step). The Result.Text is "Saved to <path>."
// on success, or an error message on failure.
func (e *Engine) resolveSave(path string) Result {
	if path == "" {
		path = e.world.ID + ".db"
	}
	db, err := persistence.Open(path)
	if err != nil {
		return Result{OK: false, Text: fmt.Sprintf("save: open %s: %v", path, err)}
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		return Result{OK: false, Text: fmt.Sprintf("save: migrate: %v", err)}
	}
	if err := db.Snapshot(e.world); err != nil {
		return Result{OK: false, Text: fmt.Sprintf("save: snapshot: %v", err)}
	}
	return Result{OK: true, Text: fmt.Sprintf("Saved to %s.", path)}
}

// resolveBuy handles "buy <item> [quantity]" — adds items
// to the player's inventory and deducts Coin. Phase 18 reads
// the per-item price from the worldpack's item catalog
// (w.Items) instead of the Phase 17.6 hardcoded priceList.
// Phase 19 requires a merchant at the player's same location
// and decrements the merchant's stock.
//
// Quantity defaults to 1 when 0 or negative. The buy is
// rejected if the player can't afford it, no merchant is at
// the same location, the merchant is out of stock, or the
// item is unknown. The buy copies the catalog's Weight,
// Value, and MaxDurability into the inventory stack so a
// switch back to a pre-buy world preserves the metadata at
// acquisition time.
func (e *Engine) resolveBuy(item string, quantity int) Result {
	if item == "" {
		return Result{OK: false, Text: "Buy what?"}
	}
	player := e.player()
	if player == nil {
		return Result{OK: false, Text: "You need a player character to buy. (Set Options.PlayerID.)"}
	}
	if quantity <= 0 {
		quantity = 1
	}
	key := strings.ToLower(item)
	catalog, ok := e.world.Items[key]
	if !ok {
		return Result{OK: false, Text: fmt.Sprintf("I don't know the price of %q.", item)}
	}
	// Phase 19: find a merchant at the same location.
	merchant := e.findMerchantAt(player.LocationID)
	if merchant == nil {
		return Result{OK: false, Text: fmt.Sprintf("There is no merchant here to sell you %s.", item)}
	}
	// Check the merchant's stock. The merchant's Inventory
	// is their stock-on-hand.
	stock, ok := merchant.Inventory[key]
	if !ok || stock.Count < quantity {
		have := 0
		if ok {
			have = stock.Count
		}
		return Result{OK: false, Text: fmt.Sprintf("%s has only %d %s; can't sell you %d.", merchant.Name, have, item, quantity)}
	}
	cost := catalog.Value * quantity
	if e.world.Coin < cost {
		return Result{OK: false, Text: fmt.Sprintf("You can't afford %d %s (%d coin); you only have %d.", quantity, item, cost, e.world.Coin)}
	}
	e.world.Coin -= cost
	// Add to player's inventory, creating or updating the
	// stack. The stack's metadata (Weight, Value,
	// MaxDurability) is copied from the catalog at
	// acquisition time.
	stack := e.world.Inventory[key]
	stack.Name = key
	stack.Count += quantity
	stack.Weight = catalog.Weight
	stack.Value = catalog.Value
	stack.MaxDurability = catalog.MaxDurability
	e.world.Inventory[key] = stack
	// Decrement the merchant's stock. If the count drops to
	// 0, the entry is removed (consistent with the player's
	// inventory handling on sell).
	stock.Count -= quantity
	if stock.Count <= 0 {
		delete(merchant.Inventory, key)
	} else {
		merchant.Inventory[key] = stock
	}
	return Result{OK: true, Text: fmt.Sprintf("You buy %d %s from %s for %d coin. (Coin: %d)", quantity, item, merchant.Name, cost, e.world.Coin)}
}

// resolveSell handles "sell <item> [quantity]" — removes
// items from the player's inventory and adds Coin. Phase 18
// reads the per-item price from the worldpack's item catalog
// (w.Items). Phase 19 requires a merchant at the player's
// same location and increments the merchant's stock.
//
// The sell is rejected if the player doesn't have the item
// (or enough of it), if no merchant is at the same location,
// or if the item is unknown.
func (e *Engine) resolveSell(item string, quantity int) Result {
	if item == "" {
		return Result{OK: false, Text: "Sell what?"}
	}
	player := e.player()
	if player == nil {
		return Result{OK: false, Text: "You need a player character to sell. (Set Options.PlayerID.)"}
	}
	if quantity <= 0 {
		quantity = 1
	}
	key := strings.ToLower(item)
	catalog, ok := e.world.Items[key]
	if !ok {
		return Result{OK: false, Text: fmt.Sprintf("I don't know the price of %q.", item)}
	}
	// Phase 19: find a merchant at the same location.
	merchant := e.findMerchantAt(player.LocationID)
	if merchant == nil {
		return Result{OK: false, Text: fmt.Sprintf("There is no merchant here to buy %s.", item)}
	}
	stack, ok := e.world.Inventory[key]
	if !ok || stack.Count < quantity {
		have := 0
		if ok {
			have = stack.Count
		}
		return Result{OK: false, Text: fmt.Sprintf("You only have %d %s; can't sell %d.", have, item, quantity)}
	}
	value := catalog.Value * quantity
	e.world.Coin += value
	stack.Count -= quantity
	if stack.Count <= 0 {
		delete(e.world.Inventory, key)
	} else {
		e.world.Inventory[key] = stack
	}
	// Increment the merchant's stock. Create the entry if
	// the merchant didn't have this item before (e.g., a
	// merchant who only sold swords now has bread).
	mstock := merchant.Inventory[key]
	mstock.Name = key
	mstock.Count += quantity
	mstock.Weight = catalog.Weight
	mstock.Value = catalog.Value
	mstock.MaxDurability = catalog.MaxDurability
	merchant.Inventory[key] = mstock
	return Result{OK: true, Text: fmt.Sprintf("You sell %d %s to %s for %d coin. (Coin: %d)", quantity, item, merchant.Name, value, e.world.Coin)}
}

// resolveBranch handles "branch <name>" — saves the current
// world to a named branch file in branches/<name>.db.
// Phase 17.7: branches are stored in a `branches/`
// subdirectory relative to the current working directory.
// The branch name is validated to prevent path traversal
// (no "/", "\", "..", or empty names).
//
// Branch does not advance time (it's a disk operation,
// not a simulation step). The current world is left
// unchanged; use "switch" to restore a branch into the
// current world.
func (e *Engine) resolveBranch(name string) Result {
	if name == "" {
		return Result{OK: false, Text: "Branch what? (Usage: branch <name>)"}
	}
	if !isValidBranchName(name) {
		return Result{OK: false, Text: fmt.Sprintf("Invalid branch name %q (no '/', '\\\\', '..', or empty).", name)}
	}
	path := branchPath(name)
	if err := os.MkdirAll("branches", 0o755); err != nil {
		return Result{OK: false, Text: fmt.Sprintf("branch: mkdir branches: %v", err)}
	}
	db, err := persistence.Open(path)
	if err != nil {
		return Result{OK: false, Text: fmt.Sprintf("branch: open %s: %v", path, err)}
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		return Result{OK: false, Text: fmt.Sprintf("branch: migrate: %v", err)}
	}
	if err := db.Snapshot(e.world); err != nil {
		return Result{OK: false, Text: fmt.Sprintf("branch: snapshot: %v", err)}
	}
	return Result{OK: true, Text: fmt.Sprintf("Branched to %q (saved to %s).", name, path)}
}

// resolveSwitch handles "switch <name>" — restores the world
// from a named branch file (branches/<name>.db). The
// current world's in-memory state is replaced with the
// branch's state. This is destructive: any unsaved changes
// to the current world are lost.
//
// Switch does not advance time. After a switch, the
// world's tick, people, locations, relationships, and
// memories all reflect the branch's state.
func (e *Engine) resolveSwitch(name string) Result {
	if name == "" {
		return Result{OK: false, Text: "Switch to what? (Usage: switch <name>)"}
	}
	if !isValidBranchName(name) {
		return Result{OK: false, Text: fmt.Sprintf("Invalid branch name %q.", name)}
	}
	path := branchPath(name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Result{OK: false, Text: fmt.Sprintf("Branch %q does not exist (no file at %s).", name, path)}
	}
	db, err := persistence.Open(path)
	if err != nil {
		return Result{OK: false, Text: fmt.Sprintf("switch: open %s: %v", path, err)}
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		return Result{OK: false, Text: fmt.Sprintf("switch: migrate: %v", err)}
	}
	// Restore into the existing world. The Restore function
	// clears the world's people, locations, relationships,
	// and memories before loading from the DB.
	if err := db.Restore(e.world); err != nil {
		return Result{OK: false, Text: fmt.Sprintf("switch: restore: %v", err)}
	}
	// Ensure the Inventory map is initialized after Restore
	// (a legacy DB written before Phase 17.6 may not have
	// serialized the inventory).
	if e.world.Inventory == nil {
		e.world.Inventory = make(map[string]core.Item)
	}
	return Result{OK: true, Text: fmt.Sprintf("Switched to %q. (tick %d, %d people)", name, e.world.Tick, len(e.world.People))}
}

// lookPerson renders a person's details. Delegates to the
// Narrator's EventLook template (or the Narrator's LLM for
// significant events, though look is routine).
func (e *Engine) lookPerson(p *core.Person) Result {
	if e.narrator != nil {
		text := e.narrator.Narrate(context.Background(), e.world, narrator.Event{
			Type:   narrator.EventLook,
			Person: p,
		})
		return Result{OK: true, Text: text}
	}
	return Result{OK: true, Text: fmt.Sprintf("%s (%s, %d) is at %s.",
		p.Name, p.Gender, p.AgeAt(e.world.Tick), locationNameOrID(e.world, p.LocationID))}
}

// lookLocation renders a location and its people.
func (e *Engine) lookLocation(locationID string) Result {
	if locationID == "" {
		var first *core.Location
		for _, l := range e.world.Locations {
			if first == nil || l.ID < first.ID {
				first = l
			}
		}
		if first == nil {
			return Result{OK: true, Text: "The world has no locations."}
		}
		locationID = first.ID
	}
	loc, ok := e.world.Locations[locationID]
	if !ok {
		return Result{OK: false, Text: fmt.Sprintf("I don't know the location %q.", locationID)}
	}
	people := e.world.LivingPeopleAt(locationID)
	if len(people) == 0 {
		return Result{OK: true, Text: fmt.Sprintf("%s is empty.", loc.Name)}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s (population %d, cap %d) ===\n", loc.Name, loc.Population, loc.PopulationCap)
	for _, p := range people {
		fmt.Fprintf(&b, "  %s (%s, %d)\n", p.Name, p.Gender, p.AgeAt(e.world.Tick))
	}
	return Result{OK: true, Text: b.String()}
}

// renderTalk delegates to the Narrator for talk events.
// Falls back to a short template if the Narrator is nil.
// Threads ctx so REPL cancellation cancels in-flight LLM calls.
func (e *Engine) renderTalk(ctx context.Context, p *core.Person) string {
	if e.narrator != nil {
		return e.narrator.Narrate(ctx, e.world, narrator.Event{
			Type:   narrator.EventTalk,
			Person: p,
		})
	}
	return fmt.Sprintf("You talk to %s.", p.Name)
}

// renderTravel delegates to the Narrator for travel events.
// Threads ctx so REPL cancellation cancels in-flight LLM calls.
func (e *Engine) renderTravel(ctx context.Context, l *core.Location) string {
	if e.narrator != nil {
		return e.narrator.Narrate(ctx, e.world, narrator.Event{
			Type:     narrator.EventTravel,
			Location: l,
		})
	}
	return fmt.Sprintf("You travel to %s.", l.Name)
}

// Player returns the PlayerID person, or nil if the world
// has no player set. Exported so the REPL can access the
// current player for interactive prompts.
func (e *Engine) Player() *core.Person {
	return e.player()
}

// player returns the PlayerID person, or nil if the world
// has no player set. Phase 17.5 uses the world-level
// PlayerID field (added to core.World); if it's empty, the
// engine operates in world-level mode (no player-scoped
// actions like travel).
func (e *Engine) player() *core.Person {
	id := e.world.PlayerID
	if id == "" {
		return nil
	}
	p, ok := e.world.People[id]
	if !ok || !p.Alive {
		return nil
	}
	return p
}

// advanceTick advances the world's tick counter and clock
// by the given number of ticks. When a TickFn is set
// (Phase 31), it is called n times — each call runs the
// full tick pipeline (sim.Tick increments w.Tick and
// w.Now internally, plus all engines mutate state). When
// no TickFn is set, advanceTick falls back to clock-only
// advancement: w.Tick += n and w.Now += n days. This
// fallback keeps older test code working unchanged.
//
// Errors from the TickFn are NOT returned from this helper
// (the helper's signature is void for backward compat).
// Callers that need error propagation can wrap advanceTick
// with a function that returns error, or set the world's
// state directly. Phase 31 v1 swallows TickFn errors so
// the player sees the action's primary effect even if a
// downstream engine hiccuped; the underlying sim.Tick
// already has its own error path for unrecoverable engine
// failures.
func (e *Engine) advanceTick(n int64) {
	if n <= 0 {
		return
	}
	if e.tickFn != nil {
		// Full-pipeline mode: invoke the per-tick callback
		// n times. Each invocation increments w.Tick and
		// w.Now (sim.Tick does that internally) and runs
		// every engine in the simulation. This is the
		// "for each tick in the elapsed time, run full
		// tick pipeline" path from SIMULATION_TICK_SPEC.md
		// §5.3, applied to player duration actions.
		for i := int64(0); i < n; i++ {
			_ = e.tickFn()
		}
		return
	}
	// Clock-only fallback: keep w.Tick and w.Now in sync
	// without invoking any engine. Used by tests that want
	// a deterministic "time passes" without engine mutation
	// (e.g., action_test.go's pre-Phase 31 tests).
	e.world.Tick += n
	e.world.Now = e.world.Now.AddDate(0, 0, int(n))
}

// findPerson looks up a person in three stages, in order:
//
//  1. Exact ID match (handles internal callers that already
//     know the ID).
//  2. Exact (case-insensitive) full-name match ("Lily
//     Kensington" matches the literal "lily kensington").
//  3. First-token match (case-insensitive). This is the
//     common case for the REPL: a player types
//     "talk Lily" rather than "talk Lily Kensington", and
//     we want the lookup to find the right person. When
//     multiple people share a first name, sorted-ID
//     iteration picks a stable winner so the result is
//     deterministic for tests and replays.
//
// Returns nil if target is empty or no match is found.
func (e *Engine) findPerson(target string) *core.Person {
	if target == "" {
		return nil
	}
	// 1. Exact ID match.
	if p, ok := e.world.People[target]; ok {
		return p
	}
	lower := strings.ToLower(target)
	// 2. Exact full-name match (case-insensitive).
	for _, p := range e.world.People {
		if strings.ToLower(p.Name) == lower {
			return p
		}
	}
	// 3. First-token match. Sort IDs for deterministic ties.
	ids := make([]string, 0, len(e.world.People))
	for id := range e.world.People {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := e.world.People[id]
		parts := strings.Fields(p.Name)
		if len(parts) > 0 && strings.ToLower(parts[0]) == lower {
			return p
		}
	}
	return nil
}

// findMerchantAt returns the first living merchant at the
// given location, or nil if no merchant is present. Phase
// 19: a merchant is a Person with IsMerchant=true. Used by
// resolveBuy/resolveSell to enforce the same-location rule.
// When multiple merchants are at the same location, the
// first by sorted ID wins (deterministic for reproducible
// tests). Phase 20+ may add a way to address a specific
// merchant.
func (e *Engine) findMerchantAt(locationID string) *core.Person {
	var ids []string
	for id, p := range e.world.People {
		if p.Alive && p.IsMerchant && p.LocationID == locationID {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return e.world.People[ids[0]]
}

// findLocation looks up a location by exact ID first, then
// by case-insensitive name match. Returns nil if not found.
func (e *Engine) findLocation(target string) *core.Location {
	if l, ok := e.world.Locations[target]; ok {
		return l
	}
	lower := strings.ToLower(target)
	for _, l := range e.world.Locations {
		if strings.ToLower(l.Name) == lower {
			return l
		}
	}
	return nil
}

// applyTalkDelta applies a talk memory's TrustDelta to the
// player→target relationship. Uses the same O(1) pattern
// as RelationshipEngine.ApplyMemoryDeltas. If no player is
// set, the OwnerID is "" and the delta is a no-op.
//
// NOTE: this is a local reimplementation of
// simulation.RelationshipEngine.ApplyMemoryDeltas. We don't
// import simulation here to avoid a dependency cycle (the
// CLI wires the action engine after the simulation is
// constructed). Phase 18+ may inject a *RelationshipEngine
// to share the canonical path.
func applyTalkDelta(w *core.World, mem core.Memory, targetID string) {
	if mem.OwnerID == "" || targetID == "" || mem.OwnerID == targetID {
		return
	}
	// Search for an existing relationship.
	for i := range w.Relationships {
		if w.Relationships[i].FromID == mem.OwnerID && w.Relationships[i].ToID == targetID {
			w.Relationships[i].Trust = clampAxis(w.Relationships[i].Trust + mem.TrustDelta)
			return
		}
	}
	// Create a new relationship with the trust baked in.
	w.Relationships = append(w.Relationships, core.Relationship{
		FromID: mem.OwnerID,
		ToID:   targetID,
		Trust:  clampAxis(50 + mem.TrustDelta),
	})
}

// clampAxis returns v clamped to [0, 100]. Mirrors
// simulation.clampAxis to avoid a cross-package import.
func clampAxis(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// locationNameOrID returns the human-readable name of a
// location, or the raw ID if the location is unknown.
func locationNameOrID(w *core.World, id string) string {
	if l, ok := w.Locations[id]; ok {
		return l.Name
	}
	return id
}

// playerID returns the world's PlayerID, or "" if unset.
// Wrapped in a function for symmetry with future fields.
func playerID(w *core.World) string {
	return w.PlayerID
}

// isValidBranchName reports whether name is safe to use as
// a branch filename. Rejects empty names, names containing
// path separators, and parent-directory references. This
// is a security check to prevent path traversal (e.g.,
// "../../etc/passwd").
func isValidBranchName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	// Reject names that start with a dot (hidden files).
	if strings.HasPrefix(name, ".") {
		return false
	}
	return true
}

// branchPath returns the filesystem path for a branch name.
// Branches are stored in a `branches/` subdirectory relative
// to the current working directory.
func branchPath(name string) string {
	return filepath.Join("branches", name+".db")
}
