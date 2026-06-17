// Package story defines the v2 authored-content data types per
// ARCHITECTURE.md §5–§8.
//
// Phase 36.B will land four files:
//
//   - nodes.go      StoryNode struct (§5) — ID, Title, Text, Choices,
//                   Events.
//   - choices.go    Choice struct (§6) — ID, Text, Conditions,
//                   Effects, NextNodeID.
//   - conditions.go Condition interface + concrete impls (§7):
//                   Flag, VariableGE, RelationshipGE, HasItem,
//                   HasEnding, plus the "or"/"and" combinators
//                   referenced by PHASES.md §36.B's acceptance test.
//   - effects.go    Effect interface + concrete impls (§8):
//                   SetFlag, ClearFlag, ModifyVariable,
//                   ModifyRelationship, ModifyReputation, AddItem,
//                   RemoveItem, TriggerEvent.
//
// story is the only v2 package that contains pure data definitions;
// all choice/condition/effect semantics live here and nowhere else.
// The engine, state, events, endings, content, and ui packages all
// read these types but never re-define them.
package story
