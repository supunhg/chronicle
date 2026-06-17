// Package ui renders the v2 choice menu per ARCHITECTURE.md §23
// (Render Story, Render Choices, Player Selection) and §1's
// "The player never types commands" principle.
//
// Phase 36.F will land cli.go, the only file in this package. It:
//
//   - Prints the current StoryNode.Text verbatim (no substitution,
//     no LLM, no procedural prose — per §1 + §2 non-goals).
//   - Prints each visible choice as `[N] Text` (or `[Requires X]`
//     for choices whose Conditions are currently failing — locked
//     display honours PHASES.md §36.F's optional locked-choice spec).
//   - Reads numeric selection via bufio.Scanner.Input.
//   - Returns the selected ChoiceID; the engine handles everything
//     else (state mutation, event triggering, next-node load).
//
// There is NO free-text interpretation. Any input that isn't a
// numeric choice or an exact choice ID is re-prompted. This is the
// v2 hook for §1: "The player selects from available choices" —
// there is no other input mode in the game.
package ui
