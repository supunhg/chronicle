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
//     dispatched to the Action Engine (Phase 17.5), which
//     mutates the world and returns rendered text.
//
// Phase 17.5 wiring: execTalk, execTravel, execSleep, and
// the read-only verbs delegate to action.Engine.Resolve.
// The Action Engine is responsible for world mutations
// (talk creates a memory + trust delta, travel moves the
// player + advances time, sleep advances time) and text
// rendering (delegated to the Narrator).
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

	"github.com/chronicle-dev/chronicle/internal/action"
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
	// Action is the action engine (Phase 17.5) that
	// mutates the world and renders text for the 12 spec
	// verbs. Optional: when nil, the REPL falls back to
	// the Phase 17.3 read-only stubs (no world mutations).
	// When supplied, execTalk/execTravel/execSleep and the
	// read-only verbs all delegate to action.Engine.Resolve.
	// Constructed by the CLI layer from the world + narrator.
	Action *action.Engine
}

// REPL is the in-game command loop. Construct via New.
type REPL struct {
	world    *core.World
	parser   *intent.Parser
	narrator *narrator.Narrator
	action   *action.Engine
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
		action:   opts.Action,
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
// for REPL meta-commands (quit, exit, help, people, auto-tick,
// advance); everything else goes through the intent parser.
func (r *REPL) execute(ctx context.Context, line string) error {
	lower := strings.ToLower(line)
	switch lower {
	case "quit", "exit":
		fmt.Fprintln(r.out, "Goodbye.")
		r.running = false
		return nil
	case "help", "?":
		r.execHelp()
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
		if r.action != nil {
			res, err := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionSave, Target: in.Target})
			if err != nil {
				fmt.Fprintf(r.out, "error: %v\n", err)
				return nil
			}
			fmt.Fprintln(r.out, res.Text)
			return nil
		}
		return r.execSaveLegacy(in.Target)
	case intent.ActionBuy, intent.ActionSell:
		if r.action != nil {
			res, err := r.action.Resolve(ctxTODO(), intent.Intent{Action: in.Action, Target: in.Target, Args: intent.Args{Quantity: in.Args.Quantity}})
			if err != nil {
				fmt.Fprintf(r.out, "error: %v\n", err)
				return nil
			}
			fmt.Fprintln(r.out, res.Text)
			return nil
		}
		fmt.Fprintf(r.out, "%s is not yet implemented (Phase 17.4+).\n", in.Action)
	case intent.ActionBranch, intent.ActionSwitch:
		if r.action != nil {
			res, err := r.action.Resolve(ctxTODO(), intent.Intent{Action: in.Action, Target: in.Target})
			if err != nil {
				fmt.Fprintf(r.out, "error: %v\n", err)
				return nil
			}
			fmt.Fprintln(r.out, res.Text)
			return nil
		}
		fmt.Fprintf(r.out, "%s is not yet implemented (Phase 17.7+).\n", in.Action)
	default:
		return fmt.Errorf("unknown action %q", in.Action)
	}
	return nil
}

// execTime prints the current tick and the simulated date.
// Phase 17.5: delegates to action.Engine.Resolve.
func (r *REPL) execTime() {
	if r.action != nil {
		res, _ := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionTime})
		fmt.Fprintln(r.out, res.Text)
		return
	}
	fmt.Fprintf(r.out, "Tick %d (%s)\n", r.world.Tick, r.world.Now.Format("2006-01-02"))
}

// execHelp prints the REPL's command reference: the 12 spec
// verbs grouped by category, the meta-commands, and a one-line
// pointer to the spec for the full grammar. The output is
// static (no world state), so the help is the same on every
// prompt. `?` is a short alias for `help`.
func (r *REPL) execHelp() {
	fmt.Fprintln(r.out, helpText())
}

// helpText is the canonical help string printed by `help` and
// `?`. Pulled out as a function so tests can assert on the
// exact content and future changes only need to be made in
// one place.
func helpText() string {
	return `Chronicle REPL — commands

Reading
  look                  Show your current location and the people there.
  look <name|place>     Show a specific person or location.
  inspect <name>        Show a person's details (same as "look <name>").
  people                List all alive people in the world.
  time                  Show the current sim tick and date.

Acting
  talk <name>           Talk to a person (creates a memory + trust delta).
  travel <place>        Travel to a location (advances 1 tick).
  sleep [hours]         Sleep (default 8 hours; advances time).
  buy <item> [qty]      Buy from a merchant at your location.
  sell <item> [qty]     Sell to a merchant at your location.
  inventory             Show your inventory and coin.

Saving & branching
  save [path.db]        Snapshot the world to a SQLite file.
  branch <name>         Save a named branch of the current world.
  switch <name>         Restore a named branch into the current world.
  info                  Hint: use "chronicle info <path.db>" from the shell.

Pacing
  advance day           Advance 1 sim day.
  advance week          Advance 7 sim days.
  advance month         Advance 30 sim days.
  auto-tick on|off      Toggle auto-advance (one tick before each prompt).

Meta
  help, ?               Show this help text.
  quit, exit            Leave the REPL.

Tip: type "people" to see who's alive, then "talk <name>" to start a conversation.
`
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
// Phase 17.5: delegates to action.Engine.Resolve.
func (r *REPL) execLook(target string) {
	if r.action != nil {
		res, _ := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionLook, Target: target})
		if !res.OK {
			fmt.Fprintln(r.out, res.Text)
			return
		}
		fmt.Fprintln(r.out, res.Text)
		return
	}
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
// Phase 17.5: delegates to action.Engine.Resolve.
func (r *REPL) execInspect(target string) {
	if r.action != nil {
		res, _ := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionInspect, Target: target})
		if !res.OK {
			fmt.Fprintln(r.out, res.Text)
			return
		}
		fmt.Fprintln(r.out, res.Text)
		return
	}
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
// Phase 17.5: delegates to action.Engine.Resolve which
// creates a memory record, applies a trust delta, and
// returns rendered text. When no Action engine is set,
// prints a Phase 17.3 stub for backwards compatibility.
func (r *REPL) execTalk(target string) {
	if r.action != nil {
		res, err := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionTalk, Target: target})
		if err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			return
		}
		if !res.OK {
			fmt.Fprintln(r.out, res.Text)
			return
		}
		fmt.Fprintln(r.out, res.Text)
		return
	}
	p := r.findPerson(target)
	if p == nil {
		fmt.Fprintf(r.out, "I don't see %q here.\n", target)
		return
	}
	fmt.Fprintf(r.out, "You talk to %s.\n", p.Name)
}

// execTravel renders narration for traveling to a location.
// Phase 17.5: delegates to action.Engine.Resolve which
// moves the player, advances time, and returns rendered
// text. When no Action engine is set, prints a Phase 17.3
// stub.
func (r *REPL) execTravel(target string) {
	if r.action != nil {
		res, err := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionTravel, Target: target})
		if err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			return
		}
		if !res.OK {
			fmt.Fprintln(r.out, res.Text)
			return
		}
		fmt.Fprintln(r.out, res.Text)
		return
	}
	l := r.findLocation(target)
	if l == nil {
		fmt.Fprintf(r.out, "I don't know the location %q.\n", target)
		return
	}
	fmt.Fprintf(r.out, "You travel to %s.\n", l.Name)
}

// execInventory is a stub: the player concept (and thus
// inventory) is added in a later phase. The command is
// accepted and acknowledged so the parser wiring is
// exercised.
// Phase 17.5: delegates to action.Engine.Resolve.
func (r *REPL) execInventory() {
	if r.action != nil {
		res, _ := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionInventory})
		fmt.Fprintln(r.out, res.Text)
		return
	}
	fmt.Fprintln(r.out, "Inventory: (not yet implemented — Phase 17.4+)")
}

// execSleep advances the simulation by hours/24 ticks
// (rounded up to at least 1). Phase 17.5: delegates to
// action.Engine.Resolve which advances the world's tick
// and clock. When no Action engine is set, uses the
// legacy TickFn path.
func (r *REPL) execSleep(hours int) {
	if r.action != nil {
		res, err := r.action.Resolve(ctxTODO(), intent.Intent{Action: intent.ActionSleep, Args: intent.Args{Hours: hours}})
		if err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			return
		}
		fmt.Fprintln(r.out, res.Text)
		return
	}
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

// execSaveLegacy is the Phase 17.3 save path, kept as a
// fallback when no action engine is configured. Phase
// 17.6 routes save through action.Engine.Resolve instead;
// this remains for backwards compatibility with tests that
// don't supply an action engine.
func (r *REPL) execSaveLegacy(path string) error {
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

// ctxTODO returns a fresh background context. Used for
// action.Engine.Resolve calls that don't need REPL
// cancellation propagation (Phase 17.5; Phase 18+ can
// thread the REPL's Run context through).
func ctxTODO() context.Context {
	return context.Background()
}
