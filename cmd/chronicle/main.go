// Command chronicle is the entry point for the Chronicle simulation engine.
//
// Phase 17.3: in-game REPL available via `-repl` on play/resume.
//// chronicle [play-flags]              - default; load a worldpack, bootstrap, simulate
//	chronicle save [flags]              - play + snapshot the post-tick world to a SQLite DB
//	chronicle resume <db-path> [-ticks] - restore from a SQLite snapshot, simulate more
//	chronicle info <db-path>            - print snapshot metadata (no ticks run)
//	chronicle diff <db1> <db2>          - compare two snapshots (metadata, rules, people, social)
//	chronicle doctor                    - check OPENCODE_ZEN_API_KEY and endpoint reachability
//
// The play and resume subcommands accept a `-repl` flag that
// drops the user into an in-game REPL (Phase 17.3) after the
// initial ticks. The REPL accepts commands like `time`, `look`,
// `talk alice`, `advance day`, `auto-tick on`, and `quit`.
// Typed commands are dispatched through the intent parser
// (Phase 17.2); the REPL just executes the resulting Intent
// against the current World.
//
// The save subcommand accepts two flags that wire a death-detection
// hook into the play workflow:
//
//	-auto-resume          - if set, auto-resume the saved DB when the
//	                        post-save world is in a "game over" state
//	                        (no alive people).
//	-auto-resume-ticks N  - number of ticks to run on auto-resume
//	                        (default 100, same as the resume subcommand).
//
// This is useful for long-horizon runs where extinction is possible:
// `chronicle save -auto-resume -ticks 3650 -seed 12345` plays 10
// years, snapshots, and if everyone died, immediately runs 100 more
// ticks on the snapshot in an attempt to recover.
//
// The info subcommand is a read-only inspection tool: it opens a
// snapshot, prints ID/seed/tick/rules/people-counts, and closes
// without running any ticks. Use it to verify a save before
// resuming, or to debug a misbehaving world.
//
// The diff subcommand is read-only across both inputs: it restores
// both worlds, prints a side-by-side report of what changed, and
// closes both DBs without writing back. Use it for branching
// timelines (save A, fork into B, then diff to see the delta) or to
// compare two runs of the same seed with different rules.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/intent"
	"github.com/chronicle-dev/chronicle/internal/llm"
	"github.com/chronicle-dev/chronicle/internal/narrator"
	"github.com/chronicle-dev/chronicle/internal/persistence"
	"github.com/chronicle-dev/chronicle/internal/repl"
	"github.com/chronicle-dev/chronicle/internal/simulation"
	"github.com/chronicle-dev/chronicle/internal/tick"
	"github.com/chronicle-dev/chronicle/internal/worldpack"
)

const version = "0.0.0"

func main() {
	// Subcommand dispatch. Explicit subcommands are matched first;
	// anything else (including no args) defaults to the play workflow.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "resume":
			if err := runResumeCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
				os.Exit(1)
			}
			return
		case "save":
			if err := runSaveCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
				os.Exit(1)
			}
			return
		case "info":
			if err := runInfoCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
				os.Exit(1)
			}
			return
		case "diff":
			if err := runDiffCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
				os.Exit(1)
			}
			return
		case "doctor":
			if err := runDoctorCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}
	if err := runPlayCmd(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
		os.Exit(1)
	}
}

// runPlayCmd is the default "play from scratch" workflow.
// Loads a worldpack, bootstraps a fresh world, and runs N ticks.
// Does not save; use `chronicle save` for that.
func runPlayCmd(args []string) error {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	packDir := fs.String("pack", "worldpacks/frontier", "Path to worldpack directory containing the six YAML files")
	ticks := fs.Int("ticks", 100, "Number of ticks to run (1 tick = 1 simulated day)")
	seed := fs.Int64("seed", 12345, "World seed for deterministic RNG")
	replFlag := fs.Bool("repl", false, "If set, drop into the in-game REPL after the initial ticks")
	if err := fs.Parse(args); err != nil {
		return err
	}

	w, err := runPlay(*packDir, *ticks, *seed)
	if err != nil {
		return err
	}
	printSummary(w, "Final state")
	if *replFlag {
		return enterREPL(w)
	}
	return nil
}

// runPlay is the testable core of the play workflow. It loads a
// worldpack, bootstraps a fresh world, and runs numTicks ticks. The
// returned world is the post-tick state. Does not touch the
// filesystem beyond reading the worldpack directory.
func runPlay(packDir string, numTicks int, seed int64) (*core.World, error) {
	fmt.Fprintf(os.Stderr, "chronicle v%s\n", version)

	// 1. Load the worldpack
	pack, err := worldpack.Load(packDir)
	if err != nil {
		return nil, fmt.Errorf("load pack: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Loaded pack %q: %d locations, %d factions, %d occupations, %d action rules\n",
		pack.Region.Name, len(pack.Locations), len(pack.Factions),
		len(pack.Occupations), len(pack.ActionRules))

	// 2. Create a fresh world
	worldID := fmt.Sprintf("%08x", uint32(seed))
	w := core.NewWorld(worldID, seed, time.Date(1400, 1, 1, 0, 0, 0, 0, time.UTC))

	// 3. Bootstrap from the pack
	if err := worldpack.Bootstrap(pack, w, seed); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	alive := 0
	for _, p := range w.People {
		if p.Alive {
			alive++
		}
	}
	fmt.Fprintf(os.Stderr, "Bootstrapped world %q (seed=%d): %d people across %d locations\n",
		w.ID, seed, alive, len(w.Locations))

	// 4. Wire up the simulation engines. Tick order per
	// SIMULATION_TICK_SPEC.md §2: Population → Relationship → Goal
	// → Memory. The MemoryEngine is given a reference to the
	// RelationshipEngine so it can call ApplyMemoryDeltas at
	// memory creation time (the O(1) cached-aggregate pattern per
	// spec §5.2).
	popEng := simulation.NewPopulationEngine()
	relEng := simulation.NewRelationshipEngine()
	memEng := &simulation.MemoryEngine{RelationshipEngine: relEng}
	sim := tick.NewSimulation(seed,
		popEng,
		relEng,
		simulation.NewGoalEngine(),
		memEng,
	)
	if err := sim.Init(w); err != nil {
		return nil, fmt.Errorf("sim init: %w", err)
	}

	// 5. Run ticks
	for i := 0; i < numTicks; i++ {
		if err := sim.Tick(w); err != nil {
			return nil, fmt.Errorf("tick %d: %w", i, err)
		}
	}

	return w, nil
}

// SaveOptions controls optional behavior of runSaveWithOptions. The
// zero value is safe: no auto-resume, no custom resume fn. The CLI
// layer (runSaveCmd) populates this struct from its flags.
//
// ResumeFn is the function called when auto-resume fires. The CLI
// passes runResume; tests pass a mock to record the call. If
// AutoResume is true and ResumeFn is nil, runSaveWithOptions
// returns an error.
type SaveOptions struct {
	AutoResume      bool
	AutoResumeTicks int
	ResumeFn        func(dbPath string, numTicks int) (*core.World, error)
}

// runSaveCmd is the "play + snapshot" workflow. Runs the play workflow
// and then writes the post-tick world to a SQLite snapshot. A later
// `chronicle resume <path>` can pick up the saved state.
//
// Usage:
//
//	chronicle save [flags]
//
// Flags mirror `chronicle [play-flags]` plus `-out <path>` and the
// auto-resume pair. The default output path is <world-id>.db,
// computed from the seed.
func runSaveCmd(args []string) error {
	fs := flag.NewFlagSet("save", flag.ContinueOnError)
	packDir := fs.String("pack", "worldpacks/frontier", "Path to worldpack directory containing the six YAML files")
	ticks := fs.Int("ticks", 100, "Number of ticks to run before snapshotting (1 tick = 1 simulated day)")
	seed := fs.Int64("seed", 12345, "World seed for deterministic RNG")
	outPath := fs.String("out", "", "Output snapshot path; default is <world-id>.db (world-id derived from seed)")
	autoResume := fs.Bool("auto-resume", false, "If set, auto-resume the saved DB when the post-save world is in a game-over state (no alive people)")
	autoResumeTicks := fs.Int("auto-resume-ticks", 100, "Number of ticks to run on auto-resume (only meaningful with -auto-resume)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	opts := SaveOptions{
		AutoResume:      *autoResume,
		AutoResumeTicks: *autoResumeTicks,
		ResumeFn:        runResume,
	}
	w, resolved, autoResumed, err := runSaveWithOptions(*packDir, *ticks, *seed, *outPath, opts)
	if err != nil {
		return err
	}
	if autoResumed {
		// runResume (invoked from inside saveAndMaybeResume) already
		// printed its own summary. Don't print a second one here.
		fmt.Fprintf(os.Stderr, "Auto-resume complete; final state shown above.\n")
		return nil
	}
	printSummary(w, fmt.Sprintf("Final state (saved to %s)", resolved))
	return nil
}

// runSave is the back-compat wrapper used by the existing tests. It
// calls runSaveWithOptions with default options (no auto-resume)
// and discards the autoResumed bool.
func runSave(packDir string, numTicks int, seed int64, outPath string) (*core.World, string, error) {
	w, path, _, err := runSaveWithOptions(packDir, numTicks, seed, outPath, SaveOptions{
		ResumeFn: runResume,
	})
	return w, path, err
}

// runSaveWithOptions is the full testable core of the save subcommand
// with options. It calls runPlay to obtain the post-tick world, then
// delegates to saveAndMaybeResume for the snapshot + optional
// auto-resume. The third return value is true if auto-resume
// actually fired (caller can use this to decide whether to print a
// summary, since the inner runResume already printed one).
func runSaveWithOptions(packDir string, numTicks int, seed int64, outPath string, opts SaveOptions) (*core.World, string, bool, error) {
	w, err := runPlay(packDir, numTicks, seed)
	if err != nil {
		return nil, "", false, err
	}
	return saveAndMaybeResume(w, outPath, opts)
}

// saveAndMaybeResume writes w to a SQLite snapshot at outPath
// (defaulting to <world-id>.db if empty) and, if opts.AutoResume is
// true and the world is in a game-over state, calls opts.ResumeFn
// to run additional ticks. The returned world is the post-resume
// state (if auto-resume fired) or the post-save state (otherwise).
// The third return value is true iff auto-resume actually fired.
//
// This is split out from runSaveWithOptions so tests can drive it
// with a synthetic world (no pack needed) and a mock ResumeFn.
func saveAndMaybeResume(w *core.World, outPath string, opts SaveOptions) (*core.World, string, bool, error) {
	// Default the output path to <world-id>.db if the caller didn't
	// specify one. This makes `chronicle save -seed 12345` write to
	// 00003039.db, which is unique per seed and round-trippable.
	if outPath == "" {
		outPath = w.ID + ".db"
	}

	db, err := persistence.Open(outPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("open %s: %w", outPath, err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		return nil, "", false, fmt.Errorf("migrate: %w", err)
	}
	if err := db.Snapshot(w); err != nil {
		return nil, "", false, fmt.Errorf("snapshot: %w", err)
	}

	if opts.AutoResume && isGameOver(w) {
		if opts.ResumeFn == nil {
			return nil, "", false, fmt.Errorf("auto-resume enabled but SaveOptions.ResumeFn is nil")
		}
		fmt.Fprintf(os.Stderr, "Game over detected (0 alive people); auto-resuming %s for %d ticks\n",
			outPath, opts.AutoResumeTicks)
		resumed, err := opts.ResumeFn(outPath, opts.AutoResumeTicks)
		if err != nil {
			return nil, "", false, fmt.Errorf("auto-resume: %w", err)
		}
		return resumed, outPath, true, nil
	}

	return w, outPath, false, nil
}

// isGameOver reports whether the world has reached a terminal state
// in which no further simulation is possible. The current definition
// is "no alive people" (extinction). Future hooks could detect
// "no fertile couples", "no one of working age", etc.
func isGameOver(w *core.World) bool {
	for _, p := range w.People {
		if p.Alive {
			return false
		}
	}
	return true
}

// runInfoCmd is the "inspect a snapshot" workflow. Opens a SQLite
// snapshot, restores the world into memory, and prints its metadata
// (ID, seed, tick, world-time, rules summary, people counts, location
// count). Does NOT run any ticks — this is a read-only inspection
// command. Use it to verify a save before resuming, or to debug a
// misbehaving world without committing to more simulation.
//
// Usage:
//
//	chronicle info <db-path>
func runInfoCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: chronicle info <db-path>")
	}
	return runInfo(args[0])
}

// runInfo is the testable core of the info subcommand. It opens the
// SQLite snapshot at dbPath, runs migrations, restores the world
// into memory, and prints a one-shot metadata report to stderr.
// The DB is closed before returning; no ticks are run, no world is
// returned. Read-only: the DB file is not mutated.
func runInfo(dbPath string) error {
	fmt.Fprintf(os.Stderr, "chronicle v%s\n", version)

	w, closer, err := openSnapshot(dbPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer closer()

	// Header.
	fmt.Fprintf(os.Stderr, "\n--- Info for %s ---\n", dbPath)

	// Metadata.
	fmt.Fprintf(os.Stderr, "World ID:  %s\n", w.ID)
	fmt.Fprintf(os.Stderr, "Seed:      %d\n", w.Seed)
	fmt.Fprintf(os.Stderr, "Tick:      %d\n", w.Tick)
	if w.Now.IsZero() {
		fmt.Fprintf(os.Stderr, "Now:       (unset)\n")
	} else {
		fmt.Fprintf(os.Stderr, "Now:       %s\n", w.Now.Format(time.RFC3339))
	}

	// World rules. nil rules means the world was snapshotted without
	// a worldpack (legacy or test world); flag this for the user.
	if w.Rules == nil {
		fmt.Fprintf(os.Stderr, "\nWorld Rules: (none, engine defaults will be used)\n")
	} else {
		fmt.Fprintf(os.Stderr, "\nWorld Rules:\n")
		fmt.Fprintf(os.Stderr, "  AdultAge:               %d\n", w.Rules.AdultAge)
		fmt.Fprintf(os.Stderr, "  FertileMinAge:          %d\n", w.Rules.FertileMinAge)
		fmt.Fprintf(os.Stderr, "  FertileMaxAge:          %d\n", w.Rules.FertileMaxAge)
		fmt.Fprintf(os.Stderr, "  AnnualDeathChance:      %f\n", w.Rules.AnnualDeathChance)
		fmt.Fprintf(os.Stderr, "  MinBirthIntervalTicks:  %d\n", w.Rules.MinBirthIntervalTicks)
		fmt.Fprintf(os.Stderr, "  MaxChildren:            %d\n", w.Rules.MaxChildren)
		fmt.Fprintf(os.Stderr, "  MigrationFraction:      %f\n", w.Rules.MigrationFraction)
		fmt.Fprintf(os.Stderr, "  MinMigrantsPerTick:     %d\n", w.Rules.MinMigrantsPerTick)
	}

	// People + location counts.
	alive := 0
	for _, p := range w.People {
		if p.Alive {
			alive++
		}
	}
	fmt.Fprintf(os.Stderr, "\nPeople:     %d alive / %d total\n", alive, len(w.People))
	fmt.Fprintf(os.Stderr, "Locations:  %d\n", len(w.Locations))

	// Relationships + memories counts. Useful for inspecting a
	// saved world before resuming — shows whether the snapshot
	// contains the social graph and event history you'd expect.
	fmt.Fprintf(os.Stderr, "Relationships:  %d\n", len(w.Relationships))
	fmt.Fprintf(os.Stderr, "Memories:       %d\n", len(w.Memories))

	return nil
}

// runDiffCmd is the "compare two snapshots" workflow. Opens two
// SQLite snapshots, restores both worlds, and prints a side-by-side
// comparison of their metadata, rules, and people. Read-only: neither
// DB is mutated. Use it for branching timelines: save world A, fork
// into B with different rules/seeds, then diff to see what changed.
//
// Usage:
//
//	chronicle diff <db1> <db2>
func runDiffCmd(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: chronicle diff <db1> <db2>")
	}
	return runDiff(args[0], args[1])
}

// runDiff is the testable core of the diff subcommand. It opens
// both DBs, runs migrations, restores both worlds, and prints a
// one-shot comparison report to stderr. Neither DB is mutated.
func runDiff(dbPath1, dbPath2 string) error {
	fmt.Fprintf(os.Stderr, "chronicle v%s\n", version)

	w1, close1, err := openSnapshot(dbPath1)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath1, err)
	}
	defer close1()

	w2, close2, err := openSnapshot(dbPath2)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath2, err)
	}
	defer close2()

	// Header.
	fmt.Fprintf(os.Stderr, "\n--- Diff: %s vs %s ---\n", dbPath1, dbPath2)

	// Metadata (ID, seed, tick).
	fmt.Fprintf(os.Stderr, "\nMetadata:\n")
	diffMetadata(w1, w2)

	// World rules (8 fields).
	fmt.Fprintf(os.Stderr, "\nWorld Rules:\n")
	diffRules(w1, w2)

	// People (added / removed / changed, plus alive/total counts).
	fmt.Fprintf(os.Stderr, "\nPeople:\n")
	diffPeople(w1, w2)

	// Relationships + memories counts (just the totals — we don't
	// try to diff individual relationships or memories, since that
	// would dominate the output for a world with thousands of
	// records).
	fmt.Fprintf(os.Stderr, "\nSocial:\n")
	fmt.Fprintf(os.Stderr, "  Relationships:  %d  ->  %d", len(w1.Relationships), len(w2.Relationships))
	if len(w1.Relationships) != len(w2.Relationships) {
		fmt.Fprintf(os.Stderr, " (CHANGED)")
	}
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "  Memories:       %d  ->  %d", len(w1.Memories), len(w2.Memories))
	if len(w1.Memories) != len(w2.Memories) {
		fmt.Fprintf(os.Stderr, " (CHANGED)")
	}
	fmt.Fprintf(os.Stderr, "\n")

	// Top-level summary so the user can scan a single line to know
	// whether anything changed.
	metaChanged := metaDiffCount(w1, w2)
	rulesChanged := rulesDiffCount(w1, w2)
	peopleAdded, peopleRemoved, peopleChanged := peopleDiffCounts(w1, w2)
	relChanged := 0
	if len(w1.Relationships) != len(w2.Relationships) {
		relChanged = 1
	}
	memChanged := 0
	if len(w1.Memories) != len(w2.Memories) {
		memChanged = 1
	}
	total := metaChanged + rulesChanged + peopleAdded + peopleRemoved + peopleChanged + relChanged + memChanged
	if total == 0 {
		fmt.Fprintf(os.Stderr, "\n--- 0 differences ---\n")
	} else {
		fmt.Fprintf(os.Stderr, "\n--- %d differences: %d metadata, %d rules, %d added, %d removed, %d changed, %d relationships, %d memories ---\n",
			total, metaChanged, rulesChanged, peopleAdded, peopleRemoved, peopleChanged, relChanged, memChanged)
	}

	return nil
}

// metaDiffCount returns the number of metadata fields that differ
// between w1 and w2 (out of 3: ID, seed, tick).
func metaDiffCount(w1, w2 *core.World) int {
	n := 0
	if w1.ID != w2.ID {
		n++
	}
	if w1.Seed != w2.Seed {
		n++
	}
	if w1.Tick != w2.Tick {
		n++
	}
	return n
}

// rulesDiffCount returns the number of WorldRules fields that differ.
// Returns 0 if either side has nil rules (treated as "different in
// a way not counted as field-level changes" — the summary just
// notes the nil case separately).
func rulesDiffCount(w1, w2 *core.World) int {
	if w1.Rules == nil || w2.Rules == nil {
		return 0
	}
	n := 0
	if w1.Rules.AdultAge != w2.Rules.AdultAge {
		n++
	}
	if w1.Rules.FertileMinAge != w2.Rules.FertileMinAge {
		n++
	}
	if w1.Rules.FertileMaxAge != w2.Rules.FertileMaxAge {
		n++
	}
	if w1.Rules.AnnualDeathChance != w2.Rules.AnnualDeathChance {
		n++
	}
	if w1.Rules.MinBirthIntervalTicks != w2.Rules.MinBirthIntervalTicks {
		n++
	}
	if w1.Rules.MaxChildren != w2.Rules.MaxChildren {
		n++
	}
	if w1.Rules.MigrationFraction != w2.Rules.MigrationFraction {
		n++
	}
	if w1.Rules.MinMigrantsPerTick != w2.Rules.MinMigrantsPerTick {
		n++
	}
	return n
}

// peopleDiffCounts returns (added, removed, changed) counts for
// diffPeople.
func peopleDiffCounts(w1, w2 *core.World) (int, int, int) {
	added, removed, changed := 0, 0, 0
	for id, person2 := range w2.People {
		person1, ok := w1.People[id]
		if !ok {
			added++
			continue
		}
		if len(personDiffs(person1, person2)) > 0 {
			changed++
		}
	}
	for id := range w1.People {
		if _, ok := w2.People[id]; !ok {
			removed++
		}
	}
	return added, removed, changed
}

// openSnapshot is a small helper that opens a DB, migrates, and
// restores into a fresh world. Returns the world and a closer func
// that closes the DB (discarding the error — callers use defer and
// don't act on close errors). Used by both runInfo and runDiff.
func openSnapshot(dbPath string) (*core.World, func(), error) {
	db, err := persistence.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	w := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(w); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return w, func() { _ = db.Close() }, nil
}

// diffMetadata prints the side-by-side comparison of the two worlds'
// top-level metadata: ID, seed, tick.
func diffMetadata(w1, w2 *core.World) {
	if w1.ID != w2.ID {
		fmt.Fprintf(os.Stderr, "  World ID:  %q -> %q (CHANGED)\n", w1.ID, w2.ID)
	} else {
		fmt.Fprintf(os.Stderr, "  World ID:  %q (same)\n", w1.ID)
	}
	if w1.Seed != w2.Seed {
		fmt.Fprintf(os.Stderr, "  Seed:      %d -> %d (CHANGED)\n", w1.Seed, w2.Seed)
	} else {
		fmt.Fprintf(os.Stderr, "  Seed:      %d (same)\n", w1.Seed)
	}
	if w1.Tick != w2.Tick {
		delta := w2.Tick - w1.Tick
		fmt.Fprintf(os.Stderr, "  Tick:      %d -> %d (db2 is %+d)\n", w1.Tick, w2.Tick, delta)
	} else {
		fmt.Fprintf(os.Stderr, "  Tick:      %d (same)\n", w1.Tick)
	}
}

// diffRules prints the field-by-field comparison of the two worlds'
// WorldRules. Handles the nil cases: both nil, one nil, both present.
func diffRules(w1, w2 *core.World) {
	switch {
	case w1.Rules == nil && w2.Rules == nil:
		fmt.Fprintf(os.Stderr, "  (both worlds have no rules)\n")
		return
	case w1.Rules == nil:
		fmt.Fprintf(os.Stderr, "  (db1 has no rules; db2 has rules)\n")
		fmt.Fprintf(os.Stderr, "  db2 rules:\n")
		printRulesBlock("    ", w2.Rules)
		return
	case w2.Rules == nil:
		fmt.Fprintf(os.Stderr, "  (db2 has no rules; db1 has rules)\n")
		fmt.Fprintf(os.Stderr, "  db1 rules:\n")
		printRulesBlock("    ", w1.Rules)
		return
	}
	// Both present; compare field by field.
	type ruleField struct {
		name string
		a, b interface{}
	}
	fields := []ruleField{
		{"AdultAge", w1.Rules.AdultAge, w2.Rules.AdultAge},
		{"FertileMinAge", w1.Rules.FertileMinAge, w2.Rules.FertileMinAge},
		{"FertileMaxAge", w1.Rules.FertileMaxAge, w2.Rules.FertileMaxAge},
		{"AnnualDeathChance", w1.Rules.AnnualDeathChance, w2.Rules.AnnualDeathChance},
		{"MinBirthIntervalTicks", w1.Rules.MinBirthIntervalTicks, w2.Rules.MinBirthIntervalTicks},
		{"MaxChildren", w1.Rules.MaxChildren, w2.Rules.MaxChildren},
		{"MigrationFraction", w1.Rules.MigrationFraction, w2.Rules.MigrationFraction},
		{"MinMigrantsPerTick", w1.Rules.MinMigrantsPerTick, w2.Rules.MinMigrantsPerTick},
	}
	changed := 0
	for _, f := range fields {
		if f.a != f.b {
			fmt.Fprintf(os.Stderr, "  %-26s %v -> %v (CHANGED)\n", f.name+":", f.a, f.b)
			changed++
		}
	}
	if changed == 0 {
		fmt.Fprintf(os.Stderr, "  (all 8 rules fields are equal)\n")
	}
}

// printRulesBlock prints all 8 WorldRules fields with a per-line
// prefix. Used by diffRules when one side has no rules.
func printRulesBlock(prefix string, r *core.WorldRules) {
	fmt.Fprintf(os.Stderr, "%sAdultAge:               %d\n", prefix, r.AdultAge)
	fmt.Fprintf(os.Stderr, "%sFertileMinAge:          %d\n", prefix, r.FertileMinAge)
	fmt.Fprintf(os.Stderr, "%sFertileMaxAge:          %d\n", prefix, r.FertileMaxAge)
	fmt.Fprintf(os.Stderr, "%sAnnualDeathChance:      %f\n", prefix, r.AnnualDeathChance)
	fmt.Fprintf(os.Stderr, "%sMinBirthIntervalTicks:  %d\n", prefix, r.MinBirthIntervalTicks)
	fmt.Fprintf(os.Stderr, "%sMaxChildren:            %d\n", prefix, r.MaxChildren)
	fmt.Fprintf(os.Stderr, "%sMigrationFraction:      %f\n", prefix, r.MigrationFraction)
	fmt.Fprintf(os.Stderr, "%sMinMigrantsPerTick:     %d\n", prefix, r.MinMigrantsPerTick)
}

// diffPeople prints the people-level comparison: added (in db2 not
// db1), removed (in db1 not db2), changed (same ID, different
// fields), plus alive/total counts for both worlds.
func diffPeople(w1, w2 *core.World) {
	var added, removed, changed []string
	for id, person2 := range w2.People {
		person1, ok := w1.People[id]
		if !ok {
			added = append(added, id)
			continue
		}
		if diffs := personDiffs(person1, person2); len(diffs) > 0 {
			changed = append(changed, fmt.Sprintf("%s (%s)", id, strings.Join(diffs, ", ")))
		}
	}
	for id := range w1.People {
		if _, ok := w2.People[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		fmt.Fprintf(os.Stderr, "  (no people differences)\n")
	} else {
		if len(added) > 0 {
			fmt.Fprintf(os.Stderr, "  Added (%d):    %s\n", len(added), strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			fmt.Fprintf(os.Stderr, "  Removed (%d):  %s\n", len(removed), strings.Join(removed, ", "))
		}
		if len(changed) > 0 {
			fmt.Fprintf(os.Stderr, "  Changed (%d):\n", len(changed))
			for _, c := range changed {
				fmt.Fprintf(os.Stderr, "    %s\n", c)
			}
		}
	}

	// Alive/total counts for both worlds.
	alive1 := 0
	for _, p := range w1.People {
		if p.Alive {
			alive1++
		}
	}
	alive2 := 0
	for _, p := range w2.People {
		if p.Alive {
			alive2++
		}
	}
	fmt.Fprintf(os.Stderr, "  Counts:  %d alive / %d total  ->  %d alive / %d total\n",
		alive1, len(w1.People), alive2, len(w2.People))
}

// personDiffs returns a list of field-level differences between two
// persons with the same ID. Empty list means they're identical.
// Compares the 9 mutable Person fields: Alive, LocationID, Class,
// Occupation, SpouseID, FatherID, MotherID, BirthTick, DeathTick.
//
// Note: Name and Gender are intentionally excluded. Name is a
// display label (not game state) and Gender is fixed at birth
// (the engine never changes it), so neither contributes to a
// meaningful timeline diff.
func personDiffs(a, b *core.Person) []string {
	var diffs []string
	if a.Alive != b.Alive {
		diffs = append(diffs, fmt.Sprintf("Alive: %v -> %v", a.Alive, b.Alive))
	}
	if a.LocationID != b.LocationID {
		diffs = append(diffs, fmt.Sprintf("LocationID: %q -> %q", a.LocationID, b.LocationID))
	}
	if a.Class != b.Class {
		diffs = append(diffs, fmt.Sprintf("Class: %q -> %q", a.Class, b.Class))
	}
	if a.Occupation != b.Occupation {
		diffs = append(diffs, fmt.Sprintf("Occupation: %q -> %q", a.Occupation, b.Occupation))
	}
	if a.SpouseID != b.SpouseID {
		diffs = append(diffs, fmt.Sprintf("SpouseID: %q -> %q", a.SpouseID, b.SpouseID))
	}
	if a.FatherID != b.FatherID {
		diffs = append(diffs, fmt.Sprintf("FatherID: %q -> %q", a.FatherID, b.FatherID))
	}
	if a.MotherID != b.MotherID {
		diffs = append(diffs, fmt.Sprintf("MotherID: %q -> %q", a.MotherID, b.MotherID))
	}
	if a.BirthTick != b.BirthTick {
		diffs = append(diffs, fmt.Sprintf("BirthTick: %d -> %d", a.BirthTick, b.BirthTick))
	}
	if a.DeathTick != b.DeathTick {
		diffs = append(diffs, fmt.Sprintf("DeathTick: %d -> %d", a.DeathTick, b.DeathTick))
	}
	return diffs
}

// runDoctorCmd is the "check LLM setup" workflow. Resolves the
// LLM config (env > yaml > default), checks the API key is set,
// and pings the configured endpoint to verify reachability and
// auth. Read-only: no world state is touched, no LLM call is
// made to /v1/chat/completions.
//
// Usage:
//
//	chronicle doctor
func runDoctorCmd(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to llm.yaml (default: skip file lookup)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runDoctor(*configPath)
}

// runDoctor is the testable core of the doctor subcommand. It
// prints a short report of the resolved LLM config and the
// outcome of the ping. Returns nil on full success (key set
// AND endpoint reachable AND auth accepted); returns a non-nil
// error otherwise so the CLI exit code reflects failure.
func runDoctor(configPath string) error {
	fmt.Fprintf(os.Stderr, "chronicle v%s (doctor)\n", version)

	cfg, err := llm.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nLLM config:\n")
	fmt.Fprintf(os.Stderr, "  Endpoint:  %s\n", cfg.Endpoint)
	fmt.Fprintf(os.Stderr, "  Model:     %s\n", cfg.Model)
	fmt.Fprintf(os.Stderr, "  Timeout:   %s\n", cfg.Timeout)
	if cfg.APIKey == "" {
		fmt.Fprintf(os.Stderr, "  API key:   (not set — set %s to a non-empty value)\n", llm.EnvAPIKey)
		fmt.Fprintf(os.Stderr, "\nFAIL: %s is not set.\n", llm.EnvAPIKey)
		return fmt.Errorf("missing %s", llm.EnvAPIKey)
	}
	fmt.Fprintf(os.Stderr, "  API key:   %s\n", maskAPIKey(cfg.APIKey))

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	client := llm.NewClient(
		llm.WithEndpoint(cfg.Endpoint),
		llm.WithAPIKey(cfg.APIKey),
		llm.WithTimeout(cfg.Timeout),
	)
	fmt.Fprintf(os.Stderr, "\nPinging %s ...\n", cfg.Endpoint)
	if err := client.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		return err
	}
	fmt.Fprintf(os.Stderr, "OK: endpoint reachable, auth accepted.\n")
	return nil
}

// maskAPIKey returns a short, non-reversible representation of
// an API key for the doctor's report. Shows the first 4 and
// last 4 characters; replaces the middle with "...". Keys
// shorter than 9 characters are fully masked so a short test
// key like "sk-test" doesn't leak more than it should.
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// runResumeCmd is the "resume from snapshot" workflow.
// Opens a SQLite snapshot, restores the world, and runs N more ticks.
//
// Usage:
//
//	chronicle resume <db-path> [-ticks N]
func runResumeCmd(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	ticks := fs.Int("ticks", 100, "Number of ticks to run after resuming")
	replFlag := fs.Bool("repl", false, "If set, drop into the in-game REPL after the resumed ticks")
	if err := fs.Parse(args); err != nil {
		return err
	}

	positional := fs.Args()
	if len(positional) < 1 {
		return fmt.Errorf("usage: chronicle resume <db-path> [-ticks N]")
	}
	dbPath := positional[0]
	w, err := runResume(dbPath, *ticks)
	if err != nil {
		return err
	}
	if *replFlag {
		return enterREPL(w)
	}
	return nil
}

// enterREPL constructs and runs the in-game REPL on the given
// world. It creates a fresh *tick.Simulation (whose engines
// operate on the same world) and an *intent.Parser (with the
// LLM client configured from env/yaml/defaults), then drops
// the user into the REPL prompt loop. Blocks until the user
// types `quit`, `exit`, or EOFs stdin.
//
// The simulation created here is independent of any simulation
// used by the caller (e.g., the one that ran the initial ticks
// in runPlay/runResume). Both simulations operate on the same
// world — the engines are stateless readers/writers of world
// state, so the second simulation picks up exactly where the
// first left off.
func enterREPL(w *core.World) error {
	// LLM client: env > yaml > default. If the API key is empty,
	// the LLM fallback will fail with a clear error; the rule
	// parser still works.
	llmCfg, _ := llm.LoadConfig("")
	llmClient := llm.NewClient(
		llm.WithEndpoint(llmCfg.Endpoint),
		llm.WithAPIKey(llmCfg.APIKey),
		llm.WithModel(llmCfg.Model),
		llm.WithTimeout(llmCfg.Timeout),
	)
	parser := intent.New(llmClient, w)

	// Fresh simulation. The engines are stateless; both this
	// sim and the one that ran the initial ticks read from and
	// write to the same world.
	popEng := simulation.NewPopulationEngine()
	relEng := simulation.NewRelationshipEngine()
	memEng := &simulation.MemoryEngine{RelationshipEngine: relEng}
	sim := tick.NewSimulation(w.Seed,
		popEng,
		relEng,
		simulation.NewGoalEngine(),
		memEng,
	)
	if err := sim.Init(w); err != nil {
		return fmt.Errorf("repl: sim init: %w", err)
	}
	tickFn := func() error { return sim.Tick(w) }

	// Narrator (Phase 17.4): renders narrative text for execTalk
	// and execTravel. The LLM client is the same one used by
	// the intent parser — shared config, shared rate limits (in
	// a future phase). If the API key is empty, the Narrator
	// will silently fall back to templates; the REPL still
	// works.
	nar := narrator.New(llmClient, narrator.DefaultMinTicksBetweenCalls)

	r := repl.New(w, parser, repl.Options{
		TickFn:   tickFn,
		Narrator: nar,
		// In defaults to os.Stdin, Out defaults to os.Stderr.
	})
	fmt.Fprintln(os.Stderr, "\nEntering REPL. Type 'quit' or 'exit' to leave, 'help' for a command list.")
	return r.Run(context.Background())
}

// runResume is the testable core of the resume subcommand. It opens
// the SQLite snapshot at dbPath, runs migrations, restores the world,
// wires up the default engines, and runs numTicks more ticks. The
// returned world is the post-tick state.
//
// numTicks may be 0 (load only, useful for inspection or tests).
func runResume(dbPath string, numTicks int) (*core.World, error) {
	fmt.Fprintf(os.Stderr, "chronicle v%s\n", version)

	db, err := persistence.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	w := core.NewWorld("", 0, time.Time{})
	if err := db.Restore(w); err != nil {
		return nil, fmt.Errorf("restore: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Resumed world %q (seed=%d, tick=%d) from %s\n",
		w.ID, w.Seed, w.Tick, dbPath)
	if w.Rules != nil {
		fmt.Fprintf(os.Stderr, "  rules: AnnualDeathChance=%.3f, FertileMinAge=%d, MigrationFraction=%.2f\n",
			w.Rules.AnnualDeathChance, w.Rules.FertileMinAge, w.Rules.MigrationFraction)
	} else {
		fmt.Fprintf(os.Stderr, "  rules: (none, engine defaults will be used)\n")
	}

	// An empty world is a valid state (e.g., all NPCs died mid-game).
	// Warn but do not error: the caller can decide what to do with it.
	if len(w.People) == 0 {
		fmt.Fprintf(os.Stderr, "  warning: snapshot at %s has no people\n", dbPath)
	}

	// Same engine wiring as runPlay: Population → Relationship → Goal
	// → Memory. The MemoryEngine is given a reference to the
	// RelationshipEngine so it can call ApplyMemoryDeltas for any
	// new memories created this tick.
	popEng := simulation.NewPopulationEngine()
	relEng := simulation.NewRelationshipEngine()
	memEng := &simulation.MemoryEngine{RelationshipEngine: relEng}
	sim := tick.NewSimulation(w.Seed,
		popEng,
		relEng,
		simulation.NewGoalEngine(),
		memEng,
	)
	if err := sim.Init(w); err != nil {
		return nil, fmt.Errorf("sim init: %w", err)
	}

	startTick := w.Tick
	for i := 0; i < numTicks; i++ {
		if err := sim.Tick(w); err != nil {
			return nil, fmt.Errorf("tick %d: %w", i, err)
		}
	}

	if numTicks > 0 {
		printSummary(w, fmt.Sprintf("Final state (resumed +%d ticks from tick %d)", numTicks, startTick))
	} else {
		printSummary(w, "Resumed state (no ticks run)")
	}
	return w, nil
}

func printSummary(w *core.World, header string) {
	alive := 0
	for _, p := range w.People {
		if p.Alive {
			alive++
		}
	}
	fmt.Fprintf(os.Stderr, "\n--- %s (tick=%d, %s) ---\n", header, w.Tick, w.Now.Format("2006-01-02"))
	fmt.Fprintf(os.Stderr, "Population: %d alive / %d total\n", alive, len(w.People))

	// Sort location IDs for stable output. Location.Population is the
	// tally from the last RecomputeLocationPopulations call (done in
	// Bootstrap and after the last engine in Simulation); no need to
	// recompute here.
	ids := make([]string, 0, len(w.Locations))
	for id := range w.Locations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		loc := w.Locations[id]
		fmt.Fprintf(os.Stderr, "  %-20s pop=%-3d cap=%-3d pressure=%d\n",
			loc.Name, loc.Population, loc.PopulationCap, loc.Pressure)
	}
	if travelers := len(w.LivingPeopleAt("")); travelers > 0 {
		fmt.Fprintf(os.Stderr, "  %-20s pop=%-3d (no fixed location)\n", "travelers", travelers)
	}
}
