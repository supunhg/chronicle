# Chronicle — V1 Specification

**Status:** Draft v0.2 (post-corrections)
**Date:** 2026-06-14
**Revision:** v0.2 incorporates 3 critical risk fixes (3-layer action system,
causal memory anchoring, closed production loops), the 3-tier simulation
model, explicit tick discipline and RNG contract, LLM cache key, and
population dynamics rules.
**Companion to:** `ARCHITECTURE.md`

This spec is a concrete build plan for Chronicle v1. It is consistent with the
five core principles in `ARCHITECTURE.md` (simulation-first, persistent worlds,
story emergence, deterministic simulation, world-pack architecture) and
refines them with specific decisions made during the design interview.

---

## 1. Vision Recap

Chronicle is a persistent reality simulation engine. Players inhabit living
worlds populated by autonomous individuals who remember, form relationships,
pursue goals, create history, and continue existing long after the player
leaves or dies.

The engine is responsible for reality. LLMs are responsible only for
interpretation and narration. The simulation must remain valid even if all
AI text generation is removed.

**V1 is a "unit test" for human society.** If relationships, inheritance,
ambition, memory, and reputation work in the v1 world pack, they will work
in any future pack. If they don't, no amount of magic or dragons will save
the design.

---

## 2. V1 World Pack — "The Free Marches"

A frontier region between rival city-states. Late medieval / early
Renaissance (1400–1500 equivalent). No magic. No combat. No quests. Social
mobility and emergent stories are the gameplay.

### 2.1 Region

- **Name:** The Free Marches
- **Size:** ~120 km × 120 km
- **Geography:**
  - 1 town (Blackwater, pop. 80)
  - 4 villages
  - 1 monastery
  - 1 fort
  - 1 trade route
  - 1 forest
  - 1 river

### 2.2 Population

- **Total:** 150 NPCs at world creation
- **Distribution:**

  | Group     | Count |
  |-----------|-------|
  | Town      | 80    |
  | Villages  | 50    |
  | Travelers | 10    |
  | Nobility  | 5     |
  | Clergy    | 5     |

- **Gender ratio:** 51% female / 49% male
- **Age distribution:**

  | Range  | Share |
  |--------|-------|
  | 0–15   | 25%   |
  | 16–30  | 30%   |
  | 31–50  | 30%   |
  | 51+    | 15%   |

  Children, marriages, births, and inheritance must all be present from day
  one. No protected age group.

### 2.3 Factions

Exactly four. Each has a goal, a membership pool, and a structural role.

- **Merchant Guild** — Profit, trade, influence. Members: shopkeepers,
  traders, caravan owners.
- **Town Council** — Stability, taxes, law. Members: mayor, officials,
  landowners.
- **Faith of the Dawn** — Charity, morality, social influence. Members:
  priests, monks, followers.
- **Frontier League** — Independence, expansion, land ownership. Members:
  hunters, settlers, farmers.

### 2.4 Economy

Five resources, nothing more:

- Food
- Wood
- Iron
- Cloth
- Coin

V1 economy must remain lightweight. Inflation, shortages, and trade are
in-scope; banking and complex finance are v2.

### 2.5 Occupations

~15 archetypes:

`farmer`, `merchant`, `blacksmith`, `innkeeper`, `guard`, `priest`, `baker`,
`carpenter`, `hunter`, `teacher`, `laborer`, `miller`, `trader`, `mayor`,
`clerk`.

### 2.6 Social Classes

Three: Lower, Middle, Upper. Class affects marriage eligibility, reputation
gains/losses, and the set of opportunities an NPC can pursue.

### 2.7 Initial Tensions

Every world starts with unresolved pressure to seed events:

- **Trade crisis** — food prices rising
- **Council corruption** — mayor accused of favoritism
- **Religious dispute** — temple seeking greater influence

Nothing catastrophic. Just enough pressure to generate events.

### 2.8 V1 World Pack Directory

```
worldpacks/frontier/
  rules.yaml         # tunable parameters: birth rate, marriage rules, etc.
  entities.yaml      # locations, buildings, resources
  factions.yaml      # 4 factions with goals and members
  events.yaml        # event templates and triggers
  occupations.yaml   # 15 occupations and their needs weights
  generation.yaml    # world gen parameters: 150 NPCs, age dist, gender ratio
```

The engine loads packs dynamically. The pack is the unit of genre.

---

## 3. Tech Stack

- **Language:** Go (latest stable, 1.22+)
- **CLI library:** [Kong](https://github.com/alecthomas/kong) — strongly typed
  commands, low boilerplate, keeps the CLI layer thin
- **SQLite driver:** `modernc.org/sqlite` (pure Go, no CGO) for portability
  and easy cross-compile
- **LLM provider:** OpenCode Zen only (single OpenAI-compatible endpoint)
- **LLM client:** standard `net/http` with a small typed wrapper
  (no heavyweight SDK)
- **Testing:** stdlib `testing` + table-driven tests for simulation;
  golden-file tests for narration templates

### 3.1 Module Layout

```
chronicle/
  cmd/
    chronicle/         # main entry point
  internal/
    cli/               # Kong command definitions (thin)
    game/              # game loop, prompt, output rendering
    world/             # world files, world IDs, load/save
    simulation/        # engines: world, population, relationship, goal,
                       # economy, event, memory, timeline (no LLM imports)
    llm/               # OpenCode Zen client, intent LLM, narrator LLM,
                       # world AI (imports no internal/* packages except
                       # shared types)
    timeline/          # branching, snapshots, diff
    persistence/       # SQLite access, schema, migrations
    pack/              # world pack loader
  worldpacks/
    frontier/          # v1 pack (see §2.8)
  docs/
  .goreleaser.yaml
  go.mod
  go.sum
```

**Architectural invariant:** `internal/simulation` MUST NOT import
`internal/llm`. The boundary is enforced in CI by an `go vet`-style check
or a simple `grep` test in the build pipeline. This is the single most
important rule in the codebase. The LLM is a sink, not a source.

---

## 4. LLM Architecture — Hybrid Pipeline

The LLM is invoked in three distinct roles. Each is gated, cost-controlled,
and bypassable for testing.

```
Player Input
     ↓
Intent Parser
   ├─ Rule Parser (95%)        — regex, command aliases
   └─ Intent LLM (5%)          — fallback for complex NL
     ↓
Simulation Engine
     ↓
Event Resolver
     ↓
Narration Decision
   ├─ Template Renderer        — most turns
   └─ Narrator LLM             — important moments only
     ↓
Player
```

A fourth, asynchronous role runs on a schedule:

```
Weekly Tick
     ↓
World AI                       — generates rumors, books, legends,
                                 cultural shifts, religious texts
     ↓
These become simulation inputs (state mutations), not narration.
```

### 4.1 Rule Parser (~95% of inputs)

Examples that must be handled without an LLM call:

- `look`
- `inventory`
- `sleep`
- `travel north`
- `talk elena`
- `inspect marcus`
- `buy bread`
- `sell sword`
- `time`
- `save`
- `branch before_war`
- `switch merchant_path`

Implementation: a registry of command → typed Action mapping. Unknown
verbs fall through to the Intent LLM.

### 4.2 Intent LLM (fallback, ~5%)

Invoked only when the rule parser fails. Used for inputs like:

> "I want to secretly spread rumors that the duke murdered his wife."

Prompt instructs the LLM to return strict JSON:

```json
{
  "action": "spread_rumor",
  "target": "duke",
  "stealth": true
}
```

The JSON is validated against a schema before reaching the simulation.
Unknown action types are rejected. This is the choke point for
prompt-injection defense.

### 4.3 Narrator LLM (gated, infrequent)

Most turns are rendered from local templates. The Narrator LLM is invoked
only when the action resolution produces output flagged as "narratively
significant" — first kisses, deaths, betrayals, lineage transfers, major
political events.

Prompt receives a structured fact block (never raw world state):

```
Facts:
- Elena trust=75
- Mood=happy
- Recent memory: "player gifted flowers"
- Relationship history: [...]
```

LLM returns narrative text. It cannot mutate state.

### 4.4 World AI (weekly, asynchronous)

Runs on a configurable schedule (default: once per in-game week). Generates:

- 5 rumors (current circulating gossip)
- 2 political tensions
- 1 local legend or cultural artifact
- Optional: religious text, song, recipe

World AI output is parsed into typed records and committed to the
simulation as state. This is the *only* sanctioned LLM-to-state path,
and it goes through the same validation pipeline as player actions.

### 4.5 Narration and Determinism

Determinism applies to **world state**, not prose. The simulation must
reproduce identical state given identical seed + actions + time. Narration
may vary across replays. This is explicit in the spec to prevent confusion
later.

---

## 5. Simulation Architecture

Chronicle's simulation is organized into three strict tiers. Every engine
belongs to exactly one tier. This prevents random engine sprawl and keeps
the codebase legible as it grows.

```
LEVEL 1: Entity System
  - People, Locations, Factions, Items
  - Pure data, no behavior
  - Persisted in SQLite

LEVEL 2: Systems
  - Pure functions over entity state
  - Economy, Relationships, Goals, Memory, Population, Events, Timeline
  - Systems communicate ONLY through entity state
  - No system imports another system's package
  - No system calls the LLM

LEVEL 3: Orchestration
  - Tick Engine (drives simulation forward)
  - Event Resolver (handles cross-system events)
  - Time Advancement (advances sim time)
  - Deterministic RNG (single seeded source)
  - Narration Decision (template vs narrator LLM)
```

**Architectural invariant:** A Tier 2 system may not import any other Tier
2 system. Cross-system effects (e.g. an economic shock changing a
relationship) must go through entity state. This rule is enforced in CI.

### 5.1 Population Engine (Tier 2)

**Responsibilities:** births, deaths, aging, migration, marriages, family
trees.

**Tick frequency:** daily.

**Population dynamics:**

- **Soft population cap** per settlement, configurable in world pack
  (e.g. Blackwater = 80). When exceeded, **migration pressure** rises.
- **Migration pressure** is a per-settlement scalar (0–100). It modifies
  the probability that an NPC chooses to leave for a less-crowded
  location. Higher pressure = more emigration.
- **Family inheritance constraints:** on parent death, the heir is the
  closest living relative by blood (ties broken by age, then by
  relationship score with the deceased). Only one heir per estate;
  other children must strike out (migration, marriage, occupation
  change).
- **Birth cap:** a person can have at most 1 child per 12 sim-months and
  at most 6 children total.

**Determinism:** fully deterministic given seed.

### 5.2 Relationship Engine (Tier 2)

**Responsibilities:** friendship, romance, rivalries, betrayal, trust
evolution.

**Tick frequency:** daily.

**Axes (v1):** trust, respect, fear, attraction, loyalty.

**Memory-driven deltas:** relationships change in response to events
recorded in the Memory Engine. The deltas are stored on the memory
record itself (see §5.6), not recomputed from history. This keeps
updates O(1) per event instead of O(history).

**Open question:** v1.1 may add `familiarity`, `gratitude`, `resentment`.
Schema migration is straightforward because axes live in a JSON column.

### 5.3 Goal Engine (Tier 2) — Utility AI with 3-Layer Actions

The Goal Engine is the heart of "autonomous individuals who pursue goals."
It is a **utility AI with hierarchical goals and a 3-layer action system**,
not a behavior tree.

#### NPC Architecture

```
NPC
 ├─ Traits          (permanent modifiers)
 ├─ Needs           (dynamic, hunger/wealth/companionship/safety/...)
 ├─ Long-Term Goals (years-long ambitions)
 ├─ Memories        (influence scoring)
 ├─ Relationships   (influence scoring)
 └─ Resources       (current holdings)
```

#### 3-Layer Action System

Where do candidate actions come from? Three distinct sources, evaluated
in order. This bounds the action space and prevents explosion.

**Layer 1 — Hard Actions (engine-defined verbs)**
A finite, registered set of ~30 verbs: `work`, `travel`, `sleep`, `eat`,
`talk`, `inspect`, `buy`, `sell`, `give`, `take`, `marry`, `propose`,
`complain`, `gossip`, etc. Always available to any NPC who meets the
preconditions. Defined as typed Go functions.

**Layer 2 — Contextual Actions (rule-expanded)**
Generated from the NPC's current state via rules in the world pack.

Examples:
- `loneliness > 60` AND `target.present` → enable `court`
- `wealth > 1000` AND `has_property` → enable `start_business`
- `health < 30` → enable `seek_healing`
- `spouse.recently_died` AND `age > 40` → enable `seek_remarriage`

Rules live in `worldpacks/frontier/actions.yaml`. The engine evaluates
the rule set per NPC per tick.

**Layer 3 — Opportunistic Actions (environment triggers)**
Event-driven actions that appear for specific NPCs when the Event Engine
fires. Examples:
- `fire` event → nearby NPCs get `flee_fire` available
- `child_born` event → parents get `raise_child` available
- `theft_opportunity` event → nearby NPCs with low honesty get `steal`
- `market_day` event → merchants get `open_stall` available

The Event Engine decides which actions become available to which NPCs.
NPCs then score them like any other candidate.

This 3-layer system means the action space per NPC per tick is bounded
(Layer 1 ≈ 30 verbs, Layer 2 ≈ 5–10 contextual, Layer 3 ≈ 0–3
opportunistic). Total: ~40 actions to score per NPC per tick. Cheap.

#### Utility Calculation

```
score(action) =
    need_satisfaction       (0–100)
  + goal_alignment          (0–50)
  + personality_modifiers   (-50 to +50, from traits)
  + memory_modifiers        (-60 to +60, from causal memories)
  + relationship_modifiers  (-50 to +50, from relationship axes)
  + small_randomness        (-5 to +5, from deterministic RNG)
```

The NPC picks the highest-scoring action. Ties are broken by
deterministic RNG (see §7.3 and `SIMULATION_TICK_SPEC.md`).

#### Hierarchical Goals

NPCs never directly pursue `Become Noble`. They evaluate the actionable
subgoal with the highest utility. A character with `loneliness: 80` and
`ambition: 90` will not pursue `Become Noble` through a sub-action that
scores low for them.

#### What V1 Ships

- 3-layer action system
- Rule constraints
- Utility scoring with all five modifier sources
- Hierarchical goals
- Memory- and relationship-influenced scoring
- Deterministic tie-breaking

#### What V2 Adds

- Lightweight planner that generates candidate actions from a goal
- Dynamic goal generation (NPCs invent new long-term goals from events)

### 5.4 Economy Engine (Tier 2) — Closed Production Loops

The economy is not just "tracked state." It is a network of **closed
production loops** with explicit causality.

#### Resources (5)

Food, Wood, Iron, Cloth, Coin.

#### Production Loops (closed)

```
Farmer:
  output:   { food: 1.0 / day }
  requires: { tools: 0.05, labor: 1.0 }

Woodcutter:
  output:   { wood: 1.0 / day }
  requires: { tools: 0.05, labor: 1.0 }

Blacksmith:
  output:   { tools: 0.05 / day }
  requires: { iron: 0.2, wood: 0.1, labor: 1.0, coal: 0.1 }

Weaver:
  output:   { cloth: 1.0 / day }
  requires: { tools: 0.02, labor: 1.0 }

Miner:
  output:   { iron: 0.5 / day }
  requires: { tools: 0.1, labor: 1.0 }
```

**Productivity multiplier:** having Tools above a threshold multiplies
output. Without tools, an NPC works at 0.5x productivity. With 5+ tools,
1.5x. Tool scarcity ripples through the entire economy.

**Coin flow:** coin comes from selling outputs and external trade.
Without trade, coin slowly bleeds out of the local economy (no minting
in v1).

**Closed-loop rule:** every output must have at least one input loop.
If a resource has no producer, the engine flags it as a v2 candidate.

#### Trade

Locations connected by trade routes (defined in `entities.yaml`) can
exchange goods. Trade is automatic — the engine moves goods from
surplus to deficit locations on a daily tick. Prices are set by simple
supply/demand curves.

#### Inflation

Coin supply vs. goods supply. If coin outpaces goods, prices rise. If
goods outpace coin, prices fall. No central bank in v1.

#### What V2 Adds

- Banking, lending, interest
- Minting and currency manipulation
- Complex multi-stage production (e.g. smelting requires ore + fuel)

### 5.5 Event Engine (Tier 2)

Generates events as **simulation outcomes**, never as scripted stories.

**V1 event kinds:** `fire`, `famine`, `crime`, `political_unrest`,
`religious_movement`, `epidemic`, `birth`, `death`, `marriage`,
`theft_opportunity`, `market_day`, `bandit_attack`, `good_harvest`,
`bad_harvest`.

Events are consequences of state, not plot devices. The Event Engine
reads current state (food stores, faction tension, weather, NPC mood
distributions) and probabilistically generates events per tick.

**Event outcomes** can:
- Trigger opportunistic actions (Layer 3 of the 3-layer system)
- Add records to the Memory Engine for affected NPCs
- Modify entity state (death, marriage, location change)
- Create follow-up events (a fire causes a `displaced_population` event)

### 5.6 Memory Engine (Tier 2) — Causal Anchoring

Memory is not a flat JSON blob. It is a **causally anchored** record
that explains *why* something mattered and *what* it changed.

#### Memory Schema

```go
type Memory struct {
    ID                string
    OwnerID           string      // who remembers
    EventID           string      // what happened
    CauseEventID      string      // why (cascading — links to prior cause)
    Tick              int64
    Importance        float64     // 0–1, derived from event severity
    Recency           float64     // decays over time
    EmotionalScore    float64     // 0–1
    TrustDelta        float64     // how this event changed trust in target
    RelationshipDelta float64     // how this event changed the relationship
    Description       string
    Tags              []string
}
```

#### Why Causal Anchoring

Without `CauseEventID`, NPCs forget *why* something mattered. They know
"trust decreased" but not "because the player killed my brother, who was
killed because the player was falsely accused, which was caused by..."
Causal chains let NPCs reason about blame, gratitude, and pattern
recognition.

Without `TrustDelta` and `RelationshipDelta`, relationship state has to
be recomputed from event history every time it is queried. Storing the
delta on the memory makes relationship updates O(1).

#### Decay

Only significant memories persist indefinitely (Importance > 0.7). Minor
memories decay. Decayed memories leave a trace in the relationship
score itself — the delta is "baked in" — even after the memory is gone.

#### LLM Compression

V1 keeps memory compression **deterministic** (rule-based summarization
of nearby similar memories). LLM memory compression is deferred to v2
pending a strict contract: "the LLM may merge or summarize memories but
may not alter `TrustDelta` or `RelationshipDelta` fields or delete
events."

### 5.7 Timeline Engine (Tier 2)

Git-like branching of world state.

- `chronicle branch before_war` creates a branch from the current tick
- Branches are stored as per-branch SQLite files in `branches/`
- Each branch replays from the parent tick on first open
- Operations: `branch`, `switch`, `list`, `delete`, `diff`, `log`
- Merging is **out of scope for v1**. Conflicts are resolved by the
  player choosing one branch to keep.

### 5.8 Orchestration Layer (Tier 3)

The orchestration layer is the only tier that knows about the others.

**Components:**

- **Tick Engine** — drives simulation forward. For each tick, runs all
  Tier 2 systems in a fixed order. See `SIMULATION_TICK_SPEC.md` for the
  exact sequence.
- **Event Resolver** — collects events emitted by Tier 2 systems,
  applies follow-up effects, dispatches memory records, and triggers
  Layer 3 opportunistic actions.
- **Time Advancement** — converts player actions and auto-tick rates
  into tick deltas. Owns the "advance day" / "advance week" semantics.
- **Deterministic RNG** — single seeded source of randomness for the
  entire sim. Seeded by `(world_seed, tick, entity_id)`. See §7.3 and
  the tick spec.
- **Narration Decision** — chooses between template renderer and
  narrator LLM based on the event's narrativity score.

**Architectural invariant:** the orchestration layer is the only place
that imports both Tier 1 and Tier 2. It is also the only place that
imports `internal/llm`. The LLM boundary is preserved.

---

## 6. Player Model — Lineage

The player is **a consciousness moving through history, not a protagonist.**

### 6.1 Start

- Random commoner background from a fixed pool: farmer's child, merchant
  apprentice, orphan, priest trainee, hunter, laborer, blacksmith
  apprentice
- Age 16–22
- No chosen-one status, no prophecy, no special powers
- Class: typically Lower or Middle
- Starts inside the town of Blackwater

### 6.2 Death and Succession

On death, the game computes a successor score:

```
score = relationship_strength
      + family_connection
      + age
      + proximity
      + shared_history
      + inheritance_rights
      + faction_membership
```

Highest-scoring candidate is auto-selected. The player sees:

```
Supun Hewagamage died peacefully at age 84.

Funeral attendees: 143
The Chronicle continues.

Successor:
Amelia Hewagamage
Age: 27
Occupation: Merchant
Relationship: Daughter

Reason:
Closest surviving heir and primary inheritor.

Press Enter to continue.
Or type: successors
```

Typing `successors` shows the full ranked list.

### 6.3 Continuation Modes

The player can choose from five modes on death:

1. **Heir** (default) — closest blood relative
2. **Family** — any relative (son, daughter, brother, cousin, grandchild)
3. **Character** — any known living character
4. **Observer** — no body; watch the world, possess someone later
5. **End Bloodline** — close the world; the chronicle ends with the death

Default flow is fully automatic. The player only acts if they want to.

### 6.4 Legacy Record

On death, generate a legacy record:

```
SUPUN HEWAGAMAGE
Born: 1427
Died: 1511

Achievements:
- Founded Blackwater Trading Guild
- Served as Mayor
- Started the Grain Riots

Relationships:
- Married Elena
- 4 Children

Reputation:
Respected Merchant

Legacy Score: 712
```

The legacy becomes part of world history. NPCs reference it. Future
generations know the name.

### 6.5 Lineage Test

The "leave descendants" success criterion is a first-class feature.
V1 will be evaluated on whether a 5-generation playthrough produces
memorable emergent stories.

---

## 7. Time Model and Tick Discipline

Two orthogonal concepts: **sim time** (the world's clock) and **player
time** (how long the player's commands consume).

### 7.1 Sim Time Control

- Default: paused (tick rate = 0)
- Player commands:
  - `advance day` — advances time by 1 day, runs all engines
  - `advance week` — advances time by 7 days, runs all engines
  - `advance month` — advances time by 30 days, runs all engines
  - `auto-tick on` — runs the sim at a configurable real-time rate
    (e.g. 1 sim-hour per N real-seconds)
  - `auto-tick off`
- `time` command shows current sim date and pending events

### 7.2 Player Time

Actions consume sim time. Examples:

| Action               | Duration  |
|----------------------|-----------|
| Talk to someone      | 15 min    |
| Visit market         | 30 min    |
| Travel to village    | 4 hrs     |
| Work shift           | 8 hrs     |
| Sleep                | 8 hrs     |

When the player issues a duration action:

1. Time advances by the action's duration
2. All engines tick for the elapsed time
3. The action is resolved at the new time
4. Events generated during the elapsed time are resolved
5. Narration is produced

### 7.3 Tick Discipline and Determinism

**The simulation always runs forward in time, never backward.** When
time advances, it advances by an integer number of ticks. There is no
"partial tick" and no "lazy tick."

**Tick order is fixed and total.** For each tick, all engines run in
the order specified in `SIMULATION_TICK_SPEC.md`. The order is part of
the determinism contract — changing the order changes the result.

**RNG is fully deterministic.** The single `Rand` source is seeded by:

```go
rng = seeded(world_seed + tick + entity_id)
```

Where:
- `world_seed` is the seed used to create the world (stored in
  `world.db`)
- `tick` is the current sim tick number
- `entity_id` is the ID of the entity that needs a random value
  (NPC, event, location, etc.)

Two callers needing randomness for the same `(tick, entity_id)` get
the same value. Two callers needing randomness for different
`(tick, entity_id)` get different (but reproducible) values.

**Global RNG stream is forbidden.** No `math/rand` global. No
`time.Now()` as a seed. The tick RNG is the only source of randomness
in the sim.

### 7.4 Auto-Tick Behavior

When auto-tick is on:
- The orchestration layer advances time at the configured rate
- Each advance triggers a full tick
- The LLM is rate-limited (see §11.3)
- Player input interrupts auto-tick temporarily

### 7.5 The 1-Command-1-Day Rule is Broken

`ARCHITECTURE.md` originally implied one tick per command. V1 explicitly
rejects that. Actions have duration. Travel is expensive. The world
feels real because choices have weight.

---

## 8. Command Pipeline

```
Player Input
   ↓
Intent Extraction (rule parser, fallback to intent LLM)
   ↓
Action Validation (schema check, preconditions, state check)
   ↓
Simulation Update (engines tick if sim time has advanced)
   ↓
Event Resolution
   ↓
Narration Decision (template or narrator LLM)
   ↓
Response
```

### 8.1 Action Validation is Strict

Validation is a typed schema check, not a vibe check:

- Action verb is in the registered action set
- All required fields present
- All references (target Person, Location, Item) exist
- Preconditions met (e.g. you can't `buy` from a closed market)
- The LLM **cannot bypass validation** even if it returns valid JSON for
  a non-existent action. Validation is the last gate.

### 8.2 Example

Player input: `"Ask Elena to marry me."`

- Rule parser fails (no exact verb match)
- Intent LLM returns: `{"action": "propose_marriage", "target": "elena"}`
- Validation: `propose_marriage` is registered, `elena` exists and is
  alive, player is unmarried, etc.
- Simulation: evaluate relationship; Elena's acceptance probability is
  computed deterministically from trust, romance, family approval, etc.
- Result: `accepted` or `rejected` with structured reason
- Narration: template if routine, Narrator LLM if first marriage proposal
  in this world

---

## 9. Persistence

### 9.1 On-Disk Layout

Default location follows XDG:

- Linux: `~/.local/share/chronicle/`
- macOS: `~/Library/Application Support/chronicle/`
- Windows: `%AppData%\chronicle\`

Resolution order:

1. `--world-dir` flag
2. `CHRONICLE_WORLD_DIR` env var
3. XDG default

### 9.2 Directory Layout

```
~/.local/share/chronicle/
  worlds/
    7c52e5c3/                  # world ID, not world name (IDs are unique)
      metadata.yaml            # name, seed, created, last played
      world.db                 # current head of main timeline
      branches/
        before_war.db
        merchant_path.db
      config.yaml              # world pack reference, settings
  packs/                       # user-installed world packs
    frontier/
  cache/                       # narrator LLM cache, embeddings
  logs/                        # crash logs, debug output
```

### 9.3 World IDs

Worlds are addressed by short ID (8 hex chars), not by name. Names are
not unique; users will create "Kingdom of Ashes" three times. `metadata.yaml`
maps name ↔ ID for display.

### 9.4 Branching Storage

Two options were considered:
- Per-branch SQLite files in `branches/` (simpler, copy-on-write)
- Single `timeline.db` with branch rows (cleaner, harder to reason about)

V1 chooses **per-branch SQLite files**. Each branch is its own DB; the
parent tick is replayed on first open. This trades disk for clarity and
avoids schema gymnastics.

### 9.5 SQLite Schema (v1)

```sql
CREATE TABLE world_meta (
  key   TEXT PRIMARY KEY,
  value TEXT
);

CREATE TABLE people (
  id          TEXT PRIMARY KEY,
  name        TEXT,
  gender      TEXT,
  birth_tick  INTEGER,
  death_tick  INTEGER,
  alive       INTEGER,
  location_id TEXT,
  class       TEXT,
  occupation  TEXT,
  traits_json TEXT,
  needs_json  TEXT,
  goals_json  TEXT,
  legacy      TEXT
);

CREATE TABLE relationships (
  from_id   TEXT,
  to_id     TEXT,
  trust     REAL,
  respect   REAL,
  fear      REAL,
  attraction REAL,
  loyalty   REAL,
  history_json TEXT,
  PRIMARY KEY (from_id, to_id)
);

CREATE TABLE memories (
  id                TEXT PRIMARY KEY,
  owner_id          TEXT,
  event_id          TEXT,         -- what happened
  cause_event_id    TEXT,         -- why (cascading)
  tick              INTEGER,
  importance        REAL,
  recency           REAL,
  emotional         REAL,
  trust_delta       REAL,         -- baked-in relationship change
  relationship_delta REAL,
  description       TEXT,
  tags_json         TEXT
);

CREATE TABLE locations (
  id        TEXT PRIMARY KEY,
  name      TEXT,
  region    TEXT,
  population INTEGER,
  economy_json TEXT
);

CREATE TABLE factions (
  id    TEXT PRIMARY KEY,
  name  TEXT,
  goals_json TEXT,
  members_json TEXT
);

CREATE TABLE events (
  id              TEXT PRIMARY KEY,
  parent_event_id TEXT,         -- for event chains
  tick            INTEGER,
  kind            TEXT,
  payload_json    TEXT
);

CREATE TABLE inventory (
  person_id TEXT,
  resource  TEXT,
  amount    REAL,
  PRIMARY KEY (person_id, resource)
);
```

Each `world.db` represents one branch's head. Replay from a parent tick
is the merge primitive; explicit merge is v2.

---

## 10. CLI Surface (v1)

Top-level commands (Kong):

```
chronicle play [world-id-or-name]    # start/resume a world
chronicle new <name>                 # create a new world with a pack
chronicle list                       # list worlds
chronicle delete <world-id>          # delete a world
chronicle branch <name>              # branch current world
chronicle switch <world-id>          # switch to another world
chronicle timeline <world-id>        # show branch graph
chronicle pack list                  # list installed world packs
chronicle pack install <source>      # install a pack
chronicle export <world-id>          # export world archive
chronicle import <archive>           # import world archive
chronicle version
chronicle doctor                     # environment + LLM key check
```

In-game REPL (paused by default):

```
> look
> talk elena
> travel north
> inspect marcus
> time
> advance day
> advance week
> auto-tick on
> save
> branch before_war
> quit
```

Unknown verbs fall through to the Intent LLM with a confirmation prompt
before the LLM-derived action runs.

---

## 11. LLM Configuration

### 11.1 API Key

Single env var: `OPENCODE_ZEN_API_KEY` (or whatever the OpenCode Zen
convention is — to be confirmed against their docs at integration time).

`chronicle doctor` checks that the key is set and the endpoint is
reachable.

### 11.2 Model Selection

OpenCode Zen exposes multiple models. V1 uses one model for all three
roles, configurable via `config.yaml`:

```yaml
llm:
  provider: opencode-zen
  base_url: https://api.opencode.ai/v1
  intent_model: <model-id>
  narrator_model: <model-id>
  world_ai_model: <model-id>
  max_tokens_intent: 256
  max_tokens_narrator: 600
  max_tokens_world_ai: 1200
```

Default model IDs are set in code; users can override.

### 11.3 Cost Controls

- Narrator LLM is **never** called more than once per tick
- Intent LLM is rate-limited per turn (max 1 call per command)
- World AI runs on a fixed schedule, not per turn
- All LLM outputs are cached

#### LLM Cache Key

The cache key is a hash of:

```
(world_hash + action_type + npc_state_summary + prompt_template_version)
```

Where:
- `world_hash` — hash of the world state at the time of the call
  (changes whenever any entity in the affected region changes)
- `action_type` — the verb (e.g. `propose_marriage`, `greet`)
- `npc_state_summary` — small JSON of the relevant NPC's state
  (relationship, mood, recent memory)
- `prompt_template_version` — version of the prompt template, so
  template changes invalidate the cache

Cache hits return the previously generated text. Cache misses call the
LLM. The cache is content-addressable storage in `cache/llm/`.

#### LLM Rate Limits

- Narrator: max 1 call per 4 sim-hours (configurable)
- Intent: max 1 call per command
- World AI: max 1 call per sim-week

If a rate limit is hit, fall back to template rendering.

---

## 12. Distribution

Priority-ordered:

1. **Primary:** Prebuilt binaries via GoReleaser for linux/darwin/windows
   on amd64 and arm64. Install script at `chronicle.sh/install.sh`. Optional
   Homebrew tap. Optional scoop/winget/chocolatey manifests.
2. **Secondary:** `go install github.com/chronicle-dev/chronicle/cmd/chronicle@latest`
   for developers.
3. **Tertiary:** Docker image (`chronicle`) for server-side or container
   use. Mounted volume for `~/.local/share/chronicle`.

CI: GitHub Actions → GoReleaser → binaries → GitHub Release on tag.

The module path `github.com/chronicle-dev/chronicle` is a placeholder
until the user picks a final org.

---

## 13. V1 Success Criteria

A player should be able to:

- [ ] Start as a randomized nobody in Blackwater
- [ ] Live decades in a single world
- [ ] Form meaningful relationships that influence NPC behavior over time
- [ ] Influence Blackwater's politics, economy, or culture
- [ ] Leave descendants
- [ ] Die and have the world continue
- [ ] Have the world generate stories worth retelling

A test of merit: a 5-generation playthrough produces at least one story
the player wants to tell someone else. If yes, v1 succeeds.

---

## 14. V2 Scope (Explicitly Out of v1)

These are deliberately deferred:

- Magic system
- Combat system
- Crafting
- Quests
- Multiplayer
- Banking and complex finance
- Lightweight planner for the Goal Engine
- LLM-driven memory compression (with strict loss contract)
- Branch merging
- World registry / installable community worlds
- TUI / Web / Discord frontends

V2 will be specified separately. V1 must be playable and demonstrably
emergent before v2 work begins.

---

## 15. Risks and Open Questions

### 15.1 Performance

150 NPCs with daily ticks is fine. The 3-layer action system bounds
scoring to ~40 actions per NPC per tick. If we scale to thousands, we
will need spatial partitioning or a tick-staggering strategy. **Not
blocking v1.**

### 15.2 Relationship Schema

Five axes may be too thin for "meaningful relationships." V1 ships with
5; v1.1 may add `familiarity`, `gratitude`, `resentment`. Easy schema
migration because axes live in a JSON column.

### 15.3 Memory Compression

Currently deterministic. LLM compression is allowed in
`ARCHITECTURE.md` but introduces a state-mutation vector. V1 defers
this. When added, the contract must specify: "the LLM may merge or
summarize memories but may not alter `TrustDelta` or
`RelationshipDelta` fields or delete events."

### 15.4 Prompt Injection Defense

The Intent LLM and World AI are the two paths where untrusted text
(player input or LLM-generated prose) meets a state-mutating system.
The validation layer is the choke point. We should add fuzz tests that
feed adversarial inputs and assert that no unregistered action verb or
invalid target ever reaches the simulation.

### 15.5 Action Space Balance

The 3-layer action system is bounded, but the **quality** of contextual
rules (Layer 2) determines NPC richness. Too few = boring NPCs. Too
many = chaos. The `actions.yaml` for `frontier` needs careful tuning.
A "behavior test" — running 1000 NPCs through a year and inspecting the
distribution of chosen actions — will help validate tuning.

### 15.6 Causal Memory Chain Length

`CauseEventID` creates chains. A 5-generation lineage can have
thousands of causally linked events. The Memory Engine must cap chain
length (e.g. stop linking beyond 10 generations) and provide a
"summary" mechanism for ancient history. **TBD in v1.1.**

### 15.7 Production Loop Tuning

The 5-resource closed-loop economy is small but still has ~15
parameters (production rates, consumption rates, tool multipliers,
trade costs). Miscalibration can cause cascading collapse or runaway
growth. The world pack should ship with conservative defaults and a
`chronicle economy inspect` command for debugging.

### 15.8 World Pack Authoring

V1 ships with one pack (`frontier`). The pack format needs to be stable
and well-documented before community packs are realistic.
`docs/pack-format.md` is a v1.0 deliverable.

### 15.9 Discoverability

The CLI is the only surface in v1. There's no map view, no NPC list,
no "what's happening in town" feed. For a social sim, the player's
**ability to notice** is half the gameplay. A `chronicle status`
command that surfaces nearby events, gossip, and tensions is a likely
v1.1 addition.

---

## 16. Definition of Done for V1

- [ ] A new contributor can clone the repo, run `make test`, and have all
      tests pass
- [ ] `chronicle new test-world --pack frontier` creates a working world
- [ ] `chronicle play test-world` enters the in-game REPL
- [ ] A scripted 5-generation playthrough produces non-trivial emergent
      stories across 100 runs with the same seed
- [ ] LLM is removable: a `--no-llm` flag runs the sim with template-only
      narration and intent parser only
- [ ] All 150 NPCs advance through a simulated year in under 60 seconds
      on a mid-range laptop
- [ ] World can be exported and re-imported without state loss
- [ ] Two branches of the same world can be created and switched between
- [ ] `chronicle doctor` reports a clean environment given a valid API key

---

## Appendix A — Decision Log

| # | Decision | Choice | Reason |
|---|----------|--------|--------|
| 1 | LLM provider | OpenCode Zen only | User-specified |
| 2 | Language | Go | User-specified |
| 3 | V1 world pack | "frontier" (Free Marches) | Late medieval, no magic, no combat — best test of the sim engine |
| 4 | V1 scope: magic/combat | Out of v1, soft lore only | Consistent with `ARCHITECTURE.md` V1 |
| 5 | Player model | Lineage (Person, dies, succession) | Tests the lineage success criterion |
| 6 | Time control | Hybrid (paused by default, advance commands, auto-tick) | Best UX for both pacing and idle play |
| 7 | Economy driver | Corrupt / failing institutions | Politics-driven, not resource-driven |
| 8 | LLM call pattern | Rule parser + Intent LLM fallback + Template renderer + Narrator LLM for important moments + World AI weekly | Cost, control, determinism |
| 9 | Goal Engine | Utility AI with hierarchical goals | Best fit for long-term NPC ambitions |
| 10 | Lineage transfer | Auto-pick with override + 5 continuation modes | Strongest narrative continuity |
| 11 | Gender ratio | 51% F / 49% M (fixed in world pack) | Reverses the user's earlier "more women - less men" idea — they concluded the imbalance wasn't needed for v1 |
| 12 | CLI library | Kong | Typed, low boilerplate, keeps CLI thin |
| 13 | Save location | XDG with `--world-dir` and env var overrides | OS-native, zero-config default |
| 14 | Distribution | GoReleaser (primary), go install, Docker | Native binaries first, alternatives for power users |
| 15 | SQLite driver | `modernc.org/sqlite` (pure Go) | No CGO, easy cross-compile |
| 16 | LLM client | stdlib `net/http` + small wrapper | No heavy SDK dependency |
| 17 | World ID scheme | Short hex, not name | Names are not unique |
| 18 | Branch storage | Per-branch SQLite files in `branches/` | Simpler than unified `timeline.db` |
| 19 | Time model | Actions consume sim time | Travel costs hours, work costs 8 hours |
| 20 | Memory compression | Deterministic in v1; LLM compression deferred | Avoids the state-mutation vector |
| 21 | Simulation architecture | 3-tier (Entity / Systems / Orchestration) | Prevents random engine sprawl |
| 22 | Action system | 3-layer (Hard / Contextual / Opportunistic) | Bounds the action space |
| 23 | Memory model | Causal anchoring with TrustDelta/RelationshipDelta | Long-term coherence + O(1) updates |
| 24 | Economy | Closed production loops with explicit input/output | Causal, not cosmetic |
| 25 | Tick model | Player action → time advances → sim catches up | Matches the advance-day UX |
| 26 | RNG | `seeded(world_seed + tick + entity_id)` | Determinism, no global rand |
| 27 | LLM cache key | `(world_hash + action_type + npc_state + prompt_ver)` | Reuse identical narration |
| 28 | Population dynamics | Soft caps + migration pressure + family inheritance rules | Prevents runaway demographics |
| 29 | Validation rigor | Schema check + fuzz tests for Intent LLM and World AI | Defense against prompt injection |

