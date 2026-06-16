# Chronicle — V1 Phase Checklist

This is the working checklist the user asked for: "step by step, mark
off a phase from a checklist to keep track." Each phase has a goal, an
acceptance test, and a status. The full spec is in
[`ARCHITECTURE.md`](./ARCHITECTURE.md).

**Legend:** ✅ done (committed) · 🔧 done (in working tree, uncommitted)
· ⬜ not started · ⚠️ partially done

> **Note:** Phases 30–33 were completed as shipped work. Phases 31–33
> were repurposed from earlier placeholder definitions (REPL inspection,
> `--no-llm`, World AI) to match the actual work done (time advancement,
> stress testing, immersive text adventure transformation). The original
> placeholder phases were renumbered accordingly (old 31→34, old 32→34,
> old 33→35).

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
| **30** | Auto-succession + 5 continuation modes (Heir/Family/Character/Observer/End Bloodline) + legacy record on death | `TestLineageTransfer` | ✅ done |
| **30.1** | Lineage end-to-end playtest (5 tests: heir/successors/observer/end_bloodline/no-candidates) | `TestLineagePlaytest_*` | ✅ done |

## Phase 31 — Action-duration time advancement

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **31** | Travel/sleep/walk advance simulation ticks via full tick pipeline (SetTickFn). Phase 31 wires the per-tick callback so player actions trigger the same world evolution as auto-tick. | `TestTravelAdvancesTicks`, `TestSleepAdvancesTicks` | ✅ done |

## Phase 32 — 10-seed stress test + determinism audit

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **32** | 10 seeds × 200 years, all hash-match replay. Determinism audit of engine iteration order, RNG streams, and retention caps. | `TestStressReplay` | ✅ done |

## Phase 33 — Immersive text adventure transformation

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **33** | Reshape Chronicle from a simulation dashboard into a living fantasy world. Overhaul narrator (second-person sensory), add adventure verbs (search, pray, status), expand worldpack (7 locations, 30+ buildings, 8 landmarks, 8 trade routes), rewrite help text. | Build + all tests pass | ✅ done |
| **33.1** | Update README.md to reflect immersive text adventure vision | README rewrite | ✅ done |
| **33.2** | Wire BuildLocationGossip and BuildPersonGossip into conversation system. NPCs now respond with what they know when asked about places or people. Word-boundary matching via nameMatch helper. | `go build` + `go vet` clean | ✅ done |
| **33.3** | Update all narrator template renderers (tplLook, tplTalk, tplTravel, tplDeath, tplBirth, tplFirstMeet, tplTime, tplInventory) to immersive second-person voice. Fix NPCActivity pronoun clashes. Gendered pronouns (He/She/They, his/her/their). | All narrator + repl tests pass | ✅ done |
| **33.4** | Make resolveStatus produce immersive narrative output (LLM-first character journal). Replace simulation dashboard character sheet with flowing prose. | All action tests pass | ✅ done |
| **33.5** | Extract duplicated gendered pronoun logic into pronounSubject, pronounPossessive, genderNoun helpers. | All tests pass unchanged | ✅ done |

## Phase 34 — `--no-llm` flag

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **34** | Disable LLM entirely (template + rule parser only) | `TestNoLLM` | ⬜ not started |

## Phase 35 — World AI (weekly)

| Sub | Goal | Acceptance test | Status |
|---|---|---|---|
| **35** | Asynchronous rumor/legend/religious-text generation on a weekly tick | `TestWorldAI_GeneratesRumors` | ⬜ not started |

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

## Status after Phase 33

The user can now play an immersive text adventure:

```bash
./chronicle new mygame --seed 42 -repl
> look                        # atmospheric scene description
> walk                         # interactive destination picker (buildings with → prefix)
> walk to the inn              # building exploration with unique atmospherics
> talk elena                   # NPC speaks in character, shares gossip
> tell me about millbrook      # NPC shares what they know about the place
> search                       # atmospheric search results
> pray                         # temple detection, seasonal reflection
> status                       # immersive narrative character journal
> inspect marcus               # rich person description
> listen                       # ambient sounds and overheard conversations
> travel millbrook             # journey narration with terrain, encounters, weather
> inventory                    # immersive inventory check
> time                         # seasonal/time description
> save                         # save your world
> quit                         # leave the world
```

…and resume later with `./chronicle resume mygame.db -repl`.
