# Choice + Condition + Effect + Event + Ending YAML Spec (v2.0)

> Status: canonical spec for the v2 conditional / effectual /
> event / ending surface area. The StoryNode + inline Choice
> shape lives in [docs/story-node-yaml.md](./story-node-yaml.md);
> this document focuses on **conditions**, **effects**,
> **events** (`events.yaml`), and **endings** (`endings.yaml`).
> All parsing is owned by `internal/story/yaml.go` per
> PHASES.md §37.A + §37.B.

A v2 authored world is conditional. Choices gate on conditions
(§7), mutate state via effects (§8), trigger events (§13), and
unlock endings (§19). This document specifies the YAML shape for
each of those four kinds, plus the canonical polymorphism that
unifies the condition and effect record formats.

---

## 1. Conditions: single-key polymorphism (reference)

Every condition is a **single-key YAML map**. The key names the
condition kind; the value is the kind's payload.

```yaml
conditions:
  - flag: "FoundTemple"                          # story.Flag
  - variable: { key: "Courage", value: 30 }      # story.VariableGE
  - relationship:                               # story.RelationshipGE
      character: "Elara"
      axis: trust            # trust | affection | respect
      value: 50
  - has_item: "DragonKey"                       # story.HasItem
  - has_ending: "hero"                          # story.HasEnding
  - or:                                         # story.Or (any-of)
      - flag: "A"
      - flag: "B"
  - and:                                        # story.And (all-of)
      - flag: "A"
      - flag: "B"
  - not:                                        # story.Not (negation)
      flag: "rested"
```

### Condition kind reference

| Kind             | Payload shape              | Range / semantics                                  |
|------------------|----------------------------|----------------------------------------------------|
| `flag`           | `<string>`                 | True iff `WorldState.Flags[Key]` is set.           |
| `variable`       | `{ key, value }`           | True iff `WorldState.Variables[key] >= value`.     |
| `relationship`   | `{ character, axis, value }` | True iff `Relationships[character].<axis> >= value`. Axis ∈ { trust, affection, respect }. |
| `has_item`       | `<string>`                 | True iff `WorldState.Inventory.Items[Key] > 0`.    |
| `has_ending`     | `<string>`                 | True iff `ID` is in `WorldState.EndingsUnlocked`.  |
| `or`             | list of single-key-map     | True iff at least one inner condition is true.    |
| `and`            | list of single-key-map     | True iff every inner condition is true.            |
| `not`            | single-key-map             | True iff inner condition is false.                |

A condition **must** have exactly one key. A condition with two
or more keys is a Load error.

### Combinators

- `or` and `and` take a YAML list of single-key conditions.
- `not` takes a single-key condition.
- An empty `or` is **false** (no path to true).
- An empty `and` is **true** (vacuous truth).

---

## 2. Effects: single-key polymorphism (reference)

Effects use the same single-key-map convention as conditions.
Effects apply in declaration order on Choice selection.

```yaml
effects:
  - set_flag: "opened_gate"                     # story.SetFlag
  - clear_flag: "stale_signal"                  # story.ClearFlag
  - modify_variable: { key: "Courage", value: 90 }   # story.ModifyVariable
  - modify_relationship:                        # story.ModifyRelationship
      character: "Elara"
      axis: affection       # trust | affection | respect
      value: 10             # clamped to [-100, +100]
  - modify_reputation:                          # story.ModifyReputation
      faction: kingdom       # kingdom | mages | dragons | underworld
      value: 5               # clamped to [-100, +100]
  - add_item:                                   # story.AddItem
      key: "Royal Seal"
      count: 1
  - remove_item:                                # story.RemoveItem
      key: "Torch"
      count: 1
  - trigger_event: "keeper_arrives"             # story.TriggerEvent
```

### Effect kind reference

| Kind                  | Payload shape              | Semantics                                   |
|-----------------------|----------------------------|---------------------------------------------|
| `set_flag`            | `<string>`                 | Sets `WorldState.Flags[Key] = true`.        |
| `clear_flag`          | `<string>`                 | Removes `WorldState.Flags[Key]`.            |
| `modify_variable`     | `{ key, value }`           | Sets `WorldState.Variables[key] = value`.   |
| `modify_relationship` | `{ character, axis, value }` | Sets `Relationships[character].<axis> = clamp(value)`. |
| `modify_reputation`   | `{ faction, value }`       | Sets `Reputation.<faction> = clamp(value)`. Faction ∈ { kingdom, mages, dragons, underworld }. |
| `add_item`            | `{ key, count }`           | Increments `Inventory.Items[Key]` by `count`. `count < 0` errors. |
| `remove_item`         | `{ key, count }`           | Decrements `Inventory.Items[Key]` by `count` (clamped at 0). |
| `trigger_event`       | `<string>`                 | Appends ID to `WorldState.TriggeredEvents` for Phase 36.D's consumer. |

> **Clamp semantics.** Both `modify_relationship` and
> `modify_reputation` clamp `value` to `[-100, +100]` per
> ARCHITECTURE.md §9 + §11. Authors may write generous values;
> the runtime clamps before mutating.

> **First-failure stops the chain.** The first effect whose
> `Apply` returns an error halts the chain (Phase 36.A's
> no-rollback contract). A future phase may add a transactional
> snapshot for atomic multi-effect choices.

---

## 3. Events YAML (`events.yaml`)

Events are authored scenes that auto-fire when their conditions
match (§13). They're queued by `trigger_event: <id>` effects and
consumed at Step's "Check Events" stage by Phase 36.D's
`internal/events.Trigger`.

```yaml
events:
  - id: "ally_call"
    node_id: "act1.ally_appears"
    conditions:
      - flag: "began_journey"

  - id: "dragon_stirs"
    node_id: "act2.dragon_reveal"
    # No conditions: always fires when queued.
```

### Event field reference

| Field         | Required | Type | Notes                                            |
|---------------|----------|------|--------------------------------------------------|
| `id`          | ✓        | str  | Stable identifier. Unique within `events.yaml`.  |
| `node_id`     | ✓        | str  | Redirect target when event fires. `""` allowed for a "pure sidebar" event (no redirect). |
| `conditions`  |          | list | Optional gating. See §1. Empty list = always fires when queued. |

> **Deterministic ordering.** Two events whose conditions both
> pass in the same Step are resolved by queue insertion order
> (the order their `TriggerEvent` effects were applied on the
> chosen Choice on this Step). The first match wins; later
> entries are not considered for the redirect.

---

## 4. Endings YAML (`endings.yaml`)

Endings are evaluated at the finale (§19). The engine evaluates
every entry in priority order; highest-priority valid ending
wins, ties broken by ID lexicographic order (§18A).

```yaml
endings:
  - id: "hero"
    priority: 1
    conditions:
      - flag: "mid_completed"

  - id: "elara_romance"
    priority: 5
    conditions:
      - relationship: { character: "Elara", axis: affection, value: 75 }

  - id: "fallback"
    priority: 0
    # No conditions = always valid (a §19 "default" pattern).
```

### Ending field reference

| Field         | Required | Type | Notes                                            |
|---------------|----------|------|--------------------------------------------------|
| `id`          | ✓        | str  | Stable identifier. Unique within `endings.yaml`. |
| `priority`    | ✓        | int  | Higher priority wins. Ties broken by ID order.    |
| `conditions`  |          | list | Optional gating. See §1. Empty list = always valid. |

Three v2 ending categories per ARCHITECTURE.md §20: Hero,
Dragon Sovereign, World Guardian, Archmage, Shadow Lord,
Corruption, Kingdom, Dragon Alliance, Elara Romance, Selene
Romance, Orion Romance, Wanderer. Of those, three are romance
variants (Elara/Selene/Orion) and the rest are non-romance.
A complete v2 world ships 12 reachable endings; PHASES.md §38.E
gates `TestAllEndingsReachable` against this count.

---

## 5. Parser contract (Phase 37.A + §37.B)

Phase 37.A introduced the **"one canonical parser per file type,
in the package that owns the return type"** rule, so the spec
doc, the schema test, and the production loader cannot drift
apart. Three public APIs:

- `LoadStoryNodes([]byte) ([]StoryNode, error)` — lives in
  `internal/story/yaml.go` (Phase 37.A).
- `EventsFromYAML([]byte) ([]Event, error)` — lives in
  `internal/story/yaml.go` (Phase 37.B).
- `EndingsFromYAML([]byte) ([]endings.Ending, error)` — lives
  in `internal/endings/yaml.go` (Phase 37.B). Placed in the
  endings package rather than story to avoid the import cycle
  `content → endings → story → endings`; endings already imports
  story via `story.Condition`, so the reverse direction is safe.

Two public helpers, both in `internal/story/yaml.go`, used by
all three parsers:

- `UnmarshalCondition(map[string]any) (Condition, error)` —
  single-key-map dispatch for the 8 condition kinds.
- `UnmarshalEffect(map[string]any) (Effect, error)` —
  single-key-map dispatch for the 8 effect kinds.

### Fail-fast error contract

Every authoring mistake is loud:

| Authoring mistake                              | Error                                                                       |
|------------------------------------------------|-----------------------------------------------------------------------------|
| YAML unparseable                               | `EventsFromYAML: parse: <yaml.v3 error>` / `EndingsFromYAML: parse: ...`    |
| Event / ending with empty `id`                 | `story: <file>: <kind>[i] has empty id`                                      |
| Condition with multiple keys                   | `condition expected single-key map (got N keys)`                             |
| Effect with multiple keys                      | `effect expected single-key map (got N keys)`                                |
| Unknown condition kind                         | `unknown condition kind "<key>"`                                             |
| Unknown effect kind                            | `unknown effect kind "<key>"`                                                |
| Unknown relationship axis                     | `relationship.axis "<axis>" unrecognized (want trust/affection/respect)`    |
| Unknown faction in modify_reputation           | `modify_reputation.faction "<x>" unrecognized (want kingdom/mages/...)`     |

`internal/content/loader.go` is the **only** production caller
of all three parsers (PHASES.md §36.E was Phase 37.A; the events
+ endings migration to `EventsFromYAML`/`EndingsFromYAML` lands
in Phase 37.B). Drift between doc, test, and production is
impossible by construction: there's exactly one parser for each
file type.

---

## 6. Cross-references

- [docs/story-node-yaml.md](./story-node-yaml.md) — StoryNode +
  inline Choice shape (where conditions + effects live).
- [ARCHITECTURE.md §6 Choice System](../ARCHITECTURE.md)
- [ARCHITECTURE.md §7 Conditions](../ARCHITECTURE.md)
- [ARCHITECTURE.md §8 Effects](../ARCHITECTURE.md)
- [ARCHITECTURE.md §13 Event System](../ARCHITECTURE.md)
- [ARCHITECTURE.md §19 Ending System](../ARCHITECTURE.md)
- [PHASES.md §37.B](../PHASES.md)
- [internal/story/yaml.go](../internal/story/yaml.go) (`LoadStoryNodes`, `EventsFromYAML`)
- [internal/endings/yaml.go](../internal/endings/yaml.go) (`EndingsFromYAML`)
- [internal/story/nodes_test.go](../internal/story/nodes_test.go)
- [internal/story/events_endings_test.go](../internal/story/events_endings_test.go)
- [internal/endings/yaml_test.go](../internal/endings/yaml_test.go)
- [internal/content/loader.go](../internal/content/loader.go)
