# Companion YAML Spec (v2.0)

> Status: canonical spec for the v2 companion roster
> (`companions.yaml`). StoryNode + Choice shape lives in
> [docs/story-node-yaml.md](./story-node-yaml.md); Condition /
> Effect / Event / Ending shapes live in
> [docs/choice-yaml.md](./choice-yaml.md). This document
> focuses on **companions** specifically.
>
> Parsing is owned by `internal/story/yaml.go` per
> PHASES.md §37.A + §37.C and the "one canonical parser per
> file type, in the package that owns the return type" rule.

A v2 authored world optionally ships a **companion roster**.
Companions are the named characters the protagonist can
acquire (via authored choices, romance arcs, or starting
party). They govern relationship-driven events in §10
(Trust / Affection / Respect deltas with named NPCs) and
the romance variants of §20's twelve endings.

`companions.yaml` is **optional** per PHASES.md §36.E: a
content directory with no companions.yaml is valid as long
as no protagonist's `starting_party` references a missing
companion.

---

## 1. Companion YAML Schema (`companions.yaml`)

```yaml
companions:
  - id: "Elara"
    description: "A ranger from the eastern wood."

  - id: "Selene"
    description: "An elven mage exiled from the crystal spire."

  - id: "Orion"
    description: "A grizzled mercenary captain."
```

### Companion field reference (Phase 37.C)

| Field         | Required | Type | Notes                                            |
|---------------|----------|------|--------------------------------------------------|
| `id`          | ✓        | str  | Stable identifier. Unique across `companions.yaml`. Referenced by `protagonists.yaml::starting_party` and by `nodes.yaml::next_node_id` patterns (e.g. `kael.companion_elara_origin`). |
| `description` |          | str  | Short tooltip / selection label. Free-text.      |

### Future-extension tolerance

Phase 38 (companion depth) is expected to add the following
fields, and the Phase 37.C parser must **tolerate them
silently** so that adding them is additive:

- `backstory_node: "<node-id>"` — first node visited
  alongside this companion.
- `romance_axis: trust | affection | respect` — the
  primary relationship axis the romance variant tracks
  for this companion.
- `romance_threshold: <int>` — the relationship value at
  which the romance variant's conditions unlock.

The parser uses `gopkg.in/yaml.v3`'s default (lenient) mode,
which silently drops unknown fields. Pin this behavior with
the schema test `TestCompanionsFromYAML_UnknownFieldTolerated`.

---

## 2. Parser contract (Phase 37.C)

Phase 37.C extends `internal/story/yaml.go` with the public
`CompanionsFromYAML` API. The companion `Companion` type
also lands in the story package (not content) so the parser
and the type it produces share one canonical home:

- `CompanionsFromYAML([]byte) ([]Companion, error)` — lives
  in `internal/story/yaml.go` (Phase 37.C). Returns the
  parsed companions in input order.

Two public helpers, both in `internal/story/yaml.go`, used
by the companion-bearing and choice-bearing parsers (NOT
by the companion parser itself):

- `UnmarshalCondition(map[string]any) (Condition, error)` —
  single-key-map dispatch for the 8 condition kinds.
- `UnmarshalEffect(map[string]any) (Effect, error)` —
  single-key-map dispatch for the 8 effect kinds.

---

## 3. Fail-fast error contract

Every authoring mistake is loud:

| Authoring mistake                              | Error                                                                       |
|------------------------------------------------|-----------------------------------------------------------------------------|
| YAML unparseable                               | `CompanionsFromYAML: parse: <yaml.v3 error>`                               |
| Companion with empty `id`                      | `story: CompanionsFromYAML: companion[i] has empty id`                    |
| Duplicate `id` across companions              | `story: CompanionsFromYAML: companion "<id>" duplicated`                   |

By convention: error messages start with the canonical
parser's full symbol name (`story: CompanionsFromYAML:
...`) so production logs can pivot straight to the spec
doc.

---

## 4. Companion → protagonist (`starting_party`) wire

`internal/content/loader.go::validatePartyCompanions`
runs after both files are parsed; it ensures every entry in
`protagonists.yaml::[*].starting_party` matches a
`companions.yaml::[*].id`. The mapping is keyed by `id`.
The loader therefore keys the parsed slice into a
`map[string]story.Companion` for O(1) lookup. The loader
itself owns NO companion-validation logic — the parser
owns the duplicate-id gate; the loader owns ONLY the file
I/O + the cross-reference gate.

This split honors the §37.A single-canonical-parser rule:
companion parsing logic lives in exactly one place
(`internal/story/yaml.go`); cross-reference logic lives in
exactly one place (`internal/content/loader.go::validate*`).

---

## 5. Cross-references

- [docs/story-node-yaml.md](./story-node-yaml.md) —
  StoryNode + inline Choice shape (companions reference
  `backstory_node` IDs via `next_node_id`).
- [docs/choice-yaml.md](./choice-yaml.md) — Condition /
  Effect / Event / Ending shapes (no companion-specific
  polymorphism; companions are passive, gated by
  starting_party + §10 relationship events).
- [ARCHITECTURE.md §10 Relationship Events](../ARCHITECTURE.md)
- [ARCHITECTURE.md §15 Character Profile](../ARCHITECTURE.md)
- [ARCHITECTURE.md §20 Endings](../ARCHITECTURE.md)
- [PHASES.md §37.C](../PHASES.md)
- [PHASES.md §38.B Companion roster authoring](../PHASES.md)
- [internal/story/yaml.go](../internal/story/yaml.go) —
  `LoadStoryNodes`, `EventsFromYAML`, `CompanionsFromYAML`.
- [internal/endings/yaml.go](../internal/endings/yaml.go) —
  `EndingsFromYAML`.
- [internal/story/nodes_test.go](../internal/story/nodes_test.go)
- [internal/story/events_endings_test.go](../internal/story/events_endings_test.go)
- [internal/endings/yaml_test.go](../internal/endings/yaml_test.go)
- [internal/story/companions_test.go](../internal/story/companions_test.go)
- [internal/content/loader.go](../internal/content/loader.go)
