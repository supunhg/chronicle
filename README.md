# Chronicle

> You are not watching a simulation. You are living in a world.
>
> A frontier of villages and towns, where autonomous people remember,
> form relationships, pursue goals, and create history — whether you
> are there to witness it or not.

Chronicle is an immersive text adventure powered by a persistent
reality simulation engine. You ARE your character. You walk the
cobblestone streets, talk to the blacksmith, search the ruins, pray
at the temple, and live your life in The Free Marches.

The simulation runs 7 production engines under the hood — population,
relationships, marriage, memory, goals, economy, and events — but the
player experiences it as a living, breathing world.

> **Status:** The simulation substrate is solid. The immersive text
> adventure layer (atmospheric narration, NPC dialogue, exploration,
> gossip system) is active and growing. See
> [`PHASES.md`](./PHASES.md) for the roadmap.

## What's in the box

### The Living World

**The Free Marches** — a frontier region with 7 locations, each with
its own buildings, people, and character. Blackwater is the bustling
town (12 buildings including The Drunken Fox Inn, Black Duck Tavern,
River Docks). Four villages, a monastery, and a fort each have their
own buildings. 8 landmarks dot the landscape (Standing Stones,
Raven's Crossing, Widow's Peak). 8 trade routes connect them all.

**150 autonomous NPCs** — each with personality traits, needs, goals,
memories, family ties, and relationships across 5 axes (trust, respect,
fear, attraction, loyalty). They work, gossip, marry, have children,
migrate, and pursue their ambitions whether you interact with them
or not.

**4 factions** — Merchant Guild, Town Council, Faith of the Dawn,
Frontier League — each with their own goals, allies, and rivals.

### The Player Experience

**Immersive narration** — LLM-first atmospheric descriptions for
everything you do. Look around and get sensory-rich scene descriptions.
Walk between settlements and encounter hawks, waymarkers, merchants
on the road. Enter buildings and feel the warmth of the inn's hearth
or the heat of the smithy's forge. All narrator templates use
second-person sensory voice matching the LLM system prompt — no
jarring switches between LLM and template modes.

**NPC dialogue** — Multi-turn conversations with LLM-driven NPCs who
speak in character, share gossip about people and places (ask "tell me
about Millbrook" or "what do you think of Elena"), reference their
memories and relationships, and react to your trust level. Gossip
system injects NPC knowledge about locations and people into dialogue.

**Character journal** — The `status` command produces an immersive
narrative moment of introspection — flowing prose about your identity,
wealth, family, and recent experiences. Coin amounts graded into
sensory descriptions, family references are emotional, memories woven
into reflection. No bullet points, no data dumps.

**Exploration** — Walk interactively (pick a destination, choose your
distance). Search buildings and surroundings. Pray at temples. Listen
to the world around you. Every command produces atmospheric text.

**20+ verbs** — look, inspect, talk, walk, travel, search, listen,
pray, wait, sleep, buy, sell, inventory, status, time, people, save,
branch, switch, character, and more. 45+ natural language aliases
("go", "enter", "visit", "examine", "rummage", "worship", etc.).

### The Engine

**7 simulation engines** — Population, Relationship, Marriage, Memory,
Goal, Economy, Event. Deterministic tick loop with full replay
validation.

**Persistence** — SQLite with full save/load round-trip.

**LLM layer** — OpenAI-compatible client. Narrator uses LLM for
important moments, templates for routine events. Intent parser
falls back to the LLM for unknown verbs. All LLM calls are
rate-limited and cacheable.

**Determinism** — `core.WorldHash` (SHA256) is the canonical state
fingerprint. Same seed + same actions = same world.

## Build, Test, Run

```bash
# Build
make build                       # produces ./chronicle
# or: go build -o chronicle ./cmd/chronicle

# Run all tests (slow integration tests included; budget ~30 min)
make test
# or: go test -count=1 -timeout 30m ./...

# Run just the fast unit tests
go test -count=1 -short ./...

# Vet
make vet

# Tidy dependencies
make tidy
```

### CLI Examples

```bash
# Play from scratch (default worldpack, 100 ticks, seed 12345)
./chronicle
./chronicle -ticks 100 -seed 12345
./chronicle -pack worldpacks/frontier -ticks 365 -seed 7

# Play and drop into the in-game REPL after the initial ticks
./chronicle -ticks 100 -seed 12345 -repl

# Save a snapshot
./chronicle save -ticks 100 -seed 12345 -out myrun.db

# Resume from a snapshot (with optional -repl)
./chronicle resume myrun.db -ticks 50
./chronicle resume myrun.db -ticks 50 -repl

# Inspect a snapshot (read-only)
./chronicle info myrun.db

# Diff two snapshots
./chronicle diff run1.db run2.db

# Check LLM config + API key + endpoint
./chronicle doctor

# Auto-resume on extinction
./chronicle save -ticks 3650 -auto-resume -auto-resume-ticks 100 -seed 12345
```

### In-Game REPL

After `./chronicle -repl`, you live in the world:

```
> look                        Take in your surroundings
> walk                         Wander interactively (pick where to go)
> walk to the inn              Walk to a specific building
> talk elena                   Start a conversation (NPC speaks in character)
> search                       Search your surroundings
> pray                         Find a quiet moment of reflection
> status                       Your character journal
> inspect marcus               Study someone in detail
> listen                       Pause and listen to the world
> travel millbrook             Journey to another settlement
> buy bread                    Trade with merchants
> inventory                    Check what you're carrying
> time                         What season and hour is it?
> save                         Save your world
> quit                         Leave the world
```

During conversations, just type naturally. Say "bye" to end.
All output goes to **stderr**.

## API Keys

The LLM integration is partial. The current simulation is fully
template-based and **does not require any API key**.

To enable the LLM-gated narration path (talk events):

```bash
export OPENCODE_ZEN_API_KEY="sk-..."
# (or whatever key convention OpenCode Zen uses)
```

The `chronicle doctor` command checks the key and endpoint reachability.
A `--no-llm` flag for explicit disable is on the roadmap
([`PHASES.md`](./PHASES.md) Phase 32).

## Project Layout

```
chronicle/
├── cmd/chronicle/         # main entry point + CLI subcommands
├── internal/
│   ├── action/            # action engine (20+ verbs including walk, search, pray, status)
│   ├── conversation/      # NPC dialogue + gossip/rumor system
│   ├── core/              # domain types (World, Person, Relationship, Memory, Location with Buildings)
│   ├── intent/            # intent parser (rule-based + LLM fallback, 18 actions + 30+ aliases)
│   ├── llm/               # OpenAI-compatible client, config, doctor
│   ├── narrator/          # LLM-first atmospheric narration (scenes, people, walks, buildings, journeys)
│   ├── persistence/       # SQLite-backed storage + migrations
│   ├── repl/              # in-game REPL (interactive walk, conversation mode, adventure help)
│   ├── simulation/        # 7 engines: population, relationship, marriage, memory, goal, economy, event
│   ├── tick/              # orchestration layer + deterministic RNG
│   ├── lineage/           # player death + succession + legacy records
│   └── worldpack/         # YAML worldpack loader + bootstrap
├── worldpacks/frontier/   # "The Free Marches" — 7 locations, 30+ buildings, 8 landmarks
├── docs/DETERMINISM.md    # determinism contract
├── SIMULATION_TICK_SPEC.md
├── ARCHITECTURE.md        # canonical spec (vision + design + DoD)
├── PHASES.md              # roadmap
├── go.mod
├── go.sum
└── Makefile
```

## Determinism

The simulation is fully deterministic given identical
`(world_seed, tick, input state)`. RNG is seeded by
`(worldSeed, tick, entityID)` per `docs/DETERMINISM.md §3`. No
global `math/rand`, no `time.Now()` in engines. `core.WorldHash` is the
canonical fingerprint for replay validation, save/load verification, and
branch divergence detection.

## License

TBD.
