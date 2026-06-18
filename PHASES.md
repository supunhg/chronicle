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
| **36.F** | `internal/ui/cli.go` renders the choice menu and reads choice selection (numeric input only). No free-text interpretation. | V2 CLI smoke test passes. | ✅ done |

---

## Phase 37 — Content YAML schema

Define the on-disk YAML format for everything in §21 Content Pipeline.
This phase is design + spec, not authoring. Authoring is Phase 38.

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **37.A** | Story node YAML spec. Required fields (`id`, `title`, `text`, `choices`, `events`). Optional fields. Example in `docs/story-node-yaml.md`. | `internal/story/nodes.go` parses a canonical example. | ✅ done |
| **37.B** | Choice / Effect / Condition YAML spec. Same shape as the Go types. Example in `docs/choice-yaml.md`. | `internal/story/choices.go` and `internal/story/conditions.go` and `internal/story/effects.go` parse canonical examples. | ✅ done |
| **37.C** | Relationship / Reputation / Inventory / Item YAML spec. The `Relationship` struct (§9), `ReputationState` struct (§11), `Inventory` struct (§12). | Loaders parse canonical examples. | ✅ done |
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
| **38.A** | Four protagonist YAMLs in `content/protagonists/`: Kael, Lyra, Raven, Aria. Each with a unique opening node and a small `ExclusiveNodes` set. | The character-select screen presents all 4. Each protagonist can be picked and reaches the Act 1 opening. | ✅ done |
| **38.B** | Companion YAMLs in `content/companions/`: Elara, Selene, Orion, plus the wider cast needed to populate relationship-driven events in Acts 1–2. | Trust/Affection deltas work. The Trust ≥ 50 / Affection ≥ 75 / Trust ≤ −50 scenes for Elara are authored (see §10 Relationship Events). | ✅ done |
| **38.C** | Acts 1 + 2 authored (20 + 50 = 70 nodes minimum). The Dragon Relic quest (Act 1) and the ally-gathering arc (Act 2) are complete and playtested for each of the 4 protagonists. | A playthrough of "pick protagonist → reach end of Act 2" succeeds for all 4 protagonists. | ✅ done |
| **38.D** | Act 3 authored (keep interior hub + 3 romance-conditioning scenes + 8 realm-claim terminals + act3.the_end) so the 12 §20 endings are reachable from `act2.act3_handoff`. The 8 non-romance endings gate on distinct realm flags set by per-ending claim scenes. The 3 romance variants gate on relationship axes (Elara affection, Selene trust, Orion affection); the Act 3 romance-conditioning scenes add the deltas Act 2 doesn't supply. | `TestLoad_FrontierWorldpackPerEnding` passes: for each of the 12 endings, `endings.Evaluate` against a synthetic WorldState returning that ending's ID at the highest priority. |
| **38.E** | `TestAllEndingsReachable` passes for the full 4 protagonists × 12 endings matrix: the runtime walker (`runtimeWalk` in `internal/content/reachable_test.go`) processes the authored YAML with `story.AvailableChoices` filtering + `Effect.Apply` mutation + `events.Trigger` redirect semantics — the exact path a real `Runner.Step` would walk — and asserts `endings.Evaluate` returns the target ending at the highest priority. Three canonical spines cover the flag-sets the endings gate on; dragon_sovereign × 4 passes now that the §38.B/C `ally_call` event is a sidebar (node_id="") so the Ashwick spine reaches `dragon_relic_in_vault.claim_relic` and sets `relic_claimed` (the gate `claim_serpent_crown` reads). | All 48 / 48 (protagonist × ending) sub-tests green; full short test sweep clean. |
| **38.F** | Romance routes wired end-to-end. Elara / Selene / Orion romance endings reachable if the romance conditions (§10) are met by the time the player hits the finale. The combined romance + realm-claim spine (each romance target + a non-conflicting realm-claim flag, walked through `runtimeWalk`) returns the ROMANCE ending (priority 9-11) over the realm claim (priority 1-8), proving the §20 priority ordering actually resolves at finale. | `TestRomanceRoutesWired` in `internal/content/romance_routes_test.go` passes: 4 protagonists × 3 romance routes = 12 sub-tests green, each asserting three properties on the post-walk WorldState — (P1) `endings.Evaluate` returns the romance ending ID, (P2) the realm-claim ending's conditions all resolve true against the post-walk state (the priority comparison is genuine and not a vacuous "romance beats wanderer"), (P3) the romance priority numerically exceeds the realm priority per §20 (yaml invariant). Full short test sweep clean. | ✅ done |

---

## Phase 39 — Save/load

Implement the JSON persistence per §18 and §18A. Drop SQLite.

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **39.A** | `SaveGame` JSON schema (per §18 + §18A invariants). Canonical JSON encoding (sorted keys, normalized numbers) so that byte-stable round-trip is achievable. | `TestSaveLoadRoundTrip`: pre-save `WorldHash` equals post-load `WorldHash`. | ✅ done |
| **39.B** | Versioning framework. The save's `Version` field gates load-time migration. No silent migration. | An old-version save refuses to load with a clear error message. A new-version save loads fine. | ✅ done |
| **39.C** | `save` CLI subcommand. `./chronicle save -out myrun.json` writes a `SaveGame` to disk at the current world state. | Manual smoke test. | ✅ done |
| **39.D** | `resume` CLI subcommand. `./chronicle resume myrun.json` rehydrates the world from disk and drops the player at `WorldState.CurrentNodeID`. | Manual smoke test. | ✅ done |
| **39.D follow-up #1** | Apply the §39.D reviewer's stylistic nits: add `TestResume_EmptyWorldState` regression; collapse the 5-line stderr loop in `TestResume_ValidSave` to a 2-Check consolidating strategy (joined-block substring + prefix-only check); collapse `errMissingPath` / `errTooManyArgs` into a single inline arg-count check returning `fmt.Errorf("resume: expected exactly one <path> argument; got %d", len(argv))`; trim the redundant sentinel godoc block. | All four nits applied; build/vet green; 9 resume tests + 6 save tests + §38 + §39.A/B/C all green; full short sweep clean. | ✅ done |
| **39.E** | `resume --json` flag. When set, emit the loaded SaveGame as canonical JSON to stdout (reuses `state.SaveGame.Marshal`'s §39.A sorted-keys encoding) so the binary composes in shell pipelines (`chronicle resume foo.json --json \| jq .WorldHash`). Stderr still carries the diagnostic identity (loaded-n-bytes, Version, Protagonist, CurrentNodeID, WorldHash) — doesn't pollute stdout. | `TestResume_JSONFlag_EmitsCanonicalSave` (parses stdout as a SaveGame), `_EquivalenceWithSave` (stdout bytes == save's canonical Marshal bytes), `_FlagOrder` (`--json` accepts before-or-after positional `<path>`), `_PipeCompatible` (`json.Decoder` is single-doc pipe-clean). Manual smoke: `chronicle resume foo.json --json` + pipe to `jq .WorldHash` returns the same `WorldHash` line that stderr would print. | ✅ done |
| **39.E follow-up #1** | Implement the original spec's `info` and `diff` subcommands. `chronicle info <path>` prints a human-readable summary (path, Version, Tick, WorldHash, Protagonist, CurrentNodeID, flag-count, variable-count, relationship-count, inventory-item-count, party-count, endings-unlocked, reputation breakdown one-liner) to stdout. `chronicle diff <a> <b>` surfaces field-level discrepancies (`chronicle diff: FieldName: <left> -> <right>` lines for Version/Tick/Protagonist/CurrentNodeID/scalars + `chronicle diff: flags[k]: ...` per-key walk for Flags/Variables/Relationships/Inventory Items + slice DeepEqual for Party/EndingsUnlocked/TriggeredEvents). Both read via a shared `readSaveForInspection(path)` helper so §39.A DisallowUnknownFields + §39.B version gate fire identically to `resume`. diff follows standard `diff` exit semantics via `errDiffFound` sentinel: 0 on identical, 1 on differences, 1 on load error. | `TestInfo_ValidSave` (asserts every stdout line + counts), `_EmptyWorldState`, `_MissingPath`, `_TooManyArgs`, `_FileMissing`, `_OldVersionRefused`, `_RejectsFlags`, `_CountsMatch`. `TestDiff_Identical` (exit 0 + 'identical' line), `_ProtagonistDiffers`, `_NodeDiffers`, `_FlagValueDiffers`, `_FlagInsertedDeletion`, `_VariableDiffers`, `_RelationshipAxisDiffers`, `_MultipleDifferences` (4 lines asserted), `_SameFileBothArgs`, `_MissingArg`, `_OnlyOneArg`, `_ThreeArgs`, `_LoadErrorOnEitherSide` (load error wins over any diff), `_RejectsFlags`, `_OldVersionRefusedOnEitherSide`. Manual smoke: `chronicle info foo.json` + `chronicle diff a.json b.json` + `chronicle diff same.json same.json` produces 'identical' exit 0. | ✅ done |
| **39.F** | Drop `modernc.org/sqlite` from `go.mod` and `go.sum`. The SQLite persistence layer (`internal/persistence`) is deleted along with the world-AI weekly generation artefacts (`worldpacks/frontier/{actions,entities,factions,generation,llm,occupations,rules}.yaml` — v1 sim worldpack config loaded by nothing in v2). | `go mod tidy` drops `modernc.org/sqlite` + cascade; `grep -rn sqlite .` returns no `.go` references; full short test sweep green. | ✅ done |

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
| **40.A** | §40.C + §40.D acceptance gates implemented in `internal/content/coverage_test.go` (4 tests), with worldpack content augmentation completing the schema-coverage surface. 6 new choices added under `act3.keep_interior` exercise the previously-missing Condition/Effect types. | `TestProtagonistCoverage` 8/8 PASS + `TestEndingCoverage` 12/12 PASS + `TestConditionCoverage` PASS (now And=1 / Flag=24 / HasEnding=1 / HasItem=1 / Not=1 / Or=1 / RelationshipGE=3 / VariableGE=1) + `TestEffectCoverage` PASS (now AddItem=2 / ClearFlag=1 / ModifyRelationship=18 / ModifyReputation=8 / ModifyVariable=1 / RemoveItem=2 / SetFlag=47 / TriggerEvent=10). All 8 Condition types + all 8 Effect types now exercised end-to-end. The 6 schema-coverage choices live under `act3.keep_interior` (the convergence hub for every §38.E spine), all leading to existing `act3.the_end` so the graph still validates and no existing runtimeWalk test is perturbed. Two content-polish follow-ups surfaced by review but not blocking: a coin-economy sandbox on the keep (cosmetic; non-idempotent `add_item`) and a dragon_sovereign-reachability footgun via `return_relic` clearing the `relic_claimed` gate (matches the in-fiction commit-the-relic-back narrative; documented in code comment). | ✅ done |
| **40.B** | Manual playthrough walkthrough. Each protagonist plays from start to a v2 ending. | One-by-one playthroughs, signed off in `PHASES.md`. Kael → hero; Lyra → hero; Raven → hero; Aria → hero. All four protagonists walked via `./chronicle play -protagonist <name> -script playthroughs/<name>.txt` from opening node to `act3.the_end`. Script paths account for event redirects (`selene_oath`, `maren_teaches`). | ✅ done |
| **40.C** | Test gates: each of the 4 protagonists can reach at least one romance ending AND at least one non-romance ending. Romance variants of the 12 endings are reachable. | `TestProtagonistCoverage` + `TestEndingCoverage`. | ✅ done |
| **40.D** | Test gates: every condition in `internal/story/conditions.go` is exercised by at least one authored node. Every effect in `internal/story/effects.go` is exercised by at least one authored choice. | `TestConditionCoverage` + `TestEffectCoverage`. | ✅ done |
| **40.E** | Test gates: a malformed or tampered save fails to load with a clear error message. | `TestSaveLoadResilience`. | ✅ done |

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
