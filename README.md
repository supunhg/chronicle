# Chronicle

> A persistent reality simulation engine where autonomous individuals
> remember, form relationships, pursue goals, create history, and
> continue existing long after the player leaves or dies.

Chronicle is a Go implementation of the spec in
[`chronicle-spec.md`](./chronicle-spec.md) and the determinism contract
in [`SIMULATION_TICK_SPEC.md`](./SIMULATION_TICK_SPEC.md). The
architectural intent is in [`ARCHITECTURE.md`](./ARCHITECTURE.md).

---

## Current Status (Phases 1–15)

**Core simulation + persistence + CLI are done and tested.** The full
v1 from the spec is not yet complete — LLM integration, the economy
engine, the event engine, and goal-engine action selection are still
pending.

What works today:
- **Population engine** — aging, mortality, births, migration, family trees
- **Relationship engine** — co-location formation, axis decay toward neutral, O(1) memory-delta application
- **Memory engine** — causally-anchored birth/death records
- **SQLite persistence** — world_meta, people, world_rules, relationships, memories
- **Five CLI subcommands** — `play`, `save`, `resume`, `info`, `diff`
- **Auto-resume hook** — automatically resume a saved DB on game-over
- **~50+ tests** — all passing; `go build` and `go vet` clean

What's still pending for full v1:
- LLM integration (Intent LLM, Narrator LLM, World AI)
- Economy engine (closed production loops)
- Event engine
- Goal engine action selection (3-layer system, utility scoring)
- Branching timelines (`chronicle branch`, `chronicle switch`)
- In-game REPL / TUI

---

## Build, Test, Run

```bash
# Build
make build                       # produces ./chronicle
# or: go build -o chronicle ./cmd/chronicle

# Test
make test                        # runs all tests
# or: go test ./...

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

# Save a snapshot
./chronicle save -ticks 100 -seed 12345 -out myrun.db

# Resume from a snapshot
./chronicle resume myrun.db -ticks 50

# Inspect a snapshot (read-only)
./chronicle info myrun.db

# Diff two snapshots
./chronicle diff run1.db run2.db

# Auto-resume on extinction
./chronicle save -ticks 3650 -auto-resume -auto-resume-ticks 100 -seed 12345
```

All output goes to **stderr** (metadata, progress, summaries). The
binary is silent on stdout.

---

## API Keys

The LLM integration is not yet implemented (Phase 17+). The current
simulation is fully template-based and **does not require any API key**.

When the LLM layer is added, the spec calls for OpenCode Zen with a
single env var:

```bash
export OPENCODE_ZEN_API_KEY="sk-..."
# or whatever key convention OpenCode Zen uses at integration time
```

The `chronicle doctor` command (planned) will check the key and
endpoint reachability.

---

## Project Layout

```
chronicle/
├── cmd/chronicle/         # main entry point + CLI subcommands
├── internal/
│   ├── core/              # domain types (World, Person, Relationship, Memory, …)
│   ├── persistence/       # SQLite-backed storage + migrations
│   ├── simulation/        # engines: Population, Relationship, Memory, Goal
│   ├── tick/              # orchestration layer + deterministic RNG
│   └── worldpack/         # YAML worldpack loader + bootstrap
├── worldpacks/frontier/   # the v1 worldpack ("The Free Marches")
├── chronicle-spec.md      # full v1 spec
├── SIMULATION_TICK_SPEC.md # determinism contract
├── ARCHITECTURE.md        # architectural intent
├── go.mod
├── go.sum
└── Makefile
```

---

## Determinism

The simulation is fully deterministic given identical
`(world_seed, tick, input state)`. RNG is seeded by
`(worldSeed, tick, entityID)` per `SIMULATION_TICK_SPEC.md §3`. No
global `math/rand`, no `time.Now()` in engines.

---

## License

TBD.
