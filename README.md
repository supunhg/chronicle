# Chronicle

> A persistent reality simulation engine where autonomous individuals
> remember, form relationships, pursue goals, create history, and
> continue existing long after the player leaves or dies.

Chronicle is a Go implementation of the spec in
[`ARCHITECTURE.md`](./ARCHITECTURE.md). The determinism contract is in
[`docs/DETERMINISM.md`](./docs/DETERMINISM.md). The simulation tick order
is in [`SIMULATION_TICK_SPEC.md`](./SIMULATION_TICK_SPEC.md).

> **Status:** Phases 1–25 committed. Phase 26 (Stability & Persistence
> Validation) in progress. The simulation substrate is solid; the player
> experience layer (lineage transfer, `chronicle new`, `help` command,
> LLM-driven narration) is the v1 work remaining. See
> [`PHASES.md`](./PHASES.md) for the checklist.

## What's in the box

**Simulation core** — all 7 production engines wired through a
deterministic tick loop:

- **Population** — aging, mortality, births, migration, family trees
- **Relationship** — co-location bond formation, axis decay toward neutral, O(1) memory-delta application
- **Marriage** — pair matching with trust/age/class constraints
- **Memory** — causally-anchored birth/death records with TrustDelta/RelationshipDelta baked into the relationship cache
- **Goal** — utility AI with hierarchical goals and a 3-layer action system
- **Economy** — closed production/consumption loops, price recalc, shortage detection
- **Event** — 4 state-driven rules (Famine, Crime, Political, Religious) with per-(rule, location) cooldown

**Persistence** — SQLite with 4 migrations; full save/load round-trip
preserves every field `core.WorldHash` covers.

**CLI** — 6 subcommands: `play` (default), `save`, `resume`, `info`,
`diff`, `doctor`. All play-tick subcommands support `-repl`.

**In-game REPL** — fully functional. 12 verbs (`look`, `inspect`, `talk`,
`travel`, `sleep`, `inventory`, `time`, `save`, `buy`, `sell`, `branch`,
`switch`) plus meta-commands (`people`, `auto-tick on|off`, `advance
day|week|month`, `quit`).

**LLM layer** — OpenAI-compatible client (`internal/llm`), env-var auth
(`OPENCODE_ZEN_API_KEY`), `chronicle doctor` to validate. Narrator uses
templates by default; LLM-gated for significant events. Intent parser
falls back to the LLM for unknown verbs.

**Worldpack** — `worldpacks/frontier` ("The Free Marches") with 150 NPCs,
4 factions, 1 town + 4 villages + monastery + fort + forest + river.

**Determinism** — `core.WorldHash` (SHA256) is the canonical state
fingerprint; `TestDeterministicReplay` and `TestDifferentSeedsDiverge`
are the v1 acceptance gates.

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

After `./chronicle -repl`, you can type any of:

```
> look
> look alice
> inspect marcus
> talk elena
> travel blackwater
> sleep
> sleep 8
> inventory
> time
> save [path.db]
> buy bread
> sell sword
> branch before_war
> switch merchant_path
> people
> auto-tick on
> auto-tick off
> advance day
> advance week
> advance month
> quit
```

Aliases: `i` → `inventory`, `date` → `time`.

All output goes to **stderr** (metadata, progress, summaries). The
binary is silent on stdout.

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
│   ├── action/            # action engine (talk, travel, sleep, buy, sell, save, branch, switch)
│   ├── core/              # domain types (World, Person, Relationship, Memory, ...)
│   ├── intent/            # intent parser (rule-based + LLM fallback)
│   ├── llm/               # OpenCode Zen client, config, doctor
│   ├── narrator/          # template renderer + LLM-gated narration
│   ├── persistence/       # SQLite-backed storage + migrations
│   ├── repl/              # in-game REPL (12 verbs + auto-tick + advance)
│   ├── simulation/        # 7 engines + marriage
│   ├── tick/              # orchestration layer + deterministic RNG
│   └── worldpack/         # YAML worldpack loader + bootstrap
├── worldpacks/frontier/   # the v1 worldpack ("The Free Marches")
├── docs/DETERMINISM.md    # determinism contract
├── SIMULATION_TICK_SPEC.md
├── ARCHITECTURE.md        # canonical v1 spec (vision + design + DoD)
├── PHASES.md              # v1 phase checklist
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
