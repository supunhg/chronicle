# Chronicle — V1 Phase Checklist

This is the working checklist the user asked for: "step by step, mark
off a phase from a checklist to keep track." Each phase has a goal, an
acceptance test, and a status. The full spec is in
[`ARCHITECTURE.md`](./ARCHITECTURE.md).

**Legend:** ✅ done (committed) · 🔧 done (in working tree, uncommitted)
· ⬜ not started · ⚠️ partially done

---

## Phase 26 — Stability & Persistence Validation (in progress)

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **26.A** | Snapshot+Restore preserves every field `core.WorldHash` covers | `TestSaveLoadRoundTrip` (hash_before==hash_after) | 🔧 done |
| **26.B** | Live event/memory log stays bounded | `TestTrimEvents_*` + `TestTrimMemories_*` | 🔧 done (FIFO fix this session) |
| **26.C** | 10 seeds × 200 years all hash-match the seed-anchored replay | `TestStressReplay` (new) | ⬜ not started |
| **26.D** | 5-Generation test finishes in < 10 minutes (CourtAction perf) | `TestFiveGenerationSimulation` under timeout | ⬜ not started |
| **26.E** | Engines iterate maps in sorted order (eliminate latent non-determinism) | `TestSortedPeople_*` + `TestEconomyEngine_*` | 🔧 done |

## Phase 27 — Doc consolidation

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **27** | One canonical ARCHITECTURE.md (this work) | README links to ARCHITECTURE.md; `chronicle-spec.md` deleted; ARCHITECTURE.md contains all spec content | 🔧 done (this session) |

## Phase 28 — REPL help

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **28** | `> help` prints the 12 verbs + meta-commands | `TestREPL_Help` | ✅ done |

## Phase 29 — `chronicle new <name>`

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **29** | `chronicle new <name> --pack frontier --seed X` creates a working world with one command | `TestNewCmd_CreatesWorld` | ✅ done |

## Phase 30 — Player lineage on death

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **30** | Auto-succession + 5 continuation modes (Heir/Family/Character/Observer/End Bloodline) + legacy record on death | `TestLineageTransfer` | ⬜ not started |

## Phase 31 — REPL relationship/memory inspection

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **31** | `relations <name>` / `memories <name>` / `mood` / `goals` commands | `TestREPL_Relations`, `TestREPL_Memories` | ⬜ not started |

## Phase 32 — `--no-llm` flag

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **32** | Disable LLM entirely (template + rule parser only) | `TestNoLLM` | ⬜ not started |

## Phase 33 — World AI (weekly)

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **33** | Asynchronous rumor/legend/religious-text generation on a weekly tick | `TestWorldAI_GeneratesRumors` | ⬜ not started |

## Phase 34 — LLM cache

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **34** | Cache key per spec §11.3, hit-rate > 0% in a 100-tick run | `TestLLMCache` | ⬜ not started |

## Phase 35 — XDG world directory

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **35** | `~/.local/share/chronicle/worlds/<id>/` layout, `metadata.yaml`, `config.yaml` | `TestXDGPath` | ⬜ not started |

## Phase 36 — `chronicle branch` / `chronicle switch` / `chronicle timeline` CLI

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **36** | Top-level subcommands (currently REPL-only for branch/switch) | `TestBranchCmd`, `TestSwitchCmd`, `TestTimelineCmd` | ⬜ not started |

## Phase 37 — `chronicle export` / `chronicle import`

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **37** | World archive round-trip | `TestExportImport` | ⬜ not started |

## Phase 38 — SQLite test deadlock fix

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **38** | `TestResume_EngineUsesRestoredRules` under 60s (one-liner: `db.SetMaxOpenConns(1)` on test DBs) | `TestResume_EngineUsesRestoredRules` passes under 60s | ⬜ not started |

## Phase 39 — `chronicle list` / `chronicle delete` / `chronicle pack list`

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **39** | World + pack registry commands | `TestListCmd`, `TestDeleteCmd`, `TestPackListCmd` | ⬜ not started |

## Phase 40 — Final v1 acceptance run

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **40** | All 9 Definition-of-Done items green + manual smoke test (start as nobody in Blackwater, play for 50 years, have an heir) | `make test` passes; manual playthrough works end-to-end | ⬜ not started |

---

## Suggested next 3 phases (after this commit lands)

1. **Phase 38 (SQLite test fix).** Trivial one-liner: `db.SetMaxOpenConns(1)` in the test DB. Catches a latent test-infrastructure bug; unblocks the `cmd/chronicle` test suite.
2. **Phase 30 (Player lineage on death).** The big one for "die and continue" — auto-succession with the 5 continuation modes (Heir/Family/Character/Observer/End Bloodline) and the legacy record on death.
3. **Phase 31 (REPL relationship/memory inspection).** `relations <name>` and `memories <name>` commands so the player can see the social graph they're building.

After those three, the path to "playable v1" is:
- Phase 32 (`--no-llm`)
- Phase 26.D (CourtAction perf — unblocks the v1 acceptance suite)
- Phase 40 (final acceptance)

## Status after Phase 28 + 29 (this commit)

The user can now play the game end-to-end:

```bash
./chronicle new mygame --seed 42 -repl
> help
> look
> people
> talk elena
> travel blackwater
> sleep
> auto-tick on
> save mygame-saved.db
> quit
```

…and resume later with `./chronicle resume mygame.db -repl`.
