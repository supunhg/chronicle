# Simulation Tick Specification

**Status:** Draft v0.1
**Date:** 2026-06-14
**Companion to:** `chronicle-spec.md` §5 (Simulation Architecture) and §7 (Time Model)

This document defines the exact execution order of the simulation. It is
the **determinism contract**: any implementation that follows this spec
must produce identical world state given identical inputs.

---

## 1. Tick Lifecycle

A "tick" is one atomic step of the simulation. The orchestration layer
(Tier 3, see `chronicle-spec.md` §5.0) drives ticks. There is no partial
tick and no lazy tick.

### 1.1 When Ticks Happen

Ticks are triggered by:

1. **Player duration action.** A command like `travel north` (4 hrs) or
   `sleep` (8 hrs) advances time by the action's duration and triggers
   the corresponding number of ticks.
2. **Advance command.** `advance day` = 1 tick. `advance week` = 7 ticks.
   `advance month` = 30 ticks.
3. **Auto-tick.** When enabled, the orchestrator advances time at a
   configured real-time rate and ticks for each interval.

### 1.2 Tick Boundaries

A tick boundary is the point between two ticks. **No state is read or
written across tick boundaries outside the orchestration layer.** A Tier 2
system invoked during tick N reads and writes only state for tick N.

If an event in tick N needs to affect tick N+1 (e.g. a relationship
formed today influences tomorrow's action), it does so by writing to
state at the end of tick N. The next tick reads that state.

### 1.3 Mid-Tick Events

A "mid-tick event" is an event that occurs *during* a tick, e.g. a fire
starts at 2 pm during a 4-hour travel action. Mid-tick events are
resolved at the **end** of the current tick, in the order they were
emitted. They do not interrupt the current engine's execution.

This is a deliberate simplification. Real-world causality is finer, but
for simulation purposes, "end of tick" is good enough and keeps the
implementation tractable.

---

## 2. Tick Order

For each tick, the orchestration layer runs the engines in this exact
order. Reordering any step changes the result.

```
┌─────────────────────────────────────────────────────────────┐
│ TICK N                                                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  1. ADVANCE_TIME                                             │
│     - Update current_tick counter                           │
│     - Update sim clock                                       │
│                                                             │
│  2. AGING_ENGINE                                             │
│     - Increment age for all living NPCs                     │
│     - Check for age-based events (coming of age, etc.)      │
│                                                             │
│  3. ECONOMY_ENGINE                                           │
│     - Production: workers produce resources                 │
│     - Consumption: all living consume food                   │
│     - Trade: move goods between connected locations         │
│     - Price update: recalculate based on supply/demand      │
│     - Tool productivity recalculation                       │
│                                                             │
│  4. POPULATION_ENGINE                                        │
│     - Mortality check (age, health, starvation)             │
│     - Birth check (fertility, pair bonding)                 │
│     - Aging out of parents (children mature)                │
│     - Migration (driven by settlement pressure)             │
│     - Family tree updates                                   │
│                                                             │
│  5. RELATIONSHIP_ENGINE                                      │
│     - Apply memory-driven deltas to relationships           │
│     - Decay old relationships slightly                      │
│     - Check for relationship-based events                   │
│       (becoming friends, falling in love, etc.)             │
│                                                             │
│  6. GOAL_ENGINE                                              │
│     - For each NPC:                                         │
│       a. Compute current needs                              │
│       b. Generate candidate actions (3-layer system)        │
│       c. Score each action (utility AI)                     │
│       d. Choose highest-scoring action                      │
│       e. Apply action to entity state                       │
│     - Resolve conflicts (two NPCs want the same resource)  │
│                                                             │
│  7. EVENT_ENGINE                                             │
│     - Probabilistically generate new events from state      │
│     - Apply follow-up effects of mid-tick events            │
│     - Trigger Layer 3 opportunistic actions                 │
│                                                             │
│  8. MEMORY_ENGINE                                            │
│     - For each event in this tick:                          │
│       - Create memory records for affected NPCs              │
│       - Set causal anchors (CauseEventID chain)             │
│       - Set trust_delta and relationship_delta              │
│     - Apply memory decay to old memories                    │
│     - Summarize clusters of similar memories (deterministic)│
│                                                             │
│  9. PERSISTENCE_FLUSH                                        │
│     - Write all state changes to SQLite                     │
│     - Commit transaction                                    │
│                                                             │
│  10. NARRATION_DECISION                                      │
│     - For each significant event in this tick:              │
│       - Decide: template or narrator LLM?                   │
│       - Render output                                       │
│     - Emit narration to player                              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 2.1 Why This Order

- **Aging before everything** — age is a precondition for mortality
  and for "coming of age" events.
- **Economy before Population** — starvation depends on food
  production; we need the food count before checking who dies.
- **Population before Relationship** — death affects relationships
  (e.g. spouse loses a partner).
- **Relationship before Goal** — relationship state feeds into Goal
  Engine scoring.
- **Goal before Event** — the actions NPCs chose in this tick can
  trigger events.
- **Event before Memory** — events are recorded as memories.
- **Persistence before Narration** — narration is generated after
  state is committed, so the LLM never observes uncommitted state.
- **Narration last** — narration is the output of the tick, never an
  input.

### 2.2 Per-Tick Invariants

- All Tier 2 systems are called exactly once per tick, in the order
  above.
- State mutations by a Tier 2 system are visible to later Tier 2
  systems in the same tick.
- A Tier 2 system may not read or write state from a future tick.
- The orchestrator holds the only write lock for the duration of the
  tick.

---

## 3. Deterministic RNG Contract

### 3.1 Single Source of Randomness

The simulation uses **one** `*rand.Rand` per tick scope, seeded by:

```go
func TickRand(worldSeed int64, tick int64) *rand.Rand {
    return rand.New(rand.NewSource(worldSeed + tick))
}
```

For per-entity randomness:

```go
func EntityRand(worldSeed int64, tick int64, entityID string) *rand.Rand {
    h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", worldSeed, tick, entityID)))
    seed := int64(binary.BigEndian.Uint64(h[:8]))
    return rand.New(rand.NewSource(seed))
}
```

### 3.2 Forbidden Patterns

- `math/rand` global functions (`rand.Intn`, `rand.Float64`, etc.)
- `time.Now()` as a seed
- `crypto/rand` for game state (use only for non-game entropy like
  world ID generation)
- Concurrent goroutines that race for randomness
- Reading RNG state mid-tick from a different scope

### 3.3 Determinism Test

For every engine, the following property must hold:

```
For any (world, action) pair, two runs with the same world seed and
the same action sequence must produce identical state after every
tick.
```

This is enforced by a test suite that replays 10,000 ticks across all
engines and asserts state equality. The test fails fast on the first
divergent tick.

---

## 4. NPC Decision Loop

For each NPC, every daily tick, the Goal Engine runs this loop:

```
1. UPDATE_NEEDS
   - hunger = max(0, hunger - 5)
   - wealth drifts toward 0 (consumption)
   - loneliness drifts up over time
   - safety decays if in dangerous area

2. COLLECT_HARD_ACTIONS
   - From the registered verb list, pick those whose preconditions
     are met for this NPC

3. COLLECT_CONTEXTUAL_ACTIONS
   - Evaluate actions.yaml rules against current NPC state
   - For each rule that fires, add the action to candidates

4. COLLECT_OPPORTUNISTIC_ACTIONS
   - For each event in this tick that targets this NPC, add the
     event's action to candidates

5. SCORE_EACH_ACTION
   - Compute utility per the formula in §5.3 of chronicle-spec.md
   - Use EntityRand(worldSeed, tick, NPC.id) for the small_randomness
     term

6. CHOOSE_BEST_ACTION
   - Highest score wins
   - Ties broken by EntityRand(worldSeed, tick, NPC.id + ":tie")

7. APPLY_ACTION
   - Mutate entity state per the action's effect
   - Emit any triggered events to the Event Resolver
```

The loop runs for every NPC in the world. For 150 NPCs and ~40 actions
each, this is ~6,000 utility evaluations per tick. Well within budget.

### 4.1 Action Conflict Resolution

If two NPCs choose mutually exclusive actions (e.g. both want to
"buy the last horse"), the orchestrator resolves conflicts in
**NPC ID alphabetical order** (deterministic). The first NPC wins;
the second NPC's action fails and they pick a new action from their
candidates list (or default to a Layer 1 fallback like `idle`).

---

## 5. Action Resolution Lifecycle (Player Action)

When the player issues a command, the following happens:

```
1. INTENT_EXTRACTION
   - Try rule parser. If it returns a typed Action, skip to step 2.
   - If rule parser fails, call Intent LLM.
   - If LLM returns valid Action JSON, validate against schema.
   - If validation passes, continue.
   - If validation fails or LLM errors, ask player to rephrase.

2. ACTION_VALIDATION
   - Action verb is in the registered action set
   - All required fields are present
   - All entity references exist and are accessible
   - Preconditions are met (target alive, location reachable, etc.)
   - If any check fails, return error to player

3. TIME_ADVANCEMENT
   - Look up action's duration
   - Advance sim time by duration
   - For each tick in the elapsed time:
     - Run full tick pipeline (see §2)
     - Player action is held in suspension

4. ACTION_RESOLUTION
   - At the new sim time, apply the action's effects
   - The action may trigger events (which are resolved at the end
     of the next tick)

5. NARRATION
   - Decide: template or narrator LLM
   - Render output
   - Display to player
```

### 5.1 Why Player Actions Are Suspended

If the player issues a 4-hour travel action, the world advances 4 hours
*before* the travel resolves. This means:

- NPCs in the destination may have moved
- Time of day changes
- Events may have happened during the travel

This is realistic. A 4-hour journey is a 4-hour journey, and the world
does not freeze while you travel.

### 5.2 Edge Case: Death Mid-Action

If the player dies during the suspended action (e.g. attacked while
traveling, though combat is v2), the action is cancelled and the
lineage-transfer flow begins immediately at the end of the tick.

### 5.3 Edge Case: LLM Failure

If the LLM is unavailable, the action can still resolve. Narration
falls back to templates. The simulation never depends on the LLM.

### 5.4 Edge Case: Action Validation Rejection

If validation fails, the player sees a specific error. The tick does
not advance. Examples:

- `> marry elena` → "Elena is dead."
- `> buy horse` → "You have no coin."
- `> travel north` → "There is no path north from here."

---

## 6. Auto-Tick Behavior

When `auto-tick on` is enabled with rate `R` sim-hours per real-second:

```
Every (1/R) real-seconds:
  advance sim time by 1 sim-hour
  run full tick pipeline
  if any LLM-eligible event: queue for next LLM batch
  emit narration (batched, not per tick)
```

The LLM is rate-limited: max 1 call per 4 sim-hours. If multiple
LLM-eligible events occur in a batch, they are merged into a single
LLM call with all relevant facts.

Player input (a typed command) **interrupts** auto-tick. The current
tick completes, then auto-tick pauses until the player issues another
`auto-tick on` or completes the command.

### 6.1 Auto-Tick Pause Conditions

Auto-tick pauses when:

- The player issues a command
- The player is reading narration
- The player is in a confirmation prompt (LLM-derived action)

Auto-tick resumes on the next `auto-tick on` or after the player types
`continue`.

---

## 7. Edge Cases and Failure Modes

### 7.1 World Pack Missing

If the world pack referenced by a world is deleted, the world is
marked "orphan" and refuses to load. The player is prompted to
reinstall the pack or delete the world.

### 7.2 SQLite Corruption

If the SQLite file is corrupt, the orchestrator refuses to load and
prints a diagnostic. The player is told to restore from backup.

### 7.3 Branch Divergence

If two branches are created from the same parent tick and both are
advanced, they diverge. There is no auto-merge in v1. The player
picks one branch to keep or keeps both as separate timelines.

### 7.4 RNG Stream Bug

If the determinism test (see §3.3) fails, the orchestrator aborts and
prints the failing tick. This is a hard error in v1 — determinism is
non-negotiable.

### 7.5 Event Cascade

If a single event triggers N follow-up events, all N are processed
within the same tick. There is no "next tick" for follow-ups. This
prevents runaway event chains but may need refinement in v2.

### 7.6 Time Travel Attempts

The orchestrator MUST reject any attempt to set the sim clock
backward. If a save/load sequence would result in a backward time, the
load is refused (or, for branch loading, the branch is rebased to the
load time and a diff is shown).

---

## 8. Test Strategy

### 8.1 Unit Tests

Each engine has a unit test that:

- Sets up a minimal world
- Runs N ticks
- Asserts state is what was expected

### 8.2 Determinism Tests

For each engine, run 1000 ticks twice with the same seed. Assert state
is byte-identical.

### 8.3 Property Tests

For the Goal Engine, run 10,000 simulated NPCs through a year and
assert:

- Distribution of chosen actions is not degenerate (no NPC picks the
  same action > 80% of the time)
- Relationship deltas match the memory deltas
- Causal memory chains do not exceed 10 generations

### 8.4 Integration Tests

A scripted 5-generation playthrough with fixed seeds produces a known
output file. Any deviation is a bug.

### 8.5 Performance Tests

- 150 NPCs × 365 ticks (one simulated year) completes in < 60 seconds
  on a mid-range laptop
- A 50-generation playthrough (≈ 7,500 NPC-years) completes in
  < 10 minutes
- Auto-tick sustains 1 sim-hour per real-second with 150 NPCs

### 8.6 Fuzz Tests

The action validation layer is fuzzed with:

- Adversarial player inputs (prompt injection attempts)
- Invalid action types
- Out-of-range values
- Unicode edge cases

Any input that bypasses validation and reaches the simulation is a
P0 bug.

---

## 9. Open Questions

- Should mid-tick events be processed immediately, or always at the
  end of the tick? (Currently: end of tick. May change for v2 if
  certain events need earlier intervention.)
- Should the orchestrator support tick staggering (different NPCs
  ticked on different real-time schedules for performance)? (v2
  concern.)
- Should auto-tick pause on certain event types? (e.g. auto-tick
  pauses when the player is in a conversation.) **TBD.**
- How should lineage transfer interact with auto-tick? **TBD.**

---

## 10. Summary

The tick spec is the **determinism contract**. Any implementation that
violates the order, the RNG rules, or the action lifecycle will
produce different state for the same inputs — and therefore fail the
integration tests.

The two rules to memorize:

1. **Time only goes forward, in integer ticks.**
2. **RNG is seeded by `(world_seed, tick, entity_id)`. Nothing else.**

If you remember those, the rest follows.
