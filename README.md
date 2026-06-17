# Chronicle v2

> A handcrafted choice-driven fantasy adventure.
>
> You are the protagonist of a branching story. Every decision you
> make — who you trust, what you spare, which faction you stand with —
> shapes the path you walk and the ending you reach.

Chronicle v2 is a **branching fantasy adventure engine** with five
handcrafted acts, four playable protagonists, twelve endgame routes,
and three romance targets. There is no AI narration. Story content is
authored by writers and represented as data. The engine is a
deterministic narrative state machine.

> **Status:** Documentation has pivoted to v2 in commit `Phase 34`.
> The current codebase is still the v1 simulation design; the code
> migration is tracked in [`PHASES.md`](./PHASES.md). For the binding
> pivot plan, see [`chronicle-v2-pivot-spec.md`](./chronicle-v2-pivot-spec.md).

## What's in the box

### The Story

Five authored **acts** of a single branching tale — the discovery of
the Dragon Relic, the gathering of allies, the truth behind the Void
Dragon, the war that decides the world's future, and the finale that
resolves it. Act totals: **20 + 50 + 40 + 50 + 20 = 180 nodes** across
the acts (aspirational target; content authoring is the next phase).

Every act is a graph of **story nodes**, each presenting authored
prose and a list of **choices**. A choice's effects can set flags,
modify variables, change relationships, alter faction reputation, or
trigger follow-up scenes.

### The Cast

**Four protagonists** at game start:

- **Kael** — warrior-scholar in exile
- **Lyra** — half-elven runesmith
- **Raven** — disgraced royal heir
- **Aria** — orphan raised by the Faith of the Dawn

Each protagonist has a unique opening sequence and a set of
**ExclusiveNodes** that only they can reach.

**Three romance targets** unlock as your relationships grow:

- **Elara** — Trust ≥ 50 unlocks her backstory; Affection ≥ 75 opens the
  Elara route. Trust ≤ −50 opens her betrayal route.
- **Selene** — Archmage of the West; the dragon-touched path.
- **Orion** — Wandering mercenary; the wanderer path.

(Plus a wider cast of named companions whose Trust / Affection / Respect
axes can shape which scenes and endings you have access to.)

### The World

**Four factions** your standing with shapes what's available to you:

- **Kingdom** — the standing crown, its law and its lords.
- **Mages** — the Arcanum and its keepers of forbidden lore.
- **Dragons** — the old powers, slumbering and waking.
- **Underworld** — the syndicate that runs the shadows between cities.

Reputation ranges −100 to +100 per faction. Some endings require
you to be at a particular standing with a particular faction; others
flip the score and watch what comes next.

### The Player Experience

**Pure choice menu.** You do not type free-form commands. You read the
node, see the choices, and pick one. The route through the story is
entirely up to you.

**Handcrafted prose.** Every `StoryNode.Text` was written by a writer.
There is no template, no LLM, no procedural substitution. The prose
on the screen is exactly what the writer authored.

**Relationship-driven events.** As your Trust, Affection, and Respect
with a companion grow or shrink, scenes unlock automatically. Some
require a flag set earlier in the story. Some require you to be at the
right place at the right time. The author wired all of it.

**Save / load at any node.** One `.json` save file holds your entire
`WorldState` (current node, all flags, all variables, all
relationship scores, all reputation scores, your inventory, your
party, your unlocked endings). Load it back and pick up exactly where
you left off. Save/load is byte-stable round-trip; no migrations
until v2.1.

**Twelve endings.** Reach the finale and the engine evaluates every
ending in priority order. Highest valid ending wins. Most endings
have a romance variant (Elara/Selene/Orion) and a non-romance variant.

## Build, Test, Run

```bash
# Build
make build                       # produces ./chronicle
# or: go build -o chronicle ./cmd/chronicle

# Run all tests
make test
# or: go test -count=1 -timeout 30m ./...

# Run just the fast unit tests
go test -count=1 -short ./...

# Vet
make vet

# Tidy dependencies
make tidy
```

### CLI Examples (v2)

```bash
# Play a fresh story (pick a protagonist at start)
./chronicle

# Save your current story
./chronicle save -out myrun.json

# Resume from a save
./chronicle resume myrun.json

# Inspect a save (read-only)
./chronicle info myrun.json

# Diff two saves
./chronicle diff run1.json run2.json
```

> The `diff`, `info`, `resume`, and `save` subcommands are planned for
> the post-migration phases; today the codebase is still v1 and the
> baked-in commands are the v1 simulation subcommands. They will be
> replaced as PHASES.md Phase 39+ lands.

### In-Game Flow

You start, you pick a protagonist, you read the opening node, you pick
your first choice, and the engine advances. There is no typing — there
is only choosing.

```
Kael's story begins.

"You stand at the threshold of Greyhall Keep. The wind carries the
smell of woodsmoke and something older — incense, or the memory of
incense, from the ruins on the eastern ridge…"

  [1] Enter the keep.
  [2] Head for the eastern ridge.
  [3] Wait in the square and watch.

> 1

You enter the keep.
…
```

The route through the next chapter is entirely in your hands.

## Determinism

Chronicle v2 is deterministic end-to-end on the player path:

- **No RNG in the player loop.** Choice resolution, condition
  evaluation, effect application, and event triggering are all
  pure functions of `(WorldState, StoryNode, ChoiceID)`. Re-run the
  same save against the same authored content and you get the same
  next state.
- **Save/load round-trip is byte-stable.**
  `SaveGame → disk JSON → SaveGame` preserves the in-memory state
  exactly. See `ARCHITECTURE.md §18A`.
- **Content is content-addressed.** Authored YAML is hashed at load.
  Hash mismatches are a hard error — no silent divergence.

There is no LLM dependency. Story text is the writer's text. The
engine never generates prose.

## Project Layout

### v2 target tree (canonical for the v2 design)

```text
chronicle/
├── cmd/
│   └── chronicle/                # main entry point + CLI subcommands
├── internal/
│   ├── engine/                   # runtime orchestrator (runner.go)
│   ├── story/                    # StoryNode + Choice + Condition + Effect types and loader
│   ├── state/                    # WorldState + SaveGame + JSON (un)marshal
│   ├── events/                   # authored event trigger evaluator
│   ├── endings/                  # ending evaluation (highest-valid-wins priority)
│   ├── content/                  # YAML content loader + version verification
│   └── ui/                       # choice-menu CLI renderer
├── content/
│   ├── protagonists/             # Kael, Lyra, Raven, Aria YAMLs
│   ├── companions/               # Elara, Selene, Orion, etc. YAMLs
│   ├── acts/                     # act1.yaml … act5.yaml
│   └── endings.yaml
├── ARCHITECTURE.md               # this file's sibling (canonical v2 spec)
├── PHASES.md                     # v2 phase plan
├── chronicle-v2-pivot-spec.md    # binding plan behind the v2 doc pivot
└── go.mod
```

### Current code (v1 — being retired)

The current codebase under `internal/` is still the v1 simulation
design (simulation engines, LLM-first narration, SQLite per-branch
persistence). It does not match the v2 target tree above. The
retirement and migration are tracked in [`PHASES.md`](./PHASES.md),
starting at Phase 35.

## License

TBD.
