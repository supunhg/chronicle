# Chronicle — Architecture & V1 Specification

**Status:** v1 specification (the canonical spec; `chronicle-spec.md` is deprecated)
**Last updated:** 2026-06-15
**Module:** `github.com/chronicle-dev/chronicle`

This document is the single source of truth for Chronicle's design and v1 scope.
It merges the original `ARCHITECTURE.md` (vision + principles) with the
`chronicle-spec.md` (concrete build plan) into one canonical spec.

For the determinism contract, see [`docs/DETERMINISM.md`](./docs/DETERMINISM.md).
For the simulation tick order, see [`SIMULATION_TICK_SPEC.md`](./SIMULATION_TICK_SPEC.md).

---

# Part I — Vision & Principles

## 1. Vision

Chronicle is not a game.

Chronicle is a persistent reality simulation engine.

Players do not complete quests.

Players inhabit living worlds populated by autonomous individuals who remember,
form relationships, pursue goals, create history, and continue existing long
after the player leaves or dies.

The engine is responsible for reality.

LLMs are responsible only for interpretation and narration.

The simulation must remain valid even if all AI text generation is removed.

> **V1 is a "unit test" for human society.** If relationships, inheritance,
> ambition, memory, and reputation work in the v1 world pack, they will work
> in any future pack. If they don't, no amount of magic or dragons will save
> the design.

## 2. Core Design Principles

### 2.1 Simulation First

World state is the source of truth. Never allow an LLM to invent facts.

- **Bad:** Player asks "Am I married?" → LLM replies "Yes."
- **Good:** Simulation: `marriage_exists = true` → LLM narrates from the fact.

### 2.2 Persistent Worlds

Worlds never reset automatically. Worlds continue evolving indefinitely. A
player may die; the world survives. Future generations inherit consequences.

### 2.3 Story Emergence

No mandatory main quest. No predefined hero. No chosen one. Stories emerge
naturally from relationships, politics, economy, disasters, wars, inventions,
ambitions.

### 2.4 Deterministic Simulation

Given identical (world state, seed, actions), the simulation produces identical
results. See `docs/DETERMINISM.md` for the full contract.

### 2.5 World Pack Architecture

The engine is genre-agnostic. Fantasy, cyberpunk, post-apocalypse, and space
opera all run on the same simulation core. Genre-specific content lives in
world packs (`worldpacks/<pack-name>/`).

## 3. High-Level Architecture

```
Player
   ↓
CLI Interface (chronicle play | save | resume | info | diff | doctor)
   ↓
Command Pipeline (intent parser → action engine)
   ↓
Simulation Engine (Tier 3 orchestration)
   ├── Population Engine
   ├── Relationship Engine
   ├── Marriage Engine
   ├── Memory Engine
   ├── Goal Engine
   ├── Economy Engine
   └── Event Engine
   ↓
Persistence Layer (SQLite, per-branch)
   ↓
Narration Layer (LLM; bypassable with --no-llm)
```

**Architectural invariant:** `internal/simulation` MUST NOT import
`internal/llm`. The boundary is enforced in CI. The LLM is a sink, not a
source — it can narrate, but it cannot mutate state.

---

# Part II — V1 World Pack: "The Free Marches"

## 4. Setting

A frontier region between rival city-states. Late medieval / early Renaissance
(1400–1500 equivalent). No magic, no combat, no quests. Social mobility and
emergent stories are the gameplay.

### 4.1 Region

- **Name:** The Free Marches
- **Size:** ~120 km × 120 km
- **Geography:** 1 town (Blackwater, pop. 80), 4 villages, 1 monastery, 1 fort, 1 trade route, 1 forest, 1 river

### 4.2 Population (150 NPCs at bootstrap)

| Group | Count |
|---|---|
| Town | 80 |
| Villages | 50 |
| Travelers | 10 |
| Nobility | 5 |
| Clergy | 5 |

- **Gender ratio:** 51% female / 49% male
- **Age distribution:** 0–15 (25%), 16–30 (30%), 31–50 (30%), 51+ (15%)

Children, marriages, births, and inheritance are all present from day one. No
protected age group.

### 4.3 Factions (exactly 4)

| Faction | Goal | Members |
|---|---|---|
| **Merchant Guild** | Profit, trade, influence | shopkeepers, traders, caravan owners |
| **Town Council** | Stability, taxes, law | mayor, officials, landowners |
| **Faith of the Dawn** | Charity, morality, social influence | priests, monks, followers |
| **Frontier League** | Independence, expansion, land ownership | hunters, settlers, farmers |

### 4.4 Economy (5 resources)

Food, Wood, Iron, Cloth, Coin. Closed production loops with explicit
input/output. See §10.4.

### 4.5 Occupations (~15 archetypes)

`farmer`, `merchant`, `blacksmith`, `innkeeper`, `guard`, `priest`, `baker`,
`carpenter`, `hunter`, `teacher`, `laborer`, `miller`, `trader`, `mayor`, `clerk`.

### 4.6 Social Classes

Three: Lower, Middle, Upper. Class affects marriage eligibility, reputation
gains/losses, and the set of opportunities an NPC can pursue.

### 4.7 Initial Tensions

Every world starts with unresolved pressure to seed events: trade crisis
(food prices rising), council corruption (mayor accused of favoritism),
religious dispute (temple seeking greater influence).

### 4.8 Worldpack Directory Layout

```
worldpacks/frontier/
  rules.yaml         # tunable parameters: birth rate, marriage rules, etc.
  entities.yaml      # locations, buildings, resources
  factions.yaml      # 4 factions with goals and members
  events.yaml        # event templates and triggers
  occupations.yaml   # 15 occupations and their needs weights
  generation.yaml    # world gen parameters: 150 NPCs, age dist, gender ratio
  actions.yaml       # 3-layer action rules (Phase 21+)
```

The engine loads packs dynamically. The pack is the unit of genre.

---

# Part III — Tech Stack

## 5. Languages, Libraries, Providers

- **Language:** Go (1.22+)
- **CLI library:** stdlib `flag` (current); Kong is a v1.1 candidate for typed commands
- **SQLite driver:** `modernc.org/sqlite` (pure Go, no CGO)
- **LLM provider:** OpenCode Zen only (OpenAI-compatible endpoint)
- **LLM client:** stdlib `net/http` + small typed wrapper (no heavy SDK)
- **Testing:** stdlib `testing` + table-driven tests + `t.TempDir()` for isolation

## 6. Module Layout

```
chronicle/
  cmd/chronicle/         # main entry point + CLI subcommands
  internal/
    action/              # action engine (talk, travel, sleep, buy, sell, save, branch, switch)
    cli/                 # (future) typed command definitions
    core/                # domain types (World, Person, Relationship, Memory, …)
    intent/              # intent parser (rule-based + LLM fallback)
    llm/                 # OpenCode Zen client, config, doctor
    narrator/            # template renderer + LLM-gated narration
    persistence/         # SQLite access, schema, migrations
    repl/                # in-game REPL (12 verbs + auto-tick + advance)
    simulation/          # engines: population, relationship, marriage, memory, goal, economy, event
    tick/                # orchestration layer + deterministic RNG
    worldpack/           # YAML worldpack loader + bootstrap
  worldpacks/frontier/   # v1 worldpack
  docs/                  # design + determinism docs
  chronicle-spec.md      # DEPRECATED — content merged into this file
  SIMULATION_TICK_SPEC.md
  ARCHITECTURE.md        # this file
  go.mod
  go.sum
  Makefile
```

**Architectural invariant (CI-enforced):** `internal/simulation` MUST NOT
import `internal/llm`. The LLM is a sink, not a source.

---

# Part IV — LLM Architecture

## 7. Hybrid Pipeline

The LLM is invoked in three distinct roles. Each is gated, cost-controlled,
and bypassable for testing.

```
Player Input
     ↓
Intent Parser
   ├─ Rule Parser (95%)        — regex, command aliases
   └─ Intent LLM (5%)          — fallback for complex NL
     ↓
Action Engine
     ↓
Narration Decision
   ├─ Template Renderer        — most turns
   └─ Narrator LLM             — important moments only
     ↓
Player

A fourth, asynchronous role runs on a schedule:

Weekly Tick
     ↓
World AI                       — generates rumors, books, legends,
                                 cultural shifts, religious texts
     ↓
These become simulation inputs (state mutations), not narration.
```

### 7.1 Rule Parser (~95% of inputs)

Handles `look`, `inventory`, `sleep`, `travel <loc>`, `talk <name>`,
`inspect <name>`, `buy <item>`, `sell <item>`, `time`, `save`, `branch <name>`,
`switch <name>`, `people`, `auto-tick on|off`, `advance day|week|month`,
`quit`, `exit`, and aliases (`i`, `date`).

Unknown verbs fall through to the Intent LLM.

### 7.2 Intent LLM (fallback, ~5%)

Invoked only when the rule parser fails. Used for inputs like:

> "I want to secretly spread rumors that the duke murdered his wife."

Prompt instructs the LLM to return strict JSON:

```json
{ "action": "spread_rumor", "target": "duke", "stealth": true }
```

The JSON is validated against a schema before reaching the simulation.
Unknown action types are rejected. This is the choke point for
prompt-injection defense.

### 7.3 Narrator LLM (gated, infrequent)

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

### 7.4 World AI (weekly, asynchronous) — NOT YET IMPLEMENTED (Phase 33)

Runs on a configurable schedule (default: once per in-game week). Generates
5 rumors, 2 political tensions, 1 local legend or cultural artifact.

Output is parsed into typed records and committed to the simulation as
state. This is the *only* sanctioned LLM-to-state path, and it goes
through the same validation pipeline as player actions.

### 7.5 Determinism

Determinism applies to **world state**, not prose. The simulation must
reproduce identical state given identical seed + actions + time. Narration
may vary across replays. This is explicit to prevent confusion later.

---

# Part V — Simulation Architecture

## 8. Three-Tier Model

Chronicle's simulation is organized into three strict tiers. Every engine
belongs to exactly one tier. This prevents random engine sprawl.

```
LEVEL 1: Entity System
  - People, Locations, Factions, Items
  - Pure data, no behavior
  - Persisted in SQLite

LEVEL 2: Systems
  - Pure functions over entity state
  - Economy, Relationships, Goals, Memory, Population, Events, Marriage
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

## 9. Tick Order (pinned by v1 acceptance tests)

For each tick, all 7 engines run in this exact order:

```
Population → Relationship → Marriage → Memory → Goal → Economy → Event
```

Reordering any two engines changes the result. The order is part of the
determinism contract (see `docs/DETERMINISM.md` §4).

## 10. Per-Engine Specs

### 10.1 Population Engine (Tier 2)

**Responsibilities:** births, deaths, aging, migration, marriages, family trees.

**Tick frequency:** daily.

**Population dynamics:**

- **Soft population cap** per settlement (configurable in worldpack, e.g. Blackwater=80). When exceeded, migration pressure rises.
- **Migration pressure** is a per-settlement scalar (0–100). Modifies probability an NPC leaves for a less-crowded location.
- **Family inheritance constraints:** on parent death, the heir is the closest living relative by blood (ties: age, then relationship score). Only one heir per estate; other children must strike out.
- **Birth cap:** max 1 child per 12 sim-months and max 6 children total per person.

**Determinism:** fully deterministic given seed.

### 10.2 Relationship Engine (Tier 2)

**Responsibilities:** friendship, romance, rivalries, betrayal, trust evolution.

**Tick frequency:** daily.

**Axes (v1):** trust, respect, fear, attraction, loyalty.

**Memory-driven deltas:** relationships change in response to events recorded
in the Memory Engine. The deltas are stored on the memory record itself (see
§10.6), not recomputed from history. Updates are O(1) per event.

### 10.3 Marriage Engine (Tier 2)

Pair matching with trust/age/class constraints. Uses a deterministic scan of
eligible partners per location per tick.

### 10.4 Economy Engine (Tier 2) — Closed Production Loops

5 resources: Food, Wood, Iron, Cloth, Coin.

Production loops (closed):

```
Farmer:      output { food: 1.0/day }   requires { tools: 0.05, labor: 1.0 }
Woodcutter:  output { wood: 1.0/day }   requires { tools: 0.05, labor: 1.0 }
Blacksmith:  output { tools: 0.05/day } requires { iron: 0.2, wood: 0.1, coal: 0.1, labor: 1.0 }
Weaver:      output { cloth: 1.0/day }  requires { tools: 0.02, labor: 1.0 }
Miner:       output { iron: 0.5/day }   requires { tools: 0.1, labor: 1.0 }
```

**Productivity multiplier:** Tools above a threshold multiplies output. Without
tools, an NPC works at 0.5× productivity. With 5+ tools, 1.5×.

**Coin flow:** coin comes from selling outputs and external trade. Without
trade, coin slowly bleeds out of the local economy (no minting in v1).

**Closed-loop rule:** every output must have at least one input loop. If a
resource has no producer, the engine flags it as a v2 candidate.

**Trade:** locations connected by trade routes can exchange goods. Trade is
automatic — the engine moves goods from surplus to deficit locations daily.
Prices are set by simple supply/demand curves.

**Inflation:** coin supply vs. goods supply. If coin outpaces goods, prices
rise. If goods outpace coin, prices fall. No central bank in v1.

### 10.5 Event Engine (Tier 2)

Generates events as **simulation outcomes**, never as scripted stories.

**V1 event kinds:** `fire`, `famine`, `crime` (TheftWave), `political_unrest`
(CouncilScandal), `religious_movement` (RevivalMovement), `epidemic`, `birth`,
`death`, `marriage`, `theft_opportunity`, `market_day`, `bandit_attack`,
`good_harvest`, `bad_harvest`.

Events are consequences of state, not plot devices. The Event Engine reads
current state (food stores, faction tension, NPC mood distributions) and
probabilistically generates events per tick.

**Event outcomes** can:
- Trigger opportunistic actions (Layer 3 of the 3-layer system)
- Add records to the Memory Engine for affected NPCs
- Modify entity state (death, marriage, location change)
- Create follow-up events (a fire causes a `displaced_population` event)

**Phase 23 v1 ships 4 rules** (Famine, Crime, Political, Religious) with a
per-(rule, location) cooldown of 30 ticks to prevent event spam.

### 10.6 Memory Engine (Tier 2) — Causal Anchoring

Memory is not a flat JSON blob. It is a **causally anchored** record that
explains *why* something mattered and *what* it changed.

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

Without `CauseEventID`, NPCs forget *why* something mattered. Causal chains
let NPCs reason about blame, gratitude, and pattern recognition.

Without `TrustDelta` and `RelationshipDelta`, relationship state has to be
recomputed from event history every time it is queried. Storing the delta
on the memory makes relationship updates O(1).

**Decay:** Only significant memories persist indefinitely (Importance > 0.7).
Minor memories decay. Decayed memories leave a trace in the relationship
score — the delta is "baked in".

**LLM Compression (v1: deferred; v2: strict contract).** When added, the
contract must specify: "the LLM may merge or summarize memories but may
not alter `TrustDelta` or `RelationshipDelta` fields or delete events."

### 10.7 Goal Engine (Tier 2) — Utility AI with 3-Layer Actions

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

**Layer 1 — Hard Actions (engine-defined verbs).** A finite, registered set
of ~30 verbs: `work`, `travel`, `sleep`, `eat`, `talk`, `inspect`, `buy`,
`sell`, `give`, `take`, `marry`, `propose`, `complain`, `gossip`, etc.
Always available to any NPC who meets the preconditions. Defined as typed
Go functions.

**Layer 2 — Contextual Actions (rule-expanded).** Generated from the NPC's
current state via rules in the worldpack (`actions.yaml`).

Examples:
- `loneliness > 60` AND `target.present` → enable `court`
- `wealth > 1000` AND `has_property` → enable `start_business`
- `health < 30` → enable `seek_healing`
- `spouse.recently_died` AND `age > 40` → enable `seek_remarriage`

**Layer 3 — Opportunistic Actions (environment triggers).** Event-driven
actions that appear for specific NPCs when the Event Engine fires.

- `fire` event → nearby NPCs get `flee_fire` available
- `child_born` event → parents get `raise_child` available
- `theft_opportunity` event → nearby NPCs with low honesty get `steal`
- `market_day` event → merchants get `open_stall` available

The action space per NPC per tick is bounded: ~40 actions to score. Cheap.

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

The NPC picks the highest-scoring action. Ties broken by deterministic RNG
(see `SIMULATION_TICK_SPEC.md` §3).

### 10.8 Orchestration Layer (Tier 3)

The only tier that knows about the others.

- **Tick Engine** — drives simulation forward. For each tick, runs all Tier 2
  systems in fixed order.
- **Event Resolver** — collects events emitted by Tier 2 systems, applies
  follow-up effects, dispatches memory records, triggers Layer 3
  opportunistic actions.
- **Time Advancement** — converts player actions and auto-tick rates into
  tick deltas.
- **Deterministic RNG** — single seeded source of randomness for the entire
  sim. Seeded by `(world_seed, tick, entity_id)`. See `docs/DETERMINISM.md`.
- **Narration Decision** — chooses between template renderer and narrator
  LLM based on the event's narrativity score.

**Architectural invariant:** the orchestration layer is the only place that
imports both Tier 1 and Tier 2. It is also the only place that imports
`internal/llm`. The LLM boundary is preserved.

---

# Part VI — Player Model: Lineage

## 11. The Player is a Consciousness, Not a Protagonist

### 11.1 Start

- Random commoner background from a fixed pool: farmer's child, merchant
  apprentice, orphan, priest trainee, hunter, laborer, blacksmith apprentice
- Age 16–22
- No chosen-one status, no prophecy, no special powers
- Class: typically Lower or Middle
- Starts inside the town of Blackwater

### 11.2 Death and Succession (NOT YET IMPLEMENTED — Phase 30)

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

### 11.3 Continuation Modes

The player can choose from five modes on death:

1. **Heir** (default) — closest blood relative
2. **Family** — any relative (son, daughter, brother, cousin, grandchild)
3. **Character** — any known living character
4. **Observer** — no body; watch the world, possess someone later
5. **End Bloodline** — close the world; the chronicle ends with the death

### 11.4 Legacy Record

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

### 11.5 Lineage Test

The "leave descendants" success criterion is a first-class feature. V1 will
be evaluated on whether a 5-generation playthrough produces memorable
emergent stories.

---

# Part VII — Time Model and Tick Discipline

## 12. Sim Time vs Player Time

Two orthogonal concepts: **sim time** (the world's clock) and **player
time** (how long the player's commands consume).

### 12.1 Sim Time Control

- Default: paused (tick rate = 0)
- Player commands: `advance day` (1 tick), `advance week` (7), `advance
  month` (30)
- `auto-tick on` — runs the sim at a configurable real-time rate
- `auto-tick off`
- `time` command shows current sim date and pending events

### 12.2 Player Time (Action Durations)

| Action | Duration |
|---|---|
| Talk to someone | 15 min |
| Visit market | 30 min |
| Travel to village | 4 hrs |
| Work shift | 8 hrs |
| Sleep | 8 hrs |

When the player issues a duration action:
1. Time advances by the action's duration
2. All engines tick for the elapsed time
3. The action is resolved at the new time
4. Events generated during the elapsed time are resolved
5. Narration is produced

### 12.3 Tick Discipline and Determinism

- **The simulation always runs forward in time, never backward.** Time
  advances in integer ticks. No partial ticks, no lazy ticks.
- **Tick order is fixed and total.** See §9.
- **RNG is fully deterministic.** Seeded by `(world_seed, tick,
  entity_id)`. See `docs/DETERMINISM.md`.
- **Global RNG stream is forbidden.** No `math/rand` global. No
  `time.Now()` as a seed. The tick RNG is the only source of randomness.

---

# Part VIII — Command Pipeline & Persistence

## 13. Command Pipeline

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

### 13.1 Action Validation is Strict

Validation is a typed schema check:
- Action verb is in the registered action set
- All required fields present
- All references (target Person, Location, Item) exist
- Preconditions met (e.g. you can't `buy` from a closed market)
- The LLM **cannot bypass validation** even if it returns valid JSON for a
  non-existent action. Validation is the last gate.

### 13.2 Example

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

## 14. Persistence

### 14.1 On-Disk Layout (DEVIATION: currently CWD-relative; XDG layout is Phase 35)

**Current v1:** snapshots are written to `<cwd>/<world-id>.db` (or `<cwd>/<path>`
if specified). The world ID is the 8-char hex of the seed.

**Target v1.1 (XDG):** `~/.local/share/chronicle/worlds/<id>/world.db` with
`metadata.yaml`, `config.yaml`, and `branches/<name>.db`.

Resolution order:
1. `--world-dir` flag
2. `CHRONICLE_WORLD_DIR` env var
3. XDG default (v1.1)

### 14.2 Schema (v4)

```sql
CREATE TABLE world_meta (key TEXT PRIMARY KEY, value TEXT);
-- keys: id, seed, tick, now, coin, player_id

CREATE TABLE people (
  id TEXT PRIMARY KEY, name, gender, birth_tick, death_tick, alive,
  location_id, class, occupation,
  is_merchant, traits_json, needs_json, goals_json,
  father_id, mother_id, spouse_id
);

CREATE TABLE world_rules (key TEXT PRIMARY KEY, value TEXT);
-- 8 rule fields: AdultAge, FertileMinAge, FertileMaxAge, AnnualDeathChance,
--               MinBirthIntervalTicks, MaxChildren, MigrationFraction,
--               MinMigrantsPerTick

CREATE TABLE relationships (
  from_id, to_id, trust, respect, fear, attraction, loyalty, history_json,
  PRIMARY KEY (from_id, to_id)
);

CREATE TABLE memories (
  id, owner_id, event_id, cause_event_id, tick, importance, recency,
  emotional, trust_delta, relationship_delta, description, tags_json
);

CREATE TABLE locations (
  id, name, region, population, population_cap, pressure,
  last_shortage_tick, settlement_json, prices_json
);

CREATE TABLE factions (
  id, name, color, base_location,
  goals_json, members_json, rivals_json, allies_json
);

CREATE TABLE events (
  id, parent_event_id, tick, kind, location, payload_json
);

CREATE TABLE inventory (person_id, resource, amount, PRIMARY KEY (person_id, resource));
-- Phase 18: weight, value, max_durability columns

CREATE TABLE items (name, count, weight, value, max_durability);
-- Phase 26 Part A: catalog for save/load round-trip
```

Each `world.db` represents one branch's head. Replay from a parent tick is
the merge primitive; explicit merge is v2.

### 14.3 World IDs

Worlds are addressed by short ID (8 hex chars), not by name. Names are not
unique; users will create "Kingdom of Ashes" three times.

### 14.4 Branching Storage

Two options were considered:
- Per-branch SQLite files in `branches/` (current; simpler, copy-on-write)
- Single `timeline.db` with branch rows (cleaner, harder to reason about)

V1 uses **per-branch SQLite files**. Each branch is its own DB; the parent
tick is replayed on first open. The action engine writes branches to
`./branches/<name>.db` (CWD-relative). Per-world `branches/` is a v1.1 polish.

---

# Part IX — CLI Surface (v1)

## 15. Top-Level Commands (current)

```
chronicle [play-flags]              # default; load a worldpack, bootstrap, simulate
chronicle save [flags]              # play + snapshot the post-tick world to a SQLite DB
chronicle resume <db-path> [-ticks] # restore from a SQLite snapshot, simulate more
chronicle info <db-path>            # print snapshot metadata (no ticks run)
chronicle diff <db1> <db2>          # compare two snapshots
chronicle doctor                    # check OPENCODE_ZEN_API_KEY and endpoint reachability
```

All subcommands that play ticks support `-repl` to drop into the in-game REPL
after the initial ticks.

## 16. Top-Level Commands (planned per spec §10; NOT YET IMPLEMENTED)

```
chronicle new <name>                 # create a new world with a pack
chronicle list                       # list worlds
chronicle delete <world-id>          # delete a world
chronicle branch <name>              # branch current world (REPL already has this)
chronicle switch <world-id>          # switch to another world (REPL already has this)
chronicle timeline <world-id>        # show branch graph
chronicle pack list                  # list installed world packs
chronicle pack install <source>      # install a pack
chronicle export <world-id>          # export world archive
chronicle import <archive>           # import world archive
chronicle version
```

These are deferred to a v1.1 polish.

## 17. In-Game REPL (current — fully functional)

```
> look
> look alice
> inspect marcus
> talk elena
> travel blackwater
> sleep [hours]
> inventory
> time
> save [path]
> buy bread [qty]
> sell sword [qty]
> branch before_war
> switch merchant_path
> people
> auto-tick on
> auto-tick off
> advance day
> advance week
> advance month
> quit
> exit
```

Aliases: `i` → inventory, `date` → time.

## 18. LLM Configuration

### 18.1 API Key

Single env var: `OPENCODE_ZEN_API_KEY`.

`chronicle doctor` checks that the key is set and the endpoint is reachable.

### 18.2 Model Selection

OpenCode Zen exposes multiple models. V1 uses one model for all three roles
(intent, narrator, world AI), configurable via `llm.yaml`:

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

### 18.3 Cost Controls

- Narrator LLM is **never** called more than once per tick
- Intent LLM is rate-limited per turn (max 1 call per command)
- World AI runs on a fixed schedule, not per turn
- All LLM outputs are cached (NOT YET IMPLEMENTED — Phase 34)

#### LLM Cache Key (planned)

```
(world_hash + action_type + npc_state_summary + prompt_template_version)
```

Cache hits return the previously generated text. Cache misses call the LLM.
The cache is content-addressable storage in `cache/llm/`.

#### LLM Rate Limits

- Narrator: max 1 call per 4 sim-hours (configurable)
- Intent: max 1 call per command
- World AI: max 1 call per sim-week

If a rate limit is hit, fall back to template rendering.

---

# Part X — Distribution

## 19. Distribution Tiers (priority-ordered)

1. **Primary:** Prebuilt binaries via GoReleaser for linux/darwin/windows
   on amd64 and arm64. Install script at `chronicle.sh/install.sh`. Optional
   Homebrew tap. Optional scoop/winget/chocolatey manifests.
2. **Secondary:** `go install github.com/chronicle-dev/chronicle/cmd/chronicle@latest`
   for developers.
3. **Tertiary:** Docker image (`chronicle`) for server-side or container
   use. Mounted volume for `~/.local/share/chronicle`.

CI: GitHub Actions → GoReleaser → binaries → GitHub Release on tag.

The module path `github.com/chronicle-dev/chronicle` is a placeholder until
the user picks a final org.

---

# Part XI — V1 Definition of Done

## 20. Success Criteria

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

## 21. Definition of Done

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

# Part XII — V2 Scope (Explicitly Out of v1)

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
- Magic, dragons, space ships, etc.

V2 will be specified separately. V1 must be playable and demonstrably
emergent before v2 work begins.

---

# Part XIII — Risks and Open Questions

## 22. Performance

150 NPCs with daily ticks is fine. The 3-layer action system bounds scoring
to ~40 actions per NPC per tick. If we scale to thousands, we will need
spatial partitioning or a tick-staggering strategy. **Not blocking v1.**

**Known bottleneck:** `CourtAction.Execute` (5.5M action evaluations across
36,500 ticks) is the current dominant cost in the 5-Generation acceptance
test. A v1 optimization is needed (cache candidate partners per location
per K ticks, or pre-filter by relationship axes).

## 23. Relationship Schema

Five axes may be too thin for "meaningful relationships." V1 ships with 5;
v1.1 may add `familiarity`, `gratitude`, `resentment`. Easy schema
migration because axes live in a JSON column.

## 24. Memory Compression

Currently deterministic. LLM compression is allowed but introduces a
state-mutation vector. V1 defers this. When added, the contract must
specify: "the LLM may merge or summarize memories but may not alter
`TrustDelta` or `RelationshipDelta` fields or delete events."

## 25. Prompt Injection Defense

The Intent LLM and World AI are the two paths where untrusted text (player
input or LLM-generated prose) meets a state-mutating system. The validation
layer is the choke point. We should add fuzz tests that feed adversarial
inputs and assert that no unregistered action verb or invalid target ever
reaches the simulation.

## 26. Action Space Balance

The 3-layer action system is bounded, but the **quality** of contextual
rules (Layer 2) determines NPC richness. Too few = boring NPCs. Too many
= chaos. The `actions.yaml` for `frontier` needs careful tuning. A
"behavior test" — running 1,000 NPCs through a year and inspecting the
distribution of chosen actions — will help validate tuning.

## 27. Causal Memory Chain Length

`CauseEventID` creates chains. A 5-generation lineage can have thousands of
causally linked events. The Memory Engine must cap chain length (e.g. stop
linking beyond 10 generations) and provide a "summary" mechanism for
ancient history. **TBD in v1.1.**

## 28. Production Loop Tuning

The 5-resource closed-loop economy is small but still has ~15 parameters
(production rates, consumption rates, tool multipliers, trade costs).
Miscalibration can cause cascading collapse or runaway growth. The world
pack should ship with conservative defaults and a `chronicle economy
inspect` command for debugging.

## 29. World Pack Authoring

V1 ships with one pack (`frontier`). The pack format needs to be stable
and well-documented before community packs are realistic. `docs/pack-format.md`
is a v1.0 deliverable (deferred to v1.1).

## 30. Discoverability

The CLI is the only surface in v1. There's no map view, no NPC list, no
"what's happening in town" feed. For a social sim, the player's
**ability to notice** is half the gameplay. A `chronicle status` command
that surfaces nearby events, gossip, and tensions is a likely v1.1
addition. The REPL has a `people` command but no relationships/memories
inspection.

---

# Part XIV — Decision Log

| # | Decision | Choice | Reason |
|---|---|---|---|
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
| 12 | CLI library | stdlib `flag` (Kong is a v1.1 candidate) | Stdlib is enough; low dependency surface |
| 13 | Save location | CWD-relative in v1, XDG in v1.1 | OS-native, zero-config default |
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
| 30 | Retention caps | `MaxLiveEvents=10K`, `MaxLiveMemories=100K` with FIFO trim to `Archived*` (Phase 26 Part B) | Bounded growth for hash cost and memory |
| 31 | Engine iteration order | Sorted-by-ID (Phase 26 Part E) | Eliminates Go map-iteration non-determinism in engine appends |
| 32 | Doc consolidation | Merge `chronicle-spec.md` into `ARCHITECTURE.md` (Phase 27) | One canonical spec, no drift between two files |

---

# Part XV — License

TBD.
