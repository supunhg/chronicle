// Package action is a v2 placeholder.
//
// As of Phase 35.B (v1 engine retirement), the v1 action engine
// has been retired. The 18+ resolve* handlers that branched on
// intent.Action* are gone — internal/intent was deleted.
//
// v2 actions live on StoryNode.Choices and are evaluated by
// internal/engine (Phase 36). This stub remains so the v1-derived
// package tree still compiles after the v1 engine retirement.
//
// See ARCHITECTURE.md §22 for the v2 target package tree and
// chronicle-v2-pivot-spec.md for the binding retirement plan.
package action
