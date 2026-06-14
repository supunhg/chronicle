# ARCHITECTURE.md

## Chronicle

### Vision

Chronicle is not a game.

Chronicle is a persistent reality simulation engine.

Players do not complete quests.

Players inhabit living worlds populated by autonomous individuals who remember, form relationships, pursue goals, create history, and continue existing long after the player leaves or dies.

The engine is responsible for reality.

LLMs are responsible only for interpretation and narration.

The simulation must remain valid even if all AI text generation is removed.

---

# Core Design Principles

## 1. Simulation First

World state is the source of truth.

Never allow an LLM to invent facts.

Bad:

Player: "Am I married?"

LLM: "Yes."

Good:

Simulation:
marriage_exists = true

LLM:
Generates narration based on state.

---

## 2. Persistent Worlds

Worlds never reset automatically.

Worlds continue evolving indefinitely.

A player may die.

The world survives.

Future generations inherit consequences.

---

## 3. Story Emergence

No mandatory main quest.

No predefined hero.

No chosen one.

Stories emerge naturally from:

* relationships
* politics
* economy
* disasters
* wars
* inventions
* ambitions

---

## 4. Deterministic Simulation

World updates must be reproducible.

Given:

* identical world state
* identical seed
* identical actions

The simulation should produce identical results.

---

## 5. World Pack Architecture

Engine must be genre agnostic.

Fantasy, cyberpunk, post-apocalypse and space opera should all run on the same simulation core.

Genre-specific content belongs in world packs.

---

# High Level Architecture

Player

↓

CLI Interface

↓

Command Pipeline

↓

Simulation Engine

├── World Engine
├── Population Engine
├── Relationship Engine
├── Event Engine
├── Economy Engine
├── Timeline Engine
├── Memory Engine
└── Persistence Layer

↓

Narration Layer (LLM)

---

# System Overview

## Simulation Layer

Responsible for reality.

Contains:

* entities
* locations
* relationships
* history
* world state

Never calls LLMs.

---

## Narration Layer

Responsible for:

* descriptions
* dialogue
* atmosphere
* storytelling

Receives simulation facts.

Cannot modify world state.

---

# Domain Model

## World

World {
ID
Name
Seed

CurrentTime

Config

Regions
Locations

Factions

Population

History

Metadata
}

---

## Person

Person {
ID

Name
Age

Gender

Occupation

Location

Traits

Goals

Resources

Relationships

Memories

Health

Status

Alive
}

---

## Relationship

Relationship {
TargetID

Trust
Respect
Fear
Attraction
Loyalty

History
}

---

## Memory

Memory {
ID

Timestamp

Importance

Participants

Description

Tags
}

---

## Location

Location {
ID

Name

Region

Population

Economy

Buildings

Resources
}

---

## Faction

Faction {
ID

Name

Goals

Resources

Members

Reputation

Relations
}

---

# Simulation Engines

## Population Engine

Responsibilities:

* births
* deaths
* aging
* migration
* marriages
* family trees

Tick Frequency:

Daily

---

## Relationship Engine

Responsibilities:

* friendship
* romance
* rivalries
* betrayal
* trust evolution

Tick Frequency:

Daily

---

## Goal Engine

Each person owns goals.

Examples:

Become wealthy

Find spouse

Gain power

Start business

Learn magic

Goals generate actions.

Actions affect world state.

---

## Economy Engine

Tracks:

* resources
* production
* trade
* shortages
* inflation

Initial implementation should remain lightweight.

---

## Event Engine

Generates events.

Examples:

Fire

Famine

Crime

Political unrest

Religious movements

War

Events are simulation outcomes.

Not scripted stories.

---

## Memory Engine

Stores meaningful experiences.

Every memory has:

importance score

recency score

emotional score

Only significant memories persist indefinitely.

Minor memories decay.

---

## Timeline Engine

Supports branching realities.

Example:

chronicle branch before_war

Creates alternate timeline.

World history diverges.

Inspired conceptually by Git branching.

---

# World Packs

Structure:

worldpacks/

fantasy/
cyberpunk/
space/
apocalypse/

Each pack defines:

rules.yaml

entities.yaml

factions.yaml

events.yaml

occupations.yaml

generation.yaml

The engine loads packs dynamically.

---

# Persistence

Structure:

worlds/

kingdom_of_ashes/

world.db

history.log

config.yaml

timeline/

Worlds are self-contained.

Can be copied, shared and forked.

---

# Database

SQLite

Primary storage.

Reasons:

* single file
* portable
* reliable
* easy backups

Potential migration path:

PostgreSQL

Not required for v1.

---

# LLM Integration

The LLM never updates state.

Allowed:

* narration
* dialogue generation
* summarization
* memory compression

Forbidden:

* world mutations
* relationship updates
* event generation

LLM Output:

Narrative only.

---

# Command Pipeline

Player Input

↓

Intent Extraction

↓

Action Validation

↓

Simulation Update

↓

Event Resolution

↓

Narrative Generation

↓

Response

---

Example:

Player:
"Ask Elena to marry me."

Pipeline:

Intent:
ProposeMarriage

Simulation:
Evaluate relationship

Result:
Accepted

Narration:
Generated by LLM

---

# V1 Scope

One settlement.

100 NPCs.

Daily simulation.

Relationships.

Memory.

Births.

Deaths.

Natural language commands.

Single world pack.

SQLite persistence.

Branching timelines.

CLI interface.

No combat.

No magic.

No crafting.

No quests.

No multiplayer.

---

# Success Criteria

A player should be able to:

Start as a nobody.

Live decades.

Form meaningful relationships.

Influence society.

Leave descendants.

Die.

Observe the world continue without them.

Generate stories worth remembering.

If this is achieved, Chronicle succeeds.
