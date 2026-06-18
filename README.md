# Chronicle

> A handcrafted, choice-driven fantasy adventure.
>
> You are the protagonist of a branching story. Every decision you
> make — who you trust, what you spare, which faction you stand with —
> shapes the path you walk and the ending you reach.

Chronicle is a **branching fantasy adventure engine**. All story content
is authored by writers and represented as data. The engine is a
deterministic narrative state machine — there is no AI narration, no
procedural prose, and no randomness in the player path.

---

## What's in this release

This release covers **Acts 1–3** of the Frontier story:

- **~43 story nodes** across three acts
- **4 playable protagonists**, each with a unique opening
- **12 distinct endings**, from heroic sacrifice to dark descent
- **3 romance routes** that unlock as your relationships deepen
- **4 factions** whose regard for you shapes what paths open
- **Save / load at any point** — one JSON file holds your entire journey

### The Cast

**Four protagonists** at game start:

- **Kael** — warrior-scholar in exile
- **Lyra** — half-elven runesmith
- **Raven** — disgraced royal heir
- **Aria** — orphan raised by the Faith of the Dawn

Each protagonist has a unique opening sequence and exclusive scenes
no other protagonist can reach.

**Three romance targets** unlock through the story:

- **Elara** — the ranger who walks beside you
- **Selene** — archmage of the crystal spire
- **Orion** — wandering mercenary of the northern camps

**Four factions** track your standing:

- **Kingdom** — the standing crown and its lords
- **Mages** — the Arcanum and its keepers of lore
- **Dragons** — the old powers, slumbering and waking
- **Underworld** — the syndicate between the cities

### The Player Experience

**Pure choice menu.** You read the scene, see the choices, and pick
one. The route through the story is entirely up to you.

**Handcrafted prose.** Every word on the screen was written by a
writer. There is no template substitution, no LLM generation, no
procedural filler.

**Relationship-driven events.** As your Trust, Affection, and Respect
with companions grow, new scenes unlock automatically. Some require
flags you set earlier. Some require the right faction standing. The
author wired all of it.

**Save / load at any node.** One `.json` save file holds your entire
`WorldState`. Load it back and pick up exactly where you left off.

**Twelve endings.** Reach the finale and the engine evaluates every
ending in priority order. Highest valid ending wins. Most endings have
both a romance and a non-romance variant.

---

## Build, Test, Run

```bash
# Build
make build                       # produces ./chronicle

# Run all tests
make test

# Run just the fast unit tests
go test -count=1 -short ./...

# Vet
make vet
```

### CLI

```bash
# Start an interactive playthrough (pick a protagonist at start)
./chronicle play -protagonist Kael

# Play through a scripted sequence of choices
./chronicle play -protagonist Kael -script playthroughs/kael.txt

# Save your current story
./chronicle save -out myrun.json

# Resume from a save
./chronicle resume myrun.json

# Inspect a save (read-only)
./chronicle info myrun.json

# Diff two saves
./chronicle diff run1.json run2.json
```

### In-Game Flow

You pick a protagonist, read the opening node, pick your first choice,
and the engine advances. There is no typing — there is only choosing.

```
Kael's story begins.

"You scan the barren slope where your order's keep once stood.
The wind has scoured the stones clean..."

  [1] Continue south, toward Ashwick.

> 1

You stand at the threshold of Greyhall Keep...

  [1] Enter the keep.
  [2] Head for the eastern ridge.
  [3] Wait in the square and watch.

> 1
```

The route through the next chapter is entirely in your hands.

---

## Determinism

Chronicle is deterministic end-to-end:

- **No RNG in the player loop.** Choice resolution, condition
evaluation, effect application, and event triggering are all pure
functions of `(WorldState, StoryNode, ChoiceID)`.
- **Save/load round-trip is stable.** `SaveGame → disk JSON →
SaveGame` preserves state exactly.
- **No AI dependency.** Story text is the writer's text. The engine
never generates prose.

---

## Project Layout

```text
chronicle/
├── cmd/
│   └── chronicle/                # CLI entry point
├── internal/
│   ├── engine/                   # runtime orchestrator
│   ├── story/                    # StoryNode + Choice + Condition + Effect
│   ├── state/                    # WorldState + SaveGame
│   ├── events/                   # authored event trigger evaluator
│   ├── endings/                  # ending evaluation
│   ├── content/                  # YAML content loader
│   └── ui/                     # choice-menu renderer
├── worldpacks/
│   └── frontier/                 # authored story content
│       ├── protagonists.yaml
│       ├── nodes.yaml
│       ├── events.yaml
│       ├── companions.yaml
│       └── endings.yaml
├── playthroughs/                 # committed scripted spines for regression testing
├── ARCHITECTURE.md               # engine design spec
├── PHASES.md                     # development phase plan
└── go.mod
```

---

## License

MIT — see [LICENSE](./LICENSE).
