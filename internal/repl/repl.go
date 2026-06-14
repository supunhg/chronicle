// Package repl implements the in-game read-eval-print loop for
// Chronicle. After a play or resume, the REPL drops the user
// into a > prompt where they can type commands.
//
// Two dispatch layers:
//
//  1. Special REPL meta-commands: quit, exit, people,
//     auto-tick on|off, advance day|week|month. These are
//     not part of the 12-verb spec — they're REPL-only
//     affordances for pacing and inspection.
//  2. Everything else goes through the intent parser
//     (Phase 17.2). The resulting Intent.Action is
//     dispatched to a per-action executor.
//
// The REPL does NOT execute world mutations. execTravel
// and execTalk call into the Narrator (Phase 17.4) for
// text rendering; the actual world mutation (move, time
// advancement) is Phase 17.5 (Action Engine). Phase 17.4
// proves the narration wiring: typed commands flow through
// the parser, the parser's output is dispatched to a
// per-action executor, the executor calls Narrator.Narrate,
// and the rendered text is what the player sees.
//
// Threading: Run is single-threaded. The simulation and
// the REPL share the world pointer; the REPL never holds
// a long-lived reference to a tick result.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
	"github.com/chronicle-dev/chronicle/internal/narrator"
	"github.com/chronicle-dev/chronicle/internal/persistence"
)

// Options configures a REPL. The zero value is safe: default
// reader is os.Stdin, default writer is os.Stderr, default
// auto-tick is off, default PlayerID is "". Tests inject
// custom In/Out/TickFn.
type Options struct {
	// In is the input source. Default: os.Stdin.
	In io.Reader
	// Out is the output destination (prompt + command
	// output). Default: os.Stderr.
	Out io.Writer
	// PlayerID is the ID of the person the player controls.
	// Empty means "no specific player" — the REPL shows a
	// world-level view. Phase 17.3: always empty; player
	// identity is added in a later phase.
	PlayerID string
	// AutoTick is the initial auto-tick state. When true,
	// the simulation advances by one tick before each
	// prompt. Default: false.
	AutoTick bool
	// TickFn advances the simulation by one tick. It is
	// called by the REPL for auto-tick, for sleep, and for
	// advance day/week/month. Required: callers that
	// don't supply one will get nil-panic on the first
	// tick. Constructed by the CLI layer from a
	// *tick.Simulation.
	TickFn func() error
	// Narrator renders narrative text for execTalk and
	// execTravel. Optional: when nil, the REPL prints
	// a short stub ("You talk to X" / "You travel to Y")
	// for backwards compatibility with Phase 17.3
	// callers. When supplied, the executor delegates to
	// Narrator.Narrate and prints the rendered text.
	// Constructed by the CLI layer from the resolved LLM
	// client and the current world.
	Narrator *narrator.Narrator
}

// REPL is the in-game command loop. Construct via New.
type REPL struct {
	world    *core.World
	parser   *intent.Parser
	narrator *narrator.Narrator
	in       *bufio.Scanner
	out      io.Writer
	playerID string
	autoTick bool
	tickFn   func() error
	running  bool
}

// New constructs a REPL. The world and parser are required.
// opts is optional; the zero value uses defaults.
func New(w *core.World, parser *intent.Parser, opts Options) *REPL {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	scanner := bufio.NewScanner(in)
	return &REPL{
		world:    w,
		parser:   parser,
		narrator: opts.Narrator,
		in:       scanner,
		out:      out,
		playerID: opts.PlayerID,
		autoTick: opts.AutoTick,
		tickFn:   opts.TickFn,
	}
}

// Run is the main REPL loop. It reads lines from input,
// dispatches them, and repeats until quit/exit/EOF or
// context cancellation.
//
// Each iteration:
//
//  1. Game-over check (no alive people): print a message
//     and return.
//  2. Auto-tick (if enabled): advance one tick.
//  3. Print the > prompt.
//  4. Read a line.
//  5. Parse and execute.
//
// The context controls cancellation. When the context is
// cancelled, Run returns ctx.Err().
func (r *REPL) Run(ctx context.Context) error {
	r.running = true
	for r.running {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.isGameOver() {
			fmt.Fprintln(r.out, "\nEveryone has died. The world continues without you.")
			return nil
		}
		if r.autoTick {
			if r.tickFn == nil {
				return fmt.Errorf("repl: auto-tick enabled but TickFn is nil")
			}
			if err := r.tickFn(); err != nil {
				return fmt.Errorf("repl: auto-tick: %w", err)
			}
		}
		fmt.Fprint(r.out, "> ")
		if !r.in.Scan() {
			if err := r.in.Err(); err != nil {
				return fmt.Errorf("repl: read: %w", err)
			}
			return nil // clean EOF
		}
		line := strings.TrimSpace(r.in.Text())
		if line == "" {
			continue
		}
		if err := r.execute(ctx, line); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
		}
	}
	return nil
}

// isGameOver returns true if the world has no alive people.
// An empty world (len(People) == 0) is NOT game-over — it's
// a valid state for a freshly-created or pre-game world.
func (r *REPL) isGameOver() bool {
	if len(r.world.People) == 0 {
		return false
	}
	for _, p := range r.world.People {
		if p.Alive {
			return false
		}
	}
	return true
}

// execute parses and runs one command line. First checks
// for REPL meta-commands (quit, exit, people, auto-tick,
// advance); everything else goes through the intent parser.
func (r *REPL) execute(ctx context.Context, line string) error {
	lower := strings.ToLower(line)
	switch lower {
	case "quit", "exit":
		fmt.Fprintln(r.out, "Goodbye.")
		r.running = false
		return nil
	case "people":
		r.execPeople()
		return nil
	}
	if rest, ok := strings.CutPrefix(lower, "auto-tick "); ok {
		return r.execAutoTick(strings.TrimSpace(rest))
	}
	if rest, ok := strings.CutPrefix(lower, "advance "); ok {
		return r.execAdvance(strings.TrimSpace(rest))
	}
	// Everything else → intent parser → dispatch.
	in, err := r.parser.Parse(ctx, line)
	if err != nil {
		return err
	}
	return r.dispatch(in)
}

// dispatch routes an Intent to its per-action executor.
// Fully-implemented verbs call the executor; stub verbs
// (buy, sell, branch, switch) print a "not yet implemented"
// note so the player knows the command was understood.
func (r *REPL) dispatch(in intent.Intent) error {
	switch in.Action {
	case intent.ActionLook:
		r.execLook(in.Target)
	case intent.ActionInventory:
		r.execInventory()
	case intent.ActionTime:
		r.execTime()
	case intent.ActionTalk:
		r.execTalk(in.Target)
	case intent.ActionInspect:
		r.execInspect(in.Target)
	case intent.ActionTravel:
		r.execTravel(in.Target)
	case intent.ActionSleep:
		r.execSleep(in.Args.Hours)
	case intent.ActionSave:
		return r.execSave(in.Target)
	case intent.ActionBuy, intent.ActionSell:
		fmt.Fprintf(r.out, "%s is not yet implemented (Phase 17.4+).\n", in.Action)
	case intent.ActionBranch, intent.ActionSwitch:
		fmt.Fprintf(r.out, "%s is not yet implemented (Phase 17.5+).\n", in.Action)
	default:
		return fmt.Errorf("unknown action %q", in.Action)
	}
	return nil
}

// execTime prints the current tick and the simulated date.
func (r *REPL) execTime() {
	fmt.Fprintf(r.out, "Tick %d (%s)\n", r.world.Tick, r.world.Now.Format("2006-01-02"))
}

// execPeople lists all alive people with their age and
// location. Used for world-level inspection when the
// player has no specific location.
func (r *REPL) execPeople() {
	people := r.world.LivingPeople()
	if len(people) == 0 {
		fmt.Fprintln(r.out, "No one is alive.")
		return
	}
	fmt.Fprintf(r.out, "\n=== %d alive people ===\n", len(people))
	for _, p := range people {
		loc := "nowhere"
		if p.LocationID != "" {
			if l, ok := r.world.Locations[p.LocationID]; ok {
				loc = l.Name
			}
		}
		fmt.Fprintf(r.out, "  %s (%s, %d, %s)\n",
			p.Name, p.Gender, p.AgeAt(r.world.Tick), loc)
	}
}

// execLook with no target shows the first location
// (sorted by ID) and its people. With a target, it looks
// up a person or location by ID or name (case-insensitive).
func (r *REPL) execLook(target string) {
	if target == "" {
		r.execLookLocation("")
		return
	}
	if p := r.findPerson(target); p != nil {
		r.printPerson(p)
		return
	}
	if l := r.findLocation(target); l != nil {
		r.execLookLocation(l.ID)
		return
	}
	fmt.Fprintf(r.out, "I don't see %q here.\n", target)
}

// execLookLocation prints a location header and its people.
// If locationID is empty, the first location (by ID) is used.
func (r *REPL) execLookLocation(locationID string) {
	if locationID == "" {
		var first *core.Location
		for _, l := range r.world.Locations {
			if first == nil || l.ID < first.ID {
				first = l
			}
		}
		if first == nil {
			fmt.Fprintln(r.out, "The world has no locations.")
			return
		}
		locationID = first.ID
	}
	loc, ok := r.world.Locations[locationID]
	if !ok {
		fmt.Fprintf(r.out, "I don't know the location %q.\n", locationID)
		return
	}
	fmt.Fprintf(r.out, "\n=== %s (population %d, cap %d) ===\n",
		loc.Name, loc.Population, loc.PopulationCap)
	people := r.world.LivingPeopleAt(locationID)
	if len(people) == 0 {
		fmt.Fprintln(r.out, "  (no one is here)")
		return
	}
	for _, p := range people {
		fmt.Fprintf(r.out, "  %s (%s, %d)\n",
			p.Name, p.Gender, p.AgeAt(r.world.Tick))
	}
}

// execInspect shows a single person's full details:
// status, gender, age, location, occupation, and family
// links (if present).
func (r *REPL) execInspect(target string) {
	p := r.findPerson(target)
	if p == nil {
		fmt.Fprintf(r.out, "I don't see %q.\n", target)
		return
	}
	r.printPerson(p)
}

// printPerson is the shared formatter for look/inspect.
// Status is "alive" or "deceased"; location shows the
// human-readable name when known.
func (r *REPL) printPerson(p *core.Person) {
	status := "alive"
	if !p.Alive {
		status = "deceased"
	}
	loc := "nowhere"
	if p.LocationID != "" {
		if l, ok := r.world.Locations[p.LocationID]; ok {
			loc = l.Name
		}
	}
	fmt.Fprintf(r.out, "\n=== %s (%s) ===\n", p.Name, status)
	fmt.Fprintf(r.out, "  Gender: %s\n", p.Gender)
	fmt.Fprintf(r.out, "  Age: %d\n", p.AgeAt(r.world.Tick))
	fmt.Fprintf(r.out, "  Location: %s\n", loc)
	if p.Occupation != "" {
		fmt.Fprintf(r.out, "  Occupation: %s\n", p.Occupation)
	}
	if p.Class != "" {
		fmt.Fprintf(r.out, "  Class: %s\n", p.Class)
	}
	if p.SpouseID != "" {
		if spouse, ok := r.world.People[p.SpouseID]; ok {
			fmt.Fprintf(r.out, "  Spouse: %s\n", spouse.Name)
		}
	}
	if p.FatherID != "" {
		if father, ok := r.world.People[p.FatherID]; ok {
			fmt.Fprintf(r.out, "  Father: %s\n", father.Name)
		}
	}
	if p.MotherID != "" {
		if mother, ok := r.world.People[p.MotherID]; ok {
			fmt.Fprintf(r.out, "  Mother: %s\n", mother.Name)
		}
	}
}

// execTalk renders narration for talking to a person.
// If a Narrator is configured, delegates to Narrator.Narrate
// with EventType=EventTalk; the Narrator decides whether to
// call the LLM (rate-limited, cached) or fall back to a
// template. If no Narrator is set, prints a Phase 17.3
// stub for backwards compatibility.
func (r *REPL) execTalk(target string) {
	p := r.findPerson(target)
	if p == nil {
		fmt.Fprintf(r.out, "I don't see %q here.\n", target)
		return
	}
	if r.narrator == nil {
		fmt.Fprintf(r.out, "You talk to %s.\n", p.Name)
		return
	}
	text := r.narrator.Narrate(context.Background(), r.world, narrator.Event{
		Type:   narrator.EventTalk,
		Person: p,
	})
	fmt.Fprintln(r.out, text)
}

// execTravel renders narration for traveling to a location.
// If a Narrator is configured, delegates to Narrator.Narrate
// with EventType=EventTravel. If no Narrator is set, prints
// a Phase 17.3 stub. The actual world mutation (moving the
// player to the destination) is Phase 17.5.
func (r *REPL) execTravel(target string) {
	l := r.findLocation(target)
	if l == nil {
		fmt.Fprintf(r.out, "I don't know the location %q.\n", target)
		return
	}
	if r.narrator == nil {
		fmt.Fprintf(r.out, "You travel to %s.\n", l.Name)
		return
	}
	text := r.narrator.Narrate(context.Background(), r.world, narrator.Event{
		Type:     narrator.EventTravel,
		Location: l,
	})
	fmt.Fprintln(r.out, text)
}

// execInventory is a stub: the player concept (and thus
// inventory) is added in a later phase. The command is
// accepted and acknowledged so the parser wiring is
// exercised.
func (r *REPL) execInventory() {
	fmt.Fprintln(r.out, "Inventory: (not yet implemented — Phase 17.4+)")
}

// execSleep advances the simulation by hours/24 ticks
// (rounded up to at least 1). A nil TickFn is reported
// as an error so the CLI layer can surface a clear
// "REPL was constructed without a tick function" message.
// Hours is clamped to a week (7*24) to prevent a typo
// or malicious input from spinning the loop.
func (r *REPL) execSleep(hours int) {
	if hours <= 0 {
		hours = 8
	}
	if hours > 7*24 {
		hours = 7 * 24
	}
	if r.tickFn == nil {
		fmt.Fprintln(r.out, "error: cannot advance time (TickFn is nil)")
		return
	}
	ticks := int64(hours / 24)
	if ticks < 1 {
		ticks = 1
	}
	for i := int64(0); i < ticks; i++ {
		if err := r.tickFn(); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			return
		}
	}
	fmt.Fprintf(r.out, "You sleep for %d hours. (tick %d)\n", hours, r.world.Tick)
}

// execSave writes the current world to a SQLite snapshot.
// If path is empty, defaults to <world-id>.db. Reuses the
// persistence layer from Phase 12/13.
func (r *REPL) execSave(path string) error {
	if path == "" {
		path = r.world.ID + ".db"
	}
	db, err := persistence.Open(path)
	if err != nil {
		return fmt.Errorf("save: open %s: %w", path, err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		return fmt.Errorf("save: migrate: %w", err)
	}
	if err := db.Snapshot(r.world); err != nil {
		return fmt.Errorf("save: snapshot: %w", err)
	}
	fmt.Fprintf(r.out, "Saved to %s.\n", path)
	return nil
}

// execAutoTick toggles the auto-advance mode. Accepts
// on/true/1 and off/false/0; anything else is an error.
func (r *REPL) execAutoTick(arg string) error {
	switch strings.ToLower(arg) {
	case "on", "true", "1":
		r.autoTick = true
		fmt.Fprintln(r.out, "Auto-tick enabled.")
	case "off", "false", "0":
		r.autoTick = false
		fmt.Fprintln(r.out, "Auto-tick disabled.")
	default:
		return fmt.Errorf("auto-tick: expected on or off, got %q", arg)
	}
	return nil
}

// execAdvance runs the simulation forward by 1 day, 7
// days, or 30 days. Anything else is an error.
func (r *REPL) execAdvance(unit string) error {
	if r.tickFn == nil {
		return fmt.Errorf("advance: TickFn is nil")
	}
	var n int
	switch strings.ToLower(unit) {
	case "day":
		n = 1
	case "week":
		n = 7
	case "month":
		n = 30
	default:
		return fmt.Errorf("advance: expected day, week, or month, got %q", unit)
	}
	for i := 0; i < n; i++ {
		if err := r.tickFn(); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.out, "Advanced %d day(s). (tick %d)\n", n, r.world.Tick)
	return nil
}

// findPerson looks up a person by exact ID first, then
// by case-insensitive name match. Returns nil if not
// found. Used by look, inspect, talk.
func (r *REPL) findPerson(target string) *core.Person {
	if p, ok := r.world.People[target]; ok {
		return p
	}
	lower := strings.ToLower(target)
	for _, p := range r.world.People {
		if strings.ToLower(p.Name) == lower {
			return p
		}
	}
	return nil
}

// findLocation looks up a location by exact ID first,
// then by case-insensitive name match. Returns nil if
// not found. Used by look and travel.
func (r *REPL) findLocation(target string) *core.Location {
	if l, ok := r.world.Locations[target]; ok {
		return l
	}
	lower := strings.ToLower(target)
	for _, l := range r.world.Locations {
		if strings.ToLower(l.Name) == lower {
			return l
		}
	}
	return nil
}
