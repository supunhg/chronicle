# Chronicle v2

## A Branching Fantasy Adventure Engine

Version: 2.0

---

# 1. Vision

Chronicle is a choice-driven fantasy adventure game.

Players experience a handcrafted story through decisions, relationships, reputation, and exploration.

The game does not use AI-generated content.

All story content is authored by writers and represented as data.

The engine acts as a deterministic narrative state machine.

Core principle:

```text
Story Node
    ↓
Present Choices
    ↓
Apply Effects
    ↓
Update State
    ↓
Load Next Node
```

The player never types commands.

The player selects from available choices.

---

# 2. Design Goals

## Goals

* Fully deterministic
* Save/load at any point
* Multiple protagonists
* Multiple endings
* Romance routes
* Relationship-driven events
* Replayability
* Data-driven content
* No procedural story generation

## Non Goals

* AI narration
* Open world simulation
* Emergent NPC society
* Economy simulation
* Procedural quests
* Dynamic world history

---

# 3. Core Gameplay Loop

```text
Load Story Node
        ↓
Evaluate Conditions
        ↓
Show Narrative
        ↓
Show Choices
        ↓
Player Selects Choice
        ↓
Apply Consequences
        ↓
Trigger Events
        ↓
Advance Story
```

---

# 4. World State

WorldState is the entire game state.

```go
type WorldState struct {
    Tick int

    Protagonist string

    CurrentNodeID string

    Flags map[string]bool

    Variables map[string]int

    Relationships map[string]Relationship

    Reputation ReputationState

    Inventory Inventory

    Party []string

    EndingsUnlocked []string
}
```

WorldState is fully serializable.

No runtime-only state.

---

# 5. Story Structure

Story is represented as nodes.

```go
type StoryNode struct {
    ID string

    Title string

    Text string

    Choices []Choice

    Events []Event
}
```

Example:

```text
You stand before the ruined dragon temple.

The stone doors begin to open.
```

---

# 6. Choice System

Every node contains choices.

```go
type Choice struct {
    ID string

    Text string

    Conditions []Condition

    Effects []Effect

    NextNodeID string
}
```

Example:

```text
1. Enter the temple
2. Return to town
3. Follow the dragon tracks
```

---

# 7. Conditions

Conditions determine availability.

```go
type Condition interface{}
```

Examples:

```text
Relationship(Elara) >= 50

DragonAffinity >= 30

HasItem("DragonKey")

Flag("FoundTemple")
```

Hidden choices are not displayed.

Locked choices may optionally display:

```text
[Requires Dragon Affinity 30]
```

---

# 8. Effects

Effects modify state.

```go
type Effect interface{}
```

Examples:

```text
SetFlag

ClearFlag

ModifyRelationship

ModifyReputation

AddItem

RemoveItem

SetVariable

TriggerEvent
```

---

# 9. Relationship System

Every companion has a profile.

```go
type Relationship struct {
    Trust int
    Affection int
    Respect int
}
```

Range:

```text
-100 to +100
```

---

# 10. Relationship Events

Special scenes unlock automatically.

Example:

```text
Trust >= 50
```

Unlock:

```text
Elara reveals her past.
```

Example:

```text
Affection >= 75
```

Unlock:

```text
Romance route.
```

Example:

```text
Trust <= -50
```

Unlock:

```text
Betrayal route.
```

---

# 11. Reputation System

Tracks standing with factions.

```go
type ReputationState struct {
    Kingdom int
    Mages int
    Dragons int
    Underworld int
}
```

Range:

```text
-100 to +100
```

Used for:

* dialogue
* route access
* endings

---

# 12. Inventory System

Simple narrative inventory.

```go
type Inventory struct {
    Items map[string]int
}
```

No durability.

No encumbrance.

Items are story tools.

Examples:

```text
Dragon Sigil
Ancient Key
Royal Seal
Forbidden Tome
```

---

# 13. Event System

Events are authored scenes.

```go
type Event struct {
    ID string

    Conditions []Condition

    NodeID string
}
```

Events trigger automatically.

Examples:

```text
Companion confession

Dragon attack

Royal assassination

Ancient prophecy
```

---

# 14. Story Acts

The story is divided into acts.

## Act 1

The Awakening

Goal:

Discover the Dragon Relic.

Approx:

20 nodes

---

## Act 2

The Journey

Goal:

Gather allies.

Approx:

50 nodes

---

## Act 3

The Truth

Goal:

Discover the reality behind the Void Dragon.

Approx:

40 nodes

---

## Act 4

The War

Goal:

Choose the future of the world.

Approx:

50 nodes

---

## Act 5

Finale

Goal:

Resolve the world conflict.

Approx:

20 nodes

---

# 15. Character Selection

At game start:

```text
Kael
Lyra
Raven
Aria
```

Each protagonist has:

```go
type CharacterProfile struct {
    Name string

    StartingFlags []string

    StartingVariables map[string]int

    StartingInventory []string

    ExclusiveNodes []string
}
```

---

# 16. Story Flags

Flags represent major decisions.

Examples:

```text
JoinedDragons

SavedKing

UsedForbiddenMagic

RomancedElara

DestroyedSeal

FreedVoidDragon
```

Flags are permanent.

---

# 17. Variables

Variables represent continuous values.

Examples:

```text
DragonAffinity

Corruption

Courage

Fame

Honor
```

Range usually:

```text
0-100
```

---

# 18. Save System

Save file:

```go
type SaveGame struct {
    Version int

    WorldState WorldState
}
```

JSON serialization.

One file.

No migrations until v2.1.

---

# 18A. Determinism & Save Round-Trip

**Status:** Chronicle v2 determinism contract.

**Scope.** Unlike the v1 simulation, the v2 authored-content engine has
no runtime RNG in the player path. Choice resolution is deterministic
given the saved `WorldState` (a `SaveGame` JSON object) and the authored
story-graph content (`content/`). Narration text is the authored text on
the node — there is no procedural prose, no LLM prose, no template RNG.

**Determinism invariants.**

1. **Save/load round-trip.** `SaveGame → disk → SaveGame` is a literal
   byte-stable round trip. A load produces a `WorldState` that compares
   equal to the saved state under struct-equality after JSON
   normalize/canonicalize.

2. **Story traversal determinism.** Given a fixed `SaveGame` and fixed
   authored content, the engine produces the same `NodeID → ChoiceID →
   Effect[]` traversal every run. Authored content is content-addressed
   (hashed) at load; mismatches in content are a hard error, not silent
   divergence.

3. **Versioning and migration are explicit.** The save's `Version` field
   gates load-time migration. No silent migration. The migration code
   lives in `internal/content/migrate.go` (or equivalent v2 path).

**Forbidden patterns in v2 player-path code.**

- `math/rand` global functions.
- `time.Now()` as a state-mutating input.
- `crypto/rand` for game state (use only for non-game entropy like save
  ID generation).

**Forbidden patterns in v2 content.**

- LLM-generated prose embedded in node `Text` fields.
- Procedurally generated choices or effects.

**Hash (for reference).** The v2 save's `WorldHash(w)` is a SHA256 of the
canonicalized `SaveGame` JSON (sorted keys, normalized numbers). It is
used for save-load regression tests, NOT for cross-simulation
reproducibility (v2 has no simulation in the player path).

---

# 19. Ending System

Endings are evaluated at finale.

```go
type Ending struct {
    ID string

    Priority int

    Conditions []Condition
}
```

Highest valid ending wins.

---

# 20. Planned Endings

Hero Ending

Dragon Sovereign Ending

World Guardian Ending

Archmage Ending

Shadow Lord Ending

Corruption Ending

Kingdom Ending

Dragon Alliance Ending

Elara Romance Ending

Selene Romance Ending

Orion Romance Ending

Wanderer Ending

Total:

12 major endings.

---

# 21. Content Pipeline

Story content stored as data.

```text
content/

    protagonists/

    companions/

    acts/

        act1.yaml
        act2.yaml
        act3.yaml
        act4.yaml
        act5.yaml

    endings.yaml
```

---

# 22. Engine Architecture

```text
cmd/

internal/

    engine/

        runner.go

    story/

        nodes.go
        choices.go
        conditions.go
        effects.go

    state/

        world.go
        save.go

    events/

        events.go

    endings/

        endings.go

    content/

        loader.go

    ui/

        cli.go
```

> **Status (2026-06-17):** The current Chronicle codebase still implements
> the v1 simulation design (deterministic 7-engine tick loop, 150-NPC
> autonomous goal/relationship/memory/marriage/economy/event engines, an
> authorization layer, and a per-branch persistence layer). This
> `ARCHITECTURE.md` describes the v2 design that the codebase will be
> migrated to. The migration is tracked in `PHASES.md`. See
> `chronicle-v2-pivot-spec.md` for the binding plan behind this pivot.

---

# 23. Runtime Flow

```text
Load Save
        ↓
Load Node
        ↓
Evaluate Conditions
        ↓
Render Story
        ↓
Render Choices
        ↓
Player Selection
        ↓
Apply Effects
        ↓
Check Events
        ↓
Load Next Node
```

---

# 24. Testing Strategy

Unit Tests

* conditions
* effects
* relationship updates
* inventory updates
* ending evaluation

Integration Tests

* complete route playthroughs
* save/load consistency
* ending validation

Content Tests

* no broken node references
* no missing endings
* no impossible choices

---

# 25. Definition of Done

Chronicle v1 is complete when:

* Four protagonists implemented
* Five acts written
* Minimum 150 story nodes
* Minimum 300 choices
* Minimum 12 endings
* Romance routes implemented
* Save/load works
* All node references validate
* No AI dependency exists
* Full playthrough possible from start to finish

At that point Chronicle becomes a complete narrative RPG rather than a simulation experiment.
