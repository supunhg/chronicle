# Changelog

All notable changes to this project are documented in this file.

## [0.1.1] — 2026-06-18

### Added
- Player-facing README with build, play, save/load, and project-layout instructions.
- MIT LICENSE.
- `play` CLI subcommand: interactive (`-protagonist`) and scripted (`-script`) playthrough modes.
- `save`, `resume`, `info`, and `diff` CLI subcommands for JSON save-game round-trips.
- `TestPlaythroughScriptedSpine` regression test that walks each protagonist's committed script through the engine.

### Story — Acts 1–3 (The Frontier)
- **4 protagonists** (Kael, Lyra, Raven, Aria) with unique openings and exclusive scenes.
- **~43 story nodes** across three acts.
- **12 endings** reachable from all 4 protagonists:
  - 8 non-romance endings (wanderer, hero, dragon_alliance, kingdom, archmage, shadow_lord, world_guardian, corruption, dragon_sovereign)
  - 3 romance endings (elara_romance, selene_romance, orion_romance)
- **3 romance routes** with relationship-conditioning scenes in Act 3.
- **4 factions** (Kingdom, Mages, Dragons, Underworld) with reputation-driven choices.

### Engine
- Deterministic narrative state machine: no RNG, no LLM, no procedural prose.
- Authored-content YAML loader with fail-fast reference validation.
- `StoryNode` + `Choice` + `Condition` + `Effect` type system covering all 8 condition types and all 8 effect types.
- Event trigger system with automatic redirect handling.
- Ending evaluator with priority-ordered resolution (romance 9–11 beats realm-claim 1–8).
- JSON save/load with versioning, canonical encoding, and resilience gates for malformed/tampered input.

### Tests
- `TestAllEndingsReachable` — verifies all 48 (protagonist × ending) combinations.
- `TestRomanceRoutesWired` — verifies romance endings win priority over realm claims.
- `TestProtagonistCoverage`, `TestEndingCoverage`, `TestConditionCoverage`, `TestEffectCoverage` — schema coverage gates.
- `TestSaveLoadResilience` — malformed-input handling.
- Full short-test sweep green across all packages.

### Removed
- v1 simulation engines (Population, Relationship, Marriage, Memory, Goal, Economy, Event).
- LLM stack (`internal/llm`, `internal/narrator`, `internal/intent`).
- SQLite persistence layer (`modernc.org/sqlite`).
- Free-text REPL and intent parser.
