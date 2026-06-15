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
type Engine struct {
	world    *core.World
	narrator *narrator.Narrator
}

// New constructs an Action Engine. The world is required and
// is mutated in place as actions resolve. The narrator is
// optional; when nil, the engine uses short template strings
// for all narration (no LLM calls, no cache).
func New(w *core.World, n *narrator.Narrator) *Engine {
	return &Engine{world: w, narrator: n}
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
	default:
		return Result{}, fmt.Errorf("action: unknown action %q", in.Action)
	}
}

// resolveLook handles "look" — read-only, no world changes.
// With a target, it shows a person or location. Without a
// target, it shows the first location (sorted by ID) and
// its people. Narration is delegated to the Narrator when
// present, otherwise the engine uses its own template.
func (e *Engine) resolveLook(target string) Result {
	if target == "" {
		return e.lookLocation("")
	}
	if p := e.findPerson(target); p != nil {
		return e.lookPerson(p)
	}
	if l := e.findLocation(target); l != nil {
		return e.lookLocation(l.ID)
	}
	return Result{OK: false, Text: fmt.Sprintf("I don't see %q here.", target)}
}

// resolveInspect handles "inspect <name>" — read-only, shows
// a person's full details. Currently identical to lookPerson;
// Phase 17.6+ may add an inspect-specific template (e.g.,
// showing traits, needs, goals).
func (e *Engine) resolveInspect(target string) Result {
	if strings.TrimSpace(target) == "" {
		return Result{OK: false, Text: "Inspect whom? (Usage: inspect <name>)"}
	}
	p := e.findPerson(target)
	if p == nil {
		return Result{OK: false, Text: fmt.Sprintf("I don't see %q here.", target)}
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
// (a day on the road). Narration is delegated to the
// Narrator.
//
// The target must be a known location. The player must
// exist (Phase 17.5 adds the PlayerID field to Options;
// if no player is set, travel is a no-op with a clear
// message — world-level travel is a Phase 18+ concern).
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

	// Move the player and advance time by 1 tick.
	oldLoc := player.LocationID
	player.LocationID = l.ID
	e.advanceTick(1)

	// Record the travel as a memory. Low importance, no
	// trust delta (travel doesn't change relationships).
	mem := core.Memory{
		ID:             fmt.Sprintf("mem-travel-%d-%s", e.world.Tick, player.ID),
		OwnerID:        player.ID,
		EventID:        fmt.Sprintf("travel-%d-%s", e.world.Tick, l.ID),
		Tick:           e.world.Tick,
		Importance:     0.1,
		Recency:        1.0,
		EmotionalScore: 0.0,
		Description:    fmt.Sprintf("traveled from %s to %s", locationNameOrID(e.world, oldLoc), l.Name),
		Tags:           []string{"travel"},
	}
	e.world.Memories = append(e.world.Memories, mem)

	// Narration: delegate to the Narrator if present.
	text := e.renderTravel(ctx, l)
	return Result{OK: true, Text: text, TicksAdvanced: 1}
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
	return Result{
		OK:            true,
		Text:          fmt.Sprintf("You sleep for %d hours. (tick %d)", hours, e.world.Tick),
		TicksAdvanced: ticks,
	}
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
// by the given number of ticks. This mirrors what
// tick.Simulation.Tick does, but the action engine doesn't
// have a full simulation (it operates per-action). The REPL
// can override this by passing a TickFn that does the full
// simulation; for now, the action engine's advanceTick is
// sufficient for Phase 17.5 (time advancement only, no
// engine side effects).
func (e *Engine) advanceTick(n int64) {
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
