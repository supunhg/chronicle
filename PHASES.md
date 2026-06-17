# Chronicle v2 — Phase Checklist

This is the working phase list for the Chronicle v2 migration. It
picks up after Phase 33 (immersive text adventure transformation),
which was the last phase of the **v1 simulation design**.

The v1 simulation design is now retired in the documentation
(see [`ARCHITECTURE.md`](./ARCHITECTURE.md)). The remaining work is
**retire the v1 code, build the v2 code, author the content, ship
v2**. This checklist tracks that work.

**Legend:** ✅ done (committed) · 🔧 done (in working tree, uncommitted)
· ⬜ not started · ⚠️ partially done

For the binding plan behind this phase list, see
[`chronicle-v2-pivot-spec.md`](./chronicle-v2-pivot-spec.md).

---

## Phase 34 — v2 doc pivot

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **34** | Documentation pivoted from v1 simulation spec to v2 branching-adventure spec. ARCHITECTURE.md, README.md, PHASES.md rewritten; SIMULATION_TICK_SPEC.md deleted; docs/DETERMINISM.md stubbed; chronicle-v2-pivot-spec.md added. Single atomic commit pushed to `origin/main`. No code changes. | `git log -1` on `origin/main` shows the Phase 34 commit message per `chronicle-v2-pivot-spec.md §8.2`. All pre-commit verification items in spec §8.4 pass. | ✅ done |

---

## Phase 35 — v1 engine retirement

Retire the v1 simulation packages from the codebase. Per decision
**D4** in the pivot spec, the simulation engines (Population,
Relationship, Marriage, Memory, Goal, Economy, Event) and the LLM
stack are no longer part of the Chronicle design.

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **35.A** | `internal/simulation` deleted. All 7 engines removed. Tests for those engines removed. | `rm -rf internal/simulation`; `go build ./...`; `go test ./...` | ✅ done |
| **35.B** | `internal/llm`, `internal/narrator`, `internal/intent` deleted. The `OPENCODE_ZEN_API_KEY` flow stops being referenced anywhere. | `grep -r OPENCODE_ZEN .` returns nothing in `*.go` | ✅ done |
| **35.C** | `internal/repl` slimmed to a story-node-renderer shell (no free-text REPL, no intent parser). May eventually be replaced by `internal/ui`. | REPL only renders nodes and selects from authored choices. |
| **35.D** | `internal/worldpack` slimmed to nothing — the worldpack is no longer the unit of genre. Engine reads authored content from `content/` directly. | `rm -rf internal/worldpack` once the loader lives in `internal/content/loader.go`. | ✅ done |
| **35.E** | `internal/lineage` and `internal/simulation/legacy` deleted. Lineage transfer is not part of v2. | `grep -r lineage .` returns nothing in `*.go`. | ✅ done |
| **35.F** | `internal/tick` deleted. There is no tick loop in v2. | `rm -rf internal/tick`. RNG helpers (EntityRand, TickRand) are gone. | ✅ done |

---

## Phase 36 — v2 module scaffold

Stand up the v2 module tree per `ARCHITECTURE.md §22` and README's
Project Layout v2 target.

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **36.A** | `internal/engine/` created. `runner.go` implements the §23 Runtime Flow (Load Save → Load Node → Evaluate Conditions → Render Story → Render Choices → Player Selection → Apply Effects → Check Events → Load Next Node). | `internal/engine` compiles; smoke test runs a 3-node story to completion. | ✅ done |
| **36.B** | `internal/story/` created. `nodes.go` defines the `StoryNode` type. `choices.go` defines the `Choice` type. `conditions.go` defines the `Condition` interface and all concrete condition types (`Flag`, `VariableGE`, `RelationshipGE`, `HasItem`, `HasEnding`, etc.). `effects.go` defines the `Effect` interface and all concrete effect types (`SetFlag`, `ClearFlag`, `ModifyVariable`, `ModifyRelationship`, `ModifyReputation`, `AddItem`, `RemoveItem`, `TriggerEvent`, etc.). | All types match `ARCHITECTURE.md §4–§8` verbatim. | ✅ done |
| **36.C** | `internal/state/` created. `world.go` defines `WorldState` (§4). `save.go` defines `SaveGame` (§18) and the JSON marshal/unmarshal. | Save/load round-trip preserves all fields (`TestSaveLoadRoundTrip`). | ✅ done |
| **36.D** | `internal/events/` and `internal/endings/` created. `internal/events/events.go` triggers authored events when their conditions match. `internal/endings/endings.go` evaluates endings in priority order and returns the highest-valid ending. | Triggered events fire deterministically; ending evaluation is deterministic. | ✅ done |
| **36.E** | `internal/content/loader.go` reads YAML content. Fail-fast on any reference error (broken node ID, missing companion YAML, etc.). | Tests on broken content fail with a clear error message, no silent divergence. | ✅ done |
| **36.F** | `internal/ui/cli.go` renders the choice menu and reads choice selection (numeric input only). No free-text interpretation. | V2 CLI smoke test passes. |

---

## Phase 37 — Content YAML schema

Define the on-disk YAML format for everything in §21 Content Pipeline.
This phase is design + spec, not authoring. Authoring is Phase 38.

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **37.A** | Story node YAML spec. Required fields (`id`, `title`, `text`, `choices`, `events`). Optional fields. Example in `docs/story-node-yaml.md`. | `internal/story/nodes.go` parses a canonical example. |
| **37.B** | Choice / Effect / Condition YAML spec. Same shape as the Go types. Example in `docs/choice-yaml.md`. | `internal/story/choices.go` and `internal/story/conditions.go` and `internal/story/effects.go` parse canonical examples. |
| **37.C** | Relationship / Reputation / Inventory / Item YAML spec. The `Relationship` struct (§9), `ReputationState` struct (§11), `Inventory` struct (§12). | Loaders parse canonical examples. |
| **37.D** | Character profile YAML spec. The `CharacterProfile` struct (§15) — `name`, `starting_flags`, `starting_variables`, `starting_inventory`, `exclusive_nodes`. | Loaders parse canonical examples. |
| **37.E** | Ending YAML spec. The `Ending` struct (§19) — `id`, `priority`, `conditions`. | Loader parses canonical examples; ending evaluation returns the highest-valid ending. |
| **37.F** | Content addressing. Each YAML is hashed at load. Mismatched content is a hard error. | `internal/content/loader.go` exposes `ContentHash(name string) string`. |

---

## Phase 38 — Content authoring

Write the actual story. Aspirational first cut: 5 acts ≥ 150 nodes,
≥ 300 choices, 12 endings reachable, all 4 protagonists playable, all
3 romance routes reachable.

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **38.A** | Four protagonist YAMLs in `content/protagonists/`: Kael, Lyra, Raven, Aria. Each with a unique opening node and a small `ExclusiveNodes` set. | The character-select screen presents all 4. Each protagonist can be picked and reaches the Act 1 opening. |
| **38.B** | Companion YAMLs in `content/companions/`: Elara, Selene, Orion, plus the wider cast needed to populate relationship-driven events in Acts 1–2. | Trust/Affection deltas work. The Trust ≥ 50 / Affection ≥ 75 / Trust ≤ −50 scenes for Elara are authored (see §10 Relationship Events). |
| **38.C** | Acts 1 + 2 authored (20 + 50 = 70 nodes minimum). The Dragon Relic quest (Act 1) and the ally-gathering arc (Act 2) are complete and playtested for each of the 4 protagonists. | A playthrough of "pick protagonist → reach end of Act 2" succeeds for all 4 protagonists. |
| **38.D** | Acts 3–5 authored (40 + 50 + 20 = 110 nodes). The Void Dragon reveal (Act 3), the war (Act 4), the finale (Act 5) are complete. | A full playthrough from start to a v2 ending succeeds. |
| **38.E** | `content/endings.yaml` (12 endings per §20). Endings are reachable from a believable playthrough. Some are only reachable from specific protagonists. | `TestAllEndingsReachable` passes (a graph search from each protagonist's start confirms every ending is reachable). |
| **38.F** | Romance routes wired end-to-end. Elara / Selene / Orion romance endings reachable if the romance conditions (§10) are met by the time the player hits the finale. | A playthrough that meets the romance conditions for each of the three targets lands on the corresponding romance ending. |

---

## Phase 39 — Save/load

Implement the JSON persistence per §18 and §18A. Drop SQLite.

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **39.A** | `SaveGame` JSON schema (per §18 + §18A invariants). Canonical JSON encoding (sorted keys, normalized numbers) so that byte-stable round-trip is achievable. | `TestSaveLoadRoundTrip`: pre-save `WorldHash` equals post-load `WorldHash`. |
| **39.B** | Versioning framework. The save's `Version` field gates load-time migration. No silent migration. | An old-version save refuses to load with a clear error message. A new-version save loads fine. |
| **39.C** | `save` CLI subcommand. `./chronicle save -out myrun.json` writes a `SaveGame` to disk at the current world state. | Manual smoke test. |
| **39.D** | `resume` CLI subcommand. `./chronicle resume myrun.json` rehydrates the world from disk and drops the player at `WorldState.CurrentNodeID`. | Manual smoke test. |
| **39.E** | `info` and `diff` subcommands. Read-only inspection and comparison of two `.json` saves. | Manual smoke test. |
| **39.F** | Drop `modernc.org/sqlite` from `go.mod` and `go.sum`. The SQLite persistence layer (`internal/persistence`) is deleted along with the world-AI weekly generation artefacts. | `go mod tidy` + `grep sqlite .` returns no `.go` references. |

---

## Phase 40 — v2 acceptance

The v2 Definition of Done, lifted from `ARCHITECTURE.md §25`:

- [ ] Four protagonists implemented
- [ ] Five acts written
- [ ] Minimum 150 story nodes
- [ ] Minimum 300 choices
- [ ] Minimum 12 endings
- [ ] Romance routes implemented
- [ ] Save/load works
- [ ] All node references validate
- [ ] No AI dependency exists
- [ ] Full playthrough possible from start to finish

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **40.A** | All 10 Definition-of-Done items green. | Manual + integration suite check. |
| **40.B** | Manual playthrough walkthrough. Each protagonist plays from start to a v2 ending. | One-by-one playthroughs, signed off in `PHASES.md`. |
| **40.C** | Test gates: each of the 4 protagonists can reach at least one romance ending AND at least one non-romance ending. Romance variants of the 12 endings are reachable. | `TestProtagonistCoverage` + `TestEndingCoverage`. |
| **40.D** | Test gates: every condition in `internal/story/conditions.go` is exercised by at least one authored node. Every effect in `internal/story/effects.go` is exercised by at least one authored choice. | `TestConditionCoverage` + `TestEffectCoverage`. |
| **40.E** | Test gates: a malformed or tampered save fails to load with a clear error message. | `TestSaveLoadResilience`. |

---

## Post-Pivot Backlog

Not phases per se — open items captured by the pivot spec (§12) that
need decisions before their owning phase can start:

- [ ] Pick final org/repo name (currently `github.com/chronicle-dev/chronicle`).
- [ ] Pick license (currently TBD).
- [ ] Decide whether the `cmd/chronicle` entry point stays or moves to a v2 path.
- [ ] Decide whether `internal/repl` is fully deleted in Phase 35.C or kept as a terminal-only shell that hands off to the v2 choice renderer.
- [ ] Decide whether `content/` is single-folder or per-act subfolders (the pivot spec adopted the per-act subfolders layout; revisit if authoring suggests otherwise).
- [ ] Decide whether the saved `.json` files are human-readable pretty-printed or compact. Spec calls for canonical sorted-key encoding, but readability is a separate dimension.

## Suggested next phase after Phase 34 lands

**Phase 35 — v1 engine retirement**. The highest-leverage next step.
Once the v1 packages are deleted, Phase 36's v2 scaffold lands on a
clean tree. Phase 37's YAML schema can then be designed and tested
against a v2 runnable engine in Phase 38. Phase 39's save/load and
Phase 40's acceptance run close out the v2 cut.
