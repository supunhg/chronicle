# Story Node + Choice YAML Spec (v2.0)

> Status: canonical spec for `internal/story/yaml.go`'s
> `LoadStoryNodes` (PHASES.md §37.A). The content loader
> (`internal/content/loader.go`, Phase 36.E) reads a directory
> of YAML files matching this schema; the production engine
> reads the resulting `*story.Graph` and `[]Story.Choice` records
> (ARCHITECTURE.md §5–§8).

A Chronicle v2 authored world lives in a directory whose top-level
files each serialize one slice of the v2 model. The names of the
files (`nodes.yaml`, `events.yaml`, `endings.yaml`,
`protagonists.yaml`, optional `companions.yaml`) are part of the
contract — writers do not pick freely.

This document specifies the **StoryNode + Choice** shape that lives
in `nodes.yaml`. Other files have their own specs (PHASES.md
§37.B–§37.E).

---

## Top-level layout

```yaml
nodes:
  - id: "act1.dragon_temple_entrance"          # required, stable string
    title: "EX01 — The frontier town of Ashwick"   # required
    text: "..."                                # required, prose body
    is_final: false                            # optional, default false
    choices:                                   # required (can be empty for is_final=true)
      - { id: "enter", text: "Enter the temple.", next_node_id: "act1.dragon_temple_inside" }
      - { id: "wait",  text: "Wait and watch.",   next_node_id: "act1.dragon_temple_wait" }
    events: []                                  # optional, default []
```

### Field reference

| Field      | Required | Type       | Notes                                             |
|------------|----------|------------|---------------------------------------------------|
| `id`       | ✓        | string     | Stable identifier. Must match `[a-z0-9_.]+`.      |
| `title`    | ✓        | string     | Heading printed above `Text`. Cannot be empty.    |
| `text`     | ✓        | string     | Authored prose. Empty is allowed (headline-only). |
| `is_final` |          | bool       | Marks a terminal node. Default `false`.           |
| `choices`  | ✓        | list       | Each entry matches the **Choice shape** below.    |
| `events`   |          | list       | Per-node authored events (Phase 38+). Each entry  |
|            |          |            | has `id`, `node_id`, optional `conditions`.      |

A node's `choices` list **must** be empty when `is_final: true`.
The loader rejects non-empty `choices` on final nodes with a
clear error (Phase 36.E fail-fast contract; §24 "no impossible
choices").

---

## Choice shape (inline)

Choices live under `nodes[].choices`. Each choice is a record:

```yaml
- id: "open_gate"
  text: "Push the gate open."
  # all of the following are optional:
  conditions:
    - flag: "spoke_to_keeper"
    - variable: { key: "Courage", value: 30 }
  effects:
    - set_flag: "opened_gate"
    - modify_variable: { key: "Courage", value: 50 }
    - trigger_event: "keeper_arrives"
  next_node_id: "act2.gate_passage"
```

| Field           | Required | Type | Notes                                       |
|-----------------|----------|------|---------------------------------------------|
| `id`            | ✓        | str  | Stable choice identifier.                   |
| `text`          | ✓        | str  | Display prose.                              |
| `conditions`    |          | list | Optional gating. Empty means always available. |
| `effects`       |          | list | Applied in declaration order on selection.  |
| `next_node_id`  | ✓        | str  | Must reference an existing `nodes[].id`.    |

Empty `conditions` means *always available* (passes
`AvailableChoices` filtering unconditionally). Empty `effects`
is allowed but mechanical-only — the choice then functions as
a pure redirect.

---

## Conditions: single-key polymorphism

Every condition is a **single-key YAML map**. The key names the
condition kind; the value is the kind's payload. This convention
keeps conditions compact while still being strict about which
keywords are accepted.

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

A condition **must** have exactly one key. A condition with two
or more keys is a Load error (`Condition expected single-key map
(got N keys)`). This protects against typos like a condition that
silently drops one half of its intent.

### Combinators (or / and / not)

`or` and `and` take a YAML list of single-key conditions as their
value. `not` takes a single-key condition as its value.

An empty `or` is always **false** (no path to true). An empty
`and` is vacuously **true** (no path to false). Authoring an
empty combinator is loud at parse time only when it's malformed;
use the empty lists deliberately when the conditional "thread"
is implicit (you usually do not want this).

---

## Effects: single-key polymorphism (parallel shape)

Effects use the same single-key-map convention as conditions:

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

Effects apply in declaration order. The first effect's failure
cancels the chain (Phase 36.A's no-rollback contract); future
phases may add a transactional snapshot for multi-effect choices.

---

## Canonical example: `EX01 — The frontier town of Ashwick`

This file matches the **README** "In-Game Flow" sample session
and is the canonical Phase 37.A acceptance fixture:

```yaml
nodes:
  - id: "act1.ashwick_entrance"
    title: "EX01 — The frontier town of Ashwick"
    text: |
      You stand at the threshold of Greyhall Keep. The wind
      carries the smell of woodsmoke and something older —
      incense, or the memory of incense, from the ruins on
      the eastern ridge.
    is_final: false
    choices:
      - id: "enter_keep"
        text: "Enter the keep."
        next_node_id: "act1.greyhall_keep"
        effects:
          - set_flag: "entered_keep"
      - id: "head_east"
        text: "Head for the eastern ridge."
        next_node_id: "act1.eastern_ridge"
        conditions:
          - variable: { key: "Courage", value: 30 }
      - id: "wait_watch"
        text: "Wait in the square and watch."
        next_node_id: "act1.wait_at_square"

  - id: "act1.greyhall_keep"
    title: "Inside Greyhall Keep"
    text: "The hall is quieter than it should be."
    choices:
      - id: "speak_to_keeper"
        text: "Speak to the keeper."
        next_node_id: "act1.keeper_interview"

  - id: "act1.eastern_ridge"
    title: "The Eastern Ridge"
    text: "Old ruins stretch along the ridge."
    choices:
      - id: "investigate_ruins"
        text: "Investigate the ruins."
        next_node_id: "act1.void_dragon_reveal"
        effects:
          - modify_variable: { key: "Courage", value: 60 }
          - trigger_event: "dragon_stirs"
```

This fixture is the basis for `internal/story/nodes_test.go`'s
`TestLoadStoryNodes_CanonicalExample`. It exercises:
- `is_final: false` with multiple choices
- A choice gated by a `variable` condition
- Effects across multiple kinds
- A `TriggerEvent` calling an event ID that must exist in
  the production `events.yaml`

---

## Parser contract (Phase 37.A)

The canonical parser is **`internal/story.LoadStoryNodes([]byte)
([]StoryNode, error)`**. The contract:

- **Input**: raw YAML bytes from `nodes.yaml`.
- **Output**: an ordered slice of `StoryNode` values matching
  in declaration order; never `nil` (zero-length for empty input).
- **Errors**:
  - YAML parse error → wrapped `yaml.v3` error.
  - Empty `id` → `node has empty id`.
  - Empty `id` on a choice → `choice has empty id`.
  - Condition/effect with multiple keys → `single-key map (got N keys)`.
  - Unknown condition/effect kind → `unknown condition kind` or
    `unknown effect kind` with the offending key quoted.
- **No silent divergence**: every author mistake is loud and
  names the exact field.

`internal/content/loader.go::readNodes` is the only production
caller; `internal/story/nodes_test.go` is the canonical test
fixture. There is no other StoryNode parser in the v2 codebase
as of Phase 37.A.

---

## Cross-references

- [ARCHITECTURE.md §5 Story Structure](../ARCHITECTURE.md)
- [ARCHITECTURE.md §6 Choice System](../ARCHITECTURE.md)
- [ARCHITECTURE.md §7 Conditions](../ARCHITECTURE.md)
- [ARCHITECTURE.md §8 Effects](../ARCHITECTURE.md)
- [PHASES.md §37.A](../PHASES.md)
- [README.md "In-Game Flow" sample](../README.md)
- [internal/story/yaml.go](../internal/story/yaml.go)
- [internal/story/nodes_test.go](../internal/story/nodes_test.go)
