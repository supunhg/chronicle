// `diff` CLI subcommand. §39.E follow-up #1.
//
// `./chronicle diff <path-a> <path-b>` prints field-level
// discrepancies between two SaveGames on stdout, prefixing
// each line with `chronicle diff:` so a downstream grep can
// filter diff output cleanly.
//
// Per-field, per-`FieldName: <left> -> <right>` is the line
// shape — easy to eyeball in a terminal, easy to grep in a
// shell. Maps (Flags, Variables, Relationships, Inventory
// Items) are walked per-key. Slices (Party, EndingsUnlocked,
// TriggeredEvents) are treated as ordered lists; mismatch in
// length or position is surfaced.
//
// Reads via readSaveForInspection (same path as info), so §39.A's
// DisallowUnknownFields and §39.B's load-time version gate fire
// here the same way they fire in `resume`. If either file fails
// to load, that load-error is the return value — diff never
// produces a half-comparison.
//
// Exit semantics follow the standard `diff` convention:
//   - exit 0 if identical (single `chronicle diff: identical` line)
//   - exit 1 if differences found (signalled via errDiffFound)
//   - exit 2 if either file failed to load (load error is
//     distinguishable from differences via errors.Is check in
//     main.go's `case "diff"` dispatcher)
package main

import (
	"fmt"
	"io"
	"reflect"
	"sort"

	"github.com/chronicle-dev/chronicle/internal/state"
)

// runDiff implements the `diff` subcommand. argv is the
// post-subcommand argument vector (e.g., for
// `./chronicle diff a.json b.json`, argv is
// []string{"a.json", "b.json"}). diff takes no flags.
// stdout receives diff lines ("FieldName: <left> -> <right>");
// stderr receives the read/parse-error path.
func runDiff(argv []string, stdout, stderr io.Writer) error {
	flagArgs, positional := splitFlagsPositional(argv)
	if len(flagArgs) != 0 {
		return fmt.Errorf("diff: unrecognized flag %q (no flags today)", flagArgs[0])
	}
	if len(positional) != 2 {
		fmt.Fprintln(stderr, "chronicle diff: usage: chronicle diff <path-a> <path-b>")
		return fmt.Errorf("diff: expected exactly two <path> arguments; got %d", len(positional))
	}
	a, err := readSaveForInspection(positional[0])
	if err != nil {
		return fmt.Errorf("diff: load %q: %w", positional[0], err)
	}
	b, err := readSaveForInspection(positional[1])
	if err != nil {
		return fmt.Errorf("diff: load %q: %w", positional[1], err)
	}
	if diffSaveGames(a, b, stdout) {
		return errDiffFound
	}
	fmt.Fprintln(stdout, "chronicle diff: identical")
	return nil
}

// errDiffFound is the typed sentinel runDiff returns when the
// compared SaveGames differ. main.go's `case "diff"` handler
// uses errors.Is to distinguish this sentinel (exit 1,
// differences found) from wrapped load errors (exit 2,
// something broke before comparison could complete).
var errDiffFound = fmt.Errorf("chronicle diff: differences found")

// diffSaveGames walks the SaveGame struct, printing each field
// that differs and returning true if any difference was found.
// Empty lines or pathological formatting is NOT its job — line
// shape stays grep-friendly (`FieldName: <left> -> <right>`).
func diffSaveGames(a, b state.SaveGame, stdout io.Writer) bool {
	differed := false
	if a.Version != b.Version {
		fmt.Fprintf(stdout, "chronicle diff: Version: %d -> %d\n", a.Version, b.Version)
		differed = true
	}
	if a.WorldState.Tick != b.WorldState.Tick {
		fmt.Fprintf(stdout, "chronicle diff: Tick: %d -> %d\n", a.WorldState.Tick, b.WorldState.Tick)
		differed = true
	}
	if a.WorldState.Protagonist != b.WorldState.Protagonist {
		fmt.Fprintf(stdout,
			"chronicle diff: Protagonist: %q -> %q\n",
			a.WorldState.Protagonist, b.WorldState.Protagonist)
		differed = true
	}
	if a.WorldState.CurrentNodeID != b.WorldState.CurrentNodeID {
		fmt.Fprintf(stdout,
			"chronicle diff: CurrentNodeID: %q -> %q\n",
			a.WorldState.CurrentNodeID, b.WorldState.CurrentNodeID)
		differed = true
	}
	// Maps: per-key walk so insertion/deletion/value-change
	// differences are all surfaced.
	if diffBoolMap("flags", a.WorldState.Flags, b.WorldState.Flags, stdout) {
		differed = true
	}
	if diffIntMap("variables", a.WorldState.Variables, b.WorldState.Variables, stdout) {
		differed = true
	}
	// Relationships (map of structs) — walk per companion.
	if diffRelationshipMap(a.WorldState.Relationships, b.WorldState.Relationships, stdout) {
		differed = true
	}
	// Inventory Items (nested map).
	if diffIntMap("inventory", a.WorldState.Inventory.Items, b.WorldState.Inventory.Items, stdout) {
		differed = true
	}
	// Slices: per-position comparison.
	if !reflect.DeepEqual(a.WorldState.Party, b.WorldState.Party) {
		fmt.Fprintf(stdout,
			"chronicle diff: Party: %v -> %v\n",
			a.WorldState.Party, b.WorldState.Party)
		differed = true
	}
	if !reflect.DeepEqual(a.WorldState.EndingsUnlocked, b.WorldState.EndingsUnlocked) {
		fmt.Fprintf(stdout,
			"chronicle diff: EndingsUnlocked: %v -> %v\n",
			a.WorldState.EndingsUnlocked, b.WorldState.EndingsUnlocked)
		differed = true
	}
	if !reflect.DeepEqual(a.WorldState.TriggeredEvents, b.WorldState.TriggeredEvents) {
		fmt.Fprintf(stdout,
			"chronicle diff: TriggeredEvents: %v -> %v\n",
			a.WorldState.TriggeredEvents, b.WorldState.TriggeredEvents)
		differed = true
	}
	return differed
}

// diffBoolMap walks the union of two map[string]bool,
// prefixing each differing key with `chronicle diff:
// <label>.<key>:`. Returns true if any difference was found.
// Keys are emitted sorted so the output is deterministic.
func diffBoolMap(label string, a, b map[string]bool, stdout io.Writer) bool {
	differed := false
	keys := unionKeys(a, b)
	sort.Strings(keys)
	for _, k := range keys {
		av, aOk := a[k]
		bv, bOk := b[k]
		if av != bv || aOk != bOk {
			fmt.Fprintf(stdout,
				"chronicle diff: %s[%s]: %v -> %v\n",
				label, k, av, bv)
			differed = true
		}
	}
	return differed
}

// diffIntMap is the int-keyed analogue of diffBoolMap.
func diffIntMap(label string, a, b map[string]int, stdout io.Writer) bool {
	differed := false
	keys := unionKeys(a, b)
	sort.Strings(keys)
	for _, k := range keys {
		av, aOk := a[k]
		bv, bOk := b[k]
		if av != bv || aOk != bOk {
			fmt.Fprintf(stdout,
				"chronicle diff: %s[%s]: %v -> %v\n",
				label, k, av, bv)
			differed = true
		}
	}
	return differed
}

// diffRelationshipMap walks per-companion axis differences.
// Each Relationship has three axes (Trust/Affection/Respect);
// we surface each that differs on its own line.
// Companions only in one of the two maps get a "trivial" zero-value
// comparison so the asymmetry is visible.
func diffRelationshipMap(a, b map[string]state.Relationship, stdout io.Writer) bool {
	differed := false
	keys := unionKeys(a, b)
	sort.Strings(keys)
	for _, k := range keys {
		av, aOk := a[k]
		bv, bOk := b[k]
		if av == bv {
			continue
		}
		fmt.Fprintf(stdout,
			"chronicle diff: relationships[%s]: {Trust=%d Affection=%d Respect=%d} -> {Trust=%d Affection=%d Respect=%d} (left=%t right=%t)\n",
			k,
			av.Trust, av.Affection, av.Respect,
			bv.Trust, bv.Affection, bv.Respect,
			aOk, bOk,
		)
		differed = true
	}
	return differed
}

// unionKeys returns the sorted union of two maps' keys.
func unionKeys[V any](a, b map[string]V) []string {
	set := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		set[k] = struct{}{}
	}
	for k := range b {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}
